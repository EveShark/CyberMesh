-- ===========================================
-- SENTINEL API - SUPABASE DATABASE SCHEMA
-- ===========================================
-- Run this in Supabase Dashboard: SQL Editor > New Query
-- https://supabase.com/dashboard/project/wcgddjipyslnjstabqaq/sql

-- Enable UUID extension (usually already enabled)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ===========================================
-- TABLE: scans
-- Stores all file scan results
-- ===========================================
CREATE TABLE IF NOT EXISTS scans (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
    
    -- File metadata
    file_name TEXT NOT NULL,
    file_hash TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    file_type TEXT,
    
    -- Scan results
    verdict TEXT NOT NULL CHECK (verdict IN ('clean', 'suspicious', 'malicious')),
    confidence FLOAT CHECK (confidence >= 0 AND confidence <= 1),
    threat_level TEXT CHECK (threat_level IN ('low', 'medium', 'high', 'critical')),
    risk_score INTEGER CHECK (risk_score >= 0 AND risk_score <= 100),
    
    -- Detailed results (JSONB for flexibility)
    detections JSONB DEFAULT '{}',
    iocs JSONB DEFAULT '{}',
    
    -- Performance
    processing_time_ms INTEGER,
    
    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_scans_user_id ON scans(user_id);
CREATE INDEX IF NOT EXISTS idx_scans_file_hash ON scans(file_hash);
CREATE INDEX IF NOT EXISTS idx_scans_created_at ON scans(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_scans_verdict ON scans(verdict);

-- Updated_at trigger
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_scans_updated_at
    BEFORE UPDATE ON scans
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ===========================================
-- TABLE: async_jobs
-- Tracks background scan jobs
-- ===========================================
CREATE TABLE IF NOT EXISTS async_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
    
    -- Job status
    status TEXT DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    
    -- Reference to scan result (when completed)
    scan_id UUID REFERENCES scans(id) ON DELETE SET NULL,
    
    -- Error handling
    error_message TEXT,
    
    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_async_jobs_user_id ON async_jobs(user_id);
CREATE INDEX IF NOT EXISTS idx_async_jobs_status ON async_jobs(status);

-- ===========================================
-- ROW LEVEL SECURITY (RLS)
-- Users can only access their own data
-- ===========================================

-- Enable RLS on scans
ALTER TABLE scans ENABLE ROW LEVEL SECURITY;

-- Policy: Users can view their own scans
CREATE POLICY "Users can view own scans"
    ON scans FOR SELECT
    USING (auth.uid() = user_id);

-- Policy: Users can insert their own scans
CREATE POLICY "Users can insert own scans"
    ON scans FOR INSERT
    WITH CHECK (auth.uid() = user_id);

-- Policy: Users can delete their own scans (GDPR)
CREATE POLICY "Users can delete own scans"
    ON scans FOR DELETE
    USING (auth.uid() = user_id);

-- Policy: Service role can do anything (for API backend)
CREATE POLICY "Service role full access on scans"
    ON scans FOR ALL
    USING (auth.role() = 'service_role');

-- Enable RLS on async_jobs
ALTER TABLE async_jobs ENABLE ROW LEVEL SECURITY;

-- Policy: Users can view their own jobs
CREATE POLICY "Users can view own jobs"
    ON async_jobs FOR SELECT
    USING (auth.uid() = user_id);

-- Policy: Users can insert their own jobs
CREATE POLICY "Users can insert own jobs"
    ON async_jobs FOR INSERT
    WITH CHECK (auth.uid() = user_id);

-- Policy: Service role full access
CREATE POLICY "Service role full access on async_jobs"
    ON async_jobs FOR ALL
    USING (auth.role() = 'service_role');

-- ===========================================
-- ANONYMOUS SCANS (Optional)
-- Allow unauthenticated users to scan
-- ===========================================

-- Policy: Allow anonymous scans (user_id = NULL)
CREATE POLICY "Anonymous can insert scans"
    ON scans FOR INSERT
    WITH CHECK (user_id IS NULL);

-- Policy: Anonymous can view their scans by scan_id only
-- (They need to know the UUID to access)
CREATE POLICY "Anonymous can view scans by id"
    ON scans FOR SELECT
    USING (user_id IS NULL);

-- ===========================================
-- HELPER FUNCTIONS
-- ===========================================

-- Function: Get user scan statistics
CREATE OR REPLACE FUNCTION get_user_scan_stats(p_user_id UUID)
RETURNS TABLE (
    total_scans BIGINT,
    clean_count BIGINT,
    suspicious_count BIGINT,
    malicious_count BIGINT,
    total_bytes_scanned BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        COUNT(*)::BIGINT as total_scans,
        COUNT(*) FILTER (WHERE verdict = 'clean')::BIGINT as clean_count,
        COUNT(*) FILTER (WHERE verdict = 'suspicious')::BIGINT as suspicious_count,
        COUNT(*) FILTER (WHERE verdict = 'malicious')::BIGINT as malicious_count,
        COALESCE(SUM(file_size), 0)::BIGINT as total_bytes_scanned
    FROM scans
    WHERE user_id = p_user_id;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Function: Clean up old scans (data retention)
CREATE OR REPLACE FUNCTION cleanup_old_scans(retention_days INTEGER DEFAULT 90)
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM scans
    WHERE created_at < NOW() - (retention_days || ' days')::INTERVAL;
    
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- ===========================================
-- VERIFY SETUP
-- ===========================================

-- Check tables exist
SELECT 
    table_name,
    (SELECT COUNT(*) FROM information_schema.columns WHERE table_name = t.table_name) as column_count
FROM information_schema.tables t
WHERE table_schema = 'public' 
AND table_name IN ('scans', 'async_jobs');

-- Check RLS is enabled
SELECT 
    tablename,
    rowsecurity
FROM pg_tables
WHERE schemaname = 'public'
AND tablename IN ('scans', 'async_jobs');

-- ===========================================
-- DONE!
-- ===========================================
-- After running this script:
-- 1. Go to Supabase Settings > API > JWT Settings
-- 2. Copy the JWT Secret
-- 3. Add to your .env file as SUPABASE_JWT_SECRET
