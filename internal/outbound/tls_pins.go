package outbound

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	sbTLS "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

// Keep the pin types separate: hashing DER certificates and hashing SPKI are
// different trust constraints. This extends the TLS schema of sing-box 1.12.
type pinnedTLSOptions struct {
	option.OutboundTLSOptions
	CertificateSHA256          badoption.Listable[string] `json:"certificate_sha256,omitempty"`
	CertificatePublicKeySHA256 badoption.Listable[string] `json:"certificate_public_key_sha256,omitempty"`
}

func parseSHA256Pins(values []string, field string, allowHex bool) ([][sha256.Size]byte, error) {
	pins := make([][sha256.Size]byte, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		var decoded []byte
		if allowHex {
			hexValue := strings.ReplaceAll(value, ":", "")
			if len(hexValue) == sha256.Size*2 {
				decoded, _ = hex.DecodeString(hexValue)
			}
		}
		if len(decoded) != sha256.Size {
			var err error
			decoded, err = base64.StdEncoding.DecodeString(value)
			if err != nil {
				decoded = nil
			}
		}
		if len(decoded) != sha256.Size {
			return nil, fmt.Errorf("tls.%s: expected a %d-byte SHA256 digest", field, sha256.Size)
		}
		pins = append(pins, [sha256.Size]byte(decoded))
	}
	return pins, nil
}

func newPinnedTLSConfig(ctx context.Context, server string, options pinnedTLSOptions) (sbTLS.Config, error) {
	certPins, err := parseSHA256Pins(options.CertificateSHA256, "certificate_sha256", true)
	if err != nil {
		return nil, err
	}
	keyPins, err := parseSHA256Pins(options.CertificatePublicKeySHA256, "certificate_public_key_sha256", false)
	if err != nil {
		return nil, err
	}
	if !options.Enabled {
		return nil, fmt.Errorf("certificate pinning requires TLS")
	}
	if options.UTLS != nil && options.UTLS.Enabled {
		return nil, fmt.Errorf("certificate pinning requires standard TLS, not uTLS")
	}
	if options.Reality != nil && options.Reality.Enabled {
		return nil, fmt.Errorf("certificate pinning is incompatible with reality")
	}
	if len(keyPins) > 0 && (len(options.Certificate) > 0 || options.CertificatePath != "") {
		return nil, fmt.Errorf("certificate_public_key_sha256 conflicts with certificate or certificate_path")
	}
	// Native sing-box public-key pinning replaces CA/hostname trust. Certificate
	// pins from Clash/Hysteria additionally constrain the configured TLS policy.
	if len(keyPins) > 0 {
		options.Insecure = true
	}
	config, err := sbTLS.NewSTDClient(ctx, server, options.OutboundTLSOptions)
	if err != nil {
		return nil, err
	}
	stdConfig, err := config.Config()
	if err != nil {
		return nil, err
	}
	previousVerify := stdConfig.VerifyConnection
	stdConfig.VerifyConnection = func(state tls.ConnectionState) error {
		if previousVerify != nil {
			if err := previousVerify(state); err != nil {
				return err
			}
		}
		if len(state.PeerCertificates) == 0 {
			return fmt.Errorf("TLS peer did not provide a certificate")
		}
		leaf := state.PeerCertificates[0]
		if len(certPins) > 0 && !matchesSHA256(certPins, sha256.Sum256(leaf.Raw)) {
			return fmt.Errorf("server certificate SHA256 pin mismatch")
		}
		if len(keyPins) > 0 {
			// Match sing-box's native SPKI pin semantics: the pinned public key
			// is the trust anchor, without adding CA, name or expiry validation.
			publicKey, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
			if err != nil {
				return fmt.Errorf("marshal server public key: %w", err)
			}
			if !matchesSHA256(keyPins, sha256.Sum256(publicKey)) {
				return fmt.Errorf("server certificate public key SHA256 pin mismatch")
			}
		}
		return nil
	}
	// ShadowTLS v3's standard handshake bridge forwards VerifyPeerCertificate
	// but not VerifyConnection. Keep pins effective on that path as well.
	verifyConnection := stdConfig.VerifyConnection
	stdConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		certs := make([]*x509.Certificate, 0, len(rawCerts))
		for _, raw := range rawCerts {
			cert, err := x509.ParseCertificate(raw)
			if err != nil {
				return err
			}
			certs = append(certs, cert)
		}
		return verifyConnection(tls.ConnectionState{PeerCertificates: certs, VerifiedChains: verifiedChains})
	}
	return config, nil
}

func matchesSHA256(pins [][sha256.Size]byte, hash [sha256.Size]byte) bool {
	for _, pin := range pins {
		if pin == hash {
			return true
		}
	}
	return false
}
