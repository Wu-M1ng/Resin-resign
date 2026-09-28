package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Resinat/Resin/internal/outbound"
	"github.com/Resinat/Resin/internal/routing"
)

const (
	socks5Version                 = 0x05
	socks5MethodNoAuth            = 0x00
	socks5MethodUserPass          = 0x02
	socks5MethodNoAcceptable      = 0xFF
	socks5CommandConnect          = 0x01
	socks5AddressTypeIPv4         = 0x01
	socks5AddressTypeDomain       = 0x03
	socks5AddressTypeIPv6         = 0x04
	socks5ReplySucceeded          = 0x00
	socks5ReplyGeneralFailure     = 0x01
	socks5ReplyCommandUnsupported = 0x07
	socks5ReplyAddressUnsupported = 0x08
	socks5UserPassVersion         = 0x01
	socks5UserPassStatusSuccess   = 0x00
	socks5UserPassStatusFailure   = 0x01
)

var socks5HandshakeTimeout = 15 * time.Second

// RelayCredential describes the endpoint and lifecycle scope of a Feed token.
// The resolver is the authorization boundary for relay credentials; callers
// must not replace these fields with values supplied by the client.
type RelayCredential struct {
	PlatformName string
	RelayPort    int
	RequireTLS   bool
	Revoked      <-chan struct{}
}

// RelayCredentialResolver validates a scoped Feed token. It is intentionally
// separate from the global proxy token used by the regular HTTP/SOCKS5
// interfaces.
type RelayCredentialResolver func(token string) (RelayCredential, bool)

// Socks5InboundConfig holds dependencies for the SOCKS5 inbound handler.
type Socks5InboundConfig struct {
	ProxyToken              string
	RelayCredentialResolver RelayCredentialResolver
	Router                  *routing.Router
	Pool                    outbound.PoolAccessor
	Health                  HealthRecorder
	Events                  EventEmitter
	MetricsSink             MetricsEventSink
	ProxyBypassRules        []string
}

// Socks5Inbound implements SOCKS5 CONNECT over a raw TCP connection.
type Socks5Inbound struct {
	token                   string
	relayCredentialResolver RelayCredentialResolver
	tunnel                  tunnelDeps
	events                  EventEmitter
}

type socks5HandshakeResult struct {
	platformName    string
	account         string
	target          string
	relayCredential *RelayCredential
	ok              bool
}

// NewSocks5Inbound creates a new SOCKS5 inbound handler.
func NewSocks5Inbound(cfg Socks5InboundConfig) *Socks5Inbound {
	ev := cfg.Events
	if ev == nil {
		ev = NoOpEventEmitter{}
	}
	return &Socks5Inbound{
		token:                   cfg.ProxyToken,
		relayCredentialResolver: cfg.RelayCredentialResolver,
		tunnel: tunnelDeps{
			router:      cfg.Router,
			pool:        cfg.Pool,
			health:      cfg.Health,
			metricsSink: cfg.MetricsSink,
			bypass:      NewTargetBypassMatcher(cfg.ProxyBypassRules),
		},
		events: ev,
	}
}

// ServeConn handles a SOCKS5 session on an already-accepted TCP connection.
func (s *Socks5Inbound) ServeConn(conn net.Conn) {
	s.ServeConnContext(context.Background(), conn)
}

// ServeConnContext handles a SOCKS5 session with a caller-provided base context.
func (s *Socks5Inbound) ServeConnContext(baseCtx context.Context, conn net.Conn) {
	if conn == nil {
		return
	}
	defer conn.Close()
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	reader := bufio.NewReader(conn)

	handshakeCtx, cancelHandshake := context.WithTimeout(baseCtx, socks5HandshakeTimeout)
	defer cancelHandshake()
	handshakePhase := startSocks5HandshakePhase(handshakeCtx, conn)
	defer handshakePhase.Stop()

	policy := InboundPolicyFromContext(baseCtx)
	requireAuthInfo := policy.RequireProxyAuthInfo && s.token == ""
	endpoint, endpointSet := InboundEndpointFromContext(baseCtx)
	handshake := s.performHandshake(conn, reader, requireAuthInfo, endpoint, endpointSet)
	if !handshake.ok {
		return
	}
	handshakePhase.Stop()

	lifecycle := newRequestLifecycleFromMetadata(
		s.events,
		conn.RemoteAddr().String(),
		"",
		ProxyTypeSocks5Forward,
		true,
	)
	lifecycle.setTarget(handshake.target, "")
	lifecycle.setAccount(handshake.account)
	defer lifecycle.finish()

	sessionCtx := baseCtx
	stopRelayWatcher := func() {}
	if handshake.relayCredential != nil {
		sessionCtx, stopRelayWatcher = watchRelayRevocation(sessionCtx, conn, handshake.relayCredential.Revoked)
	}
	defer stopRelayWatcher()
	prepare := prepareConnectTunnel(
		sessionCtx,
		s.tunnel,
		handshake.platformName,
		handshake.account,
		handshake.target,
	)
	if prepare.route.PlatformID != "" {
		lifecycle.setRouteResult(prepare.route)
	}
	if prepare.session == nil {
		if prepare.proxyErr != nil {
			lifecycle.setProxyError(prepare.proxyErr)
			if prepare.upstreamStage != "" {
				lifecycle.setUpstreamError(prepare.upstreamStage, prepare.upstreamErr)
			}
			lifecycle.setNetOK(false)
			_ = writeSocks5Reply(conn, socks5ReplyGeneralFailure, nil)
		} else if prepare.canceled {
			lifecycle.setNetOK(true)
		}
		return
	}

	if err := writeSocks5Reply(conn, socks5ReplySucceeded, prepare.session.upstreamConn.LocalAddr()); err != nil {
		prepare.session.upstreamConn.Close()
		lifecycle.setProxyError(ErrUpstreamRequestFailed)
		lifecycle.setUpstreamError("socks5_connect_response_write", err)
		lifecycle.setNetOK(false)
		return
	}

	relay := pumpPreparedTunnel(conn, reader, prepare.session, tunnelPumpOptions{
		onFirstIngressByte: lifecycle.markFirstByteReceived,
	})
	lifecycle.addIngressBytes(relay.ingressBytes)
	lifecycle.addEgressBytes(relay.egressBytes)
	if relay.proxyErr != nil {
		lifecycle.setProxyError(relay.proxyErr)
		lifecycle.setUpstreamError(relay.upstreamStage, relay.upstreamErr)
	}
	lifecycle.setNetOK(relay.netOK)
	prepare.session.recordResult(relay.netOK)
}

func (s *Socks5Inbound) performHandshake(conn net.Conn, reader *bufio.Reader, requireAuthInfo bool, endpoint InboundEndpoint, endpointSet bool) socks5HandshakeResult {
	method, ok := s.negotiateMethod(conn, reader, requireAuthInfo)
	if !ok {
		return socks5HandshakeResult{}
	}

	result := socks5HandshakeResult{ok: true}
	if method == socks5MethodUserPass {
		var authOK bool
		result.platformName, result.account, result.relayCredential, authOK = s.authenticateUserPassWithEndpoint(conn, reader, requireAuthInfo, endpoint, endpointSet)
		if !authOK {
			return socks5HandshakeResult{}
		}
	}

	target, replyCode, ok := readSocks5ConnectRequest(reader)
	if !ok {
		if replyCode != 0 {
			_ = writeSocks5Reply(conn, replyCode, nil)
		}
		return socks5HandshakeResult{}
	}

	result.target = target
	return result
}

func watchRelayRevocation(ctx context.Context, conn net.Conn, revoked <-chan struct{}) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	if revoked == nil {
		return sessionCtx, cancel
	}
	stop := make(chan struct{})
	var stopOnce sync.Once
	stopWatcher := func() {
		stopOnce.Do(func() {
			close(stop)
			cancel()
		})
	}
	go func() {
		select {
		case <-revoked:
			cancel()
			if conn != nil {
				_ = conn.Close()
			}
		case <-stop:
		}
	}()
	return sessionCtx, stopWatcher
}

type socks5HandshakePhase struct {
	conn     net.Conn
	stopCh   chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func startSocks5HandshakePhase(baseCtx context.Context, conn net.Conn) *socks5HandshakePhase {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	phase := &socks5HandshakePhase{
		conn:   conn,
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	go phase.run(baseCtx)
	return phase
}

func (p *socks5HandshakePhase) run(baseCtx context.Context) {
	defer close(p.done)
	if p == nil || p.conn == nil {
		return
	}

	select {
	case <-p.stopCh:
		return
	case <-baseCtx.Done():
		// Interrupt any blocking handshake read/write. The handler clears these
		// deadlines before transitioning into the long-lived tunnel phase.
		_ = p.conn.SetReadDeadline(time.Now())
		_ = p.conn.SetWriteDeadline(time.Now())
	}
}

func (p *socks5HandshakePhase) Stop() {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() {
		if p.stopCh != nil {
			close(p.stopCh)
		}
		if p.done != nil {
			<-p.done
		}
		if p.conn != nil {
			_ = p.conn.SetReadDeadline(time.Time{})
			_ = p.conn.SetWriteDeadline(time.Time{})
		}
	})
}

func (s *Socks5Inbound) negotiateMethod(conn net.Conn, reader *bufio.Reader, requireAuthInfo bool) (byte, bool) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, false
	}
	if header[0] != socks5Version {
		return 0, false
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return 0, false
	}

	selected := byte(socks5MethodNoAcceptable)
	if s.token != "" || s.relayCredentialResolver != nil || requireAuthInfo {
		if containsSocks5Method(methods, socks5MethodUserPass) {
			selected = socks5MethodUserPass
		}
	} else {
		switch {
		case containsSocks5Method(methods, socks5MethodUserPass):
			selected = socks5MethodUserPass
		case containsSocks5Method(methods, socks5MethodNoAuth):
			selected = socks5MethodNoAuth
		}
	}

	if _, err := conn.Write([]byte{socks5Version, selected}); err != nil {
		return 0, false
	}
	if selected == socks5MethodNoAcceptable {
		return 0, false
	}
	return selected, true
}

func (s *Socks5Inbound) authenticateUserPass(conn net.Conn, reader *bufio.Reader, requireAuthInfo bool) (string, string, bool) {
	platformName, account, _, ok := s.authenticateUserPassWithEndpoint(conn, reader, requireAuthInfo, InboundEndpoint{}, false)
	return platformName, account, ok
}

func (s *Socks5Inbound) authenticateUserPassWithEndpoint(conn net.Conn, reader *bufio.Reader, requireAuthInfo bool, endpoint InboundEndpoint, endpointSet bool) (string, string, *RelayCredential, bool) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", "", nil, false
	}
	if header[0] != socks5UserPassVersion {
		_, _ = conn.Write([]byte{socks5UserPassVersion, socks5UserPassStatusFailure})
		return "", "", nil, false
	}

	username := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, username); err != nil {
		return "", "", nil, false
	}

	plen := []byte{0}
	if _, err := io.ReadFull(reader, plen); err != nil {
		return "", "", nil, false
	}
	password := make([]byte, int(plen[0]))
	if _, err := io.ReadFull(reader, password); err != nil {
		return "", "", nil, false
	}

	if requireAuthInfo && (len(username) == 0 || len(password) == 0) {
		_, _ = conn.Write([]byte{socks5UserPassVersion, socks5UserPassStatusFailure})
		return "", "", nil, false
	}

	platformName, account := parseV1PlatformAccountIdentity(string(username))
	var relayCredential *RelayCredential
	passwordValue := string(password)
	switch {
	case s.token != "" && passwordValue == s.token:
		// The global token is valid on any endpoint and does not carry a Feed
		// revocation scope.
	case s.relayCredentialResolver != nil:
		if !endpointSet || !endpoint.TLSEnabled {
			_, _ = conn.Write([]byte{socks5UserPassVersion, socks5UserPassStatusFailure})
			return "", "", nil, false
		}
		credential, ok := s.relayCredentialResolver(passwordValue)
		if !ok || credential.PlatformName == "" || credential.RelayPort < 1 || credential.RelayPort > 65535 || !credential.RequireTLS || credential.Revoked == nil || endpoint.Port != credential.RelayPort {
			_, _ = conn.Write([]byte{socks5UserPassVersion, socks5UserPassStatusFailure})
			return "", "", nil, false
		}
		// The Feed token, rather than the client-supplied username, chooses the
		// platform. Keep only the optional account suffix from the username.
		platformName = credential.PlatformName
		relayCredential = &credential
	case s.token != "":
		_, _ = conn.Write([]byte{socks5UserPassVersion, socks5UserPassStatusFailure})
		return "", "", nil, false
	}

	if _, err := conn.Write([]byte{socks5UserPassVersion, socks5UserPassStatusSuccess}); err != nil {
		return "", "", nil, false
	}

	return platformName, account, relayCredential, true
}

func containsSocks5Method(methods []byte, candidate byte) bool {
	for _, method := range methods {
		if method == candidate {
			return true
		}
	}
	return false
}

func readSocks5ConnectRequest(reader *bufio.Reader) (string, byte, bool) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", 0, false
	}
	if header[0] != socks5Version {
		return "", socks5ReplyGeneralFailure, false
	}
	if header[1] != socks5CommandConnect {
		return "", socks5ReplyCommandUnsupported, false
	}

	host, ok := readSocks5Address(reader, header[3])
	if !ok {
		if header[3] != socks5AddressTypeIPv4 && header[3] != socks5AddressTypeDomain && header[3] != socks5AddressTypeIPv6 {
			return "", socks5ReplyAddressUnsupported, false
		}
		return "", socks5ReplyGeneralFailure, false
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBuf); err != nil {
		return "", 0, false
	}
	port := strconv.Itoa(int(binary.BigEndian.Uint16(portBuf)))
	return net.JoinHostPort(host, port), 0, true
}

func readSocks5Address(reader *bufio.Reader, atyp byte) (string, bool) {
	switch atyp {
	case socks5AddressTypeIPv4:
		ip := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(reader, ip); err != nil {
			return "", false
		}
		return net.IP(ip).String(), true
	case socks5AddressTypeIPv6:
		ip := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(reader, ip); err != nil {
			return "", false
		}
		return net.IP(ip).String(), true
	case socks5AddressTypeDomain:
		size := []byte{0}
		if _, err := io.ReadFull(reader, size); err != nil {
			return "", false
		}
		domain := make([]byte, int(size[0]))
		if _, err := io.ReadFull(reader, domain); err != nil {
			return "", false
		}
		return string(domain), true
	default:
		return "", false
	}
}

func writeSocks5Reply(w io.Writer, replyCode byte, addr net.Addr) error {
	atyp := byte(socks5AddressTypeIPv4)
	hostBytes := []byte{0, 0, 0, 0}
	port := uint16(0)

	if tcpAddr, ok := addr.(*net.TCPAddr); ok && tcpAddr != nil {
		if ip4 := tcpAddr.IP.To4(); ip4 != nil {
			atyp = socks5AddressTypeIPv4
			hostBytes = append([]byte(nil), ip4...)
		} else if ip16 := tcpAddr.IP.To16(); ip16 != nil {
			atyp = socks5AddressTypeIPv6
			hostBytes = append([]byte(nil), ip16...)
		}
		if tcpAddr.Port >= 0 && tcpAddr.Port <= 65535 {
			port = uint16(tcpAddr.Port)
		}
	}

	resp := make([]byte, 0, 6+len(hostBytes))
	resp = append(resp, socks5Version, replyCode, 0x00, atyp)
	resp = append(resp, hostBytes...)
	resp = binary.BigEndian.AppendUint16(resp, port)
	_, err := w.Write(resp)
	return err
}
