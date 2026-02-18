"""Standalone runner for the hybrid Sentinel pipeline."""

from __future__ import annotations

import argparse
import json
import os
import time
from dataclasses import asdict, is_dataclass
from enum import Enum
from pathlib import Path
from typing import Any

from sentinel.agents import SentinelOrchestrator
from sentinel.agents.event_builder import build_flow_event
from sentinel.ingest import OSSAdapter, load_adapter_spec, load_records
from sentinel.telemetry.adapters import parse_csv, parse_json, parse_ipfix
from sentinel.telemetry.adapters import parse_aws_vpc_flow_log
from sentinel.ingest.telemetry_ingest import load_telemetry_jsonl, telemetry_records_to_flow_events, TelemetryIngestLimits


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


def _write_ndjson(path: Path, items: list[dict[str, Any]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        for item in items:
            f.write(json.dumps(_normalize(item), sort_keys=True))
            f.write("\n")


def _truncate_errors(errors: list[str], *, max_items: int = 50, max_total_chars: int = 16_384) -> list[str]:
    """Bound error lists so output artifacts cannot explode in size."""
    out: list[str] = []
    total = 0
    for e in errors or []:
        if not isinstance(e, str):
            continue
        if len(out) >= max_items:
            break
        # Cap per-string and overall size.
        s = e[:1024]
        if total + len(s) > max_total_chars:
            break
        out.append(s)
        total += len(s)
    if errors and len(out) < len(errors):
        out.append(f"... truncated {len(errors) - len(out)} more")
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description="Sentinel hybrid pipeline runner")
    parser.add_argument("file", help="Path to file or telemetry payload")
    parser.add_argument(
        "--models-path",
        default=None,
        help="Path to ML models directory",
    )
    parser.add_argument(
        "--enable-llm",
        action="store_true",
        help="Enable LLM reasoning (off by default)",
    )
    parser.add_argument(
        "--no-fast-path",
        action="store_true",
        help="Disable fast-path (hash/YARA/signatures)",
    )
    parser.add_argument(
        "--no-threat-intel",
        action="store_true",
        help="Disable threat intel enrichment",
    )
    parser.add_argument(
        "--no-external-yara-rules",
        action="store_true",
        help="Disable external YARA rules (use built-in only)",
    )
    parser.add_argument(
        "--telemetry",
        action="store_true",
        help="Treat input as telemetry payload (CSV/JSON/IPFIX)",
    )
    parser.add_argument(
        "--dir",
        action="store_true",
        help="Treat input as a directory and analyze all files under it (FILE modality). Requires --out.",
    )
    parser.add_argument(
        "--max-files",
        type=int,
        default=0,
        help="Max number of files to analyze in --dir mode (0 = no limit).",
    )
    parser.add_argument(
        "--max-file-bytes",
        type=int,
        default=50 * 1024 * 1024,
        help="Skip files larger than this size in bytes in --dir mode.",
    )
    parser.add_argument(
        "--adapter-spec",
        default=None,
        help="Adapter spec path (JSON/YAML). If set, ingest input via OSSAdapter and route through analyze_event().",
    )
    parser.add_argument(
        "--adapter-format",
        choices=["json", "ndjson", "text"],
        default=None,
        help="Adapter input format for --adapter-spec (default: infer from suffix)",
    )
    parser.add_argument(
        "--adapter-max-payload-bytes",
        type=int,
        default=0,
        help="Override adapter max_payload_bytes for this run (0 = use spec default).",
    )
    parser.add_argument(
        "--max-events",
        type=int,
        default=0,
        help="Max number of events to analyze (0 = no limit).",
    )
    parser.add_argument(
        "--out",
        default=None,
        help="Optional output NDJSON path for analysis results (recommended for large corpora).",
    )
    parser.add_argument(
        "--tenant-id",
        default="cli",
        help="Tenant id for telemetry analysis (default: cli)",
    )
    parser.add_argument(
        "--sequential",
        action="store_true",
        help="Run agents sequentially (baseline mode)",
    )
    parser.add_argument(
        "--format",
        choices=["csv", "json", "ipfix"],
        default="json",
        help="Telemetry payload format (default: json)",
    )
    args = parser.parse_args()

    engine = SentinelOrchestrator(
        models_path=args.models_path,
        enable_llm=args.enable_llm,
        enable_fast_path=not args.no_fast_path,
        enable_threat_intel=not args.no_threat_intel,
        use_external_rules=not args.no_external_yara_rules,
        sequential=args.sequential,
    )

    input_path = Path(args.file)

    if args.dir:
        if not input_path.exists() or not input_path.is_dir():
            print(json.dumps({"errors": [f"input is not a directory: {input_path}"]}, indent=2, sort_keys=True))
            return 2
        if not args.out:
            print(json.dumps({"errors": ["--dir requires --out (NDJSON output path)"]}, indent=2, sort_keys=True))
            return 2

        max_files = int(args.max_files or 0)
        max_bytes = int(args.max_file_bytes or 0)

        items: list[dict[str, Any]] = []
        analyzed = 0
        skipped = 0
        errors = 0
        t0 = time.perf_counter()

        for p in sorted(input_path.rglob("*")):
            if not p.is_file():
                continue
            if max_files and analyzed >= max_files:
                break
            if max_bytes and p.stat().st_size > max_bytes:
                skipped += 1
                continue
            try:
                result = engine.analyze_file(str(p), skip_fast_path=args.no_fast_path)
                items.append({"file_path": str(p), "result": result})
            except Exception as exc:  # pylint: disable=broad-except
                errors += 1
                items.append({"file_path": str(p), "error": str(exc)})
            analyzed += 1

        _write_ndjson(Path(args.out), items)
        elapsed_ms = (time.perf_counter() - t0) * 1000
        payload = {
            "input_dir": str(input_path),
            "files_analyzed": analyzed,
            "files_skipped": skipped,
            "files_errors": errors,
            "analysis_time_ms": round(elapsed_ms, 2),
            "out": str(args.out),
        }
        print(json.dumps(payload, indent=2, sort_keys=True))
        strict = os.getenv("SENTINEL_CLI_STRICT", "").strip().lower() in ("1", "true", "yes", "on")
        return 0 if (not strict or errors == 0) else 2

    # Adapter ingest mode: convert JSON/NDJSON via adapter spec into CanonicalEvents,
    # then route through analyze_event() so gating/metadata behavior is consistent.
    if args.adapter_spec:
        spec = load_adapter_spec(args.adapter_spec)
        if args.adapter_max_payload_bytes and args.adapter_max_payload_bytes > 0:
            spec.limits.max_payload_bytes = int(args.adapter_max_payload_bytes)
        fmt = args.adapter_format
        if not fmt:
            if input_path.suffix.lower() in (".jsonl", ".ndjson", ".log"):
                fmt = "ndjson"
            elif input_path.suffix.lower() in (".txt",):
                fmt = "text"
            else:
                fmt = "json"

        records, load_errs = load_records(input_path, fmt, spec.limits, records_path=spec.records_path)
        if load_errs:
            print(json.dumps({"errors": load_errs}, indent=2, sort_keys=True))
            return 2

        if args.max_events and args.max_events > 0:
            records = records[: args.max_events]

        adapter = OSSAdapter(spec)
        events, evt_errs = adapter.records_to_events(records, tenant_id=args.tenant_id)

        t0 = time.perf_counter()
        results = [engine.analyze_event(ev) for ev in events]
        elapsed_ms = (time.perf_counter() - t0) * 1000

        out_path = None
        if args.out:
            out_path = Path(args.out)
            items = list(results)
            if evt_errs:
                # Persist ingest errors alongside results so batch artifacts don't silently drop failures.
                items.append(
                    {
                        "_type": "ingest_summary",
                        "source": "oss_adapter",
                        "input": str(input_path),
                        "adapter_spec": str(args.adapter_spec),
                        "records": len(records),
                        "events": len(events),
                        "errors": _truncate_errors(list(evt_errs)),
                        "threat_level": {"value": "unknown"},
                        "metadata": {
                            "degraded": True,
                            "degraded_reasons": ["adapter_event_errors"],
                            "agents": {},
                        },
                        "event": {
                            "id": "ingest-summary",
                            "timestamp": time.time(),
                            "tenant_id": args.tenant_id,
                            "source": str(spec.source),
                            "modality": str(spec.modality),
                            "features_version": str(spec.features_version),
                        },
                    }
                )
            _write_ndjson(out_path, items)

        payload = {
            "adapter_spec": str(args.adapter_spec),
            "input": str(input_path),
            "format": fmt,
            "records": len(records),
            "events": len(events),
            "load_errors": load_errs,
            "event_errors": _truncate_errors(list(evt_errs)),
            "analysis_time_ms": round(elapsed_ms, 2),
            "out": str(out_path) if out_path else None,
            "results": None if out_path else _normalize(results),
        }
        print(json.dumps(payload, indent=2, sort_keys=True))
        # Adapter record errors are non-fatal in batch replay mode; return 0 unless strict.
        strict = os.getenv("SENTINEL_CLI_STRICT", "").strip().lower() in ("1", "true", "yes", "on")
        return 0 if (not strict or not (load_errs or evt_errs)) else 2

    if not args.telemetry:
        result = engine.analyze_file(str(input_path), skip_fast_path=args.no_fast_path)
        print(json.dumps(_normalize(result), indent=2, sort_keys=True))
        return 0

    if args.format == "json" and input_path.suffix.lower() in (".jsonl", ".ndjson"):
        # JSONL ingest path: preserve record-level context, enforce limits, and apply OPA/attestation
        # consistently via analyze_event().
        records_iter, load_errs = load_telemetry_jsonl(
            input_path,
            limits=TelemetryIngestLimits(),
        )
        if load_errs:
            print(json.dumps({"errors": load_errs}, indent=2, sort_keys=True))
            return 2
        events, ingest_errs = telemetry_records_to_flow_events(
            records_iter,
            tenant_default=args.tenant_id,
            source="cli_telemetry",
            limits=TelemetryIngestLimits(),
        )
        results = [engine.analyze_event(ev) for ev in events]
        if args.out:
            out_items = list(results)
            if ingest_errs:
                out_items.append(
                    {
                        "_type": "ingest_summary",
                        "source": "telemetry_jsonl",
                        "input": str(input_path),
                        "events": len(events),
                        "errors": _truncate_errors(list(ingest_errs)),
                        "threat_level": {"value": "unknown"},
                        "metadata": {
                            "degraded": True,
                            "degraded_reasons": ["telemetry_ingest_errors"],
                            "agents": {},
                        },
                        "event": {
                            "id": "ingest-summary",
                            "timestamp": time.time(),
                            "tenant_id": args.tenant_id,
                            "source": "cli_telemetry",
                            "modality": "network_flow",
                            "features_version": "NetworkFlowFeaturesV1",
                        },
                    }
                )
            _write_ndjson(Path(args.out), out_items)
            output = {
                "input": str(input_path),
                "telemetry_format": "jsonl",
                "tenant_id": args.tenant_id,
                "events": len(events),
                "ingest_errors": _truncate_errors(list(ingest_errs)),
                "out": str(args.out),
            }
            print(json.dumps(output, indent=2, sort_keys=True))
            # Ingest errors in telemetry JSONL are non-fatal for analysis; return 0 unless explicitly strict.
            strict = os.getenv("SENTINEL_CLI_STRICT", "").strip().lower() in ("1", "true", "yes", "on")
            return 0 if (not strict or not ingest_errs) else 2
        output = {"ingest_errors": ingest_errs, "results": _normalize(results)}
        print(json.dumps(output, indent=2, sort_keys=True))
        strict = os.getenv("SENTINEL_CLI_STRICT", "").strip().lower() in ("1", "true", "yes", "on")
        return 0 if (not strict or not ingest_errs) else 2

    payload_bytes = input_path.read_bytes()
    if args.format == "csv":
        text = payload_bytes.decode("utf-8", errors="ignore")
        first = ""
        for line in text.splitlines():
            if line.strip():
                first = line.strip()
                break
        # Auto-detect AWS VPC Flow Logs (space-delimited v2+).
        if first:
            parts = first.split()
            if len(parts) >= 14 and parts[0] in ("2", "3", "4", "5"):
                features_list, errors = parse_aws_vpc_flow_log(text)
            else:
                features_list, errors = parse_csv(text)
        else:
            features_list, errors = [], ["empty csv payload"]
    elif args.format == "ipfix":
        features_list, errors = parse_ipfix(payload_bytes)
    else:
        features_list, errors = parse_json(payload_bytes.decode("utf-8", errors="ignore"))

    if errors:
        print(json.dumps({"errors": errors}, indent=2, sort_keys=True))
        return 2

    # Apply OPA/attestation consistently by analyzing CanonicalEvents.
    events = [
        build_flow_event(
            features=f,
            raw_context={"_input_path": str(input_path), "_input_type": f"telemetry_{args.format}"},
            tenant_id=args.tenant_id,
            source="cli_telemetry",
        )
        for f in features_list
    ]
    results = [engine.analyze_event(ev) for ev in events]
    if args.out:
        _write_ndjson(Path(args.out), results)
        output = {
            "input": str(input_path),
            "telemetry_format": args.format,
            "tenant_id": args.tenant_id,
            "events": len(events),
            "out": str(args.out),
        }
        print(json.dumps(output, indent=2, sort_keys=True))
        return 0
    print(json.dumps(_normalize(results), indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
