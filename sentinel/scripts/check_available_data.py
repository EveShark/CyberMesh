"""Check what malware data we have available."""
import json
from pathlib import Path

# Check EMBER challenge set
challenge_dir = Path(r'B:\sentinel\data\evaluation_datasets\ember2024_data\challenge')
sample_file = list(challenge_dir.glob('*.jsonl'))[0]

print('=' * 70)
print('AVAILABLE MALWARE DATA')
print('=' * 70)

print('\n[1] EMBER Challenge Set (Real Malware Metadata):')
families = {}
behaviors = set()

with open(sample_file) as f:
    for i, line in enumerate(f):
        data = json.loads(line)
        family = data.get('family', 'unknown')
        families[family] = families.get(family, 0) + 1
        
        for b in data.get('behavior', []):
            behaviors.add(b)
        
        if i < 3:
            print(f'  Sample {i+1}:')
            print(f'    SHA256: {data.get("sha256", "N/A")[:40]}...')
            print(f'    Family: {family}')
            print(f'    File type: {data.get("file_type", "N/A")}')

print(f'\n  Total samples: 6,315')
print(f'  Unique families: {len(families)}')
print(f'  Top families: {sorted(families.items(), key=lambda x: -x[1])[:10]}')
print(f'  Behaviors detected: {len(behaviors)}')

# Check MalwareBazaar hashes
mb_file = Path(r'B:\sentinel\data\evaluation_datasets\malwarebazaar\recent_hashes.csv')
if mb_file.exists():
    print('\n[2] MalwareBazaar Recent Hashes:')
    with open(mb_file, 'rb') as f:
        content = f.read().decode('utf-8', errors='ignore')
        lines = [l for l in content.split('\n') if l and not l.startswith('#')]
        print(f'  Total entries: {len(lines)}')

# What we can do
print('\n' + '=' * 70)
print('WHAT WE CAN DO WITH THIS DATA')
print('=' * 70)
print('''
1. HASH LOOKUP TEST:
   - Add EMBER malware hashes to our threat intel database
   - Test that Sentinel correctly flags known malware by hash
   - This tests our threat_intel integration

2. YARA RULE VALIDATION:
   - EMBER has malware families (emotet, qakbot, etc.)
   - Check if our YARA rules cover these families
   - Identify gaps in YARA coverage

3. BEHAVIOR ANALYSIS:
   - EMBER includes behavior tags (ransomware, backdoor, etc.)
   - Validate our detection aligns with expected behaviors

4. CANNOT DO (need actual files):
   - Test PE ML model on real binaries
   - Test full pipeline on actual malware
   - Measure true detection rate
''')
