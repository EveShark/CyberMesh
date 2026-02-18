"""
EnsembleVoter - Combines DetectionCandidates into a single EnsembleDecision.

Uses weighted voting based on calibrated_config.json to determine final verdict.
"""

import json
from pathlib import Path
from typing import Dict, List, Optional
from collections import defaultdict

from ..contracts import (
    DetectionCandidate,
    EnsembleDecision,
    ThreatType,
)
from ..logging import get_logger

logger = get_logger(__name__)

# Default config path
DEFAULT_CONFIG_PATH = Path(__file__).parent.parent.parent / "calibrated_config.json"


class EnsembleVoter:
    """
    Combines detection candidates into a single ensemble decision.
    
    Uses config-driven weights and thresholds to:
    1. Weight each candidate by provider/signal
    2. Aggregate scores per threat type
    3. Apply profile-specific thresholds
    4. Determine should_publish and abstention reasons
    
    Example usage:
        voter = EnsembleVoter.from_config_file("calibrated_config.json")
        decision = voter.decide(candidates, profile="balanced")
    """
    
    def __init__(self, config: dict):
        """
        Initialize EnsembleVoter with configuration.
        
        Args:
            config: Configuration dict with voting_config, provider_thresholds,
                    threshold_profiles, etc.
        """
        self._config = config
        self._voting_config = config.get("voting_config", {})
        self._provider_thresholds = config.get("provider_thresholds", {})
        self._threshold_profiles = config.get("threshold_profiles", {})
        
        # Extract default weights per provider
        self._weights = {}
        for provider, cfg in self._provider_thresholds.items():
            self._weights[provider] = cfg.get("weight", 1.0)
    
    @classmethod
    def from_config_file(cls, path: Optional[str] = None) -> "EnsembleVoter":
        """
        Create EnsembleVoter from config file.
        
        Args:
            path: Path to config JSON (defaults to calibrated_config.json)
            
        Returns:
            Configured EnsembleVoter instance
        """
        config_path = Path(path) if path else DEFAULT_CONFIG_PATH
        
        if not config_path.exists():
            logger.warning(f"Config file not found: {config_path}, using defaults")
            return cls({})
        
        with open(config_path) as f:
            config = json.load(f)
        
        return cls(config)
    
    def decide(
        self,
        candidates: List[DetectionCandidate],
        profile: str = "balanced",
    ) -> EnsembleDecision:
        """
        Make ensemble decision from detection candidates.
        
        Args:
            candidates: List of DetectionCandidate objects
            profile: Threshold profile ("security_first", "balanced", "low_fp")
            
        Returns:
            EnsembleDecision with final verdict
        """
        # Handle empty candidates
        if not candidates:
            return EnsembleDecision(
                should_publish=False,
                threat_type=ThreatType.ANOMALY,
                final_score=0.0,
                confidence=0.0,
                candidates=[],
                abstention_reason="no_candidates",
                profile=profile,
                metadata={"reason": "No detection candidates provided"},
            )
        
        # Get profile thresholds
        profile_config = self._threshold_profiles.get(profile, {})
        min_malicious_ratio = profile_config.get(
            "min_malicious_ratio",
            self._voting_config.get("min_malicious_vote_ratio", 0.4)
        )
        
        # Group candidates by threat type and compute weighted scores
        threat_scores: Dict[ThreatType, float] = defaultdict(float)
        threat_weights: Dict[ThreatType, float] = defaultdict(float)
        threat_confidences: Dict[ThreatType, List[float]] = defaultdict(list)
        
        for candidate in candidates:
            weight = self._get_weight(candidate)
            threat_scores[candidate.threat_type] += weight * candidate.calibrated_score
            threat_weights[candidate.threat_type] += weight
            threat_confidences[candidate.threat_type].append(candidate.confidence)
        
        # Compute weighted average per threat type
        threat_avg_scores: Dict[ThreatType, float] = {}
        for threat_type in threat_scores:
            if threat_weights[threat_type] > 0:
                threat_avg_scores[threat_type] = (
                    threat_scores[threat_type] / threat_weights[threat_type]
                )
            else:
                threat_avg_scores[threat_type] = 0.0
        
        # Find dominant threat type
        if threat_avg_scores:
            dominant_threat = max(threat_avg_scores, key=threat_avg_scores.get)
            final_score = threat_avg_scores[dominant_threat]
        else:
            dominant_threat = ThreatType.ANOMALY
            final_score = 0.0
        
        # Compute ensemble confidence
        all_confidences = [c.confidence for c in candidates]
        avg_confidence = sum(all_confidences) / len(all_confidences) if all_confidences else 0.0
        
        # Compute weighted vote ratio
        total_weight = sum(threat_weights.values())
        malicious_weight = sum(
            threat_weights[t] for t in threat_weights
            if t in (ThreatType.MALWARE, ThreatType.C2_COMMUNICATION, ThreatType.BOTNET)
        )
        vote_ratio = malicious_weight / total_weight if total_weight > 0 else 0.0
        
        # Determine should_publish
        should_publish = (
            final_score >= min_malicious_ratio and
            vote_ratio >= min_malicious_ratio
        )
        
        # Build abstention reason if not publishing
        abstention_reason = None
        if not should_publish:
            if final_score < min_malicious_ratio:
                abstention_reason = f"below_threshold:score={final_score:.2f}<{min_malicious_ratio}"
            elif vote_ratio < min_malicious_ratio:
                abstention_reason = f"below_threshold:vote_ratio={vote_ratio:.2f}<{min_malicious_ratio}"
            else:
                abstention_reason = "below_threshold:unknown"
        
        return EnsembleDecision(
            should_publish=should_publish,
            threat_type=dominant_threat,
            final_score=final_score,
            confidence=avg_confidence,
            candidates=candidates,
            llr=self._compute_llr(candidates),
            abstention_reason=abstention_reason,
            profile=profile,
            metadata={
                "weights_used": {c.signal_id: self._get_weight(c) for c in candidates},
                "threat_scores": {t.value: s for t, s in threat_avg_scores.items()},
                "vote_ratio": vote_ratio,
                "threshold": min_malicious_ratio,
                "voting_strategy": self._voting_config.get("strategy", "WEIGHTED_MAJORITY"),
            },
        )
    
    def _get_weight(self, candidate: DetectionCandidate) -> float:
        """
        Get weight for a candidate based on signal_id or agent_id.
        
        Looks up weight in provider_thresholds config, falls back to 1.0.
        """
        # Try exact signal_id match
        if candidate.signal_id in self._weights:
            return self._weights[candidate.signal_id]
        
        # Try to match provider name patterns
        signal_lower = candidate.signal_id.lower()
        for provider_name, weight in self._weights.items():
            if provider_name.lower() in signal_lower:
                return weight
        
        # Try agent_id patterns (e.g., "sentinel.ml.malware_pe")
        agent_parts = candidate.agent_id.split(".")
        if len(agent_parts) >= 3:
            provider_hint = agent_parts[-1]
            for provider_name, weight in self._weights.items():
                if provider_name.lower() in provider_hint.lower():
                    return weight
        
        # Default weight
        return 1.0
    
    def _compute_llr(self, candidates: List[DetectionCandidate]) -> float:
        """
        Compute log-likelihood ratio for evidence strength.
        
        Simple approximation: average calibrated score transformed to LLR.
        """
        if not candidates:
            return 0.0
        
        avg_score = sum(c.calibrated_score for c in candidates) / len(candidates)
        
        # Avoid log(0) or log(inf)
        eps = 1e-6
        avg_score = max(eps, min(1 - eps, avg_score))
        
        # LLR = log(P(malicious) / P(benign))
        import math
        llr = math.log(avg_score / (1 - avg_score))
        
        return llr
