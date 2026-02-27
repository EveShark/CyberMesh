-- Migration: 006_control_policy_outbox.sql
-- Purpose: Durable commit->publish outbox with leased single-dispatcher fencing.

CREATE TABLE IF NOT EXISTS control_policy_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    block_height BIGINT NOT NULL,
    block_ts BIGINT NOT NULL,
    tx_index INT NOT NULL,
    policy_id STRING NOT NULL,
    rule_hash BYTES NOT NULL,
    payload BYTES NOT NULL,
    status STRING NOT NULL DEFAULT 'pending', -- pending|publishing|published|retry|terminal_failed|acked
    retries INT8 NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NULL,
    last_error STRING NULL,
    lease_holder STRING NULL,
    lease_epoch INT8 NOT NULL DEFAULT 0,
    kafka_topic STRING NULL,
    kafka_partition INT8 NULL,
    kafka_offset INT8 NULL,
    published_at TIMESTAMPTZ NULL,
    ack_result STRING NULL,
    ack_reason STRING NULL,
    ack_controller STRING NULL,
    acked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_control_policy_outbox UNIQUE (block_height, policy_id, rule_hash)
);

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_pending
ON control_policy_outbox (status, next_retry_at, created_at);

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_policy
ON control_policy_outbox (policy_id, created_at DESC);

CREATE TABLE IF NOT EXISTS control_dispatcher_leases (
    lease_key STRING PRIMARY KEY,
    holder_id STRING NOT NULL,
    epoch INT8 NOT NULL DEFAULT 0,
    lease_until TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
