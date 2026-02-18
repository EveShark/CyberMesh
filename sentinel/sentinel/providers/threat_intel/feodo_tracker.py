"""Feodo Tracker C2 intelligence provider."""

import time
import threading
from typing import Set, Optional, Dict, Any

import httpx

from .base_intel import BaseThreatIntelProvider, IntelResult
from ...parsers.base import FileType
from ...logging import get_logger

logger = get_logger(__name__)


class FeodoTrackerProvider(BaseThreatIntelProvider):
    """
    Feodo Tracker provider for C2 server detection.
    
    Tracks command and control servers for:
    - Dridex
    - Emotet  
    - TrickBot
    - QakBot (QBot)
    - BazarLoader/BazarBackdoor
    
    Uses public JSON feed - no API key required.
    Feed is cached locally and refreshed periodically.
    
    API: https://feodotracker.abuse.ch/
    """
    
    BLOCKLIST_URL = "https://feodotracker.abuse.ch/downloads/ipblocklist_recommended.json"
    REFRESH_INTERVAL = 3600  # 1 hour
    
    _blocklist: Dict[str, Dict[str, Any]] = {}
    _last_refresh: float = 0
    _lock = threading.Lock()
    
    def __init__(self, api_key: str = "", timeout: int = 30):
        # No API key needed, but keep signature for compatibility
        super().__init__(api_key=api_key or "none", timeout=timeout)
        self._ensure_blocklist_loaded()
    
    @property
    def name(self) -> str:
        return "feodo_tracker"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.SCRIPT, FileType.PCAP}
    
    def _ensure_blocklist_loaded(self) -> None:
        """Ensure blocklist is loaded and fresh."""
        with self._lock:
            now = time.time()
            if now - self._last_refresh < self.REFRESH_INTERVAL and self._blocklist:
                return
            
            try:
                self._refresh_blocklist()
            except Exception as e:
                logger.error(f"Failed to refresh Feodo blocklist: {e}")
                # Keep using stale data if available
    
    def _refresh_blocklist(self) -> None:
        """Download and parse the Feodo blocklist."""
        logger.info("Refreshing Feodo Tracker blocklist...")
        
        try:
            with httpx.Client(timeout=self.timeout) as client:
                response = client.get(self.BLOCKLIST_URL)
                response.raise_for_status()
                data = response.json()
            
            new_blocklist = {}
            
            for entry in data:
                ip = entry.get("ip_address")
                if not ip:
                    continue
                
                new_blocklist[ip] = {
                    "ip": ip,
                    "port": entry.get("port"),
                    "status": entry.get("status", "unknown"),
                    "hostname": entry.get("hostname"),
                    "as_number": entry.get("as_number"),
                    "as_name": entry.get("as_name"),
                    "country": entry.get("country"),
                    "first_seen": entry.get("first_seen"),
                    "last_online": entry.get("last_online"),
                    "malware": entry.get("malware", "unknown"),
                }
            
            self._blocklist = new_blocklist
            self._last_refresh = time.time()
            
            logger.info(f"Feodo blocklist loaded: {len(self._blocklist)} C2 IPs")
            
        except Exception as e:
            logger.error(f"Feodo blocklist refresh failed: {e}")
            raise
    
    def lookup_ip(self, ip: str) -> Optional[IntelResult]:
        """Check IP against Feodo Tracker C2 list."""
        cache_key = f"ip:{ip}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        self._ensure_blocklist_loaded()
        
        c2_data = self._blocklist.get(ip)
        
        if not c2_data:
            # IP not in C2 list - this is good news
            result = IntelResult(
                source="feodo_tracker",
                query=ip,
                query_type="ip",
                is_malicious=False,
                confidence=0.5,
                risk_score=0.0,
                categories=["not_listed"],
                raw_data={"is_c2": False},
            )
            self._cache[cache_key] = result
            return result
        
        # IP is a known C2 server
        malware = c2_data.get("malware", "unknown")
        status = c2_data.get("status", "unknown")
        
        # Higher risk for currently online C2s
        if status == "online":
            risk_score = 95.0
            confidence = 0.95
        else:
            risk_score = 85.0
            confidence = 0.85
        
        result = IntelResult(
            source="feodo_tracker",
            query=ip,
            query_type="ip",
            is_malicious=True,
            confidence=confidence,
            risk_score=risk_score,
            categories=[f"c2:{malware}", f"status:{status}"],
            tags=[
                f"malware:{malware}",
                f"port:{c2_data.get('port', 'unknown')}",
                f"as:{c2_data.get('as_name', 'unknown')}",
            ],
            first_seen=c2_data.get("first_seen"),
            last_seen=c2_data.get("last_online"),
            raw_data={
                "is_c2": True,
                "malware": malware,
                "status": status,
                "port": c2_data.get("port"),
                "hostname": c2_data.get("hostname"),
                "as_number": c2_data.get("as_number"),
                "as_name": c2_data.get("as_name"),
                "country": c2_data.get("country"),
            },
        )
        
        self._cache[cache_key] = result
        logger.warning(f"Feodo: {ip} is C2 for {malware} ({status})")
        return result
    
    def lookup_domain(self, domain: str) -> Optional[IntelResult]:
        """Check if domain matches any C2 hostname."""
        cache_key = f"domain:{domain}"
        if cache_key in self._cache:
            return self._cache[cache_key]
        
        self._ensure_blocklist_loaded()
        
        # Search by hostname
        domain_lower = domain.lower()
        for ip, c2_data in self._blocklist.items():
            hostname = c2_data.get("hostname", "")
            if hostname and domain_lower == hostname.lower():
                malware = c2_data.get("malware", "unknown")
                result = IntelResult(
                    source="feodo_tracker",
                    query=domain,
                    query_type="domain",
                    is_malicious=True,
                    confidence=0.9,
                    risk_score=90.0,
                    categories=[f"c2:{malware}"],
                    tags=[f"resolves_to:{ip}"],
                    raw_data={
                        "is_c2": True,
                        "malware": malware,
                        "c2_ip": ip,
                    },
                )
                self._cache[cache_key] = result
                return result
        
        # Domain not found
        result = IntelResult(
            source="feodo_tracker",
            query=domain,
            query_type="domain",
            is_malicious=False,
            confidence=0.4,
            risk_score=0.0,
            categories=["not_listed"],
            raw_data={"is_c2": False},
        )
        self._cache[cache_key] = result
        return result
    
    def get_stats(self) -> Dict[str, Any]:
        """Get provider statistics."""
        return {
            "blocklist_size": len(self._blocklist),
            "last_refresh": self._last_refresh,
            "cache_size": len(self._cache),
        }
