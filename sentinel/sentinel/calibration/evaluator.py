"""
Calibration evaluation tools.

Provides:
- Reliability diagrams (calibration curves)
- Brier score computation
- Expected Calibration Error (ECE)
- Maximum Calibration Error (MCE)
"""

import json
from dataclasses import dataclass, field, asdict
from pathlib import Path
from typing import List, Dict, Any, Optional, Tuple
import numpy as np

from ..logging import get_logger

logger = get_logger(__name__)


@dataclass
class CalibrationMetrics:
    """Metrics for evaluating calibration quality."""
    brier_score: float = 1.0  # Mean squared error (lower is better)
    expected_calibration_error: float = 1.0  # ECE (lower is better)
    maximum_calibration_error: float = 1.0  # MCE (lower is better)
    accuracy: float = 0.0
    
    # Per-bin data for reliability diagram
    bin_confidences: List[float] = field(default_factory=list)
    bin_accuracies: List[float] = field(default_factory=list)
    bin_counts: List[int] = field(default_factory=list)
    
    # Threshold-specific metrics
    threshold_precision: Dict[str, float] = field(default_factory=dict)
    threshold_recall: Dict[str, float] = field(default_factory=dict)
    threshold_f1: Dict[str, float] = field(default_factory=dict)
    
    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)
    
    def is_well_calibrated(self, ece_threshold: float = 0.1) -> bool:
        """Check if model is well-calibrated."""
        return self.expected_calibration_error < ece_threshold


class CalibrationEvaluator:
    """
    Evaluator for assessing calibration quality.
    
    Provides tools for:
    - Computing calibration metrics (Brier, ECE, MCE)
    - Generating reliability diagram data
    - Threshold analysis
    """
    
    def __init__(self, n_bins: int = 10):
        """
        Initialize evaluator.
        
        Args:
            n_bins: Number of bins for reliability diagram (default: 10)
        """
        self.n_bins = n_bins
        
    def evaluate(
        self,
        probabilities: np.ndarray,
        labels: np.ndarray,
        thresholds: Optional[List[float]] = None
    ) -> CalibrationMetrics:
        """
        Evaluate calibration quality.
        
        Args:
            probabilities: Predicted probabilities (n_samples,)
            labels: True binary labels, 1=malicious, 0=clean (n_samples,)
            thresholds: Optional thresholds to compute precision/recall at
            
        Returns:
            CalibrationMetrics with all computed metrics
        """
        probabilities = np.asarray(probabilities).flatten()
        labels = np.asarray(labels).flatten()
        
        if len(probabilities) != len(labels):
            raise ValueError("Probabilities and labels must have same length")
        
        if len(probabilities) == 0:
            return CalibrationMetrics()
        
        metrics = CalibrationMetrics()
        
        # Brier score
        metrics.brier_score = float(np.mean((probabilities - labels) ** 2))
        
        # Accuracy at 0.5 threshold
        predictions = (probabilities >= 0.5).astype(int)
        metrics.accuracy = float(np.mean(predictions == labels))
        
        # Reliability diagram data and ECE/MCE
        bin_data = self._compute_bin_data(probabilities, labels)
        metrics.bin_confidences = bin_data["confidences"]
        metrics.bin_accuracies = bin_data["accuracies"]
        metrics.bin_counts = bin_data["counts"]
        metrics.expected_calibration_error = bin_data["ece"]
        metrics.maximum_calibration_error = bin_data["mce"]
        
        # Threshold-specific metrics
        thresholds = thresholds or [0.3, 0.5, 0.7, 0.85, 0.9, 0.95]
        for thresh in thresholds:
            precision, recall, f1 = self._compute_threshold_metrics(
                probabilities, labels, thresh
            )
            thresh_key = f"{thresh:.2f}"
            metrics.threshold_precision[thresh_key] = precision
            metrics.threshold_recall[thresh_key] = recall
            metrics.threshold_f1[thresh_key] = f1
        
        return metrics
    
    def _compute_bin_data(
        self,
        probabilities: np.ndarray,
        labels: np.ndarray
    ) -> Dict[str, Any]:
        """Compute reliability diagram bin data."""
        bin_boundaries = np.linspace(0, 1, self.n_bins + 1)
        
        confidences = []
        accuracies = []
        counts = []
        
        ece = 0.0
        mce = 0.0
        total_samples = len(probabilities)
        
        for i in range(self.n_bins):
            bin_lower = bin_boundaries[i]
            bin_upper = bin_boundaries[i + 1]
            
            # Include right boundary for last bin
            if i == self.n_bins - 1:
                bin_mask = (probabilities >= bin_lower) & (probabilities <= bin_upper)
            else:
                bin_mask = (probabilities >= bin_lower) & (probabilities < bin_upper)
            
            bin_size = int(np.sum(bin_mask))
            counts.append(bin_size)
            
            if bin_size > 0:
                bin_accuracy = float(np.mean(labels[bin_mask]))
                bin_confidence = float(np.mean(probabilities[bin_mask]))
                
                accuracies.append(bin_accuracy)
                confidences.append(bin_confidence)
                
                # Calibration error for this bin
                bin_error = abs(bin_accuracy - bin_confidence)
                ece += (bin_size / total_samples) * bin_error
                mce = max(mce, bin_error)
            else:
                # Empty bin - use bin center
                bin_center = (bin_lower + bin_upper) / 2
                confidences.append(bin_center)
                accuracies.append(0.0)
        
        return {
            "confidences": confidences,
            "accuracies": accuracies,
            "counts": counts,
            "ece": float(ece),
            "mce": float(mce),
        }
    
    def _compute_threshold_metrics(
        self,
        probabilities: np.ndarray,
        labels: np.ndarray,
        threshold: float
    ) -> Tuple[float, float, float]:
        """Compute precision, recall, F1 at a given threshold."""
        predictions = (probabilities >= threshold).astype(int)
        
        tp = int(np.sum((predictions == 1) & (labels == 1)))
        fp = int(np.sum((predictions == 1) & (labels == 0)))
        fn = int(np.sum((predictions == 0) & (labels == 1)))
        
        precision = tp / (tp + fp) if (tp + fp) > 0 else 0.0
        recall = tp / (tp + fn) if (tp + fn) > 0 else 0.0
        f1 = 2 * precision * recall / (precision + recall) if (precision + recall) > 0 else 0.0
        
        return precision, recall, f1
    
    def generate_reliability_diagram_data(
        self,
        probabilities: np.ndarray,
        labels: np.ndarray,
        provider_name: str = "unknown"
    ) -> Dict[str, Any]:
        """
        Generate data for plotting a reliability diagram.
        
        Returns JSON-serializable dict with all data needed for plotting.
        """
        metrics = self.evaluate(probabilities, labels)
        
        return {
            "provider_name": provider_name,
            "n_samples": len(probabilities),
            "n_bins": self.n_bins,
            "bin_confidences": metrics.bin_confidences,
            "bin_accuracies": metrics.bin_accuracies,
            "bin_counts": metrics.bin_counts,
            "brier_score": metrics.brier_score,
            "ece": metrics.expected_calibration_error,
            "mce": metrics.maximum_calibration_error,
            "accuracy": metrics.accuracy,
            "is_well_calibrated": metrics.is_well_calibrated(),
        }
    
    def find_optimal_threshold(
        self,
        probabilities: np.ndarray,
        labels: np.ndarray,
        metric: str = "f1",
        min_precision: float = 0.0
    ) -> Tuple[float, Dict[str, float]]:
        """
        Find optimal decision threshold.
        
        Args:
            probabilities: Predicted probabilities
            labels: True labels
            metric: Metric to optimize ("f1", "precision", "recall")
            min_precision: Minimum precision constraint
            
        Returns:
            Tuple of (optimal_threshold, metrics_at_threshold)
        """
        probabilities = np.asarray(probabilities).flatten()
        labels = np.asarray(labels).flatten()
        
        best_threshold = 0.5
        best_score = 0.0
        best_metrics = {}
        
        # Search over thresholds
        for thresh in np.arange(0.01, 0.99, 0.01):
            precision, recall, f1 = self._compute_threshold_metrics(
                probabilities, labels, thresh
            )
            
            # Skip if precision constraint not met
            if precision < min_precision:
                continue
            
            # Select score based on metric
            if metric == "f1":
                score = f1
            elif metric == "precision":
                score = precision
            elif metric == "recall":
                score = recall
            else:
                score = f1
            
            if score > best_score:
                best_score = score
                best_threshold = thresh
                best_metrics = {
                    "precision": precision,
                    "recall": recall,
                    "f1": f1,
                    "threshold": thresh,
                }
        
        return best_threshold, best_metrics
    
    def save_evaluation(
        self,
        metrics: CalibrationMetrics,
        path: Path,
        provider_name: str = "unknown"
    ) -> None:
        """Save evaluation results to JSON."""
        path = Path(path)
        path.parent.mkdir(parents=True, exist_ok=True)
        
        data = {
            "provider_name": provider_name,
            "metrics": metrics.to_dict(),
            "is_well_calibrated": metrics.is_well_calibrated(),
        }
        
        with open(path, "w") as f:
            json.dump(data, f, indent=2)
        
        logger.info(f"Saved calibration evaluation to {path}")


def print_calibration_report(metrics: CalibrationMetrics, provider_name: str = "Provider"):
    """Print a formatted calibration report."""
    print(f"\n{'='*60}")
    print(f"CALIBRATION REPORT: {provider_name}")
    print(f"{'='*60}")
    print(f"\nOverall Metrics:")
    print(f"  Brier Score:  {metrics.brier_score:.4f} (lower is better)")
    print(f"  ECE:          {metrics.expected_calibration_error:.4f} (lower is better)")
    print(f"  MCE:          {metrics.maximum_calibration_error:.4f}")
    print(f"  Accuracy:     {metrics.accuracy:.2%}")
    print(f"  Well-calibrated: {'YES' if metrics.is_well_calibrated() else 'NO'}")
    
    print(f"\nReliability Diagram Data:")
    print(f"  {'Bin':<6} {'Confidence':<12} {'Accuracy':<12} {'Count':<8} {'Error':<8}")
    print(f"  {'-'*46}")
    for i, (conf, acc, cnt) in enumerate(zip(
        metrics.bin_confidences, metrics.bin_accuracies, metrics.bin_counts
    )):
        error = abs(conf - acc) if cnt > 0 else 0
        print(f"  {i+1:<6} {conf:<12.3f} {acc:<12.3f} {cnt:<8} {error:<8.3f}")
    
    if metrics.threshold_precision:
        print(f"\nThreshold Analysis:")
        print(f"  {'Threshold':<12} {'Precision':<12} {'Recall':<12} {'F1':<12}")
        print(f"  {'-'*48}")
        for thresh in sorted(metrics.threshold_precision.keys()):
            p = metrics.threshold_precision[thresh]
            r = metrics.threshold_recall[thresh]
            f1 = metrics.threshold_f1[thresh]
            print(f"  {thresh:<12} {p:<12.3f} {r:<12.3f} {f1:<12.3f}")
    
    print(f"{'='*60}\n")
