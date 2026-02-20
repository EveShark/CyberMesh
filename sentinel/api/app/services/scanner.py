"""Sentinel malware detection engine wrapper."""

import sys
import time
from typing import Dict, Any, Optional, List
from pathlib import Path

from ..config import get_settings
from ..models.schemas import (
    Verdict,
    ThreatLevel,
    DetectionType,
    Severity,
    Detection,
    DetectionSummary,
    IOCs,
    FileInfo,
    ChartData,
    ChartDataset,
)
from .file_handler import format_file_size


class SentinelScanner:
    """Wrapper for Sentinel malware detection engine."""
    
    _engine = None
    _initialized = False
    
    @classmethod
    def initialize(cls) -> bool:
        """Initialize Sentinel engine."""
        if cls._initialized:
            return True
        
        settings = get_settings()
        # The sentinel package is in B:\sentinel, so we need parent of api folder
        sentinel_base = Path(__file__).parent.parent.parent.parent
        
        # Add sentinel base to path
        if str(sentinel_base) not in sys.path:
            sys.path.insert(0, str(sentinel_base))
        
        try:
            # Import sentinel modules
            import logging
            logging.basicConfig(level=logging.WARNING)
            
            from sentinel.agents import AnalysisEngine
            
            cls._engine = AnalysisEngine(
                enable_llm=False,
                enable_fast_path=True,
                enable_threat_intel=True,  # Network intel: CrowdSec, Feodo, Spamhaus
            )
            cls._initialized = True
            return True
            
        except Exception as e:
            print(f"Failed to initialize Sentinel: {e}")
            import traceback
            traceback.print_exc()
            return False
    
    @classmethod
    def get_engine(cls):
        """Get initialized engine."""
        if not cls._initialized:
            cls.initialize()
        return cls._engine
    
    @classmethod
    async def scan_file(cls, file_path: str) -> Dict[str, Any]:
        """
        Scan a file using Sentinel engine.
        
        Args:
            file_path: Path to file to scan
            
        Returns:
            Scan result dictionary
        """
        engine = cls.get_engine()
        if not engine:
            raise RuntimeError("Sentinel engine not initialized")
        
        start_time = time.perf_counter()
        
        try:
            # Run analysis
            result = engine.analyze(file_path)
            
            processing_time = int((time.perf_counter() - start_time) * 1000)
            
            # Convert to our format
            return cls._format_result(result, processing_time)
            
        except Exception as e:
            raise RuntimeError(f"Scan failed: {str(e)}")
    
    @classmethod
    def _format_result(cls, result: Any, processing_time: int) -> Dict[str, Any]:
        """Format Sentinel result to API response format."""
        
        # Result is a dict, get values directly
        threat_level_str = result.get("threat_level", "clean")
        if hasattr(threat_level_str, 'value'):
            threat_level_str = threat_level_str.value
        threat_level_str = str(threat_level_str).lower()
        
        # Map verdict from threat_level
        verdict_map = {
            "clean": Verdict.CLEAN,
            "suspicious": Verdict.SUSPICIOUS,
            "malicious": Verdict.MALICIOUS,
        }
        verdict = verdict_map.get(threat_level_str, Verdict.CLEAN)
        
        # Map threat level
        threat_level_map = {
            "clean": ThreatLevel.LOW,
            "low": ThreatLevel.LOW,
            "suspicious": ThreatLevel.MEDIUM,
            "medium": ThreatLevel.MEDIUM,
            "malicious": ThreatLevel.HIGH,
            "high": ThreatLevel.HIGH,
            "critical": ThreatLevel.CRITICAL,
        }
        threat_level = threat_level_map.get(threat_level_str, ThreatLevel.LOW)
        
        # Get confidence from result
        confidence = result.get("confidence", 0.5)
        
        # Calculate risk score (0-100)
        risk_score = cls._calculate_risk_score(verdict, confidence, threat_level)
        
        # Format detections
        detections = cls._format_detections(result)
        
        # Extract IOCs
        iocs = cls._extract_iocs(result)
        
        # Generate chart data
        charts = cls._generate_charts(detections, risk_score)
        
        return {
            "verdict": verdict,
            "confidence": round(confidence, 2),
            "threat_level": threat_level,
            "risk_score": risk_score,
            "processing_time_ms": processing_time,
            "detections": detections,
            "iocs": iocs,
            "charts": charts,
        }
    
    @classmethod
    def _calculate_risk_score(
        cls,
        verdict: Verdict,
        confidence: float,
        threat_level: ThreatLevel,
    ) -> int:
        """Calculate risk score 0-100."""
        
        # Base score from verdict
        base_scores = {
            Verdict.CLEAN: 0,
            Verdict.SUSPICIOUS: 40,
            Verdict.MALICIOUS: 70,
        }
        base = base_scores.get(verdict, 0)
        
        # Add confidence modifier
        confidence_modifier = confidence * 20
        
        # Add threat level modifier
        level_modifiers = {
            ThreatLevel.LOW: 0,
            ThreatLevel.MEDIUM: 5,
            ThreatLevel.HIGH: 10,
            ThreatLevel.CRITICAL: 15,
        }
        level_mod = level_modifiers.get(threat_level, 0)
        
        score = int(base + confidence_modifier + level_mod)
        return min(100, max(0, score))
    
    @classmethod
    def _format_detections(cls, result: Dict[str, Any]) -> DetectionSummary:
        """Format detections from Sentinel result dict."""
        
        items = []
        severity_counts = {"critical": 0, "high": 0, "medium": 0, "low": 0}
        
        # Process findings (list of Finding objects as strings)
        findings = result.get('findings', []) or []
        
        for finding in findings:
            # Parse finding string if needed
            if isinstance(finding, str):
                # Format: "Finding(source='strings_analyzer', severity='high', description='...', ...)"
                source = ""
                severity_str = "medium"
                description = finding
                
                # Extract source
                if "source='" in finding:
                    start = finding.find("source='") + 8
                    end = finding.find("'", start)
                    source = finding[start:end]
                
                # Extract severity
                if "severity='" in finding:
                    start = finding.find("severity='") + 10
                    end = finding.find("'", start)
                    severity_str = finding[start:end].lower()
                
                # Extract description
                if "description='" in finding:
                    start = finding.find("description='") + 13
                    end = finding.find("'", start)
                    description = finding[start:end]
            else:
                # It's an object
                source = getattr(finding, 'source', '') or ''
                severity_str = str(getattr(finding, 'severity', 'medium')).lower()
                description = getattr(finding, 'description', str(finding))
            
            severity_map = {
                "critical": Severity.CRITICAL,
                "high": Severity.HIGH,
                "medium": Severity.MEDIUM,
                "low": Severity.LOW,
                "informational": Severity.LOW,
            }
            severity = severity_map.get(severity_str, Severity.MEDIUM)
            
            # Determine detection type from source
            if 'yara' in source.lower():
                det_type = DetectionType.YARA
            elif 'hash' in source.lower():
                det_type = DetectionType.HASH
            elif 'ml' in source.lower():
                det_type = DetectionType.ML
            elif 'string' in source.lower():
                det_type = DetectionType.STRINGS
            else:
                det_type = DetectionType.SIGNATURE
            
            # Extract name from description or source
            name = source if source else "Detection"
            if description and ':' in description:
                name = description.split(':')[0]
            
            items.append(Detection(
                type=det_type,
                name=name,
                severity=severity,
                description=description,
            ))
            
            severity_counts[severity.value] += 1
        
        return DetectionSummary(
            total=len(items),
            critical=severity_counts["critical"],
            high=severity_counts["high"],
            medium=severity_counts["medium"],
            low=severity_counts["low"],
            items=items[:20],  # Limit to 20 items
        )
    
    @classmethod
    def _extract_iocs(cls, result: Dict[str, Any]) -> IOCs:
        """Extract IOCs from Sentinel result dict."""
        
        domains = []
        ips = []
        urls = []
        
        # Extract from indicators list
        indicators = result.get('indicators', []) or []
        
        for indicator in indicators:
            if isinstance(indicator, dict):
                ind_type = indicator.get('type', '').lower()
                value = indicator.get('value', '')
            else:
                # Handle string representation
                ind_type = ''
                value = str(indicator)
            
            if 'domain' in ind_type:
                domains.append(value)
            elif 'ip' in ind_type:
                ips.append(value)
            elif 'url' in ind_type:
                urls.append(value)
        
        # Also extract from parsed_file if available
        parsed_file = result.get('parsed_file', '')
        if parsed_file and isinstance(parsed_file, str):
            # Extract URLs from string representation
            if "urls=[" in parsed_file:
                try:
                    start = parsed_file.find("urls=[") + 6
                    end = parsed_file.find("]", start)
                    urls_str = parsed_file[start:end]
                    for url in urls_str.replace("'", "").split(","):
                        url = url.strip()
                        if url and url.startswith("http"):
                            urls.append(url)
                except:
                    pass
            
            # Extract domains
            if "domains=[" in parsed_file:
                try:
                    start = parsed_file.find("domains=[") + 9
                    end = parsed_file.find("]", start)
                    domains_str = parsed_file[start:end]
                    for domain in domains_str.replace("'", "").split(","):
                        domain = domain.strip()
                        if domain and '.' in domain:
                            domains.append(domain)
                except:
                    pass
        
        # Deduplicate
        domains = list(set(domains))[:50]
        ips = list(set(ips))[:50]
        urls = list(set(urls))[:50]
        
        return IOCs(
            total=len(domains) + len(ips) + len(urls),
            domains=domains,
            ips=ips,
            urls=urls,
        )
    
    @classmethod
    def _generate_charts(cls, detections: DetectionSummary, risk_score: int) -> ChartData:
        """Generate chart data for frontend visualization."""
        
        # Severity breakdown chart
        severity_breakdown = ChartDataset(
            labels=["Critical", "High", "Medium", "Low"],
            values=[
                detections.critical,
                detections.high,
                detections.medium,
                detections.low,
            ],
            colors=["#dc2626", "#f97316", "#eab308", "#22c55e"],
        )
        
        # Detection sources chart
        source_counts = {"yara": 0, "hash": 0, "ml": 0, "strings": 0, "signature": 0}
        for item in detections.items:
            source_counts[item.type.value] = source_counts.get(item.type.value, 0) + 1
        
        detection_sources = ChartDataset(
            labels=["YARA", "Hash DB", "ML", "Strings", "Signatures"],
            values=[
                source_counts["yara"],
                source_counts["hash"],
                source_counts["ml"],
                source_counts["strings"],
                source_counts["signature"],
            ],
            colors=["#3b82f6", "#8b5cf6", "#06b6d4", "#10b981", "#f59e0b"],
        )
        
        # Risk gauge
        risk_gauge = {
            "value": risk_score,
            "max": 100,
            "zones": [
                {"min": 0, "max": 30, "color": "#22c55e", "label": "Low"},
                {"min": 30, "max": 60, "color": "#eab308", "label": "Medium"},
                {"min": 60, "max": 85, "color": "#f97316", "label": "High"},
                {"min": 85, "max": 100, "color": "#dc2626", "label": "Critical"},
            ],
        }
        
        return ChartData(
            severity_breakdown=severity_breakdown,
            detection_sources=detection_sources,
            risk_gauge=risk_gauge,
        )
    
    @classmethod
    async def lookup_hash(cls, file_hash: str) -> Optional[Dict[str, Any]]:
        """
        Lookup hash in malware database.
        
        Args:
            file_hash: SHA256 hash to lookup
            
        Returns:
            Hash info if found, None otherwise
        """
        engine = cls.get_engine()
        if not engine:
            return None
        
        try:
            # Try to access hash lookup through fast path router
            if hasattr(engine, 'fast_path_router') and engine.fast_path_router:
                router = engine.fast_path_router
                if hasattr(router, 'hash_lookup') and router.hash_lookup:
                    result = router.hash_lookup.lookup(file_hash)
                    if result and result.verdict.value == 'known_malware':
                        return {
                            "known": True,
                            "verdict": "malicious",
                            "family": result.malware_family or "Unknown",
                            "source": result.source or "local",
                        }
            
            return {"known": False, "verdict": "unknown"}
            
        except Exception:
            return None
    
    @classmethod
    def is_initialized(cls) -> bool:
        """Check if engine is initialized."""
        return cls._initialized
    
    @classmethod
    def get_stats(cls) -> Dict[str, Any]:
        """Get engine statistics."""
        engine = cls.get_engine()
        
        if not engine:
            return {"initialized": False}
        
        stats = {"initialized": True}
        
        # Try to get fast path stats
        try:
            if hasattr(engine, 'fast_path_router') and engine.fast_path_router:
                stats["fast_path"] = engine.fast_path_router.stats
        except:
            pass
        
        return stats


# Initialize on import
scanner = SentinelScanner()
