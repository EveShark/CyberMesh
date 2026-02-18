"""Summarize gateway replay outputs (NDJSON) into a coverage report.

This reads `reports/parallel_*.ndjson` or any set of NDJSON analysis outputs
and produces a deterministic summary:
- agent bucket presence counts (metadata.agents.*)
- threat level distribution
- degraded/error counts
"""

from __future__ import annotations

import argparse
import glob
import json
import time
from collections import Counter
from pathlib import Path
from typing import Any, Dict, List, Tuple


def _extract_error_codes(errors: List[Any]) -> List[str]:
    codes: List[str] = []
    for e in errors or []:
        if not isinstance(e, str):
            continue
        if e.startswith("ERR_"):
            codes.append(e.split(":", 1)[0])
            continue
        if "ERR_" in e:
            i = e.find("ERR_")
            codes.append(e[i:].split(":", 1)[0].split()[0])
    return codes


def _read_ndjson(path: Path) -> List[Dict[str, Any]]:
    items: List[Dict[str, Any]] = []
    for raw in path.read_text(encoding="utf-8", errors="ignore").splitlines():
        if not raw.strip():
            continue
        try:
            obj = json.loads(raw)
        except Exception:
            continue
        if isinstance(obj, dict):
            items.append(obj)
    return items


def _unwrap_result(obj: Dict[str, Any]) -> Tuple[Dict[str, Any], str | None]:
    # --dir mode wraps as {"file_path": ..., "result": {...}} (or "error").
    if "result" in obj and isinstance(obj.get("result"), dict):
        return obj["result"], obj.get("file_path") if isinstance(obj.get("file_path"), str) else None
    return obj, None


def main() -> int:
    ap = argparse.ArgumentParser(description="Gateway coverage report generator")
    ap.add_argument("--glob", default="reports/parallel_*.ndjson", help="Glob for NDJSON result files")
    ap.add_argument("--out-json", default="reports/gateway_coverage_parallel.json", help="Output JSON path")
    ap.add_argument("--out-md", default="reports/gateway_coverage_parallel.md", help="Output Markdown path")
    args = ap.parse_args()

    paths = [Path(p) for p in sorted(glob.glob(args.glob))]
    out_json = Path(args.out_json)
    out_md = Path(args.out_md)

    agent_counts: Counter[str] = Counter()
    modality_counts: Counter[str] = Counter()
    threat_counts: Counter[str] = Counter()
    error_code_counts: Counter[str] = Counter()
    file_summaries: List[Dict[str, Any]] = []

    for p in paths:
        items = _read_ndjson(p)
        per_agents: Counter[str] = Counter()
        per_threat: Counter[str] = Counter()
        per_mod: Counter[str] = Counter()
        degraded = 0
        error_events = 0

        for obj in items:
            obj, _file_path = _unwrap_result(obj)
            tl = obj.get("threat_level")
            if isinstance(tl, dict):
                tl = tl.get("value")
            threat = str(tl)
            threat_counts[threat] += 1
            per_threat[threat] += 1

            meta = obj.get("metadata") or {}
            if isinstance(meta, dict) and meta.get("degraded"):
                degraded += 1

            errs = obj.get("errors") or []
            if errs:
                error_events += 1
            for code in _extract_error_codes(errs):
                error_code_counts[code] += 1

            agents = (meta.get("agents") or {}) if isinstance(meta, dict) else {}
            if isinstance(agents, dict):
                for a in agents.keys():
                    agent_counts[a] += 1
                    per_agents[a] += 1

            ev = obj.get("event")
            if isinstance(ev, dict) and "modality" in ev:
                m = str(ev.get("modality"))
                modality_counts[m] += 1
                per_mod[m] += 1

        file_summaries.append(
            {
                "path": str(p),
                "events": len(items),
                "degraded_events": degraded,
                "error_events": error_events,
                "agent_buckets": dict(per_agents),
                "threat_levels": dict(per_threat),
                "modalities": dict(per_mod),
            }
        )

    payload: Dict[str, Any] = {
        "generated_at": time.strftime("%Y-%m-%d %H:%M:%S"),
        "glob": args.glob,
        "files": file_summaries,
        "totals": {
            "files": len(file_summaries),
            "events": sum(f["events"] for f in file_summaries),
            "degraded_events": sum(f["degraded_events"] for f in file_summaries),
            "error_events": sum(f["error_events"] for f in file_summaries),
            "agent_buckets": dict(agent_counts),
            "threat_levels": dict(threat_counts),
            "modalities": dict(modality_counts),
            "error_codes": dict(error_code_counts),
        },
    }

    out_json.parent.mkdir(parents=True, exist_ok=True)
    out_json.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")

    lines: List[str] = []
    lines.append("# Gateway Coverage (Parallel)")
    lines.append("")
    lines.append(f"- Generated: {payload['generated_at']}")
    lines.append(f"- Glob: `{args.glob}`")
    lines.append(f"- Files: {payload['totals']['files']}")
    lines.append(f"- Events: {payload['totals']['events']}")
    lines.append(f"- Degraded events: {payload['totals']['degraded_events']}")
    lines.append(f"- Error events: {payload['totals']['error_events']}")
    lines.append("")

    lines.append("## Agent Buckets (Total Occurrences)")
    for k, v in sorted(agent_counts.items()):
        lines.append(f"- `{k}`: {v}")
    lines.append("")

    lines.append("## Threat Levels (All Results)")
    for k, v in sorted(threat_counts.items()):
        lines.append(f"- `{k}`: {v}")
    lines.append("")

    lines.append("## Error Codes (All Results)")
    if error_code_counts:
        for k, v in sorted(error_code_counts.items()):
            lines.append(f"- `{k}`: {v}")
    else:
        lines.append("- (none)")
    lines.append("")

    lines.append("## Per File")
    for f in file_summaries:
        name = Path(f["path"]).name
        agents_list = sorted(list((f.get("agent_buckets") or {}).keys()))
        lines.append(
            f"- `{name}`: events={f['events']} degraded={f['degraded_events']} errors={f['error_events']} agents={agents_list}"
        )
    lines.append("")

    out_md.write_text("\n".join(lines), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
