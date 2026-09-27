package feed

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Resinat/Resin/internal/node"
)

func testNode(hashSeed, tag, raw string) ExportNode {
	return ExportNode{Hash: node.HashFromRawOptions([]byte(hashSeed)), Tag: tag, RawOptions: json.RawMessage(raw)}
}

func TestExportEmptyProducesValidConfigs(t *testing.T) {
	for _, format := range []Format{FormatSingbox, FormatClashMeta, FormatURI, FormatV2RayBase64} {
		result, err := Export(format, nil, ExportOptions{})
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if len(result.Body) == 0 || result.NodeCount != 0 || result.SkippedCount != 0 {
			t.Fatalf("%s: unexpected empty result: %+v", format, result)
		}
	}
}

func TestExportDeduplicatesWithoutMutatingInput(t *testing.T) {
	nodeA := testNode("a", "first", `{"type":"http","server":"example.com","server_port":80}`)
	nodeB := nodeA
	nodeB.Tag = "second"
	originalTag := nodeA.Tag
	result, err := Export(FormatSingbox, []ExportNode{nodeA, nodeB}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.NodeCount != 1 || strings.Count(string(result.Body), `"type":"http"`) != 1 {
		t.Fatalf("unexpected dedup result: %+v body=%s", result, result.Body)
	}
	if nodeA.Tag != originalTag || string(nodeA.RawOptions) != `{"type":"http","server":"example.com","server_port":80}` {
		t.Fatal("export mutated input node")
	}
}

func TestExportUnknownFormatAndPolicy(t *testing.T) {
	if _, err := Export(Format("unknown"), nil, ExportOptions{}); err == nil {
		t.Fatal("expected unknown format error")
	}
	unsupported := testNode("unsupported", "x", `{"type":"tor"}`)
	if _, err := Export(FormatClashMeta, []ExportNode{unsupported}, ExportOptions{UnsupportedPolicy: UnsupportedError}); err == nil {
		t.Fatal("expected unsupported policy error")
	}
	result, err := Export(FormatClashMeta, []ExportNode{unsupported}, ExportOptions{})
	if err != nil || result.SkippedCount != 1 || len(result.SkippedTypes) != 1 || result.SkippedTypes[0] != "tor" {
		t.Fatalf("unexpected skip result: %+v err=%v", result, err)
	}
}
