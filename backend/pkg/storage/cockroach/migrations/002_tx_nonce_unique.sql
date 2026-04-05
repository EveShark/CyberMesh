-- Migration: 002_tx_nonce_unique.sql
-- Purpose: Enforce unique (producer_id, nonce) combinations to prevent replay after restart

CREATE UNIQUE INDEX IF NOT EXISTS transactions_producer_nonce_unique
    ON transactions (producer_id, nonce);
