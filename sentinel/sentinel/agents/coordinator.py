"""Coordinator agent for final verdict aggregation."""

import time
from enum import Enum
from typing import Any, Dict, List, Optional

from .base import BaseAgent
from .state import GraphState, Finding, AnalysisStage
from ..providers.base import ThreatLevel, AnalysisResult
from ..logging import get_logger

logger = get_logger(__name__)


class VotingStrategy(Enum):
    """Voting strategies for aggregating provider decisions."""
    MAJORITY = "majority"           # Most weighted votes wins
    WEIGHTED_MAJORITY = "weighted"  # Majority with minimum evidence threshold
    UNANIMOUS_CLEAN = "unanimous"   # Clean only if ALL providers say clean
    MINORITY_ALERT = "minority"     # Alert if ANY provider flags (paranoid mode)


class CoordinatorAgent(BaseAgent):
    """
    Coordinator agent that aggregates all analysis results using vote-based aggregation.
    
    ARCHITECTURE FIX: This coordinator respects provider-level decisions (threat_level)
    rather than re-interpreting raw scores. When a provider returns CLEAN, it votes CLEAN
    regardless of the raw score value.
    
    Voting Strategy (Weighted Majority with Minimum Threshold):
    1. Each provider casts a weighted vote for its threat_level
    2. Weight = base_weight * provider_confidence
    3. Final decision = threat level with most weighted votes
    4. Constraint: MALICIOUS requires minimum evidence threshold (30% of total votes)
    
    This prevents:
    - Single low-confidence detector causing false positives
    - Raw score aggregation overriding provider decisions
    - Score averaging that ignores threshold-based decisions
    """
    
    # Weight configuration for different sources (based on expected accuracy)
    WEIGHTS = {
        "entropy_analyzer": 0.15,
        "strings_analyzer": 0.20,
        "malware_pe_ml": 0.30,
        "malware_api_ml": 0.25,
        "yara_rules": 0.25,
        "anomaly_detector": 0.15,
        "glm_analyzer": 0.20,
        "metadata_analysis": 0.10,
        "llm_reasoning": 0.25,
        "script_agent": 0.35,
        "threat_intel": 0.35,  # High weight for external intel
        "default": 0.10,
    }
    
    # Minimum vote threshold for MALICIOUS verdict (prevents single-detector FPs)
    MIN_MALICIOUS_VOTE_RATIO = 0.40
    
    # Minimum vote threshold for SUSPICIOUS verdict
    # Requires significant consensus to flag as suspicious (prevents single-detector FPs)
    MIN_SUSPICIOUS_VOTE_RATIO = 0.50
    
    def __init__(self, voting_strategy: VotingStrategy = VotingStrategy.WEIGHTED_MAJORITY):
        """
        Initialize coordinator.
        
        Args:
            voting_strategy: Strategy for aggregating votes
        """
        self.voting_strategy = voting_strategy
    
    @property
    def name(self) -> str:
        return "coordinator"
    
    def __call__(self, state: GraphState) -> Dict[str, Any]:
        self._log_start(state)
        t0 = time.perf_counter()
        
        # Collect all results
        all_results: List[AnalysisResult] = []
        all_results.extend(state.get("static_results", []))
        all_results.extend(state.get("ml_results", []))
        all_results.extend(state.get("llm_results", []))
        
        all_findings = state.get("findings", [])
        reasoning_steps = state.get("reasoning_steps", [])
        
        # Compute weighted verdict
        threat_level, final_score, confidence = self._compute_verdict(
            all_results, all_findings
        )
        
        # Generate final reasoning
        final_reasoning = self._generate_reasoning(
            state, all_results, all_findings, threat_level, final_score
        )
        
        # Add coordinator reasoning
        reasoning_steps.append(
            f"Coordinator verdict: {threat_level.value} "
            f"(score={final_score:.1%}, confidence={confidence:.1%})"
        )
        
        elapsed = (time.perf_counter() - t0) * 1000
        
        # Compute total analysis time
        total_time = elapsed
        for key in ["static_analysis_time_ms", "ml_analysis_time_ms", "llm_analysis_time_ms"]:
            if state.get("metadata", {}).get(key):
                total_time += state["metadata"][key]
        
        updates = {
            "threat_level": threat_level,
            "final_score": final_score,
            "confidence": confidence,
            "final_reasoning": final_reasoning,
            "reasoning_steps": reasoning_steps,
            "stages_completed": [AnalysisStage.AGGREGATION.value],
            "current_stage": AnalysisStage.COMPLETE,
            "analysis_time_ms": total_time,
        }
        
        self._log_complete(updates)
        return updates
    
    def _compute_verdict(
        self,
        results: List[AnalysisResult],
        findings: List[Finding]
    ) -> tuple:
        """
        Compute verdict using vote-based aggregation.
        
        KEY ARCHITECTURAL CHANGE:
        - We aggregate DECISIONS (threat_level), not raw scores
        - When a provider says CLEAN, it votes CLEAN regardless of raw score
        - Raw scores are ONLY used for reporting, not decision making
        
        Returns:
            Tuple of (threat_level, vote_ratio_for_malicious, confidence)
        """
        if not results and not findings:
            return ThreatLevel.UNKNOWN, 0.0, 0.0
        
        # Initialize vote tallies
        threat_votes = {
            ThreatLevel.CLEAN: 0.0,
            ThreatLevel.SUSPICIOUS: 0.0,
            ThreatLevel.MALICIOUS: 0.0,
            ThreatLevel.CRITICAL: 0.0,
        }
        
        total_weight = 0.0
        provider_decisions = []  # For logging/debugging
        
        # Process provider results - VOTE BASED ON THREAT_LEVEL ONLY
        for result in results:
            base_weight = self.WEIGHTS.get(result.provider_name, self.WEIGHTS["default"])
            
            # Weight adjusted by provider's confidence in its decision
            weight = base_weight * result.confidence
            
            # Cast vote for the provider's DECISION, not its raw score
            if result.threat_level in threat_votes:
                threat_votes[result.threat_level] += weight
                total_weight += weight
                
                provider_decisions.append({
                    "provider": result.provider_name,
                    "decision": result.threat_level.value,
                    "raw_score": result.score,
                    "confidence": result.confidence,
                    "weight": weight,
                })
        
        # Process findings with reduced weight (secondary evidence)
        severity_to_threat = {
            "low": ThreatLevel.CLEAN,
            "medium": ThreatLevel.SUSPICIOUS,
            "high": ThreatLevel.MALICIOUS,
            "critical": ThreatLevel.CRITICAL,
        }
        
        # Count only high/critical severity findings
        # LOW and MEDIUM severity findings are informational only - don't influence verdict
        critical_count = sum(1 for f in findings if f.severity == "critical")
        high_count = sum(1 for f in findings if f.severity == "high")
        
        # CRITICAL findings strongly push toward MALICIOUS
        if critical_count > 0:
            finding_weight = self.WEIGHTS["default"] * 0.5
            threat_votes[ThreatLevel.MALICIOUS] += finding_weight * min(critical_count, 3)
            total_weight += finding_weight * min(critical_count, 3)
        
        # HIGH findings push toward SUSPICIOUS
        # Require multiple HIGH findings OR specific high-confidence security findings
        if high_count >= 2:
            finding_weight = self.WEIGHTS["default"] * 0.3
            threat_votes[ThreatLevel.SUSPICIOUS] += finding_weight * min(high_count, 3)
            total_weight += finding_weight * min(high_count, 3)
        
        # Apply voting strategy
        threat_level, vote_confidence = self._apply_voting_strategy(threat_votes, total_weight)
        
        # Compute final score as ratio of malicious+critical votes (for reporting only)
        if total_weight > 0:
            malicious_ratio = (
                threat_votes[ThreatLevel.MALICIOUS] + 
                threat_votes[ThreatLevel.CRITICAL]
            ) / total_weight
        else:
            malicious_ratio = 0.0
        
        # Compute overall confidence
        if results:
            avg_confidence = sum(r.confidence for r in results) / len(results)
        else:
            avg_confidence = 0.5
        
        # Final confidence combines provider confidence and vote agreement
        confidence = avg_confidence * 0.6 + vote_confidence * 0.4
        
        logger.debug(
            f"Vote aggregation: {threat_level.value}, "
            f"votes={dict((k.value, round(v, 3)) for k, v in threat_votes.items())}, "
            f"confidence={confidence:.2f}"
        )
        
        return threat_level, malicious_ratio, min(confidence, 1.0)
    
    def _apply_voting_strategy(
        self,
        threat_votes: Dict[ThreatLevel, float],
        total_weight: float
    ) -> tuple:
        """
        Apply voting strategy to determine final verdict.
        
        Returns:
            Tuple of (threat_level, vote_confidence)
        """
        if total_weight == 0:
            return ThreatLevel.UNKNOWN, 0.0
        
        # Calculate vote ratios
        clean_ratio = threat_votes[ThreatLevel.CLEAN] / total_weight
        suspicious_ratio = threat_votes[ThreatLevel.SUSPICIOUS] / total_weight
        malicious_ratio = (
            threat_votes[ThreatLevel.MALICIOUS] + 
            threat_votes[ThreatLevel.CRITICAL]
        ) / total_weight
        
        if self.voting_strategy == VotingStrategy.MINORITY_ALERT:
            # Paranoid mode: Any malicious vote triggers alert
            if threat_votes[ThreatLevel.CRITICAL] > 0:
                return ThreatLevel.CRITICAL, malicious_ratio
            elif threat_votes[ThreatLevel.MALICIOUS] > 0:
                return ThreatLevel.MALICIOUS, malicious_ratio
            elif threat_votes[ThreatLevel.SUSPICIOUS] > 0:
                return ThreatLevel.SUSPICIOUS, suspicious_ratio
            else:
                return ThreatLevel.CLEAN, clean_ratio
        
        elif self.voting_strategy == VotingStrategy.UNANIMOUS_CLEAN:
            # Only clean if ALL providers agree
            if malicious_ratio > 0:
                return ThreatLevel.MALICIOUS, malicious_ratio
            elif suspicious_ratio > 0:
                return ThreatLevel.SUSPICIOUS, suspicious_ratio
            else:
                return ThreatLevel.CLEAN, clean_ratio
        
        else:  # MAJORITY or WEIGHTED_MAJORITY
            # Weighted majority with minimum evidence threshold
            
            # CRITICAL: requires strong evidence
            if threat_votes[ThreatLevel.CRITICAL] / total_weight > 0.4:
                return ThreatLevel.CRITICAL, malicious_ratio
            
            # MALICIOUS: requires majority AND minimum threshold
            if (malicious_ratio > clean_ratio and 
                malicious_ratio >= self.MIN_MALICIOUS_VOTE_RATIO):
                return ThreatLevel.MALICIOUS, malicious_ratio
            
            # SUSPICIOUS: requires either:
            # 1. Clear majority (more suspicious than clean) AND significant threshold (60%+)
            # 2. Strong majority (1.3x clean) with minimum threshold (50%+)
            strong_suspicious = (
                suspicious_ratio > clean_ratio and 
                suspicious_ratio >= 0.52  # Slight majority required
            )
            moderate_suspicious = (
                suspicious_ratio > clean_ratio * 1.3 and
                suspicious_ratio >= self.MIN_SUSPICIOUS_VOTE_RATIO
            )
            
            if strong_suspicious or moderate_suspicious:
                return ThreatLevel.SUSPICIOUS, suspicious_ratio
            
            # Default to CLEAN if no threat level has majority + threshold
            return ThreatLevel.CLEAN, clean_ratio
    
    def _generate_reasoning(
        self,
        state: GraphState,
        results: List[AnalysisResult],
        findings: List[Finding],
        threat_level: ThreatLevel,
        score: float
    ) -> str:
        """Generate final reasoning summary."""
        parsed_file = state.get("parsed_file")
        file_name = parsed_file.file_name if parsed_file else "unknown"
        
        lines = [f"Analysis of {file_name}:"]
        
        # Summarize static analysis
        static_results = state.get("static_results", [])
        if static_results:
            threats = [r.threat_level.value for r in static_results]
            lines.append(f"- Static analysis: {', '.join(threats)}")
        
        # Summarize ML analysis
        ml_results = state.get("ml_results", [])
        if ml_results:
            for r in ml_results:
                lines.append(f"- ML ({r.provider_name}): {r.threat_level.value} ({r.score:.1%})")
        
        # Summarize LLM analysis
        llm_results = state.get("llm_results", [])
        if llm_results:
            for r in llm_results:
                lines.append(f"- LLM reasoning: {r.threat_level.value}")
        
        # Key findings
        high_findings = [f for f in findings if f.severity in ("high", "critical")]
        if high_findings:
            lines.append(f"- {len(high_findings)} high-severity findings detected")
        
        # Verdict
        lines.append(f"\nFinal verdict: {threat_level.value.upper()} (score: {score:.1%})")
        
        # Indicators summary
        indicators = state.get("indicators", [])
        if indicators:
            lines.append(f"- {len(indicators)} indicators of compromise found")
        
        return "\n".join(lines)
