"""
Calibration script using EMBER2024 dataset - Version 2.

Uses proper EMBER feature vectors for better discrimination.
EMBER features include: histogram (256), byteentropy (256), strings (104),
general (10), header (62), section (255), imports (1280), exports (128).
"""
import json
import os
import sys
from pathlib import Path
from typing import Dict, List, Tuple, Any
import numpy as np
from dataclasses import dataclass
from tqdm import tqdm

sys.path.insert(0, r'B:\sentinel')

# Try to import lightgbm for using EMBER models
try:
    import lightgbm as lgb
    HAS_LIGHTGBM = True
except ImportError:
    HAS_LIGHTGBM = False
    print("Warning: lightgbm not available, using simple features")


def vectorize_ember_features(features: Dict) -> np.ndarray:
    """
    Vectorize EMBER features into a single feature vector.
    
    EMBER v3 feature layout:
    - histogram: 256 values (byte histogram)
    - byteentropy: 256 values (entropy histogram)  
    - strings: ~104 values (string statistics)
    - general: ~10 values (file size, virtual size, etc.)
    - header: ~62 values (PE header fields)
    - section: 255 values (section info, up to 255 sections)
    - imports: 1280 values (import features)
    - exports: 128 values (export features)
    """
    vector_parts = []
    
    # Histogram (256)
    hist = features.get('histogram', [0] * 256)
    if isinstance(hist, list):
        hist = hist[:256] + [0] * (256 - len(hist))
    vector_parts.append(np.array(hist[:256], dtype=np.float32))
    
    # Byte entropy (256)
    entropy = features.get('byteentropy', [0] * 256)
    if isinstance(entropy, list):
        entropy = entropy[:256] + [0] * (256 - len(entropy))
    vector_parts.append(np.array(entropy[:256], dtype=np.float32))
    
    # Strings features
    strings = features.get('strings', {})
    string_feats = [
        strings.get('numstrings', 0),
        strings.get('avlength', 0),
        strings.get('printables', 0),
        strings.get('entropy', 0),
        strings.get('paths', 0),
        strings.get('urls', 0),
        strings.get('registry', 0),
        strings.get('MZ', 0),
    ]
    # Pad to consistent size
    string_feats = string_feats[:104] + [0] * (104 - len(string_feats))
    vector_parts.append(np.array(string_feats, dtype=np.float32))
    
    # General features
    general = features.get('general', {})
    general_feats = [
        general.get('size', 0),
        general.get('vsize', 0),
        general.get('has_debug', 0),
        general.get('exports', 0),
        general.get('imports', 0),
        general.get('has_relocations', 0),
        general.get('has_resources', 0),
        general.get('has_signature', 0),
        general.get('has_tls', 0),
        general.get('symbols', 0),
    ]
    vector_parts.append(np.array(general_feats[:10], dtype=np.float32))
    
    # Concatenate all
    return np.concatenate(vector_parts)


def load_and_vectorize_samples(data_dir: Path, max_samples: int = None) -> Tuple[np.ndarray, np.ndarray]:
    """Load samples and return feature matrix and labels."""
    X_list = []
    y_list = []
    
    print(f"Loading samples from {data_dir}...")
    for jsonl_file in tqdm(sorted(data_dir.glob('*.jsonl'))):
        with open(jsonl_file, 'r') as f:
            for line in f:
                data = json.loads(line)
                
                features = {
                    'histogram': data.get('histogram', []),
                    'byteentropy': data.get('byteentropy', []),
                    'strings': data.get('strings', {}),
                    'general': data.get('general', {}),
                    'header': data.get('header', {}),
                    'imports': data.get('imports', {}),
                }
                
                vec = vectorize_ember_features(features)
                X_list.append(vec)
                y_list.append(data.get('label', 0))
                
                if max_samples and len(X_list) >= max_samples:
                    return np.array(X_list), np.array(y_list)
    
    return np.array(X_list), np.array(y_list)


def train_simple_model(X_train: np.ndarray, y_train: np.ndarray):
    """Train a simple model for scoring."""
    if HAS_LIGHTGBM:
        print("Training LightGBM model...")
        train_data = lgb.Dataset(X_train, label=y_train)
        params = {
            'objective': 'binary',
            'metric': 'auc',
            'boosting_type': 'gbdt',
            'num_leaves': 31,
            'learning_rate': 0.05,
            'feature_fraction': 0.9,
            'verbose': -1,
        }
        model = lgb.train(params, train_data, num_boost_round=100)
        return model
    else:
        # Simple logistic regression fallback
        from sklearn.linear_model import LogisticRegression
        from sklearn.preprocessing import StandardScaler
        
        print("Training Logistic Regression model...")
        scaler = StandardScaler()
        X_scaled = scaler.fit_transform(X_train)
        model = LogisticRegression(max_iter=1000, random_state=42)
        model.fit(X_scaled, y_train)
        return (model, scaler)


def predict_proba(model, X: np.ndarray) -> np.ndarray:
    """Get probability predictions."""
    if HAS_LIGHTGBM:
        return model.predict(X)
    else:
        model, scaler = model
        X_scaled = scaler.transform(X)
        return model.predict_proba(X_scaled)[:, 1]


def compute_metrics_at_threshold(scores: np.ndarray, labels: np.ndarray, 
                                  threshold: float) -> Dict:
    """Compute FP, FN, TP, TN at a given threshold."""
    predictions = (scores >= threshold).astype(int)
    
    tp = int(np.sum((predictions == 1) & (labels == 1)))
    tn = int(np.sum((predictions == 0) & (labels == 0)))
    fp = int(np.sum((predictions == 1) & (labels == 0)))
    fn = int(np.sum((predictions == 0) & (labels == 1)))
    
    total = len(labels)
    positives = int(np.sum(labels == 1))
    negatives = int(np.sum(labels == 0))
    
    tpr = tp / positives if positives > 0 else 0  # Detection rate
    fpr = fp / negatives if negatives > 0 else 0  # False positive rate
    fnr = fn / positives if positives > 0 else 0  # False negative rate (miss rate)
    tnr = tn / negatives if negatives > 0 else 0  # True negative rate
    
    precision = tp / (tp + fp) if (tp + fp) > 0 else 0
    f1 = 2 * precision * tpr / (precision + tpr) if (precision + tpr) > 0 else 0
    
    return {
        'threshold': float(threshold),
        'tp': tp, 'tn': tn, 'fp': fp, 'fn': fn,
        'tpr': float(tpr),  # Detection rate
        'fpr': float(fpr),  # False positive rate
        'fnr': float(fnr),  # False negative rate
        'tnr': float(tnr),
        'precision': float(precision),
        'f1': float(f1),
    }


def find_optimal_thresholds(scores: np.ndarray, labels: np.ndarray) -> Dict:
    """Find optimal thresholds for different objectives."""
    thresholds = np.linspace(0, 1, 200)
    
    results = []
    for t in thresholds:
        metrics = compute_metrics_at_threshold(scores, labels, t)
        results.append(metrics)
    
    # Convert to arrays for easier analysis
    tpr_arr = np.array([r['tpr'] for r in results])
    fpr_arr = np.array([r['fpr'] for r in results])
    fnr_arr = np.array([r['fnr'] for r in results])
    f1_arr = np.array([r['f1'] for r in results])
    
    # AUC
    sorted_idx = np.argsort(fpr_arr)
    auc = float(np.trapz(tpr_arr[sorted_idx], fpr_arr[sorted_idx]))
    
    # Youden's J (balanced TPR-FPR)
    j_scores = tpr_arr - fpr_arr
    youden_idx = np.argmax(j_scores)
    
    # Best F1
    f1_idx = np.argmax(f1_arr)
    
    # Find thresholds at specific FPR targets
    target_fprs = [0.001, 0.005, 0.01, 0.02, 0.05, 0.10]
    thresholds_at_fpr = {}
    for target in target_fprs:
        # Find threshold where FPR is closest to target
        valid_idx = np.where(fpr_arr <= target)[0]
        if len(valid_idx) > 0:
            # Among valid, pick one with highest TPR
            best_idx = valid_idx[np.argmax(tpr_arr[valid_idx])]
            thresholds_at_fpr[f'fpr_{target:.1%}'] = results[best_idx]
    
    # Find thresholds at specific FNR targets (miss rate)
    target_fnrs = [0.01, 0.05, 0.10, 0.20]
    thresholds_at_fnr = {}
    for target in target_fnrs:
        valid_idx = np.where(fnr_arr <= target)[0]
        if len(valid_idx) > 0:
            # Among valid, pick one with lowest FPR
            best_idx = valid_idx[np.argmin(fpr_arr[valid_idx])]
            thresholds_at_fnr[f'fnr_{target:.1%}'] = results[best_idx]
    
    return {
        'auc': auc,
        'youden_optimal': results[youden_idx],
        'f1_optimal': results[f1_idx],
        'thresholds_at_fpr': thresholds_at_fpr,
        'thresholds_at_fnr': thresholds_at_fnr,
        'all_results': results,
    }


def main():
    output_dir = Path(r'B:\sentinel\data\calibration\ember_results')
    output_dir.mkdir(parents=True, exist_ok=True)
    
    print("=" * 70)
    print("EMBER2024 CALIBRATION - ML MODEL APPROACH")
    print("=" * 70)
    
    # Load PDF test data
    pdf_dir = Path(r'B:\sentinel\data\evaluation_datasets\ember2024_data\PDF_test')
    
    print("\n[1] Loading and vectorizing PDF test samples...")
    X, y = load_and_vectorize_samples(pdf_dir, max_samples=10000)
    
    n_benign = int(np.sum(y == 0))
    n_malicious = int(np.sum(y == 1))
    print(f"    Total: {len(y)} samples ({n_benign} benign, {n_malicious} malicious)")
    print(f"    Feature vector size: {X.shape[1]}")
    
    # Split into train/test
    np.random.seed(42)
    indices = np.random.permutation(len(y))
    split = int(0.7 * len(y))
    train_idx, test_idx = indices[:split], indices[split:]
    
    X_train, y_train = X[train_idx], y[train_idx]
    X_test, y_test = X[test_idx], y[test_idx]
    
    print(f"    Train: {len(y_train)}, Test: {len(y_test)}")
    
    # Train model
    print("\n[2] Training model...")
    model = train_simple_model(X_train, y_train)
    
    # Get predictions
    print("\n[3] Computing predictions...")
    train_scores = predict_proba(model, X_train)
    test_scores = predict_proba(model, X_test)
    
    print(f"    Train scores: min={train_scores.min():.3f}, max={train_scores.max():.3f}")
    print(f"    Test scores:  min={test_scores.min():.3f}, max={test_scores.max():.3f}")
    
    # Find optimal thresholds on test set
    print("\n[4] Finding optimal thresholds...")
    results = find_optimal_thresholds(test_scores, y_test)
    
    print(f"\n    AUC: {results['auc']:.4f}")
    
    print("\n    Youden Optimal (balanced FP/FN):")
    yo = results['youden_optimal']
    print(f"      Threshold: {yo['threshold']:.3f}")
    print(f"      Detection Rate (TPR): {yo['tpr']:.1%}")
    print(f"      False Positive Rate: {yo['fpr']:.1%}")
    print(f"      False Negative Rate: {yo['fnr']:.1%}")
    print(f"      F1 Score: {yo['f1']:.3f}")
    
    print("\n    Best F1 Score:")
    f1o = results['f1_optimal']
    print(f"      Threshold: {f1o['threshold']:.3f}")
    print(f"      Detection Rate (TPR): {f1o['tpr']:.1%}")
    print(f"      False Positive Rate: {f1o['fpr']:.1%}")
    print(f"      False Negative Rate: {f1o['fnr']:.1%}")
    print(f"      F1 Score: {f1o['f1']:.3f}")
    
    print("\n    Thresholds at target FP rates:")
    for name, data in results['thresholds_at_fpr'].items():
        print(f"      {name}: thresh={data['threshold']:.3f}, "
              f"TPR={data['tpr']:.1%}, FPR={data['fpr']:.1%}, FNR={data['fnr']:.1%}")
    
    print("\n    Thresholds at target FN rates (miss rates):")
    for name, data in results['thresholds_at_fnr'].items():
        print(f"      {name}: thresh={data['threshold']:.3f}, "
              f"TPR={data['tpr']:.1%}, FPR={data['fpr']:.1%}, FNR={data['fnr']:.1%}")
    
    # Validate on challenge set
    print("\n" + "=" * 70)
    print("VALIDATION ON CHALLENGE SET (Evasive Malware)")
    print("=" * 70)
    
    challenge_dir = Path(r'B:\sentinel\data\evaluation_datasets\ember2024_data\challenge')
    print("\n[5] Loading challenge samples...")
    X_challenge, y_challenge = load_and_vectorize_samples(challenge_dir, max_samples=3000)
    print(f"    Loaded {len(y_challenge)} evasive malware samples")
    
    challenge_scores = predict_proba(model, X_challenge)
    print(f"    Scores: min={challenge_scores.min():.3f}, max={challenge_scores.max():.3f}, "
          f"mean={challenge_scores.mean():.3f}")
    
    print("\n[6] Detection rates on evasive malware at different thresholds:")
    
    # Test at key thresholds
    test_thresholds = [
        ('Youden optimal', yo['threshold']),
        ('F1 optimal', f1o['threshold']),
    ]
    
    # Add FPR-based thresholds
    for name, data in results['thresholds_at_fpr'].items():
        test_thresholds.append((name, data['threshold']))
    
    for name, thresh in test_thresholds:
        detected = np.sum(challenge_scores >= thresh)
        rate = detected / len(challenge_scores)
        print(f"    {name} (thresh={thresh:.3f}): {detected}/{len(challenge_scores)} ({rate:.1%})")
    
    # Save comprehensive results
    print("\n" + "=" * 70)
    print("SAVING RESULTS")
    print("=" * 70)
    
    # Remove non-serializable items
    save_results = {
        'dataset': 'EMBER2024',
        'train_samples': len(y_train),
        'test_samples': len(y_test),
        'feature_dim': int(X.shape[1]),
        'auc': results['auc'],
        'youden_optimal': results['youden_optimal'],
        'f1_optimal': results['f1_optimal'],
        'thresholds_at_fpr': results['thresholds_at_fpr'],
        'thresholds_at_fnr': results['thresholds_at_fnr'],
        'challenge_detection': {
            name: float(np.sum(challenge_scores >= thresh) / len(challenge_scores))
            for name, thresh in test_thresholds
        }
    }
    
    output_file = output_dir / 'ember_calibration_v2.json'
    with open(output_file, 'w') as f:
        json.dump(save_results, f, indent=2)
    print(f"\nResults saved to: {output_file}")
    
    # Print recommendations
    print("\n" + "=" * 70)
    print("RECOMMENDATIONS FOR SENTINEL")
    print("=" * 70)
    
    print("\n  For SECURITY-FOCUSED deployments (minimize missed malware):")
    if 'fnr_1.0%' in results['thresholds_at_fnr']:
        rec = results['thresholds_at_fnr']['fnr_1.0%']
        print(f"    Use threshold: {rec['threshold']:.3f}")
        print(f"    Expected: {rec['tpr']:.1%} detection, {rec['fpr']:.1%} FP rate, {rec['fnr']:.1%} miss rate")
    
    print("\n  For BALANCED deployments:")
    print(f"    Use threshold: {yo['threshold']:.3f}")
    print(f"    Expected: {yo['tpr']:.1%} detection, {yo['fpr']:.1%} FP rate, {yo['fnr']:.1%} miss rate")
    
    print("\n  For LOW-FP deployments (minimize false alarms):")
    if 'fpr_1.0%' in results['thresholds_at_fpr']:
        rec = results['thresholds_at_fpr']['fpr_1.0%']
        print(f"    Use threshold: {rec['threshold']:.3f}")
        print(f"    Expected: {rec['tpr']:.1%} detection, {rec['fpr']:.1%} FP rate, {rec['fnr']:.1%} miss rate")


if __name__ == '__main__':
    main()
