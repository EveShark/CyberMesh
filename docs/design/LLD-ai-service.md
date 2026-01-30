# CyberMesh AI Service - Low-Level Design (LLD)

**Version:** 2.0.0  
**Last Updated:** 2026-01-30

---

## 📑 Navigation

**Quick Links:**
- [🏗️ Module Architecture](#2-module-architecture)
- [🤖 Detection Pipeline](#3-detection-pipeline-srcml)
- [🗳️ Ensemble Voting](#4-ensemble-voting-srcmlensemblepy)
- [🔄 Feedback Service](#5-feedback-service-srcfeedback)
- [📊 ML Models](#8-ml-models)

---

## 1. Overview

The AI Service is a **Python-based ML detection pipeline** that analyzes network telemetry, detects threats using 3 detection engines, and publishes cryptographically signed alerts to Kafka.

> [!IMPORTANT]
> The AI Service uses **3 weighted engines** (Rules 30%, Math 20%, ML 50%) with adaptive threshold tuning based on validator feedback.

---

## 2. Module Architecture

### 2.1 Package Structure

```mermaid
graph TB
   subgraph cmd["cmd/"]
        main[main.py<br/>Entry point]
    end
    
    subgraph src["src/"]
        service[service/]
        ml[ml/]
        feedback[feedback/]
        kafka[kafka/]
        utils[utils/]
        api[api/]
        config[config/]
    end
    
    main --> service
    service --> ml
    service --> kafka
    service --> feedback
    ml --> utils
    feedback --> kafka
    
    classDef entry fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef module fill:#fff9c4,stroke:#f57f17,color:#000;
    
    class main entry;
    class service,ml,feedback,kafka,utils,api,config module;
```

---

## 3. Detection Pipeline (`src/ml/`)

### 3.1 Pipeline Class Diagram

```mermaid
classDiagram
    class DetectionPipeline {
        -telemetry_source TelemetrySource
        -feature_adapter FeatureAdapter
        -engines List~Engine~
        -ensemble EnsembleVoter
        -evidence_gen EvidenceGenerator
        +process() InstrumentedResult
    }
    
    class TelemetrySource {
        <<interface>>
        +poll() List~Flow~
    }
    
    class FileTelemetrySource {
        -file_path str
        -cursor int
        +poll() List~Flow~
    }
    
    class PostgresTelemetrySource {
        -connection Connection
        +poll() List~Flow~
    }
    
    class FeatureAdapter {
        +extract(flow) FeatureVector
        +normalize(features) FeatureVector
    }
    
    TelemetrySource <|.. FileTelemetrySource
    TelemetrySource <|.. PostgresTelemetrySource
    DetectionPipeline --> TelemetrySource
    DetectionPipeline --> FeatureAdapter
```

### 3.2 Detection Engines

```mermaid
classDiagram
    class Engine {
        <<interface>>
        +predict(features) List~DetectionCandidate~
        +engine_type EngineType
        +is_ready bool
        +get_metadata() dict
    }
    
    class RulesEngine {
        -thresholds dict
        +predict(features) List~DetectionCandidate~
    }
    
    class MathEngine {
        +predict(features) List~DetectionCandidate~
        -calculate_zscore(values) float
        -calculate_entropy(values) float
    }
    
    class MLEngine {
        -models dict
        -model_cache ModelCache
        +predict(features) List~DetectionCandidate~
        +load_model(path) Model
    }
    
    Engine <|.. RulesEngine
    Engine <|.. MathEngine
    Engine <|.. MLEngine
```

### 3.3 Rules Engine Thresholds

| Rule | Feature | Default Threshold | Config |
|------|---------|-------------------|--------|
| DDoS (rule) | pps | > 1,000,000 | `DDOS_PPS_THRESHOLD` |
| Port Scan (rule) | unique_dst_ports | > 500 | `PORT_SCAN_THRESHOLD` |
| SYN Flood (rule) | syn_ack_ratio | > 10.0 | `SYN_ACK_RATIO_THRESHOLD` |
| Malware (rule) | entropy | > 7.5 | `MALWARE_ENTROPY_THRESHOLD` |

---

## 4. Ensemble Voting (`src/ml/ensemble.py`)

### 4.1 Voting Algorithm

```mermaid
flowchart TB
    subgraph Input
        R[Rules Engine<br/>Weight: 0.3]
        M[Math Engine<br/>Weight: 0.2]
        ML[ML Engine<br/>Weight: 0.5]
    end
    
    subgraph Calculation
        W[Adjusted Weight<br/>engine_weight × trust / uncertainty + ε]
        S[Weighted Final Score]
        LLR[LLR Score]
    end
    
    subgraph Decision
        AB{confidence >= MIN_CONFIDENCE?}
        TH{final_score >= adaptive threshold?}
    end
    
    R & M & ML --> W --> S --> LLR
    LLR --> AB
    AB -->|Yes| TH
    AB -->|No| ABSTAIN[Abstain]
    TH -->|Yes| PUBLISH[Publish]
    TH -->|No| ABSTAIN
    
    style R fill:#ffebee,stroke:#c62828,color:#000;
    style M fill:#fff3e0,stroke:#f57f17,color:#000;
    style ML fill:#e3f2fd,stroke:#1565c0,color:#000;
    style PUBLISH fill:#c8e6c9,stroke:#2e7d32,color:#000;
    style ABSTAIN fill:#f5f5f5,stroke:#9e9e9e,color:#000;
```

### 4.2 Thresholds by Type

| Anomaly Type | Base Threshold | Range |
|--------------|----------------|-------|
| DDoS | 0.85 | [0.50, 0.99] |
| Malware | 0.90 | [0.50, 0.99] |
| Anomaly | 0.75 | [0.50, 0.99] |
| Network Intrusion | 0.75 | [0.50, 0.99] |

> [!NOTE]
> Thresholds are **adaptive** and adjusted based on validator acceptance rates from the feedback loop.

---

## 5. Feedback Service (`src/feedback/`)

### 5.1 Component Diagram

```mermaid
classDiagram
    class FeedbackService {
        -tracker AnomalyLifecycleTracker
        -calibrator ConfidenceCalibrator
        -threshold_mgr ThresholdManager
        +on_commit(event)
        +on_rejection(event)
    }
    
    class AnomalyLifecycleTracker {
        -redis Redis
        +update_state(id, state)
        +get_state(id) State
        +get_acceptance_metrics() Metrics
    }
    
    class ConfidenceCalibrator {
        -method str
        -model IsotonicRegression
        +calibrate(raw_score) float
        +retrain(samples)
    }
    
    class ThresholdManager {
        -thresholds dict
        +adjust(anomaly_type, acceptance_rate)
        +get_threshold(anomaly_type) float
    }
    
    FeedbackService --> AnomalyLifecycleTracker
    FeedbackService --> ConfidenceCalibrator
    FeedbackService --> ThresholdManager
```

### 5.2 Calibration Methods

| Method | Algorithm | Use Case |
|--------|-----------|----------|
| Isotonic | Non-parametric monotonic | Default, flexible |
| Platt | Logistic regression | Fast, simple |

---

## 6. Kafka Integration (`src/kafka/`)

### 6.1 Producer/Consumer

```mermaid
classDiagram
    class AIProducer {
        -client Producer
        -signer Signer
        +send_anomaly(msg)
        +send_evidence(msg)
        +send_policy(msg)
    }
    
    class AIConsumer {
        -client Consumer
        -handlers dict
        +start()
        +stop()
        -dispatch(message)
    }
    
    class Signer {
        -private_key Ed25519PrivateKey
        -domain_separation bytes
        +sign(data, domain) signature, pubkey, key_id
        +verify(data, signature, pubkey, domain) bool
    }
    
    class NonceManager {
        -instance_id uint32
        -counter uint32
        +generate() bytes
        +validate(nonce) bool
    }
    
    AIProducer --> Signer
    AIProducer --> NonceManager
```

> [!WARNING]
> **Nonce Format (16 bytes):** `[8B timestamp_ms][4B instance_id][4B monotonic_counter]` - Must be unique per message.

---

## 7. Service Manager (`src/service/`)

### 7.1 Lifecycle State Machine

```mermaid
stateDiagram-v2
    [*] --> UNINITIALIZED
    UNINITIALIZED --> INITIALIZED: init()
    INITIALIZED --> STARTING: start()
    STARTING --> RUNNING: startup complete
    RUNNING --> STOPPING: stop()
    STOPPING --> STOPPED: cleanup complete
    STOPPED --> [*]
    
    RUNNING --> RUNNING: detection loop (5s interval)
```

### 7.2 Detection Loop

```mermaid
sequenceDiagram
    participant DL as DetectionLoop
    participant TS as TelemetrySource
    participant DP as DetectionPipeline
    participant RL as RateLimiter
    participant PUB as Publisher
    
    loop Every DETECTION_INTERVAL (5s)
        DL->>TS: poll()
        TS->>DL: flows[]
        DL->>DP: process(flows)
        DP->>DL: detections[]
        
        loop Each detection
            DL->>RL: allow()
            alt Allowed
                DL->>PUB: publish(detection)
            else Rate limited
                DL->>DL: skip
            end
        end
        
        DL->>DL: sleep(DETECTION_INTERVAL)
    end
```

---

## 8. ML Models

### 8.1 Model Registry

```mermaid
classDiagram
    class ModelRegistry {
        -models dict
        -signatures dict
        +load(name) Model
        +verify(name) bool
        +hot_reload()
    }
    
    class Model {
        -algorithm str
        -features int
        -version str
        +predict(features) float
    }
    
    class DDoSModel {
        -lgbm LGBMClassifier
    }
    
    class AnomalyModel {
        -iforest IsolationForest
    }
    
    class MalwareModel {
        -lgbm LGBMClassifier
        -variant str
    }
    
    ModelRegistry --> Model
    Model <|-- DDoSModel
    Model <|-- AnomalyModel
    Model <|-- MalwareModel
```

### 8.2 Model Specifications

| Model | Algorithm | Features | AUC | Signature |
|-------|-----------|----------|-----|-----------|
| ddos.pkl | LightGBM | 79 | 0.999 | ✅ Ed25519 |
| anomaly.pkl | IsolationForest | 30 | 1.000 | ✅ Ed25519 |
| malware_flow.pkl | LightGBM | 39 | 0.95 | ✅ Ed25519 |

---

## 9. API Endpoints (`src/api/`)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check ✅ |
| `/ready` | GET | Readiness probe ✅ |
| `/metrics` | GET | Prometheus metrics 📊 |
| `/detections/stats` | GET | Detection statistics |

---

## 10. Key Files Reference

| File | Purpose | Lines |
|------|---------|-------|
| `cmd/main.py` | Entry point | ~200 |
| `src/ml/pipeline.py` | Detection pipeline | ~300 |
| `src/ml/detectors.py` | 3 engines | ~400 |
| `src/ml/ensemble.py` | Ensemble voting | ~250 |
| `src/feedback/tracker.py` | Lifecycle tracker | ~350 |
| `src/feedback/calibrator.py` | Confidence calibration | ~200 |
| `src/kafka/producer.py` | Kafka producer | ~150 |

---

## 11. Related Documents

### Design Documents
- [HLD](./HLD.md) - High-level design
- [Data Flow](./DATA_FLOW.md) - System data flow

### Architecture Documents
- [AI Detection Pipeline](../architecture/02_ai_detection_pipeline.md)
- [Feedback Loop](../architecture/06_feedback_loop.md)

### Source Code
- [AI Service README](../../ai-service/README.md)

---

**[⬆️ Back to Top](#-navigation)**
