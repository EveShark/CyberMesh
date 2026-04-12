-- Migration: 041_support_delegations.sql
-- Purpose: App-owned delegated support grants used by the normalized auth path.

CREATE TABLE IF NOT EXISTS support_delegations (
    delegation_id STRING PRIMARY KEY,
    principal_id STRING NOT NULL,
    principal_type STRING NOT NULL,
    approved_by_principal_id STRING NOT NULL,
    reason_code STRING NOT NULL,
    reason_text STRING NOT NULL DEFAULT '',
    status STRING NOT NULL DEFAULT 'pending',
    break_glass BOOL NOT NULL DEFAULT false,
    starts_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_support_delegations_principal_type CHECK (principal_type IN ('user', 'service')),
    CONSTRAINT chk_support_delegations_status CHECK (status IN ('pending', 'active', 'revoked', 'expired', 'rejected')),
    CONSTRAINT chk_support_delegations_window CHECK (expires_at > starts_at)
);

CREATE INDEX IF NOT EXISTS idx_support_delegations_principal_status_window
ON support_delegations (principal_id, principal_type, status, break_glass, starts_at, expires_at);

CREATE TABLE IF NOT EXISTS support_delegation_accesses (
    delegation_id STRING NOT NULL REFERENCES support_delegations (delegation_id) ON DELETE CASCADE,
    access_id STRING NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_support_delegation_accesses PRIMARY KEY (delegation_id, access_id)
);

CREATE INDEX IF NOT EXISTS idx_support_delegation_accesses_access
ON support_delegation_accesses (access_id);
