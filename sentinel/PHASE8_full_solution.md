## Phase 8 – Full Solution to the 7 Original Issues

This document describes a concrete, end‑to‑end solution to fully resolve the 7 original CyberMesh ↔ Sentinel integration issues, **building on Phases 0–7** while **keeping the CIC 79‑feature pipeline available** as long as needed. It focuses on:

1. What “fully resolved” means for each issue.
2. Architectural and code changes required (where applicable).
3. Operational steps (config, rollout, training) needed to actually complete the work.

The intent is to convert the current ~70–75% architectural completion into a **practical path to 100% resolution** of the original problems.

---

## 0. Grounding: The 7 Original Issues

For reference, the 7 issues we set out to solve were:

1. **Flow schema lock‑in** – CyberMesh tightly bound to the 79‑feature CIC‑DDoS2019 pipeline.
2. **Different analysis units** – CyberMesh flow‑only vs Sentinel file‑only; no unified multi‑modality.
3. **Incompatible abstractions** – CyberMesh engines vs Sentinel providers; no shared detection contract.
4. **Different calibration/voting layers** – CyberMesh thresholds vs Sentinel `calibrated_config.json`.
5. **Telemetry/storage assumptions** – CyberMesh expects curated DB tables; Sentinel expects files.
6. **Transport/protocol differences** – CyberMesh Kafka vs Sentinel local/CLI.
7. **Network‑only semantics** – Feature semantics are CIC/network‑centric; no multi‑modality semantic layer.

Phases 0–7 partially resolved all of these. This document defines the remaining work and the **full solution** for each.

---

## 1. Resolve Flow Schema Lock‑in (Issue 1)

### 1.1 Target End State

* CyberMesh **no longer requires** the 79 CIC features as its primary runtime schema.
* All new models and engines consume **canonical** flow features (`NetworkFlowFeaturesV1`).
* CIC becomes **one dataset among many**, accessed via adapters and used in training/evaluation, not baked into runtime paths.
* The legacy 79‑feature path can be **retired per source** when the canonical pipeline is proven, but CIC can remain available for historical models.

### 1.2 Architecture & Code Changes

1. **Keep CIC logic encapsulated** (as already started in Phase 7):
   * Ensure CIC specifics live only in:
     * `CICDDoS2019Adapter` (raw CIC rows → `NetworkFlowFeaturesV1`).
     * `extract_cic_features()` (canonical → 79‑dim vector for existing CIC‑trained models).
   * Avoid introducing any new 79‑feature dependencies elsewhere.

2. **Implement additional flow adapters** (in CyberMesh):
   * `UNSWNB15Adapter` – UNSW‑NB15 rows → `NetworkFlowFeaturesV1`.
   * `NSLKDDAdapter` – NSL‑KDD rows → `NetworkFlowFeaturesV1`.
   * `CICIDS2017Adapter` – CICIDS2017 rows → `NetworkFlowFeaturesV1`.
   * Additional enterprise flow/NetFlow formats as needed.
   * Register these in the adapter registry (e.g. `ADAPTER_REGISTRY` in `flow_adapters.py`).

3. **Standardize `NetworkFlowFeaturesV1` semantics**:
   * For all adapters, document and enforce consistent units and meaning:
     * `flow_duration_ms` (milliseconds), `bytes_in`, `bytes_out`, `pps`, `bps`, `syn_ack_ratio`, etc.
   * Add validation helpers for canonical flows (e.g. range checks, NaN/Inf handling).

4. **Introduce canonical‑native flow models**:
   * Train new DDoS/anomaly models that **directly consume** `NetworkFlowFeaturesV1`.
   * Wrap each canonical model in a new `DetectionAgent` (e.g. `FlowMLCanonicalAgentV1`) that:
     * Accepts `CanonicalEvent` with `features_version="NetworkFlowFeaturesV1"`.
     * Does **not** rely on `extract_cic_features`.
   * Keep existing CIC‑based models wired through `FlowMLEngineAgentV2` for comparison.

5. **Wire canonical models into RolloutConfig**:
   * Add new `agent_id`s (e.g. `cybermesh.ml.canonical_v1`, `cybermesh.ml.canonical_v2`).
   * Record in `AgentConfig`:
     * `features_version="NetworkFlowFeaturesV1"`.
     * `dataset_lineage` (e.g. `"cic+unsw+nsl+enterprise_v1"`).
     * Calibration metadata (method, version, metrics).

### 1.3 Operational Steps

1. **Training & evaluation:**
   * Build an offline pipeline that:
     * Uses adapters (CIC/UNSW/NSL/CICIDS/enterprise) to produce `NetworkFlowFeaturesV1`.
     * Trains canonical models and compares them against the legacy CIC model.
   * Evaluate per dataset and per source (where possible), tracking FP/FN, precision/recall, ROC, etc.

2. **Rollout per source:**
   * For each telemetry source, use `TelemetryRegistry` + `RolloutConfig` to:
     * Start with `SourceMode=BOTH`, `DetectionMode=AGENT_SHADOW`.
     * Run canonical models in shadow alongside CIC and track performance.
     * When canonical models are acceptable, move to `AGENT_PRIMARY` for that source.
     * Eventually move `SourceMode` to `AGENT` when comfortable.

3. **Retire legacy dependencies (optionally per source):**
   * Once a source is fully on canonical models and stable:
     * Stop using `extract_cic_features` for that source’s routing.
     * Remove that source’s dependency on legacy `DetectionPipeline`.
   * Global removal of legacy 79‑feature code is only done when **all critical sources** have migrated.

---

## 2. Resolve “Different Analysis Units” (Issue 2)

### 2.1 Target End State

* The combined system can analyze **flows, files, DNS, and future modalities** in a unified way.
* Sentinel is treated as a **first‑class detection agent** for file/object analysis.
* CyberMesh and Sentinel decisions are fused in a single ensemble, regardless of modality.

### 2.2 Architecture & Code Changes

Most of this is already implemented; the remaining solution focuses on **usage and extension**:

1. **Ensure canonical schemas per modality are stable and versioned:**
   * `NetworkFlowFeaturesV1` (flows) – already defined.
   * `FileFeaturesV1` (files) – already defined in Sentinel and used by `SentinelFileAgent`.
   * `DNSFeaturesV1` (DNS) – implemented in CyberMesh.
   * Optional future: `HostFeaturesV1`, `ProcessFeaturesV1`, etc.

2. **Keep `DetectionAgent` multi‑modal:**
   * Agents advertise `input_modalities()` using the shared `Modality` enum.
   * `DetectionAgentPipeline` routes `CanonicalEvent`s to all compatible agents.

3. **Expand practical coverage of modalities:**
   * For files:
     * Ensure `SentinelFileAgent` is fully wired in `ServiceManager` and `RolloutConfig`.
     * Configure telemetry for real file streams (e.g. file upload logs, EDR file events) to produce canonical file events.
   * For DNS:
     * Wire DNS log sources into `DNSAdapter` → `DNSAgent` via telemetry config.

### 2.3 Operational Steps

1. **Turn on multi‑modality in shadow:**
   * For at least one tenant/environment, enable:
     * Flow events (NetworkFlowFeaturesV1).
     * File events (FileFeaturesV1 via SentinelFileAgent).
     * DNS events (DNSFeaturesV1 via DNSAdapter).
   * Keep `DetectionMode=AGENT_SHADOW` initially so legacy behavior remains unchanged.

2. **Cross‑modality correlation (optional but recommended):**
   * Build correlation rules/policies that:
     * Link flow, file, and DNS decisions by host/IP/user/asset.
     * E.g. “High DDoS score + high Sentinel malware score on same host within X minutes → CRITICAL.”
   * Implement these either:
     * As an additional agent (e.g. `CorrelationAgent`), or
     * In downstream SIEM/analytics, using shared identifiers in `CanonicalEvent.raw_context`.

---

## 3. Resolve Abstraction Mismatch (Issue 3)

### 3.1 Target End State

* Sentinel providers and CyberMesh engines all appear as **DetectionAgents** with unified inputs/outputs.
* Cross‑system integration uses **only** canonical events and detection candidates.

### 3.2 Architecture & Code Changes

Most of the abstraction work is already complete; the remaining solution is mostly **cleanup and discipline**:

1. **Enforce `DetectionAgent` as the only cross‑module detection interface:**
   * Integrations should not call lower‑level providers/engines directly.
   * Any new engine/component must be wrapped in a `DetectionAgent`.

2. **Keep contracts shared and synchronized:**
   * Maintain `ThreatType`, `DetectionCandidate`, `EnsembleDecision`, `CanonicalEvent`, `Modality` as a **shared library** or strictly synchronized copies between CyberMesh and Sentinel.

3. **Refactor any remaining direct usages:**
   * Audit code for any usage of:
     * Raw Sentinel `AnalysisResult` outside of `SentinelAgent`/`SentinelFileAgent`.
     * Raw CyberMesh engine outputs outside of their respective agents.
   * Wrap or route such calls through `DetectionAgent`.

### 3.3 Operational Steps

1. **Code review guardrail:**
   * Establish a rule: “All new detection logic must implement `DetectionAgent` and use canonical events.”
   * Reject changes that wire new models or providers outside this pattern.

2. **Shared contract tests:**
   * Add tests that deserialize/serialize `DetectionCandidate` and `CanonicalEvent` across both codebases to ensure compatibility.

---

## 4. Resolve Calibration/Voting Divergence (Issue 4)

### 4.1 Target End State

* Both systems use conceptually aligned ensemble configs and profiles.
* Each agent (CyberMesh or Sentinel) has explicit weights, thresholds, and profiles.
* Changing calibration in one place has predictable, explainable effects.

### 4.2 Architecture & Code Changes

1. **Define a common ensemble configuration schema (conceptual):**
   * At minimum, a conceptual schema documenting:
     * `agent_id` → weight, thresholds, profiles.
   * CyberMesh: `RolloutConfig` + `AgentConfig` (already implemented).
   * Sentinel: `calibrated_config.json` (already implemented).

2. **Map Sentinel providers to `agent_id`s:**
   * For each Sentinel provider (e.g. `malware_pe_ml`, `entropy_analyzer`, `threat_intel`):
     * Define a corresponding `agent_id` naming convention (e.g. `sentinel.malware_pe_ml`).
   * Ensure `SentinelFileAgent` sets `engine_name`/`agent_id` consistently with this scheme.

3. **Align profiles:**
   * Maintain a mapping document or module that maps:
     * Sentinel profiles (`balanced`, `security_first`, `low_fp`) to
     * CyberMesh profiles (`balanced`, `security_first`, `low_fp`) in `RolloutConfig`.
   * Implement any necessary translation logic so that:
     * When the global profile changes, both Sentinel and CyberMesh ensembles interpret it consistently.

### 4.3 Operational Steps

1. **Profile validation harness:**
   * Build a small tool or test that, for a given synthetic set of `DetectionCandidate`s, shows:
     * How Sentinel would decide (using `calibrated_config.json`).
     * How CyberMesh `AgentEnsembleVoter` would decide using `RolloutConfig`.
   * Adjust thresholds/weights until the overall behavior is aligned for standard scenarios.

2. **Governance & documentation:**
   * Maintain a calibration document that records:
     * For each agent: weight, threshold, calibration dataset, date, owner.
   * Use this as a reference for audits and incident reviews.

---

## 5. Resolve Telemetry/Storage Assumptions (Issue 5)

### 5.1 Target End State

* Telemetry sources produce **canonical events**, not raw CIC‑specific rows, for new functionality.
* Dataset‑specific quirks live in **adapters** and **TelemetryRegistry**.
* Migration from legacy sources is controlled per source via config (`SourceMode`, `DetectionMode`).

### 5.2 Architecture & Code Changes

1. **Use adapters as the default for agent pipeline ingestion:**
   * In `ServiceManager`, build or extend a function that:
     * Reads raw rows from a telemetry source.
     * Uses `get_adapter_for_source(source_id)` (from `TelemetryRegistry`) to convert rows into canonical events.
     * Feeds these events into `DetectionAgentPipeline`.

2. **Keep legacy ingestion path isolated:**
   * For sources with `SourceMode=LEGACY` or `BOTH`, continue using the existing CIC‑specific path for the legacy pipeline only.
   * Avoid writing any new code that depends on raw table schemas outside adapters.

3. **Extend TelemetryRegistry configuration:**
   * Ensure each source entry includes:
     * `mode` (LEGACY/AGENT/BOTH).
     * `modality` (FLOW/FILE/DNS/etc.).
     * `adapter_class` (when AGENT/BOTH).
     * `enabled` flag for safe deactivation.

### 5.3 Operational Steps

1. **Onboard new sources via TelemetryRegistry:**
   * For any new telemetry source, define a config entry and adapter rather than wiring it directly into legacy code.

2. **Piecemeal migration:**
   * For each existing source:
     * Start in `LEGACY`.
     * Move to `BOTH` + `AGENT_SHADOW` (agents observe but dont publish).
     * Then to `BOTH` + `AGENT_PRIMARY` when validated.
     * Finally to `AGENT` when youre confident you no longer need the legacy path.

---

## 6. Resolve Transport/Protocol Differences (Issue 6)

### 6.1 Target End State

* Sentinel and CyberMesh can interoperate without being forced to share Kafka or internal wire formats.
* Where external transport is required, it uses **canonical, versioned schemas** for events and decisions.

### 6.2 Architecture & Code Changes

Most of this has already been resolved pragmatically by integrating Sentinel **before** Kafka in CyberMesh. The full solution is:

1. **Continue treating Sentinel as a library/agent inside CyberMesh:**
   * Keep `SentinelFileAgent` as the preferred integration path.
   * Let CyberMesh publish Kafka messages using its own schemas, enriched with Sentinel results.

2. **Optional: define external canonical schemas:**
   * If you need other systems to produce/consume detection events, define canonical schemas (e.g. JSON/Avro/Protobuf) for:
     * `CanonicalEvent`.
     * `DetectionCandidate`.
     * `EnsembleDecision`.
   * Map these to Kafka topics or HTTP/gRPC APIs as needed.

### 6.3 Operational Steps

1. **If transport unification is required:**
   * Define and version external schemas and topics.
   * Implement small translators/adapters at service boundaries.

2. **If internal only:**
   * Keep the current pattern: Sentinel runs inside CyberMesh via `SentinelFileAgent`, and only CyberMesh handles Kafka.

---

## 7. Resolve Network‑Only Semantics (Issue 7)

### 7.1 Target End State

* Semantics are expressed via **per‑modality canonical schemas**, not a single network‑only semantics dict.
* Flow, file, and DNS semantics are first‑class and comparable across agents.

### 7.2 Architecture & Code Changes

1. **Canonical schemas as semantic carriers:**
   * Treat `NetworkFlowFeaturesV1`, `FileFeaturesV1`, and `DNSFeaturesV1` as the authoritative definitions of semantic fields.
   * Keep all feature calculations and domain knowledge in:
     * Adapters (raw → canonical features).
     * Modality‑specific agents (e.g. DNSAgent heuristics).

2. **Minimize legacy semantics dict usage:**
   * Gradually refactor any remaining uses of legacy semantics dicts to:
     * Use canonical feature fields directly, or
     * Derive them in a documented way from canonical features.

3. **Add semantic validation & documentation:**
   * Provide per‑field documentation (inline or in a design doc) for each canonical schema, including:
     * Meaning, units, typical ranges, nullability.
   * Add runtime validation for obviously invalid semantic combinations (e.g. negative durations, impossible ratios).

### 7.3 Operational Steps

1. **Expand DNS and file semantics as needed:**
   * When new detectors or rules require more DNS/file semantics, extend the canonical schemas rather than adding ad‑hoc fields elsewhere.

2. **Cross‑modality reasoning:**
   * Build analytics that leverage semantics across modalities (e.g. domain entropy vs flow PPS vs file TI) for richer detections.

---

## 8. Putting It All Together – Execution Plan

To fully resolve the 7 issues, follow these staged steps (after Phases 0–7):

1. **Adapters & Canonical Schemas (Flows):**
   * Implement and test additional flow adapters (UNSW, NSL‑KDD, CICIDS2017, enterprise) to `NetworkFlowFeaturesV1`.
   * Standardize field semantics across adapters.

2. **Canonical Event Bus in Practice:**
   * For each telemetry source, use `TelemetryRegistry` + adapters to produce `CanonicalEvent`s.
   * Feed these into `DetectionAgentPipeline` while preserving the legacy pipeline where needed.

3. **Canonical‑Native Flow Models:**
   * Train new models on `NetworkFlowFeaturesV1` using multiple datasets.
   * Add them as new agents and roll them out via `RolloutConfig` and shadow mode.

4. **Operational Multi‑Modality:**
   * Ensure file and DNS telemetry are connected and flowing to SentinelFileAgent and DNSAgent in at least one environment.
   * Use shadow mode to validate multi‑modal detections.

5. **Calibration Alignment:**
   * Align Sentinels `calibrated_config.json` profiles with CyberMesh `RolloutConfig` profiles.
   * Use synthetic and real scenarios to validate ensemble behavior.

6. **Per‑Source Migration & Legacy Retirement (Optional):**
   * Use `SourceMode` and `DetectionMode` to migrate sources piecemeal from LEGACY → BOTH → AGENT.
   * Retire or isolate legacy 79‑feature paths only when canonical models have proven themselves for all important sources.

Following this plan, you can move from the current ~70–75% architectural completion to a **full, operational resolution** of the 7 original issues, all while keeping CIC available as long as business and risk considerations require.

---

## 9. Addressing Identified Gaps

This section fills concrete gaps identified after the initial Phase 8 draft.

### 9.1 Gap 1 – DetectionLoop Integration with Agent & Shadow Pipelines

**Problem:** The original text mentioned wiring adapters and the `DetectionAgentPipeline` via `ServiceManager`, but did **not** specify how the runtime `DetectionLoop` should:

* Accept the new pipelines.
* Use routing decisions per telemetry source.
* Invoke shadow mode correctly.

**Full solution:**

1. **Extend `DetectionLoop` constructor (CyberMesh):**

   Add optional parameters:

   ```python
   class DetectionLoop:
       def __init__(
           self,
           pipeline: DetectionPipeline,
           agent_pipeline: Optional[DetectionAgentPipeline] = None,
           shadow_pipeline: Optional[ShadowModePipeline] = None,
           telemetry_registry: Optional[TelemetryRegistry] = None,
           service_manager: Optional[ServiceManager] = None,
           ...,
       ):
           self.pipeline = pipeline
           self.agent_pipeline = agent_pipeline
           self.shadow_pipeline = shadow_pipeline
           self.telemetry_registry = telemetry_registry
           self.service_manager = service_manager
   ```

2. **Wire these from `ServiceManager`:**

   In `ServiceManager._initialize_ml_pipeline()` (or equivalent):

   * Instantiate:
     * `self.legacy_pipeline` (existing `DetectionPipeline`).
     * `self.agent_pipeline` (`DetectionAgentPipeline`).
     * `self.shadow_pipeline` (`ShadowModePipeline`).
   * Pass all three, plus `telemetry_registry` and the `ServiceManager` instance itself, into `DetectionLoop`.

3. **Modify `DetectionLoop._run_iteration()` routing logic:**

   Inside `_run_iteration()` (for each batch/row/event):

   1. Identify `source_id` for the record.
   2. Use `service_manager.should_use_legacy_pipeline(source_id)`.
   3. Use `service_manager.should_use_agent_pipeline(source_id)`.
   4. Use `service_manager.should_agent_publish(source_id)`.
   5. If agent is enabled for this source, obtain the adapter via `service_manager.get_adapter_for_source(source_id)` and build `CanonicalEvent`s.

   Pseudocode sketch:

   ```python
   def _run_iteration(self) -> None:
       for source_id, rows in self._fetch_batches():
           use_legacy = self.service_manager.should_use_legacy_pipeline(source_id)
           use_agent = self.service_manager.should_use_agent_pipeline(source_id)
           agent_publishes = self.service_manager.should_agent_publish(source_id)

           legacy_results = None
           agent_results = None

           # 1) Legacy pipeline (79-feature CIC path)
           if use_legacy:
               legacy_results = self.pipeline.process_rows(source_id, rows)

           # 2) Agent pipeline (canonical event path)
           if use_agent and self.agent_pipeline is not None:
               adapter = self.service_manager.get_adapter_for_source(source_id)
               if adapter is None:
                   # Fallback: skip agent for this source
                   continue

               events = adapter.to_canonical_events_batch(rows)
               agent_results = []
               for event in events:
                   decision = self.agent_pipeline.process(event)
                   agent_results.append(decision)

               # 3) Shadow mode comparison
               if self.shadow_pipeline is not None and legacy_results is not None:
                   self.shadow_pipeline.process_shadow(
                       source_id=source_id,
                       legacy_results=legacy_results,
                       agent_results=agent_results,
                   )

           # 4) Publish authoritative results
           if agent_publishes and agent_results is not None:
               self._publish(agent_results)
           elif legacy_results is not None:
               self._publish(legacy_results)
   ```

4. **Edge cases:**

* If `telemetry_registry` is `None`, default to legacy‑only behavior.
* If `get_adapter_for_source()` fails, log a warning and skip the agent path for that source, without breaking the legacy path.

This ensures `DetectionLoop` becomes the real runtime integrator of **legacy**, **agent**, and **shadow** pipelines.

---

### 9.2 Gap 2 – `FileFeaturesV1` Missing in CyberMesh Contracts

**Problem:** The initial document assumed `FileFeaturesV1` existed in CyberMesh, but it only exists in Sentinel (`sentinel/contracts/schemas.py`). CyberMeshs `SentinelFileAgent` checks `features_version == "FileFeaturesV1"`, but there is no corresponding schema type in CyberMesh.

**Full solution (two options):**

1. **Short‑term (practical) – Duplicate schema in CyberMesh:**

   * Add a `FileFeaturesV1` dataclass to `src/ml/agents/contracts.py` in CyberMesh, mirroring Sentinels definition:

   ```python
   @dataclass
   class FileFeaturesV1:
       # Minimal example – mirror Sentinel's schema exactly
       entropy: float
       import_entropy: Optional[float]
       string_score_command_execution: Optional[float]
       yara_score_malware_packers: Optional[float]
       ti_score: Optional[float]
       # ... all other fields from sentinel.contracts.schemas.FileFeaturesV1
   ```

   * Ensure `SentinelFileAgent` uses this type for internal typing/validation where needed.

2. **Medium‑term (clean) – Shared contracts package:**

   * Extract shared contracts (`ThreatType`, `DetectionCandidate`, `EnsembleDecision`, `CanonicalEvent`, `Modality`, `NetworkFlowFeaturesV1`, `FileFeaturesV1`, `DNSFeaturesV1`) into a small, versioned Python package (e.g. `cybersentinel_contracts`).
   * Make both Sentinel and CyberMesh depend on this package.

**Recommendation:** Implement the short‑term duplication now to unblock work, then move to the shared package when convenient.

---

### 9.3 Gap 3 – Concrete File/DNS Telemetry Sources

**Problem:** The original document asked to “configure telemetry for real file streams and DNS logs” but did not define concrete ingestion components.

**Full solution:**

1. **Define `FileTelemetrySource` (CyberMesh):**

   * Responsibility: turn file events (paths, hashes, metadata) into `CanonicalEvent`s with `modality=FILE`.
   * Example behaviors:
     * Watch a directory for newly dropped files.
     * Consume messages from a queue (e.g. `file_scan_requests`) containing file paths or blobs.

   * Pseudocode interface:

   ```python
   class FileTelemetrySource:
       def __iter__(self) -> Iterator[Dict[str, Any]]:
           # yield raw file events: {"path": ..., "sha256": ..., "tenant_id": ...}
           ...

       def to_canonical_event(self, raw: Dict[str, Any]) -> CanonicalEvent:
           return CanonicalEvent(
               id=raw.get("id") or str(uuid4()),
               timestamp=raw["timestamp"],
               source="file_telemetry",
               tenant_id=raw.get("tenant_id"),
               modality=Modality.FILE,
               features_version="FileFeaturesV1",
               features={},  # SentinelFileAgent will populate features
               raw_context={"path": raw["path"], "sha256": raw.get("sha256")},
           )
   ```

   * `FileTelemetrySource` events are then passed to `SentinelFileAgent`, which performs analysis and fills out the file features.

2. **Define `DNSTelemetrySource` (CyberMesh):**

   * Responsibility: read DNS logs and convert them to canonical DNS events using `DNSAdapter`.
   * Input formats could include:
     * Zeek DNS logs.
     * Passive DNS (pDNS) feeds.
     * Resolver logs.

   * Pseudocode interface:

   ```python
   class DNSTelemetrySource:
       def __iter__(self) -> Iterator[Dict[str, Any]]:
           # yield raw DNS logs: {"qname": ..., "rcode": ..., "client_ip": ..., ...}
           ...

       def to_canonical_event(self, raw: Dict[str, Any]) -> CanonicalEvent:
           features = DNSAdapter().to_features(raw)
           return DNSAdapter().to_canonical_event(raw)
   ```

3. **Integration via `TelemetryRegistry`:**

   * Add entries for these sources, e.g.:

   ```json
   {
     "sources": {
       "file_telemetry": {
         "mode": "agent",
         "modality": "file",
         "adapter_class": null,
         "enabled": true
       },
       "dns_logs_zeek": {
         "mode": "agent",
         "modality": "dns",
         "adapter_class": "DNSAdapter",
         "enabled": true
       }
     }
   }
   ```

   * `DetectionLoop` (or a coordinator component) pulls from these sources and feeds `CanonicalEvent`s into `DetectionAgentPipeline`.

---

### 9.4 Gap 4 – Multi‑Modality Within a Single DetectionLoop Iteration

**Problem:** The original document did not specify how to handle batches that contain a mix of modalities (flows, files, DNS) in one iteration, or how `AgentEnsembleVoter` should treat cross‑modality decisions.

**Full solution:**

1. **Clarify decision granularity:**

   * A single `DetectionLoop` iteration may process multiple **events** from multiple **sources** and **modalities**.
   * Each `CanonicalEvent` should result in exactly one `EnsembleDecision` for that event context (e.g. one flow, one file, one DNS query).

2. **Agent pipeline behavior:**

   * `DetectionAgentPipeline.process(event)` should:
     * Route `event` to all agents supporting `event.modality`.
     * Collect `DetectionCandidate`s.
     * Use `AgentEnsembleVoter` to produce an `EnsembleDecision` for **that event only**.

   * `process_batch(events: List[CanonicalEvent])` (if implemented) should:
     * Iterate events and call `process(event)` per event.
     * Return a list of `EnsembleDecision`s, one per input event.

3. **Cross‑modality correlation (optional, higher‑level):**

   * Cross‑event, cross‑modality correlation (e.g. correlating a DNS event with a flow and a file for the same host) should:
     * Be handled by a **correlation agent** (e.g. `CorrelationAgent`) that consumes multiple event decisions, or
     * Be implemented downstream in a SIEM/UEBA system that consumes `EnsembleDecision`s tagged with common context keys (host/IP/user).

   * This keeps the per‑event ensemble logic simple and consistent.

---

### 9.5 Gap 5 – Concrete Calibration Sync Mechanism

**Problem:** The initial document described conceptually mapping Sentinel providers to `agent_id`s and aligning profiles, but did not specify where or how this mapping is represented or used.

**Full solution:**

1. **Introduce a calibration mapping module (Sentinel side):**

   * Example file: `sentinel/calibration/agent_mapping.py`.
   * Responsibilities:
     * Map Sentinel provider names → global `agent_id`s.
     * Provide helper functions to translate `calibrated_config.json` into an intermediate agent config structure.

   * Sketch:

   ```python
   PROVIDER_TO_AGENT_ID = {
       "malware_pe_ml": "sentinel.malware_pe_ml",
       "entropy_analyzer": "sentinel.entropy_analyzer",
       "yara_rules": "sentinel.yara_rules",
       "threat_intel": "sentinel.threat_intel",
       # ...
   }

   def load_sentinel_agent_calibration(path: str) -> Dict[str, Any]:
       cfg = json.load(open(path))
       agents = {}
       for provider_name, p_cfg in cfg["providers"].items():
           agent_id = PROVIDER_TO_AGENT_ID.get(provider_name)
           if not agent_id:
               continue
           agents[agent_id] = {
               "weight": p_cfg.get("weight", 1.0),
               "thresholds": p_cfg.get("thresholds", {}),
               "profiles": p_cfg.get("profiles", {}),
           }
       return agents
   ```

2. **Bridge to CyberMesh `RolloutConfig`:**

   * In CyberMesh (or in a shared tool), create a script that:
     * Reads Sentinel calibration via `load_sentinel_agent_calibration`.
     * Updates or validates `RolloutConfig.agent_configs` for `sentinel.*` agent IDs.

3. **Profile consistency:**

   * Ensure both configs share profile names (`balanced`, `security_first`, `low_fp`).
   * Where behavior differs, document and handle via explicit translation rules in the mapping module.

---

### 9.6 Gap 6 – Error Handling and Fallback Strategy

**Problem:** The original document did not describe how to degrade gracefully when agents, adapters, or external dependencies fail, or when performance degrades.

**Full solution:**

1. **Sentinel integration failures:**

   * Wrap `SentinelFileAgent` initialization and calls in try/except:
     * On import or runtime failure:
       * Log the error.
       * Mark the agent as temporarily unavailable (e.g. via an internal flag).
       * Ensure `DetectionAgentPipeline` skips this agent gracefully.
   * Optionally expose agent health in a health endpoint (e.g. `health["agents"]["sentinel.file"] = "degraded"`).

2. **Adapter failures (e.g. DNSAdapter, FlowAdapter):**

   * In `DetectionLoop._run_iteration()`:
     * Catch exceptions from `adapter.to_canonical_event(s)`.
     * Log with source_id and a sample of offending data.
     * Skip the agent path for that batch; continue processing other batches.
   * Add validation inside adapters to sanitize/normalize obviously bad values (NaN/Inf, negative durations, etc.).

3. **Performance/backlog handling:**

   * Introduce simple monitoring metrics:
     * Queue length / backlog size for input batches.
     * Latency of legacy vs agent pipelines per batch.
   * If backlog exceeds thresholds:
     * Temporarily switch `DetectionMode` to `LEGACY` for affected sources (via config or an emergency override) to shed agent load.
     * Or reduce the number of agents enabled for those sources (e.g. disable heavier ML agents while keeping lightweight rules/TI agents).

4. **Configuration‑driven safety levers:**

   * Maintain a small, well‑documented set of runtime flags/env vars, such as:
     * `FORCE_LEGACY_ONLY=true` – bypass all agent pipelines.
     * `DISABLE_SENTINEL_AGENT=true` – skip `SentinelFileAgent`.
     * `DISABLE_DNS_AGENT=true` – skip `DNSAgent`.
   * These should be checked in `ServiceManager` or `DetectionAgentPipeline` initialization and in health reporting.

Including these gap closures in Phase 8 gives you a **complete, operationally robust plan** to fully resolve the 7 original issues, including concrete runtime wiring, shared schema handling, telemetry sources, calibration sync, and graceful degradation.
