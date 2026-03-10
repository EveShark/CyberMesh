-- Migration: 015_control_policy_outbox_source_trace_columns.sql
-- Purpose: Persist upstream source event lineage for true telemetry->publish/ack causal metrics.

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS source_event_id STRING NULL;

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS source_event_ts_ms INT8 NULL;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_source_event_id
  ON control_policy_outbox (source_event_id, created_at DESC);
