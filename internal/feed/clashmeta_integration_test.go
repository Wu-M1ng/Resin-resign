//go:build with_utls

package feed

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-shadowsocks/shadowaead_2022"
	"github.com/sagernet/sing-shadowtls"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"gopkg.in/yaml.v3"
)

// Use a separately installed Mihomo executable, not Resin's enhanced outbound
// builder, to verify the public Clash format across an actual client boundary.
func TestClashMetaShadowTLSMihomoRoundTrip(t *testing.T) {
	binary := os.Getenv("RESIN_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set RESIN_TEST_MIHOMO to a Mihomo executable")
	}
	certificate := nativeFeedCertificate(t)
	cover := nativeFeedListener(t, func(conn net.Conn) {
		// Mihomo documents that ShadowTLS v2 cannot use X25519MLKEM768.
		// Keep this fixture's cover handshake compatible with both v2 and v3.
		tlsConn := tls.Server(conn, &tls.Config{
			Certificates: []tls.Certificate{certificate}, NextProtos: []string{"h2", "http/1.1"},
			CurvePreferences: []tls.CurveID{tls.X25519},
		})
		if err := tlsConn.Handshake(); err != nil {
			return
		}
		_, _ = io.WriteString(tlsConn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
		_, _ = io.Copy(io.Discard, tlsConn)
	})
	ssPassword := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	const outerPassword = `shadow;secret=with\separators`
	ss, err := shadowaead_2022.NewServiceWithPassword("2022-blake3-aes-256-gcm", ssPassword, 5, nativeFeedEcho{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		version     int
		fingerprint string
		pin         bool
	}{
		{name: "v2_default", version: 2},
		{name: "v3_ss2022", version: 3},
		{name: "v3_chrome", version: 3, fingerprint: "chrome"},
		{name: "v3_certificate_pin", version: 3, pin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			opts := map[string]string{"host": "fixture.invalid", "password": outerPassword, "skip-cert-verify": "true"}
			if tc.version != 2 {
				opts["version"] = strconv.Itoa(tc.version)
			}
			if tc.fingerprint != "" {
				opts["client-fingerprint"] = tc.fingerprint
			}
			if tc.pin {
				sum := sha256.Sum256(certificate.Certificate[0])
				opts["fingerprint"] = hex.EncodeToString(sum[:])
			}
			item := shadowTLSPluginNode(t, encodePluginOptions(opts), map[string]any{
				"server": "127.0.0.1", "server_port": listener.Addr().(*net.TCPAddr).Port, "password": ssPassword,
			})
			result, err := Export(FormatClashMeta, []ExportNode{item}, ExportOptions{UnsupportedPolicy: UnsupportedError})
			if err != nil {
				t.Fatal(err)
			}
			address := startMihomoFeedFixture(t, binary, result.Body, item.Tag)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err := io.WriteString(conn, "CONNECT 127.0.0.1:80 HTTP/1.1\r\nHost: 127.0.0.1:80\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(conn)
			responseHeader, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
			if err != nil {
				t.Fatal(err)
			}
			if responseHeader.StatusCode != http.StatusOK {
				t.Fatalf("Mihomo CONNECT failed: %s", responseHeader.Status)
			}
			const request = "mihomo-ss2022-shadowtls-echo"
			if _, err := io.WriteString(conn, request); err != nil {
				t.Fatal(err)
			}
			response := make([]byte, len(request))
			if _, err := io.ReadFull(reader, response); err != nil {
				t.Fatalf("Mihomo could not carry the exported SS2022 + ShadowTLS node: %v", err)
			}
			if string(response) != request {
				t.Fatal("Mihomo echo mismatch")
			}
		})
	}
}

func startMihomoFeedFixture(t *testing.T, binary string, body []byte, tag string) string {
	t.Helper()
	var config map[string]any
	if err := yaml.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	portReservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := portReservation.Addr().String()
	config["port"] = portReservation.Addr().(*net.TCPAddr).Port
	_ = portReservation.Close()
	config["bind-address"], config["allow-lan"] = "127.0.0.1", false
	config["mode"], config["log-level"] = "rule", "warning"
	config["rules"] = []string{"MATCH," + tag}
	encoded, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-d", dir, "-f", path)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var runErr error
	go func() { runErr = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			_ = cmd.Process.Kill()
			<-done
		}
		if t.Failed() {
			t.Logf("Mihomo fixture output: %s", output.String())
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("Mihomo exited before readiness: %v", runErr)
		default:
		}
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return address
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Mihomo fixture did not become ready")
	return ""
}
