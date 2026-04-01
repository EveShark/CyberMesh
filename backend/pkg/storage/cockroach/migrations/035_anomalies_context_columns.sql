-- Migration: 035_anomalies_context_columns.sql
-- Purpose: Extend anomalies with explicit operator/query context fields used by
-- downstream lineage and investigation paths.

ALTER TABLE anomalies
    ADD COLUMN IF NOT EXISTS flow_id STRING NULL,
    ADD COLUMN IF NOT EXISTS sensor_id STRING NULL,
    ADD COLUMN IF NOT EXISTS validator_id STRING NULL,
    ADD COLUMN IF NOT EXISTS scope_identifier STRING NULL;

CREATE INDEX IF NOT EXISTS idx_anomalies_flow_id
ON anomalies (flow_id, detected_at DESC)
WHERE flow_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_anomalies_sensor_id
ON anomalies (sensor_id, detected_at DESC)
WHERE sensor_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_anomalies_validator_id
ON anomalies (validator_id, detected_at DESC)
WHERE validator_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_anomalies_scope_identifier
ON anomalies (scope_identifier, detected_at DESC)
WHERE scope_identifier IS NOT NULL;
