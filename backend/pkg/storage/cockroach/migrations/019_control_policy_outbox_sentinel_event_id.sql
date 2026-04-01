-- Migration: 019_control_policy_outbox_sentinel_event_id.sql
-- Purpose: Persist Sentinel-owned analysis event identity on durable policy outbox rows.

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS sentinel_event_id STRING NULL;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_sentinel_event_id
  ON control_policy_outbox (sentinel_event_id, created_at DESC);
