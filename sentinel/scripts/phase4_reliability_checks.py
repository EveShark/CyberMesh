"""Phase 4 reliability / degraded-mode checks (standalone).

Evidence generator for Phase 4 TODO section 6:
- Induce agent timeout and confirm degraded + surfaced error.
- Induce partial agent failure and confirm aggregation succeeds + degraded.
- Confirm errors are never silently dropped.

LLM is disabled here by default; this focuses on the non-LLM agents/graphs.
"""

from __future__ import annotations

import argparse
import json
import os
import platform
import time
from pathlib import Path
from typing import Any, Dict, List, Tuple

import sys

BASE_DIR = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BASE_DIR))

from sentinel.agents import SentinelOrchestrator
from sentinel.agents.telemetry_agent import TelemetryRulesAgent
from sentinel.agents.sequence_risk_agent import SequenceRiskAgent
from sentinel.contracts import CanonicalEvent, Modality
from sentinel.telemetry.adapters import normalize_flow_record
from sentinel.agents.event_builder import build_flow_event


def _now_tag() -> str:
    return time.strftime("%Y%m%d_%H%M%S")


def _write_report(out_dir: Path, name: str, payload: Dict[str, Any]) -> Tuple[Path, Path]:
    out_dir.mkdir(parents=True, exist_ok=True)
    json_path = out_dir / f"{name}.json"
    md_path = out_dir / f"{name}.md"

    json_path.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")

    lines: List[str] = []
    lines.append("# Phase 4 Reliability Checks\n")
    lines.append(f"- Generated: {time.strftime('%Y-%m-%d %H:%M:%S')}")
    lines.append(f"- Host: {platform.platform()}")
    lines.append(f"- Python: {platform.python_version()}\n")

    summary = payload.get("summary", {})
    lines.append("## Summary")
    for k in sorted(summary.keys()):
        lines.append(f"- {k}: {summary[k]}")
    lines.append("")

    lines.append("## Checks")
    for c in payload.get("checks", []):
        status = "PASS" if c.get("ok") else "FAIL"
        lines.append(f"- {c.get('name')}: {status}")
        detail = c.get("detail")
        if detail:
            # Keep MD short; JSON carries full detail.
            lines.append(f"  - {str(detail)[:300]}")
    lines.append("")

    md_path.write_text("\n".join(lines), encoding="utf-8")
    return json_path, md_path


def _has_substring_errors(result: Dict[str, Any], needle: str) -> bool:
    errs = result.get("errors") or []
    return any(needle in str(e) for e in errs)


def _is_degraded(result: Dict[str, Any]) -> bool:
    meta = result.get("metadata") or {}
    return bool(isinstance(meta, dict) and meta.get("degraded"))


def _build_sample_flow_event() -> CanonicalEvent:
    record = {
        "flow_id": "phase4-rel-1",
        "ts": time.time(),
        "tenant_id": "phase4",
        "src_ip": "10.0.0.1",
        "dst_ip": "10.0.0.2",
        "src_port": 12345,
        "dst_port": 80,
        "proto": 6,
        "bytes_fwd": 100,
        "bytes_bwd": 50,
        "pkts_fwd": 2,
        "pkts_bwd": 1,
        "duration_ms": 10,
    }
    features, errs = normalize_flow_record(record)
    if errs or not features:
        raise RuntimeError(f"failed to build sample flow features: {errs}")
    return build_flow_event(
        features=features,
        raw_context=record,
        tenant_id="phase4",
        source="phase4_reliability",
        event_id="phase4-rel-flow",
        timestamp=record["ts"],
    )


def _build_sample_action_event() -> CanonicalEvent:
    return CanonicalEvent(
        id="phase4-rel-action",
        timestamp=time.time(),
        source="phase4_reliability",
        tenant_id="phase4",
        modality=Modality.ACTION_EVENT,
        features_version="ActionEventV1",
        features={"event_name": "noop", "actions": []},
        raw_context={"_adapter": "phase4_reliability"},
        labels={},
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="Phase 4 reliability checks (standalone)")
    parser.add_argument("--out-dir", default=str(BASE_DIR / "reports"), help="Reports directory")
    parser.add_argument("--timeout-s", default="0.05", help="Agent timeout seconds for induced timeout checks")
    args = parser.parse_args()

    out_dir = Path(args.out_dir)
    timeout_s = float(args.timeout_s)

    old_timeout = os.getenv("SENTINEL_AGENT_TIMEOUT_SECONDS")
    os.environ["SENTINEL_AGENT_TIMEOUT_SECONDS"] = str(timeout_s)

    checks: List[Dict[str, Any]] = []
    try:
        orch = SentinelOrchestrator(
            enable_llm=False,
            enable_threat_intel=False,
            enable_fast_path=True,
            sequential=False,
            enable_opa_gate=False,
        )

        # 1) Induce timeout in telemetry graph (FlowAgent == TelemetryRulesAgent).
        flow_event = _build_sample_flow_event()
        old_call = TelemetryRulesAgent.__call__

        def _slow_call(self, state):  # type: ignore[no-untyped-def]
            time.sleep(timeout_s * 5)
            return old_call(self, state)

        TelemetryRulesAgent.__call__ = _slow_call  # type: ignore[assignment]
        try:
            r = orch.analyze_event(flow_event)
        finally:
            TelemetryRulesAgent.__call__ = old_call  # type: ignore[assignment]

        ok = isinstance(r, dict) and _is_degraded(r) and _has_substring_errors(r, "timed out after")
        checks.append(
            {
                "name": "telemetry_agent_timeout_degraded",
                "ok": ok,
                "detail": {"degraded": _is_degraded(r), "errors_head": (r.get("errors") or [])[:2]},
            }
        )

        # 2) Induce failure in telemetry agent; aggregation should still succeed with degraded + surfaced error.
        def _fail_call(self, state):  # type: ignore[no-untyped-def]
            raise RuntimeError("phase4 induced failure")

        TelemetryRulesAgent.__call__ = _fail_call  # type: ignore[assignment]
        try:
            r2 = orch.analyze_event(flow_event)
        finally:
            TelemetryRulesAgent.__call__ = old_call  # type: ignore[assignment]

        ok2 = isinstance(r2, dict) and _is_degraded(r2) and _has_substring_errors(r2, "failed")
        checks.append(
            {
                "name": "telemetry_partial_failure_degraded",
                "ok": ok2,
                "detail": {"degraded": _is_degraded(r2), "errors_head": (r2.get("errors") or [])[:2]},
            }
        )

        # 3) Induce timeout in event graph by slowing SequenceRiskAgent (runs on ACTION_EVENT).
        action_event = _build_sample_action_event()
        old_seq_call = SequenceRiskAgent.__call__

        def _slow_seq(self, state):  # type: ignore[no-untyped-def]
            time.sleep(timeout_s * 5)
            return old_seq_call(self, state)

        SequenceRiskAgent.__call__ = _slow_seq  # type: ignore[assignment]
        try:
            r3 = orch.analyze_event(action_event)
        finally:
            SequenceRiskAgent.__call__ = old_seq_call  # type: ignore[assignment]

        ok3 = isinstance(r3, dict) and _is_degraded(r3) and _has_substring_errors(r3, "timed out after")
        checks.append(
            {
                "name": "event_agent_timeout_degraded",
                "ok": ok3,
                "detail": {"degraded": _is_degraded(r3), "errors_head": (r3.get("errors") or [])[:2]},
            }
        )

        # 4) No silent drop: if degraded is True for agent_error, errors must be non-empty.
        no_silent = True
        for rr in (r, r2, r3):
            if isinstance(rr, dict) and _is_degraded(rr):
                if not (rr.get("errors") or []):
                    no_silent = False
        checks.append({"name": "no_silent_error_drop", "ok": no_silent, "detail": ""})

    finally:
        if old_timeout is None:
            os.environ.pop("SENTINEL_AGENT_TIMEOUT_SECONDS", None)
        else:
            os.environ["SENTINEL_AGENT_TIMEOUT_SECONDS"] = old_timeout

    ok_all = all(c.get("ok") for c in checks)
    payload = {
        "summary": {
            "ok_all": ok_all,
            "checks_total": len(checks),
            "checks_passed": sum(1 for c in checks if c.get("ok")),
            "checks_failed": sum(1 for c in checks if not c.get("ok")),
            "timeout_s": timeout_s,
        },
        "checks": checks,
    }

    tag = _now_tag()
    _write_report(out_dir, f"phase4_reliability_{tag}", payload)
    _write_report(out_dir, "phase4_reliability_summary", payload)
    return 0 if ok_all else 2


if __name__ == "__main__":
    raise SystemExit(main())
