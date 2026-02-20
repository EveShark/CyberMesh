# Definition Of Done

This checklist defines when CyberMesh is considered release-ready at system level.

## 1. Contracts And Schema

- Protobuf contracts used across telemetry/sentinel/AI/backend/enforcement are versioned and checked in.
- Topic names and payload formats are consistent across code and manifests.
- DLQ routes exist for malformed/failed payloads.

## 2. Pipeline Wiring

- Telemetry publishes valid flow/features events.
- Sentinel consumes telemetry and publishes verdicts.
- AI consumes features/verdicts and publishes anomalies/evidence/policy candidates.
- Backend validates, orders, and publishes policy outputs.
- Enforcement consumes policy, applies control, and publishes ACK.

## 3. Data And Control Plane

- Data plane backends are selectable and wired: `cilium`, `gateway`, `iptables`, `nftables`, `k8s`.
- Control plane approval path is active for high-risk actions.
- ACK topics are consumed and surfaced for closed-loop confirmation.

## 4. Reliability And Safety

- No silent failure paths: errors are logged with actionable context.
- Idempotency/replay protections are active where required.
- Required retries/backoff are configured for Kafka and external dependencies.
- Startup behavior is deterministic with clear readiness checks.

## 5. Security Baseline

- Signing/verification path is enabled for policy-critical messages.
- Secrets come from env/K8s secrets only (no hardcoded credentials).
- Transport security settings (TLS/SASL/mTLS where configured) are validated.

## 6. Test Coverage And Evidence

- Unit tests for new parsers/mappers/validators and policy backends pass.
- Integration tests for telemetry -> AI and backend -> enforcement pass.
- Cluster validation scenarios in `k8s_azure/testing` pass for:
  - real data flows
  - synthetic traffic flows
  - policy apply/ack loops
- Known limitations are documented with owner + mitigation.

## 7. Documentation Accuracy

- Root `README.md`, service READMEs, and `docs/` reflect current runtime behavior.
- Operational runbooks include startup order, required env keys, and failure triage.
- Diagram flows match the implemented code paths.

## 8. Deployment Readiness

- Images are reproducible and tagged.
- Kubernetes manifests reference current image names/tags and env keys.
- Rollout/rollback steps are documented.
- Observability coverage exists for ingestion lag, DLQ depth, and policy apply outcomes.

## Exit Criteria

System is done when all sections above are satisfied, with reproducible validation evidence and no unresolved blockers on the critical path.
