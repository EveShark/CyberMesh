-- Migration: 013_control_policy_outbox_ack_trace_fallback_index.sql
-- Purpose: Ensure trace-only ACK correlation fallback remains index-backed.

BEGIN TRANSACTION;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_trace_fallback
ON control_policy_outbox (policy_id, trace_id, created_at DESC)
STORING (status, published_at, ai_event_ts_ms);

COMMIT;

