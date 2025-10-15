"""
Data structures for ML detection pipeline.

Security-first, military-grade type definitions.
"""

from dataclasses import dataclass, field
from typing import Dict, List, Optional, Any
from enum import Enum


class ThreatType(str, Enum):
    """Supported threat detection types."""
    DDOS = "ddos"
    DOS = "dos"
    MALWARE = "malware"
    ANOMALY = "anomaly"
    NETWORK_INTRUSION = "network_intrusion"
    POLICY_VIOLATION = "policy_violation"


class EngineType(str, Enum):
    """Detection engine types."""
    ML = "ml"
    RULES = "rules"
    MATH = "math"


@dataclass
class DetectionCandidate:
    """
    Detection candidate from a single engine.
    
    Attributes:
        threat_type: Type of threat detected
        raw_score: Uncalibrated model output [0,1]
        calibrated_score: Platt/Isotonic calibrated probability [0,1]
        confidence: Model uncertainty estimate [0,1], higher = more certain
        engine_type: Which engine produced this candidate
        engine_name: Specific model/rule/metric name
        features: Top-N contributing features for explainability
        metadata: Additional context (model version, timestamps, etc.)
    """
    threat_type: ThreatType
    raw_score: float
    calibrated_score: float
    confidence: float
    engine_type: EngineType
    engine_name: str
    features: Dict[str, float] = field(default_factory=dict)
    metadata: Dict[str, Any] = field(default_factory=dict)
    
    def __post_init__(self):
        """Validate score ranges."""
        if not (0.0 <= self.raw_score <= 1.0):
            raise ValueError(f"raw_score must be in [0,1], got {self.raw_score}")
        if not (0.0 <= self.calibrated_score <= 1.0):
            raise ValueError(f"calibrated_score must be in [0,1], got {self.calibrated_score}")
        if not (0.0 <= self.confidence <= 1.0):
            raise ValueError(f"confidence must be in [0,1], got {self.confidence}")


@dataclass
class EnsembleDecision:
    """
    Final decision from ensemble voting.
    
    Attributes:
        should_publish: Whether to publish to backend (passed abstention logic)
        threat_type: Detected threat type
        final_score: Weighted ensemble score [0,1]
        confidence: Ensemble confidence [0,1]
        llr: Log-likelihood ratio (evidence strength)
        candidates: All detection candidates that contributed
        abstention_reason: Why we didn't publish (if should_publish=False)
        metadata: Voting weights, thresholds used, etc.
    """
    should_publish: bool
    threat_type: ThreatType
    final_score: float
    confidence: float
    llr: float
    candidates: List[DetectionCandidate]
    abstention_reason: Optional[str] = None
    metadata: Dict[str, Any] = field(default_factory=dict)
    
    def __post_init__(self):
        """Validate decision consistency."""
        if not (0.0 <= self.final_score <= 1.0):
            raise ValueError(f"final_score must be in [0,1], got {self.final_score}")
        if not (0.0 <= self.confidence <= 1.0):
            raise ValueError(f"confidence must be in [0,1], got {self.confidence}")
        if self.should_publish and self.abstention_reason:
            raise ValueError("Cannot publish with abstention_reason set")
        if not self.should_publish and not self.abstention_reason:
            raise ValueError("Must provide abstention_reason if not publishing")


@dataclass
class InstrumentedResult:
    """
    Pipeline result with full instrumentation.
    
    Attributes:
        decision: Ensemble decision (None if pipeline failed)
        latency_ms: Per-stage latency in milliseconds
        total_latency_ms: Total pipeline latency
        feature_count: Number of features extracted
        candidate_count: Number of detection candidates
        error: Error message if pipeline failed
    """
    decision: Optional[EnsembleDecision]
    latency_ms: Dict[str, float]
    total_latency_ms: float
    feature_count: int = 0
    candidate_count: int = 0
    error: Optional[str] = None
    
    def __post_init__(self):
        """Validate latency consistency."""
        if self.total_latency_ms < 0:
            raise ValueError(f"total_latency_ms cannot be negative: {self.total_latency_ms}")
        
        # Validate stage latencies are non-negative
        for stage, latency in self.latency_ms.items():
            if latency < 0:
                raise ValueError(f"Stage '{stage}' latency cannot be negative: {latency}")
    
    @property
    def met_latency_target(self, target_ms: float = 50.0) -> bool:
        """Check if pipeline met latency target."""
        return self.total_latency_ms <= target_ms
    
    @property
    def is_success(self) -> bool:
        """Check if pipeline succeeded."""
        return self.error is None


@dataclass
class TelemetryData:
    """
    Raw telemetry data for feature extraction.
    
    Supports network flows (NetFlow/IPFIX) and file/endpoint data.
    """
    # Network flow data
    flows: List[Dict[str, Any]] = field(default_factory=list)
    
    # File/endpoint data (for malware detection)
    files: List[Dict[str, Any]] = field(default_factory=list)
    
    # Metadata
    timestamp: float = 0.0
    source_node: str = ""
    
    def has_network_data(self) -> bool:
        """Check if contains network flow data."""
        return len(self.flows) > 0
    
    def has_file_data(self) -> bool:
        """Check if contains file/malware data."""
        return len(self.files) > 0
