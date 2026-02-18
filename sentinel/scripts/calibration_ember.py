"""
Calibration script using EMBER2024 dataset.

This script:
1. Loads EMBER2024 PDF test set (12K benign + 12K malicious)
2. Extracts features compatible with our providers
3. Runs providers and collects scores
4. Generates ROC curves and finds optimal thresholds
5. Computes calibration metrics
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

@dataclass
class SampleData:
    """Holds sample data from EMBER2024."""
    sha256: str
    label: int  # 0=benign, 1=malicious
    file_type: str
    features: Dict[str, Any]
    

def load_ember_samples(data_dir: Path, max_samples: int = None) -> List[SampleData]:
    """Load samples from EMBER2024 JSONL files."""
    samples = []
    
    for jsonl_file in sorted(data_dir.glob('*.jsonl')):
        with open(jsonl_file, 'r') as f:
            for line in f:
                data = json.loads(line)
                sample = SampleData(
                    sha256=data.get('sha256', ''),
                    label=data.get('label', -1),
                    file_type=data.get('file_type', 'unknown'),
                    features={
                        'histogram': data.get('histogram', []),
                        'byteentropy': data.get('byteentropy', []),
                        'strings': data.get('strings', {}),
                        'general': data.get('general', {}),
                        'header': data.get('header', {}),
                        'imports': data.get('imports', {}),
                        'section': data.get('section', {}),
                    }
                )
                samples.append(sample)
                
                if max_samples and len(samples) >= max_samples:
                    return samples
    
    return samples


def compute_entropy_score(features: Dict) -> float:
    """Compute entropy-based score from EMBER features."""
    byteentropy = features.get('byteentropy', [])
    if not byteentropy:
        return 0.0
    
    # EMBER byteentropy is a histogram of entropy values
    # High entropy (packed/encrypted) is suspicious
    entropy_array = np.array(byteentropy)
    if len(entropy_array) == 0:
        return 0.0
    
    # Weight towards high entropy buckets (last buckets = high entropy)
    weights = np.linspace(0, 1, len(entropy_array))
    weighted_entropy = np.sum(entropy_array * weights) / (np.sum(entropy_array) + 1e-10)
    
    return float(weighted_entropy)


def compute_strings_score(features: Dict) -> float:
    """Compute strings-based score from EMBER features."""
    strings = features.get('strings', {})
    if not strings:
        return 0.0
    
    score = 0.0
    
    # Number of strings
    num_strings = strings.get('numstrings', 0)
    avg_length = strings.get('avlength', 0)
    
    # Suspicious patterns
    # High number of short strings = potentially obfuscated
    if num_strings > 1000 and avg_length < 5:
        score += 0.3
    
    # Check printable ratio
    printables = strings.get('printables', 0)
    if num_strings > 0:
        printable_ratio = printables / num_strings
        if printable_ratio < 0.5:  # Low printable ratio = suspicious
            score += 0.2
    
    # Entropy of strings
    entropy = strings.get('entropy', 0)
    if entropy > 5.5:  # High entropy strings
        score += 0.3
    
    return min(score, 1.0)


def compute_imports_score(features: Dict) -> float:
    """Compute imports-based score (similar to our PE ML)."""
    imports = features.get('imports', {})
    if not imports:
        return 0.0
    
    # EMBER imports is a dict of DLL -> list of functions
    suspicious_apis = {
        'kernel32.dll': ['VirtualAlloc', 'VirtualProtect', 'WriteProcessMemory', 
                        'CreateRemoteThread', 'LoadLibraryA', 'GetProcAddress'],
        'ntdll.dll': ['NtCreateThread', 'NtWriteVirtualMemory', 'RtlCreateUserThread'],
        'advapi32.dll': ['RegSetValueExA', 'CryptEncrypt', 'CryptDecrypt'],
        'wininet.dll': ['InternetOpenA', 'InternetConnectA', 'HttpSendRequestA'],
        'ws2_32.dll': ['socket', 'connect', 'send', 'recv'],
    }
    
    score = 0.0
    suspicious_count = 0
    
    for dll, funcs in imports.items():
        dll_lower = dll.lower()
        for susp_dll, susp_funcs in suspicious_apis.items():
            if susp_dll in dll_lower:
                for func in funcs:
                    if any(sf.lower() in func.lower() for sf in susp_funcs):
                        suspicious_count += 1
    
    # Normalize
    if suspicious_count > 10:
        score = 0.9
    elif suspicious_count > 5:
        score = 0.6
    elif suspicious_count > 2:
        score = 0.3
    elif suspicious_count > 0:
        score = 0.1
    
    return score


def compute_section_score(features: Dict) -> float:
    """Compute section-based score."""
    sections = features.get('section', {})
    if not sections:
        return 0.0
    
    score = 0.0
    
    # Check for suspicious section characteristics
    entry = sections.get('entry', '')
    if entry and entry not in ['.text', '.code', 'CODE']:
        score += 0.2  # Entry point in unusual section
    
    # High entropy sections
    sections_list = sections.get('sections', [])
    for sec in sections_list:
        if isinstance(sec, dict):
            entropy = sec.get('entropy', 0)
            if entropy > 7.0:
                score += 0.3
                break
    
    return min(score, 1.0)


def compute_combined_score(features: Dict) -> Tuple[float, Dict[str, float]]:
    """Compute combined malware score from all features."""
    scores = {
        'entropy': compute_entropy_score(features),
        'strings': compute_strings_score(features),
        'imports': compute_imports_score(features),
        'sections': compute_section_score(features),
    }
    
    # Weighted combination
    weights = {
        'entropy': 0.15,
        'strings': 0.20,
        'imports': 0.40,
        'sections': 0.25,
    }
    
    combined = sum(scores[k] * weights[k] for k in scores)
    return combined, scores


def analyze_samples(samples: List[SampleData]) -> Tuple[np.ndarray, np.ndarray, List[Dict]]:
    """Analyze all samples and return scores and labels."""
    scores = []
    labels = []
    details = []
    
    print(f"Analyzing {len(samples)} samples...")
    for sample in tqdm(samples):
        combined, individual_scores = compute_combined_score(sample.features)
        scores.append(combined)
        labels.append(sample.label)
        details.append({
            'sha256': sample.sha256,
            'label': sample.label,
            'combined_score': combined,
            **individual_scores
        })
    
    return np.array(scores), np.array(labels), details


def compute_roc_curve(scores: np.ndarray, labels: np.ndarray, 
                      num_thresholds: int = 100) -> Dict:
    """Compute ROC curve and find optimal thresholds."""
    thresholds = np.linspace(0, 1, num_thresholds)
    
    tpr_list = []  # True Positive Rate (Detection Rate)
    fpr_list = []  # False Positive Rate
    fnr_list = []  # False Negative Rate
    
    for thresh in thresholds:
        predictions = (scores >= thresh).astype(int)
        
        tp = np.sum((predictions == 1) & (labels == 1))
        tn = np.sum((predictions == 0) & (labels == 0))
        fp = np.sum((predictions == 1) & (labels == 0))
        fn = np.sum((predictions == 0) & (labels == 1))
        
        tpr = tp / (tp + fn) if (tp + fn) > 0 else 0
        fpr = fp / (fp + tn) if (fp + tn) > 0 else 0
        fnr = fn / (fn + tp) if (fn + tp) > 0 else 0
        
        tpr_list.append(tpr)
        fpr_list.append(fpr)
        fnr_list.append(fnr)
    
    # Find optimal thresholds for different goals
    tpr_arr = np.array(tpr_list)
    fpr_arr = np.array(fpr_list)
    fnr_arr = np.array(fnr_list)
    
    # Youden's J statistic (maximize TPR - FPR)
    j_scores = tpr_arr - fpr_arr
    optimal_j_idx = np.argmax(j_scores)
    
    # Find threshold for specific FP rates
    target_fp_rates = [0.01, 0.05, 0.10]  # 1%, 5%, 10%
    thresholds_at_fp = {}
    for target_fpr in target_fp_rates:
        idx = np.argmin(np.abs(fpr_arr - target_fpr))
        thresholds_at_fp[f'fpr_{int(target_fpr*100)}pct'] = {
            'threshold': float(thresholds[idx]),
            'fpr': float(fpr_arr[idx]),
            'tpr': float(tpr_arr[idx]),
            'fnr': float(fnr_arr[idx]),
        }
    
    # AUC calculation (trapezoidal)
    sorted_idx = np.argsort(fpr_arr)
    auc = np.trapz(tpr_arr[sorted_idx], fpr_arr[sorted_idx])
    
    return {
        'thresholds': thresholds.tolist(),
        'tpr': tpr_arr.tolist(),
        'fpr': fpr_arr.tolist(),
        'fnr': fnr_arr.tolist(),
        'auc': float(auc),
        'optimal_youden': {
            'threshold': float(thresholds[optimal_j_idx]),
            'tpr': float(tpr_arr[optimal_j_idx]),
            'fpr': float(fpr_arr[optimal_j_idx]),
            'fnr': float(fnr_arr[optimal_j_idx]),
            'j_score': float(j_scores[optimal_j_idx]),
        },
        'thresholds_at_fp': thresholds_at_fp,
    }


def compute_calibration_metrics(scores: np.ndarray, labels: np.ndarray, 
                                n_bins: int = 10) -> Dict:
    """Compute calibration metrics (ECE, MCE, Brier score)."""
    # Brier score
    brier = np.mean((scores - labels) ** 2)
    
    # Expected Calibration Error (ECE)
    bin_edges = np.linspace(0, 1, n_bins + 1)
    ece = 0.0
    mce = 0.0
    bin_data = []
    
    for i in range(n_bins):
        mask = (scores >= bin_edges[i]) & (scores < bin_edges[i + 1])
        if np.sum(mask) > 0:
            bin_acc = np.mean(labels[mask])
            bin_conf = np.mean(scores[mask])
            bin_size = np.sum(mask)
            
            gap = np.abs(bin_acc - bin_conf)
            ece += gap * bin_size / len(scores)
            mce = max(mce, gap)
            
            bin_data.append({
                'bin': i,
                'range': f'{bin_edges[i]:.2f}-{bin_edges[i+1]:.2f}',
                'samples': int(bin_size),
                'accuracy': float(bin_acc),
                'confidence': float(bin_conf),
                'gap': float(gap),
            })
    
    return {
        'brier_score': float(brier),
        'ece': float(ece),
        'mce': float(mce),
        'bins': bin_data,
        'interpretation': {
            'brier': 'Lower is better (0 = perfect)',
            'ece': 'Lower is better (<0.1 = well-calibrated)',
            'mce': 'Maximum calibration error in any bin',
        }
    }


def main():
    # Paths
    pdf_test_dir = Path(r'B:\sentinel\data\evaluation_datasets\ember2024_data\PDF_test')
    challenge_dir = Path(r'B:\sentinel\data\evaluation_datasets\ember2024_data\challenge')
    output_dir = Path(r'B:\sentinel\data\calibration\ember_results')
    output_dir.mkdir(parents=True, exist_ok=True)
    
    print("=" * 70)
    print("EMBER2024 CALIBRATION ANALYSIS")
    print("=" * 70)
    
    # Load PDF test samples (balanced dataset)
    print("\n[1] Loading PDF test samples...")
    pdf_samples = load_ember_samples(pdf_test_dir, max_samples=5000)  # Start with subset
    
    benign = sum(1 for s in pdf_samples if s.label == 0)
    malicious = sum(1 for s in pdf_samples if s.label == 1)
    print(f"    Loaded {len(pdf_samples)} samples: {benign} benign, {malicious} malicious")
    
    # Analyze samples
    print("\n[2] Computing scores for PDF samples...")
    scores, labels, details = analyze_samples(pdf_samples)
    
    # Score distribution
    print("\n[3] Score distribution:")
    print(f"    Benign samples:    min={scores[labels==0].min():.3f}, "
          f"max={scores[labels==0].max():.3f}, mean={scores[labels==0].mean():.3f}")
    print(f"    Malicious samples: min={scores[labels==1].min():.3f}, "
          f"max={scores[labels==1].max():.3f}, mean={scores[labels==1].mean():.3f}")
    
    # Compute ROC curve
    print("\n[4] Computing ROC curve...")
    roc_results = compute_roc_curve(scores, labels)
    print(f"    AUC: {roc_results['auc']:.4f}")
    print(f"    Optimal threshold (Youden): {roc_results['optimal_youden']['threshold']:.3f}")
    print(f"      - TPR (Detection): {roc_results['optimal_youden']['tpr']:.1%}")
    print(f"      - FPR: {roc_results['optimal_youden']['fpr']:.1%}")
    print(f"      - FNR (Missed): {roc_results['optimal_youden']['fnr']:.1%}")
    
    print("\n    Thresholds at target FP rates:")
    for name, data in roc_results['thresholds_at_fp'].items():
        print(f"      {name}: threshold={data['threshold']:.3f}, "
              f"TPR={data['tpr']:.1%}, FPR={data['fpr']:.1%}, FNR={data['fnr']:.1%}")
    
    # Calibration metrics
    print("\n[5] Computing calibration metrics...")
    cal_results = compute_calibration_metrics(scores, labels)
    print(f"    Brier Score: {cal_results['brier_score']:.4f}")
    print(f"    ECE: {cal_results['ece']:.4f}")
    print(f"    MCE: {cal_results['mce']:.4f}")
    
    # Save results
    results = {
        'dataset': 'EMBER2024 PDF Test',
        'samples': len(pdf_samples),
        'benign': benign,
        'malicious': malicious,
        'roc': roc_results,
        'calibration': cal_results,
    }
    
    output_file = output_dir / 'pdf_calibration_results.json'
    with open(output_file, 'w') as f:
        json.dump(results, f, indent=2)
    print(f"\n[6] Results saved to: {output_file}")
    
    # Test on challenge set
    print("\n" + "=" * 70)
    print("VALIDATION ON CHALLENGE SET (Evasive Malware)")
    print("=" * 70)
    
    print("\n[7] Loading challenge samples...")
    challenge_samples = load_ember_samples(challenge_dir, max_samples=2000)
    print(f"    Loaded {len(challenge_samples)} evasive malware samples")
    
    print("\n[8] Computing scores for challenge samples...")
    challenge_scores, challenge_labels, _ = analyze_samples(challenge_samples)
    
    # Test detection at different thresholds
    print("\n[9] Detection rates on evasive malware:")
    for name, data in roc_results['thresholds_at_fp'].items():
        thresh = data['threshold']
        detected = np.sum(challenge_scores >= thresh)
        detection_rate = detected / len(challenge_scores)
        print(f"    At {name} threshold ({thresh:.3f}): "
              f"{detected}/{len(challenge_scores)} detected ({detection_rate:.1%})")
    
    # Optimal threshold
    opt_thresh = roc_results['optimal_youden']['threshold']
    opt_detected = np.sum(challenge_scores >= opt_thresh)
    opt_rate = opt_detected / len(challenge_scores)
    print(f"    At optimal threshold ({opt_thresh:.3f}): "
          f"{opt_detected}/{len(challenge_scores)} detected ({opt_rate:.1%})")
    
    print("\n" + "=" * 70)
    print("RECOMMENDATIONS")
    print("=" * 70)
    
    # Recommend threshold based on security posture
    print("\n  Security-focused (minimize FN, accept more FP):")
    fpr_10 = roc_results['thresholds_at_fp']['fpr_10pct']
    print(f"    Threshold: {fpr_10['threshold']:.3f}")
    print(f"    Expected: {fpr_10['tpr']:.1%} detection, {fpr_10['fpr']:.1%} FP rate")
    
    print("\n  Balanced (Youden optimal):")
    print(f"    Threshold: {opt_thresh:.3f}")
    print(f"    Expected: {roc_results['optimal_youden']['tpr']:.1%} detection, "
          f"{roc_results['optimal_youden']['fpr']:.1%} FP rate")
    
    print("\n  Low-FP focused (minimize FP, accept more FN):")
    fpr_1 = roc_results['thresholds_at_fp']['fpr_1pct']
    print(f"    Threshold: {fpr_1['threshold']:.3f}")
    print(f"    Expected: {fpr_1['tpr']:.1%} detection, {fpr_1['fpr']:.1%} FP rate")


if __name__ == '__main__':
    main()
