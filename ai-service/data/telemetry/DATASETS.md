# Dataset Download Instructions

## CIC-DDoS2019 (Network Traffic)

**Source:** Canadian Institute for Cybersecurity  
**URL:** https://www.unb.ca/cic/datasets/ddos-2019.html

**What to download:**
- CSV files with flow-based features (NetFlow format)
- Benign traffic samples
- DDoS attack samples (SYN flood, UDP flood, etc.)

**Place in:**
```
data/telemetry/flows/benign/*.csv
data/telemetry/flows/ddos/*.csv
```

**Sample structure (CSV columns):**
- Source IP, Dest IP, Source Port, Dest Port
- Protocol, Flow Duration
- Total Fwd Packets, Total Bwd Packets
- Flow Bytes/s, Flow Packets/s
- SYN Flag Count, ACK Flag Count
- Avg Packet Size, Flow IAT Mean/Std/Max/Min

**Note:** Start with small sample (1000 benign + 1000 attack flows) for testing.

---

## EMBER (Malware Binaries)

**Source:** Endgame Malware BEnchmark for Research  
**URL:** https://github.com/elastic/ember

**What to download:**
- EMBER 2018 dataset (smaller, 1.1M samples)
- Pre-extracted features (ember_features.jsonl) OR
- Raw PE files for custom extraction

**Place in:**
```
data/telemetry/files/benign/*.exe (or .json if using pre-extracted)
data/telemetry/files/malware/*.exe (or .json)
```

**Feature format (if using pre-extracted):**
- General: size, vsize, has_debug, has_relocations, has_resources, etc.
- Header: machine type, characteristics, dll_characteristics
- Imports: library names, function names (hashed/vectorized)
- Exports: count, names
- Section: names, sizes, entropy, virtual size
- Byte histogram: 256-dimensional histogram of byte values
- String information: string lengths, printable/entropy stats

**Note:** Start with 1000 benign + 1000 malware samples for testing.

---

## Alternative: Bot-IoT / UNSW-NB15 (Network Traffic)

If CIC-DDoS2019 unavailable:

**Bot-IoT:**
- https://research.unsw.edu.au/projects/bot-iot-dataset
- IoT-specific DDoS/DoS attacks

**UNSW-NB15:**
- https://research.unsw.edu.au/projects/unsw-nb15-dataset
- General network intrusion detection

---

## Quick Start (Synthetic for Testing)

If real datasets not available yet, create synthetic samples:

```python
# Run this to generate test data
python -m src.ml.test_data_generator
```

This will create:
- 100 benign network flows (JSON)
- 100 DDoS attack flows (JSON)
- 50 benign PE metadata (JSON)
- 50 malware PE metadata (JSON)

**WARNING:** Synthetic data for pipeline testing ONLY. Train on real datasets!

---

## Dataset Preparation Script

```bash
# After downloading, prepare datasets
cd ai-service
python training/prepare_datasets.py --cic-ddos /path/to/cic-ddos2019 --ember /path/to/ember
```

This will:
1. Parse CSV/JSON files
2. Normalize to common format
3. Split train/validation/test (70/15/15)
4. Save to data/telemetry/
