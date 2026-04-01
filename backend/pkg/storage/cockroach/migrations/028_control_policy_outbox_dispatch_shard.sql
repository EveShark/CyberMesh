-- Migration: 028_control_policy_outbox_dispatch_shard.sql
-- Purpose: Support lease-safe parallel outbox dispatch across fixed shard buckets.

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS dispatch_shard STRING NULL;

UPDATE control_policy_outbox
SET dispatch_shard = 'shard:000'
WHERE dispatch_shard IS NULL;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_dispatch_claim
  ON control_policy_outbox (dispatch_shard, status, next_retry_at, created_at)
  STORING (updated_at, lease_holder, lease_epoch)
  WHERE dispatch_shard IS NOT NULL;
