-- Migration: 021_policy_ack_event_ids.sql
-- Purpose: Persist explicit immutable ACK event identifiers on ACK latest-state and history tables.

BEGIN TRANSACTION;

ALTER TABLE policy_acks
  ADD COLUMN IF NOT EXISTS ack_event_id STRING;

CREATE INDEX IF NOT EXISTS idx_policy_acks_ack_event_id
  ON policy_acks (ack_event_id);

ALTER TABLE policy_ack_events
  ADD COLUMN IF NOT EXISTS ack_event_id STRING;

CREATE INDEX IF NOT EXISTS idx_policy_ack_events_ack_event_id
  ON policy_ack_events (ack_event_id, observed_at DESC);

COMMIT;
