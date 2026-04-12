-- Migration: 043_support_delegations_break_glass.sql
-- Purpose: Add approval reference metadata required for break-glass workflow.

ALTER TABLE support_delegations
ADD COLUMN IF NOT EXISTS approval_reference STRING NULL;
