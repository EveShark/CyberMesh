"""
Sentinel API - Malware Detection Service

FastAPI application entry point with middleware configuration.
"""

import time
import uuid
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from fastapi.exceptions import RequestValidationError

from .config import get_settings
from .api.v1.router import api_router
from .core.exceptions import SentinelAPIException, sentinel_exception_handler
from .services.scanner import SentinelScanner
from . import __version__


# Lifespan context manager for startup/shutdown
@asynccontextmanager
async def lifespan(app: FastAPI):
    """Application lifespan - startup and shutdown events."""
    
    # Startup
    print(f"Starting Sentinel API v{__version__}")
    print("Sentinel engine will be initialized on first scan request (lazy loading)")
    
    yield
    
    # Shutdown
    print("Shutting down Sentinel API")


# Create FastAPI application
settings = get_settings()

app = FastAPI(
    title="Sentinel API",
    description="Malware Detection API powered by CyberMesh Sentinel",
    version=__version__,
    docs_url="/docs" if settings.app_debug else None,
    redoc_url="/redoc" if settings.app_debug else None,
    lifespan=lifespan,
)


# CORS middleware
cors_origins = settings.cors_origins.split(",") if settings.cors_origins != "*" else ["*"]

app.add_middleware(
    CORSMiddleware,
    allow_origins=cors_origins,
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
    expose_headers=["X-Request-ID", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"],
)


# Request ID middleware
@app.middleware("http")
async def add_request_id(request: Request, call_next):
    """Add unique request ID to each request."""
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4()))
    request.state.request_id = request_id
    
    start_time = time.perf_counter()
    
    response = await call_next(request)
    
    # Add response headers
    process_time = time.perf_counter() - start_time
    response.headers["X-Request-ID"] = request_id
    response.headers["X-Process-Time"] = f"{process_time:.3f}s"
    
    return response


# Exception handlers
@app.exception_handler(SentinelAPIException)
async def handle_sentinel_exception(request: Request, exc: SentinelAPIException):
    """Handle Sentinel API exceptions with RFC 9457 format."""
    return await sentinel_exception_handler(request, exc)


@app.exception_handler(RequestValidationError)
async def handle_validation_error(request: Request, exc: RequestValidationError):
    """Handle Pydantic validation errors."""
    errors = []
    for error in exc.errors():
        field = ".".join(str(loc) for loc in error["loc"])
        errors.append({
            "field": field,
            "message": error["msg"],
        })
    
    return JSONResponse(
        status_code=422,
        content={
            "type": "https://api.sentinel.cybermesh.ai/errors/validation-error",
            "title": "Validation Error",
            "status": 422,
            "detail": "Request validation failed",
            "instance": str(request.url.path),
            "trace_id": getattr(request.state, "request_id", "unknown"),
            "errors": errors,
        },
    )


@app.exception_handler(Exception)
async def handle_generic_exception(request: Request, exc: Exception):
    """Handle unexpected exceptions."""
    # Log the error
    print(f"Unexpected error: {exc}")
    
    # Don't expose internal errors in production
    detail = str(exc) if settings.app_debug else "An unexpected error occurred"
    
    return JSONResponse(
        status_code=500,
        content={
            "type": "https://api.sentinel.cybermesh.ai/errors/internal-error",
            "title": "Internal Server Error",
            "status": 500,
            "detail": detail,
            "instance": str(request.url.path),
            "trace_id": getattr(request.state, "request_id", "unknown"),
        },
    )


# Include API router
app.include_router(api_router)


# Root endpoint
@app.get("/", include_in_schema=False)
async def root():
    """Root endpoint - API information."""
    return {
        "name": "Sentinel API",
        "version": __version__,
        "description": "Malware Detection API powered by CyberMesh",
        "docs": "/docs" if settings.app_debug else None,
        "health": "/health",
    }


# Run with: uvicorn app.main:app --reload
if __name__ == "__main__":
    import uvicorn
    
    uvicorn.run(
        "app.main:app",
        host=settings.app_host,
        port=settings.app_port,
        reload=settings.app_debug,
    )
