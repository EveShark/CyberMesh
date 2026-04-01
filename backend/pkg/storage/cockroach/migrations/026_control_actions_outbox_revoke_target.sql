-- Migration: 026_control_actions_outbox_revoke_target.sql
-- Allow revoke mutations to journal both the source outbox row and the newly
-- created revoke outbox row under target_kind='outbox_revoke'.

ALTER TABLE control_actions_journal
DROP CONSTRAINT IF EXISTS ck_control_actions_target;

ALTER TABLE control_actions_journal
ADD CONSTRAINT ck_control_actions_target CHECK (
    ((target_kind = 'outbox') AND (outbox_id IS NOT NULL)) OR
    ((target_kind = 'lease') AND (lease_key IS NOT NULL)) OR
    ((target_kind = 'outbox_revoke') AND (outbox_id IS NOT NULL) AND (lease_key IS NOT NULL))
);
