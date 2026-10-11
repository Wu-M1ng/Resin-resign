package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
	"gopkg.in/yaml.v3"
)

func TestPublicFeedLifecycleKeepsTokenAcrossEdits(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	platformID := mustCreatePlatform(t, srv, "public-feed-platform")

	sub := subscription.NewSubscription("feed-sub", "Feed source", "https://example.com/feed", true, false)
	cp.SubMgr.Register(sub)
	raw := []byte(`{"type":"http","server":"example.com","server_port":80}`)
	hash := node.HashFromRawOptions(raw)
	cp.Pool.AddNodeFromSub(hash, raw, sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"source-node"}})
	entry, ok := cp.Pool.GetEntry(hash)
	if !ok {
		t.Fatal("node was not added to pool")
	}
	entry.SetEgressIP(netip.MustParseAddr("203.0.113.8"))
	entry.LatencyTable.Update("example.com", 20*time.Millisecond, 10*time.Minute)
	ob := testutil.NewNoopOutbound()
	entry.Outbound.Store(&ob)
	cp.Pool.RecordResult(hash, true)
	cp.Pool.NotifyNodeDirty(hash)

	created := doJSONRequest(t, srv, http.MethodPost, "/api/v1/feeds", map[string]any{
		"name":               "Public feed",
		"platform_id":        platformID,
		"subscription_ids":   []string{},
		"default_format":     "uri",
		"enabled_formats":    []string{"uri"},
		"unsupported_policy": "skip",
	}, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create feed status: got %d, body=%s", created.Code, created.Body.String())
	}
	createdBody := decodeJSONMap(t, created)
	token, _ := createdBody["public_token"].(string)
	feedID, _ := createdBody["id"].(string)
	if token == "" || feedID == "" {
		t.Fatalf("create feed response missing token/id: %s", created.Body)
	}

	first := publicFeedRequest(srv, http.MethodGet, "/sub/"+token+"/uri", "")
	if first.Code != http.StatusOK || first.Body.Len() == 0 {
		t.Fatalf("public feed response: status=%d body=%q", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("public feed response missing ETag")
	}

	head := publicFeedRequest(srv, http.MethodHead, "/sub/"+token+"/uri", "")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD response: status=%d body=%q", head.Code, head.Body.String())
	}
	notModified := publicFeedRequest(srv, http.MethodGet, "/sub/"+token+"/uri", etag)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional response: status=%d body=%q", notModified.Code, notModified.Body.String())
	}

	updated := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/feeds/"+feedID, map[string]any{
		"pretty": false,
	}, true)
	if updated.Code != http.StatusOK {
		t.Fatalf("edit feed status: got %d, body=%s", updated.Code, updated.Body.String())
	}
	stillValid := publicFeedRequest(srv, http.MethodGet, "/sub/"+token+"/uri", "")
	if stillValid.Code != http.StatusOK {
		t.Fatalf("token changed after feed edit: status=%d body=%s", stillValid.Code, stillValid.Body.String())
	}

	rotated := doJSONRequest(t, srv, http.MethodPost, "/api/v1/feeds/"+feedID+"/actions/rotate-token", nil, true)
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate token status: got %d, body=%s", rotated.Code, rotated.Body.String())
	}
	rotatedBody := decodeJSONMap(t, rotated)
	newToken, _ := rotatedBody["public_token"].(string)
	if newToken == "" || newToken == token {
		t.Fatalf("rotate response did not issue a new token: %s", rotated.Body)
	}
	old := publicFeedRequest(srv, http.MethodGet, "/sub/"+token+"/uri", "")
	if old.Code != http.StatusNotFound {
		t.Fatalf("old token status after rotation: got %d", old.Code)
	}

	disabled := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/feeds/"+feedID, map[string]any{"enabled": false}, true)
	if disabled.Code != http.StatusOK {
		t.Fatalf("disable feed status: got %d, body=%s", disabled.Code, disabled.Body.String())
	}
	newDisabled := publicFeedRequest(srv, http.MethodGet, "/sub/"+newToken+"/uri", "")
	if newDisabled.Code != http.StatusNotFound {
		t.Fatalf("disabled token status: got %d", newDisabled.Code)
	}
}

func TestPublicFeedCanUseOriginalSubscriptionSourceWithoutPlatform(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	sub := subscription.NewSubscription("source-only-sub", "Source only", "https://example.com/feed", true, false)
	cp.SubMgr.Register(sub)
	raw := []byte(`{"type":"http","server":"example.com","server_port":80}`)
	hash := node.HashFromRawOptions(raw)
	cp.Pool.AddNodeFromSub(hash, raw, sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{"source-node"}})

	created := doJSONRequest(t, srv, http.MethodPost, "/api/v1/feeds", map[string]any{
		"name":               "Source-only feed",
		"platform_id":        "",
		"subscription_ids":   []string{sub.ID},
		"default_format":     "uri",
		"enabled_formats":    []string{"uri"},
		"unsupported_policy": "skip",
	}, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create source-only feed status: got %d, body=%s", created.Code, created.Body.String())
	}
	body := decodeJSONMap(t, created)
	token, _ := body["public_token"].(string)
	if token == "" {
		t.Fatalf("source-only feed response missing token: %s", created.Body)
	}

	public := publicFeedRequest(srv, http.MethodGet, "/sub/"+token+"/uri", "")
	if public.Code != http.StatusOK || public.Body.Len() == 0 {
		t.Fatalf("source-only public feed response: status=%d body=%q", public.Code, public.Body.String())
	}
}

func TestPublicFeedPreservesShadowTLSPluginLayers(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	sub := subscription.NewSubscription("shadowtls-feed-sub", "Plugin fixture", "https://example.com/feed", true, false)
	cp.SubMgr.Register(sub)
	nodes, err := subscription.ParseGeneralSubscription([]byte(`{"proxies":[{"name":"shadowtls-fixture","type":"ss","server":"node.example.com","port":18801,"cipher":"2022-blake3-aes-256-gcm","password":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","plugin":"shadow-tls","client-fingerprint":"chrome","plugin-opts":{"host":"tls.example.com","password":"plugin-fixture","version":3}}]}`))
	if err != nil || len(nodes) != 1 {
		t.Fatalf("parse source: nodes=%d err=%v", len(nodes), err)
	}
	hash := node.HashFromRawOptions(nodes[0].RawOptions)
	cp.Pool.AddNodeFromSub(hash, nodes[0].RawOptions, sub.ID)
	sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{nodes[0].Tag}})
	created := doJSONRequest(t, srv, http.MethodPost, "/api/v1/feeds", map[string]any{
		"name": "Plugin feed", "subscription_ids": []string{sub.ID},
		"default_format": "clash-meta", "enabled_formats": []string{"clash-meta", "singbox"}, "unsupported_policy": "error",
	}, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create feed: status=%d body=%s", created.Code, created.Body.String())
	}
	token, _ := decodeJSONMap(t, created)["public_token"].(string)
	public := publicFeedRequest(srv, http.MethodGet, "/sub/"+token+"/clash-meta", "")
	if public.Code != http.StatusOK {
		t.Fatalf("fetch feed: status=%d body=%s", public.Code, public.Body.String())
	}
	var payload struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(public.Body.Bytes(), &payload); err != nil || len(payload.Proxies) != 1 {
		t.Fatalf("decode feed: proxies=%d err=%v", len(payload.Proxies), err)
	}
	proxy := payload.Proxies[0]
	opts, ok := proxy["plugin-opts"].(map[string]any)
	if !ok || opts["host"] != "tls.example.com" || opts["password"] != "plugin-fixture" || opts["version"] != 3 {
		t.Fatalf("public feed lost plugin options: %#v", proxy)
	}
	if proxy["client-fingerprint"] != "chrome" || proxy["plugin"] != "shadow-tls" {
		t.Fatalf("public feed lost plugin/client fingerprint: %#v", proxy)
	}
	native := publicFeedRequest(srv, http.MethodGet, "/sub/"+token+"/singbox", "")
	if native.Code != http.StatusOK {
		t.Fatalf("native feed: status=%d body=%s", native.Code, native.Body.String())
	}
	var config struct{ Outbounds []map[string]any }
	if err := json.Unmarshal(native.Body.Bytes(), &config); err != nil || len(config.Outbounds) != 2 {
		t.Fatalf("native plugin expansion: nodes=%d err=%v", len(config.Outbounds), err)
	}
	inner, outer := config.Outbounds[0], config.Outbounds[1]
	if inner["type"] != "shadowsocks" || inner["password"] != proxy["password"] || inner["method"] != proxy["cipher"] || inner["plugin"] != nil || inner["plugin_opts"] != nil || inner["detour"] != outer["tag"] {
		t.Fatal("public native feed lost inner SS credentials or detour")
	}
	if outer["type"] != "shadowtls" || outer["password"] != opts["password"] || outer["server"] != proxy["server"] || outer["version"] != float64(3) {
		t.Fatal("public native feed lost outer ShadowTLS fields")
	}
	if native.Header().Get("X-Resin-Skipped-Count") != "0" {
		t.Fatal("generated transport must not produce a skipped source node")
	}
}

func TestPublicFeedCombinesShadowTLSWithinSourceSubscription(t *testing.T) {
	srv, cp, _ := newControlPlaneTestServer(t)
	sourceIDs := []string{"source-a", "source-b"}
	for _, id := range sourceIDs {
		sub := subscription.NewSubscription(id, id, "https://example.com/feed", true, false)
		cp.SubMgr.Register(sub)
		for _, raw := range []map[string]any{
			{"type": "shadowsocks", "tag": "ss", "server": "127.0.0.1", "server_port": 10000, "method": "aes-128-gcm", "password": "inner-" + id, "detour": "transport"},
			{"type": "shadowtls", "tag": "transport", "server": id + ".example.com", "server_port": 443, "version": 3, "password": "outer-" + id, "tls": map[string]any{"enabled": true, "server_name": "tls.example.com"}},
		} {
			originalTag := raw["tag"].(string)
			// Simulate a pool entry first inserted under a different alias.
			if raw["type"] == "shadowtls" {
				raw["tag"] = "old-transport-alias"
			}
			encoded, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			hash := node.HashFromRawOptions(encoded)
			cp.Pool.AddNodeFromSub(hash, encoded, id)
			sub.ManagedNodes().StoreNode(hash, subscription.ManagedNode{Tags: []string{originalTag}})
		}
	}
	created := doJSONRequest(t, srv, http.MethodPost, "/api/v1/feeds", map[string]any{
		"name": "Paired feed", "subscription_ids": sourceIDs,
		"default_format": "clash-meta", "enabled_formats": []string{"clash-meta", "singbox"}, "unsupported_policy": "error",
	}, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create feed: status=%d body=%s", created.Code, created.Body.String())
	}
	token, _ := decodeJSONMap(t, created)["public_token"].(string)
	public := publicFeedRequest(srv, http.MethodGet, "/sub/"+token+"/clash-meta", "")
	if public.Code != http.StatusOK {
		t.Fatalf("fetch feed: status=%d body=%s", public.Code, public.Body.String())
	}
	var payload struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(public.Body.Bytes(), &payload); err != nil || len(payload.Proxies) != 2 {
		t.Fatalf("decode feed: nodes=%d err=%v", len(payload.Proxies), err)
	}
	for _, proxy := range payload.Proxies {
		id := "source-a"
		if proxy["name"] == "source-b/ss" {
			id = "source-b"
		}
		opts, _ := proxy["plugin-opts"].(map[string]any)
		if proxy["server"] != id+".example.com" || proxy["password"] != "inner-"+id || opts["password"] != "outer-"+id {
			t.Fatalf("cross-source pairing or missing credentials: %#v", proxy)
		}
	}
	native := publicFeedRequest(srv, http.MethodGet, "/sub/"+token+"/singbox", "")
	if native.Code != http.StatusOK {
		t.Fatalf("native feed: status=%d body=%s", native.Code, native.Body.String())
	}
	var config struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(native.Body.Bytes(), &config); err != nil || len(config.Outbounds) != 4 {
		t.Fatalf("native config: nodes=%d err=%v", len(config.Outbounds), err)
	}
	for _, ob := range config.Outbounds {
		if ob["type"] == "shadowsocks" {
			id := "source-a"
			if ob["tag"] == "source-b/ss" {
				id = "source-b"
			}
			if ob["detour"] != id+"/transport" {
				t.Fatalf("native detour mismatch: %#v", ob)
			}
		}
	}
}

func TestCreateFeedRequiresExactlyOneSource(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	base := map[string]any{
		"name":               "Invalid source selection",
		"default_format":     "uri",
		"enabled_formats":    []string{"uri"},
		"unsupported_policy": "skip",
	}

	for name, source := range map[string]map[string]any{
		"neither": {"platform_id": "", "subscription_ids": []string{}},
		"both":    {"platform_id": "missing-platform", "subscription_ids": []string{"missing-subscription"}},
	} {
		t.Run(name, func(t *testing.T) {
			request := make(map[string]any, len(base)+len(source))
			for key, value := range base {
				request[key] = value
			}
			for key, value := range source {
				request[key] = value
			}
			rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/feeds", request, true)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func publicFeedRequest(srv *Server, method, path, etag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(nil))
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPublicFeedRateLimiter(t *testing.T) {
	limiter := newPublicFeedRateLimiter(2, time.Minute)
	request := httptest.NewRequest(http.MethodGet, "/sub/token", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	if ok, _ := limiter.allow(request, "token"); !ok {
		t.Fatal("first request was unexpectedly rejected")
	}
	if ok, _ := limiter.allow(request, "token"); !ok {
		t.Fatal("second request was unexpectedly rejected")
	}
	ok, retryAfter := limiter.allow(request, "token")
	if ok || retryAfter <= 0 {
		t.Fatalf("third request: allowed=%v retry_after=%d", ok, retryAfter)
	}
}
