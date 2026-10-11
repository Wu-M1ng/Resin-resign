package feed

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/transport/sip003"
)

func applyClashSSPlugin(raw, proxy map[string]any) error {
	plugin, ok := stringValue(raw, "plugin")
	if !ok {
		return nil
	}
	opts, err := clashPluginOptions(raw["plugin_opts"])
	if err != nil {
		return err
	}
	switch strings.ToLower(plugin) {
	case "shadow-tls", "shadowtls":
		plugin = "shadow-tls"
		if err := normalizeClashShadowTLS(opts, proxy); err != nil {
			return err
		}
	case "obfs", "obfs-local", "simple-obfs":
		plugin = "obfs"
		for source, target := range map[string]string{"obfs": "mode", "obfs-host": "host"} {
			if err := renameClashPluginOption(opts, source, target); err != nil {
				return err
			}
		}
	case "v2ray-plugin":
		plugin = "v2ray-plugin"
		if _, isString := raw["plugin_opts"].(string); isString {
			// sing-box enables TLS by the flag's presence, regardless of its value.
			if _, present := opts["tls"]; present {
				opts["tls"] = true
			}
		}
		if value, present := opts["mux"]; present {
			if _, ok := value.(bool); !ok {
				count, err := strconv.Atoi(fmt.Sprint(value))
				if err != nil || count < 0 {
					return fmt.Errorf("invalid plugin mux option")
				}
				opts["mux"] = count > 0
			}
		}
	}
	for _, key := range []string{"tls", "skip-cert-verify"} {
		if value, present := opts[key]; present {
			enabled, err := strconv.ParseBool(fmt.Sprint(value))
			if err != nil {
				return fmt.Errorf("invalid plugin %s option", key)
			}
			opts[key] = enabled
		}
	}
	proxy["plugin"] = plugin
	proxy["plugin-opts"] = opts
	return nil
}

// Resin stores SIP003 options as a string; Clash requires a typed YAML map.
// Use the same escape parser as the outbound to preserve passwords and paths.
func clashPluginOptions(value any) (map[string]any, error) {
	opts := make(map[string]any)
	switch value := value.(type) {
	case nil:
	case map[string]any:
		for key, item := range value {
			opts[key] = item
		}
	case string:
		args, err := sip003.ParsePluginOptions(value)
		if err != nil {
			// The parser's error may contain the raw options, including passwords.
			return nil, fmt.Errorf("invalid SIP003 plugin options")
		}
		for key, values := range args {
			if len(values) != 1 {
				return nil, fmt.Errorf("duplicate plugin option %q cannot be represented in Clash", key)
			}
			opts[key] = values[0]
		}
	default:
		return nil, fmt.Errorf("plugin options must be a string or object")
	}
	return opts, nil
}

func normalizeClashShadowTLS(opts, proxy map[string]any) error {
	if err := renameClashPluginOption(opts, "sni", "host"); err != nil {
		return err
	}
	if _, ok := stringValue(opts, "host"); !ok {
		return fmt.Errorf("shadow-tls plugin has no host")
	}
	version := 2 // Match Resin's SIP003 adapter and Mihomo's default.
	if value, present := opts["version"]; present {
		var err error
		version, err = strconv.Atoi(fmt.Sprint(value))
		if err != nil || version < 1 || version > 3 {
			return fmt.Errorf("shadow-tls plugin version must be 1, 2 or 3")
		}
	}
	opts["version"] = version
	if version >= 2 {
		if _, ok := stringValue(opts, "password"); !ok {
			return fmt.Errorf("shadow-tls plugin has no password")
		}
	}
	// This option belongs to the SS node in Clash, outside plugin-opts.
	if fingerprint, ok := stringValue(opts, "client-fingerprint"); ok {
		proxy["client-fingerprint"] = fingerprint
		delete(opts, "client-fingerprint")
	}
	if value, present := opts["insecure"]; present {
		enabled, err := strconv.ParseBool(fmt.Sprint(value))
		if err != nil {
			return fmt.Errorf("invalid shadow-tls insecure option")
		}
		if other, present := opts["skip-cert-verify"]; present {
			previous, err := strconv.ParseBool(fmt.Sprint(other))
			if err != nil || previous != enabled {
				return fmt.Errorf("conflicting shadow-tls TLS verification options")
			}
		}
		opts["skip-cert-verify"] = enabled
		delete(opts, "insecure")
	}
	if alpn, ok := opts["alpn"].(string); ok {
		values := []string{}
		if alpn != "" {
			values = strings.Split(alpn, ",")
		}
		opts["alpn"] = values
	}
	if _, present := opts["certificate_public_key_sha256"]; present {
		return fmt.Errorf("Clash cannot represent shadow-tls public key pins")
	}
	return nil
}

func renameClashPluginOption(opts map[string]any, source, target string) error {
	if value, present := opts[source]; present {
		if other, present := opts[target]; present && fmt.Sprint(other) != fmt.Sprint(value) {
			return fmt.Errorf("conflicting plugin options %s and %s", source, target)
		}
		opts[target] = value
		delete(opts, source)
	}
	return nil
}
