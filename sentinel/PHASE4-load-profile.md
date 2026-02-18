# Phase 4 Load Profile v1 (Standalone Hardening)

This document defines the Phase 4 load profile used to harden and validate
standalone Sentinel. It is the source of truth for Phase 4 runs and SLAs.

## Scope
- Standalone only (no Kafka, no backend consensus).
- Security-first defaults:
  - LLM disabled.
  - Threat-intel disabled (enable only in a separate run).
  - OPA gate enabled (monitor-only).
  - Adapter attestation enforced when ingesting OSS adapter outputs (HMAC).

## Input corpora (local)
Telemetry corpus (realistic volume):
- `B:\CyberMesh\TestData\clean\deepflow\zeek_sampled.jsonl`
- `B:\CyberMesh\TestData\clean\flows\cloud\gcp_vpc_flows.jsonl`
- `B:\CyberMesh\TestData\clean\flows\cloud\azure_nsg_flows.jsonl`
- `B:\CyberMesh\TestData\clean\flows\cloud\aws_vpc_flows.log`

Telemetry corpus (negative test: schema mismatch, should fail safely):
- `B:\CyberMesh\TestData\clean\flows\cic_ids2017_sampled.jsonl` (missing IPs/proto in this format)

Scenario corpus (pattern coverage):
- `B:\CyberMesh\TestData\clean\ai_scenarios\c2_beacon.jsonl`
- `B:\CyberMesh\TestData\clean\ai_scenarios\data_exfil.jsonl`
- `B:\CyberMesh\TestData\clean\ai_scenarios\lateral_movement.jsonl`
- `B:\CyberMesh\TestData\clean\ai_scenarios\port_scan.jsonl`
- `B:\CyberMesh\TestData\clean\ai_scenarios\multi_tenant.jsonl`
- `B:\CyberMesh\TestData\clean\ai_scenarios\bare_metal_ipfix.jsonl`

Edge cases corpus:
- `B:\CyberMesh\TestData\clean\edge-cases\sparse_flows.jsonl`
- `B:\CyberMesh\TestData\clean\edge-cases\invalid_protobuf.bin`
- `B:\CyberMesh\TestData\clean\edge-cases\malformed_protobuf.bin`

OSS ingest corpus:
- `B:\CyberMesh\sentinel\oss_outputs\incoming\*`

## Workload mixes (v1)
All runs use streaming reads for JSONL/NDJSON (bounded memory).

1) Telemetry-only (primary load):
- 100% telemetry flows from Zeek deepflow and cloud flow logs

2) Scenario-only (behavior coverage):
- Even split across `ai_scenarios/*.jsonl`

3) Mixed (production-like local):
- 70% telemetry flows (CIC-IDS2017 JSONL)
- 20% scenarios (ai_scenarios JSONL)
- 10% OSS ingest (events generated from `oss_outputs/incoming/*`)

4) Edge-cases (hardening gate):
- 100% edge-cases corpus, run with strict validation and limits

## Concurrency + batch sizing (v1)
We test both parallel and sequential modes.

Event-level concurrency (orchestrator):
- Parallel: `--sequential=false` (default)
- Sequential baseline: `--sequential=true`

Load levels:
- L0 smoke: 1 worker, 500 events
- L1 baseline: 4 workers, 10,000 events
- L2 stress: 16 workers, 50,000 events

OSS batch ingest sizing:
- Batch size per file: up to 1,000 records (bounded by adapter limits)
- Repeat/permute for stress: cycle records to reach target event count

Durations:
- Ramp test: 3 minutes (step up concurrency every 30s)
- Steady-state: 10 minutes at target load
- Burst: 60 seconds at 2x target load, then 60 seconds recovery

## SLAs (v1, standalone, LLM/TI disabled)
These are strict enough to catch regressions locally, and realistic for a
single-host run without external services.

Correctness / safety:
- Error rate: < 0.5% for valid corpora (telemetry + scenarios + OSS ingest)
- Edge-case corpus: 0 hangs; failures must return stable error codes
- Determinism: identical inputs produce identical outputs (modulo timestamps)

Latency targets (analysis only, per-event):
- Telemetry flows: P95 <= 20 ms, P99 <= 50 ms
- OSS-derived events: P95 <= 25 ms, P99 <= 60 ms
- Phase-3 events (action/mcp/exfil/resilience): P95 <= 15 ms, P99 <= 40 ms

Throughput targets (single host):
- Telemetry-only L1: >= 200 events/sec sustained (parallel)
- Mixed L1: >= 100 events/sec sustained (parallel)

Resource ceilings (single host):
- Working set / RSS stable over steady-state (no monotonic growth > 10%)
- No unbounded log growth; raw_context and sanitizer truncation must cap payload

OPA expectations (monitor-only):
- OPA reachable: decision attached to `metadata.opa`
- OPA unreachable/timeout: fail-open, explicit error surfaced (no silent failure)

## Phase 4 exit gate mapping
Phase 4 is complete when:
- L0-L2 load runs produce reports in `B:\CyberMesh\sentinel\reports\`.
- All SLAs above are met (or PRD updated with accepted deviations and rationale).
- Edge-case runs prove: size limits, timeouts, dedupe, and error codes hold under load.
