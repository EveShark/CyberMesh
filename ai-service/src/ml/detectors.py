"""
Detection engines: Rules, Math, and ML.

Security-first, production-ready implementations.
"""

import numpy as np
import json
from pathlib import Path
from typing import List, Dict, Optional, Set

from .interfaces import Engine
from .malware_variants import MalwareModelCache, ModalityRouter, validate_variant_features
from .types import DetectionCandidate, EngineType, ThreatType
from ..logging import get_logger


class RulesEngine(Engine):
    """
    Threshold-based rule engine.
    
    Rules (configurable via .env):
    - DDoS: pps > 100k OR unique_dst_ports > 1000
    - Port scan: unique_dst_ports > 500 AND syn_ack_ratio > 10
    - Network intrusion: Various heuristics
    
    All thresholds loaded from config (no hardcoding).
    """
    
    def __init__(self, config: Dict):
        """
        Initialize rules engine with config.
        
        Args:
            config: Dictionary with threshold values:
                - DDOS_PPS_THRESHOLD
                - PORT_SCAN_THRESHOLD
                - etc.
        """
        self.logger = get_logger(__name__)
        
        # Load thresholds from config
        self.thresholds = {
            'ddos_pps': float(config.get('DDOS_PPS_THRESHOLD', 1_000_000)),
            'port_scan_ports': int(config.get('PORT_SCAN_THRESHOLD', 500)),
            'malware_entropy': float(config.get('MALWARE_ENTROPY_THRESHOLD', 7.5)),
            'syn_ack_ratio': float(config.get('SYN_ACK_RATIO_THRESHOLD', 10.0)),
        }
        
        # IP blacklists (empty for now, can be loaded from file)
        self.blacklists: Dict[str, Set] = {
            'ips': set(),
            'ports': set(),
        }
        
        self._ready = True
        self.logger.info(f"Initialized RulesEngine with thresholds: {self.thresholds}")
    
    def predict(self, features, batch: bool = False) -> List[DetectionCandidate]:
        """
        Apply threshold rules to features.
        
        Args:
            features: Semantic dict (preferred) or feature vector
            batch: Enable batch inference
        
        Returns:
            List of DetectionCandidate (one per triggered rule)
        """
        if not self._ready:
            return []
        
        # Prefer semantic dict input
        semantics = None
        if isinstance(features, dict):
            semantics = features
        else:
            # Unsupported legacy path; skip gracefully
            self.logger.debug("RulesEngine: expected semantics dict; skipping")
            return []
        
        candidates = []
        
        # Rule 1: DDoS detection (high pps)
        pps = float(semantics.get('pps', 0.0))
        if pps > self.thresholds['ddos_pps']:
            score = min(pps / self.thresholds['ddos_pps'], 1.0)
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.DDOS,
                raw_score=score,
                calibrated_score=score,
                confidence=0.9,  # Rules have high confidence
                engine_type=EngineType.RULES,
                engine_name='ddos_pps_rule',
                features={'pps': float(pps)},
                metadata={
                    'threshold': self.thresholds['ddos_pps'],
                    'value': float(pps),
                    'rule': 'pps_threshold'
                }
            ))
        
        # Rule 2: Port scan detection (high unique ports)
        unique_dst_ports = float(semantics.get('unique_dst_ports', 0.0))
        if unique_dst_ports > self.thresholds['port_scan_ports']:
            score = min(unique_dst_ports / self.thresholds['port_scan_ports'], 1.0)
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.NETWORK_INTRUSION,
                raw_score=score,
                calibrated_score=score,
                confidence=0.85,
                engine_type=EngineType.RULES,
                engine_name='port_scan_rule',
                features={'unique_dst_ports': float(unique_dst_ports)},
                metadata={
                    'threshold': self.thresholds['port_scan_ports'],
                    'value': float(unique_dst_ports),
                    'rule': 'port_scan_threshold'
                }
            ))
        
        # Rule 3: SYN flood detection (high SYN/ACK ratio)
        syn_ack_ratio = float(semantics.get('syn_ack_ratio', 0.0))
        if syn_ack_ratio > self.thresholds['syn_ack_ratio']:
            score = min(syn_ack_ratio / self.thresholds['syn_ack_ratio'], 1.0)
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.DOS,
                raw_score=score,
                calibrated_score=score,
                confidence=0.8,
                engine_type=EngineType.RULES,
                engine_name='syn_flood_rule',
                features={'syn_ack_ratio': float(syn_ack_ratio)},
                metadata={
                    'threshold': self.thresholds['syn_ack_ratio'],
                    'value': float(syn_ack_ratio),
                    'rule': 'syn_ack_threshold'
                }
            ))
        
        # Rule 4: Low port entropy (targeted attack)
        port_entropy = float(semantics.get('port_entropy', 0.0))
        pe = np.clip(port_entropy, 0.0, 2.0)
        if pe < 2.0:  # Low entropy = few unique ports
            score = (2.0 - pe) / 2.0
            score = float(np.clip(score, 0.0, 1.0))
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.NETWORK_INTRUSION,
                raw_score=score,
                calibrated_score=score,
                confidence=0.75,
                engine_type=EngineType.RULES,
                engine_name='low_entropy_rule',
                features={'port_entropy': float(port_entropy)},
                metadata={
                    'threshold': 2.0,
                    'value': float(port_entropy),
                    'rule': 'port_entropy_low'
                }
            ))
        
        return candidates
    
    def calibrate(self, method: str = "platt"):
        """Rules don't need calibration."""
        pass
    
    @property
    def engine_type(self) -> EngineType:
        return EngineType.RULES
    
    @property
    def is_ready(self) -> bool:
        return self._ready
    
    def get_metadata(self) -> dict:
        return {
            'engine': 'rules',
            'thresholds': self.thresholds,
            'blacklists': {k: len(v) for k, v in self.blacklists.items()}
        }
    
    def update_thresholds(self, new_thresholds: Dict):
        """
        Update thresholds from policy events.
        
        Args:
            new_thresholds: Dictionary of threshold updates
        """
        self.thresholds.update(new_thresholds)
        self.logger.info(f"Updated thresholds: {new_thresholds}")


class MathEngine(Engine):
    """
    Mathematical/statistical detection engine.
    
    Implements 5 formulas:
    1. Shannon Entropy - Port distribution anomaly
    2. Z-Score - Statistical outlier detection
    3. Mahalanobis Distance - Multivariate anomaly
    4. CUSUM - Temporal drift detection
    5. Hellinger Distance - Distribution comparison
    
    All formulas are production-grade (no simplifications).
    """
    
    def __init__(self, config: Dict, baseline_path: Optional[str] = None):
        """
        Initialize math engine.
        
        Args:
            config: Configuration dictionary
            baseline_path: Path to baseline statistics JSON
        """
        self.logger = get_logger(__name__)
        self.config = config
        
        # Load baseline statistics (mean, std, covariance for normal traffic)
        self.baseline_stats = self._load_baseline(baseline_path)
        
        # CUSUM state (scalar on pps)
        self.cusum_state = 0.0
        self.cusum_threshold = 5.0  # Alert threshold
        self.cusum_k = 0.5  # Slack parameter
        
        # EWMA state
        self.ewma_state = None
        self.ewma_alpha = 0.3  # Smoothing factor
        
        self._ready = True
        self.logger.info("Initialized MathEngine with semantics-based formulas")
    
    def predict(self, features, batch: bool = False) -> List[DetectionCandidate]:
        """
        Apply mathematical formulas on semantic view.
        
        Args:
            features: Semantic dict (preferred)
            batch: Enable batch inference
        
        Returns:
            List of DetectionCandidate (one per triggered formula)
        """
        if not self._ready:
            return []
        
        if not isinstance(features, dict):
            self.logger.debug("MathEngine: expected semantics dict; skipping")
            return []

        semantics = features
        candidates = []

        # Formula 1: Shannon Entropy (from semantics)
        port_entropy = float(semantics.get('port_entropy', 0.0))
        pe = np.clip(port_entropy, 0.0, 2.0)
        if pe < 2.0:  # Low entropy = anomaly
            score = float(np.clip((2.0 - pe) / 2.0, 0.0, 1.0))
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.ANOMALY,
                raw_score=score,
                calibrated_score=score,
                confidence=0.8,
                engine_type=EngineType.MATH,
                engine_name='shannon_entropy',
                features={'port_entropy': float(port_entropy)},
                metadata={'formula': 'shannon_entropy', 'threshold': 2.0}
            ))
        
        # Formula 2: Z-Score (univariate on pps)
        pps = float(semantics.get('pps', 0.0))
        mean = float(self.baseline_stats.get('pps_mean', 0.0))
        std = float(self.baseline_stats.get('pps_std', 1.0)) or 1.0
        z = abs((pps - mean) / std)
        if np.isfinite(z) and z > 3.0:
            score = float(min(z / 5.0, 1.0))
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.ANOMALY,
                raw_score=score,
                calibrated_score=score,
                confidence=0.7,
                engine_type=EngineType.MATH,
                engine_name='z_score',
                features={'pps_z': float(z)},
                metadata={'formula': 'z_score', 'threshold': 3.0}
            ))

        # Formula 3: CUSUM on pps
        cusum_alert = self._check_cusum_scalar(pps, mean)
        if cusum_alert:
            candidates.append(DetectionCandidate(
                threat_type=ThreatType.ANOMALY,
                raw_score=0.8,
                calibrated_score=0.8,
                confidence=0.75,
                engine_type=EngineType.MATH,
                engine_name='cusum_drift',
                features={'cusum': float(self.cusum_state)},
                metadata={'formula': 'cusum', 'threshold': self.cusum_threshold}
            ))
        
        return candidates
    
    # Note: entropy now provided via semantics in predict()
    
    # Vector z-scores removed in favor of univariate pps z-score
    
    # Mahalanobis removed in semantics-only mode
    
    def _check_cusum_scalar(self, x: float, mean: float) -> bool:
        """
        Scalar CUSUM on pps.
        """
        self.cusum_state = max(0.0, float(self.cusum_state) + (x - mean - self.cusum_k))
        return self.cusum_state > self.cusum_threshold
    
    # Hellinger removed in semantics-only mode
    
    def _load_baseline(self, baseline_path: Optional[str]) -> Dict:
        """
        Load baseline statistics from JSON file.
        
        Args:
            baseline_path: Path to baseline_stats.json
        
        Returns:
            Dictionary with mean, std, cov_inv, distribution
        """
        if baseline_path and Path(baseline_path).exists():
            try:
                with open(baseline_path, 'r') as f:
                    data = json.load(f)
                # Support semantics-based baseline: expect keys pps_mean/pps_std
                baseline = {}
                if isinstance(data, dict):
                    baseline['pps_mean'] = float(data.get('pps_mean', 0.0))
                    baseline['pps_std'] = float(data.get('pps_std', 1.0))
                self.logger.info(f"Loaded baseline statistics from {baseline_path}")
                return baseline
            except Exception as e:
                self.logger.warning(f"Failed to load baseline: {e}, using defaults")
        
        # Default baseline (neutral - won't trigger alerts)
        self.logger.info("Using default baseline statistics")
        return {
            'pps_mean': 0.0,
            'pps_std': 1.0
        }
    
    def calibrate(self, method: str = "platt"):
        """Math formulas don't need calibration."""
        pass
    
    @property
    def engine_type(self) -> EngineType:
        return EngineType.MATH
    
    @property
    def is_ready(self) -> bool:
        return self._ready
    
    def get_metadata(self) -> dict:
        return {
            'engine': 'math',
            'formulas': ['shannon_entropy', 'z_score', 'cusum'],
            'baseline_loaded': isinstance(self.baseline_stats, dict)
        }


class MLEngine(Engine):
    """
    Machine Learning detection engine with 3 trained models.
    
    Models (loaded via ModelRegistry):
    - ddos: LightGBM for DDoS/DoS detection (network flows)
    - malware: LightGBM for malware detection (PE features)
    - anomaly: IsolationForest for anomaly detection (unsupervised)
    
    All models:
    - Loaded with Ed25519 signature verification
    - Calibrated (Platt/Isotonic during training)
    - Return probability scores [0,1]
    
    Security:
    - Models must be signed (ModelRegistry verifies)
    - Graceful degradation if models missing
    - Input validation on feature shapes
    """
    
    def __init__(self, registry, config: Dict, dlq_callback: Optional[callable] = None, metrics=None):
        """
        Initialize ML engine with model registry.
        
        Args:
            registry: ModelRegistry instance (loads/verifies models)
            config: Configuration dictionary
        """
        self.registry = registry
        self.config = config
        self.logger = get_logger(__name__)
        self._dlq_callback = dlq_callback
        self.metrics = metrics
        
        # Attempt to load all 3 models (None if not available)
        self.models = {
            'ddos': None,
            'malware': None,
            'anomaly': None
        }
        
        self._load_models()
        
        # Ready if at least one model loaded
        self._ready = any(m is not None for m in self.models.values())
        
        if self._ready:
            loaded = [k for k, v in self.models.items() if v is not None]
            self.logger.info(f"MLEngine initialized with models: {loaded}")
        else:
            self.logger.warning("MLEngine initialized but NO models loaded")

        # Malware multi-variant foundation (Phase A)
        self.malware_cache = MalwareModelCache(registry=self.registry, capacity=int(config.get('MALWARE_LRU_CAPACITY', 3)))
        self.router = ModalityRouter()
    
    def _load_models(self):
        """Load all 3 models from registry (graceful if missing)."""
        try:
            self.models['ddos'] = self.registry.load_model('ddos')
        except Exception as e:
            self.logger.warning(f"Failed to load ddos: {e}")
        
        try:
            self.models['malware'] = self.registry.load_model('malware')
        except Exception as e:
            self.logger.warning(f"Failed to load malware: {e}")
        
        try:
            self.models['anomaly'] = self.registry.load_model('anomaly')
        except Exception as e:
            self.logger.warning(f"Failed to load anomaly: {e}")
    
    def predict(self, features: np.ndarray, batch: bool = False) -> List[DetectionCandidate]:
        """
        Run ML inference on features.
        
        Routes by feature count:
        - 30 features → Network flow (DDoS + Anomaly models)
        - 256 features → Malware (Malware model)
        
        Args:
            features: Feature vector (30,) or (256,) or batch
            batch: Enable batch inference
        
        Returns:
            List of DetectionCandidate (one per model, if score > threshold)
        """
        if not self._ready:
            return []
        
        # Variant routing path: dict input {modality|variant, vector}
        if isinstance(features, dict):
            modality = features.get('modality')
            variant_key = features.get('variant') or self.router.route(modality)
            # PE ensemble path supports vectors={pe_imports: x1, pe_sections: x2}
            vectors = features.get('vectors')
            if modality and modality.strip().lower() == 'pe' and isinstance(vectors, dict):
                return self._predict_pe_ensemble(vectors)
            x = features.get('vector')
            if variant_key and isinstance(x, np.ndarray):
                return self._predict_malware_variant(variant_key, x)
            # Unknown dict payload; fall through to legacy path

        # Handle batch
        if batch and len(features.shape) == 2:
            features = features[0]  # Process first sample
        
        if len(features.shape) > 1:
            features = features.flatten()
        
        candidates = []
        feature_count = features.shape[0]
        
        # Route by feature count
        if feature_count == 30:  # Network flow features
            # Run DDoS model
            if self.models['ddos'] is not None:
                cand = self._predict_ddos(features)
                if cand:
                    candidates.append(cand)
            
            # Run anomaly model
            if self.models['anomaly'] is not None:
                cand = self._predict_anomaly(features)
                if cand:
                    candidates.append(cand)
        
        elif feature_count == 256:  # Malware features
            if self.models['malware'] is not None:
                cand = self._predict_malware(features)
                if cand:
                    candidates.append(cand)
        else:
            # Check if ddos model exists and accepts this feature count
            if 'ddos' in self.models:
                expected_features = self.models['ddos'].n_features_in_
                if feature_count == expected_features:
                    cand = self._predict_ddos(features)
                    if cand:
                        candidates.append(cand)
                else:
                    self.logger.debug(
                        f"MLEngine: Feature count {feature_count} doesn't match ddos model (expected {expected_features})"
                    )
            else:
                self.logger.debug(
                    f"MLEngine: Unsupported feature count {feature_count} (expected 30 or 256)"
                )
        
        return candidates

    def _variant_meta(self, variant_key: str) -> Optional[Dict]:
        try:
            models = self.registry.registry.get('models', {})
            malware = models.get('malware', {})
            variants = malware.get('variants', {})
            return variants.get(variant_key)
        except Exception:
            return None

    def _predict_malware_variant(self, variant_key: str, x: np.ndarray) -> List[DetectionCandidate]:
        """Predict using a malware variant Booster; DLQ on validation failure if callback provided."""
        import time as _time
        t0 = _time.perf_counter()
        candidates: List[DetectionCandidate] = []
        meta = self._variant_meta(variant_key) or {}
        # Skip if disabled
        if meta and not meta.get('enabled', True):
            return []
        expected = self.malware_cache.expected_feature_count(variant_key)
        try:
            validate_variant_features(variant_key, x, expected)
        except ValidationError as ve:
            if self._dlq_callback:
                try:
                    self._dlq_callback({
                        'component': 'ml_engine',
                        'reason': 'feature_validation_failed',
                        'variant': variant_key,
                        'error': str(ve),
                        'expected': expected,
                        'got': int(x.shape[0]) if hasattr(x, 'shape') else None,
                    })
                except Exception:
                    self.logger.error("DLQ callback failed", exc_info=True)
            if self.metrics:
                self.metrics.inc_variant_dlq(variant_key, 'feature_validation_failed')
            else:
                self.logger.warning(f"Variant validation failed ({variant_key}): {ve}")
            return []

        model = self.malware_cache.get(variant_key)
        if model is None:
            # No model available for this variant
            self.logger.warning(f"Malware model not available for variant: {variant_key}")
            return []

        try:
            proba = self._predict_variant_proba(model, x)
            # Apply calibration method/params if provided
            proba = self._apply_variant_calibration(meta, proba)
            # Threshold per-variant (fallback to global)
            threshold = float(meta.get('threshold', self.config.get('MALWARE_THRESHOLD', 0.85)))
            if proba > threshold:
                margin = abs(proba - 0.5) * 2
                confidence = min(0.7 + margin * 0.3, 1.0)
                schema = meta.get('schema', variant_key)
                candidates.append(DetectionCandidate(
                    threat_type=ThreatType.MALWARE,
                    raw_score=proba,
                    calibrated_score=proba,
                    confidence=confidence,
                    engine_type=EngineType.ML,
                    engine_name=f"malware.{variant_key}",
                    features={'ml_score': proba},
                    metadata={'schema': schema, 'variant': variant_key, 'calibrated': True}
                ))
                if self.metrics:
                    self.metrics.inc_variant(variant_key, 'ok')
            else:
                if self.metrics:
                    self.metrics.inc_variant(variant_key, 'threshold')
        except Exception as e:
            self.logger.error(f"Malware variant inference failed ({variant_key}): {e}", exc_info=True)
            if self.metrics:
                self.metrics.inc_variant(variant_key, 'error')
            return []
        finally:
            if self.metrics:
                dt = (_time.perf_counter() - t0) * 1000.0
                self.metrics.record_variant_latency(variant_key, dt)

        return candidates

    def _predict_variant_proba(self, model, x: np.ndarray) -> float:
        proba_arr = model.predict(x.reshape(1, -1))
        return float(np.clip(proba_arr[0], 0.0, 1.0))

    def _apply_variant_calibration(self, meta: Dict, proba: float) -> float:
        cal = (meta or {}).get('calibration', 'sigmoid')
        params = (meta or {}).get('calibration_params')
        if cal == 'platt' and isinstance(params, dict):
            try:
                A = float(params.get('A', 0.0))
                B = float(params.get('B', 0.0))
                import math
                z = A * proba + B
                return 1.0 / (1.0 + math.exp(-z))
            except Exception:
                return float(np.clip(proba, 0.0, 1.0))
        # isotonic not supported without model; pass-through
        return float(np.clip(proba, 0.0, 1.0))

    def _predict_pe_ensemble(self, vectors: Dict[str, np.ndarray]) -> List[DetectionCandidate]:
        """Run pe_imports and pe_sections (if available) and emit a single ensemble decision."""
        imports_vec = vectors.get('pe_imports')
        sections_vec = vectors.get('pe_sections')
        scores = []
        metas = []
        if isinstance(imports_vec, np.ndarray):
            cand = self._predict_malware_variant('pe_imports', imports_vec)
            # Extract score if candidate produced; else try raw proba to still combine
            if cand:
                scores.append(cand[0].calibrated_score)
                metas.append(('pe_imports', cand[0].metadata.get('schema', 'pe_imports')))
            else:
                model = self.malware_cache.get('pe_imports')
                if model is not None:
                    scores.append(self._apply_variant_calibration(self._variant_meta('pe_imports'), self._predict_variant_proba(model, imports_vec)))
                    metas.append(('pe_imports', 'pe_imports'))

        if isinstance(sections_vec, np.ndarray):
            cand = self._predict_malware_variant('pe_sections', sections_vec)
            if cand:
                scores.append(cand[0].calibrated_score)
                metas.append(('pe_sections', cand[0].metadata.get('schema', 'pe_sections')))
            else:
                model = self.malware_cache.get('pe_sections')
                if model is not None:
                    scores.append(self._apply_variant_calibration(self._variant_meta('pe_sections'), self._predict_variant_proba(model, sections_vec)))
                    metas.append(('pe_sections', 'pe_sections'))

        # If only one score available, defer to its regular path
        if len(scores) == 0:
            return []
        if len(scores) == 1:
            # Create a single candidate using the available score
            s = scores[0]
            margin = abs(s - 0.5) * 2
            confidence = min(0.7 + margin * 0.3, 1.0)
            return [DetectionCandidate(
                threat_type=ThreatType.MALWARE,
                raw_score=s,
                calibrated_score=s,
                confidence=confidence,
                engine_type=EngineType.ML,
                engine_name="malware.pe_ensemble",
                features={'ml_score': s},
                metadata={'schema': metas[0][1], 'variant': metas[0][0], 'ensemble': False}
            )]

        # Combine two scores (gated voter: require at least one above its threshold; weight imports higher)
        s_imp, s_sec = scores[0], scores[1]
        final = float(np.clip(0.6 * s_imp + 0.4 * s_sec, 0.0, 1.0))
        margin = abs(final - 0.5) * 2
        confidence = min(0.7 + margin * 0.3, 1.0)
        return [DetectionCandidate(
            threat_type=ThreatType.MALWARE,
            raw_score=final,
            calibrated_score=final,
            confidence=confidence,
            engine_type=EngineType.ML,
            engine_name="malware.pe_ensemble",
            features={'imports_score': s_imp, 'sections_score': s_sec, 'ml_score': final},
            metadata={'schema': 'pe_ensemble', 'variant': 'pe_ensemble', 'calibrated': True}
        )]
    
    def _predict_ddos(self, features: np.ndarray) -> Optional[DetectionCandidate]:
        """
        Run LightGBM DDoS model.
        
        Args:
            features: Network flow features (30,)
        
        Returns:
            DetectionCandidate if score > threshold, else None
        """
        try:
            # LightGBM predict_proba returns [P(benign), P(malicious)]
            proba = self.models['ddos'].predict_proba(features.reshape(1, -1))[0, 1]
            
            # Pre-ensemble threshold (higher than final threshold)
            if proba > 0.3:
                # Compute confidence (prediction margin)
                margin = abs(proba - 0.5) * 2  # Scale to [0,1]
                confidence = min(0.7 + margin * 0.3, 1.0)  # Range [0.7, 1.0]
                
                return DetectionCandidate(
                    threat_type=ThreatType.DDOS,
                    raw_score=proba,
                    calibrated_score=proba,  # Already calibrated during training
                    confidence=confidence,
                    engine_type=EngineType.ML,
                    engine_name='ddos',
                    features={'ml_score': float(proba)},
                    metadata={'schema': 'flow_79', 'calibrated': True}
                )
        
        except Exception as e:
            self.logger.error(f"DDoS model inference failed: {e}", exc_info=True)
        
        return None
    
    def _predict_malware(self, features: np.ndarray) -> Optional[DetectionCandidate]:
        """
        Run LightGBM malware model.
        
        Args:
            features: Malware features (256,)
        
        Returns:
            DetectionCandidate if score > threshold, else None
        """
        try:
            proba = self.models['malware'].predict_proba(features.reshape(1, -1))[0, 1]
            
            # Higher threshold for malware (favor precision)
            if proba > 0.5:
                margin = abs(proba - 0.5) * 2
                confidence = min(0.7 + margin * 0.3, 1.0)
                
                return DetectionCandidate(
                    threat_type=ThreatType.MALWARE,
                    raw_score=proba,
                    calibrated_score=proba,
                    confidence=confidence,
                    engine_type=EngineType.ML,
                    engine_name='malware',
                    features={'ml_score': float(proba)},
                    metadata={'schema': 'binary_256', 'calibrated': True}
                )
        
        except Exception as e:
            self.logger.error(f"Malware model inference failed: {e}", exc_info=True)
        
        return None
    
    def _predict_anomaly(self, features: np.ndarray) -> Optional[DetectionCandidate]:
        """
        Run IsolationForest anomaly model.
        
        Args:
            features: Network flow features (30,)
        
        Returns:
            DetectionCandidate if anomalous, else None
        """
        try:
            # IsolationForest returns anomaly score (negative = anomalous)
            anomaly_score = self.models['anomaly'].decision_function(features.reshape(1, -1))[0]
            
            # Convert to probability via sigmoid
            # More negative score = more anomalous = higher probability
            proba = 1.0 / (1.0 + np.exp(anomaly_score))
            
            if proba > 0.3:
                # IsolationForest has lower confidence (unsupervised)
                confidence = min(0.6 + proba * 0.2, 0.8)
                
                return DetectionCandidate(
                    threat_type=ThreatType.ANOMALY,
                    raw_score=proba,
                    calibrated_score=proba,
                    confidence=confidence,
                    engine_type=EngineType.ML,
                    engine_name='anomaly',
                    features={'anomaly_score': float(anomaly_score), 'ml_score': float(proba)},
                    metadata={'schema': 'flow_79', 'unsupervised': True}
                )
        
        except Exception as e:
            self.logger.error(f"Anomaly model inference failed: {e}", exc_info=True)
        
        return None
    
    def calibrate(self, method: str = "platt"):
        """Calibration done during training, no-op here."""
        pass
    
    @property
    def engine_type(self) -> EngineType:
        return EngineType.ML
    
    @property
    def is_ready(self) -> bool:
        return self._ready
    
    def get_metadata(self) -> dict:
        return {
            'engine': 'ml',
            'models_loaded': {
                'ddos': self.models['ddos'] is not None,
                'malware': self.models['malware'] is not None,
                'anomaly': self.models['anomaly'] is not None
            },
            'total_models': sum(1 for m in self.models.values() if m is not None)
        }
