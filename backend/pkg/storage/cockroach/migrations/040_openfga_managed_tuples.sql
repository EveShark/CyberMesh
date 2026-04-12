-- Migration: 040_openfga_managed_tuples.sql
-- Purpose: Track backend-owned OpenFGA tuples for deterministic reconciliation.

CREATE TABLE IF NOT EXISTS openfga_managed_tuples (
    source STRING NOT NULL,
    user_key STRING NOT NULL,
    relation STRING NOT NULL,
    object_key STRING NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_openfga_managed_tuples PRIMARY KEY (source, user_key, relation, object_key)
);

CREATE INDEX IF NOT EXISTS idx_openfga_managed_tuples_source_updated
ON openfga_managed_tuples (source, updated_at DESC);
