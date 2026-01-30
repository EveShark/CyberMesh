# CyberMesh Data Flow Documentation

**Version:** 2.0.0  
**Last Updated:** 2026-01-30

---

## 📑 Navigation

**Quick Links:**
- [🔄 End-to-End Flow](#2-end-to-end-data-flow)
- [🤖 Detection Flow](#3-detection-flow-ai-service)
- [⚙️ Consensus Flow](#4-consensus-flow-backend)
- [🔁 Feedback Loop](#5-feedback-loop-flow)
- [🚫 Enforcement Flow](#6-enforcement-flow)
- [📨 Kafka Topics](#7-kafka-topic-mapping)
- [🗄️ Database Flow](#8-database-write-flow)

---

## 1. Overview

This document describes how data flows through the CyberMesh system, from network telemetry ingestion to threat detection, consensus validation, and policy enforcement.

> [!IMPORTANT]
> CyberMesh uses an **asynchronous, event-driven architecture** with Kafka as the central message bus connecting all services.

---

## 2. End-to-End Data Flow

### 2.1 Complete Pipeline

```mermaid
sequenceDiagram
    autonumber
    participant Net as Network
    participant AI as AI Service
    participant Kafka as Kafka
    participant Val as Validators
    participant DB as CockroachDB
    participant Agent as Enforcement Agent
    
    Net->>AI: Telemetry data
    AI->>AI: Feature extraction (79 features)
    AI->>AI: 3-Engine detection
    AI->>AI: Ensemble voting
    AI->>AI: Ed25519 sign
    AI->>Kafka: ai.anomalies.v1
    
    loop Each Validator
        Kafka->>Val: Consume anomaly
        Val->>Val: Verify signature
        Val->>Val: Add to mempool
    end
    
    Val->>Val: HotStuff Consensus (2-chain)
    Val->>Val: State machine execute
    Val->>DB: Persist block
    Val->>Kafka: control.commits.v1
    Val->>Kafka: control.policy.v1
    
    Kafka->>AI: Commit notification
    AI->>AI: Update feedback tracker
    AI->>AI: Calibrate thresholds
    
    Kafka->>Agent: Policy message
    Agent->>Agent: Parse policy
    Agent->>Agent: Apply iptables/nftables
    Agent->>Kafka: control.policy.ack.v1
```

---

## 3. Detection Flow (AI Service)

### 3.1 Detection Pipeline

```mermaid
flowchart TB
    subgraph Input
        T[Telemetry Source<br/>File or Postgres]
    end
    
    subgraph Feature["Feature Extraction"]
        FA[Feature Adapter<br/>79 features]
    end
    
    subgraph Engines["Detection Engines - Parallel"]
        R[Rules Engine<br/>Thresholds]
        M[Math Engine<br/>Statistics]
        ML[ML Engine<br/>LightGBM]
    end
    
    subgraph Voting
        E[Ensemble Voter<br/>ML=0.5, R=0.3, M=0.2]
        AB{Confidence > 0.70?}
    end
    
    subgraph Output
        EV[Evidence Generator]
        SG[Ed25519 Signer]
        RL[Rate Limiter<br/>100 per sec]
        K[Kafka Producer]
    end
    
    T --> FA --> R & M & ML
    R & M & ML --> E --> AB
    AB -->|Yes| EV --> SG --> RL --> K
    AB -->|No| X[Abstain]
    
    style R fill:#ffebee,stroke:#c62828,color:#000;
    style M fill:#fff3e0,stroke:#f57f17,color:#000;
    style ML fill:#e3f2fd,stroke:#1565c0,color:#000;
    style K fill:#c8e6c9,stroke:#2e7d32,color:#000;
    style X fill:#f5f5f5,stroke:#9e9e9e,color:#000;
```

### 3.2 Detection Loop Timing

```mermaid
gantt
    title Detection Loop (5 second cycle)
    dateFormat X
    axisFormat %L ms
    
    section Phase 1
    Poll Telemetry     :0, 100
    Feature Extraction :100, 200
    
    section Phase 2
    Rules Engine       :200, 50
    Math Engine        :200, 80
    ML Engine          :200, 150
    
    section Phase 3
    Ensemble Voting    :350, 50
    Evidence Gen       :400, 50
    Signing            :450, 20
    Publish            :470, 30
    
    section Idle
    Sleep              :500, 4500
```

> [!NOTE]
> The detection loop runs every **5 seconds**. Total processing time is ~500ms, leaving 4.5s for idle/sleep.

---

## 4. Consensus Flow (Backend)

### 4.1 HotStuff (2-Chain) Commit Flow

```mermaid
sequenceDiagram
    autonumber
    participant L as Leader (validator-0)
    participant V1 as validator-1
    participant V2 as validator-2
    participant V3 as validator-3
    participant V4 as validator-4
    
    Note over L,V4: Proposal
    L->>V1: Proposal(block, justifyQC)
    L->>V2: Proposal(block, justifyQC)
    L->>V3: Proposal(block, justifyQC)
    L->>V4: Proposal(block, justifyQC)
    
    Note over L,V4: Vote (replicas vote if safe)
    V1->>L: Vote(block_hash, view)
    V2->>L: Vote(block_hash, view)
    V3->>L: Vote(block_hash, view)
    
    Note over L,V4: Quorum Certificate (QC) formed (2f+1 signatures)
    L->>L: Form QC(block_hash, view)
    
    Note over L,V4: 2-chain commit rule
    L->>L: Commit prior block once QC formed for child
    
    Note over L,V4: Execute and persist
    L->>L: Execute state machine
    L->>DB: Persist committed block and txs
```

### 4.2 Leader Election & View Change

```mermaid
stateDiagram-v2
    [*] --> Normal: Genesis complete
    
    Normal --> LeaderTimeout: Timeout or liveness failure
    LeaderTimeout --> NewView: Pacemaker advances view
    NewView --> Normal: Leader rotation for new view
    
    Normal --> Normal: Heartbeat every 500ms
```

---

## 5. Feedback Loop Flow

### 5.1 Anomaly Lifecycle (7-State Machine)

```mermaid
stateDiagram-v2
    [*] --> DETECTED: AI detects anomaly
    
    DETECTED --> PUBLISHED: Sign and send to Kafka
    
    PUBLISHED --> ADMITTED: Validators accept
    PUBLISHED --> REJECTED: Validators reject
    PUBLISHED --> TIMEOUT: No response (5min)
    PUBLISHED --> EXPIRED: TTL exceeded (30d)
    
    ADMITTED --> COMMITTED: Consensus reached (2f+1)
    ADMITTED --> REJECTED: Consensus failed
    ADMITTED --> EXPIRED: TTL exceeded
    
    COMMITTED --> [*]: Terminal (success)
    REJECTED --> [*]: Terminal (false positive)
    TIMEOUT --> [*]: Terminal
    EXPIRED --> [*]: Terminal
    
    note right of COMMITTED
        Success - High confidence
    end note
    
    note right of REJECTED
        False positive - Lower threshold
    end note
```

### 5.2 Threshold Adjustment

```mermaid
flowchart TB
    subgraph Metrics
        ACC[Acceptance Rate<br/>committed ÷ published]
    end
    
    subgraph Decision
        C1{Rate < 70%?}
        C2{Rate > 85%?}
        C3[Stable<br/>70-85%]
    end
    
    subgraph Action
        INC[Increase Threshold<br/>+2-5%]
        DEC[Decrease Threshold<br/>-2-5%]
        KEEP[No Change]
    end
    
    ACC --> C1
    C1 -->|Yes| INC
    C1 -->|No| C2
    C2 -->|Yes| DEC
    C2 -->|No| KEEP
    
    style INC fill:#ffebee,stroke:#c62828,color:#000;
    style DEC fill:#c8e6c9,stroke:#2e7d32,color:#000;
    style KEEP fill:#e3f2fd,stroke:#1565c0,color:#000;
```

> [!TIP]
> The AI service automatically adjusts thresholds based on validator acceptance rates to minimize false positives while maintaining high detection rates.

---

## 6. Enforcement Flow

### 6.1 Policy Enforcement Pipeline

```mermaid
sequenceDiagram
    autonumber
    participant Kafka as Kafka
    participant Agent as Enforcement Agent
    participant Sched as Scheduler
    participant Enf as Enforcer
    participant Net as Network Stack
    
    Kafka->>Agent: control.policy.v1
    Agent->>Agent: Verify signature
    Agent->>Agent: Parse policy spec
    Agent->>Sched: Schedule enforcement
    
    Sched->>Enf: Apply policy
    
    alt iptables
        Enf->>Net: iptables -A INPUT -s x.x.x.x -j DROP
    else nftables
        Enf->>Net: nft add rule ip filter input drop
    else kubernetes
        Enf->>Net: Apply NetworkPolicy
    end
    
    Enf->>Agent: Result
    Agent->>Kafka: control.policy.ack.v1
```

### 6.2 Enforcement Backends

```mermaid
flowchart TB
    subgraph Agent["Enforcement Agent"]
        C[Controller]
        R[Reconciler]
    end
    
    subgraph Backends
        IPT[iptables<br/>Linux legacy]
        NFT[nftables<br/>Linux modern]
        K8S[Kubernetes<br/>NetworkPolicy]
    end
    
    subgraph Kernel
        NF[Netfilter]
        CNI[CNI Plugin]
    end
    
    C --> R
    R --> IPT --> NF
    R --> NFT --> NF
    R --> K8S --> CNI
    
    classDef agent fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef backend fill:#fff9c4,stroke:#f57f17,color:#000;
    classDef kernel fill:#c8e6c9,stroke:#2e7d32,color:#000;
    
    class C,R agent;
    class IPT,NFT,K8S backend;
    class NF,CNI kernel;
```

---

## 7. Kafka Topic Mapping

### 7.1 Message Flow Diagram

```mermaid
flowchart LR
    subgraph Producers
        AI[AI Service]
        BE[Backend]
        EA[Enforcement Agent]
    end
    
    subgraph Topics["Kafka Topics"]
        direction TB
        T1[ai.anomalies.v1]
        T2[ai.evidence.v1]  
        T3[ai.policy.v1]
        T4[control.commits.v1]
        T5[control.policy.v1]
        T6[control.reputation.v1]
        T8[control.policy.ack.v1]
        T7[ai.dlq.v1]
    end
    
    subgraph Consumers
        AI2[AI Service]
        BE2[Backend]
        EA2[Enforcement Agent]
    end
    
    AI -->|Anomalies| T1
    AI -->|Evidence| T2
    AI -->|Policy reqs| T3
    
    BE2 -->|Consume| T1
    BE2 -->|Consume| T2
    BE2 -->|Consume| T3
    
    BE -->|Commits| T4
    BE -->|Policies| T5
    BE -->|Reputation| T6
    
    AI2 -->|Consume| T4
    AI2 -->|Consume| T6
    AI2 -->|Consume| T8
    
    EA2 -->|Consume| T5
    EA -->|Policy ACKs| T8
    
    BE2 -.->|Invalid msgs| T7
    
    classDef producer fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef topic fill:#fff9c4,stroke:#f57f17,color:#000;
    classDef consumer fill:#c8e6c9,stroke:#2e7d32,color:#000;
    
    class AI,BE,EA producer;
    class T1,T2,T3,T4,T5,T6,T7,T8 topic;
    class AI2,BE2,EA2 consumer;
```

### 7.2 Message Schemas

| Topic | Producer | Consumer | Schema | Size |
|-------|----------|----------|--------|------|
| `ai.anomalies.v1` | AI | Backend | Protobuf (AnomalyEvent) | ~2KB |
| `ai.evidence.v1` | AI | Backend | Protobuf (EvidenceEvent) | ~512KB max |
| `ai.policy.v1` | AI | Backend | Protobuf (PolicyEvent) | ~1KB |
| `control.commits.v1` | Backend | AI | Protobuf (CommitEvent) | ~1KB |
| `control.policy.v1` | Backend | Agent | Protobuf (PolicyUpdateEvent) | ~2KB |
| `control.reputation.v1` | Backend | AI | Protobuf (ReputationEvent) | ~500B |
| `control.policy.ack.v1` | Agent | AI | Protobuf (PolicyAckEvent) | ~1KB |
| `ai.dlq.v1` | Backend | - | Original + error | Varies |

> [!WARNING]
> All Kafka messages **MUST** be signed with Ed25519 using domain separation. Invalid signatures are rejected to the DLQ.

---

## 8. Database Write Flow

### 8.1 Persistence Pipeline

```mermaid
sequenceDiagram
    autonumber
    participant SM as State Machine
    participant PW as Persistence Worker
    participant Q as Queue (1024)
    participant DB as CockroachDB
    
    SM->>Q: Enqueue block
    
    loop Async
        PW->>Q: Dequeue
        PW->>DB: BEGIN TRANSACTION
        PW->>DB: INSERT INTO blocks
        PW->>DB: INSERT INTO transactions
        PW->>DB: INSERT INTO anomalies
        PW->>DB: UPDATE validators
        PW->>DB: COMMIT
    end
    
    Note over PW,DB: Retry on failure (max 3)
```

### 8.2 Tables Written

```mermaid
erDiagram
    blocks ||--o{ transactions : contains
    transactions ||--o{ anomalies : includes
    validators ||--o{ blocks : proposes
    
    blocks {
        bigint height PK
        bytes32 block_hash
        bytes32 state_root
        bytes32 previous_hash
        timestamp created_at
    }
    
    transactions {
        uuid id PK
        bigint block_height FK
        string type
        bytes payload
        bytes signature
    }
    
    anomalies {
        uuid id PK
        uuid transaction_id FK
        string type
        float confidence
        string lifecycle_state
    }
    
    validators {
        string id PK
        bytes32 public_key
        float reputation
        boolean active
    }
```

---

## 9. API Data Flow

### 9.1 Dashboard Request Flow

```mermaid
sequenceDiagram
    autonumber
    participant User as User Browser
    participant CF as Cloudflare
    participant FE as Frontend
    participant BE as Backend API
    participant Cache as In-Memory Cache
    participant DB as CockroachDB
    
    User->>CF: GET /dashboard
    CF->>FE: Proxy
    FE->>User: React App (SPA)
    
    User->>CF: GET /api/dashboard/overview
    CF->>BE: Proxy (sticky session)
    
    BE->>Cache: Check cache
    
    alt Cache hit
        Cache->>BE: Cached data
    else Cache miss
        BE->>DB: Query metrics
        DB->>BE: Results
        BE->>Cache: Store (1 min TTL)
    end
    
    BE->>User: JSON response
```

> [!NOTE]
> **Sticky sessions** ensure the same backend pod handles subsequent requests, making the in-memory cache effective.

---

## 10. Related Documents

### Design Documents
- [High-Level Design](./HLD.md)
- [Backend LLD](./LLD-backend.md)
- [AI Service LLD](./LLD-ai-service.md)
- [Enforcement Agent LLD](./LLD-enforcement-agent.md)

### Architecture Documents
- [Kafka Message Bus](../architecture/04_kafka_message_bus.md)
- [Feedback Loop](../architecture/06_feedback_loop.md)
- [HotStuff Consensus](../architecture/03_hotstuff_consensus.md)

---

**[⬆️ Back to Top](#-navigation)**
