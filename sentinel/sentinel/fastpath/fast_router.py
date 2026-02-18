"""Fast path router for quick triage before full analysis."""

import time
from dataclasses import dataclass, field
from enum import Enum
from typing import Dict, List, Optional, Any

from .yara_engine import YaraEngine, YaraMatch
from .hash_lookup import HashLookup, HashLookupResult, HashVerdict
from .signatures import SignatureMatcher, SignatureMatch
from ..parsers.base import ParsedFile
from ..providers.base import ThreatLevel
from ..logging import get_logger

logger = get_logger(__name__)


class FastPathVerdict(str, Enum):
    """Fast path decision."""
    KNOWN_MALWARE = "known_malware"      # Skip to verdict: MALICIOUS
    KNOWN_CLEAN = "known_clean"          # Skip to verdict: CLEAN
    HIGH_CONFIDENCE = "high_confidence"  # High confidence detection, may skip deep analysis
    NEEDS_ANALYSIS = "needs_analysis"    # Continue to full analysis
    ERROR = "error"


@dataclass
class FastPathResult:
    """Result from fast path evaluation."""
    verdict: FastPathVerdict
    threat_level: ThreatLevel
    confidence: float
    
    # Evidence
    hash_result: Optional[HashLookupResult] = None
    yara_matches: List[YaraMatch] = field(default_factory=list)
    signature_matches: List[SignatureMatch] = field(default_factory=list)
    
    # Metadata
    skip_full_analysis: bool = False
    reasoning: str = ""
    latency_ms: float = 0.0
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary."""
        return {
            "verdict": self.verdict.value,
            "threat_level": self.threat_level.value,
            "confidence": self.confidence,
            "skip_full_analysis": self.skip_full_analysis,
            "reasoning": self.reasoning,
            "latency_ms": self.latency_ms,
            "hash_verdict": self.hash_result.verdict.value if self.hash_result else None,
            "yara_match_count": len(self.yara_matches),
            "signature_match_count": len(self.signature_matches),
        }


class FastPathRouter:
    """
    Fast path router for quick malware triage.
    
    Decision flow:
    1. Hash lookup - known malware/clean?
    2. YARA rules - quick pattern match
    3. Signatures - behavior patterns
    4. Decision - skip analysis or continue
    
    This allows:
    - Immediate verdict for known samples
    - High-confidence detection without LLM cost
    - Quick triage for high-volume scenarios
    """
    
    def __init__(
        self,
        yara_rules_path: Optional[str] = None,
        malware_hashes_path: Optional[str] = None,
        clean_hashes_path: Optional[str] = None,
        enable_yara: bool = True,
        enable_hash_lookup: bool = True,
        enable_signatures: bool = True,
        use_external_rules: bool = True,
    ):
        """
        Initialize fast path router.
        
        Args:
            yara_rules_path: Path to YARA rules
            malware_hashes_path: Path to malware hash database
            clean_hashes_path: Path to clean hash database
            enable_yara: Enable YARA scanning
            enable_hash_lookup: Enable hash lookup
            enable_signatures: Enable signature matching
        """
        self.enable_yara = enable_yara
        self.enable_hash_lookup = enable_hash_lookup
        self.enable_signatures = enable_signatures
        
        # Initialize components
        if enable_yara:
            self.yara_engine = YaraEngine(
                yara_rules_path,
                use_external_rules=use_external_rules,
            )
        else:
            self.yara_engine = None
        
        if enable_hash_lookup:
            self.hash_lookup = HashLookup(malware_hashes_path, clean_hashes_path)
        else:
            self.hash_lookup = None
        
        if enable_signatures:
            self.signature_matcher = SignatureMatcher()
        else:
            self.signature_matcher = None
    
    def evaluate(self, parsed_file: ParsedFile) -> FastPathResult:
        """
        Evaluate file through fast path.
        
        Args:
            parsed_file: Parsed file to evaluate
            
        Returns:
            FastPathResult with verdict and evidence
        """
        t0 = time.perf_counter()
        
        hash_result = None
        yara_matches = []
        signature_matches = []
        
        # Step 1: Hash lookup
        if self.enable_hash_lookup and self.hash_lookup:
            sha256 = parsed_file.hashes.get("sha256", "")
            if sha256:
                hash_result = self.hash_lookup.lookup(sha256, "sha256")
                
                if hash_result.verdict == HashVerdict.KNOWN_MALWARE:
                    return FastPathResult(
                        verdict=FastPathVerdict.KNOWN_MALWARE,
                        threat_level=ThreatLevel.MALICIOUS,
                        confidence=1.0,
                        hash_result=hash_result,
                        skip_full_analysis=True,
                        reasoning=f"Known malware: {hash_result.malware_family}",
                        latency_ms=(time.perf_counter() - t0) * 1000,
                    )
                
                if hash_result.verdict == HashVerdict.KNOWN_CLEAN:
                    return FastPathResult(
                        verdict=FastPathVerdict.KNOWN_CLEAN,
                        threat_level=ThreatLevel.CLEAN,
                        confidence=1.0,
                        hash_result=hash_result,
                        skip_full_analysis=True,
                        reasoning="Known clean file (whitelisted)",
                        latency_ms=(time.perf_counter() - t0) * 1000,
                    )
        
        # Step 2: YARA rules
        if self.enable_yara and self.yara_engine and self.yara_engine.available:
            yara_matches = self.yara_engine.match_file(parsed_file.file_path)
            
            # Check for critical YARA matches
            critical_matches = [m for m in yara_matches if m.is_malware]
            if critical_matches:
                return FastPathResult(
                    verdict=FastPathVerdict.HIGH_CONFIDENCE,
                    threat_level=ThreatLevel.MALICIOUS,
                    confidence=0.95,
                    hash_result=hash_result,
                    yara_matches=yara_matches,
                    skip_full_analysis=True,
                    reasoning=f"YARA malware detection: {critical_matches[0].rule_name}",
                    latency_ms=(time.perf_counter() - t0) * 1000,
                )
        
        # Step 3: Signature matching
        if self.enable_signatures and self.signature_matcher:
            signature_matches = self.signature_matcher.match(parsed_file)
            
            # Count severity levels
            critical_sigs = [s for s in signature_matches if s.severity == "critical"]
            high_sigs = [s for s in signature_matches if s.severity == "high"]
            
            # Multiple critical signatures = high confidence malicious
            if len(critical_sigs) >= 2:
                return FastPathResult(
                    verdict=FastPathVerdict.HIGH_CONFIDENCE,
                    threat_level=ThreatLevel.MALICIOUS,
                    confidence=0.90,
                    hash_result=hash_result,
                    yara_matches=yara_matches,
                    signature_matches=signature_matches,
                    skip_full_analysis=True,
                    reasoning=f"Multiple critical signatures: {[s.name for s in critical_sigs[:3]]}",
                    latency_ms=(time.perf_counter() - t0) * 1000,
                )
            
            # Single critical + high signatures = high confidence
            if critical_sigs and len(high_sigs) >= 2:
                return FastPathResult(
                    verdict=FastPathVerdict.HIGH_CONFIDENCE,
                    threat_level=ThreatLevel.MALICIOUS,
                    confidence=0.85,
                    hash_result=hash_result,
                    yara_matches=yara_matches,
                    signature_matches=signature_matches,
                    skip_full_analysis=False,  # Still run full analysis for confirmation
                    reasoning=f"Critical signature ({critical_sigs[0].name}) with supporting evidence",
                    latency_ms=(time.perf_counter() - t0) * 1000,
                )
        
        # Step 4: Aggregate evidence for suspicious determination
        threat_level, confidence, skip = self._aggregate_evidence(
            yara_matches, signature_matches
        )
        
        reasoning_parts = []
        if yara_matches:
            reasoning_parts.append(f"{len(yara_matches)} YARA matches")
        if signature_matches:
            reasoning_parts.append(f"{len(signature_matches)} signature matches")
        
        if not reasoning_parts:
            reasoning_parts.append("No fast path detections")
        
        latency = (time.perf_counter() - t0) * 1000
        
        return FastPathResult(
            verdict=FastPathVerdict.NEEDS_ANALYSIS if threat_level == ThreatLevel.UNKNOWN 
                    else FastPathVerdict.HIGH_CONFIDENCE,
            threat_level=threat_level,
            confidence=confidence,
            hash_result=hash_result,
            yara_matches=yara_matches,
            signature_matches=signature_matches,
            skip_full_analysis=skip,
            reasoning="; ".join(reasoning_parts),
            latency_ms=latency,
        )
    
    def _aggregate_evidence(
        self,
        yara_matches: List[YaraMatch],
        signature_matches: List[SignatureMatch]
    ) -> tuple:
        """
        Aggregate evidence from all fast path checks.
        
        Returns:
            (threat_level, confidence, skip_full_analysis)
        """
        if not yara_matches and not signature_matches:
            return ThreatLevel.UNKNOWN, 0.0, False
        
        # Score based on matches
        score = 0.0
        
        for ym in yara_matches:
            if ym.severity == "critical":
                score += 0.4
            elif ym.severity == "high":
                score += 0.25
            elif ym.severity == "medium":
                score += 0.15
            else:
                score += 0.05
        
        for sm in signature_matches:
            if sm.severity == "critical":
                score += 0.35
            elif sm.severity == "high":
                score += 0.2
            elif sm.severity == "medium":
                score += 0.1
            else:
                score += 0.05
        
        # Determine threat level
        if score >= 0.8:
            return ThreatLevel.MALICIOUS, min(score, 0.95), True
        elif score >= 0.5:
            return ThreatLevel.SUSPICIOUS, score * 0.9, False
        elif score >= 0.2:
            return ThreatLevel.SUSPICIOUS, score * 0.8, False
        else:
            return ThreatLevel.UNKNOWN, 0.0, False
    
    def evaluate_file(self, file_path: str) -> FastPathResult:
        """
        Evaluate a file path directly (without pre-parsing).
        
        For quick hash + YARA check before full parsing.
        """
        t0 = time.perf_counter()
        
        # Quick hash check
        if self.enable_hash_lookup and self.hash_lookup:
            from ..utils.hash import compute_multi_hash
            hashes = compute_multi_hash(file_path)
            sha256 = hashes.get("sha256", "")
            
            if sha256:
                hash_result = self.hash_lookup.lookup(sha256)
                
                if hash_result.verdict == HashVerdict.KNOWN_MALWARE:
                    return FastPathResult(
                        verdict=FastPathVerdict.KNOWN_MALWARE,
                        threat_level=ThreatLevel.MALICIOUS,
                        confidence=1.0,
                        hash_result=hash_result,
                        skip_full_analysis=True,
                        reasoning=f"Known malware: {hash_result.malware_family}",
                        latency_ms=(time.perf_counter() - t0) * 1000,
                    )
        
        # Quick YARA check
        yara_matches = []
        if self.enable_yara and self.yara_engine and self.yara_engine.available:
            yara_matches = self.yara_engine.match_file(file_path)
            
            critical = [m for m in yara_matches if m.is_malware]
            if critical:
                return FastPathResult(
                    verdict=FastPathVerdict.HIGH_CONFIDENCE,
                    threat_level=ThreatLevel.MALICIOUS,
                    confidence=0.95,
                    yara_matches=yara_matches,
                    skip_full_analysis=True,
                    reasoning=f"YARA malware: {critical[0].rule_name}",
                    latency_ms=(time.perf_counter() - t0) * 1000,
                )
        
        return FastPathResult(
            verdict=FastPathVerdict.NEEDS_ANALYSIS,
            threat_level=ThreatLevel.UNKNOWN,
            confidence=0.0,
            yara_matches=yara_matches,
            skip_full_analysis=False,
            reasoning="Fast path inconclusive, needs full analysis",
            latency_ms=(time.perf_counter() - t0) * 1000,
        )
    
    @property
    def stats(self) -> Dict:
        """Get fast path component statistics."""
        return {
            "yara_enabled": self.enable_yara,
            "yara_available": self.yara_engine.available if self.yara_engine else False,
            "yara_rules": self.yara_engine.rule_count if self.yara_engine else 0,
            "hash_lookup_enabled": self.enable_hash_lookup,
            "hash_db_stats": self.hash_lookup.stats if self.hash_lookup else {},
            "signatures_enabled": self.enable_signatures,
            "signature_count": self.signature_matcher.signature_count if self.signature_matcher else 0,
        }
