package feed

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Resinat/Resin/internal/subscription"
	"github.com/sagernet/sing-box/transport/sip003"
	"gopkg.in/yaml.v3"
)

func TestClashMetaShadowTLSSubscriptionRoundTrip(t *testing.T) {
	input := []byte(`proxies:
  - name: shadowtls-fixture
    type: ss
    server: node.example.com
    port: 18801
    cipher: 2022-blake3-aes-256-gcm
    password: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
    plugin: shadow-tls
    client-fingerprint: chrome
    plugin-opts:
      host: tls.example.com
      password: 'fixture;p=ss\word'
      version: 3
      skip-cert-verify: false
`)
	nodes, err := subscription.ParseGeneralSubscription(input)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("parse input: nodes=%d err=%v", len(nodes), err)
	}
	item := testNode("shadowtls", nodes[0].Tag, string(nodes[0].RawOptions))
	result, err := Export(FormatClashMeta, []ExportNode{item}, ExportOptions{UnsupportedPolicy: UnsupportedError})
	if err != nil || result.NodeCount != 1 {
		t.Fatalf("export: count=%d err=%v", result.NodeCount, err)
	}
	var payload struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(result.Body, &payload); err != nil {
		t.Fatal(err)
	}
	proxy := payload.Proxies[0]
	want := map[string]any{"host": "tls.example.com", "password": `fixture;p=ss\word`, "version": 3, "skip-cert-verify": false}
	if !reflect.DeepEqual(proxy["plugin-opts"], want) {
		t.Fatalf("plugin options lost or mistyped: got %#v, want %#v", proxy["plugin-opts"], want)
	}
	if proxy["plugin"] != "shadow-tls" || proxy["client-fingerprint"] != "chrome" {
		t.Fatalf("plugin/client fingerprint lost: %#v", proxy)
	}
	reparsed, err := subscription.ParseGeneralSubscription(result.Body)
	if err != nil || len(reparsed) != 1 {
		t.Fatalf("parse exported subscription: nodes=%d err=%v", len(reparsed), err)
	}
	var before, after map[string]any
	if err := json.Unmarshal(nodes[0].RawOptions, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reparsed[0].RawOptions, &after); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"method", "password", "plugin", "server", "server_port"} {
		if !reflect.DeepEqual(before[key], after[key]) {
			t.Fatalf("%s changed during round trip", key)
		}
	}
	beforeOpts, err := sip003.ParsePluginOptions(before["plugin_opts"].(string))
	if err != nil {
		t.Fatal(err)
	}
	afterOpts, err := sip003.ParsePluginOptions(after["plugin_opts"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeOpts, afterOpts) {
		t.Fatalf("plugin options changed during round trip: before=%v after=%v", beforeOpts, afterOpts)
	}
}

func TestClashMetaSSPluginOptions(t *testing.T) {
	for _, tc := range []struct {
		name, plugin, wantPlugin string
		options                  any
		want                     map[string]any
	}{
		{"shadowtls_defaults", "shadowtls", "shadow-tls", "host=tls.example.com;password=fixture", map[string]any{"host": "tls.example.com", "password": "fixture", "version": 2}},
		{"shadowtls_v1", "shadow-tls", "shadow-tls", "host=tls.example.com;version=1", map[string]any{"host": "tls.example.com", "version": 1}},
		{"shadowtls_object", "shadow-tls", "shadow-tls", map[string]any{"host": "tls.example.com", "password": "fixture", "version": 3}, map[string]any{"host": "tls.example.com", "password": "fixture", "version": 3}},
		{"obfs", "obfs-local", "obfs", "obfs=http;obfs-host=cdn.example.com", map[string]any{"mode": "http", "host": "cdn.example.com"}},
		{"v2ray", "v2ray-plugin", "v2ray-plugin", `mode=websocket;host=cdn.example.com;path=/a\;b\=c\\d;tls;mux=4`, map[string]any{"mode": "websocket", "host": "cdn.example.com", "path": `/a;b=c\d`, "tls": true, "mux": true}},
		{"numeric_password", "shadow-tls", "shadow-tls", "host=tls.example.com;password=123456;version=3", map[string]any{"host": "tls.example.com", "password": "123456", "version": 3}},
		{"shadowtls_tls_aliases", "shadow-tls", "shadow-tls", "sni=tls.example.com;password=fixture;insecure=true;alpn=h2,http/1.1", map[string]any{"host": "tls.example.com", "password": "fixture", "version": 2, "skip-cert-verify": true, "alpn": []any{"h2", "http/1.1"}}},
		{"v2ray_tls_presence", "v2ray-plugin", "v2ray-plugin", "mode=websocket;tls=0;mux=0", map[string]any{"mode": "websocket", "tls": true, "mux": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"type": "shadowsocks", "server": "node.example.com", "server_port": 443, "method": "aes-128-gcm", "password": "ss-fixture", "plugin": tc.plugin, "plugin_opts": tc.options})
			if err != nil {
				t.Fatal(err)
			}
			result, err := Export(FormatClashMeta, []ExportNode{testNode(tc.name, tc.name, string(raw))}, ExportOptions{UnsupportedPolicy: UnsupportedError})
			if err != nil || result.NodeCount != 1 {
				t.Fatalf("export: count=%d err=%v", result.NodeCount, err)
			}
			var payload struct {
				Proxies []map[string]any `yaml:"proxies"`
			}
			if err := yaml.Unmarshal(result.Body, &payload); err != nil {
				t.Fatal(err)
			}
			proxy := payload.Proxies[0]
			if proxy["plugin"] != tc.wantPlugin || !reflect.DeepEqual(proxy["plugin-opts"], tc.want) {
				t.Fatalf("unexpected plugin export: %#v", proxy)
			}
		})
	}
}

func TestClashMetaInvalidShadowTLSOptionsFollowUnsupportedPolicy(t *testing.T) {
	for _, options := range []any{
		nil, "", "host=tls.example.com", "password=fixture", 123,
		"host=tls.example.com;password=fixture;version=4",
		"host=tls.example.com;password=fixture;password=duplicate",
		`host=tls.example.com;password=sensitive-fixture\`,
		"host=tls.example.com;password=fixture;skip-cert-verify=invalid",
		"host=tls.example.com;password=fixture;skip-cert-verify=false;insecure=true",
		"host=tls.example.com;sni=other.example.com;password=fixture",
		"host=tls.example.com;password=fixture;certificate_public_key_sha256=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	} {
		raw, err := json.Marshal(map[string]any{"type": "shadowsocks", "server": "node.example.com", "server_port": 443, "method": "aes-128-gcm", "password": "ss-fixture", "plugin": "shadow-tls", "plugin_opts": options})
		if err != nil {
			t.Fatal(err)
		}
		item := testNode("invalid-plugin", "invalid-plugin", string(raw))
		valid := testNode("valid", "valid", `{"type":"http","server":"example.com","server_port":80}`)
		result, err := Export(FormatClashMeta, []ExportNode{item, valid}, ExportOptions{})
		if err != nil || result.NodeCount != 1 || result.SkippedCount != 1 || !reflect.DeepEqual(result.SkippedTypes, []string{"shadowsocks"}) {
			t.Fatalf("invalid plugin must be skipped without dropping valid nodes: %+v err=%v", result, err)
		}
		if _, err := Export(FormatClashMeta, []ExportNode{item}, ExportOptions{UnsupportedPolicy: UnsupportedError}); err == nil {
			t.Fatal("invalid plugin should fail strict export")
		} else if strings.Contains(err.Error(), "sensitive-fixture") {
			t.Fatal("plugin parse error leaked its password")
		}
	}
}
