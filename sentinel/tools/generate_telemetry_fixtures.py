"""Generate production-grade telemetry fixtures for testing."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Dict, List, Tuple


OUTPUT_DIR = Path(__file__).resolve().parents[1] / "testdata" / "telemetry"


def _base_ipv4() -> Dict[str, Any]:
    return {
        "src_ip": "10.0.0.5",
        "dst_ip": "192.0.2.10",
        "src_port": 44321,
        "dst_port": 80,
        "protocol": 6,
        "tot_fwd_pkts": 120,
        "tot_bwd_pkts": 10,
        "totlen_fwd_pkts": 80000,
        "totlen_bwd_pkts": 5000,
        "duration_ms": 1000,
    }


def _base_ipv6() -> Dict[str, Any]:
    return {
        "src_ip": "2001:db8::1",
        "dst_ip": "2001:db8::2",
        "src_port": 44321,
        "dst_port": 443,
        "protocol": 6,
        "tot_fwd_pkts": 200,
        "tot_bwd_pkts": 40,
        "totlen_fwd_pkts": 120000,
        "totlen_bwd_pkts": 15000,
        "duration_ms": 2000,
    }


def _mutate(base: Dict[str, Any], updates: Dict[str, Any]) -> Dict[str, Any]:
    record = dict(base)
    record.update(updates)
    return record


def _write_json(path: Path, payload: Any) -> None:
    path.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")


def _write_csv(path: Path, records: List[Dict[str, Any]], columns: List[str]) -> None:
    lines = []
    lines.append(",".join(columns))
    for rec in records:
        row = []
        for col in columns:
            val = rec.get(col, "")
            row.append(str(val))
        lines.append(",".join(row))
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def _write_raw(path: Path, content: str) -> None:
    path.write_text(content, encoding="utf-8")


def _valid_cases() -> List[Dict[str, Any]]:
    base4 = _base_ipv4()
    base6 = _base_ipv6()

    return [
        {
            "name": "valid_ipv4_min",
            "records": [base4],
            "expected_current": "pass",
        },
        {
            "name": "valid_ipv6_min",
            "records": [base6],
            "expected_current": "pass",
        },
        {
            "name": "valid_ddos_high_pps",
            "records": [_mutate(base4, {"pps": 1_500_000, "tot_fwd_pkts": 1_500_000})],
            "expected_current": "pass",
        },
        {
            "name": "valid_syn_flood",
            "records": [_mutate(base4, {"syn_flag_cnt": 10_000, "ack_flag_cnt": 10})],
            "expected_current": "pass",
        },
        {
            "name": "valid_port_scan",
            "records": [_mutate(base4, {"unique_dst_ports_batch": 700})],
            "expected_current": "pass",
        },
        {
            "name": "valid_high_bps",
            "records": [_mutate(base4, {"bps": 250 * 1024 * 1024})],
            "expected_current": "pass",
        },
        {
            "name": "valid_stealth_low_rate",
            "records": [_mutate(base4, {"duration_ms": 60000, "tot_fwd_pkts": 50, "tot_bwd_pkts": 5})],
            "expected_current": "pass",
        },
        {
            "name": "valid_private_ips",
            "records": [_mutate(base4, {"src_ip": "10.1.2.3", "dst_ip": "192.168.1.5"})],
            "expected_current": "pass",
        },
    ]


def _invalid_cases() -> List[Dict[str, Any]]:
    base = _base_ipv4()
    missing_src = dict(base)
    missing_src.pop("src_ip", None)
    missing_dst = dict(base)
    missing_dst.pop("dst_ip", None)
    missing_ports = dict(base)
    missing_ports.pop("src_port", None)
    missing_ports.pop("dst_port", None)

    return [
        {
            "name": "invalid_missing_src_ip",
            "records": [missing_src],
            "expected_current": "fail",
        },
        {
            "name": "invalid_missing_dst_ip",
            "records": [missing_dst],
            "expected_current": "fail",
        },
        {
            "name": "invalid_missing_ports",
            "records": [missing_ports],
            "expected_current": "fail",
        },
        {
            "name": "invalid_ip_format",
            "records": [dict(base, **{"src_ip": "999.999.1.1"})],
            "expected_current": "fail",
        },
        {
            "name": "invalid_injection_string",
            "records": [dict(base, **{"src_ip": "1.1.1.1;DROP TABLE"})],
            "expected_current": "fail",
        },
        {
            "name": "invalid_port_range",
            "records": [dict(base, **{"src_port": 70000})],
            "expected_current": "fail",
        },
        {
            "name": "invalid_protocol_value",
            "records": [dict(base, **{"protocol": "abc"})],
            "expected_current": "fail",
        },
        {
            "name": "invalid_non_numeric",
            "records": [dict(base, **{"tot_fwd_pkts": "abc"})],
            "expected_current": "fail",
        },
        {
            "name": "csv_extra_columns",
            "records": [dict(base, **{"extra_field": "extra"})],
            "expected_current": "pass",
        },
        {
            "name": "duration_zero",
            "records": [dict(base, **{"duration_ms": 0})],
            "expected_current": "pass",
            "expected_target": "warn",
        },
        {
            "name": "duration_negative",
            "records": [dict(base, **{"duration_ms": -100})],
            "expected_current": "pass",
            "expected_target": "fail",
        },
        {
            "name": "extreme_values",
            "records": [
                dict(
                    base,
                    **{
                        "tot_fwd_pkts": 10_000_000,
                        "totlen_fwd_pkts": 10_000_000_000,
                        "duration_ms": 1000,
                        "pps": 10_000_000,
                        "bps": 10_000_000_000,
                    },
                )
            ],
            "expected_current": "pass",
        },
    ]


def _write_case_files(case: Dict[str, Any], manifest: List[Dict[str, Any]]) -> None:
    name = case["name"]
    records = case["records"]

    # JSON list
    json_path = OUTPUT_DIR / f"{name}.json"
    _write_json(json_path, records)
    manifest.append(_manifest_entry(case, json_path, "json_list"))

    # JSON single
    json_single_path = OUTPUT_DIR / f"{name}.single.json"
    _write_json(json_single_path, records[0] if records else {})
    manifest.append(_manifest_entry(case, json_single_path, "json_single"))

    # CSV
    csv_path = OUTPUT_DIR / f"{name}.csv"
    columns = _csv_columns(records)
    _write_csv(csv_path, records, columns)
    manifest.append(_manifest_entry(case, csv_path, "csv"))

    # IPFIX JSON (current adapter expects JSON)
    ipfix_path = OUTPUT_DIR / f"{name}.ipfix.json"
    _write_json(ipfix_path, {"records": records})
    manifest.append(_manifest_entry(case, ipfix_path, "ipfix_json"))


def _csv_columns(records: List[Dict[str, Any]]) -> List[str]:
    if not records:
        return []
    # Union of keys in order of appearance
    columns: List[str] = []
    for rec in records:
        for key in rec.keys():
            if key not in columns:
                columns.append(key)
    return columns


def _manifest_entry(case: Dict[str, Any], path: Path, fmt: str) -> Dict[str, Any]:
    return {
        "name": case["name"],
        "file": str(path.name),
        "format": fmt,
        "expected_current": case.get("expected_current", "pass"),
        "expected_target": case.get("expected_target", case.get("expected_current", "pass")),
    }


def _write_invalid_raw_files(manifest: List[Dict[str, Any]]) -> None:
    bad_json_path = OUTPUT_DIR / "invalid_corrupt_json.json"
    _write_raw(bad_json_path, "{not: valid json")
    manifest.append({
        "name": "invalid_corrupt_json",
        "file": bad_json_path.name,
        "format": "json_raw",
        "expected_current": "fail",
        "expected_target": "fail",
    })

    bad_shape_path = OUTPUT_DIR / "invalid_json_shape.json"
    _write_raw(bad_shape_path, "\"just a string\"")
    manifest.append({
        "name": "invalid_json_shape",
        "file": bad_shape_path.name,
        "format": "json_raw",
        "expected_current": "fail",
        "expected_target": "fail",
    })

    bad_records_path = OUTPUT_DIR / "invalid_json_records_not_list.json"
    _write_raw(bad_records_path, "{\"records\": \"not-a-list\"}")
    manifest.append({
        "name": "invalid_json_records_not_list",
        "file": bad_records_path.name,
        "format": "json_raw",
        "expected_current": "fail",
        "expected_target": "fail",
    })

    bad_ipfix_path = OUTPUT_DIR / "invalid_ipfix_raw.bin"
    _write_raw(bad_ipfix_path, "NOT_IPFIX_BINARY")
    manifest.append({
        "name": "invalid_ipfix_raw",
        "file": bad_ipfix_path.name,
        "format": "ipfix_raw",
        "expected_current": "fail",
        "expected_target": "fail",
    })


def main() -> int:
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

    manifest: List[Dict[str, Any]] = []
    for case in _valid_cases() + _invalid_cases():
        _write_case_files(case, manifest)

    _write_invalid_raw_files(manifest)

    manifest_path = OUTPUT_DIR / "manifest.json"
    _write_json(manifest_path, manifest)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
