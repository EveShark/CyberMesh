"""Benchmarking system for provider evaluation."""

import json
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, List, Optional, Any, Tuple

from ..providers.base import Provider, AnalysisResult, ThreatLevel
from ..parsers.base import ParsedFile, FileType
from ..parsers import get_parser_for_file
from ..logging import get_logger

logger = get_logger(__name__)


@dataclass
class EvalSample:
    """A single evaluation sample with ground truth."""
    file_path: str
    file_type: FileType
    ground_truth: ThreatLevel  # Expected verdict
    label: str = ""  # e.g., "clean", "emotet", "wannacry"
    metadata: Dict[str, Any] = field(default_factory=dict)
    
    def to_dict(self) -> Dict:
        return {
            "file_path": self.file_path,
            "file_type": self.file_type.value,
            "ground_truth": self.ground_truth.value,
            "label": self.label,
            "metadata": self.metadata,
        }
    
    @classmethod
    def from_dict(cls, data: Dict) -> "EvalSample":
        return cls(
            file_path=data["file_path"],
            file_type=FileType(data["file_type"]),
            ground_truth=ThreatLevel(data["ground_truth"]),
            label=data.get("label", ""),
            metadata=data.get("metadata", {}),
        )


@dataclass
class BenchmarkResult:
    """Result of benchmarking a provider."""
    provider_name: str
    provider_version: str
    
    # Metrics
    accuracy: float = 0.0
    precision: float = 0.0
    recall: float = 0.0
    f1_score: float = 0.0
    
    # Counts
    total_samples: int = 0
    true_positives: int = 0
    true_negatives: int = 0
    false_positives: int = 0
    false_negatives: int = 0
    
    # Performance
    avg_latency_ms: float = 0.0
    total_time_ms: float = 0.0
    
    # Per-sample results
    sample_results: List[Dict] = field(default_factory=list)
    
    # Computed baseline weight
    baseline_weight: float = 0.5
    
    timestamp: float = field(default_factory=time.time)
    
    def compute_metrics(self) -> None:
        """Compute metrics from counts."""
        total = self.true_positives + self.true_negatives + self.false_positives + self.false_negatives
        
        if total > 0:
            self.accuracy = (self.true_positives + self.true_negatives) / total
        
        if self.true_positives + self.false_positives > 0:
            self.precision = self.true_positives / (self.true_positives + self.false_positives)
        
        if self.true_positives + self.false_negatives > 0:
            self.recall = self.true_positives / (self.true_positives + self.false_negatives)
        
        if self.precision + self.recall > 0:
            self.f1_score = 2 * (self.precision * self.recall) / (self.precision + self.recall)
        
        # Baseline weight: weighted combination of accuracy and F1
        # F1 is more important for imbalanced malware detection
        self.baseline_weight = 0.3 * self.accuracy + 0.7 * self.f1_score
        self.baseline_weight = max(0.1, min(0.95, self.baseline_weight))
    
    def to_dict(self) -> Dict:
        return {
            "provider_name": self.provider_name,
            "provider_version": self.provider_version,
            "accuracy": self.accuracy,
            "precision": self.precision,
            "recall": self.recall,
            "f1_score": self.f1_score,
            "total_samples": self.total_samples,
            "true_positives": self.true_positives,
            "true_negatives": self.true_negatives,
            "false_positives": self.false_positives,
            "false_negatives": self.false_negatives,
            "avg_latency_ms": self.avg_latency_ms,
            "total_time_ms": self.total_time_ms,
            "baseline_weight": self.baseline_weight,
            "timestamp": self.timestamp,
            "sample_results": self.sample_results,  # Include raw scores for calibration
        }


class EvalSuite:
    """
    Evaluation suite for benchmarking providers.
    
    Contains labeled samples and methods to:
    - Run providers against samples
    - Compute accuracy, precision, recall, F1
    - Generate baseline weights
    """
    
    def __init__(self, samples_dir: Optional[str] = None):
        """
        Initialize evaluation suite.
        
        Args:
            samples_dir: Directory containing eval samples
        """
        self.samples_dir = Path(samples_dir) if samples_dir else Path("data/eval_samples")
        self.samples: List[EvalSample] = []
        self.manifest_path = self.samples_dir / "manifest.json"
        
        self._load_samples()
    
    def _load_samples(self) -> None:
        """Load samples from manifest."""
        if not self.manifest_path.exists():
            logger.info("No eval manifest found, using built-in samples")
            self._create_builtin_samples()
            return
        
        try:
            with open(self.manifest_path, "r") as f:
                data = json.load(f)
            
            for sample_data in data.get("samples", []):
                sample = EvalSample.from_dict(sample_data)
                if Path(sample.file_path).exists():
                    self.samples.append(sample)
                else:
                    logger.warning(f"Eval sample not found: {sample.file_path}")
            
            logger.info(f"Loaded {len(self.samples)} eval samples")
            
        except Exception as e:
            logger.error(f"Failed to load eval manifest: {e}")
            self._create_builtin_samples()
    
    def _create_builtin_samples(self) -> None:
        """Create built-in test samples for evaluation."""
        # These are synthetic samples created in the tests directory
        test_dir = Path("tests/samples")
        
        builtin = [
            # Clean samples
            EvalSample(
                file_path=str(test_dir / "test.pdf"),
                file_type=FileType.PDF,
                ground_truth=ThreatLevel.CLEAN,
                label="clean_pdf",
            ),
            EvalSample(
                file_path=str(test_dir / "test_script.ps1"),
                file_type=FileType.SCRIPT,
                ground_truth=ThreatLevel.CLEAN,
                label="clean_script",
            ),
            # Suspicious/Malicious samples
            EvalSample(
                file_path=str(test_dir / "suspicious_script.ps1"),
                file_type=FileType.SCRIPT,
                ground_truth=ThreatLevel.MALICIOUS,
                label="suspicious_downloader",
            ),
            EvalSample(
                file_path=str(test_dir / "ransomware_test.ps1"),
                file_type=FileType.SCRIPT,
                ground_truth=ThreatLevel.MALICIOUS,
                label="ransomware_indicators",
            ),
        ]
        
        for sample in builtin:
            if Path(sample.file_path).exists():
                self.samples.append(sample)
        
        logger.info(f"Created {len(self.samples)} built-in eval samples")
    
    def add_sample(
        self,
        file_path: str,
        ground_truth: ThreatLevel,
        label: str = "",
        metadata: Optional[Dict] = None
    ) -> None:
        """Add a sample to the evaluation suite."""
        from ..parsers import detect_file_type
        
        file_type = detect_file_type(file_path)
        sample = EvalSample(
            file_path=file_path,
            file_type=file_type,
            ground_truth=ground_truth,
            label=label,
            metadata=metadata or {},
        )
        self.samples.append(sample)
        self._save_manifest()
    
    def _save_manifest(self) -> None:
        """Save samples manifest."""
        self.samples_dir.mkdir(parents=True, exist_ok=True)
        
        data = {
            "samples": [s.to_dict() for s in self.samples],
            "last_updated": time.time(),
        }
        
        with open(self.manifest_path, "w") as f:
            json.dump(data, f, indent=2)
    
    def get_samples_for_type(self, file_type: FileType) -> List[EvalSample]:
        """Get samples that match a file type."""
        return [s for s in self.samples if s.file_type == file_type]
    
    @property
    def sample_count(self) -> int:
        return len(self.samples)
    
    @property
    def malicious_count(self) -> int:
        return sum(1 for s in self.samples if s.ground_truth in (ThreatLevel.MALICIOUS, ThreatLevel.CRITICAL))
    
    @property
    def clean_count(self) -> int:
        return sum(1 for s in self.samples if s.ground_truth == ThreatLevel.CLEAN)


class ProviderBenchmark:
    """
    Benchmarks providers against the evaluation suite.
    
    Used to:
    - Evaluate new providers on registration
    - Re-evaluate providers periodically
    - Compare provider performance
    """
    
    def __init__(self, eval_suite: EvalSuite, results_dir: Optional[str] = None):
        """
        Initialize benchmarker.
        
        Args:
            eval_suite: Evaluation suite with labeled samples
            results_dir: Directory to save benchmark results
        """
        self.eval_suite = eval_suite
        self.results_dir = Path(results_dir) if results_dir else Path("data/benchmark_results")
        self.results_dir.mkdir(parents=True, exist_ok=True)
    
    def benchmark(self, provider: Provider, save_results: bool = True) -> BenchmarkResult:
        """
        Benchmark a provider against the evaluation suite.
        
        Args:
            provider: Provider to benchmark
            save_results: Whether to save results to disk
            
        Returns:
            BenchmarkResult with metrics and baseline weight
        """
        logger.info(f"Benchmarking provider: {provider.name} v{provider.version}")
        
        result = BenchmarkResult(
            provider_name=provider.name,
            provider_version=provider.version,
        )
        
        # Filter samples to those the provider supports
        compatible_samples = []
        for sample in self.eval_suite.samples:
            if sample.file_type in provider.supported_types:
                compatible_samples.append(sample)
        
        if not compatible_samples:
            logger.warning(f"No compatible samples for provider {provider.name}")
            result.baseline_weight = 0.5
            return result
        
        t0 = time.perf_counter()
        total_latency = 0.0
        
        for sample in compatible_samples:
            try:
                # Parse file
                parser = get_parser_for_file(sample.file_path)
                if not parser:
                    continue
                
                parsed_file = parser.parse(sample.file_path)
                
                # Run provider
                analysis_result = provider.analyze(parsed_file)
                total_latency += analysis_result.latency_ms
                
                # Compare to ground truth
                predicted = analysis_result.threat_level
                actual = sample.ground_truth
                
                # Binary classification: malicious vs clean
                predicted_malicious = predicted in (ThreatLevel.MALICIOUS, ThreatLevel.CRITICAL, ThreatLevel.SUSPICIOUS)
                actual_malicious = actual in (ThreatLevel.MALICIOUS, ThreatLevel.CRITICAL)
                
                if predicted_malicious and actual_malicious:
                    result.true_positives += 1
                elif not predicted_malicious and not actual_malicious:
                    result.true_negatives += 1
                elif predicted_malicious and not actual_malicious:
                    result.false_positives += 1
                else:
                    result.false_negatives += 1
                
                result.sample_results.append({
                    "file": sample.file_path,
                    "label": sample.label,
                    "predicted": predicted.value,
                    "actual": actual.value,
                    "score": analysis_result.score,
                    "correct": predicted_malicious == actual_malicious,
                })
                
                result.total_samples += 1
                
            except Exception as e:
                logger.error(f"Benchmark error on {sample.file_path}: {e}")
        
        result.total_time_ms = (time.perf_counter() - t0) * 1000
        if result.total_samples > 0:
            result.avg_latency_ms = total_latency / result.total_samples
        
        # Compute metrics
        result.compute_metrics()
        
        logger.info(
            f"Benchmark complete for {provider.name}: "
            f"accuracy={result.accuracy:.1%}, F1={result.f1_score:.3f}, "
            f"baseline_weight={result.baseline_weight:.3f}"
        )
        
        if save_results:
            self._save_result(result)
        
        return result
    
    def _save_result(self, result: BenchmarkResult) -> None:
        """Save benchmark result to disk."""
        filename = f"{result.provider_name}_{int(result.timestamp)}.json"
        filepath = self.results_dir / filename
        
        with open(filepath, "w") as f:
            json.dump(result.to_dict(), f, indent=2)
        
        logger.debug(f"Saved benchmark result to {filepath}")
    
    def get_latest_result(self, provider_name: str) -> Optional[BenchmarkResult]:
        """Get the most recent benchmark result for a provider."""
        results = list(self.results_dir.glob(f"{provider_name}_*.json"))
        if not results:
            return None
        
        latest = max(results, key=lambda p: p.stat().st_mtime)
        
        with open(latest, "r") as f:
            data = json.load(f)
        
        result = BenchmarkResult(
            provider_name=data["provider_name"],
            provider_version=data["provider_version"],
        )
        result.accuracy = data["accuracy"]
        result.precision = data["precision"]
        result.recall = data["recall"]
        result.f1_score = data["f1_score"]
        result.total_samples = data["total_samples"]
        result.true_positives = data["true_positives"]
        result.true_negatives = data["true_negatives"]
        result.false_positives = data["false_positives"]
        result.false_negatives = data["false_negatives"]
        result.baseline_weight = data["baseline_weight"]
        result.timestamp = data["timestamp"]
        
        return result
    
    def compare_providers(self, providers: List[Provider]) -> List[BenchmarkResult]:
        """Benchmark multiple providers and return ranked results."""
        results = [self.benchmark(p) for p in providers]
        results.sort(key=lambda r: r.f1_score, reverse=True)
        return results
