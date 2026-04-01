-- Migration: 031_control_actions_workflow_target.sql
-- Allow workflow rollback mutations to journal the workflow identifier as a
-- first-class target using lease_key as the stored workflow handle.

ALTER TABLE control_actions_journal
DROP CONSTRAINT IF EXISTS ck_control_actions_target;

ALTER TABLE control_actions_journal
ADD CONSTRAINT ck_control_actions_target CHECK (
    ((target_kind = 'outbox') AND (outbox_id IS NOT NULL)) OR
    ((target_kind = 'lease') AND (lease_key IS NOT NULL)) OR
    ((target_kind = 'outbox_revoke') AND (outbox_id IS NOT NULL) AND (lease_key IS NOT NULL)) OR
    ((target_kind = 'outbox_approve') AND (outbox_id IS NOT NULL) AND (lease_key IS NOT NULL)) OR
    ((target_kind = 'outbox_reject') AND (outbox_id IS NOT NULL) AND (lease_key IS NOT NULL)) OR
    ((target_kind = 'workflow') AND (lease_key IS NOT NULL))
);
