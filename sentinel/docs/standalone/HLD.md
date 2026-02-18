# Sentinel Standalone HLD

Last updated: 2026-02-17

## Service boundary

Sentinel owns:
- event normalization into canonical shape
- modality routing
- multi-agent analysis
- deterministic merge
- result emission

Sentinel does not own:
- raw packet capture/export (telemetry-layer adapters own that)
- enforcement-plane consensus/backend

## System view

```text
[Telemetry Layer] ----Kafka----> [Sentinel Worker] ----Kafka/NDJSON----> [Consumers]
       |                               |
       +---- UDP/IPFIX ingest          +---- shared core with CLI mode

[CLI/Batch Inputs] --------------------> [Sentinel Core] ----NDJSON/JSON----> [Reports]
```

## Primary production path (telemetry)

1. telemetry-layer gateway adapter receives UDP IPFIX on `:2055`.
2. adapter validates/maps and publishes `FlowV1` protobuf to `telemetry.flow.v1`.
3. Sentinel Kafka worker consumes that topic.
4. Sentinel decoder maps topic payload -> `CanonicalEvent(modality=network_flow)`.
5. Orchestrator runs flow path agents (`flow_agent`, `telemetry_threat_intel`, coordinator merge).
6. Sentinel publishes `sentinel.result.v1` envelope to output topic.
7. Bad records go to DLQ with structured error context.

## Agent architecture

Routing is by modality:

- `file`
  - `static_agent`, `malware_agent`, `script_agent`, `ml_agent` (optional)
- `network_flow`
  - `flow_agent`, `telemetry_threat_intel`
- `action_event`
  - `sequence_risk_agent`
- `mcp_runtime`
  - `mcp_runtime_agent`
- `exfil_event`
  - `exfil_dlp_agent`
- `resilience_event`
  - `resilience_agent`
- `scan_findings` / `rules_hit`
  - `scan_findings_agent`, `rules_hit_agent`

## Execution strategy

- Default: parallel where safe, then deterministic merge.
- Optional: sequential for debugging and baselines.
- Both modes use same output contract.

## Data contracts (high level)

Ingress contracts:
- Kafka telemetry topics with explicit schema/encoding maps.
- CLI telemetry/file/adapter inputs.

Internal contract:
- `CanonicalEvent` envelope with modality + feature payload.

Egress contract:
- `sentinel.result.v1` envelope (`payload_type=sentinel_result`).
- DLQ envelope for parse/validation/decode failures.

## Reliability and security posture

- strict validation at boundaries
- bounded error payloads
- degraded mode on partial failure (analysis still returns)
- OPA monitor gate optional; no hard block in standalone
- explicit Kafka TLS/SASL toggles

## Known runtime constraint (current)

- Telemetry flow duration is still often zero at Sentinel input in live replay, so events are marked degraded.
- Runtime fallback scoring is enabled to reduce false-clean outcomes while duration propagation is being fixed upstream.
