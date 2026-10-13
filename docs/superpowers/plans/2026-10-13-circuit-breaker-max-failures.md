# Circuit Breaker Max Failures and Dormant Probing Implementation Plan

> **For agentic workers:** Execute in this session and verify each deliverable before continuing.

**Goal:** Stop periodic health probes for nodes with consecutive failures exceeding the threshold (default 5) and cap failure counts, automatically reactivating them upon subscription refresh or manual probe.

**Architecture:** 
1. `internal/topology/pool.go`: Cap `FailureCount` at `MaxProbeFailures` (default 5). Do not let failure counters grow unboundedly.
2. `internal/probe/manager.go`: In periodic scan loops (`scanLatency`, `scanEgress`) and `isLatencyProbeDue`, skip enqueuing nodes whose `FailureCount >= MaxProbeFailures`. Manual probes (`TriggerImmediate...`) bypass this check.
3. `internal/topology/subscription_scheduler.go`: When a subscription successfully updates (both added and kept nodes), reset `FailureCount` for nodes associated with the refreshed subscription if they were dormant, giving them a fresh testing budget.
4. `internal/service/control_plane_system.go` & `RuntimeConfig`: Expose `MaxProbeFailures` (default 5, 0 = disabled/unlimited) as a configurable parameter.

**Tech Stack:** Go, atomics (`sync/atomic`), existing Resin topology pool and probe manager; no dependency upgrades.

## Constraints

- Preserve existing `MaxConsecutiveFailures` circuit-breaker trip behavior (e.g. 3 consecutive failures trips circuit open).
- `MaxProbeFailures` defines the ceiling where periodic probing stops (e.g. 5 failures). Must ensure `MaxProbeFailures >= MaxConsecutiveFailures`.
- Manual on-demand probes triggered from API or WebUI lightning button must continue to execute and update node health immediately.
- Shared nodes referenced by multiple subscriptions: resetting failure count on refresh of one subscription gives the node a fresh probe chance.
- Tests must use loopback and mock fetchers only.

## Tasks

- [x] Add unit tests in `internal/topology/pool_test.go` and `internal/probe/manager_test.go` asserting:
  - `FailureCount` is capped at `MaxProbeFailures` (5).
  - Periodic probe scan skips nodes with `FailureCount >= 5`.
  - Manual immediate probe still executes for nodes with `FailureCount >= 5`.
- [x] Implement `FailureCount` capping in `internal/topology/pool.go`.
- [x] Implement dormant probe skipping in `internal/probe/manager.go`.
- [x] Implement failure reset on subscription update in `internal/topology/subscription_scheduler.go`.
- [x] Add tests in `internal/topology/scheduler_test.go` verifying that subscription refresh resets dormant nodes.
- [x] Run `go test ./internal/topology ./internal/probe ./internal/service ./internal/api -count=1`.
- [x] Update `doc/architecture.md` / `doc/release-notes` with dormant probe policy.
- [x] Run `git diff --check` and `go build ./cmd/resin`.

Rollback: Revert to preceding commit; no database migration is involved.
