package feed

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

var uriSupported = map[string]bool{
	"vmess": true, "vless": true, "trojan": true, "shadowsocks": true,
	"hysteria2": true, "http": true, "socks": true,
}

func exportURI(nodes []ExportNode, format Format, options ExportOptions) (ExportResult, error) {
	lines := make([]string, 0, len(nodes))
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
		if !uriSupported[typ] {
			if options.UnsupportedPolicy == UnsupportedError {
				return ExportResult{}, fmt.Errorf("feed: URI does not support outbound type %q", typ)
			}
			skipped[typ] = struct{}{}
			continue
		}
		line, err := toURI(object, effectiveTag(item, object))
		if err != nil {
			if options.UnsupportedPolicy == UnsupportedError {
				return ExportResult{}, err
			}
			skipped[typ] = struct{}{}
			continue
		}
		lines = append(lines, line)
	}
	plain := strings.Join(lines, "\n")
	if len(lines) > 0 {
		plain += "\n"
	} else {
		// Keep an empty subscription as a valid line-oriented response.
		plain = "\n"
	}
	body := []byte(plain)
	if format == FormatV2RayBase64 {
		encoded := base64.StdEncoding.EncodeToString([]byte(plain))
		body = []byte(encoded + "\n")
	}
	return ExportResult{
		Body: body, ContentType: "text/plain; charset=utf-8",
		NodeCount: len(lines), SkippedCount: len(nodes) - len(lines), SkippedTypes: skippedType(skipped),
	}, nil
}

func toURI(raw map[string]any, name string) (string, error) {
	typ := outboundType(raw)
	server, ok := requiredString(raw, "server")
	if !ok {
		return "", fmt.Errorf("feed: %s outbound %q has no server", typ, name)
	}
	port, ok := uintValue(raw, "server_port")
	if !ok {
		return "", fmt.Errorf("feed: %s outbound %q has no valid server_port", typ, name)
	}
	host := server
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	address := host + ":" + strconv.FormatUint(port, 10)
	switch typ {
	case "vmess":
		return vmessURI(raw, name, server, port)
	case "vless":
		uuid, ok := requiredString(raw, "uuid")
		if !ok {
			return "", fmt.Errorf("feed: vless outbound %q has no uuid", name)
		}
		values := uriTransportValues(raw)
		return "vless://" + escapeURIComponent(uuid) + "@" + address + "?" + values.Encode() + "#" + escapeURIComponent(name), nil
	case "trojan":
		password, ok := requiredString(raw, "password")
		if !ok {
			return "", fmt.Errorf("feed: trojan outbound %q has no password", name)
		}
		values := uriTransportValues(raw)
		return "trojan://" + escapeURIComponent(password) + "@" + address + queryFragment(values, name), nil
	case "shadowsocks":
		method, methodOK := requiredString(raw, "method")
		password, passwordOK := requiredString(raw, "password")
		if !methodOK || !passwordOK {
			return "", fmt.Errorf("feed: shadowsocks outbound %q has no method/password", name)
		}
		userinfo := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + password))
		return "ss://" + userinfo + "@" + address + "#" + escapeURIComponent(name), nil
	case "hysteria2":
		password, ok := requiredString(raw, "password")
		if !ok {
			return "", fmt.Errorf("feed: hysteria2 outbound %q has no password", name)
		}
		values := uriTransportValues(raw)
		return "hysteria2://" + escapeURIComponent(password) + "@" + address + queryFragment(values, name), nil
	case "http":
		scheme := "http"
		if tls, ok := raw["tls"].(map[string]any); ok {
			if enabled, ok := boolValue(tls, "enabled"); ok && enabled {
				scheme = "https"
			}
		}
		return scheme + "://" + uriAddress(raw, server, port) + "#" + escapeURIComponent(name), nil
	case "socks":
		return "socks5://" + uriAddress(raw, server, port) + "#" + escapeURIComponent(name), nil
	default:
		return "", fmt.Errorf("feed: URI does not support outbound type %q", typ)
	}
}

func uriAddress(raw map[string]any, server string, port uint64) string {
	server = strings.TrimSpace(server)
	if strings.HasPrefix(server, "[") && strings.HasSuffix(server, "]") {
		server = strings.TrimPrefix(strings.TrimSuffix(server, "]"), "[")
	}
	address := net.JoinHostPort(server, strconv.FormatUint(port, 10))
	username, hasUsername := stringValue(raw, "username")
	password, hasPassword := stringValue(raw, "password")
	if !hasUsername {
		return address
	}
	var user *url.Userinfo
	if hasPassword {
		user = url.UserPassword(username, password)
	} else {
		user = url.User(username)
	}
	return user.String() + "@" + address
}

func vmessURI(raw map[string]any, name, server string, port uint64) (string, error) {
	uuid, ok := requiredString(raw, "uuid")
	if !ok {
		return "", fmt.Errorf("feed: vmess outbound %q has no uuid", name)
	}
	tls := ""
	sni := ""
	if tlsMap, ok := raw["tls"].(map[string]any); ok {
		if enabled, ok := boolValue(tlsMap, "enabled"); ok && enabled {
			tls = "tls"
		}
		sni, _ = stringValue(tlsMap, "server_name")
	}
	network := "tcp"
	path := ""
	if transport, ok := raw["transport"].(map[string]any); ok {
		network, _ = stringValue(transport, "type")
		if network == "" {
			network = "tcp"
		}
		path, _ = stringValue(transport, "path")
	}
	object := map[string]any{
		"v": "2", "ps": name, "add": server, "port": port, "id": uuid,
		"aid": 0, "net": network, "type": "none", "host": "", "path": path,
		"tls": tls, "sni": sni,
	}
	data, err := json.Marshal(object)
	if err != nil {
		return "", fmt.Errorf("feed: marshal vmess URI: %w", err)
	}
	return "vmess://" + base64.RawStdEncoding.EncodeToString(data), nil
}

func uriTransportValues(raw map[string]any) url.Values {
	values := url.Values{}
	if tlsMap, ok := raw["tls"].(map[string]any); ok {
		if enabled, ok := boolValue(tlsMap, "enabled"); ok && enabled {
			values.Set("security", "tls")
		}
		if reality, ok := tlsMap["reality"].(map[string]any); ok {
			if enabled, ok := boolValue(reality, "enabled"); ok && enabled {
				values.Set("security", "reality")
			}
			if publicKey, ok := stringValue(reality, "public_key"); ok {
				values.Set("pbk", publicKey)
			}
			if shortID, ok := stringValue(reality, "short_id"); ok {
				values.Set("sid", shortID)
			}
		}
		if sni, ok := stringValue(tlsMap, "server_name"); ok {
			values.Set("sni", sni)
		}
		if utls, ok := tlsMap["utls"].(map[string]any); ok {
			if fingerprint, ok := stringValue(utls, "fingerprint"); ok {
				values.Set("fp", fingerprint)
			}
		}
		if items := stringSliceValue(tlsMap, "alpn"); len(items) > 0 {
			values.Set("alpn", strings.Join(items, ","))
		}
	}
	if transport, ok := raw["transport"].(map[string]any); ok {
		if network, ok := stringValue(transport, "type"); ok {
			values.Set("type", network)
		}
		if path, ok := stringValue(transport, "path"); ok {
			values.Set("path", path)
		}
		if service, ok := stringValue(transport, "service_name"); ok {
			values.Set("serviceName", service)
		}
	}
	return values
}

func queryFragment(values url.Values, name string) string {
	query := values.Encode()
	if query != "" {
		return "?" + query + "#" + escapeURIComponent(name)
	}
	return "#" + escapeURIComponent(name)
}

func escapeURIComponent(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}
