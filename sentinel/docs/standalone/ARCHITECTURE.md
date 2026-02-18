# Sentinel Standalone Architecture

Last updated: 2026-02-17

## What this service is

Sentinel is the detection engine. It can run in two modes:

- CLI mode (`sentinel/main.py`) for local/batch scans.
- Kafka worker mode (`sentinel/scripts/kafka_gateway.py`) for streaming telemetry.

The core analyzer is shared in both modes (`SentinelOrchestrator.analyze_event`).

## Full flow: Telemetry -> Sentinel

```text
IPFIX/flow traffic
  -> telemetry-layer gateway adapter (UDP :2055)
  -> Kafka topic telemetry.flow.v1 (protobuf FlowV1)
  -> Sentinel kafka gateway (decode + normalize)
  -> Sentinel orchestrator (route + run agents)
  -> Kafka topic sentinel.verdicts.* (result envelope)
  -> (optional) sentinel.verdicts.*.dlq on decode/validation errors
```

### Runtime wiring in this repo

- Telemetry gateway adapter deploy:
  - `k8s_azure/telemetry/layer/02-gateway-adapter.yaml`
  - publishes to `telemetry.flow.v1`
- Sentinel Kafka replay worker:
  - `k8s_azure/sentinel/sentinel-kafka-telemetry-replay-job.yaml`
  - consumes `telemetry.flow.v1`
  - publishes `sentinel.verdicts.e2e.attack.v3`

## Sentinel components

- Ingest/Decode
  - `sentinel/sentinel/kafka/telemetry_decoder.py`
  - decodes `flow_v1`, `cic_v1`, `deepflow_v1`
- Routing + orchestration
  - `sentinel/sentinel/agents/orchestrator.py`
  - `sentinel/sentinel/agents/graph.py`
- Detection agents (current)
  - File path: `static_agent`, `malware_agent`, `script_agent`, `ml_agent` (optional)
  - Flow path: `flow_agent`, `telemetry_threat_intel`
  - Event path: `sequence_risk_agent`, `mcp_runtime_agent`, `exfil_dlp_agent`, `resilience_agent`
  - Scanner/rules path: `scan_findings_agent`, `rules_hit_agent`
- Policy gate (monitor mode)
  - `sentinel/sentinel/opa/gate.py`

## Inputs and outputs

Inputs:
- Files/folders (CLI)
- Telemetry payloads (CLI JSON/CSV; JSON-like IPFIX path)
- Kafka telemetry streams (worker)
- OSS adapter outputs

Outputs:
- CLI JSON or NDJSON artifacts
- Kafka result envelope (`payload_type=sentinel_result`, `schema_version=sentinel.result.v1`)
- Ingest summaries and DLQ records for bad records

## Execution model

- Parallel mode (default): independent agents run concurrently, then deterministic merge.
- Sequential mode: fixed order for debugging/baselines.

Toggle:
- CLI: `--sequential`
- Kafka path: `SENTINEL_INGEST_MODE`

## Guardrails

- Schema/field validation before analysis.
- Bounded errors and degraded metadata; no silent drop in Sentinel layer.
- OPA is monitor-only in standalone.
- Kafka path supports topic/schema/encoding maps via env.

## Current known gap

- End-to-end is functional and producing verdicts, but many telemetry events still show `flow_duration=0` and are marked degraded.
- Detection fallback for runtime flow counters is enabled, so malicious signals still surface, but upstream duration propagation still needs cleanup.
