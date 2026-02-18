# Pending Items (Non-Standalone)

## Purpose
This document explains what is still **pending** for the full integrated system
(Sentinel + AI-service + Telemetry layer). These items are **not required** for
standalone operation, but they must be completed before claiming end-to-end
production readiness for the integrated stack.

## Scope
Standalone pipeline (file + telemetry graphs) is complete and test‑passing.
The pending items below affect only the **integration** and **runtime
dependencies** that live outside this repo.

## Pending Items (Details)

### 1) Missing CyberMesh adapter/validation modules
**What is missing**
- `src.ml.agents.flow_adapters` (UNSWNB15Adapter, NSLKDDAdapter, CICIDS2017Adapter)
- `src.ml.agents.validation` (validate_file_features)
- `src.ml.telemetry_dns` (DNSLogEntry, DNSTelemetrySource)
- `src.ml.telemetry_file` (FileEvent, FileTelemetrySource)

**Why it matters**
Integration tests that validate AI‑service adapters and telemetry schemas are
skipped because these modules are not present in this repo. Without them, we
cannot verify compatibility with the full CyberMesh telemetry/ML stack.

**What is needed to close**
Provide these modules (or update integration wiring to the correct new paths),
then re‑run the integration tests that currently skip.

### 2) AI‑service / Kafka integration not validated end‑to‑end
**What is missing**
Kafka producer/consumer wiring is configured but not exercised in this repo.

**Why it matters**
We have not validated the real event flow:
Telemetry → Kafka → Sentinel → AI‑service outputs. Until that is verified,
claims of end‑to‑end integration are incomplete.

**What is needed to close**
Run a real Kafka pipeline test with the AI‑service runtime (topics, credentials,
and event schemas), and verify outputs match expectations.

### 3) ML model artifacts not available at expected path
**What is missing**
The models directory expected by default (`../CyberMesh/ai-service/data/models`)
is not present in this repo.

**Why it matters**
ML providers load in degraded mode. Standalone still works, but ML detections
are effectively disabled unless models are supplied.

**What is needed to close**
Provide the model artifacts (or configure `MODELS_PATH`) and verify ML providers
load successfully.

### 4) AI‑service runtime env configuration not exercised here
**What is missing**
Environment variables and runtime setup required by AI‑service are not exercised
in this repo, so API startup and integration‑mode behavior are not validated.

**Why it matters**
This affects production readiness of the integrated stack, not standalone.

**What is needed to close**
Run Sentinel in the AI‑service runtime with required env vars, then run the
integration smoke tests.

## Standalone Status (Confirmed)
- File + telemetry graphs are implemented and working.
- Parallel + sequential execution validated.
- Degraded‑mode reporting present.
- Full test suite in this repo passes (with integration skips noted above).
