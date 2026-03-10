-- Migration: 005_policy_acks.sql
-- Purpose: Persist enforcement ACKs emitted by enforcement-agent (control.enforcement_ack.v1)
-- Notes:
-- - This is operational telemetry for policy execution (not consensus state).
-- - Primary key is (policy_id, controller_instance) to support N agents reporting per policy.

BEGIN TRANSACTION;

CREATE TABLE IF NOT EXISTS policy_acks (
    policy_id            STRING NOT NULL,
    controller_instance  STRING NOT NULL,

    scope_identifier     STRING,
    tenant               STRING,
    region               STRING,

    result               STRING NOT NULL,
    reason               STRING,
    error_code           STRING,

    applied_at           TIMESTAMPTZ,
    acked_at             TIMESTAMPTZ,

    qc_reference         STRING,
    fast_path            BOOL NOT NULL DEFAULT FALSE,

    rule_hash            BYTES,
    producer_id          BYTES,

    observed_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (policy_id, controller_instance),

    INDEX idx_policy_acks_policy_id (policy_id),
    INDEX idx_policy_acks_acked_at (acked_at),
    INDEX idx_policy_acks_result (result)
);

COMMENT ON TABLE policy_acks IS 'Enforcement ACKs emitted by agents for control.policy.v2 policies';
COMMENT ON COLUMN policy_acks.policy_id IS 'Policy unique identifier (uuid string)';
COMMENT ON COLUMN policy_acks.controller_instance IS 'Agent instance id emitting the ACK';
COMMENT ON COLUMN policy_acks.rule_hash IS 'Rule hash for idempotency/traceability';

COMMIT;
