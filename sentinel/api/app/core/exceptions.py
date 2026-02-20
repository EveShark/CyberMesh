"""RFC 9457 compliant error handling."""

from typing import Optional, List, Dict, Any
from fastapi import Request
from fastapi.responses import JSONResponse
import uuid


class SentinelAPIException(Exception):
    """Base exception for Sentinel API with RFC 9457 Problem Details."""
    
    def __init__(
        self,
        status_code: int,
        error_type: str,
        title: str,
        detail: str,
        errors: Optional[List[Dict[str, str]]] = None,
    ):
        self.status_code = status_code
        self.error_type = error_type
        self.title = title
        self.detail = detail
        self.errors = errors or []
        super().__init__(detail)
    
    def to_response(self, request: Request, trace_id: Optional[str] = None) -> Dict[str, Any]:
        """Convert to RFC 9457 Problem Details format."""
        return {
            "type": f"https://api.sentinel.cybermesh.ai/errors/{self.error_type}",
            "title": self.title,
            "status": self.status_code,
            "detail": self.detail,
            "instance": str(request.url.path),
            "trace_id": trace_id or str(uuid.uuid4())[:8],
            "errors": self.errors if self.errors else None,
        }


class FileTooLargeError(SentinelAPIException):
    """File exceeds maximum allowed size."""
    
    def __init__(self, file_size: int, max_size: int):
        super().__init__(
            status_code=413,
            error_type="file-too-large",
            title="File Too Large",
            detail=f"File size {file_size / 1024 / 1024:.1f}MB exceeds maximum {max_size / 1024 / 1024:.0f}MB",
            errors=[{"field": "file", "message": f"Maximum file size is {max_size / 1024 / 1024:.0f}MB"}],
        )


class InvalidFileTypeError(SentinelAPIException):
    """File type is not supported."""
    
    def __init__(self, detected_type: str):
        super().__init__(
            status_code=415,
            error_type="invalid-file-type",
            title="Unsupported File Type",
            detail=f"File type '{detected_type}' is not supported for scanning",
            errors=[{"field": "file", "message": f"Unsupported file type: {detected_type}"}],
        )


class ScanNotFoundError(SentinelAPIException):
    """Scan result not found."""
    
    def __init__(self, scan_id: str):
        super().__init__(
            status_code=404,
            error_type="scan-not-found",
            title="Scan Not Found",
            detail=f"Scan with ID '{scan_id}' was not found",
        )


class RateLimitExceededError(SentinelAPIException):
    """Rate limit exceeded."""
    
    def __init__(self, retry_after: int = 60):
        super().__init__(
            status_code=429,
            error_type="rate-limit-exceeded",
            title="Rate Limit Exceeded",
            detail=f"Too many requests. Please retry after {retry_after} seconds",
        )
        self.retry_after = retry_after


class AuthenticationError(SentinelAPIException):
    """Authentication failed."""
    
    def __init__(self, detail: str = "Invalid or expired token"):
        super().__init__(
            status_code=401,
            error_type="authentication-failed",
            title="Authentication Failed",
            detail=detail,
        )


class ValidationError(SentinelAPIException):
    """Request validation failed."""
    
    def __init__(self, errors: List[Dict[str, str]]):
        super().__init__(
            status_code=422,
            error_type="validation-error",
            title="Validation Error",
            detail="Request validation failed",
            errors=errors,
        )


class ScanError(SentinelAPIException):
    """Scan processing failed."""
    
    def __init__(self, detail: str = "An error occurred during file scanning"):
        super().__init__(
            status_code=500,
            error_type="scan-error",
            title="Scan Error",
            detail=detail,
        )


async def sentinel_exception_handler(request: Request, exc: SentinelAPIException) -> JSONResponse:
    """Global exception handler for SentinelAPIException."""
    trace_id = getattr(request.state, "request_id", None)
    response_data = exc.to_response(request, trace_id)
    
    # Remove None values
    response_data = {k: v for k, v in response_data.items() if v is not None}
    
    headers = {}
    if isinstance(exc, RateLimitExceededError):
        headers["Retry-After"] = str(exc.retry_after)
    
    return JSONResponse(
        status_code=exc.status_code,
        content=response_data,
        headers=headers,
    )
