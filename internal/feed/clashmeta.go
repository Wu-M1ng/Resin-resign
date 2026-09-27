package feed

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var clashSupported = map[string]bool{
	"http": true, "socks": true, "shadowsocks": true, "vmess": true,
	"trojan": true, "vless": true, "hysteria": true, "hysteria2": true,
	"tuic": true, "wireguard": true,
}

func exportClashMeta(nodes []ExportNode, options ExportOptions) (ExportResult, error) {
	proxies := make([]map[string]any, 0, len(nodes))
	skipped := make(map[string]struct{})
	for _, item := range nodes {
		object, err := decodeObject(item.RawOptions)
		if err != nil {
			if options.UnsupportedPolicy == UnsupportedError {
				return ExportResult{}, err
			}
			skipped["invalid"] = struct{}{}
			continue
		}
		typ := outboundType(object)
		if !clashSupported[typ] {
			if options.UnsupportedPolicy == UnsupportedError {
				return ExportResult{}, fmt.Errorf("feed: clash-meta does not support outbound type %q", typ)
			}
			skipped[typ] = struct{}{}
			continue
		}
		proxy, err := toClashProxy(object, effectiveTag(item, object))
		if err != nil {
			if options.UnsupportedPolicy == UnsupportedError {
				return ExportResult{}, err
			}
			skipped[typ] = struct{}{}
			continue
		}
		proxies = append(proxies, proxy)
	}
	payload := map[string]any{"proxies": proxies}
	body, err := yaml.Marshal(payload)
	if err != nil {
		return ExportResult{}, fmt.Errorf("feed: marshal clash-meta: %w", err)
	}
	return ExportResult{
		Body: body, ContentType: "text/yaml; charset=utf-8",
		NodeCount: len(proxies), SkippedCount: len(nodes) - len(proxies), SkippedTypes: skippedType(skipped),
	}, nil
}

func toClashProxy(raw map[string]any, name string) (map[string]any, error) {
	typ := outboundType(raw)
	server, ok := requiredString(raw, "server")
	if !ok {
		return nil, fmt.Errorf("feed: %s outbound %q has no server", typ, name)
	}
	port, ok := uintValue(raw, "server_port")
	if !ok || port == 0 {
		return nil, fmt.Errorf("feed: %s outbound %q has no valid server_port", typ, name)
	}
	proxy := map[string]any{"name": name, "server": server, "port": port}
	switch typ {
	case "http":
		proxy["type"] = "http"
		copyString(raw, proxy, "username", "username")
		copyString(raw, proxy, "password", "password")
		applyClashTLS(raw, proxy)
	case "socks":
		proxy["type"] = "socks5"
		copyString(raw, proxy, "username", "username")
		copyString(raw, proxy, "password", "password")
	case "shadowsocks":
		method, methodOK := requiredString(raw, "method")
		password, passwordOK := requiredString(raw, "password")
		if !methodOK || !passwordOK {
			return nil, fmt.Errorf("feed: shadowsocks outbound %q has no method/password", name)
		}
		proxy["type"], proxy["cipher"], proxy["password"] = "ss", method, password
		copyString(raw, proxy, "plugin", "plugin")
		if pluginOpts, ok := raw["plugin_opts"].(map[string]any); ok {
			proxy["plugin-opts"] = pluginOpts
		}
	case "vmess":
		uuid, uuidOK := requiredString(raw, "uuid")
		if !uuidOK {
			return nil, fmt.Errorf("feed: vmess outbound %q has no uuid", name)
		}
		proxy["type"], proxy["uuid"] = "vmess", uuid
		copyNumber(raw, proxy, "alter_id", "alterId")
		copyString(raw, proxy, "security", "cipher")
		if _, ok := proxy["cipher"]; !ok {
			proxy["cipher"] = "auto"
		}
		applyClashTLS(raw, proxy)
		applyClashTransport(raw, proxy)
	case "trojan":
		password, passwordOK := requiredString(raw, "password")
		if !passwordOK {
			return nil, fmt.Errorf("feed: trojan outbound %q has no password", name)
		}
		proxy["type"], proxy["password"] = "trojan", password
		proxy["tls"] = true
		applyClashTLS(raw, proxy)
		applyClashTransport(raw, proxy)
	case "vless":
		uuid, uuidOK := requiredString(raw, "uuid")
		if !uuidOK {
			return nil, fmt.Errorf("feed: vless outbound %q has no uuid", name)
		}
		proxy["type"], proxy["uuid"] = "vless", uuid
		copyString(raw, proxy, "flow", "flow")
		applyClashTLS(raw, proxy)
		applyClashTransport(raw, proxy)
	case "hysteria", "hysteria2":
		proxy["type"] = typ
		if password, ok := stringValue(raw, "password"); ok {
			if typ == "hysteria2" {
				proxy["password"] = password
			} else {
				proxy["auth"] = password
			}
		} else if auth, ok := stringValue(raw, "auth"); ok {
			proxy["auth"] = auth
		}
		applyClashTLS(raw, proxy)
		if typ == "hysteria" {
			copyString(raw, proxy, "auth_str", "auth-str")
			copyString(raw, proxy, "up", "up")
			copyString(raw, proxy, "down", "down")
			copyString(raw, proxy, "obfs", "obfs")
			copyString(raw, proxy, "hop_interval", "hop-interval")
			copyValue(raw, proxy, "server_ports", "ports")
			copyNumber(raw, proxy, "recv_window_conn", "recv-window-conn")
			copyNumber(raw, proxy, "recv_window", "recv-window")
			copyBool(raw, proxy, "disable_mtu_discovery", "disable-mtu-discovery")
		} else {
			copyValue(raw, proxy, "server_ports", "ports")
			copyNumber(raw, proxy, "up_mbps", "up")
			copyNumber(raw, proxy, "down_mbps", "down")
			copyString(raw, proxy, "hop_interval", "hop-interval")
		}
		if obfs, ok := raw["obfs"].(map[string]any); ok {
			if typ == "hysteria2" {
				if value, ok := stringValue(obfs, "type"); ok {
					proxy["obfs"] = value
				}
				copyString(obfs, proxy, "password", "obfs-password")
			}
		}
	case "tuic":
		uuid, uuidOK := requiredString(raw, "uuid")
		password, passwordOK := requiredString(raw, "password")
		if !uuidOK || !passwordOK {
			return nil, fmt.Errorf("feed: tuic outbound %q has no uuid/password", name)
		}
		proxy["type"], proxy["uuid"], proxy["password"] = "tuic", uuid, password
		proxy["congestion-controller"] = firstString(raw, "congestion_control", "congestion_controller")
		if proxy["congestion-controller"] == "" {
			delete(proxy, "congestion-controller")
		}
		applyClashTLS(raw, proxy)
	case "wireguard":
		proxy["type"] = "wireguard"
		copyString(raw, proxy, "private_key", "private-key")
		copyString(raw, proxy, "public_key", "public-key")
		copyString(raw, proxy, "peer_public_key", "public-key")
		if addresses := stringSliceValue(raw, "local_address"); len(addresses) > 0 {
			proxy["ip"] = addresses[0]
			if len(addresses) > 1 {
				proxy["ipv6"] = addresses[1]
			}
		}
		copyString(raw, proxy, "pre_shared_key", "pre-shared-key")
		copyNumber(raw, proxy, "mtu", "mtu")
		if network, ok := stringValue(raw, "network"); ok && strings.EqualFold(network, "tcp") {
			proxy["udp"] = false
		}
		if peers, ok := raw["peers"].([]any); ok && len(peers) > 0 {
			if peer, ok := peers[0].(map[string]any); ok {
				copyString(peer, proxy, "public_key", "public-key")
				copyString(peer, proxy, "pre_shared_key", "pre-shared-key")
			}
		}
	}
	return proxy, nil
}

func applyClashTLS(raw, proxy map[string]any) {
	tls, ok := raw["tls"].(map[string]any)
	if !ok {
		return
	}
	if enabled, ok := boolValue(tls, "enabled"); ok {
		proxy["tls"] = enabled
	}
	copyString(tls, proxy, "server_name", "servername")
	copyString(tls, proxy, "server_name", "sni")
	copyBool(tls, proxy, "insecure", "skip-cert-verify")
	if alpn := stringSliceValue(tls, "alpn"); len(alpn) > 0 {
		proxy["alpn"] = alpn
	}
	if utls, ok := tls["utls"].(map[string]any); ok {
		copyString(utls, proxy, "fingerprint", "client-fingerprint")
	}
	if reality, ok := tls["reality"].(map[string]any); ok {
		realityOpts := map[string]any{}
		copyString(reality, realityOpts, "public_key", "public-key")
		copyString(reality, realityOpts, "short_id", "short-id")
		if len(realityOpts) > 0 {
			proxy["reality-opts"] = realityOpts
		}
	}
}

func stringSliceValue(m map[string]any, key string) []string {
	value, ok := m[key]
	if !ok {
		return nil
	}
	var values []string
	switch v := value.(type) {
	case []string:
		values = append(values, v...)
	case []any:
		for _, item := range v {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
	case string:
		values = strings.Split(v, ",")
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func applyClashTransport(raw, proxy map[string]any) {
	transport, ok := raw["transport"].(map[string]any)
	if !ok {
		return
	}
	network, _ := transport["type"].(string)
	if network == "" {
		return
	}
	proxy["network"] = network
	switch network {
	case "ws":
		ws := map[string]any{}
		copyString(transport, ws, "path", "path")
		if headers, ok := transport["headers"].(map[string]any); ok {
			ws["headers"] = headers
		}
		if len(ws) > 0 {
			proxy["ws-opts"] = ws
		}
	case "grpc":
		grpc := map[string]any{}
		copyString(transport, grpc, "service_name", "grpc-service-name")
		if len(grpc) > 0 {
			proxy["grpc-opts"] = grpc
		}
	case "http", "h2":
		httpOpts := map[string]any{}
		copyString(transport, httpOpts, "path", "path")
		if host, ok := transport["host"].([]any); ok {
			httpOpts["headers"] = map[string]any{"Host": host}
		}
		if len(httpOpts) > 0 {
			proxy["http-opts"] = httpOpts
		}
	}
}

func stringValue(m map[string]any, key string) (string, bool) {
	value, ok := m[key]
	if !ok {
		return "", false
	}
	v, ok := value.(string)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	return v, v != ""
}

func requiredString(m map[string]any, key string) (string, bool) { return stringValue(m, key) }

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := stringValue(m, key); ok {
			return value
		}
	}
	return ""
}

func uintValue(m map[string]any, key string) (uint64, bool) {
	value, ok := m[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return uint64(v), v > 0 && v == float64(uint64(v))
	case int:
		return uint64(v), v > 0
	case uint64:
		return v, v > 0
	case string:
		var n uint64
		_, err := fmt.Sscan(strings.TrimSpace(v), &n)
		return n, err == nil && n > 0
	default:
		return 0, false
	}
}

func boolValue(m map[string]any, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

func copyString(from, to map[string]any, source, target string) {
	if value, ok := stringValue(from, source); ok {
		to[target] = value
	}
}

func copyNumber(from, to map[string]any, source, target string) {
	if value, ok := from[source]; ok {
		to[target] = value
	}
}

func copyValue(from, to map[string]any, source, target string) {
	if value, ok := from[source]; ok && value != nil {
		to[target] = value
	}
}

func copyBool(from, to map[string]any, source, target string) {
	if value, ok := boolValue(from, source); ok {
		to[target] = value
	}
}
