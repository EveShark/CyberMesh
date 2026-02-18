# Sentinel → CyberMesh Integration Guide

**Goal:** Build AI Service functionality directly into Sentinel, enabling it to produce to Kafka and integrate with CyberMesh's PBFT consensus backend.

---

## Current State Analysis

### What Sentinel HAS ✅

| Component | Location | Status |
|-----------|----------|--------|
| **File Parsing** | `sentinel/parsers/` | ✅ Complete (PE, PDF, Office, Script, Android, PCAP) |
| **Detection Engines** | `sentinel/agents/` | ✅ Complete (Static, ML, LLM, YARA, Threat Intel) |
| **Threat Intel Providers** | `sentinel/providers/threat_intel/` | ✅ Built (VT, OTX, MalwareBazaar, URLhaus, AbuseIPDB, Shodan, GreyNoise) |
| **Threat Intel Enrichment** | `sentinel/threat_intel/enrichment.py` | ✅ Complete orchestration pipeline |
| **LLM Providers** | `sentinel/providers/llm/` | ✅ Complete (Groq, Qwen, Ollama, GLM) |
| **ML Models** | `sentinel/providers/ml/` | ✅ Complete (LightGBM malware classifiers) |
| **Coordinator (Ensemble)** | `sentinel/agents/coordinator.py` | ✅ Weighted voting with abstention logic |
| **LangGraph Workflow** | `sentinel/agents/graph.py` | ✅ Complete agent orchestration |
| **CLI** | `sentinel/cli.py` | ✅ Working (analyze, agentic, benchmark) |
| **REST API** | `sentinel/api/` | ✅ FastAPI with Supabase auth (basic) |
| **Config System** | `sentinel/config/` | ✅ Pydantic settings |
| **Logging** | `sentinel/logging/` | ✅ Structured logging |

### What Sentinel is MISSING ❌

| Component | Required For | Complexity |
|-----------|--------------|------------|
| **Kafka Producer** | Publishing detections to `ai.anomalies.v1` | Medium |
| **Kafka Consumer** | Receiving feedback from `control.commits.v1` | Medium |
| **Ed25519 Signer** | Cryptographic message signing | Low (copy from ai-service) |
| **Nonce Manager** | Replay protection | Low (copy from ai-service) |
| **Protobuf Contracts** | Message serialization | Medium |
| **AnomalyMessage** | Kafka message wrapper | Medium |
| **Service Manager** | Lifecycle orchestration | High |
| **Detection Loop** | Continuous background scanning | Medium |
| **Feedback Service** | Adaptive learning from consensus | Medium |
| **Rate Limiter** | Token bucket for Kafka | Low |
| **Circuit Breaker** | Kafka failure handling | Low |
| **Health API** | Kubernetes probes | Low |
| **Metrics (Prometheus)** | Observability | Low |

---

## Architecture Comparison

### Current AI Service (CyberMesh)

```
┌─────────────────────────────────────────────────────────────────┐
│                      AI SERVICE (Python)                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  DETECTION PIPELINE                                              │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐                 │
│  │ Rules      │  │ Math       │  │ ML Engine  │                 │
│  │ Engine     │  │ Engine     │  │ (LightGBM) │                 │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘                 │
│        └───────────────┼───────────────┘                         │
│                        ▼                                         │
│              ┌─────────────────┐                                 │
│              │ Ensemble Voter  │ ← Weighted voting + abstention  │
│              └────────┬────────┘                                 │
│                       │                                          │
│  CRYPTO LAYER         ▼                                          │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ Signer (Ed25519) + NonceManager (16-byte replay protection) ││
│  └─────────────────────────────────────────────────────────────┘│
│                       │                                          │
│  CONTRACTS            ▼                                          │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ AnomalyMessage → Protobuf serialization → Kafka             ││
│  └─────────────────────────────────────────────────────────────┘│
│                       │                                          │
│  KAFKA INTEGRATION    ▼                                          │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ AIProducer (confluent-kafka) → ai.anomalies.v1              ││
│  │ AIConsumer ← control.commits.v1 (feedback loop)             ││
│  └─────────────────────────────────────────────────────────────┘│
│                                                                  │
│  SERVICE LAYER                                                   │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ ServiceManager + DetectionLoop + FeedbackService            ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

### Target: Sentinel with CyberMesh Integration

```
┌─────────────────────────────────────────────────────────────────┐
│                      SENTINEL (Enhanced)                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  DETECTION PIPELINE (EXISTING)                                   │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────┐ │
│  │ Static     │  │ ML Agent   │  │ YARA       │  │ Threat     │ │
│  │ Agent      │  │            │  │ Scanner    │  │ Intel      │ │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘ │
│        └───────────────┴───────────────┴───────────────┘         │
│                                ▼                                 │
│              ┌─────────────────────────────┐                     │
│              │ Coordinator (Ensemble)      │ ← EXISTING          │
│              └────────────┬────────────────┘                     │
│                           │                                      │
│  NEW: CYBERMESH LAYER     ▼                                      │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                   sentinel/cybermesh/                        ││
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐               ││
│  │  │ Signer    │  │ Nonce     │  │ Contracts │               ││
│  │  │ (Ed25519) │  │ Manager   │  │ (Protobuf)│               ││
│  │  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘               ││
│  │        └──────────────┼──────────────┘                      ││
│  │                       ▼                                      ││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │ KafkaProducer → ai.anomalies.v1                         │││
│  │  │ KafkaConsumer ← control.commits.v1                      │││
│  │  └─────────────────────────────────────────────────────────┘││
│  │                       ▼                                      ││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │ ServiceManager + DetectionLoop + FeedbackService        │││
│  │  └─────────────────────────────────────────────────────────┘││
│  └─────────────────────────────────────────────────────────────┘│
│                                                                  │
│  EXISTING: API + CLI                                             │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ FastAPI (REST) + Typer (CLI) + Health Endpoints             ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

---

## Files to Copy from AI Service

These files can be copied with minimal modification:

### 1. Crypto Utils (Copy as-is)

| Source | Target | Changes |
|--------|--------|---------|
| `ai-service/src/utils/signer.py` | `sentinel/cybermesh/crypto/signer.py` | Update imports |
| `ai-service/src/utils/nonce.py` | `sentinel/cybermesh/crypto/nonce.py` | Update imports |
| `ai-service/src/utils/secrets.py` | `sentinel/cybermesh/crypto/secrets.py` | Update imports |

### 2. Kafka Layer (Copy and adapt)

| Source | Target | Changes |
|--------|--------|---------|
| `ai-service/src/kafka/producer.py` | `sentinel/cybermesh/kafka/producer.py` | Update imports |
| `ai-service/src/kafka/consumer.py` | `sentinel/cybermesh/kafka/consumer.py` | Update imports |

### 3. Contracts (Copy and extend)

| Source | Target | Changes |
|--------|--------|---------|
| `ai-service/src/contracts/anomaly.py` | `sentinel/cybermesh/contracts/anomaly.py` | Add file analysis fields |
| `ai-service/proto/ai_anomaly.proto` | `sentinel/cybermesh/proto/ai_anomaly.proto` | Add FileAnalysis message |

### 4. Service Layer (Adapt significantly)

| Source | Target | Changes |
|--------|--------|---------|
| `ai-service/src/service/manager.py` | `sentinel/cybermesh/service/manager.py` | Integrate with Sentinel agents |
| `ai-service/src/service/publisher.py` | `sentinel/cybermesh/service/publisher.py` | Minor updates |
| `ai-service/src/service/detection_loop.py` | `sentinel/cybermesh/service/file_watcher.py` | Replace telemetry with file watching |

### 5. Utils (Copy as-is)

| Source | Target | Changes |
|--------|--------|---------|
| `ai-service/src/utils/circuit_breaker.py` | `sentinel/cybermesh/utils/circuit_breaker.py` | None |
| `ai-service/src/utils/backoff.py` | `sentinel/cybermesh/utils/backoff.py` | None |
| `ai-service/src/utils/rate_limit.py` | `sentinel/cybermesh/utils/rate_limiter.py` | None |
| `ai-service/src/utils/validators.py` | `sentinel/cybermesh/utils/validators.py` | None |
| `ai-service/src/utils/limits.py` | `sentinel/cybermesh/utils/limits.py` | None |
| `ai-service/src/utils/time.py` | `sentinel/cybermesh/utils/time.py` | None |
| `ai-service/src/utils/errors.py` | `sentinel/cybermesh/utils/errors.py` | Extend with Sentinel errors |

---

## New Files to Create

### 1. Directory Structure

```
sentinel/cybermesh/
├── __init__.py
├── crypto/
│   ├── __init__.py
│   ├── signer.py          # Ed25519 signing
│   ├── nonce.py           # Replay protection
│   └── secrets.py         # Key loading
├── kafka/
│   ├── __init__.py
│   ├── producer.py        # AIProducer
│   └── consumer.py        # AIConsumer
├── contracts/
│   ├── __init__.py
│   ├── anomaly.py         # AnomalyMessage (extended for files)
│   ├── evidence.py        # EvidenceMessage
│   └── generated/         # Protobuf generated files
│       └── ai_anomaly_pb2.py
├── proto/
│   ├── ai_anomaly.proto   # Extended schema
│   └── ai_evidence.proto
├── service/
│   ├── __init__.py
│   ├── manager.py         # ServiceManager
│   ├── publisher.py       # MessagePublisher
│   ├── file_watcher.py    # Directory/file monitoring
│   └── feedback.py        # Feedback processing
├── utils/
│   ├── __init__.py
│   ├── circuit_breaker.py
│   ├── backoff.py
│   ├── rate_limiter.py
│   ├── validators.py
│   ├── limits.py
│   ├── time.py
│   └── errors.py
└── config/
    ├── __init__.py
    └── settings.py        # CyberMesh-specific settings
```

### 2. Extended Protobuf Schema

```protobuf
// sentinel/cybermesh/proto/ai_anomaly.proto
syntax = "proto3";

package cybermesh.ai.v1;

message AnomalyEvent {
  // Existing fields (from ai-service)
  string id = 1;
  string type = 2;
  string source = 3;
  uint32 severity = 4;
  double confidence = 5;
  int64 ts = 6;
  bytes content_hash = 7;
  bytes payload = 8;
  string model_version = 9;
  bytes producer_id = 10;
  bytes nonce = 11;
  bytes signature = 12;
  bytes pubkey = 13;
  string alg = 14;
  
  // NEW: File analysis fields
  FileAnalysis file_analysis = 20;
}

message FileAnalysis {
  string file_hash_sha256 = 1;
  string file_hash_sha1 = 2;
  string file_hash_md5 = 3;
  string file_name = 4;
  string file_type = 5;
  int64 file_size = 6;
  double entropy = 7;
  
  // Detection results
  repeated YARAMatch yara_matches = 10;
  repeated Detection detections = 11;
  repeated IOC iocs = 12;
  
  // Threat intelligence
  ThreatIntelResult threat_intel = 20;
  
  // Reasoning
  string final_reasoning = 30;
  repeated string reasoning_steps = 31;
}

message YARAMatch {
  string rule_name = 1;
  string namespace = 2;
  repeated string tags = 3;
  map<string, string> meta = 4;
}

message Detection {
  string source = 1;
  string severity = 2;
  string description = 3;
  double confidence = 4;
  map<string, string> metadata = 5;
}

message IOC {
  string type = 1;     // ip, domain, url, hash
  string value = 2;
  string context = 3;
  double risk_score = 4;
}

message ThreatIntelResult {
  bool is_known_malware = 1;
  string malware_family = 2;
  string vt_detections = 3;      // "45/70"
  string vt_threat_label = 4;
  repeated string sources = 5;
  double threat_score = 6;
}
```

### 3. AnomalyMessage Extension

```python
# sentinel/cybermesh/contracts/anomaly.py

"""
Extended AnomalyMessage for file-based malware detection.

Inherits cryptographic envelope from ai-service,
adds file analysis fields for Sentinel.
"""

import hashlib
import json
from dataclasses import dataclass, asdict
from typing import Optional, List, Dict, Any

from ..crypto.signer import Signer
from ..crypto.nonce import NonceManager
from ..utils.validators import validate_uuid4, validate_severity, validate_confidence
from ..utils.errors import ContractError
from .generated import ai_anomaly_pb2


@dataclass
class FileAnalysisData:
    """File analysis results to include in anomaly."""
    file_hash_sha256: str
    file_hash_sha1: Optional[str] = None
    file_hash_md5: Optional[str] = None
    file_name: str = ""
    file_type: str = ""
    file_size: int = 0
    entropy: float = 0.0
    
    yara_matches: List[Dict[str, Any]] = None
    detections: List[Dict[str, Any]] = None
    iocs: List[Dict[str, Any]] = None
    
    threat_intel: Optional[Dict[str, Any]] = None
    final_reasoning: str = ""
    reasoning_steps: List[str] = None
    
    def __post_init__(self):
        self.yara_matches = self.yara_matches or []
        self.detections = self.detections or []
        self.iocs = self.iocs or []
        self.reasoning_steps = self.reasoning_steps or []
    
    def to_proto(self) -> ai_anomaly_pb2.FileAnalysis:
        """Convert to protobuf message."""
        fa = ai_anomaly_pb2.FileAnalysis(
            file_hash_sha256=self.file_hash_sha256,
            file_hash_sha1=self.file_hash_sha1 or "",
            file_hash_md5=self.file_hash_md5 or "",
            file_name=self.file_name,
            file_type=self.file_type,
            file_size=self.file_size,
            entropy=self.entropy,
            final_reasoning=self.final_reasoning,
        )
        
        # Add YARA matches
        for match in self.yara_matches:
            ym = ai_anomaly_pb2.YARAMatch(
                rule_name=match.get("rule_name", ""),
                namespace=match.get("namespace", ""),
                tags=match.get("tags", []),
            )
            if match.get("meta"):
                ym.meta.update(match["meta"])
            fa.yara_matches.append(ym)
        
        # Add detections
        for det in self.detections:
            d = ai_anomaly_pb2.Detection(
                source=det.get("source", ""),
                severity=det.get("severity", "medium"),
                description=det.get("description", ""),
                confidence=det.get("confidence", 0.5),
            )
            if det.get("metadata"):
                d.metadata.update({k: str(v) for k, v in det["metadata"].items()})
            fa.detections.append(d)
        
        # Add IOCs
        for ioc in self.iocs:
            fa.iocs.append(ai_anomaly_pb2.IOC(
                type=ioc.get("type", ""),
                value=ioc.get("value", ""),
                context=ioc.get("context", ""),
                risk_score=ioc.get("risk_score", 0.0),
            ))
        
        # Add threat intel
        if self.threat_intel:
            fa.threat_intel.CopyFrom(ai_anomaly_pb2.ThreatIntelResult(
                is_known_malware=self.threat_intel.get("is_known_malware", False),
                malware_family=self.threat_intel.get("malware_family", ""),
                vt_detections=self.threat_intel.get("vt_detections", ""),
                vt_threat_label=self.threat_intel.get("vt_threat_label", ""),
                sources=self.threat_intel.get("sources", []),
                threat_score=self.threat_intel.get("threat_score", 0.0),
            ))
        
        fa.reasoning_steps.extend(self.reasoning_steps)
        
        return fa


class FileAnomalyMessage:
    """
    Anomaly message for file-based malware detection.
    
    Extends base AnomalyMessage with FileAnalysisData.
    """
    
    DOMAIN = "ai.anomaly.v1"
    
    def __init__(
        self,
        anomaly_id: str,
        source: str,  # hostname/node that submitted file
        severity: int,
        confidence: float,
        timestamp: int,
        file_analysis: FileAnalysisData,
        model_version: str,
        signer: Signer,
        nonce_manager: NonceManager,
    ):
        # Validate inputs
        validate_uuid4(anomaly_id)
        validate_severity(severity)
        validate_confidence(confidence)
        
        self.anomaly_id = anomaly_id
        self.anomaly_type = "file_malware"
        self.source = source
        self.severity = severity
        self.confidence = confidence
        self.timestamp = timestamp
        self.file_analysis = file_analysis
        self.model_version = model_version
        self.signer = signer
        self.nonce_manager = nonce_manager
        
        # Payload is JSON of file analysis
        self.payload = json.dumps(asdict(file_analysis)).encode("utf-8")
        self.content_hash = hashlib.sha256(self.payload).digest()
        self.nonce = nonce_manager.generate()
        self.pubkey = signer.public_key_bytes
        
        # Sign the message
        sign_bytes = self._build_sign_bytes()
        self.signature, _, _ = signer.sign(sign_bytes, domain=self.DOMAIN)
    
    def _build_sign_bytes(self) -> bytes:
        """Build bytes to sign (matches backend verification)."""
        import struct
        
        ts_bytes = struct.pack('>q', self.timestamp)
        producer_id_len = struct.pack('>H', len(self.pubkey))
        producer_id_bytes = producer_id_len + self.pubkey
        
        return ts_bytes + producer_id_bytes + self.nonce + self.content_hash
    
    def to_bytes(self) -> bytes:
        """Serialize to protobuf for Kafka."""
        msg = ai_anomaly_pb2.AnomalyEvent(
            id=self.anomaly_id,
            type=self.anomaly_type,
            source=self.source,
            severity=self.severity,
            confidence=self.confidence,
            ts=self.timestamp,
            content_hash=self.content_hash,
            payload=self.payload,
            model_version=self.model_version,
            producer_id=self.pubkey,
            nonce=self.nonce,
            signature=self.signature,
            pubkey=self.pubkey,
            alg="Ed25519",
            file_analysis=self.file_analysis.to_proto(),
        )
        return msg.SerializeToString()
```

---

## Integration Steps

### Phase 1: Core Infrastructure (2-3 days)

1. **Create directory structure**
   ```
   mkdir -p sentinel/cybermesh/{crypto,kafka,contracts,proto,service,utils,config}
   ```

2. **Copy and adapt crypto utils**
   - Copy `signer.py`, `nonce.py`, `secrets.py` from ai-service
   - Update import paths

3. **Copy and adapt utility functions**
   - Copy `circuit_breaker.py`, `backoff.py`, `rate_limiter.py`
   - Copy `validators.py`, `limits.py`, `time.py`, `errors.py`

4. **Create protobuf schema**
   - Create extended `ai_anomaly.proto`
   - Generate Python files: `protoc --python_out=. ai_anomaly.proto`

5. **Create contracts**
   - Implement `FileAnomalyMessage` class
   - Create `FileAnalysisData` dataclass

### Phase 2: Kafka Integration (2 days)

1. **Copy and adapt Kafka producer**
   - Copy `producer.py` from ai-service
   - Update topic names and config

2. **Copy and adapt Kafka consumer**
   - Copy `consumer.py` from ai-service
   - Implement feedback handlers

3. **Create config settings**
   - Add Kafka configuration to settings
   - Add Ed25519 key paths
   - Add node ID configuration

### Phase 3: Service Layer (3-4 days)

1. **Create ServiceManager**
   - Initialize crypto components
   - Initialize Kafka producer/consumer
   - Manage lifecycle (start/stop/health)

2. **Create MessagePublisher**
   - Convert Sentinel results to AnomalyMessage
   - Handle signing and publishing

3. **Create FileWatcher (optional)**
   - Watch directories for new files
   - Trigger analysis on new files
   - Alternative: On-demand scanning via API

4. **Create FeedbackService**
   - Process `control.commits.v1` messages
   - Update detection thresholds based on consensus
   - Track acceptance rates

### Phase 4: Integration with Sentinel Agents (2 days)

1. **Bridge coordinator output to CyberMesh**
   ```python
   # In sentinel/agents/coordinator.py or new bridge file
   
   def publish_to_cybermesh(result: Dict[str, Any], publisher: MessagePublisher):
       """Convert Sentinel result to CyberMesh anomaly."""
       if result.get("threat_level") in [ThreatLevel.SUSPICIOUS, ThreatLevel.MALICIOUS]:
           file_analysis = FileAnalysisData(
               file_hash_sha256=result["parsed_file"].hashes["sha256"],
               file_name=result["parsed_file"].file_name,
               file_type=result["parsed_file"].file_type.value,
               file_size=result["parsed_file"].file_size,
               detections=[asdict(f) for f in result.get("findings", [])],
               iocs=result.get("indicators", []),
               threat_intel=result.get("threat_intel_result"),
               final_reasoning=result.get("final_reasoning", ""),
               reasoning_steps=result.get("reasoning_steps", []),
           )
           publisher.publish_file_anomaly(file_analysis)
   ```

2. **Add CyberMesh mode to CLI**
   ```python
   @app.command()
   def cybermesh(
       watch_dir: str = typer.Option(None, help="Directory to watch"),
       publish: bool = typer.Option(True, help="Publish to Kafka"),
   ):
       """Run Sentinel in CyberMesh mode (continuous detection + Kafka publishing)."""
       ...
   ```

3. **Add CyberMesh endpoint to API**
   ```python
   @router.post("/scan/cybermesh")
   async def scan_and_publish(file: UploadFile, publish: bool = True):
       """Scan file and optionally publish to CyberMesh backend."""
       ...
   ```

### Phase 5: Testing & Validation (2 days)

1. **Unit tests**
   - Test signing/verification
   - Test nonce generation
   - Test message serialization

2. **Integration tests**
   - Test Kafka producer (mock broker)
   - Test full pipeline: file → analysis → message → Kafka

3. **End-to-end test with CyberMesh**
   - Deploy Sentinel with CyberMesh backend
   - Verify messages flow through consensus
   - Verify enforcement agent receives policies

---

## Configuration Requirements

### Environment Variables

```bash
# sentinel/.env (add these)

# CyberMesh Node Configuration
NODE_ID=1
ENVIRONMENT=development

# Ed25519 Signing Key
ED25519_SIGNING_KEY_PATH=keys/signing_key.pem

# Kafka Configuration
KAFKA_BOOTSTRAP_SERVERS=pkc-xxxxx.region.gcp.confluent.cloud:9092
KAFKA_SASL_USERNAME=your-api-key
KAFKA_SASL_PASSWORD=your-api-secret
KAFKA_TLS_ENABLED=true
KAFKA_SASL_MECHANISM=PLAIN

# Topics
KAFKA_TOPIC_ANOMALIES=ai.anomalies.v1
KAFKA_TOPIC_COMMITS=control.commits.v1
KAFKA_TOPIC_DLQ=ai.anomalies.v1.dlq

# Detection Settings
DETECTION_PUBLISH_THRESHOLD=0.7    # Min confidence to publish
DETECTION_MIN_SEVERITY=5           # Min severity (1-10)

# Rate Limiting
RATE_LIMIT_DETECTIONS_PER_SEC=100
```

### Ed25519 Key Generation

```bash
# Generate signing key
openssl genpkey -algorithm ED25519 -out keys/signing_key.pem

# Extract public key (for backend validator trust store)
openssl pkey -in keys/signing_key.pem -pubout -out keys/signing_key_public.pem
```

---

## Dependencies to Add

```txt
# Add to sentinel/requirements.txt

# CyberMesh Integration
confluent-kafka>=2.3.0
protobuf>=4.24.0
grpcio-tools>=1.59.0
prometheus-client>=0.18.0
```

---

## Data Flow Summary

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           SENTINEL + CYBERMESH                               │
└─────────────────────────────────────────────────────────────────────────────┘

1. FILE ANALYSIS (Sentinel)
   File Upload → Parser → [Static, ML, YARA, ThreatIntel] → Coordinator
                                                               │
2. DECISION                                                    ▼
   threat_level >= SUSPICIOUS? ──YES──> Publish to CyberMesh
                                │
                                NO──> Store locally only

3. MESSAGE CREATION
   FileAnalysisData → FileAnomalyMessage → Ed25519 Sign → Protobuf Serialize

4. KAFKA PUBLISH
   Protobuf bytes → AIProducer → ai.anomalies.v1 topic

5. BACKEND CONSENSUS (CyberMesh)
   Kafka Consumer → Signature Verify → Mempool → PBFT Consensus
                                                      │
6. ENFORCEMENT (if confirmed)                         ▼
   control.policy.v1 → Enforcement Agent → iptables/NetworkPolicy

7. FEEDBACK LOOP
   control.commits.v1 → Sentinel FeedbackService → Adjust thresholds
```

---

## Timeline Estimate

| Phase | Tasks | Duration |
|-------|-------|----------|
| **Phase 1** | Core infrastructure (crypto, utils, proto) | 2-3 days |
| **Phase 2** | Kafka integration | 2 days |
| **Phase 3** | Service layer (manager, publisher) | 3-4 days |
| **Phase 4** | Agent integration + CLI/API | 2 days |
| **Phase 5** | Testing & validation | 2 days |
| **Buffer** | Bug fixes, edge cases | 2 days |

**Total: ~2 weeks**

---

## Success Criteria

1. ✅ Sentinel can scan a file and produce `AnomalyEvent` to Kafka
2. ✅ Messages are signed with Ed25519 and verified by backend
3. ✅ Backend validators reach consensus on file malware detection
4. ✅ Enforcement agent blocks based on file analysis (if policy created)
5. ✅ Feedback loop adjusts Sentinel thresholds based on acceptance rate
6. ✅ Health endpoints work for Kubernetes deployment
7. ✅ Prometheus metrics exposed for observability

---

## Questions to Resolve

1. **File submission flow**: Watch directory vs. API upload vs. both?
2. **Policy creation**: Should Sentinel directly create block policies, or just anomalies?
3. **Feedback granularity**: Per-file or aggregated threshold adjustment?
4. **Multi-node**: Multiple Sentinel instances publishing to same topic?
5. **Backward compatibility**: Should messages work with existing ai-service schema?

---

## References

- AI Service: `B:\CyberMesh\ai-service\`
- Backend: `B:\CyberMesh\backend\`
- Enforcement Agent: `B:\CyberMesh\enforcement-agent\`
- Proto definitions: `B:\CyberMesh\ai-service\proto\`
- Contracts: `B:\CyberMesh\ai-service\src\contracts\`
