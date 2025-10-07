"""
Train IsolationForest for anomaly detection (unsupervised).

Trains on benign network flows only, detects deviations.
"""

import sys
import json
import hashlib
from pathlib import Path
from datetime import datetime

import numpy as np
import joblib
from sklearn.ensemble import IsolationForest
from sklearn.model_selection import train_test_split
from sklearn.metrics import classification_report, roc_auc_score, confusion_matrix

project_root = Path(__file__).parent.parent
sys.path.insert(0, str(project_root))

from src.utils.signer import Signer
from src.logging import get_logger

logger = get_logger(__name__)


def load_synthetic_anomaly_data():
    """Generate synthetic network data (benign + anomalies)."""
    logger.info("Generating synthetic anomaly dataset...")
    
    np.random.seed(44)
    n_benign = 6000
    n_anomaly = 1000
    
    # Benign (train IsolationForest on this)
    X_benign = np.random.randn(n_benign, 30)
    X_benign[:, 0] = np.random.uniform(100, 5000, n_benign)  # Normal pps
    y_benign = np.zeros(n_benign)
    
    # Anomalies (test detection)
    X_anomaly = np.random.randn(n_anomaly, 30) * 3
    X_anomaly[:, 0] = np.random.uniform(20000, 100000, n_anomaly)  # High pps
    y_anomaly = np.ones(n_anomaly)
    
    X = np.vstack([X_benign, X_anomaly])
    y = np.hstack([y_benign, y_anomaly])
    
    indices = np.random.permutation(len(X))
    X, y = X[indices], y[indices]
    
    logger.info(f"Generated {X.shape[0]} samples: {np.sum(y==1)} anomalies, {np.sum(y==0)} benign")
    return X, y


def main():
    logger.info("="*60)
    logger.info("TRAINING ANOMALY DETECTION MODEL (IsolationForest)")
    logger.info("="*60)
    
    X, y = load_synthetic_anomaly_data()
    
    # Split (train on benign only, test on mixed)
    X_benign = X[y == 0]
    X_train, X_temp = train_test_split(X_benign, test_size=0.3, random_state=42)
    X_val, _ = train_test_split(X_temp, test_size=0.5, random_state=42)
    
    # For testing, use mixed data
    X_train_full, X_test, y_train_full, y_test = train_test_split(X, y, test_size=0.2, random_state=42, stratify=y)
    
    logger.info(f"Training on {len(X_train)} benign samples")
    
    # Train IsolationForest (unsupervised)
    model = IsolationForest(
        n_estimators=100,
        max_samples='auto',
        contamination=0.1,  # Expect 10% anomalies
        random_state=42,
        verbose=0
    )
    model.fit(X_train)
    logger.info("Training complete")
    
    # Evaluate on test set (labeled)
    y_pred_scores = model.decision_function(X_test)
    y_pred = (y_pred_scores < 0).astype(int)  # Negative = anomaly
    
    # Compute metrics
    tn, fp, fn, tp = confusion_matrix(y_test, y_pred).ravel()
    fpr = fp / (fp + tn) if (fp + tn) > 0 else 0.0
    tpr = tp / (tp + fn) if (tp + fn) > 0 else 0.0
    
    # Convert scores to probabilities for AUC
    proba = 1.0 / (1.0 + np.exp(y_pred_scores))
    auc = roc_auc_score(y_test, proba)
    
    logger.info(f"Performance - AUC: {auc:.4f}, FPR: {fpr:.4f}, TPR: {tpr:.4f}")
    print(classification_report(y_test, y_pred, target_names=['Benign', 'Anomaly']))
    
    # Save
    models_dir = project_root / 'data' / 'models'
    model_path = models_dir / 'anomaly_iforest_v1.0.0.pkl'
    joblib.dump(model, model_path)
    logger.info(f"Model saved: {model_path}")
    
    # Sign
    keys_dir = project_root / 'keys'
    signer = Signer(str(keys_dir / 'signing_key.pem'), key_id='model_signing_key', domain_separation='model.v1')
    model_bytes = model_path.read_bytes()
    fingerprint = hashlib.sha256(model_bytes).hexdigest()
    signature, _, _ = signer.sign(model_bytes)
    (model_path.with_suffix('.pkl.sig')).write_bytes(signature)
    logger.info(f"Model signed: {fingerprint}")
    
    # Update registry
    registry_path = models_dir / 'model_registry.json'
    registry = json.loads(registry_path.read_text())
    registry['models']['anomaly_iforest'] = {
        'version': '1.0.0',
        'path': 'anomaly_iforest_v1.0.0.pkl',
        'fingerprint': fingerprint,
        'algorithm': 'IsolationForest',
        'calibration': 'none',
        'performance': {'auc': float(auc), 'fpr': float(fpr), 'tpr': float(tpr), 'tp': int(tp), 'fp': int(fp), 'tn': int(tn), 'fn': int(fn)},
        'trained_at': datetime.utcnow().isoformat() + 'Z',
        'feature_count': 30,
        'threat_type': 'anomaly'
    }
    registry_path.write_text(json.dumps(registry, indent=2))
    logger.info("Registry updated")
    logger.info("="*60)
    logger.info(f"COMPLETE! AUC: {auc:.4f}, FPR: {fpr:.4f}")
    logger.info("="*60)


if __name__ == '__main__':
    main()
