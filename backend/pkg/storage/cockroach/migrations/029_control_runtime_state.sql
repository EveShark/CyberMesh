-- Migration: 029_control_runtime_state.sql
-- Shared control-plane runtime flags used across validators.

CREATE TABLE IF NOT EXISTS control_runtime_state (
    state_key STRING PRIMARY KEY,
    enabled BOOL NOT NULL,
    reason_code STRING NOT NULL,
    reason_text STRING NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
