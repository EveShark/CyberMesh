#!/usr/bin/env python3
"""
Phase-4 SLA evaluator (standalone).

Goal:
- Produce explicit pass/fail results from real gateway artifacts (NDJSON outputs)
  plus a small number of targeted replays to measure wall-clock throughput.
- Avoid silent failures: missing metrics are surfaced as failures.

This script is intentionally dependency-free (stdlib only) and safe to run locally.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple

# Ensure `import sentinel` works when running via `python scripts/phase4_sla_eval.py`.
_REPO_ROOT = Path(__file__).resolve().parents[1]
if str(_REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(_REPO_ROOT))


def _read_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def _iter_ndjson(path: Path) -> Iterable[dict]:
    with path.open("rb") as f:
        for raw in f:
            line = raw.strip()
            if not line:
                continue
            try:
                obj = json.loads(line.decode("utf-8"))
            except Exception:
                # Malformed lines should be treated as an error by caller.
                yield {"_type": "parse_error", "_raw": line[:200].decode("utf-8", errors="ignore")}
                continue
            if isinstance(obj, dict):
                yield obj
            else:
                yield {"_type": "parse_error", "_raw": str(obj)[:200]}


def _p95(values: List[float]) -> Optional[float]:
    if not values:
        return None
    xs = sorted(values)
    # Nearest-rank p95.
    idx = int((0.95 * (len(xs) - 1)))
    return float(xs[idx])


@dataclass(frozen=True)
class SLAResult:
    key: str
    metric: str
    value: Optional[float]
    threshold: Optional[float]
    passed: bool
    evidence: str
    notes: str = ""

    def to_dict(self) -> dict:
        return {
            "key": self.key,
            "metric": self.metric,
            "value": self.value,
            "threshold": self.threshold,
            "passed": self.passed,
            "evidence": self.evidence,
            "notes": self.notes,
        }


def _latency_p95_from_report(path: Path) -> Tuple[Optional[float], Dict[str, Any]]:
    values: List[float] = []
    parse_errors = 0
    records = 0
    for obj in _iter_ndjson(path):
        records += 1
        if obj.get("_type") == "parse_error":
            parse_errors += 1
            continue
        if obj.get("_type") == "ingest_summary":
            continue
        v = obj.get("analysis_time_ms")
        # Some gateway artifacts wrap results for file/dir analysis as:
        # { "file_path": "...", "result": { "analysis_time_ms": ... } }
        if v is None and isinstance(obj.get("result"), dict):
            v = obj["result"].get("analysis_time_ms")
        if v is None:
            continue
        try:
            values.append(float(v))
        except Exception:
            continue
    return _p95(values), {"records": records, "samples": len(values), "parse_errors": parse_errors}


def _run_gateway(cmd: List[str], *, cwd: Path) -> Tuple[int, float, str, str]:
    t0 = time.perf_counter()
    p = subprocess.run(cmd, cwd=str(cwd), capture_output=True, text=True)
    elapsed_s = time.perf_counter() - t0
    return p.returncode, elapsed_s, p.stdout, p.stderr


def _parse_stdout_json(stdout: str) -> Optional[dict]:
    stdout = stdout.strip()
    if not stdout:
        return None
    try:
        return json.loads(stdout)
    except Exception:
        return None


def _count_telemetry_jsonl_ingest_errors(path: Path) -> Dict[str, int]:
    # Import locally to keep script usable even if Sentinel deps are partially missing.
    from sentinel.ingest.telemetry_ingest import TelemetryIngestLimits, load_telemetry_jsonl
    from sentinel.telemetry.adapters import normalize_flow_record
    from sentinel.ingest.telemetry_ingest import _extract_tenant_id, _extract_timestamp, _stable_flow_event_id

    limits = TelemetryIngestLimits()
    records_iter, load_errs = load_telemetry_jsonl(path, limits=limits)
    if load_errs:
        return {"load_errors": len(load_errs), "records": 0, "events": 0, "errors": len(load_errs)}

    seen: set[str] = set()
    now = time.time()
    counts = {
        "load_errors": 0,
        "records": 0,
        "events": 0,
        "errors": 0,
        "parse_errors": 0,
        "missing_tenant": 0,
        "future_ts": 0,
        "normalize_error": 0,
        "duplicate": 0,
    }

    for idx, record in enumerate(records_iter):
        counts["records"] += 1
        if "_parse_error" in record:
            counts["errors"] += 1
            counts["parse_errors"] += 1
            continue

        tenant_id = _extract_tenant_id(record, default="cli")
        if not tenant_id:
            counts["errors"] += 1
            counts["missing_tenant"] += 1
            continue

        ts = _extract_timestamp(record)
        if ts is not None and (ts - now) > float(limits.max_future_skew_s):
            counts["errors"] += 1
            counts["future_ts"] += 1
            ts = now

        features_obj, norm_errors = normalize_flow_record(record)
        if norm_errors or not features_obj:
            counts["errors"] += 1
            counts["normalize_error"] += 1
            continue

        features = features_obj.to_dict()
        event_id = _stable_flow_event_id(features, tenant_id=tenant_id, record=record)
        if event_id in seen:
            counts["errors"] += 1
            counts["duplicate"] += 1
            continue
        seen.add(event_id)
        counts["events"] += 1

    return counts


def _count_aws_vpc_ingest_errors(path: Path) -> Dict[str, int]:
    from sentinel.telemetry.adapters import parse_aws_vpc_flow_log

    text = path.read_text(encoding="utf-8", errors="ignore")
    features, errors = parse_aws_vpc_flow_log(text)
    lines = sum(1 for line in text.splitlines() if line.strip())
    return {"lines": lines, "events": len(features), "errors": len(errors)}


def _md_table(rows: List[List[str]]) -> str:
    if not rows:
        return ""
    out = []
    out.append("| " + " | ".join(rows[0]) + " |")
    out.append("| " + " | ".join(["---"] * len(rows[0])) + " |")
    for r in rows[1:]:
        out.append("| " + " | ".join(r) + " |")
    return "\n".join(out) + "\n"


def main() -> int:
    ap = argparse.ArgumentParser(description="Evaluate Phase-4 SLAs from real Sentinel gateway runs.")
    ap.add_argument(
        "--config",
        default=str(Path("sentinel") / "config" / "phase4_sla_v1.json"),
        help="SLA config JSON path.",
    )
    ap.add_argument(
        "--reports-dir",
        default="reports",
        help="Reports directory (where final_p4_* artifacts live).",
    )
    ap.add_argument(
        "--out-json",
        default=str(Path("reports") / "phase4_sla_eval_v1.json"),
        help="Output JSON report path.",
    )
    ap.add_argument(
        "--out-md",
        default=str(Path("reports") / "phase4_sla_eval_v1.md"),
        help="Output markdown report path.",
    )
    ap.add_argument(
        "--replay-throughput",
        action="store_true",
        help="Replay a small set of telemetry corpora through main.py to measure wall-clock throughput.",
    )
    args = ap.parse_args()

    repo = Path.cwd()
    reports_dir = repo / args.reports_dir
    cfg = _read_json(repo / args.config)

    latency_targets: Dict[str, float] = cfg.get("latency_ms_p95", {}) or {}
    throughput_targets: Dict[str, float] = cfg.get("throughput_events_per_s_min", {}) or {}
    error_rate_max: Dict[str, float] = cfg.get("error_rate_max", {}) or {}
    duplicate_rate_max: Dict[str, float] = cfg.get("duplicate_rate_max", {}) or {}

    # Map SLA keys to evidence artifacts already produced by the gateway.
    latency_evidence = {
        "action_event": reports_dir / "final_p4_20260210_action_parallel.ndjson",
        "mcp_runtime": reports_dir / "final_p4_20260210_mcp_parallel.ndjson",
        "exfil_event": reports_dir / "final_p4_20260210_exfil_parallel.ndjson",
        "resilience_event": reports_dir / "final_p4_20260210_resilience_parallel.ndjson",
        # scripts uses the max of benign + suspicious as the gating p95.
        "scripts_benign": reports_dir / "final_p4_20260210_scripts_benign_parallel.ndjson",
        "scripts_suspicious": reports_dir / "final_p4_20260210_scripts_suspicious_parallel.ndjson",
        "pe_files": reports_dir / "final_p4_20260210_pe_parallel.ndjson",
        "elf_unsupported": reports_dir / "final_p4_20260210_elf_parallel.ndjson",
    }

    results: List[SLAResult] = []
    warnings: List[str] = []

    # 1) Latency SLAs from NDJSON artifacts (analysis_time_ms per event).
    # scripts SLA is evaluated as max(p95(benign), p95(suspicious)).
    latency_p95_by_key: Dict[str, Optional[float]] = {}
    latency_debug: Dict[str, Any] = {}
    for key, path in latency_evidence.items():
        if not path.exists():
            warnings.append(f"missing latency evidence: {path}")
            latency_p95_by_key[key] = None
            latency_debug[key] = {"missing": True}
            continue
        p95, dbg = _latency_p95_from_report(path)
        latency_p95_by_key[key] = p95
        latency_debug[key] = {"path": str(path), **dbg}

    # Flatten scripts metric to the configured key name.
    scripts_p95 = None
    if latency_p95_by_key.get("scripts_benign") is not None or latency_p95_by_key.get("scripts_suspicious") is not None:
        scripts_p95 = max(
            v for v in [latency_p95_by_key.get("scripts_benign"), latency_p95_by_key.get("scripts_suspicious")] if v is not None
        )

    # Emit latency SLA results.
    for sla_key, threshold in latency_targets.items():
        if sla_key == "scripts":
            value = scripts_p95
            evidence = f"{latency_evidence['scripts_benign'].name}, {latency_evidence['scripts_suspicious'].name}"
            passed = value is not None and value <= float(threshold)
            results.append(
                SLAResult(
                    key=sla_key,
                    metric="latency_p95_ms",
                    value=value,
                    threshold=float(threshold),
                    passed=passed,
                    evidence=evidence,
                    notes="Evaluated as max(p95(benign), p95(suspicious)).",
                )
            )
            continue

        ev_path = latency_evidence.get(sla_key)
        value = latency_p95_by_key.get(sla_key)
        passed = value is not None and value <= float(threshold)
        results.append(
            SLAResult(
                key=sla_key,
                metric="latency_p95_ms",
                value=value,
                threshold=float(threshold),
                passed=passed,
                evidence=str(ev_path.name if ev_path else ""),
                notes="" if value is not None else "missing analysis_time_ms samples or missing artifact",
            )
        )

    # 2) Telemetry error rates (computed from input corpora deterministically).
    telemetry_sources = {
        "telemetry_zeek_sampled": Path(r"B:\CyberMesh\TestData\clean\deepflow\zeek_sampled.jsonl"),
        "telemetry_aws_vpc": Path(r"B:\CyberMesh\TestData\clean\flows\cloud\aws_vpc_flows.log"),
        "telemetry_c2_beacon": Path(r"B:\CyberMesh\TestData\clean\ai_scenarios\c2_beacon.jsonl"),
    }

    error_rate_debug: Dict[str, Any] = {}
    for key, max_rate in error_rate_max.items():
        src = telemetry_sources.get(key)
        if not src or not src.exists():
            results.append(
                SLAResult(
                    key=key,
                    metric="error_rate",
                    value=None,
                    threshold=float(max_rate),
                    passed=False,
                    evidence=str(src) if src else "",
                    notes="missing telemetry source corpus for error-rate evaluation",
                )
            )
            continue

        if key == "telemetry_aws_vpc":
            c = _count_aws_vpc_ingest_errors(src)
            denom = max(1, int(c.get("lines", 0)))
            rate = float(c.get("errors", 0)) / float(denom)
            error_rate_debug[key] = {"source": str(src), **c}
        else:
            c = _count_telemetry_jsonl_ingest_errors(src)
            denom = max(1, int(c.get("records", 0)))
            # Treat duplicates as a separate "drop rate" rather than a hard ingest error.
            hard_errors = int(c.get("errors", 0)) - int(c.get("duplicate", 0))
            rate = float(max(0, hard_errors)) / float(denom)
            c = dict(c)
            c["hard_errors"] = max(0, hard_errors)
            error_rate_debug[key] = {"source": str(src), **c}

        results.append(
            SLAResult(
                key=key,
                metric="error_rate",
                value=rate,
                threshold=float(max_rate),
                passed=rate <= float(max_rate),
                evidence=str(src),
                notes=(
                    ""
                    if key == "telemetry_aws_vpc"
                    else f"excludes duplicates; duplicate_rate={int(error_rate_debug[key].get('duplicate',0))/max(1,int(error_rate_debug[key].get('records',1))):.4f}"
                ),
            )
        )

    # 2B) Duplicate/drop rate (optional SLA, mainly for Zeek-like corpora).
    for key, max_rate in duplicate_rate_max.items():
        src = telemetry_sources.get(key)
        if not src or not src.exists():
            results.append(
                SLAResult(
                    key=key,
                    metric="duplicate_rate",
                    value=None,
                    threshold=float(max_rate),
                    passed=False,
                    evidence=str(src) if src else "",
                    notes="missing telemetry source corpus for duplicate-rate evaluation",
                )
            )
            continue
        if key == "telemetry_aws_vpc":
            results.append(
                SLAResult(
                    key=key,
                    metric="duplicate_rate",
                    value=0.0,
                    threshold=float(max_rate),
                    passed=True,
                    evidence=str(src),
                    notes="not applicable for AWS VPC flow logs (line-based)",
                )
            )
            continue
        c = _count_telemetry_jsonl_ingest_errors(src)
        denom = max(1, int(c.get("records", 0)))
        dup_rate = float(int(c.get("duplicate", 0))) / float(denom)
        error_rate_debug.setdefault(key, {"source": str(src)}).update({"duplicate": int(c.get("duplicate", 0)), "records": int(c.get("records", 0))})
        results.append(
            SLAResult(
                key=key,
                metric="duplicate_rate",
                value=dup_rate,
                threshold=float(max_rate),
                passed=dup_rate <= float(max_rate),
                evidence=str(src),
                notes="duplicates are a drop/dedupe signal, not a hard ingest failure",
            )
        )

    # 3) Throughput (wall-clock) using targeted gateway replays.
    throughput_debug: Dict[str, Any] = {}
    if args.replay_throughput:
        for key, min_eps in throughput_targets.items():
            src = telemetry_sources.get(key)
            if not src or not src.exists():
                results.append(
                    SLAResult(
                        key=key,
                        metric="throughput_events_per_s",
                        value=None,
                        threshold=float(min_eps),
                        passed=False,
                        evidence=str(src) if src else "",
                        notes="missing telemetry source corpus for throughput evaluation",
                    )
                )
                continue

            # Telemetry jsonl path requires --telemetry --format json and an output path.
            out_path = reports_dir / f"_sla_tmp_{key}.ndjson"
            cmd = [
                sys.executable,
                "main.py",
                str(src),
                "--telemetry",
                "--format",
                "json" if src.suffix.lower() in (".jsonl", ".ndjson") else "csv",
                "--no-threat-intel",
                "--out",
                str(out_path),
            ]
            rc, wall_s, stdout, stderr = _run_gateway(cmd, cwd=repo)
            out = _parse_stdout_json(stdout) or {}
            events = int(out.get("events", 0)) if isinstance(out, dict) else 0

            # If stdout JSON wasn't parseable, fall back to counting output lines.
            if events <= 0 and out_path.exists():
                try:
                    events = sum(1 for obj in _iter_ndjson(out_path) if obj.get("_type") != "ingest_summary")
                except Exception:
                    events = 0

            eps = (float(events) / float(wall_s)) if wall_s > 0 else None
            throughput_debug[key] = {
                "cmd": cmd,
                "returncode": rc,
                "wall_s": wall_s,
                "events": events,
                "stdout_json": out if out else None,
                "stderr_tail": (stderr or "")[-800:],
                "out": str(out_path),
            }

            passed = (rc == 0) and (eps is not None) and (eps >= float(min_eps))
            results.append(
                SLAResult(
                    key=key,
                    metric="throughput_events_per_s",
                    value=eps,
                    threshold=float(min_eps),
                    passed=passed,
                    evidence=str(src),
                    notes=f"gateway replay wall_s={wall_s:.3f}, out={out_path.name}",
                )
            )
    else:
        for key, min_eps in throughput_targets.items():
            results.append(
                SLAResult(
                    key=key,
                    metric="throughput_events_per_s",
                    value=None,
                    threshold=float(min_eps),
                    passed=False,
                    evidence="",
                    notes="throughput not evaluated (run with --replay-throughput)",
                )
            )

    # Persist reports.
    out_json = Path(args.out_json)
    out_md = Path(args.out_md)
    out_json.parent.mkdir(parents=True, exist_ok=True)
    out_md.parent.mkdir(parents=True, exist_ok=True)

    payload = {
        "sla_config": cfg,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "host": {
            "platform": sys.platform,
            "python": sys.version.split()[0],
            "cwd": str(repo),
        },
        "results": [r.to_dict() for r in results],
        "warnings": warnings,
        "debug": {
            "latency": latency_debug,
            "error_rate": error_rate_debug,
            "throughput": throughput_debug,
        },
        "overall_passed": all(r.passed for r in results),
    }
    out_json.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")

    # Markdown summary
    rows = [["Key", "Metric", "Value", "Threshold", "Pass", "Evidence/Notes"]]
    for r in results:
        v = "n/a" if r.value is None else f"{r.value:.4f}"
        t = "n/a" if r.threshold is None else f"{r.threshold:.4f}"
        rows.append([r.key, r.metric, v, t, "PASS" if r.passed else "FAIL", (r.evidence + (" | " + r.notes if r.notes else "")).strip()])

    md = []
    md.append("# Phase-4 SLA Evaluation (v1)\n")
    md.append(f"- Config: `{args.config}`\n")
    md.append(f"- Generated: `{payload['generated_at']}`\n")
    md.append(f"- Overall: `{'PASS' if payload['overall_passed'] else 'FAIL'}`\n\n")
    md.append(_md_table(rows))
    if warnings:
        md.append("## Warnings\n")
        for w in warnings:
            md.append(f"- {w}\n")
    out_md.write_text("".join(md), encoding="utf-8")

    print(json.dumps({"out_json": str(out_json), "out_md": str(out_md), "overall_passed": payload["overall_passed"]}, indent=2))
    return 0 if payload["overall_passed"] else 2


if __name__ == "__main__":
    raise SystemExit(main())
