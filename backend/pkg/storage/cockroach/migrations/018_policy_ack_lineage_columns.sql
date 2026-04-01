-- Migration: 018_policy_ack_lineage_columns.sql
-- Purpose: Persist explicit lineage identifiers on ACK latest-state and history tables.

BEGIN TRANSACTION;

ALTER TABLE policy_acks
  ADD COLUMN IF NOT EXISTS trace_id STRING NULL,
  ADD COLUMN IF NOT EXISTS source_event_id STRING NULL,
  ADD COLUMN IF NOT EXISTS sentinel_event_id STRING NULL;

CREATE INDEX IF NOT EXISTS idx_policy_acks_trace_id
  ON policy_acks (trace_id);

CREATE INDEX IF NOT EXISTS idx_policy_acks_source_event_id
  ON policy_acks (source_event_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_policy_acks_sentinel_event_id
  ON policy_acks (sentinel_event_id, observed_at DESC);

ALTER TABLE policy_ack_events
  ADD COLUMN IF NOT EXISTS trace_id STRING NULL,
  ADD COLUMN IF NOT EXISTS source_event_id STRING NULL,
  ADD COLUMN IF NOT EXISTS sentinel_event_id STRING NULL;

CREATE INDEX IF NOT EXISTS idx_policy_ack_events_trace_id
  ON policy_ack_events (trace_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_policy_ack_events_source_event_id
  ON policy_ack_events (source_event_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_policy_ack_events_sentinel_event_id
  ON policy_ack_events (sentinel_event_id, observed_at DESC);

COMMIT;
