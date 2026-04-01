-- Migration: 023_policy_command_ids.sql
-- Purpose: persist command_id across outbox and ACK tables for operator command tracing

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS command_id STRING;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_command_id
  ON control_policy_outbox (command_id, created_at DESC);

ALTER TABLE policy_acks
  ADD COLUMN IF NOT EXISTS command_id STRING;

CREATE INDEX IF NOT EXISTS idx_policy_acks_command_id
  ON policy_acks (command_id, observed_at DESC);

ALTER TABLE policy_ack_events
  ADD COLUMN IF NOT EXISTS command_id STRING;

CREATE INDEX IF NOT EXISTS idx_policy_ack_events_command_id
  ON policy_ack_events (command_id, observed_at DESC);
