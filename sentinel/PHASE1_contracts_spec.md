# Phase 1 – Shared Contracts & Canonical Schemas Specification

This document defines the shared data contracts and canonical schemas for CyberMesh ↔ Sentinel integration.
It is the output of Phase 1 and serves as the reference for all subsequent implementation phases.

---

## 1.A – Detection Contracts

### 1.A.1 – Type Inventory

**CyberMesh Types** (`ai-service/src/ml/types.py`):
- `ThreatType`: DDOS, DOS, MALWARE, ANOMALY, NETWORK_INTRUSION, POLICY_VIOLATION
- `EngineType`: ML, RULES, MATH
- `DetectionCandidate`: threat_type, raw_score, calibrated_score, confidence, engine_type, engine_name, features, metadata
- `EnsembleDecision`: should_publish, threat_type, final_score, confidence, llr, candidates, abstention_reason, metadata

**Sentinel Types** (`sentinel/providers/base.py`):
- `ThreatLevel`: CLEAN, SUSPICIOUS, MALICIOUS, CRITICAL, UNKNOWN
- `AnalysisResult`: provider_name, provider_version, threat_level, score, confidence, findings, indicators, latency_ms, error, metadata

---

### 1.A.2 – Unified ThreatType Enum

```python
class ThreatType(str, Enum):
    """Unified threat type across CyberMesh and Sentinel."""
    DDOS = "ddos"
    DOS = "dos"
    MALWARE = "malware"
    ANOMALY = "anomaly"
    NETWORK_INTRUSION = "network_intrusion"
    POLICY_VIOLATION = "policy_violation"
    BOTNET = "botnet"           # NEW: from threat intel
    C2_COMMUNICATION = "c2"     # NEW: from threat intel
```

### 1.A.3 – ThreatLevel → ThreatType Mapping

| Sentinel ThreatLevel | Default ThreatType | Context Override |
|---------------------|-------------------|------------------|
| CLEAN | (no candidate emitted) | - |
| SUSPICIOUS | ANOMALY | If network context → NETWORK_INTRUSION |
| MALICIOUS | MALWARE | If C2 intel → C2_COMMUNICATION |
| CRITICAL | MALWARE | If botnet intel → BOTNET |
| UNKNOWN | (no candidate emitted) | - |

**Mapping Function:**
```python
def map_threat_level_to_type(
    threat_level: ThreatLevel,
    context: Optional[Dict] = None
) -> Optional[ThreatType]:
    """Map Sentinel ThreatLevel to unified ThreatType."""
    if threat_level in (ThreatLevel.CLEAN, ThreatLevel.UNKNOWN):
        return None  # No candidate
    
    # Check context for specific threat types
    if context:
        if context.get("is_c2"):
            return ThreatType.C2_COMMUNICATION
        if context.get("is_botnet"):
            return ThreatType.BOTNET
        if context.get("is_network_flow"):
            return ThreatType.NETWORK_INTRUSION
    
    # Default mapping
    if threat_level == ThreatLevel.SUSPICIOUS:
        return ThreatType.ANOMALY
    elif threat_level in (ThreatLevel.MALICIOUS, ThreatLevel.CRITICAL):
        return ThreatType.MALWARE
    
    return ThreatType.ANOMALY
```

---

### 1.A.4 – Unified DetectionCandidate Contract

```python
@dataclass
class DetectionCandidate:
    """
    Detection candidate from any agent (CyberMesh engine or Sentinel provider).
    
    This is the universal detection output format that both systems produce.
    """
    # Identity
    agent_id: str           # e.g., "cybermesh.ml.ddos", "sentinel.malware_pe_ml"
    signal_id: str          # e.g., "ddos_lightgbm_v2", "entropy_analyzer"
    
    # Detection
    threat_type: ThreatType
    raw_score: float        # Uncalibrated output [0,1]
    calibrated_score: float # Calibrated probability [0,1]
    confidence: float       # Certainty estimate [0,1]
    
    # Explainability
    features: Dict[str, float] = field(default_factory=dict)  # Top contributing features
    findings: List[str] = field(default_factory=list)         # Human-readable findings
    indicators: List[Dict[str, Any]] = field(default_factory=list)  # IOCs
    
    # Metadata
    metadata: Dict[str, Any] = field(default_factory=dict)
    # Expected metadata keys:
    #   - model_version: str
    #   - schema_version: str (e.g., "NetworkFlowFeaturesV1")
    #   - latency_ms: float
    #   - provider_name: str (Sentinel only)
    #   - engine_type: str (CyberMesh only: "ml", "rules", "math")
    
    def __post_init__(self):
        if not (0.0 <= self.raw_score <= 1.0):
            raise ValueError(f"raw_score must be in [0,1], got {self.raw_score}")
        if not (0.0 <= self.calibrated_score <= 1.0):
            raise ValueError(f"calibrated_score must be in [0,1], got {self.calibrated_score}")
        if not (0.0 <= self.confidence <= 1.0):
            raise ValueError(f"confidence must be in [0,1], got {self.confidence}")
```

**Agent ID Convention:**
```
{system}.{category}.{name}

Examples:
- cybermesh.ml.ddos
- cybermesh.rules.pps_threshold
- cybermesh.math.zscore_anomaly
- sentinel.ml.malware_pe
- sentinel.static.entropy
- sentinel.static.strings
- sentinel.intel.crowdsec
- sentinel.intel.feodo
```

---

### 1.A.5 – Unified EnsembleDecision Contract

```python
@dataclass
class EnsembleDecision:
    """
    Final ensemble decision from weighted voting across all agents.
    """
    # Decision
    should_publish: bool
    threat_type: ThreatType
    final_score: float      # Weighted ensemble score [0,1]
    confidence: float       # Ensemble confidence [0,1]
    
    # Evidence
    candidates: List[DetectionCandidate]
    llr: float = 0.0        # Log-likelihood ratio (optional)
    
    # Abstention
    abstention_reason: Optional[str] = None
    
    # Profile & Config
    profile: Optional[str] = None  # "security_first", "balanced", "low_fp"
    
    # Metadata
    metadata: Dict[str, Any] = field(default_factory=dict)
    # Expected metadata keys:
    #   - weights_used: Dict[str, float]
    #   - thresholds_used: Dict[str, float]
    #   - voting_strategy: str
    #   - latency_ms: float
    
    def __post_init__(self):
        if not (0.0 <= self.final_score <= 1.0):
            raise ValueError(f"final_score must be in [0,1], got {self.final_score}")
        if not (0.0 <= self.confidence <= 1.0):
            raise ValueError(f"confidence must be in [0,1], got {self.confidence}")
        if self.should_publish and self.abstention_reason:
            raise ValueError("Cannot publish with abstention_reason set")
        if not self.should_publish and not self.abstention_reason:
            raise ValueError("Must provide abstention_reason if not publishing")
```

---

## 1.B – Canonical Feature Schemas

### 1.B.1 – NetworkFlowFeaturesV1

Canonical schema for network flow features, derived from CIC-DDoS2019 but dataset-agnostic.

| Field | Type | Required | Units | Description |
|-------|------|----------|-------|-------------|
| **Identity** |
| src_ip | str | Yes | - | Source IP address |
| dst_ip | str | Yes | - | Destination IP address |
| src_port | int | Yes | - | Source port |
| dst_port | int | Yes | - | Destination port |
| protocol | int | Yes | IANA | Protocol number (6=TCP, 17=UDP, 1=ICMP) |
| flow_id | str | No | - | Unique flow identifier |
| **Volume** |
| tot_fwd_pkts | int | Yes | count | Total forward packets |
| tot_bwd_pkts | int | Yes | count | Total backward packets |
| totlen_fwd_pkts | int | Yes | bytes | Total forward packet length |
| totlen_bwd_pkts | int | Yes | bytes | Total backward packet length |
| **Timing** |
| flow_duration | float | Yes | μs | Flow duration in microseconds |
| flow_start_ts | float | No | epoch | Flow start timestamp |
| flow_end_ts | float | No | epoch | Flow end timestamp |
| **Rates** |
| flow_byts_s | float | Yes | bytes/s | Flow bytes per second |
| flow_pkts_s | float | Yes | pkts/s | Flow packets per second |
| fwd_pkts_s | float | No | pkts/s | Forward packets per second |
| bwd_pkts_s | float | No | pkts/s | Backward packets per second |
| **Packet Length Stats** |
| fwd_pkt_len_max | float | No | bytes | Max forward packet length |
| fwd_pkt_len_min | float | No | bytes | Min forward packet length |
| fwd_pkt_len_mean | float | No | bytes | Mean forward packet length |
| fwd_pkt_len_std | float | No | bytes | Std dev forward packet length |
| bwd_pkt_len_max | float | No | bytes | Max backward packet length |
| bwd_pkt_len_min | float | No | bytes | Min backward packet length |
| bwd_pkt_len_mean | float | No | bytes | Mean backward packet length |
| bwd_pkt_len_std | float | No | bytes | Std dev backward packet length |
| pkt_len_min | float | No | bytes | Overall min packet length |
| pkt_len_max | float | No | bytes | Overall max packet length |
| pkt_len_mean | float | No | bytes | Overall mean packet length |
| pkt_len_std | float | No | bytes | Overall std dev packet length |
| pkt_len_var | float | No | bytes² | Overall variance packet length |
| **Inter-Arrival Time Stats** |
| flow_iat_mean | float | No | μs | Mean inter-arrival time |
| flow_iat_std | float | No | μs | Std dev inter-arrival time |
| flow_iat_max | float | No | μs | Max inter-arrival time |
| flow_iat_min | float | No | μs | Min inter-arrival time |
| fwd_iat_tot | float | No | μs | Total forward IAT |
| fwd_iat_mean | float | No | μs | Mean forward IAT |
| fwd_iat_std | float | No | μs | Std dev forward IAT |
| fwd_iat_max | float | No | μs | Max forward IAT |
| fwd_iat_min | float | No | μs | Min forward IAT |
| bwd_iat_tot | float | No | μs | Total backward IAT |
| bwd_iat_mean | float | No | μs | Mean backward IAT |
| bwd_iat_std | float | No | μs | Std dev backward IAT |
| bwd_iat_max | float | No | μs | Max backward IAT |
| bwd_iat_min | float | No | μs | Min backward IAT |
| **TCP Flags** |
| fin_flag_cnt | int | No | count | FIN flags |
| syn_flag_cnt | int | No | count | SYN flags |
| rst_flag_cnt | int | No | count | RST flags |
| psh_flag_cnt | int | No | count | PSH flags |
| ack_flag_cnt | int | No | count | ACK flags |
| urg_flag_cnt | int | No | count | URG flags |
| cwe_flag_count | int | No | count | CWE flags |
| ece_flag_cnt | int | No | count | ECE flags |
| fwd_psh_flags | int | No | count | Forward PSH flags |
| bwd_psh_flags | int | No | count | Backward PSH flags |
| fwd_urg_flags | int | No | count | Forward URG flags |
| bwd_urg_flags | int | No | count | Backward URG flags |
| **Header** |
| fwd_header_len | int | No | bytes | Forward header length |
| bwd_header_len | int | No | bytes | Backward header length |
| **Derived/Ratio** |
| down_up_ratio | float | No | ratio | Download/upload ratio |
| pkt_size_avg | float | No | bytes | Average packet size |
| fwd_seg_size_avg | float | No | bytes | Avg forward segment size |
| bwd_seg_size_avg | float | No | bytes | Avg backward segment size |
| **Bulk Stats** |
| fwd_byts_b_avg | float | No | bytes | Avg forward bulk bytes |
| fwd_pkts_b_avg | float | No | pkts | Avg forward bulk packets |
| fwd_blk_rate_avg | float | No | rate | Avg forward bulk rate |
| bwd_byts_b_avg | float | No | bytes | Avg backward bulk bytes |
| bwd_pkts_b_avg | float | No | pkts | Avg backward bulk packets |
| bwd_blk_rate_avg | float | No | rate | Avg backward bulk rate |
| **Subflow** |
| subflow_fwd_pkts | int | No | count | Subflow forward packets |
| subflow_fwd_byts | int | No | bytes | Subflow forward bytes |
| subflow_bwd_pkts | int | No | count | Subflow backward packets |
| subflow_bwd_byts | int | No | bytes | Subflow backward bytes |
| **Window** |
| init_fwd_win_byts | int | No | bytes | Initial forward window bytes |
| init_bwd_win_byts | int | No | bytes | Initial backward window bytes |
| fwd_act_data_pkts | int | No | count | Forward active data packets |
| fwd_seg_size_min | int | No | bytes | Min forward segment size |
| **Active/Idle** |
| active_mean | float | No | μs | Mean active time |
| active_std | float | No | μs | Std dev active time |
| active_max | float | No | μs | Max active time |
| active_min | float | No | μs | Min active time |
| idle_mean | float | No | μs | Mean idle time |
| idle_std | float | No | μs | Std dev idle time |
| idle_max | float | No | μs | Max idle time |
| idle_min | float | No | μs | Min idle time |
| **Semantic (Derived)** |
| pps | float | No | pkts/s | Packets per second (derived) |
| bps | float | No | bytes/s | Bytes per second (derived) |
| syn_ack_ratio | float | No | ratio | SYN/ACK ratio (derived) |
| unique_dst_ports_batch | int | No | count | Unique dst ports in batch |
| port_entropy_batch | float | No | bits | Port entropy in batch |

**Required Fields (13):** src_ip, dst_ip, src_port, dst_port, protocol, tot_fwd_pkts, tot_bwd_pkts, totlen_fwd_pkts, totlen_bwd_pkts, flow_duration, flow_byts_s, flow_pkts_s

**Total Fields:** 79 core + 5 semantic = 84

---

### 1.B.2 – CIC-DDoS2019 → NetworkFlowFeaturesV1 Mapping

| CIC-DDoS2019 Column | NetworkFlowFeaturesV1 Field | Transform |
|--------------------|----------------------------|-----------|
| src_port | src_port | direct |
| dst_port | dst_port | direct |
| protocol | protocol | direct |
| flow_duration | flow_duration | direct |
| tot_fwd_pkts | tot_fwd_pkts | direct |
| tot_bwd_pkts | tot_bwd_pkts | direct |
| totlen_fwd_pkts | totlen_fwd_pkts | direct |
| totlen_bwd_pkts | totlen_bwd_pkts | direct |
| fwd_pkt_len_max | fwd_pkt_len_max | direct |
| fwd_pkt_len_min | fwd_pkt_len_min | direct |
| fwd_pkt_len_mean | fwd_pkt_len_mean | direct |
| fwd_pkt_len_std | fwd_pkt_len_std | direct |
| bwd_pkt_len_max | bwd_pkt_len_max | direct |
| bwd_pkt_len_min | bwd_pkt_len_min | direct |
| bwd_pkt_len_mean | bwd_pkt_len_mean | direct |
| bwd_pkt_len_std | bwd_pkt_len_std | direct |
| flow_byts_s | flow_byts_s | direct |
| flow_pkts_s | flow_pkts_s | direct |
| ... | ... | ... (all 79 map 1:1) |

**Note:** CIC-DDoS2019 maps directly to NetworkFlowFeaturesV1 core fields. The adapter simply renames and validates.

---

### 1.B.3 – FileFeaturesV1

Canonical schema for file-based analysis features.

| Field | Type | Required | Source | Description |
|-------|------|----------|--------|-------------|
| **Identity** |
| sha256 | str | Yes | parser | SHA256 hash |
| sha1 | str | No | parser | SHA1 hash |
| md5 | str | No | parser | MD5 hash |
| file_name | str | Yes | parser | Original filename |
| file_size | int | Yes | parser | File size in bytes |
| file_type | str | Yes | parser | Detected file type |
| **Static Analysis** |
| entropy | float | Yes | entropy_analyzer | Overall file entropy |
| section_count | int | No | parser | Number of sections (PE) |
| section_entropy_max | float | No | entropy_analyzer | Max section entropy |
| section_entropy_mean | float | No | entropy_analyzer | Mean section entropy |
| import_count | int | No | parser | Number of imports (PE) |
| export_count | int | No | parser | Number of exports (PE) |
| **Strings Analysis** |
| strings_count | int | No | strings_analyzer | Total extracted strings |
| strings_suspicious_count | int | No | strings_analyzer | Suspicious strings |
| strings_score_command_exec | float | No | strings_analyzer | Command execution score |
| strings_score_cred_theft | float | No | strings_analyzer | Credential theft score |
| strings_score_download_exec | float | No | strings_analyzer | Download/execute score |
| strings_score_persistence | float | No | strings_analyzer | Persistence score |
| strings_score_obfuscation | float | No | strings_analyzer | Obfuscation score |
| **YARA** |
| yara_match_count | int | No | yara_rules | Number of YARA matches |
| yara_rule_names | List[str] | No | yara_rules | Matched rule names |
| yara_score_packers | float | No | yara_rules | Packer detection score |
| yara_score_exploits | float | No | yara_rules | Exploit detection score |
| **Threat Intelligence** |
| ti_ip_count | int | No | threat_intel | IPs found in file |
| ti_ip_malicious_count | int | No | threat_intel | Malicious IPs |
| ti_domain_count | int | No | threat_intel | Domains found |
| ti_domain_malicious_count | int | No | threat_intel | Malicious domains |
| ti_url_count | int | No | threat_intel | URLs found |
| ti_url_malicious_count | int | No | threat_intel | Malicious URLs |
| ti_is_c2 | bool | No | threat_intel | C2 indicator found |
| ti_is_botnet | bool | No | threat_intel | Botnet indicator found |
| ti_abuse_score_max | float | No | threat_intel | Max AbuseIPDB score |
| ti_crowdsec_behaviors | List[str] | No | threat_intel | CrowdSec behaviors |
| **ML Scores** |
| ml_pe_score | float | No | malware_pe_ml | PE ML model score |
| ml_api_score | float | No | malware_api_ml | API ML model score |

**Required Fields (6):** sha256, file_name, file_size, file_type, entropy

---

### 1.B.4 – Schema Versioning Rules

1. **Version Format:** `{SchemaName}V{Major}` (e.g., `NetworkFlowFeaturesV1`, `FileFeaturesV2`)

2. **Backward Compatible Changes (same version):**
   - Adding new optional fields
   - Adding new enum values
   - Relaxing validation (e.g., wider range)

3. **Breaking Changes (new version required):**
   - Removing fields
   - Renaming fields
   - Changing field types
   - Tightening validation
   - Changing field semantics/units

4. **Model Registration:**
   - Each model must declare its required schema version
   - Pipeline validates schema version match before inference
   - Mismatched versions → explicit error, not silent degradation

---

## 1.C – Canonical Event Envelope

### 1.C.1 – Event Structure

```python
@dataclass
class CanonicalEvent:
    """
    Universal event envelope for all detection pipelines.
    """
    # Identity
    id: str                 # UUID
    timestamp: float        # Unix epoch (seconds)
    
    # Source
    source: str             # e.g., "postgres_ddos", "sentinel_file_agent"
    tenant_id: str          # Tenant identifier (required for multi-tenant)
    
    # Modality
    modality: str           # "network_flow" | "file" | "dns" | "proxy"
    features_version: str   # "NetworkFlowFeaturesV1" | "FileFeaturesV1"
    
    # Payload
    features: Dict[str, Any]    # Canonical features per schema
    raw_context: Dict[str, Any] = field(default_factory=dict)  # Original record
    
    # Optional
    labels: Dict[str, str] = field(default_factory=dict)  # For training/eval
```

**JSON Example (Network Flow):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": 1732900000.0,
  "source": "postgres_ddos",
  "tenant_id": "tenant-123",
  "modality": "network_flow",
  "features_version": "NetworkFlowFeaturesV1",
  "features": {
    "src_ip": "192.168.1.100",
    "dst_ip": "10.0.0.1",
    "src_port": 45678,
    "dst_port": 80,
    "protocol": 6,
    "flow_duration": 1000000,
    "tot_fwd_pkts": 100,
    "tot_bwd_pkts": 50,
    "flow_pkts_s": 150.0,
    "syn_flag_cnt": 1,
    "ack_flag_cnt": 50
  },
  "raw_context": {
    "db_id": 12345,
    "label": "ddos"
  }
}
```

**JSON Example (File):**
```json
{
  "id": "660e8400-e29b-41d4-a716-446655440001",
  "timestamp": 1732900001.0,
  "source": "sentinel_file_agent",
  "tenant_id": "tenant-123",
  "modality": "file",
  "features_version": "FileFeaturesV1",
  "features": {
    "sha256": "abc123...",
    "file_name": "suspicious.exe",
    "file_size": 102400,
    "file_type": "pe",
    "entropy": 7.2,
    "ti_is_c2": true,
    "ti_abuse_score_max": 85.0
  },
  "raw_context": {
    "file_path": "/uploads/suspicious.exe",
    "upload_time": 1732900000.0
  }
}
```

---

### 1.C.2 – Per-Modality Required Context

| Modality | Required in features | Required in raw_context |
|----------|---------------------|------------------------|
| network_flow | src_ip, dst_ip, src_port, dst_port, protocol | flow_id (if available) |
| file | sha256, file_name, file_size, file_type | file_path (if available) |
| dns | query_name, query_type, response_code | resolver_ip |
| proxy | url, method, status_code | user_agent |

---

### 1.C.3 – PII/Sensitive Field Treatment

| Field | Sensitivity | Treatment |
|-------|-------------|-----------|
| src_ip | HIGH | Hash in logs, mask in exports |
| dst_ip | MEDIUM | Hash in logs if internal |
| tenant_id | HIGH | Never log, never export |
| file_path | MEDIUM | Truncate to filename in logs |
| raw_context | VARIES | Strip before export |

**Rules:**
1. `tenant_id` must NEVER appear in logs or exported datasets
2. IP addresses must be hashed or masked in any exported/shared data
3. `raw_context` is stripped before any cross-tenant aggregation
4. Event consumers MUST validate `tenant_id` matches their context

---

## 1.D – Exit Criteria Checklist

### 1.A – Detection Contracts
- [x] Unified `ThreatType` enum defined with BOTNET and C2_COMMUNICATION additions
- [x] `ThreatLevel` → `ThreatType` mapping documented
- [x] `DetectionCandidate` contract with agent_id/signal_id defined
- [x] `EnsembleDecision` contract with profile support defined
- [x] Agent ID naming convention documented

### 1.B – Canonical Feature Schemas
- [x] `NetworkFlowFeaturesV1` schema with 79+ fields defined
- [x] CIC-DDoS2019 → NetworkFlowFeaturesV1 mapping documented
- [x] `FileFeaturesV1` schema with static/TI/ML fields defined
- [x] Schema versioning rules documented

### 1.C – Event Envelope
- [x] `CanonicalEvent` structure defined
- [x] Per-modality required context documented
- [x] PII/sensitive field treatment documented

### 1.D – Alignment
- [x] Type stubs created in `sentinel/contracts/`
- [x] PHASE1_contracts_spec.md written
- [x] No-behavior-change verified (contracts not imported by CLI/providers/API)
- [ ] CyberMesh-side stubs (DEFERRED to Phase 4 per SOP)

### 1.D.1 – No-Behavior-Change Verification

**Verified:** The `sentinel/contracts/` module is NOT imported anywhere in:
- `sentinel/cli.py` ✅
- `sentinel/agents/` ✅
- `sentinel/providers/` ✅
- `api/` ✅

The contracts exist as pure definitions, ready for Phase 2 adoption.

### 1.D.2 – CyberMesh Alignment (Deferred)

Per SOP Phase 4:
> "Add shared types to CyberMesh: Either import from a shared package or copy the agreed DetectionCandidate / EnsembleDecision types."

CyberMesh currently uses its own types in `src/ml/types.py`. Alignment will occur in Phase 4 when we:
1. Wrap CyberMesh engines as `DetectionAgent`s
2. Introduce `DetectionAgentPipeline`
3. Add feature flag for new pipeline

**Options for Phase 4:**
- A) Copy contracts to CyberMesh (duplicate but independent)
- B) Extract to shared `cybermesh-contracts` package
- C) CyberMesh imports from Sentinel (if dependency acceptable)

---

## Next Steps (Phase 2)

With these contracts defined, Phase 2 will:
1. Implement `DetectionAgent` interface in Sentinel
2. Create `SentinelAgent` wrapping existing providers
3. Implement mini `EnsembleVoter` using these contracts
4. Validate with CLI/tests before touching CyberMesh
