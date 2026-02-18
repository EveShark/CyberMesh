"""Phase 2 performance benchmark for OSS ingest + event analysis."""
from __future__ import annotations

import argparse
import json
import platform
import statistics
import time
from dataclasses import dataclass
from pathlib import Path
import sys
from typing import Any, Dict, List, Optional, Tuple

BASE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE_DIR))

from sentinel.agents import SentinelOrchestrator
from sentinel.ingest import OSSAdapter, load_adapter_spec, load_records
CONFIG_DIR = BASE_DIR / "sentinel" / "config"
INCOMING_DIR = BASE_DIR / "oss_outputs" / "incoming"
REPORTS_DIR = BASE_DIR / "reports"


@dataclass
class SampleSpec:
    adapter: str
    file_name: str
    fmt: str
    label: str


SAMPLES: List[SampleSpec] = [
    SampleSpec("adapter_mcp_scan.json", "mcp_scan_output.json", "json", "mcp-scan"),
    SampleSpec("adapter_skill_scanner.json", "skill_scanner_output.json", "json", "skill-scanner"),
    SampleSpec("adapter_trufflehog.json", "trufflehog_output.json", "json", "trufflehog"),
    SampleSpec("adapter_rebuff.json", "rebuff_output.json", "json", "rebuff"),
    SampleSpec("adapter_falco_findings.json", "falco_output.json", "json", "falco-findings"),
    SampleSpec("adapter_bzar_rules.json", "bzar_output.log", "ndjson", "bzar"),
    SampleSpec("adapter_zeek_flow.json", "zeek_conn.log", "ndjson", "zeek-conn"),
]


def _percentile(values: List[float], pct: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    k = max(int(round((pct / 100.0) * (len(ordered) - 1))), 0)
    return ordered[min(k, len(ordered) - 1)]


def _stats(values: List[float]) -> Dict[str, float]:
    if not values:
        return {
            "count": 0,
            "min": 0.0,
            "mean": 0.0,
            "p50": 0.0,
            "p95": 0.0,
            "max": 0.0,
        }
    return {
        "count": len(values),
        "min": min(values),
        "mean": statistics.mean(values),
        "p50": _percentile(values, 50),
        "p95": _percentile(values, 95),
        "max": max(values),
    }


def _load_events(spec_path: Path, input_path: Path, fmt: str) -> Tuple[List[Any], List[str]]:
    spec = load_adapter_spec(spec_path)
    records, errors = load_records(
        input_path,
        fmt,
        spec.limits,
        records_path=spec.records_path,
    )
    if errors:
        return [], errors
    adapter = OSSAdapter(spec)
    events, event_errors = adapter.records_to_events(records, tenant_id="tenant-1")
    return events, event_errors


def main() -> int:
    parser = argparse.ArgumentParser(description="Phase 2 OSS ingest benchmark")
    parser.add_argument("--iterations", type=int, default=5, help="Analysis iterations per event")
    parser.add_argument("--warmup", type=int, default=1, help="Warmup runs per event")
    parser.add_argument("--sequential", action="store_true", help="Run orchestrator sequentially")
    parser.add_argument("--output", default=None, help="Output report path (json)")
    args = parser.parse_args()

    REPORTS_DIR.mkdir(parents=True, exist_ok=True)
    output_path = Path(args.output) if args.output else REPORTS_DIR / "phase2_benchmark.json"

    orchestrator = SentinelOrchestrator(
        enable_threat_intel=False,
        enable_llm=False,
        enable_fast_path=True,
        sequential=args.sequential,
    )

    run_meta = {
        "timestamp": time.strftime("%Y-%m-%d %H:%M:%S"),
        "iterations": args.iterations,
        "warmup": args.warmup,
        "sequential": args.sequential,
        "python": platform.python_version(),
        "platform": platform.platform(),
    }

    results: Dict[str, Any] = {"meta": run_meta, "samples": [], "errors": []}
    overall_times: List[float] = []

    for sample in SAMPLES:
        spec_path = CONFIG_DIR / sample.adapter
        input_path = INCOMING_DIR / sample.file_name
        if not input_path.exists():
            results["samples"].append({
                "label": sample.label,
                "status": "missing_input",
                "file": str(input_path),
            })
            continue

        events, errors = _load_events(spec_path, input_path, sample.fmt)
        if errors:
            results["samples"].append({
                "label": sample.label,
                "status": "ingest_error",
                "errors": errors,
            })
            continue
        if not events:
            results["samples"].append({
                "label": sample.label,
                "status": "no_events",
            })
            continue

        sample_times: List[float] = []
        failures: List[str] = []

        for event in events:
            # Warmup runs (not counted)
            for _ in range(max(args.warmup, 0)):
                orchestrator.analyze_event(event)
            for _ in range(max(args.iterations, 1)):
                t0 = time.perf_counter()
                try:
                    orchestrator.analyze_event(event)
                except Exception as exc:  # pylint: disable=broad-except
                    failures.append(str(exc))
                    continue
                elapsed = (time.perf_counter() - t0) * 1000
                sample_times.append(elapsed)
                overall_times.append(elapsed)

        results["samples"].append({
            "label": sample.label,
            "events": len(events),
            "status": "ok" if not failures else "partial",
            "analysis_ms": _stats(sample_times),
            "failures": failures[:5],
        })

    results["overall"] = {
        "analysis_ms": _stats(overall_times),
    }

    output_path.write_text(json.dumps(results, indent=2), encoding="utf-8")
    print(f"Benchmark written to: {output_path}")
    print(json.dumps(results["overall"], indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
