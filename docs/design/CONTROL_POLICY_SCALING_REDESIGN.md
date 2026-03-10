# Control Policy Scaling Redesign

**Status:** Phase 0 complete, Phase 1 complete, Phase 2 in progress  
**Last Updated:** 2026-03-08

---

## 1. Problem Statement

The current `control.policy.v1` path is not scaling as a production control plane.

Observed live behavior:
- backend publish is fast
- enforcement ACK publishing is fast once a message is consumed
- the dominant delay is `control.policy publish -> enforcement consume`
- `control.policy.v1` currently runs as a single-partition topic
- enforcement processing is effectively serialized on that lane

This is not primarily a DB problem and not primarily a frontend/API problem.
It is a control-plane throughput and ordering-topology mismatch.

---

## 2. Current Root Cause

The current architecture couples:
- one topic lane
- one effective consume lane
- synchronous per-message enforcement processing
- synchronous local persistence in the enforcement hot path

That means faster backend publishing only moves the queue downstream into Kafka before enforcement consumption.

Current live shape:
- topic: `control.policy.v1`
- partitions: `1`
- enforcement concurrency: effectively one ordered consumer lane for that topic
- ordering domain: treated as global today, even though local code already supports narrower scope semantics

---

## 3. Phase Plan

### Phase 0
Define invariants before any topology change.

### Phase 1
Add migration-grade observability for publish, consume, apply, persist, and ACK.

### Phase 2
Introduce stable partitioning on the control topic.

### Phase 3
Refactor enforcement consumption to scale with partitions while preserving in-partition order.

### Phase 4
Reduce hot-path synchronous work and formalize idempotency.

### Phase 5
Retune producer and consumer together under load.

### Phase 6
Roll out safely with migration and rollback controls.

---

## 4. Phase 0 Goals

Phase 0 defines the non-negotiable correctness rules that later phases must preserve.

Outputs:
- partitioning domain
- ordering invariants
- idempotency invariants
- ACK correctness boundary
- duplicate/retry semantics
- migration constraints

No code or manifest topology change should happen before these are explicit.

---

## 5. Phase 0 Findings From Local Code

The enforcement layer already has a natural ordering domain.

Relevant code:
- [scopeIdentifier in controller.go](/B:/CyberMesh/enforcement-agent/internal/controller/controller.go)
- [target scope parsing in parser.go](/B:/CyberMesh/enforcement-agent/internal/policy/parser.go)
- [policy spec target model in spec.go](/B:/CyberMesh/enforcement-agent/internal/policy/spec.go)

Current supported scope forms include:
- `namespace`
- `node`
- `tenant`
- `region`
- `cluster`
- `global`

The enforcement controller already derives a scoped identifier from the policy:
- `namespace:<ns>`
- `node:<node>`
- `tenant:<tenant>`
- `region:<region>`
- `cluster`
- `global`

This is the strongest existing candidate for the control-topic ordering key.

---

## 6. Phase 0 Decisions

### 6.1 Ordering Domain

Ordering must be preserved per enforcement scope, not globally across all policies.

Canonical ordering key:
- `scope_identifier`

Definition:
- use the same scope derivation already implemented in enforcement via `scopeIdentifier(spec)`

Why:
- local code already treats enforcement semantics in scoped form
- rate limiting, deduplication, and policy application logic already reason in scoped domains
- global ordering is too expensive and is not justified by the current control model

### 6.2 Partition Key

The partition key for `control.policy.v1` should be:
- `scope_identifier`

Fallback behavior:
- if scope resolution fails, route to `global`

Do not key by:
- random value
- unique `policy_id`
- raw `trace_id`

Reason:
- `policy_id` destroys ordering across related actions on the same target scope
- `trace_id` is for observability, not delivery ordering

### 6.3 Ordering Invariant

Within the same `scope_identifier`:
- messages must be applied in Kafka order
- consume/apply/ACK must preserve per-scope monotonicity

Across different `scope_identifier` values:
- messages may execute in parallel
- cross-scope order is not guaranteed and is not required

### 6.4 Idempotency Invariant

Consumers must remain safe under at-least-once delivery.

Required property:
- duplicate delivery of the same policy event for the same scope must be a no-op or converge to the same final state

Current local hint:
- enforcement already treats duplicate apply as an idempotent noop in the controller path

The idempotency identity must include:
- `policy_id`
- `scope_identifier`

If later revisions add versioned updates to the same policy ID, that contract must be extended explicitly.

### 6.5 ACK Correctness Boundary

ACK may only be emitted after:
- policy verification succeeds
- the policy has been applied according to the configured enforcement backend semantics

ACK must not be used to signal:
- merely consumed
- merely validated
- merely queued for future apply

If post-apply local persistence is moved off the hot path in later phases, that is allowed only if:
- correctness does not depend on that persistence completing before ACK and offset progression

### 6.6 Offset Commit Boundary

Offset progression must happen only after the correctness boundary for that message is satisfied.

At minimum:
- no offset commit before successful apply outcome is known
- no offset commit before duplicate-safe state transition is durable enough for replay safety

The exact storage durability point may change in Phase 4, but the replay-safety requirement does not.

### 6.7 Tenant Isolation Invariant

Partitioning and runtime traces must not weaken tenant isolation.

Rules:
- tenant metadata must never be inferred from untrusted routing fields alone
- observability joins must remain tenant-safe
- if scope is tenant-based, tenant normalization must be deterministic and canonical

### 6.8 Migration Invariant

The redesign must prefer a new topic version over an unsafe in-place semantic change when ordering behavior changes materially.

Preferred migration target:
- `control.policy.v2`

Reason:
- in-place partition-count changes can change key placement and confuse rollback
- a new topic version gives controlled cutover and rollback

---

## 7. Explicit Non-Goals For Phase 0

Phase 0 does not decide:
- exact final partition count
- exact worker pool size
- exact Kafka rebalance strategy
- exact local persistence refactor

Those belong to later phases after observability and load modeling.

---

## 8. Risks To Control In Later Phases

### Global Scope Hotspot

Policies routed to `global` or `cluster` may remain a hotspot even after partitioning.

Mitigation:
- keep those scopes rare
- measure them separately
- consider special handling only if they become dominant

### Cluster Scope Coarseness

If production-like traffic is dominated by `target.scope="cluster"`, plain `scope_identifier="cluster"` collapses all work into one Kafka key and one ordered lane.

Mitigation:
- keep enforcement semantics cluster-aware
- allow delivery sharding for cluster-scoped traffic with a deterministic routing key such as `cluster:<bucket>`
- shard only on a stable target fingerprint so the same effective target stays ordered
- keep global accounting and guardrails cluster-wide until explicitly redesigned

### Enforcement Model Drift

If manifests and runtime config do not explicitly choose an enforcement consumption model, rollout and validation become misleading.

Supported models:
- `fanout_per_node`
- `shared_group`

Current intended defaults:
- host-level DaemonSet backends such as `iptables` and `nftables` use `fanout_per_node`
- shared control-plane style backends such as `gateway` and the current `cilium` manifests use `shared_group`

### Poison Scope

One bad scope can stall a partition lane.

Mitigation:
- partition-local poison handling
- DLQ or quarantine semantics
- no global queue stall

### Duplicate Replay

Kafka redelivery or rebalance can replay messages.

Mitigation:
- strict idempotency at the enforcement boundary
- replay-safe state transitions

### Cross-Tenant Drift

Improper key derivation could mix unrelated tenants into one lane or one trace.

Mitigation:
- canonical scope derivation
- explicit tenant normalization
- tenant-safe observability joins

---

## 9. Phase 1 Entry Criteria

Phase 1 can start once these are accepted:
- ordering domain is `scope_identifier`
- partition key is `scope_identifier`
- per-scope ordering is required
- cross-scope ordering is not required
- ACK remains post-apply
- consumer remains duplicate-safe
- migration should target a new topic version

---

## 10. Master Execution Plan

This section is the execution plan for Phases 1 through 6.

Each phase includes:
- objective
- code and manifest scope
- wiring and integration expectations
- test plan
- scenarios and edge cases
- completion criteria

The phases should be executed in order.

---

## 11. Phase 1: Migration-Grade Observability

### 11.1 Objective

Make the control path measurable enough to support a topology migration without guessing.

The key requirement is to make these spans visible and trustworthy:
- publish -> Kafka availability
- Kafka availability -> enforcement consume
- enforcement consume -> apply complete
- apply complete -> ACK publish
- ACK publish -> backend ACK persisted

### 11.2 Code Scope

Backend:
- [dispatcher.go](/B:/CyberMesh/backend/pkg/control/policyoutbox/dispatcher.go)
- [store.go](/B:/CyberMesh/backend/pkg/control/policyoutbox/store.go)
- [producer.go](/B:/CyberMesh/backend/pkg/ingest/kafka/producer.go)
- [metrics.go](/B:/CyberMesh/backend/pkg/api/metrics.go)

Enforcement:
- [consumer.go](/B:/CyberMesh/enforcement-agent/internal/kafka/consumer.go)
- [controller.go](/B:/CyberMesh/enforcement-agent/internal/controller/controller.go)
- [store.go](/B:/CyberMesh/enforcement-agent/internal/state/store.go)
- [metrics package](/B:/CyberMesh/enforcement-agent/internal/metrics)

### 11.3 What To Add

Backend metrics:
- control policy publish attempts by result
- publish latency by topic
- publish counts by computed `scope_identifier`
- scope-resolution fallback count for `global`

Enforcement metrics:
- consume-to-verify latency
- verify-to-apply latency
- apply-to-ack latency
- apply-to-persist latency
- partition lag by assigned partition
- partition consume throughput
- duplicate/no-op count by scope

Derived runtime markers:
- `t_control_publish_start`
- `t_control_publish_ack`
- `t_enforcement_consume`
- `t_enforcement_apply_done`
- `t_enforcement_persist_done`
- `t_ack_publish_start`
- `t_ack_publish_done`

### 11.4 Wiring

Backend:
- compute `scope_identifier` before publishing `control.policy.v1`
- export it in logs and bounded metrics
- preserve the exact same value in message payload metadata for debugging

Enforcement:
- record partition id on consume
- record per-message stage timings inside the same handler invocation
- keep metric labels bounded to:
  - partition
  - result
  - scope_kind

Do not label metrics with:
- raw tenant
- raw policy id
- raw trace id

### 11.5 Integration

This phase must not change semantics.

No change to:
- topic names
- consumer group ids
- ACK boundary
- offset commit behavior

### 11.6 Tests

Backend:
- publish metric on success
- publish metric on retry/failure
- stable scope derivation metric emission

Enforcement:
- consume/apply/ack metric emission on success
- metric emission on apply failure
- duplicate/no-op metric emission
- per-partition metric label coverage

### 11.7 Scenarios

Positive:
- single policy on idle system
- multiple scopes under light load
- repeated policies on same scope

Negative:
- apply failure
- Kafka produce failure
- ACK publish failure
- duplicate delivery
- rebalance during processing

### 11.8 Edge Cases

- fallback to `global` scope
- empty tenant
- invalid scope in payload
- poison message
- message replay after restart

### 11.9 Completion Criteria

- we can measure `publish -> consume`, `consume -> apply`, `apply -> ack`
- metrics exist live and are stable under burst
- no semantic behavior changed

---

## 12. Phase 2: Stable Partitioning On The Control Topic

### 12.1 Objective

Replace the single global control lane with a keyed, partitioned topology that preserves per-scope ordering.

### 12.1.1 Cluster-Sharding Refinement

The initial `scope_identifier` model is correct for non-cluster scopes, but it is too coarse for scale when most traffic resolves to `scope_identifier="cluster"`.

Refinement:
- preserve semantic scope as `cluster`
- derive a delivery key of `cluster:<bucket>` for cluster-scoped policies when enabled
- compute `<bucket>` from a deterministic target fingerprint
- keep the same target on the same shard
- allow different targets to fan out across partitions

This is a delivery-sharding change, not an enforcement-correctness change.

Cluster-wide accounting such as:
- rate limits
- history windows
- duplicate guards keyed by semantic scope

may remain cluster-global until explicitly redesigned.

### 12.2 Design Decision

Create a new topic version:
- `control.policy.v2`

Do not repurpose `control.policy.v1` in place for the production migration.

### 12.3 Code Scope

Backend:
- [dispatcher.go](/B:/CyberMesh/backend/pkg/control/policyoutbox/dispatcher.go)
- [producer.go](/B:/CyberMesh/backend/pkg/ingest/kafka/producer.go)
- [config_main.go](/B:/CyberMesh/backend/pkg/config/config_main.go)

Enforcement:
- [consumer.go](/B:/CyberMesh/enforcement-agent/internal/kafka/consumer.go)
- [config package](/B:/CyberMesh/enforcement-agent/internal/config)

Manifests:
- [configmap.yaml](/B:/CyberMesh/k8s_azure/base/configmap.yaml)
- [daemonset.yaml](/B:/CyberMesh/k8s_azure/enforcement/standard/daemonset.yaml)
- any Helm mirrors of the same config

### 12.4 What To Add

Backend:
- compute `scope_identifier`
- serialize it as Kafka message key
- optionally persist `scope_identifier` alongside outbox rows for auditability
- when enabled, compute `cluster:<bucket>` from a deterministic target fingerprint for cluster-scoped policies

Kafka:
- create `control.policy.v2` with an explicit partition count

Recommended starting point:
- enough partitions to exceed current and near-term enforcement concurrency, not merely current pod count

### 12.5 Wiring

Message key:
- must be `scope_identifier`
- must be deterministic
- must survive retries unchanged

Payload:
- should include the same scope value in metadata for debugging, not as the source of truth for routing

Configuration:
- cluster sharding must be explicit and reversible
- recommended knobs:
  - `CONTROL_POLICY_CLUSTER_SHARDING_MODE`
  - `CONTROL_POLICY_CLUSTER_SHARD_BUCKETS`

### 12.6 Integration

Migration strategy:
1. create `control.policy.v2`
2. deploy producer support behind config
3. deploy enforcement support behind config
4. run canary traffic
5. cut over from v1 to v2

### 12.7 Tests

- same scope always hashes to same partition
- different scopes distribute across partitions
- retries preserve the same key
- message schema remains backward compatible where required
- same cluster target always yields the same `cluster:<bucket>`
- different cluster targets can yield different `cluster:<bucket>` values
- non-cluster scopes remain unchanged

### 12.8 Scenarios

Positive:
- many independent scopes
- one hot scope, many cold scopes
- same scope ordered updates

Negative:
- missing scope
- malformed target
- fallback to global
- topic unavailable
- cluster sharding disabled
- cluster sharding enabled with bucket count <= 1

### 12.9 Edge Cases

- `global` hotspot
- `cluster` hotspot
- tenant scope with missing tenant metadata
- node scope with missing selector

### 12.10 Completion Criteria

- producer writes keyed messages to `control.policy.v2`
- partition distribution is visible
- same-scope ordering is preserved
- unrelated scopes no longer serialize globally

---

## 13. Phase 3: Partition-Aware Enforcement Consumption

### 13.1 Objective

Scale enforcement consumption with partitions while preserving strict in-partition order.

### 13.2 Code Scope

- [consumer.go](/B:/CyberMesh/enforcement-agent/internal/kafka/consumer.go)
- [controller.go](/B:/CyberMesh/enforcement-agent/internal/controller/controller.go)
- [main.go](/B:/CyberMesh/enforcement-agent/cmd/agent/main.go)

### 13.3 Design

Required behavior:
- serial processing per partition
- parallel progress across partitions
- no unrelated partition should be blocked by one slow partition

Allowed implementation patterns:
- one goroutine queue per assigned partition
- one partition-local worker model
- bounded partition ownership with ordered handoff

Not allowed:
- one global handler lock for all partitions
- cross-partition work queue that loses per-partition order

### 13.4 Wiring

- track assigned partitions explicitly
- record partition ownership in logs
- tie offset progression to partition-local completion
- expose partition-local lag/throughput

### 13.5 Integration

Rebalances must remain safe.

On rebalance:
- stop ingesting new work for revoked partitions
- finish or safely abort in-flight work according to replay contract
- never silently lose offset ownership

### 13.6 Tests

- same partition stays ordered
- different partitions run concurrently
- rebalance revocation is safe
- restart/rejoin preserves replay safety

### 13.7 Scenarios

Positive:
- steady-state multi-partition traffic
- skewed hot partition plus cold partitions

Negative:
- rebalance during burst
- one poisoned partition
- temporary Kafka disconnect
- shutdown during in-flight apply

### 13.8 Edge Cases

- offset commit after apply but before persistence
- partition revocation during ACK publish
- consumer resume after crash

### 13.9 Completion Criteria

- measured concurrency rises with partition count
- hot partition does not stall unrelated scopes
- per-partition order remains correct

---

## 14. Phase 4: Hot-Path Persistence And Idempotency Redesign

### 14.1 Objective

Reduce the synchronous per-message cost in enforcement without weakening replay safety or ACK correctness.

### 14.2 Code Scope

- [store.go](/B:/CyberMesh/enforcement-agent/internal/state/store.go)
- [controller.go](/B:/CyberMesh/enforcement-agent/internal/controller/controller.go)
- any persistence helpers under the enforcement state package

### 14.3 Design Goals

- duplicate delivery must remain safe
- destructive re-apply must not happen on replay
- local persistence should stop capping throughput if it is not part of the correctness boundary

### 14.4 Candidate Refactors

Allowed directions:
- append-only journal with async checkpoint
- batched persistence
- persistence after ACK only if replay safety is otherwise guaranteed
- lighter idempotency index with deferred full snapshot write

Not allowed:
- ACK before apply
- offset commit before replay-safe state exists
- local fast path that makes duplicate replay destructive

### 14.5 Wiring

- separate “applied” from “persisted snapshot updated”
- record both states in metrics
- explicitly expose persistence backlog if batching is introduced

### 14.6 Tests

- duplicate replay after restart
- crash after apply before ACK
- crash after ACK before persistence checkpoint
- replay after checkpoint recovery

### 14.7 Scenarios

Positive:
- sustained load with batching enabled
- recovery from normal restart

Negative:
- disk slow
- disk unavailable
- journal replay corruption
- duplicate delivery after rebalance

### 14.8 Edge Cases

- same policy id on same scope replay
- same policy id across different scopes
- pending manual approval path
- policy expiry during replay

### 14.9 Completion Criteria

- enforcement throughput increases materially
- replay safety remains intact
- ACK correctness boundary remains valid

---

## 15. Phase 5: Tuning, Load Validation, And Production Rollout

### 15.1 Objective

Tune the redesigned system as a whole and roll it out safely.

### 15.2 Scope

Config and manifests:
- [configmap.yaml](/B:/CyberMesh/k8s_azure/base/configmap.yaml)
- [daemonset.yaml](/B:/CyberMesh/k8s_azure/enforcement/standard/daemonset.yaml)
- Helm equivalents

Operational surfaces:
- Kafka topic configuration
- enforcement replica/node placement
- backend outbox worker settings

### 15.3 Tuning Targets

Tune together:
- outbox poll interval
- outbox in-flight count
- partition count
- enforcement partition concurrency
- persistence batching thresholds

Do not tune these independently in isolation.

### 15.4 Load Validation

Required tests:
- low steady-state
- burst load
- sustained high load
- hot-scope skew
- mixed-scope traffic
- rebalance under load

Required outputs:
- per-partition lag
- publish -> consume p50/p95/p99
- consume -> apply p50/p95/p99
- apply -> ack p50/p95/p99
- duplicate/no-op counts
- persistence backlog if batching exists

### 15.5 Rollout Strategy

Preferred rollout:
1. observability lands first
2. topic created
3. producer support deployed dark
4. consumer support deployed dark
5. canary traffic on v2
6. limited cutover
7. full cutover
8. retain rollback path to v1 until stable

### 15.6 Rollback

Rollback requirements:
- disable v2 publishing by config
- keep v1 consumer path intact until v2 is proven
- no irreversible schema or topic dependency during initial cutover

### 15.7 Tests

- config-only rollback
- canary cutover rollback
- partial node failure during cutover
- mixed-version deployment safety

### 15.8 Completion Criteria

- `publish -> consume` no longer dominates at scale
- unrelated scopes scale horizontally
- hot scopes are isolated to their partitions
- backlog remains bounded under target burst rate
- rollback is proven

---

## 16. Open Questions

These should be answered as part of execution:
- Should `cluster` and `global` scopes share one lane or be isolated?
- Do any policy types require stronger ordering than `scope_identifier` provides?
- Is local enforcement persistence required before ACK, or can it be journaled/checkpointed later?
- Should topic migration be dual-write, dual-read, or cutover-only?
- What is the target partition count for the first production v2 rollout?

---

## 17. Recommendation

Proceed with:
1. Phase 1 observability
2. Phase 2 keyed partitioning on `scope_identifier`
3. Phase 3 partition-aware enforcement consumption
4. Phase 4 hot-path persistence redesign
5. Phase 5 load validation and rollout

Do not continue tuning a single-partition `control.policy.v1` path as if it were production-scale architecture.
