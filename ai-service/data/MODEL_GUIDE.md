# CyberMesh ML Pipeline - Complete Guide

## Overview
Enterprise-grade machine learning pipeline for network security threat detection with PostgreSQL-based data management. Trained on 20M+ network flows and 5M+ malware samples.

---

## Table of Contents
1. [Models](#models)
2. [Datasets](#datasets)
3. [PostgreSQL Setup](#postgresql-setup)
4. [Data Ingestion Pipeline](#data-ingestion-pipeline)
5. [Model Training](#model-training)
6. [Testing & Validation](#testing--validation)
7. [Production Deployment](#production-deployment)
8. [Database Management](#database-management)

---

## Models

### 1. DDoS Detection Model
**File:** `training/models/ddos_binary_detector_v2.pkl`
- **Algorithm:** LightGBM Binary Classifier
- **Training Data:** 20.4M network flows (62% benign, 38% attack)
- **Features:** 79 network flow features
- **Performance:** 99.98% accuracy, AUC 1.0
- **Labels:** 
  - `Benign` = Normal traffic
  - `ddos` = DDoS attack traffic
- **Use Case:** Real-time DDoS attack detection

### 2. Network Anomaly Detector
**File:** `training/models/network_anomaly_detector.pkl`
- **Algorithm:** Isolation Forest (Unsupervised)
- **Training Data:** 1M benign network flows
- **Features:** 79 network flow features
- **Purpose:** Detect zero-day attacks and unknown patterns
- **Threshold:** 0.072911 (5th percentile)
- **Use Case:** Complement supervised DDoS detector for novel attacks

### 3. Malware Detection Models (5 Models)

#### a) API Behavioral Detector
**File:** `training/models/api_malware_detector.pkl`
- **Training Data:** 1.55M API call sequences
- **Performance:** 88.6% accuracy
- **Features:** 214 API behavioral patterns
- **Labels:** `malware` / `benign`

#### b) Android Malware Detector
**File:** `training/models/android_malware_detector.pkl`
- **Training Data:** 179K Android apps
- **Performance:** 93.9% accuracy
- **Features:** Permission patterns, intents, activities

#### c) PE Imports Detector
**File:** `training/models/pe_imports_detector.pkl`
- **Training Data:** 47.5K Windows PE files
- **Performance:** 96.3% accuracy
- **Features:** Windows API imports analysis

#### d) PE Sections Classifier
**File:** `training/models/pe_sections_classifier.pkl`
- **Training Data:** 18.5K PE files
- **Performance:** 39.3% accuracy (multi-class: Trojan, Worm, Virus, Backdoor, Benign)
- **Note:** Multi-class problem, needs more training rounds

#### e) Network Malware Detector
**File:** `training/models/network_malware_detector.pkl`
- **Training Data:** 1.1M network flows
- **Performance:** 94.9% accuracy
- **Features:** Network traffic patterns from malware C&C

---

## Datasets

### DDoS Dataset Sources
1. **Final Dataset (Original):** 12.8M rows, 50/50 balanced (lab data)
2. **Unbalanced Dataset:** 7.6M rows, 83/17 realistic distribution
3. **Merged Dataset:** 20.4M rows, 62/38 realistic (CURRENT)

**84 Network Features:**
- Flow metrics: duration, bytes, packets (forward/backward)
- Timing: IAT (Inter-Arrival Time) statistics
- TCP flags: SYN, ACK, FIN, RST, PSH, URG
- Packet sizes: min, max, mean, std
- Window sizes, header lengths, flow rates

### Malware Dataset Sources
1. **MalwareBazaar:** 870K samples
2. **API Analysis Dataset:** 1.55M sequences
3. **PE Headers Dataset:** 161K Windows executables
4. **Android Dataset:** 179K apps
5. **Network Traffic Dataset:** 1.1M malicious flows

### Labels & Encoding
- **DDoS:** `Benign` / `ddos` (string labels converted to 0/1)
- **Malware:** `malware` / `benign` (0/1)
- **PE Multi-class:** `Trojan`, `Worm`, `Virus`, `Backdoor`, `Benign` (0-4)

---

## PostgreSQL Setup

### Installation (Windows)
```bash
# Download PostgreSQL 18 from official website
# Install with default settings
# Default port: 5432
# Default user: postgres
# Password: postgres (used in this project)
```

### Database Configuration
**Database Name:** `cybermesh`

**Connection Details:**
```python
DB_CONFIG = {
    'host': 'localhost',
    'database': 'cybermesh',
    'user': 'postgres',
    'password': 'postgres',
    'port': 5432
}
```

### Memory Configuration (postgresql.conf)
```ini
shared_buffers = 512MB          # 25% of RAM
work_mem = 128MB                # For large sorts/joins
maintenance_work_mem = 256MB    # For VACUUM, CREATE INDEX
effective_cache_size = 2GB      # 50% of RAM
max_connections = 100
```

### Schema Structure
```
cybermesh/
├── curated/                    # Clean, ready-to-train data
│   └── combined_ddos_full      # 20.4M rows - Current training data
├── public/                     # Default schema (empty)
└── web_attacks_raw/            # Web attack data (150 MB, future use)
```

---

## Data Ingestion Pipeline

### Step 1: Raw Data Preparation
Place CSV files in organized folders:
```
CyberMesh Datasets/
├── CMG Dataset/
│   ├── DDOS Detection/
│   │   └── final_dataset.csv
│   └── unbalanced_20_80_dataset.csv
└── ML Malware Documentation/
    ├── 1550k_malware_analysis/
    ├── android_dataset/
    └── pe_analysis/
```

### Step 2: Ingestion Script
**For DDoS data:**
```python
import psycopg2
import pandas as pd

# Connect to PostgreSQL
conn = psycopg2.connect(**DB_CONFIG)

# Read CSV
df = pd.read_csv("path/to/dataset.csv")

# Data type conversion (all columns stored as TEXT initially)
for col in df.columns:
    if col not in ['label', 'src_ip', 'dst_ip', 'flow_id']:
        df[col] = pd.to_numeric(df[col], errors='coerce')

# Handle NaN/INF
df.replace([np.inf, -np.inf], np.nan, inplace=True)
df.fillna(0, inplace=True)

# Insert into PostgreSQL
df.to_sql('table_name', conn, schema='curated', if_exists='append', index=False)
```

### Step 3: Data Validation
```sql
-- Check row counts
SELECT COUNT(*) FROM curated.combined_ddos_full;

-- Check label distribution
SELECT label, COUNT(*), 
       ROUND(COUNT(*) * 100.0 / SUM(COUNT(*)) OVER(), 2) as percentage
FROM curated.combined_ddos_full
GROUP BY label;

-- Check for NULL values
SELECT COUNT(*) - COUNT(protocol) as null_count
FROM curated.combined_ddos_full;
```

---

## Model Training

### DDoS Training (Incremental LightGBM)
```bash
cd training
python train_ddos_merged_lgb.py
```

**Key Parameters:**
- Batch size: 25,000 rows
- Total batches: 817
- Checkpoints: Every 10 batches
- Training time: ~8 hours (with interruptions)
- Class weight: 1.63 (handles 62/38 imbalance)

**Checkpoints:**
Saved in `training/checkpoints_merged_lgb/`
- Automatic resume from last checkpoint
- Can restart training anytime without data loss

### Malware Training
```bash
python train_malware_master.py
```
Trains all 5 malware models sequentially (~2 hours total)

### Anomaly Training
```bash
python train_network_anomaly.py  # ~15 minutes
```

---

## Testing & Validation

### 1. Load Model
```python
import joblib
import pandas as pd
import numpy as np

# Load model
model = joblib.load('training/models/ddos_binary_detector_v2.pkl')

# Load test data from PostgreSQL
query = "SELECT * FROM curated.combined_ddos_full LIMIT 1000"
df = pd.read_sql(query, conn)

# Prepare features
X = df.drop(['label', 'flow_id', 'src_ip', 'dst_ip', 'timestamp'], axis=1)
y = df['label'].map({'Benign': 0, 'ddos': 1})

# Convert to numeric
for col in X.columns:
    X[col] = pd.to_numeric(X[col], errors='coerce')
X.fillna(0, inplace=True)

# Predict
predictions = model.predict(X)
accuracy = (predictions == y).mean()
print(f"Accuracy: {accuracy:.4f}")
```

### 2. Test on PostgreSQL Data Directly
```python
# Test on specific time window
test_query = """
SELECT * FROM curated.combined_ddos_full 
WHERE label = 'ddos' 
LIMIT 10000
"""

# Test on benign traffic
benign_query = """
SELECT * FROM curated.combined_ddos_full 
WHERE label = 'Benign' 
LIMIT 10000
"""
```

### 3. Validate Against Holdout Set
```python
from sklearn.metrics import classification_report, confusion_matrix, roc_auc_score

# Get predictions
y_pred = model.predict(X_test)
y_proba = model.predict_proba(X_test)[:, 1]

# Metrics
print(classification_report(y_test, y_pred))
print(f"AUC: {roc_auc_score(y_test, y_proba):.4f}")
print(f"Confusion Matrix:\n{confusion_matrix(y_test, y_pred)}")
```

---

## Production Deployment

### Architecture
```
Network Traffic → Feature Extraction → Model Inference → Alert System
                                              ↓
                                    PostgreSQL Logging
```

### Real-Time Inference Pipeline
```python
import joblib
import numpy as np
from collections import deque

# Load all models
ddos_model = joblib.load('models/ddos_binary_detector_v2.pkl')
anomaly_model = joblib.load('models/network_anomaly_detector.pkl')

def extract_features(packet_flow):
    """Extract 79 features from network flow"""
    features = {
        'src_port': packet_flow.src_port,
        'dst_port': packet_flow.dst_port,
        'protocol': packet_flow.protocol,
        'flow_duration': packet_flow.duration,
        # ... extract all 79 features
    }
    return pd.DataFrame([features])

def predict_threat(flow_features):
    """Multi-model threat detection"""
    
    # DDoS detection
    ddos_pred = ddos_model.predict(flow_features)[0]
    ddos_prob = ddos_model.predict_proba(flow_features)[0][1]
    
    # Anomaly detection
    anomaly_score = anomaly_model.decision_function(flow_features)[0]
    is_anomaly = anomaly_score < 0.072911  # Threshold
    
    # Combined decision
    if ddos_pred == 1 and ddos_prob > 0.8:
        return "DDoS Attack", ddos_prob
    elif is_anomaly:
        return "Anomaly Detected", anomaly_score
    else:
        return "Benign", ddos_prob

# Real-time processing
while True:
    flow = capture_network_flow()  # Your capture logic
    features = extract_features(flow)
    threat, confidence = predict_threat(features)
    
    if threat != "Benign":
        log_to_postgres(flow, threat, confidence)
        trigger_alert(threat, confidence)
```

### Logging to PostgreSQL
```python
def log_to_postgres(flow, threat, confidence):
    """Log detections to database for analysis"""
    conn = psycopg2.connect(**DB_CONFIG)
    cur = conn.cursor()
    
    cur.execute("""
        INSERT INTO production.detections 
        (timestamp, src_ip, dst_ip, threat_type, confidence, features)
        VALUES (%s, %s, %s, %s, %s, %s)
    """, (
        datetime.now(),
        flow.src_ip,
        flow.dst_ip,
        threat,
        confidence,
        json.dumps(flow.features)
    ))
    
    conn.commit()
    conn.close()
```

### Performance Optimization
```python
# Batch predictions for higher throughput
flows_batch = []
while len(flows_batch) < 1000:  # Batch size
    flows_batch.append(capture_network_flow())

features_batch = pd.DataFrame([extract_features(f) for f in flows_batch])
predictions = ddos_model.predict(features_batch)  # Vectorized prediction
```

---

## Database Management

### Backup Strategy
```powershell
# Full database backup
pg_dump -U postgres -d cybermesh -F c -f "backup_cybermesh_$(Get-Date -Format 'yyyyMMdd').backup"

# Backup specific schema
pg_dump -U postgres -d cybermesh -n curated -F c -f "backup_curated.backup"

# Restore
pg_restore -U postgres -d cybermesh -c backup_cybermesh_20251016.backup
```

### Maintenance
```sql
-- Vacuum full to reclaim disk space after deletions
VACUUM FULL;

-- Analyze for query optimization
ANALYZE curated.combined_ddos_full;

-- Reindex for performance
REINDEX TABLE curated.combined_ddos_full;

-- Check database size
SELECT pg_size_pretty(pg_database_size('cybermesh'));

-- Check table sizes
SELECT 
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size
FROM pg_tables
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

### Data Cleanup
```python
# Delete old schemas (already done)
# malware, malware_raw, raw schemas deleted to save 21 GB

# Check remaining data
python analyze_full_database.py

# Current state: 46 GB in curated schema + 150 MB web_attacks_raw
```

### Query Performance
```sql
-- Create indexes for faster queries
CREATE INDEX idx_label ON curated.combined_ddos_full(label);
CREATE INDEX idx_src_port ON curated.combined_ddos_full(src_port);
CREATE INDEX idx_protocol ON curated.combined_ddos_full(protocol);

-- Use EXPLAIN to analyze query performance
EXPLAIN ANALYZE 
SELECT * FROM curated.combined_ddos_full 
WHERE label = 'ddos' 
LIMIT 1000;
```

---

## Model Performance Summary

| Model | Accuracy | AUC | Training Data | Use Case |
|-------|----------|-----|---------------|----------|
| DDoS Detector v2 | 99.98% | 1.0 | 20.4M flows | Production DDoS detection |
| Network Anomaly | N/A | N/A | 1M benign | Zero-day detection |
| API Malware | 88.6% | 0.95 | 1.55M samples | Malware behavior analysis |
| Android Malware | 93.9% | 0.98 | 179K apps | Mobile malware detection |
| PE Imports | 96.3% | 0.99 | 47.5K files | Windows malware detection |
| PE Sections | 39.3% | N/A | 18.5K files | Malware classification |
| Network Malware | 94.9% | 0.98 | 1.1M flows | C&C detection |

---

## Key Learnings

### Data Quality Issues
1. **All columns stored as TEXT:** PostgreSQL ingestion stored numeric data as TEXT, requiring runtime conversion
2. **NaN/INF values:** Required explicit handling with fillna(0)
3. **Label clustering:** Sequential reads hit data clusters (all benign or all attack), solved by removing validation during batch training

### Training Challenges
1. **Memory constraints:** 7 GB RAM insufficient for 5M+ rows at once, used batching
2. **Model size growth:** LightGBM checkpoint grew from 2 MB to 37 MB, slowing training to 100-170 sec/batch
3. **PostgreSQL crashes:** Heavy queries caused crashes, solved with smaller batch sizes (25K rows)

### Production Considerations
1. **Domain shift:** 50/50 balanced training gave 99.99% validation but only 80% production accuracy
2. **Solution:** Merged with realistic 83/17 unbalanced data → 62/38 final distribution
3. **Expected production:** 90-92% accuracy (realistic for DDoS detection)

---

## Next Steps

1. **Model Retraining:** PE Sections classifier needs more boosting rounds
2. **New Data:** Web attacks dataset (150 MB) ready for XSS/SQLi detection models
3. **Feature Engineering:** Add temporal features for better attack detection
4. **Ensemble Models:** Combine DDoS + Anomaly for higher accuracy
5. **Production Validation:** Deploy and measure actual production accuracy
6. **Automated Retraining:** Set up pipeline to retrain on new production data monthly

---

## File Structure
```
CyberMesh Datasets/
├── README.md                          # This file
├── training/
│   ├── models/                        # All trained models (7 models)
│   ├── checkpoints_merged_lgb/        # DDoS training checkpoints (81 files)
│   ├── train_ddos_merged_lgb.py       # DDoS training script
│   ├── train_malware_master.py        # Malware training script
│   └── train_network_anomaly.py       # Anomaly training script
├── tools/
│   ├── analyze_full_database.py       # Database inventory tool
│   └── check_db.py                    # Quick database check
└── samples/                           # Sample exports for analysis

PostgreSQL Database:
└── cybermesh (66 GB → 47 GB after cleanup)
    ├── curated/ (46 GB)
    │   └── combined_ddos_full (20.4M rows, 24 GB)
    └── web_attacks_raw/ (150 MB, 6 tables)
```

---

## Contact & Support
For questions about models, data pipeline, or PostgreSQL setup, refer to training logs and checkpoint files for detailed training history.

**Last Updated:** October 16, 2025
**Training Completed:** Batch 817/817 (100%)
**Database Status:** Optimized, 47 GB total
