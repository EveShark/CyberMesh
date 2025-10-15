# CyberMesh Backend API Layer - Product Requirements Document (PRD)

**Version:** 1.0  
**Date:** 2025-01-10  
**Status:** Draft  
**Owner:** Backend Engineering Team  

---

## 1. Executive Summary

This document specifies the requirements for the **Read-Only API Layer** of the CyberMesh Backend, a distributed cybersecurity platform backend implementing BFT consensus, deterministic state execution, and durable storage.

The API layer provides secure, read-only access to:
- System health and operational status
- Block data and transaction history
- Application state queries
- Validator information
- Prometheus-compatible metrics

**Key Design Principles:**
- **Security-First:** mTLS authentication, RBAC authorization, comprehensive audit logging
- **Read-Only:** No state modification endpoints (mutations occur only via consensus)
- **Performance:** Efficient queries with caching where appropriate
- **Observability:** Detailed metrics and audit trails for all API access

---

## 2. Background & Context

### 2.1 System Architecture Context

```
┌─────────────────────────────────────────────────────────────┐
│                    External Clients                          │
│              (Ops Dashboard, Monitoring, etc.)               │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼ (mTLS, RBAC)
┌─────────────────────────────────────────────────────────────┐
│                      API Layer (THIS PRD)                    │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────┐  │
│  │ Health   │ Metrics  │ Blocks   │ State    │Validators │  │
│  │ /health  │ /metrics │ /blocks  │ /state   │/validators│  │
│  └──────────┴──────────┴──────────┴──────────┴───────────┘  │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼ (Internal Access)
┌─────────────────────────────────────────────────────────────┐
│                    Backend Core Layers                       │
│  ┌─────────┬──────────┬────────┬──────────┬─────────────┐   │
│  │ Wiring  │ Storage  │ State  │ Mempool  │ Consensus   │   │
│  │ Service │ Adapter  │ Store  │          │ Engine      │   │
│  └─────────┴──────────┴────────┴──────────┴─────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 Current State

**Completed Components:**
- ✅ State execution layer (pkg/state)
- ✅ Storage layer (pkg/storage/cockroach)
- ✅ Consensus layer (pkg/consensus)
- ✅ Mempool (pkg/mempool)
- ✅ Wiring/orchestration (pkg/wiring)

**Missing Component:**
- ❌ API layer (pkg/api) - **THIS PRD**

### 2.3 Integration Points

The API layer will integrate with:
1. **pkg/wiring/service.go** - Main orchestration service
2. **pkg/storage/cockroach** - Database queries (blocks, transactions)
3. **pkg/state** - State store queries
4. **pkg/consensus/api** - Validator information
5. **pkg/mempool** - Mempool statistics
6. **pkg/utils** - Logger, AuditLogger, ConfigManager

---

## 3. Stakeholders

| Role | Responsibility | Needs |
|------|---------------|-------|
| **DevOps/SRE** | Monitor system health, diagnose issues | Health checks, metrics, logs |
| **Security Team** | Audit access, verify compliance | Audit logs, RBAC, mTLS |
| **Operations Dashboard** | Display system status, query data | All read endpoints |
| **Backend Engineers** | Debug issues, verify behavior | Query APIs, metrics |
| **QA/Testing** | Verify system correctness | All endpoints, deterministic responses |

---

## 4. Functional Requirements

### 4.1 Health & Readiness Endpoints

#### 4.1.1 Health Check
- **Endpoint:** `GET /health`
- **Purpose:** Basic liveness probe
- **Auth:** None (public for load balancers)
- **Response Time:** < 10ms
- **Response:**
  ```json
  {
    "status": "healthy",
    "timestamp": 1704931200,
    "version": "1.0.0"
  }
  ```

#### 4.1.2 Readiness Check
- **Endpoint:** `GET /ready`
- **Purpose:** Readiness probe (can serve traffic?)
- **Auth:** None (public for load balancers)
- **Response Time:** < 100ms
- **Checks:**
  - ✅ Database connection alive
  - ✅ Consensus engine running
  - ✅ State store accessible
  - ✅ Mempool operational
  - Implementation notes: DB check via Cockroach adapter ping; consensus via engine status (running + height/view); state via StateStore.Latest()/Root(); mempool via Stats().
- **Response:**
  ```json
  {
    "ready": true,
    "checks": {
      "database": "ok",
      "consensus": "ok",
      "state": "ok",
      "mempool": "ok"
    },
    "timestamp": 1704931200
  }
  ```

### 4.2 Metrics Endpoint

#### 4.2.1 Prometheus Metrics
- **Endpoint:** `GET /metrics`
- **Purpose:** Prometheus scraping
- **Auth:** mTLS (optional: bearer token)
- **Format:** Prometheus text format
  - If Prometheus client is not linked, expose a minimal JSON fallback at /metrics (same path, negotiated via Accept header) until dependency is added.
- **Metrics to Expose:**
  ```
  # Consensus
  consensus_height
  consensus_round
  consensus_view
  consensus_proposals_total
  consensus_commits_total
  consensus_timeouts_total
  
  # Mempool
  mempool_size_bytes
  mempool_tx_count
  mempool_admitted_total
  mempool_rejected_total
  
  # State
  state_version
  state_root
  state_apply_duration_seconds
  
  # Storage
  storage_persist_duration_seconds
  storage_persist_errors_total
  storage_integrity_violations_total
  
  # API
  api_requests_total{endpoint, method, status}
  api_request_duration_seconds{endpoint}
  api_errors_total{endpoint, error_type}
  ```

### 4.3 Block Query Endpoints

#### 4.3.1 Get Block by Height
- **Endpoint:** `GET /blocks/:height`
- **Auth:** mTLS + RBAC (role: `block_reader`)
- **Rate Limit:** 100 req/min per client
- **Parameters:**
  - `height` (path, required): Block height (uint64)
  - `include_txs` (query, optional): Include transaction details (bool, default: false)
- **Response:**
  ```json
  {
    "height": 42,
    "hash": "a1b2c3...",
    "parent_hash": "d4e5f6...",
    "state_root": "789abc...",
    "timestamp": 1704931200,
    "proposer": "validator_1",
    "transaction_count": 15,
    "transactions": [  // if include_txs=true
      {
        "hash": "tx1_hash",
        "type": "event",
        "size": 256
      }
    ]
  }
  - Notes:
    - Transaction payloads are never returned; only hash/type/size metadata.
    - Returning full transaction lists requires an adapter method to list transactions by block height; until implemented, `include_txs=true` may be gated or limited to summaries.
  ```

#### 4.3.2 Get Latest Block
- **Endpoint:** `GET /blocks/latest`
- **Auth:** mTLS + RBAC (role: `block_reader`)
- **Rate Limit:** 100 req/min per client
- **Response:** Same as 4.3.1

#### 4.3.3 List Blocks (Range Query)
- **Endpoint:** `GET /blocks?start=:start&limit=:limit`
- **Auth:** mTLS + RBAC (role: `block_reader`)
- **Rate Limit:** 50 req/min per client
- **Parameters:**
  - `start` (query, required): Starting height
  - `limit` (query, optional): Max blocks to return (default: 10, max: 100)
- **Response:**
  ```json
  {
    "blocks": [/* array of blocks */],
    "start": 100,
    "count": 10,
    "total": 500
  }
  - Notes: Responses must not include raw payloads; hashes and metadata only. Enforce `limit` ≤ 100.
  ```

### 4.4 State Query Endpoints

#### 4.4.1 Get State by Key
- **Endpoint:** `GET /state/:key`
- **Auth:** mTLS + RBAC (role: `state_reader`)
- **Rate Limit:** 200 req/min per client
- **Parameters:**
  - `key` (path, required): State key (hex-encoded)
  - `version` (query, optional): State version (default: latest)
- **Response:**
  ```json
  {
    "key": "0x1234abcd",
    "value": "0x5678ef01",
    "version": 42,
    "proof": "0xmerkle_proof..."  // optional: merkle proof
  }
  ```

#### 4.4.2 Get Current State Root
- **Endpoint:** `GET /state/root`
- **Auth:** mTLS + RBAC (role: `state_reader`)
- **Rate Limit:** 1000 req/min per client
- **Response:**
  ```json
  {
    "root": "0x789abc...",
    "version": 42,
    "height": 42
  }
  ```

### 4.5 Validator Endpoints

#### 4.5.1 List Validators
- **Endpoint:** `GET /validators`
- **Auth:** mTLS + RBAC (role: `validator_reader`)
- **Rate Limit:** 100 req/min per client
- **Response:**
  ```json
  {
    "validators": [
      {
        "id": "validator_1",
        "public_key": "0xabcd...",
        "voting_power": 100,
        "status": "active"
      }
    ],
    "total": 10
  }
  ```

#### 4.5.2 Get Validator Details
- **Endpoint:** `GET /validators/:id`
- **Auth:** mTLS + RBAC (role: `validator_reader`)
- **Rate Limit:** 100 req/min per client
- **Response:**
  ```json
  {
    "id": "validator_1",
    "public_key": "0xabcd...",
    "voting_power": 100,
    "status": "active",
    "proposed_blocks": 42,
    "uptime_percentage": 99.9
  }
  ```

### 4.6 Statistics Endpoints

#### 4.6.1 System Statistics
- **Endpoint:** `GET /stats`
- **Auth:** mTLS + RBAC (role: `stats_reader`)
- **Rate Limit:** 10 req/min per client
- **Response:**
  ```json
  {
    "chain": {
      "height": 1000,
      "state_version": 1000,
      "total_transactions": 15000
    },
    "consensus": {
      "view": 5,
      "round": 3,
      "validator_count": 10
    },
    "mempool": {
      "pending_transactions": 42,
      "size_bytes": 1048576
    }
  }
  ```

---

## 5. Non-Functional Requirements

### 5.1 Security

#### 5.1.1 Authentication
- **Requirement:** Mutual TLS (mTLS) authentication for all endpoints except `/health`
- **Implementation:**
  - Server presents certificate signed by trusted CA
  - Client presents certificate signed by trusted CA
  - Certificate validation on both sides
  - Support for certificate revocation lists (CRL)

#### 5.1.2 Authorization
- **Requirement:** Role-Based Access Control (RBAC)
- **Roles:**
  ```
  - admin: Full read access to all endpoints
  - block_reader: Access to /blocks/* endpoints
  - state_reader: Access to /state/* endpoints
  - validator_reader: Access to /validators/* endpoints
  - stats_reader: Access to /stats endpoint
  - metrics_reader: Access to /metrics endpoint
  ```
- **Implementation:**
  - Roles embedded in client certificate (CN or SAN)
  - Middleware validates role before handler execution
  - Deny by default (fail-closed)

#### 5.1.3 Rate Limiting
- **Requirement:** Per-client rate limiting to prevent DoS
- **Implementation:**
  - Token bucket algorithm
  - Limits per endpoint (see section 4)
  - Identified by client certificate fingerprint
  - 429 Too Many Requests on limit exceeded
  - Include `Retry-After` header

#### 5.1.4 Audit Logging
- **Requirement:** Log all API access for security audit
- **Log Fields:**
  - Timestamp
  - Client certificate fingerprint
  - Client role
  - Endpoint accessed
  - Method (GET, POST, etc.)
  - Response status code
  - Response time
  - Error message (if applicable)
  - NO sensitive data (keys, values, payloads)
  - Do not log raw transaction payloads or state values; only hashes, sizes, and metadata.

#### 5.1.5 Input Validation
- **Requirement:** Strict input validation on all parameters
- **Validation Rules:**
  - Block height: uint64, 0 ≤ height ≤ current_height
  - State key: hex-encoded, max 128 bytes
  - Limit: int, 1 ≤ limit ≤ 100
  - All string parameters: sanitize, no SQL injection vectors

### 5.2 Performance

#### 5.2.1 Response Times (95th percentile)
- `/health`: < 10ms
- `/ready`: < 100ms
- `/metrics`: < 500ms
- `/blocks/:height`: < 200ms (without txs), < 1s (with txs)
- `/state/:key`: < 100ms
- `/validators`: < 200ms

#### 5.2.2 Throughput
- Support 1000 req/s aggregate across all endpoints
- Support 100 concurrent connections

#### 5.2.3 Resource Usage
- Memory: < 100MB additional (beyond backend core)
- CPU: < 5% on idle, < 20% under load

### 5.3 Reliability

#### 5.3.1 Availability
- **Target:** 99.9% uptime
- **Graceful Degradation:**
  - If DB unavailable: `/health` still responds, `/ready` returns not ready
  - If consensus stopped: API still serves historical data

#### 5.3.2 Error Handling
- **Requirement:** Consistent error responses
- **Error Format:**
  ```json
  {
    "error": {
      "code": "BLOCK_NOT_FOUND",
      "message": "Block at height 9999 does not exist",
      "details": {
        "current_height": 1000
      }
    }
  }
  ```
- **Error Codes:**
  - `INVALID_REQUEST` - Bad input parameters
  - `UNAUTHORIZED` - Authentication failed
  - `FORBIDDEN` - Authorization failed (insufficient role)
  - `NOT_FOUND` - Resource not found
  - `RATE_LIMIT_EXCEEDED` - Too many requests
  - `INTERNAL_ERROR` - Server error
  - `SERVICE_UNAVAILABLE` - Component unavailable
  
  - **Error Code Mapping (implementation guidance):**
    - storage.ErrBlockNotFound → 404 NOT_FOUND (code: BLOCK_NOT_FOUND)
    - storage.ErrTransactionNotFound → 404 NOT_FOUND (code: TX_NOT_FOUND)
    - storage.ErrSnapshotNotFound → 404 NOT_FOUND (code: SNAPSHOT_NOT_FOUND)
    - validation errors (height/key/limit) → 400 INVALID_REQUEST
    - auth failures (mTLS) → 401 UNAUTHORIZED; RBAC denials → 403 FORBIDDEN
    - backend component unavailable (DB/engine) → 503 SERVICE_UNAVAILABLE
    - unhandled errors → 500 INTERNAL_ERROR

#### 5.3.3 Graceful Shutdown
- **Requirement:** Drain in-flight requests on shutdown
- **Implementation:**
  - Stop accepting new connections
  - Wait for in-flight requests (max 30s)
  - Force close remaining connections
  - Log shutdown metrics

### 5.4 Observability

#### 5.4.1 Logging
- **Requirement:** Structured logging with context
- **Log Levels:**
  - ERROR: Authentication failures, authorization denials, internal errors
  - WARN: Rate limit exceeded, deprecated endpoint usage
  - INFO: API server start/stop, configuration changes
  - DEBUG: Request/response details (dev only, never in production)

#### 5.4.2 Metrics
- **Requirement:** Prometheus-compatible metrics (see 4.2.1)
- **Additional API Metrics:**
  - Request count by endpoint/status
  - Request duration histogram
  - Active connections gauge
  - Rate limit hits counter

#### 5.4.3 Tracing (Future)
- **Optional:** OpenTelemetry distributed tracing
- **Trace Context:** Propagate trace ID through backend layers

---

## 6. API Design Specifications

### 6.1 REST Conventions

- **Base Path:** `/api/v1` (versioned API)
- **HTTP Methods:** GET only (read-only API)
- **Content-Type:** `application/json`
- **Character Encoding:** UTF-8
- **Date Format:** Unix timestamp (seconds since epoch)
- **Binary Data:** Hex-encoded with `0x` prefix

### 6.2 Common Response Headers

```
Content-Type: application/json
X-Request-ID: <uuid>
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1704931800
```

### 6.3 HTTP Status Codes

| Code | Meaning | Usage |
|------|---------|-------|
| 200 | OK | Successful request |
| 400 | Bad Request | Invalid parameters |
| 401 | Unauthorized | Authentication failed |
| 403 | Forbidden | Insufficient permissions |
| 404 | Not Found | Resource doesn't exist |
| 429 | Too Many Requests | Rate limit exceeded |
| 500 | Internal Server Error | Server error |
| 503 | Service Unavailable | Component unavailable |

### 6.4 Pagination

For endpoints returning collections:
```
GET /blocks?start=100&limit=10

Response:
{
  "data": [...],
  "pagination": {
    "start": 100,
    "limit": 10,
    "total": 1000,
    "next": "/blocks?start=110&limit=10"
  }
}
```

### 6.5 Filtering & Sorting

- **Filtering:** Query parameters (e.g., `?status=active`)
- **Sorting:** `?sort=height&order=desc`
- **Default:** Most recent first

---

## 7. Architecture & Design

### 7.1 Directory Structure

```
pkg/api/
  ├── server.go           # HTTP server setup, lifecycle
  ├── router.go           # Route registration, middleware chain
  ├── middleware.go       # Logging, CORS, panic recovery, request ID
  ├── auth.go             # mTLS verification, RBAC middleware
  ├── ratelimit.go        # Rate limiting middleware
  ├── dto.go              # Data Transfer Objects (request/response structs)
  ├── errors.go           # Error types, error response builder
  ├── health.go           # Health & readiness handlers
  ├── metrics.go          # Metrics handler, Prometheus registry
  ├── blocks.go           # Block query handlers
  ├── state.go            # State query handlers
  ├── validators.go       # Validator query handlers
  ├── stats.go            # Statistics handler
  └── util.go             # Helper functions (parsing, validation)
```

### 7.2 Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                         api.Server                           │
│                                                               │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                  Middleware Chain                    │    │
│  │  ┌──────────┬──────────┬──────────┬──────────────┐  │    │
│  │  │ Request  │  Auth    │  RBAC    │ Rate Limit   │  │    │
│  │  │   ID     │  (mTLS)  │          │              │  │    │
│  │  └──────────┴──────────┴──────────┴──────────────┘  │    │
│  └─────────────────────────────────────────────────────┘    │
│                           │                                   │
│                           ▼                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │                     Handlers                         │    │
│  │  ┌──────┬─────────┬────────┬───────┬────────────┐  │    │
│  │  │Health│ Metrics │ Blocks │ State │ Validators │  │    │
│  │  └──────┴─────────┴────────┴───────┴────────────┘  │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                 Backend Core (Wiring Service)                │
│  ┌──────────┬──────────┬────────┬──────────────────────┐    │
│  │ Storage  │  State   │Mempool │  Consensus Engine    │    │
│  │ Adapter  │  Store   │        │                      │    │
│  └──────────┴──────────┴────────┴──────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

### 7.3 Key Design Patterns

#### 7.3.1 Dependency Injection
```go
type Server struct {
    storage   storage.Adapter
    stateStore state.StateStore
    mempool   *mempool.Mempool
    engine    *consensus.Engine
    logger    *utils.Logger
    audit     *utils.AuditLogger
    metrics   *Metrics
}

func NewServer(deps Dependencies) *Server {
    // Inject all dependencies
}
```

#### 7.3.2 Middleware Pattern
```go
type Middleware func(http.Handler) http.Handler

func ChainMiddleware(h http.Handler, mw ...Middleware) http.Handler {
    for i := len(mw) - 1; i >= 0; i-- {
        h = mw[i](h)
    }
    return h
}
```

#### 7.3.3 Context Propagation
```go
type contextKey string

const (
    contextKeyClientID   contextKey = "client_id"
    contextKeyRole       contextKey = "client_role"
    contextKeyRequestID  contextKey = "request_id"
)

// Middleware adds to context, handlers read from context
```

### 7.4 Configuration

```go
type APIConfig struct {
    // Server
    ListenAddr      string        // :8443
    TLSEnabled      bool          // true (required in prod)
    TLSCertFile     string        // /etc/tls/server.crt
    TLSKeyFile      string        // /etc/tls/server.key
    TLSClientCAFile string        // /etc/tls/ca.crt (for mTLS)
    
    // Timeouts
    ReadTimeout     time.Duration // 10s
    WriteTimeout    time.Duration // 30s
    IdleTimeout     time.Duration // 60s
    ShutdownTimeout time.Duration // 30s
    
    // Rate Limiting
    RateLimitEnabled    bool // true
    RateLimitPerMinute  int  // 100
    
    // RBAC
    RBACEnabled bool // true
    
    // Observability
    EnableMetrics bool // true
    EnableAudit   bool // true
}
```

Environment variables:
```
API_LISTEN_ADDR=:8443
API_TLS_ENABLED=true
API_TLS_CERT_FILE=/etc/tls/server.crt
API_TLS_KEY_FILE=/etc/tls/server.key
API_TLS_CLIENT_CA_FILE=/etc/tls/ca.crt
API_RATE_LIMIT_PER_MINUTE=100
API_RBAC_ENABLED=true
```

---

## 8. Implementation Plan

### Phase 1: Foundation (2-3 hours)
1. Create `pkg/api/server.go` - HTTP server with graceful shutdown
2. Create `pkg/api/router.go` - Route registration
3. Create `pkg/api/middleware.go` - Basic middleware (logging, panic recovery, request ID)
4. Create `pkg/api/dto.go` - Response/request structs
5. Create `pkg/api/errors.go` - Error handling

### Phase 2: Core Endpoints (3-4 hours)
6. Create `pkg/api/health.go` - Health & readiness handlers
7. Create `pkg/api/metrics.go` - Prometheus metrics
8. Create `pkg/api/blocks.go` - Block query handlers
9. Create `pkg/api/state.go` - State query handlers
10. Create `pkg/api/validators.go` - Validator handlers
11. Create `pkg/api/stats.go` - Statistics handler

### Phase 3: Security (3-4 hours)
12. Create `pkg/api/auth.go` - mTLS verification, RBAC
13. Create `pkg/api/ratelimit.go` - Rate limiting middleware
14. Add audit logging to all endpoints
15. Add input validation

### Phase 4: Integration & Testing (2-3 hours)
16. Wire API into `pkg/wiring/service.go`
17. Add API lifecycle to `Service.Start()` / `Service.Stop()`
18. Integration testing with curl/Postman
19. Load testing (verify performance requirements)
20. Security testing (mTLS, RBAC, rate limits)

**Total Estimated Time:** 10-14 hours

---

## 9. Testing Requirements

### 9.1 Unit Tests
- **Coverage Target:** 80%
- **Test Files:** `*_test.go` for each handler
- **Mock Dependencies:** Use interfaces for storage, state, mempool
- **Test Cases:**
  - Happy path (valid request → valid response)
  - Error cases (404, 400, 500)
  - Edge cases (empty results, max limits)

### 9.2 Integration Tests
- **Setup:** Full backend stack with in-memory/test DB
- **Test Scenarios:**
  - Query block that exists
  - Query block that doesn't exist
  - Query state at specific version
  - List validators
  - Metrics endpoint returns valid Prometheus format

### 9.3 Security Tests
- **mTLS:**
  - Valid client cert → 200 OK
  - Invalid client cert → 401 Unauthorized
  - No client cert → 401 Unauthorized
- **RBAC:**
  - Client with `block_reader` role can access `/blocks`
  - Client without `block_reader` role gets 403 Forbidden
- **Rate Limiting:**
  - 101st request in 1 minute → 429 Too Many Requests
  - Rate limit resets after time window

### 9.4 Performance Tests
- **Load Test:** 1000 req/s for 5 minutes
- **Latency Test:** p95 response time < requirements
- **Concurrent Connections:** 100 clients simultaneously

---

## 10. Acceptance Criteria

### 10.1 Functional Acceptance
- [ ] All endpoints return correct responses for valid inputs
- [ ] All endpoints return appropriate errors for invalid inputs
- [ ] Pagination works correctly
- [ ] Metrics endpoint returns valid Prometheus format
- [ ] Health/readiness checks accurately reflect system state

### 10.2 Security Acceptance
- [ ] mTLS authentication enforced (except `/health`)
- [ ] RBAC correctly denies unauthorized access
- [ ] Rate limiting prevents DoS (429 after limit)
- [ ] Audit logs capture all API access
- [ ] No secrets/payloads in logs

### 10.3 Performance Acceptance
- [ ] p95 response times meet requirements
- [ ] System handles 1000 req/s sustained load
- [ ] Memory usage < 100MB additional
- [ ] CPU usage < 20% under load

### 10.4 Integration Acceptance
- [ ] API server starts/stops with wiring service
- [ ] Graceful shutdown drains connections
- [ ] API queries return data from actual backend components
- [ ] Metrics reflect actual backend state

---

## 11. Out of Scope

The following are explicitly **NOT** included in this API layer:

### 11.1 Write Operations
- No transaction submission endpoints (use Kafka ingestion path)
- No configuration modification endpoints
- No validator management endpoints
- No admin control plane (future PRD)

### 11.2 Advanced Features
- GraphQL API (future consideration)
- WebSocket/SSE streaming (future consideration)
- API versioning beyond v1 (future)
- OpenAPI/Swagger documentation (future, nice-to-have)

### 11.3 External Integrations
- No integration with external monitoring systems (Datadog, New Relic)
- No integration with external log aggregation (Splunk, ELK)
- No integration with service mesh (Istio, Linkerd)

---

## 12. Success Metrics

### 12.1 Development Metrics
- [ ] Implementation completed within 14 hours
- [ ] All acceptance criteria met
- [ ] 80%+ test coverage
- [ ] Zero critical security vulnerabilities

### 12.2 Operational Metrics (Post-Deployment)
- API uptime > 99.9%
- p95 latency < requirements
- Zero authentication bypasses
- Zero authorization bypasses
- Audit log completeness 100%

---

## 13. Dependencies

### 13.1 Internal Dependencies
- `pkg/wiring` - Service orchestration
- `pkg/storage/cockroach` - Block/transaction queries
- `pkg/state` - State queries
- `pkg/consensus/api` - Validator information
- `pkg/mempool` - Mempool statistics
- `pkg/utils` - Logger, AuditLogger, ConfigManager

### 13.2 External Dependencies
- Go 1.23+
- `github.com/prometheus/client_golang` - Prometheus metrics
- Standard library `net/http` - HTTP server
- Standard library `crypto/tls` - mTLS

### 13.3 Infrastructure Dependencies
- TLS certificates (server + client CA)
- Load balancer (for production deployment)
- Prometheus server (for metrics scraping)

---

## 14. Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| mTLS misconfiguration | High | Medium | Comprehensive testing, fail-closed by default |
| Rate limiting bypass | Medium | Low | Multiple layers (client cert + IP) |
| Query performance degradation | Medium | Medium | Add caching layer if needed, query optimization |
| Audit log volume | Low | High | Log rotation, sampling for high-volume endpoints |

---

## 15. Open Questions

1. **Rate Limiting Strategy:** Per-client cert or per-IP? Or both?
   - **Recommendation:** Per-client cert (more secure, authenticated)

2. **Caching:** Should we cache block/state queries?
   - **Recommendation:** Not in v1 (adds complexity), add if performance requires

3. **API Versioning:** Use path (`/api/v1`) or header (`Accept: application/vnd.cybermesh.v1+json`)?
   - **Recommendation:** Path-based (simpler, more visible)

4. **Pagination Default Limit:** 10, 25, or 50?
   - **Recommendation:** 10 (conservative, can increase)

---

## 16. Appendix

### 16.1 Example Client (curl)

```bash
# Health check (no auth)
curl https://api.cybermesh.local:8443/api/v1/health

# Get block with mTLS
curl --cert client.crt --key client.key --cacert ca.crt \
  https://api.cybermesh.local:8443/api/v1/blocks/42

# Get metrics
curl --cert metrics-client.crt --key metrics-client.key --cacert ca.crt \
  https://api.cybermesh.local:8443/api/v1/metrics
```

### 16.2 Example Response (Block)

```json
{
  "height": 42,
  "hash": "0xa1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456",
  "parent_hash": "0xd4e5f678901234567890abcdef1234567890abcdef1234567890abcdef123456",
  "state_root": "0x789abc012345678901234567890abcdef1234567890abcdef1234567890ab",
  "timestamp": 1704931200,
  "proposer": "validator_1",
  "transaction_count": 15,
  "size_bytes": 4096
}
```

### 16.3 Example Error Response

```json
{
  "error": {
    "code": "BLOCK_NOT_FOUND",
    "message": "Block at height 9999 does not exist",
    "details": {
      "requested_height": 9999,
      "current_height": 1000
    },
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

---

## 17. Revision History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2025-01-10 | Backend Team | Initial PRD |

---

## 18. Approval

| Role | Name | Signature | Date |
|------|------|-----------|------|
| Tech Lead | ___________ | ___________ | _____ |
| Security Lead | ___________ | ___________ | _____ |
| Product Owner | ___________ | ___________ | _____ |

---

**END OF PRD**
