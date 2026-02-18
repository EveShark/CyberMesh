# Sentinel Standalone LLD

Last updated: 2026-02-17

## Entry points

- CLI:
  - `sentinel/main.py`
- Kafka worker:
  - `sentinel/scripts/kafka_gateway.py`
  - uses `sentinel/sentinel/kafka/gateway.py`

Both paths call:
- `SentinelOrchestrator.analyze_event()`

## Kafka worker internals

Config loader:
- `sentinel/sentinel/kafka/config.py`

Core config keys used in live wiring:
- `ENABLE_KAFKA=true`
- `KAFKA_BOOTSTRAP_SERVERS`
- `KAFKA_INPUT_TOPICS`
- `KAFKA_TOPIC_SCHEMA_MAP` (example: `telemetry.flow.v1:flow_v1`)
- `KAFKA_TOPIC_ENCODING_MAP` (example: `telemetry.flow.v1:protobuf`)
- `KAFKA_OUTPUT_TOPIC`
- `KAFKA_DLQ_TOPIC`
- `KAFKA_CONSUMER_GROUP_ID`
- `KAFKA_TLS_ENABLED`, `KAFKA_SASL_ENABLED`

Topic decoder:
- `sentinel/sentinel/kafka/telemetry_decoder.py`
- supports:
  - `flow_v1` -> `network_flow`
  - `cic_v1` -> `network_flow`
  - `deepflow_v1` -> `scan_findings`

## Telemetry normalization path

1. Consume record from Kafka.
2. Decode bytes by topic encoding/schema map.
3. Build canonical event:
   - `build_flow_event()` for flow telemetry
   - `build_scan_findings_event()` for deepflow findings
4. Route by modality in orchestrator.
5. Merge findings deterministically.
6. Publish result envelope.

Error path:
- decode/validation failures are published to DLQ with bounded metadata.

## CLI telemetry path

`sentinel/main.py` supports:
- `--telemetry --format json`
- `--telemetry --format csv`
- `--telemetry --format ipfix` (JSON-like adapter path, not raw UDP capture)

For raw UDP IPFIX, use telemetry-layer gateway adapter and Kafka worker mode.

## Agent execution details

Routing/graph:
- `sentinel/sentinel/agents/graph.py`

Orchestrator:
- `sentinel/sentinel/agents/orchestrator.py`

Flow scoring logic:
- `sentinel/sentinel/agents/telemetry_agent.py`
- runtime profile enabled for `telemetry.flow*` source topics
- counter-based fallback when `flow_duration == 0`

## Determinism and degraded behavior

Deterministic merge:
- stable merge keys and conflict resolution in orchestrator/merge utilities

Degraded semantics:
- analysis still returns result
- degraded metadata is attached instead of silent failure

## Telemetry-layer integration points (current)

Upstream adapter:
- `telemetry-layer/adapters/internal/adapter/ipfix.go`
- parser:
  - `telemetry-layer/adapters/internal/parser/ipfix.go`

Current guard added:
- IPFIX telemetry gate rejects invalid timing (`timing_known=false` or `duration_ms<=0`) with explicit DLQ reason.

## Tests that back this design

Telemetry-layer:
- `telemetry-layer/adapters/internal/parser/ipfix_test.go`
- `telemetry-layer/adapters/internal/adapter/ipfix_gate_test.go`

Sentinel:
- `sentinel/tests/test_kafka_gateway.py`
- `sentinel/tests/test_kafka_telemetry_decoder.py`
- `sentinel/tests/test_telemetry_runtime_profile.py`

## Known live issue

- In live replay, many flow verdicts still carry `flow_duration=0` and degraded flags.
- Runtime fallback now allows malicious scoring from counters, but duration propagation should still be fixed at source for clean non-degraded telemetry.
