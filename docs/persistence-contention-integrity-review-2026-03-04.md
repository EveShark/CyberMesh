# Persistence and Adapter Deep Review (March 4, 2026)

## Scope

This review covers:

- `backend/pkg/storage/cockroach/adapter.go`
- `backend/pkg/wiring/persistence_worker.go`
- Observed production signals shared by the team (e.g., `class="other"=49`, canary `reason="integrity"=49`, ~63% conflict ratio, 5 validator pods writing to one CockroachDB).

The goal is to answer:

- What is a bug vs intentional strictness?
- What best practices are currently missing?
- Are we too strict, or appropriately security-first?
- What scenarios explain current metrics?

---

## External Evidence (Primary Sources)

- Go `select` semantics: if multiple cases are ready, one is chosen via uniform pseudo-random selection.
  - https://go.dev/doc/go_spec.html
- Go error wrapping and `errors.Is` for wrapped sentinel errors.
  - https://go.dev/blog/go1.13-errors
  - https://pkg.go.dev/errors
- Go cancellation pattern (`Done` channel in `select`).
  - https://go.dev/blog/context
  - https://go.dev/blog/pipelines
- CockroachDB default `SERIALIZABLE` isolation and retry behavior under contention.
  - https://www.cockroachlabs.com/docs/stable/architecture/transaction-layer
  - https://www.cockroachlabs.com/docs/stable/transaction-retry-error-reference
  - https://www.cockroachlabs.com/docs/stable/transactions
  - https://www.cockroachlabs.com/docs/stable/performance-best-practices-overview
- Leader-based consensus write-path reference (for architectural comparison).
  - https://etcd.io/docs/v3.3/faq/
  - https://etcd.io/docs/v3.3/op-guide/failures/

---

## Findings Matrix

### 1) `classifyPersistError` misclassifies wrapped integrity errors

- Code:
  - `backend/pkg/storage/cockroach/adapter.go:2907-2963`
  - `backend/pkg/storage/cockroach/adapter.go:1253-1266` (`classifyCanaryReason`)
- Severity: **High (observability correctness)**
- Classification: **Bug**

Why:

- `classifyPersistError` only maps SQLSTATE and message substrings.
- It does not first check `errors.Is(err, ErrIntegrityViolation)` / `ErrInvalidData`.
- Elsewhere, you correctly use sentinel-aware logic (`classifyCanaryReason`, `cockroach.IsRetryable`).
- Result: wrapped integrity errors can fall into `other`, exactly matching your live evidence (`other=49` aligns with canary `integrity=49`).

What is missing:

- A consistent error taxonomy path that starts with `errors.Is` sentinel checks before SQL/text fallback.

---

### 2) Transaction location mismatch in conflict verification

- Code:
  - `backend/pkg/storage/cockroach/adapter.go:1666-1677`
  - transaction schema uniqueness:
    - `backend/pkg/storage/cockroach/migrations/001_init_clean.sql:54-60`
    - `backend/pkg/storage/cockroach/migrations/001_init_clean.sql:88`
- Severity: **Medium to High (integrity failures under concurrency)**
- Classification: **Design strictness with operational side effect** (not a coding bug in isolation)

Why:

- On `tx_hash` conflict, code verifies full identity including location (`block_height`, `tx_index`) and fails closed on mismatch.
- This is a strong integrity stance and prevents silent drift/corruption.
- It is security-first and coherent with immutable-ledger semantics.

Important nuance:

- In a perfectly deterministic committed block flow, all validators should persist same `(height,index)` for a given `tx_hash`.
- Persistent location mismatches usually indicate one of:
  - non-identical payload/order being persisted across nodes,
  - stale/replayed tx inclusion path,
  - multi-writer architecture creating races around non-canonical attempts.

Are we too strict?

- For a blockchain persistence layer: **strictness is defensible**.
- The likely issue is not the strict check itself, but **who is allowed to write** and **when**.

What is missing:

- Clear cluster-level write ownership model (single canonical writer, or DB-level fencing/idempotency token for commit epoch).

---

### 3) Shutdown/enqueue race can strand accepted tasks

- Code:
  - `backend/pkg/wiring/persistence_worker.go:182-214`
  - `backend/pkg/wiring/persistence_worker.go:232-255`
  - `backend/pkg/wiring/persistence_worker.go:156`
- Severity: **Medium**
- Classification: **Bug**

Why:

- `Enqueue` checks `running`, releases lock, then `select`s on `queue <- task` vs `<-stopCh`.
- During shutdown, both send and stop receive can be ready; Go can choose either.
- Workers drain with non-blocking `default` and may exit if queue is momentarily empty.
- This can permit a task to be enqueued after shutdown signal and left unprocessed.

Best-practice gap:

- Stop/drain protocols typically enforce one of:
  - producer stop first, then close queue, then range-consume until queue close;
  - explicit two-phase stop barrier that prevents post-stop enqueue acceptance.

---

### 4) Retry backoff sleep is not cancellable

- Code:
  - `backend/pkg/wiring/persistence_worker.go:404`
- Severity: **Medium**
- Classification: **Bug / resilience gap**

Why:

- `time.Sleep(backoff)` ignores `ctx.Done()` and `stopCh`.
- On shutdown, workers can spend meaningful time sleeping even though stop is already requested.

Best-practice gap:

- Cancellation-aware backoff should use `select` over timer channel and cancellation channel(s).

---

### 5) Dedup is per-process only

- Code:
  - `backend/pkg/wiring/persistence_worker.go:70`
  - `backend/pkg/wiring/persistence_worker.go:187-195`
- Severity: **Info (architectural)**
- Classification: **Expected behavior, not a bug**

Why:

- `inflight` is local memory, so dedup scope is one pod.
- With 5 validator pods writing same DB keys, redundant writes are expected.

What is missing:

- Cross-pod dedup/fencing (e.g., leader-only persistence, lease, or commit-writer role separation).

---

### 6) P95 tail from SERIALIZABLE + multi-writer contention + attempt timeout policy

- Code:
  - `backend/pkg/storage/cockroach/adapter.go:831-833`
  - `backend/pkg/wiring/persistence_worker.go:278`
  - retry classification in `backend/pkg/storage/cockroach/errors.go:15-71`
- Severity: **Medium**
- Classification: **Architecture/performance issue (not a single-line bug)**

Why:

- CockroachDB under `SERIALIZABLE` on same keys will serialize/contend and may force retries.
- Per-attempt fixed 10s timeout plus retries can amplify latency tail.
- Your p50 low / p95 high pattern is consistent with winner/loser contention dynamics.

What is missing:

- Topology-aware write reduction (fewer concurrent writers on same keys).
- Timeout/backoff policy tied to real contention envelopes.

---

## What You Are Doing Right (Security-First)

- Fail-closed integrity checks in conflict verification.
- Sentinel retry policy treats integrity/invalid data as non-retryable.
- Audit logging for integrity-sensitive paths.
- Monotonic `consensus_metadata` guard (`excluded.height >= consensus_metadata.height`).

These are strong controls and should not be weakened casually.

---

## What You Are Missing

1. **Error taxonomy consistency**
- Canary path understands integrity; persist-failure classifier does not.
- This hides root-cause type in top-level metrics.

2. **Cluster-level write ownership**
- Multiple validators writing identical commit artifacts creates avoidable contention/noise.
- Consensus systems commonly centralize write responsibility per committed unit (leader-based examples from etcd/Raft docs).

3. **Shutdown correctness contract**
- Current queue-drain protocol is best-effort; accepted work is not strictly guaranteed to complete at stop boundary.

4. **Cancellable retry waits**
- Backoff path should honor shutdown/cancellation promptly.

5. **Operational proof signals**
- Need richer metrics that separate:
  - retryable contention (`40001` family),
  - deterministic integrity mismatch,
  - invalid input/data path.

---

## Scenario Analysis

### Scenario A: 5 pods persist same committed block, identical tx ordering

- Expected:
  - many unique-key conflicts (`ON CONFLICT DO NOTHING`),
  - low integrity mismatches,
  - mostly idempotent verification success.
- If integrity mismatches still occur, this suggests data mismatch beyond mere contention.

### Scenario B: 5 pods persist with occasional non-identical tx location for same `tx_hash`

- Expected:
  - conflict verify returns integrity violation at location check.
- This is currently treated as hard security failure (intentional).

### Scenario C: Shutdown during retry storm

- Expected:
  - workers may continue sleeping,
  - stop may timeout,
  - enqueue/stop race can strand tasks.

---

## Recommended Direction (Prioritized)

### P0 (correctness + observability)

1. Make persist-failure classification sentinel-aware (`errors.Is` first, then SQLSTATE/text fallback).
2. Fix enqueue/stop race so post-stop accepted tasks cannot be stranded.
3. Make retry backoff cancellable.

### P1 (architecture)

4. Reduce to one effective persistence writer per committed block (or introduce fencing/lease for writer role).
5. Keep strict location mismatch fail-closed unless you have a formal reorg/replay model requiring relaxed semantics.

### P2 (operability)

6. Add explicit metrics for:
   - sentinel integrity vs invalid,
   - retryable SQLSTATE subclasses,
   - enqueue accepted-after-stop attempts,
   - drain-exit with non-zero queue-late-arrival counter.

---

## Final Assessment

- **Confirmed bugs**: issue #1, #3, #4.
- **Expected but important architecture tradeoff**: issue #5.
- **Likely architecture/perf issue (needs tuning + ownership changes)**: issue #6.
- **Issue #2 is not simply “too strict”**:
  - The strictness is security-aligned.
  - The bigger problem is multi-writer persistence design and/or upstream deterministic consistency gaps.

In short: you are not wrong to be strict; you are missing coordination and cancellation/observability mechanics around that strictness.
