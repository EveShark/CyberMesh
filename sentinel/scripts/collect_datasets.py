#!/usr/bin/env python
"""
Multi-source dataset collector for Sentinel evaluation.

Sources:
1. HuggingFace: ember2018-malware, malware-detection-dataset
2. Kaggle: Malware Detection Memory Dump (if kaggle API available)
3. GitHub: theZoo samples, packed-pe dataset
4. Local: Windows system files for clean samples

Target: 500+ balanced samples (malicious/clean)
"""

import os
import sys
import json
import hashlib
import time
import urllib.request
from pathlib import Path
from typing import Dict, List, Optional
from datetime import datetime

# Paths
SCRIPT_DIR = Path(__file__).parent
PROJECT_DIR = SCRIPT_DIR.parent
EVAL_DIR = PROJECT_DIR / "eval_data"
MANIFEST_PATH = EVAL_DIR / "manifest.json"


def download_file(url: str, dest: Path, timeout: int = 60) -> bool:
    """Download a file from URL."""
    try:
        print(f"    Downloading {url[:60]}...")
        urllib.request.urlretrieve(url, dest)
        return True
    except Exception as e:
        print(f"    Failed: {e}")
        return False


def load_manifest() -> Dict:
    """Load or create manifest."""
    if MANIFEST_PATH.exists():
        with open(MANIFEST_PATH) as f:
            return json.load(f)
    return {
        "version": "2.0.0",
        "created": datetime.now().isoformat(),
        "samples": [],
        "statistics": {}
    }


def save_manifest(manifest: Dict):
    """Save manifest."""
    # Update statistics
    samples = manifest.get("samples", [])
    by_label = {}
    by_type = {}
    by_source = {}
    
    for s in samples:
        label = s.get("label", "unknown")
        ftype = s.get("file_type", "unknown")
        source = s.get("source", "unknown")
        
        by_label[label] = by_label.get(label, 0) + 1
        by_type[ftype] = by_type.get(ftype, 0) + 1
        by_source[source] = by_source.get(source, 0) + 1
    
    manifest["statistics"] = {
        "total": len(samples),
        "by_label": by_label,
        "by_type": by_type,
        "by_source": by_source,
        "updated": datetime.now().isoformat(),
    }
    
    with open(MANIFEST_PATH, "w") as f:
        json.dump(manifest, f, indent=2)


def get_existing_hashes(manifest: Dict) -> set:
    """Get existing sample hashes."""
    return {s.get("sha256", "") for s in manifest.get("samples", [])}


def collect_huggingface_ember(manifest: Dict, max_samples: int = 200) -> int:
    """Collect EMBER2018 samples from HuggingFace."""
    print("\n[HuggingFace] Collecting EMBER2018 dataset...")
    
    try:
        from datasets import load_dataset
    except ImportError:
        print("  Installing datasets library...")
        os.system(f"{sys.executable} -m pip install datasets -q")
        from datasets import load_dataset
    
    existing = get_existing_hashes(manifest)
    collected = 0
    
    try:
        # Load EMBER dataset from HuggingFace
        print("  Loading cw1521/ember2018-malware-v2...")
        dataset = load_dataset("cw1521/ember2018-malware-v2", split="train", streaming=True)
        
        mal_count = 0
        clean_count = 0
        target_per_class = max_samples // 2
        
        for sample in dataset:
            if mal_count >= target_per_class and clean_count >= target_per_class:
                break
            
            sha256 = sample.get("sha256", "")
            label_val = sample.get("label", -1)
            
            if not sha256 or sha256 in existing:
                continue
            
            # Label: 0=clean, 1=malicious
            if label_val == 1 and mal_count < target_per_class:
                label = "malicious"
                mal_count += 1
            elif label_val == 0 and clean_count < target_per_class:
                label = "clean"
                clean_count += 1
            else:
                continue
            
            manifest["samples"].append({
                "sha256": sha256,
                "file_type": "pe",
                "label": label,
                "source": "huggingface_ember2018",
                "features": {
                    "histogram": sample.get("histogram", []),
                    "byteentropy": sample.get("byteentropy", []),
                },
                "added": datetime.now().isoformat(),
            })
            existing.add(sha256)
            collected += 1
            
            if collected % 50 == 0:
                print(f"    Collected {collected} samples (mal: {mal_count}, clean: {clean_count})")
        
        print(f"  Collected {collected} EMBER samples")
        
    except Exception as e:
        print(f"  Error loading EMBER: {e}")
    
    return collected


def collect_huggingface_malware_top100(manifest: Dict, max_samples: int = 100) -> int:
    """Collect malware-top-100 dataset from HuggingFace."""
    print("\n[HuggingFace] Collecting PurCL/malware-top-100...")
    
    try:
        from datasets import load_dataset
    except ImportError:
        os.system(f"{sys.executable} -m pip install datasets -q")
        from datasets import load_dataset
    
    existing = get_existing_hashes(manifest)
    collected = 0
    
    try:
        dataset = load_dataset("PurCL/malware-top-100", split="train", streaming=True)
        
        for sample in dataset:
            if collected >= max_samples:
                break
            
            sha256 = sample.get("sha256", "")
            if not sha256 or sha256 in existing:
                continue
            
            # All samples in this dataset are malware
            labels = sample.get("labels", [])
            
            manifest["samples"].append({
                "sha256": sha256,
                "file_type": "pe",
                "label": "malicious",
                "source": "huggingface_malware_top100",
                "tags": labels,
                "added": datetime.now().isoformat(),
            })
            existing.add(sha256)
            collected += 1
        
        print(f"  Collected {collected} malware-top-100 samples")
        
    except Exception as e:
        print(f"  Error: {e}")
    
    return collected


def collect_windows_clean_samples(manifest: Dict, max_samples: int = 50) -> int:
    """Collect clean samples from Windows system directories."""
    print("\n[System] Collecting Windows clean samples...")
    
    if sys.platform != "win32":
        print("  Skipping (not Windows)")
        return 0
    
    existing = get_existing_hashes(manifest)
    collected = 0
    
    # Windows system directories with known-clean files
    win_dir = Path(os.environ.get("WINDIR", "C:\\Windows"))
    system32 = win_dir / "System32"
    
    clean_files = [
        system32 / "notepad.exe",
        system32 / "calc.exe", 
        system32 / "mspaint.exe",
        system32 / "write.exe",
        system32 / "charmap.exe",
        system32 / "cmd.exe",
        system32 / "taskmgr.exe",
        system32 / "msiexec.exe",
        system32 / "reg.exe",
        system32 / "control.exe",
        system32 / "explorer.exe",
        system32 / "regedit.exe",
        system32 / "svchost.exe",
        system32 / "services.exe",
        system32 / "lsass.exe",
        system32 / "csrss.exe",
        system32 / "winlogon.exe",
        system32 / "wininit.exe",
        system32 / "smss.exe",
        system32 / "dwm.exe",
        system32 / "conhost.exe",
        system32 / "dllhost.exe",
        system32 / "RuntimeBroker.exe",
        system32 / "SearchIndexer.exe",
        system32 / "spoolsv.exe",
        # DLLs
        system32 / "kernel32.dll",
        system32 / "user32.dll",
        system32 / "ntdll.dll",
        system32 / "advapi32.dll",
        system32 / "shell32.dll",
        system32 / "ole32.dll",
        system32 / "gdi32.dll",
        system32 / "ws2_32.dll",
        system32 / "crypt32.dll",
        system32 / "secur32.dll",
    ]
    
    for fpath in clean_files:
        if collected >= max_samples:
            break
        
        if not fpath.exists():
            continue
        
        try:
            with open(fpath, "rb") as f:
                content = f.read()
            
            sha256 = hashlib.sha256(content).hexdigest()
            
            if sha256 in existing:
                continue
            
            file_type = "dll" if fpath.suffix.lower() == ".dll" else "exe"
            
            manifest["samples"].append({
                "sha256": sha256,
                "file_name": fpath.name,
                "file_type": file_type,
                "file_size": len(content),
                "label": "clean",
                "source": "windows_system32",
                "tags": ["system", "microsoft", "signed"],
                "added": datetime.now().isoformat(),
            })
            existing.add(sha256)
            collected += 1
            print(f"    {fpath.name}")
            
        except Exception as e:
            pass
    
    print(f"  Collected {collected} Windows system samples")
    return collected


def collect_synthetic_scripts(manifest: Dict) -> int:
    """Ensure synthetic script samples are in manifest."""
    print("\n[Synthetic] Checking script samples...")
    
    scripts_dir = EVAL_DIR / "scripts"
    if not scripts_dir.exists():
        return 0
    
    existing = get_existing_hashes(manifest)
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
            "source": "synthetic",
            "local_path": f"scripts/{script.name}",
            "added": datetime.now().isoformat(),
        })
        existing.add(sha256)
        collected += 1
        print(f"    {script.name} ({label})")
    
    print(f"  Added {collected} script samples")
    return collected


def download_github_samples(manifest: Dict, max_samples: int = 50) -> int:
    """Download sample metadata from GitHub datasets."""
    print("\n[GitHub] Collecting packed-PE dataset metadata...")
    
    # The packed-PE dataset on GitHub has labels for packed vs not-packed
    # We can use not-packed as clean, packed as potentially suspicious
    
    existing = get_existing_hashes(manifest)
    collected = 0
    
    # For now, just note this as a potential source
    print("  Note: github.com/packing-box/dataset-packed-pe available for packed PE analysis")
    print("  Note: github.com/ytisf/theZoo available for live malware samples (requires caution)")
    
    return collected


def main():
    print("=" * 70)
    print("SENTINEL MULTI-SOURCE DATASET COLLECTOR")
    print("=" * 70)
    
    # Ensure directories exist
    EVAL_DIR.mkdir(parents=True, exist_ok=True)
    
    # Load manifest
    manifest = load_manifest()
    initial_count = len(manifest.get("samples", []))
    print(f"\nExisting samples in manifest: {initial_count}")
    
    total_collected = 0
    
    # 1. HuggingFace EMBER2018 (balanced PE features)
    total_collected += collect_huggingface_ember(manifest, max_samples=300)
    save_manifest(manifest)
    
    # 2. HuggingFace malware-top-100 (malware only)
    total_collected += collect_huggingface_malware_top100(manifest, max_samples=100)
    save_manifest(manifest)
    
    # 3. Windows clean samples
    total_collected += collect_windows_clean_samples(manifest, max_samples=50)
    save_manifest(manifest)
    
    # 4. Synthetic scripts
    total_collected += collect_synthetic_scripts(manifest)
    save_manifest(manifest)
    
    # 5. GitHub sources (metadata only)
    total_collected += download_github_samples(manifest, max_samples=50)
    save_manifest(manifest)
    
    # Final save
    save_manifest(manifest)
    
    # Print summary
    print("\n" + "=" * 70)
    print("COLLECTION SUMMARY")
    print("=" * 70)
    
    stats = manifest.get("statistics", {})
    print(f"\nTotal samples: {stats.get('total', 0)}")
    print(f"  New samples collected: {total_collected}")
    print(f"\nBy label: {stats.get('by_label', {})}")
    print(f"By type: {stats.get('by_type', {})}")
    print(f"By source: {stats.get('by_source', {})}")
    
    print(f"\nManifest saved to: {MANIFEST_PATH}")
    print("=" * 70)
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
