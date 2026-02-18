## Phase 0 – Ground Truth & Guardrails

This document captures the full output of **Phase 0** for the CyberMesh AI-Service ↔ Sentinel integration.
It is purely descriptive (no design changes) and is intended as the reference for all later phases.

---

## Phase 0.A – CyberMesh AI-Service Ground Truth

### 1. End-to-End Data Path (Runtime)

High-level runtime flow:

> `Postgres/File TelemetrySource` → `FlowFeatureExtractor (79 features)` → `FeatureAdapter (semantics)` → `RulesEngine / MathEngine / MLEngine` → `EnsembleVoter` → `DetectionPipeline` → `DetectionLoop + ServiceManager` → Kafka/backend

**Telemetry sources**

- `TelemetrySource` (`src/ml/telemetry.py`)
  - Abstract base for telemetry: `get_network_flows(limit)`, `get_files(limit)`, `has_data()`.

- `FileTelemetrySource` (`src/ml/telemetry.py`)
  - Reads JSON files from `data/telemetry/flows/{benign,ddos}` and `data/telemetry/files/{benign,malware}`.
  - Primarily for offline/demo; not the main production source.

- `PostgresTelemetrySource` (`src/ml/telemetry_postgres.py`)
  - Production telemetry for DDoS/flow models.
  - Constructor parameters include `db_config`, `table_name` (default `"test_ddos_binary"`), `schema` (default `"curated"`), and sampling options.
  - Uses `FlowFeatureExtractor.FEATURE_COLUMNS` as `_numeric_columns` and `DEFAULT_METADATA_COLUMNS = ["label", "src_ip", "dst_ip", "flow_id"]`.
  - `_build_select_clause()` constructs a `SELECT` that:
    - Casts each numeric column: `COALESCE(col, 0)::double precision AS col` for every col in `FlowFeatureExtractor.FEATURE_COLUMNS`.
    - Adds metadata columns (`label`, `src_ip`, `dst_ip`, `flow_id`).
  - `_compose_query(limit)` builds:
    - `SELECT <79 numeric cols + metadata> FROM {schema}.{table}`
    - Optional `TABLESAMPLE SYSTEM (sample_percent)`.
    - `WHERE label = 'ddos'`.
    - `LIMIT %s`.
  - `_normalize_rows(rows)` converts each `RealDictRow` to a dict and ensures all 79 numeric feature columns are float-valued.
  - `_test_connection()`:
    - Executes `SELECT 1 FROM {schema}.{table} WHERE label = 'ddos' LIMIT 1` and fails if no row exists.
    - Checks approximate row count and the presence of index `idx_flows_label`.

**Feature extraction & semantics**

- `FlowFeatureExtractor` (`src/ml/features_flow.py`)
  - `FEATURE_COUNT = 79` and `FEATURE_COLUMNS` is a fixed list of 79 CIC-like flow feature names, e.g.:
    - `src_port`, `dst_port`, `protocol`, `flow_duration`,
    - `tot_fwd_pkts`, `tot_bwd_pkts`, `totlen_fwd_pkts`, `totlen_bwd_pkts`,
    - `flow_byts_s`, `flow_pkts_s`,
    - `syn_flag_cnt`, `ack_flag_cnt`,
    - `active_mean`, `idle_mean`, etc.
  - `extract_raw(flows: List[Dict]) -> np.ndarray`:
    - For each flow, for each column name, reads `flow.get(col_name, 0.0)`.
    - Handles `None`, NaN, ±inf; maps `protocol` strings to numbers (TCP/UDP/ICMP).
    - Returns an `n_flows x 79` float32 array.
  - `normalize()` is currently identity; models were trained on the raw 79-feature vectors.

- `FeatureAdapter` (`src/ml/feature_adapter.py`)
  - Docstring: **"derive semantic features from CIC-DDoS2019 79-feature vectors."**
  - Builds `_col_idx = {name: i for i, name in enumerate(FlowFeatureExtractor.FEATURE_COLUMNS)}`.
  - `derive_semantics(flows, X79)`:
    - Computes batch-level stats for the first `n` flows:
      - `unique_dst_ports` = number of unique `dst_port` values.
      - `port_entropy` = Shannon entropy (bits) of the `dst_port` distribution.
    - Per-flow semantics:
      - `pps` from `flow_pkts_s` (or `(tot_fwd_pkts + tot_bwd_pkts) / flow_duration` if missing).
      - `syn_ack_ratio` from `syn_flag_cnt / max(ack_flag_cnt, 1)`.
    - Returns a list of dicts with keys: `pps`, `syn_ack_ratio`, `unique_dst_ports`, `port_entropy`.

**Engines** (`src/ml/detectors.py`)

- `RulesEngine`
  - Input: semantic dict (`pps`, `unique_dst_ports`, `syn_ack_ratio`, `port_entropy`).
  - Thresholds from config: `DDOS_PPS_THRESHOLD`, `PORT_SCAN_THRESHOLD`, `SYN_ACK_RATIO_THRESHOLD`, `MALWARE_ENTROPY_THRESHOLD`.
  - Emits `DetectionCandidate`s for `ThreatType.DDOS`, `NETWORK_INTRUSION`, `DOS` based on these semantics.

- `MathEngine`
  - Input: semantic dict.
  - Uses:
    - Port entropy anomaly (low entropy indicates concentration on a few ports).
    - Z-score on `pps` vs baseline (`baseline_stats.json` → `pps_mean` / `pps_std`).
    - CUSUM drift detection on `pps`.
  - Emits `ThreatType.ANOMALY` candidates.

- `MLEngine`
  - Loads models via `ModelRegistry` and `data/models/model_registry.json`:
    - `ddos` model:
      - LightGBM, `feature_count: 79`, `performance.dataset: "CIC-DDoS2019"`.
    - `anomaly` model:
      - IsolationForest, `feature_count: 30`.
    - `malware` variants:
      - `windows_api_seq` (1000 features), `pe_imports` (1000), `android_apk` (99), `net_flow_79` (39 features, schema `net_flow_39`).
  - `predict(features)`:
    - Handles dict inputs for malware variants (PE/APK/net_flow) and plain numpy vectors for legacy flow/malware paths.
    - For numeric vectors:
      - `feature_count == 30` → DDoS + anomaly flow models.
      - `feature_count == 256` → generic malware model.
      - Else: tries to match `feature_count` with `ddos_model.n_features_in_` (79) and routes accordingly.
  - `_predict_ddos` / `_predict_anomaly`:
    - Annotate detections with `metadata['schema'] = 'flow_79'` and `threat_type = DDOS/ANOMALY`.

**Ensemble & pipeline**

- `EnsembleVoter` (`src/ml/ensemble.py`)
  - Engine weights (from config): `ML_WEIGHT`, `RULES_WEIGHT`, `MATH_WEIGHT`.
  - Per-threat base thresholds: `DDOS_THRESHOLD`, `MALWARE_THRESHOLD`, `ANOMALY_THRESHOLD`, etc.
  - `MIN_CONFIDENCE` as a global minimum confidence for publishing.
  - For each threat type, computes:
    - Uncertainty-aware, trust-weighted engine weights.
    - Final ensemble score and confidence.
    - Log-likelihood ratio (LLR).
  - Applies abstention logic using adaptive thresholds from `AdaptiveDetection`.

- `DetectionPipeline` (`src/ml/pipeline.py`)
  - Stage 1: telemetry load
    - `flows = telemetry.get_network_flows(limit)` (Postgres or file-based, but production uses Postgres).
  - Stage 2: feature extraction
    - `features_79 = feature_extractor.extract_raw(flows)`.
    - `features_norm = feature_extractor.normalize(features_79)`.
    - `semantics_list = FeatureAdapter.derive_semantics(flows, features_79)`.
  - Stage 3: run engines
    - For each engine:
      - ML: `feats = features_norm[0]` (79-dim vector, then sliced/used according to model expectations).
      - Rules/Math: `feats = semantics_list[0]` (single semantic dict).
  - Stage 4: ensemble voting
    - `decision = ensemble.decide(all_candidates)`.
    - Attaches `network_context` (`src_ip`, `dst_ip`, `src_port`, `dst_port`, `protocol`, `flow_id`) from `flows[0]` into `decision.metadata`.
  - Stage 5: evidence generation (if `decision.should_publish`)
    - `evidence_gen.generate(decision, raw_features={'flows': flows, 'semantics': semantics_list[:5]})`.
  - Tracks per-stage latency, error handling, and per-pipeline statistics.

**Service wiring**

- `ServiceManager._initialize_ml_pipeline(settings)` (`src/service/manager.py`)
  - Builds `ml_config` (paths, thresholds, weights) from env and `Settings`.
  - Telemetry source selection:
    - If `TELEMETRY_SOURCE_TYPE == 'postgres'`:
      - Builds `db_config` (host, port, dbname, user, password, SSL options).
      - Derives `table_name` from `DB_TABLE` (default `"test_ddos_binary"`) and `schema` (`"curated"` if `"test"` in the name, else `"public"`).
      - Instantiates `PostgresTelemetrySource` with these parameters.
    - Else:
      - Uses `FileTelemetrySource` with `TELEMETRY_FLOWS_PATH` and `TELEMETRY_FILES_PATH`.
  - Instantiates:
    - `FlowFeatureExtractor`, `ModelRegistry`.
    - `RulesEngine`, `MathEngine`, `MLEngine`.
    - `EnsembleVoter`, `EvidenceGenerator`, `DetectionPipeline`.
  - Wires `DetectionPipeline` into a `DetectionLoop` with rate limiting (Phase 8) and integrates with `FeedbackService`.

### 2. Explicit CIC-DDoS2019 Dependencies

1. **Telemetry / database layer**
   - `PostgresTelemetrySource` requires:
     - A table with all 79 numeric columns named exactly as in `FlowFeatureExtractor.FEATURE_COLUMNS`.
     - A `label` column with value `'ddos'` (both for `_test_connection()` and runtime queries).
     - An index `idx_flows_label` is expected for performance (logged as critical if missing).

2. **Feature schema**
   - `FlowFeatureExtractor.FEATURE_COLUMNS` is a CIC-style flow schema with 79 fields; code comments describe it as the "single source of truth for raw flow features used by ML".
   - `FeatureAdapter` explicitly mentions "CIC-DDoS2019 79-feature vectors" and uses this schema to derive semantics.

3. **Models and registry**
   - `data/models/model_registry.json`:
     - `"ddos"` model has `"feature_count": 79` and `"performance": {"dataset": "CIC-DDoS2019", ...}`.
     - `"anomaly"` model (`feature_count: 30`) is clearly trained on a transform/subset of the same base flow features.
     - `"malware"` variant `"net_flow_79"` uses 39 features under `schema: "net_flow_39"` but is conceptually attached to network flows derived from the same pipeline.

4. **Inference logic**
   - `MLEngine` uses feature-count heuristics (30 vs 256 vs `ddos_model.n_features_in_`) to route vectors.
   - Flow-based detections (DDoS, anomaly) annotate outputs with `metadata['schema'] = 'flow_79'`.
   - `DetectionPipeline` always uses the 79-feature path as the basis for networking ML and semantics.

### 3. Current Assumptions (CyberMesh)

- Every flow dict feeding the pipeline has **all** 79 feature columns present and numeric.
- The Postgres telemetry table(s) used in production expose:
  - All 79 numeric feature columns.
  - Metadata columns: `label`, `src_ip`, `dst_ip`, `flow_id`.
- The semantics and distributions of the 79 features match the CIC-DDoS2019-style engineered flows used during training.
- DDoS/anomaly models expect input drawn from the same feature distribution (79 and 30 features) as their training datasets.
- File-based telemetry is secondary; the production path assumes curated Postgres flow tables.

---

## Phase 0.B – Sentinel Ground Truth

### 1. Detection Architecture

High-level non-agentic path:

> CLI (`sentinel/cli.py`) → `setup_registry` → `ProviderRegistry` → providers (static + ML + others) → `AnalysisResult`s → `ThreatReport` → console/JSON/HTML

**Provider abstraction**

- `Provider` (`sentinel/providers/base.py`):
  - Abstract interface for all detection providers:
    - `name`, `version`, `supported_types` (set of `FileType`).
    - `analyze(parsed_file: ParsedFile) -> AnalysisResult`.
    - `get_cost_per_call()`, `get_avg_latency_ms()`.
  - Has `can_analyze(parsed_file)` and `get_metadata()` helpers.

- `AnalysisResult` (`sentinel/providers/base.py`):
  - Standardized result structure for providers:
    - `provider_name`, `provider_version`.
    - `threat_level` (`ThreatLevel`: CLEAN, SUSPICIOUS, MALICIOUS, CRITICAL, UNKNOWN).
    - `score`, `confidence` in [0,1].
    - `findings` (list of strings), `indicators` (IoCs), `latency_ms`, `error`, `metadata`.
  - Provides helper methods `is_threat()` and `is_suspicious()`, and `to_dict()`.

**Provider registry and weighting**

- `ProviderRegistry` (`sentinel/providers/registry.py`):
  - Holds
    - `providers: Dict[str, Provider]`.
    - `stats: Dict[str, ProviderStats]`.
  - `ProviderStats` tracks:
    - `weight`, `baseline_weight`.
    - `accuracy`, `total_calls`, `correct_predictions`.
    - `avg_latency_ms`, `cost_per_call`, `enabled` flag.
    - `supported_types`.
  - Responsibilities:
    - `register(provider, baseline_weight, run_eval, eval_suite)`:
      - Optionally runs a benchmark to compute `baseline_weight`.
      - Persists stats to `data/provider_registry.json`.
    - `get_providers_for_type(file_type)` returns compatible providers sorted by `weight` (descending).
    - `update_weight(name, was_correct)`:
      - Uses an EMA of accuracy and baseline_weight to adjust `weight`.
    - `update_latency(name, latency_ms)`, `set_baseline_weight`, `get_summary()`.

**CLI `analyze` path**

- `analyze` command (`sentinel/cli.py`):
  - Loads `Settings` and configures logging.
  - Uses `setup_registry(settings)` to:
    - Register `EntropyProvider` (static) and `StringsProvider` (static) with baseline weights (0.6, 0.7).
    - Optionally register `MalwarePEProvider` (ML) if models exist, with baseline weight (0.85).
  - Detects file type via `detect_file_type(file_path)` and parses via `get_parser_for_file(file_path)` into a `ParsedFile`.
  - Uses `SmartRouter` (`sentinel/router`) with `RoutingConfig(strategy=RoutingStrategy.ENSEMBLE)` to select provider names based on `FileType`.
  - For each provider name:
    - Gets the `Provider` from `ProviderRegistry`, checks `can_analyze(parsed_file)`, and calls `analyze(...)`.
    - Records latency into `ProviderRegistry`.
  - Aggregates all `AnalysisResult`s into a `ThreatReport` (`sentinel/output`):
    - Reports overall threat level, score, confidence, per-provider verdicts, findings, indicators, analysis time.
  - Outputs report in console, JSON, or HTML via `VisualReportGenerator`.

**Agentic/LLM path**

- `agentic` command (`sentinel/cli.py`):
  - Uses `AnalysisEngine` from `sentinel/agents` to run a multi-stage agentic workflow:
    - Combines static providers, ML models, threat intel, and (optionally) LLM reasoning.
  - Produces a structured result with:
    - `threat_level`, `final_score`, `confidence`.
    - `findings`, `indicators`, `reasoning_steps`, `final_reasoning`, `analysis_time_ms`, `stages_completed`.
  - Reuses `ThreatLevel` semantics and similar verdict structure.

### 2. Calibration & Voting (Sentinel)

- `calibrated_config.json` (repo root):
  - Describes calibration and ensemble-like voting for malware detection using EMBER2024 datasets.
  - `global_metrics` summarize AUC, thresholds, FP/FN, evasive malware detection.
  - `voting_config`:
    - `strategy: WEIGHTED_MAJORITY`.
    - `min_malicious_vote_ratio`, `min_suspicious_vote_ratio`, `suspicious_majority_factor`.
  - `provider_thresholds` (per provider):
    - e.g. `malware_pe_ml`, `malware_api_ml`, `entropy_analyzer`, `strings_analyzer`, `yara_rules`, `threat_intel`.
    - Each has:
      - `malicious_threshold`, `suspicious_threshold` (where applicable).
      - `weight` in the voting scheme.
      - Additional notes (e.g. high thresholds due to FPs on clean binaries).
  - `threshold_profiles`:
    - `security_first`, `balanced`, `low_fp` profiles, each with:
      - `ml_threshold`, `min_malicious_ratio`, `min_suspicious_ratio`.
      - Expected detection and FP rates.
  - `findings_config` and `calibration_notes` document:
    - Which severities affect verdicts.
    - Gaps between lab vs real-world performance.
    - Recommendations for recalibration and additional detectors.

### 3. Current Assumptions (Sentinel)

- Providers are **file-centric**: they analyze `ParsedFile` objects and return `AnalysisResult`.
- Coordination across providers is performed through:
  - `ProviderRegistry` weights and stats.
  - External calibration configuration in `calibrated_config.json` (strategy, thresholds, weights, profiles).
- Adding/removing providers (including new LLMs or TI feeds) is largely a matter of:
  - Implementing the `Provider` interface.
  - Registering it in `ProviderRegistry`.
  - Updating calibration config, without changing the core CLI or router logic.
- Threat levels and scores are consistently expressed via `ThreatLevel` + `score`/`confidence` in [0,1].

---

## Phase 0.C – Guardrails & Risk Assessment

### 1. Guardrails (Do-Not-Break Areas)

**CyberMesh guardrails**

- Treat the current DDoS/anomaly detection behavior of AI-Service as **frozen** until the new agent-based pipeline has been validated in shadow mode.
- `FlowFeatureExtractor` and `PostgresTelemetrySource` must **not** be refactored in place during early phases; new ingestion or feature logic must be additive and controlled via configuration flags.
- Any change that touches Kafka topics, message structures, or external contracts (`AnomalyMessage`, `EvidenceMessage`, `PolicyMessage`) requires explicit review and regression testing.
- Changes that alter the feature distributions fed to existing models must go through explicit evaluation and calibration, and cannot be silently deployed.

**Sentinel guardrails**

- Existing Sentinel CLI commands (`analyze`, `agentic`) and `Provider` / `AnalysisResult` APIs remain backward compatible.
- `calibrated_config.json` remains the source of truth for Sentinel-side provider thresholds, weights, and profiles; new ensemble logic must map to it rather than redefine its semantics.
- New abstractions (`DetectionAgent`, `DetectionCandidate`, etc.) in Sentinel are additive; they must not break current reporting or provider flows until a controlled migration is done.

### 2. Risk Assessment (Current State)

**ML/data coupling risks (CyberMesh)**

- Strong coupling to CIC-DDoS2019 feature schema:
  - Telemetry layer (`PostgresTelemetrySource`) assumes the full 79-feature schema and `label='ddos'`.
  - Feature extraction and semantics (`FlowFeatureExtractor`, `FeatureAdapter`) assume CIC-like feature meanings and scales.
  - DDoS/anomaly models in `model_registry.json` are trained specifically on CIC-DDoS2019 features and distributions.
- Lack of a general telemetry/feature abstraction:
  - No generic path for UNSW-NB15, NSL-KDD, CICIDS2017, or arbitrary enterprise flows without schema hacks.

**Integration risks (CyberMesh ↔ Sentinel)**

- Different abstractions:
  - CyberMesh uses Engines and `DetectionCandidate`s based on flow semantics and ML outputs.
  - Sentinel uses Providers and `AnalysisResult`s for file-based analysis.
- Different modalities:
  - CyberMesh: primarily network flows (plus some malware models).
  - Sentinel: primarily files/objects (plus TI and LLM reasoning).
- Different transports and contracts:
  - CyberMesh: Kafka-based messages, ServiceManager lifecycle.
  - Sentinel: CLI/agent-driven workflows delivering local reports.

**Operational risks**

- Latency and cost:
  - As more models and agents are added, naive designs could run all models on all events, blowing latency SLOs and compute budgets.
  - No explicit cascades or resource budgets in the current CyberMesh pipeline.
- Silent degradation:
  - Pointing the existing 79-feature pipeline at different datasets or schemas without retraining/calibration could produce undetected drifts in detection performance.

### 3. How Later Phases Address These Risks

- Phases 1–3:
  - Introduce shared detection contracts (`DetectionCandidate`, `EnsembleDecision`) and canonical feature/event schemas.
  - Implement adapters (`CICDDoS2019Adapter`, etc.) to isolate dataset-specific logic from models.
- Phase 4–5:
  - Add a `DetectionAgentPipeline` and Sentinel integration alongside the existing CyberMesh pipeline, controlled by feature flags and tested in shadow mode.
- Phase 6–7:
  - Establish rollout states (`off`, `shadow`, `prod_low_weight`, `prod_full`) and feedback/calibration loops so that new models/agents/datasets can be added iteratively with clear observability.

---

## Phase 0.D – Readiness Gate (to Phase 1)

Phase 0 is considered **complete** when the following are true:

- CyberMesh architecture is documented (as above) with:
  - End-to-end data path.
  - Explicit CIC-DDoS2019 dependencies.
  - Stated assumptions about flows, features, and models.
- Sentinel architecture is documented (as above) with:
  - Clear provider/registry/voting flows.
  - Provider roles and calibration mechanisms via `calibrated_config.json` and `ProviderRegistry`.
- Guardrails are written and agreed for both CyberMesh and Sentinel.
- ML/data coupling, integration, and operational risks are explicitly listed with a mapping to later phases that will mitigate them.

This document serves as the **Phase 0 artifact** and baseline reference for all subsequent design and implementation work.
