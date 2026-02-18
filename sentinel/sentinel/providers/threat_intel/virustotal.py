"""
VirusTotal threat intelligence provider.

API Documentation: https://developers.virustotal.com/reference

IMPORTANT: Free tier has strict rate limits (4 req/min, 500 req/day).
Use caching aggressively.
"""

import os
import time
import hashlib
import base64
from dataclasses import dataclass, field
from typing import Dict, List, Optional, Set, Any
from pathlib import Path
from functools import lru_cache

import httpx

from .base_intel import BaseThreatIntelProvider, IntelResult
from ..base import ThreatLevel
from ...parsers.base import FileType
from ...utils.rate_limiter import TokenBucketRateLimiter, RateLimitConfig, RateLimitExceeded
from ...utils.secrets import get_api_key, is_api_available, redact_secret
from ...logging import get_logger

logger = get_logger(__name__)


API_BASE = "https://www.virustotal.com/api/v3"
DEFAULT_TIMEOUT = 30


@dataclass
class VTFileReport:
    """VirusTotal file analysis report."""
    sha256: str
    sha1: Optional[str] = None
    md5: Optional[str] = None
    file_name: Optional[str] = None
    file_type: Optional[str] = None
    file_size: Optional[int] = None
    
    # Detection stats
    malicious: int = 0
    suspicious: int = 0
    undetected: int = 0
    harmless: int = 0
    total_engines: int = 0
    
    # Analysis details
    popular_threat_label: Optional[str] = None
    suggested_threat_label: Optional[str] = None
    tags: List[str] = field(default_factory=list)
    
    # Timestamps
    first_submission: Optional[str] = None
    last_analysis: Optional[str] = None
    
    # Raw results
    engine_results: Dict[str, Dict] = field(default_factory=dict)
    
    @property
    def detection_ratio(self) -> str:
        """Return detection ratio like '45/70'."""
        return f"{self.malicious}/{self.total_engines}"
    
    @property
    def detection_rate(self) -> float:
        """Return detection rate as percentage."""
        if self.total_engines == 0:
            return 0.0
        return self.malicious / self.total_engines


@dataclass
class VTURLReport:
    """VirusTotal URL analysis report."""
    url: str
    url_id: Optional[str] = None
    
    malicious: int = 0
    suspicious: int = 0
    undetected: int = 0
    harmless: int = 0
    total_engines: int = 0
    
    categories: Dict[str, str] = field(default_factory=dict)
    last_analysis: Optional[str] = None
    
    @property
    def detection_ratio(self) -> str:
        return f"{self.malicious}/{self.total_engines}"


class VirusTotalProvider(BaseThreatIntelProvider):
    """
    VirusTotal threat intelligence provider.
    
    Features:
    - File hash lookup (SHA256, SHA1, MD5)
    - URL reputation
    - Domain reputation
    - IP reputation
    - File submission (async)
    
    IMPORTANT: Aggressive caching required due to 4 req/min limit.
    """
    
    def __init__(self, api_key: Optional[str] = None, timeout: int = DEFAULT_TIMEOUT):
        """
        Initialize VirusTotal provider.
        
        Args:
            api_key: API key (if not provided, loads from environment)
            timeout: Request timeout in seconds
        """
        # Get API key from environment if not provided
        if api_key is None:
            api_key = get_api_key("virustotal")
        
        if not api_key:
            raise ValueError(
                "VirusTotal API key required. "
                "Set VIRUSTOTAL_API_KEY environment variable."
            )
        
        super().__init__(api_key=api_key, timeout=timeout)
        
        # Strict rate limiting for free tier
        rpm = int(os.environ.get("VT_RATE_LIMIT_RPM", 4))
        rpd = int(os.environ.get("VT_RATE_LIMIT_RPD", 500))
        self._rate_limiter = TokenBucketRateLimiter(RateLimitConfig(
            requests_per_minute=rpm,
            requests_per_day=rpd,
            tokens_per_minute=1000,
            tokens_per_day=50000,
            burst_allowance=1,  # Minimal burst for VT
            cooldown_seconds=60.0,
        ))
        
        # Stats
        self._stats = {
            "hash_lookups": 0,
            "url_lookups": 0,
            "ip_lookups": 0,
            "domain_lookups": 0,
            "hits": 0,
            "misses": 0,
            "rate_limited": 0,
            "errors": 0,
        }
        
        logger.info(f"VirusTotal provider initialized (key: {redact_secret(api_key)})")
    
    @property
    def name(self) -> str:
        return "virustotal"
    
    @property
    def version(self) -> str:
        return "3.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.PDF, FileType.OFFICE, FileType.SCRIPT, FileType.ANDROID}
    
    def _make_request(self, endpoint: str, method: str = "GET", data: Optional[Dict] = None) -> Optional[Dict]:
        """Make a request to VirusTotal API."""
        try:
            self._rate_limiter.check_limit(estimated_tokens=1)
        except RateLimitExceeded as e:
            logger.warning(f"VirusTotal rate limited: {e}")
            self._stats["rate_limited"] += 1
            return None
        
        url = f"{API_BASE}/{endpoint}"
        headers = {
            "x-apikey": self.api_key,
            "Accept": "application/json",
        }
        
        try:
            with httpx.Client(timeout=self.timeout, verify=True) as client:
                if method == "GET":
                    response = client.get(url, headers=headers)
                else:
                    response = client.post(url, headers=headers, json=data)
                
                # Handle rate limiting
                if response.status_code == 429:
                    logger.warning("VirusTotal rate limit hit (429)")
                    self._rate_limiter.trigger_cooldown()
                    self._stats["rate_limited"] += 1
                    return None
                
                # Handle not found (hash not in VT)
                if response.status_code == 404:
                    self._rate_limiter.record_request(tokens_used=1, success=True)
                    return None
                
                response.raise_for_status()
                
                self._rate_limiter.record_request(tokens_used=1, success=True)
                return response.json()
                
        except httpx.TimeoutException:
            logger.warning(f"VirusTotal request timed out: {endpoint}")
            self._rate_limiter.record_request(tokens_used=1, success=False)
            self._stats["errors"] += 1
            return None
        except httpx.HTTPStatusError as e:
            logger.warning(f"VirusTotal HTTP error: {e.response.status_code}")
            self._rate_limiter.record_request(tokens_used=1, success=False)
            self._stats["errors"] += 1
            return None
        except Exception as e:
            logger.error(f"VirusTotal request failed: {e}")
            self._rate_limiter.record_request(tokens_used=1, success=False)
            self._stats["errors"] += 1
            return None
    
    def lookup_hash(self, file_hash: str) -> Optional[VTFileReport]:
        """
        Look up a file hash in VirusTotal.
        
        Args:
            file_hash: SHA256, SHA1, or MD5 hash
            
        Returns:
            VTFileReport if found, None otherwise
        """
        cache_key = f"hash:{file_hash.lower()}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        self._stats["hash_lookups"] += 1
        
        result = self._make_request(f"files/{file_hash}")
        
        if not result:
            self._stats["misses"] += 1
            self._cache[cache_key] = None
            return None
        
        self._stats["hits"] += 1
        
        # Parse response
        attrs = result.get("data", {}).get("attributes", {})
        stats = attrs.get("last_analysis_stats", {})
        
        report = VTFileReport(
            sha256=attrs.get("sha256", file_hash),
            sha1=attrs.get("sha1"),
            md5=attrs.get("md5"),
            file_name=attrs.get("meaningful_name") or attrs.get("names", [None])[0] if attrs.get("names") else None,
            file_type=attrs.get("type_description"),
            file_size=attrs.get("size"),
            malicious=stats.get("malicious", 0),
            suspicious=stats.get("suspicious", 0),
            undetected=stats.get("undetected", 0),
            harmless=stats.get("harmless", 0),
            total_engines=sum(stats.values()) if stats else 0,
            popular_threat_label=attrs.get("popular_threat_classification", {}).get("suggested_threat_label"),
            suggested_threat_label=attrs.get("popular_threat_classification", {}).get("popular_threat_name"),
            tags=attrs.get("tags", []),
            first_submission=attrs.get("first_submission_date"),
            last_analysis=attrs.get("last_analysis_date"),
            engine_results=attrs.get("last_analysis_results", {}),
        )
        
        self._cache[cache_key] = report
        
        logger.info(f"VirusTotal hit: {file_hash[:16]}... -> {report.detection_ratio} detections")
        return report
    
    def lookup_url(self, url: str) -> Optional[IntelResult]:
        """
        Check URL reputation in VirusTotal.
        
        Args:
            url: URL to check
            
        Returns:
            IntelResult if found
        """
        cache_key = f"url:{url}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        self._stats["url_lookups"] += 1
        
        # URL must be base64 encoded (without padding)
        url_id = base64.urlsafe_b64encode(url.encode()).decode().rstrip("=")
        
        result = self._make_request(f"urls/{url_id}")
        
        if not result:
            self._stats["misses"] += 1
            return None
        
        self._stats["hits"] += 1
        
        attrs = result.get("data", {}).get("attributes", {})
        stats = attrs.get("last_analysis_stats", {})
        
        malicious = stats.get("malicious", 0)
        total = sum(stats.values()) if stats else 0
        
        threat_level = ThreatLevel.CLEAN
        score = 0.0
        
        if malicious > 5:
            threat_level = ThreatLevel.MALICIOUS
            score = min(0.95, 0.5 + malicious * 0.05)
        elif malicious > 0:
            threat_level = ThreatLevel.SUSPICIOUS
            score = 0.3 + malicious * 0.1
        
        intel = IntelResult(
            provider=self.name,
            indicator_type="url",
            indicator_value=url,
            threat_level=threat_level,
            confidence=0.9 if total > 50 else 0.7,
            score=score,
            details={
                "detection_ratio": f"{malicious}/{total}",
                "categories": attrs.get("categories", {}),
                "last_analysis": attrs.get("last_analysis_date"),
            },
        )
        
        self._cache[cache_key] = intel
        return intel
    
    def lookup_ip(self, ip: str) -> Optional[IntelResult]:
        """
        Check IP reputation in VirusTotal.
        
        Args:
            ip: IP address to check
            
        Returns:
            IntelResult if found
        """
        cache_key = f"ip:{ip}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        self._stats["ip_lookups"] += 1
        
        result = self._make_request(f"ip_addresses/{ip}")
        
        if not result:
            self._stats["misses"] += 1
            return None
        
        self._stats["hits"] += 1
        
        attrs = result.get("data", {}).get("attributes", {})
        stats = attrs.get("last_analysis_stats", {})
        
        malicious = stats.get("malicious", 0)
        suspicious = stats.get("suspicious", 0)
        total = sum(stats.values()) if stats else 0
        
        threat_level = ThreatLevel.CLEAN
        score = 0.0
        
        if malicious > 3:
            threat_level = ThreatLevel.MALICIOUS
            score = min(0.95, 0.5 + malicious * 0.1)
        elif malicious > 0 or suspicious > 3:
            threat_level = ThreatLevel.SUSPICIOUS
            score = 0.3 + (malicious + suspicious * 0.5) * 0.1
        
        intel = IntelResult(
            provider=self.name,
            indicator_type="ip",
            indicator_value=ip,
            threat_level=threat_level,
            confidence=0.85,
            score=score,
            details={
                "detection_ratio": f"{malicious}/{total}",
                "country": attrs.get("country"),
                "as_owner": attrs.get("as_owner"),
                "reputation": attrs.get("reputation"),
            },
        )
        
        self._cache[cache_key] = intel
        return intel
    
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """
        Check domain reputation in VirusTotal.
        
        Args:
            domain: Domain to check
            
        Returns:
            IntelResult if found
        """
        cache_key = f"domain:{domain}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        self._stats["domain_lookups"] += 1
        
        result = self._make_request(f"domains/{domain}")
        
        if not result:
            self._stats["misses"] += 1
            return None
        
        self._stats["hits"] += 1
        
        attrs = result.get("data", {}).get("attributes", {})
        stats = attrs.get("last_analysis_stats", {})
        
        malicious = stats.get("malicious", 0)
        suspicious = stats.get("suspicious", 0)
        total = sum(stats.values()) if stats else 0
        
        threat_level = ThreatLevel.CLEAN
        score = 0.0
        
        if malicious > 3:
            threat_level = ThreatLevel.MALICIOUS
            score = min(0.95, 0.5 + malicious * 0.1)
        elif malicious > 0 or suspicious > 3:
            threat_level = ThreatLevel.SUSPICIOUS
            score = 0.3 + (malicious + suspicious * 0.5) * 0.1
        
        intel = IntelResult(
            provider=self.name,
            indicator_type="domain",
            indicator_value=domain,
            threat_level=threat_level,
            confidence=0.85,
            score=score,
            details={
                "detection_ratio": f"{malicious}/{total}",
                "categories": attrs.get("categories", {}),
                "registrar": attrs.get("registrar"),
                "creation_date": attrs.get("creation_date"),
                "reputation": attrs.get("reputation"),
            },
        )
        
        self._cache[cache_key] = intel
        return intel
    
    def to_intel_result(self, report: VTFileReport) -> IntelResult:
        """Convert VTFileReport to standard IntelResult."""
        # Determine threat level based on detection ratio
        detection_rate = report.detection_rate
        
        if detection_rate >= 0.5:
            threat_level = ThreatLevel.MALICIOUS
            score = min(0.99, 0.7 + detection_rate * 0.3)
        elif detection_rate >= 0.2:
            threat_level = ThreatLevel.MALICIOUS
            score = 0.5 + detection_rate
        elif detection_rate >= 0.05:
            threat_level = ThreatLevel.SUSPICIOUS
            score = 0.3 + detection_rate
        elif report.malicious > 0:
            threat_level = ThreatLevel.SUSPICIOUS
            score = 0.2 + detection_rate
        else:
            threat_level = ThreatLevel.CLEAN
            score = detection_rate
        
        return IntelResult(
            provider=self.name,
            indicator_type="hash",
            indicator_value=report.sha256,
            threat_level=threat_level,
            confidence=0.9 if report.total_engines > 50 else 0.75,
            score=score,
            details={
                "detection_ratio": report.detection_ratio,
                "malicious_count": report.malicious,
                "total_engines": report.total_engines,
                "threat_label": report.popular_threat_label,
                "file_type": report.file_type,
                "tags": report.tags,
            },
        )
    
    def get_remaining_quota(self) -> Dict:
        """Get remaining API quota."""
        return self._rate_limiter.get_remaining()
    
    def get_stats(self) -> Dict:
        """Get provider statistics."""
        return {
            **self._stats,
            "cache_size": len(self._cache),
            "rate_limit": self._rate_limiter.get_stats(),
        }
