-- Migration: 032_anomalies.sql
-- Purpose: Persist anomaly detections in a dedicated table so anomaly APIs do
-- not depend on transactions.payload being stored in full mode.

CREATE TABLE IF NOT EXISTS anomalies (
    anomaly_id STRING PRIMARY KEY,
    threat_type STRING NOT NULL,
    severity_value FLOAT8 NOT NULL,
    confidence FLOAT8 NOT NULL DEFAULT 0,
    title STRING NULL,
    description STRING NULL,
    source STRING NOT NULL,
    model_version STRING NULL,
    flow_key STRING NULL,
    source_event_id STRING NULL,
    source_event_ts_ms INT8 NULL,
    sentinel_event_id STRING NULL,
    block_height BIGINT NOT NULL,
    tx_hash STRING NOT NULL,
    detected_at BIGINT NOT NULL,
    raw_payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_anomalies_tx_hash
ON anomalies (tx_hash);

CREATE INDEX IF NOT EXISTS idx_anomalies_detected_at
ON anomalies (detected_at DESC);

CREATE INDEX IF NOT EXISTS idx_anomalies_block_height
ON anomalies (block_height DESC);

CREATE INDEX IF NOT EXISTS idx_anomalies_severity
ON anomalies (severity_value DESC, detected_at DESC);

CREATE INDEX IF NOT EXISTS idx_anomalies_source_event_id
ON anomalies (source_event_id, detected_at DESC);

CREATE INDEX IF NOT EXISTS idx_anomalies_sentinel_event_id
ON anomalies (sentinel_event_id, detected_at DESC);
