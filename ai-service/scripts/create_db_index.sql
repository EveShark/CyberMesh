-- =================================================================
-- CyberMesh AI Service - Database Index Creation Script
-- =================================================================
-- Purpose: Create index on flows.label for efficient DDoS queries
-- Safety: Idempotent, checks for existing/invalid indexes
-- =================================================================

\set ON_ERROR_STOP on

-- Step 1: Check if index already exists and is valid
DO $$
DECLARE
    index_state TEXT;
BEGIN
    SELECT
        CASE
            WHEN i.indisvalid THEN 'valid'
            ELSE 'invalid'
        END INTO index_state
    FROM pg_class c
    JOIN pg_index i ON i.indexrelid = c.oid
    WHERE c.relname = 'idx_flows_label';

    IF FOUND THEN
        IF index_state = 'invalid' THEN
            RAISE NOTICE 'Found INVALID index idx_flows_label - dropping...';
            DROP INDEX CONCURRENTLY IF EXISTS idx_flows_label;
        ELSE
            RAISE NOTICE 'Index idx_flows_label already exists and is valid - skipping creation';
            RETURN;
        END IF;
    ELSE
        RAISE NOTICE 'Index idx_flows_label does not exist - creating...';
    END IF;
END $$;

-- Step 2: Create index concurrently (won't block table)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_flows_label 
ON public.flows(label);

-- Step 3: Verify index was created successfully
DO $$
DECLARE
    index_valid BOOLEAN;
    index_size TEXT;
BEGIN
    SELECT i.indisvalid, pg_size_pretty(pg_relation_size(c.oid))
    INTO index_valid, index_size
    FROM pg_class c
    JOIN pg_index i ON i.indexrelid = c.oid
    WHERE c.relname = 'idx_flows_label';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'Index creation failed - idx_flows_label not found';
    END IF;

    IF NOT index_valid THEN
        RAISE EXCEPTION 'Index creation failed - idx_flows_label is INVALID';
    END IF;

    RAISE NOTICE '✓ Index idx_flows_label created successfully (size: %)', index_size;
END $$;

-- Step 4: Analyze table to update statistics (helps query planner)
ANALYZE public.flows;

-- Step 5: Verify query planner will use the index
EXPLAIN (FORMAT TEXT)
SELECT COUNT(*) FROM public.flows WHERE label = 'ddos';

-- Step 6: Show index details
SELECT
    schemaname,
    tablename,
    indexname,
    indexdef
FROM pg_indexes
WHERE indexname = 'idx_flows_label';

-- Step 7: Show table stats for monitoring
SELECT
    relname AS table_name,
    reltuples::bigint AS estimated_rows,
    pg_size_pretty(pg_total_relation_size(oid)) AS total_size
FROM pg_class
WHERE relname = 'flows';

RAISE NOTICE '=================================================================';
RAISE NOTICE '✓ Index creation complete and verified';
RAISE NOTICE '=================================================================';
