"""Validate Phase 4 curated manifest (Sentinel-relevant corpora).

Produces a report that:
- all referenced paths exist
- directories contain expected files
- excluded corpora paths exist (so exclusion is meaningful)
"""

from __future__ import annotations

import argparse
import json
import time
from pathlib import Path
from typing import Any, Dict, List, Tuple


BASE_DIR = Path(__file__).resolve().parents[1]


def _as_path(p: str) -> Path:
    # Manifest uses B:/... style; Path can handle it on Windows.
    return Path(p)


def _stat_path(p: Path) -> Dict[str, Any]:
    if not p.exists():
        return {"exists": False}
    if p.is_dir():
        files = [x for x in p.rglob("*") if x.is_file()]
        return {"exists": True, "type": "dir", "files": len(files)}
    return {"exists": True, "type": "file", "bytes": p.stat().st_size}


def _check_dir_files(dir_path: Path, files: Dict[str, str]) -> Tuple[bool, List[str]]:
    missing: List[str] = []
    for _k, fname in files.items():
        if not (dir_path / fname).exists():
            missing.append(str(dir_path / fname))
    return (len(missing) == 0), missing


def main() -> int:
    ap = argparse.ArgumentParser(description="Validate Phase 4 curated manifest")
    ap.add_argument("--manifest", default=str(BASE_DIR / "testdata" / "curated_manifest.json"))
    ap.add_argument("--out-json", default=str(BASE_DIR / "reports" / "curated_manifest_validation.json"))
    ap.add_argument("--out-md", default=str(BASE_DIR / "reports" / "curated_manifest_validation.md"))
    args = ap.parse_args()

    manifest_path = Path(args.manifest)
    data = json.loads(manifest_path.read_text(encoding="utf-8"))
    datasets = data.get("datasets") or {}

    checks: List[Dict[str, Any]] = []
    ok_all = True

    # Telemetry paths
    telemetry = (datasets.get("telemetry") or {})
    for k in ("zeek_deepflow_jsonl", "ai_scenarios_dir", "edge_cases_jsonl", "aws_vpc_flow_log"):
        item = telemetry.get(k) or {}
        p = _as_path(item.get("path", ""))
        stat = _stat_path(p)
        ok = bool(stat.get("exists"))
        ok_all = ok_all and ok
        checks.append({"name": f"telemetry.{k}", "ok": ok, "path": str(p), "stat": stat})

    excluded = (telemetry.get("excluded_cloud_jsonl") or {})
    excluded_paths = excluded.get("paths") or []
    for pstr in excluded_paths:
        p = _as_path(pstr)
        stat = _stat_path(p)
        ok = bool(stat.get("exists"))
        ok_all = ok_all and ok
        checks.append({"name": "telemetry.excluded_cloud_jsonl", "ok": ok, "path": str(p), "stat": stat})

    # Phase3 events
    phase3 = ((datasets.get("phase3_events") or {}).get("dir") or {})
    p3_dir = _as_path(phase3.get("path", ""))
    stat = _stat_path(p3_dir)
    ok = bool(stat.get("exists"))
    ok_files = False
    missing_files: List[str] = []
    if ok:
        ok_files, missing_files = _check_dir_files(p3_dir, phase3.get("files") or {})
        ok = ok and ok_files
    ok_all = ok_all and ok
    checks.append(
        {
            "name": "phase3_events.dir",
            "ok": ok,
            "path": str(p3_dir),
            "stat": stat,
            "missing_files": missing_files,
        }
    )

    # OSS outputs
    oss = ((datasets.get("oss_outputs") or {}).get("incoming_dir") or {})
    oss_dir = _as_path(oss.get("path", ""))
    stat = _stat_path(oss_dir)
    ok = bool(stat.get("exists"))
    ok_all = ok_all and ok
    checks.append({"name": "oss_outputs.incoming_dir", "ok": ok, "path": str(oss_dir), "stat": stat})

    # File corpus
    fc = ((datasets.get("file_corpus") or {}).get("base_dir") or {})
    base = _as_path(fc.get("path", ""))
    stat = _stat_path(base)
    ok = bool(stat.get("exists"))
    ok_all = ok_all and ok
    checks.append({"name": "file_corpus.base_dir", "ok": ok, "path": str(base), "stat": stat})
    for name, rel in (fc.get("paths") or {}).items():
        p = base / rel
        stat = _stat_path(p)
        ok = bool(stat.get("exists"))
        ok_all = ok_all and ok
        checks.append({"name": f"file_corpus.{name}", "ok": ok, "path": str(p), "stat": stat})

    payload = {
        "manifest": str(manifest_path),
        "generated_at": time.strftime("%Y-%m-%d %H:%M:%S"),
        "ok_all": ok_all,
        "checks": checks,
    }

    out_json = Path(args.out_json)
    out_md = Path(args.out_md)
    out_json.parent.mkdir(parents=True, exist_ok=True)
    out_json.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")

    lines: List[str] = []
    lines.append("# Curated Manifest Validation")
    lines.append("")
    lines.append(f"- manifest: `{manifest_path}`")
    lines.append(f"- generated: {payload['generated_at']}")
    lines.append(f"- ok_all: {ok_all}")
    lines.append("")
    lines.append("## Checks")
    for c in checks:
        status = "PASS" if c.get("ok") else "FAIL"
        lines.append(f"- {c['name']}: {status} ({c.get('path')})")
        missing = c.get("missing_files") or []
        if missing:
            lines.append(f"  missing_files={len(missing)}")
    lines.append("")

    out_md.write_text("\n".join(lines), encoding="utf-8")
    return 0 if ok_all else 2


if __name__ == "__main__":
    raise SystemExit(main())

