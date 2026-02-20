"""History endpoints - scan history and management."""

from typing import Dict, Any

from fastapi import APIRouter, Depends, Request, Query

from ...core.auth import get_current_user
from ...core.exceptions import ScanNotFoundError
from ...models.database import Database
from ...models.schemas import (
    HistoryResponse,
    HistoryData,
    ScanHistoryItem,
    ResponseMeta,
    Verdict,
    ThreatLevel,
)
from ...utils.helpers import generate_request_id, get_timestamp

router = APIRouter(prefix="/api/v1", tags=["History"])


@router.get("/history", response_model=HistoryResponse)
async def get_scan_history(
    request: Request,
    page: int = Query(1, ge=1, description="Page number"),
    limit: int = Query(20, ge=1, le=100, description="Items per page"),
    user: Dict[str, Any] = Depends(get_current_user),
):
    """
    Get authenticated user's scan history.
    
    - Paginated results
    - Ordered by most recent first
    - Returns summary data (not full scan details)
    """
    
    request_id = generate_request_id()
    
    # Get history from database
    scans, total = await Database.get_user_history(
        user_id=user["id"],
        page=page,
        limit=limit,
    )
    
    # Format items
    items = []
    for scan in scans:
        items.append(ScanHistoryItem(
            scan_id=scan["id"],
            file_name=scan["file_name"],
            file_hash=scan["file_hash"],
            file_size=scan["file_size"],
            verdict=Verdict(scan["verdict"]),
            threat_level=ThreatLevel(scan["threat_level"]),
            created_at=scan["created_at"],
        ))
    
    # Calculate pagination
    has_more = (page * limit) < total
    
    return HistoryResponse(
        success=True,
        data=HistoryData(
            items=items,
            total=total,
            page=page,
            limit=limit,
            has_more=has_more,
        ),
        meta=ResponseMeta(
            request_id=request_id,
            timestamp=get_timestamp(),
        ),
    )


@router.delete("/history/{scan_id}")
async def delete_scan(
    request: Request,
    scan_id: str,
    user: Dict[str, Any] = Depends(get_current_user),
) -> Dict[str, Any]:
    """
    Delete a scan from history (GDPR compliance).
    
    - Requires authentication
    - Users can only delete their own scans
    - Permanent deletion
    """
    
    request_id = generate_request_id()
    
    # Delete from database
    deleted = await Database.delete_scan(scan_id, user["id"])
    
    if not deleted:
        raise ScanNotFoundError(scan_id)
    
    return {
        "success": True,
        "data": {
            "deleted": True,
            "scan_id": scan_id,
            "message": "Scan deleted successfully",
        },
        "meta": {
            "request_id": request_id,
            "timestamp": get_timestamp().isoformat(),
        },
    }


@router.get("/stats")
async def get_user_stats(
    request: Request,
    user: Dict[str, Any] = Depends(get_current_user),
) -> Dict[str, Any]:
    """
    Get user's scan statistics.
    
    - Total scans
    - Breakdown by verdict
    - Recent activity
    """
    
    request_id = generate_request_id()
    
    # Get all user scans for stats
    scans, total = await Database.get_user_history(
        user_id=user["id"],
        page=1,
        limit=1000,  # Get all for stats
    )
    
    # Calculate stats
    verdict_counts = {"clean": 0, "suspicious": 0, "malicious": 0}
    total_size = 0
    
    for scan in scans:
        verdict = scan.get("verdict", "clean")
        verdict_counts[verdict] = verdict_counts.get(verdict, 0) + 1
        total_size += scan.get("file_size", 0)
    
    return {
        "success": True,
        "data": {
            "total_scans": total,
            "verdicts": verdict_counts,
            "total_bytes_scanned": total_size,
            "detection_rate": (
                (verdict_counts["suspicious"] + verdict_counts["malicious"]) / total * 100
                if total > 0 else 0
            ),
        },
        "meta": {
            "request_id": request_id,
            "timestamp": get_timestamp().isoformat(),
        },
    }
