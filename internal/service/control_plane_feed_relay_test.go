package service

import (
	"encoding/json"
	"testing"

	"github.com/Resinat/Resin/internal/config"
	"github.com/Resinat/Resin/internal/model"
)

func TestBuildRelayFeedNodesUsesPublicEndpointAndProxyToken(t *testing.T) {
	svc := &ControlPlaneService{EnvCfg: &config.EnvConfig{ProxyToken: "proxy-secret"}}
	nodes, err := svc.buildRelayFeedNodes(model.SubscriptionFeed{
		Name:      "Warp feed",
		RelayHost: "resin.example.com",
		RelayPort: 2261,
	}, "Warp platform", true)
	if err != nil {
		t.Fatalf("buildRelayFeedNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("relay nodes = %d, want 1", len(nodes))
	}
	var object map[string]any
	if err := json.Unmarshal(nodes[0].RawOptions, &object); err != nil {
		t.Fatalf("decode relay node: %v", err)
	}
	for key, want := range map[string]any{
		"type": "socks", "server": "resin.example.com", "server_port": float64(2261),
		"version": "5", "username": "Warp platform", "password": "proxy-secret",
	} {
		if object[key] != want {
			t.Fatalf("relay node %s = %#v, want %#v", key, object[key], want)
		}
	}
	if object["server"] == "warp" {
		t.Fatal("relay node leaked internal warp host")
	}
}

func TestBuildRelayFeedNodesReturnsEmptyWhenPlatformHasNoLiveNodes(t *testing.T) {
	svc := &ControlPlaneService{}
	nodes, err := svc.buildRelayFeedNodes(model.SubscriptionFeed{RelayHost: "resin.example.com", RelayPort: 2261}, "Warp", false)
	if err != nil {
		t.Fatalf("buildRelayFeedNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("relay nodes = %d, want empty", len(nodes))
	}
}

func TestValidateFeedSettingsRelayRequiresPlatformAndHost(t *testing.T) {
	baseFormats := []string{"singbox"}
	if err := validateFeedSettings("feed", "", []string{"sub"}, true, "resin.example.com", 2261, false, "", "singbox", baseFormats, "skip"); err == nil {
		t.Fatal("relay feed without platform source was accepted")
	}
	if err := validateFeedSettings("feed", "platform", nil, true, "https://resin.example.com", 2261, false, "", "singbox", baseFormats, "skip"); err == nil {
		t.Fatal("relay host with URL scheme was accepted")
	}
	if err := validateFeedSettings("feed", "platform", nil, true, "resin.example.com", 2261, false, "", "singbox", baseFormats, "skip"); err != nil {
		t.Fatalf("valid relay settings rejected: %v", err)
	}
}
