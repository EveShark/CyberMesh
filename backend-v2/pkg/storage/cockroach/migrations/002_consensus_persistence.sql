-- Migration: 002_consensus_persistence.sql
-- Purpose: Add dedicated consensus persistence tables for PBFT HotStuff engine restart recovery
-- Database: CockroachDB

BEGIN TRANSACTION;

CREATE TABLE IF NOT EXISTS consensus_proposals (
    block_hash      BYTEA PRIMARY KEY,
    height          BIGINT NOT NULL,
    view_number     BIGINT NOT NULL,
    proposer_id     BYTEA NOT NULL,
    proposal_cbor   BYTEA NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE consensus_proposals
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_consensus_proposals_height ON consensus_proposals (height);
CREATE INDEX IF NOT EXISTS idx_consensus_proposals_view ON consensus_proposals (view_number);

CREATE TABLE IF NOT EXISTS consensus_qcs (
    block_hash      BYTEA PRIMARY KEY,
    height          BIGINT NOT NULL,
    view_number     BIGINT NOT NULL,
    qc_cbor         BYTEA NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE consensus_qcs
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_consensus_qcs_height ON consensus_qcs (height);
CREATE INDEX IF NOT EXISTS idx_consensus_qcs_view ON consensus_qcs (view_number);

CREATE TABLE IF NOT EXISTS consensus_votes (
    vote_hash       BYTEA PRIMARY KEY,
    view_number     BIGINT NOT NULL,
    height          BIGINT NOT NULL,
    voter_id        BYTEA NOT NULL,
    block_hash      BYTEA NOT NULL,
    vote_cbor       BYTEA NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (view_number, voter_id, block_hash)
);

ALTER TABLE consensus_votes
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_consensus_votes_view ON consensus_votes (view_number);
CREATE INDEX IF NOT EXISTS idx_consensus_votes_height ON consensus_votes (height);

CREATE TABLE IF NOT EXISTS consensus_evidence (
    evidence_hash   BYTEA PRIMARY KEY,
    height          BIGINT NOT NULL,
    evidence_cbor   BYTEA NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE consensus_evidence
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_consensus_evidence_height ON consensus_evidence (height);

CREATE TABLE IF NOT EXISTS consensus_metadata (
    key             VARCHAR(50) PRIMARY KEY,
    height          BIGINT NOT NULL,
    block_hash      BYTEA NOT NULL,
    qc_cbor         BYTEA,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE consensus_metadata
    ADD COLUMN IF NOT EXISTS qc_cbor BYTEA;
ALTER TABLE consensus_metadata
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

COMMIT;
