"""Phase 4 load test harness (standalone hardening).

This is intentionally a harness-only script:
- No prod hardcoding of TestData paths.
- Streams JSONL/NDJSON inputs (bounded memory).
- Routes analysis through SentinelOrchestrator.analyze_event() so OPA/attestation
  behavior is applied consistently across modalities.
"""

from __future__ import annotations

import argparse
import json
import os
import platform
import random
import statistics
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, Iterable, Iterator, List, Optional, Tuple

import sys

BASE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE_DIR))

from sentinel.agents import SentinelOrchestrator
from sentinel.agents.event_builder import build_flow_event
from sentinel.contracts import CanonicalEvent, Modality
from sentinel.ingest import OSSAdapter, load_adapter_spec, load_records
from sentinel.telemetry.adapters import normalize_flow_record


def _maybe_import_psutil():
    try:
        import psutil  # type: ignore
    except Exception:
        return None
    return psutil


@dataclass
class RunStats:
    count: int
    min_ms: float
    mean_ms: float
    p50_ms: float
    p95_ms: float
    p99_ms: float
    max_ms: float


def _percentile(values: List[float], pct: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    k = (pct / 100.0) * (len(ordered) - 1)
    f = int(k)
    c = min(f + 1, len(ordered) - 1)
    if f == c:
        return ordered[f]
    d0 = ordered[f] * (c - k)
    d1 = ordered[c] * (k - f)
    return d0 + d1


def _stats(values: List[float]) -> RunStats:
    if not values:
        return RunStats(0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
    return RunStats(
        count=len(values),
        min_ms=min(values),
        mean_ms=statistics.mean(values),
        p50_ms=_percentile(values, 50),
        p95_ms=_percentile(values, 95),
        p99_ms=_percentile(values, 99),
        max_ms=max(values),
    )


def _read_jsonl(path: Path, max_lines: Optional[int] = None) -> Iterator[Dict[str, Any]]:
    with path.open("rb") as f:
        for idx, raw in enumerate(f):
            if max_lines is not None and idx >= max_lines:
                break
            line = raw.strip()
            if not line:
                continue
            try:
                obj = json.loads(line.decode("utf-8"))
            except Exception:
                # Let caller count parse failures deterministically.
                yield {"_parse_error": "invalid_json", "_raw": line[:200].decode("utf-8", errors="ignore")}
                continue
            if isinstance(obj, dict):
                yield obj
            else:
                yield {"_parse_error": "non_object", "_raw": str(obj)[:200]}


def _extract_tenant_id(record: Dict[str, Any], default: str) -> str:
    for key in ("tenant_id", "tenant", "tenantId"):
        value = record.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return default


def _unwrap_flow_record(record: Dict[str, Any]) -> Dict[str, Any]:
    """Best-effort unwrapping for common cloud flow wrappers (harness-only)."""
    if "jsonPayload" in record and isinstance(record["jsonPayload"], dict):
        payload = dict(record["jsonPayload"])
        # Normalize common field names.
        if "dest_ip" in payload and "dst_ip" not in payload:
            payload["dst_ip"] = payload.get("dest_ip")
        if "dest_port" in payload and "dst_port" not in payload:
            payload["dst_port"] = payload.get("dest_port")
        if "bytes_sent" in payload and "bytes_fwd" not in payload:
            payload["bytes_fwd"] = payload.get("bytes_sent")
        if "bytes_received" in payload and "bytes_bwd" not in payload:
            payload["bytes_bwd"] = payload.get("bytes_received")
        if "packets_sent" in payload and "pkts_fwd" not in payload:
            payload["pkts_fwd"] = payload.get("packets_sent")
        if "packets_received" in payload and "pkts_bwd" not in payload:
            payload["pkts_bwd"] = payload.get("packets_received")
        if "protocol" in payload and "proto" not in payload:
            payload["proto"] = payload.get("protocol")
        return payload

    # Azure NSG: records[0].properties.flows[*].flows[*].flowTuples[*] (CSV tuple string)
    if "records" in record and isinstance(record.get("records"), list):
        return record

    return record


def _flow_from_aws_vpc_log_line(line: str) -> Dict[str, Any]:
    # AWS VPC flow logs (version 2): space-separated.
    parts = [p for p in line.strip().split(" ") if p]
    if len(parts) < 14:
        return {"_parse_error": "aws_vpc_short", "_raw": line[:200]}
    # version, account-id, interface-id, srcaddr, dstaddr, srcport, dstport, protocol, packets, bytes, start, end, action, log-status
    return {
        "src_ip": parts[3],
        "dst_ip": parts[4],
        "src_port": parts[5],
        "dst_port": parts[6],
        "proto": parts[7],
        "pkts_fwd": parts[8],
        "bytes_fwd": parts[9],
        "duration_s": max(int(parts[11]) - int(parts[10]), 0),
        "action": parts[12],
        "log_status": parts[13],
    }


def _read_aws_vpc_log(path: Path, max_lines: Optional[int] = None) -> Iterator[Dict[str, Any]]:
    with path.open("r", encoding="utf-8", errors="ignore") as f:
        for idx, line in enumerate(f):
            if max_lines is not None and idx >= max_lines:
                break
            if not line.strip():
                continue
            yield _flow_from_aws_vpc_log_line(line)


def _events_from_flow_jsonl(
    jsonl_path: Path,
    tenant_default: str,
    max_events: Optional[int],
) -> Tuple[List[CanonicalEvent], Dict[str, int]]:
    events: List[CanonicalEvent] = []
    counters: Dict[str, int] = {"parse_error": 0, "normalize_error": 0, "ok": 0}

    for record in _read_jsonl(jsonl_path, max_lines=max_events):
        if "_parse_error" in record:
            counters["parse_error"] += 1
            continue

        tenant_id = _extract_tenant_id(record, tenant_default)
        unwrapped = _unwrap_flow_record(record)
        features, errors = normalize_flow_record(unwrapped)
        if errors or not features:
            counters["normalize_error"] += 1
            continue

        raw_context = dict(record)
        raw_context["_input_path"] = str(jsonl_path)
        raw_context["_input_type"] = "telemetry_jsonl"
        event = build_flow_event(
            features=features,
            raw_context=raw_context,
            tenant_id=tenant_id,
            source="phase4_loadtest",
        )
        events.append(event)
        counters["ok"] += 1
        if max_events is not None and len(events) >= max_events:
            break

    return events, counters


def _events_from_aws_vpc(
    path: Path,
    tenant_default: str,
    max_events: Optional[int],
) -> Tuple[List[CanonicalEvent], Dict[str, int]]:
    events: List[CanonicalEvent] = []
    counters: Dict[str, int] = {"parse_error": 0, "normalize_error": 0, "ok": 0}

    for record in _read_aws_vpc_log(path, max_lines=max_events):
        if "_parse_error" in record:
            counters["parse_error"] += 1
            continue
        tenant_id = _extract_tenant_id(record, tenant_default)
        features, errors = normalize_flow_record(record)
        if errors or not features:
            counters["normalize_error"] += 1
            continue
        raw_context = dict(record)
        raw_context["_input_path"] = str(path)
        raw_context["_input_type"] = "aws_vpc_flow_log"
        event = build_flow_event(
            features=features,
            raw_context=raw_context,
            tenant_id=tenant_id,
            source="phase4_loadtest",
        )
        events.append(event)
        counters["ok"] += 1
        if max_events is not None and len(events) >= max_events:
            break

    return events, counters


def _oss_events_from_incoming(
    incoming_dir: Path,
    tenant_id: str,
) -> Tuple[List[CanonicalEvent], List[str]]:
    # Reuse the same adapter set as Phase 2 benchmark.
    samples = [
        ("adapter_mcp_scan.json", "mcp_scan_output.json", "json"),
        ("adapter_skill_scanner.json", "skill_scanner_output.json", "json"),
        ("adapter_trufflehog.json", "trufflehog_output.json", "json"),
        ("adapter_rebuff.json", "rebuff_output.json", "json"),
        ("adapter_falco_findings.json", "falco_output.json", "json"),
        ("adapter_bzar_rules.json", "bzar_output.log", "ndjson"),
        ("adapter_zeek_flow.json", "zeek_conn.log", "ndjson"),
    ]

    config_dir = BASE_DIR / "sentinel" / "config"
    events: List[CanonicalEvent] = []
    errors: List[str] = []

    for adapter_name, file_name, fmt in samples:
        spec_path = config_dir / adapter_name
        input_path = incoming_dir / file_name
        if not input_path.exists():
            errors.append(f"missing_input: {input_path}")
            continue
        spec = load_adapter_spec(spec_path)
        records, rec_errors = load_records(
            input_path,
            fmt,
            spec.limits,
            records_path=spec.records_path,
        )
        if rec_errors:
            errors.extend([f"{file_name}: {e}" for e in rec_errors])
            continue
        adapter = OSSAdapter(spec)
        evts, evt_errors = adapter.records_to_events(records, tenant_id=tenant_id)
        if evt_errors:
            errors.extend([f"{file_name}: {e}" for e in evt_errors])
        events.extend(evts)

    return events, errors


def _run_events(
    orchestrator: SentinelOrchestrator,
    events: List[CanonicalEvent],
    workers: int,
    max_events: Optional[int],
    seed: int,
) -> Dict[str, Any]:
    if not events:
        return {"analysis_ms": asdict(_stats([])), "counts": {"events": 0}}

    rng = random.Random(seed)
    if max_events is None:
        selected = events
    else:
        # Deterministically cycle through event list to reach max_events.
        selected = [events[i % len(events)] for i in range(max_events)]
        rng.shuffle(selected)  # deterministic due to seed

    latencies: List[float] = []
    opa_status: Dict[str, int] = {}
    threat_levels: Dict[str, int] = {}
    errors_count = 0

    start = time.perf_counter()
    with ThreadPoolExecutor(max_workers=max(1, workers)) as ex:
        futures = [ex.submit(_analyze_one, orchestrator, ev) for ev in selected]
        for fut in as_completed(futures):
            elapsed_ms, result = fut.result()
            latencies.append(elapsed_ms)
            if isinstance(result, dict):
                tl = str(getattr(result.get("threat_level"), "value", result.get("threat_level")))
                threat_levels[tl] = threat_levels.get(tl, 0) + 1
                opa = result.get("metadata", {}).get("opa", {}) if isinstance(result.get("metadata"), dict) else {}
                status = str(opa.get("status", "missing"))
                opa_status[status] = opa_status.get(status, 0) + 1
                if result.get("errors"):
                    errors_count += 1

    wall = time.perf_counter() - start
    s = _stats(latencies)
    return {
        "analysis_ms": {
            "count": s.count,
            "min": s.min_ms,
            "mean": s.mean_ms,
            "p50": s.p50_ms,
            "p95": s.p95_ms,
            "p99": s.p99_ms,
            "max": s.max_ms,
        },
        "counts": {
            "events": len(selected),
            "events_with_errors": errors_count,
        },
        "threat_levels": threat_levels,
        "opa_status": opa_status,
        "throughput_eps": (len(selected) / wall) if wall > 0 else 0.0,
        "wall_s": wall,
    }


def _run_events_timed(
    orchestrator: SentinelOrchestrator,
    events: List[CanonicalEvent],
    workers: int,
    duration_s: float,
    seed: int,
    max_in_flight: Optional[int] = None,
) -> Dict[str, Any]:
    """Run a time-based load test with bounded in-flight work."""
    if not events:
        return {"analysis_ms": asdict(_stats([])), "counts": {"events": 0}, "wall_s": 0.0}

    max_in_flight = max_in_flight or max(64, workers * 32)
    rng = random.Random(seed)
    pool = list(events)
    rng.shuffle(pool)

    latencies: List[float] = []
    opa_status: Dict[str, int] = {}
    threat_levels: Dict[str, int] = {}
    errors_count = 0

    start_wall = time.perf_counter()
    end_wall = start_wall + max(0.0, float(duration_s))

    idx = 0
    submitted = 0
    completed = 0

    with ThreadPoolExecutor(max_workers=max(1, workers)) as ex:
        in_flight = set()

        def _submit_one() -> None:
            nonlocal idx, submitted
            ev = pool[idx % len(pool)]
            idx += 1
            fut = ex.submit(_analyze_one, orchestrator, ev)
            in_flight.add(fut)
            submitted += 1

        # Fill initial queue
        while len(in_flight) < max_in_flight and time.perf_counter() < end_wall:
            _submit_one()

        while in_flight:
            # If time is up, stop submitting and drain.
            if time.perf_counter() < end_wall:
                while len(in_flight) < max_in_flight and time.perf_counter() < end_wall:
                    _submit_one()

            done = []
            for fut in as_completed(in_flight, timeout=0.25):
                done.append(fut)
                break
            if not done:
                continue
            for fut in done:
                in_flight.remove(fut)
                elapsed_ms, result = fut.result()
                completed += 1
                latencies.append(elapsed_ms)
                if isinstance(result, dict):
                    tl = str(getattr(result.get("threat_level"), "value", result.get("threat_level")))
                    threat_levels[tl] = threat_levels.get(tl, 0) + 1
                    opa = result.get("metadata", {}).get("opa", {}) if isinstance(result.get("metadata"), dict) else {}
                    status = str(opa.get("status", "missing"))
                    opa_status[status] = opa_status.get(status, 0) + 1
                    if result.get("errors"):
                        errors_count += 1

    wall = time.perf_counter() - start_wall
    s = _stats(latencies)
    return {
        "analysis_ms": {
            "count": s.count,
            "min": s.min_ms,
            "mean": s.mean_ms,
            "p50": s.p50_ms,
            "p95": s.p95_ms,
            "p99": s.p99_ms,
            "max": s.max_ms,
        },
        "counts": {
            "events": completed,
            "submitted": submitted,
            "events_with_errors": errors_count,
            "max_in_flight": int(max_in_flight),
        },
        "threat_levels": threat_levels,
        "opa_status": opa_status,
        "throughput_eps": (completed / wall) if wall > 0 else 0.0,
        "wall_s": wall,
    }


def _analyze_one(orchestrator: SentinelOrchestrator, event: CanonicalEvent) -> Tuple[float, Dict[str, Any]]:
    t0 = time.perf_counter()
    result = orchestrator.analyze_event(event)
    elapsed_ms = (time.perf_counter() - t0) * 1000.0
    return elapsed_ms, result


def _render_md(report: Dict[str, Any]) -> str:
    overall = report.get("overall", {})
    analysis = overall.get("analysis_ms", {})
    lines = [
        "# Phase 4 Load Test Report",
        "",
        f"Profile: {report.get('meta', {}).get('profile', '')}",
        f"Sequential: {report.get('meta', {}).get('sequential', False)}",
        f"Workers: {report.get('meta', {}).get('workers', 0)}",
        f"Max events: {report.get('meta', {}).get('max_events', '')}",
        "",
        "## Overall",
        f"- Events: {overall.get('counts', {}).get('events', 0)}",
        f"- Events with errors: {overall.get('counts', {}).get('events_with_errors', 0)}",
        f"- Throughput: {overall.get('throughput_eps', 0.0):.2f} eps",
        f"- P50: {analysis.get('p50', 0.0):.2f} ms",
        f"- P95: {analysis.get('p95', 0.0):.2f} ms",
        f"- P99: {analysis.get('p99', 0.0):.2f} ms",
        f"- Max: {analysis.get('max', 0.0):.2f} ms",
        "",
        "## OPA Status",
    ]
    for k, v in sorted((overall.get("opa_status") or {}).items()):
        lines.append(f"- {k}: {v}")
    lines.extend(["", "## Threat Levels"])
    for k, v in sorted((overall.get("threat_levels") or {}).items()):
        lines.append(f"- {k}: {v}")
    if report.get("inputs"):
        lines.extend(["", "## Input Counters"])
        for name, counters in report["inputs"].items():
            lines.append(f"- {name}: {json.dumps(counters, sort_keys=True)}")
    if report.get("resource"):
        lines.extend(["", "## Resource"])
        for k, v in report["resource"].items():
            lines.append(f"- {k}: {v}")
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Phase 4 standalone load test harness")
    parser.add_argument("--mode", choices=["batch", "ramp", "steady", "burst"], default="batch")
    parser.add_argument("--profile", choices=["telemetry", "scenarios", "phase3", "mixed", "edgecases", "oss"], default="scenarios")
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--max-events", type=int, default=5000)
    parser.add_argument("--seed", type=int, default=1337)
    parser.add_argument("--sequential", action="store_true")
    parser.add_argument("--tenant-default", default="phase4")
    parser.add_argument("--models-path", default=None)
    parser.add_argument("--duration-s", type=float, default=60.0, help="Duration for steady/burst/ramp steps")
    parser.add_argument("--step-s", type=float, default=15.0, help="Ramp step duration")
    parser.add_argument("--burst-mult", type=float, default=2.0, help="Burst multiplier vs baseline workers")
    parser.add_argument("--max-in-flight", type=int, default=0, help="Override in-flight cap (0=auto)")

    # Inputs (override defaults if needed)
    parser.add_argument("--zeek-jsonl", default=str(Path("B:/CyberMesh/TestData/clean/deepflow/zeek_sampled.jsonl")))
    parser.add_argument("--scenario-dir", default=str(Path("B:/CyberMesh/TestData/clean/ai_scenarios")))
    parser.add_argument("--edgecase-dir", default=str(Path("B:/CyberMesh/TestData/clean/edge-cases")))
    # Keep cloud JSONL optional: current Sentinel telemetry adapter does not reliably normalize these.
    parser.add_argument("--gcp-jsonl", default="")
    parser.add_argument("--azure-jsonl", default="")
    parser.add_argument("--aws-vpc-log", default=str(Path("B:/CyberMesh/TestData/clean/flows/cloud/aws_vpc_flows.log")))
    parser.add_argument("--oss-incoming", default=str(BASE_DIR / "oss_outputs" / "incoming"))
    parser.add_argument(
        "--phase3-events-dir",
        default=str(BASE_DIR / "testdata" / "clean" / "phase3-events"),
        help="Directory containing action/mcp/exfil/resilience JSONL corpora",
    )

    # Security controls (harness convenience)
    parser.add_argument("--opa-url", default=None, help="Override SENTINEL_OPA_URL for this run")
    parser.add_argument("--attestation-key", default=None, help="Set SENTINEL_ADAPTER_HMAC_KEY for this run")
    parser.add_argument("--attestation-required", action="store_true", help="Set SENTINEL_ADAPTER_HMAC_REQUIRED=true")

    args = parser.parse_args()

    if args.opa_url:
        os.environ["SENTINEL_OPA_URL"] = args.opa_url
    if args.attestation_key:
        os.environ["SENTINEL_ADAPTER_HMAC_KEY"] = args.attestation_key
    if args.attestation_required:
        os.environ["SENTINEL_ADAPTER_HMAC_REQUIRED"] = "true"

    orchestrator = SentinelOrchestrator(
        enable_llm=False,
        enable_threat_intel=False,
        enable_fast_path=True,
        models_path=args.models_path,
        sequential=args.sequential,
    )

    inputs: Dict[str, Dict[str, int]] = {}
    events: List[CanonicalEvent] = []
    oss_errors: List[str] = []

    max_events = int(args.max_events) if args.max_events is not None else None

    if args.profile in ("telemetry", "mixed", "edgecases"):
        zeek_path = Path(args.zeek_jsonl)
        if zeek_path.exists():
            ev, counters = _events_from_flow_jsonl(zeek_path, args.tenant_default, max_events=None if args.profile == "mixed" else max_events)
            events.extend(ev)
            inputs["zeek_jsonl"] = counters

        if args.gcp_jsonl:
            gcp_path = Path(args.gcp_jsonl)
        else:
            gcp_path = None
        if gcp_path and gcp_path.exists():
            ev, counters = _events_from_flow_jsonl(gcp_path, args.tenant_default, max_events=None if args.profile == "mixed" else max_events)
            events.extend(ev)
            inputs["gcp_jsonl"] = counters

        if args.azure_jsonl:
            azure_path = Path(args.azure_jsonl)
        else:
            azure_path = None
        if azure_path and azure_path.exists():
            ev, counters = _events_from_flow_jsonl(azure_path, args.tenant_default, max_events=None if args.profile == "mixed" else max_events)
            events.extend(ev)
            inputs["azure_jsonl"] = counters

        aws_path = Path(args.aws_vpc_log)
        if aws_path.exists():
            ev, counters = _events_from_aws_vpc(aws_path, args.tenant_default, max_events=None if args.profile == "mixed" else max_events)
            events.extend(ev)
            inputs["aws_vpc"] = counters

    if args.profile in ("scenarios", "mixed"):
        scenario_dir = Path(args.scenario_dir)
        if scenario_dir.exists():
            for p in sorted(scenario_dir.glob("*.jsonl")):
                ev, counters = _events_from_flow_jsonl(p, args.tenant_default, max_events=None if args.profile == "mixed" else max_events)
                events.extend(ev)
                inputs[f"scenario:{p.name}"] = counters

    if args.profile == "edgecases":
        edge_dir = Path(args.edgecase_dir)
        if edge_dir.exists():
            for p in sorted(edge_dir.glob("*")):
                if p.suffix.lower() == ".jsonl":
                    ev, counters = _events_from_flow_jsonl(p, args.tenant_default, max_events=max_events)
                    events.extend(ev)
                    inputs[f"edge:{p.name}"] = counters
                else:
                    # Binary edge cases are parse-negative tests for later telemetry-layer;
                    # here we just record their presence and ensure the harness doesn't hang.
                    inputs[f"edge:{p.name}"] = {"skipped_binary": 1}

    if args.profile in ("oss", "mixed"):
        incoming = Path(args.oss_incoming)
        if incoming.exists():
            oss_events, errs = _oss_events_from_incoming(incoming, tenant_id=args.tenant_default)
            events.extend(oss_events)
            oss_errors = errs

    if args.profile in ("phase3", "mixed"):
        phase3_dir = Path(args.phase3_events_dir)
        if phase3_dir.exists():
            config_dir = BASE_DIR / "sentinel" / "config"
            phase3_specs = [
                ("adapter_action_event.json", "action_events.jsonl"),
                ("adapter_mcp_runtime.json", "mcp_runtime_events.jsonl"),
                ("adapter_exfil_event.json", "exfil_events.jsonl"),
                ("adapter_resilience_event.json", "resilience_events.jsonl"),
            ]
            for spec_name, fname in phase3_specs:
                p = phase3_dir / fname
                if not p.exists():
                    inputs[f"phase3:{fname}"] = {"missing": 1}
                    continue
                spec = load_adapter_spec(config_dir / spec_name)
                recs, rec_errors = load_records(p, "ndjson", spec.limits, records_path=spec.records_path)
                if rec_errors:
                    inputs[f"phase3:{fname}"] = {"parse_error": len(rec_errors)}
                    continue
                adapter = OSSAdapter(spec)
                evts, evt_errors = adapter.records_to_events(recs, tenant_id=args.tenant_default)
                if evt_errors:
                    inputs[f"phase3:{fname}"] = {"event_error": len(evt_errors), "ok": len(evts)}
                else:
                    inputs[f"phase3:{fname}"] = {"ok": len(evts)}
                events.extend(evts)

    # Cap total event set for mixed runs (deterministic ordering).
    events.sort(key=lambda e: (e.modality.value, e.id))
    if max_events is not None:
        events = events[: max_events]

    max_in_flight = int(args.max_in_flight) if int(args.max_in_flight) > 0 else None
    runs: Dict[str, Any] = {}

    if args.mode == "batch":
        runs["batch"] = _run_events(
            orchestrator,
            events,
            workers=int(args.workers),
            max_events=max_events,
            seed=int(args.seed),
        )
        overall = runs["batch"]
    elif args.mode == "steady":
        runs["steady"] = _run_events_timed(
            orchestrator,
            events,
            workers=int(args.workers),
            duration_s=float(args.duration_s),
            seed=int(args.seed),
            max_in_flight=max_in_flight,
        )
        overall = runs["steady"]
    elif args.mode == "ramp":
        # Ramp workers: 1 -> baseline -> 2x baseline (bounded to reasonable max).
        baseline = max(1, int(args.workers))
        levels = sorted(set([1, max(1, baseline // 2), baseline, baseline * 2]))
        step_s = max(1.0, float(args.step_s))
        ramp_results = []
        for i, w in enumerate(levels):
            ramp_seed = int(args.seed) + (i * 1000)
            ramp_results.append({
                "workers": w,
                "duration_s": step_s,
                "result": _run_events_timed(
                    orchestrator,
                    events,
                    workers=w,
                    duration_s=step_s,
                    seed=ramp_seed,
                    max_in_flight=max_in_flight,
                ),
            })
        runs["ramp"] = {"steps": ramp_results}
        overall = ramp_results[-1]["result"] if ramp_results else _run_events(orchestrator, events, 1, max_events, int(args.seed))
    else:  # burst
        baseline = max(1, int(args.workers))
        burst_workers = max(1, int(round(baseline * float(args.burst_mult))))
        step_s = max(1.0, float(args.step_s))
        # 3 phases: baseline -> burst -> recovery.
        phases = [
            ("baseline", baseline, step_s),
            ("burst", burst_workers, step_s),
            ("recovery", baseline, step_s),
        ]
        burst_results = []
        for i, (name, w, dur) in enumerate(phases):
            burst_seed = int(args.seed) + (i * 2000)
            burst_results.append({
                "phase": name,
                "workers": w,
                "duration_s": dur,
                "result": _run_events_timed(
                    orchestrator,
                    events,
                    workers=w,
                    duration_s=dur,
                    seed=burst_seed,
                    max_in_flight=max_in_flight,
                ),
            })
        runs["burst"] = {"phases": burst_results}
        overall = burst_results[1]["result"] if burst_results else _run_events(orchestrator, events, baseline, max_events, int(args.seed))

    psutil = _maybe_import_psutil()
    resource: Dict[str, Any] = {}
    if psutil:
        proc = psutil.Process(os.getpid())
        mem = proc.memory_info()
        resource["rss_bytes"] = int(getattr(mem, "rss", 0))

    report: Dict[str, Any] = {
        "meta": {
            "timestamp": time.strftime("%Y-%m-%d %H:%M:%S"),
            "mode": args.mode,
            "profile": args.profile,
            "sequential": bool(args.sequential),
            "workers": int(args.workers),
            "max_events": max_events,
            "seed": int(args.seed),
            "python": platform.python_version(),
            "platform": platform.platform(),
            "opa_url": os.getenv("SENTINEL_OPA_URL", ""),
            "opa_timeout_ms": int(os.getenv("SENTINEL_OPA_TIMEOUT_MS", "800")),
        },
        "inputs": inputs,
        "oss_errors": oss_errors[:20],
        "runs": runs,
        "overall": overall,
        "resource": resource,
    }

    reports_dir = BASE_DIR / "reports"
    reports_dir.mkdir(parents=True, exist_ok=True)
    ts = time.strftime("%Y%m%d_%H%M%S")
    mode = "seq" if args.sequential else "par"
    out_json = reports_dir / f"phase4_loadtest_{args.profile}_{args.mode}_{mode}_{ts}.json"
    out_md = out_json.with_suffix(".md")
    out_json.write_text(json.dumps(report, indent=2), encoding="utf-8")
    out_md.write_text(_render_md(report), encoding="utf-8")

    print(f"Wrote: {out_json}")
    print(f"Wrote: {out_md}")
    print(json.dumps(report["overall"], indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
