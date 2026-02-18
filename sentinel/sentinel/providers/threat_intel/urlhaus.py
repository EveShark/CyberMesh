"""
URLhaus threat intelligence provider.

API Documentation: https://urlhaus-api.abuse.ch/

No API key required - public API.
Tracks malicious URLs used for malware distribution.
"""

import os
import time
from dataclasses import dataclass, field
from typing import Dict, List, Optional, Set

import httpx

from .base_intel import BaseThreatIntelProvider, IntelResult
from ..base import ThreatLevel
from ...parsers.base import FileType
from ...utils.rate_limiter import TokenBucketRateLimiter, RateLimitConfig
from ...logging import get_logger

logger = get_logger(__name__)


API_BASE = "https://urlhaus-api.abuse.ch/v1"
DEFAULT_TIMEOUT = 15


@dataclass
class MaliciousURL:
    """Represents a malicious URL from URLhaus."""
    url: str
    url_id: Optional[int] = None
    url_status: Optional[str] = None  # online, offline
    host: Optional[str] = None
    date_added: Optional[str] = None
    threat: Optional[str] = None  # malware_download, phishing
    tags: List[str] = field(default_factory=list)
    payloads: List[Dict] = field(default_factory=list)
    reporter: Optional[str] = None
    larted: bool = False  # Reported to hosting provider
    takedown_time_seconds: Optional[int] = None


@dataclass 
class URLHostInfo:
    """Information about a host serving malicious URLs."""
    host: str
    firstseen: Optional[str] = None
    url_count: int = 0
    blacklists: Dict = field(default_factory=dict)
    urls: List[MaliciousURL] = field(default_factory=list)


class URLhausProvider(BaseThreatIntelProvider):
    """
    URLhaus threat intelligence provider.
    
    Features:
    - URL lookup (is this URL malicious?)
    - Host lookup (is this domain/IP serving malware?)
    - Payload lookup (where is this hash being distributed?)
    - Recent malicious URLs feed
    
    API key required (Auth-Key header).
    Get key from: https://auth.abuse.ch/
    """
    
    def __init__(self, api_key: Optional[str] = None, timeout: int = DEFAULT_TIMEOUT):
        """
        Initialize URLhaus provider.
        
        Args:
            api_key: Auth key from abuse.ch (or from URLHAUS_API_KEY env)
            timeout: Request timeout
        """
        # Get API key from environment if not provided
        if api_key is None:
            api_key = os.environ.get("URLHAUS_API_KEY", "").strip()
        
        super().__init__(api_key=api_key or None, timeout=timeout)
        
        # Rate limiting
        rpm = int(os.environ.get("URLHAUS_RATE_LIMIT_RPM", 100))
        rpd = int(os.environ.get("URLHAUS_RATE_LIMIT_RPD", 10000))
        self._rate_limiter = TokenBucketRateLimiter(RateLimitConfig(
            requests_per_minute=rpm,
            requests_per_day=rpd,
            tokens_per_minute=100000,
            tokens_per_day=10000000,
        ))
        
        # Stats
        self._stats = {
            "url_lookups": 0,
            "host_lookups": 0,
            "payload_lookups": 0,
            "hits": 0,
            "misses": 0,
            "errors": 0,
        }
    
    @property
    def name(self) -> str:
        return "urlhaus"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.PDF, FileType.OFFICE, FileType.SCRIPT}
    
    def _make_request(self, endpoint: str, data: Dict) -> Optional[Dict]:
        """Make a POST request to URLhaus API."""
        self._rate_limiter.check_limit(estimated_tokens=1)
        
        url = f"{API_BASE}/{endpoint}/"
        
        try:
            headers = {
                "Accept": "application/json",
                "User-Agent": "Sentinel-Threat-Scanner/1.0",
            }
            # Add Auth-Key if available
            if self.api_key:
                headers["Auth-Key"] = self.api_key
            
            with httpx.Client(timeout=self.timeout, verify=True) as client:
                response = client.post(
                    url,
                    data=data,
                    headers=headers,
                )
                response.raise_for_status()
                
                self._rate_limiter.record_request(tokens_used=1, success=True)
                return response.json()
                
        except httpx.TimeoutException:
            logger.warning(f"URLhaus request timed out: {endpoint}")
            self._rate_limiter.record_request(tokens_used=1, success=False)
            self._stats["errors"] += 1
            return None
        except httpx.HTTPStatusError as e:
            logger.warning(f"URLhaus HTTP error: {e.response.status_code}")
            self._rate_limiter.record_request(tokens_used=1, success=False)
            self._stats["errors"] += 1
            return None
        except Exception as e:
            logger.error(f"URLhaus request failed: {e}")
            self._rate_limiter.record_request(tokens_used=1, success=False)
            self._stats["errors"] += 1
            return None
    
    def lookup_url(self, url: str) -> Optional[IntelResult]:
        """
        Check if a URL is known to be malicious.
        
        Args:
            url: URL to check
            
        Returns:
            IntelResult if URL is malicious, None otherwise
        """
        cache_key = f"url:{url}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        self._stats["url_lookups"] += 1
        
        result = self._make_request("url", {"url": url})
        
        if not result:
            return None
        
        if result.get("query_status") != "ok":
            self._stats["misses"] += 1
            self._cache[cache_key] = None
            return None
        
        self._stats["hits"] += 1
        
        # Parse URL data
        mal_url = MaliciousURL(
            url=url,
            url_id=result.get("id"),
            url_status=result.get("url_status"),
            host=result.get("host"),
            date_added=result.get("date_added"),
            threat=result.get("threat"),
            tags=result.get("tags", []) or [],
            payloads=result.get("payloads", []) or [],
            reporter=result.get("reporter"),
            larted=result.get("larted") == "true",
            takedown_time_seconds=result.get("takedown_time_seconds"),
        )
        
        intel = self._to_intel_result(mal_url)
        self._cache[cache_key] = intel
        
        logger.info(f"URLhaus hit: {url[:50]}... -> {mal_url.threat or 'malicious'}")
        return intel
    
    def lookup_host(self, host: str) -> Optional[URLHostInfo]:
        """
        Check if a host (domain/IP) is serving malicious content.
        
        Args:
            host: Domain or IP address
            
        Returns:
            URLHostInfo if host is known, None otherwise
        """
        cache_key = f"host:{host}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        self._stats["host_lookups"] += 1
        
        result = self._make_request("host", {"host": host})
        
        if not result:
            return None
        
        if result.get("query_status") != "ok":
            self._stats["misses"] += 1
            self._cache[cache_key] = None
            return None
        
        self._stats["hits"] += 1
        
        # Parse URLs
        urls = []
        for url_data in result.get("urls", [])[:20]:  # Limit to 20
            urls.append(MaliciousURL(
                url=url_data.get("url", ""),
                url_id=url_data.get("id"),
                url_status=url_data.get("url_status"),
                date_added=url_data.get("date_added"),
                threat=url_data.get("threat"),
                tags=url_data.get("tags", []) or [],
                reporter=url_data.get("reporter"),
                larted=url_data.get("larted") == "true",
            ))
        
        host_info = URLHostInfo(
            host=host,
            firstseen=result.get("firstseen"),
            url_count=result.get("url_count", len(urls)),
            blacklists=result.get("blacklists", {}),
            urls=urls,
        )
        
        self._cache[cache_key] = host_info
        
        logger.info(f"URLhaus host hit: {host} -> {host_info.url_count} malicious URLs")
        return host_info
    
    def lookup_payload(self, sha256_hash: str) -> Optional[Dict]:
        """
        Look up URLs distributing a specific payload hash.
        
        Args:
            sha256_hash: SHA256 hash of payload
            
        Returns:
            Dict with payload info and distribution URLs
        """
        cache_key = f"payload:{sha256_hash}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        self._stats["payload_lookups"] += 1
        
        result = self._make_request("payload", {"sha256_hash": sha256_hash})
        
        if not result:
            return None
        
        if result.get("query_status") != "ok":
            self._stats["misses"] += 1
            self._cache[cache_key] = None
            return None
        
        self._stats["hits"] += 1
        self._cache[cache_key] = result
        
        url_count = result.get("url_count", 0)
        logger.info(f"URLhaus payload hit: {sha256_hash[:16]}... -> {url_count} distribution URLs")
        
        return result
    
    def lookup_ip(self, ip: str) -> Optional[IntelResult]:
        """Look up IP as a host."""
        host_info = self.lookup_host(ip)
        if host_info and host_info.url_count > 0:
            return IntelResult(
                provider=self.name,
                indicator_type="ip",
                indicator_value=ip,
                threat_level=ThreatLevel.MALICIOUS,
                confidence=0.85,
                score=min(0.95, 0.5 + host_info.url_count * 0.05),
                details={
                    "url_count": host_info.url_count,
                    "firstseen": host_info.firstseen,
                    "blacklists": host_info.blacklists,
                },
            )
        return None
    
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """Look up domain as a host."""
        host_info = self.lookup_host(domain)
        if host_info and host_info.url_count > 0:
            return IntelResult(
                provider=self.name,
                indicator_type="domain",
                indicator_value=domain,
                threat_level=ThreatLevel.MALICIOUS,
                confidence=0.85,
                score=min(0.95, 0.5 + host_info.url_count * 0.05),
                details={
                    "url_count": host_info.url_count,
                    "firstseen": host_info.firstseen,
                    "blacklists": host_info.blacklists,
                },
            )
        return None
    
    def get_recent_urls(self, limit: int = 100) -> List[MaliciousURL]:
        """
        Get recently added malicious URLs.
        
        Args:
            limit: Maximum number of URLs to return
            
        Returns:
            List of recent malicious URLs
        """
        result = self._make_request("urls/recent", {"limit": str(limit)})
        
        if not result or result.get("query_status") != "ok":
            return []
        
        urls = []
        for url_data in result.get("urls", []):
            urls.append(MaliciousURL(
                url=url_data.get("url", ""),
                url_id=url_data.get("id"),
                url_status=url_data.get("url_status"),
                host=url_data.get("host"),
                date_added=url_data.get("date_added"),
                threat=url_data.get("threat"),
                tags=url_data.get("tags", []) or [],
                reporter=url_data.get("reporter"),
            ))
        
        logger.info(f"Retrieved {len(urls)} recent URLs from URLhaus")
        return urls
    
    def _to_intel_result(self, mal_url: MaliciousURL) -> IntelResult:
        """Convert MaliciousURL to standard IntelResult."""
        # Determine threat level based on status and payloads
        if mal_url.url_status == "online":
            threat_level = ThreatLevel.MALICIOUS
            score = 0.95
        else:
            threat_level = ThreatLevel.SUSPICIOUS
            score = 0.7
        
        return IntelResult(
            provider=self.name,
            indicator_type="url",
            indicator_value=mal_url.url,
            threat_level=threat_level,
            confidence=0.9,
            score=score,
            details={
                "threat": mal_url.threat,
                "status": mal_url.url_status,
                "host": mal_url.host,
                "tags": mal_url.tags,
                "date_added": mal_url.date_added,
                "payloads": len(mal_url.payloads),
            },
        )
    
    def get_stats(self) -> Dict:
        """Get provider statistics."""
        return {
            **self._stats,
            "cache_size": len(self._cache),
            "rate_limit": self._rate_limiter.get_stats(),
        }
