# CyberMesh Telemetry Layer

Telemetry Layer ingests network signals (flows, IDS alerts, PCAP requests), normalizes into canonical schemas, aggregates into time windows, derives CIC features, and publishes to Sentinel/AI consumers.

This folder contains **production code** (Go + Python) and **test gates** (scripts) used to validate the end-to-end telemetry pipeline before deployment.

## What’s In Here

- `adapters/` (Go)
  - Normalize external sources into canonical telemetry events.
  - Includes gateway adapter inputs (normalized/mapped/cloud/IPFIX) and IDS adapters (Zeek/Suricata) for deepflow.
- `stream-processor/` (Go)
  - Window/aggregate `flow.v1` into `flow.agg.v1`, optional deepflow bridge.
  - DLQ on decode/validation/schema-registry issues.
- `feature-transformer_python/` (Python)
  - Transform `flow.agg.v1` into `cic.v1` features (`telemetry.features.v1`).
  - Emits feature coverage/mask and DLQ on transform/coverage errors.
- `pcap-service/` (Go)
  - Consume `pcap.request.v1` and emit `pcap.result.v1` (+ DLQ) with strict allowlists and guardrails.
- `proto/` + `schemas/`
  - Canonical Protobuf contracts and JSON schemas used by the pipeline.
- `scripts/`
  - Operational helpers (topic creation, schema registration) and **gates** (deterministic validations).
- `env/`
  - Test env files used to ensure Telemetry/AI/Backend are reading the same Kafka creds/topic overrides.

## Kafka Topics (Canonical)

Telemetry:
- `telemetry.flow.v1` / `telemetry.flow.v1.dlq`
- `telemetry.flow.agg.v1`
- `telemetry.features.v1` / `telemetry.features.v1.dlq`
- `telemetry.deepflow.v1` / `telemetry.deepflow.v1.dlq`
- `pcap.request.v1`
- `pcap.result.v1` / `pcap.result.v1.dlq`

Downstream:
- Sentinel consumes `telemetry.flow.v1` / `telemetry.deepflow.v1` and publishes `sentinel.verdicts.v1`.
- AI consumes `sentinel.verdicts.v1` (integrated path) and can also consume `telemetry.features.v1` (direct model path).

## Configuration

For local validation, use the shared test env:
- `env/integration_test.env`

This keeps Kafka creds/topics consistent across Telemetry, AI-service, and backend during smoke runs.

Important env knobs (common):
- Kafka: `KAFKA_BOOTSTRAP_SERVERS`/`KAFKA_BROKERS`, `KAFKA_TLS_ENABLED`, `KAFKA_SASL_ENABLED`, `KAFKA_SASL_MECHANISM`, `KAFKA_SASL_USERNAME`, `KAFKA_SASL_PASSWORD`
- Stream processor: `KAFKA_INPUT_TOPIC(S)`, `KAFKA_OUTPUT_TOPIC`, `KAFKA_DLQ_TOPIC`, `AGGREGATION_WINDOW_SEC`
- Feature transformer: `FEATURE_INPUT_TOPIC`, `FEATURE_OUTPUT_TOPIC`, `FEATURE_DLQ_TOPIC`, `FEATURE_MIN_COVERAGE`, `FEATURE_INPUT_ENCODING`, `FEATURE_OUTPUT_ENCODING`
- Schema registry (optional): `SCHEMA_REGISTRY_ENABLED`, `SCHEMA_REGISTRY_URL`, `SCHEMA_REGISTRY_USERNAME`, `SCHEMA_REGISTRY_PASSWORD`
- PCAP service: `PCAP_REQUEST_TOPIC`, `PCAP_RESULT_TOPIC`, `PCAP_RESULT_DLQ_TOPIC`, `PCAP_DRY_RUN`, `PCAP_ALLOWED_TENANTS`, `PCAP_ALLOWED_REQUESTERS`, `PCAP_MAX_DURATION_MS`, `PCAP_MAX_BYTES`

## Definition of Done (Core Pipeline)

Code is considered “done” (excluding deployment) when the following gates pass:

### Gate A1: Telemetry -> AI policy publish
- `python telemetry-layer/scripts/e2e_telemetry_to_ai_detection_one_flow.py`

### Gate A2: AI policy -> backend/enforcement ACK
- `python telemetry-layer/scripts/gate_telemetry_to_ack_one_flow.py`

### Gate B/C: Flow pipeline correctness
- Gate B (DLQs: strict coverage + invalid payload):
  - `python telemetry-layer/scripts/gates/gates_flow_pipeline.py --env env/integration_test.env --gate c`
- Gate C (batch replay, counts + latency):
  - `python telemetry-layer/scripts/gates/gates_flow_pipeline.py --env env/integration_test.env --gate d --count 200 --timeout-sec 240`

### Gate E: Deepflow + PCAP combined
- `make gate-deepflow-pcap`

Logs:
- Flow gates: `telemetry-layer/test-logs/gates-flow-pipeline/`
- Deepflow+PCAP gate: under `telemetry-layer/test-logs/` (run-specific folder)

## Unified End-to-End Validation

Run the consolidated validator (required scenarios `A1`, `A2`, `B`, `C`, `E`, `F_G`):
- `make validate-core`

Run extended profile (adds optional dataplane proofs for Cilium/Gateway/HostFW):
- `make validate-full`

Artifacts:
- report artifacts are written under `telemetry-layer/test-logs/` (run-specific folder)
- scenario logs in the same folder

## Docs

- Developer gate runbook: `telemetry-layer/docs/GATE_VALIDATION_DEV_GUIDE.md`
- Telemetry LLD: `docs/design/LLD-telemetry-layer.md`
- Implementation status: `telemetry-layer/docs/IMPLEMENTATION_STATUS.md`
- Pending infra/deploy: `telemetry-layer/docs/PENDING_DEPLOYMENT_TASKS.md`
