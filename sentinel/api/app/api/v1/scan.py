"""Scan endpoints - core file scanning functionality."""

import time
from typing import Dict, Any, Optional

from fastapi import APIRouter, UploadFile, File, Depends, Request, Query

from ...core.auth import get_current_user, get_optional_user
from ...core.exceptions import ScanNotFoundError, ScanError
from ...core.rate_limit import rate_limiter
from ...services.scanner import SentinelScanner
from ...services.file_handler import file_handler, format_file_size
from ...services.cache import scan_cache
from ...models.database import Database
from ...models.schemas import (
    ScanResponse,
    ScanResult,
    FileInfo,
    ResponseMeta,
    Verdict,
    ThreatLevel,
)
from ...config import get_settings
from ...utils.helpers import generate_request_id, get_timestamp

router = APIRouter(prefix="/api/v1", tags=["Scanning"])


@router.post("/scan", response_model=ScanResponse)
async def scan_file(
    request: Request,
    file: UploadFile = File(..., description="File to scan"),
    user: Optional[Dict[str, Any]] = Depends(get_optional_user),
):
    """
    Upload and scan a file for malware.
    
    - Supports files up to 50MB (configurable)
    - Results are cached by file hash
    - Authenticated users get scan history saved
    
    Returns detailed scan results including:
    - Verdict (clean/suspicious/malicious)
    - Confidence score
    - Detections from YARA, ML, hash lookup
    - Extracted IOCs (domains, IPs, URLs)
    - Chart data for visualization
    """
    
    settings = get_settings()
    request_id = generate_request_id()
    request.state.request_id = request_id
    
    # Rate limiting
    if settings.enable_rate_limit:
        user_key = f"user:{user['id']}" if user else f"ip:{request.client.host}"
        allowed, info = rate_limiter.is_allowed(user_key)
        if not allowed:
            from ...core.exceptions import RateLimitExceededError
            raise RateLimitExceededError(info.get("retry_after", 60))
    
    start_time = time.perf_counter()
    temp_path = None
    
    try:
        # Process upload with validation
        temp_path, file_hash, file_size, file_type = await file_handler.process_upload(file)
        
        # Check cache first
        if settings.enable_cache:
            cached = scan_cache.get(file_hash)
            if cached:
                # Return cached result with updated metadata
                cached_result = ScanResult(
                    scan_id=cached.get("scan_id", request_id[:8]),
                    **{k: v for k, v in cached.items() if k != "scan_id"},
                    file=FileInfo(
                        name=file.filename or "unknown",
                        hash=file_hash,
                        size=file_size,
                        size_human=format_file_size(file_size),
                        type=file_type,
                    ),
                )
                
                return ScanResponse(
                    success=True,
                    data=cached_result,
                    meta=ResponseMeta(
                        request_id=request_id,
                        processing_time_ms=int((time.perf_counter() - start_time) * 1000),
                        timestamp=get_timestamp(),
                    ),
                )
        
        # Run Sentinel scan
        scan_result = await SentinelScanner.scan_file(temp_path)
        
        # Create file info
        file_info = FileInfo(
            name=file.filename or "unknown",
            hash=file_hash,
            size=file_size,
            size_human=format_file_size(file_size),
            type=file_type,
        )
        
        # Calculate total processing time
        total_time = int((time.perf_counter() - start_time) * 1000)
        
        # Build response
        result = ScanResult(
            scan_id=request_id[:8],
            verdict=scan_result["verdict"],
            confidence=scan_result["confidence"],
            threat_level=scan_result["threat_level"],
            risk_score=scan_result["risk_score"],
            processing_time_ms=total_time,
            file=file_info,
            detections=scan_result["detections"],
            iocs=scan_result["iocs"],
            charts=scan_result["charts"],
        )
        
        # Cache result
        if settings.enable_cache:
            cache_data = {
                "scan_id": result.scan_id,
                "verdict": result.verdict,
                "confidence": result.confidence,
                "threat_level": result.threat_level,
                "risk_score": result.risk_score,
                "processing_time_ms": scan_result["processing_time_ms"],
                "detections": result.detections,
                "iocs": result.iocs,
                "charts": result.charts,
            }
            scan_cache.set(file_hash, cache_data)
        
        # Save to database if authenticated
        if user:
            try:
                db_result = await Database.save_scan(
                    user_id=user["id"],
                    file_name=file.filename or "unknown",
                    file_hash=file_hash,
                    file_size=file_size,
                    file_type=file_type,
                    verdict=result.verdict.value,
                    confidence=result.confidence,
                    threat_level=result.threat_level.value,
                    risk_score=result.risk_score,
                    detections=result.detections.model_dump() if result.detections else {},
                    iocs=result.iocs.model_dump() if result.iocs else {},
                    processing_time_ms=total_time,
                )
                if db_result:
                    result.scan_id = db_result.get("id", result.scan_id)
            except Exception as e:
                # Log but don't fail the scan
                print(f"Failed to save scan to database: {e}")
        
        return ScanResponse(
            success=True,
            data=result,
            meta=ResponseMeta(
                request_id=request_id,
                processing_time_ms=total_time,
                timestamp=get_timestamp(),
            ),
        )
        
    except Exception as e:
        if "FileTooLarge" in str(type(e).__name__) or "InvalidFileType" in str(type(e).__name__):
            raise
        raise ScanError(f"Scan failed: {str(e)}")
        
    finally:
        # Always cleanup temp file
        if temp_path:
            file_handler.cleanup(temp_path)


@router.post("/scan/hash")
async def scan_hash(
    request: Request,
    hash: str = Query(..., description="SHA256 hash to lookup", min_length=64, max_length=64),
    user: Optional[Dict[str, Any]] = Depends(get_optional_user),
) -> Dict[str, Any]:
    """
    Lookup a file hash in the malware database.
    
    - No file upload required
    - Instant response for known hashes
    - Returns malware family if known
    """
    
    request_id = generate_request_id()
    start_time = time.perf_counter()
    
    # Normalize hash
    file_hash = hash.lower().strip()
    
    # Lookup in Sentinel
    result = await SentinelScanner.lookup_hash(file_hash)
    
    processing_time = int((time.perf_counter() - start_time) * 1000)
    
    if result and result.get("known"):
        return {
            "success": True,
            "data": {
                "hash": file_hash,
                "known": True,
                "verdict": result.get("verdict", "malicious"),
                "family": result.get("family", "Unknown"),
                "source": result.get("source", "local"),
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
            "known": False,
            "verdict": "unknown",
            "message": "Hash not found in malware database",
        },
        "meta": {
            "request_id": request_id,
            "processing_time_ms": processing_time,
            "timestamp": get_timestamp().isoformat(),
        },
    }


@router.get("/scans/{scan_id}")
async def get_scan(
    request: Request,
    scan_id: str,
    user: Dict[str, Any] = Depends(get_current_user),
) -> Dict[str, Any]:
    """
    Get a specific scan result by ID.
    
    - Requires authentication
    - Users can only access their own scans
    """
    
    request_id = generate_request_id()
    
    # Get from database
    scan = await Database.get_scan(scan_id, user["id"])
    
    if not scan:
        raise ScanNotFoundError(scan_id)
    
    return {
        "success": True,
        "data": {
            "scan_id": scan["id"],
            "file_name": scan["file_name"],
            "file_hash": scan["file_hash"],
            "file_size": scan["file_size"],
            "file_type": scan["file_type"],
            "verdict": scan["verdict"],
            "confidence": scan["confidence"],
            "threat_level": scan["threat_level"],
            "risk_score": scan["risk_score"],
            "detections": scan.get("detections", {}),
            "iocs": scan.get("iocs", {}),
            "processing_time_ms": scan["processing_time_ms"],
            "created_at": scan["created_at"],
        },
        "meta": {
            "request_id": request_id,
            "timestamp": get_timestamp().isoformat(),
        },
    }
