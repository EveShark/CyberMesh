#!/usr/bin/env python
"""
Fast dataset collector - downloads pre-built CSV/features datasets.
"""

import os
import sys
import json
import hashlib
import time
import urllib.request
from pathlib import Path
from datetime import datetime

PROJECT_DIR = Path(__file__).parent.parent
EVAL_DIR = PROJECT_DIR / "eval_data"
MANIFEST_PATH = EVAL_DIR / "manifest.json"


def load_manifest():
    if MANIFEST_PATH.exists():
        with open(MANIFEST_PATH) as f:
            return json.load(f)
    return {"version": "2.0.0", "samples": [], "statistics": {}}


def save_manifest(manifest):
    samples = manifest.get("samples", [])
    by_label = {}
    by_type = {}
    by_source = {}
    
    for s in samples:
        by_label[s.get("label", "unknown")] = by_label.get(s.get("label", "unknown"), 0) + 1
        by_type[s.get("file_type", "unknown")] = by_type.get(s.get("file_type", "unknown"), 0) + 1
        by_source[s.get("source", "unknown")] = by_source.get(s.get("source", "unknown"), 0) + 1
    
    manifest["statistics"] = {
        "total": len(samples),
        "by_label": by_label,
        "by_type": by_type, 
        "by_source": by_source,
        "updated": datetime.now().isoformat(),
    }
    
    with open(MANIFEST_PATH, "w") as f:
        json.dump(manifest, f, indent=2)


def collect_windows_samples(manifest, max_samples=100):
    """Collect Windows system files as clean samples."""
    print("\n[1/3] Collecting Windows system samples...")
    
    if sys.platform != "win32":
        print("  Skipping (not Windows)")
        return 0
    
    existing = {s.get("sha256", "") for s in manifest.get("samples", [])}
    collected = 0
    
    win_dir = Path(os.environ.get("WINDIR", "C:\\Windows"))
    system32 = win_dir / "System32"
    
    # EXEs
    exe_files = list(system32.glob("*.exe"))[:50]
    # DLLs
    dll_files = list(system32.glob("*.dll"))[:50]
    
    for fpath in exe_files + dll_files:
        if collected >= max_samples:
            break
        
        try:
            with open(fpath, "rb") as f:
                content = f.read()
            
            sha256 = hashlib.sha256(content).hexdigest()
            if sha256 in existing:
                continue
            
            manifest["samples"].append({
                "sha256": sha256,
                "file_name": fpath.name,
                "file_type": "dll" if fpath.suffix.lower() == ".dll" else "exe",
                "file_size": len(content),
                "label": "clean",
                "source": "windows_system32",
                "added": datetime.now().isoformat(),
            })
            existing.add(sha256)
            collected += 1
            
        except:
            pass
    
    print(f"  Collected {collected} Windows samples")
    return collected


def collect_script_samples(manifest):
    """Collect local script samples."""
    print("\n[2/3] Collecting script samples...")
    
    scripts_dir = EVAL_DIR / "scripts"
    if not scripts_dir.exists():
        return 0
    
    existing = {s.get("sha256", "") for s in manifest.get("samples", [])}
    collected = 0
    
    for script in scripts_dir.iterdir():
        if not script.is_file():
            continue
        
        with open(script, "rb") as f:
            content = f.read()
        
        sha256 = hashlib.sha256(content).hexdigest()
        if sha256 in existing:
            continue
        
        label = "malicious" if "malicious" in script.name.lower() else "clean"
        
        manifest["samples"].append({
            "sha256": sha256,
            "file_name": script.name,
            "file_type": "script",
            "file_size": len(content),
            "label": label,
            "source": "synthetic_scripts",
            "local_path": f"scripts/{script.name}",
            "added": datetime.now().isoformat(),
        })
        existing.add(sha256)
        collected += 1
    
    print(f"  Collected {collected} script samples")
    return collected


def generate_synthetic_pe_features(manifest, count=400):
    """Generate synthetic PE feature samples for calibration testing."""
    print(f"\n[3/3] Generating {count} synthetic PE feature samples...")
    
    import random
    import numpy as np
    
    existing = {s.get("sha256", "") for s in manifest.get("samples", [])}
    collected = 0
    
    # Generate balanced malicious/clean samples
    for i in range(count):
        # Alternate between malicious and clean
        is_malicious = i % 2 == 0
        
        # Generate fake sha256
        sha256 = hashlib.sha256(f"synthetic_{i}_{time.time()}".encode()).hexdigest()
        if sha256 in existing:
            continue
        
        # Generate realistic PE features
        if is_malicious:
            # Malicious patterns: higher entropy, suspicious imports
            entropy = random.uniform(6.5, 7.8)
            imports = random.randint(50, 200)
            exports = random.randint(0, 5)
            suspicious_apis = random.randint(5, 20)
            packed = random.random() > 0.6
            category = random.choice(["trojan", "ransomware", "backdoor", "worm", "spyware", "downloader"])
        else:
            # Clean patterns: normal entropy, fewer suspicious indicators
            entropy = random.uniform(4.0, 6.5)
            imports = random.randint(20, 100)
            exports = random.randint(0, 50)
            suspicious_apis = random.randint(0, 3)
            packed = random.random() > 0.9
            category = None
        
        # Generate histogram (byte distribution)
        if is_malicious and packed:
            # Packed: more uniform distribution
            hist = [random.randint(800, 1200) for _ in range(256)]
        else:
            # Normal: peaked distribution
            hist = [random.randint(0, 100) for _ in range(256)]
            # Add peaks at common byte values
            for peak in [0x00, 0x20, 0x2E, 0x65, 0x6F, 0x74, 0xFF]:
                hist[peak] = random.randint(5000, 20000)
        
        manifest["samples"].append({
            "sha256": sha256,
            "file_type": "pe",
            "label": "malicious" if is_malicious else "clean",
            "source": "synthetic_pe",
            "category": category,
            "features": {
                "entropy": round(entropy, 3),
                "imports": imports,
                "exports": exports,
                "suspicious_apis": suspicious_apis,
                "packed": packed,
                "histogram": hist[:10],  # Just first 10 for space
            },
            "added": datetime.now().isoformat(),
        })
        existing.add(sha256)
        collected += 1
    
    print(f"  Generated {collected} synthetic PE samples")
    return collected


def main():
    print("=" * 60)
    print("SENTINEL FAST DATASET COLLECTOR")
    print("=" * 60)
    
    EVAL_DIR.mkdir(parents=True, exist_ok=True)
    manifest = load_manifest()
    
    initial = len(manifest.get("samples", []))
    print(f"\nExisting samples: {initial}")
    
    total = 0
    
    # Collect real Windows samples
    total += collect_windows_samples(manifest, max_samples=100)
    save_manifest(manifest)
    
    # Collect script samples  
    total += collect_script_samples(manifest)
    save_manifest(manifest)
    
    # Generate synthetic PE features for calibration
    total += generate_synthetic_pe_features(manifest, count=400)
    save_manifest(manifest)
    
    # Summary
    print("\n" + "=" * 60)
    print("SUMMARY")
    print("=" * 60)
    
    stats = manifest["statistics"]
    print(f"\nTotal samples: {stats['total']}")
    print(f"New samples: {total}")
    print(f"\nBy label: {stats['by_label']}")
    print(f"By type: {stats['by_type']}")
    print(f"By source: {stats['by_source']}")
    
    print(f"\nManifest: {MANIFEST_PATH}")
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
