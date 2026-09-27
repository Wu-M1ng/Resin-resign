package feed

import (
	"encoding/json"
	"testing"
)

func TestSingboxOverridesTagAndPreservesOptions(t *testing.T) {
	item := testNode("singbox", "订阅A/节点1", `{"type":"vmess","tag":"old","server":"example.com","server_port":443,"uuid":"00000000-0000-0000-0000-000000000001"}`)
	result, err := Export(FormatSingbox, []ExportNode{item}, ExportOptions{Pretty: true})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Outbounds) != 1 || payload.Outbounds[0]["tag"] != item.Tag || payload.Outbounds[0]["server"] != "example.com" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}
