"""
Unified threat intelligence enrichment pipeline.

Orchestrates all threat intel sources:
- VirusTotal (hash, URL, IP, domain)
- MalwareBazaar (hash)
- URLhaus (URL, host)
- AbuseIPDB (IP)
- OTX (IP, domain, hash)
- GreyNoise (IP)
- Shodan (IP)
- CrowdSec (IP - botnet behaviors, DDoS)
- Feodo Tracker (IP - C2 servers)
- Spamhaus (IP - blocklists)
"""

import re
import time
from dataclasses import dataclass, field
from enum import Enum
from typing import Dict, List, Optional, Set, Any
from concurrent.futures import ThreadPoolExecutor, as_completed

from ..providers.threat_intel import (
    MalwareBazaarProvider,
    URLhausProvider,
    VirusTotalProvider,
    AbuseIPDBProvider,
    OTXProvider,
    GreyNoiseProvider,
    ShodanProvider,
    CrowdSecProvider,
    FeodoTrackerProvider,
    SpamhausProvider,
)
from ..providers.threat_intel.base_intel import IntelResult
from ..providers.base import ThreatLevel
from ..parsers.base import ParsedFile
from ..utils.secrets import get_api_key, is_api_available
from ..logging import get_logger

logger = get_logger(__name__)


class IOCType(Enum):
    """Types of Indicators of Compromise."""
    HASH_SHA256 = "sha256"
    HASH_SHA1 = "sha1"
    HASH_MD5 = "md5"
    IP = "ip"
    DOMAIN = "domain"
    URL = "url"
    EMAIL = "email"


@dataclass
class IOC:
    """An Indicator of Compromise."""
    type: IOCType
    value: str
    context: Optional[str] = None  # Where it was found
    
    def __hash__(self):
        return hash((self.type, self.value.lower()))
    
    def __eq__(self, other):
        if not isinstance(other, IOC):
            return False
        return self.type == other.type and self.value.lower() == other.value.lower()


@dataclass
class HashIntel:
    """Intelligence about a file hash."""
    sha256: str
    is_known_malware: bool = False
    malware_family: Optional[str] = None
    vt_detections: Optional[str] = None  # "45/70"
    vt_threat_label: Optional[str] = None
    mb_tags: List[str] = field(default_factory=list)
    first_seen: Optional[str] = None
    sources: List[str] = field(default_factory=list)


@dataclass
class IPIntel:
    """Intelligence about an IP address."""
    ip: str
    is_malicious: bool = False
    abuse_score: Optional[int] = None  # AbuseIPDB score
    is_known_attacker: bool = False
    is_noise: bool = False  # GreyNoise
    country: Optional[str] = None
    asn: Optional[str] = None
    tags: List[str] = field(default_factory=list)
    sources: List[str] = field(default_factory=list)
    # Network threat intel fields (CrowdSec, Feodo, Spamhaus)
    is_botnet: bool = False
    botnet_behaviors: List[str] = field(default_factory=list)
    is_c2: bool = False
    c2_malware_family: Optional[str] = None
    in_blocklist: bool = False
    blocklist_sources: List[str] = field(default_factory=list)


@dataclass
class URLIntel:
    """Intelligence about a URL."""
    url: str
    is_malicious: bool = False
    threat_type: Optional[str] = None  # malware_download, phishing
    status: Optional[str] = None  # online, offline
    vt_detections: Optional[str] = None
    sources: List[str] = field(default_factory=list)


@dataclass
class DomainIntel:
    """Intelligence about a domain."""
    domain: str
    is_malicious: bool = False
    categories: Dict[str, str] = field(default_factory=dict)
    vt_detections: Optional[str] = None
    registrar: Optional[str] = None
    creation_date: Optional[str] = None
    sources: List[str] = field(default_factory=list)


@dataclass
class EnrichmentResult:
    """Combined enrichment result from all sources."""
    file_hash_intel: Optional[HashIntel] = None
    ip_intel: List[IPIntel] = field(default_factory=list)
    url_intel: List[URLIntel] = field(default_factory=list)
    domain_intel: List[DomainIntel] = field(default_factory=list)
    
    threat_score: float = 0.0  # Aggregated 0-1
    confidence: float = 0.0
    
    sources_queried: List[str] = field(default_factory=list)
    sources_matched: List[str] = field(default_factory=list)
    
    errors: List[str] = field(default_factory=list)
    latency_ms: float = 0.0
    
    @property
    def has_threat_intel(self) -> bool:
        """Check if any threat intel was found."""
        return bool(
            self.file_hash_intel or 
            self.ip_intel or 
            self.url_intel or 
            self.domain_intel
        )
    
    @property
    def is_known_malicious(self) -> bool:
        """Check if any IOC is known malicious."""
        if self.file_hash_intel and self.file_hash_intel.is_known_malware:
            return True
        if any(ip.is_malicious for ip in self.ip_intel):
            return True
        if any(url.is_malicious for url in self.url_intel):
            return True
        if any(d.is_malicious for d in self.domain_intel):
            return True
        return False
    
    def to_dict(self) -> Dict:
        """Convert to dictionary for serialization."""
        return {
            "file_hash_intel": {
                "sha256": self.file_hash_intel.sha256,
                "is_known_malware": self.file_hash_intel.is_known_malware,
                "malware_family": self.file_hash_intel.malware_family,
                "vt_detections": self.file_hash_intel.vt_detections,
                "sources": self.file_hash_intel.sources,
            } if self.file_hash_intel else None,
            "ip_intel": [
                {"ip": ip.ip, "is_malicious": ip.is_malicious, "abuse_score": ip.abuse_score}
                for ip in self.ip_intel
            ],
            "url_intel": [
                {"url": url.url, "is_malicious": url.is_malicious, "threat_type": url.threat_type}
                for url in self.url_intel
            ],
            "domain_intel": [
                {"domain": d.domain, "is_malicious": d.is_malicious}
                for d in self.domain_intel
            ],
            "threat_score": self.threat_score,
            "confidence": self.confidence,
            "sources_queried": self.sources_queried,
            "sources_matched": self.sources_matched,
            "latency_ms": self.latency_ms,
        }


# Regex patterns for IOC extraction
IOC_PATTERNS = {
    IOCType.HASH_SHA256: re.compile(r'\b[a-fA-F0-9]{64}\b'),
    IOCType.HASH_SHA1: re.compile(r'\b[a-fA-F0-9]{40}\b'),
    IOCType.HASH_MD5: re.compile(r'\b[a-fA-F0-9]{32}\b'),
    IOCType.IP: re.compile(r'\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b'),
    IOCType.DOMAIN: re.compile(r'\b(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}\b'),
    IOCType.URL: re.compile(r'https?://[^\s<>"{}|\\^`\[\]]+'),
    IOCType.EMAIL: re.compile(r'\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b'),
}

# Private IP ranges to exclude
PRIVATE_IP_RANGES = [
    re.compile(r'^10\.'),
    re.compile(r'^172\.(1[6-9]|2[0-9]|3[0-1])\.'),
    re.compile(r'^192\.168\.'),
    re.compile(r'^127\.'),
    re.compile(r'^0\.'),
]


class ThreatEnrichment:
    """
    Unified threat intelligence enrichment engine.
    
    Orchestrates all threat intel sources with:
    - Prioritized querying (most valuable sources first)
    - Caching to avoid redundant API calls
    - Parallel queries where possible
    - Rate limit awareness
    - Result aggregation and scoring
    """
    
    def __init__(
        self,
        enable_vt: bool = True,
        enable_mb: bool = True,
        enable_urlhaus: bool = True,
        enable_abuseipdb: bool = True,
        enable_otx: bool = True,
        enable_greynoise: bool = True,
        enable_shodan: bool = True,
        enable_crowdsec: bool = True,
        enable_feodo: bool = True,
        enable_spamhaus: bool = True,
        max_workers: int = 4,
    ):
        """
        Initialize enrichment engine.
        
        Args:
            enable_*: Enable/disable specific providers
            max_workers: Max parallel threads for queries
        """
        self.max_workers = max_workers
        self._providers: Dict[str, Any] = {}
        self._enabled: Dict[str, bool] = {}
        
        # Initialize providers based on availability
        self._init_providers(
            enable_vt, enable_mb, enable_urlhaus,
            enable_abuseipdb, enable_otx, enable_greynoise, enable_shodan,
            enable_crowdsec, enable_feodo, enable_spamhaus
        )
        
        logger.info(f"ThreatEnrichment initialized with providers: {list(self._providers.keys())}")
    
    def _init_providers(
        self,
        enable_vt: bool,
        enable_mb: bool,
        enable_urlhaus: bool,
        enable_abuseipdb: bool,
        enable_otx: bool,
        enable_greynoise: bool,
        enable_shodan: bool,
        enable_crowdsec: bool,
        enable_feodo: bool,
        enable_spamhaus: bool,
    ) -> None:
        """Initialize available providers."""
        
        # MalwareBazaar
        if enable_mb:
            try:
                mb_key = get_api_key("malwarebazaar")
                self._providers["malware_bazaar"] = MalwareBazaarProvider(api_key=mb_key)
                self._enabled["malware_bazaar"] = True
            except Exception as e:
                logger.warning(f"Could not init MalwareBazaar: {e}")
        
        # URLhaus
        if enable_urlhaus:
            try:
                uh_key = get_api_key("urlhaus")
                self._providers["urlhaus"] = URLhausProvider(api_key=uh_key)
                self._enabled["urlhaus"] = True
            except Exception as e:
                logger.warning(f"Could not init URLhaus: {e}")
        
        # VirusTotal - needs key
        if enable_vt and is_api_available("virustotal"):
            try:
                self._providers["virustotal"] = VirusTotalProvider()
                self._enabled["virustotal"] = True
            except Exception as e:
                logger.warning(f"Could not init VirusTotal: {e}")
        
        # AbuseIPDB - needs key
        if enable_abuseipdb and is_api_available("abuseipdb"):
            try:
                key = get_api_key("abuseipdb")
                self._providers["abuseipdb"] = AbuseIPDBProvider(api_key=key)
                self._enabled["abuseipdb"] = True
            except Exception as e:
                logger.warning(f"Could not init AbuseIPDB: {e}")
        
        # OTX - needs key
        if enable_otx and is_api_available("otx"):
            try:
                key = get_api_key("otx")
                self._providers["otx"] = OTXProvider(api_key=key)
                self._enabled["otx"] = True
            except Exception as e:
                logger.warning(f"Could not init OTX: {e}")
        
        # GreyNoise - needs key
        if enable_greynoise and is_api_available("greynoise"):
            try:
                key = get_api_key("greynoise")
                self._providers["greynoise"] = GreyNoiseProvider(api_key=key)
                self._enabled["greynoise"] = True
            except Exception as e:
                logger.warning(f"Could not init GreyNoise: {e}")
        
        # Shodan - needs key
        if enable_shodan and is_api_available("shodan"):
            try:
                key = get_api_key("shodan")
                self._providers["shodan"] = ShodanProvider(api_key=key)
                self._enabled["shodan"] = True
            except Exception as e:
                logger.warning(f"Could not init Shodan: {e}")
        
        # CrowdSec CTI - needs key (botnet behaviors, DDoS sources)
        if enable_crowdsec and is_api_available("crowdsec"):
            try:
                key = get_api_key("crowdsec")
                self._providers["crowdsec"] = CrowdSecProvider(api_key=key)
                self._enabled["crowdsec"] = True
            except Exception as e:
                logger.warning(f"Could not init CrowdSec: {e}")
        
        # Feodo Tracker - no key needed (C2 server tracking)
        if enable_feodo:
            try:
                self._providers["feodo"] = FeodoTrackerProvider()
                self._enabled["feodo"] = True
            except Exception as e:
                logger.warning(f"Could not init Feodo Tracker: {e}")
        
        # Spamhaus - no key needed (DNS blocklist)
        if enable_spamhaus:
            try:
                self._providers["spamhaus"] = SpamhausProvider()
                self._enabled["spamhaus"] = True
            except Exception as e:
                logger.warning(f"Could not init Spamhaus: {e}")
    
    def extract_iocs(self, content: str) -> List[IOC]:
        """
        Extract IOCs from text content.
        
        Args:
            content: Text to extract IOCs from
            
        Returns:
            List of unique IOCs
        """
        iocs = set()
        
        for ioc_type, pattern in IOC_PATTERNS.items():
            for match in pattern.finditer(content):
                value = match.group()
                
                # Skip private IPs
                if ioc_type == IOCType.IP:
                    if any(r.match(value) for r in PRIVATE_IP_RANGES):
                        continue
                
                # Skip common false positive domains and file extensions
                if ioc_type == IOCType.DOMAIN:
                    lower_val = value.lower()
                    # Skip example domains
                    if lower_val in ("example.com", "localhost", "test.com"):
                        continue
                    # Skip common file extensions being matched as domains
                    file_extensions = (".exe", ".dll", ".bat", ".ps1", ".vbs", ".js", 
                                      ".tmp", ".log", ".txt", ".doc", ".pdf", ".zip")
                    if any(lower_val.endswith(ext) for ext in file_extensions):
                        continue
                    # Skip code object/method patterns (e.g., objHTTP.Open, disk.FreeSpace)
                    code_patterns = ("obj", "wscript", "adodb", "msxml", "createobject", 
                                    "disk.", "osinfo.", "info.", "data.", "file.", "path.")
                    if any(pat in lower_val for pat in code_patterns):
                        continue
                    # Skip single-label names (no actual TLD)
                    parts = lower_val.split(".")
                    if len(parts) < 2:
                        continue
                    # Skip if TLD looks like a method/property name (capital letters, common code patterns)
                    tld = parts[-1]
                    if len(tld) > 10 or tld in ("open", "close", "send", "write", "read", "shell", "stream", "type"):
                        continue
                    # Skip common code patterns
                    if lower_val.count(".") == 1 and parts[0] in ("obj", "str", "int", "var", "set", "get"):
                        continue
                
                iocs.add(IOC(type=ioc_type, value=value))
        
        return list(iocs)
    
    def extract_iocs_from_file(self, parsed_file: ParsedFile) -> List[IOC]:
        """Extract IOCs from a parsed file."""
        iocs = set()
        
        # Add file hash
        sha256 = parsed_file.hashes.get("sha256")
        if sha256:
            iocs.add(IOC(type=IOCType.HASH_SHA256, value=sha256, context="file_hash"))
        
        # Extract from strings
        content = "\n".join(parsed_file.strings[:500])
        for ioc in self.extract_iocs(content):
            ioc.context = "strings"
            iocs.add(ioc)
        
        # Extract from URLs
        for url in parsed_file.urls:
            iocs.add(IOC(type=IOCType.URL, value=url, context="extracted_url"))
        
        return list(iocs)
    
    def enrich_file(self, parsed_file: ParsedFile) -> EnrichmentResult:
        """
        Enrich a parsed file with threat intelligence.
        
        Args:
            parsed_file: Parsed file to enrich
            
        Returns:
            EnrichmentResult with all intel
        """
        t0 = time.perf_counter()
        
        iocs = self.extract_iocs_from_file(parsed_file)
        result = self.enrich_iocs(iocs)
        
        result.latency_ms = (time.perf_counter() - t0) * 1000
        return result
    
    def enrich_iocs(self, iocs: List[IOC]) -> EnrichmentResult:
        """
        Enrich a list of IOCs with threat intelligence.
        
        Args:
            iocs: List of IOCs to enrich
            
        Returns:
            EnrichmentResult with all intel
        """
        t0 = time.perf_counter()
        result = EnrichmentResult()
        
        # Group IOCs by type
        hashes = [i for i in iocs if i.type in (IOCType.HASH_SHA256, IOCType.HASH_SHA1, IOCType.HASH_MD5)]
        ips = [i for i in iocs if i.type == IOCType.IP]
        urls = [i for i in iocs if i.type == IOCType.URL]
        domains = [i for i in iocs if i.type == IOCType.DOMAIN]
        
        # Enrich hashes (prioritize SHA256)
        sha256_iocs = [i for i in hashes if i.type == IOCType.HASH_SHA256]
        if sha256_iocs:
            hash_intel = self._enrich_hash(sha256_iocs[0].value)
            if hash_intel:
                result.file_hash_intel = hash_intel
                result.sources_matched.extend(hash_intel.sources)
        
        # Enrich IPs (parallel)
        if ips:
            for ip_ioc in ips[:10]:  # Limit to 10
                ip_intel = self._enrich_ip(ip_ioc.value)
                if ip_intel:
                    result.ip_intel.append(ip_intel)
                    result.sources_matched.extend(ip_intel.sources)
        
        # Enrich URLs (parallel)
        if urls:
            for url_ioc in urls[:10]:  # Limit to 10
                url_intel = self._enrich_url(url_ioc.value)
                if url_intel:
                    result.url_intel.append(url_intel)
                    result.sources_matched.extend(url_intel.sources)
        
        # Enrich domains
        if domains:
            for domain_ioc in domains[:10]:  # Limit to 10
                domain_intel = self._enrich_domain(domain_ioc.value)
                if domain_intel:
                    result.domain_intel.append(domain_intel)
                    result.sources_matched.extend(domain_intel.sources)
        
        # Track queried sources
        result.sources_queried = list(self._enabled.keys())
        result.sources_matched = list(set(result.sources_matched))
        
        # Calculate aggregate score
        result.threat_score, result.confidence = self._calculate_score(result)
        
        result.latency_ms = (time.perf_counter() - t0) * 1000
        
        logger.info(
            f"Enrichment complete: score={result.threat_score:.2f}, "
            f"sources={len(result.sources_matched)}, "
            f"latency={result.latency_ms:.0f}ms"
        )
        
        return result
    
    def _enrich_hash(self, sha256: str) -> Optional[HashIntel]:
        """Enrich a file hash."""
        intel = HashIntel(sha256=sha256)
        
        # Query MalwareBazaar first (fast, no key)
        if "malware_bazaar" in self._providers:
            try:
                mb = self._providers["malware_bazaar"]
                sample = mb.lookup_hash(sha256)
                if sample:
                    intel.is_known_malware = True
                    intel.malware_family = sample.family
                    intel.mb_tags = sample.tags
                    intel.first_seen = sample.first_seen
                    intel.sources.append("malware_bazaar")
            except Exception as e:
                logger.warning(f"MalwareBazaar lookup failed: {e}")
        
        # Query VirusTotal (rate limited)
        if "virustotal" in self._providers:
            try:
                vt = self._providers["virustotal"]
                report = vt.lookup_hash(sha256)
                if report:
                    intel.vt_detections = report.detection_ratio
                    intel.vt_threat_label = report.popular_threat_label
                    if report.malicious > 5:
                        intel.is_known_malware = True
                    if not intel.malware_family and report.popular_threat_label:
                        intel.malware_family = report.popular_threat_label
                    intel.sources.append("virustotal")
            except Exception as e:
                logger.warning(f"VirusTotal lookup failed: {e}")
        
        return intel if intel.sources else None
    
    def _enrich_ip(self, ip: str) -> Optional[IPIntel]:
        """Enrich an IP address."""
        intel = IPIntel(ip=ip)
        
        # AbuseIPDB
        if "abuseipdb" in self._providers:
            try:
                provider = self._providers["abuseipdb"]
                result = provider.lookup_ip(ip)
                if result:
                    intel.abuse_score = result.raw_data.get("abuseConfidenceScore")
                    intel.country = result.raw_data.get("countryCode")
                    if intel.abuse_score and intel.abuse_score > 50:
                        intel.is_malicious = True
                        intel.is_known_attacker = True
                    intel.sources.append("abuseipdb")
            except Exception as e:
                logger.warning(f"AbuseIPDB lookup failed: {e}")
        
        # GreyNoise
        if "greynoise" in self._providers:
            try:
                provider = self._providers["greynoise"]
                result = provider.lookup_ip(ip)
                if result:
                    intel.is_noise = result.raw_data.get("noise", False)
                    if result.is_malicious:
                        intel.is_malicious = True
                    intel.sources.append("greynoise")
            except Exception as e:
                logger.warning(f"GreyNoise lookup failed: {e}")
        
        # VirusTotal (if not rate limited)
        if "virustotal" in self._providers:
            try:
                vt = self._providers["virustotal"]
                result = vt.lookup_ip(ip)
                if result and result.threat_level == ThreatLevel.MALICIOUS:
                    intel.is_malicious = True
                    intel.sources.append("virustotal")
            except Exception as e:
                logger.debug(f"VirusTotal IP lookup failed: {e}")
        
        # CrowdSec CTI - botnet behaviors, DDoS sources
        if "crowdsec" in self._providers:
            try:
                provider = self._providers["crowdsec"]
                result = provider.lookup_ip(ip)
                if result:
                    intel.is_botnet = result.raw_data.get("is_botnet", False)
                    intel.botnet_behaviors = result.raw_data.get("behaviors", [])
                    if result.is_malicious:
                        intel.is_malicious = True
                    if result.raw_data.get("country"):
                        intel.country = intel.country or result.raw_data.get("country")
                    if result.raw_data.get("as_name"):
                        intel.asn = intel.asn or result.raw_data.get("as_name")
                    intel.sources.append("crowdsec")
            except Exception as e:
                logger.warning(f"CrowdSec lookup failed: {e}")
        
        # Feodo Tracker - C2 server detection
        if "feodo" in self._providers:
            try:
                provider = self._providers["feodo"]
                result = provider.lookup_ip(ip)
                if result:
                    intel.is_c2 = result.raw_data.get("is_c2", False)
                    intel.c2_malware_family = result.raw_data.get("malware")
                    if intel.is_c2:
                        intel.is_malicious = True
                    intel.sources.append("feodo")
            except Exception as e:
                logger.warning(f"Feodo Tracker lookup failed: {e}")
        
        # Spamhaus - blocklist lookup
        if "spamhaus" in self._providers:
            try:
                provider = self._providers["spamhaus"]
                result = provider.lookup_ip(ip)
                if result:
                    intel.in_blocklist = result.raw_data.get("in_blocklist", False)
                    if result.raw_data.get("listings"):
                        intel.blocklist_sources = [
                            l.get("list", "") for l in result.raw_data.get("listings", [])
                        ]
                    if result.is_malicious:
                        intel.is_malicious = True
                    intel.sources.append("spamhaus")
            except Exception as e:
                logger.warning(f"Spamhaus lookup failed: {e}")
        
        return intel if intel.sources else None
    
    def _enrich_url(self, url: str) -> Optional[URLIntel]:
        """Enrich a URL."""
        intel = URLIntel(url=url)
        
        # URLhaus first (fast, no key)
        if "urlhaus" in self._providers:
            try:
                provider = self._providers["urlhaus"]
                result = provider.lookup_url(url)
                if result:
                    intel.is_malicious = result.is_malicious
                    intel.threat_type = result.raw_data.get("threat")
                    intel.status = result.raw_data.get("status")
                    intel.sources.append("urlhaus")
            except Exception as e:
                logger.warning(f"URLhaus lookup failed: {e}")
        
        # VirusTotal
        if "virustotal" in self._providers:
            try:
                vt = self._providers["virustotal"]
                result = vt.lookup_url(url)
                if result:
                    intel.vt_detections = result.raw_data.get("detection_ratio")
                    if result.is_malicious:
                        intel.is_malicious = True
                    intel.sources.append("virustotal")
            except Exception as e:
                logger.debug(f"VirusTotal URL lookup failed: {e}")
        
        return intel if intel.sources else None
    
    def _enrich_domain(self, domain: str) -> Optional[DomainIntel]:
        """Enrich a domain."""
        intel = DomainIntel(domain=domain)
        
        # VirusTotal
        if "virustotal" in self._providers:
            try:
                vt = self._providers["virustotal"]
                result = vt.lookup_domain(domain)
                if result:
                    intel.vt_detections = result.raw_data.get("detection_ratio")
                    intel.categories = result.raw_data.get("categories", {})
                    intel.registrar = result.raw_data.get("registrar")
                    intel.creation_date = result.raw_data.get("creation_date")
                    if result.is_malicious:
                        intel.is_malicious = True
                    intel.sources.append("virustotal")
            except Exception as e:
                logger.debug(f"VirusTotal domain lookup failed: {e}")
        
        # OTX
        if "otx" in self._providers:
            try:
                provider = self._providers["otx"]
                result = provider.lookup_domain(domain)
                if result and result.threat_level in (ThreatLevel.SUSPICIOUS, ThreatLevel.MALICIOUS):
                    intel.is_malicious = intel.is_malicious or result.threat_level == ThreatLevel.MALICIOUS
                    intel.sources.append("otx")
            except Exception as e:
                logger.debug(f"OTX domain lookup failed: {e}")
        
        return intel if intel.sources else None
    
    def _calculate_score(self, result: EnrichmentResult) -> tuple:
        """Calculate aggregate threat score and confidence."""
        scores = []
        
        if result.file_hash_intel:
            if result.file_hash_intel.is_known_malware:
                scores.append(0.95)
            elif result.file_hash_intel.vt_detections:
                # Parse VT detections
                try:
                    mal, total = result.file_hash_intel.vt_detections.split("/")
                    rate = int(mal) / int(total) if int(total) > 0 else 0
                    scores.append(0.3 + rate * 0.6)
                except:
                    pass
        
        for ip in result.ip_intel:
            if ip.is_malicious:
                scores.append(0.8)
            elif ip.abuse_score and ip.abuse_score > 30:
                scores.append(0.4 + ip.abuse_score / 200)
        
        for url in result.url_intel:
            if url.is_malicious:
                scores.append(0.9)
        
        for domain in result.domain_intel:
            if domain.is_malicious:
                scores.append(0.7)
        
        if not scores:
            return 0.0, 0.0
        
        # Use max score (any malicious indicator is concerning)
        threat_score = max(scores)
        
        # Confidence based on number of sources
        confidence = min(0.95, 0.5 + len(result.sources_matched) * 0.1)
        
        return threat_score, confidence
    
    def get_stats(self) -> Dict:
        """Get enrichment engine statistics."""
        stats = {
            "enabled_providers": list(self._enabled.keys()),
            "provider_stats": {},
        }
        
        for name, provider in self._providers.items():
            if hasattr(provider, "get_stats"):
                stats["provider_stats"][name] = provider.get_stats()
        
        return stats
