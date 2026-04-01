-- Migration: 030_control_actions_outbox_policy_decision_targets.sql
-- Allow approve/reject policy decision mutations to journal both the source
-- outbox row and the newly created decision outbox row.

ALTER TABLE control_actions_journal
DROP CONSTRAINT IF EXISTS ck_control_actions_target;

ALTER TABLE control_actions_journal
ADD CONSTRAINT ck_control_actions_target CHECK (
    ((target_kind = 'outbox') AND (outbox_id IS NOT NULL)) OR
    ((target_kind = 'lease') AND (lease_key IS NOT NULL)) OR
    ((target_kind = 'outbox_revoke') AND (outbox_id IS NOT NULL) AND (lease_key IS NOT NULL)) OR
    ((target_kind = 'outbox_approve') AND (outbox_id IS NOT NULL) AND (lease_key IS NOT NULL)) OR
    ((target_kind = 'outbox_reject') AND (outbox_id IS NOT NULL) AND (lease_key IS NOT NULL))
);
