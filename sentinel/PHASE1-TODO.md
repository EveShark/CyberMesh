# Phase 1 TODO (Standalone Core)

This checklist tracks Phase 1 wiring, integration, testing, and edge cases.
Only items with verified evidence are marked complete.

## Wiring
- [x] File graph path is wired (parse -> fast-path -> parallel -> merge/dedupe -> coordinator)
- [x] Telemetry graph path is wired (validate -> parallel -> merge/dedupe -> coordinator)
- [x] Modality routing for file vs telemetry is wired
- [x] Deterministic merge/dedupe enabled in both graphs

## Integration (Standalone)
- [x] CLI run: file analysis (`main.py <file>`) verified manually
- [x] CLI run: telemetry analysis (`--telemetry --format json|csv|ipfix`) verified manually
- [x] Telemetry adapters normalize into `NetworkFlowFeaturesV1`

## Testing
- [x] Parallel vs sequential parity validated on baseline samples
- [x] Strict E2E tests: file pipeline
- [x] Strict E2E tests: telemetry pipeline
- [x] Degraded-mode metadata emitted when models/tools are missing
- [x] Per-agent timing metadata captured

## Edge Cases (Phase 1)
File:
- [x] Unsupported file type -> explicit error
- [x] Parser timeout -> safe error
- [x] Missing YARA/hash/signatures -> degraded mode

Telemetry:
- [x] Invalid JSON / invalid CSV -> error
- [x] Missing required fields -> error
- [x] Invalid IP or port range -> error
- [x] Negative duration -> reject
- [x] Zero duration -> degraded reason
- [x] IPFIX non-UTF8 (binary) -> error

Pipeline:
- [x] Agent exception -> captured, pipeline continues
- [x] Duplicate findings/indicators -> dedupe
- [x] Partial failure in parallel branches -> metadata contains error

## Phase 1 Exit Criteria
- [x] CLI file + telemetry runs verified
- [x] E2E file + telemetry tests pass
- [x] Deterministic outputs for identical inputs
- [x] Degraded-mode visibility in results
