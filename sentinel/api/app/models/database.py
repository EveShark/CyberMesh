"""Supabase database client and operations."""

from typing import Optional, List, Dict, Any
from datetime import datetime
from supabase import create_client, Client

from ..config import get_settings


class Database:
    """Supabase database wrapper."""
    
    _client: Optional[Client] = None
    
    @classmethod
    def get_client(cls) -> Client:
        """Get or create Supabase client."""
        if cls._client is None:
            settings = get_settings()
            cls._client = create_client(
                settings.supabase_url,
                settings.supabase_key,
            )
        return cls._client
    
    @classmethod
    async def save_scan(
        cls,
        user_id: Optional[str],
        file_name: str,
        file_hash: str,
        file_size: int,
        file_type: str,
        verdict: str,
        confidence: float,
        threat_level: str,
        risk_score: int,
        detections: Dict[str, Any],
        iocs: Optional[Dict[str, Any]],
        processing_time_ms: int,
    ) -> Dict[str, Any]:
        """Save scan result to database."""
        client = cls.get_client()
        
        data = {
            "user_id": user_id,
            "file_name": file_name,
            "file_hash": file_hash,
            "file_size": file_size,
            "file_type": file_type,
            "verdict": verdict,
            "confidence": confidence,
            "threat_level": threat_level,
            "risk_score": risk_score,
            "detections": detections,
            "iocs": iocs,
            "processing_time_ms": processing_time_ms,
        }
        
        result = client.table("scans").insert(data).execute()
        
        if result.data:
            return result.data[0]
        return {}
    
    @classmethod
    async def get_scan(cls, scan_id: str, user_id: Optional[str] = None) -> Optional[Dict[str, Any]]:
        """Get scan by ID."""
        client = cls.get_client()
        
        query = client.table("scans").select("*").eq("id", scan_id)
        
        if user_id:
            query = query.eq("user_id", user_id)
        
        result = query.execute()
        
        if result.data:
            return result.data[0]
        return None
    
    @classmethod
    async def get_scan_by_hash(cls, file_hash: str, user_id: Optional[str] = None) -> Optional[Dict[str, Any]]:
        """Get most recent scan by file hash."""
        client = cls.get_client()
        
        query = client.table("scans").select("*").eq("file_hash", file_hash)
        
        if user_id:
            query = query.eq("user_id", user_id)
        
        result = query.order("created_at", desc=True).limit(1).execute()
        
        if result.data:
            return result.data[0]
        return None
    
    @classmethod
    async def get_user_history(
        cls,
        user_id: str,
        page: int = 1,
        limit: int = 20,
    ) -> tuple[List[Dict[str, Any]], int]:
        """Get user's scan history with pagination."""
        client = cls.get_client()
        
        offset = (page - 1) * limit
        
        # Get total count
        count_result = client.table("scans").select("id", count="exact").eq("user_id", user_id).execute()
        total = count_result.count or 0
        
        # Get paginated results
        result = (
            client.table("scans")
            .select("id, file_name, file_hash, file_size, verdict, threat_level, created_at")
            .eq("user_id", user_id)
            .order("created_at", desc=True)
            .range(offset, offset + limit - 1)
            .execute()
        )
        
        return result.data or [], total
    
    @classmethod
    async def delete_scan(cls, scan_id: str, user_id: str) -> bool:
        """Delete scan by ID (user must own it)."""
        client = cls.get_client()
        
        result = (
            client.table("scans")
            .delete()
            .eq("id", scan_id)
            .eq("user_id", user_id)
            .execute()
        )
        
        return len(result.data) > 0 if result.data else False
    
    @classmethod
    async def create_async_job(cls, user_id: str) -> Dict[str, Any]:
        """Create async job record."""
        client = cls.get_client()
        
        data = {
            "user_id": user_id,
            "status": "pending",
        }
        
        result = client.table("async_jobs").insert(data).execute()
        
        if result.data:
            return result.data[0]
        return {}
    
    @classmethod
    async def update_async_job(
        cls,
        job_id: str,
        status: str,
        scan_id: Optional[str] = None,
        error_message: Optional[str] = None,
    ) -> bool:
        """Update async job status."""
        client = cls.get_client()
        
        data = {"status": status}
        
        if scan_id:
            data["scan_id"] = scan_id
        if error_message:
            data["error_message"] = error_message
        if status in ("completed", "failed"):
            data["completed_at"] = datetime.utcnow().isoformat()
        
        result = client.table("async_jobs").update(data).eq("id", job_id).execute()
        
        return len(result.data) > 0 if result.data else False
    
    @classmethod
    async def get_async_job(cls, job_id: str, user_id: str) -> Optional[Dict[str, Any]]:
        """Get async job by ID."""
        client = cls.get_client()
        
        result = (
            client.table("async_jobs")
            .select("*, scans(*)")
            .eq("id", job_id)
            .eq("user_id", user_id)
            .execute()
        )
        
        if result.data:
            return result.data[0]
        return None


# Convenience functions
db = Database()
