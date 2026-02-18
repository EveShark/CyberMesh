"""Examine EMBER2024 dataset structure."""
import json
import os
from pathlib import Path

# Examine PDF test set (has both benign and malicious)
pdf_dir = Path(r'B:\sentinel\data\evaluation_datasets\ember2024_data\PDF_test')

print('=== PDF Test Set ===')
benign = 0
malicious = 0
for jsonl_file in pdf_dir.glob('*.jsonl'):
    with open(jsonl_file, 'r') as f:
        for line in f:
            data = json.loads(line)
            if data.get('label') == 0:
                benign += 1
            else:
                malicious += 1

print(f'Benign PDFs: {benign}')
print(f'Malicious PDFs: {malicious}')
print(f'Total: {benign + malicious}')

print('\n=== Challenge Set ===')
# Read a sample from the challenge set
challenge_dir = Path(r'B:\sentinel\data\evaluation_datasets\ember2024_data\challenge')

# Count total samples
total_samples = 0
file_types = {}

for jsonl_file in challenge_dir.glob('*.jsonl'):
    with open(jsonl_file, 'r') as f:
        for line in f:
            total_samples += 1
            data = json.loads(line)
            ft = data.get('file_type', 'unknown')
            file_types[ft] = file_types.get(ft, 0) + 1

print(f'Total challenge samples: {total_samples}')
print(f'File types: {file_types}')

# Read first sample
sample_file = list(challenge_dir.glob('*.jsonl'))[0]
with open(sample_file, 'r') as f:
    first_line = f.readline()
    data = json.loads(first_line)
    
print(f'\nSample entry keys: {list(data.keys())}')
print(f'SHA256: {data.get("sha256", "N/A")[:32]}...')
print(f'File type: {data.get("file_type", "N/A")}')
print(f'Label: {data.get("label", "N/A")}')
print(f'Signature: {data.get("signature", "N/A")}')

# Check features
if 'features' in data:
    features = data['features']
    print(f'\nFeature sections: {list(features.keys())}')
