"""Unified detection types and data contracts."""

from dataclasses import dataclass, field
from enum import Enum
from typing import Dict, List, Optional, Any


class ThreatType(str, Enum):
    """Threat classification categories."""
    DDOS = "ddos"
    DOS = "dos"
    MALWARE = "malware"
    ANOMALY = "anomaly"
    NETWORK_INTRUSION = "network_intrusion"
    POLICY_VIOLATION = "policy_violation"
    BOTNET = "botnet"
    C2_COMMUNICATION = "c2"


class Modality(str, Enum):
    """Event modality - what type of data is being analyzed."""
    NETWORK_FLOW = "network_flow"
    FILE = "file"
    DNS = "dns"
    PROXY = "proxy"
    PROCESS_EVENT = "process_event"
    AUTH_EVENT = "auth_event"
    CLOUD_EVENT = "cloud_event"
    SCAN_FINDINGS = "scan_findings"
    ACTION_EVENT = "action_event"
    MCP_RUNTIME = "mcp_runtime"
    EXFIL_EVENT = "exfil_event"
    RESILIENCE_EVENT = "resilience_event"


@dataclass
class DetectionCandidate:
    """
    Detection candidate from any agent (CyberMesh engine or Sentinel provider).
    
    This is the universal detection output format that both systems produce.
    
    Attributes:
        agent_id: Hierarchical agent identifier (e.g., "sentinel.ml.malware_pe")
        signal_id: Specific signal/model name (e.g., "entropy_analyzer")
        threat_type: Type of threat detected
        raw_score: Uncalibrated model output [0,1]
        calibrated_score: Calibrated probability [0,1]
        confidence: Certainty estimate [0,1], higher = more certain
        features: Top contributing features for explainability
        findings: Human-readable findings
        indicators: IOCs (IPs, domains, hashes, etc.)
        metadata: Additional context (model version, latency, etc.)
    """
    # Identity
    agent_id: str
    signal_id: str
    
    # Detection
    threat_type: ThreatType
    raw_score: float
    calibrated_score: float
    confidence: float
    
    # Explainability
    features: Dict[str, float] = field(default_factory=dict)
    findings: List[str] = field(default_factory=list)
    indicators: List[Dict[str, Any]] = field(default_factory=list)
    
    # Metadata
    metadata: Dict[str, Any] = field(default_factory=dict)
    
    def __post_init__(self):
        """Validate score ranges."""
        if not (0.0 <= self.raw_score <= 1.0):
            raise ValueError(f"raw_score must be in [0,1], got {self.raw_score}")
        if not (0.0 <= self.calibrated_score <= 1.0):
            raise ValueError(f"calibrated_score must be in [0,1], got {self.calibrated_score}")
        if not (0.0 <= self.confidence <= 1.0):
            raise ValueError(f"confidence must be in [0,1], got {self.confidence}")
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary."""
        return {
            "agent_id": self.agent_id,
            "signal_id": self.signal_id,
            "threat_type": self.threat_type.value,
            "raw_score": self.raw_score,
            "calibrated_score": self.calibrated_score,
            "confidence": self.confidence,
            "features": self.features,
            "findings": self.findings,
            "indicators": self.indicators,
            "metadata": self.metadata,
        }


@dataclass
class EnsembleDecision:
    """
    Final ensemble decision from weighted voting across all agents.
    
    Attributes:
        should_publish: Whether to publish to backend (passed abstention logic)
        threat_type: Detected threat type
        final_score: Weighted ensemble score [0,1]
        confidence: Ensemble confidence [0,1]
        candidates: All detection candidates that contributed
        llr: Log-likelihood ratio (evidence strength)
        abstention_reason: Why we didn't publish (if should_publish=False)
        profile: Detection profile used ("security_first", "balanced", "low_fp")
        metadata: Voting weights, thresholds used, etc.
    """
    # Decision
    should_publish: bool
    threat_type: ThreatType
    final_score: float
    confidence: float
    
    # Evidence
    candidates: List[DetectionCandidate]
    llr: float = 0.0
    
    # Abstention
    abstention_reason: Optional[str] = None
    
    # Profile
    profile: Optional[str] = None
    
    # Metadata
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
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary."""
        return {
            "should_publish": self.should_publish,
            "threat_type": self.threat_type.value,
            "final_score": self.final_score,
            "confidence": self.confidence,
            "candidates": [c.to_dict() for c in self.candidates],
            "llr": self.llr,
            "abstention_reason": self.abstention_reason,
            "profile": self.profile,
            "metadata": self.metadata,
        }


@dataclass
class CanonicalEvent:
    """
    Universal event envelope for all detection pipelines.
    
    Attributes:
        id: Unique event identifier (UUID)
        timestamp: Unix epoch timestamp (seconds)
        source: Event source identifier (e.g., "postgres_ddos", "sentinel_file_agent")
        tenant_id: Tenant identifier (required for multi-tenant isolation)
        modality: Event modality (network_flow, file, dns, proxy)
        features_version: Schema version (e.g., "NetworkFlowFeaturesV1")
        features: Canonical features per schema
        raw_context: Original record/additional context
        labels: Labels for training/evaluation (optional)
    """
    # Identity
    id: str
    timestamp: float
    
    # Source
    source: str
    tenant_id: str
    
    # Modality
    modality: Modality
    features_version: str
    
    # Payload
    features: Dict[str, Any]
    raw_context: Dict[str, Any] = field(default_factory=dict)
    
    # Optional
    labels: Dict[str, str] = field(default_factory=dict)
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary."""
        return {
            "id": self.id,
            "timestamp": self.timestamp,
            "source": self.source,
            "tenant_id": self.tenant_id,
            "modality": self.modality.value,
            "features_version": self.features_version,
            "features": self.features,
            "raw_context": self.raw_context,
            "labels": self.labels,
        }
