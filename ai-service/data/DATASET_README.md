# Test Datasets for OCI/GKE Deployment

**21 Testing Scenarios | 5.6M Rows | 4-Phase Validation Plan**

---

## Quick Overview

This directory contains **21 ready-to-use datasets** (2.60 GB, 5.5M rows total). All datasets exported and verified from PostgreSQL database.

**Testing Goal:** Validate DDoS detection model across multiple attack types, distributions, and edge cases before production deployment.

---

## Directory Structure

```
test_datasets_ready/
├── README.md                                            (This file)
│
├── PHASE 0: Smoke Test (10K rows, 4.8 MB)
│   └── phase0_smoke_test_10k_50_50.csv
│
├── PHASE 1: Core Baselines (2.5M rows, 1.48 GB)
│   ├── phase1_production_baseline_500k_62_38.csv       (500K rows, 258 MB)
│   ├── phase1_test_v2_500k_62_38.csv                   (500K rows, 299 MB)
│   ├── phase1_balanced_500k_50_50.csv                  (500K rows, 342 MB)
│   ├── phase1_benign_only_500k_pure.csv                (500K rows, 281 MB)
│   └── phase1_attack_only_500k_pure.csv                (500K rows, 293 MB)
│
├── PHASE 2: Robustness (1.5M rows, 878 MB)
│   ├── phase2_multiclass_dos_300k_26_74.csv            (300K rows, 142 MB)
│   ├── phase2_ddos_external_100k_39_61.csv             (100K rows, 43 MB)
│   ├── phase2_portscan_100k_88_12.csv                  (100K rows, 42 MB)
│   ├── phase2_apt_infiltration_100k_extreme_imbalance  (100K rows, 43 MB)
│   ├── phase2_realistic_90_10_500k.csv                 (500K rows, 305 MB)
│   └── phase2_moderate_70_30_500k.csv                  (500K rows, 302 MB)
│
├── PHASE 3: Attack Diversity (424K rows, 123 MB)
│   ├── phase3_brute_force_13k_attack_only.csv          (13K rows, 2.8 MB)
│   ├── phase3_dns_spoofing_179k_attack_only.csv        (179K rows, 38 MB)
│   ├── phase3_slowloris_23k_attack_only.csv            (23K rows, 5.4 MB)
│   ├── phase3_sql_injection_5k_attack_only.csv         (5K rows, 1.1 MB)
│   ├── phase3_xss_attack_4k_attack_only.csv            (4K rows, 0.8 MB)
│   └── phase3_reconnaissance_200k_mixed.csv            (200K rows, 75 MB)
│
└── PHASE 4: Domain Validation (923K rows, 186 MB)
    ├── phase4_vuln_scan_373k_attack_only.csv           (373K rows, 80 MB)
    ├── phase4_sdn_ddos_500k_50_50.csv                  (500K rows, 94 MB)
    └── phase4_phishing_urls_50k_41_59.csv              (50K rows, 12 MB)

TOTAL: 21 files, 5.5M rows, 2.60 GB
```

---

## 21 Testing Scenarios

### **PHASE 0: Smoke Test** (1 scenario)
Quick 30-second validation before full testing.

| File | Rows | Split | Source | Use Case |
|------|------|-------|--------|----------|
| `phase0_smoke_test_10k_50_50.csv` ✅ | 10K | 50/50 | **Ready** | CI/CD fast validation |

---

### **PHASE 1: Core Baselines** (5 scenarios)
Establish baseline metrics and measure false positive/negative rates.

| File | Rows | Split | Source | Use Case |
|------|------|-------|--------|----------|
| `phase1_production_baseline_500k_62_38.csv` ✅ | 500K | 19/81 | **Ready** | Production baseline (sequential read) |
| `phase1_test_v2_500k_62_38.csv` ✅ | 500K | 51/49 | **Ready** | Validation consistency |
| `phase1_balanced_500k_50_50.csv` ✅ | 500K | 49/51 | **Ready** | Distribution invariance |
| `phase1_benign_only_500k_pure.csv` ✅ | 500K | 100/0 | **Ready** | False positive rate (normal ops) |
| `phase1_attack_only_500k_pure.csv` ✅ | 500K | 0/100 | **Ready** | False negative rate |

**Expected Results:**
- Production baseline: 90-95% accuracy
- Benign-only: <1% false positive rate
- Attack-only: <5% false negative rate

---

### **PHASE 2: Robustness & Edge Cases** (6 scenarios)

| File | Rows | Split | Source | Use Case |
|------|------|-------|--------|----------|
| `phase2_multiclass_dos_300k_26_74.csv` ✅ | 300K | 26/74 | **Ready** | Multi-class DoS detection (4 types) |
| `phase2_ddos_external_100k_39_61.csv` ✅ | 100K | 39/61 | **Ready** | Cross-dataset generalization |
| `phase2_portscan_100k_88_12.csv` ✅ | 100K | 88/12 | **Ready** | Rare event detection |
| `phase2_apt_infiltration_100k_extreme_imbalance.csv` ✅ | 100K | 100/0 | **Ready** | Extreme imbalance (18 attacks only!) |
| `phase2_realistic_90_10_500k.csv` ✅ | 500K | 90/10 | **Ready** | Realistic attack rate |
| `phase2_moderate_70_30_500k.csv` ✅ | 500K | 70/30 | **Ready** | Higher attack frequency |

**Expected Results:**
- Multi-class: Model should identify specific attack types or default to "attack"
- Extreme imbalance: Catch at least 50% of 18 infiltration samples
- Realistic rates: Same accuracy as baseline

---

### **PHASE 3: Attack Type Diversity** (6 scenarios)

| File | Rows | Split | Source | Use Case |
|------|------|-------|--------|----------|
| `phase3_reconnaissance_200k_mixed.csv` ✅ | 200K | 88/12 | **Ready** | Early-stage detection (port scan + DNS) |
| `phase3_slowloris_23k_attack_only.csv` ✅ | 23K | 0/100 | **Ready** | Specific DDoS variant |
| `phase3_brute_force_13k_attack_only.csv` ✅ | 13K | 0/100 | **Ready** | Credential attacks |
| `phase3_dns_spoofing_179k_attack_only.csv` ✅ | 179K | 0/100 | **Ready** | DNS hijacking |
| `phase3_sql_injection_5k_attack_only.csv` ✅ | 5K | 0/100 | **Ready** | Database attacks |
| `phase3_xss_attack_4k_attack_only.csv` ✅ | 4K | 0/100 | **Ready** | Web XSS vectors |

**Expected Results:**
- Pure attack files: Model should detect or fail gracefully (domain mismatch acceptable)
- Reconnaissance: Catch port scans before actual attacks

---

### **PHASE 4: Domain & Infrastructure Validation** (3 scenarios)

| File | Rows | Split | Source | Use Case |
|------|------|-------|--------|----------|
| `phase4_sdn_ddos_500k_50_50.csv` ✅ | 500K | 50/50 | **Ready** | SDN infrastructure (26 features) |
| `phase4_phishing_urls_50k_41_59.csv` ✅ | 50K | 41/59 | **Ready** | Wrong domain sanity check (URL features) |
| `phase4_vuln_scan_373k_attack_only.csv` ✅ | 373K | 0/100 | **Ready** | Security scanning |

**Expected Results:**
- SDN: Different feature set, test feature compatibility
- Phishing: Should fail (wrong domain = URL features, not network flows)

---

## Label Information

### **Binary Labels:**
- **Benign/BENIGN/Normal/0:** Legitimate traffic
- **DDoS/ddos/Attack/1:** Attack traffic

### **Multi-Class Labels (cic_ids_wednesday):**
- BENIGN (77,656 rows - 25.9%)
- DoS Hulk (211,049 rows - 70.4%)
- DoS slowloris (5,796 rows - 1.9%)
- DoS Slowhttptest (5,499 rows - 1.8%)

### **Pure Attack Files (web_attacks_raw):**
No labels - attack type identified by filename. All rows are attacks (100%).

---

## Features

**CIC-IDS datasets:** 85 columns (network flow features)
- Flow metrics: duration, packets, bytes
- Timing: Inter-arrival times (IAT)
- TCP flags: SYN, ACK, FIN, RST, PSH, URG
- Statistics: min, max, mean, std for packet sizes

**Web attack datasets:** 39 columns
- Different feature set from training data
- May not work with current model (expected)

**SDN dataset:** 26 columns
- SDN-specific features
- Tests model's feature flexibility

**Phishing dataset:** 56 columns (URL features)
- Lexical features, not network flows
- Sanity check (should fail gracefully)

---

## Testing Execution Order

### **Priority 1 - Must Run First:**
```bash
1. smoke_test_10k.csv              (30 seconds)
2. benign_only_500k.csv            (Measure FP rate)
3. attack_only_500k.csv            (Measure FN rate)
4. production_baseline_500k.csv    (Establish baseline)
5. balanced_test_500k.csv          (Distribution check)
```

### **Priority 2 - Core Validation:**
```bash
6. test_v2_500k.csv
7. cic_ids_wednesday.csv
8. cic_ids_friday_ddos.csv
9. mixed_90_benign.csv
```

### **Priority 3 - Edge Cases:**
```bash
10-15. Remaining Phase 2 & Phase 3 files
```

### **Priority 4 - Optional:**
```bash
16-21. Domain validation files
```

---

## Expected Timing (GKE)

- **Phase 0:** 30 seconds
- **Phase 1:** 2-3 hours (5 files × 500K rows)
- **Phase 2:** 2-3 hours (6 files × 100-500K rows)
- **Phase 3:** 1-2 hours (6 files × 5-200K rows)
- **Phase 4:** 2-3 hours (3 files × 50-500K rows)

**Total: 7-11 hours for all 21 scenarios**

---

## Acceptance Criteria

### **Pass Criteria:**
- **Phase 1 Baseline:** ≥90% accuracy
- **Benign-only:** <1% false positive rate
- **Attack-only:** <5% false negative rate
- **Multi-class:** Model detects as "attack" even if can't classify specific type
- **Edge cases:** Degrades gracefully (doesn't crash)

### **Fail Criteria:**
- Production baseline <85% accuracy
- False positive rate >5%
- False negative rate >10%
- System crashes on any dataset

### **Expected Failures (OK):**
- Phishing URLs (wrong domain - URL features vs network flows)
- Web attacks (different feature set, may not match training data)

---

## Next Steps

1. **Upload to OCI bucket** or **mount as PVC in GKE** ✅ Ready
2. **Run Phase 0** (smoke test) first
3. **If Phase 0 passes:** Run Phase 1 (core baselines)
4. **If Phase 1 passes:** Continue with Phase 2-4
5. **Document results** for each scenario

---

## File Verification

**All 21 files exported and verified:**
- Row counts: ✅ All match expected
- File integrity: ✅ All files readable
- Label distributions: ✅ Verified accurate
- Total size: 2.60 GB
- Total rows: 5,457,830

---

## Notes

- All CSV files use UTF-8 encoding
- Column names are lowercase with underscores
- Missing values already handled (filled with 0)
- Numeric columns stored as TEXT in database (convert during inference)
- Web attack files have no label column (attack type = filename)

---

**Last Updated:** October 17, 2025  
**Status:** ✅ All 21 files exported and verified  
**Model:** ddos_binary_detector_v2.pkl (LightGBM)  
**Training Data:** 20.4M rows (62% benign / 38% DDoS)  
**Training Accuracy:** 99.98% (AUC 1.0)  
**Total Test Data:** 5.5M rows across 21 scenarios (2.60 GB)
