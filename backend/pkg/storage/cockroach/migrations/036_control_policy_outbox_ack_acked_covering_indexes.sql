-- Migration: 036_control_policy_outbox_ack_acked_covering_indexes.sql
-- Purpose: Align ACK correlation query predicates that include status='acked'
-- with dedicated covering indexes to avoid fallback scans on hot ACK paths.

BEGIN TRANSACTION;

-- Exact correlation path including already-acked rows.
CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_exact_all_covering
ON control_policy_outbox (policy_id, rule_hash, trace_id, created_at DESC)
STORING (published_at, ai_event_ts_ms, source_event_ts_ms)
WHERE status IN ('publishing', 'published', 'acked');

-- Hash fallback correlation path including already-acked rows.
CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_hash_all_covering
ON control_policy_outbox (policy_id, rule_hash, created_at DESC)
STORING (published_at, ai_event_ts_ms, source_event_ts_ms)
WHERE status IN ('publishing', 'published', 'acked');

-- Trace fallback correlation path including already-acked rows.
CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_trace_all_covering
ON control_policy_outbox (policy_id, trace_id, created_at DESC)
STORING (published_at, ai_event_ts_ms, source_event_ts_ms)
WHERE status IN ('publishing', 'published', 'acked');

COMMIT;
