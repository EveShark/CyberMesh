-- Migration: 027_control_policy_outbox_semantic_key.sql
-- Purpose: Support active semantic dedupe for aggregated policy intents.

ALTER TABLE control_policy_outbox
  ADD COLUMN IF NOT EXISTS semantic_key STRING NULL;

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_semantic_active
  ON control_policy_outbox (semantic_key, created_at DESC)
  STORING (status, policy_id, rule_hash)
  WHERE semantic_key IS NOT NULL
    AND status IN ('pending', 'retry', 'publishing', 'published');

-- DB-level active semantic fence to close check-then-insert races under concurrent writers.
CREATE UNIQUE INDEX IF NOT EXISTS uq_control_policy_outbox_semantic_active
  ON control_policy_outbox (semantic_key)
  WHERE semantic_key IS NOT NULL
    AND status IN ('pending', 'retry', 'publishing', 'published');
