"""Standalone OSS ingest runner (batch)."""

from __future__ import annotations

import argparse
import json
from dataclasses import asdict, is_dataclass
from enum import Enum
from pathlib import Path
from typing import Any

from sentinel.agents import SentinelOrchestrator
from sentinel.ingest import load_adapter_spec, load_records, OSSAdapter


def _normalize(obj: Any) -> Any:
    if isinstance(obj, dict):
        return {k: _normalize(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [_normalize(v) for v in obj]
    if hasattr(obj, "to_dict"):
        return _normalize(obj.to_dict())
    if is_dataclass(obj):
        return _normalize(asdict(obj))
    if isinstance(obj, Enum):
        return obj.value
    if isinstance(obj, Path):
        return str(obj)
    return obj


def main() -> int:
    parser = argparse.ArgumentParser(description="OSS ingest runner (standalone)")
    parser.add_argument("--adapter-spec", required=True, help="Path to adapter spec (json/yaml)")
    parser.add_argument("--input", required=True, help="Path to OSS output file")
    parser.add_argument("--format", choices=["json", "ndjson"], default="ndjson")
    parser.add_argument("--tenant-id", default=None, help="Override tenant_id")
    parser.add_argument("--no-threat-intel", action="store_true", help="Disable threat intel")
    args = parser.parse_args()

    spec = load_adapter_spec(args.adapter_spec)
    records, errors = load_records(
        args.input,
        args.format,
        spec.limits,
        records_path=spec.records_path,
    )
    if errors:
        print(json.dumps({"errors": errors}, indent=2))
        return 2

    adapter = OSSAdapter(spec)
    events, event_errors = adapter.records_to_events(records, tenant_id=args.tenant_id)
    if event_errors:
        print(json.dumps({"errors": event_errors}, indent=2))
        return 2

    orchestrator = SentinelOrchestrator(enable_threat_intel=not args.no_threat_intel)
    results = [orchestrator.analyze_event(e) for e in events]
    print(json.dumps(_normalize(results), indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
