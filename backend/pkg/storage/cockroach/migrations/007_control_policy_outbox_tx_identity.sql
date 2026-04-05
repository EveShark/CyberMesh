-- Migration: 007_control_policy_outbox_tx_identity.sql
-- Purpose: Guarantee one durable outbox row per committed policy transaction.

DROP INDEX IF EXISTS uq_control_policy_outbox CASCADE;

CREATE UNIQUE INDEX IF NOT EXISTS uq_control_policy_outbox_tx
  ON control_policy_outbox (block_height, tx_index);
