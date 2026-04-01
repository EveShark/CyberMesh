-- Migration: 034_control_actions_journal_targets.sql
-- Purpose: Promote workflow and policy target identifiers to first-class audit
-- columns for control-plane mutations.

ALTER TABLE control_actions_journal
    ADD COLUMN IF NOT EXISTS workflow_id STRING NULL,
    ADD COLUMN IF NOT EXISTS policy_id STRING NULL;

CREATE INDEX IF NOT EXISTS idx_control_actions_journal_workflow
ON control_actions_journal (workflow_id, created_at DESC)
WHERE workflow_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_control_actions_journal_policy
ON control_actions_journal (policy_id, created_at DESC)
WHERE policy_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_control_actions_journal_target_kind
ON control_actions_journal (target_kind, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_control_actions_journal_action_type
ON control_actions_journal (action_type, created_at DESC);
