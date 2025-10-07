-- Migration: 002_tx_nonce_unique.sql
-- Purpose: Enforce unique (producer_id, nonce) combinations to prevent replay after restart

ALTER TABLE transactions
    ADD CONSTRAINT transactions_producer_nonce_unique
    UNIQUE (producer_id, nonce);
