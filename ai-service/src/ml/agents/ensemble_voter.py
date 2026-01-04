"""EnsembleVoter for DetectionAgentPipeline."""

import math
from typing import Dict, List, Optional, TYPE_CHECKING
from collections import defaultdict

from .contracts import DetectionCandidate, EnsembleDecision, ThreatType
from ...logging import get_logger

if TYPE_CHECKING:
    from .rollout_config import RolloutConfig

logger = get_logger(__name__)


class AgentEnsembleVoter:
    """
    Ensemble voter for DetectionAgentPipeline.
    
    Combines detection candidates from multiple agents using
    weighted voting based on configuration. Supports rollout states
    where shadow/off agents have zero effective weight.
    """
    
    def __init__(
        self,
        config: Dict,
        rollout_config: Optional["RolloutConfig"] = None,
    ):
        """
        Initialize voter with configuration.
        
        Args:
            config: Dict with weights and thresholds:
                - agent_weights: Dict[agent_id, weight]
                - min_malicious_ratio: float
                - profiles: Dict[profile_name, thresholds]
            rollout_config: Optional RolloutConfig for state-aware weighting
        """
        self._config = config
        self._agent_weights = config.get('agent_weights', {})
        self._min_malicious_ratio = config.get('min_malicious_ratio', 0.4)
        self._profiles = config.get('profiles', {
            'balanced': {'min_malicious_ratio': 0.4},
            'security_first': {'min_malicious_ratio': 0.3},
            'low_fp': {'min_malicious_ratio': 0.6},
        })
        self._rollout_config = rollout_config
    
    def decide(
        self,
        candidates: List[DetectionCandidate],
        profile: str = "balanced",
        tenant_id: Optional[str] = None,
    ) -> EnsembleDecision:
        """
        Make ensemble decision from detection candidates.
        
        Args:
            candidates: List of DetectionCandidate from all agents
            profile: Threshold profile name
            tenant_id: Optional tenant for per-tenant weight overrides
            
        Returns:
            EnsembleDecision with final verdict
        """
        self._current_tenant_id = tenant_id  # Store for _get_weight
        if not candidates:
            return EnsembleDecision(
                should_publish=False,
                threat_type=ThreatType.ANOMALY,
                final_score=0.0,
                confidence=0.0,
                candidates=[],
                abstention_reason="no_candidates",
                profile=profile,
                metadata={"reason": "No detection candidates"},
            )
        
        # Get profile thresholds
        profile_config = self._profiles.get(profile, {})
        min_ratio = profile_config.get('min_malicious_ratio', self._min_malicious_ratio)
        
        # Group by threat type and compute weighted scores
        threat_scores: Dict[ThreatType, float] = defaultdict(float)
        threat_weights: Dict[ThreatType, float] = defaultdict(float)
        
        for candidate in candidates:
            weight = self._get_weight(candidate)
            threat_scores[candidate.threat_type] += weight * candidate.calibrated_score
            threat_weights[candidate.threat_type] += weight
        
        # Compute weighted average per threat type
        threat_avg: Dict[ThreatType, float] = {}
        for tt in threat_scores:
            if threat_weights[tt] > 0:
                threat_avg[tt] = threat_scores[tt] / threat_weights[tt]
            else:
                threat_avg[tt] = 0.0
        
        # Find dominant threat
        if threat_avg:
            dominant = max(threat_avg, key=threat_avg.get)
            final_score = threat_avg[dominant]
        else:
            dominant = ThreatType.ANOMALY
            final_score = 0.0
        
        # Compute confidence
        confidences = [c.confidence for c in candidates]
        avg_confidence = sum(confidences) / len(confidences) if confidences else 0.0
        
        # Compute vote ratio for malicious types
        total_weight = sum(threat_weights.values())
        malicious_types = {ThreatType.MALWARE, ThreatType.DDOS, ThreatType.DOS, 
                          ThreatType.BOTNET, ThreatType.C2_COMMUNICATION}
        malicious_weight = sum(threat_weights[t] for t in malicious_types if t in threat_weights)
        vote_ratio = malicious_weight / total_weight if total_weight > 0 else 0.0
        
        # Decision
        should_publish = final_score >= min_ratio and vote_ratio >= min_ratio
        
        abstention_reason = None
        if not should_publish:
            if final_score < min_ratio:
                abstention_reason = f"below_threshold:score={final_score:.2f}<{min_ratio}"
            else:
                abstention_reason = f"below_threshold:vote_ratio={vote_ratio:.2f}<{min_ratio}"
        
        return EnsembleDecision(
            should_publish=should_publish,
            threat_type=dominant,
            final_score=final_score,
            confidence=avg_confidence,
            candidates=candidates,
            llr=self._compute_llr(candidates),
            abstention_reason=abstention_reason,
            profile=profile,
            metadata={
                "weights_used": {c.signal_id: self._get_weight(c) for c in candidates},
                "threat_scores": {t.value: s for t, s in threat_avg.items()},
                "vote_ratio": vote_ratio,
                "threshold": min_ratio,
            },
        )
    
    def _get_weight(self, candidate: DetectionCandidate) -> float:
        """
        Get effective weight for a candidate.
        
        Uses rollout config if available (for state-aware weighting),
        otherwise falls back to static agent_weights.
        """
        tenant_id = getattr(self, '_current_tenant_id', None)
        
        # Use rollout config if available
        if self._rollout_config:
            agent_config = self._rollout_config.get_agent(candidate.agent_id)
            if agent_config:
                return agent_config.get_effective_weight(tenant_id)
        
        # Fall back to static weights
        if candidate.agent_id in self._agent_weights:
            return self._agent_weights[candidate.agent_id]
        if candidate.signal_id in self._agent_weights:
            return self._agent_weights[candidate.signal_id]
        return 1.0
    
    def get_effective_weights(self, tenant_id: Optional[str] = None) -> Dict[str, float]:
        """
        Get effective weights for all configured agents.
        
        Args:
            tenant_id: Optional tenant for per-tenant overrides
            
        Returns:
            Dict mapping agent_id to effective weight
        """
        if self._rollout_config:
            return self._rollout_config.get_effective_weights(tenant_id)
        return dict(self._agent_weights)
    
    def _compute_llr(self, candidates: List[DetectionCandidate]) -> float:
        """Compute log-likelihood ratio."""
        if not candidates:
            return 0.0
        avg_score = sum(c.calibrated_score for c in candidates) / len(candidates)
        eps = 1e-6
        avg_score = max(eps, min(1 - eps, avg_score))
        return math.log(avg_score / (1 - avg_score))
