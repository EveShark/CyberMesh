# Local Sentinel to AI Replay

This runbook validates the local wiring before AKS rollout.

## Scope

It tests two separate contracts:

1. `telemetry.flow.v1 -> sentinel.verdicts.v1 -> ai-service`
2. `phase3-events -> Sentinel adapter/orchestrator`

These must be split because the live Kafka gateway only consumes `telemetry.flow.v1` protobuf payloads, while `sentinel/testdata/clean/phase3-events` contains adapter-based event data.

## What the harness does

`scripts/local_sentinel_ai_replay.py`:

1. Starts a disposable local Kafka broker in Docker
2. Creates the Sentinel/AI topics
3. Starts the real Sentinel Kafka gateway
4. Starts the real `ai-service`
5. Publishes local flow protobuf fixtures to `telemetry.flow.v1`
6. Injects one known non-clean Sentinel verdict to validate the AI consumer contract
7. Queries:
   - `/detections/history`
   - `/detections/stats`
   - `/detections/suspicious-nodes`
8. Replays the phase3 corpus through adapter mode
9. Writes a report to `tmp/local_sentinel_ai/report.json`
10. Freezes sample contract fixtures under `tmp/local_sentinel_ai/fixtures/`

## Run

From repo root:

```powershell
python scripts/local_sentinel_ai_replay.py
```

Keep the local Kafka broker running for repeated iterations:

```powershell
python scripts/local_sentinel_ai_replay.py --keep-kafka
```

Limit scenarios:

```powershell
python scripts/local_sentinel_ai_replay.py --flow-scenarios ddos,port-scan --phase3-scenarios exfil,resilience
```

## Expected outcomes

Transport contract:

- `telemetry.flow.v1` offset delta > 0
- `sentinel.verdicts.v1` offset delta > 0
- AI history count > 0

Adapter contract:

- `action` should be mostly clean
- `exfil` should produce malicious findings
- `resilience` should produce suspicious/malicious findings
- `mcp` should produce suspicious findings

Frozen artifacts:

- `sample_telemetry_flow.json`
- `sample_sentinel_verdict.json`
- `sample_ai_history_response.json`
- `sample_ai_metrics_response.json`
- `sample_ai_suspicious_response.json`

## Interpretation

If AI history is populated but `detections_total` is still zero:

- the Sentinel consumer path is working
- the detection-loop metrics surface is still separate from Sentinel metrics

If history is populated but `suspicious-nodes` is empty:

- events are reaching AI
- but they still do not carry `validator_id`
- node suspicion cannot be derived from those events yet

If gateway logs show repeated `Starting analysis of unknown`:

- Kafka transport is working
- scenario/profile semantics are weak or missing in the flow payloads

## Current scenario coverage

Flow transport scenarios:

- `baseline`
- `ddos`
- `port-scan`
- `exfil`

Adapter scenarios:

- `action`
- `exfil`
- `resilience`
- `mcp`

## Current edge cases covered

- benign baseline traffic
- non-clean malicious Sentinel verdict injection
- adapter payload size handling for `mcp`
- transport success without assuming validator identity
- separation of gateway semantics from AI consumer semantics
