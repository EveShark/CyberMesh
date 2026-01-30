# CyberMesh Backend - Low-Level Design (LLD)

**Version:** 2.0.0  
**Last Updated:** 2026-01-30

---

## 📑 Navigation

**Quick Links:**
- [🏗️ Module Architecture](#2-module-architecture)
- [⚙️ Consensus Engine](#3-consensus-module-pkgconsensus)
- [🔄 State Machine](#4-state-machine-pkgstate)
- [📨 Kafka Integration](#5-kafka-integration-pkgingestkafka)
- [🔌 API Layer](#9-api-layer-pkgapi)

---

## 1. Overview

The Backend is a **Byzantine Fault Tolerant (BFT) consensus engine** written in Go that validates AI-generated security alerts. This document covers the internal architecture, key modules, class diagrams, and algorithms.

> [!IMPORTANT]
> The backend uses **HotStuff 2-chain consensus** with 5 validators, tolerating 1 Byzantine failure (2f+1 = 3 quorum).

---

## 2. Module Architecture

### 2.1 Package Structure

```mermaid
graph TB
    subgraph cmd["cmd/"]
        main[main.go<br/>Entry point]
    end
    
    subgraph pkg["pkg/"]
        consensus[consensus/]
        state[state/]
        ingest[ingest/kafka/]
        mempool[mempool/]
        storage[storage/cockroach/]
        p2p[p2p/]
        api[api/]
        utils[utils/]
        wiring[wiring/]
    end
    
    main --> wiring
    wiring --> consensus
    wiring --> ingest
    wiring --> mempool
    wiring --> storage
    wiring --> p2p
    wiring --> api
    
    consensus --> state
    consensus --> mempool
    ingest --> mempool
    state --> storage
    
    classDef entry fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef pkg fill:#fff9c4,stroke:#f57f17,color:#000;
    
    class main entry;
    class consensus,state,ingest,mempool,storage,p2p,api,utils,wiring pkg;
```

---

## 3. Consensus Module (`pkg/consensus/`)

### 3.1 Component Diagram

```mermaid
classDiagram
    class ConsensusEngine {
        +Start()
        +Stop()
        +ProposeBlock()
        +ReceiveMessage()
    }
    
    class HotStuff {
        -currentView uint64
        -currentHeight uint64
        -lockedQC QC
        +OnProposal()
        +OnVote()
        +CreateProposal()
        +AdvanceView()
    }
    
    class Pacemaker {
        -currentView uint64
        +OnQC()
        +AdvanceView()
    }
    
    class GenesisCoordinator {
        -readyAttestations map
        +StartGenesis()
        +OnReadyAttestation()
        +GenerateCertificate()
    }
    
    class QuorumVerifier {
        +HasQuorum() bool
        +VerifyQC() bool
    }
    
    ConsensusEngine --> HotStuff
    ConsensusEngine --> Pacemaker
    ConsensusEngine --> GenesisCoordinator
    HotStuff --> QuorumVerifier
```

### 3.2 HotStuff (2-Chain) State Machine

```mermaid
stateDiagram-v2
    [*] --> Idle: Initialize
    
    Idle --> Proposed: Leader proposes block
    Proposed --> Voted: Replicas vote if safe
    Voted --> QCFormed: 2f+1 votes form a QC
    QCFormed --> Committed: 2-chain commit rule satisfied
    Committed --> Idle: Execute and persist committed block
    
    note right of QCFormed
        Quorum: 3 of 5 validators
    end note
```

### 3.3 Key Algorithms

#### Leader Selection (Rotation / View-Based)

```go
function GetLeader(view, validators):
    active = validators.filter(v => v.reputation >= MIN_REPUTATION)
    sorted = active.sortBy(v => v.id)
    index = view % len(sorted)
    return sorted[index]
```

#### Quorum Calculation (BFT)

```
N = total validators
f = (N - 1) / 3                // Byzantine failures tolerated
Quorum = 2f + 1                // Need 2f+1 votes (e.g., for QC formation)

Example (N=5): f=1, Quorum=3
```

---

## 4. State Machine (`pkg/state/`)

### 4.1 Executor Flow

```mermaid
flowchart TB
    subgraph Input
        B[Block with Transactions]
    end
    
    subgraph Execution
        V[Validate Transaction]
        N[Check Nonce]
        R[Call Reducer]
        S[Update State]
        M[Update Merkle Tree]
    end
    
    subgraph Output
        VER[New Version]
        ROOT[State Root]
        REC[Receipts]
    end
    
    B --> V
    V -->|Valid| N
    V -->|Invalid| SKIP[Skip Transaction]
    N -->|Unique| R
    N -->|Duplicate| SKIP
    R --> S --> M
    M --> VER & ROOT & REC
    
    style V fill:#e3f2fd,stroke:#1565c0,color:#000;
    style SKIP fill:#ffebee,stroke:#c62828,color:#000;
    style ROOT fill:#c8e6c9,stroke:#2e7d32,color:#000;
```

> [!WARNING]
> **Fail-Closed Design**: Any invalid transaction invalidates the entire block. This prevents state divergence across validators.

### 4.2 Reducer Functions

```mermaid
classDiagram
    class Executor {
        +ApplyBlock(block, timestamp) Result
        -applyTransaction(tx, state) error
    }
    
    class Reducers {
        +ApplyAnomalyEvent(state, event) error
        +ApplyEvidenceEvent(state, event) error
        +ApplyPolicyEvent(state, event) error
    }
    
    class MemStore {
        -versions map~uint64~State
        +Get(key) Value
        +Set(key, value)
        +Delete(key)
        +Commit() uint64
    }
    
    class MerkleTree {
        +Insert(key, hash)
        +GetRoot() bytes32
        +GenerateProof(key) Proof
    }
    
    Executor --> Reducers
    Executor --> MemStore
    MemStore --> MerkleTree
```

### 4.3 State Model

```mermaid
erDiagram
    AnomalyState {
        string id PK
        string type
        float confidence
        string source_ip
        string dest_ip
        int timestamp
        string status
    }
    
    PolicyState {
        string id PK
        string type
        string action
        string target
        int ttl
        boolean active
    }
    
    ValidatorState {
        string id PK
        float reputation
        int blocks_proposed
        int blocks_committed
        boolean active
    }
```

---

## 5. Kafka Integration (`pkg/ingest/kafka/`)

### 5.1 Consumer Architecture

```mermaid
classDiagram
    class KafkaConsumer {
        -client sarama.ConsumerGroup
        -handlers map~string~Handler
        +Start()
        +Stop()
        +ConsumeClaim()
    }
    
    class MessageVerifier {
        -cryptoService CryptoService
        +VerifySignature(msg) error
        +VerifyContentHash(msg) error
        +VerifyTimestamp(msg) error
        +VerifyNonce(msg) error
    }
    
    class SchemaParser {
        +ParseAnomalyEvent(bytes) AnomalyEvent
        +ParseEvidenceEvent(bytes) EvidenceEvent
        +ParsePolicyEvent(bytes) PolicyEvent
    }
    
    class DLQProducer {
        +SendToDLQ(msg, error)
    }
    
    KafkaConsumer --> MessageVerifier
    KafkaConsumer --> SchemaParser
    MessageVerifier --> DLQProducer
```

### 5.2 Message Verification Flow

```mermaid
flowchart TB
    M[Kafka Message] --> P[Parse Protobuf]
    P --> S[Verify Signature]
    S -->|Invalid| DLQ[Dead Letter Queue]
    S -->|Valid| H[Verify Content Hash]
    H -->|Invalid| DLQ
    H -->|Valid| T[Verify Timestamp]
    T -->|Skewed| DLQ
    T -->|Valid| N[Verify Nonce]
    N -->|Duplicate| DLQ
    N -->|Unique| MP[Add to Mempool]
    
    style S fill:#e3f2fd,stroke:#1565c0,color:#000;
    style MP fill:#c8e6c9,stroke:#2e7d32,color:#000;
    style DLQ fill:#ffebee,stroke:#c62828,color:#000;
```

> [!NOTE]
> All Kafka messages use **Ed25519 signatures** with domain separation. Invalid messages are rejected to the DLQ topic.

---

## 6. Mempool (`pkg/mempool/`)

### 6.1 Class Diagram

```mermaid
classDiagram
    class Mempool {
        -transactions map~string~Transaction
        -nonceTracker NonceTracker
        -rateLimiter RateLimiter
        +Add(tx) error
        +Remove(txID)
        +GetBatch(maxTxs) []Transaction
        +Size() int
    }
    
    class NonceTracker {
        -seen map~string~time.Time
        -ttl time.Duration
        +HasSeen(nonce) bool
        +MarkSeen(nonce)
        +Cleanup()
    }
    
    class RateLimiter {
        -tokens int
        -rate int
        +Allow() bool
        +Refill()
    }
    
    Mempool --> NonceTracker
    Mempool --> RateLimiter
```

### 6.2 Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| `MEMPOOL_MAX_TXS` | 1000 | Max transactions |
| `MEMPOOL_MAX_BYTES` | 10MB | Max total size |
| `MEMPOOL_NONCE_TTL` | 15m | Nonce expiration |
| `MEMPOOL_RATE_PER_SECOND` | 1000 | Rate limit |

---

## 7. Storage (`pkg/storage/cockroach/`)

### 7.1 Adapter Pattern

```mermaid
classDiagram
    class StorageAdapter {
        <<interface>>
        +SaveBlock(block) error
        +GetBlock(height) Block
        +SaveTransaction(tx) error
        +GetTransaction(id) Transaction
    }
    
    class CockroachAdapter {
        -db *sql.DB
        -pool *pgxpool.Pool
        +SaveBlock(block) error
        +GetBlock(height) Block
    }
    
    class ConnectionPool {
        -maxConns int
        -idleConns int
        +Get() *sql.Conn
        +Release(conn)
    }
    
    StorageAdapter <|.. CockroachAdapter
    CockroachAdapter --> ConnectionPool
```

### 7.2 Write Path

```mermaid
sequenceDiagram
    participant PW as PersistenceWorker
    participant Q as Queue
    participant DB as CockroachDB
    
    loop Every Block
        PW->>Q: Dequeue block
        PW->>DB: BEGIN
        PW->>DB: INSERT blocks
        PW->>DB: INSERT transactions (batch)
        PW->>DB: UPDATE validators
        PW->>DB: COMMIT
        
        alt Failure
            PW->>PW: Retry (max 3)
            PW->>PW: Exponential backoff
        end
    end
```

---

## 8. P2P Networking (`pkg/p2p/`)

### 8.1 LibP2P Integration

```mermaid
graph TB
    subgraph P2P["libp2p Stack"]
        H[Host]
        PS[PubSub<br/>GossipSub]
        DHT[DHT<br/>Kademlia]
        MDNS[mDNS<br/>Local Discovery]
    end
    
    subgraph Topics
        T1[consensus]
        T2[blocks]
        T3[txs]
    end
    
    H --> PS
    H --> DHT
    H --> MDNS
    PS --> T1 & T2 & T3
    
    classDef stack fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef topic fill:#fff9c4,stroke:#f57f17,color:#000;
    
    class H,PS,DHT,MDNS stack;
    class T1,T2,T3 topic;
```

### 8.2 Message Types

| Topic | Message | Purpose |
|-------|---------|---------|
| `consensus/proposal` | Proposal | HotStuff proposal broadcast |
| `consensus/vote` | Vote | HotStuff vote broadcast |
| `consensus/viewchange` | ViewChange | View change signaling |
| `consensus/newview` | NewView | New view announcement |
| `consensus/heartbeat` | Heartbeat | Liveness/activation heartbeats |
| `blocks` | BlockAnnouncement | Block propagation |
| `txs` | Transaction | Transaction gossip |

---

## 9. API Layer (`pkg/api/`)

### 9.1 Endpoints

All endpoints are served under `/api/v1`:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check (public) ✅ |
| `/ready` | GET | Readiness probe (public) ✅ |
| `/metrics` | GET | Prometheus metrics 📊 |
| `/blocks/latest` | GET | Latest block |
| `/blocks` | GET | List blocks |
| `/blocks/{height}` | GET | Get block by height |
| `/state/root` | GET | State root |
| `/state/{key}` | GET | State lookup |
| `/validators` | GET | List validators |
| `/validators/{id}` | GET | Validator details |
| `/dashboard/overview` | GET | Dashboard overview |
| `/anomalies` | GET | List anomalies |
| `/ai/metrics` | GET | AI metrics (proxy) |

### 9.2 Middleware & Auth

The router applies middleware in this order:

1. Panic recovery
2. Request logging
3. Request ID
4. Concurrency limiting (optional)
5. IP allowlist (optional)
6. Rate limiting (optional)
7. Global authentication (mTLS/Bearer tokens)
8. CORS headers (optional)
9. Security headers

### 9.3 Response Flow

```mermaid
flowchart LR
    R[Request] --> MW[Middleware<br/>Auth, Logging, RateLimit]
    MW --> H[Handler]
    H --> C[Cache Check]
    C -->|Hit| RES[Response]
    C -->|Miss| DB[(Database)]
    DB --> RES
    
    style MW fill:#e3f2fd,stroke:#1565c0,color:#000;
    style C fill:#fff9c4,stroke:#f57f17,color:#000;
    style RES fill:#c8e6c9,stroke:#2e7d32,color:#000;
```

---

## 10. Key Files Reference

| File | Purpose | Lines |
|------|---------|-------|
| `cmd/cybermesh/main.go` | Entry point, signal handling | ~1100 |
| `pkg/consensus/api/engine.go` | Consensus orchestration | ~2000 |
| `pkg/consensus/pbft/pbft.go` | HotStuff (2-chain) engine | ~1000 |
| `pkg/consensus/genesis/coordinator.go` | Genesis ceremony | ~800 |
| `pkg/state/executor.go` | Transaction execution | ~400 |
| `pkg/ingest/kafka/consumer.go` | Kafka consumer | ~900 |
| `pkg/ingest/kafka/verifier.go` | Signature verification | ~300 |
| `pkg/mempool/mempool.go` | Transaction pool | ~300 |
| `pkg/storage/cockroach/adapter.go` | DB adapter | ~600 |

---

## 11. Related Documents

### Design Documents
- [HLD](./HLD.md) - High-level design
- [Data Flow](./DATA_FLOW.md) - System data flow

### Architecture Documents
- [HotStuff Consensus](../architecture/03_hotstuff_consensus.md)
- [Genesis Bootstrap](../architecture/07_genesis_bootstrap.md)
- [State Machine](../architecture/05_state_machine.md)

### Source Code
- [Backend README](../../backend/README.md)

---

**[⬆️ Back to Top](#-navigation)**
