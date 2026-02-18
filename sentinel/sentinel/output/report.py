"""Threat report data structures."""

import json
import time
from dataclasses import dataclass, field, asdict
from typing import Any, Dict, List, Optional

from ..providers.base import ThreatLevel, AnalysisResult


@dataclass
class ProviderVerdict:
    """Verdict from a single provider."""
    provider_name: str
    threat_level: ThreatLevel
    score: float
    confidence: float
    findings: List[str] = field(default_factory=list)
    latency_ms: float = 0.0
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "provider_name": self.provider_name,
            "threat_level": self.threat_level.value,
            "score": self.score,
            "confidence": self.confidence,
            "findings": self.findings,
            "latency_ms": self.latency_ms,
        }


@dataclass
class AttackChainNode:
    """Node in attack chain visualization."""
    step: int
    action: str
    details: str = ""
    
    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)


@dataclass
class ThreatReport:
    """
    Complete threat analysis report.
    
    Contains aggregated results from all providers,
    final verdict, and explanation.
    """
    file_id: str
    file_hash: str
    file_name: str
    file_type: str
    file_size: int
    
    threat_level: ThreatLevel
    final_score: float
    confidence: float
    
    provider_verdicts: List[ProviderVerdict] = field(default_factory=list)
    findings: List[str] = field(default_factory=list)
    indicators: List[Dict[str, str]] = field(default_factory=list)
    attack_chain: List[AttackChainNode] = field(default_factory=list)
    
    reasoning: str = ""
    
    analysis_time_ms: float = 0.0
    timestamp: float = field(default_factory=time.time)
    
    metadata: Dict[str, Any] = field(default_factory=dict)
    
    @classmethod
    def from_analysis_results(
        cls,
        file_id: str,
        file_hash: str,
        file_name: str,
        file_type: str,
        file_size: int,
        results: List[AnalysisResult],
        reasoning: str = "",
        analysis_time_ms: float = 0.0,
    ) -> "ThreatReport":
        """Create report from multiple analysis results."""
        provider_verdicts = []
        all_findings = []
        all_indicators = []
        
        for result in results:
            verdict = ProviderVerdict(
                provider_name=result.provider_name,
                threat_level=result.threat_level,
                score=result.score,
                confidence=result.confidence,
                findings=result.findings,
                latency_ms=result.latency_ms,
            )
            provider_verdicts.append(verdict)
            all_findings.extend(result.findings)
            
            for indicator in result.indicators:
                all_indicators.append({
                    "type": indicator.type,
                    "value": indicator.value,
                    "context": indicator.context,
                })
        
        if results:
            weights = [r.confidence for r in results]
            total_weight = sum(weights) or 1.0
            
            final_score = sum(
                r.score * r.confidence for r in results
            ) / total_weight
            
            avg_confidence = sum(weights) / len(weights)
            
            threat_scores = {
                ThreatLevel.CLEAN: 0,
                ThreatLevel.SUSPICIOUS: 1,
                ThreatLevel.MALICIOUS: 2,
                ThreatLevel.CRITICAL: 3,
                ThreatLevel.UNKNOWN: 0,
            }
            
            weighted_threat = sum(
                threat_scores.get(r.threat_level, 0) * r.confidence
                for r in results
            ) / total_weight
            
            if weighted_threat >= 2.0:
                final_threat = ThreatLevel.MALICIOUS
            elif weighted_threat >= 1.0:
                final_threat = ThreatLevel.SUSPICIOUS
            else:
                final_threat = ThreatLevel.CLEAN
        else:
            final_score = 0.0
            avg_confidence = 0.0
            final_threat = ThreatLevel.UNKNOWN
        
        return cls(
            file_id=file_id,
            file_hash=file_hash,
            file_name=file_name,
            file_type=file_type,
            file_size=file_size,
            threat_level=final_threat,
            final_score=final_score,
            confidence=avg_confidence,
            provider_verdicts=provider_verdicts,
            findings=list(set(all_findings)),
            indicators=all_indicators,
            reasoning=reasoning,
            analysis_time_ms=analysis_time_ms,
        )
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary."""
        return {
            "file_id": self.file_id,
            "file_hash": self.file_hash,
            "file_name": self.file_name,
            "file_type": self.file_type,
            "file_size": self.file_size,
            "threat_level": self.threat_level.value,
            "final_score": self.final_score,
            "confidence": self.confidence,
            "provider_verdicts": [v.to_dict() for v in self.provider_verdicts],
            "findings": self.findings,
            "indicators": self.indicators,
            "attack_chain": [n.to_dict() for n in self.attack_chain],
            "reasoning": self.reasoning,
            "analysis_time_ms": self.analysis_time_ms,
            "timestamp": self.timestamp,
            "metadata": self.metadata,
        }
    
    def to_json(self, indent: int = 2) -> str:
        """Convert to JSON string."""
        return json.dumps(self.to_dict(), indent=indent)
    
    def get_consensus(self) -> str:
        """Get consensus string for display."""
        if not self.provider_verdicts:
            return "No verdicts"
        
        malicious = sum(1 for v in self.provider_verdicts if v.threat_level == ThreatLevel.MALICIOUS)
        suspicious = sum(1 for v in self.provider_verdicts if v.threat_level == ThreatLevel.SUSPICIOUS)
        clean = sum(1 for v in self.provider_verdicts if v.threat_level == ThreatLevel.CLEAN)
        total = len(self.provider_verdicts)
        
        return f"{malicious}/{total} MALICIOUS, {suspicious}/{total} SUSPICIOUS, {clean}/{total} CLEAN"
