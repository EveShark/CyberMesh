#!/usr/bin/env python
"""
Provider Calibration Script for Sentinel.

1. Run all samples through each provider
2. Record raw scores and ground truth
3. Compute optimal thresholds (maximize F1)
4. Calculate provider weights
5. Output calibrated_config.json
6. Print confusion matrices
"""

import os
import sys
import json
import time
import logging
import numpy as np
from pathlib import Path
from typing import Dict, List, Tuple, Optional
from dataclasses import dataclass, field
from collections import defaultdict

# Setup path and logging BEFORE importing sentinel modules
sys.path.insert(0, str(Path(__file__).parent.parent))

from dotenv import load_dotenv
load_dotenv(Path(__file__).parent.parent / ".env")

# Configure logging to avoid circular import issues
logging.basicConfig(level=logging.WARNING, format='%(levelname)s: %(message)s')


@dataclass
class SampleResult:
    """Result from running a sample through a provider."""
    sample_id: str
    sha256: str
    ground_truth: str  # "malicious" or "clean"
    provider: str
    raw_score: float
    findings_count: int = 0
    latency_ms: float = 0.0
    error: Optional[str] = None


@dataclass
class ProviderStats:
    """Statistics for a provider."""
    name: str
    malicious_scores: List[float] = field(default_factory=list)
    clean_scores: List[float] = field(default_factory=list)
    malicious_mean: float = 0.0
    malicious_std: float = 0.0
    clean_mean: float = 0.0
    clean_std: float = 0.0
    optimal_threshold: float = 0.5
    precision: float = 0.0
    recall: float = 0.0
    f1: float = 0.0
    accuracy: float = 0.0
    weight: float = 0.0
    confusion_matrix: Dict[str, int] = field(default_factory=dict)


def load_manifest() -> List[Dict]:
    """Load evaluation manifest."""
    manifest_path = Path(__file__).parent.parent / "eval_data" / "manifest.json"
    with open(manifest_path) as f:
        data = json.load(f)
    return data.get("samples", [])


def get_sample_content(sample: Dict) -> Optional[bytes]:
    """Load sample content if available."""
    eval_dir = Path(__file__).parent.parent / "eval_data"
    
    # Try to find actual file
    file_path = sample.get("file_path", "")
    if file_path:
        full_path = eval_dir / file_path
        if full_path.exists() and full_path.suffix not in (".json", ".features"):
            try:
                with open(full_path, "rb") as f:
                    return f.read()
            except:
                pass
    
    # For scripts directory
    scripts_dir = eval_dir / "scripts"
    if scripts_dir.exists():
        for script in scripts_dir.iterdir():
            if script.is_file():
                # Check if sha256 matches
                import hashlib
                with open(script, "rb") as f:
                    content = f.read()
                sha = hashlib.sha256(content).hexdigest()
                if sha == sample.get("sha256"):
                    return content
    
    return None


def run_yara_provider(samples: List[Dict]) -> List[SampleResult]:
    """Run YARA provider on samples."""
    results = []
    
    try:
        from sentinel.fastpath.yara_engine import YaraEngine
        engine = YaraEngine()
        print(f"  YARA engine loaded: {engine.stats.get('rules_loaded', 0)} rules")
    except Exception as e:
        print(f"  YARA engine failed: {e}")
        return results
    
    scripts_dir = Path(__file__).parent.parent / "eval_data" / "scripts"
    
    for sample in samples:
        # Only process script files we can actually scan
        if sample.get("file_type") != "script":
            continue
            
        # Find the actual file
        file_path = None
        for script in scripts_dir.iterdir():
            import hashlib
            with open(script, "rb") as f:
                content = f.read()
            sha = hashlib.sha256(content).hexdigest()
            if sha == sample.get("sha256"):
                file_path = script
                break
        
        if not file_path:
            continue
        
        t0 = time.perf_counter()
        try:
            matches = engine.scan_file(str(file_path))
            score = min(1.0, len(matches) * 0.2)  # 0.2 per match, max 1.0
            
            results.append(SampleResult(
                sample_id=sample.get("id", ""),
                sha256=sample.get("sha256", ""),
                ground_truth=sample.get("label", "unknown"),
                provider="yara",
                raw_score=score,
                findings_count=len(matches),
                latency_ms=(time.perf_counter() - t0) * 1000,
            ))
        except Exception as e:
            results.append(SampleResult(
                sample_id=sample.get("id", ""),
                sha256=sample.get("sha256", ""),
                ground_truth=sample.get("label", "unknown"),
                provider="yara",
                raw_score=0.0,
                error=str(e),
                latency_ms=(time.perf_counter() - t0) * 1000,
            ))
    
    return results


def run_entropy_provider(samples: List[Dict]) -> List[SampleResult]:
    """Run entropy analysis on samples."""
    results = []
    
    try:
        from sentinel.providers.static import EntropyProvider
        provider = EntropyProvider()
    except Exception as e:
        print(f"  Entropy provider failed: {e}")
        return results
    
    scripts_dir = Path(__file__).parent.parent / "eval_data" / "scripts"
    
    for sample in samples:
        if sample.get("file_type") != "script":
            continue
        
        # Find file
        file_path = None
        for script in scripts_dir.iterdir():
            import hashlib
            with open(script, "rb") as f:
                content = f.read()
            sha = hashlib.sha256(content).hexdigest()
            if sha == sample.get("sha256"):
                file_path = script
                break
        
        if not file_path:
            continue
        
        t0 = time.perf_counter()
        try:
            from sentinel.parsers import get_parser_for_file
            parser = get_parser_for_file(str(file_path))
            if parser:
                parsed = parser.parse(str(file_path))
                result = provider.analyze(parsed)
                score = result.score
                
                results.append(SampleResult(
                    sample_id=sample.get("id", ""),
                    sha256=sample.get("sha256", ""),
                    ground_truth=sample.get("label", "unknown"),
                    provider="entropy",
                    raw_score=score,
                    findings_count=len(result.findings),
                    latency_ms=(time.perf_counter() - t0) * 1000,
                ))
        except Exception as e:
            results.append(SampleResult(
                sample_id=sample.get("id", ""),
                sha256=sample.get("sha256", ""),
                ground_truth=sample.get("label", "unknown"),
                provider="entropy",
                raw_score=0.0,
                error=str(e),
                latency_ms=(time.perf_counter() - t0) * 1000,
            ))
    
    return results


def run_strings_provider(samples: List[Dict]) -> List[SampleResult]:
    """Run strings analysis on samples."""
    results = []
    
    try:
        from sentinel.providers.static import StringsProvider
        provider = StringsProvider()
    except Exception as e:
        print(f"  Strings provider failed: {e}")
        return results
    
    scripts_dir = Path(__file__).parent.parent / "eval_data" / "scripts"
    
    for sample in samples:
        if sample.get("file_type") != "script":
            continue
        
        file_path = None
        for script in scripts_dir.iterdir():
            import hashlib
            with open(script, "rb") as f:
                content = f.read()
            sha = hashlib.sha256(content).hexdigest()
            if sha == sample.get("sha256"):
                file_path = script
                break
        
        if not file_path:
            continue
        
        t0 = time.perf_counter()
        try:
            from sentinel.parsers import get_parser_for_file
            parser = get_parser_for_file(str(file_path))
            if parser:
                parsed = parser.parse(str(file_path))
                result = provider.analyze(parsed)
                score = result.score
                
                results.append(SampleResult(
                    sample_id=sample.get("id", ""),
                    sha256=sample.get("sha256", ""),
                    ground_truth=sample.get("label", "unknown"),
                    provider="strings",
                    raw_score=score,
                    findings_count=len(result.findings),
                    latency_ms=(time.perf_counter() - t0) * 1000,
                ))
        except Exception as e:
            pass
    
    return results


def run_pe_ml_provider(samples: List[Dict]) -> List[SampleResult]:
    """Run PE ML provider on EMBER feature samples."""
    results = []
    
    try:
        from sentinel.providers.ml.malware_pe import MalwarePEProvider
        models_path = Path(__file__).parent.parent.parent / "CyberMesh" / "ai-service" / "data" / "models"
        provider = MalwarePEProvider(models_path=str(models_path))
        print(f"  PE ML provider loaded")
    except Exception as e:
        print(f"  PE ML provider failed: {e}")
        return results
    
    eval_dir = Path(__file__).parent.parent / "eval_data"
    
    for sample in samples:
        if sample.get("file_type") != "pe":
            continue
        
        # Load EMBER features
        file_path = sample.get("file_path", "")
        if not file_path.endswith(".features.json"):
            continue
        
        full_path = eval_dir / file_path
        if not full_path.exists():
            continue
        
        t0 = time.perf_counter()
        try:
            with open(full_path) as f:
                features = json.load(f)
            
            # Use provider's feature-based prediction if available
            if hasattr(provider, 'predict_from_features'):
                score = provider.predict_from_features(features)
            else:
                # Fallback: use histogram entropy as proxy
                hist = features.get("histogram", [])
                if hist:
                    total = sum(hist)
                    if total > 0:
                        probs = [h/total for h in hist if h > 0]
                        entropy = -sum(p * np.log2(p) for p in probs if p > 0)
                        score = min(1.0, entropy / 8.0)  # Normalize to 0-1
                    else:
                        score = 0.0
                else:
                    score = 0.0
            
            results.append(SampleResult(
                sample_id=sample.get("id", ""),
                sha256=sample.get("sha256", ""),
                ground_truth=sample.get("label", "unknown"),
                provider="pe_ml",
                raw_score=score,
                latency_ms=(time.perf_counter() - t0) * 1000,
            ))
        except Exception as e:
            pass
    
    return results


def run_behavioral_signatures(samples: List[Dict]) -> List[SampleResult]:
    """Run behavioral signatures on samples."""
    results = []
    
    try:
        from sentinel.fastpath.signatures import SignatureMatcher
        engine = SignatureMatcher()
        print(f"  Signature engine loaded: {len(engine._patterns)} patterns")
    except Exception as e:
        print(f"  Signature engine failed: {e}")
        return results
    
    scripts_dir = Path(__file__).parent.parent / "eval_data" / "scripts"
    
    for sample in samples:
        if sample.get("file_type") != "script":
            continue
        
        file_path = None
        for script in scripts_dir.iterdir():
            import hashlib
            with open(script, "rb") as f:
                content = f.read()
            sha = hashlib.sha256(content).hexdigest()
            if sha == sample.get("sha256"):
                file_path = script
                break
        
        if not file_path:
            continue
        
        t0 = time.perf_counter()
        try:
            from sentinel.parsers import get_parser_for_file
            parser = get_parser_for_file(str(file_path))
            if parser:
                parsed = parser.parse(str(file_path))
                matches = engine.match(parsed)
                score = min(1.0, len(matches) * 0.25)  # 0.25 per match
                
                results.append(SampleResult(
                    sample_id=sample.get("id", ""),
                    sha256=sample.get("sha256", ""),
                    ground_truth=sample.get("label", "unknown"),
                    provider="signatures",
                    raw_score=score,
                    findings_count=len(matches),
                    latency_ms=(time.perf_counter() - t0) * 1000,
                ))
        except Exception as e:
            pass
    
    return results


def compute_optimal_threshold(scores: List[float], labels: List[str]) -> Tuple[float, float, float, float, float]:
    """
    Find optimal threshold that maximizes F1 score.
    
    Returns: (threshold, precision, recall, f1, accuracy)
    """
    if not scores or not labels:
        return 0.5, 0.0, 0.0, 0.0, 0.0
    
    best_f1 = 0.0
    best_threshold = 0.5
    best_metrics = (0.0, 0.0, 0.0, 0.0)
    
    # Try thresholds from 0.1 to 0.9
    for threshold in np.arange(0.05, 0.95, 0.05):
        tp = fp = tn = fn = 0
        
        for score, label in zip(scores, labels):
            predicted_malicious = score >= threshold
            actual_malicious = label == "malicious"
            
            if predicted_malicious and actual_malicious:
                tp += 1
            elif predicted_malicious and not actual_malicious:
                fp += 1
            elif not predicted_malicious and actual_malicious:
                fn += 1
            else:
                tn += 1
        
        precision = tp / (tp + fp) if (tp + fp) > 0 else 0.0
        recall = tp / (tp + fn) if (tp + fn) > 0 else 0.0
        f1 = 2 * precision * recall / (precision + recall) if (precision + recall) > 0 else 0.0
        accuracy = (tp + tn) / len(scores) if scores else 0.0
        
        if f1 > best_f1:
            best_f1 = f1
            best_threshold = threshold
            best_metrics = (precision, recall, f1, accuracy)
    
    return best_threshold, best_metrics[0], best_metrics[1], best_metrics[2], best_metrics[3]


def compute_confusion_matrix(scores: List[float], labels: List[str], threshold: float) -> Dict[str, int]:
    """Compute confusion matrix at given threshold."""
    tp = fp = tn = fn = 0
    
    for score, label in zip(scores, labels):
        predicted_malicious = score >= threshold
        actual_malicious = label == "malicious"
        
        if predicted_malicious and actual_malicious:
            tp += 1
        elif predicted_malicious and not actual_malicious:
            fp += 1
        elif not predicted_malicious and actual_malicious:
            fn += 1
        else:
            tn += 1
    
    return {"TP": tp, "FP": fp, "TN": tn, "FN": fn}


def print_confusion_matrix(name: str, cm: Dict[str, int]):
    """Pretty print confusion matrix."""
    tp, fp, tn, fn = cm["TP"], cm["FP"], cm["TN"], cm["FN"]
    
    print(f"\n  {name} Confusion Matrix:")
    print(f"  {'':15} | Pred Malicious | Pred Clean")
    print(f"  {'-'*15}-+-{'-'*14}-+-{'-'*10}")
    print(f"  {'Actual Malicious':15} | {tp:^14} | {fn:^10}")
    print(f"  {'Actual Clean':15} | {fp:^14} | {tn:^10}")


def main():
    print("=" * 70)
    print("SENTINEL PROVIDER CALIBRATION")
    print("=" * 70)
    
    # Load samples
    samples = load_manifest()
    print(f"\nLoaded {len(samples)} samples from manifest")
    
    # Count by type
    by_type = defaultdict(int)
    by_label = defaultdict(int)
    for s in samples:
        by_type[s.get("file_type", "unknown")] += 1
        by_label[s.get("label", "unknown")] += 1
    
    print(f"  By type: {dict(by_type)}")
    print(f"  By label: {dict(by_label)}")
    
    # Run providers
    all_results: Dict[str, List[SampleResult]] = {}
    
    print("\n" + "-" * 70)
    print("Running providers...")
    print("-" * 70)
    
    # YARA
    print("\n[1/5] YARA Engine...")
    all_results["yara"] = run_yara_provider(samples)
    print(f"  Processed {len(all_results['yara'])} samples")
    
    # Entropy
    print("\n[2/5] Entropy Provider...")
    all_results["entropy"] = run_entropy_provider(samples)
    print(f"  Processed {len(all_results['entropy'])} samples")
    
    # Strings
    print("\n[3/5] Strings Provider...")
    all_results["strings"] = run_strings_provider(samples)
    print(f"  Processed {len(all_results['strings'])} samples")
    
    # Behavioral Signatures
    print("\n[4/5] Behavioral Signatures...")
    all_results["signatures"] = run_behavioral_signatures(samples)
    print(f"  Processed {len(all_results['signatures'])} samples")
    
    # PE ML
    print("\n[5/5] PE ML Provider...")
    all_results["pe_ml"] = run_pe_ml_provider(samples)
    print(f"  Processed {len(all_results['pe_ml'])} samples")
    
    # Compute statistics per provider
    print("\n" + "-" * 70)
    print("Computing statistics...")
    print("-" * 70)
    
    provider_stats: Dict[str, ProviderStats] = {}
    
    for provider_name, results in all_results.items():
        if not results:
            continue
        
        stats = ProviderStats(name=provider_name)
        
        # Separate by ground truth
        for r in results:
            if r.error:
                continue
            if r.ground_truth == "malicious":
                stats.malicious_scores.append(r.raw_score)
            elif r.ground_truth == "clean":
                stats.clean_scores.append(r.raw_score)
        
        if not stats.malicious_scores and not stats.clean_scores:
            continue
        
        # Compute mean/std
        if stats.malicious_scores:
            stats.malicious_mean = np.mean(stats.malicious_scores)
            stats.malicious_std = np.std(stats.malicious_scores)
        
        if stats.clean_scores:
            stats.clean_mean = np.mean(stats.clean_scores)
            stats.clean_std = np.std(stats.clean_scores)
        
        # Compute optimal threshold
        all_scores = stats.malicious_scores + stats.clean_scores
        all_labels = ["malicious"] * len(stats.malicious_scores) + ["clean"] * len(stats.clean_scores)
        
        threshold, precision, recall, f1, accuracy = compute_optimal_threshold(all_scores, all_labels)
        stats.optimal_threshold = threshold
        stats.precision = precision
        stats.recall = recall
        stats.f1 = f1
        stats.accuracy = accuracy
        
        # Confusion matrix
        stats.confusion_matrix = compute_confusion_matrix(all_scores, all_labels, threshold)
        
        provider_stats[provider_name] = stats
    
    # Compute weights: weight = 0.3×accuracy + 0.7×F1
    total_weight = 0.0
    for stats in provider_stats.values():
        stats.weight = 0.3 * stats.accuracy + 0.7 * stats.f1
        total_weight += stats.weight
    
    # Normalize weights
    if total_weight > 0:
        for stats in provider_stats.values():
            stats.weight /= total_weight
    
    # Print results
    print("\n" + "=" * 70)
    print("CALIBRATION RESULTS")
    print("=" * 70)
    
    print(f"\n{'Provider':<15} {'Mal Mean':<10} {'Mal Std':<10} {'Clean Mean':<12} {'Clean Std':<10}")
    print("-" * 70)
    for name, stats in sorted(provider_stats.items()):
        print(f"{name:<15} {stats.malicious_mean:<10.3f} {stats.malicious_std:<10.3f} "
              f"{stats.clean_mean:<12.3f} {stats.clean_std:<10.3f}")
    
    print(f"\n{'Provider':<15} {'Threshold':<12} {'Precision':<12} {'Recall':<10} {'F1':<10} {'Accuracy':<10} {'Weight':<10}")
    print("-" * 90)
    for name, stats in sorted(provider_stats.items(), key=lambda x: -x[1].f1):
        print(f"{name:<15} {stats.optimal_threshold:<12.2f} {stats.precision:<12.2f} "
              f"{stats.recall:<10.2f} {stats.f1:<10.2f} {stats.accuracy:<10.2f} {stats.weight:<10.3f}")
    
    # Print confusion matrices
    print("\n" + "=" * 70)
    print("CONFUSION MATRICES")
    print("=" * 70)
    
    for name, stats in sorted(provider_stats.items()):
        print_confusion_matrix(name, stats.confusion_matrix)
    
    # Generate calibrated config
    config = {
        "version": "1.0.0",
        "calibrated_at": time.strftime("%Y-%m-%dT%H:%M:%S"),
        "samples_used": len(samples),
        "providers": {}
    }
    
    for name, stats in provider_stats.items():
        config["providers"][name] = {
            "threshold": round(stats.optimal_threshold, 3),
            "weight": round(stats.weight, 4),
            "precision": round(stats.precision, 3),
            "recall": round(stats.recall, 3),
            "f1": round(stats.f1, 3),
            "accuracy": round(stats.accuracy, 3),
            "malicious_mean": round(stats.malicious_mean, 3),
            "malicious_std": round(stats.malicious_std, 3),
            "clean_mean": round(stats.clean_mean, 3),
            "clean_std": round(stats.clean_std, 3),
        }
    
    # Save config
    config_path = Path(__file__).parent.parent / "calibrated_config.json"
    with open(config_path, "w") as f:
        json.dump(config, f, indent=2)
    
    print("\n" + "=" * 70)
    print(f"Calibrated config saved to: {config_path}")
    print("=" * 70)
    
    # Print final config
    print("\nFinal calibrated_config.json:")
    print(json.dumps(config, indent=2))
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
