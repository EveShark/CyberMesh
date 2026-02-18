"""Compare sequential vs parallel gateway outputs for determinism (stable fields only).

Inputs are NDJSON files produced by `main.py --out ...`.
Matches entries by:
- event.id if present (adapter/event outputs)
- file_path wrapper key for --dir outputs

Ignores timing/timestamps and other inherently nondeterministic fields.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any, Dict, List, Tuple


STABLE_KEYS = (
    "threat_level",
    "final_score",
    "confidence",
    "findings",
    "indicators",
    "errors",
)


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


def _get_key(obj: Dict[str, Any]) -> str:
    if "file_path" in obj and isinstance(obj.get("file_path"), str):
        return f"file:{obj['file_path']}"
    ev = obj.get("event")
    if isinstance(ev, dict) and ev.get("id"):
        return f"event:{ev.get('id')}"
    # Fallback: stable hash over stable keys (best-effort)
    core = {k: obj.get(k) for k in STABLE_KEYS if k in obj}
    return "hash:" + str(hash(json.dumps(core, sort_keys=True, default=str)))


def _stable(obj: Any) -> Any:
    if isinstance(obj, dict):
        out: Dict[str, Any] = {}
        for k in STABLE_KEYS:
            if k in obj:
                out[k] = _stable(obj.get(k))
        meta = obj.get("metadata")
        if isinstance(meta, dict):
            out_meta: Dict[str, Any] = {}
            for mk in ("degraded", "degraded_reasons"):
                if mk in meta:
                    out_meta[mk] = _stable(meta.get(mk))
            opa = meta.get("opa")
            if isinstance(opa, dict):
                out_meta["opa"] = {"status": opa.get("status"), "allow": opa.get("allow"), "reason": opa.get("reason")}
            if out_meta:
                out["metadata"] = out_meta
        return out
    if isinstance(obj, list):
        return [_stable(v) for v in obj]
    if hasattr(obj, "value"):
        return getattr(obj, "value")
    return obj


def _index(items: List[Dict[str, Any]]) -> Dict[str, Dict[str, Any]]:
    out: Dict[str, Dict[str, Any]] = {}
    for it in items:
        out[_get_key(it)] = it
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description="Gateway determinism checker (sequential vs parallel)")
    ap.add_argument("--baseline", required=True, help="Baseline NDJSON (typically sequential)")
    ap.add_argument("--candidate", required=True, help="Candidate NDJSON (typically parallel)")
    ap.add_argument("--out-json", default="reports/gateway_determinism.json", help="Output JSON")
    ap.add_argument("--out-md", default="reports/gateway_determinism.md", help="Output Markdown")
    args = ap.parse_args()

    base_path = Path(args.baseline)
    cand_path = Path(args.candidate)

    base_items = _read_ndjson(base_path)
    cand_items = _read_ndjson(cand_path)
    base_idx = _index(base_items)
    cand_idx = _index(cand_items)

    keys = sorted(set(base_idx.keys()) | set(cand_idx.keys()))
    mismatches: List[Dict[str, Any]] = []
    missing_in_candidate = 0
    missing_in_baseline = 0

    for k in keys:
        b = base_idx.get(k)
        c = cand_idx.get(k)
        if b is None:
            missing_in_baseline += 1
            continue
        if c is None:
            missing_in_candidate += 1
            continue
        sb = _stable(b.get("result") if "result" in b else b)
        sc = _stable(c.get("result") if "result" in c else c)
        if sb != sc:
            mismatches.append({"key": k, "baseline": sb, "candidate": sc})

    payload = {
        "baseline": str(base_path),
        "candidate": str(cand_path),
        "baseline_events": len(base_items),
        "candidate_events": len(cand_items),
        "missing_in_baseline": missing_in_baseline,
        "missing_in_candidate": missing_in_candidate,
        "mismatches": mismatches[:50],
        "mismatch_count": len(mismatches),
        "ok": (missing_in_baseline == 0 and missing_in_candidate == 0 and len(mismatches) == 0),
    }

    out_json = Path(args.out_json)
    out_md = Path(args.out_md)
    out_json.parent.mkdir(parents=True, exist_ok=True)
    out_json.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")

    lines: List[str] = []
    lines.append("# Gateway Determinism Check")
    lines.append("")
    lines.append(f"- baseline: `{base_path}`")
    lines.append(f"- candidate: `{cand_path}`")
    lines.append(f"- baseline events: {len(base_items)}")
    lines.append(f"- candidate events: {len(cand_items)}")
    lines.append(f"- missing in candidate: {missing_in_candidate}")
    lines.append(f"- missing in baseline: {missing_in_baseline}")
    lines.append(f"- mismatch count: {len(mismatches)}")
    lines.append(f"- ok: {payload['ok']}")
    lines.append("")
    if mismatches:
        lines.append("## Sample Mismatches (first 5)")
        for m in mismatches[:5]:
            lines.append(f"- {m['key']}")
    out_md.write_text("\n".join(lines), encoding="utf-8")
    return 0 if payload["ok"] else 2


if __name__ == "__main__":
    raise SystemExit(main())

