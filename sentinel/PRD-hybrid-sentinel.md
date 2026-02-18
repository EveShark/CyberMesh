# PRD: Hybrid Sentinel Integration (File + Telemetry)

## Title
Hybrid, Parallel Multi-Agent Pipeline for Sentinel (Standalone + AI-Service Integration)

## Problem Statement
Sentinel currently runs a sequential file-analysis workflow and does not ingest telemetry (CSV/IPFIX/JSON). This limits latency, makes it hard to plug in new agents, and blocks full integration with the broader AI-service pipeline. We need a hybrid pipeline with **two graphs**—one for file analysis and one for telemetry—so Sentinel can run standalone and integrate with AI-service/Kafka at scale.

## Goals
- Reduce end-to-end detection latency via parallel agent execution.
- Keep a fast-path short-circuit for known malware/clean samples.
- Add telemetry ingestion (CSV/IPFIX/JSON) and minimal **production-grade** telemetry analysis.
- Make it easy to add new agents (Zeek, Sigma, Falco, RITA, BZAR, etc.).
- Preserve standalone use (CLI/API) and library integration (AI service).
- Ensure Kafka is supported and required for integrated mode (optional for standalone).

## Non-Goals
- Full UI changes.
- Replacing the existing API; it should remain functional.
- Full-scale production telemetry pipeline (backpressure, autoscaling) in phase 1.

## Users & Use Cases
- Security engineers running file scans via API/CLI.
- AI service pipeline calling Sentinel as a library.
- Telemetry-driven detection (network flows, logs, signals).
- Future agents plugged in for network/identity/cloud.

## Success Metrics
- P95 latency improvement (target: 30–50% reduction on multi-agent runs).
- ≥95% successful completion even if one agent fails.
- New agent can be added with minimal wiring effort (config + registration).
- Telemetry ingest produces valid detections with <5s processing time.

## Functional Requirements
1. **Two-graph architecture**:
   - FileAnalysisGraph
   - TelemetryAnalysisGraph
   - Orchestrator routes by modality.
2. File graph: Parse → Fast-path → Parallel agents → Merge/Dedupe → Coordinator.
3. Telemetry graph: Ingest → Normalize → Parallel minimal agents → Merge/Dedupe → Coordinator.
4. Parallel execution for independent agents (static, malware, script, ML, threat-intel, LLM gated).
5. Telemetry ingestion:
   - Accept CSV/IPFIX/JSON payloads.
   - Convert into `NetworkFlowFeaturesV1` (or future canonical schemas).
6. Minimal telemetry agents (Phase 1):
   - Rules/heuristics (pps, bytes/sec, SYN ratio, port scans).
   - Optional threat-intel enrichment on IPs/domains.
7. Network/telemetry plug-in path for Zeek/Sigma/Falco/RITA/BZAR outputs.
8. Deterministic aggregation: consistent verdict from same inputs.
9. Degraded-mode reporting: if YARA/models/hash DB/threat-intel missing.
10. Standalone runner optional: thin CLI/main wrapper around library.
11. Library-first API: clear function that AI service can call.
12. Kafka required for integrated mode; optional for standalone.

## Non-Functional Requirements
- Reliability: agent failure must not crash pipeline.
- Latency: timeouts for slow parsers/providers.
- Security: redact PII before LLM reasoning.
- Observability: logs + per-agent timing in metadata.
- Compatibility: preserve current API behavior.

## Architecture (Target)
### Orchestrator (Modality Router)
- Routes **FILE** → FileAnalysisGraph
- Routes **TELEMETRY** → TelemetryAnalysisGraph

### FileAnalysisGraph (Hybrid)
1. Parse file
2. Fast-path (hash/YARA/signatures)
3. If no short-circuit → run agents in parallel:
   - StaticAnalysisAgent
   - MalwareAgent
   - ScriptAgent (if script)
   - MLAnalysisAgent
   - ThreatIntelAgent
   - LLMReasoningAgent (gated)
4. Merge + dedupe findings/indicators
5. Coordinator / Ensemble decision

### TelemetryAnalysisGraph (Minimal, Production-Grade)
1. Ingest telemetry (CSV/IPFIX/JSON)
2. Normalize to canonical features (`NetworkFlowFeaturesV1`)
3. Run minimal telemetry agents in parallel:
   - Rules/heuristics agent
   - Optional threat-intel enrichment
4. Merge + dedupe
5. Coordinator / Ensemble decision

Inputs:
- File path (today)
- Telemetry events (CSV/IPFIX/JSON)

Outputs:
- Verdict, confidence, threat level, findings, indicators, reasoning steps

## Edge Cases & Risks (Must Handle)
- Agent failures (LLM/Threat-Intel unavailable) → continue with error metadata.
- Missing models / YARA → degraded mode warnings in result.
- Duplicate findings across agents → dedupe merge step.
- Large files → timeouts & size limits.
- LLM prompt PII exposure → redact.
- Telemetry schema mismatch → validation errors + safe fallback.
- IPFIX parsing failures → safe drop with error reporting.

## Baseline Validation (Sequential vs Parallel)
Date: February 3, 2026

Scope:
- File sample: `B:\CyberMesh\sentinel\main.py` (fast-path disabled, LLM/threat-intel disabled, external YARA rules disabled)
- Telemetry sample: `B:\CyberMesh\sentinel\testdata\telemetry\valid_syn_flood.json` (threat-intel disabled)

Results:
- File verdict parity: CLEAN (parallel 19.73 ms, sequential 7.16 ms)
- Telemetry verdict parity: SUSPICIOUS (parallel 4.92 ms, sequential 1.92 ms)
- Degraded mode: ML models missing warnings observed (expected in this environment)

Notes:
- This baseline validates parity and execution paths; broader benchmarking is required for realistic latency deltas.

## Steps (Start → End)
Start:
1. Confirm baseline pipeline run (fast-path + sequential graph).
2. Create PRD (this).

Implementation Steps:
3. Add merge/dedupe utility for parallel outputs.
4. Update graph to hybrid:
   - Parse → Fast-path
   - Parallel branches for eligible agents
   - Merge → Coordinator
5. Wire missing agents:
   - Add ScriptAgent, MalwareAgent to graph
6. Add degraded-mode warnings (YARA missing, models missing, hash DB missing, threat-intel down).
7. Add per-agent timing + error metadata.
8. Add optional thin main.py runner (CLI mode only).
9. Add telemetry ingestion adapters:
   - CSV → NetworkFlowFeaturesV1
   - IPFIX → NetworkFlowFeaturesV1
   - JSON → NetworkFlowFeaturesV1
10. Add TelemetryAnalysisGraph and modality router.
11. Add minimal telemetry agent (rules/heuristics + optional intel).

End:
12. Integration tests:
   - Fast-path short-circuit
   - Parallel run with multiple agents
   - Partial failure survives
   - Telemetry ingestion → detection flow
13. Document usage for standalone + AI service integration.

## Open Questions
- Should LLM be enabled by default in production?
- How aggressive should dedupe be (source+description vs. content hash)?
- Which agents must be required vs. optional?
- Telemetry formats priority for Phase 1: CSV vs IPFIX vs JSON?
- Backpressure strategy for telemetry ingestion?
