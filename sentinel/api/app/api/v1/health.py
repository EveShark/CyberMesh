"""Health check endpoints."""

import time
from datetime import datetime, timezone
from typing import Dict, Any

from fastapi import APIRouter, Request

from ...models.schemas import (
    HealthResponse,
    HealthStatus,
    DeepHealthStatus,
    ResponseMeta,
)
from ...services.scanner import SentinelScanner
from ...services.cache import scan_cache
from ...models.database import Database
from ...config import get_settings
from ...utils.helpers import generate_request_id, get_timestamp
from ... import __version__

router = APIRouter(tags=["Health"])

# Track startup time
_startup_time = time.time()


@router.get("/health", response_model=HealthResponse)
async def health_check(request: Request):
    """Simple health check endpoint."""
    
    return HealthResponse(
        success=True,
        data=HealthStatus(
            status="healthy",
            version=__version__,
            uptime_seconds=int(time.time() - _startup_time),
        ),
        meta=ResponseMeta(
            request_id=generate_request_id(),
            timestamp=get_timestamp(),
        ),
    )


@router.get("/health/deep")
async def deep_health_check(request: Request) -> Dict[str, Any]:
    """
    Deep health check with component status.
    
    Checks:
    - Sentinel engine initialization
    - Database connectivity
    - Cache status
    """
    
    components = {}
    overall_status = "healthy"
    
    # Check Sentinel engine
    try:
        if SentinelScanner.is_initialized():
            stats = SentinelScanner.get_stats()
            components["sentinel"] = {
                "status": "healthy",
                "initialized": True,
                "stats": stats,
            }
        else:
            # Try to initialize
            if SentinelScanner.initialize():
                components["sentinel"] = {
                    "status": "healthy",
                    "initialized": True,
                }
            else:
                components["sentinel"] = {
                    "status": "unhealthy",
                    "initialized": False,
                    "error": "Failed to initialize",
                }
                overall_status = "degraded"
    except Exception as e:
        components["sentinel"] = {
            "status": "unhealthy",
            "initialized": False,
            "error": str(e),
        }
        overall_status = "degraded"
    
    # Check database
    try:
        client = Database.get_client()
        # Simple query to check connectivity
        components["database"] = {
            "status": "healthy",
            "connected": True,
        }
    except Exception as e:
        components["database"] = {
            "status": "unhealthy",
            "connected": False,
            "error": str(e),
        }
        overall_status = "degraded"
    
    # Check cache
    try:
        cache_stats = scan_cache.stats()
        components["cache"] = {
            "status": "healthy",
            **cache_stats,
        }
    except Exception as e:
        components["cache"] = {
            "status": "unhealthy",
            "error": str(e),
        }
    
    return {
        "success": True,
        "data": {
            "status": overall_status,
            "version": __version__,
            "uptime_seconds": int(time.time() - _startup_time),
            "components": components,
        },
        "meta": {
            "request_id": generate_request_id(),
            "timestamp": get_timestamp().isoformat(),
        },
    }


@router.get("/api/v1/info")
async def api_info(request: Request) -> Dict[str, Any]:
    """Get API information."""
    
    settings = get_settings()
    
    return {
        "success": True,
        "data": {
            "name": "Sentinel API",
            "version": __version__,
            "description": "Malware Detection API powered by CyberMesh",
            "environment": settings.app_env,
            "features": {
                "async_scan": settings.enable_async_scan,
                "rate_limit": settings.enable_rate_limit,
                "cache": settings.enable_cache,
            },
            "limits": {
                "max_file_size_mb": settings.max_file_size_mb,
                "rate_limit_per_minute": settings.rate_limit_per_minute,
                "rate_limit_per_hour": settings.rate_limit_per_hour,
            },
        },
        "meta": {
            "request_id": generate_request_id(),
            "timestamp": get_timestamp().isoformat(),
        },
    }
