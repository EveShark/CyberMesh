-- Migration: 003_genesis_certificate.sql
-- Purpose: Persist genesis certificate for durable restoration across validator restarts

BEGIN TRANSACTION;

CREATE TABLE IF NOT EXISTS genesis_certificates (
    id SERIAL PRIMARY KEY,
    certificate BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Enforce single-row table by constraining id to 1. Using CHECK keeps UPSERT simple.
ALTER TABLE genesis_certificates
    ADD CONSTRAINT genesis_certificates_singleton CHECK (id = 1);

COMMIT;
