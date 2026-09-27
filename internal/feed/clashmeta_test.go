package feed

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestClashMetaMapsVmessTLSAndWebSocket(t *testing.T) {
	item := testNode("clash", "vmess-node", `{"type":"vmess","server":"example.com","server_port":443,"uuid":"00000000-0000-0000-0000-000000000001","tls":{"enabled":true,"server_name":"sni.example"},"transport":{"type":"ws","path":"/websocket","headers":{"Host":"cdn.example"}}}`)
	result, err := Export(FormatClashMeta, []ExportNode{item}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ContentType != "text/yaml; charset=utf-8" || result.NodeCount != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	var payload struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(result.Body, &payload); err != nil {
		t.Fatal(err)
	}
	proxy := payload.Proxies[0]
	if proxy["name"] != item.Tag || proxy["type"] != "vmess" || proxy["network"] != "ws" {
		t.Fatalf("unexpected proxy: %+v", proxy)
	}
	if !strings.Contains(string(result.Body), "servername: sni.example") {
		t.Fatalf("missing TLS servername: %s", result.Body)
	}
}

func TestClashMetaSkipsMissingRequiredFields(t *testing.T) {
	item := testNode("bad", "bad-http", `{"type":"http","server":"example.com"}`)
	result, err := Export(FormatClashMeta, []ExportNode{item}, ExportOptions{})
	if err != nil || result.NodeCount != 0 || result.SkippedCount != 1 || len(result.SkippedTypes) != 1 || result.SkippedTypes[0] != "http" {
		t.Fatalf("unexpected result: %+v err=%v", result, err)
	}
}

func TestClashMetaPreservesTLSALPNAsList(t *testing.T) {
	item := testNode("alpn", "alpn-node", `{"type":"trojan","server":"example.com","server_port":443,"password":"secret","tls":{"enabled":true,"alpn":["h2","http/1.1"]}}`)
	result, err := Export(FormatClashMeta, []ExportNode{item}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Body), "alpn:") || !strings.Contains(string(result.Body), "- h2") || !strings.Contains(string(result.Body), "- http/1.1") {
		t.Fatalf("ALPN was not emitted as a YAML list: %s", result.Body)
	}
}

func TestClashMetaMapsSocksTLS(t *testing.T) {
	item := testNode("socks-tls", "resin-relay", `{"type":"socks","server":"resin.example.com","server_port":2261,"version":"5","username":"Warp","password":"secret","tls":{"enabled":true,"server_name":"resin.example.com"}}`)
	result, err := Export(FormatClashMeta, []ExportNode{item}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body := string(result.Body)
	for _, want := range []string{"type: socks5", "tls: true", "sni: resin.example.com", "username: Warp"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in SOCKS TLS output: %s", want, body)
		}
	}
}

func TestClashMetaMapsWireGuardAddressesAndPeerKey(t *testing.T) {
	item := testNode("wg", "wg-node", `{"type":"wireguard","server":"wg.example.com","server_port":51820,"private_key":"private","peer_public_key":"peer","local_address":["10.0.0.2/32","2001:db8::2/128"],"mtu":1280,"peers":[{"public_key":"peer"}]}`)
	result, err := Export(FormatClashMeta, []ExportNode{item}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body := string(result.Body)
	if !strings.Contains(body, "private-key: private") || !strings.Contains(body, "public-key: peer") || !strings.Contains(body, "ip: 10.0.0.2/32") || !strings.Contains(body, "ipv6: 2001:db8::2/128") {
		t.Fatalf("wireguard fields were not mapped: %s", body)
	}
	if strings.Contains(body, "peer-public-key") || strings.Contains(body, "local-address") {
		t.Fatalf("sing-box-only wireguard fields leaked into Clash output: %s", body)
	}
}
