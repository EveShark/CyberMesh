"""Anomaly detection provider using IsolationForest model."""

import os
import time
import json
import re
import warnings
from pathlib import Path
from typing import Set, Optional, Dict, Any

import numpy as np
import joblib
from sklearn.exceptions import InconsistentVersionWarning

from ..base import Provider, AnalysisResult, ThreatLevel, Indicator
from ...parsers.base import ParsedFile, FileType
from ...utils.errors import ModelLoadError
from ...utils.signer import verify_model_fingerprint
from ...logging import get_logger

logger = get_logger(__name__)


class AnomalyProvider(Provider):
    """
    Anomaly detection using IsolationForest model.
    
    Detects statistical anomalies in file features that don't match
    known patterns. Useful for detecting zero-day or novel malware.
    
    Model info (from CyberMesh):
    - Algorithm: IsolationForest
    - Features: 30
    - AUC: 1.0 (on training data)
    """
    
    def __init__(
        self,
        models_path: str,
        model_registry_path: Optional[str] = None,
        contamination: float = 0.1,
        verify_fingerprint: bool = False
    ):
        """
        Initialize anomaly provider.
        
        Args:
            models_path: Path to models directory
            model_registry_path: Path to model_registry.json
            contamination: Expected proportion of anomalies (for threshold)
            verify_fingerprint: Whether to verify model fingerprint
        """
        self.models_path = Path(models_path)
        self.contamination = contamination
        self.verify_fingerprint = verify_fingerprint
        self._latency_ema = 20.0
        
        self.model = None
        self.model_metadata: Dict[str, Any] = {}
        self._last_load_compat: Dict[str, Any] = {}
        
        registry_path = model_registry_path or self.models_path / "model_registry.json"
        self._load_model(Path(registry_path))
    
    @property
    def name(self) -> str:
        return "anomaly_detector"
    
    @property
    def version(self) -> str:
        return self.model_metadata.get("version", "1.0.0")
    
    @property
    def supported_types(self) -> Set[FileType]:
        # Anomaly detection works on any file with extractable features
        return {FileType.PE, FileType.SCRIPT, FileType.PCAP, FileType.PDF, FileType.OFFICE}
    
    def get_cost_per_call(self) -> float:
        return 0.0
    
    def get_avg_latency_ms(self) -> float:
        return self._latency_ema
    
    def _load_model(self, registry_path: Path) -> None:
        """Load model from registry."""
        try:
            if registry_path.exists():
                with open(registry_path, "r") as f:
                    registry = json.load(f)
                
                models = registry.get("models", {})
                anomaly_config = models.get("anomaly", {})
                
                if anomaly_config.get("path"):
                    model_file = self.models_path / anomaly_config["path"]
                    if model_file.exists():
                        # Verify fingerprint if enabled
                        if self.verify_fingerprint:
                            fingerprint = anomaly_config.get("fingerprint", "")
                            verify_model_fingerprint(str(model_file), fingerprint)
                        
                        self.model = self._safe_joblib_load(model_file)
                        self.model_metadata = dict(anomaly_config)
                        if self._last_load_compat:
                            self.model_metadata.setdefault("compat", {}).update(self._last_load_compat)
                        logger.info(f"Loaded anomaly model: {model_file}")
                        return
            
            # Fallback to direct path
            model_file = self.models_path / "anomaly.pkl"
            if model_file.exists():
                self.model = self._safe_joblib_load(model_file)
                self.model_metadata = {"feature_count": 30, "version": "1.0.0"}
                if self._last_load_compat:
                    self.model_metadata.setdefault("compat", {}).update(self._last_load_compat)
                logger.info(f"Loaded anomaly model: {model_file}")
            else:
                logger.warning("No anomaly model found")
                
        except Exception as e:
            logger.error(f"Failed to load anomaly model: {e}")
            raise ModelLoadError(f"Failed to load anomaly model: {e}")

    def _safe_joblib_load(self, model_file: Path) -> Any:
        """
        Load a joblib model while collapsing sklearn version-mismatch warnings.

        Security-first behavior:
        - Always record version-mismatch metadata if observed.
        - If `SENTINEL_STRICT_MODEL_COMPAT=1`, refuse to load on mismatch.
        """
        strict = os.getenv("SENTINEL_STRICT_MODEL_COMPAT", "").strip().lower() in ("1", "true", "yes", "on")
        self._last_load_compat = {}
        mismatch_train: Optional[str] = None
        mismatch_runtime: Optional[str] = None

        with warnings.catch_warnings(record=True) as w:
            warnings.simplefilter("always", InconsistentVersionWarning)
            # Avoid spamming: only keep the first few warnings; we summarize below.
            model = joblib.load(model_file)

        # Try to infer the serialized sklearn version from warning text.
        for ww in w:
            msg = str(getattr(ww, "message", ww))
            # Prefer category match; fall back to text match so we never miss it.
            is_iv = getattr(ww, "category", None) is InconsistentVersionWarning
            if not is_iv and "Trying to unpickle estimator" not in msg:
                continue
            m = re.search(
                r"from version ([0-9]+\\.[0-9]+\\.[0-9]+) when using version ([0-9]+\\.[0-9]+\\.[0-9]+)",
                msg,
            )
            if m:
                mismatch_train = m.group(1)
                mismatch_runtime = m.group(2)
                break

        if mismatch_train and mismatch_runtime and mismatch_train != mismatch_runtime:
            self._last_load_compat = {
                "sklearn_trained_version": mismatch_train,
                "sklearn_runtime_version": mismatch_runtime,
            "sklearn_inconsistent_warning_count": sum(
                1
                for ww in w
                if getattr(ww, "category", None) is InconsistentVersionWarning
                or isinstance(getattr(ww, "message", None), InconsistentVersionWarning)
            ),
        }
            if strict:
                raise ModelLoadError(
                    f"sklearn version mismatch for {model_file.name}: trained {mismatch_train}, runtime {mismatch_runtime} "
                    f"(set SENTINEL_STRICT_MODEL_COMPAT=0 to allow degraded load)"
                )
            # Warn once; do not emit 100+ warnings.
            logger.warning(
                f"sklearn version mismatch for {model_file.name}: trained {mismatch_train}, runtime {mismatch_runtime} "
                f"(loaded anyway; results may be unreliable)."
            )

        return model
    
    def analyze(self, parsed_file: ParsedFile) -> AnalysisResult:
        t0 = time.perf_counter()
        
        if self.model is None:
            return AnalysisResult(
                provider_name=self.name,
                provider_version=self.version,
                threat_level=ThreatLevel.UNKNOWN,
                score=0.0,
                confidence=0.0,
                findings=["Model not loaded"],
                error="Model not available",
                latency_ms=(time.perf_counter() - t0) * 1000,
            )
        
        try:
            features = self._extract_features(parsed_file)
            
            # IsolationForest returns -1 for anomalies, 1 for normal
            prediction = self.model.predict(features.reshape(1, -1))[0]
            
            # Get anomaly score (lower = more anomalous)
            if hasattr(self.model, 'score_samples'):
                anomaly_score = self.model.score_samples(features.reshape(1, -1))[0]
                # Convert to 0-1 range where 1 = most anomalous
                # Typical scores are negative, more negative = more anomalous
                normalized_score = 1.0 / (1.0 + np.exp(anomaly_score))  # Sigmoid
            else:
                # Fallback: use prediction directly
                normalized_score = 0.8 if prediction == -1 else 0.2
            
            # Determine threat level
            if prediction == -1:  # Anomaly detected
                if normalized_score >= 0.8:
                    threat_level = ThreatLevel.MALICIOUS
                else:
                    threat_level = ThreatLevel.SUSPICIOUS
            else:
                threat_level = ThreatLevel.CLEAN
            
            # Calculate confidence based on how far from decision boundary
            confidence = min(0.5 + abs(normalized_score - 0.5), 0.9)
            
            findings = []
            indicators = []
            
            if prediction == -1:
                findings.append(
                    f"Anomaly detected: file exhibits unusual statistical patterns "
                    f"(score: {normalized_score:.3f})"
                )
                indicators.append(Indicator(
                    type="anomaly",
                    value=f"score_{normalized_score:.2f}",
                    context="isolation_forest",
                ))
                
                # Add feature-specific insights
                anomalous_features = self._identify_anomalous_features(features)
                if anomalous_features:
                    findings.append(f"Anomalous features: {', '.join(anomalous_features[:3])}")
            
            latency = (time.perf_counter() - t0) * 1000
            self._latency_ema = 0.9 * self._latency_ema + 0.1 * latency
            
            return AnalysisResult(
                provider_name=self.name,
                provider_version=self.version,
                threat_level=threat_level,
                score=normalized_score,
                confidence=confidence,
                findings=findings,
                indicators=indicators,
                latency_ms=latency,
                metadata={
                    "raw_prediction": int(prediction),
                    "anomaly_score": float(normalized_score),
                    "feature_count": len(features),
                }
            )
            
        except Exception as e:
            logger.error(f"Anomaly analysis failed: {e}")
            return AnalysisResult(
                provider_name=self.name,
                provider_version=self.version,
                threat_level=ThreatLevel.UNKNOWN,
                score=0.0,
                confidence=0.0,
                error=str(e),
                latency_ms=(time.perf_counter() - t0) * 1000,
            )
    
    def _extract_features(self, parsed_file: ParsedFile) -> np.ndarray:
        """
        Extract 30 features for anomaly detection.
        
        Features include:
        - File size statistics
        - Entropy measures
        - String statistics
        - Import/section counts (for PE)
        - URL/IP counts
        """
        expected_features = self.model_metadata.get("feature_count", 30)
        features = np.zeros(expected_features, dtype=np.float32)
        
        # Feature 0: Normalized file size (log scale)
        features[0] = np.log1p(parsed_file.file_size) / 20.0
        
        # Feature 1: Overall entropy (normalized)
        features[1] = parsed_file.entropy / 8.0
        
        # Feature 2-4: String statistics
        features[2] = min(len(parsed_file.strings) / 1000.0, 1.0)
        if parsed_file.strings:
            avg_len = np.mean([len(s) for s in parsed_file.strings[:100]])
            features[3] = min(avg_len / 50.0, 1.0)
        
        # Feature 5: URL count
        features[5] = min(len(parsed_file.urls) / 10.0, 1.0)
        
        # Feature 6-10: Import statistics (PE files)
        features[6] = min(len(parsed_file.imports) / 500.0, 1.0)
        
        suspicious_imports = parsed_file.metadata.get("suspicious_imports", [])
        features[7] = min(len(suspicious_imports) / 20.0, 1.0)
        
        # Feature 11-15: Section statistics (PE files)
        sections = parsed_file.metadata.get("sections", [])
        features[11] = min(len(sections) / 10.0, 1.0)
        
        if sections:
            entropies = [s.get("entropy", 0) for s in sections if isinstance(s, dict)]
            if entropies:
                features[12] = np.mean(entropies) / 8.0
                features[13] = np.max(entropies) / 8.0
                features[14] = np.std(entropies) / 4.0
        
        # Feature 16-20: Script-specific
        features[16] = 1.0 if parsed_file.metadata.get("obfuscation_indicators") else 0.0
        features[17] = min(len(parsed_file.metadata.get("suspicious_patterns", [])) / 10.0, 1.0)
        
        # Feature 21-25: Office/PDF specific
        features[21] = 1.0 if parsed_file.metadata.get("has_macros") else 0.0
        features[22] = 1.0 if parsed_file.metadata.get("has_javascript") else 0.0
        
        # Feature 26-29: Android specific
        permissions = parsed_file.metadata.get("permissions", [])
        features[26] = min(len(permissions) / 30.0, 1.0)
        
        dangerous_perms = parsed_file.metadata.get("dangerous_permissions", [])
        features[27] = min(len(dangerous_perms) / 10.0, 1.0)
        
        return features
    
    def _identify_anomalous_features(self, features: np.ndarray) -> list:
        """Identify which features contributed most to anomaly."""
        anomalous = []
        
        # Check for outlier values
        if features[1] > 0.9:  # Very high entropy
            anomalous.append("high_entropy")
        if features[7] > 0.5:  # Many suspicious imports
            anomalous.append("suspicious_imports")
        if features[16] > 0:  # Obfuscation detected
            anomalous.append("obfuscation")
        if features[21] > 0:  # Has macros
            anomalous.append("macros")
        if features[22] > 0:  # Has JavaScript
            anomalous.append("javascript")
        if features[27] > 0.5:  # Many dangerous permissions
            anomalous.append("dangerous_permissions")
        
        return anomalous
