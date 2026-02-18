# PRD: Sentinel OSS Integration (Standalone)

## 1) Title & Document Control
- Title: Sentinel OSS Integration PRD (Standalone)
- Owner: Sentinel
- Status: Draft (standalone scope)
- Date: February 3, 2026

## 2) Problem Statement
We need a production‑grade, standalone integration of proven OSS detection and
monitoring tools without rebuilding them. Sentinel must ingest OSS tool outputs
locally (files/NDJSON/JSON), normalize them into canonical schemas, correlate
signals, and produce deterministic verdicts and findings. This PRD is
standalone‑only and does not include AI‑service, Kafka, or backend consensus.

## 3) Goals / Non‑Goals
### Goals
- Integrate OSS tools as external services/sidecars with clean adapters.
- Normalize tool outputs into canonical schemas (internal Protobuf or JSON).
- Provide modular agents with parallel execution and deterministic merge/dedupe.
- Support local policy gating (OPA in monitor‑only mode initially).
- Preserve standalone Sentinel operation with clear degraded‑mode visibility.

### Non‑Goals
- Wiring to AI‑service / Kafka / BFT.
- Embedding OSS tool source code into Sentinel.
- Full SOC UI or SOAR workflows.

## 4) Scope
### In‑Scope (Standalone)
- Offline/batch ingest of OSS tool outputs (files/NDJSON/JSON).
- Adapter‑based normalization into canonical events.
- Agent wiring for new modalities.
- Local policy gate (OPA) behavior defined.
- Resilience/degraded‑mode behavior in standalone pipeline.

### Out‑of‑Scope
- Distributed deployment, Kafka topics, gRPC streaming.
- Cross‑service consensus or backend commit logic.

## 5) Assumptions & Constraints
- OSS tools run as external sidecars; Sentinel ingests their outputs.
- Ingest is local (filesystem/NDJSON/JSON).
- Adapters are config‑driven, not hardcoded.
- Missing fields are tolerated via raw_context passthrough.

## 6) Users & Use Cases
- Security engineers running standalone scans.
- Batch analysis of OSS telemetry outputs for validation/testing.
- Security R&D teams adding new agents and adapters locally.

## 7) Success Metrics / SLAs (Standalone)
Phase targets:
- Phase 1: P95 latency <= 2s per telemetry event; >= 99% success rate.
- Phase 2: P95 latency <= 3s per OSS event (batch ingest); >= 99% success rate.
- Phase 3: P95 latency <= 4s with advanced agents; >= 99% success rate.
- Deterministic outputs for identical inputs.
- Degraded‑mode visibility for missing sources.

Measured baseline (2026‑02‑05, parallel, LLM+threat‑intel disabled):
- Overall: P50 0.62 ms, P95 2.87 ms, Max 3.09 ms (50 event analyses).
- Report: `reports/phase2_benchmark.json` and `reports/phase2_benchmark.md`.

## 8) High‑Level Architecture (Standalone)
```
OSS Tools (Zeek/Falco/Sigma/RITA/BZAR/etc.)
            |
   Local Ingest + Normalize (Adapters)
            |
        Sentinel Agents
            |
   Coordinator + Local Policy Gate
            |
         Verdict Output
```

## 9) OSS Components & Licensing (Standalone Posture)
Decision posture per component:
- Zeek (BSD): sidecar service; ingest outputs.
- Falco (Apache‑2.0): sidecar service; ingest outputs.
- Sigma rules: external ruleset; ingest rule hits only.
- RITA: sidecar analytics; ingest summarized findings only.
- BZAR: clarify definition + license before integration.
- mcp‑scan (Apache‑2.0): sidecar scanner; ingest findings only.
- Skill Scanner (Apache‑2.0): sidecar scanner; ingest findings only.
- TruffleHog: sidecar scanner only (do not embed in core).
- Rebuff (Apache‑2.0): optional sidecar; ingest findings only.
- OPA (Apache‑2.0): local policy gate (monitor‑only initially).

## 10) Integration Strategy (Standalone)
### Principles
- OSS tools emit data; Sentinel consumes normalized events.
- No direct CLI calls in hot path; ingest files/NDJSON/JSON.
- Adapters are stateless and config‑driven.

### Adapter Pattern
1) Tool output -> Adapter -> CanonicalEvent / Features
2) CanonicalEvent routed by modality
3) Agents run in parallel
4) Merge/Dedupe -> Coordinator -> Local Policy Gate -> Output

## 11) Deterministic Merge/Dedupe Specification
- Primary dedupe key: `event_id` if provided.
- Secondary key: hash of normalized core fields (per modality).
- Conflict resolution: prefer highest severity, then highest confidence.
- Correlation window: 5 minutes (default), configurable per modality.
- Late arrivals: process if within window; otherwise mark as late in metadata.

## 12) Canonical Schemas & Formats
### Internal Canonical Format
- Protobuf preferred for internal processing.
- Envelope fields: event_id, schema_version, timestamp, source, modality,
  features, raw_context, findings, tenant_id.

### Ingest Formats (Standalone)
- JSON/NDJSON (primary ingest for OSS outputs).
- CSV only for offline/batch.
- IPFIX binary must be converted upstream to JSON.

### Compatibility Rules
- Required core fields + optional extensions via raw_context.
- Backward‑compatible schema evolution only.

## 13) Schema Governance
- Registry: Git + CI schema checks (standalone).
- Breaking‑change policy: new required fields require version bump.
- Deprecation: minimum 2 releases before removal.
- Conformance tests: each adapter must populate required fields per modality.
- Phase 3 v1 schemas align to JSON‑RPC envelope (MCP 2025‑11‑25) and MCP tools spec (2025‑06‑18):
  - `tools/list` response includes `tools[]` with `name`, `description`, `inputSchema`, `annotations`
  - `tools/call` request includes `params.name` and `params.arguments`
  - `notifications/tools/list_changed` sent when `listChanged` capability is enabled
- Phase 3 v1 schemas align to CloudEvents required attributes (`specversion`, `id`, `source`, `type`)
  and OTel LogRecord core fields (Timestamp, ObservedTimestamp, SeverityText/Number, Body,
  Attributes, TraceId/SpanId, Resource, InstrumentationScope).

## 14) Event Modalities & Routing
Planned modalities:
- FILE
- NETWORK_FLOW
- PROCESS_EVENT
- AUTH_EVENT
- CLOUD_EVENT
- SCAN_FINDINGS
- DNS_EVENT (if required)

Routing performed by SentinelOrchestrator or equivalent modality router.

## 15) Agent Inventory
### Current Agents (Standalone)
File graph:
- StaticAnalysisAgent
- ScriptAgent
- MalwareAgent (YARA + ML)
- MLAnalysisAgent (fallback)
- ThreatIntelAgent
- LLMReasoningAgent (gated)
- CoordinatorAgent

Telemetry graph:
- TelemetryRulesAgent
- TelemetryThreatIntelAgent
- CoordinatorAgent

### Planned Agents (Standalone)
Core modality agents:
- FlowAgent (Zeek, RITA)
- ProcessAgent (Falco)
- RulesHitAgent (Sigma, BZAR)
- ScannerFindingsAgent (mcp‑scan, skill‑scanner, TruffleHog, Rebuff)
- PolicyGateAgent (OPA; monitor‑only initially)

Security‑first agents:
- Toolchain/Sequence Risk Agent
- Session/Intent (UEBA) Agent
- MCP Runtime Controls Agent
- Identity Agent
- Cloud IAM Agent
- Exfil/DLP Agent
- Resilience Agent

## 16) Execution Model (Standalone)
- Parallel execution by default with deterministic merge/dedupe.
- Sequential mode only for baseline validation.
- Per‑agent timing + error metadata captured.
- Degraded mode emitted on partial failure or missing dependencies.

## 17) Telemetry Ingestion (Standalone Adapters)
Adapters map OSS outputs to canonical features:
- Zeek -> NetworkFlowFeaturesV1 + DNS Event
- Falco -> ProcessEvent
- Sigma -> RulesHitEvent
- RITA -> FlowSummaryEvent / RulesHitEvent
- BZAR -> RulesHitEvent (definition required)
- mcp‑scan / skill‑scanner / TruffleHog / Rebuff -> ScanFindingsEvent

## 18) Correlation & Decisioning
- Coordinator aggregates agent outputs into final verdict.
- Local correlation fuses multi‑signal evidence across modalities.
- OPA Policy Gate runs locally (monitor‑only in early phases).

OPA failure behavior (standalone):
- Monitor‑only: fail‑open, log policy error in metadata.

## 19) Security Boundary for Untrusted Tool Output
- Input size limits per adapter (max payload size, max record count).
- Parsing timeouts and strict validation per adapter.
- Adapter outputs signed (HMAC) by ingest layer to prevent spoofed findings.
- Tenant isolation: every envelope must carry tenant_id; reject otherwise.

## 20) Performance & Scalability (Standalone)
- Local backpressure policy (drop vs delay vs queue).
- Bounded timeouts for all agents and parsers.
- Parallel execution with deterministic merge.

## 21) Edge Cases & Failure Modes
Data:
- Schema drift, unknown fields, partial records.
- Out‑of‑order events, late arrivals.
- Duplicate events across sensors.
Pipeline:
- Backpressure, large payloads, hot partitions (local).
- Tool timeouts and partial failure.
Security:
- Tool output poisoning.
- PII leakage in LLM prompts/logs.
Logic:
- Conflicting findings (rules vs intel).
- Chain‑of‑events risk (benign steps -> malicious chain).
Policy:
- OPA unreachable (monitor‑only behavior).

## 22) Phased Delivery Plan (Standalone)
### Phase 0: Foundations
- Finalize canonical schemas + envelope.
- Define adapter config format.
- Document deterministic merge/dedupe rules.

### Phase 1: Standalone Core
- File + telemetry graphs stable.
- JSON/CSV/IPFIX‑JSON ingest supported.
- Deterministic merge/dedupe validated.

### Phase 2: OSS Ingest (Batch)
- Adapters for Zeek + Falco + Sigma + scanners.
- FlowAgent + ProcessAgent + RulesHitAgent + ScannerFindingsAgent.
- Local PolicyGateAgent in monitor‑only mode.

### Phase 3: Advanced Detection
- Sequence Risk Agent.
- MCP Runtime Controls Agent.
- Exfil/DLP Agent.
- Resilience Agent.

### Phase 4: Standalone Hardening
- Local load testing.
- Strict validation + size/time limits verified.
- OPA behavior documented and enforced (monitor‑only).

## 23) Dependencies (Standalone)
- Local OSS tool runtimes.
- Adapter config files.
- Optional local OPA runtime.

## 24) Open Questions
- Exact definition and data format for BZAR.
- Required latency SLA per modality (if different from phase defaults).
- Minimum data retention for batch replay.
- Which OSS tools are mandatory in Phase 2.

## 25) Risks & Mitigations
- OSS schema changes -> adapters + raw_context passthrough.
- Tool failures -> degraded mode + resilience agent.
- Tool output spoofing -> HMAC signing + strict validation.
- Scale bottlenecks -> backpressure + bounded timeouts.

## 26) Phase Status & Verification (Standalone)
Status as of 2026-02-06:
- Phase 2 (OSS Ingest): Completed and tested.
  - Test run: `pytest -q` => 315 passed, 12 skipped.
  - Skips: optional models/CyberMesh adapters not present in this standalone env.
  - Benchmark: `scripts/bench_phase2.py` (results in `reports/phase2_benchmark.json` / `.md`).
- Phase 3 (Advanced Detection): Implemented and exercised via E2E replay gate.
  - E2E: `scripts/phase4_e2e_replay.py` (results in `reports/phase4_e2e_*.json` / `.md`).
  - Phase-3 corpora: `testdata/clean/phase3-events/*`.
- Phase 4 (Standalone Hardening): In progress.
  - Completed (verified):
    - Validation + limits (oversize/malformed/missing tenant strict/dedupe/future ts/IPv6/out-of-order) and redaction/bounds checks.
    - OPA monitor-only unreachable + bounded timeout surfaced (fail-open).
    - Telemetry CLI path applies OPA/tenant context via `SentinelOrchestrator.analyze_event()`.
    - Evidence: `reports/phase4_hardening_summary.json` / `.md`.
  - Pending:
    - Reliability/degraded-mode induction tests (agent timeout/partial failure).
    - OPA reachable test (requires local OPA server or CI service).
    - Deterministic merge validation under high concurrency (beyond current local gate).
    - SLA pass/fail table and operator guidance section in Phase 4 docs.
