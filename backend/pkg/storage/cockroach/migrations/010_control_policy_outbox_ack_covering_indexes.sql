-- Migration: 010_control_policy_outbox_ack_covering_indexes.sql
-- Purpose: Add covering indexes for ACK correlation exact/fallback lookups.

BEGIN TRANSACTION;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_fallback_covering
ON control_policy_outbox (policy_id, rule_hash)
STORING (status, published_at, created_at, ai_event_ts_ms);

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_exact_covering
ON control_policy_outbox (policy_id, rule_hash, trace_id)
STORING (status, published_at, created_at, ai_event_ts_ms);

COMMIT;
