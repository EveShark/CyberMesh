"""AlienVault OTX threat intelligence provider."""

from typing import Set, Optional

import httpx

from .base_intel import BaseThreatIntelProvider, IntelResult
from ...parsers.base import FileType
from ...logging import get_logger

logger = get_logger(__name__)


class OTXProvider(BaseThreatIntelProvider):
    """
    AlienVault OTX (Open Threat Exchange) provider.
    
    Provides threat intelligence from the world's largest
    open threat intelligence community.
    """
    
    API_BASE = "https://otx.alienvault.com/api/v1"
    
    def __init__(self, api_key: str, timeout: int = 10):
        super().__init__(api_key=api_key, timeout=timeout)
    
    @property
    def name(self) -> str:
        return "otx"
    
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
        """Check IP against OTX."""
        cache_key = f"ip:{ip}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        try:
            headers = {"X-OTX-API-KEY": self.api_key}
            
            with httpx.Client(timeout=self.timeout) as client:
                response = client.get(
                    f"{self.API_BASE}/indicators/IPv4/{ip}/general",
                    headers=headers,
                )
                response.raise_for_status()
                data = response.json()
            
            pulse_count = data.get("pulse_info", {}).get("count", 0)
            
            # More pulses = more concerning
            is_malicious = pulse_count >= 3
            risk_score = min(pulse_count * 15, 100)
            
            # Get reputation data
            reputation = data.get("reputation", 0)
            if reputation and reputation < 0:
                is_malicious = True
                risk_score = max(risk_score, abs(reputation) * 10)
            
            result = IntelResult(
                source="otx",
                query=ip,
                query_type="ip",
                is_malicious=is_malicious,
                confidence=min(pulse_count / 10.0, 0.9) if pulse_count else 0.3,
                risk_score=float(risk_score),
                categories=[p.get("name", "") for p in data.get("pulse_info", {}).get("pulses", [])[:5]],
                tags=data.get("pulse_info", {}).get("related", {}).get("other", [])[:5],
                raw_data=data,
            )
            
            self._cache[cache_key] = result
            return result
            
        except httpx.HTTPStatusError as e:
            logger.warning(f"OTX API error for {ip}: {e.response.status_code}")
            return None
        except Exception as e:
            logger.error(f"OTX lookup failed for {ip}: {e}")
            return None
    
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """Check domain against OTX."""
        cache_key = f"domain:{domain}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        try:
            headers = {"X-OTX-API-KEY": self.api_key}
            
            with httpx.Client(timeout=self.timeout) as client:
                response = client.get(
                    f"{self.API_BASE}/indicators/domain/{domain}/general",
                    headers=headers,
                )
                response.raise_for_status()
                data = response.json()
            
            pulse_count = data.get("pulse_info", {}).get("count", 0)
            is_malicious = pulse_count >= 3
            risk_score = min(pulse_count * 15, 100)
            
            result = IntelResult(
                source="otx",
                query=domain,
                query_type="domain",
                is_malicious=is_malicious,
                confidence=min(pulse_count / 10.0, 0.9) if pulse_count else 0.3,
                risk_score=float(risk_score),
                categories=[p.get("name", "") for p in data.get("pulse_info", {}).get("pulses", [])[:5]],
                raw_data=data,
            )
            
            self._cache[cache_key] = result
            return result
            
        except httpx.HTTPStatusError as e:
            logger.warning(f"OTX API error for {domain}: {e.response.status_code}")
            return None
        except Exception as e:
            logger.error(f"OTX lookup failed for {domain}: {e}")
            return None
    
    def lookup_hash(self, file_hash: str) -> Optional[IntelResult]:
        """Check file hash against OTX."""
        cache_key = f"hash:{file_hash}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        try:
            headers = {"X-OTX-API-KEY": self.api_key}
            
            with httpx.Client(timeout=self.timeout) as client:
                response = client.get(
                    f"{self.API_BASE}/indicators/file/{file_hash}/general",
                    headers=headers,
                )
                response.raise_for_status()
                data = response.json()
            
            pulse_count = data.get("pulse_info", {}).get("count", 0)
            is_malicious = pulse_count >= 1  # Any pulse for a hash is concerning
            
            result = IntelResult(
                source="otx",
                query=file_hash,
                query_type="hash",
                is_malicious=is_malicious,
                confidence=0.9 if pulse_count else 0.3,
                risk_score=100.0 if pulse_count else 0.0,
                categories=[p.get("name", "") for p in data.get("pulse_info", {}).get("pulses", [])[:5]],
                raw_data=data,
            )
            
            self._cache[cache_key] = result
            return result
            
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return None  # Hash not found
            logger.warning(f"OTX API error for hash: {e.response.status_code}")
            return None
        except Exception as e:
            logger.error(f"OTX hash lookup failed: {e}")
            return None
