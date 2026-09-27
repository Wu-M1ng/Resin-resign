package feed

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestURIAndBase64Exports(t *testing.T) {
	item := testNode("uri", "trojan / 节点", `{"type":"trojan","server":"example.com","server_port":443,"password":"p@ss word","tls":{"enabled":true,"server_name":"sni.example"}}`)
	result, err := Export(FormatURI, []ExportNode{item}, ExportOptions{})
	if err != nil || result.NodeCount != 1 || !strings.HasPrefix(string(result.Body), "trojan://") {
		t.Fatalf("unexpected uri result: %+v err=%v", result, err)
	}
	if !strings.Contains(string(result.Body), "p%40ss%20word") || !strings.Contains(string(result.Body), "%E8%8A%82%E7%82%B9") {
		t.Fatalf("URI values were not escaped: %s", result.Body)
	}
	encoded, err := Export(FormatV2RayBase64, []ExportNode{item}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded.Body)))
	if err != nil || string(decoded) != string(result.Body) {
		t.Fatalf("base64 body mismatch: decoded=%q uri=%q err=%v", decoded, result.Body, err)
	}
}

func TestVMessURI(t *testing.T) {
	item := testNode("vmess", "vmess-node", `{"type":"vmess","server":"example.com","server_port":443,"uuid":"00000000-0000-0000-0000-000000000001","tls":{"enabled":true,"server_name":"sni.example"},"transport":{"type":"ws","path":"/x"}}`)
	result, err := Export(FormatURI, []ExportNode{item}, ExportOptions{})
	if err != nil || !strings.HasPrefix(string(result.Body), "vmess://") {
		t.Fatalf("unexpected result: %+v err=%v", result, err)
	}
}

func TestHTTPAndSOCKSURIUseStandardUserinfo(t *testing.T) {
	for _, item := range []ExportNode{
		testNode("http", "http-node", `{"type":"http","server":"example.com","server_port":8080,"username":"u@example.com","password":"p ss"}`),
		testNode("socks", "socks-node", `{"type":"socks","server":"example.com","server_port":1080,"username":"u","password":"p"}`),
	} {
		result, err := Export(FormatURI, []ExportNode{item}, ExportOptions{})
		if err != nil {
			t.Fatal(err)
		}
		body := strings.TrimSpace(string(result.Body))
		if !strings.Contains(body, "%40") && strings.Contains(body, "http://") {
			t.Fatalf("HTTP userinfo was not escaped: %s", body)
		}
		if !strings.Contains(body, "@example.com:") {
			t.Fatalf("credentials were not emitted in URI userinfo: %s", body)
		}
		if strings.Contains(body, "username=") || strings.Contains(body, "password=") {
			t.Fatalf("credentials were emitted as non-standard query fields: %s", body)
		}
	}
}

func TestVLESSURIMapsRealityFields(t *testing.T) {
	item := testNode("reality", "reality-node", `{"type":"vless","server":"example.com","server_port":443,"uuid":"00000000-0000-0000-0000-000000000001","tls":{"enabled":true,"server_name":"sni.example","reality":{"enabled":true,"public_key":"public","short_id":"abcd"},"utls":{"enabled":true,"fingerprint":"chrome"}}}`)
	result, err := Export(FormatURI, []ExportNode{item}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body := string(result.Body)
	for _, expected := range []string{"security=reality", "pbk=public", "sid=abcd", "fp=chrome"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing Reality query %q in %s", expected, body)
		}
	}
}
