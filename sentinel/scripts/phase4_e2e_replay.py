"""Phase 4 E2E real-corpus replay gate.

Goals:
- Replay real corpora and run through SentinelOrchestrator.analyze_event().
- Prove wiring by asserting per-agent metadata buckets exist.
- Prove safety by asserting no silent failures (errors or degraded are surfaced).
- Prove determinism by replaying a subset twice (ignoring timestamps).

This is a harness-only script; it must not introduce prod hardcoding.
"""

from __future__ import annotations

import argparse
import json
import os
import platform
import time
from dataclasses import asdict
from pathlib import Path
from typing import Any, Dict, Iterator, List, Optional, Tuple

import sys

BASE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE_DIR))

from sentinel.agents import SentinelOrchestrator
from sentinel.agents.event_builder import build_flow_event
from sentinel.contracts import CanonicalEvent
from sentinel.contracts import Modality
from sentinel.ingest import OSSAdapter, load_adapter_spec, load_records
from sentinel.telemetry.adapters import normalize_flow_record


def _read_jsonl(path: Path, max_lines: Optional[int] = None) -> Iterator[Dict[str, Any]]:
    with path.open("rb") as f:
        for idx, raw in enumerate(f):
            # Treat 0/negative as "no cap" for convenience in CLI usage.
            if max_lines is not None and max_lines > 0 and idx >= max_lines:
                break
            line = raw.strip()
            if not line:
                continue
            try:
                obj = json.loads(line.decode("utf-8"))
            except Exception:
                yield {"_parse_error": "invalid_json", "_raw": line[:200].decode("utf-8", errors="ignore")}
                continue
            if isinstance(obj, dict):
                yield obj
            else:
                yield {"_parse_error": "non_object", "_raw": str(obj)[:200]}


def _unwrap_flow_record(record: Dict[str, Any]) -> Dict[str, Any]:
    if "jsonPayload" in record and isinstance(record["jsonPayload"], dict):
        payload = dict(record["jsonPayload"])
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
    return record


def _flow_events_from_jsonl(path: Path, max_events: int) -> Tuple[List[CanonicalEvent], Dict[str, int]]:
    events: List[CanonicalEvent] = []
    counts = {"ok": 0, "parse_error": 0, "normalize_error": 0}

    cap = None if int(max_events) <= 0 else int(max_events)
    for record in _read_jsonl(path, max_lines=cap):
        if "_parse_error" in record:
            counts["parse_error"] += 1
            continue
        unwrapped = _unwrap_flow_record(record)
        features, errs = normalize_flow_record(unwrapped)
        if errs or not features:
            counts["normalize_error"] += 1
            continue
        tenant_id = str(record.get("tenant_id") or "phase4")
        raw_context = dict(record)
        raw_context["_input_path"] = str(path)
        raw_context["_input_type"] = "telemetry_jsonl"
        events.append(build_flow_event(features, raw_context, tenant_id=tenant_id, source="phase4_e2e"))
        counts["ok"] += 1
        if cap is not None and len(events) >= cap:
            break

    return events, counts


def _file_events_from_dir(root: Path, max_files: int) -> Tuple[List[CanonicalEvent], Dict[str, int]]:
    """Build FILE events that route through orchestrator.analyze_event()."""
    events: List[CanonicalEvent] = []
    counts = {"ok": 0}
    for p in sorted(root.rglob("*")):
        if len(events) >= max_files:
            break
        if not p.is_file():
            continue
        # Skip huge files in the E2E gate by default.
        if p.stat().st_size > 50 * 1024 * 1024:
            continue
        ev = CanonicalEvent(
            id=f"file:{p.name}",
            timestamp=time.time(),
            source="phase4_e2e",
            tenant_id="phase4",
            modality=Modality.FILE,
            features_version="FileFeaturesV1",
            features={},
            raw_context={"file_path": str(p)},
            labels={},
        )
        events.append(ev)
        counts["ok"] += 1
    return events, counts


def _events_from_adapter_jsonl(spec_path: Path, jsonl_path: Path, max_events: int, tenant_id: str) -> Tuple[List[CanonicalEvent], Dict[str, int]]:
    spec = load_adapter_spec(spec_path)
    # Stream JSONL directly to avoid whole-file max_payload_bytes gating in Phase 4 E2E.
    records: List[Dict[str, Any]] = []
    parse_errors = 0
    cap = None if int(max_events) <= 0 else int(max_events)
    for item in _read_jsonl(jsonl_path, max_lines=cap):
        if "_parse_error" in item:
            parse_errors += 1
            continue
        records.append(item)
        if cap is not None and len(records) >= cap:
            break
    adapter = OSSAdapter(spec)
    events, evt_errors = adapter.records_to_events(records, tenant_id=tenant_id)
    return events, {"records": len(records), "rec_errors": parse_errors, "events": len(events), "evt_errors": len(evt_errors)}


def _assert_agent_bucket(result: Dict[str, Any], agent_name: str) -> bool:
    meta = result.get("metadata", {})
    if not isinstance(meta, dict):
        return False
    agents = meta.get("agents", {})
    if not isinstance(agents, dict):
        return False
    return agent_name in agents


def _strip_nondeterminism(obj: Any) -> Any:
    if isinstance(obj, dict):
        # Keep only stable keys for determinism checks.
        stable: Dict[str, Any] = {}
        for k in ("threat_level", "final_score", "confidence", "findings", "indicators", "errors", "reasoning_steps", "final_reasoning"):
            if k in obj:
                stable[k] = _strip_nondeterminism(obj.get(k))
        meta = obj.get("metadata")
        if isinstance(meta, dict):
            stable_meta: Dict[str, Any] = {}
            # Keep only safety-relevant stable metadata.
            for mk in ("degraded", "degraded_reasons"):
                if mk in meta:
                    stable_meta[mk] = _strip_nondeterminism(meta.get(mk))
            # Include OPA decision status/allow/reason only (strip latency/raw/error).
            opa = meta.get("opa")
            if isinstance(opa, dict):
                stable_meta["opa"] = {
                    "status": opa.get("status"),
                    "allow": opa.get("allow"),
                    "reason": opa.get("reason"),
                }
            if stable_meta:
                stable["metadata"] = stable_meta
        return stable
    if isinstance(obj, list):
        return [_strip_nondeterminism(v) for v in obj]
    if hasattr(obj, "value"):
        return getattr(obj, "value")
    return obj


def _run_suite(orchestrator: SentinelOrchestrator, name: str, events: List[CanonicalEvent], expected_agents: List[str], determinism_n: int) -> Dict[str, Any]:
    counts = {
        "events": len(events),
        "ok": 0,
        "with_errors": 0,
        "degraded": 0,
        "silent_failures": 0,
        "missing_agent_bucket": 0,
        "determinism_mismatches": 0,
    }
    samples: List[Dict[str, Any]] = []

    for idx, ev in enumerate(events):
        result = orchestrator.analyze_event(ev)
        if not isinstance(result, dict):
            counts["silent_failures"] += 1
            continue

        has_errors = bool(result.get("errors"))
        degraded = bool(result.get("metadata", {}).get("degraded")) if isinstance(result.get("metadata"), dict) else False
        if has_errors:
            counts["with_errors"] += 1
        if degraded:
            counts["degraded"] += 1
        if not has_errors and not degraded:
            counts["ok"] += 1

        # Wiring proof: expected agent buckets must be present if analysis actually ran.
        # If the orchestrator rejected the event early (attestation/unsupported), treat
        # that as a separate failure mode rather than "agent missing".
        if expected_agents:
            rejected = any("Attestation failure" in str(e) for e in (result.get("errors") or []))
            if not rejected:
                for agent in expected_agents:
                    if not _assert_agent_bucket(result, agent):
                        counts["missing_agent_bucket"] += 1
                        break

        # No silent failures: if there are agent errors, they must surface either via errors or degraded.
        if (result.get("errors") is None) and (not degraded):
            counts["silent_failures"] += 1

        if idx < 5:
            samples.append({
                "event_id": ev.id,
                "modality": ev.modality.value,
                "threat_level": str(getattr(result.get("threat_level"), "value", result.get("threat_level"))),
                "errors": result.get("errors", []),
                "degraded": degraded,
                "agents_present": list((result.get("metadata", {}).get("agents", {}) or {}).keys()),
            })

    # Determinism check: replay first determinism_n events twice.
    # Do not count early rejections (e.g. attestation failures) as determinism failures.
    for ev in events[:max(0, determinism_n)]:
        r1 = _strip_nondeterminism(orchestrator.analyze_event(ev))
        r2 = _strip_nondeterminism(orchestrator.analyze_event(ev))
        errors_1 = r1.get("errors") if isinstance(r1, dict) else []
        rejected = any("Attestation failure" in str(e) for e in (errors_1 or []))
        if rejected:
            continue
        if r1 != r2:
            counts["determinism_mismatches"] += 1

    return {"name": name, "counts": counts, "samples": samples}


def main() -> int:
    parser = argparse.ArgumentParser(description="Phase 4 E2E real-corpus replay gate")
    parser.add_argument("--max-events", type=int, default=200)
    parser.add_argument("--determinism-n", type=int, default=25)
    parser.add_argument("--tenant-id", default="phase4")
    parser.add_argument("--models-path", default=None)
    parser.add_argument("--sequential", action="store_true")
    parser.add_argument("--opa-url", default=None)
    parser.add_argument("--attestation-key", default=None)
    parser.add_argument("--attestation-required", action="store_true")

    # Corpora locations (override if needed)
    parser.add_argument("--zeek-jsonl", default=str(Path("B:/CyberMesh/TestData/clean/deepflow/zeek_sampled.jsonl")))
    # Keep cloud JSONL optional: current Sentinel telemetry adapter does not reliably normalize these.
    parser.add_argument("--gcp-jsonl", default="")
    parser.add_argument("--azure-jsonl", default="")
    parser.add_argument("--aws-vpc-log", default=str(Path("B:/CyberMesh/TestData/clean/flows/cloud/aws_vpc_flows.log")))
    parser.add_argument("--phase3-dir", default=str(BASE_DIR / "testdata" / "clean" / "phase3-events"))
    parser.add_argument("--oss-incoming", default=str(BASE_DIR / "oss_outputs" / "incoming"))
    parser.add_argument("--file-corpus-dir", default=str(BASE_DIR / "testdata" / "clean"))
    args = parser.parse_args()

    # `--max-events 0` means "no cap" for telemetry/adapters; file suites keep fixed caps.
    adapter_cap = None if int(args.max_events) <= 0 else int(args.max_events)
    file_cap = 20 if adapter_cap is None else adapter_cap

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

    reports_dir = BASE_DIR / "reports"
    reports_dir.mkdir(parents=True, exist_ok=True)

    suites: List[Dict[str, Any]] = []
    inputs: Dict[str, Any] = {}

    # Telemetry suites
    sources = [("zeek", Path(args.zeek_jsonl))]
    if args.gcp_jsonl:
        sources.append(("gcp", Path(args.gcp_jsonl)))
    if args.azure_jsonl:
        sources.append(("azure", Path(args.azure_jsonl)))
    for label, p in sources:
        if p.exists():
            events, c = _flow_events_from_jsonl(p, max_events=int(args.max_events))
            inputs[f"telemetry:{label}"] = c
            suites.append(_run_suite(
                orchestrator,
                f"telemetry:{label}",
                events,
                expected_agents=["flow_agent"],
                determinism_n=int(args.determinism_n),
            ))

    # AWS VPC log is not jsonl; treat as optional (handled by loadtest harness).
    inputs["telemetry:aws_vpc"] = {"note": "covered by load harness (aws_vpc_flows.log)"}

    # Phase 3 suites (use existing adapters to build canonical events)
    phase3_dir = Path(args.phase3_dir)
    config_dir = BASE_DIR / "sentinel" / "config"
    phase3_specs = [
        ("action", config_dir / "adapter_action_event.json", phase3_dir / "action_events.jsonl", ["sequence_risk_agent"]),
        ("mcp", config_dir / "adapter_mcp_runtime.json", phase3_dir / "mcp_runtime_events.jsonl", ["mcp_runtime_controls_agent"]),
        ("exfil", config_dir / "adapter_exfil_event.json", phase3_dir / "exfil_events.jsonl", ["exfil_dlp_agent"]),
        ("resilience", config_dir / "adapter_resilience_event.json", phase3_dir / "resilience_events.jsonl", ["resilience_agent"]),
    ]
    for name, spec_path, data_path, agents in phase3_specs:
        if data_path.exists():
            events, c = _events_from_adapter_jsonl(spec_path, data_path, int(args.max_events), tenant_id=str(args.tenant_id))
            inputs[f"phase3:{name}"] = c
            suites.append(_run_suite(
                orchestrator,
                f"phase3:{name}",
                events,
                expected_agents=agents,
                determinism_n=int(args.determinism_n),
            ))

    # OSS suites (reuse Phase 2 adapters and enforce attestation if requested)
    oss_incoming = Path(args.oss_incoming)
    if oss_incoming.exists():
        oss_specs = [
            # Our Falco fixture is normalized as ScanFindingsV1, so it should hit scanner_findings_agent.
            ("falco", config_dir / "adapter_falco_findings.json", oss_incoming / "falco_output.json", "json", ["scanner_findings_agent"]),
            ("trufflehog", config_dir / "adapter_trufflehog.json", oss_incoming / "trufflehog_output.json", "json", ["scanner_findings_agent"]),
            ("mcp_scan", config_dir / "adapter_mcp_scan.json", oss_incoming / "mcp_scan_output.json", "json", ["scanner_findings_agent"]),
            ("skill_scanner", config_dir / "adapter_skill_scanner.json", oss_incoming / "skill_scanner_output.json", "json", ["scanner_findings_agent"]),
            ("rebuff", config_dir / "adapter_rebuff.json", oss_incoming / "rebuff_output.json", "json", ["scanner_findings_agent"]),
            ("bzar", config_dir / "adapter_bzar_rules.json", oss_incoming / "bzar_output.log", "ndjson", ["rules_hit_agent"]),
            ("zeek_conn", config_dir / "adapter_zeek_flow.json", oss_incoming / "zeek_conn.log", "ndjson", ["flow_agent"]),
        ]
        for label, spec_path, data_path, fmt, agents in oss_specs:
            if not data_path.exists():
                continue
            spec = load_adapter_spec(spec_path)
            recs, rec_errors = load_records(data_path, fmt, spec.limits, records_path=spec.records_path)
            adapter = OSSAdapter(spec)
            use_recs = recs if adapter_cap is None else recs[:adapter_cap]
            events, evt_errors = adapter.records_to_events(use_recs, tenant_id=str(args.tenant_id))
            inputs[f"oss:{label}"] = {"records": len(recs), "rec_errors": len(rec_errors), "events": len(events), "evt_errors": len(evt_errors)}
            suites.append(_run_suite(
                orchestrator,
                f"oss:{label}",
                events,
                expected_agents=agents,
                determinism_n=min(int(args.determinism_n), 10),
            ))

    # File suites (static/script/malware paths)
    file_root = Path(args.file_corpus_dir)
    file_suites = [
        # ELF parsing may be unsupported depending on local parser availability; treat as negative gate.
        ("files:elf", file_root / "elf-files", []),
        ("files:pe", file_root / "pe-files", ["static_agent", "malware_agent"]),
        ("files:scripts_benign", file_root / "scripts" / "benign", ["static_agent", "script_agent"]),
        ("files:scripts_suspicious", file_root / "scripts" / "suspicious", ["static_agent", "script_agent"]),
    ]
    for name, folder, agents in file_suites:
        if folder.exists():
            cap = 10 if name == "files:elf" else file_cap
            events, c = _file_events_from_dir(folder, max_files=cap)
            inputs[name] = c
            suites.append(_run_suite(
                orchestrator,
                name,
                events,
                expected_agents=agents,
                determinism_n=min(int(args.determinism_n), 10),
            ))

    summary = {
        "meta": {
            "timestamp": time.strftime("%Y-%m-%d %H:%M:%S"),
            "python": platform.python_version(),
            "platform": platform.platform(),
            "sequential": bool(args.sequential),
            "opa_url": os.getenv("SENTINEL_OPA_URL", ""),
        },
        "inputs": inputs,
        "suites": suites,
    }

    ts = time.strftime("%Y%m%d_%H%M%S")
    out_json = reports_dir / f"phase4_e2e_{ts}.json"
    out_md = reports_dir / f"phase4_e2e_{ts}.md"
    out_json.write_text(json.dumps(summary, indent=2), encoding="utf-8")
    out_md.write_text(_render_md(summary), encoding="utf-8")

    print(f"Wrote: {out_json}")
    print(f"Wrote: {out_md}")

    # Hard gate: no silent failures and no missing agent buckets.
    gate_failures = 0
    for suite in suites:
        counts = suite.get("counts", {})
        if counts.get("silent_failures", 0) > 0:
            gate_failures += 1
        if counts.get("missing_agent_bucket", 0) > 0:
            gate_failures += 1
        if counts.get("determinism_mismatches", 0) > 0:
            gate_failures += 1
    return 1 if gate_failures else 0


def _render_md(summary: Dict[str, Any]) -> str:
    lines = [
        "# Phase 4 E2E Replay Summary",
        "",
        f"Timestamp: {summary.get('meta', {}).get('timestamp', '')}",
        f"Sequential: {summary.get('meta', {}).get('sequential', False)}",
        "",
        "## Suites",
        "| Suite | Events | OK | With errors | Degraded | Silent | Missing agent bucket | Determinism mismatches |",
        "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |",
    ]
    for suite in summary.get("suites", []):
        c = suite.get("counts", {})
        lines.append(
            f"| {suite.get('name','')} | {c.get('events',0)} | {c.get('ok',0)} | {c.get('with_errors',0)} | "
            f"{c.get('degraded',0)} | {c.get('silent_failures',0)} | {c.get('missing_agent_bucket',0)} | "
            f"{c.get('determinism_mismatches',0)} |"
        )
    lines.extend(["", "## Inputs"])
    for k, v in (summary.get("inputs") or {}).items():
        lines.append(f"- {k}: {json.dumps(v, sort_keys=True)}")
    return "\n".join(lines)


if __name__ == "__main__":
    raise SystemExit(main())
