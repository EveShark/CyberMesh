# CyberMesh Backend — Architecture and Implementation Plan

This document captures the architecture, current status, and a concrete, security‑first implementation plan for the Go backend of the CyberMesh distributed cybersecurity platform. It is intended as a handoff plan for multiple engineers (per layer), following Agile delivery.

## 1) Project context and scope

- Mission: achieve a tamper‑evident, highly available, distributed cybersecurity control plane where nodes agree on security events, evidence, and policy updates via BFT consensus.
- Layer split:
  - AI Layer (Python): anomaly detection, controllers, ML models, producers to Kafka/message bus. Not part of this repo.
  - Go Backend (this repo): deterministic control plane — P2P + Consensus + Application/State Execution, secure ingestion from Kafka, storage, audit, and APIs.
- Security posture: cryptographic integrity, replay protection, strict size/timestamp limits, IP allowlisting, peer scoring/quarantine, audit log integrity, least‑privilege network exposure.

## 2) Current status (done)

### utils (security primitives and infra helpers)
- Structured logger (zap) with redaction/sampling; audit logger with integrity chaining and HMAC signing; secure config manager (env + secrets); crypto service (Ed25519 signing, AES‑GCM encryption, key rotation, replay cache); IP allowlist validation; HTTP client builder.

### config (topology, node and consensus configuration)
- Topology discovery (role→instances, cluster size, quorum math); node constants; consensus constants (Byzantine thresholds); enhanced configuration loader; secure token manager; system status helpers.

### p2p (network layer)
- libp2p host + GossipSub v1.1 + DHT; connection gating (allowlist + quarantine + trusted peers); peer state manager (health, scoring, quarantine) with liveness/decay; topic validators; discovery loop; bootstrap dialing.
- Consensus bridge: maps topics ↔ consensus engine (`AttachConsensusHandlers`), injects a router publisher for outbound messages.

### consensus (deterministic agreement)
- types: single source of truth for consensus interfaces and domain constants.
- messages: canonical CBOR encoder/decoder with strict limits, signature domain separation, timestamp skew checks, and validator for Proposal/Vote/QC/ViewChange/NewView/Heartbeat/Evidence.
- leader: Rotation (deterministic selection with reputation/quarantine gates), Pacemaker (AIMD timeouts, view changes; publishes VC/NV), Heartbeat manager (leader liveness; publishes heartbeats and enforces progress).
- pbft: HotStuff (2‑chain) skeleton using messages/types; quorum verifier (2f+1, duplicate signer detection), storage (replay window, in‑mem + backend hooks), vote aggregation, commit rule; evidence creation on equivocation.
- api/engine: orchestrates encoder, validator, rotation, pacemaker, heartbeat, quorum, storage, and HotStuff; inbound p2p decode/verify/route; outbound publisher interface with topic mapping; adapters to bridge utils to types/messages.

### Integration highlights
- Inbound: p2p topic → engine.OnMessageReceived → messages.Validator → leader/pacemaker/heartbeat → pbft.
- Outbound: engine publishes Proposal/Vote; pacemaker publishes ViewChange/NewView; heartbeat publishes Heartbeats; topic mapping configurable.
- Security: strict CBOR; signature domain separation; timestamp skew and replay prevention; IP allowlist enforced; peer scoring and quarantine; audit logging on critical paths.

## 3) Repository layout and responsibilities

```
backend/
├── go.mod, go.sum                         # Module: backend (Go 1.21)
├── PLAN.md                                # This plan
└── pkg/
    ├── utils/                             # Logging, audit, crypto, config manager, IP allowlist, HTTP, net helpers
    │   ├── audit.go, crypto.go, env.go, http.go, logger.go, net.go, errors.go, serialize.go, time.go
    │   └── ...
    ├── config/                            # Topology, node, security, consensus/storage config builders and status
    │   ├── config_main.go, config_node.go, config_security.go, config_consensus.go, config_storage.go, config_infra.go
    │   └── ...
    ├── p2p/                               # Router (libp2p host, GossipSub, DHT), State (peer health/reputation)
    │   ├── router.go, state.go, consensus_bridge.go
    │   └── ...
    └── consensus/                         # Consensus stack
        ├── types/                         # Interfaces and domain constants
        │   ├── common.go, interfaces.go
        ├── messages/                      # Canonical structs + encoder/validator
        │   ├── types.go, encoding.go, validation.go
        ├── leader/                        # Rotation, Pacemaker, Heartbeat (+ outbound publishers)
        │   ├── rotation.go, leader.go, heartbeat.go
        ├── pbft/                          # HotStuff, Storage, Quorum
        │   ├── pbft.go, storage.go, quorum.go
        └── api/                           # Engine orchestrator + adapters
            ├── engine.go, adapters.go, util_adapters.go,
            ├── encoder_adapter.go, validator_adapter.go,
            ├── pacemaker_publisher_adapter.go, heartbeat_publisher_adapter.go
```

Roles:
- utils: shared primitives; no business logic; no secrets in logs; key rotation ready.
- config: authoritative config ingestion and derived constants; validates security baselines.
- p2p: resilient, policy‑enforced message transport; no business semantics beyond validation/enforcement.
- consensus: deterministic ordering; never depends on ML; only on validated, canonical messages.

## 4) Network topics (defaults)

```
consensus/proposal
consensus/vote
consensus/viewchange
consensus/newview
consensus/heartbeat
consensus/evidence
```

Overridable via `EngineConfig.Topics`.

## 5) Security posture (summary)

- Canonical CBOR, domain‑separated signatures, timestamp skew enforcement, replay cache.
- Allowlist‑first P2P, topic size caps, gossip peer scoring, quarantine enforcement.
- Audit logs with integrity chaining and signing; strict redaction in logs.
- DoS controls planned at ingestion (Kafka) and mempool; rate limits, size caps, dedupe, signature checks.
- Key management supports rotation; no secrets logged; TLS and mTLS planned for APIs.

## 6) Next layers to implement (Application/State Execution + Ingestion + APIs)

### 6.1 Application/State Execution layer (Go)
- Data model: canonical, versioned types for security events, evidence, policy updates; optional validator‑set governance ops.
- Mempool: admission pipeline (authN/Z, size/version, signature, nonce/replay, dedupe, rate‑limit), priority queues, eviction policies.
- Block builder: deterministic selection/ordering; block size/weight/age constraints; builds header (tx root, state root hint) and body.
- Executor (state machine): applies events deterministically (reputation scoring, quarantine, allow/deny policy, evidence impact, governance); idempotent on replays; emits audit.
- State storage: merkleized state root; snapshots + pruning; fast queries; durable storage (pluggable backend).
- Consensus hooks: engine.SubmitBlock from leader; engine.OnCommit → executor; evidence generation → outbound publish.

### 6.2 Kafka integration (Go)
- Consumers: topics (versioned)
  - `ai.anomalies.v1` — anomaly/alert events
  - `ai.evidence.v1` — cryptographic evidence and supporting artifacts
  - `ai.policy.v1` — proposed policy updates/governance inputs
  Schema: Protobuf (preferred) or Avro with schema registry; include: version, type, payload, content hash, nonce, created_at, producer_id, signature bundle (pubkey, sig, alg), optional chain of custody.
  Admission aligns with mempool pipeline (verify → dedupe → rate‑limit → persist).
- Producers (outbound):
  - `control.commits.v1` — block commit notifications (height/hash/state_root/tx_count)
  - `control.reputation.v1` — validator/peer reputation changes
  - `control.policy.v1` — effective policy updates and allow/deny changes
  - `control.evidence.v1` — confirmed evidence and actions taken
  Delivery: idempotent producers; consumer offsets committed post‑admission; DLQ topics for permanent failures; replay tooling.
- Partitioning/keys: key by entity (e.g., offender_id or policy id) for ordering locality; consumer group per service; backpressure via max in‑flight.

### 6.3 API layer (Go)
- Read‑only state/query: health, metrics, node/validator status, policy snapshots, historical blocks.
- Admin endpoints (restricted): governance ops if required; evidence queries; maintenance.
- Security: mTLS, OAuth2/JWT/RBAC, idempotency keys, size/time limits, full audit.

### 6.4 Observability
- Structured logs with redaction; optional tracing with secure sampling.

### 6.5 CockroachDB storage adapter (Go)

Rationale: distributed, strongly‑consistent SQL with serializable isolation and geo‑partitioning.

Responsibilities:
- Durable storage of blocks, QCs, transactions (security events/evidence/policy), validator metadata, and merkleized state snapshots.
- Idempotent, transactional commits per block; efficient queries for APIs and analytics.

Schema (initial draft):
- `blocks` (PK: height)
  - height BIGINT, hash BYTES UNIQUE, parent_hash BYTES, proposer_id BYTES, tx_count INT,
    tx_root BYTES, state_root BYTES, qc BYTES (serialized QC), committed_at TIMESTAMP
- `transactions` (PK: (block_height, idx))
  - block_height BIGINT, idx INT, tx_id BYTES UNIQUE, type STRING, payload BYTES,
    content_hash BYTES, producer_id STRING, created_at TIMESTAMP, signature BYTES, pubkey BYTES
- `evidence` (PK: evidence_id)
  - evidence_id BYTES UNIQUE, type STRING, offender_id BYTES, view BIGINT, height BIGINT,
    reporter_id BYTES, proof BYTES, created_at TIMESTAMP, signature BYTES, status STRING
- `state_snapshots` (PK: state_root)
  - state_root BYTES UNIQUE, version_height BIGINT, created_at TIMESTAMP, meta JSONB
- `state_kv` (PK: (namespace, key_hash, version))
  - namespace STRING, key_hash BYTES, key BYTES, value BYTES, version BIGINT, created_at TIMESTAMP
  - Use for merkle node/materialized state; or store merkle nodes under a dedicated namespace
- `validators` (PK: validator_id)
  - validator_id BYTES, pubkey BYTES, reputation FLOAT8, active BOOL, joined_view BIGINT, updated_at TIMESTAMP

Indexes:
- `blocks(hash)`, `transactions(tx_id)`, `evidence(offender_id)`, `evidence(view,height)`, `validators(active)`.

Transactions/Isolation:
- Wrap full block commit in a SERIALIZABLE transaction: insert block, QCs, transactions, evidence, state snapshot deltas; use UPSERT for idempotency; unique constraints prevent duplicates.
- On conflict, verify content matches (hash equality) to ensure idempotent replays are safe.

Partitioning/Locality:
- Range/partition `blocks` and `transactions` by height ranges for pruning and locality; zone configs per region if multi‑region.

Encryption/Backups:
- Enable cluster encryption at rest; store sensitive payloads encrypted at application layer (AES‑GCM) when appropriate.
- Scheduled full/incremental backups; regular restore drills; integrity checks (hash verification of blocks and state roots).

Changefeeds (optional):
- CockroachDB changefeeds for analytics/monitoring sinks; never used for consensus‑critical paths.

State model:
- Merkle root calculated per commit; `state_kv` stores versioned materialized values or merkle nodes with compacting GC. Snapshots/pruning policy (retain N versions; prune beyond finalized horizon).

- Prometheus: p2p/consensus/mempool/executor/Kafka counters/histograms; liveness/readiness.
- Structured logs with redaction; optional tracing with secure sampling.

## 7) Interfaces and ownership (per team)

### Consensus ↔ Application
- SubmitBlock(Block) (leader only) — provided by engine; block must be deterministic and within size/weight limits.
- OnCommit callback — executes committed block in executor; updates state; emits Kafka events and audit.

### Ingestion ↔ Application
- Kafka consumer delivers canonical messages to mempool admission; on acceptance, persisted in mempool and available for block builder.

### P2P ↔ Engine
- Already integrated: inbound decode/verify/route; outbound publish for proposal/vote/viewchange/newview/heartbeat; evidence publish pending finalization.

## 8) Backlog (Agile‑ready)

Epics:
1. Application/State Execution Layer
   - Define schemas (events/evidence/policy/governance) and canonical encodings.
   - Implement mempool (admission, priority, eviction) with tests.
   - Implement block builder and header/body format; integrate with engine.SubmitBlock.
   - Implement executor and state storage (Merkle root, snapshots, pruning).
   - Wire OnCommit to executor; emit audit and Kafka notifications.

2. Kafka Integration
   - Consumer group, secure config (SASL/TLS), schema validation, signature verification, dedupe, DLQ.
   - Producers for commit/reputation/policy updates.

3. API & Observability
   - Read‑only gRPC/HTTP with mTLS/RBAC; liveness/readiness; metrics endpoints.
   - Structured logs/tracing; SLOs and alerting rules.

4. Governance & Validator‑Set Management (optional, later)
   - Transactions for adding/removing validators, weight changes, key rotations.
   - Policy for activation epochs and safety checks.

Hardening tasks:
- Evidence broadcasting from PBFT; finalize JustifyQC propagation.
- Engine state to expose Highest/LastCommitted QC for validator checks.
- Storage backend adapter (LevelDB/Badger or SQL) with encryption at rest.

## 9) Milestones & deliverables

- M1: Application schemas + mempool skeleton + tests.
- M2: Block builder + executor stub; engine.SubmitBlock wired; dry‑run execution path.
- M3: State storage with Merkle root; commit integration; audit + metrics.
- M4: Kafka consumers/producers; end‑to‑end from Kafka → commit → Kafka.
- M5: Read‑only API; hardened security (mTLS/RBAC); dashboards/alerts.
- M6: Evidence broadcast + slashing/quarantine policy refinements.
- M7: Storage backend in production mode + backup/restore procedures.

## 10) Risks and mitigations

- Replay/DoS at ingestion: strict nonce/replay windows, quotas, signature checks before admission.
- Inconsistent state: deterministic executor, versioned schemas, feature flags, strict validation.
- Gossip abuse: topic size caps, app‑specific peer score, quarantine, allowlist enforcement.
- Key handling: rotation support, no secrets in logs, use KMS/HSM where available.

## 11) Configuration (selected keys)

- P2P: `P2P_LISTEN_PORT`, `P2P_TOPICS`, `P2P_MAX_MESSAGE_SIZE`, `ALLOWED_IPS`, `TRUSTED_P2P_PEERS`.
- Consensus: `CONSENSUS_*` (timeouts, sizes, cache, AIMD), `NODE_TYPE`, `NODE_ID`.
- Topology: `CLUSTER_TOPOLOGY`, `QUORUM_SIZE`.
- Audit/Logging: `AUDIT_LOG_PATH`, `AUDIT_SIGNING_KEY`, `LOG_LEVEL`.
- Kafka (planned): brokers, TLS/SASL, topic names; consumer/producer tuning.

## 12) Definition of Done (per layer)

- All security checks implemented (size, time, signature, replay, allowlists).
- Unit/integration tests; metrics and audit in place; no sensitive data in logs.
- Static analysis/lint clean; build reproducible; configuration validated.
- Documentation of interfaces and invariants; handoff notes updated.

## 13) Getting started for each engineer

- Read this plan and the repo tree; confirm boundaries for your layer.
- Implement against interfaces in `pkg/consensus/types` and existing adapters.
- Keep deterministic behavior; avoid non‑deterministic sources in executor.
- Prefer small PRs with targeted scope; include metrics and audit for new paths.

---

This plan is the single source for coordination across teams; update it as layers evolve (following Agile change control). Security remains the primary, non‑negotiable acceptance criterion.

## 14) Application/State Execution Layer (detailed plan)

Objective: deterministic state machine for cybersecurity events/evidence/policy; DoS‑hardened mempool; deterministic block builder; executor + tamper‑evident state; wired to consensus (SubmitBlock/OnCommit), Kafka ingestion, and CockroachDB persistence.

### Integration points
- Ingest: Kafka → verify (schema/sig/nonce/size) → mempool admission.
- Propose: Leader uses BlockBuilder to build canonical block → engine.SubmitBlock.
- Commit: engine.OnCommit → Executor applies txs → update state root → persist (blocks/txs/state/QC) → produce Kafka notifications → audit logs.

### Proposed repo tree and files

```
backend/
└── pkg/
    └──
      ├── model/                     # Canonical data types and encoding
      │   ├── event.go               # Anomaly/alert tx (schema + validation)
      │   ├── evidence.go            # Evidence tx
      │   ├── policy.go              # Policy/governance tx
      │   └── codec.go               # Canonical encode/hash, versioning
      ├── mempool/
      │   ├── admission.go           # AuthN/Z, size/version, sig+nonce/replay, rate-limit, dedupe
      │   ├── mempool.go             # Queues, priority, eviction, TTL
      │   ├── priority.go            # Scoring (severity/age/proof)
      │   └── metrics.go
      ├── block/
      │   ├── types.go               # AppBlock implementing consensus/types.Block
      │   ├── header.go              # tx_root, state_root_hint, proposer, parent
      │   ├── limits.go              # size/weight/age caps
      │   └── builder.go             # Deterministic selection/ordering from mempool
      ├── executor/
      │   ├── executor.go            # Apply block deterministically
      │   ├── reducers.go            # Reputation/quarantine/policy reducers
      │   ├── state_machine.go       # Tx->state transitions (pure, deterministic)
      │   └── metrics.go
      ├── state/
      │   ├── store.go               # Versioned state interface
      │   ├── merkle.go              # State root, proofs
      │   ├── snapshot.go            # Snapshots + pruning
      │   └── query.go               # Read APIs (internal)
      ├── storage/
      │   └── cockroach/
      │       ├── adapter.go         # Tx model (SERIALIZABLE), idempotent UPSERTs
      │       ├── schema.go          # DDL builder or embed migrations
      │       └── migrations/
      │           ├── 001_init.sql   # blocks, transactions, evidence, state_kv, validators, snapshots
      │           └── 002_indexes.sql
      ├── ingest/
      │   └── kafka/
      │       ├── consumer.go        # ai.anomalies.v1 / ai.evidence.v1 / ai.policy.v1
      │       ├── schema.go          # Protobuf bindings + validation
      │       ├── verifier.go        # Signature/pubkey/nonce checks
      │       └── producer.go        # control.commits.v1 / reputation.v1 / policy.v1 / evidence.v1
      ├── api/
      │   ├── server.go              # gRPC/HTTP (read-only + admin)
      │   ├── handlers.go            # State/blocks/validators queries
      │   ├── auth.go                # mTLS/JWT/RBAC, idempotency
      │   └── dto.go                 # Response DTOs
      └── wiring/
            ├── service.go             # Compose mempool/builder/executor/state/storage
            ├── proposer.go            # Leader loop: build+SubmitBlock
            └── commit_handler.go      # Engine OnCommit -> Executor -> Storage -> Kafka
```

### Plan of action (phased)
1. Model + Canonical Encoding
   - Implement app/model (event/evidence/policy) with strict validation and canonical hash/encoding; unit tests.
2. Mempool
   - Admission pipeline (authN/Z hook, size/version, sig+nonce/replay, rate-limit, dedupe), priority queues, eviction; metrics.
3. Block Builder
   - AppBlock implementing consensus/types.Block; header with tx_root; deterministic selection/ordering; limits (count/bytes/time).
4. Executor + State
   - Pure reducers (reputation/quarantine/policy); state store with Merkle root; snapshots/pruning; idempotent commit; metrics.
5. CockroachDB Adapter
   - Migrations; adapter with SERIALIZABLE TX per block; idempotent UPSERT; integrity checks; encryption at rest; indices.
6. Kafka
   - Consumers (ai.*.v1) → admission; Producers (control.*.v1) on commit; DLQ and replay strategy; exactly-once where feasible.
7. Wiring
   - proposer.go to trigger SubmitBlock when leader; commit_handler.go to execute block on OnCommit and emit Kafka + audit.
8. API (read-only)
   - Health/metrics/state/blocks/validators; mTLS/JWT/RBAC; rate limits; audit.
