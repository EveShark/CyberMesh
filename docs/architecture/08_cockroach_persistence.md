# Architecture 8: Cockroach Persistence
## Schema, Migrations, and Write Paths (Backend)

**Last Updated:** 2026-02-25

This document describes what the backend persists to CockroachDB and how.

Primary code references:
- DB adapter: `backend/pkg/storage/cockroach/adapter.go`
- Migrations: `backend/pkg/storage/cockroach/migrations/*.sql`
- Consensus persistence tables: `backend/pkg/storage/cockroach/migrations/002_consensus_persistence.sql`
- Genesis certificate table: `backend/pkg/storage/cockroach/migrations/003_genesis_certificate.sql` and `004_genesis_certificate_keyed.sql`
- Control policy outbox tables: `backend/pkg/storage/cockroach/migrations/006_control_policy_outbox.sql`, `007_control_policy_outbox_tx_identity.sql`
- Outbox performance indexes: `backend/pkg/storage/cockroach/migrations/009_control_policy_outbox_perf_indexes.sql`

---

## 1. What Is Persisted

The backend persists:

- Committed blocks (`blocks`)
- Transactions per block with envelope/signature metadata (`transactions`)
- Versioned state snapshots (`state_versions`)
- Security/audit events (`audit_logs`)
- Validator metadata (`validators`)
- Reputation/quarantine/policy state tables (schema exists; usage depends on code paths)
- Consensus restart state (proposals, votes, QCs, metadata)
- Genesis certificate (for durable bootstrap restore)
- Control-policy publish intents and dispatcher leases (outbox)

---

## 2. Schema Source of Truth

The schema is defined by SQL migrations under:

```text
backend/pkg/storage/cockroach/migrations/
  001_init.sql
  002_consensus_persistence.sql
  003_genesis_certificate.sql
  004_genesis_certificate_keyed.sql
  ...
```

From these migrations, the DB includes (at least) the following tables:

```text
Core:
  blocks
  transactions
  state_versions
  validators
  audit_logs
  state_reputation
  state_quarantine
  state_policies

Consensus persistence:
  consensus_proposals
  consensus_qcs
  consensus_votes
  consensus_evidence
  consensus_metadata

Genesis:
  genesis_certificates

Control-policy publish durability:
  control_policy_outbox
  control_dispatcher_leases
```

---

## 3. PersistBlock: Transactional Write Path

`adapter.PersistBlock(ctx, blk, receipts, stateRoot)` writes a committed block and its transactions.

Key properties from `backend/pkg/storage/cockroach/adapter.go`:

- Uses a DB transaction with SERIALIZABLE isolation:
  - `db.BeginTx(... Isolation: sql.LevelSerializable)`
- Uses INSERT ... ON CONFLICT DO NOTHING and then verifies idempotency by re-reading:
  - if a row already exists, the adapter checks that the existing hash/content matches (defense against corruption/mismatch)
- Writes:
  - the block header row (includes tx_root from the block header; QC fields are TODO placeholders in current code)
  - the transactions for that block, including:
    - producer_id, nonce, content_hash, pubkey, signature, alg
    - payload JSONB and optional custody chain JSONB
    - status derived from receipts
  - outbox rows for policy transactions (same DB transaction as block persistence)

```mermaid
flowchart TD
    Start([PersistBlock Called]) --> Begin[BEGIN SERIALIZABLE Transaction]
    
    Begin --> Upsert1[Upsert Block Row]
    Upsert1 --> Check1{Row Exists?}
    
    Check1 -- Yes --> Verify1[Verify Hash Matches]
    Check1 -- No --> Insert1[Insert New Block]
    
    Verify1 --> Upsert2[Upsert Transactions]
    Insert1 --> Upsert2
    
    Upsert2 --> Loop{For Each Tx}
    Loop --> Insert2[Insert Tx with Metadata]
    Insert2 --> Store[Store: producer_id, nonce, hash,<br/>signature, payload JSONB]
    Store --> Loop
    Loop -- Policy Tx --> Outbox[UPSERT control_policy_outbox row]
    Outbox --> Upsert3
    
    Loop -- Done --> Upsert3[Upsert State Version]
    Upsert3 --> Commit[COMMIT Transaction]
    
    Commit --> Success([Success])
    
    Check1 -- Hash Mismatch --> Fail
    Verify1 -- Mismatch --> Fail[ROLLBACK - Corruption Detected]
    Fail --> Error([Error])
    
    style Begin fill:#e3f2fd,stroke:#1565c0,color:#000;
    style Commit fill:#c8e6c9,stroke:#2e7d32,color:#000;
    style Fail fill:#ffcccc,stroke:#ff0000,color:#000;
    style Success fill:#ccffcc,stroke:#00aa00,color:#000;
    style Error fill:#ffcccc,stroke:#ff0000,color:#000;
```

---

## 4. Consensus Persistence (Restart Recovery)

The adapter prepares statements for consensus persistence tables (proposals, votes, QCs, evidence, metadata).

This data is intended to support restarting the HotStuff engine without losing critical in-flight consensus state.

---

## 5. Durable Policy Outbox and ACK Correlation

The backend commit-to-publish path uses a transactional outbox model:

- `PersistBlock` writes outbox rows atomically with committed block/tx rows.
- A leased dispatcher claims outbox rows and publishes to `control.policy.v2`.
- Publish metadata (topic/partition/offset) is persisted.
- ACK store upserts `policy_acks` and marks correlated outbox row as `acked`.

This enforces:

- single logical publisher authority
- idempotent identity across commit/publish retries
- durable forensic trace from commit -> publish -> ack

---

## 6. Genesis Certificate Persistence

The genesis coordinator persists the genesis certificate through the storage backend:

- `LoadGenesisCertificate(ctx) ([]byte, bool, error)`
- `SaveGenesisCertificate(ctx, data []byte) error`

The schema starts as a singleton row (migration 003) and is later extended to a keyed identity (migration 004).

---

## 7. Related Documents

- System overview: `docs/architecture/01_system_overview.md`
- Genesis bootstrap: `docs/architecture/07_genesis_bootstrap.md`
- Security model: `docs/architecture/09_security_model.md`
