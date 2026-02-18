# Phase 3 TODO (Advanced Detection — Standalone)

This checklist tracks Phase 3 planning, wiring, testing, and edge cases.
Only items with verified evidence should be marked complete.

## Schema Grounding
- [x] Pull MCP spec/SDK fields (no guessing)
- [x] Define Phase‑3 schemas in `sentinel/contracts/schemas.py`
- [x] Version schemas + update governance notes

## Adapter Mapping (telemetry‑layer compatibility)
- [x] Define adapter mappings for MCP runtime telemetry
- [x] Define adapter mappings for sequence/toolchain events
- [x] Define adapter mappings for exfil/DLP events
- [x] Define adapter mappings for resilience/health events

## Agent Implementation
- [x] Sequence Risk Agent
- [x] MCP Runtime Controls Agent
- [x] Exfil/DLP Agent
- [x] Resilience Agent

## Wiring / Integration
- [x] Add new modality routing in orchestrator
- [x] Parallel execution + deterministic merge/dedupe verified
- [x] Degraded‑mode behavior on partial failures
- [x] Route ACTION_EVENT, MCP_RUNTIME, EXFIL_EVENT, RESILIENCE_EVENT to agents

## Validation / Hardening
- [x] Strict input validation per schema
- [x] Size limits + timeouts enforced
- [x] PII redaction where needed
- [x] Agents return structured errors (no crashes)
- [x] Degraded‑mode metadata emitted on partial failures
- [x] Explicit error codes for malformed payloads/oversize/invalid fields

## Testing
- [x] Unit tests for each agent
- [x] Integration tests with realistic synthetic payloads
- [x] Partial‑failure tests (timeouts, missing fields, invalid payloads)
- [x] Dedupe tests for concurrent chains
- [x] Edge‑case tests for each modality (below) are explicitly verified

## Benchmark
- [x] Phase‑3 performance baseline recorded (P50/P95/Max)
- [x] Report stored in `reports/phase3_benchmark.json` + `.md`

## Edge Cases to Validate
Sequence Risk:
- [x] Out‑of‑order or missing steps
- [x] Concurrent chains sharing tools
- [x] Duplicate events / replay

MCP Runtime Controls:
- [x] New MCP server appears
- [x] Tool schema changes mid‑run
- [x] Oversized or encoded arguments
- [x] Tool output contains instruction payloads

Exfil/DLP:
- [x] Chunked/encoded exfil
- [x] Allowed destinations used for exfil
- [x] Missing/incorrect data classification

Resilience:
- [x] Agent timeouts / silent failures
- [x] Missing telemetry (sensor blind)
- [x] Clock skew between event and observed time

## Plan of Action (Remaining)
All Phase 3 checklist items are complete for standalone scope.
