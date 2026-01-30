# CyberMesh High-Level Design (HLD)

**Version:** 2.0.0  
**Last Updated:** 2026-01-30  
**Authors:** Architecture Team

---

## 📑 Navigation

**Quick Links:**
- [🎯 Executive Summary](#1-executive-summary)
- [🏗️ Architecture](#2-system-context-c4-level-1)
- [⚡ Performance](#81-performance)
- [🔒 Security](#83-security)
- [📚 Related Docs](#9-related-documentation)

---

## 1. Executive Summary

CyberMesh is a **distributed cybersecurity threat detection and response platform** that combines real-time ML-based anomaly detection with Byzantine Fault Tolerant (BFT) consensus to validate and enforce security policies across a network.

### 🎯 Key Capabilities

| Capability | Description |
|------------|-------------|
| ⚡ **Real-time Detection** | 3-engine ML pipeline (Rules, Math, ML) with 5s interval |
| 🛡️ **BFT Consensus** | 5 validator nodes, tolerates 1 Byzantine failure |
| 🔐 **Cryptographic Integrity** | Ed25519 signatures on all messages |
| 🔄 **Adaptive Learning** | Validator feedback loop for threshold tuning |
| 🚀 **Automated Enforcement** | iptables/nftables/Kubernetes policy automation |

> [!IMPORTANT]
> CyberMesh uses **HotStuff 2-chain consensus** for lower latency compared to traditional PBFT. Block finality typically occurs within 1-2 seconds under healthy network conditions.

---

## 2. System Context (C4 Level 1)

```mermaid
graph TB
    User[Security Operations Team]
    
    subgraph CyberMesh["CyberMesh Platform"]
        Core[Core Services]
    end
    
    Network[Network Infrastructure<br/>Routers, Switches, Endpoints]
    Kafka[(Kafka Cluster<br/>Confluent Cloud)]
    DB[(CockroachDB<br/>Cloud)]
    Cache[(Redis<br/>Upstash)]
    
    Network -->|Telemetry Data| Core
    User -->|Monitor & Review| Core
    Core -->|Pub/Sub| Kafka
    Core -->|Persist State| DB
    Core -->|Feedback State| Cache
    Core -->|Enforce Policies| Network
    
    classDef platform fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef external fill:#fff3e0,stroke:#f57f17,color:#000;
    classDef data fill:#c8e6c9,stroke:#2e7d32,color:#000;
    
    class Core platform;
    class Network,Kafka external;
    class DB,Cache data;
```

---

## 3. Container Diagram (C4 Level 2)

The container diagram shows the major deployable components within CyberMesh.

```mermaid
graph TB
    User[Security Ops]
    
    subgraph CyberMesh["CyberMesh Platform"]
        FE[Frontend<br/>React/TypeScript<br/>Dashboard UI]
        BE[Backend Validators<br/>Go<br/>BFT Consensus & API]
        AI[AI Service<br/>Python<br/>ML Detection Pipeline]
        Agent[Enforcement Agent<br/>Go<br/>DaemonSet Policy Enforcer]
    end
    
    Kafka[(Kafka)]
    DB[(CockroachDB)]
    Redis[(Redis)]
    
    User -->|HTTPS| FE
    FE -->|REST API| BE
    AI -->|Publish Detections| Kafka
    BE -->|Consume Detections| Kafka
    BE -->|Publish Commits/Policies| Kafka
    BE -->|Persist Blocks| DB
    AI -->|Feedback State| Redis
    Agent -->|Subscribe Policies| Kafka
    Agent -.->|ACK Optional| Kafka
    
    classDef frontend fill:#61dafb,stroke:#000,color:#000;
    classDef backend fill:#00add8,stroke:#000,color:#fff;
    classDef ai fill:#3776ab,stroke:#000,color:#fff;
    classDef data fill:#c8e6c9,stroke:#2e7d32,color:#000;
    
    class FE frontend;
    class BE,Agent backend;
    class AI ai;
    class Kafka,DB,Redis data;
```

---

## 4. Component Architecture

### 4.1 High-Level Data Flow

```mermaid
flowchart LR
    subgraph Sources
        T[Network Telemetry]
    end
    
    subgraph AI["AI Service"]
        FE[Feature Extraction<br/>79 features]
        DE[Detection Engines<br/>Rules + Math + ML]
        EN[Ensemble Voter]
        EV[Evidence Generator]
        SG[Ed25519 Signer]
    end
    
    subgraph Kafka["Kafka Topics"]
        K1[ai.anomalies.v1]
        K2[control.commits.v1]
        K3[control.policy.v1]
        K4[control.policy.ack.v1]
    end
    
    subgraph Backend["Backend Validators x5"]
        VE[Signature Verifier]
        MP[Mempool]
        CO[HotStuff Consensus]
        SM[State Machine]
        PE[Persistence]
    end
    
    subgraph Enforcement["Enforcement Agent"]
        PC[Policy Consumer]
        EN2[Enforcer<br/>iptables/nftables/k8s]
        AC[Ack Publisher<br/>optional]
    end
    
    subgraph Storage
        DB[(CockroachDB)]
    end
    
    T --> FE --> DE --> EN --> EV --> SG
    SG --> K1
    K1 --> VE --> MP --> CO --> SM --> PE --> DB
    CO --> K2
    CO --> K3
    AC --> K4
    K3 --> PC --> EN2
    EN2 --> AC
    
    classDef ai fill:#3776ab,stroke:#fff,color:#fff;
    classDef topic fill:#fff9c4,stroke:#f57f17,color:#000;
    classDef backend fill:#00add8,stroke:#fff,color:#fff;
    classDef agent fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef db fill:#c8e6c9,stroke:#2e7d32,color:#000;
    
    class FE,DE,EN,EV,SG ai;
    class K1,K2,K3,K4 topic;
    class VE,MP,CO,SM,PE backend;
    class PC,EN2,AC agent;
    class DB db;
```

### 4.2 Component Summary

| Component | Technology | Purpose | Port |
|-----------|------------|---------|------|
| **Frontend** | React, TypeScript, Vite | Dashboard UI | 3000 (in-cluster) |
| **Backend Validators** | Go 1.25.1 | BFT consensus, API | 443 (HTTPS), 8001 (P2P), 9100 (metrics) |
| **AI Service** | Python 3.11, LightGBM | ML detection | 8080 (API), 10000 (metrics) |
| **Enforcement Agent** | Go 1.25.1 | Policy enforcement | 9094 (metrics/health/control) |
| **Kafka** | Confluent Cloud | Message broker | 9092 (TLS) |
| **CockroachDB** | CockroachDB 21+ | Distributed SQL | 26257 |
| **Redis** | Upstash Redis | Caching, state | 6379 |

> [!NOTE]
> Ports above reflect current `k8s_gke/` manifests. Local/dev ports may differ.

---

## 5. Key Architectural Decisions

### 5.1 BFT Consensus (HotStuff)

| Decision | Rationale |
|----------|-----------|
| **5 validators** | Tolerates f=1 Byzantine failure (2f+1 = 3 quorum) |
| **HotStuff 2-chain** | Lower latency than classic 3-phase PBFT |
| **Leader rotation** | Round-robin with reputation scoring |
| **Heartbeat protocol** | 500ms interval, 3s timeout for leader failure detection |

### 5.2 3-Engine ML Pipeline

```mermaid
flowchart TD
    Input[Telemetry Data] --> Extract[Feature Extraction<br/>79 Features]
    
    Extract --> E1[Rules Engine<br/>Weight: 0.3]
    Extract --> E2[Math Engine<br/>Weight: 0.2]  
    Extract --> E3[ML Engine<br/>Weight: 0.5]
    
    E1 & E2 & E3 --> Vote[Ensemble Voter<br/>Weighted Average]
    
    Vote -->|Score >= Threshold| Detect[Anomaly Detected]
    Vote -->|Score < Threshold| Normal[Normal Traffic]
    
    style E1 fill:#ffebee,stroke:#c62828,color:#000;
    style E2 fill:#fff3e0,stroke:#f57f17,color:#000;
    style E3 fill:#e3f2fd,stroke:#1565c0,color:#000;
    style Detect fill:#c8e6c9,stroke:#2e7d32,color:#000;
    style Normal fill:#f5f5f5,stroke:#9e9e9e,color:#000;
```

| Engine | Technique | Weight | Use Case |
|--------|-----------|--------|----------|
| **Rules** | Threshold-based | 0.3 | DDoS pps, port scans |
| **Math** | Statistical (Z-score, entropy, CUSUM) | 0.2 | Statistical anomalies |
| **ML** | LightGBM models | 0.5 | Complex pattern detection |

### 5.3 Cryptographic Security

```mermaid
flowchart TB
    subgraph Signing["Message Signing"]
        A[Message Payload] --> B[Domain Separation<br/>Prepend Topic Name]
        B --> C[Ed25519 Sign]
        E[Nonce 16 bytes] --> C
        C --> D[Signature 64 bytes]
    end
    
    subgraph Verification["Message Verification"]
        F[Received Message] --> G[Reconstruct Payload]
        G --> H[Ed25519 Verify]
        H --> I{Valid?}
        I -->|Yes| J[Accept to Pipeline]
        I -->|No| K[Reject to DLQ]
    end
    
    style C fill:#c8e6c9,stroke:#2e7d32,color:#000;
    style J fill:#c8e6c9,stroke:#2e7d32,color:#000;
    style K fill:#ffebee,stroke:#c62828,color:#000;
```

**Nonce Format (16 bytes):** `[8B timestamp_ms][4B instance_id][4B monotonic_counter]`

> [!WARNING]
> All Kafka messages MUST be signed with Ed25519. Messages without valid signatures are rejected to the DLQ topic.

---

## 6. Deployment Architecture

### 6.1 GKE Cluster Topology

```mermaid
graph TB
    subgraph Internet
        Users[End Users]
        CDN[Cloudflare CDN]
    end
    
    subgraph GKE["GKE Cluster us-central1"]
        subgraph ns["Namespace: cybermesh"]
            subgraph Validators["StatefulSet"]
                V0[validator-0<br/>NODE_ID=1]
                V1[validator-1<br/>NODE_ID=2]
                V2[validator-2<br/>NODE_ID=3]
                V3[validator-3<br/>NODE_ID=4]
                V4[validator-4<br/>NODE_ID=5]
            end
            
            AI[AI Service<br/>Deployment]
            FE[Frontend<br/>Deployment]
            PG[(Postgres<br/>StatefulSet)]
            
            subgraph DS["DaemonSet"]
                EA[Enforcement Agent<br/>Every Node]
            end
        end
    end
    
    subgraph External["External Services"]
        KF[Kafka<br/>Confluent Cloud]
        CR[CockroachDB<br/>Cloud]
        RD[Redis<br/>Upstash]
    end
    
    Users --> CDN
    CDN --> FE
    FE --> V0
    V0 & V1 & V2 & V3 & V4 <-.->|P2P Mesh| V0
    AI <-->|Pub/Sub| KF
    V0 <-->|Consume/Produce| KF
    V0 -->|Persist| CR
    AI -->|Cache| RD
    KF -->|Policies| EA
    
    classDef validator fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef service fill:#fff9c4,stroke:#f57f17,color:#000;
    classDef external fill:#c8e6c9,stroke:#2e7d32,color:#000;
    
    class V0,V1,V2,V3,V4 validator;
    class AI,FE,EA service;
    class KF,CR,RD,PG external;
```

### 6.2 Kubernetes Resources

| Resource | Type | Replicas | Notes |
|----------|------|----------|-------|
| validator | StatefulSet | 5 | P2P via headless service |
| ai-service | Deployment | 1 | HPA planned for scaling |
| frontend | Deployment | 2 | Behind Cloudflare CDN |
| enforcement-agent | DaemonSet | N | One per node (hostNetwork) |
| postgres | StatefulSet | 1 | Local AI cache |
| redis | Deployment | 1 | Optional local cache |

---

## 7. Integration Points

### 7.1 Kafka Topics

```mermaid
graph LR
    subgraph Producers
        AI_P[AI Service]
        BE_P[Backend]
    end
    
    subgraph Topics["Kafka Topics"]
        T1[ai.anomalies.v1]
        T2[ai.evidence.v1]
        T3[control.commits.v1]
        T4[control.policy.v1]
        T5[control.policy.ack.v1]
        T6[ai.dlq.v1]
    end
    
    subgraph Consumers
        BE_C[Backend]
        AI_C[AI Service]
        EA_C[Enforcement Agent]
    end
    
    AI_P --> T1 & T2
    BE_P --> T3 & T4
    EA_C --> T5
    
    T1 & T2 --> BE_C
    T3 --> AI_C
    T4 --> EA_C
    
    BE_C -.->|Invalid Msgs| T6
    
    classDef producer fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef topic fill:#fff9c4,stroke:#f57f17,color:#000;
    classDef consumer fill:#c8e6c9,stroke:#2e7d32,color:#000;
    
    class AI_P,BE_P producer;
    class T1,T2,T3,T4,T5,T6 topic;
    class BE_C,AI_C,EA_C consumer;
```

### 7.2 Wire Format

> [!NOTE]
> CyberMesh uses **Protobuf** message definitions as canonical wire contracts for Kafka payloads. Schemas live under `backend/proto/` and service-local `proto/` directories.

### 7.3 API Authentication

| Method | Description |
|--------|-------------|
| **mTLS Client Certs** | Production mode, role derived from CN |
| **Bearer Tokens** | Dev/staging, validated against allowlist |
| **RBAC** | Enabled when client CA configured |

---

## 8. Non-Functional Requirements

### 8.1 Performance

| Metric | Target | Current | Status |
|--------|--------|---------|--------|
| Detection latency | < 5s | 5s (loop interval) | ✅ Meeting |
| Consensus latency | < 2s | 1-2s (healthy) | ✅ Meeting |
| API response (P95) | < 500ms | ~200ms | ✅ Exceeding |
| Throughput | 100 detections/s | 100/s (rate limited) | ✅ Meeting |

### 8.2 Availability

| Component | Target | Strategy |
|-----------|--------|----------|
| Validators | 99.9% | 5-node BFT (tolerates 1 failure) |
| AI Service | 99.5% | Restart policy, HPA |
| Database | 99.99% | CockroachDB multi-region |

### 8.3 Security

- ✅ **TLS** for inter-service communication
- ✅ **mTLS** supported for production environments
- ✅ **Ed25519** signatures on all Kafka messages
- ✅ **Network Policies** restricting pod-to-pod traffic
- ✅ **Secrets** managed via Kubernetes Secrets

> [!CAUTION]
> In production, ALWAYS enable mTLS and signature verification. Dev mode relaxations should NEVER be deployed to production environments.

---

## 9. Related Documentation

### Architecture Documents
- [System Overview](../architecture/01_system_overview.md) - End-to-end data flow
- [AI Detection Pipeline](../architecture/02_ai_detection_pipeline.md) - ML pipeline details
- [HotStuff Consensus](../architecture/03_hotstuff_consensus.md) - BFT protocol
- [Kafka Message Bus](../architecture/04_kafka_message_bus.md) - Topic topology

### Design Documents
- [Backend LLD](./LLD-backend.md) - Go service internals
- [AI Service LLD](./LLD-ai-service.md) - Python ML architecture
- [Enforcement Agent LLD](./LLD-enforcement-agent.md) - Policy enforcement

### Source Code
- [Backend README](../../backend/README.md)
- [AI Service README](../../ai-service/README.md)
- [Enforcement Agent](../../enforcement-agent/BUILD_SUMMARY.md)

---

## 10. Technology Stack

| Layer | Technology |
|-------|------------|
| **Frontend** | React 18, TypeScript, Vite, TailwindCSS, Recharts |
| **Backend** | Go 1.25.1, libp2p, IBM Sarama (Kafka), pgx (DB) |
| **AI** | Python 3.11, LightGBM, scikit-learn, confluent-kafka |
| **Enforcement** | Go 1.25.1, iptables, nftables, client-go |
| **Database** | CockroachDB 21+, PostgreSQL 12+ |
| **Message Queue** | Apache Kafka (Confluent Cloud) |
| **Cache** | Redis 6+ (Upstash) |
| **Infrastructure** | GKE, Docker, Kubernetes 1.27+ |
| **CDN** | Cloudflare |
| **Monitoring** | Prometheus, Grafana (planned) |

---

**[⬆️ Back to Top](#-navigation)**
