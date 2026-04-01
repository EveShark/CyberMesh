-- Migration: 033_state_policies_projection.sql
-- Purpose: Extend legacy state_policies so control-plane writes can maintain a
-- durable current-state projection for operator workflows.

ALTER TABLE state_policies
    ADD COLUMN IF NOT EXISTS policy_id_text STRING NULL,
    ADD COLUMN IF NOT EXISTS workflow_id STRING NULL,
    ADD COLUMN IF NOT EXISTS tenant STRING NULL,
    ADD COLUMN IF NOT EXISTS current_status STRING NULL,
    ADD COLUMN IF NOT EXISTS latest_action STRING NULL,
    ADD COLUMN IF NOT EXISTS latest_ack_result STRING NULL,
    ADD COLUMN IF NOT EXISTS latest_ack_reason STRING NULL,
    ADD COLUMN IF NOT EXISTS trace_id STRING NULL,
    ADD COLUMN IF NOT EXISTS anomaly_id STRING NULL,
    ADD COLUMN IF NOT EXISTS source_event_id STRING NULL,
    ADD COLUMN IF NOT EXISTS sentinel_event_id STRING NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_state_policies_policy_id_text
ON state_policies (policy_id_text)
WHERE policy_id_text IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_state_policies_workflow_id
ON state_policies (workflow_id)
WHERE workflow_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_state_policies_tenant_status
ON state_policies (tenant, current_status)
WHERE tenant IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_state_policies_trace_id
ON state_policies (trace_id)
WHERE trace_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_state_policies_anomaly_id
ON state_policies (anomaly_id)
WHERE anomaly_id IS NOT NULL;
