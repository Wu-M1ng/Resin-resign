package feed

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func shadowTLSPluginNode(t *testing.T, opts string, extra map[string]any) ExportNode {
	t.Helper()
	raw := map[string]any{
		"type": "shadowsocks", "tag": "original", "server": "node.example.com", "server_port": 18801,
		"method": "2022-blake3-aes-256-gcm", "password": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"plugin": "shadow-tls", "plugin_opts": opts,
	}
	for key, value := range extra {
		raw[key] = value
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return testNode(string(encoded), "source/HK 01", string(encoded))
}

func nativeFeedOutbounds(t *testing.T, result ExportResult) []map[string]any {
	t.Helper()
	var payload struct{ Outbounds []map[string]any }
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Outbounds
}

func TestExportShadowTLSPluginNative(t *testing.T) {
	item := shadowTLSPluginNode(t, `host=www.php.net;password=outer\;secret\=with\\separators;version=3;skip-cert-verify=false;client-fingerprint=chrome`, nil)
	before := string(item.RawOptions)
	result, err := Export(FormatSingbox, []ExportNode{item, item}, ExportOptions{UnsupportedPolicy: UnsupportedError})
	if err != nil {
		t.Fatal(err)
	}
	out := nativeFeedOutbounds(t, result)
	if result.NodeCount != 2 || result.SkippedCount != 0 || len(out) != 2 {
		t.Fatalf("expected native pair, got nodes=%d skipped=%d", result.NodeCount, result.SkippedCount)
	}
	inner, outer := out[0], out[1]
	if inner["type"] != "shadowsocks" || inner["tag"] != item.Tag || inner["detour"] != outer["tag"] || inner["tag"] == outer["tag"] {
		t.Fatal("inner proxy lost its tag or transport reference")
	}
	if inner["method"] != "2022-blake3-aes-256-gcm" || inner["password"] != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" || inner["plugin"] != nil || inner["plugin_opts"] != nil || inner["network"] != "tcp" {
		t.Fatal("inner SS credentials/options were not preserved as native options")
	}
	if outer["type"] != "shadowtls" || outer["server"] != "node.example.com" || outer["server_port"] != float64(18801) || outer["password"] != `outer;secret=with\separators` || outer["version"] != float64(3) {
		t.Fatal("transport endpoint, version or password changed")
	}
	wantTLS := map[string]any{"enabled": true, "server_name": "www.php.net", "insecure": false, "alpn": []any{"h2", "http/1.1"}, "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}}
	if !reflect.DeepEqual(outer["tls"], wantTLS) {
		t.Fatalf("TLS mismatch: %#v", outer["tls"])
	}
	if before != string(item.RawOptions) {
		t.Fatal("export mutated source node")
	}
	// Re-exporting the native pair must neither re-expand nor break detour.
	var againNodes []ExportNode
	for _, raw := range out {
		data, _ := json.Marshal(raw)
		againNodes = append(againNodes, testNode(string(data), raw["tag"].(string), string(data)))
	}
	again, err := Export(FormatSingbox, againNodes, ExportOptions{UnsupportedPolicy: UnsupportedError})
	if err != nil || string(again.Body) != string(result.Body) {
		t.Fatalf("native output is not stable on re-export: %v", err)
	}
}

func TestExportShadowTLSPluginDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, opts string
		version    float64
		alpn       []any
		insecure   bool
	}{
		{"default_v2", "host=tls.example.com;password=fixture", 2, []any{"h2", "http/1.1"}, false},
		{"v1_no_password", "sni=tls.example.com;version=1;alpn=", 1, nil, false},
		{"aliases", "sni=tls.example.com;version=3;password=fixture;insecure=true;alpn=http/1.1;", 3, []any{"http/1.1"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := shadowTLSPluginNode(t, tc.opts, map[string]any{"plugin": "shadowtls"})
			result, err := Export(FormatSingbox, []ExportNode{item}, ExportOptions{UnsupportedPolicy: UnsupportedError})
			if err != nil {
				t.Fatal(err)
			}
			out := nativeFeedOutbounds(t, result)
			if len(out) != 2 {
				t.Fatal("plugin was not expanded")
			}
			tls := out[1]["tls"].(map[string]any)
			if out[1]["version"] != tc.version || tls["server_name"] != "tls.example.com" || (tls["insecure"] == true) != tc.insecure {
				t.Fatal("plugin defaults or TLS aliases changed")
			}
			if tc.alpn == nil {
				if v, ok := tls["alpn"].([]any); ok && len(v) != 0 {
					t.Fatal("explicit empty ALPN must stay empty")
				}
			} else if !reflect.DeepEqual(tls["alpn"], tc.alpn) {
				t.Fatalf("ALPN changed: %v", tls["alpn"])
			}
			// Generated native pairs must still work with the existing flat
			// exporter, including v1's TLS defaults and explicit empty ALPN.
			var pair []ExportNode
			for _, raw := range out {
				data, _ := json.Marshal(raw)
				pair = append(pair, testNode(string(data), raw["tag"].(string), string(data)))
			}
			flat, err := Export(FormatClashMeta, pair, ExportOptions{UnsupportedPolicy: UnsupportedError})
			if err != nil || flat.NodeCount != 1 || flat.SkippedCount != 0 {
				t.Fatalf("generated pair cannot be flattened: %v", err)
			}
		})
	}
}

func TestExportShadowTLSPluginTagsAndDialer(t *testing.T) {
	item := shadowTLSPluginNode(t, "host=tls.example.com;password=fixture;version=3", map[string]any{
		"detour": "hop", "connect_timeout": "5s", "domain_resolver": "local", "tcp_fast_open": true,
		"udp_over_tcp": true, "multiplex": map[string]any{"enabled": false},
	})
	item.SourceTags = map[string][]string{"a": {"original"}}
	hop := testNode("hop", item.Tag+" [ShadowTLS]", `{"type":"socks","tag":"hop","server":"127.0.0.1","server_port":1080}`)
	hop.SourceTags = map[string][]string{"a": {"hop"}}
	parent := testNode("parent", "parent", `{"type":"http","detour":"original","server":"example.com","server_port":80}`)
	parent.SourceTags = map[string][]string{"a": {"parent"}}
	second := shadowTLSPluginNode(t, "host=other.example.com;password=fixture", nil)
	second.SourceTags = map[string][]string{"b": {"original"}}
	nodes := []ExportNode{item, hop, parent, second}
	result, err := Export(FormatSingbox, nodes, ExportOptions{UnsupportedPolicy: UnsupportedError})
	if err != nil {
		t.Fatal(err)
	}
	out := nativeFeedOutbounds(t, result)
	if len(out) != 6 || result.SkippedCount != 0 {
		t.Fatalf("expected two pairs and two ordinary nodes, got %d", len(out))
	}
	byTag := map[any]map[string]any{}
	for _, raw := range out {
		if byTag[raw["tag"]] != nil {
			t.Fatal("generated tags collide with source tags")
		}
		byTag[raw["tag"]] = raw
	}
	inner := byTag[item.Tag]
	outer := byTag[inner["detour"]]
	if outer["detour"] != hop.Tag || byTag[parent.Tag]["detour"] != item.Tag {
		t.Fatal("existing detours were not preserved through expansion")
	}
	for key, want := range map[string]any{"connect_timeout": "5s", "domain_resolver": "local", "tcp_fast_open": true} {
		if inner[key] != nil || outer[key] != want {
			t.Fatalf("dialer option %s must move to the transport", key)
		}
	}
	if inner["udp_over_tcp"] != true || inner["multiplex"] == nil || inner["network"] != nil || outer["udp_over_tcp"] != nil || outer["multiplex"] != nil {
		t.Fatal("UoT/multiplex must remain on inner SS with UDP available")
	}
	again, _ := Export(FormatSingbox, nodes, ExportOptions{UnsupportedPolicy: UnsupportedError})
	if string(again.Body) != string(result.Body) {
		t.Fatal("tag allocation is not deterministic")
	}
}

func TestExportShadowTLSPluginInvalidOptions(t *testing.T) {
	for _, opts := range []string{
		"", "host=tls.example.com", "password=fixture", "host=tls.example.com;password=fixture;version=0",
		"host=tls.example.com;password=fixture;version=4", "host=tls.example.com;password=fixture;password=duplicate",
		`host=tls.example.com;password=sensitive-fixture\`,
		"host=tls.example.com;password=fixture;insecure=invalid",
		"host=tls.example.com;password=fixture;insecure=true;skip-cert-verify=false",
		"host=tls.example.com;password=fixture;fingerprint=certificate-pin",
		"host=tls.example.com;password=fixture;certificate_public_key_sha256=public-key-pin",
		"host=tls.example.com;version=1;client-fingerprint=chrome",
		"host=tls.example.com;password=fixture;unknown=value",
	} {
		item := shadowTLSPluginNode(t, opts, nil)
		valid := testNode("valid", "valid", `{"type":"http","server":"example.com","server_port":80}`)
		parent := testNode("parent", "parent", `{"type":"http","detour":"original","server":"example.com","server_port":80}`)
		result, err := Export(FormatSingbox, []ExportNode{item, parent, valid}, ExportOptions{})
		if err != nil || result.NodeCount != 1 || result.SkippedCount != 2 || !reflect.DeepEqual(result.SkippedTypes, []string{"http", "shadowsocks"}) {
			t.Fatalf("failed conversion must skip parent and dependency: nodes=%d skipped=%d err=%v", result.NodeCount, result.SkippedCount, err)
		}
		if _, err := Export(FormatSingbox, []ExportNode{item}, ExportOptions{UnsupportedPolicy: UnsupportedError}); err == nil {
			t.Fatal("strict export accepted unsupported plugin options")
		} else if strings.Contains(err.Error(), "sensitive-fixture") {
			t.Fatal("conversion error leaked a password")
		}
	}
	for _, extra := range []map[string]any{
		{"server_port": 0}, {"server_port": 65536}, {"server_port": 443.5}, {"server": ""}, {"method": ""}, {"password": ""},
		{"plugin_opts": true}, {"network": "udp"}, {"network": []string{"tcp", "udp"}}, {"tls": map[string]any{"enabled": true}},
	} {
		item := shadowTLSPluginNode(t, "host=tls.example.com;password=fixture", extra)
		if _, err := Export(FormatSingbox, []ExportNode{item}, ExportOptions{UnsupportedPolicy: UnsupportedError}); err == nil {
			t.Fatalf("strict export accepted unrepresentable SS fields: %v", extra)
		}
	}
}

func TestExportShadowTLSPluginClashKeepsBothLayers(t *testing.T) {
	item := shadowTLSPluginNode(t, "host=www.php.net;password=outer-fixture;version=3", nil)
	result, err := Export(FormatClashMeta, []ExportNode{item}, ExportOptions{UnsupportedPolicy: UnsupportedError})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct{ Proxies []map[string]any }
	if err := yaml.Unmarshal(result.Body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Proxies) != 1 {
		t.Fatal("expected one combined Clash node")
	}
	p := payload.Proxies[0]
	wantOptions := map[string]any{"host": "www.php.net", "password": "outer-fixture", "version": 3}
	if p["type"] != "ss" || p["cipher"] != "2022-blake3-aes-256-gcm" || p["password"] != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" || p["plugin"] != "shadow-tls" || !reflect.DeepEqual(p["plugin-opts"], wantOptions) {
		t.Fatal("Clash output lost the SS layer or ShadowTLS plugin options")
	}
}
