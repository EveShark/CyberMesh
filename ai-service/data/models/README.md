# ML Models Directory

## Purpose
Stores trained ML models with Ed25519 signatures for integrity verification.

## Structure
```
models/
├── ddos_lgbm_v1.0.0.pkl          # LightGBM DDoS/DoS detector
├── ddos_lgbm_v1.0.0.pkl.sig      # Ed25519 signature
├── malware_lgbm_v1.0.0.pkl       # LightGBM malware detector  
├── malware_lgbm_v1.0.0.pkl.sig   # Ed25519 signature
├── anomaly_iforest_v1.0.0.pkl    # IsolationForest anomaly detector
├── anomaly_iforest_v1.0.0.pkl.sig # Ed25519 signature
└── model_registry.json            # Model metadata/fingerprints
```

## Model Format
- **Serialization:** joblib (Python pickle alternative, more efficient)
- **Signature:** Ed25519 (64 bytes) over model file SHA-256 hash
- **Versioning:** Semantic versioning (major.minor.patch)

## Security
- All models MUST be signed with SIGNING_KEY
- Signatures verified on load by ModelRegistry
- Unsigned/tampered models rejected
- File permissions: 0600 (owner read/write only on Unix)

## Model Metadata
Each model has metadata in `model_registry.json`:
```json
{
  "model_name": "ddos_lgbm",
  "version": "1.0.0",
  "fingerprint": "sha256:abc123...",
  "signature": "ed25519:def456...",
  "trained_on": "2025-01-03T12:00:00Z",
  "dataset": "CIC-DDoS2019",
  "calibration": "platt",
  "performance": {
    "accuracy": 0.99,
    "fpr": 0.0008,
    "recall": 0.97
  }
}
```

## Training
Models trained with scripts in `training/`:
- `train_ddos.py` → ddos_lgbm
- `train_malware.py` → malware_lgbm  
- `train_anomaly.py` → anomaly_iforest

## Hot-Reload
Models can be hot-reloaded without service restart:
1. Train new version
2. Sign with SIGNING_KEY
3. Place in models/ directory
4. Update model_registry.json
5. Trigger reload (SIGHUP or API call)
6. Blue/green deployment (old model kept until new validated)
