"""
Ensemble voting with weighted decision-making.

Implements formulas:
- #4: Trust-weighted confidence
- #5: BFT resilience
- #7: F-beta score calibration
- #9: Weighted ensemble voting
- #11: Log-likelihood ratio (LLR)
- #12: Uncertainty-aware ensemble

Military-grade consensus with abstention logic.
"""

import numpy as np
from typing import List, Dict, Optional
from .types import DetectionCandidate, EnsembleDecision, EngineType, ThreatType
from .adaptive import AdaptiveDetection
from ..logging import get_logger


class EnsembleVoter:
    """
    Ensemble decision maker with weighted voting and abstention.
    
    Configuration-driven weights and thresholds (from .env).
    Adaptive thresholds from FeedbackService integration.
    """
    
    def __init__(self, config: Dict, adaptive: Optional[AdaptiveDetection] = None):
        """
        Initialize ensemble voter.
        
        Args:
            config: Dictionary with:
                - ML_WEIGHT, RULES_WEIGHT, MATH_WEIGHT
                - DDOS_THRESHOLD, MALWARE_THRESHOLD, ANOMALY_THRESHOLD
                - MIN_CONFIDENCE
            adaptive: AdaptiveDetection instance (optional, for feedback loop)
        """
        self.logger = get_logger(__name__)
        self.adaptive = adaptive or AdaptiveDetection()
        
        # Engine weights (from config)
        self.weights = {
            EngineType.ML: float(config.get('ML_WEIGHT', 0.5)),
            EngineType.RULES: float(config.get('RULES_WEIGHT', 0.3)),
            EngineType.MATH: float(config.get('MATH_WEIGHT', 0.2)),
        }
        
        # Base thresholds (can be overridden by adaptive)
        self.base_thresholds = {
            ThreatType.DDOS: float(config.get('DDOS_THRESHOLD', 0.7)),
            ThreatType.DOS: float(config.get('DDOS_THRESHOLD', 0.7)),
            ThreatType.MALWARE: float(config.get('MALWARE_THRESHOLD', 0.85)),
            ThreatType.ANOMALY: float(config.get('ANOMALY_THRESHOLD', 0.75)),
            ThreatType.NETWORK_INTRUSION: float(config.get('ANOMALY_THRESHOLD', 0.75)),
            ThreatType.POLICY_VIOLATION: float(config.get('ANOMALY_THRESHOLD', 0.75)),
        }
        
        # Minimum confidence for publishing
        self.min_confidence = float(config.get('MIN_CONFIDENCE', 0.85))
        
        # Trust scores (learned from feedback, start equal)
        self.trust_scores = {
            EngineType.ML: 1.0,
            EngineType.RULES: 0.9,
            EngineType.MATH: 0.8,
        }
        
        self.logger.info(
            f"Initialized EnsembleVoter: weights={self.weights}, "
            f"min_confidence={self.min_confidence}, "
            f"adaptive={'enabled' if adaptive else 'disabled'}"
        )
    
    def decide(self, candidates: List[DetectionCandidate]) -> EnsembleDecision:
        """
        Make ensemble decision with abstention logic.
        
        Process:
        1. Group candidates by threat_type
        2. Vote on each threat_type (weighted voting)
        3. Compute LLR, confidence
        4. Apply abstention logic
        5. Return best decision
        
        Args:
            candidates: List of DetectionCandidate from all engines
        
        Returns:
            EnsembleDecision with should_publish flag
        """
        if not candidates:
            return EnsembleDecision(
                should_publish=False,
                threat_type=ThreatType.ANOMALY,
                final_score=0.0,
                confidence=0.0,
                llr=0.0,
                candidates=[],
                abstention_reason='no_candidates'
            )
        
        # Group by threat type
        by_threat = {}
        for c in candidates:
            by_threat.setdefault(c.threat_type, []).append(c)
        
        # Vote on each threat type
        decisions = []
        for threat_type, threat_candidates in by_threat.items():
            decision = self._vote_on_threat(threat_type, threat_candidates)
            decisions.append(decision)
        
        # Pick highest score
        best_decision = max(decisions, key=lambda d: d.final_score)
        
        # Apply abstention logic with adaptive threshold
        threat_type_str = best_decision.threat_type.value if best_decision.threat_type else "anomaly"
        base_threshold = self.base_thresholds.get(best_decision.threat_type, 0.7)
        threshold = self.adaptive.get_threshold(threat_type_str, base_threshold)
        
        if best_decision.confidence < self.min_confidence:
            best_decision.should_publish = False
            best_decision.abstention_reason = (
                f'confidence_too_low_{best_decision.confidence:.2f}_<_{self.min_confidence}'
            )
        elif best_decision.final_score < threshold:
            best_decision.should_publish = False
            best_decision.abstention_reason = (
                f'score_below_threshold_{best_decision.final_score:.2f}_<_{threshold}'
            )
        else:
            best_decision.should_publish = True
            best_decision.abstention_reason = None
        
        return best_decision
    
    def _vote_on_threat(
        self,
        threat_type: ThreatType,
        candidates: List[DetectionCandidate]
    ) -> EnsembleDecision:
        """
        Vote on single threat type using weighted ensemble formulas.
        
        Formulas applied:
        - #9: Weighted voting
        - #4: Trust-weighted confidence
        - #12: Uncertainty-aware weighting
        - #11: LLR
        
        Args:
            threat_type: Threat type
            candidates: Candidates for this threat
        
        Returns:
            EnsembleDecision (should_publish set by caller)
        """
        # Formula #12: Uncertainty-aware weighting
        # weight_i = (engine_weight × trust) / (uncertainty + ε)
        adjusted_weights = []
        scores = []
        confidences = []
        
        for c in candidates:
            engine_weight = self.weights.get(c.engine_type, 0.1)
            trust = self.trust_scores.get(c.engine_type, 0.5)
            uncertainty = 1.0 - c.confidence
            
            # Adjusted weight (higher for confident predictions)
            adjusted_weight = (engine_weight * trust) / (uncertainty + 0.1)
            adjusted_weights.append(adjusted_weight)
            scores.append(c.calibrated_score)
            confidences.append(c.confidence)
        
        # Formula #9: Weighted voting
        # final_score = Σ(weight × score) / Σ(weight)
        total_weight = sum(adjusted_weights)
        if total_weight == 0:
            total_weight = 1e-10
        
        final_score = sum(w * s for w, s in zip(adjusted_weights, scores)) / total_weight
        
        # Formula #4: Trust-weighted confidence
        # confidence = Σ(trust_i × conf_i) / Σ(trust_i)
        trust_values = [self.trust_scores.get(c.engine_type, 0.5) for c in candidates]
        total_trust = sum(trust_values)
        if total_trust == 0:
            total_trust = 1.0
        
        final_confidence = sum(t * conf for t, conf in zip(trust_values, confidences)) / total_trust
        
        # Formula #11: Log-likelihood ratio
        llr = self.compute_llr(final_score)
        
        return EnsembleDecision(
            should_publish=False,  # Set by caller after abstention check
            threat_type=threat_type,
            final_score=final_score,
            confidence=final_confidence,
            llr=llr,
            candidates=candidates,
            abstention_reason='pending_decision',
            metadata={
                'adjusted_weights': [float(w) for w in adjusted_weights],
                'engine_types': [c.engine_type.value for c in candidates],
                'num_engines': len(set(c.engine_type for c in candidates))
            }
        )
    
    def compute_llr(self, calibrated_score: float) -> float:
        """
        Formula #11: Log-Likelihood Ratio
        LLR = log(P(E|malicious) / P(E|benign))
            = log(score / (1 - score))
        
        Args:
            calibrated_score: Calibrated probability [0,1]
        
        Returns:
            LLR value (positive = evidence for malicious)
        """
        # Clip to avoid log(0)
        score = np.clip(calibrated_score, 1e-10, 1 - 1e-10)
        llr = np.log(score / (1 - score))
        return float(llr)
    
    def compute_bft_resilience(self, num_nodes: int, max_faulty: int) -> float:
        """
        Formula #5: BFT Resilience
        resilience = 1 - f/(3f+1)
        
        Byzantine Fault Tolerance formula.
        
        Args:
            num_nodes: Total nodes in cluster
            max_faulty: Maximum faulty nodes tolerated (f)
        
        Returns:
            Resilience score [0,1]
        """
        if max_faulty == 0:
            return 1.0
        
        resilience = 1 - (max_faulty / (3 * max_faulty + 1))
        return float(resilience)
    
    def calibrate_threshold_fbeta(
        self,
        threat_type: ThreatType,
        beta: float,
        y_true: np.ndarray,
        y_scores: np.ndarray
    ) -> float:
        """
        Formula #7: F-beta score threshold calibration
        F_β = (1+β²) × (precision × recall) / (β² × precision + recall)
        
        Find threshold that maximizes F-beta:
        - β=2 for DDoS (favor recall)
        - β=0.5 for malware (favor precision)
        
        Args:
            threat_type: Threat type to calibrate
            beta: Beta parameter (2=recall, 0.5=precision)
            y_true: Ground truth labels [0,1]
            y_scores: Predicted scores [0,1]
        
        Returns:
            Optimal threshold
        """
        best_threshold = 0.5
        best_fbeta = 0.0
        
        for threshold in np.arange(0.1, 0.95, 0.05):
            y_pred = (y_scores >= threshold).astype(int)
            
            # Compute precision and recall
            tp = np.sum((y_pred == 1) & (y_true == 1))
            fp = np.sum((y_pred == 1) & (y_true == 0))
            fn = np.sum((y_pred == 0) & (y_true == 1))
            
            precision = tp / (tp + fp + 1e-10)
            recall = tp / (tp + fn + 1e-10)
            
            # F-beta score
            fbeta = ((1 + beta**2) * precision * recall) / (beta**2 * precision + recall + 1e-10)
            
            if fbeta > best_fbeta:
                best_fbeta = fbeta
                best_threshold = threshold
        
        # Update threshold
        self.thresholds[threat_type] = best_threshold
        self.logger.info(
            f"Calibrated {threat_type.value} threshold: {best_threshold:.2f} "
            f"(F{beta}={best_fbeta:.3f})"
        )
        
        return best_threshold
    
    def update_weights(self, feedback: Dict[EngineType, bool]):
        """
        Update engine trust scores based on feedback.
        
        Called from handle_reputation_event() when backend confirms/rejects detection.
        
        Args:
            feedback: {EngineType: was_correct}
        """
        for engine_type, correct in feedback.items():
            current_trust = self.trust_scores.get(engine_type, 0.5)
            
            if correct:
                # Increase trust (max 1.0)
                new_trust = min(1.0, current_trust * 1.1)
            else:
                # Decrease trust (min 0.1)
                new_trust = max(0.1, current_trust * 0.9)
            
            self.trust_scores[engine_type] = new_trust
        
        self.logger.info(f"Updated trust scores: {self.trust_scores}")
    
    def get_metadata(self) -> dict:
        """Get ensemble metadata for instrumentation."""
        return {
            'weights': {k.value: v for k, v in self.weights.items()},
            'base_thresholds': {k.value: v for k, v in self.base_thresholds.items()},
            'trust_scores': {k.value: v for k, v in self.trust_scores.items()},
            'min_confidence': self.min_confidence,
            'adaptive_enabled': self.adaptive.feedback_service is not None
        }
