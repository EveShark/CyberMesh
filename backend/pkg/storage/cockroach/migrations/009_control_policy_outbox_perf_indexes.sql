-- Migration: 009_control_policy_outbox_perf_indexes.sql
-- Purpose: Improve outbox claim and ACK-correlation query performance under sustained load.

BEGIN TRANSACTION;

-- Claim path hot index: status/retry eligibility + age ordering.
CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_claim_hot
ON control_policy_outbox (status, next_retry_at, updated_at, created_at);

-- ACK correlation exact-match hot index.
CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_exact
ON control_policy_outbox (policy_id, rule_hash, trace_id, created_at DESC);

-- ACK correlation fallback hot index.
CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_fallback
ON control_policy_outbox (policy_id, rule_hash, created_at DESC);

COMMIT;
