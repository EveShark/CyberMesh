#!/usr/bin/env python
"""
Sample collector for Sentinel evaluation suite.

1. MalwareBazaar: Fetch recent malware samples
2. Clean samples: Collect known-good files
3. Update manifest.json with metadata
"""

import os
import sys
import json
import hashlib
import shutil
import time
from pathlib import Path
from datetime import datetime
from typing import Optional, Dict, List, Set

sys.path.insert(0, str(Path(__file__).parent.parent))

from dotenv import load_dotenv
load_dotenv(Path(__file__).parent.parent / ".env")

import httpx

# Directories
EVAL_DIR = Path(__file__).parent.parent / "eval_data"
MALICIOUS_DIR = EVAL_DIR / "malicious"
CLEAN_DIR = EVAL_DIR / "clean"
MANIFEST_PATH = EVAL_DIR / "manifest.json"

# MalwareBazaar API
MB_API = "https://mb-api.abuse.ch/api/v1/"
MB_API_KEY = os.environ.get("MALWAREBAZAAR_API_KEY", "")

# File type filters
WANTED_TYPES = {"exe", "dll", "pdf", "doc", "docx", "xls", "xlsx", "ps1", "vbs", "bat", "js", "jar"}


def compute_sha256(file_path: Path) -> str:
    """Compute SHA256 hash of a file."""
    sha256 = hashlib.sha256()
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            sha256.update(chunk)
    return sha256.hexdigest()


def load_manifest() -> Dict:
    """Load existing manifest or create new one."""
    if MANIFEST_PATH.exists():
        with open(MANIFEST_PATH) as f:
            return json.load(f)
    return {"samples": [], "metadata": {"created": datetime.now().isoformat()}}


def save_manifest(manifest: Dict):
    """Save manifest to disk."""
    if "metadata" not in manifest:
        manifest["metadata"] = {"created": datetime.now().isoformat()}
    manifest["metadata"]["updated"] = datetime.now().isoformat()
    with open(MANIFEST_PATH, "w") as f:
        json.dump(manifest, f, indent=2)


def get_existing_hashes(manifest: Dict) -> Set[str]:
    """Get set of existing sample hashes."""
    return {s["sha256"] for s in manifest.get("samples", [])}


def fetch_malwarebazaar_samples(limit: int = 200) -> List[Dict]:
    """Fetch recent samples from MalwareBazaar API."""
    print(f"\n[MalwareBazaar] Fetching {limit} recent samples...")
    
    headers = {"Accept": "application/json"}
    if MB_API_KEY:
        headers["Auth-Key"] = MB_API_KEY
    
    samples = []
    
    try:
        with httpx.Client(timeout=30) as client:
            # Get recent samples
            response = client.post(
                MB_API,
                data={"query": "get_recent", "limit": str(limit)},
                headers=headers,
            )
            response.raise_for_status()
            data = response.json()
            
            if data.get("query_status") == "ok":
                for sample in data.get("data", []):
                    file_type = sample.get("file_type", "").lower()
                    file_ext = sample.get("file_name", "").split(".")[-1].lower() if "." in sample.get("file_name", "") else ""
                    
                    # Filter by wanted types
                    if file_type in WANTED_TYPES or file_ext in WANTED_TYPES:
                        samples.append({
                            "sha256": sample.get("sha256_hash"),
                            "sha1": sample.get("sha1_hash"),
                            "md5": sample.get("md5_hash"),
                            "file_name": sample.get("file_name"),
                            "file_type": file_type or file_ext,
                            "file_size": sample.get("file_size"),
                            "signature": sample.get("signature"),
                            "tags": sample.get("tags", []),
                            "first_seen": sample.get("first_seen"),
                        })
                
                print(f"  Found {len(samples)} samples matching wanted types")
            else:
                print(f"  API error: {data.get('query_status')}")
                
    except Exception as e:
        print(f"  Error fetching samples: {e}")
    
    return samples


def download_malware_sample(sha256: str, dest_dir: Path) -> Optional[Path]:
    """Download a sample from MalwareBazaar (encrypted zip)."""
    headers = {"Accept": "application/octet-stream"}
    if MB_API_KEY:
        headers["Auth-Key"] = MB_API_KEY
    
    try:
        with httpx.Client(timeout=60) as client:
            response = client.post(
                MB_API,
                data={"query": "get_file", "sha256_hash": sha256},
                headers=headers,
            )
            
            if response.status_code == 200 and len(response.content) > 0:
                # Save as encrypted zip (password: infected)
                zip_path = dest_dir / f"{sha256}.zip"
                with open(zip_path, "wb") as f:
                    f.write(response.content)
                return zip_path
                
    except Exception as e:
        print(f"    Download error: {e}")
    
    return None


def collect_malware_samples(manifest: Dict, max_samples: int = 50) -> int:
    """Collect malware samples from MalwareBazaar."""
    MALICIOUS_DIR.mkdir(parents=True, exist_ok=True)
    
    existing = get_existing_hashes(manifest)
    samples = fetch_malwarebazaar_samples(limit=200)
    
    # Filter out existing
    new_samples = [s for s in samples if s["sha256"] not in existing]
    print(f"  {len(new_samples)} new samples (excluding {len(samples) - len(new_samples)} duplicates)")
    
    downloaded = 0
    
    for sample in new_samples[:max_samples]:
        sha256 = sample["sha256"]
        print(f"  [{downloaded+1}/{max_samples}] Downloading {sha256[:16]}... ({sample['file_type']})", end=" ")
        
        zip_path = download_malware_sample(sha256, MALICIOUS_DIR)
        
        if zip_path and zip_path.exists():
            print("OK")
            
            # Add to manifest
            manifest["samples"].append({
                "sha256": sha256,
                "file_name": sample["file_name"],
                "file_type": sample["file_type"],
                "file_size": sample["file_size"],
                "label": "malicious",
                "source": "malwarebazaar",
                "tags": sample.get("tags", []),
                "signature": sample.get("signature"),
                "first_seen": sample.get("first_seen"),
                "local_path": str(zip_path.relative_to(EVAL_DIR)),
                "added": datetime.now().isoformat(),
            })
            downloaded += 1
        else:
            print("FAILED")
        
        # Rate limit
        time.sleep(0.5)
    
    return downloaded


def collect_clean_samples(manifest: Dict) -> int:
    """Collect clean samples from system directories."""
    CLEAN_DIR.mkdir(parents=True, exist_ok=True)
    
    existing = get_existing_hashes(manifest)
    collected = 0
    
    print("\n[Clean Samples] Collecting known-good files...")
    
    # Windows system files
    clean_sources = []
    
    if sys.platform == "win32":
        # Windows paths
        win_dir = Path(os.environ.get("WINDIR", "C:\\Windows"))
        clean_sources = [
            (win_dir / "System32" / "notepad.exe", "exe"),
            (win_dir / "System32" / "calc.exe", "exe"),
            (win_dir / "System32" / "mspaint.exe", "exe"),
            (win_dir / "System32" / "write.exe", "exe"),
            (win_dir / "System32" / "charmap.exe", "exe"),
            (win_dir / "System32" / "control.exe", "exe"),
            (win_dir / "System32" / "taskmgr.exe", "exe"),
            (win_dir / "System32" / "cmd.exe", "exe"),
            (win_dir / "System32" / "msiexec.exe", "exe"),
            (win_dir / "System32" / "reg.exe", "exe"),
        ]
    else:
        # Linux paths
        clean_sources = [
            (Path("/usr/bin/ls"), "elf"),
            (Path("/usr/bin/cat"), "elf"),
            (Path("/usr/bin/grep"), "elf"),
            (Path("/usr/bin/find"), "elf"),
            (Path("/usr/bin/head"), "elf"),
            (Path("/usr/bin/tail"), "elf"),
            (Path("/usr/bin/sort"), "elf"),
            (Path("/usr/bin/uniq"), "elf"),
            (Path("/usr/bin/wc"), "elf"),
            (Path("/usr/bin/diff"), "elf"),
        ]
    
    for src_path, file_type in clean_sources:
        if not src_path.exists():
            continue
            
        sha256 = compute_sha256(src_path)
        
        if sha256 in existing:
            print(f"  [SKIP] {src_path.name} (duplicate)")
            continue
        
        # Copy to clean dir
        dest_path = CLEAN_DIR / src_path.name
        try:
            shutil.copy2(src_path, dest_path)
            print(f"  [OK] {src_path.name} ({file_type})")
            
            manifest["samples"].append({
                "sha256": sha256,
                "file_name": src_path.name,
                "file_type": file_type,
                "file_size": src_path.stat().st_size,
                "label": "clean",
                "source": "system",
                "tags": ["system", "windows" if sys.platform == "win32" else "linux"],
                "local_path": str(dest_path.relative_to(EVAL_DIR)),
                "added": datetime.now().isoformat(),
            })
            collected += 1
            
        except Exception as e:
            print(f"  [FAIL] {src_path.name}: {e}")
    
    return collected


def print_summary(manifest: Dict):
    """Print summary statistics."""
    samples = manifest.get("samples", [])
    
    # Count by label
    malicious = [s for s in samples if s["label"] == "malicious"]
    clean = [s for s in samples if s["label"] == "clean"]
    
    # Count by type
    types = {}
    for s in samples:
        t = s.get("file_type", "unknown")
        types[t] = types.get(t, 0) + 1
    
    # Count by source
    sources = {}
    for s in samples:
        src = s.get("source", "unknown")
        sources[src] = sources.get(src, 0) + 1
    
    print("\n" + "=" * 60)
    print("SAMPLE COLLECTION SUMMARY")
    print("=" * 60)
    print(f"\nTotal samples: {len(samples)}")
    print(f"  Malicious: {len(malicious)}")
    print(f"  Clean: {len(clean)}")
    
    print(f"\nBy file type:")
    for t, count in sorted(types.items(), key=lambda x: -x[1]):
        print(f"  {t}: {count}")
    
    print(f"\nBy source:")
    for src, count in sorted(sources.items(), key=lambda x: -x[1]):
        print(f"  {src}: {count}")
    
    print(f"\nManifest: {MANIFEST_PATH}")
    print("=" * 60)


def main():
    print("=" * 60)
    print("SENTINEL SAMPLE COLLECTOR")
    print("=" * 60)
    
    # Ensure directories exist
    EVAL_DIR.mkdir(parents=True, exist_ok=True)
    
    # Load manifest
    manifest = load_manifest()
    print(f"\nLoaded manifest with {len(manifest.get('samples', []))} existing samples")
    
    # Collect malware samples
    malware_count = collect_malware_samples(manifest, max_samples=50)
    print(f"\n  Downloaded {malware_count} new malware samples")
    
    # Collect clean samples
    clean_count = collect_clean_samples(manifest)
    print(f"\n  Collected {clean_count} new clean samples")
    
    # Save manifest
    save_manifest(manifest)
    
    # Print summary
    print_summary(manifest)
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
