# ShadowTLS Feed Pairing Implementation Plan

> **For agentic workers:** Execute the tasks below in this session; verify each deliverable before continuing.

**Goal:** Export existing SS → ShadowTLS pairs as usable combined subscriptions without inventing SS credentials.

**Architecture:** Resolve detour references against original tags within their source subscription before changing display tags. Clash and URI flatten supported pairs into SS with SIP003 shadow-tls options; sing-box retains native outbounds with repaired detour tags. Orphan transports, missing/ambiguous dependencies, cycles and nonrepresentable combinations follow the feed's skip/error policy.

**Tech Stack:** Go, existing sing-box SIP003 parser, JSON, yaml.v3; no dependency upgrades.

**Spec:** User request: add Shadowsocks + ShadowTLS when exporting subscriptions; existing passwords and encryption methods must come from an actual SS parent.

## Constraints

- No changes to stored source nodes, outbound runtime or live deployment.
- Keep two passwords distinct; preserve TLS verification and native version default 1.
- Never pair nodes by host, display name or a tag from another subscription.
- Keep native sing-box chains native; recommend Clash output for Karing's combined representation.
- Tests use example.com, loopback addresses and fixture passwords only.

## Tasks

- [x] Add `internal/feed/shadowtls_export_test.go`: paired export for all four formats, password escaping and round trip, native detour rewrite, orphan/missing/cyclic/ambiguous references, unsupported TLS, no source mutation.
- [x] Run `go test ./internal/feed -run 'TestExportShadowTLS' -count=1` and confirm failures demonstrate the current gap.
- [x] Add dependency preparation in `internal/feed/shadowtls_export.go`, integrate it through `Export`, and serialize SS plugin queries in `uri.go`.
- [x] Add source-tag metadata to `ExportNode`; populate it from enabled, selected subscription membership in `internal/service/control_plane_feed.go` so deduplicated aliases remain resolvable.
- [x] Extend `internal/api/handler_feed_public_test.go` to cover same-named transport tags in different source subscriptions and aliases after node deduplication.
- [x] Run `go test ./internal/feed ./internal/subscription ./internal/service ./internal/api -count=1`; fix regressions within this change.
- [x] Update `doc/subscription-feeds.md` with format behavior, counts, unsupported policy and deployment/client refresh steps.
- [x] Run `git diff --check` and `go build -trimpath -tags 'with_quic with_wireguard with_grpc with_utls' -o .local/resin-shadowtls-feed.exe ./cmd/resin`.

Additional validation passed: `go test -tags with_utls ./internal/outbound -run 'TestShadowTLSPluginSubscriptionRoundTrip/v3_feed_' -count=1`. Both Clash and URI exports of a native pair were reparsed and carried an actual SS echo through a loopback ShadowTLS v3 service, including a password containing escaped separators. Karing itself and remote subscriber servers were not exercised.

Rollback: rebuild from the preceding commit or restore the preceding executable; no database migration is involved.
