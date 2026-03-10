-- Migration: 016_control_policy_outbox_tx_verify_covering.sql
-- Purpose: Remove index-join overhead for tx-identity outbox conflict verification.
-- Query shape:
--   SELECT block_height, tx_index, policy_id, rule_hash
--   FROM control_policy_outbox
--   WHERE (block_height = $1 AND tx_index = $2) OR ...
--
-- Existing unique index uq_control_policy_outbox_tx is not covering for policy_id/rule_hash,
-- so Cockroach plans an index join against the primary index.
-- This covering index keeps verification reads on a single index lookup path.

CREATE INDEX IF NOT EXISTS idx_control_policy_outbox_tx_verify_covering
ON control_policy_outbox (block_height, tx_index)
STORING (policy_id, rule_hash);

