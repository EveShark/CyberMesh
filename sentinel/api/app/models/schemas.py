"""Pydantic models for request/response schemas."""

from typing import Optional, List, Dict, Any
from datetime import datetime
from pydantic import BaseModel, Field
from enum import Enum


class Verdict(str, Enum):
    """Scan verdict enum."""
    CLEAN = "clean"
    SUSPICIOUS = "suspicious"
    MALICIOUS = "malicious"


class ThreatLevel(str, Enum):
    """Threat level enum."""
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


class DetectionType(str, Enum):
    """Detection source type."""
    YARA = "yara"
    HASH = "hash"
    ML = "ml"
    STRINGS = "strings"
    SIGNATURE = "signature"


class Severity(str, Enum):
    """Detection severity."""
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"
    CRITICAL = "critical"


# Request Models

class ScanRequest(BaseModel):
    """Request model for hash-only scan."""
    hash: str = Field(..., description="SHA256 hash to lookup")


class ScanOptions(BaseModel):
    """Optional scan parameters."""
    deep_scan: bool = False
    extract_iocs: bool = True
    timeout: Optional[int] = None


# Response Models

class FileInfo(BaseModel):
    """File information in scan response."""
    name: str
    hash: str
    size: int
    size_human: str
    type: str
    mime_type: Optional[str] = None


class Detection(BaseModel):
    """Individual detection result."""
    type: DetectionType
    name: str
    severity: Severity
    description: Optional[str] = None
    metadata: Optional[Dict[str, Any]] = None


class DetectionSummary(BaseModel):
    """Summary of all detections."""
    total: int
    critical: int = 0
    high: int = 0
    medium: int = 0
    low: int = 0
    items: List[Detection]


class IOCs(BaseModel):
    """Indicators of Compromise."""
    total: int = 0
    domains: List[str] = []
    ips: List[str] = []
    urls: List[str] = []
    emails: List[str] = []
    hashes: List[str] = []


class ChartDataset(BaseModel):
    """Chart data for frontend visualization."""
    labels: List[str]
    values: List[int]
    colors: Optional[List[str]] = None


class ChartData(BaseModel):
    """All chart data for response."""
    severity_breakdown: Optional[ChartDataset] = None
    detection_sources: Optional[ChartDataset] = None
    risk_gauge: Optional[Dict[str, Any]] = None


class ScanResult(BaseModel):
    """Complete scan result data."""
    scan_id: str
    verdict: Verdict
    confidence: float = Field(..., ge=0.0, le=1.0)
    threat_level: ThreatLevel
    risk_score: int = Field(..., ge=0, le=100)
    processing_time_ms: int
    file: FileInfo
    detections: DetectionSummary
    iocs: Optional[IOCs] = None
    charts: Optional[ChartData] = None


class ResponseMeta(BaseModel):
    """Metadata included in all responses."""
    request_id: str
    processing_time_ms: Optional[int] = None
    timestamp: datetime
    api_version: str = "1.0.0"


class ScanResponse(BaseModel):
    """Standard scan response wrapper."""
    success: bool = True
    data: ScanResult
    meta: ResponseMeta


class AsyncScanResponse(BaseModel):
    """Response for async scan submission."""
    success: bool = True
    data: Dict[str, Any]
    meta: ResponseMeta


class HashLookupResponse(BaseModel):
    """Response for hash lookup."""
    success: bool = True
    data: Dict[str, Any]
    meta: ResponseMeta


# History Models

class ScanHistoryItem(BaseModel):
    """Single item in scan history."""
    scan_id: str
    file_name: str
    file_hash: str
    file_size: int
    verdict: Verdict
    threat_level: ThreatLevel
    created_at: datetime


class HistoryData(BaseModel):
    """Paginated history data."""
    items: List[ScanHistoryItem]
    total: int
    page: int
    limit: int
    has_more: bool


class HistoryResponse(BaseModel):
    """History endpoint response."""
    success: bool = True
    data: HistoryData
    meta: ResponseMeta


# Health Models

class HealthStatus(BaseModel):
    """Health check status."""
    status: str  # healthy, degraded, unhealthy
    version: str
    uptime_seconds: Optional[int] = None


class DeepHealthStatus(BaseModel):
    """Deep health check with component status."""
    status: str
    version: str
    components: Dict[str, Dict[str, Any]]


class HealthResponse(BaseModel):
    """Health endpoint response."""
    success: bool = True
    data: HealthStatus
    meta: ResponseMeta


# Generic Response Models

class APIResponse(BaseModel):
    """Generic API response wrapper."""
    success: bool = True
    data: Optional[Any] = None
    meta: ResponseMeta


class ErrorDetail(BaseModel):
    """Individual error detail."""
    field: Optional[str] = None
    message: str


class ErrorResponse(BaseModel):
    """RFC 9457 compliant error response."""
    type: str
    title: str
    status: int
    detail: str
    instance: str
    trace_id: str
    errors: Optional[List[ErrorDetail]] = None
