package feed

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Resinat/Resin/internal/subscription"
	"github.com/sagernet/sing-box/transport/sip003"
)

func shadowTLSPair() []ExportNode {
	return []ExportNode{
		testNode("ss-inner", "source/usable", `{"type":"shadowsocks","tag":"ss","server":"127.0.0.1","server_port":10000,"method":"2022-blake3-aes-256-gcm","password":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","detour":"transport"}`),
		testNode("st-outer", "source/transport", `{"type":"shadowtls","tag":"transport","server":"node.example.com","server_port":443,"version":3,"password":"outer;p=ss\\word","tls":{"enabled":true,"server_name":"tls.example.com","insecure":false,"alpn":["h2","http/1.1"],"utls":{"enabled":true,"fingerprint":"chrome"}}}`),
	}
}

func TestExportShadowTLSPair(t *testing.T) {
	for _, format := range []Format{FormatClashMeta, FormatURI, FormatV2RayBase64, FormatSingbox} {
		t.Run(string(format), func(t *testing.T) {
			nodes := shadowTLSPair()
			before, _ := json.Marshal(nodes)
			result, err := Export(format, nodes, ExportOptions{UnsupportedPolicy: UnsupportedError})
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 1
			if format == FormatSingbox {
				wantCount = 2
			}
			if result.NodeCount != wantCount || result.SkippedCount != 0 || len(result.SkippedTypes) != 0 {
				t.Fatalf("counts: nodes=%d skipped=%d types=%v", result.NodeCount, result.SkippedCount, result.SkippedTypes)
			}
			parsed, err := subscription.ParseGeneralSubscription(result.Body)
			if err != nil || len(parsed) != wantCount {
				t.Fatalf("parse exported nodes=%d err=%v", len(parsed), err)
			}
			var inner map[string]any
			for _, p := range parsed {
				var raw map[string]any
				if err := json.Unmarshal(p.RawOptions, &raw); err != nil {
					t.Fatal(err)
				}
				if raw["type"] == "shadowsocks" {
					inner = raw
				}
			}
			if inner == nil || inner["password"] != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" || inner["method"] != "2022-blake3-aes-256-gcm" {
				t.Fatal("SS credentials changed or inner node missing")
			}
			if format == FormatSingbox {
				if inner["detour"] != "source/transport" {
					t.Fatalf("native detour not rewritten: %v", inner["detour"])
				}
			} else {
				if inner["plugin"] != "shadow-tls" || inner["server"] != "node.example.com" || inner["server_port"] != float64(443) || inner["detour"] != nil {
					t.Fatalf("pair not flattened: %v", inner)
				}
				opts, _ := inner["plugin_opts"].(string)
				args, err := sip003.ParsePluginOptions(opts)
				if err != nil {
					t.Fatal(err)
				}
				for key, want := range map[string]string{"password": `outer;p=ss\word`, "host": "tls.example.com", "version": "3", "client-fingerprint": "chrome", "alpn": "h2,http/1.1", "skip-cert-verify": "false"} {
					if got, _ := args.Get(key); got != want {
						t.Fatalf("plugin %s: got %q want %q", key, got, want)
					}
				}
			}
			after, _ := json.Marshal(nodes)
			if string(before) != string(after) {
				t.Fatal("export mutated source nodes")
			}
		})
	}
}

func TestExportShadowTLSIncompletePairs(t *testing.T) {
	pair := shadowTLSPair()
	for _, tc := range []struct {
		name  string
		nodes []ExportNode
	}{
		{"orphan", pair[1:]},
		{"missing_transport", pair[:1]},
		{"ambiguous", append(shadowTLSPair(), testNode("other-transport", "other/transport", `{"type":"shadowtls","tag":"transport","server":"other.example.com","server_port":443,"tls":{"enabled":true}}`))},
		{"cycle", []ExportNode{testNode("cycle", "cycle", `{"type":"shadowtls","tag":"cycle","detour":"cycle","server":"example.com","server_port":443,"tls":{"enabled":true}}`)}},
	} {
		for _, format := range []Format{FormatClashMeta, FormatURI, FormatV2RayBase64, FormatSingbox} {
			t.Run(tc.name+"/"+string(format), func(t *testing.T) {
				if _, err := Export(format, tc.nodes, ExportOptions{UnsupportedPolicy: UnsupportedError}); err == nil {
					t.Fatal("strict export accepted incomplete pair")
				}
				result, err := Export(format, tc.nodes, ExportOptions{})
				if err != nil || result.NodeCount != 0 || result.SkippedCount != len(tc.nodes) {
					t.Fatalf("skip result: nodes=%d skipped=%d err=%v", result.NodeCount, result.SkippedCount, err)
				}
			})
		}
	}
}

func TestExportShadowTLSRejectsLossyTLSConversion(t *testing.T) {
	nodes := shadowTLSPair()
	nodes[1].RawOptions = json.RawMessage(strings.Replace(string(nodes[1].RawOptions), `"insecure":false`, `"insecure":false,"certificate_path":"private-ca.pem"`, 1))
	for _, format := range []Format{FormatClashMeta, FormatURI, FormatV2RayBase64} {
		if _, err := Export(format, nodes, ExportOptions{UnsupportedPolicy: UnsupportedError}); err == nil {
			t.Fatalf("%s silently dropped custom CA", format)
		}
	}
	if _, err := Export(FormatSingbox, nodes, ExportOptions{UnsupportedPolicy: UnsupportedError}); err != nil {
		t.Fatalf("native chain should preserve TLS: %v", err)
	}
}

func TestExportShadowTLSNativeDefaultVersion(t *testing.T) {
	nodes := shadowTLSPair()
	nodes[1].RawOptions = json.RawMessage(`{"type":"shadowtls","tag":"transport","server":"node.example.com","server_port":443,"tls":{"enabled":true,"server_name":"tls.example.com"}}`)
	result, err := Export(FormatClashMeta, nodes, ExportOptions{UnsupportedPolicy: UnsupportedError})
	if err != nil || !strings.Contains(string(result.Body), "version: 1") {
		t.Fatalf("native default must remain v1: %s err=%v", result.Body, err)
	}
}

func TestExportShadowTLSSharedTransport(t *testing.T) {
	nodes := shadowTLSPair()
	second := testNode("ss-second", "second", strings.Replace(string(nodes[0].RawOptions), `"tag":"ss"`, `"tag":"ss-second"`, 1))
	nodes = append(nodes, second)
	result, err := Export(FormatClashMeta, nodes, ExportOptions{UnsupportedPolicy: UnsupportedError})
	if err != nil || result.NodeCount != 2 || result.SkippedCount != 0 || !reflect.DeepEqual(result.SkippedTypes, []string{}) {
		t.Fatalf("shared transport: nodes=%d skipped=%d err=%v", result.NodeCount, result.SkippedCount, err)
	}
}

func TestExportShadowTLSDoesNotPairAcrossSources(t *testing.T) {
	nodes := shadowTLSPair()
	nodes[0].SourceTags = map[string][]string{"a": {"ss"}}
	nodes[1].SourceTags = map[string][]string{"b": {"transport"}}
	for _, format := range []Format{FormatClashMeta, FormatSingbox} {
		result, err := Export(format, nodes, ExportOptions{})
		if err != nil || result.NodeCount != 0 || result.SkippedCount != 2 {
			t.Fatalf("must not pair a with b: nodes=%d skipped=%d err=%v", result.NodeCount, result.SkippedCount, err)
		}
	}
}

func TestExportShadowTLSRejectsAmbiguousDeduplicatedParent(t *testing.T) {
	nodes := shadowTLSPair()
	nodes[0].SourceTags = map[string][]string{"a": {"ss"}, "b": {"ss"}}
	nodes[1].SourceTags = map[string][]string{"a": {"transport"}}
	other := testNode("other", "other", strings.Replace(string(nodes[1].RawOptions), "node.example.com", "other.example.com", 1))
	other.SourceTags = map[string][]string{"b": {"transport"}}
	nodes = append(nodes, other)
	if _, err := Export(FormatClashMeta, nodes, ExportOptions{UnsupportedPolicy: UnsupportedError}); err == nil || !strings.Contains(err.Error(), "different nodes") {
		t.Fatalf("expected ambiguous source pairing error: %v", err)
	}
}

func TestExportShadowTLSNativeTagsAreUnique(t *testing.T) {
	nodes := shadowTLSPair()
	nodes[0].Tag, nodes[1].Tag = "same", "same"
	result, err := Export(FormatSingbox, nodes, ExportOptions{UnsupportedPolicy: UnsupportedError})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct{ Outbounds []map[string]any }
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Outbounds[0]["tag"] == payload.Outbounds[1]["tag"] || payload.Outbounds[0]["detour"] != payload.Outbounds[1]["tag"] {
		t.Fatal("duplicate display names broke native detour")
	}
}
