package outbound

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
)

type pinTestCertificate struct {
	pair            tls.Certificate
	certPEM, keyPEM string
	certPin, keyPin string
}

func newPinTestCertificate(t *testing.T, expired bool) pinTestCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture.invalid"},
		DNSNames: []string{"fixture.invalid"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if expired {
		template.NotAfter = now.Add(-time.Minute)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	certHash, keyHash := sha256.Sum256(der), sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return pinTestCertificate{pair, certPEM, keyPEM, hex.EncodeToString(certHash[:]), base64.StdEncoding.EncodeToString(keyHash[:])}
}

func TestPinnedTLSHandshake(t *testing.T) {
	cert := newPinTestCertificate(t, false)
	expired := newPinTestCertificate(t, true)
	for _, tc := range []struct {
		name              string
		certificate       pinTestCertificate
		certPins, keyPins []string
		insecure          bool
		wantError         string
	}{
		{"certificate", cert, []string{cert.certPin}, nil, true, ""},
		{"wrong_certificate", cert, []string{strings.Repeat("ab", 32)}, nil, true, "pin mismatch"},
		{"certificate_preserves_CA_validation", cert, []string{cert.certPin}, nil, false, "certificate"},
		{"public_key", cert, nil, []string{cert.keyPin}, true, ""},
		{"public_key_replaces_CA_trust", cert, nil, []string{cert.keyPin}, false, ""},
		{"wrong_public_key", cert, nil, []string{base64.StdEncoding.EncodeToString(make([]byte, 32))}, true, "pin mismatch"},
		{"public_key_preserves_native_pin_only_trust", expired, nil, []string{expired.keyPin}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := newPinnedTLSConfig(context.Background(), "127.0.0.1", pinnedTLSOptions{
				OutboundTLSOptions: option.OutboundTLSOptions{Enabled: true, ServerName: "fixture.invalid", Insecure: tc.insecure},
				CertificateSHA256:  tc.certPins, CertificatePublicKeySHA256: tc.keyPins,
			})
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			clientSide, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			serverSide, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer clientSide.Close()
			defer serverSide.Close()
			_ = clientSide.SetDeadline(time.Now().Add(3 * time.Second))
			_ = serverSide.SetDeadline(time.Now().Add(3 * time.Second))
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = tls.Server(serverSide, &tls.Config{Certificates: []tls.Certificate{tc.certificate.pair}}).Handshake()
				_ = serverSide.Close()
			}()
			client, err := config.Client(clientSide)
			if err != nil {
				t.Fatal(err)
			}
			err = client.HandshakeContext(context.Background())
			_ = clientSide.Close()
			<-done
			if tc.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("got %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestPinnedTLSRejectsMalformedPins(t *testing.T) {
	for _, pin := range []string{"", "chrome", "ab", strings.Repeat("0", 63), base64.StdEncoding.EncodeToString(make([]byte, 32)) + "!"} {
		if _, err := parseSHA256Pins([]string{pin}, "certificate_sha256", true); err == nil {
			t.Errorf("accepted malformed pin %q", pin)
		}
	}
}
