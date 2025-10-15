"""
Train LightGBM model for DDoS/DoS detection.

Security-first, military-grade ML training:
- Calibration (Platt scaling)
- Ed25519 model signing
- SHA-256 fingerprinting
- Performance evaluation

NO MOCKS - Real LightGBM training.
"""

import sys
import json
import hashlib
from pathlib import Path
from datetime import datetime

import numpy as np
import joblib
from sklearn.model_selection import train_test_split
from sklearn.calibration import CalibratedClassifierCV
from sklearn.metrics import classification_report, roc_auc_score, confusion_matrix
import lightgbm as lgb

# Add project root to path
project_root = Path(__file__).parent.parent
sys.path.insert(0, str(project_root))

from src.utils.signer import Signer
from src.logging import get_logger

logger = get_logger(__name__)


def load_synthetic_ddos_data():
    """
    Generate synthetic DDoS dataset for proof-of-concept.
    
    Features (30 dims matching NetworkFlowExtractor):
    - pps, bps, packet_size_avg, etc.
    
    Labels:
    - 0: Benign
    - 1: DDoS attack
    
    Returns:
        X (n, 30), y (n,)
    """
    logger.info("Generating synthetic DDoS dataset...")
    
    np.random.seed(42)
    n_benign = 5000
    n_ddos = 3000
    
    # Benign traffic (normal network flows)
    X_benign = np.random.randn(n_benign, 30)
    X_benign[:, 0] = np.random.uniform(100, 5000, n_benign)  # Normal pps
    X_benign[:, 1] = np.random.uniform(1e5, 1e7, n_benign)   # Normal bps
    X_benign[:, 2] = np.random.uniform(64, 1500, n_benign)   # Packet size
    y_benign = np.zeros(n_benign)
    
    # DDoS traffic (high volume, abnormal patterns)
    X_ddos = np.random.randn(n_ddos, 30) * 2  # Higher variance
    X_ddos[:, 0] = np.random.uniform(50000, 500000, n_ddos)  # Very high pps
    X_ddos[:, 1] = np.random.uniform(1e8, 1e9, n_ddos)       # Very high bps
    X_ddos[:, 2] = np.random.uniform(40, 100, n_ddos)        # Small packets (SYN flood)
    y_ddos = np.ones(n_ddos)
    
    # Combine
    X = np.vstack([X_benign, X_ddos])
    y = np.hstack([y_benign, y_ddos])
    
    # Shuffle
    indices = np.random.permutation(len(X))
    X = X[indices]
    y = y[indices]
    
    logger.info(f"Generated dataset: {X.shape[0]} samples, {np.sum(y == 1)} DDoS, {np.sum(y == 0)} benign")
    return X, y


def train_ddos_model(X_train, y_train, X_val, y_val):
    """
    Train LightGBM classifier for DDoS detection.
    
    Hyperparameters optimized for imbalanced classification.
    
    Args:
        X_train, y_train: Training data
        X_val, y_val: Validation data
    
    Returns:
        Trained LightGBM model
    """
    logger.info("Training LightGBM DDoS model...")
    
    # Class weights (handle imbalance)
    n_benign = np.sum(y_train == 0)
    n_ddos = np.sum(y_train == 1)
    scale_pos_weight = n_benign / n_ddos
    
    # LightGBM parameters (tuned for security)
    params = {
        'objective': 'binary',
        'metric': 'auc',
        'boosting_type': 'gbdt',
        'num_leaves': 31,
        'max_depth': 7,
        'learning_rate': 0.05,
        'n_estimators': 100,
        'scale_pos_weight': scale_pos_weight,
        'min_child_samples': 20,
        'subsample': 0.8,
        'colsample_bytree': 0.8,
        'reg_alpha': 0.1,
        'reg_lambda': 0.1,
        'random_state': 42,
        'verbose': -1
    }
    
    model = lgb.LGBMClassifier(**params)
    model.fit(
        X_train, y_train,
        eval_set=[(X_val, y_val)],
        eval_metric='auc'
    )
    
    logger.info("Training complete")
    return model


def calibrate_model(model, X_val, y_val, method='sigmoid'):
    """
    Apply Platt scaling (sigmoid calibration) to model.
    
    Ensures predict_proba() outputs well-calibrated probabilities.
    
    Args:
        model: Trained LightGBM model
        X_val, y_val: Validation data
        method: 'sigmoid' (Platt) or 'isotonic'
    
    Returns:
        Calibrated model
    """
    logger.info(f"Applying {method} calibration...")
    
    calibrated = CalibratedClassifierCV(
        model,
        method=method,
        cv='prefit'  # Use validation set (already fitted)
    )
    calibrated.fit(X_val, y_val)
    
    logger.info("Calibration complete")
    return calibrated


def evaluate_model(model, X_test, y_test):
    """
    Evaluate model performance.
    
    Metrics:
    - AUC-ROC
    - Precision/Recall/F1
    - Confusion matrix
    - FPR (critical for security)
    
    Args:
        model: Trained model
        X_test, y_test: Test data
    
    Returns:
        Dict with metrics
    """
    logger.info("Evaluating model...")
    
    y_pred = model.predict(X_test)
    y_proba = model.predict_proba(X_test)[:, 1]
    
    # AUC
    auc = roc_auc_score(y_test, y_proba)
    
    # Confusion matrix
    tn, fp, fn, tp = confusion_matrix(y_test, y_pred).ravel()
    fpr = fp / (fp + tn) if (fp + tn) > 0 else 0.0
    tpr = tp / (tp + fn) if (tp + fn) > 0 else 0.0
    
    metrics = {
        'auc': float(auc),
        'fpr': float(fpr),
        'tpr': float(tpr),
        'tp': int(tp),
        'fp': int(fp),
        'tn': int(tn),
        'fn': int(fn)
    }
    
    logger.info(f"Performance - AUC: {auc:.4f}, FPR: {fpr:.4f}, TPR: {tpr:.4f}")
    logger.info(f"Confusion Matrix - TP: {tp}, FP: {fp}, TN: {tn}, FN: {fn}")
    
    print(classification_report(y_test, y_pred, target_names=['Benign', 'DDoS']))
    
    return metrics


def sign_model(model_path: Path, signer: Signer):
    """
    Sign model file with Ed25519.
    
    Creates:
    - <model>.pkl - Model file
    - <model>.pkl.sig - Ed25519 signature
    
    Args:
        model_path: Path to .pkl file
        signer: Signer instance
    """
    logger.info(f"Signing model: {model_path}")
    
    # Read model bytes
    model_bytes = model_path.read_bytes()
    
    # Compute SHA-256 fingerprint
    fingerprint = hashlib.sha256(model_bytes).hexdigest()
    logger.info(f"Model fingerprint: {fingerprint}")
    
    # Sign (returns tuple: signature, public_key_bytes, key_id)
    signature, public_key_bytes, key_id = signer.sign(model_bytes)
    
    # Save signature
    sig_path = model_path.with_suffix('.pkl.sig')
    sig_path.write_bytes(signature)
    
    logger.info(f"Signature saved: {sig_path}")
    return fingerprint


def main():
    """Train, calibrate, sign DDoS model."""
    logger.info("=" * 60)
    logger.info("TRAINING DDOS DETECTION MODEL")
    logger.info("=" * 60)
    
    # 1. Load data
    X, y = load_synthetic_ddos_data()
    
    # 2. Split (60% train, 20% val, 20% test)
    X_train, X_temp, y_train, y_temp = train_test_split(X, y, test_size=0.4, random_state=42, stratify=y)
    X_val, X_test, y_val, y_test = train_test_split(X_temp, y_temp, test_size=0.5, random_state=42, stratify=y_temp)
    
    logger.info(f"Split - Train: {len(X_train)}, Val: {len(X_val)}, Test: {len(X_test)}")
    
    # 3. Train
    model = train_ddos_model(X_train, y_train, X_val, y_val)
    
    # 4. Calibrate
    calibrated_model = calibrate_model(model, X_val, y_val, method='sigmoid')
    
    # 5. Evaluate
    metrics = evaluate_model(calibrated_model, X_test, y_test)
    
    # 6. Save model
    models_dir = project_root / 'data' / 'models'
    models_dir.mkdir(parents=True, exist_ok=True)
    
    model_path = models_dir / 'ddos_lgbm_v1.0.0.pkl'
    joblib.dump(calibrated_model, model_path)
    logger.info(f"Model saved: {model_path}")
    
    # 7. Sign model
    keys_dir = project_root / 'keys'
    private_key_path = keys_dir / 'signing_key.pem'
    signer = Signer(str(private_key_path), key_id='model_signing_key', domain_separation='model.v1')
    fingerprint = sign_model(model_path, signer)
    
    # 8. Update registry
    registry_path = models_dir / 'model_registry.json'
    
    if registry_path.exists():
        registry = json.loads(registry_path.read_text())
    else:
        registry = {'models': {}}
    
    registry['models']['ddos_lgbm'] = {
        'version': '1.0.0',
        'path': 'ddos_lgbm_v1.0.0.pkl',
        'fingerprint': fingerprint,
        'algorithm': 'LightGBM',
        'calibration': 'sigmoid',
        'performance': metrics,
        'trained_at': datetime.utcnow().isoformat() + 'Z',
        'feature_count': 30,
        'threat_type': 'ddos'
    }
    
    registry_path.write_text(json.dumps(registry, indent=2))
    logger.info(f"Registry updated: {registry_path}")
    
    logger.info("=" * 60)
    logger.info("TRAINING COMPLETE!")
    logger.info("=" * 60)
    logger.info(f"Model: {model_path}")
    logger.info(f"AUC: {metrics['auc']:.4f}")
    logger.info(f"FPR: {metrics['fpr']:.4f}")
    logger.info("=" * 60)


if __name__ == '__main__':
    main()
