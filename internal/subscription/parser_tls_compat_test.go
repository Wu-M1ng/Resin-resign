package subscription

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseGeneralSubscription_ShadowTLSClientFingerprintSeparator(t *testing.T) {
	for _, tc := range []struct{ options, want string }{
		{`host=fixture.invalid;password=pass;`, `host=fixture.invalid;password=pass;client-fingerprint=chrome`},
		{`host=fixture.invalid;password=pass\;`, `host=fixture.invalid;password=pass\;;client-fingerprint=chrome`},
		{`host=fixture.invalid;password=pass\\;`, `host=fixture.invalid;password=pass\\;client-fingerprint=chrome`},
	} {
		input, err := json.Marshal(map[string]any{"proxies": []any{map[string]any{
			"type": "ss", "server": "127.0.0.1", "port": 443, "cipher": "aes-128-gcm", "password": "ss-fixture",
			"plugin": "shadow-tls", "plugin-opts": tc.options, "client-fingerprint": "chrome",
		}}})
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := ParseGeneralSubscription(input)
		if err != nil || len(nodes) != 1 {
			t.Fatalf("nodes=%d err=%v", len(nodes), err)
		}
		got := parseNodeRaw(t, nodes[0].RawOptions)["plugin_opts"]
		if got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}

func TestParseGeneralSubscription_Hysteria2CertificatePins(t *testing.T) {
	const pin = "c083d33c12db8a02d88a5169fb9cee3dae9291fffe38c579891a9dcff55304fb"
	cases := []struct{ name, input string }{
		{"clash", `{"proxies":[{"type":"hysteria2","server":"127.0.0.1","port":443,"password":"fixture","fingerprint":"` + pin + `","client-fingerprint":"chrome"}]}`},
		{"uri", "hy2://fixture@127.0.0.1:443?pinSHA256=" + pin + "&fp=chrome"},
		{"uri_fingerprint", "hy2://fixture@127.0.0.1:443?fingerprint=" + pin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := ParseGeneralSubscription([]byte(tc.input))
			if err != nil || len(nodes) != 1 {
				t.Fatalf("nodes=%d err=%v", len(nodes), err)
			}
			tls := mustMapField(t, parseNodeRaw(t, nodes[0].RawOptions), "tls")
			if _, ok := tls["utls"]; ok {
				t.Fatal("Hysteria2 must not enable uTLS")
			}
			pins := mustSliceField(t, tls, "certificate_sha256")
			if !containsAnyString(pins, pin) {
				t.Fatalf("certificate pin lost: %v", pins)
			}
			if _, ok := tls["certificate_public_key_sha256"]; ok {
				t.Fatal("certificate hash is not a public key hash")
			}
		})
	}
}

func TestParseGeneralSubscription_Hysteria2ClientFingerprintIgnored(t *testing.T) {
	for _, input := range []string{
		`{"proxies":[{"type":"hysteria2","server":"127.0.0.1","port":443,"password":"fixture","client-fingerprint":"chrome"}]}`,
		"hy2://fixture@127.0.0.1:443?fp=chrome",
	} {
		nodes, err := ParseGeneralSubscription([]byte(input))
		if err != nil || len(nodes) != 1 {
			t.Fatalf("nodes=%d err=%v", len(nodes), err)
		}
		if strings.Contains(string(nodes[0].RawOptions), `"utls"`) {
			t.Fatal("uTLS is not supported over Hysteria2 QUIC")
		}
	}
}
