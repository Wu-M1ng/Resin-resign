package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/subscription"
	"github.com/Resinat/Resin/internal/testutil"
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
