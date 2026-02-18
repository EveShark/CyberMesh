"""AbuseIPDB threat intelligence provider."""

from typing import Set, Optional

import httpx

from .base_intel import BaseThreatIntelProvider, IntelResult
from ..base import ThreatLevel
from ...parsers.base import FileType
from ...logging import get_logger

logger = get_logger(__name__)


class AbuseIPDBProvider(BaseThreatIntelProvider):
    """
    AbuseIPDB IP reputation provider.
    
    Checks IP addresses against the AbuseIPDB database
    which tracks malicious IPs reported by the community.
    """
    
    API_BASE = "https://api.abuseipdb.com/api/v2"
    
    def __init__(self, api_key: str, timeout: int = 10):
        super().__init__(api_key=api_key, timeout=timeout)
    
    @property
    def name(self) -> str:
        return "abuseipdb"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {
            FileType.PE, FileType.PDF, FileType.OFFICE, 
            FileType.SCRIPT, FileType.PCAP
        }
    
    def lookup_ip(self, ip: str) -> Optional[IntelResult]:
        """Check IP against AbuseIPDB."""
        cache_key = f"ip:{ip}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        try:
            headers = {
                "Key": self.api_key,
                "Accept": "application/json",
            }
            
            params = {
                "ipAddress": ip,
                "maxAgeInDays": 90,
            }
            
            with httpx.Client(timeout=self.timeout) as client:
                response = client.get(
                    f"{self.API_BASE}/check",
                    headers=headers,
                    params=params,
                )
                response.raise_for_status()
                data = response.json().get("data", {})
            
            abuse_score = data.get("abuseConfidenceScore", 0)
            is_malicious = abuse_score >= 50
            
            categories = []
            for cat_id in data.get("reports", [])[:5]:
                if isinstance(cat_id, dict):
                    categories.append(cat_id.get("categories", []))
            
            result = IntelResult(
                source="abuseipdb",
                query=ip,
                query_type="ip",
                is_malicious=is_malicious,
                confidence=abuse_score / 100.0,
                risk_score=float(abuse_score),
                categories=categories[:5] if categories else [],
                tags=[data.get("usageType", ""), data.get("isp", "")],
                last_seen=data.get("lastReportedAt"),
                raw_data=data,
            )
            
            self._cache[cache_key] = result
            return result
            
        except httpx.HTTPStatusError as e:
            logger.warning(f"AbuseIPDB API error for {ip}: {e.response.status_code}")
            return None
        except Exception as e:
            logger.error(f"AbuseIPDB lookup failed for {ip}: {e}")
            return None
    
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """AbuseIPDB doesn't support domain lookups directly."""
        # Could resolve domain to IP and check that
        return None
