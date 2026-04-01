-- Migration: 025_policy_ack_event_id_uniqueness.sql
-- Purpose: enforce global uniqueness of immutable ACK event identifiers on history rows.

BEGIN TRANSACTION;

CREATE UNIQUE INDEX IF NOT EXISTS idx_policy_ack_events_ack_event_id_unique
  ON policy_ack_events (ack_event_id)
  WHERE ack_event_id IS NOT NULL;

COMMIT;
