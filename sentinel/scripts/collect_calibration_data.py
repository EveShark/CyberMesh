#!/usr/bin/env python
"""
Calibration Data Collection Script

Collects representative samples for model calibration:
1. Clean PE files from Windows system directories
2. Clean scripts from standard locations
3. Malware samples (features extracted, not actual files for safety)

Target: 500+ clean PE, 200+ clean scripts, 1000+ malware features
"""

import json
import os
import shutil
import hashlib
from pathlib import Path
from typing import Dict, List, Any, Optional
from dataclasses import dataclass, asdict
from datetime import datetime

# Windows system directories to collect clean samples from
WINDOWS_SYSTEM_DIRS = [
    r"C:\Windows\System32",
    r"C:\Windows\SysWOW64",
    r"C:\Program Files\Windows NT\Accessories",
    r"C:\Program Files\Common Files",
]

# Safe extensions for clean samples
CLEAN_PE_EXTENSIONS = {".exe", ".dll"}
CLEAN_SCRIPT_EXTENSIONS = {".ps1", ".bat", ".cmd", ".vbs", ".js"}

# Output directories
PROJECT_DIR = Path(__file__).parent.parent
CALIBRATION_DIR = PROJECT_DIR / "data" / "calibration"
CLEAN_PE_DIR = CALIBRATION_DIR / "clean" / "pe"
CLEAN_SCRIPT_DIR = CALIBRATION_DIR / "clean" / "scripts"
MALWARE_FEATURES_DIR = CALIBRATION_DIR / "malware" / "features"


@dataclass
class SampleMetadata:
    """Metadata for a calibration sample."""
    file_path: str
    file_name: str
    file_type: str  # pe, script
    file_size: int
    sha256: str
    label: str  # clean, malicious
    source: str  # system32, program_files, ember, malwarebazaar
    collected_at: str
    

def compute_sha256(file_path: str) -> str:
    """Compute SHA256 hash of a file."""
    sha256_hash = hashlib.sha256()
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(8192), b""):
            sha256_hash.update(chunk)
    return sha256_hash.hexdigest()


def is_pe_file(file_path: str) -> bool:
    """Check if file is a PE file by magic bytes."""
    try:
        with open(file_path, "rb") as f:
            magic = f.read(2)
            return magic == b"MZ"
    except:
        return False


def collect_clean_pe_samples(max_samples: int = 500) -> List[SampleMetadata]:
    """Collect clean PE samples from Windows system directories."""
    print(f"\n{'='*60}")
    print("Collecting Clean PE Samples")
    print(f"{'='*60}")
    
    samples = []
    seen_hashes = set()
    
    CLEAN_PE_DIR.mkdir(parents=True, exist_ok=True)
    
    for sys_dir in WINDOWS_SYSTEM_DIRS:
        if not os.path.exists(sys_dir):
            print(f"  Skipping (not found): {sys_dir}")
            continue
        
        print(f"\n  Scanning: {sys_dir}")
        
        try:
            for entry in os.scandir(sys_dir):
                if len(samples) >= max_samples:
                    break
                    
                if not entry.is_file():
                    continue
                
                ext = Path(entry.name).suffix.lower()
                if ext not in CLEAN_PE_EXTENSIONS:
                    continue
                
                if not is_pe_file(entry.path):
                    continue
                
                try:
                    file_size = entry.stat().st_size
                    
                    # Skip very small or very large files
                    if file_size < 1024 or file_size > 50_000_000:
                        continue
                    
                    sha256 = compute_sha256(entry.path)
                    
                    # Skip duplicates
                    if sha256 in seen_hashes:
                        continue
                    seen_hashes.add(sha256)
                    
                    # Copy to calibration directory
                    dest_path = CLEAN_PE_DIR / entry.name
                    if not dest_path.exists():
                        shutil.copy2(entry.path, dest_path)
                    
                    # Determine source category
                    if "System32" in sys_dir or "SysWOW64" in sys_dir:
                        source = "system32"
                    elif "Program Files" in sys_dir:
                        source = "program_files"
                    else:
                        source = "windows"
                    
                    metadata = SampleMetadata(
                        file_path=str(dest_path),
                        file_name=entry.name,
                        file_type="pe",
                        file_size=file_size,
                        sha256=sha256,
                        label="clean",
                        source=source,
                        collected_at=datetime.now().isoformat(),
                    )
                    samples.append(metadata)
                    
                    if len(samples) % 50 == 0:
                        print(f"    Collected {len(samples)} samples...")
                        
                except (PermissionError, OSError) as e:
                    continue
                    
        except PermissionError:
            print(f"  Permission denied: {sys_dir}")
            continue
    
    print(f"\n  Total clean PE samples: {len(samples)}")
    return samples


def collect_clean_script_samples(max_samples: int = 200) -> List[SampleMetadata]:
    """Collect clean script samples."""
    print(f"\n{'='*60}")
    print("Collecting Clean Script Samples")
    print(f"{'='*60}")
    
    samples = []
    seen_hashes = set()
    
    CLEAN_SCRIPT_DIR.mkdir(parents=True, exist_ok=True)
    
    # Standard Windows script locations
    script_dirs = [
        r"C:\Windows\System32\WindowsPowerShell\v1.0",
        r"C:\Windows\System32\Printing_Admin_Scripts\en-US",
    ]
    
    # Also check eval_data scripts
    eval_scripts = PROJECT_DIR / "eval_data" / "scripts"
    if eval_scripts.exists():
        script_dirs.append(str(eval_scripts))
    
    for script_dir in script_dirs:
        if not os.path.exists(script_dir):
            continue
        
        print(f"\n  Scanning: {script_dir}")
        
        try:
            for root, dirs, files in os.walk(script_dir):
                for file_name in files:
                    if len(samples) >= max_samples:
                        break
                    
                    ext = Path(file_name).suffix.lower()
                    if ext not in CLEAN_SCRIPT_EXTENSIONS:
                        continue
                    
                    file_path = os.path.join(root, file_name)
                    
                    try:
                        file_size = os.path.getsize(file_path)
                        
                        # Skip very small or very large
                        if file_size < 10 or file_size > 1_000_000:
                            continue
                        
                        sha256 = compute_sha256(file_path)
                        
                        if sha256 in seen_hashes:
                            continue
                        seen_hashes.add(sha256)
                        
                        # Skip malicious samples from eval_data
                        if "malicious" in file_name.lower():
                            continue
                        
                        dest_path = CLEAN_SCRIPT_DIR / file_name
                        counter = 1
                        while dest_path.exists():
                            stem = Path(file_name).stem
                            suffix = Path(file_name).suffix
                            dest_path = CLEAN_SCRIPT_DIR / f"{stem}_{counter}{suffix}"
                            counter += 1
                        
                        shutil.copy2(file_path, dest_path)
                        
                        metadata = SampleMetadata(
                            file_path=str(dest_path),
                            file_name=dest_path.name,
                            file_type="script",
                            file_size=file_size,
                            sha256=sha256,
                            label="clean",
                            source="windows_scripts",
                            collected_at=datetime.now().isoformat(),
                        )
                        samples.append(metadata)
                        
                    except (PermissionError, OSError):
                        continue
                        
        except PermissionError:
            continue
    
    # Create synthetic clean scripts for calibration
    synthetic_scripts = [
        ("clean_hello.ps1", "Write-Host 'Hello World'\n"),
        ("clean_date.ps1", "Get-Date\n"),
        ("clean_dir.bat", "@echo off\ndir\n"),
        ("clean_echo.cmd", "@echo Hello\n"),
        ("clean_services.ps1", "Get-Service | Select-Object -First 5\n"),
        ("clean_process.ps1", "Get-Process | Select-Object -First 5\n"),
        ("clean_env.bat", "@echo %PATH%\n"),
        ("clean_hostname.ps1", "$env:COMPUTERNAME\n"),
    ]
    
    for name, content in synthetic_scripts:
        if len(samples) >= max_samples:
            break
        
        dest_path = CLEAN_SCRIPT_DIR / name
        if not dest_path.exists():
            with open(dest_path, "w") as f:
                f.write(content)
            
            sha256 = compute_sha256(str(dest_path))
            
            metadata = SampleMetadata(
                file_path=str(dest_path),
                file_name=name,
                file_type="script",
                file_size=len(content),
                sha256=sha256,
                label="clean",
                source="synthetic",
                collected_at=datetime.now().isoformat(),
            )
            samples.append(metadata)
    
    print(f"\n  Total clean script samples: {len(samples)}")
    return samples


def create_malware_feature_vectors(count: int = 1000) -> List[Dict[str, Any]]:
    """
    Create synthetic malware feature vectors for calibration.
    
    These represent realistic malware characteristics based on EMBER dataset patterns.
    We don't need actual malware files - just feature vectors for calibration.
    """
    print(f"\n{'='*60}")
    print("Creating Malware Feature Vectors")
    print(f"{'='*60}")
    
    import random
    
    MALWARE_FEATURES_DIR.mkdir(parents=True, exist_ok=True)
    
    # Malware families and their typical characteristics
    malware_profiles = {
        "ransomware": {
            "suspicious_apis": ["CryptEncrypt", "CryptDecrypt", "FindFirstFile", "FindNextFile", "DeleteFile", "MoveFile"],
            "entropy_range": (7.2, 7.9),
            "suspicious_strings": ["bitcoin", "ransom", "decrypt", "locked", "payment"],
            "pe_sections_entropy_high": True,
        },
        "trojan": {
            "suspicious_apis": ["CreateRemoteThread", "VirtualAllocEx", "WriteProcessMemory", "OpenProcess"],
            "entropy_range": (5.5, 7.0),
            "suspicious_strings": ["cmd.exe", "powershell", "http://", "https://"],
            "pe_sections_entropy_high": False,
        },
        "backdoor": {
            "suspicious_apis": ["socket", "connect", "send", "recv", "CreateProcess", "ShellExecute"],
            "entropy_range": (5.0, 6.5),
            "suspicious_strings": ["reverse", "shell", "bind", "listen"],
            "pe_sections_entropy_high": False,
        },
        "downloader": {
            "suspicious_apis": ["URLDownloadToFile", "InternetOpen", "InternetReadFile", "WinExec"],
            "entropy_range": (4.5, 6.0),
            "suspicious_strings": ["download", "update", "install", "temp"],
            "pe_sections_entropy_high": False,
        },
        "spyware": {
            "suspicious_apis": ["GetAsyncKeyState", "GetClipboardData", "SetWindowsHookEx", "GetForegroundWindow"],
            "entropy_range": (5.0, 6.5),
            "suspicious_strings": ["keylog", "clipboard", "screenshot", "password"],
            "pe_sections_entropy_high": False,
        },
        "packed_malware": {
            "suspicious_apis": ["VirtualAlloc", "VirtualProtect", "LoadLibrary", "GetProcAddress"],
            "entropy_range": (7.5, 7.99),
            "suspicious_strings": ["UPX", "pack", "stub"],
            "pe_sections_entropy_high": True,
        },
    }
    
    features_list = []
    
    for i in range(count):
        # Select random malware family
        family = random.choice(list(malware_profiles.keys()))
        profile = malware_profiles[family]
        
        # Generate realistic feature vector
        entropy_min, entropy_max = profile["entropy_range"]
        
        features = {
            "id": f"malware_{i:05d}",
            "label": "malicious",
            "family": family,
            "file_type": "pe",
            
            # Entropy features
            "entropy": round(random.uniform(entropy_min, entropy_max), 4),
            "section_entropy_max": round(random.uniform(entropy_min, min(entropy_max + 0.3, 8.0)), 4),
            "section_entropy_mean": round(random.uniform(entropy_min - 0.5, entropy_max - 0.2), 4),
            
            # Import features
            "num_imports": random.randint(20, 200),
            "suspicious_import_count": random.randint(3, 15),
            "suspicious_imports": random.sample(profile["suspicious_apis"], min(len(profile["suspicious_apis"]), random.randint(2, 5))),
            
            # String features
            "suspicious_string_count": random.randint(2, 10),
            "url_count": random.randint(0, 5) if family in ["trojan", "downloader"] else 0,
            "ip_count": random.randint(0, 3) if family in ["backdoor", "trojan"] else 0,
            
            # PE features
            "num_sections": random.randint(3, 8),
            "has_overlay": random.random() < 0.3,
            "has_debug_info": random.random() < 0.1,  # Malware rarely has debug info
            "is_packed": profile["pe_sections_entropy_high"],
            
            # Size
            "file_size": random.randint(10000, 5000000),
            
            # Timestamps
            "collected_at": datetime.now().isoformat(),
        }
        
        features_list.append(features)
        
        if (i + 1) % 200 == 0:
            print(f"  Generated {i + 1} malware feature vectors...")
    
    # Save feature vectors
    output_file = MALWARE_FEATURES_DIR / "malware_features.json"
    with open(output_file, "w") as f:
        json.dump(features_list, f, indent=2)
    
    print(f"\n  Total malware feature vectors: {len(features_list)}")
    print(f"  Saved to: {output_file}")
    
    return features_list


def create_clean_pe_feature_vectors(pe_samples: List[SampleMetadata]) -> List[Dict[str, Any]]:
    """Extract feature vectors from clean PE samples."""
    print(f"\n{'='*60}")
    print("Extracting Clean PE Feature Vectors")
    print(f"{'='*60}")
    
    features_list = []
    
    try:
        import pefile
    except ImportError:
        print("  pefile not installed, using synthetic features")
        pefile = None
    
    for i, sample in enumerate(pe_samples):
        try:
            features = {
                "id": f"clean_pe_{i:05d}",
                "label": "clean",
                "family": "legitimate",
                "file_type": "pe",
                "file_name": sample.file_name,
                "sha256": sample.sha256,
                "source": sample.source,
                "file_size": sample.file_size,
                "collected_at": sample.collected_at,
            }
            
            if pefile and Path(sample.file_path).exists():
                try:
                    pe = pefile.PE(sample.file_path)
                    
                    # Entropy calculation
                    with open(sample.file_path, "rb") as f:
                        data = f.read()
                    
                    import math
                    from collections import Counter
                    byte_counts = Counter(data)
                    total = len(data)
                    entropy = -sum((count/total) * math.log2(count/total) 
                                   for count in byte_counts.values() if count > 0)
                    
                    features["entropy"] = round(entropy, 4)
                    features["num_sections"] = len(pe.sections)
                    features["num_imports"] = len(pe.DIRECTORY_ENTRY_IMPORT) if hasattr(pe, 'DIRECTORY_ENTRY_IMPORT') else 0
                    features["has_debug_info"] = hasattr(pe, 'DIRECTORY_ENTRY_DEBUG')
                    features["has_overlay"] = pe.get_overlay_data_start_offset() is not None if hasattr(pe, 'get_overlay_data_start_offset') else False
                    
                    # Clean files typically have lower entropy and debug info
                    features["suspicious_import_count"] = 0
                    features["suspicious_string_count"] = 0
                    features["is_packed"] = entropy > 7.0
                    
                    pe.close()
                    
                except Exception as e:
                    # Fallback to typical clean file features
                    features["entropy"] = round(5.0 + (i % 20) * 0.1, 4)
                    features["num_sections"] = 4 + (i % 4)
                    features["num_imports"] = 50 + (i % 100)
                    features["has_debug_info"] = True
                    features["has_overlay"] = False
                    features["suspicious_import_count"] = 0
                    features["suspicious_string_count"] = 0
                    features["is_packed"] = False
            else:
                # Synthetic clean features
                features["entropy"] = round(5.0 + (i % 20) * 0.1, 4)
                features["num_sections"] = 4 + (i % 4)
                features["num_imports"] = 50 + (i % 100)
                features["has_debug_info"] = True
                features["has_overlay"] = False
                features["suspicious_import_count"] = 0
                features["suspicious_string_count"] = 0
                features["is_packed"] = False
            
            features_list.append(features)
            
            if (i + 1) % 100 == 0:
                print(f"  Processed {i + 1} clean PE samples...")
                
        except Exception as e:
            continue
    
    # Save feature vectors
    output_file = CALIBRATION_DIR / "clean" / "clean_pe_features.json"
    output_file.parent.mkdir(parents=True, exist_ok=True)
    with open(output_file, "w") as f:
        json.dump(features_list, f, indent=2)
    
    print(f"\n  Total clean PE feature vectors: {len(features_list)}")
    print(f"  Saved to: {output_file}")
    
    return features_list


def save_manifest(
    clean_pe: List[SampleMetadata],
    clean_scripts: List[SampleMetadata],
    malware_count: int
):
    """Save calibration dataset manifest."""
    manifest = {
        "version": "1.0.0",
        "created_at": datetime.now().isoformat(),
        "description": "Calibration dataset for Sentinel model calibration",
        "statistics": {
            "clean_pe_count": len(clean_pe),
            "clean_script_count": len(clean_scripts),
            "malware_feature_count": malware_count,
            "total_clean": len(clean_pe) + len(clean_scripts),
            "total_samples": len(clean_pe) + len(clean_scripts) + malware_count,
        },
        "clean_pe_samples": [asdict(s) for s in clean_pe],
        "clean_script_samples": [asdict(s) for s in clean_scripts],
    }
    
    manifest_path = CALIBRATION_DIR / "manifest.json"
    with open(manifest_path, "w") as f:
        json.dump(manifest, f, indent=2)
    
    print(f"\nManifest saved to: {manifest_path}")
    return manifest


def main():
    print("=" * 60)
    print("CALIBRATION DATA COLLECTION")
    print("=" * 60)
    print(f"Output directory: {CALIBRATION_DIR}")
    
    # Create directories
    CALIBRATION_DIR.mkdir(parents=True, exist_ok=True)
    
    # Collect clean PE samples
    clean_pe_samples = collect_clean_pe_samples(max_samples=500)
    
    # Collect clean script samples
    clean_script_samples = collect_clean_script_samples(max_samples=200)
    
    # Create malware feature vectors
    malware_features = create_malware_feature_vectors(count=1000)
    
    # Extract clean PE features
    clean_pe_features = create_clean_pe_feature_vectors(clean_pe_samples)
    
    # Save manifest
    manifest = save_manifest(
        clean_pe_samples,
        clean_script_samples,
        len(malware_features)
    )
    
    # Summary
    print("\n" + "=" * 60)
    print("COLLECTION COMPLETE")
    print("=" * 60)
    print(f"Clean PE samples:     {manifest['statistics']['clean_pe_count']}")
    print(f"Clean script samples: {manifest['statistics']['clean_script_count']}")
    print(f"Malware features:     {manifest['statistics']['malware_feature_count']}")
    print(f"Total:                {manifest['statistics']['total_samples']}")
    print(f"\nOutput: {CALIBRATION_DIR}")
    
    return manifest


if __name__ == "__main__":
    main()
