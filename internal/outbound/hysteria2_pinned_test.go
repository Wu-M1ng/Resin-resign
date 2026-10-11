//go:build with_quic

package outbound

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/subscription"
	sbTLS "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-quic/hysteria2"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type hy2EchoHandler struct{}

func (hy2EchoHandler) NewConnectionEx(_ context.Context, conn net.Conn, _, _ M.Socksaddr, onClose N.CloseHandlerFunc) {
	defer conn.Close()
	_, err := io.Copy(conn, conn)
	if onClose != nil {
		onClose(err)
	}
}
func (hy2EchoHandler) NewPacketConnectionEx(_ context.Context, conn N.PacketConn, _, _ M.Socksaddr, onClose N.CloseHandlerFunc) {
	_ = conn.Close()
	if onClose != nil {
		onClose(nil)
	}
}

func TestHysteria2PinnedSubscriptionRoundTrip(t *testing.T) {
	certificate := newPinTestCertificate(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := log.NewNOPFactory().NewLogger("fixture")
	tlsConfig, err := sbTLS.NewServer(ctx, logger, option.InboundTLSOptions{
		Enabled: true, Certificate: []string{certificate.certPEM}, Key: []string{certificate.keyPEM},
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := hysteria2.NewService[string](hysteria2.ServiceOptions{Context: ctx, Logger: logger, TLSConfig: tlsConfig, Handler: hy2EchoHandler{}, UDPTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server.UpdateUsers([]string{"fixture"}, []string{"hy2-password"})
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := server.Start(listener); err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	port := listener.LocalAddr().(*net.UDPAddr).Port
	b, err := NewSingboxBuilderWithConfig(SingboxBuilderConfig{DNSUpstreams: []string{"local"}})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for _, tc := range []struct {
		name, format, pin string
		bad               bool
	}{
		{"clash_certificate", "clash", certificate.certPin, false},
		{"uri_certificate", "uri", certificate.certPin, false},
		{"native_public_key", "native", certificate.keyPin, false},
		{"wrong_certificate", "clash", strings.Repeat("ab", 32), true},
		{"wrong_public_key", "native", base64.StdEncoding.EncodeToString(make([]byte, 32)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input []byte
			switch tc.format {
			case "clash":
				input = []byte(fmt.Sprintf(`{"proxies":[{"type":"hysteria2","server":"127.0.0.1","port":%d,"password":"hy2-password","skip-cert-verify":true,"fingerprint":%q,"client-fingerprint":"chrome"}]}`, port, tc.pin))
			case "uri":
				input = []byte(fmt.Sprintf("hy2://hy2-password@127.0.0.1:%d?insecure=1&pinSHA256=%s&fp=chrome", port, url.QueryEscape(tc.pin)))
			case "native":
				input = []byte(fmt.Sprintf(`{"outbounds":[{"type":"hysteria2","server":"127.0.0.1","server_ports":["%d:%d"],"domain_resolver":"local","password":"hy2-password","tls":{"enabled":true,"insecure":true,"server_name":"fixture.invalid","certificate_public_key_sha256":[%q]}}]}`, port, port, tc.pin))
			}
			nodes, err := subscription.ParseGeneralSubscription(input)
			if err != nil || len(nodes) != 1 {
				t.Fatalf("parse nodes=%d err=%v", len(nodes), err)
			}
			ob, err := b.Build(nodes[0].RawOptions)
			if err != nil {
				t.Fatal(err)
			}
			defer common.Close(ob)
			dialCtx, dialCancel := context.WithTimeout(ctx, 4*time.Second)
			defer dialCancel()
			conn, err := ob.DialContext(dialCtx, "tcp", M.ParseSocksaddr("127.0.0.1:80"))
			if tc.bad {
				if conn != nil {
					conn.Close()
				}
				if err == nil || !strings.Contains(err.Error(), "pin mismatch") {
					t.Fatalf("got %v, want pin mismatch", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
			if _, err := conn.Write([]byte("hy2-echo")); err != nil {
				t.Fatal(err)
			}
			data := make([]byte, 8)
			if _, err := io.ReadFull(conn, data); err != nil {
				t.Fatal(err)
			}
			if string(data) != "hy2-echo" {
				t.Fatalf("echo: %q", data)
			}
		})
	}
}

func TestHysteria2PinOptionsRejectInvalid(t *testing.T) {
	b := newTestSingboxBuilder(t)
	defer b.Close()
	for _, field := range []string{"certificate_sha256", "certificate_public_key_sha256"} {
		_, err := b.Build(json.RawMessage(fmt.Sprintf(`{"type":"hysteria2","server":"127.0.0.1","server_port":443,"password":"fixture","tls":{"enabled":true,"%s":["bad"]}}`, field)))
		if err == nil || !strings.Contains(err.Error(), "SHA256 digest") {
			t.Fatalf("%s: %v", field, err)
		}
	}
}
