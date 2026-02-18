## SOP: Decoupling CyberMesh AI-Service & Integrating Sentinel

### Phase 0 – Ground Truth & Guardrails

**Objectives**

- Capture current CyberMesh and Sentinel detection paths.
- Define non‑negotiable constraints so we don’t break the existing AI‑Service.

**Guardrails**

- Do **not** change the behavior of the current CyberMesh AI‑Service DDoS/anomaly pipeline without a feature flag.
- No "magic" dataset swaps: any new source requires an explicit adapter and validation.
- New code must be config‑driven and observable (metrics, logs, health checks).

**Checklist**

- [ ] Document current CyberMesh path:
  - [ ] `PostgresTelemetrySource` → `FlowFeatureExtractor` → `FeatureAdapter` → `Rules/Math/ML` → `EnsembleVoter` → `DetectionPipeline`.
- [ ] Document Sentinel path:
  - [ ] `ProviderRegistry` → providers (`ml`, `static`, `threat_intel`, `llm`) → `AnalysisResult` → voting via `calibrated_config.json`.
- [ ] Confirm no in‑place refactors of CyberMesh core until a prototype is proven in Sentinel.

---

### Phase 1 – Shared Contracts & Canonical Schemas (Design Only)

**Objectives**

- Design shared detection contracts and canonical feature schemas.
- Mirror Threat Intel / LLM design principles for flows and files.

**Deliverables**

1. **Detection Contracts** (language‑agnostic spec; later Python dataclasses):

   - `DetectionCandidate` with fields such as:
     - `threat_type`, `raw_score`, `calibrated_score`, `confidence`.
     - `agent_id`, `signal_id`.
     - `features` (top signals), `metadata`.
   - `EnsembleDecision` with fields such as:
     - `should_publish`, `threat_type`, `final_score`, `confidence`.
     - `candidates`, `profile`, `abstention_reason`, `metadata`.

2. **Canonical Feature Schemas**:

   - `NetworkFlowFeaturesV1` (examples):
     - `src_ip`, `dst_ip`, `src_port`, `dst_port`, `protocol`.
     - `bytes_in`, `bytes_out`, `packets_in`, `packets_out`.
     - `flow_duration_ms`, `pps`, `bpp`, `syn_count`, `ack_count`, `tcp_flag_vector`, etc.
   - `FileFeaturesV1` (examples):
     - `entropy`, `section_entropy_mean`, `import_entropy`.
     - `strings_score_command_execution`, `strings_score_cred_theft`.
     - `yara_score_malware_packers`, `ti_score_ip`, `ti_score_domain`, etc.

3. **Canonical Event Envelope**:

   ```json
   {
     "id": "uuid",
     "timestamp": 1234567890.0,
     "source": "postgres_ddos" | "netflow_collector" | "sentinel_file_agent",
     "modality": "network_flow" | "file" | "dns" | "...",
     "features_version": "NetworkFlowFeaturesV1" | "FileFeaturesV1",
     "features": { "..." },
     "raw_context": { "..." }
   }
   ```

**Checklist**

- [ ] Spec for `DetectionCandidate` / `EnsembleDecision` reviewed and accepted.
- [ ] Spec for `NetworkFlowFeaturesV1` agreed.
- [ ] Spec for `FileFeaturesV1` agreed.
- [ ] Canonical event envelope format agreed.

---

### Phase 2 – Sentinel Prototype: Sentinel as an Agent

**Objectives**

- Implement detection contracts and ensemble logic inside Sentinel only.
- Prove Sentinel can behave like a `DetectionAgent`, independent of CyberMesh.

**Steps**

1. Implement shared types in Sentinel (e.g. `sentinel/agents/types.py`):
   - `ThreatType` enum, `DetectionCandidate`, `EnsembleDecision`.

2. Implement `DetectionAgent` interface in Sentinel:

   ```python
   class DetectionAgent(ABC):
       @abstractmethod
       def input_modalities(self) -> List[str]:
           ...

       @abstractmethod
       def analyze(self, event: Dict) -> List[DetectionCandidate]:
           ...
   ```

3. Implement `SentinelAgent`:
   - For `modality == "file"` and `features_version == "FileFeaturesV1"`:
     - Run configured providers (`malware_pe_ml`, `entropy_analyzer`, `strings_analyzer`, `yara_rules`, `threat_intel`, etc.).
     - Map each `AnalysisResult` to a `DetectionCandidate`.
     - Use `calibrated_config.json` thresholds/weights to set scores and confidence.

4. Implement a minimal `EnsembleVoter` in Sentinel:
   - Config‑driven (JSON/YAML) similar to `calibrated_config.json`.
   - Input: list of `DetectionCandidate`s.
   - Output: `EnsembleDecision`.

5. Wire into Sentinel CLI/tests:
   - Build an event for a file.
   - Run `SentinelAgent` → candidates → `EnsembleVoter` → decision.

**Checklist**

- [ ] `DetectionCandidate` / `EnsembleDecision` implemented in Sentinel.
- [ ] `DetectionAgent` interface implemented.
- [ ] `SentinelAgent` wraps core providers (ml, static, threat_intel, etc.).
- [ ] Mini `EnsembleVoter` implemented using Sentinel calibration config.
- [ ] CLI/tests show full file → providers → candidates → decision path.
- [ ] No CyberMesh changes yet.

---

### Phase 3 – Adapters & Canonical Events

**Objectives**

- Implement adapters for flows/files and event builders in a shared library.
- Start producing canonical events without touching the existing CyberMesh runtime path.

**Steps**

1. Implement flow adapters (library only):
   - `CICDDoS2019Adapter`: CIC rows/dicts → `NetworkFlowFeaturesV1`.
   - Future: `UNSWNB15Adapter`, `NSLKDDAdapter`, `CICIDS2017Adapter`, `EnterpriseFlowAdapter`.

2. Implement generic event builders:
   - `build_event_from_flow(raw_flow, source_id)` → canonical event.
   - `build_event_from_file(file_metadata, source_id)` → canonical event.

3. Integrate adapters into Sentinel tests:
   - Use CIC row/dict → adapter → event.
   - Optionally run through a `FlowAgent` stub and the Sentinel ensemble for validation.

**Checklist**

- [ ] `CICDDoS2019Adapter` implemented and unit‑tested (sanity checks for key metrics like pps).
- [ ] Event builders implemented for flows and files.
- [ ] Sentinel tests validate adapter + event building without changing CyberMesh.

---

### Phase 4 – Introduce DetectionAgent & Canonical Events into CyberMesh (Non‑Breaking)

**Objectives**

- Bring shared contracts and agent abstraction into CyberMesh.
- Wrap existing engines as agents, alongside the current pipeline.
- Keep current behavior as default.

**Steps**

1. Add shared types to CyberMesh:
   - Either import from a shared package or copy the agreed `DetectionCandidate` / `EnsembleDecision` types.

2. Implement CyberMesh agents:
   - `RulesEngineAgent` → wraps `RulesEngine`.
   - `MathEngineAgent` → wraps `MathEngine`.
   - `MLEngineAgent` → wraps `MLEngine` (flow and malware variants).

3. Implement a new `DetectionAgentPipeline` in CyberMesh:
   - Consumes canonical events.
   - Routes events by `modality` to the appropriate agents.
   - Collects `DetectionCandidate`s and applies a new ensemble (config‑driven).

4. Add a feature flag/config:
   - `USE_AGENT_PIPELINE = false` by default.
   - Keep existing `DetectionPipeline` untouched and active.

**Checklist**

- [ ] Shared detection types present in CyberMesh.
- [ ] `RulesEngineAgent`, `MathEngineAgent`, `MLEngineAgent` implemented.
- [ ] `DetectionAgentPipeline` implemented and unit‑tested.
- [ ] Existing `DetectionPipeline` still passes all existing tests.
- [ ] Feature flag ensures new pipeline is disabled by default.

---

### Phase 5 – Sentinel ↔ CyberMesh Integration (Shadow Mode)

**Objectives**

- Connect Sentinel as a `DetectionAgent` to CyberMesh.
- Run in shadow mode to evaluate impact without affecting production.

**Steps**

1. Decide integration mode for Sentinel:
   - Library integration (import Sentinel modules) **or**
   - Service integration (gRPC/HTTP endpoint).

2. Implement CyberMesh‑side `SentinelAgent`:
   - Library mode: reuse `SentinelAgent` implementation.
   - Service mode: CyberMesh agent sends events to Sentinel service and receives `DetectionCandidate`s.

3. Wire `SentinelAgent` into `DetectionAgentPipeline`:
   - Triggered for `modality == "file"` (and other relevant modalities if extended).

4. Run in shadow mode:
   - Keep `USE_AGENT_PIPELINE = false` for production output.
   - Feed the same telemetry into both:
     - Existing `DetectionPipeline`.
     - New `DetectionAgentPipeline` (with Sentinel).
   - Log and compare:
     - Detection coverage.
     - FP/FN differences.
     - Latency/throughput impact.

**Checklist**

- [ ] `SentinelAgent` operational from CyberMesh.
- [ ] `DetectionAgentPipeline` runs with CyberMesh agents + `SentinelAgent`.
- [ ] Shadow metrics collected and reviewed.
- [ ] No change to production decisions yet.

---

### Phase 6 – Rollout & Governance

**Objectives**

- Safely promote the new architecture to production.
- Maintain control and auditability as more models/agents are added.

**Steps**

1. Define rollout states per agent/model:
   - `off` → `shadow` → `prod_low_weight` → `prod_full`.

2. Unify ensemble configuration:
   - Single config file listing all `agent.signal` entries with weights/thresholds.
   - Profiles (`security_first`, `balanced`, `low_fp`) mapped across agents.

3. Progressive rollout:
   - Start with all new agents in `shadow`.
   - Move selected agents to `prod_low_weight` for a subset of customers.
   - Gradually move to `prod_full` once metrics and business owners approve.

4. Governance & observability:
   - Extend model/agent registry to track:
     - Input schema version.
     - Training dataset lineage.
     - Calibration method.
     - Status (`shadow`, `prod_low_weight`, `prod_full`).
   - Ensure per‑agent and per‑variant metrics are visible in dashboards.

**Checklist**

- [ ] Rollout states defined and implemented in config.
- [ ] Unified ensemble configuration covers all agents (CyberMesh + Sentinel).
- [ ] New architecture enabled for a small cohort and monitored.
- [ ] Runbooks updated: how to enable/disable agents, change weights, and roll back.

---

### Phase 7 – Extending to New Datasets & Modalities

**Objectives**

- Make adding new datasets/telemetry sources and models routine and safe.

**Steps**

1. For a new dataset or telemetry source:
   - Implement an adapter to a canonical schema (e.g. `NetworkFlowFeaturesV1`).
   - Validate feature mapping against sample data.

2. For a new model:
   - Train against canonical features.
   - Register it in the model registry with:
     - Input schema version.
     - Dataset lineage and metrics.
     - Intended threat types.
   - Add it to an existing or new `DetectionAgent`.
   - Start at `shadow` state.

3. For a new modality (e.g. DNS, proxy):
   - Define a new canonical schema (e.g. `DNSFeaturesV1`).
   - Implement adapters for key sources.
   - Implement one or more agents for that modality.

**Checklist**

- [ ] Adapter pattern used for each new raw source.
- [ ] New models registered with schema + lineage + metrics.
- [ ] New models/agents start in `shadow` and go through rollout states.

---

This SOP is intended to make the CyberMesh–Sentinel integration repeatable, auditable, and scalable, while preserving the stability of the existing AI‑Service and aligning flows/ML with the same pluggable, config‑driven design you already use for Threat Intel and LLMs.

---

### Cross‑Cutting Concerns (Performance, Security, Observability, Testing)

These items apply across multiple phases and should be kept in mind throughout implementation.

#### Performance, SLOs, and Resource Budgets

**Objectives**

- Define and enforce latency and throughput targets per modality and agent.
- Avoid uncontrolled growth in compute cost as models and agents are added.

**Checklist**

- [ ] Define SLOs per modality (e.g. `network_flow` P95 ≤ 50ms, `file` P95 ≤ 200ms end‑to‑end).
- [ ] Define per‑agent latency budgets and maximum number of heavy models per event.
- [ ] Implement cascaded evaluation where cheap agents (rules/math/TI) gate expensive ones (deep ML, LLMs, sandbox).
- [ ] Add metrics for per‑agent latency and throughput and dashboard them.

#### Security and Tenant Isolation

**Objectives**

- Ensure multi‑tenant deployments cannot leak data between customers.
- Make event schemas and logs safe from inadvertent sensitive data exposure.

**Checklist**

- [ ] Include a `tenant_id` or equivalent in the canonical event envelope.
- [ ] Ensure all DetectionAgents and logs tag events and metrics with `tenant_id`.
- [ ] Validate canonical events (schema + type + bounds) before passing them to agents.
- [ ] Define which fields (e.g. `src_ip`, `dst_ip`, usernames, file paths) must be masked, hashed, or redacted in logs.
- [ ] Ensure adapters do not mix data across tenants when reading from shared sources.

#### Observability and Debuggability

**Objectives**

- Provide clear visibility into how each adapter and agent behaves.
- Enable deep debugging of detection decisions without overwhelming logs.

**Checklist**

- [ ] For each DetectionAgent, export metrics:
  - `candidates_total`, `published_total`, `avg_confidence`, `latency_ms` (avg/P95), `errors_total`.
- [ ] For each adapter, export metrics:
  - `events_mapped_total`, `mapping_errors_total`, `dropped_events_total`.
- [ ] Implement structured logging with event IDs and correlation IDs.
- [ ] Add a debug mode to log full decision trails for selected events (input features, candidates, ensemble weights and final decision).

#### Feedback Loop and Calibration

**Objectives**

- Continuously improve thresholds and calibration using real‑world feedback.
- Keep per‑agent calibration and thresholds versioned and auditable.

**Checklist**

- [ ] Ensure DetectionAgent outputs are fed into a shared feedback/calibration mechanism.
- [ ] Track calibration metadata per agent/model (method, version, last updated, dataset used).
- [ ] Define a process for updating thresholds/weights based on feedback, including review and rollback.
- [ ] Align Sentinel's `calibrated_config.json` profiles with CyberMesh ensemble profiles where possible.

#### Testing Strategy

**Objectives**

- Prevent regressions in the existing AI‑Service.
- Validate adapters, agents, and ensembles in isolation and end‑to‑end.

**Checklist**

- [ ] Unit tests for all adapters (golden input/output pairs, edge cases, error handling).
- [ ] Unit tests for DetectionAgents (given a canonical event, expected candidates).
- [ ] Unit tests for ensemble logic (various combinations of candidates and profiles).
- [ ] Integration tests for the existing `DetectionPipeline` to ensure behavior is unchanged when new flags are off.
- [ ] Integration tests for `DetectionAgentPipeline` using synthetic and real data.
- [ ] Shadow tests comparing old vs. new pipelines on the same datasets and telemetry (score distributions, decision deltas, latency).

#### Data Privacy and Compliance

**Objectives**

- Ensure telemetry and detections respect privacy and compliance requirements.

**Checklist**

- [ ] Classify fields in canonical schemas as: required, optional, sensitive.
- [ ] Define which fields are allowed in logs, in training datasets, and in external exports.
- [ ] Implement anonymization/tokenization where required (e.g. for lower environments, offline training sets).
- [ ] Document and review data retention policies for telemetry, detections, and feedback data.
