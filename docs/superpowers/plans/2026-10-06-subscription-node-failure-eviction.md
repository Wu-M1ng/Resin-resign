# Subscription Node Failure Eviction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an opt-in per-subscription threshold that automatically quarantines a node after repeated active health-probe failures, without treating subscription-download failures as node failures or deleting shared nodes from other subscriptions.

**Architecture:** Keep `MaxConsecutiveFailures` as the global circuit-breaker threshold and add `node_evict_after_failures` as a subscription policy. Introduce a separate active-probe failure counter on `NodeEntry`, because the existing `FailureCount` also receives passive proxy-request results. Reuse the existing `Evicted` subscription-node state and two-pass cleanup; removing one subscription reference deletes the global node only when no other non-evicted subscription references remain. Add an explicit restore action because an evicted hash must not be silently re-added on every feed refresh.

**Tech Stack:** Go, SQLite migrations, React/TypeScript WebUI, existing `SubscriptionScheduler`, `EphemeralCleaner`, `GlobalNodePool`, and API contract tests.

**Spec:** `DESIGN.md` sections “订阅与节点池”, “健康探测与熔断”, and “订阅 API”; this plan extends those contracts with the fields and actions described below.

## Global Constraints

- Existing defaults remain unchanged: `node_evict_after_failures=0` disables automatic count-based eviction.
- `MaxConsecutiveFailures` continues to control circuit opening and platform routing visibility.
- Subscription fetch/parse failures never evict existing nodes.
- Passive proxy-request failures do not increment the new active-probe eviction counter.
- Eviction is per subscription; a shared node remains in the global pool while another non-evicted subscription references it.
- Existing `ephemeral` and `ephemeral_node_evict_delay` behavior remains supported.
- Every automatic eviction is confirmed by the existing two-pass TOCTOU check and persisted through the cache dirty-set.
- An evicted node is not re-added merely because the same hash appears in a later refresh; restoration is explicit.

---

### Task 1: Extend the subscription policy and persistence contract

**Files:**
- Modify: `internal/model/models.go`
- Modify: `internal/subscription/subscription.go`
- Modify: `internal/service/control_plane_subscription.go`
- Modify: `internal/state/repo_state.go`
- Create: `internal/state/migrations/state/000015_subscriptions_add_node_evict_after_failures.up.sql`
- Create: `internal/state/migrations/state/000015_subscriptions_add_node_evict_after_failures.down.sql`
- Modify: `webui/src/features/subscriptions/types.ts`

**Interfaces:**
- Add `NodeEvictAfterFailures int` to `model.Subscription` and the runtime `subscription.Subscription`.
- Add JSON field `node_evict_after_failures` to create/update/response contracts.
- `0` means disabled; positive values are valid; negative values return `400 INVALID_ARGUMENT`.
- New subscriptions default to `0`; existing rows receive `0` through the migration default.

- [ ] **Step 1: Write state and service contract tests**

Add tests covering the default value, a valid positive value, and rejection of `-1` for both create and patch requests. Add a state-repository round-trip assertion so the value survives restart.

- [ ] **Step 2: Run the focused tests and verify the new assertions fail**

Run: `go test ./internal/service ./internal/state -run 'Subscription|StateRepo' -count=1`

Expected: FAIL because the field, migration, and SQL columns do not exist yet.

- [ ] **Step 3: Add the SQLite migration and update state SQL**

Use:

```sql
ALTER TABLE subscriptions
ADD COLUMN node_evict_after_failures INTEGER NOT NULL DEFAULT 0;
```

Update `UpsertSubscription` and `ListSubscriptions` in `internal/state/repo_state.go`, and copy the value during bootstrap and service update flows.

- [ ] **Step 4: Add runtime getters/setters and API validation**

Expose a thread-safe `NodeEvictAfterFailures()` getter and setter on `subscription.Subscription`. Parse the JSON field alongside the existing `ephemeral` fields and reject values below zero.

- [ ] **Step 5: Update TypeScript request/response types**

Add the optional create/update field and required response field to `webui/src/features/subscriptions/types.ts`.

- [ ] **Step 6: Run the focused tests and verify they pass**

Run: `go test ./internal/service ./internal/state ./internal/api -run 'Subscription|StateRepo' -count=1`

Expected: PASS.

### Task 2: Track active probe failures separately from passive failures

**Files:**
- Modify: `internal/node/entry.go`
- Modify: `internal/topology/pool.go`
- Modify: `internal/probe/manager.go`
- Modify: `internal/model/models.go`
- Modify: `internal/state/repo_cache.go`
- Modify: `internal/state/migrations/cache/000001_cache_base.up.sql` only for fresh-schema documentation if required by the migration framework
- Create: `internal/state/migrations/cache/000003_nodes_dynamic_add_probe_failure_count.up.sql`
- Create: `internal/state/migrations/cache/000003_nodes_dynamic_add_probe_failure_count.down.sql`
- Modify: `cmd/resin/main.go`

**Interfaces:**
- Add `ProbeFailureCount atomic.Int32` to `node.NodeEntry`.
- Add `GlobalNodePool.RecordProbeResult(hash node.Hash, success bool)`.
- `RecordProbeResult` updates the existing circuit-breaker state through the current health path and increments/resets only `ProbeFailureCount`.
- `RecordPassiveResult` keeps its current behavior and does not increment `ProbeFailureCount` on failure.

- [ ] **Step 1: Write failing pool and probe tests**

Cover these cases:

```text
three active probe failures -> ProbeFailureCount=3 and circuit open
one active probe success -> ProbeFailureCount=0 and circuit closed
passive failure -> FailureCount may increase, ProbeFailureCount stays unchanged
```

- [ ] **Step 2: Run the tests and verify they fail**

Run: `go test ./internal/topology ./internal/probe -run 'ProbeFailure|RecordProbe|Passive' -count=1`

Expected: FAIL because the separate counter and method do not exist.

- [ ] **Step 3: Implement the counter and pool method**

Keep the existing `RecordResult` circuit logic intact. `RecordProbeResult` should call that logic and update `ProbeFailureCount` on the same live entry. Fire `OnNodeDynamicChanged` when the new counter changes so cache persistence is scheduled.

- [ ] **Step 4: Route active probes through the new method**

Change egress and latency probe failure/success paths in `internal/probe/manager.go` to call `RecordProbeResult`.

- [ ] **Step 5: Persist and restore the counter**

Extend the cache dynamic row model, bulk upsert/load SQL, `markNodeRemovedDirty` readers, and bootstrap restore in `cmd/resin/main.go`. Existing databases must read missing values as zero through the migration default.

- [ ] **Step 6: Run focused and restart tests**

Run: `go test ./internal/topology ./internal/probe ./internal/state ./cmd/resin -run 'Probe|Restart|Dynamic' -count=1`

Expected: PASS.

### Task 3: Apply the threshold in the automatic subscription cleaner

**Files:**
- Modify: `internal/topology/ephemeral_cleaner.go`
- Modify: `internal/topology/subscription_cleanup.go`
- Modify: `internal/topology/subscription_scheduler.go`
- Modify: `cmd/resin/main.go`
- Modify: `internal/service/control_plane_subscription.go`

**Interfaces:**
- Extend the cleanup predicate so it can inspect the subscription's `ManagedNode` and the live `NodeEntry`.
- A node qualifies for count-based eviction when its subscription is not already evicted and `entry.ProbeFailureCount >= sub.NodeEvictAfterFailures()`.
- Preserve the existing ephemeral predicates: no outbound with a non-empty build error, or circuit open longer than `ephemeral_node_evict_delay`.
- Use the existing two-pass confirmation; a success between scans resets the active counter and prevents eviction.

- [ ] **Step 1: Write cleaner tests**

Cover:

```text
threshold=0 -> no count-based eviction
counter below threshold -> retained
counter at threshold -> marked Evicted and removed from that subscription's pool reference
shared node -> remains in pool through the other subscription
probe success between scans -> not evicted
ephemeral time-based cleanup -> unchanged
```

- [ ] **Step 2: Run the tests and verify they fail**

Run: `go test ./internal/topology -run 'Cleaner|Cleanup' -count=1`

Expected: FAIL for the new threshold cases.

- [ ] **Step 3: Implement the predicate and cleaner wiring**

Run the cleaner for subscriptions that are `ephemeral` or have a positive `node_evict_after_failures`. Keep the existing operation lock, `CleanupSubscriptionNodesWithConfirmNoLock`, pool reference removal, and `onNodeEvicted` persistence callback.

- [ ] **Step 4: Preserve failure state across refreshes**

When a successful subscription refresh keeps a node hash, preserve its `Evicted` state and tags exactly as today. Do not reset the global active-probe counter during feed refresh; only a successful active probe resets it.

- [ ] **Step 5: Run topology and API integration tests**

Run: `go test ./internal/topology ./internal/service ./internal/api -run 'Subscription|Cleanup|Evict' -count=1`

Expected: PASS.

### Task 4: Add explicit restoration of evicted subscription nodes

**Files:**
- Modify: `internal/service/control_plane_subscription.go`
- Modify: `internal/api/handler_subscription.go`
- Modify: `internal/api/server.go`
- Modify: `webui/src/features/subscriptions/api.ts`
- Modify: `webui/src/features/subscriptions/SubscriptionPage.tsx`
- Modify: `webui/src/features/subscriptions/types.ts`

**Interfaces:**
- Add `ControlPlaneService.RestoreSubscriptionEvictedNodes(id string) (int, error)`.
- Add `POST /api/v1/subscriptions/{id}/actions/restore-evicted-nodes`.
- The action clears `Evicted` for all managed rows, marks cache rows dirty, and re-adds live pool entries. It then triggers a normal refresh so rows whose global node was deleted can be rebuilt from current subscription content.

- [ ] **Step 1: Write API and service tests**

Verify the action returns the restored count, clears the persisted flag, re-adds a live shared node, and does not resurrect a node absent from the current source until refresh.

- [ ] **Step 2: Run the tests and verify they fail**

Run: `go test ./internal/service ./internal/api -run 'Restore.*Evicted|restore-evicted' -count=1`

Expected: FAIL because the action is not registered.

- [ ] **Step 3: Implement the action and route**

Use the same `WithOpLock` and persistence-dirty conventions as `CleanupSubscriptionCircuitOpenNodes`. Do not delete the subscription itself.

- [ ] **Step 4: Add the WebUI operation**

Add a secondary action beside “立即清理” labelled “恢复已清理节点”, show the restored count, and invalidate the subscription/node queries after success.

- [ ] **Step 5: Run the API contract and production build**

Run: `go test ./internal/api ./internal/service -run 'Subscription|Restore' -count=1` and `npm run build` from `webui`.

Expected: PASS.

### Task 5: Document semantics and operational defaults

**Files:**
- Modify: `DESIGN.md`
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `doc/release-notes/v1.2.0.md`

- [ ] **Step 1: Document the distinction between failure classes**

State explicitly that feed URL fetch/parse failure only updates `last_error`; it does not delete nodes. Active node probe failures increment the node health counters. `RetryDownloader`'s two proxy attempts are unrelated to node eviction.

- [ ] **Step 2: Document the new API field and action**

Document `node_evict_after_failures`, its `0` disabled default, and the restore action. Explain that the threshold counts active probe failures, not elapsed time, so the real wall-clock time depends on latency/egress probe intervals.

- [ ] **Step 3: Add an example safe policy**

Use a conservative example such as `node_evict_after_failures=6`, keep `MaxConsecutiveFailures=3`, and advise enabling the policy only for subscriptions whose source is expected to contain disposable/rotating nodes.

- [ ] **Step 4: Run documentation checks**

Run: `git diff --check`.

Expected: PASS.

### Task 6: Full regression validation

**Files:**
- Test: existing Go and WebUI test suites; no new production files.

- [ ] **Step 1: Run focused Go tests**

Run: `go test ./internal/topology ./internal/probe ./internal/service ./internal/api ./internal/state ./cmd/resin -count=1`.

- [ ] **Step 2: Run the WebUI production build**

Run from `webui`: `npm run build`.

- [ ] **Step 3: Run formatting and diff checks**

Run: `gofmt -w` on modified Go files, then `git diff --check`.

- [ ] **Step 4: Record known unrelated failures**

If the existing full-suite failures in `internal/proxy/TestPumpPreparedTunnelReader_ClientReadResetAfterIngressDoesNotFail`, `internal/routing/TestRouteRequest_SameIPRotationMissRecreatesLease`, or `go vet` unreachable-code reporting recur, list them separately from this feature's focused results.

## Operational Result

With the default value `0`, current behavior is unchanged. To enable automatic removal for one subscription, set `node_evict_after_failures` to a positive number such as `6`; the node is first circuit-broken at the existing global threshold, remains eligible for active probes, and is marked evicted after the configured number of consecutive active probe failures. A successful active probe resets the counter. The node is removed only from that subscription's managed set; a shared reference keeps it alive globally. Use the restore action to clear the quarantine and trigger a refresh.

## Self-Review

- Source fetch/parse failure is excluded from eviction, so a temporary provider outage cannot wipe a subscription.
- Passive proxy failures are excluded from the new counter, so one target's failure cannot automatically delete a node.
- Shared-node reference counting remains in `GlobalNodePool.RemoveNodeFromSub`.
- Existing time-based ephemeral cleanup and manual cleanup remain available.
- The plan includes migration, restart behavior, UI/API contracts, recovery, tests, and documentation.
