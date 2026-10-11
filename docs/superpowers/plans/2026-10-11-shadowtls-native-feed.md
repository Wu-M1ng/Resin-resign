# ShadowTLS Native Feed Implementation Plan

> **For agentic workers:** Execute in this session and verify each deliverable before continuing.

**Goal:** Export Resin's combined Shadowsocks + ShadowTLS plugin nodes as native sing-box outbounds usable without Resin's SIP003 extension.

**Architecture:** Expand plugin nodes during dependency preparation, after resolving original references and before propagating dependency failures. Preserve the SS entry tag and credentials; add a uniquely tagged ShadowTLS transport and connect it with `detour`. Resolve original detours on the transport, propagate conversion failures to dependents, and count generated outbounds separately from skipped source nodes.

**Tech Stack:** Go, existing sing-box v1.12.21 option schema and SIP003 parser; no dependency upgrades.

**Spec:** User's combined `2022-blake3-aes-256-gcm` node works via Resin HTTP but fails in Karing with `plugin not found: shadow-tls`. Fix sing-box subscription output and verify Clash retains both layers.

## Constraints

- Preserve separate SS/ShadowTLS passwords, escaped separators, plugin default version 2, ALPN, SNI, verification settings and supported client fingerprints.
- Never silently discard certificate pins: this native sing-box version cannot represent Resin's pin extensions; use the existing skip/error policy.
- Keep dialer settings on the external transport and SS/UoT/multiplex settings on the inner proxy. Default to TCP unless an existing UoT/multiplex setting carries UDP over TCP; reject explicit unsupported raw UDP.
- Test only loopback services and fixture credentials. Do not edit stored subscriptions, runtime plugin behavior or live services.
- Report native sing-box verification separately from unverified Karing import behavior.

## Tasks

- [x] Add `internal/feed/shadowtls_native_test.go` covering the actual combined input, two passwords, defaults/escaping, tags, chains, skip counts, unsupported TLS and unchanged Clash output. Run `go test ./internal/feed -run TestExportShadowTLSPlugin -count=1` and observe the missing expansion.
- [x] Add `internal/feed/shadowtls_native.go` with `expandShadowTLSPlugin(raw map[string]any) (inner, outer map[string]any, err error)`; integrate in `shadowtls_export.go`. Use sing-box's options to validate fields, SIP003's parser for plugin options, and reserve all source display tags before allocating generated transport tags.
- [x] Add `internal/feed/shadowtls_native_integration_test.go` under `with_utls`: feed export → unmodified native sing-box → loopback ShadowTLS → SS echo, including SS2022 and authentication/CA rejection. Never import Resin's outbound package in this test.
- [x] Run `go test -tags with_utls ./internal/feed -count=1` and `go test ./internal/subscription ./internal/service ./internal/api -count=1`.
- [x] Update `doc/subscription-feeds.md` with output shape, limits and Karing refresh instructions. Build `.local/resin-shadowtls-native-feed.exe` using the project's production tags and run `git diff --check`.

Validation passed: native SS2022 over ShadowTLS v1/v2/v3, v3 Chrome uTLS, legacy AES-128-GCM, wrong SS/ShadowTLS passwords, untrusted certificate rejection, native-to-Clash re-export, and public Feed API conversion from Clash source to native output. Integration explicitly asserts the Resin SIP003 plugin is absent. Karing and remote subscription servers were not exercised.

Rollback: use the previous executable; there is no database migration.
