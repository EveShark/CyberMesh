-- Migration: 017_policy_ack_events.sql
-- Purpose: Preserve append-only ACK history alongside the latest-state policy_acks table.

BEGIN TRANSACTION;

CREATE TABLE IF NOT EXISTS policy_ack_events (
    event_id             UUID NOT NULL DEFAULT gen_random_uuid(),
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

    PRIMARY KEY (event_id),

    INDEX idx_policy_ack_events_policy_id (policy_id, observed_at DESC),
    INDEX idx_policy_ack_events_qc_reference (qc_reference, observed_at DESC),
    INDEX idx_policy_ack_events_acked_at (acked_at DESC),
    INDEX idx_policy_ack_events_policy_controller (policy_id, controller_instance, observed_at DESC)
);

COMMENT ON TABLE policy_ack_events IS 'Append-only enforcement ACK history emitted by enforcement-agent (control.enforcement_ack.v1).';
COMMENT ON COLUMN policy_ack_events.event_id IS 'Unique ACK event identifier generated at ingest time.';
COMMENT ON COLUMN policy_ack_events.policy_id IS 'Policy unique identifier (uuid string)';
COMMENT ON COLUMN policy_ack_events.controller_instance IS 'Agent instance id emitting the ACK';

COMMIT;
