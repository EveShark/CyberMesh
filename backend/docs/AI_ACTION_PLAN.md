# CyberMesh AI Service — Action Plan (Updated)

## 1) Purpose & boundary
- Python AI detects anomalies/evidence/policy, signs each message (Ed25519 + nonce), and publishes to Kafka for the Go backend to order/commit. It consumes backend feedback. No P2P, no consensus, no ledger authority.

## 2) Kafka contracts (Protobuf)
- Producers (AI → Backend):
  - ai.anomalies.v1: { id, type, source, severity, confidence, ts, content_hash, payload, model_version, producer_id, nonce, signature, pubkey }
  - ai.evidence.v1: { id, evidence_type, refs, proof_blob, ts, producer_id, nonce, signature, pubkey }
  - ai.policy.v1: { id, action, rule, params, ts, producer_id, nonce, signature, pubkey }
- Consumers (Backend → AI):
  - control.commits.v1, control.reputation.v1, control.policy.v1, control.evidence.v1
- Canonical encoding with content_hash = SHA‑256(Protobuf bytes). TLS 1.3 + SASL; idempotent producers; DLQ for permanent failures.

## 3) Model focus (current datasets)
- DDoS/DoS: flow/window features (pps, bps, SYN/ACK ratio, unique dst/ports, inter‑arrival) with LightGBM/XGBoost baseline; add a lightweight anomaly model (IsolationForest/Autoencoder) for zero‑day; calibrate probabilities; target high recall at very low FPR.
- Malware: static EMBER‑style features (sections/imports/byte histograms) with LightGBM baseline; optional behavioral (API‑call TF‑IDF + LR/GBDT) if telemetry exists; calibrate; “abstain” on low confidence.
- Ensemble: rules + ML with weighted vote and per‑class thresholds.

## 4) Sprint AI‑A — Foundations
- Protobuf contracts + codegen; validators; size/timestamp checks; deterministic content_hash.
- Security: signer (Ed25519), nonce/replay window, redaction; strict limits.
- IO: Idempotent Kafka producer (TLS 1.3/SASL), DLQ, backpressure; basic consumer scaffold.
- Detection: minimal pipelines for DDoS/DoS and Malware (static) with metrics.
- Platform: env‑based config, structured JSON logs, Prometheus metrics; unit tests.

## 5) Sprint AI‑B — Integration & Ops
- Feedback: consumers for control.*.v1 to tune thresholds/metrics.
- Models: signed/versioned artifacts; hot‑reload (blue/green, instant rollback on validation failure).
- API: health/readiness and secured admin (JWT and/or mTLS), rate limits.
- Delivery: Docker/K8s manifests; E2E test AI → Kafka → Backend → feedback; SLO dashboards.
- Optional (deferred): Redis cache for features/metadata (non‑authoritative).

## 6) Security baselines
- TLS 1.3 everywhere; Ed25519 signing; nonce + replay window; timestamp skew enforcement; size caps; rate limits.
- Admin API secured with JWT; mTLS optional. AES‑256‑GCM for encrypting stored secrets/artifacts. No secrets in logs; strict redaction.

## 7) Configuration
- 12‑factor env‑based config with typed validation; secrets via env/secret manager; distinct dev/stage/prod profiles.

## 8) Monitoring & SLOs
- Prometheus metrics: publish latency, retries, DLQ, signature failures, pipeline latency. Structured JSON logs with redaction. Health/readiness endpoints and basic alerts.

## 9) Acceptance criteria
- All outbound messages are schema‑valid, signed, within size/time limits, and produced idempotently. Hot‑reload is safe with rollback. E2E loop (AI → Kafka → Backend → feedback) is stable with metrics and alerts.

## 10) Out of scope
- P2P/libp2p, consensus, authoritative DB/ledger, and in‑service training pipelines (training remains out‑of‑band).
