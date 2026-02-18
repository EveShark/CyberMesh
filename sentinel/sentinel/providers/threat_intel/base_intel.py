"""Base class for Threat Intelligence providers."""

import time
from abc import abstractmethod
from typing import List, Dict, Any, Optional
from dataclasses import dataclass, field

from ..base import Provider, AnalysisResult, ThreatLevel, Indicator
from ...parsers.base import ParsedFile
from ...logging import get_logger

logger = get_logger(__name__)


@dataclass
class IntelResult:
    """Result from threat intelligence lookup."""
    source: str
    query: str
    query_type: str  # ip, domain, url, hash
    
    is_malicious: bool = False
    confidence: float = 0.0
    risk_score: float = 0.0
    
    categories: List[str] = field(default_factory=list)
    tags: List[str] = field(default_factory=list)
    
    first_seen: Optional[str] = None
    last_seen: Optional[str] = None
    
    raw_data: Dict[str, Any] = field(default_factory=dict)


class BaseThreatIntelProvider(Provider):
    """
    Base class for threat intelligence providers.
    
    These providers query external APIs to get reputation
    and threat data for IPs, domains, URLs, and hashes
    found in analyzed files.
    """
    
    def __init__(self, api_key: str, timeout: int = 10):
        self.api_key = api_key
        self.timeout = timeout
        self._latency_ema = 500.0  # Higher default for API calls
        self._cache: Dict[str, IntelResult] = {}
    
    def get_cost_per_call(self) -> float:
        return 0.0  # Most have free tiers
    
    def get_avg_latency_ms(self) -> float:
        return self._latency_ema
    
    def analyze(self, parsed_file: ParsedFile) -> AnalysisResult:
        """
        Analyze file by looking up extracted indicators.
        """
        t0 = time.perf_counter()
        
        # Extract indicators from parsed file
        ips = self._extract_ips(parsed_file)
        domains = self._extract_domains(parsed_file)
        urls = parsed_file.urls
        file_hash = parsed_file.hashes.get("sha256", "")
        
        findings = []
        indicators = []
        max_risk = 0.0
        
        # Lookup each indicator type
        for ip in ips[:5]:  # Limit lookups
            result = self.lookup_ip(ip)
            if result and result.is_malicious:
                findings.append(f"{self.name}: IP {ip} flagged as malicious")
                indicators.append(Indicator(
                    type="malicious_ip",
                    value=ip,
                    context=f"Risk score: {result.risk_score:.1f}",
                ))
                max_risk = max(max_risk, result.risk_score)
        
        for domain in domains[:5]:
            result = self.lookup_domain(domain)
            if result and result.is_malicious:
                findings.append(f"{self.name}: Domain {domain} flagged as malicious")
                indicators.append(Indicator(
                    type="malicious_domain",
                    value=domain,
                    context=f"Risk score: {result.risk_score:.1f}",
                ))
                max_risk = max(max_risk, result.risk_score)
        
        # Determine threat level
        if max_risk >= 80:
            threat_level = ThreatLevel.MALICIOUS
        elif max_risk >= 50:
            threat_level = ThreatLevel.SUSPICIOUS
        elif max_risk > 0:
            threat_level = ThreatLevel.SUSPICIOUS
        else:
            threat_level = ThreatLevel.CLEAN
        
        # No indicators found
        if not ips and not domains and not urls:
            threat_level = ThreatLevel.CLEAN
            findings.append(f"{self.name}: No network indicators found")
        
        latency = (time.perf_counter() - t0) * 1000
        self._latency_ema = 0.9 * self._latency_ema + 0.1 * latency
        
        confidence = 0.7 if findings else 0.5
        
        return AnalysisResult(
            provider_name=self.name,
            provider_version=self.version,
            threat_level=threat_level,
            score=max_risk / 100.0,
            confidence=confidence,
            findings=findings,
            indicators=indicators,
            latency_ms=latency,
            metadata={
                "ips_checked": len(ips),
                "domains_checked": len(domains),
                "max_risk_score": max_risk,
            }
        )
    
    def _extract_ips(self, parsed_file: ParsedFile) -> List[str]:
        """Extract IP addresses from parsed file."""
        import re
        ips = []
        
        # From strings
        ip_pattern = r'\b(?:\d{1,3}\.){3}\d{1,3}\b'
        for s in parsed_file.strings:
            found = re.findall(ip_pattern, s)
            ips.extend(found)
        
        # From metadata
        if "ip_addresses" in parsed_file.metadata:
            ips.extend(parsed_file.metadata["ip_addresses"])
        
        # Filter private IPs
        public_ips = []
        for ip in set(ips):
            parts = ip.split(".")
            if len(parts) == 4:
                try:
                    first = int(parts[0])
                    second = int(parts[1])
                    # Skip private ranges
                    if first == 10:
                        continue
                    if first == 172 and 16 <= second <= 31:
                        continue
                    if first == 192 and second == 168:
                        continue
                    if first == 127:
                        continue
                    public_ips.append(ip)
                except ValueError:
                    continue
        
        return public_ips
    
    def _extract_domains(self, parsed_file: ParsedFile) -> List[str]:
        """Extract domains from parsed file URLs."""
        from urllib.parse import urlparse
        
        domains = set()
        for url in parsed_file.urls:
            try:
                parsed = urlparse(url)
                if parsed.netloc:
                    domains.add(parsed.netloc.lower())
            except Exception:
                pass
        
        # Filter out IPs and localhost
        filtered = []
        for d in domains:
            if not d.replace(".", "").isdigit():  # Not an IP
                if "localhost" not in d and "127.0.0.1" not in d:
                    filtered.append(d)
        
        return filtered
    
    @abstractmethod
    def lookup_ip(self, ip: str) -> Optional[IntelResult]:
        """Lookup IP address reputation."""
        pass
    
    @abstractmethod
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """Lookup domain reputation."""
        pass
    
    def lookup_hash(self, file_hash: str) -> Optional[IntelResult]:
        """Lookup file hash (optional, not all APIs support)."""
        return None
