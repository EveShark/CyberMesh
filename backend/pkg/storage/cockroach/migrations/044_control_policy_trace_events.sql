-- Migration: 044_control_policy_trace_events.sql
-- Purpose: Introduce an append-only canonical control-policy trace event ledger
-- so runtime and durable stages can be projected without inferring causality
-- from mutable outbox or ACK rows.

CREATE TABLE IF NOT EXISTS control_policy_trace_events (
    event_id STRING PRIMARY KEY,
    event_key STRING NOT NULL,
    policy_id STRING NOT NULL,
    trace_id STRING NOT NULL,
    stage STRING NOT NULL,
    stage_class STRING NOT NULL,
    stage_source STRING NOT NULL,
    timestamp_ms INT8 NOT NULL,
    request_id STRING NULL,
    command_id STRING NULL,
    workflow_id STRING NULL,
    source_event_id STRING NULL,
    sentinel_event_id STRING NULL,
    outbox_id STRING NULL,
    ack_event_id STRING NULL,
    rule_hash BYTES NULL,
    scope_identifier STRING NULL,
    tenant STRING NULL,
    region STRING NULL,
    reason STRING NULL,
    height INT8 NULL,
    tx_index INT8 NULL,
    view_no INT8 NULL,
    qc_ts_ms INT8 NULL,
    kafka_partition INT8 NULL,
    kafka_offset INT8 NULL,
    details_json JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_control_policy_trace_events_event_key
ON control_policy_trace_events (event_key);

CREATE INDEX IF NOT EXISTS idx_control_policy_trace_events_trace_ts
ON control_policy_trace_events (trace_id, timestamp_ms ASC, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_control_policy_trace_events_policy_ts
ON control_policy_trace_events (policy_id, timestamp_ms ASC, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_control_policy_trace_events_command_ts
ON control_policy_trace_events (command_id, timestamp_ms ASC)
WHERE command_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_control_policy_trace_events_source_event_ts
ON control_policy_trace_events (source_event_id, timestamp_ms ASC)
WHERE source_event_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_control_policy_trace_events_sentinel_event_ts
ON control_policy_trace_events (sentinel_event_id, timestamp_ms ASC)
WHERE sentinel_event_id IS NOT NULL;
