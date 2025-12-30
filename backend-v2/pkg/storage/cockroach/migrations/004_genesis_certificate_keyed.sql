-- Migration: 004_genesis_certificate_keyed.sql
-- Purpose: Key genesis certificates by deployment identity (network_id, config_hash, peer_hash)

BEGIN TRANSACTION;

-- Drop legacy singleton constraint if present.
ALTER TABLE genesis_certificates
	DROP CONSTRAINT IF EXISTS genesis_certificates_singleton;

-- Add identity columns (idempotent).
ALTER TABLE genesis_certificates
	ADD COLUMN IF NOT EXISTS network_id STRING NOT NULL DEFAULT 'cybermesh';

ALTER TABLE genesis_certificates
	ADD COLUMN IF NOT EXISTS config_hash BYTES NOT NULL DEFAULT '\x';

ALTER TABLE genesis_certificates
	ADD COLUMN IF NOT EXISTS peer_hash BYTES NOT NULL DEFAULT '\x';

-- Backfill default hashes to avoid nulls for existing rows.
UPDATE genesis_certificates
	SET config_hash = '\x'
	WHERE config_hash IS NULL;

UPDATE genesis_certificates
	SET peer_hash = '\x'
	WHERE peer_hash IS NULL;

-- Ensure (network_id, config_hash, peer_hash) is unique.
CREATE UNIQUE INDEX IF NOT EXISTS genesis_certificates_identity_idx
	ON genesis_certificates (network_id, config_hash, peer_hash);

COMMIT;
