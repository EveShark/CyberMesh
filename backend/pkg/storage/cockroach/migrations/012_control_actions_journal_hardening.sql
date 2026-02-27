-- Migration: 012_control_actions_journal_hardening.sql
-- Purpose: Harden immutable action journal with lease epoch lineage and tamper-evident decision hash.

BEGIN TRANSACTION;

ALTER TABLE control_actions_journal
  ADD COLUMN IF NOT EXISTS before_lease_epoch INT8 NULL;

ALTER TABLE control_actions_journal
  ADD COLUMN IF NOT EXISTS after_lease_epoch INT8 NULL;

ALTER TABLE control_actions_journal
  ADD COLUMN IF NOT EXISTS decision_hash STRING NULL;

CREATE INDEX IF NOT EXISTS idx_control_actions_journal_forensics
ON control_actions_journal (created_at DESC, action_type, tenant_scope);

CREATE INDEX IF NOT EXISTS idx_control_actions_journal_decision_hash
ON control_actions_journal (decision_hash);

COMMIT;
