#!/usr/bin/env python
"""
Simplified Provider Calibration for Sentinel.
Runs on available script samples and EMBER PE features.
"""

import os
import sys
import json
import time
import hashlib
import numpy as np
from pathlib import Path
from typing import Dict, List, Tuple
from dataclasses import dataclass, field
from collections import defaultdict

# Paths
SCRIPT_DIR = Path(__file__).parent
PROJECT_DIR = SCRIPT_DIR.parent
EVAL_DIR = PROJECT_DIR / "eval_data"
SCRIPTS_DIR = EVAL_DIR / "scripts"
RULES_DIR = PROJECT_DIR / "rules"


@dataclass 
class ProviderStats:
    name: str
    scores: List[Tuple[float, str]] = field(default_factory=list)  # (score, label)
    threshold: float = 0.5
    precision: float = 0.0
    recall: float = 0.0
    f1: float = 0.0
    accuracy: float = 0.0
    weight: float = 0.0
    tp: int = 0
    fp: int = 0
    tn: int = 0
    fn: int = 0


def load_script_samples() -> List[Dict]:
    """Load script samples from eval_data/scripts."""
    samples = []
    if not SCRIPTS_DIR.exists():
        return samples
    
    for script in SCRIPTS_DIR.iterdir():
        if not script.is_file():
            continue
        
        with open(script, "rb") as f:
            content = f.read()
        
        sha256 = hashlib.sha256(content).hexdigest()
        label = "malicious" if "malicious" in script.name.lower() else "clean"
        
        samples.append({
            "path": script,
            "name": script.name,
            "sha256": sha256,
            "label": label,
            "content": content,
            "type": "script",
        })
    
    return samples


def load_ember_samples() -> List[Dict]:
    """Load PE feature samples from manifest (including synthetic)."""
    samples = []
    
    manifest_path = EVAL_DIR / "manifest.json"
    if not manifest_path.exists():
        return samples
    
    with open(manifest_path) as f:
        manifest = json.load(f)
    
    for sample in manifest.get("samples", []):
        file_type = sample.get("file_type", "")
        if file_type not in ("pe", "exe", "dll"):
            continue
        
        # Get features from manifest or create minimal features
        features = sample.get("features", {})
        if not features:
            # Create minimal features for Windows samples
            features = {
                "entropy": 5.0,  # Default normal entropy
                "imports": 50,
                "exports": 10,
            }
        
        samples.append({
            "path": None,
            "name": sample.get("file_name", sample.get("sha256", "")[:16]),
            "sha256": sample.get("sha256", ""),
            "label": sample.get("label", "unknown"),
            "features": features,
            "type": "pe",
            "source": sample.get("source", "unknown"),
        })
    
    return samples


def run_yara(samples: List[Dict]) -> ProviderStats:
    """Run YARA rules on script samples."""
    stats = ProviderStats(name="yara")
    
    try:
        import yara
    except ImportError:
        print("  YARA not installed")
        return stats
    
    # Compile rules
    rule_files = list(RULES_DIR.rglob("*.yar")) + list(RULES_DIR.rglob("*.yara"))
    compiled = []
    
    for rf in rule_files[:100]:  # Limit for speed
        try:
            rule = yara.compile(filepath=str(rf))
            compiled.append(rule)
        except:
            pass
    
    print(f"  Compiled {len(compiled)} YARA rules")
    
    for sample in samples:
        if sample["type"] != "script":
            continue
        
        content = sample.get("content", b"")
        matches = []
        
        for rule in compiled:
            try:
                m = rule.match(data=content)
                matches.extend(m)
            except:
                pass
        
        score = min(1.0, len(matches) * 0.2)
        stats.scores.append((score, sample["label"]))
    
    return stats


def run_entropy(samples: List[Dict]) -> ProviderStats:
    """Calculate entropy scores."""
    stats = ProviderStats(name="entropy")
    
    for sample in samples:
        if sample["type"] != "script":
            continue
        
        content = sample.get("content", b"")
        if not content:
            continue
        
        # Calculate Shannon entropy
        byte_counts = [0] * 256
        for byte in content:
            byte_counts[byte] += 1
        
        length = len(content)
        entropy = 0.0
        for count in byte_counts:
            if count > 0:
                p = count / length
                entropy -= p * np.log2(p)
        
        # Normalize to 0-1 (max entropy is 8 bits)
        score = entropy / 8.0
        
        # High entropy (>0.9) is suspicious
        stats.scores.append((score, sample["label"]))
    
    return stats


def run_strings(samples: List[Dict]) -> ProviderStats:
    """Run string pattern analysis."""
    stats = ProviderStats(name="strings")
    
    # Suspicious patterns
    patterns = [
        b"Invoke-Expression", b"IEX", b"DownloadString", b"DownloadFile",
        b"WebClient", b"Net.WebClient", b"Start-Process", b"cmd /c",
        b"powershell -e", b"-encodedcommand", b"FromBase64String",
        b"WScript.Shell", b"Scripting.FileSystemObject",
        b"certutil", b"bitsadmin", b"regsvr32", b"mshta",
        b"schtasks", b"at.exe", b"reg add", b"HKLM\\",
        b"http://", b"https://", b"ftp://",
    ]
    
    for sample in samples:
        if sample["type"] != "script":
            continue
        
        content = sample.get("content", b"")
        content_lower = content.lower()
        
        matches = sum(1 for p in patterns if p.lower() in content_lower)
        score = min(1.0, matches * 0.1)  # 0.1 per match
        
        stats.scores.append((score, sample["label"]))
    
    return stats


def run_pe_features(samples: List[Dict]) -> ProviderStats:
    """Analyze PE feature samples using entropy and import heuristics."""
    stats = ProviderStats(name="pe_features")
    
    for sample in samples:
        if sample["type"] != "pe":
            continue
        
        features = sample.get("features", {})
        
        # Score based on multiple features
        score = 0.0
        
        # 1. Entropy (high entropy = suspicious)
        entropy = features.get("entropy", 5.0)
        if entropy > 7.0:
            score += 0.4  # Very high entropy (packed/encrypted)
        elif entropy > 6.5:
            score += 0.25  # High entropy
        elif entropy > 6.0:
            score += 0.1  # Slightly elevated
        
        # 2. Suspicious API count
        suspicious_apis = features.get("suspicious_apis", 0)
        score += min(0.3, suspicious_apis * 0.03)  # Up to 0.3 for many suspicious APIs
        
        # 3. Packed indicator
        if features.get("packed", False):
            score += 0.2
        
        # 4. Import/export ratio heuristic
        imports = features.get("imports", 50)
        exports = features.get("exports", 10)
        
        if imports > 150:
            score += 0.1  # Unusually many imports
        if exports == 0 and imports > 50:
            score += 0.1  # No exports but many imports (common in malware)
        
        # 5. Histogram entropy if available
        hist = features.get("histogram", [])
        if hist and len(hist) >= 10:
            total = sum(hist)
            if total > 0:
                probs = [h/total for h in hist if h > 0]
                hist_entropy = -sum(p * np.log2(p) for p in probs if p > 0)
                # Very uniform distribution (packed) is suspicious
                if hist_entropy > 7.5:
                    score += 0.15
        
        score = min(1.0, score)
        stats.scores.append((score, sample["label"]))
    
    return stats


def run_signatures(samples: List[Dict]) -> ProviderStats:
    """Run behavioral signature patterns."""
    stats = ProviderStats(name="signatures")
    
    # High-confidence malicious patterns
    signatures = [
        (b"Invoke-Mimikatz", 0.9),
        (b"Get-GPPPassword", 0.9),
        (b"Invoke-Shellcode", 0.9),
        (b"AmsiUtils", 0.8),
        (b"Reflection.Assembly", 0.6),
        (b"VirtualAlloc", 0.5),
        (b"CreateThread", 0.4),
        (b"WriteProcessMemory", 0.7),
        (b"OpenProcess", 0.4),
        (b"-nop -w hidden", 0.8),
        (b"bypass", 0.3),
        (b"payload", 0.4),
        (b"shellcode", 0.8),
        (b"reverse_tcp", 0.9),
        (b"meterpreter", 0.95),
    ]
    
    for sample in samples:
        if sample["type"] != "script":
            continue
        
        content = sample.get("content", b"")
        content_lower = content.lower()
        
        max_score = 0.0
        for pattern, weight in signatures:
            if pattern.lower() in content_lower:
                max_score = max(max_score, weight)
        
        stats.scores.append((max_score, sample["label"]))
    
    return stats


def compute_metrics(stats: ProviderStats) -> ProviderStats:
    """Compute optimal threshold and metrics."""
    if not stats.scores:
        return stats
    
    best_f1 = 0.0
    best_threshold = 0.5
    best_metrics = {}
    
    # Try thresholds
    for threshold in np.arange(0.05, 0.95, 0.05):
        tp = fp = tn = fn = 0
        
        for score, label in stats.scores:
            pred_mal = score >= threshold
            actual_mal = label == "malicious"
            
            if pred_mal and actual_mal:
                tp += 1
            elif pred_mal and not actual_mal:
                fp += 1
            elif not pred_mal and actual_mal:
                fn += 1
            else:
                tn += 1
        
        precision = tp / (tp + fp) if (tp + fp) > 0 else 0.0
        recall = tp / (tp + fn) if (tp + fn) > 0 else 0.0
        f1 = 2 * precision * recall / (precision + recall) if (precision + recall) > 0 else 0.0
        accuracy = (tp + tn) / len(stats.scores)
        
        if f1 > best_f1:
            best_f1 = f1
            best_threshold = threshold
            best_metrics = {
                "precision": precision,
                "recall": recall,
                "f1": f1,
                "accuracy": accuracy,
                "tp": tp, "fp": fp, "tn": tn, "fn": fn,
            }
    
    stats.threshold = best_threshold
    stats.precision = best_metrics.get("precision", 0)
    stats.recall = best_metrics.get("recall", 0)
    stats.f1 = best_metrics.get("f1", 0)
    stats.accuracy = best_metrics.get("accuracy", 0)
    stats.tp = best_metrics.get("tp", 0)
    stats.fp = best_metrics.get("fp", 0)
    stats.tn = best_metrics.get("tn", 0)
    stats.fn = best_metrics.get("fn", 0)
    
    return stats


def main():
    print("=" * 70)
    print("SENTINEL PROVIDER CALIBRATION")
    print("=" * 70)
    
    # Load samples
    print("\nLoading samples...")
    script_samples = load_script_samples()
    ember_samples = load_ember_samples()
    
    print(f"  Script samples: {len(script_samples)}")
    print(f"  EMBER PE samples: {len(ember_samples)}")
    
    all_samples = script_samples + ember_samples
    
    # Count labels
    mal_count = sum(1 for s in all_samples if s["label"] == "malicious")
    clean_count = sum(1 for s in all_samples if s["label"] == "clean")
    print(f"  Total: {len(all_samples)} (malicious: {mal_count}, clean: {clean_count})")
    
    # Run providers
    print("\n" + "-" * 70)
    print("Running providers...")
    print("-" * 70)
    
    providers = []
    
    print("\n[1/5] YARA...")
    stats = run_yara(all_samples)
    stats = compute_metrics(stats)
    providers.append(stats)
    print(f"  Samples: {len(stats.scores)}, F1: {stats.f1:.3f}")
    
    print("\n[2/5] Entropy...")
    stats = run_entropy(all_samples)
    stats = compute_metrics(stats)
    providers.append(stats)
    print(f"  Samples: {len(stats.scores)}, F1: {stats.f1:.3f}")
    
    print("\n[3/5] Strings...")
    stats = run_strings(all_samples)
    stats = compute_metrics(stats)
    providers.append(stats)
    print(f"  Samples: {len(stats.scores)}, F1: {stats.f1:.3f}")
    
    print("\n[4/5] Signatures...")
    stats = run_signatures(all_samples)
    stats = compute_metrics(stats)
    providers.append(stats)
    print(f"  Samples: {len(stats.scores)}, F1: {stats.f1:.3f}")
    
    print("\n[5/5] PE Features...")
    stats = run_pe_features(all_samples)
    stats = compute_metrics(stats)
    providers.append(stats)
    print(f"  Samples: {len(stats.scores)}, F1: {stats.f1:.3f}")
    
    # Compute weights
    print("\n" + "-" * 70)
    print("Computing weights...")
    print("-" * 70)
    
    total_weight = 0.0
    for p in providers:
        if p.scores:
            p.weight = 0.3 * p.accuracy + 0.7 * p.f1
            total_weight += p.weight
    
    if total_weight > 0:
        for p in providers:
            p.weight /= total_weight
    
    # Print results
    print("\n" + "=" * 70)
    print("CALIBRATION RESULTS")
    print("=" * 70)
    
    print(f"\n{'Provider':<15} {'Threshold':<10} {'Precision':<10} {'Recall':<10} {'F1':<10} {'Accuracy':<10} {'Weight':<10}")
    print("-" * 85)
    
    for p in sorted(providers, key=lambda x: -x.f1):
        if p.scores:
            print(f"{p.name:<15} {p.threshold:<10.2f} {p.precision:<10.3f} {p.recall:<10.3f} "
                  f"{p.f1:<10.3f} {p.accuracy:<10.3f} {p.weight:<10.4f}")
    
    # Confusion matrices
    print("\n" + "=" * 70)
    print("CONFUSION MATRICES")
    print("=" * 70)
    
    for p in providers:
        if not p.scores:
            continue
        print(f"\n  {p.name}:")
        print(f"  {'':20} | Pred Malicious | Pred Clean")
        print(f"  {'-'*20}-+-{'-'*14}-+-{'-'*10}")
        print(f"  {'Actual Malicious':20} | {p.tp:^14} | {p.fn:^10}")
        print(f"  {'Actual Clean':20} | {p.fp:^14} | {p.tn:^10}")
    
    # Generate config
    config = {
        "version": "1.0.0",
        "calibrated_at": time.strftime("%Y-%m-%dT%H:%M:%S"),
        "samples_used": len(all_samples),
        "providers": {}
    }
    
    for p in providers:
        if p.scores:
            # Compute malicious/clean means
            mal_scores = [s for s, l in p.scores if l == "malicious"]
            clean_scores = [s for s, l in p.scores if l == "clean"]
            
            config["providers"][p.name] = {
                "threshold": round(p.threshold, 3),
                "weight": round(p.weight, 4),
                "precision": round(p.precision, 3),
                "recall": round(p.recall, 3),
                "f1": round(p.f1, 3),
                "accuracy": round(p.accuracy, 3),
                "malicious_mean": round(np.mean(mal_scores), 3) if mal_scores else 0.0,
                "malicious_std": round(np.std(mal_scores), 3) if mal_scores else 0.0,
                "clean_mean": round(np.mean(clean_scores), 3) if clean_scores else 0.0,
                "clean_std": round(np.std(clean_scores), 3) if clean_scores else 0.0,
            }
    
    # Save config
    config_path = PROJECT_DIR / "calibrated_config.json"
    with open(config_path, "w") as f:
        json.dump(config, f, indent=2)
    
    print("\n" + "=" * 70)
    print(f"Config saved to: {config_path}")
    print("=" * 70)
    
    print("\ncalibrated_config.json:")
    print(json.dumps(config, indent=2))
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
