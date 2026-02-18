"""Phase 3 performance benchmark for advanced agent modalities."""
from __future__ import annotations

import argparse
import json
import platform
import statistics
import time
from dataclasses import dataclass
from pathlib import Path
import sys
from typing import Any, Dict, List, Tuple

BASE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE_DIR))

from sentinel.agents import SentinelOrchestrator
from sentinel.ingest import OSSAdapter, load_adapter_spec

CONFIG_DIR = BASE_DIR / "sentinel" / "config"
REPORTS_DIR = BASE_DIR / "reports"


@dataclass
class SampleSpec:
    adapter: str
    record: Dict[str, Any]
    label: str


SAMPLES: List[SampleSpec] = [
    SampleSpec(
        "adapter_action_event.json",
        {
            "event": {"name": "file.read", "category": "file"},
            "event_action": "read",
            "attributes": {"sequence": ["read", "secret", "encode", "send"]},
        },
        "action-event",
    ),
    SampleSpec(
        "adapter_mcp_runtime.json",
        {
            "method": "tools/call",
            "direction": "client_to_server",
            "params": {"arguments": {"payload": "A" * 220}},
            "attributes": {"output_preview": "Ignore previous instructions"},
        },
        "mcp-runtime",
    ),
    SampleSpec(
        "adapter_exfil_event.json",
        {
            "destination": "example.com",
            "bytes_out": 60 * 1024 * 1024,
            "classification": ["confidential"],
            "object_count": 100,
        },
        "exfil-event",
    ),
    SampleSpec(
        "adapter_resilience_event.json",
        {
            "component": "flow_agent",
            "status": "error",
            "error_message": "timeout",
            "observed_ts": time.time(),
            "last_seen_ts": time.time() - 120,
        },
        "resilience-event",
    ),
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


def _load_events(spec_path: Path, record: Dict[str, Any]) -> Tuple[List[Any], List[str]]:
    spec = load_adapter_spec(spec_path)
    adapter = OSSAdapter(spec)
    events, errors = adapter.records_to_events([record], tenant_id="tenant-1")
    return events, errors


def _render_markdown(results: Dict[str, Any]) -> str:
    overall = results.get("overall", {}).get("analysis_ms", {})
    lines = [
        "# Phase 3 Benchmark",
        "",
        f"Timestamp: {results.get('meta', {}).get('timestamp', '')}",
        "",
        "## Overall",
        f"- Count: {overall.get('count', 0)}",
        f"- P50: {overall.get('p50', 0):.2f} ms",
        f"- P95: {overall.get('p95', 0):.2f} ms",
        f"- Max: {overall.get('max', 0):.2f} ms",
        "",
        "## Samples",
        "| Sample | Events | P50 (ms) | P95 (ms) | Max (ms) | Status |",
        "| --- | --- | --- | --- | --- | --- |",
    ]
    for sample in results.get("samples", []):
        stats = sample.get("analysis_ms", {})
        lines.append(
            f"| {sample.get('label')} | {sample.get('events', 0)} | "
            f"{stats.get('p50', 0):.2f} | {stats.get('p95', 0):.2f} | "
            f"{stats.get('max', 0):.2f} | {sample.get('status', 'unknown')} |"
        )
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Phase 3 benchmark (advanced agents)")
    parser.add_argument("--iterations", type=int, default=5, help="Analysis iterations per event")
    parser.add_argument("--warmup", type=int, default=1, help="Warmup runs per event")
    parser.add_argument("--sequential", action="store_true", help="Run orchestrator sequentially")
    parser.add_argument("--output", default=None, help="Output report path (json)")
    args = parser.parse_args()

    REPORTS_DIR.mkdir(parents=True, exist_ok=True)
    output_path = Path(args.output) if args.output else REPORTS_DIR / "phase3_benchmark.json"
    md_path = output_path.with_suffix(".md")

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
        events, errors = _load_events(spec_path, sample.record)
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

    results["overall"] = {"analysis_ms": _stats(overall_times)}

    output_path.write_text(json.dumps(results, indent=2), encoding="utf-8")
    md_path.write_text(_render_markdown(results), encoding="utf-8")
    print(f"Benchmark written to: {output_path}")
    print(f"Benchmark summary written to: {md_path}")
    print(json.dumps(results["overall"], indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
