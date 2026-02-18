#!/usr/bin/env python
"""
Calibrate provider thresholds from benchmark results.

Uses the raw scores from benchmark results to find optimal thresholds
that maximize F1 score for each provider.
"""

import json
import numpy as np
from pathlib import Path
from typing import Dict, List, Tuple
from datetime import datetime

PROJECT_DIR = Path(__file__).parent.parent
RESULTS_DIR = PROJECT_DIR / "data" / "benchmark_results"
CONFIG_PATH = PROJECT_DIR / "calibrated_config.json"


def load_latest_results() -> Dict[str, List[Dict]]:
    """Load latest benchmark results per provider."""
    if not RESULTS_DIR.exists():
        print(f"No benchmark results found at {RESULTS_DIR}")
        return {}
    
    # Group by provider, keep latest
    provider_results = {}
    
    for result_file in RESULTS_DIR.glob("*.json"):
        with open(result_file) as f:
            data = json.load(f)
        
        provider = data.get("provider_name", "")
        timestamp = data.get("timestamp", 0)
        
        if provider not in provider_results or timestamp > provider_results[provider]["timestamp"]:
            provider_results[provider] = data
    
    return provider_results


def compute_optimal_threshold(sample_results: List[Dict]) -> Tuple[float, Dict]:
    """
    Find optimal threshold that maximizes F1 score.
    
    Returns: (optimal_threshold, metrics_dict)
    """
    if not sample_results:
        return 0.5, {}
    
    # Extract scores and labels
    scores = []
    labels = []  # True = malicious, False = clean
    
    for sr in sample_results:
        score = sr.get("score", 0.0)
        actual = sr.get("actual", "clean")
        is_malicious = actual in ("malicious", "critical", "suspicious")
        
        scores.append(score)
        labels.append(is_malicious)
    
    if not scores:
        return 0.5, {}
    
    # Try different thresholds
    best_f1 = 0.0
    best_threshold = 0.5
    best_metrics = {}
    
    for threshold in np.arange(0.01, 0.99, 0.01):
        tp = fp = tn = fn = 0
        
        for score, is_mal in zip(scores, labels):
            pred_mal = score >= threshold
            
            if pred_mal and is_mal:
                tp += 1
            elif pred_mal and not is_mal:
                fp += 1
            elif not pred_mal and is_mal:
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
            best_metrics = {
                "precision": precision,
                "recall": recall,
                "f1": f1,
                "accuracy": accuracy,
                "tp": tp,
                "fp": fp,
                "tn": tn,
                "fn": fn,
            }
    
    return best_threshold, best_metrics


def main():
    print("=" * 60)
    print("CALIBRATE FROM BENCHMARK RESULTS")
    print("=" * 60)
    
    # Load benchmark results
    results = load_latest_results()
    
    if not results:
        print("\nNo benchmark results found.")
        print("Run: python -m sentinel.cli benchmark all")
        return 1
    
    print(f"\nLoaded results for {len(results)} providers")
    
    # Compute optimal thresholds
    calibrated = {
        "version": "1.0.0",
        "calibrated_at": datetime.now().isoformat(),
        "providers": {}
    }
    
    print("\n" + "-" * 60)
    print(f"{'Provider':<20} {'Threshold':<12} {'F1':<10} {'Precision':<12} {'Recall':<10}")
    print("-" * 60)
    
    for provider, data in results.items():
        sample_results = data.get("sample_results", [])
        
        if not sample_results:
            print(f"{provider:<20} {'N/A':<12} {'N/A':<10}")
            continue
        
        threshold, metrics = compute_optimal_threshold(sample_results)
        
        # Compute weight: 0.3*accuracy + 0.7*F1
        weight = 0.3 * metrics.get("accuracy", 0) + 0.7 * metrics.get("f1", 0)
        
        # Score statistics
        mal_scores = [sr["score"] for sr in sample_results if sr["actual"] in ("malicious", "critical")]
        clean_scores = [sr["score"] for sr in sample_results if sr["actual"] == "clean"]
        
        calibrated["providers"][provider] = {
            "threshold": round(threshold, 3),
            "weight": round(weight, 4),
            "precision": round(metrics.get("precision", 0), 3),
            "recall": round(metrics.get("recall", 0), 3),
            "f1": round(metrics.get("f1", 0), 3),
            "accuracy": round(metrics.get("accuracy", 0), 3),
            "malicious_mean": round(np.mean(mal_scores), 3) if mal_scores else 0.0,
            "malicious_std": round(np.std(mal_scores), 3) if mal_scores else 0.0,
            "clean_mean": round(np.mean(clean_scores), 3) if clean_scores else 0.0,
            "clean_std": round(np.std(clean_scores), 3) if clean_scores else 0.0,
            "confusion": {
                "tp": metrics.get("tp", 0),
                "fp": metrics.get("fp", 0),
                "tn": metrics.get("tn", 0),
                "fn": metrics.get("fn", 0),
            }
        }
        
        print(f"{provider:<20} {threshold:<12.3f} {metrics.get('f1', 0):<10.3f} "
              f"{metrics.get('precision', 0):<12.3f} {metrics.get('recall', 0):<10.3f}")
    
    print("-" * 60)
    
    # Print confusion matrices
    print("\n" + "=" * 60)
    print("CONFUSION MATRICES")
    print("=" * 60)
    
    for provider, pdata in calibrated["providers"].items():
        cm = pdata.get("confusion", {})
        if not cm:
            continue
        
        print(f"\n  {provider}:")
        print(f"  {'':20} | Pred Malicious | Pred Clean")
        print(f"  {'-'*20}-+-{'-'*14}-+-{'-'*10}")
        print(f"  {'Actual Malicious':20} | {cm['tp']:^14} | {cm['fn']:^10}")
        print(f"  {'Actual Clean':20} | {cm['fp']:^14} | {cm['tn']:^10}")
    
    # Normalize weights
    total_weight = sum(p.get("weight", 0) for p in calibrated["providers"].values())
    if total_weight > 0:
        for p in calibrated["providers"].values():
            p["weight"] = round(p["weight"] / total_weight, 4)
    
    # Save config
    with open(CONFIG_PATH, "w") as f:
        json.dump(calibrated, f, indent=2)
    
    print("\n" + "=" * 60)
    print(f"Calibrated config saved to: {CONFIG_PATH}")
    print("=" * 60)
    
    print("\ncalibrated_config.json:")
    print(json.dumps(calibrated, indent=2))
    
    return 0


if __name__ == "__main__":
    exit(main())
