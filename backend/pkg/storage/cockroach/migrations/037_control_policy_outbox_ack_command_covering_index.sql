-- Migration: 037_control_policy_outbox_ack_command_covering_index.sql
-- Purpose: Add command-id covering index for ACK correlation hot path.

BEGIN TRANSACTION;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_ack_command_all_covering
ON control_policy_outbox (policy_id, command_id, created_at DESC)
STORING (published_at, ai_event_ts_ms, source_event_ts_ms)
WHERE status IN ('publishing', 'published', 'acked');

COMMIT;
