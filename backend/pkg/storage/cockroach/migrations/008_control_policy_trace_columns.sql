-- Migration: 008_control_policy_trace_columns.sql
-- Purpose: Add causal tracing columns for durable control-policy outbox rows.

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS trace_id STRING NULL;

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS ai_event_ts_ms INT8 NULL;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_trace_id
  ON control_policy_outbox (trace_id);
