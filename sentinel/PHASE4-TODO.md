# Phase 4 TODO (Standalone Hardening)

This checklist tracks Phase 4 hardening work for standalone Sentinel.
Only mark items complete when there is verified evidence (tests, benchmarks, reports).

## 1) Load Profile (Source Of Truth)
- [x] Define workload mixes (file / telemetry / OSS batch / Phase-3 events)
- [x] Use `B:\CyberMesh\TestData\clean\flows\cic_ids2017_sampled.jsonl` as primary telemetry load corpus (realistic volume)
- [x] Use `B:\CyberMesh\TestData\clean\ai_scenarios\*.jsonl` as scenario corpus (C2 beacon, exfil, lateral movement, port scan, multi-tenant)
- [x] Use `B:\CyberMesh\TestData\clean\edge-cases\*.jsonl` as edge-case corpus (duplicates, out-of-order, IPv6, future timestamps, sparse fields)
- [x] Define concurrency + batch sizes (ramp, steady, burst)
- [x] Define SLAs (P50/P95/P99, throughput, error rate)
- [x] Define resource ceilings (CPU %, RSS/working set, disk IO) for local runs
- [x] Documented in `PHASE4-load-profile.md`
- [x] Curate Sentinel-relevant corpora (exclude full-project TestData not currently normalized by Sentinel) (see `reports/curated_manifest_validation.md`)
- [x] Curated manifest stored in `testdata/curated_manifest.json`

## 1B) Phase-4 Closure Items (Blocking "100% Standalone E2E")
- [x] Telemetry: normalize Zeek-style JSONL (`B:\\CyberMesh\\TestData\\clean\\deepflow\\zeek_sampled.jsonl`) into `NetworkFlowFeaturesV1` (verified via `reports/final_p4_20260210_telemetry_zeek_*_v3.ndjson` and determinism `reports/final_p4_20260210_det_telemetry_zeek_v3.md`).
- [x] Telemetry: support AWS VPC Flow Logs text format (`B:\\CyberMesh\\TestData\\clean\\flows\\cloud\\aws_vpc_flows.log`) as a first-class ingestion path (verified via `reports/final_p4_20260210_telemetry_aws_vpc_parallel_v1.ndjson`).
- [x] Define Phase-4 SLAs based on current baseline and desired production targets; then re-run and mark pass/fail (see `sentinel/config/phase4_sla_v1.json`).
- [x] Produce SLA pass/fail table from real gateway runs (see `reports/phase4_sla_eval_v1.md`).
- [x] Sigma `.txt` artifact decision: v1 text-ingest adapter path implemented (adapter spec `sentinel/config/adapter_sigma_artifact_text.json`, evidence `reports/final_p4_20260210_oss_sigma_artifact_text.ndjson`).
- [ ] ML model reproducibility: address sklearn pickle version mismatch (pin runtime, or re-export/retrain models), then re-run baseline.

## 2) Load Harness
- [x] Add a Phase-4 load runner script (repeatable, deterministic seed)
- [x] Support parallel vs sequential toggles
- [x] Support per-modality and mixed-modality runs
- [x] Support JSONL/NDJSON inputs (streaming read, bounded memory)
- [x] Emit machine-readable reports (json) + human summary (md)
- [x] Capture run metadata (host, python, platform, config toggles)

## 3) Execute Load Tests (Local)
- [x] Step-load run (ramp up concurrency / rate)
- [x] Steady-state run (sustained duration)
- [x] Burst run (short spikes)
- [x] Store reports in `reports/phase4_loadtest_*.json` and `.md`

## 3B) E2E Real-Corpus Replay Gate (Wiring + Behavior)
- [x] Add E2E replay runner that uses real corpora (no synthetic-only)
- [x] Assert expected agents executed (per-agent metadata buckets in output)
- [x] Assert no silent failures (errors surfaced OR degraded flagged)
- [x] Assert deterministic outputs for identical inputs (ignore timestamps)
- [x] Store E2E summary in `reports/phase4_e2e_*.json` and `.md`
- [x] Run gateway E2E replay via `main.py` (parallel + sequential) and store artifacts:
- [x] Coverage: `reports/final_p4_20260210_gateway_coverage.md`
- [x] Determinism diffs: `reports/final_p4_20260210_det_*.md`

## 4) Validation + Limits Under Load
- [x] Oversize inputs rejected with stable error codes (see `reports/phase4_hardening_summary.md`)
- [x] Malformed JSON/NDJSON rejected with stable error codes (see `reports/phase4_hardening_summary.md`)
- [x] Missing `tenant_id` rejected/quarantined (per ingest policy; strict mode) (see `reports/phase4_hardening_summary.md`)
- [x] Parse timeouts enforced (no hangs) (see `sentinel/tests/test_oss_adapter_timeout.py`)
- [x] Deep sanitization prevents log/metadata bloat (nested truncation) (see `reports/phase4_hardening_summary.md`)
- [x] IPv6 records accepted/rejected deterministically (no crashes) (see `reports/phase4_hardening_summary.md`)
- [x] Future timestamps handled deterministically (flagged at ingest, no crashes) (see `reports/phase4_hardening_summary.md`)
- [x] Duplicate records handled deterministically (dedupe + error codes where applicable) (see `reports/phase4_hardening_summary.md`)
- [x] Out-of-order timestamps handled deterministically (no crashes) (see `reports/phase4_hardening_summary.md`)

## 5) OPA Gate (Monitor-Only) Hardening
- [x] OPA reachable: policy decision recorded in output metadata (see `reports/opa_reachable_summary.md`)
- [x] OPA unreachable: fail-open behavior verified and documented (see `reports/phase4_hardening_summary.md`)
- [x] OPA latency: bounded timeout and clear error surfaced (no silent failure) (see `reports/phase4_hardening_summary.md`)
- [x] Policy caching behavior verified as v1 behavior (see `tests/test_opa_cache.py`)
- [x] Telemetry CLI path applies OPA/tenant context via `SentinelOrchestrator.analyze_event()` (`main.py --telemetry`)

## 6) Reliability / Degraded Mode
- [x] Induce agent timeout: pipeline returns degraded and surfaces errors (see `reports/phase4_reliability_summary.md`)
- [x] Induce partial agent failure: aggregation succeeds and flags degraded (see `reports/phase4_reliability_summary.md`)
- [x] No silent drop of errors (errors always surfaced) (see `reports/phase4_reliability_summary.md`)

## 7) Security Hardening Checks
- [x] Attestation required mode: reject missing/invalid signatures (see `reports/phase4_hardening_summary.md`)
- [x] Redaction verified: no secrets/PII emitted in logs or raw_context (best-effort) (see `reports/phase4_hardening_summary.md`)
- [x] Poisoning resistance: very long strings/nested arrays do not crash or explode memory (see `reports/phase4_hardening_summary.md`)
- [x] Deterministic merge rules validated under high concurrency (see `reports/merge_determinism_stress.md`)

## 8) Documentation + Exit Gate
- [ ] Update `PRD-oss-integration.md` Phase 4 status with measured results
- [x] Create `reports/phase4_hardening_summary.md` including:
- [x] SLA pass/fail table (see `reports/phase4_sla_eval_v1.md`)
- [x] Key bottlenecks and mitigations (if any) (see `reports/phase4_benchmark_summary_20260210.md`)
- [x] Known limits and operator guidance (see `reports/phase4_benchmark_summary_20260210.md`)
- [x] Benchmark suite uses real corpora and stores reports (see `reports/phase4_e2e_20260210_201134.md`, `reports/phase4_sla_eval_v1.md`)

## Phase 4 Exit Criteria
- [x] Load tests completed and reports stored (see `reports/phase4_loadtest_*.md` / `.json`)
- [x] SLAs met for the defined load profile (see `reports/phase4_sla_eval_v1.md`)
- [x] OPA monitor-only behavior verified (reachable/unreachable/timeout) (see `reports/opa_reachable_summary.md`, `reports/phase4_hardening_summary.md`, `tests/test_opa_cache.py`)
- [x] Validation + limits proven under load (no hangs, bounded resource use) (see `reports/phase4_hardening_summary.md`)
- [x] Degraded mode and error reporting proven under induced failures (see `reports/phase4_reliability_summary.md`)

## Known Gaps / Follow-Ups (Not Blocking Phase-4 E2E Proof)
- OSS adapter runs should not require initializing the FILE fast-path (YARA/signatures). Implemented via lazy file-engine init in `sentinel/sentinel/agents/orchestrator.py`.
- Sigma output (`oss_outputs/incoming/sigma_output_esql.txt`) is a rules/query artifact; v1 text ingest is supported via `--adapter-format text` and `sentinel/config/adapter_sigma_artifact_text.json`.
- Telemetry normalization for Zeek-style JSONL (`TestData/clean/deepflow/zeek_sampled.jsonl`) is aligned to `NetworkFlowFeaturesV1` (see `reports/final_p4_20260210_telemetry_zeek_*_v3.ndjson`).
- Telemetry CSV ingest supports AWS VPC flow-log text format (`TestData/clean/flows/cloud/aws_vpc_flows.log`) (see `reports/final_p4_20260210_telemetry_aws_vpc_parallel_v1.ndjson`).
