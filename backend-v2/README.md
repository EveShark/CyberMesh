# CyberMesh Backend

**Byzantine Fault Tolerant consensus engine for distributed cybersecurity operations**

Version: 1.0.0 | Go 1.24.0 | Production-Ready

---

## Overview

CyberMesh Backend is a Byzantine Fault Tolerant (BFT) consensus system that validates AI-generated security alerts through distributed consensus. The backend consumes messages from Kafka, verifies Ed25519 signatures, achieves consensus using PBFT, executes transactions through a deterministic state machine, and persists validated data to CockroachDB. The system tolerates up to f Byzantine failures where f = (N-1)/3.

**Core Capabilities:**
- PBFT consensus (3-phase: Pre-Prepare → Prepare → Commit)
- Deterministic state machine (pure functions, reproducible execution)
- Ed25519 signature verification (Kafka messages)
- Leader election with heartbeat-based rotation
- Genesis ceremony (cluster-wide initialization with clock skew tolerance)
- CockroachDB persistence (distributed SQL with TLS)
- P2P networking (libp2p with mDNS discovery)

**System Flow:**
```
Kafka Consumer → Ed25519 Verification → Mempool Validation → PBFT Consensus →
State Machine Execution → CockroachDB Persistence → Kafka Producer (control.commits.v1)
```

---

## Architecture Overview

### Component Interaction

The backend operates in a 5-layer architecture:

1. **Ingestion Layer** (`pkg/ingest/kafka/`)
   - Consumes from Kafka topics: ai.anomalies.v1, ai.evidence.v1, ai.policy.v1
   - Verifies Ed25519 signatures (domain: ai.anomaly.v1)
   - Validates content hash (SHA-256), timestamp skew (5-minute window), nonce uniqueness
   - Rejects invalid messages to Dead Letter Queue (ai.dlq.v1)

2. **Mempool Layer** (`pkg/mempool/`)
   - Transaction pool with rate limiting (1000/second)
   - Nonce tracking with 15-minute TTL (replay protection)
   - Duplicate detection
   - Max capacity: 1000 transactions, 10MB total

3. **Consensus Layer** (`pkg/consensus/`)
   - PBFT engine: 3-phase commit (pre-prepare, prepare, commit)
   - Leader election: heartbeat-based with round-robin rotation
   - Quorum: 2f+1 (e.g., 3 of 4 validators)
   - View change: timeout-triggered recovery for failed leaders
   - Genesis coordinator: cluster-wide initialization with attestations

4. **State Machine Layer** (`pkg/state/`)
   - Deterministic transaction execution (NO I/O, NO time.Now(), NO rand.Random())
   - Pure functions for reproducible state transitions
   - Merkle tree for state integrity verification
   - Snapshot support for fast node synchronization

5. **Storage Layer** (`pkg/storage/cockroach/`)
   - CockroachDB distributed SQL database
   - TLS encryption with connection pooling (50 max, 10 idle)
   - Idempotent operations (retry-safe)
   - Tables: blocks, transactions, anomalies, validators

### Integration with AI Service

The backend receives anomaly detections from AI service via Kafka and returns commitment notifications:

- **Incoming**: ai.anomalies.v1, ai.evidence.v1, ai.policy.v1 (AI → Backend)
- **Outgoing**: control.commits.v1, control.reputation.v1 (Backend → AI)
- **Message Format**: Protobuf with Ed25519 signatures

See `ai-service/README.md` for AI service architecture.

---

## System Requirements

**Runtime:**
- Go 1.24.0 or higher
- CockroachDB 21+ or PostgreSQL 12+ (for persistence)
- Kafka broker (Confluent Cloud or self-hosted with TLS + SASL)
- Minimum 4 validators for BFT (tolerates 1 Byzantine failure)

**Development:**
- OpenSSL (for Ed25519 key generation)
- Git (for version control)
- Make (optional, for build automation)

**Operating Systems:**
- Linux (tested on Ubuntu 20.04+)
- macOS (tested on Monterey+)
- Windows 10/11 (tested with native Go)

**Hardware (per validator node):**
- CPU: 2+ cores
- RAM: 4GB minimum, 8GB recommended
- Disk: 50GB for database + logs
- Network: 1Gbps

---

## Quick Start

### 1. Install Dependencies

```bash
cd backend
go mod download
go mod verify
```

### 2. Generate Ed25519 Keys

Generate signing keys for each validator node:

```bash
# For validator 1
openssl genpkey -algorithm ED25519 -out keys/validator_1_private.pem

# Extract public key
openssl pkey -in keys/validator_1_private.pem -pubout -out keys/validator_1_public.pem

# Convert to hex format for .env
# (Use provided script or manually convert)
```

Repeat for all validators (1-5 for 5-node cluster).

### 3. Configure Environment

Copy `.env.example` to `.env` or use existing `.env`:

**Critical Variables:**
- `NODE_ID=1` (must match CONSENSUS_NODES)
- `CONSENSUS_NODES=1,2,3,4,5`
- `QUORUM_SIZE=3` (2f+1, where f=1 for 4 nodes)
- `DB_DSN=postgresql://user:pass@host:26257/cybermesh?sslmode=verify-full`
- `KAFKA_BROKERS=localhost:9092` (or Confluent Cloud)
- `ENCRYPTION_KEY=<64-char hex>` (32 bytes)
- `CRYPTO_SIGNING_KEY_HEX=<hex>` (64 bytes Ed25519 private key)
- `VALIDATOR_1_PUBKEY_HEX=<hex>`, `VALIDATOR_2_PUBKEY_HEX=<hex>`, ... (32 bytes each)

### 4. Initialize Database

```bash
# Create database
cockroach sql --url="$DB_DSN" -e "CREATE DATABASE IF NOT EXISTS cybermesh;"

# Run migrations (if migration system exists)
# go run cmd/migrate/main.go up
```

### 5. Build and Run

```bash
# Build binary
go build -o bin/cybermesh ./cmd/cybermesh/main.go

# Run
./bin/cybermesh

# Or run directly
go run ./cmd/cybermesh/main.go
```

### 6. Verify Health

```bash
# Check node started successfully
curl http://localhost:8443/health

# Check consensus status (if API enabled)
curl http://localhost:8443/status
```

**Expected Output:**
```
=================================================================================
CyberMesh Backend - Full Wiring Bootstrap
=================================================================================

[INFO] Loaded environment from: .env
Startup complete. Kafka ingest, mempool, and consensus wiring are active.
Press Ctrl+C to initiate shutdown.
```

---

## Configuration

### Node Identity

```bash
NODE_ID=1                              # Unique validator ID (1-5)
NODE_VERSION=1.0.0                     # Node version
NODE_TYPE=validator                    # Node role (validator, observer)
ENVIRONMENT=development                # development|staging|production
REGION=local                           # Geographic region
```

### Cluster Topology

```bash
CLUSTER_TOPOLOGY=control:1,gateway:1,storage:1,observer:1,ingest:1
CONSENSUS_NODES=1,2,3,4,5             # Validator node IDs (comma-separated)
TRUSTED_NODES=1,2,3,4,5               # Trusted peer node IDs
QUORUM_SIZE=3                         # Minimum votes for consensus (2f+1)
CONSENSUS_MIN_QUORUM_SIZE=3           # Enforce minimum quorum in pre-validation
```

**Important:** `NODE_ID` must be present in `CONSENSUS_NODES` list.

### Database (CockroachDB)

```bash
DB_DSN=postgresql://user:password@host:26257/cybermesh?sslmode=verify-full
DATABASE_URL=postgresql://user:password@host:26257/cybermesh  # Alternative
DB_TLS=true                           # Enable TLS (enforced in production)
```

**Connection Pooling:**
- Max connections: 50
- Idle connections: 10
- Connection timeout: 30 seconds

### Kafka Configuration

```bash
# Broker
KAFKA_BROKERS=pkc-xxx.gcp.confluent.cloud:9092  # Comma-separated list
KAFKA_TLS_ENABLED=true
KAFKA_SASL_MECHANISM=SCRAM-SHA-256    # Or PLAIN (dev only)
KAFKA_SASL_USERNAME=<api-key>
KAFKA_SASL_PASSWORD=<api-secret>
```

**Consumer Topics (AI → Backend):**
```bash
KAFKA_INPUT_TOPICS=ai.anomalies.v1,ai.evidence.v1,ai.policy.v1
KAFKA_CONSUMER_GROUP_ID=cybermesh-consensus  # Base group ID
KAFKA_DLQ_TOPIC=ai.dlq.v1            # Dead letter queue
KAFKA_MAX_TIMESTAMP_SKEW=5m          # Max clock skew tolerance
```

**Producer Topics (Backend → AI):**
```bash
CONTROL_COMMITS_TOPIC=control.commits.v1
CONTROL_REPUTATION_TOPIC=control.reputation.v1
CONTROL_POLICY_TOPIC=control.policy.v1
CONTROL_EVIDENCE_TOPIC=control.evidence.v1
```

**Consumer Strategy:**
Each node uses unique consumer group: `cybermesh-consensus-node-1`, `cybermesh-consensus-node-2`, etc. This ensures all nodes consume all partitions for consensus.

### Security & Cryptography

**Encryption Keys:**
```bash
ENCRYPTION_KEY=<64-char hex>          # AES-256 key (32 bytes)
SECRET_KEY=<64-char hex>              # HMAC secret (32 bytes)
SALT=<32+ chars>                      # Password hashing salt
JWT_SECRET=<128+ chars>               # JWT signing key (production: 128+ chars)
```

**Ed25519 Signing (Backend):**
```bash
CRYPTO_SIGNING_KEY_HEX=<128-char hex>  # Ed25519 private key (64 bytes)
CRYPTO_SIGNING_KEY_ID=node-1           # Key identifier
CRYPTO_AUDIT_ENABLED=false             # Enable crypto audit logging
```

**Control Plane Signing:**
```bash
CONTROL_SIGNING_KEY_PATH=keys/backend_signing_key.pem
CONTROL_SIGNING_KEY_ID=node-1
CONTROL_SIGNING_DOMAIN=control.commits.v1
CONTROL_PRODUCER_ID=node-1
```

**Validator Public Keys (Required for each consensus node):**
```bash
VALIDATOR_1_PUBKEY_HEX=<64-char hex>   # Node 1 public key (32 bytes)
VALIDATOR_2_PUBKEY_HEX=<64-char hex>   # Node 2 public key
VALIDATOR_3_PUBKEY_HEX=<64-char hex>   # Node 3 public key
VALIDATOR_4_PUBKEY_HEX=<64-char hex>   # Node 4 public key
VALIDATOR_5_PUBKEY_HEX=<64-char hex>   # Node 5 public key
```

**Production Security Requirements:**
- All keys must be cryptographically random (no placeholders)
- TLS enforced for Kafka + CockroachDB
- SASL mechanism: SCRAM-SHA-256 or SCRAM-SHA-512 (PLAIN not allowed)
- JWT secret minimum 128 characters
- File permissions: 0600 for key files

### Consensus Parameters

```bash
CONSENSUS_BLOCK_TIMEOUT=5s            # Block proposal timeout
CONSENSUS_BASE_TIMEOUT=10s            # Base consensus timeout
CONSENSUS_MAX_TIMEOUT=60s             # Maximum timeout (AIMD)
CONSENSUS_MIN_TIMEOUT=5s              # Minimum timeout
CONSENSUS_TIMEOUT_INCREASE_FACTOR=1.5 # AIMD multiplicative increase
CONSENSUS_TIMEOUT_DECREASE_DELTA=200ms # AIMD additive decrease
CONSENSUS_VIEWCHANGE_TIMEOUT=30s      # View change timeout
CONSENSUS_HEARTBEAT_INTERVAL=500ms    # Leader heartbeat interval
CONSENSUS_MAX_IDLE_TIME=3s            # Max time without heartbeat
CONSENSUS_MISSED_HEARTBEATS=6         # Missed heartbeats before timeout
CONSENSUS_HEARTBEAT_JITTER=0.05       # Heartbeat timing jitter (5%)
```

**AIMD (Additive Increase Multiplicative Decrease):**
- Enabled: `CONSENSUS_ENABLE_AIMD=true`
- Increases timeout on failures (×1.5)
- Decreases timeout on success (-200ms)
- Prevents timeout oscillation

**Reputation System:**
```bash
CONSENSUS_ENABLE_REPUTATION=true      # Enable leader reputation tracking
CONSENSUS_MIN_LEADER_REPUTATION=0.7   # Minimum reputation to be leader
```

**Quarantine:**
```bash
CONSENSUS_ENABLE_QUARANTINE=true      # Enable malicious node quarantine
```

### Genesis Ceremony

```bash
GENESIS_READY_TIMEOUT=30s             # Timeout for ready attestations
GENESIS_CERTIFICATE_TIMEOUT=60s       # Timeout for certificate collection
GENESIS_CLOCK_SKEW_TOLERANCE=15m      # Clock skew tolerance during genesis
GENESIS_READY_REFRESH_INTERVAL=30s    # Ready message refresh interval
GENESIS_STATE_PATH=/app/data/genesis_state.json  # Genesis state file
```

Genesis ceremony ensures all validators start with identical configuration before consensus begins.

### Block Building

```bash
BLOCK_MAX_TXS=500                     # Max transactions per block
BLOCK_MAX_BYTES=4194304               # Max block size (4MB)
BLOCK_MIN_MEMPOOL_TXS=1               # Min transactions to build block
BLOCK_BUILD_INTERVAL=500ms            # Block building interval
```

### Mempool Configuration

```bash
MEMPOOL_MAX_TXS=1000                  # Max transactions in mempool
MEMPOOL_MAX_BYTES=10485760            # Max mempool size (10MB)
MEMPOOL_NONCE_TTL=15m                 # Nonce TTL (replay protection)
MEMPOOL_SKEW_TOLERANCE=5m             # Timestamp skew tolerance
MEMPOOL_RATE_PER_SECOND=1000          # Rate limit (transactions/second)
```

### P2P Networking (Multi-Node Consensus)

```bash
ENABLE_P2P=true                       # Enable P2P networking
P2P_BOOTSTRAP_PEERS=<multiaddr-list>  # Comma-separated bootstrap peers
P2P_TOPICS=consensus,blocks,txs       # PubSub topics
RENDEZVOUS_NS=cybermesh/prod          # Rendezvous namespace
P2P_ENABLE_MDNS=true                  # Enable mDNS local discovery
P2P_HEARTBEAT_INTERVAL=5s             # Peer heartbeat interval
P2P_LIVENESS_TIMEOUT=20s              # Peer liveness timeout
P2P_DISCOVERY_INTERVAL=15s            # Peer discovery interval
ALLOWED_IPS=127.0.0.0/8,::1/128,10.0.0.0/8  # IP allowlist (CIDR notation)
```

**Multiaddr Format:**
```
/dns4/validator-0.headless.svc/tcp/8001/p2p/12D3KooW...
```

### Persistence Worker

```bash
ENABLE_PERSISTENCE=true               # Enable database persistence
PERSIST_QUEUE_SIZE=1024               # Persistence queue size
PERSIST_RETRY_MAX=3                   # Max retry attempts
PERSIST_RETRY_BACKOFF_MS=100          # Initial backoff (ms)
PERSIST_MAX_BACKOFF_MS=5000           # Max backoff (ms)
PERSIST_WORKERS=1                     # Number of worker threads
PERSIST_SHUTDOWN_TIMEOUT=30s          # Graceful shutdown timeout
```

### API Server (Optional)

```bash
ENABLE_API=false                      # Enable HTTP API server
API_LISTEN_ADDR=:8443                 # API listen address
API_TLS_ENABLED=false                 # Enable TLS for API (production: true)
```

### Logging & Audit

```bash
LOG_LEVEL=info                        # debug|info|warn|error
LOG_DEVELOPMENT=false                 # Development mode (pretty-print)
AUDIT_LOG_PATH=./logs/audit.log       # Audit log file path
AUDIT_ENABLE_SIGNING=true             # Sign audit logs with HMAC
AUDIT_SIGNING_KEY=<64+ chars>         # HMAC signing key for audit logs
```

---

## Project Structure

```
backend/
├── cmd/
│   └── cybermesh/
│       └── main.go               # Entry point (startup, signal handling, graceful shutdown)
│
├── pkg/                          # Core implementation
│   ├── consensus/                # Consensus layer
│   │   ├── consensus.go          # Public API
│   │   ├── pbft/                 # PBFT implementation
│   │   │   ├── pbft.go           # PBFT engine (3-phase commit)
│   │   │   ├── quorum.go         # Quorum calculations (2f+1)
│   │   │   ├── storage.go        # Consensus state storage
│   │   │   └── types.go          # PBFT types
│   │   ├── leader/               # Leader election
│   │   │   ├── leader.go         # Leader election logic
│   │   │   ├── rotation.go       # Round-robin rotation
│   │   │   ├── heartbeat.go      # Heartbeat protocol
│   │   │   └── types.go          # Leader types
│   │   ├── messages/             # Consensus messages
│   │   │   ├── types.go          # Message definitions (Proposal, Vote, ViewChange, NewView)
│   │   │   ├── validation.go     # Message validation
│   │   │   └── encoding.go       # Message serialization (CBOR)
│   │   ├── genesis/              # Genesis ceremony
│   │   │   └── coordinator.go    # Genesis coordinator (cluster initialization)
│   │   ├── api/                  # Consensus API adapters
│   │   │   ├── engine.go         # ConsensusEngine (main orchestrator)
│   │   │   ├── adapters.go       # Adapter implementations
│   │   │   └── types.go          # API types
│   │   └── types/                # Consensus interfaces
│   │       ├── interfaces.go     # Core interfaces (CryptoService, Logger, etc.)
│   │       └── common.go         # Common types (ValidatorID, BlockHash, etc.)
│   │
│   ├── state/                    # Deterministic state machine
│   │   ├── executor.go           # Transaction execution (CRITICAL: NO I/O)
│   │   │                         # ❌ NO database calls, NO network, NO file I/O
│   │   │                         # ❌ NO time.Now(), NO rand.Random()
│   │   │                         # ✅ Pure functions only
│   │   ├── reducers.go           # State reducers (ApplyEvent, ApplyEvidence, ApplyPolicy)
│   │   ├── model.go              # State models (Transaction types)
│   │   ├── merkle.go             # Merkle tree (state integrity)
│   │   ├── store.go              # In-memory state store (MemStore)
│   │   ├── snapshot.go           # State snapshots (fast sync)
│   │   ├── query.go              # State queries (read-only)
│   │   └── codec.go              # State serialization
│   │
│   ├── ingest/
│   │   └── kafka/                # Kafka integration
│   │       ├── consumer.go       # Kafka consumer (IBM sarama library)
│   │       ├── producer.go       # Kafka producer (control messages)
│   │       ├── verifier.go       # Ed25519 signature verification
│   │       │                     # Verifies: signature, content hash, timestamp, nonce
│   │       ├── signing.go        # Message signing (control.commits.v1)
│   │       ├── schema.go         # Protobuf parsing (AnomalyMsg, EvidenceMsg, PolicyMsg)
│   │       ├── config.go         # Kafka configuration
│   │       └── scram.go          # SASL/SCRAM authentication
│   │
│   ├── mempool/                  # Transaction pool
│   │   ├── mempool.go            # Mempool implementation
│   │   ├── nonce.go              # Nonce tracking (replay protection)
│   │   └── validation.go         # Transaction validation
│   │
│   ├── storage/                  # Database layer
│   │   └── cockroach/            # CockroachDB adapter
│   │       ├── adapter.go        # CockroachDB adapter implementation
│   │       ├── connection.go     # Connection pool management
│   │       ├── schema.go         # Database schema
│   │       └── queries.go        # SQL queries
│   │
│   ├── block/                    # Block structure
│   │   ├── block.go              # Block definition
│   │   ├── builder.go            # Block builder (mempool → block)
│   │   └── validation.go         # Block validation
│   │
│   ├── p2p/                      # P2P networking (libp2p)
│   │   ├── router.go             # P2P router (PubSub)
│   │   ├── discovery.go          # Peer discovery (mDNS + DHT)
│   │   ├── state.go              # P2P state management
│   │   └── types.go              # P2P types
│   │
│   ├── api/                      # HTTP API (optional)
│   │   ├── server.go             # HTTP server
│   │   ├── router.go             # Route definitions
│   │   ├── health.go             # Health checks (/health)
│   │   ├── blocks.go             # Block queries (/blocks)
│   │   ├── validators.go         # Validator queries (/validators)
│   │   ├── state.go              # State queries (/state)
│   │   └── middleware.go         # Middleware (auth, logging, rate limiting)
│   │
│   ├── config/                   # Configuration management
│   │   ├── config.go             # Config loader
│   │   ├── topology.go           # Cluster topology
│   │   └── validation.go         # Config validation
│   │
│   ├── utils/                    # Utilities
│   │   ├── logger.go             # Zap logger wrapper
│   │   ├── crypto.go             # CryptoService (Ed25519 signing/verification)
│   │   ├── config_manager.go     # ConfigManager (environment variables)
│   │   ├── audit.go              # AuditLogger (HMAC-signed audit logs)
│   │   ├── allowlist.go          # IPAllowlist (CIDR-based)
│   │   └── errors.go             # Custom error types
│   │
│   └── wiring/                   # Dependency injection
│       ├── service.go            # Service orchestrator
│       ├── persistence.go        # PersistenceWorker
│       └── config.go             # Wiring configuration
│
├── proto/                        # Protobuf definitions (7 message types)
│   ├── ai_anomaly.proto          # AnomalyEvent (AI → Backend)
│   ├── ai_evidence.proto         # EvidenceEvent (AI → Backend)
│   ├── ai_policy.proto           # PolicyEvent (AI → Backend)
│   ├── control_commits.proto     # CommitEvent (Backend → AI)
│   ├── control_reputation.proto  # ReputationEvent (Backend → AI)
│   ├── control_policy.proto      # PolicyUpdateEvent (Backend → AI)
│   └── control_evidence.proto    # EvidenceRequestEvent (Backend → AI)
│
├── bin/                          # Compiled binaries
│   └── cybermesh                 # Main executable
│
├── keys/                         # Cryptographic keys (generated at setup)
│   ├── validator_1_private.pem   # Validator 1 Ed25519 private key
│   ├── validator_2_private.pem   # Validator 2 Ed25519 private key
│   └── backend_signing_key.pem   # Control plane signing key
│
├── certs/                        # TLS certificates (for production)
│   ├── root.crt                  # CA certificate
│   ├── server.crt                # Server certificate
│   └── server.key                # Server private key
│
├── scripts/                      # Utility scripts
│   ├── generate_keys.sh          # Ed25519 key generation
│   └── init_db.sh                # Database initialization
│
├── .env                          # Environment configuration (634 lines)
├── go.mod                        # Go module definition
├── go.sum                        # Go module checksums
├── Makefile                      # Build automation (optional)
└── Dockerfile                    # Docker image definition
```

---

## Core Concepts

### PBFT Consensus

**3-Phase Commit Protocol:**

1. **Pre-Prepare Phase**
   - Leader proposes block to all validators
   - Validators verify leader signature and block validity
   - Validators enter Prepare phase if valid

2. **Prepare Phase**
   - Validators broadcast Prepare votes to all peers
   - Each validator waits for 2f Prepare votes (quorum)
   - Validators enter Commit phase when quorum reached

3. **Commit Phase**
   - Validators broadcast Commit votes to all peers
   - Each validator waits for 2f+1 Commit votes (supermajority)
   - Validators execute block when supermajority reached

**Quorum Calculation:**
```
Total validators: N = 4
Byzantine failures tolerated: f = (N-1)/3 = 1
Quorum (Prepare): 2f = 2
Supermajority (Commit): 2f+1 = 3
```

**File:** `pkg/consensus/pbft/pbft.go`, `pkg/consensus/pbft/quorum.go`

### Leader Election

**Heartbeat-Based Rotation:**

- Leader sends heartbeat every 500ms to all validators
- Validators monitor heartbeat arrival
- If 6 consecutive heartbeats missed (3 seconds), trigger view change
- Round-robin rotation with reputation scoring
- Minimum reputation 0.7 required to be leader

**View Change Protocol:**

1. Validator detects leader timeout
2. Broadcast ViewChange message to all peers
3. Collect 2f+1 ViewChange messages (supermajority)
4. New leader elected (next in round-robin)
5. New leader broadcasts NewView message
6. Resume consensus at new view

**File:** `pkg/consensus/leader/leader.go`, `pkg/consensus/leader/rotation.go`, `pkg/consensus/leader/heartbeat.go`

### Deterministic State Machine

**CRITICAL RULES (Enforced):**

❌ **PROHIBITED in `pkg/state/`:**
- Database calls (`sql.DB`, `pgx`, etc.)
- Network operations (`net.Dial`, `http.Client`, etc.)
- File I/O (`os.Open`, `io.Reader`, etc.)
- `time.Now()` - breaks determinism (use passed `now` parameter)
- `rand.Random()` - breaks determinism (use deterministic PRNG if needed)
- External dependencies (only `pkg/state` internal imports)

✅ **REQUIRED:**
- Pure functions (same input → same output)
- Accept timestamp as parameter (from block header)
- Reproducible operations (any validator executing same tx gets same result)
- State transitions via reducers (ApplyEvent, ApplyEvidence, ApplyPolicy)

**Transaction Execution Flow:**

```
1. ApplyBlock() called with transactions + block timestamp
2. Begin transaction on latest state version
3. For each transaction:
   a. Validate: signature, nonce, timestamp, payload
   b. Check nonce uniqueness (replay protection)
   c. Call reducer: ApplyEvent / ApplyEvidence / ApplyPolicy
   d. Update state store (key-value mutations)
4. Commit all changes atomically
5. Compute Merkle root (state integrity proof)
6. Return new version, root, and receipts
```

**State Store:**
- In-memory key-value store (MemStore)
- Versioned (each block = new version)
- Merkle tree for integrity
- Snapshot support for state sync

**File:** `pkg/state/executor.go`, `pkg/state/reducers.go`, `pkg/state/merkle.go`

### Transaction Lifecycle

**7-State Machine:**

1. **RECEIVED** - Kafka consumer received message
2. **VERIFIED** - Ed25519 signature + content hash verified
3. **MEMPOOL** - Added to mempool (nonce checked, rate limited)
4. **PROPOSED** - Leader included in block proposal
5. **PREPARED** - 2f validators voted in Prepare phase
6. **COMMITTED** - 2f+1 validators voted in Commit phase (consensus reached)
7. **EXECUTED** - State machine executed transaction, persisted to database

**File:** `pkg/ingest/kafka/consumer.go`, `pkg/mempool/mempool.go`, `pkg/consensus/pbft/pbft.go`, `pkg/state/executor.go`

### Genesis Ceremony

The genesis ceremony ensures all validators start with identical configuration before consensus begins:

**Process:**

1. Each validator loads configuration (timeouts, quorum size, validator set)
2. Compute ConfigHash (SHA-256 of all consensus parameters)
3. Compute PeerHash (SHA-256 of sorted validator IDs)
4. Broadcast ReadyAttestation (ConfigHash + PeerHash + signature)
5. Collect ReadyAttestations from all validators
6. Verify all validators have identical ConfigHash and PeerHash
7. If match: generate GenesisCertificate, activate consensus timers
8. If mismatch: abort startup (configuration inconsistency detected)

**Clock Skew Tolerance:**
- Genesis: 15 minutes (GENESIS_CLOCK_SKEW_TOLERANCE)
- Runtime: 5 seconds (CONSENSUS_CLOCK_SKEW_TOLERANCE)

**Purpose:**
- Prevent split-brain (different configurations)
- Detect misconfiguration before consensus starts
- Ensure deterministic genesis state across cluster

**File:** `pkg/consensus/genesis/coordinator.go`

---

## Kafka Integration

### Message Verification

**Ed25519 Signature Verification Process:**

1. Parse message from Kafka (Protobuf)
2. Extract: payload, signature, public_key, nonce, timestamp
3. Reconstruct signed data: `domain_separation + payload`
   - Domain: "ai.anomaly.v1" for anomalies
4. Verify Ed25519 signature: `ed25519.Verify(public_key, signed_data, signature)`
5. Verify content hash: `SHA-256(payload) == msg.content_hash`
6. Verify timestamp skew: `|msg.timestamp - now| < 5 minutes`
7. Verify nonce uniqueness: Check mempool nonce tracker
8. If all pass: accept message, else send to DLQ (ai.dlq.v1)

**File:** `pkg/ingest/kafka/verifier.go`

### Consumer Configuration

**Strategy:** All-Partitions-Per-Node

Each validator node uses unique consumer group:
```
Node 1: cybermesh-consensus-node-1
Node 2: cybermesh-consensus-node-2
Node 3: cybermesh-consensus-node-3
```

This ensures **all nodes consume all messages** for consensus. If using shared consumer group, only one node would receive each message (incorrect for BFT).

**File:** `pkg/ingest/kafka/consumer.go`

### Producer (Control Messages)

**Publishing to AI Service:**

After consensus commits block, backend publishes CommitEvent to `control.commits.v1`:

```
CommitEvent {
  height: 1234
  block_hash: [32]byte (SHA-256)
  state_root: [32]byte (Merkle root)
  tx_count: 10
  anomaly_ids: ["uuid1", "uuid2", ...] (committed anomaly UUIDs)
  timestamp: Unix seconds
  signature: [64]byte (Ed25519)
  public_key: [32]byte (validator public key)
}
```

AI service consumes this to update anomaly lifecycle (COMMITTED state) and trigger adaptive learning.

**File:** `pkg/ingest/kafka/producer.go`, `pkg/ingest/kafka/signing.go`

---

## Security

### Ed25519 Signing & Verification

**Algorithm:** Ed25519 (Elliptic Curve Digital Signature)
- Key size: 32 bytes (256 bits)
- Signature size: 64 bytes (512 bits)
- Library: Go `crypto/ed25519` (native implementation)

**Signing Process (Backend → AI):**
```
1. Generate signing_data = domain_separation + payload
2. signature = Ed25519_sign(private_key, signing_data)
3. Attach signature, public_key to message
4. Publish to Kafka
```

**Verification Process (AI → Backend):**
```
1. Reconstruct signing_data = domain_separation + payload
2. valid = Ed25519_verify(public_key, signing_data, signature)
3. If valid: proceed, else reject to DLQ
```

**Domain Separation:**
- `ai.anomaly.v1` - For anomaly messages
- `ai.evidence.v1` - For evidence messages
- `ai.policy.v1` - For policy messages
- `control.commits.v1` - For commit messages

Prevents signature reuse across message types.

**File:** `pkg/utils/crypto.go`, `pkg/ingest/kafka/verifier.go`, `pkg/ingest/kafka/signing.go`

### Nonce Management & Replay Protection

**Nonce Format (16 bytes):**
```
[8 bytes timestamp_ms][4 bytes node_id][4 bytes random]
```

**Replay Protection:**
- Mempool tracks seen nonces with 15-minute TTL
- Nonce must be unique (reject duplicates)
- Nonce timestamp must be within skew tolerance (5 minutes)
- Expired nonces removed from tracker every 60 seconds

**File:** `pkg/mempool/nonce.go`

### TLS Configuration

**CockroachDB TLS:**
- Required in production (`DB_TLS=true`)
- Certificate verification: `sslmode=verify-full`
- Root CA cert: `certs/root.crt`

**Kafka TLS:**
- Enabled: `KAFKA_TLS_ENABLED=true`
- SASL mechanism: SCRAM-SHA-256 (strong password hashing)
- TLS + SASL provides encryption + authentication

**No Custom Certificates Required:**
- Confluent Cloud handles TLS internally
- CockroachDB Cloud provides TLS endpoints

### Audit Logging

**Features:**
- HMAC-signed audit logs (tamper-proof)
- Structured JSON format
- Component: consensus
- Events: block proposals, commits, view changes, validator actions

**Configuration:**
```bash
AUDIT_LOG_PATH=./logs/audit.log
AUDIT_ENABLE_SIGNING=true
AUDIT_SIGNING_KEY=<64+ chars>  # HMAC key
```

**File:** `pkg/utils/audit.go`

---

## Database Schema

**Tables:**

1. **blocks**
   - height (PRIMARY KEY)
   - block_hash (32 bytes, indexed)
   - state_root (32 bytes)
   - previous_hash (32 bytes)
   - timestamp (Unix seconds)
   - tx_count
   - proposer_id (32 bytes)

2. **transactions**
   - tx_id (UUID, PRIMARY KEY)
   - block_height (FOREIGN KEY → blocks.height)
   - tx_index (position in block)
   - tx_type (event, evidence, policy)
   - content_hash (32 bytes, indexed)
   - payload (bytea)
   - producer_id (32 bytes)
   - nonce (16 bytes, indexed)
   - signature (64 bytes)
   - timestamp (Unix seconds)

3. **anomalies**
   - anomaly_id (UUID, PRIMARY KEY)
   - anomaly_type (ddos, malware, anomaly, etc.)
   - severity (1-10)
   - confidence (0.0-1.0)
   - source (IP or hostname)
   - block_height (FOREIGN KEY → blocks.height)
   - created_at (timestamp)

4. **validators**
   - validator_id (32 bytes, PRIMARY KEY)
   - public_key (32 bytes)
   - reputation (0.0-1.0)
   - is_active (boolean)
   - joined_view (uint64)

**Indexes:**
- blocks.block_hash (lookup by hash)
- transactions.content_hash (duplicate detection)
- transactions.nonce (replay protection)
- anomalies.anomaly_type (filtering)
- anomalies.block_height (range queries)

**Connection Pooling:**
- Max connections: 50
- Idle connections: 10
- Connection lifetime: 30 seconds
- Idle timeout: 5 minutes

**File:** `pkg/storage/cockroach/schema.go`, `pkg/storage/cockroach/connection.go`

---

## Monitoring & Metrics

### Consensus Metrics

```
consensus_view                  - Current view number
consensus_height                - Current block height
consensus_leader_id             - Current leader ID
consensus_quorum_size           - Required quorum size
consensus_validator_count       - Total validators
consensus_proposals_total       - Total proposals received
consensus_prepares_total        - Total prepare votes
consensus_commits_total         - Total commit votes
consensus_view_changes_total    - Total view changes
consensus_blocks_committed      - Total blocks committed
```

### Performance Metrics

```
block_build_latency_ms          - Time to build block
consensus_round_latency_ms      - Time to reach consensus
state_execution_latency_ms      - Time to execute transactions
persistence_latency_ms          - Time to persist to database
kafka_consume_latency_ms        - Time to consume Kafka message
kafka_produce_latency_ms        - Time to publish Kafka message
```

### Health Metrics

```
mempool_size                    - Current mempool transaction count
mempool_bytes                   - Current mempool size in bytes
kafka_consumer_lag              - Kafka consumer lag (messages behind)
database_connections_active     - Active database connections
p2p_peers_connected             - Connected P2P peers
```

**File:** `pkg/api/metrics.go` (if API enabled)

---

## Deployment

### Docker

**Build Image:**
```bash
docker build -t cybermesh-backend:latest -f Dockerfile .
```

**Run Container:**
```bash
docker run -d --name validator-1 \
  --env-file .env \
  -e NODE_ID=1 \
  -p 8443:8443 \
  -v ./data:/app/data \
  -v ./keys:/app/keys:ro \
  -v ./certs:/app/certs:ro \
  cybermesh-backend:latest
```

### Multi-Node Cluster

**Requirements:**
- Minimum 4 validators for BFT (tolerates 1 failure)
- Each node needs unique NODE_ID (1-4)
- All nodes must have identical configuration (genesis ceremony enforces this)
- P2P networking enabled (ENABLE_P2P=true)
- Bootstrap peers configured (P2P_BOOTSTRAP_PEERS)

**Startup Order:**
1. Start all nodes simultaneously (or within genesis timeout window)
2. Nodes exchange ReadyAttestations
3. Genesis ceremony completes when all nodes ready
4. Consensus timers activate
5. Normal operation begins

**File:** `cmd/cybermesh/main.go` (startup sequence)

### Production Checklist

**Pre-Deployment:**
- [ ] Generate Ed25519 keys for all validators
- [ ] Set `ENVIRONMENT=production`
- [ ] Configure CockroachDB with TLS (DB_TLS=true)
- [ ] Configure Kafka with TLS + SCRAM-SHA-256
- [ ] Set strong secrets (ENCRYPTION_KEY, JWT_SECRET min 128 chars)
- [ ] Configure P2P bootstrap peers (all validator multiaddrs)
- [ ] Set QUORUM_SIZE correctly (2f+1 for N validators)
- [ ] Verify all validator public keys in .env (VALIDATOR_X_PUBKEY_HEX)

**Post-Deployment:**
- [ ] Verify all nodes completed genesis ceremony
- [ ] Check consensus_view increments (blocks committing)
- [ ] Monitor Kafka consumer lag (should be 0)
- [ ] Verify database persistence (blocks table populated)
- [ ] Check audit logs for anomalies
- [ ] Monitor P2P peer connections (N-1 peers expected)

**Security:**
- [ ] TLS enabled for Kafka + CockroachDB
- [ ] SASL mechanism: SCRAM-SHA-256 (not PLAIN)
- [ ] Key file permissions: 0600 (owner read/write only)
- [ ] No secrets in logs (verify with `grep -i password logs/`)
- [ ] IP allowlist configured (ALLOWED_IPS)
- [ ] Audit logging enabled with HMAC signing

---

## Troubleshooting

### Node Won't Start

**Symptom:** Node exits immediately after startup

**Common Causes:**

1. **NODE_ID not in CONSENSUS_NODES**
   ```
   Error: local validator id abc123... is not present in consensus set
   ```
   **Fix:** Ensure `NODE_ID=1` and `CONSENSUS_NODES=1,2,3,4` includes your node ID

2. **Missing Validator Public Key**
   ```
   Error: missing VALIDATOR_1_PUBKEY_HEX for validator node 1
   ```
   **Fix:** Set `VALIDATOR_1_PUBKEY_HEX=<hex>` in .env for all consensus nodes

3. **Database Connection Failed**
   ```
   Error: cockroach connection failed: connection refused
   ```
   **Fix:** Verify `DB_DSN`, check CockroachDB is running, verify TLS certificates

4. **Kafka Auth Failed**
   ```
   Error: SASL authentication failed
   ```
   **Fix:** Verify `KAFKA_SASL_USERNAME` and `KAFKA_SASL_PASSWORD`

### Consensus Hangs (No Blocks Committing)

**Symptom:** `consensus_view` stuck at 0, no blocks committing

**Debug Steps:**

1. Check quorum size:
   ```
   CONSENSUS_NODES=1,2,3,4  # 4 validators
   QUORUM_SIZE=3            # 2f+1 = 3 (must have 3 of 4 online)
   ```

2. Verify all nodes completed genesis:
   ```bash
   grep "genesis ceremony completed" logs/*.log
   # Should see message from all nodes
   ```

3. Check P2P connectivity:
   ```bash
   grep "p2p_peers_connected" logs/*.log
   # Should show N-1 peers for N-node cluster
   ```

4. Check leader election:
   ```bash
   grep "heartbeat" logs/*.log
   # Should see heartbeat messages from leader
   ```

**Common Fixes:**
- Restart nodes if genesis ceremony failed
- Verify P2P bootstrap peers are correct
- Check firewall rules (allow P2P port 8001)
- Increase `GENESIS_READY_TIMEOUT` if nodes starting slowly

### State Mismatch (Different Merkle Roots)

**Symptom:**
```
Error: state root mismatch after executing block
Expected: abc123...
Got: def456...
```

**Cause:** Non-deterministic code in `pkg/state/` violating determinism rules

**Debug Steps:**

1. Check for prohibited operations in `pkg/state/`:
   - Database calls: `grep -r "sql\\.DB\\|pgx" pkg/state/`
   - time.Now(): `grep -r "time\\.Now" pkg/state/`
   - rand.Random(): `grep -r "rand\\.Random" pkg/state/`

2. Verify all reducers use passed timestamp parameter (not `time.Now()`)

3. Check for external dependencies:
   ```bash
   go list -f '{{.Imports}}' ./pkg/state/
   # Should only show pkg/state internal imports
   ```

**Fix:** Remove non-deterministic code, restart all nodes with clean state

### Ed25519 Verification Failed

**Symptom:**
```
Error: signature verification failed for anomaly <uuid>
```

**Debug Steps:**

1. Check public key configuration:
   ```bash
   echo $VALIDATOR_1_PUBKEY_HEX
   # Should be 64-character hex string (32 bytes)
   ```

2. Verify key consistency:
   ```bash
   # Extract public key from private key
   openssl pkey -in keys/validator_1_private.pem -pubout -outform DER | tail -c 32 | xxd -p -c 64
   # Should match VALIDATOR_1_PUBKEY_HEX
   ```

3. Check clock skew:
   ```bash
   # Compare timestamps
   date +%s  # Backend timestamp
   # vs msg.timestamp in Kafka message
   # Should be within 5 minutes (300 seconds)
   ```

4. Verify domain separation:
   ```bash
   grep "domain_separation" pkg/ingest/kafka/verifier.go
   # Should match AI service domain (ai.anomaly.v1)
   ```

**Common Fixes:**
- Regenerate keys if mismatch detected
- Sync system clocks (use NTP)
- Verify AI service using correct signing domain

### Database Connection Failed

**Symptom:**
```
Error: dial tcp: connection refused
```

**Debug Steps:**

1. Test database connectivity:
   ```bash
   cockroach sql --url="$DB_DSN" -e "SELECT 1;"
   # Should return: 1
   ```

2. Verify DSN format:
   ```
   postgresql://user:pass@host:26257/cybermesh?sslmode=verify-full
   ```

3. Check TLS certificates (if using verify-full):
   ```bash
   ls -lh certs/root.crt
   # Should exist and be readable
   ```

4. Test without TLS (development only):
   ```bash
   DB_TLS=false
   DB_DSN=postgresql://user:pass@host:26257/cybermesh?sslmode=disable
   ```

**Common Fixes:**
- Verify CockroachDB is running: `systemctl status cockroachdb`
- Check firewall rules (allow port 26257)
- Verify TLS certificates are valid (not expired)
- Increase connection timeout: `CONNECT_TIMEOUT=30` in DSN

### Memory Leak

**Symptom:** Memory usage grows over time, process OOM killed

**Common Causes:**
- Mempool not cleaning up expired nonces
- P2P peer connections accumulating
- State store versions not garbage collected

**Debug Steps:**

1. Check mempool size:
   ```bash
   grep "mempool_size" logs/*.log
   # Should not exceed MEMPOOL_MAX_TXS (1000)
   ```

2. Monitor memory usage:
   ```bash
   ps aux | grep cybermesh
   # Check RSS (resident set size)
   ```

3. Profile memory:
   ```bash
   go tool pprof http://localhost:6060/debug/pprof/heap
   # (requires pprof endpoint enabled)
   ```

**Common Fixes:**
- Reduce `MEMPOOL_NONCE_TTL` (default 15 minutes)
- Reduce `P2P_LIVENESS_TIMEOUT` (default 20 seconds)
- Restart service daily (systemd timer)
- Upgrade to 8GB RAM (if currently 4GB)

---

## Known Issues

### Genesis Ceremony Clock Skew

**Status:** Mitigated with GENESIS_CLOCK_SKEW_TOLERANCE

**Symptom:** Genesis ceremony fails with "timestamp skew exceeded" in some multi-region deployments

**Workaround:** Increase `GENESIS_CLOCK_SKEW_TOLERANCE=30m` for geographically distributed clusters

**Long-term Fix:** Implement NTP synchronization requirement in startup validation

---

## Project Status

**Version:** 1.0.0

**Go Version:** 1.24.0

**Production Readiness:** 100%

**Components:**
- ✅ PBFT Consensus (3-phase commit)
- ✅ Leader Election (heartbeat-based rotation)
- ✅ Genesis Ceremony (cluster initialization)
- ✅ Deterministic State Machine (pure functions)
- ✅ Ed25519 Verification (Kafka messages)
- ✅ CockroachDB Persistence (TLS + connection pooling)
- ✅ P2P Networking (libp2p + mDNS discovery)
- ✅ Audit Logging (HMAC-signed)

**Dependencies (via go.mod):**
- IBM sarama (Kafka client)
- libp2p (P2P networking)
- Zap (structured logging)
- pgx (PostgreSQL driver)
- Protobuf (message serialization)

---

## Additional Resources

**Code Documentation:**
- Package documentation: `go doc backend/pkg/<package>`
- Function documentation: `go doc backend/pkg/<package>.<function>`

**External Links:**
- CockroachDB Documentation: https://www.cockroachlabs.com/docs/
- libp2p Documentation: https://docs.libp2p.io/
- PBFT Paper: http://pmg.csail.mit.edu/papers/osdi99.pdf
- Ed25519 Cryptography: https://ed25519.cr.yp.to/

**Integration:**
- AI Service README: `../ai-service/README.md`
- Protobuf Specifications: `proto/`

---

**Last Updated:** 2025-10-22  
**Maintainers:** CyberMesh Backend Team  
**License:** (Specify license if applicable)
