#!/usr/bin/env python3
"""Fail fast when telemetry verdicts indicate duration-quality regression."""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def _iter_results(path: Path):
    for line in path.read_text(encoding="utf-8", errors="ignore").splitlines():
        raw = line.strip()
        if not raw or not raw.startswith("{"):
            continue
        try:
            obj = json.loads(raw)
        except Exception:
            continue
        yield obj


def main() -> int:
    ap = argparse.ArgumentParser(description="Telemetry duration quality gate.")
    ap.add_argument("--input", required=True, help="NDJSON file with sentinel.result.v1 envelopes")
    ap.add_argument(
        "--max-zero-duration-ratio",
        type=float,
        default=0.2,
        help="Max allowed ratio of records with counters>0 and flow_duration==0",
    )
    ap.add_argument(
        "--min-records",
        type=int,
        default=50,
        help="Minimum records required to enforce gate",
    )
    args = ap.parse_args()

    path = Path(args.input)
    if not path.exists():
        print(f"ERROR: input file not found: {path}")
        return 2

    total = 0
    zero_duration_with_counters = 0
    for obj in _iter_results(path):
        analysis = obj.get("payload", {}).get("analysis", {})
        flow = analysis.get("flow_features", {})
        if not isinstance(flow, dict):
            continue
        total += 1
        counters = int(flow.get("tot_fwd_pkts", 0)) + int(flow.get("tot_bwd_pkts", 0))
        counters += int(flow.get("totlen_fwd_pkts", 0)) + int(flow.get("totlen_bwd_pkts", 0))
        duration = float(flow.get("flow_duration", 0.0) or 0.0)
        if counters > 0 and duration <= 0:
            zero_duration_with_counters += 1

    if total < args.min_records:
        print(
            f"SKIP: records={total} < min_records={args.min_records}; "
            "not enough sample size for quality gate"
        )
        return 0

    ratio = zero_duration_with_counters / float(total)
    print(
        "telemetry_duration_quality "
        f"records={total} zero_duration_with_counters={zero_duration_with_counters} ratio={ratio:.4f}"
    )
    if ratio > args.max_zero_duration_ratio:
        print(
            "FAIL: duration quality gate violated "
            f"(ratio {ratio:.4f} > max {args.max_zero_duration_ratio:.4f})"
        )
        return 1
    print("PASS: duration quality gate")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

