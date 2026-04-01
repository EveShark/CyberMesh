-- Migration: 022_policy_request_ids.sql
-- Purpose: persist request_id across outbox and ACK tables for operator/API tracing

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS request_id STRING;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_request_id
  ON control_policy_outbox (request_id, created_at DESC);

ALTER TABLE policy_acks
  ADD COLUMN IF NOT EXISTS request_id STRING;

CREATE INDEX IF NOT EXISTS idx_policy_acks_request_id
  ON policy_acks (request_id, observed_at DESC);

ALTER TABLE policy_ack_events
  ADD COLUMN IF NOT EXISTS request_id STRING;

CREATE INDEX IF NOT EXISTS idx_policy_ack_events_request_id
  ON policy_ack_events (request_id, observed_at DESC);
