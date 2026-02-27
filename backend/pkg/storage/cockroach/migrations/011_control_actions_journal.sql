-- Migration: 011_control_actions_journal.sql
-- Purpose: Immutable audit/idempotency journal for control-plane mutation APIs.

BEGIN TRANSACTION;

CREATE TABLE IF NOT EXISTS control_actions_journal (
    action_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action_type STRING NOT NULL,
    target_kind STRING NOT NULL,
    outbox_id UUID NULL,
    lease_key STRING NULL,
    actor STRING NOT NULL,
    reason_code STRING NOT NULL,
    reason_text STRING NOT NULL,
    idempotency_key STRING NOT NULL,
    request_id STRING NOT NULL,
    before_status STRING NULL,
    after_status STRING NULL,
    tenant_scope STRING NULL,
    classification STRING NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT ck_control_actions_target CHECK (
        (target_kind = 'outbox' AND outbox_id IS NOT NULL)
        OR (target_kind = 'lease' AND lease_key IS NOT NULL)
    ),
    CONSTRAINT uq_control_actions_idempotency UNIQUE (action_type, idempotency_key, actor)
);

CREATE INDEX IF NOT EXISTS idx_control_actions_journal_outbox
ON control_actions_journal (outbox_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_control_actions_journal_lease
ON control_actions_journal (lease_key, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_control_actions_journal_created
ON control_actions_journal (created_at DESC);

COMMIT;
