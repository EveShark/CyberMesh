-- Migration: 014_control_policy_outbox_ai_event_ts_normalize.sql
-- Purpose: Normalize ai_event_ts_ms historical rows to millisecond epoch units.

BEGIN TRANSACTION;

-- Seconds -> milliseconds (roughly years 2000..2100 in seconds).
UPDATE control_policy_outbox
SET ai_event_ts_ms = ai_event_ts_ms * 1000
WHERE ai_event_ts_ms BETWEEN 946684800 AND 4102444800;

-- Microseconds -> milliseconds.
UPDATE control_policy_outbox
SET ai_event_ts_ms = CAST(ai_event_ts_ms / 1000 AS INT8)
WHERE ai_event_ts_ms BETWEEN 946684800000000 AND 4102444800000000;

-- Nanoseconds -> milliseconds.
UPDATE control_policy_outbox
SET ai_event_ts_ms = CAST(ai_event_ts_ms / 1000000 AS INT8)
WHERE ai_event_ts_ms BETWEEN 946684800000000000 AND 4102444800000000000;

COMMIT;
