"""Threat intelligence endpoints."""

import time
from typing import Dict, Any, Optional

from fastapi import APIRouter, Request, Query, Depends

from ...core.auth import get_optional_user
from ...services.scanner import SentinelScanner
from ...utils.helpers import generate_request_id, get_timestamp

router = APIRouter(prefix="/api/v1/threats", tags=["Threat Intelligence"])


@router.get("/hash/{file_hash}")
async def lookup_hash(
    request: Request,
    file_hash: str,
    user: Optional[Dict[str, Any]] = Depends(get_optional_user),
) -> Dict[str, Any]:
    """
    Lookup a file hash in the threat intelligence database.
    
    - Supports SHA256 hashes
    - Returns malware family if known
    - No authentication required
    """
    
    request_id = generate_request_id()
    start_time = time.perf_counter()
    
    # Normalize hash
    file_hash = file_hash.lower().strip()
    
    # Validate hash format
    if len(file_hash) != 64 or not all(c in '0123456789abcdef' for c in file_hash):
        return {
            "success": False,
            "error": {
                "type": "validation_error",
                "message": "Invalid SHA256 hash format. Must be 64 hexadecimal characters.",
            },
            "meta": {
                "request_id": request_id,
                "timestamp": get_timestamp().isoformat(),
            },
        }
    
    # Lookup in Sentinel
    result = await SentinelScanner.lookup_hash(file_hash)
    
    processing_time = int((time.perf_counter() - start_time) * 1000)
    
    if result and result.get("known"):
        return {
            "success": True,
            "data": {
                "hash": file_hash,
                "found": True,
                "verdict": "malicious",
                "family": result.get("family", "Unknown"),
                "source": result.get("source", "local"),
                "threat_level": "high",
            },
            "meta": {
                "request_id": request_id,
                "processing_time_ms": processing_time,
                "timestamp": get_timestamp().isoformat(),
            },
        }
    
    return {
        "success": True,
        "data": {
            "hash": file_hash,
            "found": False,
            "message": "Hash not found in threat database",
        },
        "meta": {
            "request_id": request_id,
            "processing_time_ms": processing_time,
            "timestamp": get_timestamp().isoformat(),
        },
    }


@router.get("/stats")
async def threat_stats(
    request: Request,
) -> Dict[str, Any]:
    """
    Get threat intelligence database statistics.
    
    - Number of known malware hashes
    - YARA rule count
    - Database last updated
    """
    
    request_id = generate_request_id()
    
    # Get stats from Sentinel
    stats = SentinelScanner.get_stats()
    
    fast_path = stats.get("fast_path", {})
    
    return {
        "success": True,
        "data": {
            "malware_hashes": fast_path.get("hash_db_stats", {}).get("malware_hashes", 0),
            "yara_rules": fast_path.get("yara_rules", 0),
            "signatures": fast_path.get("signature_count", 0),
            "engine_initialized": stats.get("initialized", False),
        },
        "meta": {
            "request_id": request_id,
            "timestamp": get_timestamp().isoformat(),
        },
    }
