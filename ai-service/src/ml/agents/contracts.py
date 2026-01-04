"""Shared detection contracts and canonical schemas."""

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
    """Event modality."""
    NETWORK_FLOW = "network_flow"
    FILE = "file"
    DNS = "dns"
    PROXY = "proxy"


@dataclass
class DetectionCandidate:
    """Detection candidate from any agent."""
    agent_id: str
    signal_id: str
    threat_type: ThreatType
    raw_score: float
    calibrated_score: float
    confidence: float
    features: Dict[str, float] = field(default_factory=dict)
    findings: List[str] = field(default_factory=list)
    indicators: List[Dict[str, Any]] = field(default_factory=list)
    metadata: Dict[str, Any] = field(default_factory=dict)
    
    def __post_init__(self):
        if not (0.0 <= self.raw_score <= 1.0):
            raise ValueError(f"raw_score must be in [0,1], got {self.raw_score}")
        if not (0.0 <= self.calibrated_score <= 1.0):
            raise ValueError(f"calibrated_score must be in [0,1], got {self.calibrated_score}")
        if not (0.0 <= self.confidence <= 1.0):
            raise ValueError(f"confidence must be in [0,1], got {self.confidence}")
    
    def to_dict(self) -> Dict[str, Any]:
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
    """Final ensemble decision from weighted voting."""
    should_publish: bool
    threat_type: ThreatType
    final_score: float
    confidence: float
    candidates: List[DetectionCandidate]
    llr: float = 0.0
    abstention_reason: Optional[str] = None
    profile: Optional[str] = None
    metadata: Dict[str, Any] = field(default_factory=dict)
    
    def __post_init__(self):
        if not (0.0 <= self.final_score <= 1.0):
            raise ValueError(f"final_score must be in [0,1], got {self.final_score}")
        if not (0.0 <= self.confidence <= 1.0):
            raise ValueError(f"confidence must be in [0,1], got {self.confidence}")
        if self.should_publish and self.abstention_reason:
            raise ValueError("Cannot publish with abstention_reason set")
        if not self.should_publish and not self.abstention_reason:
            raise ValueError("Must provide abstention_reason if not publishing")
    
    def to_dict(self) -> Dict[str, Any]:
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
    """Universal event envelope for detection pipelines."""
    id: str
    timestamp: float
    source: str
    tenant_id: str
    modality: Modality
    features_version: str
    features: Dict[str, Any]
    raw_context: Dict[str, Any] = field(default_factory=dict)
    labels: Dict[str, str] = field(default_factory=dict)
    
    def to_dict(self) -> Dict[str, Any]:
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


@dataclass
class NetworkFlowFeaturesV1:
    """Canonical schema for network flow features."""
    src_ip: str
    dst_ip: str
    src_port: int
    dst_port: int
    protocol: int
    tot_fwd_pkts: int
    tot_bwd_pkts: int
    totlen_fwd_pkts: int
    totlen_bwd_pkts: int
    flow_duration: float
    flow_byts_s: float
    flow_pkts_s: float
    
    # Optional fields with defaults
    flow_id: Optional[str] = None
    fwd_pkts_s: float = 0.0
    bwd_pkts_s: float = 0.0
    fwd_pkt_len_max: float = 0.0
    fwd_pkt_len_min: float = 0.0
    fwd_pkt_len_mean: float = 0.0
    fwd_pkt_len_std: float = 0.0
    bwd_pkt_len_max: float = 0.0
    bwd_pkt_len_min: float = 0.0
    bwd_pkt_len_mean: float = 0.0
    bwd_pkt_len_std: float = 0.0
    pkt_len_min: float = 0.0
    pkt_len_max: float = 0.0
    pkt_len_mean: float = 0.0
    pkt_len_std: float = 0.0
    pkt_len_var: float = 0.0
    flow_iat_mean: float = 0.0
    flow_iat_std: float = 0.0
    flow_iat_max: float = 0.0
    flow_iat_min: float = 0.0
    fwd_iat_tot: float = 0.0
    fwd_iat_mean: float = 0.0
    fwd_iat_std: float = 0.0
    fwd_iat_max: float = 0.0
    fwd_iat_min: float = 0.0
    bwd_iat_tot: float = 0.0
    bwd_iat_mean: float = 0.0
    bwd_iat_std: float = 0.0
    bwd_iat_max: float = 0.0
    bwd_iat_min: float = 0.0
    fin_flag_cnt: int = 0
    syn_flag_cnt: int = 0
    rst_flag_cnt: int = 0
    psh_flag_cnt: int = 0
    ack_flag_cnt: int = 0
    urg_flag_cnt: int = 0
    cwe_flag_count: int = 0
    ece_flag_cnt: int = 0
    fwd_psh_flags: int = 0
    bwd_psh_flags: int = 0
    fwd_urg_flags: int = 0
    bwd_urg_flags: int = 0
    fwd_header_len: int = 0
    bwd_header_len: int = 0
    down_up_ratio: float = 0.0
    pkt_size_avg: float = 0.0
    fwd_seg_size_avg: float = 0.0
    bwd_seg_size_avg: float = 0.0
    fwd_byts_b_avg: float = 0.0
    fwd_pkts_b_avg: float = 0.0
    fwd_blk_rate_avg: float = 0.0
    bwd_byts_b_avg: float = 0.0
    bwd_pkts_b_avg: float = 0.0
    bwd_blk_rate_avg: float = 0.0
    subflow_fwd_pkts: int = 0
    subflow_fwd_byts: int = 0
    subflow_bwd_pkts: int = 0
    subflow_bwd_byts: int = 0
    init_fwd_win_byts: int = 0
    init_bwd_win_byts: int = 0
    fwd_act_data_pkts: int = 0
    fwd_seg_size_min: int = 0
    active_mean: float = 0.0
    active_std: float = 0.0
    active_max: float = 0.0
    active_min: float = 0.0
    idle_mean: float = 0.0
    idle_std: float = 0.0
    idle_max: float = 0.0
    idle_min: float = 0.0
    pps: Optional[float] = None
    bps: Optional[float] = None
    syn_ack_ratio: Optional[float] = None
    
    def to_dict(self) -> Dict[str, Any]:
        return {k: v for k, v in self.__dict__.items() if v is not None}
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "NetworkFlowFeaturesV1":
        return cls(**{k: v for k, v in data.items() if k in cls.__dataclass_fields__})


@dataclass
class FileFeaturesV1:
    """
    Canonical schema for file-based analysis features (V1).
    
    Combines static analysis, strings, YARA, threat intel, and ML scores.
    Mirrors sentinel.contracts.schemas.FileFeaturesV1 for cross-system compatibility.
    """
    # Identity (Required)
    sha256: str
    file_name: str
    file_size: int
    file_type: str
    
    # Static Analysis (Required)
    entropy: float
    
    # Identity (Optional)
    sha1: Optional[str] = None
    md5: Optional[str] = None
    
    # Static Analysis (Optional)
    section_count: int = 0
    section_entropy_max: float = 0.0
    section_entropy_mean: float = 0.0
    import_count: int = 0
    export_count: int = 0
    
    # Strings Analysis (Optional)
    strings_count: int = 0
    strings_suspicious_count: int = 0
    strings_score_command_exec: float = 0.0
    strings_score_cred_theft: float = 0.0
    strings_score_download_exec: float = 0.0
    strings_score_persistence: float = 0.0
    strings_score_obfuscation: float = 0.0
    
    # YARA (Optional)
    yara_match_count: int = 0
    yara_rule_names: List[str] = field(default_factory=list)
    yara_score_packers: float = 0.0
    yara_score_exploits: float = 0.0
    
    # Threat Intelligence (Optional)
    ti_ip_count: int = 0
    ti_ip_malicious_count: int = 0
    ti_domain_count: int = 0
    ti_domain_malicious_count: int = 0
    ti_url_count: int = 0
    ti_url_malicious_count: int = 0
    ti_is_c2: bool = False
    ti_is_botnet: bool = False
    ti_abuse_score_max: float = 0.0
    ti_crowdsec_behaviors: List[str] = field(default_factory=list)
    
    # ML Scores (Optional)
    ml_pe_score: float = 0.0
    ml_api_score: float = 0.0
    
    def __post_init__(self):
        if not self.sha256 or len(self.sha256) != 64:
            raise ValueError(f"sha256 must be 64 hex characters, got '{self.sha256}'")
        if self.file_size < 0:
            raise ValueError(f"file_size cannot be negative, got {self.file_size}")
        if self.entropy < 0.0 or self.entropy > 8.0:
            raise ValueError(f"entropy must be in [0, 8], got {self.entropy}")
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for serialization."""
        result = {}
        for k, v in self.__dict__.items():
            if isinstance(v, list):
                result[k] = v.copy()
            else:
                result[k] = v
        return result
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "FileFeaturesV1":
        """Create from dictionary."""
        return cls(**{
            k: v for k, v in data.items()
            if k in cls.__dataclass_fields__
        })
    
    @classmethod
    def required_fields(cls) -> List[str]:
        """Return list of required field names."""
        return ["sha256", "file_name", "file_size", "file_type", "entropy"]


@dataclass
class DNSFeaturesV1:
    """
    Canonical schema for DNS-based analysis features (V1).
    
    Used for DNS anomaly detection, DGA detection, and threat intel enrichment.
    """
    # Required fields
    query_name: str
    query_type: str
    response_code: int
    
    # Domain analysis
    domain_length: int = 0
    domain_entropy: float = 0.0
    subdomain_count: int = 0
    subdomain_length_max: int = 0
    subdomain_length_mean: float = 0.0
    
    # Character analysis
    digit_ratio: float = 0.0
    consonant_ratio: float = 0.0
    vowel_ratio: float = 0.0
    special_char_count: int = 0
    
    # N-gram features
    bigram_avg_rank: float = 0.0
    trigram_avg_rank: float = 0.0
    
    # Query patterns
    query_count_1min: int = 0
    query_count_5min: int = 0
    unique_subdomains_1min: int = 0
    nxdomain_count_1min: int = 0
    
    # Response features
    ttl_min: int = 0
    ttl_max: int = 0
    answer_count: int = 0
    
    # Context
    client_ip: Optional[str] = None
    server_ip: Optional[str] = None
    timestamp: Optional[float] = None
    
    # Threat Intel
    ti_is_known_malicious: bool = False
    ti_is_dga: bool = False
    ti_category: Optional[str] = None
    
    def __post_init__(self):
        if not self.query_name:
            raise ValueError("query_name is required")
        if self.domain_entropy < 0.0:
            raise ValueError(f"domain_entropy cannot be negative, got {self.domain_entropy}")
        if self.domain_length < 0:
            raise ValueError(f"domain_length cannot be negative, got {self.domain_length}")
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for serialization."""
        return {k: v for k, v in self.__dict__.items() if v is not None}
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "DNSFeaturesV1":
        """Create from dictionary."""
        return cls(**{
            k: v for k, v in data.items()
            if k in cls.__dataclass_fields__
        })
    
    @classmethod
    def required_fields(cls) -> List[str]:
        """Return list of required field names."""
        return ["query_name", "query_type", "response_code"]
