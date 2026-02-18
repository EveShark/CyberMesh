"""Spamhaus blocklist provider using DNS lookups."""

import socket
from typing import Set, Optional, Dict, List

from .base_intel import BaseThreatIntelProvider, IntelResult
from ...parsers.base import FileType
from ...logging import get_logger

logger = get_logger(__name__)


# Spamhaus return code mappings
# https://www.spamhaus.org/faq/section/DNSBL%20Usage#200
SPAMHAUS_CODES = {
    # SBL - Spamhaus Block List
    "127.0.0.2": ("SBL", "Direct spam source, spam operation, or spam service"),
    "127.0.0.3": ("SBL", "Direct spam source, spam operation, or spam service"),
    
    # CSS - Composite Snowshoe Spamhaus
    "127.0.0.4": ("CSS", "Snowshoe spam source"),
    
    # XBL - Exploits Block List
    "127.0.0.5": ("XBL", "Illegal 3rd party exploits (open proxies)"),
    "127.0.0.6": ("XBL", "Illegal 3rd party exploits (open proxies)"),
    "127.0.0.7": ("XBL", "Illegal 3rd party exploits (open proxies)"),
    
    # PBL - Policy Block List  
    "127.0.0.10": ("PBL", "End-user IP range (ISP maintained)"),
    "127.0.0.11": ("PBL", "End-user IP range (Spamhaus maintained)"),
    
    # DROP/EDROP
    "127.0.0.9": ("DROP", "Hijacked netblock, do not route"),
    
    # SBL Advisory
    "127.0.0.255": ("SBL", "Test record"),
}

# Blocklists to query
BLOCKLISTS = [
    "zen.spamhaus.org",  # Combined (SBL + XBL + PBL)
    "sbl.spamhaus.org",  # Spam sources
    "xbl.spamhaus.org",  # Exploits/botnets
]


class SpamhausProvider(BaseThreatIntelProvider):
    """
    Spamhaus DNS-based blocklist provider.
    
    Checks IP against:
    - SBL (Spamhaus Block List) - Spam sources
    - XBL (Exploits Block List) - Botnets, exploits
    - PBL (Policy Block List) - Dynamic IPs
    - DROP (Don't Route or Peer) - Hijacked netblocks
    
    Uses DNS queries - no API key required.
    """
    
    def __init__(self, api_key: str = "", timeout: int = 5):
        # No API key needed
        super().__init__(api_key=api_key or "none", timeout=timeout)
        socket.setdefaulttimeout(timeout)
    
    @property
    def name(self) -> str:
        return "spamhaus"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.SCRIPT, FileType.PCAP, FileType.PDF, FileType.OFFICE}
    
    def _reverse_ip(self, ip: str) -> str:
        """Reverse IP octets for DNS lookup."""
        parts = ip.split(".")
        return ".".join(reversed(parts))
    
    def _dns_lookup(self, query: str) -> List[str]:
        """Perform DNS lookup and return all A records."""
        try:
            answers = socket.gethostbyname_ex(query)
            return answers[2]  # List of IP addresses
        except socket.gaierror:
            return []
        except socket.timeout:
            logger.warning(f"DNS timeout for {query}")
            return []
        except Exception as e:
            logger.debug(f"DNS lookup failed for {query}: {e}")
            return []
    
    def lookup_ip(self, ip: str) -> Optional[IntelResult]:
        """Check IP against Spamhaus blocklists."""
        cache_key = f"ip:{ip}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        # Skip private IPs
        parts = ip.split(".")
        if len(parts) != 4:
            return None
        
        try:
            first = int(parts[0])
            second = int(parts[1])
            if first == 10 or first == 127:
                return None
            if first == 172 and 16 <= second <= 31:
                return None
            if first == 192 and second == 168:
                return None
        except ValueError:
            return None
        
        reversed_ip = self._reverse_ip(ip)
        
        listings: List[Dict] = []
        blocklist_codes: List[str] = []
        
        # Query ZEN (combined list) first
        zen_query = f"{reversed_ip}.zen.spamhaus.org"
        zen_results = self._dns_lookup(zen_query)
        
        for result_ip in zen_results:
            code_info = SPAMHAUS_CODES.get(result_ip, ("UNKNOWN", "Unknown listing"))
            list_name, description = code_info
            listings.append({
                "list": list_name,
                "code": result_ip,
                "description": description,
            })
            blocklist_codes.append(list_name)
        
        if not listings:
            # IP not listed - clean
            result = IntelResult(
                source="spamhaus",
                query=ip,
                query_type="ip",
                is_malicious=False,
                confidence=0.6,
                risk_score=0.0,
                categories=["not_listed"],
                raw_data={"in_blocklist": False, "listings": []},
            )
            self._cache[cache_key] = result
            return result
        
        # Calculate risk based on which lists the IP is on
        unique_lists = set(blocklist_codes)
        
        # DROP is most severe - hijacked netblock
        if "DROP" in unique_lists:
            risk_score = 95.0
            confidence = 0.95
        # XBL indicates botnet/exploit
        elif "XBL" in unique_lists:
            risk_score = 85.0
            confidence = 0.9
        # SBL indicates spam source
        elif "SBL" in unique_lists:
            risk_score = 75.0
            confidence = 0.85
        # CSS is snowshoe spam
        elif "CSS" in unique_lists:
            risk_score = 70.0
            confidence = 0.8
        # PBL is policy-based (dynamic IPs)
        elif "PBL" in unique_lists:
            risk_score = 30.0  # Lower risk, just dynamic IP
            confidence = 0.7
        else:
            risk_score = 50.0
            confidence = 0.6
        
        # Only mark as malicious for severe lists
        is_malicious = bool(unique_lists & {"DROP", "XBL", "SBL", "CSS"})
        
        result = IntelResult(
            source="spamhaus",
            query=ip,
            query_type="ip",
            is_malicious=is_malicious,
            confidence=confidence,
            risk_score=risk_score,
            categories=list(unique_lists),
            tags=[f"listing:{l['code']}" for l in listings],
            raw_data={
                "in_blocklist": True,
                "listings": listings,
                "blocklist_codes": blocklist_codes,
            },
        )
        
        self._cache[cache_key] = result
        
        if is_malicious:
            logger.warning(f"Spamhaus: {ip} listed in {unique_lists}")
        else:
            logger.debug(f"Spamhaus: {ip} listed in {unique_lists} (low severity)")
        
        return result
    
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """Check domain against Spamhaus DBL."""
        cache_key = f"domain:{domain}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        # Query Domain Block List
        dbl_query = f"{domain}.dbl.spamhaus.org"
        dbl_results = self._dns_lookup(dbl_query)
        
        if not dbl_results:
            result = IntelResult(
                source="spamhaus",
                query=domain,
                query_type="domain",
                is_malicious=False,
                confidence=0.5,
                risk_score=0.0,
                categories=["not_listed"],
                raw_data={"in_blocklist": False},
            )
            self._cache[cache_key] = result
            return result
        
        # Domain is listed
        result = IntelResult(
            source="spamhaus",
            query=domain,
            query_type="domain",
            is_malicious=True,
            confidence=0.85,
            risk_score=80.0,
            categories=["DBL"],
            tags=[f"dbl_response:{r}" for r in dbl_results],
            raw_data={
                "in_blocklist": True,
                "dbl_codes": dbl_results,
            },
        )
        
        self._cache[cache_key] = result
        logger.warning(f"Spamhaus DBL: {domain} is listed")
        return result
