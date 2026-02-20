# CyberMesh Sentinel Docker Image

This image packages Sentinel as a Kafka gateway worker for the standalone/integrated telemetry path.

## Image

- Repository (example): `<registry>/cybermesh-sentinel`
- Dockerfile: `docker/sentinel/Dockerfile`
- Default command: `python /app/sentinel/scripts/kafka_gateway.py`

## What is included

- `sentinel/sentinel` runtime package
- `sentinel/scripts/kafka_gateway.py`
- Telemetry protobuf Python bindings from `telemetry-layer/proto/gen/python`

The Dockerfile intentionally does **not** copy large/non-runtime paths such as project-level `TestData`.

## Build

From repo root:

```bash
docker build -f docker/sentinel/Dockerfile -t <registry>/cybermesh-sentinel:latest .
```

## Push

```bash
docker push <registry>/cybermesh-sentinel:latest
```

## Local run (quick smoke)

```bash
docker run --rm \
  -e ENABLE_KAFKA=false \
  <registry>/cybermesh-sentinel:latest \
  python /app/sentinel/scripts/kafka_gateway.py --max-messages 1
```

Common registry prefixes:

- `asia-southeast1-docker.pkg.dev/<project>/<repo>`
- `ghcr.io/<org>`
- `<account>.dkr.ecr.<region>.amazonaws.com`
- `<registry>.azurecr.io`
- `<region>.ocir.io/<tenancy>/<repo>`

## Kubernetes usage

Reference manifests:

- `k8s_azure/sentinel/sentinel-kafka-ai-integration-job.yaml`
- `k8s_azure/sentinel/sentinel-kafka-ai-integration-v2-job.yaml`
- `k8s_azure/sentinel/sentinel-kafka-smoke-job.yaml`

These manifests provide the runtime env for:

- Kafka bootstrap/auth
- input topic/schema/encoding map
- output and DLQ topics
- ingest mode (`parallel` or `sequential`)

## Runtime prerequisites

- Kafka reachable from pod
- Input topic populated (`telemetry.flow.v1` by default in integration jobs)
- `TELEMETRY_PROTO_PATH` set (defaults used by manifests)
- Correct topic schema/encoding map in env:
  - `KAFKA_TOPIC_SCHEMA_MAP`
  - `KAFKA_TOPIC_ENCODING_MAP`

## Troubleshooting

- If gateway publishes only DLQ:
  - verify input encoding matches producer (`protobuf` vs `json`)
  - verify `KAFKA_TOPIC_SCHEMA_MAP` for each input topic
- If no output to `sentinel.verdicts.v1`:
  - check orchestrator errors in pod logs
  - confirm required fields (`tenant_id`, timestamp skew, modality/features)
- If protobuf decode fails:
  - confirm telemetry proto bindings are present at `/app/telemetry_proto`
  - confirm producer schema version matches decoder expectations
