#!/usr/bin/env python3
"""
Process YARA rules from signature-base and yara-rules repos.
- Extract all .yar/.yara files
- Test compilation
- Deduplicate by rule name
- Organize by category
"""

import os
import re
import shutil
import hashlib
from pathlib import Path
from collections import defaultdict
from typing import Dict, List, Set, Tuple

try:
    import yara
except ImportError:
    print("ERROR: yara-python not installed. Run: pip install yara-python")
    exit(1)


# Category mappings based on filename patterns
CATEGORY_PATTERNS = {
    "malware": [
        r"^apt_", r"^crime_", r"^mal_", r"^MALW_", r"^RAT_", r"^RANSOM_",
        r"malware", r"backdoor", r"trojan", r"worm", r"virus", r"rootkit",
        r"^APT_", r"^hiddencobra", r"lazarus", r"ransomware", r"stealer",
    ],
    "exploit": [
        r"^expl_", r"^exploit_", r"^CVE-", r"^cve_", r"vuln", r"vulnerability",
    ],
    "packer": [
        r"^pack", r"packer", r"cryptor", r"obfusc", r"enigma", r"upx",
    ],
    "webshell": [
        r"webshell", r"^WShell", r"shell", r"^thor-webshell",
    ],
    "document": [
        r"^maldoc", r"^Maldoc", r"office", r"macro", r"^gen_doc", r"^gen_excel",
        r"^general_office", r"pdf", r"rtf", r"ole",
    ],
    "generic": [
        r"^gen_", r"^generic", r"^general_", r"^GEN_", r"suspicious", r"anomal",
    ],
}


def categorize_file(filename: str) -> str:
    """Determine category based on filename."""
    name_lower = filename.lower()
    
    for category, patterns in CATEGORY_PATTERNS.items():
        for pattern in patterns:
            if re.search(pattern, filename, re.IGNORECASE):
                return category
    
    # Check parent directory for hints
    return "generic"


def extract_rule_names(content: str) -> List[str]:
    """Extract rule names from YARA file content."""
    # Match: rule RuleName { or rule RuleName : tag1 tag2 {
    pattern = r'^\s*(?:private\s+)?rule\s+(\w+)'
    return re.findall(pattern, content, re.MULTILINE)


def test_compile(file_path: Path) -> Tuple[bool, str, List[str]]:
    """Test if a YARA file compiles. Returns (success, error_msg, rule_names)."""
    try:
        content = file_path.read_text(encoding='utf-8', errors='ignore')
        rule_names = extract_rule_names(content)
        
        # Skip files with external variables (require runtime values)
        if 'ext_vars' in file_path.name.lower():
            return False, "External variables required", rule_names
        
        # Try to compile
        yara.compile(filepath=str(file_path))
        return True, "", rule_names
        
    except yara.SyntaxError as e:
        return False, f"Syntax: {str(e)[:100]}", []
    except yara.Error as e:
        return False, f"YARA: {str(e)[:100]}", []
    except Exception as e:
        return False, f"Error: {str(e)[:100]}", []


def process_yara_repos(
    signature_base: Path,
    yara_rules: Path,
    output_dir: Path,
) -> Dict:
    """Process both repos and organize rules."""
    
    stats = {
        "total_files": 0,
        "compiled_ok": 0,
        "compile_errors": [],
        "rules_by_category": defaultdict(int),
        "deduplicated": 0,
        "total_rules": 0,
    }
    
    # Track seen rule names for deduplication
    seen_rules: Dict[str, Path] = {}  # rule_name -> source_file
    
    # Create output directories
    categories = ["malware", "exploit", "packer", "webshell", "document", "generic"]
    for cat in categories:
        (output_dir / cat).mkdir(parents=True, exist_ok=True)
    
    # Collect all .yar/.yara files
    all_files = []
    
    for repo_path in [signature_base, yara_rules]:
        if not repo_path.exists():
            print(f"WARNING: {repo_path} not found")
            continue
            
        for ext in ["*.yar", "*.yara"]:
            for f in repo_path.rglob(ext):
                # Skip index files and deprecated
                if "index" in f.name.lower() or "deprecated" in str(f).lower():
                    continue
                all_files.append(f)
    
    print(f"Found {len(all_files)} YARA files to process")
    stats["total_files"] = len(all_files)
    
    # Process each file
    for i, file_path in enumerate(all_files):
        if (i + 1) % 50 == 0:
            print(f"Processing {i + 1}/{len(all_files)}...")
        
        success, error, rule_names = test_compile(file_path)
        
        if not success:
            stats["compile_errors"].append({
                "file": str(file_path.name),
                "error": error,
            })
            continue
        
        stats["compiled_ok"] += 1
        
        # Determine category
        category = categorize_file(file_path.name)
        
        # Check for duplicate rules
        new_rules = []
        for rule in rule_names:
            if rule in seen_rules:
                stats["deduplicated"] += 1
                # Prefer signature-base over yara-rules
                if "signature-base" in str(file_path) and "yara-rules" in str(seen_rules[rule]):
                    seen_rules[rule] = file_path
            else:
                seen_rules[rule] = file_path
                new_rules.append(rule)
        
        if new_rules:
            # Copy file to output directory
            dest = output_dir / category / file_path.name
            
            # Handle filename collisions
            if dest.exists():
                base = dest.stem
                suffix = dest.suffix
                counter = 1
                while dest.exists():
                    dest = output_dir / category / f"{base}_{counter}{suffix}"
                    counter += 1
            
            shutil.copy2(file_path, dest)
            stats["rules_by_category"][category] += len(new_rules)
            stats["total_rules"] += len(new_rules)
    
    return stats


def main():
    base_dir = Path(__file__).parent.parent
    
    signature_base = base_dir / "data" / "signature-base" / "yara"
    yara_rules = base_dir / "data" / "yara-rules"
    output_dir = base_dir / "rules"
    
    # Also check root of signature-base for stray .yar files
    signature_base_root = base_dir / "data" / "signature-base"
    
    print("=" * 60)
    print("YARA Rules Processor")
    print("=" * 60)
    print(f"Signature-base: {signature_base}")
    print(f"Yara-rules: {yara_rules}")
    print(f"Output: {output_dir}")
    print()
    
    # Clear existing rules
    if output_dir.exists():
        shutil.rmtree(output_dir)
    output_dir.mkdir(parents=True)
    
    # Process using root paths to catch all files
    stats = process_yara_repos(
        signature_base_root,
        yara_rules,
        output_dir,
    )
    
    # Print summary
    print()
    print("=" * 60)
    print("SUMMARY")
    print("=" * 60)
    print(f"Total files processed: {stats['total_files']}")
    print(f"Successfully compiled: {stats['compiled_ok']}")
    print(f"Compilation errors: {len(stats['compile_errors'])}")
    print(f"Deduplicated rules: {stats['deduplicated']}")
    print(f"Total unique rules: {stats['total_rules']}")
    print()
    print("Rules by category:")
    for cat, count in sorted(stats["rules_by_category"].items()):
        print(f"  {cat}: {count}")
    
    if stats["compile_errors"]:
        print()
        print(f"First 10 compile errors:")
        for err in stats["compile_errors"][:10]:
            print(f"  - {err['file']}: {err['error']}")
    
    # Write stats to file
    stats_file = output_dir / "processing_stats.txt"
    with open(stats_file, "w") as f:
        f.write("YARA Rules Processing Statistics\n")
        f.write("=" * 40 + "\n")
        f.write(f"Total files: {stats['total_files']}\n")
        f.write(f"Compiled OK: {stats['compiled_ok']}\n")
        f.write(f"Errors: {len(stats['compile_errors'])}\n")
        f.write(f"Deduplicated: {stats['deduplicated']}\n")
        f.write(f"Total rules: {stats['total_rules']}\n")
        f.write("\nBy category:\n")
        for cat, count in sorted(stats["rules_by_category"].items()):
            f.write(f"  {cat}: {count}\n")
        f.write("\nCompile errors:\n")
        for err in stats["compile_errors"]:
            f.write(f"  {err['file']}: {err['error']}\n")
    
    print()
    print(f"Stats written to: {stats_file}")
    print("Done!")


if __name__ == "__main__":
    main()
