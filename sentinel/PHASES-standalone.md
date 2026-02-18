# Standalone Phase Execution Plan (Wiring + Integration + Testing)

This document expands the standalone PRD with execution details.
It avoids PRD bloat while preserving the phase‑by‑phase plan.

## Phase 0 — Foundations
**Wiring**
- Define canonical envelope v1 and modality list.
- Define adapter config format and versioning rules.
- Finalize deterministic merge/dedupe specification.

**Integration**
- None (design only).

**Testing**
- Define schema lint rules and adapter conformance checklist.

**Edge Cases**
- Schema drift policy and raw_context passthrough.
- Late arrivals and correlation window rules.

**Acceptance**
- Envelope v1 approved.
- Merge/dedupe spec approved.
- Governance policy approved.

**SLA**
- N/A (design phase).

**Complexity**
- Low–Medium.

## Phase 1 — Standalone Core (File + Telemetry)
**Wiring**
- File + telemetry graphs stable.
- Deterministic merge/dedupe validated.

**Integration**
- Local CLI flow for file + telemetry.
- JSON/CSV/IPFIX‑JSON ingest.

**Testing**
- Parallel vs sequential parity.
- Degraded‑mode visibility.
- Strict E2E fixture runs.

**Edge Cases**
- Invalid payloads, missing fields.
- Invalid IP/port, negative/zero duration.
- Parser timeout and agent failure.

**Acceptance**
- CLI runs file + telemetry end‑to‑end.
- Deterministic outputs for identical inputs.
- Degraded metadata visible for missing models/sensors.

**SLA**
- P95 <= 2s per telemetry event.
- Success rate >= 99%.

**Complexity**
- Medium.

## Phase 2 — OSS Ingest (Batch)
**Wiring**
- Adapters for Zeek/Falco/Sigma/scanners.
- Flow/Process/RulesHit/ScannerFindings agents wired.
- Modality routing extended.

**Integration**
- Local ingest of OSS outputs (NDJSON/JSON files).
- Local policy gate (OPA monitor‑only).

**Testing**
- Adapter conformance tests.
- OSS fixture replay and merge/dedupe validation.

**Edge Cases**
- Schema drift, partial records.
- Duplicate events across sensors.
- Oversized payloads and tool output poisoning.

**Acceptance**
- Adapters pass conformance checks.
- Agents produce expected findings on OSS fixtures.
- Policy gate emits metadata (monitor‑only).

**SLA**
- P95 <= 3s per batch event.
- Success rate >= 99%.

**Complexity**
- Medium–High.

## Phase 3 — Advanced Detection
**Wiring**
- Sequence Risk Agent.
- MCP Runtime Controls Agent.
- Exfil/DLP Agent.
- Resilience Agent.

**Integration**
- Session/tool‑call telemetry inputs (local).

**Testing**
- Chain‑of‑events scenarios.
- MCP schema drift tests.
- Telemetry gap simulation.

**Edge Cases**
- Out‑of‑order events.
- Missing session IDs.
- False positives on benign chains.

**Acceptance**
- Agents emit findings on synthetic chains.
- Resilience alerts on missing telemetry.

**SLA**
- P95 <= 4s per event.
- Success rate >= 99%.

**Complexity**
- High.

## Phase 4 — Standalone Hardening
**Wiring**
- Strict size/time limits for adapters and agents.
- OPA behavior documented and enforced (monitor‑only).

**Integration**
- Local load testing and failure injection.

**Testing**
- Sustained run.
- Backpressure handling.
- Data loss policy validation.

**Edge Cases**
- Hot‑partition spikes.
- Persistent tool failures.
- Silent sensor drop.

**Acceptance**
- P95 <= 5s under sustained local load.
- Success rate >= 99% over 24h.
- No silent failures (resilience alerts).

**SLA**
- See acceptance targets above.

**Complexity**
- High.
