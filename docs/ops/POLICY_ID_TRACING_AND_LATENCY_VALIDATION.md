# Policy ID Tracing And Latency Validation

## Purpose

This document is the operational source of truth for:

- tracing one `policy_id` end to end
- understanding which latency numbers are exact vs approximate
- running scenario-based validation
- comparing script output against exact per-policy traces

This is not a design-history document. It is a practical guide for operators and engineers validating the live policy pipeline.

## Scope

The policy pipeline covered here is:

`Telemetry -> Sentinel -> AI -> Backend -> Control -> ACK`

This document focuses on:

- `policy_id` tracing
- exact policy latency
- script-based scenario validation
- interpreting aggregate dashboard metrics safely

## Architecture Summary

The current production path is:

1. telemetry adapters publish `telemetry.flow.v1`
2. Sentinel consumes telemetry and publishes `sentinel.verdicts.v1`
3. AI consumes verdicts and publishes `ai.policy.v1`
4. backend consumes AI policy events
5. backend reaches commit and writes `control_policy_outbox`
6. backend publishes `control.policy.v2`
7. enforcement consumes control policy and publishes `control.enforcement_ack.v1`
8. backend stores/serves ACK state

Important topics:

- `telemetry.flow.v1`
- `sentinel.verdicts.v1`
- `ai.policy.v1`
- `control.policy.v2`
- `control.enforcement_ack.v1`

Important note:

- `control.policy.v2` is the active control topic for scoped/sharded routing
- `control.policy.v1` is legacy and should not be used as the primary validation target

## Policy ID Tracing

### Primary source of truth

Use:

- `/api/v1/control/trace/{policyId}`

This is the exact per-policy trace view.

It returns:

- `outbox`
- `acks`
- `runtime_markers`
- `materialized`

The `materialized` section is the one to use for exact stage and latency interpretation.

### How to get a policy ID

Use:

```powershell
Invoke-RestMethod -Uri "https://20.109.229.240/api/v1/control/outbox?limit=10" -SkipCertificateCheck
```

Then choose a fresh `policy_id`.

### How to trace a policy ID

Use:

```powershell
Invoke-RestMethod -Uri "https://20.109.229.240/api/v1/control/trace/<POLICY_ID>" -SkipCertificateCheck
```

Example:

```powershell
Invoke-RestMethod -Uri "https://20.109.229.240/api/v1/control/trace/608b6078-8d40-4801-9ab6-ac486bf52fa5" -SkipCertificateCheck
```

### What to read from the trace

Use these fields first:

- `materialized.stages`
- `materialized.latencies`

Most useful stage values:

- `t_source_event`
- `t_telemetry_ingest`
- `t_sentinel_consume`
- `t_sentinel_emit`
- `t_ai_sentinel_consume`
- `t_ai_decision_done`
- `t_backend_consume`
- `t_commit`
- `t_outbox_row_created`
- `t_control_publish_ack`
- `t_ack`

Most useful derived latencies:

- `source_to_ai_decision`
- `ai_decision_to_outbox_created`
- `outbox_created_to_published`
- `published_to_ack`
- `ai_decision_to_ack`

## Latency Semantics

### Exact per-policy latency

Source:

- `/api/v1/control/trace/{policyId}`

Use this for:

- exact workflow latency
- stage-by-stage causal analysis
- per-policy debugging

This is the highest-signal latency source.

### Script primary latency

Source:

- `k8s_azure/testing/scripts/python/blob_validate.py`

Current primary script block:

- `script_primary_latency_ms`

This is policy-correlated and stronger than topic-pair approximation, but it is still only as good as pair coverage.

Use this for:

- scenario-level comparisons
- quick validation across many runs

Do not treat it as stronger than exact per-policy traces.

### Topic timestamp approximation

Source:

- `topic_timestamp_approx_ms`

Method:

- resampled topic timestamp pairing

This is non-causal.

Use it only for:

- rough topic drift checks
- sanity checks when pair coverage is strong

Do not use it as the source of truth for:

- `ai.policy -> control.policy`
- `control.policy -> ack`

especially when pair counts are sparse.

### Aggregate dashboard metrics

Sources:

- `/api/v1/dashboard/overview`
- `/api/v1/ai/metrics`

These are service or cluster aggregate metrics, not per-policy latency.

Examples:

- `avg_block_time_seconds`
- `network.avg_latency_ms`
- `ai.metrics.loop.avg_latency_ms`

Use them for:

- health
- cluster state
- service runtime monitoring

Do not compare them directly to per-policy trace latency.

## Telemetry Latency Semantics

Telemetry tracing was corrected so that:

- `source_event_ts_ms` remains lineage metadata
- `telemetry_ingest_ts_ms` is the real adapter-side ingest timestamp
- `t_telemetry_ingest` now uses the real ingest timestamp

This means:

- `source_to_ai_decision` is now mostly a source-to-ingest freshness metric plus downstream processing
- `telemetry_to_sentinel_emit` and `sentinel_emit_to_ai_decision` are the real telemetry/Sentinel/AI processing segments

Practical interpretation:

- if `source_to_ai_decision` is high but `telemetry_to_sentinel_emit` and `sentinel_emit_to_ai_decision` are low, the delay is upstream of telemetry ingest, not in telemetry/Sentinel/AI processing

## Frontend vs Backend Metrics

The frontend mostly consumes aggregate data from:

- `/api/v1/dashboard/overview`

and some AI-specific data from:

- `/api/v1/ai/metrics`

These are not the same as per-policy trace latency.

Examples:

- `Avg Block Interval`
  - historical block spacing
  - not current policy latency
- `P2P Avg Peer Latency`
  - router/ping health metric
  - not control-policy latency
- AI loop runtime cards
  - detection-loop runtime/freshness
  - not per-policy end-to-end control latency

If exact workflow latency is needed, use:

- `/api/v1/control/trace/{policyId}`

## Testing Scenarios

### Blob / real-data scenarios

Available families include:

- baseline
- ddos
- port-scan
- east-west
- north-south
- exfiltration
- mixed/netflow
- stress/load
- malware
- lateral-movement
- c2-beacon

Examples:

- `blob-baseline`
- `blob-ddos`
- `blob-port-scan`
- `blob-east-west`
- `blob-north-south`
- `blob-exfiltration`
- `blob-netflow-multi`
- `blob-stress-load`

### Non-blob / synthetic scenarios

Available families include:

- ddos
- port-scan
- east-west
- north-south
- stress-load
- pcap replay
- malware
- exfiltration
- lateral movement

Examples:

- `ddos`
- `port-scan`
- `east-west`
- `north-south`
- `stress-load`
- `pcap-replay`

## Recommended Validation Matrix

Use `10` sequential runs, not parallel.

Recommended set:

1. `blob-baseline`
2. `ddos`
3. `blob-ddos`
4. `blob-port-scan`
5. `east-west`
6. `blob-east-west`
7. `north-south`
8. `blob-north-south`
9. `blob-exfiltration`
10. `stress-load`

Why sequential:

- clean windows
- clean topic deltas
- cleaner `policy_id` sampling
- less backpressure contamination across runs

## Scenario Matrix Result Reference

Current matrix artifacts:

- [scenario_matrix_20260309](B:\CyberMesh\k8s_azure\testing\reports\scenario_matrix_20260309)

Key script-side outcomes:

- `blob-baseline`
  - no policy path triggered
- `blob-ddos`
  - `ai->control p95 ~711 ms`
  - `control->ack p95 ~5 ms`
  - `ai->ack p95 ~2507 ms`
- `blob-port-scan`
  - `ai->control p95 ~983 ms`
  - `control->ack p95 ~11 ms`
  - `ai->ack p95 ~5447 ms`
- `blob-east-west`
  - `ai->control p95 ~1260 ms`
  - `control->ack p95 ~5 ms`
  - `ai->ack p95 ~1382 ms`
- `ddos`
  - `ai->control p95 ~1324 ms`
  - `control->ack p95 ~4 ms`
  - `ai->ack p95 ~3870 ms`
- `east-west`
  - `ai->control p95 ~1505 ms`
  - `control->ack p95 ~4 ms`
  - `ai->ack p95 ~1385 ms`

Outlier script-side results:

- `north-south`
- `blob-north-south`
- `stress-load`

These should not be trusted from script-side policy correlation alone without exact trace sampling.

## Exact Trace Validation Of Outliers

The outlier scenarios were checked against exact policy traces.

### North-south

Sample policy IDs:

- `50f73cea-89f3-4f8f-889b-0ff3404bdd73`
- `c8fc6bdf-9d90-41de-9960-81cf7d81ffe7`
- `eca76792-3f05-4201-a73d-2ec99130cc69`

Exact `ai_decision_to_ack`:

- `1304 ms`
- `1476 ms`
- `1392 ms`

### Blob north-south

Sample policy IDs:

- `44c45abb-ab19-4b2f-a1e3-58be295d39de`
- `638fa644-2711-4456-a9c5-aabaa0845bdf`
- `63697d50-5b54-42a3-8455-6d356396e1a2`

Exact `ai_decision_to_ack`:

- `3506 ms`
- `2993 ms`
- `2472 ms`

### Stress-load

Sample policy IDs:

- `9ba5b807-24df-4fc5-bd5a-d4a048493698`
- `a53017a4-27b8-4e39-96b3-a4fd1309343b`
- `b82242f4-88f6-4e36-a604-afda85a8362a`

Exact `ai_decision_to_ack`:

- `1405 ms`
- `1280 ms`
- `1299 ms`

### Conclusion

The script overstated those workloads because common pair coverage was too sparse.

Use exact per-policy traces as the source of truth for those scenarios.

## Current Practical Interpretation

### What to trust first

1. `/api/v1/control/trace/{policyId}`
2. `script_primary_latency_ms`
3. `topic_timestamp_approx_ms`
4. dashboard aggregate cards

### If the script says a scenario is extremely slow

Do this:

1. pull 3 to 5 fresh `policy_id`s from `/api/v1/control/outbox`
2. fetch `/api/v1/control/trace/{policyId}`
3. compare:
   - `ai_decision_to_outbox_created`
   - `outbox_created_to_published`
   - `published_to_ack`
   - `ai_decision_to_ack`

If those exact traces are normal, the script number is a correlation artifact.

## Operational Playbook

### Get fresh policy IDs

```powershell
Invoke-RestMethod -Uri "https://20.109.229.240/api/v1/control/outbox?limit=10" -SkipCertificateCheck
```

### Trace one policy

```powershell
Invoke-RestMethod -Uri "https://20.109.229.240/api/v1/control/trace/<POLICY_ID>" -SkipCertificateCheck
```

### Get ACK rows for one policy

```powershell
Invoke-RestMethod -Uri "https://20.109.229.240/api/v1/policies/acks?policy_id=<POLICY_ID>" -SkipCertificateCheck
```

### Check aggregate dashboard data

```powershell
Invoke-RestMethod -Uri "https://20.109.229.240/api/v1/dashboard/overview" -SkipCertificateCheck
```

### Check AI loop aggregate metrics

```powershell
Invoke-RestMethod -Uri "https://20.109.229.240/api/v1/ai/metrics" -SkipCertificateCheck
```

## Known Limitations

1. `topic_timestamp_approx_ms` is non-causal.
2. `script_primary_latency_ms` can still be weak when pair coverage is sparse.
3. aggregate dashboard metrics are not per-policy latency.
4. some durable timestamps are coarser than runtime markers and should not be over-interpreted at sub-second precision.

## Current Truth Summary

At the time of this document:

- telemetry tracing semantics are fixed
- per-policy trace is the best latency source
- dashboard and AI loop cards are aggregate health/runtime metrics
- north-south and stress outliers from script output were mostly overstatements from sparse correlation
- exact policy traces show current healthy flows in roughly the `1s` to `3.5s` `ai_decision_to_ack` range depending on scenario

## Next Steps

Recommended next investigations should use exact per-policy trace first for:

- `ai_decision_to_outbox_created`
- `outbox_created_to_published`
- `published_to_ack`

Only after exact per-policy traces confirm a real bottleneck should aggregate or script-side numbers be used to generalize that result.
