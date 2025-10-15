# Phase 6 - ML Detection Pipeline Design Specification

**Status:** Design Phase  
**Target:** Military-grade, security-first, no mocks  
**Approach:** File-based telemetry → Full ML pipeline → Real datasets  

---

## 1. ARCHITECTURE OVERVIEW

```
┌─────────────────────────────────────────────────────────────────┐
│                         ServiceManager                           │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                   DetectionPipeline                         │ │
│  │                                                             │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌─────────────────┐  │ │
│  │  │ Telemetry    │→ │  Feature     │→ │   3 Engines     │  │ │
│  │  │ Source       │  │  Extraction  │  │  (ML/Rules/Math)│  │ │
│  │  │ (Files)      │  │              │  │                 │  │ │
│  │  └──────────────┘  └──────────────┘  └─────────────────┘  │ │
│  │                                              ↓              │ │
│  │  ┌──────────────┐  ┌──────────────┐  ┌─────────────────┐  │ │
│  │  │   Publish    │← │   Evidence   │← │    Ensemble     │  │ │
│  │  │  (Kafka)     │  │  Generator   │  │    Voter        │  │ │
│  │  └──────────────┘  └──────────────┘  └─────────────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

**Flow:** Telemetry → Features → Engines → Ensemble → Evidence → Kafka

---

## 2. FILE STRUCTURE (8 NEW FILES)

```
src/ml/
├── __init__.py              # ✅ DONE - Exports types/interfaces
├── types.py                 # ✅ DONE - Data structures (DetectionCandidate, etc.)
├── interfaces.py            # ✅ DONE - Engine ABC interface
├── telemetry.py             # NEW - File-based data loading
├── features.py              # NEW - Feature extraction (network + malware)
├── serving.py               # NEW - Model registry + calibration
├── detectors.py             # NEW - 3 engines (ML, Rules, Math)
├── ensemble.py              # NEW - Voting + LLR + abstention
├── evidence.py              # NEW - Quality scoring + custody
├── pipeline.py              # NEW - Orchestrator with instrumentation
└── metrics.py               # NEW - Prometheus metrics

training/                    # NEW - Offline training (separate from src/)
├── train_ddos.py
├── train_malware.py
└── train_anomaly.py
```

---

## 3. DETAILED FILE SPECS

### **3.1 telemetry.py** (~200 lines)

```python
class TelemetrySource(ABC):
    """Abstract telemetry source."""
    @abstractmethod
    def get_network_flows(self, limit: int) -> List[Dict]
    
    @abstractmethod
    def get_files(self, limit: int) -> List[Dict]


class FileTelemetrySource(TelemetrySource):
    """
    Read telemetry from data/telemetry/ directory.
    
    Network flows: JSON files with NetFlow-like structure
    Files: PE binaries or pre-extracted JSON features
    
    Config (from .env):
    - TELEMETRY_FLOWS_PATH (default: data/telemetry/flows)
    - TELEMETRY_FILES_PATH (default: data/telemetry/files)
    """
    
    def __init__(self, config: Dict):
        self.flows_path = Path(config.get("flows_path"))
        self.files_path = Path(config.get("files_path"))
        self.logger = get_logger(__name__)
    
    def get_network_flows(self, limit: int = 100) -> List[Dict]:
        """
        Load network flow data from JSON files.
        
        Expected format:
        {
            "src_ip": "192.168.1.1",
            "dst_ip": "10.0.0.1",
            "src_port": 12345,
            "dst_port": 80,
            "protocol": "TCP",
            "duration": 10.5,
            "packets_fwd": 100,
            "packets_bwd": 50,
            "bytes_fwd": 50000,
            "bytes_bwd": 25000,
            "syn_count": 1,
            "ack_count": 50,
            "timestamp": 1234567890.123
        }
        
        Returns: List of flow dictionaries
        """
        pass
    
    def get_files(self, limit: int = 50) -> List[Dict]:
        """
        Load file/malware data.
        
        Can read:
        1. Pre-extracted JSON (EMBER-style features)
        2. Raw PE files (extract features on-the-fly)
        
        Expected JSON format:
        {
            "file_path": "sample.exe",
            "size": 102400,
            "sections": [...],
            "imports": [...],
            "byte_histogram": [256 values],
            "entropy": 7.2,
            "timestamp": 1234567890.123
        }
        
        Returns: List of file metadata dictionaries
        """
        pass
```

**Dependencies:** pathlib, json, logging  
**Security:** Validate paths (no directory traversal), check file permissions

---

### **3.2 features.py** (~400 lines)

```python
class NetworkFlowExtractor:
    """
    Extract features from network flow data.
    
    Features (30 total):
    - Flow stats: pps, bps, duration
    - Packet ratios: fwd/bwd, SYN/ACK ratio
    - Port statistics: unique ports, well-known ports
    - Inter-arrival times: mean, std, min, max
    - Protocol distribution
    - Window aggregations: 5s, 30s, 5min windows
    """
    
    def __init__(self):
        self.scaler = StandardScaler()  # Fitted on training data
        self.scaler_fitted = False
        self.feature_names = [...]  # 30 features
    
    def extract(self, flows: List[Dict], window_sec: int = 5) -> np.ndarray:
        """
        Extract features from flow records.
        
        Args:
            flows: List of flow dictionaries
            window_sec: Aggregation window (default 5s)
        
        Returns:
            Feature matrix (n_windows, 30)
        """
        # 1. Basic flow stats
        pps = packets / duration
        bps = bytes / duration
        
        # 2. Ratios
        fwd_bwd_ratio = packets_fwd / (packets_bwd + 1e-10)
        syn_ack_ratio = syn_count / (ack_count + 1e-10)
        
        # 3. Port stats
        unique_dst_ports = len(set(dst_ports))
        
        # 4. Inter-arrival times
        iat_mean = np.mean(inter_arrival_times)
        iat_std = np.std(inter_arrival_times)
        
        # 5. Window aggregation
        windows = aggregate_by_time(flows, window_sec)
        for window in windows:
            window_features = [pps_mean, pps_std, pps_max, ...]
        
        # 6. Normalize
        if self.scaler_fitted:
            features = self.scaler.transform(features)
        
        return features


class MalwareStaticExtractor:
    """
    Extract EMBER-style static features from PE files.
    
    Features (256 total, EMBER-compatible):
    - General: size, vsize, has_debug, has_relocations (10)
    - Header: machine, characteristics, dll_characteristics (10)
    - Sections: count, names (hashed), sizes, entropy (40)
    - Imports: library count, function count, common APIs (50)
    - Exports: count, names (20)
    - Byte histogram: 256-bin histogram (256 → PCA to 50)
    - Strings: printable %, avg length, entropy (10)
    - Data directories: count, sizes (10)
    """
    
    def __init__(self):
        self.scaler = StandardScaler()
        self.scaler_fitted = False
        self.feature_names = [...]  # 256 features
    
    def extract(self, file_data: Dict) -> np.ndarray:
        """
        Extract features from PE file or pre-extracted JSON.
        
        Args:
            file_data: File metadata dictionary or path
        
        Returns:
            Feature vector (256,)
        """
        # If pre-extracted JSON, just validate and normalize
        if "byte_histogram" in file_data:
            features = self._from_json(file_data)
        else:
            # Extract from raw PE (requires pefile library)
            features = self._from_pe(file_data["file_path"])
        
        # Normalize
        if self.scaler_fitted:
            features = self.scaler.transform(features.reshape(1, -1))
        
        return features[0]
    
    def _compute_entropy(self, data: bytes) -> float:
        """Shannon entropy of byte sequence."""
        if len(data) == 0:
            return 0.0
        
        histogram = np.bincount(np.frombuffer(data, dtype=np.uint8), minlength=256)
        probabilities = histogram / len(data)
        probabilities = probabilities[probabilities > 0]
        
        return -np.sum(probabilities * np.log2(probabilities))
```

**Dependencies:** numpy, scikit-learn  
**Security:** Validate file paths, limit file size (max 50MB), sandbox PE parsing

---

### **3.3 serving.py** (~400 lines)

```python
class ModelRegistry:
    """
    Model loading with Ed25519 signature verification.
    
    Security:
    - All models must be signed with SIGNING_KEY
    - Signature verified on load (SHA-256 hash signed)
    - Model fingerprints tracked in model_registry.json
    - Hot-reload with blue/green deployment
    """
    
    def __init__(self, config: Dict, signer: Signer):
        self.models_path = Path(config.get("models_path", "data/models"))
        self.signer = signer
        self.registry = self._load_registry()
        self.loaded_models = {}  # {model_name: model_object}
        self.calibrators = {}    # {model_name: CalibratedClassifierCV}
        self.logger = get_logger(__name__)
    
    def load_model(self, model_name: str, version: str = "latest") -> Any:
        """
        Load and verify model.
        
        Args:
            model_name: "ddos_lgbm", "malware_lgbm", "anomaly_iforest"
            version: Semantic version or "latest"
        
        Returns:
            Loaded model object
        
        Raises:
            ValueError: If signature invalid or model not found
            RuntimeError: If model loading fails
        """
        # 1. Get model metadata
        metadata = self.registry.get(model_name, {}).get(version)
        if not metadata:
            raise ValueError(f"Model {model_name}:{version} not in registry")
        
        # 2. Load model file
        model_path = self.models_path / metadata["filename"]
        sig_path = self.models_path / metadata["signature_file"]
        
        # 3. Verify signature
        with open(model_path, "rb") as f:
            model_bytes = f.read()
        
        model_hash = hashlib.sha256(model_bytes).digest()
        
        with open(sig_path, "rb") as f:
            signature = f.read()
        
        try:
            self.signer.verify_signature(model_hash, signature)
        except Exception as e:
            raise ValueError(f"Model signature verification failed: {e}")
        
        # 4. Load with joblib
        model = joblib.load(model_path)
        
        # 5. Cache
        self.loaded_models[model_name] = model
        
        self.logger.info(f"Loaded model: {model_name}:{version}")
        return model
    
    def get_calibrated_model(self, model_name: str) -> CalibratedClassifierCV:
        """
        Get model wrapped in calibration.
        
        Returns:
            Calibrated model (Platt or Isotonic scaling applied)
        """
        if model_name in self.calibrators:
            return self.calibrators[model_name]
        
        # Load base model
        base_model = self.load_model(model_name)
        
        # Calibration already applied during training (saved as calibrated)
        # Just return wrapped version
        return base_model
    
    def hot_reload(self, model_name: str):
        """
        Blue/green model reload without downtime.
        
        Process:
        1. Load new model version
        2. Verify signature
        3. Run validation checks
        4. Swap atomically
        5. Keep old model for 60s (graceful rollback)
        """
        # TODO: Implement in Phase 6.5
        pass
    
    def _load_registry(self) -> Dict:
        """Load model_registry.json."""
        registry_path = self.models_path / "model_registry.json"
        if not registry_path.exists():
            return {}
        
        with open(registry_path) as f:
            return json.load(f)
```

**Dependencies:** joblib, hashlib, json, pathlib  
**Security:** Ed25519 verification, file permission checks (0600), size limits

---

### **3.4 detectors.py** (~600 lines) - **LARGEST FILE**

```python
class MLEngine(Engine):
    """
    ML-based detection engine.
    
    Models:
    - ddos_lgbm: LightGBM for DDoS/DoS detection
    - malware_lgbm: LightGBM for malware detection
    - anomaly_iforest: IsolationForest for anomaly detection
    
    All models calibrated (Platt/Isotonic) for probability outputs.
    """
    
    def __init__(self, registry: ModelRegistry, config: Dict):
        self.registry = registry
        self.config = config
        self.models = {}
        self.logger = get_logger(__name__)
        self._ready = False
    
    def initialize(self):
        """Load all models from registry."""
        try:
            self.models["ddos"] = self.registry.load_model("ddos_lgbm")
            self.models["malware"] = self.registry.load_model("malware_lgbm")
            self.models["anomaly"] = self.registry.load_model("anomaly_iforest")
            self._ready = True
        except Exception as e:
            self.logger.error(f"Failed to load models: {e}")
            self._ready = False
    
    def predict(self, features: np.ndarray, batch: bool = False) -> List[DetectionCandidate]:
        """
        Run inference on all 3 models.
        
        Args:
            features: Feature vector (30 for network, 256 for malware)
            batch: Enable batch inference
        
        Returns:
            List of DetectionCandidate (one per model, if score > threshold)
        """
        if not self._ready:
            return []
        
        candidates = []
        
        # Network flow features → DDoS + Anomaly models
        if features.shape[-1] == 30:  # Network features
            # DDoS model
            ddos_score = self.models["ddos"].predict_proba(features.reshape(1, -1))[0, 1]
            ddos_conf = self._compute_confidence(self.models["ddos"], features)
            
            if ddos_score > 0.3:  # Pre-ensemble threshold
                candidates.append(DetectionCandidate(
                    threat_type=ThreatType.DDOS,
                    raw_score=ddos_score,
                    calibrated_score=ddos_score,  # Already calibrated
                    confidence=ddos_conf,
                    engine_type=EngineType.ML,
                    engine_name="ddos_lgbm",
                    metadata={"model_version": "1.0.0"}
                ))
            
            # Anomaly model
            anomaly_score = self._iforest_to_prob(self.models["anomaly"].decision_function(features.reshape(1, -1))[0])
            
            if anomaly_score > 0.3:
                candidates.append(DetectionCandidate(
                    threat_type=ThreatType.ANOMALY,
                    raw_score=anomaly_score,
                    calibrated_score=anomaly_score,
                    confidence=0.8,  # IsolationForest has lower confidence
                    engine_type=EngineType.ML,
                    engine_name="anomaly_iforest",
                    metadata={"model_version": "1.0.0"}
                ))
        
        # File features → Malware model
        elif features.shape[-1] == 256:  # Malware features
            malware_score = self.models["malware"].predict_proba(features.reshape(1, -1))[0, 1]
            malware_conf = self._compute_confidence(self.models["malware"], features)
            
            if malware_score > 0.5:  # Higher threshold for malware
                candidates.append(DetectionCandidate(
                    threat_type=ThreatType.MALWARE,
                    raw_score=malware_score,
                    calibrated_score=malware_score,
                    confidence=malware_conf,
                    engine_type=EngineType.ML,
                    engine_name="malware_lgbm",
                    metadata={"model_version": "1.0.0"}
                ))
        
        return candidates
    
    def _compute_confidence(self, model, features: np.ndarray) -> float:
        """
        Compute model confidence (not just probability).
        
        Methods:
        - Prediction margin (difference between top 2 classes)
        - Calibration curve distance
        - Model ensemble variance (if available)
        """
        probs = model.predict_proba(features.reshape(1, -1))[0]
        margin = abs(probs[1] - probs[0])  # Distance from decision boundary
        return min(margin * 2, 1.0)  # Scale to [0,1]
    
    def _iforest_to_prob(self, score: float) -> float:
        """Convert IsolationForest anomaly score to probability."""
        # IsolationForest returns negative scores (more negative = more anomalous)
        # Transform to [0,1] probability
        return 1.0 / (1.0 + np.exp(score))  # Sigmoid transform
    
    @property
    def engine_type(self) -> EngineType:
        return EngineType.ML
    
    @property
    def is_ready(self) -> bool:
        return self._ready
    
    def get_metadata(self) -> dict:
        return {
            "engine": "ml",
            "models_loaded": len(self.models),
            "models": list(self.models.keys())
        }
    
    def calibrate(self, method: str = "platt"):
        """Calibration done during training, no-op here."""
        pass


class RulesEngine(Engine):
    """
    Threshold-based rule engine.
    
    Rules:
    - DDoS: pps > 100k OR unique_dst_ports > 1000
    - Port scan: unique_dst_ports > 500 AND syn_ack_ratio > 10
    - Malware: entropy > 7.5 OR suspicious_imports
    
    Configurable via .env or policy updates.
    """
    
    def __init__(self, config: Dict):
        self.config = config
        self.thresholds = {
            "ddos_pps": config.get("DDOS_PPS_THRESHOLD", 100000),
            "port_scan_ports": config.get("PORT_SCAN_THRESHOLD", 500),
            "malware_entropy": config.get("MALWARE_ENTROPY_THRESHOLD", 7.5),
        }
        self.blacklists = {
            "ips": set(config.get("IP_BLACKLIST", "").split(",")),
            "imports": {"VirtualAllocEx", "WriteProcessMemory", "CreateRemoteThread"}  # Injection APIs
        }
        self.logger = get_logger(__name__)
        self._ready = True
    
    def predict(self, features: np.ndarray, batch: bool = False) -> List[DetectionCandidate]:
        """
        Apply threshold rules.
        
        Returns:
            List of DetectionCandidate (one per triggered rule)
        """
        candidates = []
        
        # Network flow rules
        if features.shape[-1] == 30:
            pps = features[0]  # Assume pps is first feature
            unique_ports = features[5]  # Assume unique_dst_ports is 6th feature
            
            # DDoS rule
            if pps > self.thresholds["ddos_pps"]:
                score = min(pps / self.thresholds["ddos_pps"], 1.0)
                candidates.append(DetectionCandidate(
                    threat_type=ThreatType.DDOS,
                    raw_score=score,
                    calibrated_score=score,
                    confidence=0.9,  # Rules have high confidence
                    engine_type=EngineType.RULES,
                    engine_name="ddos_pps_rule",
                    metadata={"threshold": self.thresholds["ddos_pps"], "value": pps}
                ))
            
            # Port scan rule
            if unique_ports > self.thresholds["port_scan_ports"]:
                score = min(unique_ports / self.thresholds["port_scan_ports"], 1.0)
                candidates.append(DetectionCandidate(
                    threat_type=ThreatType.NETWORK_INTRUSION,
                    raw_score=score,
                    calibrated_score=score,
                    confidence=0.85,
                    engine_type=EngineType.RULES,
                    engine_name="port_scan_rule",
                    metadata={"threshold": self.thresholds["port_scan_ports"], "value": unique_ports}
                ))
        
        # Malware rules
        elif features.shape[-1] == 256:
            entropy = features[10]  # Assume entropy is 11th feature
            
            if entropy > self.thresholds["malware_entropy"]:
                score = min(entropy / 8.0, 1.0)  # Max entropy is 8
                candidates.append(DetectionCandidate(
                    threat_type=ThreatType.MALWARE,
                    raw_score=score,
                    calibrated_score=score,
                    confidence=0.75,  # Lower confidence for heuristic
                    engine_type=EngineType.RULES,
                    engine_name="high_entropy_rule",
                    metadata={"threshold": self.thresholds["malware_entropy"], "value": entropy}
                ))
        
        return candidates
    
    @property
    def engine_type(self) -> EngineType:
        return EngineType.RULES
    
    @property
    def is_ready(self) -> bool:
        return self._ready
    
    def get_metadata(self) -> dict:
        return {
            "engine": "rules",
            "thresholds": self.thresholds,
            "blacklists": {k: len(v) for k, v in self.blacklists.items()}
        }
    
    def calibrate(self, method: str = "platt"):
        """Rules don't need calibration."""
        pass
    
    def update_thresholds(self, new_thresholds: Dict):
        """Update thresholds from policy events."""
        self.thresholds.update(new_thresholds)
        self.logger.info(f"Updated thresholds: {new_thresholds}")


class MathEngine(Engine):
    """
    Statistical/mathematical detection engine.
    
    Formulas (5):
    1. Shannon Entropy - Port distribution anomaly
    2. Z-Score - Statistical outlier detection
    3. Mahalanobis Distance - Multivariate anomaly
    4. CUSUM/EWMA - Temporal drift detection
    5. Hellinger Distance - Distribution comparison
    """
    
    def __init__(self, config: Dict):
        self.config = config
        self.baseline_stats = self._load_baseline()  # Mean/cov from benign data
        self.cusum_state = {}  # Per-feature CUSUM state
        self.ewma_state = {}   # Per-feature EWMA state
        self.logger = get_logger(__name__)
        self._ready = True
    
    def predict(self, features: np.ndarray, batch: bool = False) -> List[DetectionCandidate]:
        """
        Apply 5 mathematical formulas.
        
        Returns:
            List of DetectionCandidate (one per triggered formula)
        """
        candidates = []
        
        # 1. Shannon Entropy (on port distribution)
        if features.shape[-1] == 30:
            port_entropy = self._shannon_entropy(features)
            if port_entropy < 2.0:  # Low entropy = port scan
                candidates.append(DetectionCandidate(
                    threat_type=ThreatType.NETWORK_INTRUSION,
                    raw_score=(2.0 - port_entropy) / 2.0,
                    calibrated_score=(2.0 - port_entropy) / 2.0,
                    confidence=0.8,
                    engine_type=EngineType.MATH,
                    engine_name="shannon_entropy",
                    metadata={"entropy": port_entropy, "threshold": 2.0}
                ))
        
        # 2. Z-Score (on each feature)
        z_scores = self._compute_z_scores(features)
        max_z = np.max(np.abs(z_scores))
        if max_z > 3.0:  # 3-sigma rule
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.ANOMALY,
                raw_score=min(max_z / 5.0, 1.0),
                calibrated_score=min(max_z / 5.0, 1.0),
                confidence=0.7,
                engine_type=EngineType.MATH,
                engine_name="z_score",
                metadata={"max_z": max_z, "feature_idx": np.argmax(np.abs(z_scores))}
            ))
        
        # 3. Mahalanobis Distance
        mahal_dist = self._mahalanobis_distance(features)
        if mahal_dist > 10.0:  # Threshold from chi-square distribution
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.ANOMALY,
                raw_score=min(mahal_dist / 20.0, 1.0),
                calibrated_score=min(mahal_dist / 20.0, 1.0),
                confidence=0.85,
                engine_type=EngineType.MATH,
                engine_name="mahalanobis",
                metadata={"distance": mahal_dist, "threshold": 10.0}
            ))
        
        # 4. CUSUM (detect drift)
        cusum_alert = self._check_cusum(features)
        if cusum_alert:
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.ANOMALY,
                raw_score=0.8,
                calibrated_score=0.8,
                confidence=0.75,
                engine_type=EngineType.MATH,
                engine_name="cusum_drift",
                metadata={"cusum_value": cusum_alert}
            ))
        
        # 5. Hellinger Distance (distribution comparison)
        hellinger = self._hellinger_distance(features)
        if hellinger > 0.5:
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.ANOMALY,
                raw_score=hellinger,
                calibrated_score=hellinger,
                confidence=0.7,
                engine_type=EngineType.MATH,
                engine_name="hellinger",
                metadata={"distance": hellinger, "threshold": 0.5}
            ))
        
        return candidates
    
    def _shannon_entropy(self, features: np.ndarray) -> float:
        """Formula #1: Shannon Entropy = -Σ(p_i × log₂(p_i))"""
        # Assume features[5] is port distribution histogram
        port_hist = features[5:15]  # Example: 10-bin port histogram
        port_hist = port_hist / (np.sum(port_hist) + 1e-10)  # Normalize
        port_hist = port_hist[port_hist > 0]
        return -np.sum(port_hist * np.log2(port_hist))
    
    def _compute_z_scores(self, features: np.ndarray) -> np.ndarray:
        """Formula #2: Z-Score = |x - μ| / σ"""
        mean = self.baseline_stats["mean"]
        std = self.baseline_stats["std"]
        return (features - mean) / (std + 1e-10)
    
    def _mahalanobis_distance(self, features: np.ndarray) -> float:
        """Formula #3: D² = (x - μ)ᵀ Σ⁻¹ (x - μ)"""
        mean = self.baseline_stats["mean"]
        cov_inv = self.baseline_stats["cov_inv"]
        diff = features - mean
        return np.sqrt(diff @ cov_inv @ diff.T)
    
    def _check_cusum(self, features: np.ndarray) -> Optional[float]:
        """Formula #4: CUSUM = max(0, S_{t-1} + (x_t - μ - k))"""
        # Simplified: check if any feature exceeds CUSUM threshold
        # Full implementation would track state over time
        return None  # Placeholder
    
    def _hellinger_distance(self, features: np.ndarray) -> float:
        """Formula #5: H(P,Q) = √(1 - Σ√(p_i × q_i))"""
        # Compare feature distribution to baseline
        # Simplified: compare histogram
        baseline_hist = self.baseline_stats.get("histogram", np.ones(len(features)))
        baseline_hist = baseline_hist / np.sum(baseline_hist)
        current_hist = features / (np.sum(features) + 1e-10)
        
        return np.sqrt(1 - np.sum(np.sqrt(baseline_hist * current_hist)))
    
    def _load_baseline(self) -> Dict:
        """Load baseline statistics from benign data."""
        # TODO: Compute during training and save
        # For now, return dummy values
        return {
            "mean": np.zeros(30),  # Will be computed from training data
            "std": np.ones(30),
            "cov_inv": np.eye(30),
            "histogram": np.ones(30)
        }
    
    @property
    def engine_type(self) -> EngineType:
        return EngineType.MATH
    
    @property
    def is_ready(self) -> bool:
        return self._ready
    
    def get_metadata(self) -> dict:
        return {
            "engine": "math",
            "formulas": ["shannon_entropy", "z_score", "mahalanobis", "cusum", "hellinger"]
        }
    
    def calibrate(self, method: str = "platt"):
        """Math formulas don't need calibration."""
        pass
```

**Dependencies:** numpy, scipy  
**Security:** Validate feature ranges, prevent numerical overflow

---

### **3.5 ensemble.py** (~500 lines)

```python
class EnsembleVoter:
    """
    Ensemble decision with weighted voting + abstention.
    
    Formulas:
    - #4: Trust-weighted confidence
    - #7: F-beta score threshold calibration
    - #9: Weighted ensemble voting
    - #11: Log-likelihood ratio (LLR)
    - #12: Uncertainty-aware ensemble
    - #5: BFT resilience
    """
    
    def __init__(self, config: Dict):
        self.weights = {
            EngineType.ML: config.get("ML_WEIGHT", 0.5),
            EngineType.RULES: config.get("RULES_WEIGHT", 0.3),
            EngineType.MATH: config.get("MATH_WEIGHT", 0.2),
        }
        self.thresholds = {
            ThreatType.DDOS: config.get("DDOS_THRESHOLD", 0.7),
            ThreatType.MALWARE: config.get("MALWARE_THRESHOLD", 0.85),
            ThreatType.ANOMALY: config.get("ANOMALY_THRESHOLD", 0.75),
        }
        self.min_confidence = config.get("MIN_CONFIDENCE", 0.85)
        self.trust_scores = {EngineType.ML: 1.0, EngineType.RULES: 0.9, EngineType.MATH: 0.8}
        self.logger = get_logger(__name__)
    
    def decide(self, candidates: List[DetectionCandidate]) -> EnsembleDecision:
        """
        Make final decision with abstention logic.
        
        Process:
        1. Group candidates by threat_type
        2. For each threat_type:
           a. Weighted voting (Formula #9)
           b. Trust-weighted confidence (Formula #4)
           c. Uncertainty-aware weighting (Formula #12)
           d. Compute LLR (Formula #11)
        3. Apply abstention logic
        4. Return decision
        
        Args:
            candidates: List of DetectionCandidate from all engines
        
        Returns:
            EnsembleDecision with should_publish flag
        """
        if not candidates:
            return EnsembleDecision(
                should_publish=False,
                threat_type=ThreatType.ANOMALY,
                final_score=0.0,
                confidence=0.0,
                llr=0.0,
                candidates=[],
                abstention_reason="no_candidates"
            )
        
        # Group by threat type
        by_threat = {}
        for c in candidates:
            by_threat.setdefault(c.threat_type, []).append(c)
        
        # Vote on each threat type
        decisions = []
        for threat_type, threat_candidates in by_threat.items():
            decision = self._vote_on_threat(threat_type, threat_candidates)
            decisions.append(decision)
        
        # Pick highest score
        best_decision = max(decisions, key=lambda d: d.final_score)
        
        # Apply abstention logic
        if best_decision.confidence < self.min_confidence:
            best_decision.should_publish = False
            best_decision.abstention_reason = f"confidence_too_low_{best_decision.confidence:.2f}"
        elif best_decision.final_score < self.thresholds.get(best_decision.threat_type, 0.7):
            best_decision.should_publish = False
            best_decision.abstention_reason = f"score_below_threshold_{best_decision.final_score:.2f}"
        else:
            best_decision.should_publish = True
        
        return best_decision
    
    def _vote_on_threat(self, threat_type: ThreatType, candidates: List[DetectionCandidate]) -> EnsembleDecision:
        """
        Vote on single threat type.
        
        Formulas applied:
        - #9: Weighted voting
        - #4: Trust-weighted confidence
        - #12: Uncertainty-aware weighting
        - #11: LLR
        """
        # Formula #12: Uncertainty-aware weighting
        # weight_i = accuracy_i / (uncertainty_i + ε)
        adjusted_weights = []
        scores = []
        confidences = []
        
        for c in candidates:
            engine_weight = self.weights[c.engine_type]
            trust = self.trust_scores[c.engine_type]
            uncertainty = 1.0 - c.confidence
            
            # Adjust weight by uncertainty
            adjusted_weight = (engine_weight * trust) / (uncertainty + 0.1)
            adjusted_weights.append(adjusted_weight)
            scores.append(c.calibrated_score)
            confidences.append(c.confidence)
        
        # Formula #9: Weighted voting
        # weighted_sum = Σ(weight × score) / Σ(weight)
        total_weight = sum(adjusted_weights)
        final_score = sum(w * s for w, s in zip(adjusted_weights, scores)) / total_weight
        
        # Formula #4: Trust-weighted confidence
        # confidence = Σ(trust_i × score_i) / Σ(trust_i)
        trust_weights = [self.trust_scores[c.engine_type] for c in candidates]
        final_confidence = sum(t * conf for t, conf in zip(trust_weights, confidences)) / sum(trust_weights)
        
        # Formula #11: Log-likelihood ratio
        llr = self.compute_llr(final_score)
        
        return EnsembleDecision(
            should_publish=False,  # Will be set by abstention logic
            threat_type=threat_type,
            final_score=final_score,
            confidence=final_confidence,
            llr=llr,
            candidates=candidates,
            metadata={
                "weights": adjusted_weights,
                "engine_types": [c.engine_type.value for c in candidates]
            }
        )
    
    def compute_llr(self, calibrated_score: float) -> float:
        """
        Formula #11: Log-likelihood ratio
        LLR = log(P(E|malicious) / P(E|benign))
            = log(score / (1 - score))
        
        Args:
            calibrated_score: Calibrated probability [0,1]
        
        Returns:
            LLR value (positive = evidence for malicious)
        """
        # Avoid log(0)
        score = np.clip(calibrated_score, 1e-10, 1 - 1e-10)
        return np.log(score / (1 - score))
    
    def compute_bft_resilience(self, num_nodes: int, max_faulty: int) -> float:
        """
        Formula #5: BFT Resilience
        resilience = 1 - f/(3f+1)
        
        Args:
            num_nodes: Total nodes in cluster
            max_faulty: Maximum faulty nodes tolerated
        
        Returns:
            Resilience score [0,1]
        """
        if max_faulty == 0:
            return 1.0
        return 1 - (max_faulty / (3 * max_faulty + 1))
    
    def calibrate_threshold_fbeta(self, threat_type: ThreatType, beta: float, 
                                   y_true: np.ndarray, y_scores: np.ndarray) -> float:
        """
        Formula #7: F-beta score threshold calibration
        F_β = (1+β²) × (precision × recall) / (β² × precision + recall)
        
        Find threshold that maximizes F-beta:
        - β=2 for DDoS (favor recall)
        - β=0.5 for malware (favor precision)
        
        Args:
            threat_type: Threat type
            beta: Beta parameter (2 for recall, 0.5 for precision)
            y_true: Ground truth labels
            y_scores: Predicted scores
        
        Returns:
            Optimal threshold
        """
        from sklearn.metrics import fbeta_score
        
        best_threshold = 0.5
        best_fbeta = 0.0
        
        for threshold in np.arange(0.1, 0.95, 0.05):
            y_pred = (y_scores >= threshold).astype(int)
            fbeta = fbeta_score(y_true, y_pred, beta=beta, zero_division=0)
            
            if fbeta > best_fbeta:
                best_fbeta = fbeta
                best_threshold = threshold
        
        self.thresholds[threat_type] = best_threshold
        self.logger.info(f"Calibrated {threat_type} threshold: {best_threshold:.2f} (F{beta}={best_fbeta:.3f})")
        return best_threshold
    
    def update_weights(self, feedback: Dict):
        """
        Update engine weights based on backend feedback.
        
        Called from handle_reputation_event() when backend confirms/rejects detection.
        """
        # Increase weight for correct engines, decrease for incorrect
        for engine_type, correct in feedback.items():
            if correct:
                self.trust_scores[engine_type] = min(1.0, self.trust_scores[engine_type] * 1.1)
            else:
                self.trust_scores[engine_type] = max(0.1, self.trust_scores[engine_type] * 0.9)
        
        self.logger.info(f"Updated trust scores: {self.trust_scores}")
```

**Dependencies:** numpy, scikit-learn  
**Security:** Validate weights sum to reasonable values, prevent division by zero

---

### **3.6 evidence.py** (~300 lines)

```python
class EvidenceGenerator:
    """
    Generate evidence packages with quality scoring.
    
    Formula #11: Evidence strength (log-likelihood)
    """
    
    def __init__(self, signer: Signer, config: Dict):
        self.signer = signer
        self.config = config
        self.nonce_manager = NonceManager(...)  # Reuse existing
        self.logger = get_logger(__name__)
    
    def generate(self, decision: EnsembleDecision, 
                 raw_features: Dict, 
                 node_id: str) -> bytes:
        """
        Generate signed evidence package.
        
        Process:
        1. Compute evidence quality score
        2. Build chain-of-custody
        3. Package top-N features
        4. Build protobuf EvidenceMessage
        5. Sign with Ed25519
        
        Args:
            decision: Ensemble decision
            raw_features: Original feature data for explainability
            node_id: This node's ID
        
        Returns:
            Serialized, signed EvidenceMessage
        """
        # 1. Quality scoring
        quality_score = self.compute_evidence_strength(decision)
        
        # 2. Chain-of-custody
        custody_chain = self._build_custody_chain(decision, node_id)
        
        # 3. Top-N features
        top_features = self._extract_top_features(decision, raw_features)
        
        # 4. Build protobuf
        evidence_msg = EvidenceMessage(
            evidence_id=str(uuid.uuid4()),
            related_anomaly_id=decision.metadata.get("anomaly_id", ""),
            quality_score=quality_score,
            evidence_type="detection",
            data=json.dumps({
                "threat_type": decision.threat_type.value,
                "final_score": decision.final_score,
                "confidence": decision.confidence,
                "llr": decision.llr,
                "top_features": top_features,
                "engines": [c.engine_name for c in decision.candidates]
            }).encode(),
            timestamp=int(time.time()),
            chain_of_custody=[custody_chain]
        )
        
        # 5. Sign
        payload = evidence_msg.SerializeToString()
        content_hash = hashlib.sha256(payload).digest()
        signature = self.signer.sign(content_hash)
        nonce = self.nonce_manager.generate()
        
        evidence_msg.signature = signature
        evidence_msg.nonce = nonce
        
        return evidence_msg.SerializeToString()
    
    def compute_evidence_strength(self, decision: EnsembleDecision) -> float:
        """
        Formula #11: Evidence Strength = log(P(E|H1) / P(E|H0))
        
        Where:
        - H1 = malicious hypothesis
        - H0 = benign hypothesis
        - E = observed evidence (detection candidates)
        
        Uses LLR from decision + quality factors:
        - Number of agreeing engines
        - Confidence consistency
        - Detection persistence (if available)
        
        Returns:
            Quality score [0,1]
        """
        # Base strength from LLR
        base_strength = 1.0 / (1.0 + np.exp(-decision.llr))  # Sigmoid of LLR
        
        # Agreement bonus (more engines = stronger evidence)
        num_engines = len(set(c.engine_type for c in decision.candidates))
        agreement_bonus = num_engines / 3.0  # Max 3 engine types
        
        # Confidence consistency (low variance = more reliable)
        confidences = [c.confidence for c in decision.candidates]
        conf_variance = np.var(confidences)
        consistency_bonus = 1.0 - min(conf_variance, 1.0)
        
        # Combine
        quality = (base_strength * 0.5 + 
                   agreement_bonus * 0.3 + 
                   consistency_bonus * 0.2)
        
        return np.clip(quality, 0.0, 1.0)
    
    def _build_custody_chain(self, decision: EnsembleDecision, node_id: str) -> ChainOfCustody:
        """
        Build cryptographic custody chain.
        
        Chain:
        1. Detection (this node)
        2. Ensemble vote (this node)
        3. Evidence generation (this node)
        
        Each step signed.
        """
        custody = ChainOfCustody(
            handler_node_id=node_id,
            timestamp=int(time.time()),
            action="evidence_generation",
            previous_hash=b"",  # TODO: Hash of previous step
            data_fingerprint=hashlib.sha256(str(decision.candidates).encode()).digest()
        )
        
        # Sign custody entry
        custody_bytes = f"{custody.handler_node_id}{custody.timestamp}{custody.action}".encode()
        custody.signature = self.signer.sign(custody_bytes)
        
        return custody
    
    def _extract_top_features(self, decision: EnsembleDecision, 
                              raw_features: Dict, top_n: int = 10) -> List[Dict]:
        """
        Extract top-N contributing features for explainability.
        
        Methods:
        - SHAP values (if available)
        - Feature importance from models
        - Heuristic: features with highest deviation from baseline
        
        Returns:
            List of {feature_name, value, importance}
        """
        # Simplified: return features from candidates metadata
        top_features = []
        for candidate in decision.candidates[:top_n]:
            if candidate.features:
                for feat_name, feat_value in list(candidate.features.items())[:3]:
                    top_features.append({
                        "name": feat_name,
                        "value": float(feat_value),
                        "source": candidate.engine_name
                    })
        
        return top_features[:top_n]
    
    def verify_custody_chain(self, evidence: EvidenceMessage) -> bool:
        """
        Verify chain-of-custody integrity.
        
        Checks:
        - All signatures valid
        - Hash chain unbroken
        - Timestamps monotonic
        - No tampering
        
        Returns:
            True if chain valid
        """
        # TODO: Implement full verification
        return True
```

**Dependencies:** hashlib, uuid, json, time  
**Security:** Ed25519 signing on all custody entries, tamper detection

---

### **3.7 pipeline.py** (~400 lines)

```python
class DetectionPipeline:
    """
    Main orchestrator with full instrumentation.
    
    Flow:
    telemetry → features → engines → ensemble → evidence
    
    Instrumented with:
    - Per-stage latency tracking
    - Prometheus metrics
    - Circuit breaker
    - Error handling
    """
    
    def __init__(self, 
                 telemetry_source: TelemetrySource,
                 feature_extractors: Dict,
                 engines: List[Engine],
                 ensemble: EnsembleVoter,
                 evidence_generator: EvidenceGenerator,
                 metrics: PrometheusMetrics,
                 circuit_breaker: CircuitBreaker):
        self.telemetry = telemetry_source
        self.extractors = feature_extractors
        self.engines = engines
        self.ensemble = ensemble
        self.evidence_gen = evidence_generator
        self.metrics = metrics
        self.circuit_breaker = circuit_breaker
        self.logger = get_logger(__name__)
    
    def process(self, trigger_event: Optional[Dict] = None) -> InstrumentedResult:
        """
        Run full detection pipeline.
        
        Args:
            trigger_event: Optional commit event that triggered this (for metadata)
        
        Returns:
            InstrumentedResult with decision and latency breakdown
        """
        start_time = time.perf_counter()
        latencies = {}
        
        try:
            # Stage 1: Load telemetry
            stage_start = time.perf_counter()
            flows = self.telemetry.get_network_flows(limit=100)
            files = self.telemetry.get_files(limit=50)
            latencies["telemetry_load"] = (time.perf_counter() - stage_start) * 1000
            
            if not flows and not files:
                return InstrumentedResult(
                    decision=None,
                    latency_ms=latencies,
                    total_latency_ms=(time.perf_counter() - start_time) * 1000,
                    error="no_telemetry_data"
                )
            
            # Stage 2: Feature extraction
            stage_start = time.perf_counter()
            all_features = []
            feature_count = 0
            
            if flows:
                network_features = self.extractors["network"].extract(flows)
                all_features.append(("network", network_features))
                feature_count += network_features.shape[0] * network_features.shape[1]
            
            if files:
                for file_data in files:
                    malware_features = self.extractors["malware"].extract(file_data)
                    all_features.append(("malware", malware_features))
                    feature_count += malware_features.shape[0]
            
            latencies["feature_extraction"] = (time.perf_counter() - stage_start) * 1000
            
            # Stage 3: Run all engines
            stage_start = time.perf_counter()
            all_candidates = []
            
            for feature_type, features in all_features:
                for engine in self.engines:
                    if not engine.is_ready:
                        continue
                    
                    try:
                        candidates = engine.predict(features, batch=False)
                        all_candidates.extend(candidates)
                    except Exception as e:
                        self.logger.error(f"Engine {engine.engine_type} failed: {e}", exc_info=True)
            
            latencies["engine_inference"] = (time.perf_counter() - stage_start) * 1000
            
            # Stage 4: Ensemble decision
            stage_start = time.perf_counter()
            decision = self.ensemble.decide(all_candidates)
            latencies["ensemble_vote"] = (time.perf_counter() - stage_start) * 1000
            
            # Stage 5: Evidence generation (if publishing)
            if decision.should_publish:
                stage_start = time.perf_counter()
                evidence = self.evidence_gen.generate(
                    decision=decision,
                    raw_features={"flows": flows, "files": files},
                    node_id=self.config.get("NODE_ID", "unknown")
                )
                decision.metadata["evidence"] = evidence
                latencies["evidence_generation"] = (time.perf_counter() - stage_start) * 1000
            
            # Calculate total
            total_latency = (time.perf_counter() - start_time) * 1000
            
            # Record metrics
            self.metrics.record_pipeline_result(decision, latencies, total_latency)
            
            return InstrumentedResult(
                decision=decision,
                latency_ms=latencies,
                total_latency_ms=total_latency,
                feature_count=feature_count,
                candidate_count=len(all_candidates)
            )
        
        except Exception as e:
            self.logger.error(f"Pipeline failed: {e}", exc_info=True)
            self.circuit_breaker.record_failure()
            
            return InstrumentedResult(
                decision=None,
                latency_ms=latencies,
                total_latency_ms=(time.perf_counter() - start_time) * 1000,
                error=str(e)
            )
```

**Dependencies:** time  
**Security:** Exception handling, circuit breaker integration

---

### **3.8 metrics.py** (~200 lines)

```python
from prometheus_client import Histogram, Counter, Gauge

class PrometheusMetrics:
    """
    Prometheus metrics for ML pipeline.
    
    Metrics:
    - Latency histograms (per stage)
    - Detection counters (by threat type)
    - Confidence/LLR distributions
    - Engine health
    """
    
    def __init__(self):
        # Latency metrics
        self.telemetry_latency = Histogram(
            "ml_telemetry_load_ms",
            "Telemetry loading latency",
            buckets=[1, 5, 10, 25, 50, 100]
        )
        self.feature_latency = Histogram(
            "ml_feature_extraction_ms",
            "Feature extraction latency",
            buckets=[1, 5, 10, 25, 50, 100]
        )
        self.engine_latency = Histogram(
            "ml_engine_inference_ms",
            "Engine inference latency",
            buckets=[1, 5, 10, 25, 50, 100]
        )
        self.ensemble_latency = Histogram(
            "ml_ensemble_vote_ms",
            "Ensemble voting latency",
            buckets=[1, 2, 5, 10, 25]
        )
        self.total_latency = Histogram(
            "ml_pipeline_total_ms",
            "Total pipeline latency",
            buckets=[10, 25, 50, 100, 200, 500]
        )
        
        # Detection metrics
        self.detections = Counter(
            "ml_detections_total",
            "Total detections",
            ["threat_type", "decision"]
        )
        self.abstentions = Counter(
            "ml_abstentions_total",
            "Abstentions by reason",
            ["reason"]
        )
        
        # Quality metrics
        self.confidence_dist = Histogram(
            "ml_confidence_distribution",
            "Detection confidence distribution",
            buckets=[0.5, 0.6, 0.7, 0.8, 0.85, 0.9, 0.95, 0.99]
        )
        self.llr_dist = Histogram(
            "ml_llr_distribution",
            "Log-likelihood ratio distribution",
            buckets=[-5, -2, -1, 0, 1, 2, 5, 10]
        )
        
        # Engine health
        self.engine_ready = Gauge(
            "ml_engine_ready",
            "Engine ready status",
            ["engine_type"]
        )
    
    def record_pipeline_result(self, decision: EnsembleDecision, 
                               latencies: Dict, total_latency: float):
        """Record full pipeline metrics."""
        # Latencies
        for stage, latency in latencies.items():
            if stage == "telemetry_load":
                self.telemetry_latency.observe(latency)
            elif stage == "feature_extraction":
                self.feature_latency.observe(latency)
            elif stage == "engine_inference":
                self.engine_latency.observe(latency)
            elif stage == "ensemble_vote":
                self.ensemble_latency.observe(latency)
        
        self.total_latency.observe(total_latency)
        
        # Decision
        if decision:
            if decision.should_publish:
                self.detections.labels(
                    threat_type=decision.threat_type.value,
                    decision="publish"
                ).inc()
                self.confidence_dist.observe(decision.confidence)
                self.llr_dist.observe(decision.llr)
            else:
                self.abstentions.labels(reason=decision.abstention_reason).inc()
```

**Dependencies:** prometheus_client  
**Security:** No sensitive data in metrics

---

## 4. INTEGRATION POINTS

### **4.1 ServiceManager Changes**

```python
# In src/service/manager.py
def initialize(self, settings):
    # Existing initialization...
    
    # NEW: Initialize ML pipeline
    self._initialize_ml_pipeline(settings)

def _initialize_ml_pipeline(self, settings):
    """Initialize ML detection pipeline."""
    # Telemetry source
    telemetry_config = {
        "flows_path": settings.get("TELEMETRY_FLOWS_PATH", "data/telemetry/flows"),
        "files_path": settings.get("TELEMETRY_FILES_PATH", "data/telemetry/files")
    }
    telemetry_source = FileTelemetrySource(telemetry_config)
    
    # Feature extractors
    feature_extractors = {
        "network": NetworkFlowExtractor(),
        "malware": MalwareStaticExtractor()
    }
    
    # Model registry
    registry = ModelRegistry(settings, self.signer)
    
    # Engines
    engines = [
        MLEngine(registry, settings),
        RulesEngine(settings),
        MathEngine(settings)
    ]
    for engine in engines:
        if hasattr(engine, 'initialize'):
            engine.initialize()
    
    # Ensemble
    ensemble = EnsembleVoter(settings)
    
    # Evidence generator
    evidence_gen = EvidenceGenerator(self.signer, settings)
    
    # Metrics
    metrics = PrometheusMetrics()
    
    # Pipeline
    self.detection_pipeline = DetectionPipeline(
        telemetry_source=telemetry_source,
        feature_extractors=feature_extractors,
        engines=engines,
        ensemble=ensemble,
        evidence_generator=evidence_gen,
        metrics=metrics,
        circuit_breaker=self.circuit_breaker
    )
    
    self.logger.info("ML detection pipeline initialized")
```

### **4.2 Handlers Integration**

```python
# In src/service/handlers.py
def handle_commit_event(self, event: CommitEvent):
    """Process commit event with ML pipeline."""
    # Log event (existing)
    self.logger.info(f"Received commit: height={event.height}")
    
    # NEW: Run detection pipeline
    try:
        result = self.service_manager.detection_pipeline.process(
            trigger_event={"commit_height": event.height}
        )
        
        # Publish if detection made
        if result.decision and result.decision.should_publish:
            self._publish_detection(result.decision, result)
        
        # Log latency
        if result.total_latency_ms > 50:
            self.logger.warning(
                f"Pipeline latency exceeded target: {result.total_latency_ms:.2f}ms"
            )
    
    except Exception as e:
        self.logger.error(f"Detection pipeline failed: {e}", exc_info=True)

def _publish_detection(self, decision: EnsembleDecision, result: InstrumentedResult):
    """Publish anomaly to backend."""
    evidence = decision.metadata.get("evidence", b"")
    
    self.publisher.publish_anomaly(
        anomaly_id=str(uuid.uuid4()),
        anomaly_type=decision.threat_type.value,
        source="ml_pipeline",
        severity=self._score_to_severity(decision.final_score),
        confidence=decision.confidence,
        payload=evidence,
        model_version="1.0.0"
    )

def handle_reputation_event(self, event: ReputationEvent):
    """Update ensemble weights based on feedback."""
    # Parse feedback
    feedback = {
        EngineType.ML: event.correct_ml,
        EngineType.RULES: event.correct_rules,
        EngineType.MATH: event.correct_math
    }
    
    # Update ensemble
    self.service_manager.detection_pipeline.ensemble.update_weights(feedback)

def handle_policy_update_event(self, event: PolicyUpdateEvent):
    """Reload rules engine thresholds."""
    new_thresholds = json.loads(event.policy_data)
    
    for engine in self.service_manager.detection_pipeline.engines:
        if engine.engine_type == EngineType.RULES:
            engine.update_thresholds(new_thresholds)
```

---

## 5. TRAINING PIPELINE (OFFLINE)

### **training/train_ddos.py** (~400 lines)

```python
# Offline training script (not part of runtime service)

def train_ddos_model():
    """
    Train LightGBM on CIC-DDoS2019.
    
    Steps:
    1. Load dataset from data/telemetry/flows/
    2. Extract features (NetworkFlowExtractor)
    3. Time-split: 70% train, 15% val, 15% test
    4. Handle class imbalance (SMOTE or class weights)
    5. Train LightGBM
    6. Calibrate with Platt scaling
    7. Evaluate (accuracy, FPR, recall)
    8. Sign model with Ed25519
    9. Export to data/models/
    """
    # Load data
    X_train, y_train, X_val, y_val, X_test, y_test = load_cic_ddos_data()
    
    # Train
    model = lgb.LGBMClassifier(
        n_estimators=100,
        max_depth=7,
        learning_rate=0.05,
        class_weight='balanced'
    )
    model.fit(X_train, y_train)
    
    # Calibrate
    calibrated = CalibratedClassifierCV(model, method='sigmoid', cv='prefit')
    calibrated.fit(X_val, y_val)
    
    # Evaluate
    y_pred = calibrated.predict(X_test)
    y_proba = calibrated.predict_proba(X_test)[:, 1]
    
    fpr = compute_fpr(y_test, y_proba, threshold=0.7)
    recall = recall_score(y_test, y_pred)
    
    print(f"FPR: {fpr:.4f}, Recall: {recall:.4f}")
    
    # Sign and save
    save_signed_model(calibrated, "ddos_lgbm_v1.0.0.pkl")
```

**Similar for:**
- training/train_malware.py (EMBER dataset)
- training/train_anomaly.py (IsolationForest on benign data)

---

## 6. TESTING & VALIDATION

### **tests/test_pipeline.py** - Latency validation
### **tests/test_accuracy.py** - FPR < 0.001 validation
### **tests/test_e2e.py** - End-to-end with Kafka

---

## 7. CONFIGURATION (.env additions)

```bash
# ML Pipeline Configuration
TELEMETRY_FLOWS_PATH=data/telemetry/flows
TELEMETRY_FILES_PATH=data/telemetry/files
MODELS_PATH=data/models

# Ensemble Weights
ML_WEIGHT=0.5
RULES_WEIGHT=0.3
MATH_WEIGHT=0.2

# Detection Thresholds
DDOS_THRESHOLD=0.7
MALWARE_THRESHOLD=0.85
ANOMALY_THRESHOLD=0.75
MIN_CONFIDENCE=0.85

# Rules Engine Thresholds
DDOS_PPS_THRESHOLD=100000
PORT_SCAN_THRESHOLD=500
MALWARE_ENTROPY_THRESHOLD=7.5
```

---

## 8. ACCEPTANCE CRITERIA

**Must pass before Phase 6 complete:**

1. **Latency:** Total pipeline < 50ms (P95)
2. **FPR:** False positive rate < 0.001 on benign test set
3. **Confidence:** Published detections have confidence > 0.85
4. **Signatures:** All evidence packages have valid Ed25519 signatures
5. **Models:** All 3 models load and inference successfully
6. **Engines:** All 3 engines (ML/Rules/Math) operational
7. **Formulas:** All 12 mathematical formulas implemented correctly
8. **End-to-end:** Flow data → pipeline → signed anomaly published to Kafka

---

## 9. IMPLEMENTATION PHASES

### **Phase 6a: Foundation** (Tasks 1-9)
- Setup (requirements, directories)
- Telemetry loading
- Feature extraction
- Model serving

### **Phase 6b: Detection** (Tasks 10-13)
- 3 Engines (ML, Rules, Math)
- All 12 formulas

### **Phase 6c: Decision** (Tasks 14-20)
- Ensemble voting
- Evidence generation
- Pipeline orchestration

### **Phase 6d: Integration** (Tasks 21-27)
- Wire into ServiceManager
- Handler updates
- Feedback loops

### **Phase 6e: Training** (Tasks 28-31)
- Offline model training
- Dataset preparation
- Model signing

### **Phase 6f: Validation** (Tasks 32-35)
- Acceptance tests
- Performance validation
- End-to-end testing

---

**END OF SPEC**
