# Phase 2 TODO (Standalone OSS Ingest — Batch)

This checklist tracks Phase 2 wiring, integration, testing, and edge cases.
Only items with verified evidence should be marked complete.

## Wiring
- [x] Add Zeek adapter (NDJSON/JSON) -> NETWORK_FLOW (+ DNS_EVENT if needed)
- [x] Add Falco adapter (NDJSON/JSON) -> PROCESS_EVENT
- [x] Add Sigma/BZAR adapter -> RULES_HIT or SCAN_FINDINGS
- [x] Add scanner adapters (mcp-scan / skill-scanner / TruffleHog / Rebuff) -> SCAN_FINDINGS
- [x] Add FlowAgent
- [x] Add ProcessAgent
- [x] Add RulesHitAgent
- [x] Add ScannerFindingsAgent
- [x] Extend modality routing for new modalities
- [x] Ensure deterministic merge/dedupe applies to new agents

## Integration (Standalone)
- [x] Local batch ingest of OSS outputs (NDJSON/JSON files)
- [x] Normalize into canonical envelope v1
- [x] Local OPA gate (monitor-only) attached to results

## Testing
- [x] Adapter conformance tests for required fields
- [x] Zeek fixture replay
- [x] Falco fixture replay
- [x] Sigma/BZAR fixture replay
- [x] Scanner fixture replay
- [x] Merge/dedupe across OSS sources validated
- [x] OPA unavailable -> fail-open (monitor-only) behavior validated

## Edge Cases (Phase 2)
Adapters/Data:
- [x] Schema drift (unknown fields -> raw_context)
- [x] Mixed valid/invalid records in batch
- [x] Oversized payloads (size limits enforced)
- [x] Malformed JSON/NDJSON -> error

Correlation/Dedupe:
- [x] Duplicate events from multiple sensors
- [x] Conflicting severity between tools

Security:
- [x] Tool output poisoning detection (sanitize/validate)
- [x] Spoofed findings rejected (missing/invalid signature)
- [x] Missing tenant_id -> reject/quarantine

Performance:
- [x] Large batch sizes handled with bounded timeouts
- [x] Local backpressure policy applied

## Phase 2 Exit Criteria
- [x] Core adapters and agents wired
- [x] OSS fixture replays pass
- [x] Deterministic merge/dedupe verified on OSS data
- [x] OPA gate integrated (monitor-only)
