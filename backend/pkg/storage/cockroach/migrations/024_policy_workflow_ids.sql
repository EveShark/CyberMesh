-- Migration: 024_policy_workflow_ids.sql
-- Purpose: persist workflow_id across outbox and ACK tables for operator workflow tracing

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS workflow_id STRING;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_workflow_id
  ON control_policy_outbox (workflow_id, created_at DESC);

ALTER TABLE policy_acks
  ADD COLUMN IF NOT EXISTS workflow_id STRING;

CREATE INDEX IF NOT EXISTS idx_policy_acks_workflow_id
  ON policy_acks (workflow_id, observed_at DESC);

ALTER TABLE policy_ack_events
  ADD COLUMN IF NOT EXISTS workflow_id STRING;

CREATE INDEX IF NOT EXISTS idx_policy_ack_events_workflow_id
  ON policy_ack_events (workflow_id, observed_at DESC);
