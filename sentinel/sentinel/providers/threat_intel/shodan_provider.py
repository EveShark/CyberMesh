"""Shodan threat intelligence provider."""

from typing import Set, Optional

import httpx

from .base_intel import BaseThreatIntelProvider, IntelResult
from ...parsers.base import FileType
from ...logging import get_logger

logger = get_logger(__name__)


class ShodanProvider(BaseThreatIntelProvider):
    """
    Shodan network intelligence provider.
    
    Provides information about devices/services running
    on IP addresses, useful for identifying C2 infrastructure.
    """
    
    API_BASE = "https://api.shodan.io"
    
    def __init__(self, api_key: str, timeout: int = 10):
        super().__init__(api_key=api_key, timeout=timeout)
    
    @property
    def name(self) -> str:
        return "shodan"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.SCRIPT, FileType.PCAP}
    
    def lookup_ip(self, ip: str) -> Optional[IntelResult]:
        """Check IP against Shodan."""
        cache_key = f"ip:{ip}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        try:
            params = {"key": self.api_key}
            
            with httpx.Client(timeout=self.timeout) as client:
                response = client.get(
                    f"{self.API_BASE}/shodan/host/{ip}",
                    params=params,
                )
                response.raise_for_status()
                data = response.json()
            
            # Analyze services for suspicious patterns
            services = data.get("data", [])
            ports = data.get("ports", [])
            
            # Known malicious patterns
            suspicious_ports = {4444, 5555, 31337, 1337, 6667, 6666}  # Common backdoor ports
            suspicious_products = {"metasploit", "cobalt", "covenant", "empire"}
            
            risk_score = 0.0
            is_malicious = False
            tags = []
            
            # Check for suspicious ports
            found_suspicious_ports = suspicious_ports.intersection(set(ports))
            if found_suspicious_ports:
                risk_score += 30 * len(found_suspicious_ports)
                tags.extend([f"port:{p}" for p in found_suspicious_ports])
            
            # Check for suspicious products
            for service in services:
                product = service.get("product", "").lower()
                for sus in suspicious_products:
                    if sus in product:
                        risk_score += 50
                        tags.append(f"product:{sus}")
                        is_malicious = True
            
            # Check vulnerabilities
            vulns = data.get("vulns", [])
            if vulns:
                risk_score += min(len(vulns) * 10, 30)
                tags.append(f"vulns:{len(vulns)}")
            
            risk_score = min(risk_score, 100)
            is_malicious = is_malicious or risk_score >= 50
            
            result = IntelResult(
                source="shodan",
                query=ip,
                query_type="ip",
                is_malicious=is_malicious,
                confidence=0.7 if services else 0.4,
                risk_score=risk_score,
                categories=[s.get("_shodan", {}).get("module", "") for s in services[:5]],
                tags=tags[:10],
                raw_data={
                    "ports": ports,
                    "hostnames": data.get("hostnames", []),
                    "org": data.get("org", ""),
                    "isp": data.get("isp", ""),
                    "country": data.get("country_code", ""),
                },
            )
            
            self._cache[cache_key] = result
            return result
            
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 404:
                return None  # IP not in Shodan
            logger.warning(f"Shodan API error for {ip}: {e.response.status_code}")
            return None
        except Exception as e:
            logger.error(f"Shodan lookup failed for {ip}: {e}")
            return None
    
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """Shodan doesn't have direct domain lookup, resolve to IP first."""
        # Could implement DNS resolution here
        return None
