# Architecture 0: Runtime Defaults
## Source-of-Truth Defaults Used by Current Code

**Last Updated:** 2026-02-25

This file captures runtime defaults that frequently drift in higher-level docs.

## 1. Policy ACK Topic

Current code defaults:
- AI: `TOPIC_CONTROL_POLICY_ACK=control.enforcement_ack.v1`
  - `ai-service/src/config/loader.py`
- Backend: `CONTROL_POLICY_ACK_TOPIC=control.enforcement_ack.v1`
  - `backend/pkg/control/policyack/config.go`
- Enforcement agent: `ACK_TOPIC=control.enforcement_ack.v1`
  - `enforcement-agent/internal/config/config.go`
- Policy canary tool: `--ack-topic control.enforcement_ack.v1`
  - `enforcement-agent/cmd/policy-canary/main.go`

Topic naming is fully configuration-driven; any non-default name must be set consistently in AI/backend/agent env.

## 2. Enforcement Backends

Supported `ENFORCEMENT_BACKEND` values in current code:
- `cilium`
- `gateway`
- `iptables`
- `nftables`
- `kubernetes` / `k8s`
- `noop`

Source:
- `enforcement-agent/internal/enforcer/enforcer.go`

## 3. Telemetry Topics (Core)

Current telemetry pipeline defaults:
- `telemetry.flow.v1`
- `telemetry.flow.agg.v1`
- `telemetry.features.v1`
- `telemetry.deepflow.v1`
- `pcap.request.v1`
- `pcap.result.v1`

DLQs:
- `telemetry.flow.v1.dlq`
- `telemetry.features.v1.dlq`
- `telemetry.deepflow.v1.dlq`
- `pcap.result.v1.dlq`

Primary sources:
- `telemetry-layer/stream-processor/cmd/processor/main.go`
- `telemetry-layer/feature-transformer_python/main.py`
- `telemetry-layer/pcap-service/cmd/pcap/main.go`
- `telemetry-layer/adapters/cmd/*/main.go`

## 4. Sentinel Integration Topics (Current)

Sentinel input has two layers of defaults:

Code fallback defaults (worker config):
- `telemetry.features.v1` -> Sentinel input fallback when no input topic env is set
  - `sentinel/sentinel/kafka/config.py`

Current integration deployment override:
- `telemetry.flow.v1` -> Sentinel input (explicitly configured)
  - `k8s_azure/sentinel/sentinel-kafka-ai-integration-job.yaml`

Sentinel output path:
- `sentinel.verdicts.v1` -> AI Sentinel adapter input
- `sentinel.verdicts.v1.dlq` -> Sentinel DLQ

Primary sources:
- `sentinel/sentinel/kafka/config.py`
- `sentinel/sentinel/kafka/gateway.py`
- `k8s_azure/sentinel/sentinel-kafka-ai-integration-job.yaml`
- `ai-service/src/service/sentinel_adapter.py`

## 5. AI Sentinel Adapter Gates

Current env gates used by AI service:
- `SENTINEL_ADAPTER_ENABLED`
- `SENTINEL_ADAPTER_MODE` (`shadow` or `prod`)
- `SENTINEL_INPUT_TOPIC` (default: `sentinel.verdicts.v1`)
- `SENTINEL_INPUT_ENCODING` (`protobuf`/`json`/`auto`)
- `SENTINEL_POLICY_ENABLED` (enable Sentinel -> `ai.policy.v1` path)

Primary sources:
- `ai-service/src/service/sentinel_adapter.py`
- `k8s_azure/ai-service/configmap.yaml`

## 6. Backend Control-Policy Outbox Defaults

Current backend defaults (when outbox is enabled):

- `CONTROL_POLICY_OUTBOX_ENABLED=true`
- `CONTROL_POLICY_OUTBOX_LEASE_KEY=control.policy.dispatcher`
- `CONTROL_POLICY_OUTBOX_LEASE_TTL=10s`
- `CONTROL_POLICY_OUTBOX_RECLAIM_AFTER=30s`
- `CONTROL_POLICY_OUTBOX_POLL_INTERVAL=500ms`
- `CONTROL_POLICY_OUTBOX_DRAIN_MAX_DURATION=2s`
- `CONTROL_POLICY_OUTBOX_BATCH_SIZE=100`
- `CONTROL_POLICY_OUTBOX_BATCH_SIZE_MIN=25`
- `CONTROL_POLICY_OUTBOX_BATCH_SIZE_MAX=400`
- `CONTROL_POLICY_OUTBOX_ADAPTIVE_BATCH=true`
- `CONTROL_POLICY_OUTBOX_MAX_IN_FLIGHT=8`
- `CONTROL_POLICY_OUTBOX_MARK_WORKERS=4`
- `CONTROL_POLICY_OUTBOX_INTERNAL_QUEUE_SIZE=256`
- `CONTROL_POLICY_OUTBOX_DRAIN_BATCHES=4`
- `CONTROL_POLICY_OUTBOX_MAX_RETRIES=8`
- `CONTROL_POLICY_OUTBOX_RETRY_INITIAL=250ms`
- `CONTROL_POLICY_OUTBOX_RETRY_MAX=30s`
- `CONTROL_POLICY_OUTBOX_RETRY_JITTER_RATIO=0.2`
- `CONTROL_POLICY_OUTBOX_LOG_THROTTLE=5s`

Primary source:
- `backend/pkg/wiring/service.go`

## 7. Backend ACK Consumer Worker Defaults

Current backend ACK consumer defaults:

- `CONTROL_POLICY_ACK_WORKERS=4`
- `CONTROL_POLICY_ACK_WORK_QUEUE_SIZE=256`
- `CONTROL_POLICY_ACK_SOFT_THROTTLE_ENABLED=true`
- `CONTROL_POLICY_ACK_SOFT_THROTTLE_SLEEP=200ms`
- `CONTROL_POLICY_ACK_SOFT_THROTTLE_WINDOW=3`

Primary source:
- `backend/pkg/control/policyack/config.go`
