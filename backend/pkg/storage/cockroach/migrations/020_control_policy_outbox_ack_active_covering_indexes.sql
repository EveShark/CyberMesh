-- Migration: 020_control_policy_outbox_ack_active_covering_indexes.sql
-- Purpose: Add active-row partial covering indexes for ACK correlation hot paths.

BEGIN TRANSACTION;

-- Exact correlation path: policy_id + rule_hash + trace_id
CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_exact_active_covering
ON control_policy_outbox (policy_id, rule_hash, trace_id, created_at DESC)
STORING (published_at, ai_event_ts_ms, source_event_ts_ms)
WHERE status IN ('publishing', 'published');

-- Hash fallback path: policy_id + rule_hash
CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_hash_active_covering
ON control_policy_outbox (policy_id, rule_hash, created_at DESC)
STORING (published_at, ai_event_ts_ms, source_event_ts_ms)
WHERE status IN ('publishing', 'published');

-- Trace fallback path: policy_id + trace_id
CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_trace_active_covering
ON control_policy_outbox (policy_id, trace_id, created_at DESC)
STORING (published_at, ai_event_ts_ms, source_event_ts_ms)
WHERE status IN ('publishing', 'published');

COMMIT;
