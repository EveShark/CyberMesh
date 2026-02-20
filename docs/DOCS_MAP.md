# CyberMesh Docs Map

Purpose: one-page navigation for developers and operators.

## Start Here

- `README.md` - repo entry point, service links, deployment scope split.
- `docs/ARCHITECTURE.md` - architecture portal.
- `docs/ARCHITECTURE_INDEX.md` - full architecture/design index.

## Architecture

- `docs/architecture/01_system_overview.md` - full E2E control/data flow.
- `docs/architecture/02_ai_detection_pipeline.md` - AI detection path.
- `docs/architecture/03_hotstuff_consensus.md` - backend consensus.
- `docs/architecture/04_kafka_message_bus.md` - topics and contracts.
- `docs/architecture/05_state_machine.md` - deterministic execution.
- `docs/architecture/06_feedback_loop.md` - feedback path.
- `docs/architecture/09_security_model.md` - signing, replay, trust.
- `docs/architecture/13_sentinel_integration.md` - sentinel in the main path.

## Design (HLD/LLD)

- `docs/design/HLD.md` - system HLD.
- `docs/design/DATA_FLOW.md` - sequence-level flow.
- `docs/design/DEPLOYMENT.md` - deployment topology and rollout notes.
- `docs/design/LLD-telemetry-layer.md` - telemetry internals.
- `docs/design/LLD-sentinel.md` - sentinel internals.
- `docs/design/LLD-ai-service.md` - AI service internals.
- `docs/design/LLD-backend.md` - backend internals.
- `docs/design/LLD-enforcement-agent.md` - enforcement internals.
- `docs/design/LLD-frontend.md` - frontend internals.

## Service READMEs

- `telemetry-layer/README.md`
- `sentinel/README.md`
- `ai-service/README.md`
- `backend/README.md`
- `enforcement-agent/README.md`
- `cybermesh-frontend/README.md`

## Validation & Gates

- `telemetry-layer/docs/GATE_VALIDATION.md` - gate model and run order.
- `k8s_azure/testing/README.md` - cluster test harness usage.
- `k8s_azure/testing/REAL_DATA_PIPELINE_GUIDE.md` - blob-based real data path.
- `k8s_azure/testing/SYNTHETIC_TRAFFIC_PIPELINE_GUIDE.md` - synthetic path.

## Definition of Done

- `docs/DEFINITION_OF_DONE.md` - release-readiness checklist for full pipeline.
