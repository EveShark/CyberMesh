"""API v1 router - aggregates all v1 endpoints."""

from fastapi import APIRouter

from .health import router as health_router
from .scan import router as scan_router
from .history import router as history_router
from .threats import router as threats_router

# Create main API router
api_router = APIRouter()

# Include all routers
api_router.include_router(health_router)
api_router.include_router(scan_router)
api_router.include_router(history_router)
api_router.include_router(threats_router)
