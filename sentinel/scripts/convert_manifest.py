#!/usr/bin/env python
"""
Convert eval_data/manifest.json to Sentinel's EvalSuite format.
Creates data/eval_samples/manifest.json with proper structure.
"""

import json
import shutil
from pathlib import Path

PROJECT_DIR = Path(__file__).parent.parent
EVAL_DATA_DIR = PROJECT_DIR / "eval_data"
EVAL_SAMPLES_DIR = PROJECT_DIR / "data" / "eval_samples"


def main():
    print("=" * 60)
    print("CONVERT MANIFEST TO EVALSUITE FORMAT")
    print("=" * 60)
    
    # Load source manifest
    src_manifest = EVAL_DATA_DIR / "manifest.json"
    if not src_manifest.exists():
        print(f"Source manifest not found: {src_manifest}")
        return 1
    
    with open(src_manifest) as f:
        data = json.load(f)
    
    print(f"\nSource: {src_manifest}")
    print(f"  Total samples: {len(data.get('samples', []))}")
    
    # Create destination directory
    EVAL_SAMPLES_DIR.mkdir(parents=True, exist_ok=True)
    
    # Convert samples
    converted_samples = []
    skipped = 0
    
    # Map labels to ThreatLevel values
    label_map = {
        "malicious": "malicious",
        "clean": "clean",
        "suspicious": "suspicious",
    }
    
    # Map file types
    type_map = {
        "script": "script",
        "pe": "pe",
        "exe": "pe",
        "dll": "pe",
        "pdf": "pdf",
        "doc": "document",
        "docx": "document",
        "xls": "document",
        "xlsx": "document",
        "android": "android",
        "apk": "android",
        "pcap": "pcap",
    }
    
    # First, add all script files directly from scripts directory
    scripts_dir = EVAL_DATA_DIR / "scripts"
    if scripts_dir.exists():
        for script in scripts_dir.iterdir():
            if not script.is_file():
                continue
            
            # Determine label from filename
            label = "malicious" if "malicious" in script.name.lower() else "clean"
            ground_truth = label
            
            # Copy to eval_samples
            dest_subdir = "malicious" if ground_truth == "malicious" else "clean"
            dest_dir = EVAL_SAMPLES_DIR / dest_subdir
            dest_dir.mkdir(parents=True, exist_ok=True)
            
            dest_path = dest_dir / script.name
            if not dest_path.exists():
                shutil.copy2(script, dest_path)
            
            converted_samples.append({
                "file_path": str(dest_path),
                "file_type": "script",
                "ground_truth": ground_truth,
                "label": script.stem,
                "metadata": {"source": "scripts_dir"}
            })
            print(f"  Added script: {script.name} ({label})")
    
    # Then process manifest samples
    for sample in data.get("samples", []):
        # Get file path
        local_path = sample.get("local_path", "")
        file_name = sample.get("file_name", "")
        
        # Try to find actual file
        actual_path = None
        
        if local_path:
            full_path = EVAL_DATA_DIR / local_path
            if full_path.exists():
                actual_path = full_path
        
        if not actual_path and file_name:
            # Check scripts directory
            scripts_path = EVAL_DATA_DIR / "scripts" / file_name
            if scripts_path.exists():
                actual_path = scripts_path
            
            # Check clean directory
            clean_path = EVAL_DATA_DIR / "clean" / file_name
            if clean_path.exists():
                actual_path = clean_path
            
            # Check malicious directory
            mal_path = EVAL_DATA_DIR / "malicious" / file_name
            if mal_path.exists():
                actual_path = mal_path
        
        if not actual_path:
            skipped += 1
            continue
        
        # Map file type
        src_type = sample.get("file_type", "unknown")
        file_type = type_map.get(src_type, "binary")
        
        # Map label to ground truth
        label = sample.get("label", "unknown")
        ground_truth = label_map.get(label, "unknown")
        
        if ground_truth == "unknown":
            skipped += 1
            continue
        
        # Copy file to eval_samples directory
        dest_subdir = "malicious" if ground_truth == "malicious" else "clean"
        dest_dir = EVAL_SAMPLES_DIR / dest_subdir
        dest_dir.mkdir(parents=True, exist_ok=True)
        
        dest_path = dest_dir / actual_path.name
        if not dest_path.exists():
            shutil.copy2(actual_path, dest_path)
        
        converted_samples.append({
            "file_path": str(dest_path),
            "file_type": file_type,
            "ground_truth": ground_truth,
            "label": sample.get("category", "") or label,
            "metadata": {
                "sha256": sample.get("sha256", ""),
                "source": sample.get("source", ""),
                "tags": sample.get("tags", []),
            }
        })
    
    # Save converted manifest
    dest_manifest = EVAL_SAMPLES_DIR / "manifest.json"
    
    output = {
        "version": "1.0.0",
        "description": "Converted from eval_data for Sentinel EvalSuite",
        "samples": converted_samples,
    }
    
    with open(dest_manifest, "w") as f:
        json.dump(output, f, indent=2)
    
    # Summary
    print(f"\nDestination: {dest_manifest}")
    print(f"  Converted: {len(converted_samples)}")
    print(f"  Skipped (no file/unknown label): {skipped}")
    
    # Count by type
    by_type = {}
    by_truth = {}
    for s in converted_samples:
        t = s["file_type"]
        g = s["ground_truth"]
        by_type[t] = by_type.get(t, 0) + 1
        by_truth[g] = by_truth.get(g, 0) + 1
    
    print(f"\nBy file type: {by_type}")
    print(f"By ground truth: {by_truth}")
    
    print("\n" + "=" * 60)
    print("Done! Now run: python -m sentinel.cli benchmark all")
    print("=" * 60)
    
    return 0


if __name__ == "__main__":
    exit(main())
