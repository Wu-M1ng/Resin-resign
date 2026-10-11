package feed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/sip003"
)

// expandShadowTLSPlugin removes Resin's process-local SIP003 extension from
// native feeds. Tags and detours are assigned by prepareFeedDependencies.
func expandShadowTLSPlugin(raw map[string]any) (inner, outer map[string]any, err error) {
	plugin, _ := stringValue(raw, "plugin")
	if outboundType(raw) != "shadowsocks" || (strings.ToLower(plugin) != "shadow-tls" && strings.ToLower(plugin) != "shadowtls") {
		return raw, nil, nil
	}
	// Validate against the native schema before moving non-SS fields to the
	// transport. This includes all dialer options without maintaining a second
	// field list, and prevents silently dropping unknown/security options.
	var parsed struct {
		Type string `json:"type"`
		Tag  string `json:"tag"`
		option.ShadowsocksOutboundOptions
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot encode ShadowTLS plugin node")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		// Decoder errors may include input values; never expose credentials.
		return nil, nil, fmt.Errorf("invalid or unsupported Shadowsocks fields for native ShadowTLS export")
	}
	if strings.TrimSpace(parsed.Server) == "" || parsed.ServerPort == 0 || strings.TrimSpace(parsed.Method) == "" || parsed.Password == "" {
		return nil, nil, fmt.Errorf("ShadowTLS plugin node requires server, port, SS method and SS password")
	}
	tls, version, password, err := nativeShadowTLSOptions(parsed.PluginOptions)
	if err != nil {
		return nil, nil, err
	}
	inner = make(map[string]any)
	outer = map[string]any{"type": "shadowtls", "version": version, "tls": tls}
	if password != "" {
		outer["password"] = password
	}
	for key, value := range raw {
		switch key {
		case "plugin", "plugin_opts":
		case "type", "tag", "method", "password", "network", "udp_over_tcp", "multiplex":
			inner[key] = value
		case "server", "server_port":
			inner[key], outer[key] = value, value
		default:
			// Dialing now happens in the outer node, including any existing hop.
			outer[key] = value
		}
	}
	// Native ShadowTLS cannot send raw UDP. Existing UoT or multiplex can carry
	// it over TCP, so retain those settings instead of disabling them.
	udpOverTCP := parsed.UDPOverTCP != nil && parsed.UDPOverTCP.Enabled
	multiplex := parsed.Multiplex != nil && parsed.Multiplex.Enabled
	if !udpOverTCP && !multiplex {
		if parsed.Network != "" && parsed.Network != "tcp" {
			return nil, nil, fmt.Errorf("native ShadowTLS cannot carry raw UDP; use TCP or an existing UoT/multiplex configuration")
		}
		inner["network"] = "tcp"
	}
	return inner, outer, nil
}

func nativeShadowTLSOptions(encoded string) (map[string]any, int, string, error) {
	args, err := sip003.ParsePluginOptions(encoded)
	if err != nil {
		return nil, 0, "", fmt.Errorf("invalid ShadowTLS SIP003 options")
	}
	for key, values := range args {
		if len(values) != 1 {
			return nil, 0, "", fmt.Errorf("duplicate ShadowTLS plugin option")
		}
		switch key {
		case "host", "sni", "password", "version", "alpn", "skip-cert-verify", "insecure", "client-fingerprint":
		case "fingerprint", "certificate_public_key_sha256":
			return nil, 0, "", fmt.Errorf("native sing-box 1.12 cannot represent ShadowTLS certificate pins")
		default:
			return nil, 0, "", fmt.Errorf("unsupported ShadowTLS plugin option")
		}
	}
	get := func(key string) string { value, _ := args.Get(key); return value }
	version := 2 // Plugin default, unlike native ShadowTLS's v1 default.
	if value, present := args.Get("version"); present {
		version, err = strconv.Atoi(value)
		if err != nil || version < 1 || version > 3 {
			return nil, 0, "", fmt.Errorf("ShadowTLS plugin version must be 1, 2 or 3")
		}
	}
	password := get("password")
	if version >= 2 && password == "" {
		return nil, 0, "", fmt.Errorf("ShadowTLS plugin has no password")
	}
	host := strings.TrimSpace(get("host"))
	if host == "" {
		host = strings.TrimSpace(get("sni"))
	}
	if host == "" {
		return nil, 0, "", fmt.Errorf("ShadowTLS plugin has no TLS server name")
	}
	insecure, hasInsecure := false, false
	for _, key := range []string{"skip-cert-verify", "insecure"} {
		if value, present := args.Get(key); present {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return nil, 0, "", fmt.Errorf("invalid ShadowTLS TLS verification option")
			}
			if hasInsecure && insecure != parsed {
				return nil, 0, "", fmt.Errorf("conflicting ShadowTLS TLS verification options")
			}
			insecure, hasInsecure = parsed, true
		}
	}
	tls := map[string]any{"enabled": true, "server_name": host, "insecure": insecure}
	if alpn, present := args.Get("alpn"); present {
		if alpn != "" {
			tls["alpn"] = strings.Split(alpn, ",")
		}
	} else {
		tls["alpn"] = []string{"h2", "http/1.1"}
	}
	// The native ShadowTLS v1 outbound enforces TLS 1.2 itself.
	if fingerprint := strings.TrimSpace(get("client-fingerprint")); fingerprint != "" {
		if version == 1 {
			return nil, 0, "", fmt.Errorf("ShadowTLS v1 cannot use a client fingerprint")
		}
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint}
	}
	return tls, version, password, nil
}
