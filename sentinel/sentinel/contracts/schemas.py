"""Canonical feature schemas for detection pipelines."""

from dataclasses import dataclass, field
from typing import List, Optional, Dict, Any


@dataclass
class NetworkFlowFeaturesV1:
    """
    Canonical schema for network flow features (V1).
    
    Derived from CIC-DDoS2019 but dataset-agnostic.
    All 79 CIC features map directly to this schema.
    """
    # Identity (Required)
    src_ip: str
    dst_ip: str
    src_port: int
    dst_port: int
    protocol: int  # IANA protocol number (6=TCP, 17=UDP, 1=ICMP)
    
    # Volume (Required)
    tot_fwd_pkts: int
    tot_bwd_pkts: int
    totlen_fwd_pkts: int
    totlen_bwd_pkts: int
    
    # Timing (Required)
    flow_duration: float  # microseconds
    
    # Rates (Required)
    flow_byts_s: float
    flow_pkts_s: float
    
    # Identity (Optional)
    flow_id: Optional[str] = None
    flow_start_ts: Optional[float] = None
    flow_end_ts: Optional[float] = None
    
    # Rates (Optional)
    fwd_pkts_s: float = 0.0
    bwd_pkts_s: float = 0.0
    
    # Packet Length Stats (Optional)
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
    
    # Inter-Arrival Time Stats (Optional)
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
    
    # TCP Flags (Optional)
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
    
    # Header (Optional)
    fwd_header_len: int = 0
    bwd_header_len: int = 0
    
    # Derived/Ratio (Optional)
    down_up_ratio: float = 0.0
    pkt_size_avg: float = 0.0
    fwd_seg_size_avg: float = 0.0
    bwd_seg_size_avg: float = 0.0
    
    # Bulk Stats (Optional)
    fwd_byts_b_avg: float = 0.0
    fwd_pkts_b_avg: float = 0.0
    fwd_blk_rate_avg: float = 0.0
    bwd_byts_b_avg: float = 0.0
    bwd_pkts_b_avg: float = 0.0
    bwd_blk_rate_avg: float = 0.0
    
    # Subflow (Optional)
    subflow_fwd_pkts: int = 0
    subflow_fwd_byts: int = 0
    subflow_bwd_pkts: int = 0
    subflow_bwd_byts: int = 0
    
    # Window (Optional)
    init_fwd_win_byts: int = 0
    init_bwd_win_byts: int = 0
    fwd_act_data_pkts: int = 0
    fwd_seg_size_min: int = 0
    
    # Active/Idle (Optional)
    active_mean: float = 0.0
    active_std: float = 0.0
    active_max: float = 0.0
    active_min: float = 0.0
    idle_mean: float = 0.0
    idle_std: float = 0.0
    idle_max: float = 0.0
    idle_min: float = 0.0
    
    # Semantic (Derived, Optional)
    pps: Optional[float] = None  # packets per second
    bps: Optional[float] = None  # bytes per second
    syn_ack_ratio: Optional[float] = None
    unique_dst_ports_batch: Optional[int] = None
    port_entropy_batch: Optional[float] = None
    
    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for serialization."""
        return {
            k: v for k, v in self.__dict__.items()
            if v is not None
        }
    
    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "NetworkFlowFeaturesV1":
        """Create from dictionary."""
        return cls(**{
            k: v for k, v in data.items()
            if k in cls.__dataclass_fields__
        })
    
    @classmethod
    def required_fields(cls) -> List[str]:
        """Return list of required field names."""
        return [
            "src_ip", "dst_ip", "src_port", "dst_port", "protocol",
            "tot_fwd_pkts", "tot_bwd_pkts", "totlen_fwd_pkts", "totlen_bwd_pkts",
            "flow_duration", "flow_byts_s", "flow_pkts_s"
        ]


@dataclass
class FileFeaturesV1:
    """
    Canonical schema for file-based analysis features (V1).
    
    Combines static analysis, strings, YARA, threat intel, and ML scores.
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
class ProcessEventV1:
    """
    Canonical schema for process/runtime events (V1).
    """
    # Identity (Required)
    event_type: str
    process_name: str
    pid: int
    ppid: int
    user: str
    command_line: str

    # Optional context
    host: Optional[str] = None
    container_id: Optional[str] = None
    rule: Optional[str] = None
    severity: Optional[str] = None
    tags: List[str] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        """Convert to dictionary for serialization."""
        result: Dict[str, Any] = {}
        for k, v in self.__dict__.items():
            if isinstance(v, list):
                result[k] = v.copy()
            elif v is not None:
                result[k] = v
        return result

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ProcessEventV1":
        """Create from dictionary."""
        return cls(**{
            k: v for k, v in data.items()
            if k in cls.__dataclass_fields__
        })

    @classmethod
    def required_fields(cls) -> List[str]:
        """Return list of required field names."""
        return ["event_type", "process_name", "pid", "ppid", "user", "command_line"]


@dataclass
class ScanFindingV1:
    """Single scan finding from a rule/scanner."""
    rule_id: str
    rule_name: str
    severity: str
    description: str
    location: Optional[str] = None
    evidence: Optional[str] = None
    tags: List[str] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        result: Dict[str, Any] = {}
        for k, v in self.__dict__.items():
            if isinstance(v, list):
                result[k] = v.copy()
            elif v is not None:
                result[k] = v
        return result

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ScanFindingV1":
        return cls(**{
            k: v for k, v in data.items()
            if k in cls.__dataclass_fields__
        })


@dataclass
class ScanFindingsV1:
    """
    Canonical schema for scanner or rules findings (V1).
    """
    tool: str
    findings: List[ScanFindingV1] = field(default_factory=list)
    summary: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        result: Dict[str, Any] = {
            "tool": self.tool,
            "findings": [f.to_dict() for f in self.findings],
        }
        if self.summary is not None:
            result["summary"] = self.summary
        return result

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ScanFindingsV1":
        findings = [
            ScanFindingV1.from_dict(item)
            for item in data.get("findings", [])
            if isinstance(item, dict)
        ]
        return cls(
            tool=data.get("tool", ""),
            findings=findings,
            summary=data.get("summary"),
        )

    @classmethod
    def required_fields(cls) -> List[str]:
        return ["tool", "findings"]


@dataclass
class ActionEventV1:
    """
    Canonical schema for action/sequence events (V1).

    Designed for toolchain / sequence risk analysis.
    """
    # Required
    event_name: str

    # Optional event categorization (ECS-aligned)
    event_category: List[str] = field(default_factory=list)
    event_type: List[str] = field(default_factory=list)
    event_action: Optional[str] = None
    event_outcome: Optional[str] = None

    # Sequence context (optional but recommended)
    session_id: Optional[str] = None
    chain_id: Optional[str] = None
    step_id: Optional[str] = None
    step_index: Optional[int] = None

    # Actor / tool context
    actor_id: Optional[str] = None
    tool_name: Optional[str] = None
    tool_namespace: Optional[str] = None

    # Resource / destination
    resource: Optional[str] = None
    destination: Optional[str] = None
    bytes_out: Optional[int] = None
    bytes_in: Optional[int] = None

    # Additional structured context (OTel attributes-like)
    attributes: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        result: Dict[str, Any] = {}
        for k, v in self.__dict__.items():
            if v is None:
                continue
            result[k] = v if not isinstance(v, list) else v.copy()
        return result

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ActionEventV1":
        return cls(**{
            k: v for k, v in data.items()
            if k in cls.__dataclass_fields__
        })

    @classmethod
    def required_fields(cls) -> List[str]:
        return ["event_name"]


@dataclass
class MCPRuntimeEventV1:
    """
    Canonical schema for MCP runtime telemetry (V1).

    Captures JSON-RPC message details, tool calls, and responses.
    Aligned to MCP tooling primitives (tools/list, tools/call, list_changed).
    """
    # Required
    method: str
    direction: str  # client_to_server | server_to_client

    # JSON-RPC envelope (spec: jsonrpc, id, method, params/result/error)
    jsonrpc: Optional[str] = None
    request_id: Optional[str] = None
    params: Dict[str, Any] = field(default_factory=dict)
    result: Dict[str, Any] = field(default_factory=dict)
    error_data: Optional[Dict[str, Any]] = None

    # MCP protocol version (optional)
    protocol_version: Optional[str] = None

    # Session / identity
    server_id: Optional[str] = None
    client_id: Optional[str] = None
    session_id: Optional[str] = None

    # Tool call context
    tool_name: Optional[str] = None
    tool_schema_hash: Optional[str] = None
    tool_schema_version: Optional[str] = None
    arguments: Dict[str, Any] = field(default_factory=dict)
    arguments_size_bytes: Optional[int] = None

    # Result context (tools/call response)
    result_content_types: List[str] = field(default_factory=list)
    is_error: Optional[bool] = None
    error_code: Optional[int] = None
    error_message: Optional[str] = None

    # Tool discovery context (tools/list response)
    tools: List[Dict[str, Any]] = field(default_factory=list)
    cursor: Optional[str] = None
    next_cursor: Optional[str] = None
    list_changed: Optional[bool] = None

    # Additional structured context
    attributes: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        result: Dict[str, Any] = {}
        for k, v in self.__dict__.items():
            if v is None:
                continue
            result[k] = v if not isinstance(v, list) else v.copy()
        return result

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "MCPRuntimeEventV1":
        return cls(**{
            k: v for k, v in data.items()
            if k in cls.__dataclass_fields__
        })

    @classmethod
    def required_fields(cls) -> List[str]:
        return ["method", "direction"]


@dataclass
class ExfilEventV1:
    """
    Canonical schema for data transfer / exfil events (V1).
    """
    # Required
    destination: str
    bytes_out: int

    # Optional network context
    src_ip: Optional[str] = None
    dst_ip: Optional[str] = None
    src_host: Optional[str] = None
    dst_host: Optional[str] = None
    dst_domain: Optional[str] = None
    dst_url: Optional[str] = None
    src_user: Optional[str] = None
    dst_user: Optional[str] = None

    # Data details
    bytes_in: Optional[int] = None
    object_count: Optional[int] = None
    mime_types: List[str] = field(default_factory=list)
    data_classification: List[str] = field(default_factory=list)
    encoding: Optional[str] = None
    compressed: Optional[bool] = None

    # Policy outcome
    policy_decision: Optional[str] = None  # allow | deny | monitor
    event_outcome: Optional[str] = None

    # Additional structured context
    attributes: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        result: Dict[str, Any] = {}
        for k, v in self.__dict__.items():
            if v is None:
                continue
            result[k] = v if not isinstance(v, list) else v.copy()
        return result

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ExfilEventV1":
        return cls(**{
            k: v for k, v in data.items()
            if k in cls.__dataclass_fields__
        })

    @classmethod
    def required_fields(cls) -> List[str]:
        return ["destination", "bytes_out"]


@dataclass
class ResilienceEventV1:
    """
    Canonical schema for detection-plane health events (V1).
    """
    # Required
    component: str  # agent/adapter/provider name
    status: str     # ok | degraded | error

    # Optional health metadata
    error_type: Optional[str] = None
    error_message: Optional[str] = None
    latency_ms: Optional[float] = None
    backlog_depth: Optional[int] = None
    missed_heartbeats: Optional[int] = None
    last_seen_ts: Optional[float] = None
    observed_ts: Optional[float] = None

    # Additional structured context
    attributes: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        result: Dict[str, Any] = {}
        for k, v in self.__dict__.items():
            if v is None:
                continue
            result[k] = v if not isinstance(v, list) else v.copy()
        return result

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ResilienceEventV1":
        return cls(**{
            k: v for k, v in data.items()
            if k in cls.__dataclass_fields__
        })

    @classmethod
    def required_fields(cls) -> List[str]:
        return ["component", "status"]
