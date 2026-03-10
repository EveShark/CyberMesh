# CyberMesh Data Flow Documentation

**Version:** 1
**Last Updated:** 2026-02-25

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
    participant TL as Telemetry Layer
    participant Kafka as Kafka
    participant S as Sentinel
    participant AI as AI Service
    participant Val as Validators
    participant DB as CockroachDB
    participant Agent as Enforcement Agent
    participant Outbox as Policy Outbox Dispatcher
    
    Net->>TL: Telemetry data (flows/alerts/pcap)
    TL->>Kafka: telemetry.flow.v1 / telemetry.deepflow.v1
    TL->>Kafka: telemetry.flow.agg.v1 / telemetry.features.v1
    TL->>Kafka: pcap.result.v1 (on request)
    Kafka->>S: telemetry.flow.v1 / telemetry.deepflow.v1
    S->>S: Decode + validate + orchestrate agents
    S->>Kafka: sentinel.verdicts.v1
    Kafka->>AI: sentinel.verdicts.v1
    AI->>AI: Sentinel adapter + policy/anomaly mapping
    AI->>AI: Evidence enrichment + Ed25519 sign
    AI->>Kafka: ai.anomalies.v1
    
    loop Each Validator
        Kafka->>Val: Consume anomaly
        Val->>Val: Verify signature
        Val->>Val: Add to mempool
    end
    
    Val->>Val: HotStuff Consensus (2-chain)
    Val->>Val: State machine execute
    Val->>DB: Persist block + outbox rows (single transaction)
    Val->>Kafka: control.commits.v1
    Val->>Outbox: claim pending policy rows (lease/fencing)
    Outbox->>Kafka: control.policy.v2
    
    Kafka->>AI: Commit notification
    AI->>AI: Update feedback tracker
    AI->>AI: Calibrate thresholds
    
    Kafka->>Agent: Policy message
    Agent->>Agent: Parse policy
    Agent->>Agent: Apply cilium/gateway/iptables/nftables/k8s
    Agent->>Kafka: control.enforcement_ack.v1 (default)
    Kafka->>Val: ACK consume + correlate to outbox row
```

---

## 3. Detection Flow (AI Service)

### 3.1 Detection Pipeline

```mermaid
flowchart TB
    subgraph Input
        T[Sentinel Source<br/>Kafka sentinel.verdicts.v1]
    end
    
    subgraph Feature["Sentinel Adapter"]
        FA[Schema + Signature Validation<br/>sentinel.result.v1]
    end
    
    subgraph Engines["Detection Engines - Parallel"]
        R[Policy Mapper<br/>Threat -> action]
        M[Anomaly Mapper<br/>Score -> severity]
        ML[Optional ML Enrichment<br/>existing models]
    end
    
    subgraph Voting
        E[Decision Merger<br/>deterministic]
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
    Poll Sentinel      :0, 80
    Adapter Validate   :80, 120

    section Phase 2
    Policy Mapper      :200, 60
    Anomaly Mapper     :200, 60
    ML Enrichment      :200, 120
    
    section Phase 3
    Ensemble Voting    :350, 50
    Evidence Gen       :400, 50
    Signing            :450, 20
    Publish            :470, 30
    
    section Idle
    Sleep              :500, 4500
```

> [!NOTE]
> In integrated runtime, AI consumes `sentinel.verdicts.v1`. `telemetry.features.v1` remains available for direct model paths and offline analysis.

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

### 4.3 Commit -> Publish Outbox Path

```mermaid
sequenceDiagram
    autonumber
    participant C as Consensus Commit
    participant DB as CockroachDB
    participant O as control_policy_outbox
    participant D as Dispatcher (lease holder)
    participant K as Kafka control.policy.v2
    participant A as ACK Consumer

    C->>DB: BEGIN
    C->>DB: write blocks + txs
    C->>O: upsert outbox rows (pending)
    C->>DB: COMMIT

    D->>DB: acquire/renew lease epoch
    D->>O: claim pending/retry rows
    D->>K: publish policy
    D->>O: mark published(partition,offset)

    A->>DB: upsert policy_acks
    A->>O: mark correlated row acked
```

Properties of this path:

- single logical writer (lease + fencing epoch)
- durable idempotency at outbox identity
- retry/backoff with terminal failure state
- ACK closure tracked from `policy_acks` back to outbox rows

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
    
    Kafka->>Agent: control.policy.v2
    Agent->>Agent: Verify signature
    Agent->>Agent: Parse policy spec
    Agent->>Sched: Schedule enforcement
    
    Sched->>Enf: Apply policy
    
    alt cilium
        Enf->>Net: Apply CiliumNetworkPolicy/CiliumClusterwideNetworkPolicy
    else gateway
        Enf->>Net: Apply Gateway egress policy translation
    else iptables
        Enf->>Net: iptables -A INPUT -s x.x.x.x -j DROP
    else nftables
        Enf->>Net: nft add rule ip filter input drop
    else kubernetes
        Enf->>Net: Apply NetworkPolicy
    end
    
    Enf->>Agent: Result
    Agent->>Kafka: control.enforcement_ack.v1
```

### 6.2 Enforcement Backends

```mermaid
flowchart TB
    subgraph Agent["Enforcement Agent"]
        C[Controller]
        R[Reconciler]
    end
    
    subgraph Backends
        CIL[Cilium<br/>CiliumPolicy CRDs]
        GW[Gateway<br/>Policy translation]
        IPT[iptables<br/>Linux legacy]
        NFT[nftables<br/>Linux modern]
        K8S[Kubernetes<br/>NetworkPolicy]
    end
    
    subgraph Kernel
        NF[Netfilter]
        CNI[CNI Plugin]
    end
    
    C --> R
    R --> CIL --> CNI
    R --> GW --> CNI
    R --> IPT --> NF
    R --> NFT --> NF
    R --> K8S --> CNI
    
    classDef agent fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef backend fill:#fff9c4,stroke:#f57f17,color:#000;
    classDef kernel fill:#c8e6c9,stroke:#2e7d32,color:#000;
    
    class C,R agent;
    class CIL,GW,IPT,NFT,K8S backend;
    class NF,CNI kernel;
```

### 6.3 Control Plane vs Data Plane (L3/L4)

```mermaid
flowchart LR
    AI[AI Service] -->|ai.policy.v1| BE[Backend Consensus]
    BE -->|control.policy.v2| AG[Enforcement Agent]
    AG -->|program rules| DP[Kernel/CNI/Gateway Data Plane]
    AG -->|control.enforcement_ack.v1| FB[Backend ACK Correlation]
```

Plane responsibilities:

- Control plane:
  - `AI -> Backend` generates and validates policy intent
  - backend consensus authorizes policy publication to `control.policy.v2`
- Data plane:
  - enforcement backend programs runtime network controls
  - applies L3/L4 controls and reports execution ACK

L3/L4 mapping in runtime:

| Path | L3 (IP/CIDR/identity) | L4 (port/protocol/state) | Typical scope |
|---|---|---|---|
| Cilium/Kubernetes | yes | yes | East-West |
| Gateway | yes | yes | North-South |
| iptables/nftables | yes | yes | Host fallback and guardrail |

---

## 7. Kafka Topic Mapping

### 7.1 Message Flow Diagram

```mermaid
flowchart LR
    subgraph Producers
        TL[Telemetry Layer]
        S[Sentinel]
        AI[AI Service]
        BE[Backend]
        EA[Enforcement Agent]
    end
    
    subgraph Topics["Kafka Topics"]
        direction TB
        F1[telemetry.flow.v1]
        F2[sentinel.verdicts.v1]
        F3[telemetry.flow.agg.v1]
        F4[telemetry.features.v1]
        F5[telemetry.deepflow.v1]
        F6[pcap.request.v1]
        F7[pcap.result.v1]
        T1[ai.anomalies.v1]
        T2[ai.evidence.v1]  
        T3[ai.policy.v1]
        T4[control.commits.v1]
        T5[control.policy.v2]
        T6[control.reputation.v1]
        T8[control.enforcement_ack.v1]
        T7[ai.dlq.v1]
    end
    
    subgraph Consumers
        AI2[AI Service]
        BE2[Backend]
        EA2[Enforcement Agent]
        TL2[Telemetry Layer]
    end
    
    TL -->|Flows/Alerts| F1
    TL -->|Aggregates| F3
    TL -->|Features| F4
    TL -->|Deepflow| F5
    S -->|Verdicts| F2
    TL -->|PCAP Result| F7
    TL2 -->|PCAP Request| F6
    
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
    S -->|Consume| F1
    AI2 -->|Consume| F2
    
    EA2 -->|Consume| T5
    EA -->|Policy ACKs| T8
    
    BE2 -.->|Invalid msgs| T7
    
    classDef producer fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef topic fill:#fff9c4,stroke:#f57f17,color:#000;
    classDef consumer fill:#c8e6c9,stroke:#2e7d32,color:#000;
    
    class TL,S,AI,BE,EA producer;
    class F1,F2,F3,F4,F5,F6,F7,T1,T2,T3,T4,T5,T6,T7,T8 topic;
    class AI2,BE2,EA2,TL2 consumer;
```

### 7.2 Message Schemas

| Topic | Producer | Consumer | Schema | Size |
|-------|----------|----------|--------|------|
| `telemetry.flow.v1` | Telemetry | Telemetry | Protobuf/JSON (FlowV1) | varies |
| `sentinel.verdicts.v1` | Sentinel | AI | Protobuf (`SentinelResultEvent`) | ~1-16KB |
| `telemetry.flow.agg.v1` | Telemetry | Telemetry | Protobuf/JSON (FlowAgg) | varies |
| `telemetry.features.v1` | Telemetry | AI/Ops | Protobuf/JSON (CIC v1) | ~3-8KB |
| `telemetry.deepflow.v1` | Telemetry | Telemetry | Protobuf/JSON (DeepFlowV1) | varies |
| `pcap.request.v1` | Backend/AI/Ops | Telemetry | Protobuf (PcapRequestV1) | ~1KB |
| `pcap.result.v1` | Telemetry | Backend/AI/Ops | Protobuf (PcapResultV1) | ~1KB |
| `ai.anomalies.v1` | AI | Backend | Protobuf (AnomalyEvent) | ~2KB |
| `ai.evidence.v1` | AI | Backend | Protobuf (EvidenceEvent) | ~512KB max |
| `ai.policy.v1` | AI | Backend | Protobuf (PolicyEvent) | ~1KB |
| `control.commits.v1` | Backend | AI | Protobuf (CommitEvent) | ~1KB |
| `control.policy.v2` | Backend | Agent | Protobuf (PolicyUpdateEvent) | ~2KB |
| `control.reputation.v1` | Backend | AI | Protobuf (ReputationEvent) | ~500B |
| `control.enforcement_ack.v1` | Agent | Backend (AI optional) | Protobuf (PolicyAckEvent) | ~1KB |
| `ai.dlq.v1` | Backend | - | Original + error | Varies |

> [!WARNING]
> All Kafka messages **MUST** be signed with Ed25519 using domain separation. Invalid signatures are rejected to the DLQ.

> [!NOTE]
> The policy-ack topic name is configuration-driven:
> `TOPIC_CONTROL_POLICY_ACK` (AI), `CONTROL_POLICY_ACK_TOPIC` (backend), `ACK_TOPIC` (agent).
> Current default is `control.enforcement_ack.v1`.

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
    participant O as control_policy_outbox
    
    SM->>Q: Enqueue block
    
    loop Async
        PW->>Q: Dequeue
        PW->>DB: BEGIN TRANSACTION
        PW->>DB: INSERT INTO blocks
        PW->>DB: INSERT INTO transactions
        PW->>O: UPSERT outbox rows (policy tx only)
        PW->>DB: UPDATE validators
        PW->>DB: COMMIT
    end
    
    Note over PW,DB: Retry on failure (max 3)
```

### 8.2 Tables Written

```mermaid
erDiagram
    blocks ||--o{ transactions : contains
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

    control_policy_outbox {
        uuid id PK
        bigint block_height
        string policy_id
        bytes rule_hash
        string status
        int retries
        timestamp created_at
        timestamp published_at
        timestamp acked_at
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
    
    User->>CF: GET /api/v1/dashboard/overview
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
