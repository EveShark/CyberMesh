# Model Training Scripts

**Purpose:** Offline training for 3 ML models

**Models:**
1. `ddos_lgbm` - DDoS/DoS detection (LightGBM on network flows)
2. `malware_lgbm` - Malware detection (LightGBM on PE features)
3. `anomaly_iforest` - Anomaly detection (IsolationForest unsupervised)

**Process:**
1. Train model on dataset
2. Apply calibration (Platt/Isotonic)
3. Evaluate performance (FPR, recall)
4. Sign with Ed25519
5. Export to `../data/models/`

**Security:**
- All models signed with SIGNING_KEY
- SHA-256 fingerprints tracked
- Model registry updated

**Usage:**
```bash
# Install training dependencies
pip install -r requirements-train.txt

# Train DDoS model
python training/train_ddos.py

# Train malware model  
python training/train_malware.py

# Train anomaly model
python training/train_anomaly.py
```

**Output:**
```
data/models/
├── ddos_lgbm_v1.0.0.pkl
├── ddos_lgbm_v1.0.0.pkl.sig
├── malware_lgbm_v1.0.0.pkl
├── malware_lgbm_v1.0.0.pkl.sig
├── anomaly_iforest_v1.0.0.pkl
├── anomaly_iforest_v1.0.0.pkl.sig
└── model_registry.json
```
