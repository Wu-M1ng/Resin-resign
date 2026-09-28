# Per-Feed Relay Credential Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Stop relay subscription feeds from embedding the global `RESIN_PROXY_TOKEN` while preserving ordinary proxy authentication.

**Architecture:** Reuse each feed's existing random public token as its relay SOCKS5 credential. Public rendering passes the request token into the relay node; the SOCKS5 inbound resolves that token against enabled relay feeds and receives the current platform name, so the client cannot select another platform with the feed credential. No plaintext relay credential is persisted and no database migration is needed.

**Tech Stack:** Go, SQLite-backed state engine, existing Feed renderer, existing SOCKS5 inbound, Go tests, Markdown documentation.

**Spec:** `doc/subscription-feeds.md` and the existing Feed/endpoint behavior.

## Global Constraints

- Keep `RESIN_PROXY_TOKEN` for ordinary HTTP/SOCKS5 access.
- Do not persist a new plaintext credential.
- Keep Feed token rotation and Feed enable/disable revocation semantics.
- Relay remains restricted to Platform-backed feeds.
- Existing non-relay Feed formats and endpoint configuration remain compatible.

---

### Task 1: Define failing coverage

**Files:**
- Modify: `internal/service/control_plane_feed_relay_test.go`
- Modify: `internal/proxy/socks5_test.go`

- [ ] Assert a relay node uses the Feed token and never `RESIN_PROXY_TOKEN`.
- [ ] Assert a relay credential resolver can accept a scoped token and return its platform while rejecting unknown tokens.

### Task 2: Render scoped relay credentials

**Files:**
- Modify: `internal/service/control_plane_feed.go`
- Modify: `internal/service/control_plane_feed_relay_test.go`

- [ ] Pass the public Feed token through `RenderFeed` into the renderer.
- [ ] Include the token as the relay node password only for public Feed rendering.
- [ ] Keep admin preview output free of credentials and separate preview/public cache entries.
- [ ] Remove the dependency on `EnvCfg.ProxyToken` from relay node generation.

### Task 3: Authorize scoped SOCKS5 relay credentials

**Files:**
- Modify: `internal/proxy/socks5.go`
- Modify: `internal/proxy/socks5_test.go`
- Modify: `internal/service/control_plane_feed.go`

- [ ] Add a resolver callback type to the SOCKS5 configuration.
- [ ] Resolve a non-global password through enabled Feed token hashes.
- [ ] Route accepted scoped credentials to the resolver's platform, ignoring a forged platform name.
- [ ] Preserve existing global-token and empty-token compatibility behavior.

### Task 4: Wire runtime and document behavior

**Files:**
- Modify: `cmd/resin/app_runtime.go`
- Modify: `doc/subscription-feeds.md`

- [ ] Wire `ControlPlaneService` as the resolver for the SOCKS5 inbound.
- [ ] Document that relay passwords are Feed-scoped and rotate with the Feed token.
- [ ] Document that old clients refresh their Feed after token rotation.

### Task 5: Validate

- [ ] Run focused service and proxy tests.
- [ ] Run `gofmt` on modified Go files.
- [ ] Run `go test ./internal/service ./internal/proxy ./internal/api ./cmd/resin`.
- [ ] Run the web build only if frontend files are touched.

