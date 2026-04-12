-- Migration: 042_support_delegations_audit_fields.sql
-- Purpose: Add approval and revocation audit fields for support delegations.

ALTER TABLE support_delegations
ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ NULL;

ALTER TABLE support_delegations
ADD COLUMN IF NOT EXISTS revoked_by_principal_id STRING NULL;

ALTER TABLE support_delegations
ADD COLUMN IF NOT EXISTS revoked_reason_code STRING NULL;

ALTER TABLE support_delegations
ADD COLUMN IF NOT EXISTS revoked_reason_text STRING NULL;
