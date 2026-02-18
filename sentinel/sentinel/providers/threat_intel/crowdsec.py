"""CrowdSec CTI threat intelligence provider."""

from typing import Set, Optional, List

import httpx

from .base_intel import BaseThreatIntelProvider, IntelResult
from ...parsers.base import FileType
from ...logging import get_logger

logger = get_logger(__name__)


class CrowdSecProvider(BaseThreatIntelProvider):
    """
    CrowdSec CTI provider for IP reputation.
    
    Provides:
    - Botnet classification and behaviors
    - Attack scenarios (ssh:bruteforce, http:exploit, etc.)
    - Background noise score
    - Confidence level
    - First/last seen timestamps
    
    API: https://docs.crowdsec.net/u/cti_api/
    """
    
    API_BASE = "https://cti.api.crowdsec.net/v2"
    
    def __init__(self, api_key: str, timeout: int = 10):
        super().__init__(api_key=api_key, timeout=timeout)
    
    @property
    def name(self) -> str:
        return "crowdsec"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.SCRIPT, FileType.PCAP, FileType.PDF, FileType.OFFICE}
    
    def lookup_ip(self, ip: str) -> Optional[IntelResult]:
        """Check IP against CrowdSec CTI."""
        cache_key = f"ip:{ip}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        try:
            headers = {
                "x-api-key": self.api_key,
                "Accept": "application/json",
            }
            
            with httpx.Client(timeout=self.timeout) as client:
                response = client.get(
                    f"{self.API_BASE}/smoke/{ip}",
                    headers=headers,
                )
                
                if response.status_code == 404:
                    # IP not in CrowdSec database
                    result = IntelResult(
                        source="crowdsec",
                        query=ip,
                        query_type="ip",
                        is_malicious=False,
                        confidence=0.3,
                        risk_score=0.0,
                        categories=["not_observed"],
                    )
                    self._cache[cache_key] = result
                    return result
                
                response.raise_for_status()
                data = response.json()
            
            # Parse reputation
            reputation = data.get("reputation", "unknown")
            is_malicious = reputation in ("malicious", "suspicious")
            
            # Parse behaviors (e.g., ["ssh:bruteforce", "http:exploit"])
            behaviors = []
            for behavior in data.get("behaviors", []):
                if isinstance(behavior, dict):
                    behaviors.append(behavior.get("name", ""))
                elif isinstance(behavior, str):
                    behaviors.append(behavior)
            
            # Parse attack details
            attack_details = []
            for attack in data.get("attack_details", []):
                if isinstance(attack, dict):
                    attack_details.append(attack.get("name", ""))
            
            # Background noise score (0-10)
            background_noise = data.get("background_noise_score", 0)
            if isinstance(background_noise, str):
                try:
                    background_noise = float(background_noise)
                except ValueError:
                    background_noise = 0
            
            # Confidence (0-100)
            confidence_raw = data.get("confidence", 50)
            if isinstance(confidence_raw, str):
                try:
                    confidence_raw = float(confidence_raw)
                except ValueError:
                    confidence_raw = 50
            confidence = confidence_raw / 100.0
            
            # Calculate risk score
            if reputation == "malicious":
                risk_score = 80.0 + (background_noise * 2)
            elif reputation == "suspicious":
                risk_score = 50.0 + (background_noise * 3)
            else:
                risk_score = background_noise * 5
            risk_score = min(100.0, risk_score)
            
            # Check for botnet indicators
            is_botnet = any(
                "bot" in b.lower() or "ddos" in b.lower() or "scan" in b.lower()
                for b in behaviors
            )
            
            # Parse location
            location = data.get("location", {})
            country = location.get("country") if isinstance(location, dict) else None
            
            # Parse AS info
            as_info = data.get("as_name", "")
            
            result = IntelResult(
                source="crowdsec",
                query=ip,
                query_type="ip",
                is_malicious=is_malicious,
                confidence=confidence,
                risk_score=risk_score,
                categories=behaviors[:10],
                tags=[
                    f"reputation:{reputation}",
                    f"noise:{background_noise}",
                    f"as:{as_info}" if as_info else "",
                ],
                first_seen=data.get("history", {}).get("first_seen"),
                last_seen=data.get("history", {}).get("last_seen"),
                raw_data={
                    "reputation": reputation,
                    "behaviors": behaviors,
                    "attack_details": attack_details,
                    "background_noise_score": background_noise,
                    "is_botnet": is_botnet,
                    "country": country,
                    "as_name": as_info,
                    "scores": data.get("scores", {}),
                },
            )
            
            self._cache[cache_key] = result
            logger.debug(
                f"CrowdSec: {ip} -> {reputation} "
                f"(score={risk_score:.0f}, behaviors={len(behaviors)})"
            )
            return result
            
        except httpx.HTTPStatusError as e:
            if e.response.status_code == 429:
                logger.warning("CrowdSec rate limit exceeded")
            else:
                logger.warning(f"CrowdSec API error for {ip}: {e.response.status_code}")
            return None
        except Exception as e:
            logger.error(f"CrowdSec lookup failed for {ip}: {e}")
            return None
    
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """CrowdSec doesn't support domain lookups directly."""
        return None
