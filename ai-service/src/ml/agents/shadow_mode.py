"""Shadow mode infrastructure for comparing legacy vs agent pipeline."""

import json
import time
from dataclasses import dataclass, field
from typing import Dict, List, Optional, Any
from collections import defaultdict

from .contracts import EnsembleDecision, CanonicalEvent, ThreatType
from ...logging import get_logger

logger = get_logger(__name__)


@dataclass
class ShadowComparison:
    """Comparison record between legacy and agent pipeline decisions."""
    event_id: str
    timestamp: float
    tenant_id: str
    modality: str
    
    # Legacy pipeline decision
    legacy_threat: Optional[str]
    legacy_score: float
    legacy_confidence: float
    legacy_publish: bool
    
    # Agent pipeline decision
    agent_threat: Optional[str]
    agent_score: float
    agent_confidence: float
    agent_publish: bool
    agent_candidates_count: int
    agents_contributing: List[str]
    
    # Latencies
    legacy_latency_ms: float
    agent_latency_ms: float
    
    # Computed differences
    score_delta: float = 0.0
    publish_match: bool = True
    threat_match: bool = True
    
    def __post_init__(self):
        self.score_delta = abs(self.legacy_score - self.agent_score)
        self.publish_match = self.legacy_publish == self.agent_publish
        self.threat_match = self.legacy_threat == self.agent_threat
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "event_id": self.event_id,
            "timestamp": self.timestamp,
            "tenant_id": self.tenant_id,
            "modality": self.modality,
            "legacy": {
                "threat": self.legacy_threat,
                "score": self.legacy_score,
                "confidence": self.legacy_confidence,
                "publish": self.legacy_publish,
                "latency_ms": self.legacy_latency_ms,
            },
            "agent": {
                "threat": self.agent_threat,
                "score": self.agent_score,
                "confidence": self.agent_confidence,
                "publish": self.agent_publish,
                "latency_ms": self.agent_latency_ms,
                "candidates_count": self.agent_candidates_count,
            },
            "agents_contributing": self.agents_contributing,
            "score_delta": self.score_delta,
            "publish_match": self.publish_match,
            "threat_match": self.threat_match,
        }
    
    def to_json(self) -> str:
        return json.dumps(self.to_dict())


@dataclass
class ShadowMetrics:
    """Aggregated metrics for shadow mode evaluation."""
    
    # Counts
    total_events: int = 0
    publish_matches: int = 0
    threat_matches: int = 0
    
    # FP/FN tracking
    agent_extra_detections: int = 0  # Agent detects, legacy doesn't
    agent_missed_detections: int = 0  # Legacy detects, agent doesn't
    
    # Score statistics
    score_deltas: List[float] = field(default_factory=list)
    legacy_scores: List[float] = field(default_factory=list)
    agent_scores: List[float] = field(default_factory=list)
    
    # Latency statistics
    legacy_latencies: List[float] = field(default_factory=list)
    agent_latencies: List[float] = field(default_factory=list)
    
    # Per-threat-type tracking
    per_threat_counts: Dict[str, int] = field(default_factory=lambda: defaultdict(int))
    per_agent_counts: Dict[str, int] = field(default_factory=lambda: defaultdict(int))
    
    def update(self, comparison: ShadowComparison):
        """Update metrics with a new comparison."""
        self.total_events += 1
        
        if comparison.publish_match:
            self.publish_matches += 1
        
        if comparison.threat_match:
            self.threat_matches += 1
        
        # FP/FN tracking
        if comparison.agent_publish and not comparison.legacy_publish:
            self.agent_extra_detections += 1
        elif comparison.legacy_publish and not comparison.agent_publish:
            self.agent_missed_detections += 1
        
        # Score tracking
        self.score_deltas.append(comparison.score_delta)
        self.legacy_scores.append(comparison.legacy_score)
        self.agent_scores.append(comparison.agent_score)
        
        # Latency tracking
        self.legacy_latencies.append(comparison.legacy_latency_ms)
        self.agent_latencies.append(comparison.agent_latency_ms)
        
        # Per-threat counts
        if comparison.agent_threat:
            self.per_threat_counts[comparison.agent_threat] += 1
        
        # Per-agent counts
        for agent_id in comparison.agents_contributing:
            self.per_agent_counts[agent_id] += 1
    
    def get_summary(self) -> Dict[str, Any]:
        """Get summary statistics."""
        if not self.total_events:
            return {"total_events": 0}
        
        def safe_avg(values: List[float]) -> float:
            return sum(values) / len(values) if values else 0.0
        
        def safe_p95(values: List[float]) -> float:
            if not values:
                return 0.0
            sorted_vals = sorted(values)
            idx = int(len(sorted_vals) * 0.95)
            return sorted_vals[min(idx, len(sorted_vals) - 1)]
        
        return {
            "total_events": self.total_events,
            "publish_match_rate": self.publish_matches / self.total_events,
            "threat_match_rate": self.threat_matches / self.total_events,
            "agent_extra_detections": self.agent_extra_detections,
            "agent_missed_detections": self.agent_missed_detections,
            "score_delta_avg": safe_avg(self.score_deltas),
            "score_delta_max": max(self.score_deltas) if self.score_deltas else 0.0,
            "legacy_score_avg": safe_avg(self.legacy_scores),
            "agent_score_avg": safe_avg(self.agent_scores),
            "legacy_latency_avg_ms": safe_avg(self.legacy_latencies),
            "legacy_latency_p95_ms": safe_p95(self.legacy_latencies),
            "agent_latency_avg_ms": safe_avg(self.agent_latencies),
            "agent_latency_p95_ms": safe_p95(self.agent_latencies),
            "latency_overhead_avg_ms": safe_avg(self.agent_latencies) - safe_avg(self.legacy_latencies),
            "per_threat_counts": dict(self.per_threat_counts),
            "per_agent_counts": dict(self.per_agent_counts),
        }


class ShadowModeCollector:
    """
    Collects and logs shadow mode comparisons.
    
    Runs both legacy and agent pipelines on the same events,
    compares results, and logs metrics without affecting production.
    """
    
    def __init__(
        self,
        config: Optional[Dict] = None,
        log_sample_rate: float = 0.1,
    ):
        """
        Initialize collector.
        
        Args:
            config: Optional configuration
            log_sample_rate: Fraction of events to log detailed comparisons (0.0 to 1.0)
        """
        self._config = config or {}
        self._log_sample_rate = max(0.0, min(1.0, log_sample_rate))
        self._metrics = ShadowMetrics()
        self._sample_counter = 0
        self._enabled = True
    
    @property
    def metrics(self) -> ShadowMetrics:
        """Get current metrics."""
        return self._metrics
    
    def record_comparison(
        self,
        event: CanonicalEvent,
        legacy_decision: Optional[Dict[str, Any]],
        agent_decision: EnsembleDecision,
    ) -> ShadowComparison:
        """
        Record a comparison between legacy and agent pipeline decisions.
        
        Args:
            event: The canonical event that was processed
            legacy_decision: Legacy pipeline decision dict (or None if not run)
            agent_decision: Agent pipeline decision
            
        Returns:
            ShadowComparison record
        """
        # Extract legacy values (default to no detection)
        legacy_threat = None
        legacy_score = 0.0
        legacy_confidence = 0.0
        legacy_publish = False
        legacy_latency = 0.0
        
        if legacy_decision:
            legacy_threat = legacy_decision.get("threat_type")
            if hasattr(legacy_threat, "value"):
                legacy_threat = legacy_threat.value
            legacy_score = legacy_decision.get("score", 0.0)
            legacy_confidence = legacy_decision.get("confidence", 0.0)
            legacy_publish = legacy_decision.get("should_publish", False)
            legacy_latency = legacy_decision.get("latency_ms", 0.0)
        
        # Extract agent values
        agent_threat = agent_decision.threat_type.value if agent_decision.threat_type else None
        agents_contributing = list(set(
            c.agent_id for c in agent_decision.candidates
        ))
        
        comparison = ShadowComparison(
            event_id=event.id,
            timestamp=time.time(),
            tenant_id=event.tenant_id,
            modality=event.modality.value,
            legacy_threat=legacy_threat,
            legacy_score=legacy_score,
            legacy_confidence=legacy_confidence,
            legacy_publish=legacy_publish,
            agent_threat=agent_threat,
            agent_score=agent_decision.final_score,
            agent_confidence=agent_decision.confidence,
            agent_publish=agent_decision.should_publish,
            agent_candidates_count=len(agent_decision.candidates),
            agents_contributing=agents_contributing,
            legacy_latency_ms=legacy_latency,
            agent_latency_ms=agent_decision.metadata.get("latency_ms", 0.0),
        )
        
        # Update metrics
        self._metrics.update(comparison)
        
        # Log sample
        self._sample_counter += 1
        if self._should_log_detail():
            logger.info(
                "Shadow comparison",
                extra={"shadow_data": comparison.to_dict()}
            )
        
        return comparison
    
    def _should_log_detail(self) -> bool:
        """Determine if we should log detailed comparison."""
        if self._log_sample_rate <= 0:
            return False
        if self._log_sample_rate >= 1.0:
            return True
        # Sample based on counter
        return (self._sample_counter % int(1 / self._log_sample_rate)) == 0
    
    def get_summary(self) -> Dict[str, Any]:
        """Get summary of all collected metrics."""
        return self._metrics.get_summary()
    
    def reset(self):
        """Reset all collected metrics."""
        self._metrics = ShadowMetrics()
        self._sample_counter = 0
    
    def enable(self):
        """Enable shadow mode collection."""
        self._enabled = True
    
    def disable(self):
        """Disable shadow mode collection."""
        self._enabled = False
    
    @property
    def is_enabled(self) -> bool:
        """Check if shadow mode is enabled."""
        return self._enabled


class ShadowModePipeline:
    """
    Wrapper that runs both pipelines and collects shadow comparisons.
    
    This is the main integration point for shadow mode evaluation.
    Production decisions come from legacy pipeline only.
    """
    
    def __init__(
        self,
        agent_pipeline,
        collector: Optional[ShadowModeCollector] = None,
    ):
        """
        Initialize shadow mode pipeline.
        
        Args:
            agent_pipeline: DetectionAgentPipeline instance
            collector: Optional collector (created if not provided)
        """
        self._agent_pipeline = agent_pipeline
        self._collector = collector or ShadowModeCollector()
    
    @property
    def collector(self) -> ShadowModeCollector:
        """Get the shadow mode collector."""
        return self._collector
    
    def process_shadow(
        self,
        event: CanonicalEvent,
        legacy_decision: Optional[Dict[str, Any]] = None,
        profile: Optional[str] = None,
    ) -> EnsembleDecision:
        """
        Process event through agent pipeline and record shadow comparison.
        
        Args:
            event: Canonical event to process
            legacy_decision: Decision from legacy pipeline (for comparison)
            profile: Detection profile to use
            
        Returns:
            Agent pipeline decision (for logging/analysis only)
        """
        # Run agent pipeline
        agent_decision = self._agent_pipeline.process(event, profile=profile)
        
        # Record comparison
        if self._collector.is_enabled:
            self._collector.record_comparison(event, legacy_decision, agent_decision)
        
        return agent_decision
    
    def get_metrics(self) -> Dict[str, Any]:
        """Get shadow mode metrics."""
        return self._collector.get_summary()
