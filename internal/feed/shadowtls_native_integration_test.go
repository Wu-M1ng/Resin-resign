//go:build with_utls

package feed

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/sip003"
	"github.com/sagernet/sing-shadowsocks"
	"github.com/sagernet/sing-shadowsocks/shadowaead"
	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	"github.com/sagernet/sing-shadowtls"
	sbJSON "github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// This package must not import Resin/internal/outbound: that would register the
// very plugin whose absence in ordinary clients caused the export failure.
func TestExportShadowTLSPluginNativeRoundTrip(t *testing.T) {
	if _, err := sip003.CreatePlugin(context.Background(), "shadow-tls", "", nil, N.SystemDialer, M.Socksaddr{}); err == nil || !strings.Contains(err.Error(), "plugin not found") {
		t.Fatalf("test must use the unmodified sing-box plugin registry: %v", err)
	}
	certificate := nativeFeedCertificate(t)
	cover := nativeFeedListener(t, func(conn net.Conn) {
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, NextProtos: []string{"h2", "http/1.1"}})
		if err := tlsConn.Handshake(); err != nil {
			return
		}
		_, _ = tlsConn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"))
		_, _ = io.Copy(io.Discard, tlsConn)
	})
	const outerPassword = `shadow;secret=with\separators`
	ssPassword := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	for _, tc := range []struct {
		name, method, fingerprint        string
		version                          int
		wrongOuter, wrongInner, verifyCA bool
	}{
		{name: "v1_ss2022", version: 1},
		{name: "default_v2_ss2022", version: 2},
		{name: "v3_ss2022", version: 3},
		{name: "v3_ss2022_chrome", version: 3, fingerprint: "chrome"},
		{name: "v3_aes128", version: 3, method: "aes-128-gcm"},
		{name: "wrong_shadowtls_password", version: 3, wrongOuter: true},
		{name: "wrong_ss_password", version: 3, wrongInner: true},
		{name: "untrusted_certificate", version: 3, verifyCA: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = "2022-blake3-aes-256-gcm"
			}
			var ss shadowsocks.Service
			var err error
			if strings.HasPrefix(method, "2022-") {
				ss, err = shadowaead_2022.NewServiceWithPassword(method, ssPassword, 5, nativeFeedEcho{}, time.Now)
			} else {
				ss, err = shadowaead.NewService(method, nil, ssPassword, 5, nativeFeedEcho{})
			}
			if err != nil {
				t.Fatal(err)
			}
			transport, err := shadowtls.NewService(shadowtls.ServiceConfig{
				Version: tc.version, Password: outerPassword, Users: []shadowtls.User{{Name: "fixture", Password: outerPassword}},
				Handshake: shadowtls.HandshakeConfig{Server: M.ParseSocksaddr(cover.Addr().String()), Dialer: N.SystemDialer},
				Handler:   nativeFeedShadowHandler{ss}, Logger: log.NewNOPFactory().NewLogger("fixture"),
			})
			if err != nil {
				t.Fatal(err)
			}
			listener := nativeFeedListener(t, func(conn net.Conn) {
				_ = transport.NewConnection(context.Background(), conn, M.SocksaddrFromNet(conn.RemoteAddr()), M.Socksaddr{}, nil)
			})
			password := outerPassword
			if tc.wrongOuter {
				password = "wrong-fixture-password"
			}
			opts := map[string]string{"host": "fixture.invalid", "password": password, "skip-cert-verify": strconv.FormatBool(!tc.verifyCA)}
			if tc.version != 2 {
				opts["version"] = strconv.Itoa(tc.version)
			}
			if tc.fingerprint != "" {
				opts["client-fingerprint"] = tc.fingerprint
			}
			innerPassword := ssPassword
			if tc.wrongInner {
				innerPassword = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
			}
			item := shadowTLSPluginNode(t, encodePluginOptions(opts), map[string]any{
				"server": "127.0.0.1", "server_port": listener.Addr().(*net.TCPAddr).Port,
				"method": method, "password": innerPassword,
			})
			result, err := Export(FormatSingbox, []ExportNode{item}, ExportOptions{UnsupportedPolicy: UnsupportedError})
			if err != nil {
				t.Fatal(err)
			}
			ctx := include.Context(context.Background())
			var config option.Options
			if err := sbJSON.UnmarshalContext(ctx, result.Body, &config); err != nil {
				t.Fatalf("native sing-box rejected the exported schema: %v", err)
			}
			config.Log = &option.LogOptions{Disabled: true}
			client, err := box.New(box.Options{Options: config, Context: ctx})
			if err != nil {
				t.Fatalf("native sing-box could not build the exported chain: %v", err)
			}
			t.Cleanup(func() { _ = client.Close() })
			if err := client.Start(); err != nil {
				t.Fatal(err)
			}
			proxy, found := client.Outbound().Outbound(item.Tag)
			if !found {
				t.Fatal("SS entry missing from native outbound manager")
			}
			err = nativeFeedRoundTrip(proxy)
			if tc.wrongOuter || tc.wrongInner || tc.verifyCA {
				if err == nil {
					t.Fatal("invalid credentials or untrusted certificate allowed traffic")
				}
				if tc.verifyCA && !strings.Contains(err.Error(), "certificate") {
					t.Fatalf("expected certificate validation error, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("native SS -> ShadowTLS round trip: %v", err)
			}
		})
	}
}

func nativeFeedRoundTrip(dialer N.Dialer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", M.ParseSocksaddr("127.0.0.1:80"))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	const request = "native-ss-shadowtls-echo"
	if _, err := io.WriteString(conn, request); err != nil {
		return err
	}
	response := make([]byte, len(request))
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if string(response) != request {
		return fmt.Errorf("SS echo payload mismatch")
	}
	return nil
}

type nativeFeedEcho struct{}

func (nativeFeedEcho) NewConnection(_ context.Context, conn net.Conn, _ M.Metadata) error {
	defer conn.Close()
	_, err := io.Copy(conn, conn)
	return err
}
func (nativeFeedEcho) NewPacketConnection(_ context.Context, conn N.PacketConn, _ M.Metadata) error {
	return conn.Close()
}
func (nativeFeedEcho) NewError(context.Context, error) {}

type nativeFeedShadowHandler struct{ ss shadowsocks.Service }

func (h nativeFeedShadowHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	defer conn.Close()
	err := h.ss.NewConnection(ctx, conn, M.Metadata{Source: source, Destination: destination})
	if onClose != nil {
		onClose(err)
	}
}

func nativeFeedListener(t *testing.T, handler func(net.Conn)) net.Listener {
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

func nativeFeedCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"fixture.invalid"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
