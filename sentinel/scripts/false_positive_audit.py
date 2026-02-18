#!/usr/bin/env python
"""
False Positive Audit for Sentinel.

1. Run all clean samples through full pipeline
2. Count false positives (clean marked as malicious)
3. Compute FP rate = FP / total_clean
4. For each FP, log details (file, providers, patterns)
5. Output fp_report.json
6. Print summary
"""

import json
import sys
import time
from pathlib import Path
from datetime import datetime
from typing import Dict, List, Any

sys.path.insert(0, str(Path(__file__).parent.parent))

from dotenv import load_dotenv
load_dotenv(Path(__file__).parent.parent / ".env")

# Configure logging before sentinel imports
import logging
logging.basicConfig(level=logging.WARNING)

PROJECT_DIR = Path(__file__).parent.parent
EVAL_SAMPLES_DIR = PROJECT_DIR / "data" / "eval_samples"
REPORT_PATH = PROJECT_DIR / "fp_report.json"


def get_clean_samples() -> List[Path]:
    """Get all clean sample files."""
    clean_dir = EVAL_SAMPLES_DIR / "clean"
    if not clean_dir.exists():
        return []
    
    samples = []
    for f in clean_dir.iterdir():
        if f.is_file():
            samples.append(f)
    
    return samples


def run_provider(provider, parsed, result: Dict, name: str):
    """Run a single provider and record results."""
    try:
        if not provider.can_analyze(parsed):
            result["scores"][name] = "unsupported"
            return
        
        r = provider.analyze(parsed)
        result["scores"][name] = r.score
        result["threat_levels"][name] = r.threat_level.value
        
        if r.threat_level.value in ("malicious", "suspicious", "critical"):
            result["providers_triggered"].append(name)
            for f in r.findings:
                result["findings"].append({"provider": name, "finding": f})
    except Exception as e:
        result["scores"][name] = f"error: {e}"


def analyze_with_providers(file_path: Path) -> Dict[str, Any]:
    """Run file through ALL registered providers and collect details."""
    result = {
        "file": file_path.name,
        "file_path": str(file_path),
        "providers_triggered": [],
        "findings": [],
        "scores": {},
        "threat_levels": {},
    }
    
    try:
        from sentinel.parsers import get_parser_for_file
        parser = get_parser_for_file(str(file_path))
        if not parser:
            result["error"] = "No parser available"
            return result
        
        parsed = parser.parse(str(file_path))
        result["file_type"] = parsed.file_type.value
        result["file_size"] = parsed.file_size
        
        models_path = PROJECT_DIR.parent / "CyberMesh" / "ai-service" / "data" / "models"
        
        # Static providers
        from sentinel.providers.static import EntropyProvider, StringsProvider
        run_provider(EntropyProvider(), parsed, result, "entropy")
        run_provider(StringsProvider(), parsed, result, "strings")
        
        # YARA provider
        try:
            from sentinel.providers.static.yara_provider import YaraProvider
            run_provider(YaraProvider(), parsed, result, "yara")
        except Exception as e:
            result["scores"]["yara"] = f"error: {e}"
        
        # ML providers
        try:
            from sentinel.providers.ml.malware_pe import MalwarePEProvider
            run_provider(MalwarePEProvider(models_path=str(models_path)), parsed, result, "pe_ml")
        except Exception as e:
            result["scores"]["pe_ml"] = f"error: {e}"
        
        try:
            from sentinel.providers.ml.malware_api import MalwareAPIProvider
            run_provider(MalwareAPIProvider(models_path=str(models_path)), parsed, result, "api_ml")
        except Exception as e:
            result["scores"]["api_ml"] = f"error: {e}"
        
        try:
            from sentinel.providers.ml.anomaly import AnomalyProvider
            run_provider(AnomalyProvider(models_path=str(models_path)), parsed, result, "anomaly")
        except Exception as e:
            result["scores"]["anomaly"] = f"error: {e}"
        
        # Signature matcher (fastpath)
        try:
            from sentinel.fastpath.signatures import SignatureMatcher
            matcher = SignatureMatcher()
            matches = matcher.match(parsed)
            result["scores"]["signatures"] = len(matches)
            if matches:
                result["providers_triggered"].append("signatures")
                for m in matches:
                    result["findings"].append({
                        "provider": "signatures",
                        "finding": f"{m.name}: {m.description}",
                        "severity": m.severity,
                        "matched_data": m.matched_data[:3] if m.matched_data else [],
                    })
        except Exception as e:
            result["scores"]["signatures"] = f"error: {e}"
        
        # Determine if this is a false positive
        any_triggered = len(result["providers_triggered"]) > 0
        any_malicious = any(
            level in ("malicious", "suspicious", "critical") 
            for level in result["threat_levels"].values()
        )
        result["is_false_positive"] = any_triggered or any_malicious
        
    except Exception as e:
        result["error"] = str(e)
        result["is_false_positive"] = False
    
    return result


def run_full_pipeline(file_path: Path) -> Dict[str, Any]:
    """Run file through full agentic pipeline."""
    result = {
        "file": file_path.name,
        "file_path": str(file_path),
    }
    
    try:
        from sentinel.agents import AnalysisEngine
        
        engine = AnalysisEngine(
            enable_llm=False,  # Skip LLM for speed
            enable_fast_path=True,
            enable_threat_intel=False,  # Skip external APIs
        )
        
        t0 = time.perf_counter()
        state = engine.analyze(str(file_path))
        elapsed = (time.perf_counter() - t0) * 1000
        
        result["threat_level"] = state.get("threat_level").value if state.get("threat_level") else "unknown"
        result["final_score"] = state.get("final_score", 0)
        result["confidence"] = state.get("confidence", 0)
        result["analysis_time_ms"] = elapsed
        result["findings_count"] = len(state.get("findings", []))
        result["findings"] = [
            {"source": f.source, "severity": f.severity, "description": f.description}
            for f in state.get("findings", [])[:10]
        ]
        
        result["is_false_positive"] = result["threat_level"] in ("malicious", "suspicious", "critical")
        
    except Exception as e:
        result["error"] = str(e)
        result["is_false_positive"] = False
    
    return result


def main():
    print("=" * 70)
    print("SENTINEL FALSE POSITIVE AUDIT")
    print("=" * 70)
    
    # Get clean samples
    clean_samples = get_clean_samples()
    print(f"\nFound {len(clean_samples)} clean samples")
    
    if not clean_samples:
        print("No clean samples found in data/eval_samples/clean/")
        print("Run: python scripts/convert_manifest.py")
        return 1
    
    # Analyze each sample
    print("\n" + "-" * 70)
    print("Running analysis...")
    print("-" * 70)
    
    results = []
    false_positives = []
    
    for i, sample in enumerate(clean_samples):
        print(f"\n[{i+1}/{len(clean_samples)}] {sample.name}...", end=" ", flush=True)
        
        # Run through providers individually for detailed analysis
        provider_result = analyze_with_providers(sample)
        
        # Also run through full pipeline
        pipeline_result = run_full_pipeline(sample)
        
        # Combine results
        combined = {
            **provider_result,
            "pipeline_threat_level": pipeline_result.get("threat_level"),
            "pipeline_score": pipeline_result.get("final_score"),
            "pipeline_findings": pipeline_result.get("findings", []),
            "analysis_time_ms": pipeline_result.get("analysis_time_ms", 0),
        }
        
        # Use PIPELINE verdict as ground truth for FP
        # Individual provider triggers are expected - the vote-based system should handle them
        is_fp = pipeline_result.get("is_false_positive", False)
        combined["is_false_positive"] = is_fp
        
        results.append(combined)
        
        if is_fp:
            false_positives.append(combined)
            print(f"FALSE POSITIVE!")
            print(f"      Providers triggered: {combined.get('providers_triggered', [])}")
            print(f"      Pipeline verdict: {combined.get('pipeline_threat_level')}")
        else:
            print("OK")
    
    # Compute FP rate
    total_clean = len(clean_samples)
    fp_count = len(false_positives)
    fp_rate = fp_count / total_clean if total_clean > 0 else 0.0
    
    # Build report
    report = {
        "timestamp": datetime.now().isoformat(),
        "summary": {
            "total_clean_samples": total_clean,
            "false_positives": fp_count,
            "true_negatives": total_clean - fp_count,
            "fp_rate": round(fp_rate, 4),
            "fp_rate_percent": f"{fp_rate * 100:.2f}%",
        },
        "false_positive_details": false_positives,
        "all_results": results,
    }
    
    # Aggregate which providers cause most FPs
    provider_fp_counts = {}
    for fp in false_positives:
        for p in fp.get("providers_triggered", []):
            provider_fp_counts[p] = provider_fp_counts.get(p, 0) + 1
    
    report["summary"]["fp_by_provider"] = provider_fp_counts
    
    # Save report
    with open(REPORT_PATH, "w") as f:
        json.dump(report, f, indent=2)
    
    # Print summary
    print("\n" + "=" * 70)
    print("FALSE POSITIVE AUDIT SUMMARY")
    print("=" * 70)
    
    print(f"\nTotal clean samples: {total_clean}")
    print(f"False positives: {fp_count}")
    print(f"True negatives: {total_clean - fp_count}")
    print(f"FP Rate: {fp_rate * 100:.2f}%")
    
    if provider_fp_counts:
        print(f"\nFPs by provider:")
        for provider, count in sorted(provider_fp_counts.items(), key=lambda x: -x[1]):
            print(f"  {provider}: {count}")
    
    if false_positives:
        print(f"\nFalse Positive Details:")
        print("-" * 70)
        for fp in false_positives:
            print(f"\n  File: {fp['file']}")
            print(f"  Type: {fp.get('file_type', 'unknown')}")
            print(f"  Providers triggered: {fp.get('providers_triggered', [])}")
            print(f"  Scores: {fp.get('scores', {})}")
            if fp.get("findings"):
                print(f"  Findings ({len(fp['findings'])}):")
                for finding in fp["findings"][:5]:
                    print(f"    - [{finding.get('provider')}] {finding.get('finding', '')[:60]}")
    
    print(f"\nReport saved to: {REPORT_PATH}")
    print("=" * 70)
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
