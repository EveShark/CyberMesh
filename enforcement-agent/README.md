# CyberMesh Enforcement Agent

Consumes signed policy events from Kafka, validates them (signature + hash), converts `rule_data` into an actionable `PolicySpec`, applies enforcement via a backend (iptables/nftables/Kubernetes), persists local state for reconciliation/expiry, and optionally publishes ACKs back to Kafka.

## Low-Level Architecture (LLA)

```text
                           +------------------------------+
                           |  Trusted Keys (PEM files)    |
                           |  CONTROL_POLICY_TRUSTED_KEYS |
                           +---------------+--------------+
                                           |
                                           v
 +-------------------+     +------------------------------+      +----------------------+
 | Kafka             |     | kafka.Consumer (sarama)      |      | controller.Controller |
 | control.policy.v1 +---->| internal/kafka/consumer.go   +----->| internal/controller   |
 +-------------------+     | consumer group + lag metrics |      | HandleMessage()       |
                           +------------------------------+      +----------+-----------+
                                                                              |
                                                                              | Verify + parse
                                                                              v
                                                                +--------------------------+
                                                                | policy.TrustedKeys       |
                                                                | internal/policy/event.go |
                                                                | - ed25519 verify         |
                                                                | - sha256 rule_hash check |
                                                                +------------+-------------+
                                                                             |
                                                                             | JSON -> PolicySpec
                                                                             v
                                                                +--------------------------+
                                                                | policy.ParseSpec         |
                                                                | internal/policy/parser.go|
                                                                | (currently: rule_type=   |
                                                                |  "block")                |
                                                                +------------+-------------+
                                                                             |
                                                                             | Guardrails + approval + dedupe
                                                                             v
                                                                +--------------------------+
                                                                | Guardrails               |
                                                                | internal/controller       |
                                                                | - allowlist checks        |
                                                                | - cooldown / max active   |
                                                                | - rate limits (local/redis|
                                                                | - manual approval staging |
                                                                +------------+-------------+
                                                                             |
                                                                             | Apply policy
                                                                             v
                                                         +-------------------+-------------------+
                                                         | enforcer.Enforcer (Factory)           |
                                                         | internal/enforcer/enforcer.go         |
                                                         |  - iptables (linux)                   |
                                                         |  - nftables (linux)                   |
                                                         |  - kubernetes NetworkPolicy           |
                                                         |  - noop                               |
                                                         +-------------------+-------------------+
                                                                             |
                                                                             | Persist for reconcile/expiry
                                                                             v
                                                                +--------------------------+
                                                                | state.Store              |
                                                                | internal/state/store.go  |
                                                                | - snapshot JSON file     |
                                                                | - TTL/pre-consensus/     |
                                                                |   rollback deadlines     |
                                                                +------------+-------------+
                                                                             |
                                                                             | Background loops
                                                                             v
                 +----------------------------+                 +----------------------------+
                 | scheduler.Scheduler        |                 | reconciler.Reconciler      |
                 | internal/scheduler         |                 | internal/reconciler        |
                 | - expire TTL deadlines     |                 | - periodic re-apply        |
                 | - remove from backend      |                 | - optional ledger snapshot |
                 +----------------------------+                 +----------------------------+

  Optional ACK pipeline (enabled via ACK_ENABLED=true):

    controller.emitAck()
          |
          v
    +-------------------+   +-------------------+   +------------------+   +-------------------+
    | ack.Batching      |-->| ack.Retrying      |-->| ack.BoltQueue     |-->| ack.KafkaPublisher|
    | internal/ack      |   | internal/ack      |   | (bbolt on disk)   |   | -> Kafka topic     |
    | (optional)        |   | queue+worker      |   | ACK_QUEUE_PATH    |   | control.policy.ack |
    +-------------------+   +-------------------+   +------------------+   +-------------------+

  HTTP server (always on, METRICS_ADDR):
    GET /metrics
    GET /healthz, GET /readyz
    GET/POST /control/kill-switch?enabled=true|false
```

## Repo Layout (What We Use)

Note: This README intentionally excludes `bin/` and `test/` (per project usage), and does not document `build.ps1` / `build.sh`.

### Entrypoint

- `cmd/agent/main.go`
  - Loads env config (`internal/config`)
  - Loads trusted keys (`internal/policy`)
  - Creates metrics registry (`internal/metrics`)
  - Chooses enforcement backend (`internal/enforcer`)
  - Opens state store (`internal/state`)
  - Starts:
    - Kafka consumer loop (`internal/kafka`)
    - Scheduler expiry loop (`internal/scheduler`)
    - Reconciler loop + optional ledger sync (`internal/reconciler`, `internal/ledger`)
    - HTTP server (`/metrics`, `/healthz`, `/readyz`, `/control/kill-switch`)
  - Optionally enables ACK publish pipeline (`internal/ack`)

### Core Runtime Packages

- `internal/config/config.go`
  - Env-driven runtime configuration (Kafka, backend selection, state, ACK, rate limiting, fast-path, metrics).
- `internal/kafka/consumer.go`
  - Sarama consumer-group wrapper with TLS/SASL support and lag/error metrics hooks.
- `internal/controller/controller.go`
  - Main decision engine:
    - Unmarshal protobuf event
    - Verify signer + hash and parse `PolicySpec`
    - Dedupe on `(policy_id + scope)` using `rule_hash`
    - Manual approval staging (`guardrails.approval_required`)
    - Guardrails checks (allowlists, cooldowns, max active, rate limits)
    - Apply via enforcer + persist to state store
    - Emit ACKs (optional)
- `internal/controller/fastpath.go`
  - Fast-path eligibility computation (pre-consensus enforcement gating).
- `internal/policy/spec.go`
  - `PolicySpec` domain model used by enforcers/state/reconciler.
- `internal/policy/parser.go`
  - Converts `rule_data` JSON into a validated `PolicySpec`.
  - Currently supports `rule_type == "block"`.
- `internal/policy/event.go`
  - Loads trusted Ed25519 public keys from `.pem` files.
  - Verifies signatures + validates `rule_hash` against `rule_data`.
- `internal/enforcer/enforcer.go`
  - Backend factory + common `Enforcer` interface.
  - Backends:
    - `internal/enforcer/iptables/*` (linux build)
    - `internal/enforcer/nftables/*` (linux build)
    - `internal/enforcer/kubernetes/kubernetes.go` (NetworkPolicy)
    - `noop` backend for testing/dev.
- `internal/enforcer/common/scope.go`
  - Validates scope metadata before applying in a backend.
- `internal/state/store.go`
  - Persists applied policies + timing metadata to disk (JSON snapshot + lock file).
  - Tracks rate-limit history across scopes + pending approvals.
- `internal/scheduler/scheduler.go`
  - Expires policies when TTL / pre-consensus TTL / rollback deadlines elapse.
- `internal/reconciler/reconciler.go`
  - Periodically re-applies stored policies, and optionally reconciles against a ledger snapshot.
- `internal/ledger/file_provider.go`
  - Optional ledger snapshot provider that reads a JSON file of `PolicySpec[]`.
- `internal/ratelimit/*`
  - Local in-process rate limiting (`local.go`) and Redis-coordinated limiting (`redis.go` + `redis_adapter.go`).
- `internal/control/killswitch.go`
  - Runtime kill switch toggle; used by scheduler/reconciler/controller and exposed via HTTP.
- `internal/metrics/metrics.go`
  - Prometheus metrics recorder + handler for `/metrics`.
- `internal/metrics/ack_queue.go`
  - Specialized ACK queue metrics helper (not required if using `metrics.Recorder`, but kept for compatibility).
- `internal/ack/*`
  - Optional ACK pipeline:
    - `publisher.go`: publish `PolicyAckEvent` to Kafka (optional header signing)
    - `queue.go`: durable bbolt queue
    - `retrier.go`: worker that drains queue to publisher with retry
    - `batching.go`: optional batching wrapper
    - `signing.go`: Ed25519 signer for ACK payloads

### Protobuf (Schema + Generated Code)

- `proto/control_policy.proto`
  - Protobuf schema for inbound policy events.
  - `go_package` is `backend/proto;pb` and this repo's Go code imports `backend/proto` (see `go.mod`).
- `proto/control_policy.pb.go`
  - Generated code for `PolicyUpdateEvent` (do not edit by hand).
- `proto/control_policy_ack.pb.go`
  - Generated code for `PolicyAckEvent` (do not edit by hand).

### Deployment Artifacts

- `Dockerfile`
  - Multi-stage build to produce a Linux agent image with runtime dependencies (e.g., iptables).
- `deploy/daemonset.yaml`
  - Kubernetes DaemonSet (hostNetwork/privileged) for host-level enforcement.
- `deploy/rbac.yaml`
  - ServiceAccount + Role/RoleBinding needed for Kubernetes backend and cluster discovery.
- `deploy/backend-trusted-keys-secret.yaml`
  - K8s Secret manifest holding PEM-encoded trusted public keys (mounted into `CONTROL_POLICY_TRUSTED_KEYS`).
- `systemd/enforcement-agent.service`
  - Systemd unit for running agent on a VM/bare metal host.

### Module Metadata / Docs / Artifacts

- `go.mod` / `go.sum`
  - Go module metadata. Note `replace backend => ../backend`.
- `go.mod.bak`
  - Backup/legacy module file (not used by builds unless explicitly swapped in).
- `BUILD_SUMMARY.md`
  - Build + deployment notes for the initial release.
- `RELEASE_CHECKLIST.md`
  - Operational checklist for deployments.
- `agent.exe`
  - Prebuilt Windows binary artifact (not used for Linux/Kubernetes deployments).

## Configuration (Environment Variables)

### Required

- `CONTROL_POLICY_BROKERS` (comma-separated)
- `CONTROL_POLICY_TRUSTED_KEYS` (directory containing `*.pem` Ed25519 public keys)
- `ENFORCEMENT_STATE_PATH` (path to persisted state snapshot, JSON)

### Common

- `CONTROL_POLICY_TOPIC` (default: `control.policy.v1`)
- `CONTROL_POLICY_GROUP` (default: `policy-enforcement-agent`)
- `CONTROL_POLICY_TLS` (`true|false`)
- `CONTROL_POLICY_TLS_CA`, `CONTROL_POLICY_TLS_CERT`, `CONTROL_POLICY_TLS_KEY` (optional TLS materials)
- `KAFKA_SASL_ENABLED` (`true|false`), `KAFKA_SASL_MECHANISM` (`PLAIN|SCRAM-SHA-256|SCRAM-SHA-512`), `KAFKA_SASL_USERNAME`, `KAFKA_SASL_PASSWORD`

### Enforcement

- `ENFORCEMENT_BACKEND` (default: `iptables`; other values: `nftables`, `kubernetes`/`k8s`, `noop`)
- `ENFORCEMENT_DRY_RUN` (`true|false`) - log what would be applied without changing system state
- `ENFORCEMENT_KILL_SWITCH_ENABLED` (`true|false`) - start with kill switch enabled

Backend-specific:
- `ENFORCER_IPTABLES_BIN` (default: `iptables`)
- `ENFORCER_NFT_BIN` (default: `nft`)
- `ENFORCER_KUBE_CONFIG`, `ENFORCER_KUBE_CONTEXT`, `ENFORCER_KUBE_NAMESPACE` (default: `default`)
- `ENFORCER_KUBE_QPS` (default: `5.0`), `ENFORCER_KUBE_BURST` (default: `10`)
- `ENFORCER_SELECTOR_NAMESPACE_PREFIX`, `ENFORCER_SELECTOR_NODE_PREFIX`
  - Required for selector-only policies in iptables/nftables (used to map selectors to ipset/nft set names).

### State / Reconciliation

- `ENFORCEMENT_STATE_HISTORY_RETENTION` (default: `10m`)
- `ENFORCEMENT_STATE_LOCK_TIMEOUT` (default: `3s`)
- `ENFORCEMENT_STATE_CHECKSUM` (default: `true`)
- `ENFORCEMENT_RECONCILE_INTERVAL` (default: `30s`)
- `ENFORCEMENT_RECONCILER_MAX_BACKOFF` (default: `4 * reconcile interval`)
- `ENFORCEMENT_EXPIRATION_INTERVAL` (default: `5s`)
- `ENFORCEMENT_SCHEDULER_MAX_BACKOFF` (default: `4 * expiration interval`)

Optional ledger sync:
- `LEDGER_SNAPSHOT_PATH` (path to JSON `[]PolicySpec`)
- `LEDGER_DRIFT_GRACE` (duration; if set, controls how often ledger reconciliation runs)

### Rate Limiting (Guardrails Coordination)

- `RATE_LIMIT_COORDINATOR` (`local|redis`, default: `local`)
Redis options:
- `RATE_LIMIT_REDIS_ADDR`, `RATE_LIMIT_REDIS_USERNAME`, `RATE_LIMIT_REDIS_PASSWORD`, `RATE_LIMIT_REDIS_DB`
- `RATE_LIMIT_KEY_PREFIX` (optional)

### ACK Publishing (Optional)

- `ACK_ENABLED` (`true|false`, default: `false`)
- `ACK_TOPIC` (default: `control.policy.ack.v1`)
- `ACK_BROKERS` (defaults to `CONTROL_POLICY_BROKERS`)
- `ACK_CLIENT_ID` (default: `policy-ack-publisher`)
- `ACK_RETRY_MAX` (default: `5`)
- `ACK_RETRY_BACKOFF` (default: `500ms`)
- `ACK_QUEUE_PATH` (defaults next to `ENFORCEMENT_STATE_PATH` as `ack-queue.db`)
- `ACK_QUEUE_MAX_SIZE` (default: `10000`)
Batching:
- `ACK_BATCH_ENABLED` (`true|false`)
- `ACK_BATCH_MAX_SIZE` (default: `50`)
- `ACK_BATCH_INTERVAL` (default: `250ms`)
Signing:
- `ACK_SIGNING_ENABLED` (`true|false`)
- `ACK_SIGNING_KEY_PATH` (private key; supports PEM/hex/base64/raw)

### Fast-Path (Pre-Consensus)

- `FAST_PATH_ENABLED` (`true|false`)
- `FAST_PATH_MIN_CONFIDENCE` (default: `0.9`)
- `FAST_PATH_SIGNALS_REQUIRED` (default: `2`)

### Observability / Ops

- `METRICS_ADDR` (default: `:9094`)
- `LOG_LEVEL` (`debug|info|warn|error`, default: `info`)
- `SHUTDOWN_TIMEOUT` (default: `15s`)

## HTTP Endpoints

The agent exposes a small HTTP server on `METRICS_ADDR`:

- `GET /metrics` - Prometheus metrics
- `GET /healthz` - backend health check (if backend implements `HealthChecker`)
- `GET /readyz` - backend readiness check (if backend implements `ReadyChecker`)
- `GET /control/kill-switch` - returns `{"enabled": <bool>}`
- `POST /control/kill-switch?enabled=true|false` - toggles kill switch at runtime

## Notes / Gotchas

- `backend/proto` import: Go code imports protobuf types from the `backend` module (see `go.mod` line `replace backend => ../backend`). The `proto/` directory here contains schemas and generated code aligned to that `go_package`, but the build expects the sibling `../backend` module to be present.
- Non-Linux builds: iptables/nftables are `//go:build linux` and have stub implementations for other platforms.
- Selector-only policies: For iptables/nftables, policies that target selectors (e.g., namespace/node) require selector set prefixes (`ENFORCER_SELECTOR_NAMESPACE_PREFIX`, `ENFORCER_SELECTOR_NODE_PREFIX`) so the backend can map selectors into set names.

