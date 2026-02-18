"""Entropy-based analysis provider."""

import time
from typing import Set

from ..base import Provider, AnalysisResult, ThreatLevel, Indicator
from ...parsers.base import ParsedFile, FileType
from ...logging import get_logger

logger = get_logger(__name__)


class EntropyProvider(Provider):
    """
    Entropy-based detection provider.
    
    Analyzes file entropy to detect:
    - Packed/compressed executables
    - Encrypted content
    - Obfuscated code
    """
    
    HIGH_ENTROPY_THRESHOLD = 7.0
    VERY_HIGH_ENTROPY_THRESHOLD = 7.5
    SECTION_ENTROPY_THRESHOLD = 7.2
    
    def __init__(self):
        self._latency_ema = 5.0
    
    @property
    def name(self) -> str:
        return "entropy_analyzer"
    
    @property
    def version(self) -> str:
        return "1.0.0"
    
    @property
    def supported_types(self) -> Set[FileType]:
        return {FileType.PE, FileType.PDF, FileType.OFFICE, FileType.SCRIPT}
    
    def get_cost_per_call(self) -> float:
        return 0.0
    
    def get_avg_latency_ms(self) -> float:
        return self._latency_ema
    
    def analyze(self, parsed_file: ParsedFile) -> AnalysisResult:
        t0 = time.perf_counter()
        
        findings = []
        indicators = []
        score = 0.0
        
        file_entropy = parsed_file.entropy
        
        if file_entropy >= self.VERY_HIGH_ENTROPY_THRESHOLD:
            score = 0.8
            findings.append(f"Very high file entropy: {file_entropy:.2f} (likely packed/encrypted)")
            indicators.append(Indicator(
                type="entropy",
                value=str(file_entropy),
                context="very_high_file_entropy"
            ))
        elif file_entropy >= self.HIGH_ENTROPY_THRESHOLD:
            score = 0.5
            findings.append(f"High file entropy: {file_entropy:.2f} (possibly packed)")
            indicators.append(Indicator(
                type="entropy",
                value=str(file_entropy),
                context="high_file_entropy"
            ))
        
        if parsed_file.sections:
            high_entropy_sections = []
            for section in parsed_file.sections:
                section_entropy = section.get("entropy", 0)
                section_name = section.get("name", "unknown")
                
                if section_entropy >= self.SECTION_ENTROPY_THRESHOLD:
                    high_entropy_sections.append(f"{section_name}:{section_entropy:.2f}")
                    
                    if section_name.lower() in [".text", ".code"]:
                        score = max(score, 0.7)
                        findings.append(
                            f"Code section '{section_name}' has high entropy: {section_entropy:.2f}"
                        )
            
            if high_entropy_sections:
                indicators.append(Indicator(
                    type="section_entropy",
                    value=", ".join(high_entropy_sections),
                    context="high_entropy_sections"
                ))
        
        if parsed_file.raw_features.get("avg_section_entropy", 0) >= 6.5:
            avg_entropy = parsed_file.raw_features["avg_section_entropy"]
            score = max(score, 0.4)
            findings.append(f"High average section entropy: {avg_entropy:.2f}")
        
        if score >= 0.7:
            threat_level = ThreatLevel.SUSPICIOUS
        elif score >= 0.4:
            threat_level = ThreatLevel.SUSPICIOUS
        else:
            threat_level = ThreatLevel.CLEAN
        
        confidence = min(0.6 + score * 0.3, 0.9)
        
        latency = (time.perf_counter() - t0) * 1000
        self._latency_ema = 0.9 * self._latency_ema + 0.1 * latency
        
        return AnalysisResult(
            provider_name=self.name,
            provider_version=self.version,
            threat_level=threat_level,
            score=score,
            confidence=confidence,
            findings=findings,
            indicators=indicators,
            latency_ms=latency,
            metadata={
                "file_entropy": file_entropy,
                "thresholds": {
                    "high": self.HIGH_ENTROPY_THRESHOLD,
                    "very_high": self.VERY_HIGH_ENTROPY_THRESHOLD,
                }
            }
        )
