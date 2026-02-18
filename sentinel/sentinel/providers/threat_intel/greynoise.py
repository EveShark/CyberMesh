"""GreyNoise threat intelligence provider."""

from typing import Set, Optional

import httpx

from .base_intel import BaseThreatIntelProvider, IntelResult
from ...parsers.base import FileType
from ...logging import get_logger

logger = get_logger(__name__)


class GreyNoiseProvider(BaseThreatIntelProvider):
    """
    GreyNoise IP classification provider.
    
    Classifies IP addresses as malicious, benign, or noise
    based on observed scanning behavior.
    """
    
    API_BASE = "https://api.greynoise.io/v3"
    
    def __init__(self, api_key: str, timeout: int = 10):
        super().__init__(api_key=api_key, timeout=timeout)
    
    @property
    def name(self) -> str:
        return "greynoise"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.SCRIPT, FileType.PCAP}
    
    def lookup_ip(self, ip: str) -> Optional[IntelResult]:
        """Check IP against GreyNoise."""
        cache_key = f"ip:{ip}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        try:
            headers = {
                "key": self.api_key,
                "Accept": "application/json",
            }
            
            with httpx.Client(timeout=self.timeout) as client:
                response = client.get(
                    f"{self.API_BASE}/community/{ip}",
                    headers=headers,
                )
                response.raise_for_status()
                data = response.json()
            
            classification = data.get("classification", "unknown")
            noise = data.get("noise", False)
            riot = data.get("riot", False)  # Rule It Out - known benign
            
            # Determine risk
            if riot:
                # Known benign (CDN, cloud provider, etc.)
                is_malicious = False
                risk_score = 0.0
            elif classification == "malicious":
                is_malicious = True
                risk_score = 85.0
            elif classification == "benign":
                is_malicious = False
                risk_score = 10.0
            elif noise:
                # Scanner/crawler but not necessarily malicious
                is_malicious = False
                risk_score = 30.0
            else:
                is_malicious = False
                risk_score = 20.0
            
            result = IntelResult(
                source="greynoise",
                query=ip,
                query_type="ip",
                is_malicious=is_malicious,
                confidence=0.8 if classification != "unknown" else 0.4,
                risk_score=risk_score,
                categories=[classification],
                tags=[
                    f"noise:{noise}",
                    f"riot:{riot}",
                    data.get("name", ""),
                ],
                last_seen=data.get("last_seen"),
                raw_data=data,
            )
            
            self._cache[cache_key] = result
            return result
            
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                # IP not seen by GreyNoise
                return IntelResult(
                    source="greynoise",
                    query=ip,
                    query_type="ip",
                    is_malicious=False,
                    confidence=0.3,
                    risk_score=0.0,
                    categories=["not_observed"],
                )
            logger.warning(f"GreyNoise API error for {ip}: {e.response.status_code}")
            return None
        except Exception as e:
            logger.error(f"GreyNoise lookup failed for {ip}: {e}")
            return None
    
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """GreyNoise doesn't support domain lookups."""
        return None
