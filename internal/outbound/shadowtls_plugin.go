package outbound

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/adapter"
	sbTLS "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/sip003"
	"github.com/sagernet/sing-shadowtls"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// Register before builders run: sing-box's plugin registry is process-wide.
func init() {
	sip003.RegisterPlugin("shadow-tls", newShadowTLSPlugin)
	sip003.RegisterPlugin("shadowtls", newShadowTLSPlugin)
}

func newShadowTLSPlugin(ctx context.Context, args sip003.Args, _ adapter.Router, dialer N.Dialer, server M.Socksaddr) (sip003.Plugin, error) {
	for key, values := range args {
		switch key {
		case "host", "sni", "password", "version", "alpn", "skip-cert-verify", "insecure", "fingerprint", "client-fingerprint", "certificate_public_key_sha256":
		default:
			return nil, fmt.Errorf("shadow-tls: unsupported plugin option %q", key)
		}
		if len(values) != 1 {
			return nil, fmt.Errorf("shadow-tls: duplicate plugin option %q", key)
		}
	}
	get := func(key string) string { value, _ := args.Get(key); return value }
	version := 2 // Clash/Mihomo's default, unlike native ShadowTLS's v1 default.
	if raw, present := args.Get("version"); present {
		var err error
		version, err = strconv.Atoi(raw)
		if err != nil || version < 1 || version > 3 {
			return nil, fmt.Errorf("shadow-tls: version must be 1, 2 or 3")
		}
	}
	clientFingerprint := strings.TrimSpace(get("client-fingerprint"))
	if version == 1 && clientFingerprint != "" {
		return nil, fmt.Errorf("shadow-tls v1 requires standard TLS 1.2 and does not support client-fingerprint; remove the client fingerprint")
	}
	password := get("password")
	if version >= 2 && password == "" {
		return nil, fmt.Errorf("shadow-tls: missing plugin password")
	}
	host := strings.TrimSpace(get("host"))
	if host == "" {
		host = strings.TrimSpace(get("sni"))
	}
	if host == "" {
		return nil, fmt.Errorf("shadow-tls: missing host (TLS server name)")
	}
	tlsOptions := pinnedTLSOptions{OutboundTLSOptions: option.OutboundTLSOptions{Enabled: true, ServerName: host}}
	for _, key := range []string{"skip-cert-verify", "insecure"} {
		if raw, present := args.Get(key); present {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, fmt.Errorf("shadow-tls: invalid %s", key)
			}
			if other := get("skip-cert-verify"); key == "insecure" && other != "" {
				previous, _ := strconv.ParseBool(other)
				if previous != value {
					return nil, fmt.Errorf("shadow-tls: conflicting TLS verification options")
				}
			}
			tlsOptions.Insecure = value
		}
	}
	if alpn, present := args.Get("alpn"); present {
		if alpn != "" {
			tlsOptions.ALPN = strings.Split(alpn, ",")
		}
	} else {
		tlsOptions.ALPN = []string{"h2", "http/1.1"}
	}
	if version == 1 {
		tlsOptions.MinVersion, tlsOptions.MaxVersion = "1.2", "1.2"
	}
	if pin, present := args.Get("fingerprint"); present {
		tlsOptions.CertificateSHA256 = []string{pin}
	}
	if pin, present := args.Get("certificate_public_key_sha256"); present {
		tlsOptions.CertificatePublicKeySHA256 = []string{pin}
	}
	var verifyConnection func(tls.ConnectionState) error
	var tlsConfig sbTLS.Config
	var err error
	if len(tlsOptions.CertificateSHA256) > 0 || len(tlsOptions.CertificatePublicKeySHA256) > 0 {
		tlsConfig, err = newPinnedTLSConfig(ctx, server.AddrString(), tlsOptions)
		if err != nil {
			return nil, fmt.Errorf("shadow-tls: %w", err)
		}
		standard, err := tlsConfig.Config()
		if err != nil {
			return nil, err
		}
		verifyConnection = standard.VerifyConnection
		if len(tlsOptions.CertificatePublicKeySHA256) > 0 {
			tlsOptions.Insecure = true
		}
	}
	if clientFingerprint != "" {
		tlsOptions.UTLS = &option.OutboundUTLSOptions{Enabled: true, Fingerprint: clientFingerprint}
		tlsConfig, err = sbTLS.NewClient(ctx, server.AddrString(), tlsOptions.OutboundTLSOptions)
	} else if tlsConfig == nil {
		tlsConfig, err = sbTLS.NewClient(ctx, server.AddrString(), tlsOptions.OutboundTLSOptions)
	}
	if err != nil {
		return nil, fmt.Errorf("shadow-tls: %w", err)
	}
	handshake := func(ctx context.Context, conn net.Conn, sessionID shadowtls.TLSSessionIDGeneratorFunc) error {
		// Each v3 connection needs its own callback; do not mutate the template.
		config := tlsConfig.Clone()
		if version == 3 {
			if generator, ok := config.(sbTLS.WithSessionIDGenerator); ok {
				generator.SetSessionIDGenerator(sessionID)
			} else {
				standard, err := config.Config()
				if err != nil {
					return err
				}
				return shadowtls.DefaultTLSHandshakeFunc(password, standard)(ctx, conn, sessionID)
			}
		}
		tlsConn, err := sbTLS.ClientHandshake(ctx, conn, config)
		if err != nil {
			return err
		}
		if verifyConnection != nil {
			return verifyConnection(tlsConn.ConnectionState())
		}
		return nil
	}
	return shadowtls.NewClient(shadowtls.ClientConfig{
		Version: version, Password: password, Server: server, Dialer: dialer,
		TLSHandshake: handshake, Logger: log.NewNOPFactory().NewLogger("shadow-tls"),
	})
}
