-- Migration: 039_auth_access_membership_role.sql
-- Purpose: Persist explicit access role for OpenFGA tuple ownership.

ALTER TABLE auth_access_memberships
  ADD COLUMN IF NOT EXISTS role STRING NOT NULL DEFAULT 'viewer';

ALTER TABLE auth_access_memberships
  ADD CONSTRAINT IF NOT EXISTS chk_auth_access_memberships_role
  CHECK (role IN ('platform_admin', 'support_delegate', 'admin', 'analyst', 'viewer', 'responder'));

CREATE INDEX IF NOT EXISTS idx_auth_access_memberships_principal_role
ON auth_access_memberships (principal_id, status, role, access_id);
