"""
Populate Sentinel's hash database with known malware hashes.

Sources:
1. EMBER2024 Challenge Set - 6,315 evasive malware hashes
2. MalwareBazaar Recent - ~650 recent malware hashes
"""
import json
import csv
from pathlib import Path
from datetime import datetime
from typing import Dict, List, Set

def load_ember_hashes(challenge_dir: Path) -> Dict[str, Dict]:
    """Load malware hashes from EMBER2024 challenge set."""
    hashes = {}
    
    print(f"Loading EMBER2024 hashes from {challenge_dir}...")
    
    for jsonl_file in sorted(challenge_dir.glob('*.jsonl')):
        with open(jsonl_file, 'r') as f:
            for line in f:
                data = json.loads(line)
                sha256 = data.get('sha256', '').lower()
                if sha256:
                    hashes[sha256] = {
                        'family': data.get('family') or 'Unknown',
                        'source': 'EMBER2024',
                        'file_type': data.get('file_type', 'Unknown'),
                        'md5': data.get('md5', ''),
                        'sha1': data.get('sha1', ''),
                        'behaviors': data.get('behavior', []),
                        'first_seen': data.get('first_submission_date', ''),
                        'severity': 'high',  # Challenge set = evasive malware
                    }
    
    print(f"  Loaded {len(hashes)} EMBER2024 hashes")
    return hashes


def load_malwarebazaar_hashes(csv_file: Path) -> Dict[str, Dict]:
    """Load malware hashes from MalwareBazaar CSV export."""
    hashes = {}
    
    print(f"Loading MalwareBazaar hashes from {csv_file}...")
    
    with open(csv_file, 'rb') as f:
        content = f.read().decode('utf-8', errors='ignore')
    
    lines = content.strip().split('\n')
    
    # Skip comment lines and find header
    data_lines = []
    header = None
    for line in lines:
        if line.startswith('#'):
            # Parse header from comment
            if 'first_seen_utc' in line:
                # Header line
                header = line.lstrip('# ').split(',')
            continue
        if line.strip():
            data_lines.append(line)
    
    if not header:
        # Default header based on MalwareBazaar format
        header = ['first_seen_utc', 'sha256_hash', 'md5_hash', 'sha1_hash', 
                  'reporter', 'file_name', 'file_type_guess', 'mime_type',
                  'signature', 'clamav', 'vtpercent', 'imphash', 'ssdeep', 'tlsh']
    
    for line in data_lines:
        try:
            # Parse CSV line
            parts = line.split(',')
            if len(parts) >= 9:
                sha256 = parts[1].strip().strip('"').lower()
                if sha256 and len(sha256) == 64:
                    hashes[sha256] = {
                        'family': parts[8].strip().strip('"') or 'Unknown',  # signature
                        'source': 'MalwareBazaar',
                        'file_type': parts[6].strip().strip('"') if len(parts) > 6 else 'Unknown',
                        'md5': parts[2].strip().strip('"') if len(parts) > 2 else '',
                        'sha1': parts[3].strip().strip('"') if len(parts) > 3 else '',
                        'first_seen': parts[0].strip().strip('"') if parts[0] else '',
                        'severity': 'high',
                    }
        except Exception as e:
            continue  # Skip malformed lines
    
    print(f"  Loaded {len(hashes)} MalwareBazaar hashes")
    return hashes


def merge_hashes(sources: List[Dict[str, Dict]]) -> Dict[str, Dict]:
    """Merge multiple hash sources, preferring more detailed info."""
    merged = {}
    
    for source in sources:
        for sha256, info in source.items():
            if sha256 not in merged:
                merged[sha256] = info
            else:
                # Merge info, keeping non-empty values
                existing = merged[sha256]
                for key, value in info.items():
                    if value and (key not in existing or not existing[key]):
                        existing[key] = value
                # Track multiple sources
                if 'sources' not in existing:
                    existing['sources'] = [existing.get('source', 'Unknown')]
                if info.get('source') not in existing['sources']:
                    existing['sources'].append(info.get('source'))
    
    return merged


def create_hash_database(output_path: Path, hashes: Dict[str, Dict]) -> None:
    """Create the hash database file."""
    
    # Gather statistics
    families = {}
    sources = {}
    file_types = {}
    
    for sha256, info in hashes.items():
        family = info.get('family', 'Unknown')
        families[family] = families.get(family, 0) + 1
        
        source = info.get('source', 'Unknown')
        sources[source] = sources.get(source, 0) + 1
        
        ftype = info.get('file_type', 'Unknown')
        file_types[ftype] = file_types.get(ftype, 0) + 1
    
    database = {
        'version': '2.0',
        'created': datetime.now().isoformat(),
        'statistics': {
            'total_hashes': len(hashes),
            'by_source': sources,
            'by_family': dict(sorted(families.items(), key=lambda x: -x[1])[:50]),
            'by_file_type': file_types,
        },
        'hashes': hashes,
    }
    
    output_path.parent.mkdir(parents=True, exist_ok=True)
    
    with open(output_path, 'w') as f:
        json.dump(database, f, indent=2)
    
    print(f"\nDatabase saved to: {output_path}")
    print(f"Total hashes: {len(hashes)}")


def main():
    print("=" * 70)
    print("POPULATING SENTINEL HASH DATABASE")
    print("=" * 70)
    
    # Paths
    ember_dir = Path(r'B:\sentinel\data\evaluation_datasets\ember2024_data\challenge')
    mb_file = Path(r'B:\sentinel\data\evaluation_datasets\malwarebazaar\recent_hashes.csv')
    output_file = Path(r'B:\sentinel\data\malware_hashes.json')
    
    all_sources = []
    
    # Load EMBER2024 hashes
    if ember_dir.exists():
        ember_hashes = load_ember_hashes(ember_dir)
        all_sources.append(ember_hashes)
    else:
        print(f"Warning: EMBER directory not found: {ember_dir}")
    
    # Load MalwareBazaar hashes
    if mb_file.exists():
        mb_hashes = load_malwarebazaar_hashes(mb_file)
        all_sources.append(mb_hashes)
    else:
        print(f"Warning: MalwareBazaar file not found: {mb_file}")
    
    if not all_sources:
        print("Error: No hash sources found!")
        return
    
    # Merge all sources
    print("\nMerging hash databases...")
    merged = merge_hashes(all_sources)
    
    # Create output database
    create_hash_database(output_file, merged)
    
    # Show summary
    print("\n" + "=" * 70)
    print("SUMMARY")
    print("=" * 70)
    
    # Count by source
    sources = {}
    for info in merged.values():
        src = info.get('source', 'Unknown')
        sources[src] = sources.get(src, 0) + 1
    
    print("\nHashes by source:")
    for src, count in sorted(sources.items(), key=lambda x: -x[1]):
        print(f"  {src}: {count:,}")
    
    # Top families
    families = {}
    for info in merged.values():
        fam = info.get('family', 'Unknown')
        if fam and fam != 'Unknown':
            families[fam] = families.get(fam, 0) + 1
    
    print("\nTop malware families:")
    for fam, count in sorted(families.items(), key=lambda x: -x[1])[:15]:
        print(f"  {fam}: {count}")
    
    print(f"\n✓ Database ready at: {output_file}")
    print(f"✓ Total malware hashes: {len(merged):,}")


if __name__ == '__main__':
    main()
