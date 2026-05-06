-- Migration: 045_control_policy_trace_events_stage_ack_indexes.sql
-- Purpose: Support control-panel and validation scans by stage/time and exact
-- ACK-attempt correlation without full scans of the append-only trace ledger.

CREATE INDEX IF NOT EXISTS idx_control_policy_trace_events_stage_ts
ON control_policy_trace_events (stage, timestamp_ms ASC)
STORING (policy_id, trace_id, command_id, ack_event_id, rule_hash);

CREATE INDEX IF NOT EXISTS idx_control_policy_trace_events_ack_stage_ts
ON control_policy_trace_events (ack_event_id, stage, timestamp_ms ASC)
STORING (policy_id, trace_id, command_id, rule_hash)
WHERE ack_event_id IS NOT NULL AND ack_event_id != '';
