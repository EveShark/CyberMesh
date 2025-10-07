# CyberMesh Backend — Immediate Plan of Action (Aligned with AI Service)

## 1) Purpose and scope
- Define the immediate, security‑first work required on the Go backend, aligned with the Python AI service.
- Clarify boundaries: AI publishes signed events to Kafka; backend performs P2P, BFT consensus, deterministic execution, and durable storage.

## 2) Roles and boundaries (final)
- AI/Processing Nodes (Python):
  - Detection pipelines (math/rules/ML), ensemble, model mgmt.
  - Produce signed messages to Kafka: ai.anomalies.v1, ai.evidence.v1, ai.policy.v1.
  - Consume feedback from backend: control.*.v1 (commits/reputation/policy) for training and tuning.
  - No P2P/consensus participation; no ledger/state authority.
- Backend Validators/Control/Storage/Gateway (Go):
  - libp2p networking, BFT consensus (HotStuff 2‑chain), message validation.
  - Mempool admission, deterministic block building, state execution.
  - CockroachDB durable storage, audit, metrics, read‑only APIs.

## 3) Current backend status (done)
- utils, config, p2p, consensus integrated; inbound/outbound consensus messaging wired; audit, allowlisting, skew/replay checks in place.

## 4) Next layer (backend): Application/State Execution
- Implement deterministic state machine for events/evidence/policy.
- DoS‑hardened mempool, deterministic block builder.
- Executor with merkleized state; CockroachDB adapter; Kafka ingest/emit.

## 5) Repository structure updates (flattened, consistent with current layout)
```
backend/
└── pkg/
    ├── mempool/                 # Admission, queues, priority, eviction, metrics
    │   ├── admission.go
    │   ├── mempool.go
    │   ├── priority.go
    │   └── metrics.go
    ├── block/                   # App block types and deterministic builder
    │   ├── types.go             # AppBlock implements consensus/types.Block
    │   ├── header.go            # tx_root, state_root_hint, proposer, parent
    │   ├── limits.go            # size/weight/age caps
    │   └── builder.go
    ├── state/                   # Deterministic state execution and storage
    │   ├── model.go             # Canonical tx models (event/evidence/policy)
    │   ├── codec.go             # Canonical encoding/hash, versioning
    │   ├── executor.go          # Apply block deterministically
    │   ├── reducers.go          # Reputation/quarantine/policy reducers
    │   ├── merkle.go            # State root, proofs
    │   ├── store.go             # Versioned state interface
    │   ├── snapshot.go          # Snapshots + pruning
    │   └── query.go             # Internal read helpers
    ├── ingest/
    │   └── kafka/
    │       ├── consumer.go      # ai.anomalies.v1 / ai.evidence.v1 / ai.policy.v1
    │       ├── schema.go        # Protobuf bindings + validation
    │       ├── verifier.go      # Sig/pubkey/nonce/size/version checks
    │       └── producer.go      # control.commits.v1 / reputation.v1 / policy.v1 / evidence.v1
    ├── storage/
    │   └── cockroach/
    │       ├── adapter.go       # SERIALIZABLE TX per block; idempotent UPSERTs
    │       ├── schema.go        # DDL/migrations wiring
    │       └── migrations/
    │           ├── 001_init.sql # blocks, transactions, evidence, state_kv, validators, snapshots
    │           └── 002_indexes.sql
    ├── api/                     # Read‑only APIs (health, metrics, queries, admin)
    │   ├── server.go
    │   ├── handlers.go
    │   ├── auth.go              # mTLS/JWT/RBAC, idempotency, rate limits
    │   └── dto.go
    └── wiring/
        ├── service.go           # Compose mempool/builder/executor/state/storage
        ├── proposer.go          # Leader loop: build block + engine.SubmitBlock
        └── commit_handler.go    # engine.OnCommit -> execute -> persist -> produce
```

## 6) Kafka contracts (align with AI README, de‑duplicated)
- Versioned topics and Protobuf schemas (single canonical schema registry):
  - ai.anomalies.v1: { id, type, source, severity, confidence, ts, payload_hash, payload, model_version, producer_id, nonce, signature, pubkey }
  - ai.evidence.v1: { id, evidence_type, refs, proof_blob, ts, producer_id, nonce, signature, pubkey }
  - ai.policy.v1: { id, action, rule, params, ts, producer_id, nonce, signature, pubkey }
  - control.commits.v1: { height, hash, state_root, tx_count, ts }
  - control.reputation.v1: { entity_id, delta, reason, ts }
  - control.policy.v1: { rule_id, change, ts }
  - control.evidence.v1: { evidence_id, status, action, ts }
- Security: TLS/SASL, producer idempotence, DLQ topics, consumer group with strict admission pipeline.

## 7) CockroachDB adapter (summary)
- Tables: blocks, transactions, evidence, state_kv, state_snapshots, validators.
- SERIALIZABLE TX per block; UPSERT idempotency; hash equality checks on conflicts.
- Partition by height; encryption at rest; scheduled backups; integrity checks.

## 8) Sprint plan (2 sprints)
- Sprint A (backend focus):
  1) state/model + codec + tests.
  2) mempool admission (sig/nonce/size/version/rate) + queues/eviction + metrics.
  3) block/types/header/limits + deterministic builder; wire proposer.go.
- Sprint B:
  4) executor + reducers + merkle + store + snapshots + tests.
  5) cockroach adapter + migrations; commit_handler.go (execute->persist->produce).
  6) kafka consumer/producer; end‑to‑end from Kafka → commit → Kafka.
  7) minimal read‑only api (health/metrics/state/blocks/validators) with mTLS/RBAC.

## 9) Acceptance and security gates
- All admission paths enforce: schema version, size caps, timestamp skew, signature+pubkey verification, nonce/replay protection, rate limits, dedupe.
- Deterministic executor; state root reproducible from block + prior root.
- Audit on admission/commit/evidence; Prometheus metrics for each stage.
- No sensitive data in logs; mTLS on APIs; configs validated at startup.

## 10) Redundancy cleanup vs AI README
- AI removes P2P/gossip/peer manager and “backend consensus coordination.”
- AI keeps Kafka produce/consume, REST/health, metrics, Redis cache if needed.
- Backend remains sole authority for P2P/consensus/state/ledger.

## 11) Owners (suggested)
- Mempool/Builder: Engineer A
- Executor/State/Merkle: Engineer B
- CockroachDB Adapter/Migrations: Engineer C
- Kafka Consumers/Producers: Engineer D
- Wiring/API/Observability: Engineer E

## 12) Start criteria
- Confirm topic names and Protobuf schemas with AI team.
- Provision CockroachDB with TLS; create service accounts and credentials.
- Define RBAC and mTLS settings for read‑only API.

## 13) Out of scope (backend)
- Training pipelines, model management, AI REST detection endpoints (remain in Python AI service).
