-- Migration: 001_init.sql
-- Purpose: Initialize CyberMesh distributed cybersecurity control plane schema
-- Database: CockroachDB (distributed, BFT-safe)
-- Security: Military-grade, tamper-evident, auditable

-- =============================================================================
-- BLOCKS TABLE
-- Stores committed blocks from BFT consensus
-- =============================================================================
CREATE TABLE IF NOT EXISTS blocks (
    -- Primary key
    height          BIGINT PRIMARY KEY,
    
    -- Block identification
    block_hash      BYTEA NOT NULL UNIQUE,
    parent_hash     BYTEA NOT NULL,
    state_root      BYTEA NOT NULL,
    
    -- Consensus metadata
    proposer_id     BYTEA NOT NULL,
    view_number     BIGINT NOT NULL,
    timestamp       TIMESTAMPTZ NOT NULL,
    
    -- Block content
    tx_count        INT NOT NULL DEFAULT 0,
    tx_root         BYTEA NOT NULL,
    
    -- BFT proof
    qc_view         BIGINT NOT NULL,
    qc_signatures   BYTEA NOT NULL,
    
    -- Timestamps
    committed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Indexes
    INDEX idx_blocks_hash (block_hash),
    INDEX idx_blocks_proposer (proposer_id),
    INDEX idx_blocks_timestamp (timestamp),
    INDEX idx_blocks_committed (committed_at)
);

COMMENT ON TABLE blocks IS 'BFT consensus committed blocks';
COMMENT ON COLUMN blocks.height IS 'Monotonic block height';
COMMENT ON COLUMN blocks.block_hash IS 'SHA-256 hash of block content';
COMMENT ON COLUMN blocks.parent_hash IS 'Hash of previous block (chain linkage)';
COMMENT ON COLUMN blocks.state_root IS 'Merkle root of state after applying block';
COMMENT ON COLUMN blocks.proposer_id IS 'Validator ID that proposed this block';
COMMENT ON COLUMN blocks.qc_signatures IS 'Aggregated BFT quorum signatures (2f+1)';

-- =============================================================================
-- TRANSACTIONS TABLE
-- Stores all transactions (EventTx, EvidenceTx, PolicyTx) with envelopes
-- =============================================================================
CREATE TABLE IF NOT EXISTS transactions (
    -- Primary key
    tx_hash         BYTEA PRIMARY KEY,
    
    -- Block reference
    block_height    BIGINT NOT NULL,
    tx_index        INT NOT NULL,
    
    -- Transaction type
    tx_type         VARCHAR(20) NOT NULL CHECK (tx_type IN ('event', 'evidence', 'policy')),
    
    -- Envelope (mandatory for all transactions)
    producer_id     BYTEA NOT NULL,
    nonce           BYTEA NOT NULL,
    content_hash    BYTEA NOT NULL,
    algorithm       VARCHAR(20) NOT NULL,
    public_key      BYTEA NOT NULL,
    signature       BYTEA NOT NULL,
    
    -- Transaction payload
    payload         JSONB NOT NULL,
    
    -- Chain-of-custody (for EvidenceTx)
    custody_chain   JSONB,
    
    -- Execution result
    status          VARCHAR(20) NOT NULL CHECK (status IN ('success', 'failed', 'skipped')),
    error_msg       TEXT,
    
    -- Timestamps
    submitted_at    TIMESTAMPTZ NOT NULL,
    executed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Constraints
    UNIQUE (block_height, tx_index),
    FOREIGN KEY (block_height) REFERENCES blocks(height) ON DELETE CASCADE,
    
    -- Indexes
    INDEX idx_tx_producer (producer_id),
    INDEX idx_tx_type (tx_type),
    INDEX idx_tx_block (block_height),
    INDEX idx_tx_status (status),
    INDEX idx_tx_submitted (submitted_at),
    INDEX idx_tx_content_hash (content_hash)
);

COMMENT ON TABLE transactions IS 'All transactions with mandatory envelopes and signatures';
COMMENT ON COLUMN transactions.producer_id IS 'AI service or node that produced this transaction';
COMMENT ON COLUMN transactions.nonce IS 'Monotonic nonce for replay protection';
COMMENT ON COLUMN transactions.content_hash IS 'SHA-256 of payload for integrity verification';
COMMENT ON COLUMN transactions.custody_chain IS 'Chain-of-custody entries for evidence (EvidenceTx only)';

-- =============================================================================
-- STATE_REPUTATION TABLE
-- Tracks reputation scores for AI producers/nodes
-- =============================================================================
CREATE TABLE IF NOT EXISTS state_reputation (
    -- Primary key
    producer_id     BYTEA PRIMARY KEY,
    
    -- Reputation data
    score           NUMERIC(10, 6) NOT NULL DEFAULT 100.0 CHECK (score >= 0 AND score <= 100),
    total_events    BIGINT NOT NULL DEFAULT 0,
    violations      BIGINT NOT NULL DEFAULT 0,
    last_violation  TIMESTAMPTZ,
    
    -- State version
    updated_height  BIGINT NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Indexes
    INDEX idx_reputation_score (score),
    INDEX idx_reputation_violations (violations)
);

COMMENT ON TABLE state_reputation IS 'Reputation scores for event producers';
COMMENT ON COLUMN state_reputation.score IS 'Reputation score (0-100), decreases on violations';
COMMENT ON COLUMN state_reputation.updated_height IS 'Block height of last update';

-- =============================================================================
-- STATE_QUARANTINE TABLE
-- Tracks quarantined producers/nodes
-- =============================================================================
CREATE TABLE IF NOT EXISTS state_quarantine (
    -- Primary key
    producer_id     BYTEA PRIMARY KEY,
    
    -- Quarantine data
    reason          TEXT NOT NULL,
    evidence_hash   BYTEA NOT NULL,
    quarantined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ,
    permanent       BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- State version
    applied_height  BIGINT NOT NULL,
    
    -- Indexes
    INDEX idx_quarantine_expires (expires_at),
    INDEX idx_quarantine_permanent (permanent)
);

COMMENT ON TABLE state_quarantine IS 'Quarantined producers (blocked from submitting)';
COMMENT ON COLUMN state_quarantine.permanent IS 'If true, quarantine never expires';
COMMENT ON COLUMN state_quarantine.evidence_hash IS 'Hash of evidence transaction that triggered quarantine';

-- =============================================================================
-- STATE_POLICIES TABLE
-- Stores active security policies (allow/deny rules)
-- =============================================================================
CREATE TABLE IF NOT EXISTS state_policies (
    -- Primary key
    policy_id       BYTEA PRIMARY KEY,
    
    -- Policy metadata
    policy_type     VARCHAR(20) NOT NULL CHECK (policy_type IN ('allow', 'deny', 'rate_limit', 'threshold')),
    target          VARCHAR(50) NOT NULL,
    
    -- Policy content
    rules           JSONB NOT NULL,
    
    -- Status
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    priority        INT NOT NULL DEFAULT 100,
    
    -- Versioning
    created_height  BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_height  BIGINT,
    updated_at      TIMESTAMPTZ,
    
    -- Indexes
    INDEX idx_policies_type (policy_type),
    INDEX idx_policies_target (target),
    INDEX idx_policies_active (active)
);

COMMENT ON TABLE state_policies IS 'Active security policies and rules';
COMMENT ON COLUMN state_policies.target IS 'What the policy applies to (e.g., producer_id, event_type)';
COMMENT ON COLUMN state_policies.priority IS 'Higher priority policies evaluated first';

-- =============================================================================
-- AUDIT_LOGS TABLE
-- Tamper-evident audit log for security events
-- =============================================================================
CREATE TABLE IF NOT EXISTS audit_logs (
    -- Primary key
    id              BIGSERIAL PRIMARY KEY,
    
    -- Audit metadata
    event_type      VARCHAR(50) NOT NULL,
    severity        VARCHAR(20) NOT NULL CHECK (severity IN ('DEBUG', 'INFO', 'WARN', 'ERROR', 'CRITICAL', 'SECURITY')),
    
    -- Event data
    actor           TEXT,
    action          TEXT NOT NULL,
    resource        TEXT,
    result          VARCHAR(20),
    
    -- Context
    fields          JSONB NOT NULL DEFAULT '{}'::JSONB,
    
    -- Tamper detection
    sequence_num    BIGINT NOT NULL UNIQUE,
    prev_hash       BYTEA,
    record_hash     BYTEA NOT NULL,
    
    -- Timestamps
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Indexes
    INDEX idx_audit_event_type (event_type),
    INDEX idx_audit_severity (severity),
    INDEX idx_audit_timestamp (timestamp),
    INDEX idx_audit_actor (actor),
    INDEX idx_audit_sequence (sequence_num)
);

COMMENT ON TABLE audit_logs IS 'Tamper-evident audit log with hash chaining';
COMMENT ON COLUMN audit_logs.sequence_num IS 'Monotonic sequence for detecting gaps';
COMMENT ON COLUMN audit_logs.prev_hash IS 'Hash of previous audit record (chain linkage)';
COMMENT ON COLUMN audit_logs.record_hash IS 'HMAC of this record for tamper detection';

-- =============================================================================
-- VALIDATORS TABLE
-- Tracks validator nodes in the consensus network
-- =============================================================================
CREATE TABLE IF NOT EXISTS validators (
    -- Primary key
    validator_id    BYTEA PRIMARY KEY,
    
    -- Node identity
    public_key      BYTEA NOT NULL UNIQUE,
    peer_id         TEXT,
    
    -- Status
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    reputation      NUMERIC(10, 6) NOT NULL DEFAULT 100.0 CHECK (reputation >= 0 AND reputation <= 100),
    
    -- Consensus participation
    joined_height   BIGINT NOT NULL,
    last_seen       TIMESTAMPTZ,
    blocks_proposed BIGINT NOT NULL DEFAULT 0,
    blocks_voted    BIGINT NOT NULL DEFAULT 0,
    
    -- Network info
    address         TEXT,
    
    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Indexes
    INDEX idx_validators_active (is_active),
    INDEX idx_validators_reputation (reputation),
    INDEX idx_validators_last_seen (last_seen)
);

COMMENT ON TABLE validators IS 'BFT validator nodes participating in consensus';
COMMENT ON COLUMN validators.reputation IS 'Node reputation (decreases on Byzantine behavior)';
COMMENT ON COLUMN validators.joined_height IS 'Block height when validator joined network';

-- =============================================================================
-- STATE_VERSIONS TABLE
-- Tracks state root history for merkle proofs and rollback
-- =============================================================================
CREATE TABLE IF NOT EXISTS state_versions (
    -- Primary key
    version         BIGINT PRIMARY KEY,
    
    -- State root
    state_root      BYTEA NOT NULL UNIQUE,
    
    -- Block reference
    block_height    BIGINT NOT NULL UNIQUE,
    block_hash      BYTEA NOT NULL,
    
    -- Statistics
    tx_count        INT NOT NULL DEFAULT 0,
    reputation_changes INT NOT NULL DEFAULT 0,
    policy_changes  INT NOT NULL DEFAULT 0,
    quarantine_changes INT NOT NULL DEFAULT 0,
    
    -- Timestamp
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Foreign key
    FOREIGN KEY (block_height) REFERENCES blocks(height) ON DELETE CASCADE,
    
    -- Indexes
    INDEX idx_state_versions_block (block_height),
    INDEX idx_state_versions_root (state_root)
);

COMMENT ON TABLE state_versions IS 'State root history for merkle proofs and state verification';
COMMENT ON COLUMN state_versions.version IS 'Monotonic state version number';

-- =============================================================================
-- FUNCTIONS
-- =============================================================================

-- Function to verify audit log chain integrity
CREATE OR REPLACE FUNCTION verify_audit_chain(start_seq BIGINT, end_seq BIGINT)
RETURNS TABLE(sequence_num BIGINT, is_valid BOOLEAN, error_msg TEXT) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        a.sequence_num,
        CASE 
            WHEN a.sequence_num = start_seq THEN TRUE
            WHEN prev.record_hash = a.prev_hash THEN TRUE
            ELSE FALSE
        END as is_valid,
        CASE
            WHEN a.sequence_num = start_seq THEN 'First record in range'
            WHEN prev.record_hash = a.prev_hash THEN 'Chain valid'
            ELSE 'Chain broken: prev_hash mismatch'
        END as error_msg
    FROM audit_logs a
    LEFT JOIN audit_logs prev ON prev.sequence_num = a.sequence_num - 1
    WHERE a.sequence_num BETWEEN start_seq AND end_seq
    ORDER BY a.sequence_num;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION verify_audit_chain IS 'Verify tamper-evident audit log chain integrity';

-- =============================================================================
-- GRANTS (Apply appropriate permissions in production)
-- =============================================================================
-- TODO: Set up role-based access control with least privilege principle
-- Example:
-- GRANT SELECT, INSERT, UPDATE ON blocks TO cybermesh_writer;
-- GRANT SELECT ON blocks TO cybermesh_reader;

-- =============================================================================
-- SECURITY NOTES
-- =============================================================================
-- 1. All timestamps use TIMESTAMPTZ for proper timezone handling
-- 2. BYTEA columns store cryptographic hashes and signatures (32-64 bytes)
-- 3. JSONB used for flexible policy/payload storage with indexing capability
-- 4. Foreign keys enforce referential integrity
-- 5. CHECK constraints enforce data validation at DB level
-- 6. Audit log uses hash chaining for tamper detection
-- 7. Indexes optimize query performance for common access patterns
-- 8. Comments document schema for future maintainers
