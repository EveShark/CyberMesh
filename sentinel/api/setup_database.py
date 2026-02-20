#!/usr/bin/env python
"""
Setup Supabase database tables for Sentinel API.

This script uses the Supabase Management API to create the required tables.
You need a Personal Access Token (PAT) from https://supabase.com/dashboard/account/tokens

Usage:
    python setup_database.py <SUPABASE_ACCESS_TOKEN>
    
Or set the environment variable:
    SUPABASE_ACCESS_TOKEN=sbp_xxxxx python setup_database.py
"""

import os
import sys
import httpx

# Get project reference from Supabase URL
# URL format: https://<project_ref>.supabase.co
SUPABASE_URL = os.getenv("SUPABASE_URL", "https://wcgddjipyslnjstabqaq.supabase.co")
PROJECT_REF = SUPABASE_URL.split("//")[1].split(".")[0]

MANAGEMENT_API_URL = f"https://api.supabase.com/v1/projects/{PROJECT_REF}/database/query"

SQL_SCHEMA = """
-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Table: scans
CREATE TABLE IF NOT EXISTS scans (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
    file_name TEXT NOT NULL,
    file_hash TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    file_type TEXT,
    verdict TEXT NOT NULL CHECK (verdict IN ('clean', 'suspicious', 'malicious')),
    confidence FLOAT CHECK (confidence >= 0 AND confidence <= 1),
    threat_level TEXT CHECK (threat_level IN ('low', 'medium', 'high', 'critical')),
    risk_score INTEGER CHECK (risk_score >= 0 AND risk_score <= 100),
    detections JSONB DEFAULT '{}',
    iocs JSONB DEFAULT '{}',
    processing_time_ms INTEGER,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for scans
CREATE INDEX IF NOT EXISTS idx_scans_user_id ON scans(user_id);
CREATE INDEX IF NOT EXISTS idx_scans_file_hash ON scans(file_hash);
CREATE INDEX IF NOT EXISTS idx_scans_created_at ON scans(created_at DESC);

-- Table: async_jobs
CREATE TABLE IF NOT EXISTS async_jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
    status TEXT DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    scan_id UUID REFERENCES scans(id) ON DELETE SET NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

-- Index for async_jobs
CREATE INDEX IF NOT EXISTS idx_async_jobs_user_id ON async_jobs(user_id);
"""

RLS_POLICIES = """
-- Enable RLS
ALTER TABLE scans ENABLE ROW LEVEL SECURITY;
ALTER TABLE async_jobs ENABLE ROW LEVEL SECURITY;

-- Drop existing policies if any
DROP POLICY IF EXISTS "Users can view own scans" ON scans;
DROP POLICY IF EXISTS "Users can insert own scans" ON scans;
DROP POLICY IF EXISTS "Users can delete own scans" ON scans;
DROP POLICY IF EXISTS "Anonymous can insert scans" ON scans;
DROP POLICY IF EXISTS "Anonymous can view scans" ON scans;
DROP POLICY IF EXISTS "Users can view own jobs" ON async_jobs;
DROP POLICY IF EXISTS "Users can insert own jobs" ON async_jobs;

-- Scans policies
CREATE POLICY "Users can view own scans" ON scans FOR SELECT USING (auth.uid() = user_id);
CREATE POLICY "Users can insert own scans" ON scans FOR INSERT WITH CHECK (auth.uid() = user_id OR user_id IS NULL);
CREATE POLICY "Users can delete own scans" ON scans FOR DELETE USING (auth.uid() = user_id);
CREATE POLICY "Anonymous can insert scans" ON scans FOR INSERT WITH CHECK (user_id IS NULL);
CREATE POLICY "Anonymous can view scans" ON scans FOR SELECT USING (user_id IS NULL);

-- Async jobs policies
CREATE POLICY "Users can view own jobs" ON async_jobs FOR SELECT USING (auth.uid() = user_id);
CREATE POLICY "Users can insert own jobs" ON async_jobs FOR INSERT WITH CHECK (auth.uid() = user_id);
"""


def run_sql(access_token: str, sql: str) -> dict:
    """Execute SQL via Supabase Management API."""
    headers = {
        "Authorization": f"Bearer {access_token}",
        "Content-Type": "application/json",
    }
    
    response = httpx.post(
        MANAGEMENT_API_URL,
        headers=headers,
        json={"query": sql},
        timeout=60.0,
    )
    
    if response.status_code == 201 or response.status_code == 200:
        return {"success": True, "data": response.json()}
    else:
        return {"success": False, "error": response.text, "status": response.status_code}


def main():
    # Get access token
    access_token = os.getenv("SUPABASE_ACCESS_TOKEN")
    
    if not access_token and len(sys.argv) > 1:
        access_token = sys.argv[1]
    
    if not access_token:
        print("Error: SUPABASE_ACCESS_TOKEN required")
        print("\nGet your token from: https://supabase.com/dashboard/account/tokens")
        print("\nUsage:")
        print("  python setup_database.py <TOKEN>")
        print("  or")
        print("  SUPABASE_ACCESS_TOKEN=sbp_xxx python setup_database.py")
        sys.exit(1)
    
    print(f"Project Reference: {PROJECT_REF}")
    print(f"API URL: {MANAGEMENT_API_URL}")
    print()
    
    # Create tables
    print("Creating tables...")
    result = run_sql(access_token, SQL_SCHEMA)
    if result["success"]:
        print("  Tables created successfully")
    else:
        print(f"  Error: {result.get('error', 'Unknown error')}")
        if result.get("status") == 401:
            print("  -> Check your access token is valid")
        elif result.get("status") == 404:
            print("  -> Check your project reference is correct")
        sys.exit(1)
    
    # Set up RLS
    print("Setting up Row Level Security...")
    result = run_sql(access_token, RLS_POLICIES)
    if result["success"]:
        print("  RLS policies created successfully")
    else:
        print(f"  Warning: {result.get('error', 'Unknown error')}")
        print("  (This might be OK if policies already exist)")
    
    print()
    print("Database setup complete!")
    print()
    print("Next steps:")
    print("1. Get your JWT secret from Supabase Dashboard:")
    print("   Settings -> API -> JWT Settings -> JWT Secret")
    print("2. Add to .env: SUPABASE_JWT_SECRET=your-secret")
    print("3. Start the API: python -m uvicorn app.main:app --reload")


if __name__ == "__main__":
    main()
