-- Migration: 038_auth_access_memberships.sql
-- Purpose: App-owned trusted access memberships for authenticated principals.

CREATE TABLE IF NOT EXISTS auth_access_memberships (
    principal_id STRING NOT NULL,
    access_id STRING NOT NULL,
    status STRING NOT NULL DEFAULT 'active',
    is_primary BOOL NOT NULL DEFAULT false,
    source STRING NOT NULL DEFAULT 'manual',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_auth_access_memberships PRIMARY KEY (principal_id, access_id),
    CONSTRAINT chk_auth_access_memberships_status CHECK (status IN ('active', 'revoked'))
);

CREATE INDEX IF NOT EXISTS idx_auth_access_memberships_principal_status
ON auth_access_memberships (principal_id, status, is_primary DESC, access_id ASC);

CREATE INDEX IF NOT EXISTS idx_auth_access_memberships_access_status
ON auth_access_memberships (access_id, status);
