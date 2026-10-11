package outbound

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/subscription"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-shadowsocks/shadowaead"
	"github.com/sagernet/sing-shadowtls"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type ssEchoHandler struct{}

func (ssEchoHandler) NewConnection(_ context.Context, conn net.Conn, _ M.Metadata) error {
	defer conn.Close()
	_, err := io.Copy(conn, conn)
	return err
}
func (ssEchoHandler) NewPacketConnection(_ context.Context, conn N.PacketConn, _ M.Metadata) error {
	return conn.Close()
}
func (ssEchoHandler) NewError(_ context.Context, _ error) {}

type shadowToSSHandler struct{ ss *shadowaead.Service }

func (h shadowToSSHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	defer conn.Close()
	err := h.ss.NewConnection(ctx, conn, M.Metadata{Source: source, Destination: destination})
	if onClose != nil {
		onClose(err)
	}
}

// Every accepted connection is closed before returning from cleanup, including
// deliberate authentication failures. No test contacts an external server.
func serveFixtureTCP(t *testing.T, handler func(net.Conn)) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	var lock sync.Mutex
	connections := make(map[net.Conn]struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			lock.Lock()
			connections[conn] = struct{}{}
			lock.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				defer func() { lock.Lock(); delete(connections, conn); lock.Unlock() }()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				handler(conn)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		lock.Lock()
		for conn := range connections {
			_ = conn.Close()
		}
		lock.Unlock()
		workers.Wait()
	})
	return listener
}

func TestShadowTLSPluginSubscriptionRoundTrip(t *testing.T) {
	certificate := newPinTestCertificate(t, false)
	cover := serveFixtureTCP(t, func(conn net.Conn) {
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate.pair}, NextProtos: []string{"h2", "http/1.1"}})
		if err := tlsConn.Handshake(); err != nil {
			return
		}
		_, _ = tlsConn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"))
		_, _ = io.Copy(io.Discard, tlsConn)
	})
	const ssPassword = "ss-fixture-password"
	const pluginPassword = `shadow;secret=with\separators`
	ss, err := shadowaead.NewService("aes-128-gcm", nil, ssPassword, 5, ssEchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                                                    string
		version                                                 int
		clientFingerprint                                       string
		wrongPassword, wrongPin, uri, concurrent, stringOptions bool
	}{
		{name: "v1", version: 1},
		{name: "v2_default", version: 2},
		{name: "v2_utls", version: 2, clientFingerprint: "chrome"},
		{name: "v3", version: 3},
		{name: "v3_uri", version: 3, uri: true},
		{name: "v3_string_trailing_separator", version: 3, clientFingerprint: "chrome", stringOptions: true},
		{name: "v3_utls_concurrent", version: 3, clientFingerprint: "chrome", concurrent: true},
		{name: "v3_wrong_password", version: 3, wrongPassword: true},
		{name: "v2_wrong_password", version: 2, wrongPassword: true},
		{name: "v3_wrong_pin", version: 3, wrongPin: true},
		{name: "v3_utls_wrong_pin", version: 3, clientFingerprint: "chrome", wrongPin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, err := shadowtls.NewService(shadowtls.ServiceConfig{
				Version: tc.version, Password: pluginPassword, Users: []shadowtls.User{{Name: "fixture", Password: pluginPassword}},
				Handshake: shadowtls.HandshakeConfig{Server: M.ParseSocksaddr(cover.Addr().String()), Dialer: N.SystemDialer},
				Handler:   shadowToSSHandler{ss}, Logger: log.NewNOPFactory().NewLogger("fixture"),
			})
			if err != nil {
				t.Fatal(err)
			}
			listener := serveFixtureTCP(t, func(conn net.Conn) {
				_ = service.NewConnection(context.Background(), conn, M.SocksaddrFromNet(conn.RemoteAddr()), M.Socksaddr{}, nil)
			})
			password, pin := pluginPassword, certificate.certPin
			if tc.wrongPassword {
				password = "wrong-password"
			}
			if tc.wrongPin {
				pin = strings.Repeat("ab", 32)
			}
			pluginOpts := map[string]any{"host": "fixture.invalid", "password": password, "skip-cert-verify": true, "fingerprint": pin}
			if tc.name != "v2_default" {
				pluginOpts["version"] = tc.version
			}
			proxy := map[string]any{"type": "ss", "server": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port, "cipher": "aes-128-gcm", "password": ssPassword, "plugin": "shadow-tls", "plugin-opts": pluginOpts}
			if tc.clientFingerprint != "" {
				proxy["client-fingerprint"] = tc.clientFingerprint
			}
			if tc.stringOptions {
				escape := strings.NewReplacer(`\`, `\\`, `;`, `\;`, `=`, `\=`)
				proxy["plugin-opts"] = "host=fixture.invalid;version=3;password=" + escape.Replace(password) + ";skip-cert-verify=true;fingerprint=" + pin + ";"
			}
			input, err := json.Marshal(map[string]any{"proxies": []any{proxy}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.uri {
				escape := strings.NewReplacer(`\`, `\\`, `;`, `\;`, `=`, `\=`)
				opts := "shadow-tls;host=fixture.invalid;version=3;password=" + escape.Replace(password) + ";skip-cert-verify=true;fingerprint=" + pin
				input = []byte("ss://aes-128-gcm:" + ssPassword + "@" + listener.Addr().String() + "?plugin=" + url.QueryEscape(opts))
			}
			nodes, err := subscription.ParseGeneralSubscription(input)
			if err != nil || len(nodes) != 1 {
				t.Fatalf("nodes=%d err=%v", len(nodes), err)
			}
			builder, err := NewSingboxBuilderWithConfig(SingboxBuilderConfig{DNSUpstreams: []string{"local"}})
			if err != nil {
				t.Fatal(err)
			}
			defer builder.Close()
			ob, err := builder.Build(nodes[0].RawOptions)
			if err != nil && tc.clientFingerprint != "" && strings.Contains(err.Error(), "uTLS is not included") {
				t.Skip(err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer common.Close(ob)
			roundTrip := func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				conn, err := ob.DialContext(ctx, "tcp", M.ParseSocksaddr("127.0.0.1:80"))
				if err != nil {
					return err
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				if _, err = conn.Write([]byte("ss-echo")); err != nil {
					return err
				}
				data := make([]byte, 7)
				if _, err = io.ReadFull(conn, data); err != nil {
					return err
				}
				if string(data) != "ss-echo" {
					return fmt.Errorf("unexpected echo %q", data)
				}
				return nil
			}
			if tc.concurrent {
				results := make(chan error, 6)
				for i := 0; i < 6; i++ {
					go func() { results <- roundTrip() }()
				}
				for i := 0; i < 6; i++ {
					if err := <-results; err != nil {
						t.Error(err)
					}
				}
			} else {
				err := roundTrip()
				if tc.wrongPassword || tc.wrongPin {
					if err == nil {
						t.Fatal("invalid credentials/pin allowed a round trip")
					}
					if tc.wrongPin && !strings.Contains(err.Error(), "pin mismatch") {
						t.Fatalf("wrong pin: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestShadowTLSPluginRejectsInvalidOptions(t *testing.T) {
	b := newTestSingboxBuilder(t)
	defer b.Close()
	for _, opts := range []string{
		"host=fixture.invalid;version=1;client-fingerprint=chrome",
		"host=fixture.invalid;version=9;password=x",
		"host=fixture.invalid;version=3",
		"version=3;password=x",
		"host=fixture.invalid;password=x;skip-cert-verify=wrong",
		"host=fixture.invalid;password=x;fingerprint=bad",
		"host=fixture.invalid;password=x;fingerprint=;fingerprint=x",
		"host=fixture.invalid;password=x;unknown-pin=value",
	} {
		raw, _ := json.Marshal(map[string]any{"type": "shadowsocks", "server": "127.0.0.1", "server_port": 443, "method": "aes-128-gcm", "password": "ss", "plugin": "shadow-tls", "plugin_opts": opts})
		if _, err := b.Build(raw); err == nil {
			t.Errorf("accepted invalid options %q", opts)
		}
	}
}
