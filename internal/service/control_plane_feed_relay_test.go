package service

import (
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/feed"
	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/platform"
	"github.com/Resinat/Resin/internal/state"
	"github.com/Resinat/Resin/internal/topology"
)

func TestBuildRelayFeedNodesUsesPublicEndpointAndFeedToken(t *testing.T) {
	const feedToken = "feed-scoped-secret"
	const globalProxyToken = "global-proxy-secret"

	svc := &ControlPlaneService{}
	nodes, err := svc.buildRelayFeedNodes(model.SubscriptionFeed{
		Name:            "Warp feed",
		RelayHost:       "resin.example.com",
		RelayPort:       2261,
		RelayTLS:        true,
		RelayServerName: "resin.example.com",
	}, "Warp platform", true, feedToken)
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
		"version": "5", "username": "Warp platform", "password": feedToken,
	} {
		if object[key] != want {
			t.Fatalf("relay node %s = %#v, want %#v", key, object[key], want)
		}
	}
	if object["server"] == "warp" {
		t.Fatal("relay node leaked internal warp host")
	}
	if object["password"] == globalProxyToken {
		t.Fatal("relay node leaked the global proxy token")
	}
}

func TestBuildRelayFeedNodesOmitsPasswordForPreview(t *testing.T) {
	svc := &ControlPlaneService{}
	nodes, err := svc.buildRelayFeedNodes(model.SubscriptionFeed{
		Name:            "Warp feed",
		RelayHost:       "resin.example.com",
		RelayPort:       2261,
		RelayTLS:        true,
		RelayServerName: "resin.example.com",
	}, "Warp platform", true, "")
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
	if _, ok := object["password"]; ok {
		t.Fatal("preview relay node should not contain a password")
	}
}

func TestBuildRelayFeedNodesReturnsEmptyWhenPlatformHasNoLiveNodes(t *testing.T) {
	svc := &ControlPlaneService{}
	nodes, err := svc.buildRelayFeedNodes(model.SubscriptionFeed{RelayHost: "resin.example.com", RelayPort: 2261}, "Warp", false, "feed-token")
	if err != nil {
		t.Fatalf("buildRelayFeedNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("relay nodes = %d, want empty", len(nodes))
	}
}

func TestResolveRelayCredentialUsesEnabledFeedBinding(t *testing.T) {
	engine, closer, err := state.PersistenceBootstrap(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	defer closer.Close()

	pool := topology.NewGlobalNodePool(topology.PoolConfig{
		GeoLookup:              func(netip.Addr) string { return "" },
		MaxConsecutiveFailures: func() int { return 3 },
		LatencyDecayWindow:     func() time.Duration { return time.Minute },
	})
	plat := platform.NewPlatform("platform-1", "BoundPlatform", nil, nil)
	pool.RegisterPlatform(plat)

	const token = "feed-scoped-secret"
	feedModel := model.SubscriptionFeed{
		ID:                 "feed-1",
		Name:               "Relay feed",
		PlatformID:         plat.ID,
		RelayEnabled:       true,
		RelayHost:          "resin.example.com",
		RelayPort:          2261,
		RelayTLS:           true,
		RelayServerName:    "resin.example.com",
		DefaultFormat:      "singbox",
		EnabledFormatsJSON: `["singbox"]`,
		UnsupportedPolicy:  "skip",
		Enabled:            true,
		TokenHash:          feed.HashToken(token),
		CreatedAtNs:        1,
		UpdatedAtNs:        2,
	}
	if err := engine.InsertFeed(feedModel); err != nil {
		t.Fatalf("InsertFeed: %v", err)
	}

	svc := &ControlPlaneService{Engine: engine, Pool: pool}
	got, ok := svc.ResolveRelayCredential(token)
	if !ok || got.PlatformName != plat.Name || got.RelayPort != 2261 || !got.RequireTLS || got.Revoked == nil {
		t.Fatalf("ResolveRelayCredential = (%+v, %v), want bound TLS credential", got, ok)
	}
	if unknown, ok := svc.ResolveRelayCredential("wrong-token"); ok || unknown.PlatformName != "" {
		t.Fatalf("unknown token resolved as (%+v, %v)", unknown, ok)
	}

	if _, err := svc.UpdateFeed(feedModel.ID, json.RawMessage(`{"enabled":false}`)); err != nil {
		t.Fatalf("UpdateFeed: %v", err)
	}
	select {
	case <-got.Revoked:
	case <-time.After(time.Second):
		t.Fatal("Feed update did not revoke active relay credentials")
	}
	if disabled, ok := svc.ResolveRelayCredential(token); ok || disabled.PlatformName != "" {
		t.Fatalf("disabled relay feed resolved as (%+v, %v)", disabled, ok)
	}
	if _, err := svc.UpdateFeed(feedModel.ID, json.RawMessage(`{"enabled":true}`)); err != nil {
		t.Fatalf("re-enable Feed: %v", err)
	}
	active, ok := svc.ResolveRelayCredential(token)
	if !ok || active.Revoked == nil {
		t.Fatalf("re-enabled Feed did not resolve: %+v, %v", active, ok)
	}
	rotated, err := svc.RotateFeedToken(feedModel.ID)
	if err != nil || rotated.PublicToken == "" {
		t.Fatalf("RotateFeedToken: response=%+v err=%v", rotated, err)
	}
	select {
	case <-active.Revoked:
	case <-time.After(time.Second):
		t.Fatal("token rotation did not revoke the previous relay credential")
	}
	rotatedCredential, ok := svc.ResolveRelayCredential(rotated.PublicToken)
	if !ok {
		t.Fatal("rotated Feed token did not resolve")
	}
	if err := svc.DeleteFeed(feedModel.ID); err != nil {
		t.Fatalf("DeleteFeed: %v", err)
	}
	select {
	case <-rotatedCredential.Revoked:
	case <-time.After(time.Second):
		t.Fatal("Feed deletion did not revoke the active relay credential")
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
	if err := validateFeedSettings("feed", "platform", nil, true, "resin.example.com", 2261, false, "", "singbox", baseFormats, "skip"); err == nil {
		t.Fatal("plaintext relay settings were accepted")
	}
	if err := validateFeedSettings("feed", "platform", nil, true, "resin.example.com", 2261, true, "resin.example.com", "singbox", baseFormats, "skip"); err != nil {
		t.Fatalf("valid TLS relay settings rejected: %v", err)
	}
}
