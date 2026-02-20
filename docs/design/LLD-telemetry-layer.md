# CyberMesh Telemetry Layer - Low-Level Design

**Version:** 1
**Last Updated:** 2026-02-20  
**Authors:** Architecture Team

---

## Navigation

- [Architecture](#2-architecture)
- [Components](#3-components)
- [Kafka Topics](#4-kafka-topics)
- [Schemas](#5-schemas)
- [Security and Validation](#6-security--validation)
- [Validation Gates](#7-validation-gate-driven)
- [Deployment Notes](#8-deployment-notes)

---

## 1. Overview

This layer is the ingestion and normalization path between raw network signals and Sentinel/AI consumers.
It ingests flows/alerts/PCAP requests, validates and normalizes them, aggregates by flow window, derives CIC features, and publishes feature vectors for AI consumers.

**Design goals:**
- One canonical path for K8s/Hubble, Zeek/Suricata, IPFIX, and PCAP workflows.
- Schema-first contracts with explicit DLQ behavior for invalid payloads.
- Support both central and edge feature extraction without changing downstream topics.

---

## 2. Architecture

### 2.1 Module Architecture (Data Path)

```mermaid
graph TB
  subgraph Telemetry["Telemetry Layer"]
    A[Adapters<br/>Zeek/Suricata/IPFIX]
    SP[Stream Processor<br/>Aggregation + Validation]
    FT[Feature Transformer<br/>CIC v1]
    PCAP[PCAP Service<br/>Request/Result]
  end

  subgraph Downstream["Downstream Consumers"]
    S[Sentinel]
    AI[AI Service]
  end

  subgraph Kafka["Kafka Topics"]
    F1[telemetry.flow.v1]
    SV[sentinel.verdicts.v1]
    F2[telemetry.flow.agg.v1]
    F3[telemetry.features.v1]
    DF[telemetry.deepflow.v1]
    PR[pcap.request.v1]
    PRR[pcap.result.v1]
    DLQ1[telemetry.flow.v1.dlq]
    DLQ2[telemetry.features.v1.dlq]
    DLQ3[telemetry.deepflow.v1.dlq]
    DLQ4[pcap.result.v1.dlq]
  end

  A --> F1
  A --> DF
  DF --> SP --> F2 --> FT --> F3
  F1 --> S --> SV --> AI
  F3 --> AI
  PR --> PCAP --> PRR
```

---

## 3. Components

### 3.1 Adapters (Go)
**Purpose:** Convert external sources into canonical telemetry events.  
**Inputs:** Zeek JSON, Suricata EVE, IPFIX, cloud flow logs, gateway/baremetal sensor records.  
**Outputs:** `telemetry.flow.v1` and/or `telemetry.deepflow.v1` + DLQ on parse/validation errors.

**Key files:**
- `telemetry-layer/adapters/internal/deepflow/runner.go`
- `telemetry-layer/adapters/internal/parser/deepflow.go`
- `telemetry-layer/adapters/cmd/zeek/main.go`
- `telemetry-layer/adapters/cmd/suricata/main.go`

**Validation:** `telemetry-layer/adapters/internal/validate/deepflow.go`

---

### 3.2 Stream Processor (Go)
**Purpose:** Aggregate flows per 5‑tuple window and normalize metadata.  
**Inputs:** `telemetry.flow.v1` + optional `telemetry.deepflow.v1`  
**Outputs:** `telemetry.flow.agg.v1` + DLQ on schema/validation errors.

**Key files:**
- `telemetry-layer/stream-processor/cmd/processor/main.go`
- `telemetry-layer/stream-processor/internal/aggregate/`
- `telemetry-layer/stream-processor/internal/codec/`

**Deepflow bridge:** controlled by `DEEPFLOW_FLOW_BRIDGE_ENABLED=true`.

---

### 3.3 Feature Transformer (Python)
**Purpose:** Convert `flow.agg.v1` into CIC feature vectors.  
**Inputs:** `telemetry.flow.agg.v1`  
**Outputs:** `telemetry.features.v1` (JSON or Protobuf)

**Key files:**
- `telemetry-layer/feature-transformer_python/transformer.py`
- `telemetry-layer/feature-transformer_python/config.py`

**Edge/Central routing:**
- `FEATURE_ROLE` (`central` or `edge`)
- `EDGE_FEATURE_SOURCE_IDS`, `EDGE_FEATURE_SOURCE_TYPES`

---

### 3.4 PCAP Service (Go)
**Purpose:** Process `pcap.request.v1` and publish `pcap.result.v1`.  
**Inputs:** `pcap.request.v1`  
**Outputs:** `pcap.result.v1` + DLQ on error.

**Key files:**
- `telemetry-layer/pcap-service/cmd/pcap/main.go`
- `telemetry-layer/pcap-service/internal/validate/request.go`
- `telemetry-layer/pcap-service/internal/storage/`
- `telemetry-layer/pcap-service/internal/capture/`

**Modes:** `mock`, `file`, `libpcap` (compile with `-tags=pcap`).

---

### 3.5 Package Structure (Developer Map)

This diagram is intentionally "where is the code" rather than "what is the data flow".

```mermaid
graph TB
  subgraph TL["telemetry-layer/"]
    subgraph A["adapters/ (Go)"]
      A1["cmd/baremetal/main.go"]
      A2["cmd/gateway/main.go"]
      A3["cmd/cloudlogs/main.go"]
      A4["cmd/zeek/main.go"]
      A5["cmd/suricata/main.go"]
      Aint["internal/<br/>parser, validate, kafka, adapter"]
    end
    subgraph SP["stream-processor/ (Go)"]
      SP1["cmd/processor/main.go"]
      SPint["internal/<br/>aggregate, codec, kafka, validate"]
    end
    subgraph FT["feature-transformer_python/ (Python)"]
      FT1["transformer.py"]
      FT2["config.py"]
    end
    subgraph P["pcap-service/ (Go)"]
      P1["cmd/pcap/main.go"]
      Pint["internal/<br/>validate, capture, storage, kafka"]
    end
    subgraph Proto["proto/"]
      Psrc["*.proto"]
      G["gen/go/"]
      Py["gen/python/"]
    end
  end
```

---

## 4. Kafka Topics

| Topic | Producer | Consumer | Purpose |
|------|----------|----------|---------|
| `telemetry.flow.v1` | Ingest/bridge | Stream processor, Sentinel | Raw flows |
| `sentinel.verdicts.v1` | Sentinel | AI service | Multi-agent verdict stream |
| `telemetry.flow.agg.v1` | Stream processor | Feature transformer | Aggregated flows |
| `telemetry.features.v1` | Feature transformer | AI service/Ops | CIC features |
| `telemetry.deepflow.v1` | Adapters | Stream processor | IDS/deepflow events |
| `pcap.request.v1` | Backend/AI/Ops | PCAP service | Capture request |
| `pcap.result.v1` | PCAP service | Backend/AI/Ops | Capture result |
| `*.dlq` | Telemetry components | Ops | Invalid/error messages |

---

## 5. Schemas

**Location:** `telemetry-layer/proto/`  
**Primary v1 schemas:**
- `telemetry_flow_v1.proto`
- `telemetry_deepflow_v1.proto`
- `telemetry_feature_v1.proto`
- `telemetry_pcap_request_v1.proto`
- `telemetry_pcap_result_v1.proto`
- `telemetry_dlq_v1.proto`

**Wire formats:** JSON (dev/test) and Protobuf (canonical).

---

## 6. Security & Validation

### 6.1 DLQ Semantics (No Silent Failure)

DLQs are owned by the producer component (not Kafka). A DLQ record should carry:
- `error_code` (stable, searchable)
- `reason` (human readable)
- original `topic`/`partition`/`offset` if available
- original payload bytes (or truncated/redacted form if size/PII constraints apply)

```mermaid
flowchart TB
  In[Kafka record] --> Decode{Decode}
  Decode -->|ok| Validate{Validate}
  Decode -->|fail| DLQ[Publish to *.dlq\nerror_code=DECODE\nreason=...]

  Validate -->|ok| SR{Schema Registry enabled?}
  Validate -->|fail| DLQ2[Publish to *.dlq\nerror_code=VALIDATION\nreason=...]

  SR -->|no| Pub[Publish to output topic]
  SR -->|yes| SRV{Registry validate}
  SRV -->|ok| Pub
  SRV -->|fail| DLQ3[Publish to *.dlq\nerror_code=SCHEMA\nreason=...]
```

### 6.2 Feature Coverage (Missing Features Are Security-Significant)

If upstream capture cannot provide timing/flags/IAT/active-idle features, the transformer must:
- mark them as missing in `feature_mask`
- compute `feature_coverage` accurately
- avoid silently coercing missing values to numeric zeros in a way that looks "present"

### 6.3 Schema Registry (Optional)

When enabled, producers validate payloads against the registry subject policy before publishing to canonical topics.

**PCAP controls:**
- Tenant/requester allowlists.
- Duration and max‑bytes limits.
- Optional retention and legal‑hold tags.

---

## 7. Validation (Gate-Driven)

Gate-driven validation provides deterministic "known good" checks without relying on old Kafka offsets or shared topics.

Recommended gate-driven validation (local/staging):
- Telemetry -> AI ingest: `python telemetry-layer/scripts/smoke_telemetry_to_ai_publish.py --env env/integration_test.env`
- Flow pipeline: `python telemetry-layer/scripts/gates/gates_flow_pipeline.py --env env/integration_test.env --gate b|c|d`
- Deepflow + PCAP: `python telemetry-layer/scripts/gates/gate_phase3_deepflow_pcap.py --env env/integration_test.env --timeout-sec 90`

Notes:
- Gates use isolated Kafka topics per run so they are not affected by deployed components or old offsets.
- Use `env/integration_test.env` so Telemetry/AI/Backend read consistent Kafka creds and topic overrides.

---

## 8. Deployment Notes

**Recommended workloads**
- `telemetry-stream-processor` (Deployment)
- `telemetry-feature-transformer` (Deployment)
- `telemetry-adapters` (Deployment/DaemonSet per segment)
- `telemetry-pcap-service` (Deployment)

**Config/Secrets**
- `telemetry-config` (runtime config)
- `telemetry-secrets` (Kafka + Schema Registry credentials)

---

## Appendix A: End-to-End Sequences

### A.1 Happy Path (Flows -> Features -> AI)

```mermaid
sequenceDiagram
  autonumber
  participant S as Sensor/Bridge/Adapter
  participant K as Kafka
  participant SP as Stream Processor
  participant FT as Feature Transformer
  participant AI as AI Service

  S->>K: Produce FlowV1 (telemetry.flow.v1)
  SP->>K: Consume telemetry.flow.v1
  SP->>K: Produce FlowAggV1 (telemetry.flow.agg.v1)
  FT->>K: Consume telemetry.flow.agg.v1
  FT->>K: Produce CicFeaturesV1 (telemetry.features.v1)
  AI->>K: Consume telemetry.features.v1
  AI->>K: Produce AnomalyEvent (ai.anomalies.v1)
```

### A.2 Deepflow (Zeek/Suricata -> DeepFlowV1 -> Optional Bridge)

```mermaid
sequenceDiagram
  autonumber
  participant IDS as Zeek/Suricata
  participant A as Deepflow Adapter
  participant K as Kafka
  participant SP as Stream Processor

  IDS->>A: JSON logs (EVE/Zeek)
  A->>K: DeepFlowV1 (telemetry.deepflow.v1)
  SP->>K: Consume telemetry.deepflow.v1 (optional)
  note over SP: Optional deepflow->flow bridge\n(DEEPFLOW_FLOW_BRIDGE_ENABLED=true)
  SP->>K: Publish FlowAgg updates (telemetry.flow.agg.v1)
```

### A.3 PCAP Request -> Result (Access Controlled)

```mermaid
sequenceDiagram
  autonumber
  participant Caller as Backend/AI/Ops
  participant K as Kafka
  participant P as PCAP Service
  participant Store as Object Store (optional)

  Caller->>K: PcapRequestV1 (pcap.request.v1)
  P->>K: Consume pcap.request.v1
  P->>P: AuthZ allowlists + limits
  alt Allowed
    P->>P: Capture (dry-run/file/libpcap)
    opt Store enabled
      P->>Store: Put pcap bytes + metadata
    end
    P->>K: PcapResultV1 (pcap.result.v1)
  else Denied/Invalid
    P->>K: DLQ record (pcap.result.v1.dlq)
  end
```

---

## Appendix B: Deployment Topology (Typical)

```mermaid
graph TB
  subgraph Segments["Network Segments / Clusters"]
    DS1["Adapters<br/>DaemonSet/Deployment"]
    GW["North-South Gateway Sensor<br/>optional"]
    DS2["Enforcement Agent<br/>DaemonSet"]
  end

  subgraph Telemetry["Telemetry Layer (Central)"]
    SP["Stream Processor<br/>Deployment"]
    FT["Feature Transformer<br/>Deployment"]
    PCAP["PCAP Service<br/>Deployment"]
  end

  subgraph Platform["Platform Services"]
    K[(Kafka)]
    SR[(Schema Registry)]
    DB[(DB - optional sinks)]
  end

  subgraph AI["AI/Backend"]
    AIS[AI Service]
    BE[Backend]
  end

  DS1 -->|telemetry.flow.v1 / telemetry.deepflow.v1| K
  GW -->|telemetry.flow.v1| K
  K --> SP -->|telemetry.flow.agg.v1| K
  K --> FT -->|telemetry.features.v1| K
  K --> PCAP -->|pcap.result.v1| K
  K --> AIS
  AIS -->|ai.anomalies.v1| K --> BE

  SR -. optional validate .- DS1
  SR -. optional validate .- SP
  SR -. optional validate .- FT
  SR -. optional validate .- PCAP
```

**[⬆️ Back to Top](#-navigation)**
