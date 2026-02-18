"""Adapters to convert telemetry payloads into NetworkFlowFeaturesV1."""

from __future__ import annotations

import csv
import json
import ipaddress
from typing import Any, Dict, List, Optional, Tuple

from ..contracts.schemas import NetworkFlowFeaturesV1


def parse_csv(text: str, delimiter: str = ",") -> Tuple[List[NetworkFlowFeaturesV1], List[str]]:
    """Parse CSV payload into NetworkFlowFeaturesV1 list."""
    features_list: List[NetworkFlowFeaturesV1] = []
    errors: List[str] = []

    reader = csv.DictReader(text.splitlines(), delimiter=delimiter)
    for idx, row in enumerate(reader):
        features, errs = normalize_flow_record(row)
        if errs:
            errors.append(f"row {idx + 1}: {', '.join(errs)}")
            continue
        if features:
            features_list.append(features)

    return features_list, errors


def parse_aws_vpc_flow_log(text: str) -> Tuple[List[NetworkFlowFeaturesV1], List[str]]:
    """
    Parse AWS VPC Flow Logs (space-delimited, v2+) into NetworkFlowFeaturesV1.

    Reference format (v2):
    version account-id interface-id srcaddr dstaddr srcport dstport protocol packets bytes start end action log-status

    Notes:
    - Direction isn't present, so we map all packets/bytes to the "forward" direction and set backward to 0.
    - Duration is derived from end-start (seconds) and converted to microseconds.
    - Records with missing core fields are rejected with errors.
    """
    features_list: List[NetworkFlowFeaturesV1] = []
    errors: List[str] = []

    for idx, raw in enumerate(text.splitlines()):
        line = raw.strip()
        if not line:
            continue
        parts = line.split()
        if len(parts) < 14:
            errors.append(f"row {idx + 1}: invalid vpc flow log (expected >=14 fields)")
            continue

        version = parts[0]
        if version not in ("2", "3", "4", "5"):
            errors.append(f"row {idx + 1}: unsupported vpc flow log version: {version}")
            continue

        # Fixed-position fields per AWS spec (v2+).
        # We ignore account/interface/action/log-status for now; keep parsing strict for the 5-tuple.
        src_ip = parts[3]
        dst_ip = parts[4]
        src_port = parts[5]
        dst_port = parts[6]
        proto = parts[7]
        packets = parts[8]
        bytes_ = parts[9]
        start = parts[10]
        end = parts[11]

        record: Dict[str, Any] = {
            "src_ip": src_ip,
            "dst_ip": dst_ip,
            "src_port": src_port,
            "dst_port": dst_port,
            "protocol": proto,
            "tot_fwd_pkts": packets,
            "tot_bwd_pkts": 0,
            "totlen_fwd_pkts": bytes_,
            "totlen_bwd_pkts": 0,
        }

        # Derive duration in microseconds (end/start are epoch seconds).
        try:
            s = float(start)
            e = float(end)
            duration_s = max(e - s, 0.0)
            record["flow_duration"] = duration_s * 1_000_000.0
        except Exception:
            # Keep required-field validation strict: duration is required.
            record["flow_duration"] = None

        features, errs = normalize_flow_record(record)
        if errs:
            errors.append(f"row {idx + 1}: {', '.join(errs)}")
            continue
        if features:
            features_list.append(features)

    return features_list, errors


def parse_json(text: str) -> Tuple[List[NetworkFlowFeaturesV1], List[str]]:
    """Parse JSON payload into NetworkFlowFeaturesV1 list."""
    features_list: List[NetworkFlowFeaturesV1] = []
    errors: List[str] = []

    try:
        payload = json.loads(text)
    except json.JSONDecodeError as exc:
        return [], [f"invalid json: {exc}"]

    records: List[Dict[str, Any]]
    if isinstance(payload, list):
        records = payload
    elif isinstance(payload, dict) and "records" in payload and isinstance(payload["records"], list):
        records = payload["records"]
    elif isinstance(payload, dict):
        records = [payload]
    else:
        return [], ["json payload must be object or list"]

    for idx, record in enumerate(records):
        features, errs = normalize_flow_record(record)
        if errs:
            errors.append(f"record {idx + 1}: {', '.join(errs)}")
            continue
        if features:
            features_list.append(features)

    return features_list, errors


def parse_ipfix(payload: bytes | str) -> Tuple[List[NetworkFlowFeaturesV1], List[str]]:
    """
    Minimal IPFIX adapter.
    
    If payload is JSON (string/bytes), reuse JSON parsing. Otherwise return error.
    """
    if isinstance(payload, bytes):
        try:
            text = payload.decode("utf-8")
        except UnicodeDecodeError:
            return [], ["ipfix payload is not utf-8 json"]
    else:
        text = payload

    return parse_json(text)


def normalize_flow_record(record: Dict[str, Any]) -> Tuple[Optional[NetworkFlowFeaturesV1], List[str]]:
    """Normalize a flow record to NetworkFlowFeaturesV1."""
    errors: List[str] = []

    src_ip = _get_ip(record, ["src_ip", "source_ip", "sip"])
    dst_ip = _get_ip(record, ["dst_ip", "destination_ip", "dip"])
    src_port = _get_int(record, ["src_port", "sport", "source_port"])
    dst_port = _get_int(record, ["dst_port", "dport", "destination_port"])
    protocol = _get_proto(record, ["protocol", "proto", "ip_proto"])

    tot_fwd_pkts = _get_int(record, ["tot_fwd_pkts", "pkts_fwd", "packets_fwd", "forward_packets"])
    tot_bwd_pkts = _get_int(record, ["tot_bwd_pkts", "pkts_bwd", "packets_bwd", "backward_packets"])
    totlen_fwd_pkts = _get_int(
        record,
        [
            "totlen_fwd_pkts",
            "bytes_fwd",
            "bytes_forward",
            "forward_bytes",
            # Zeek http.log style
            "request_body_len",
            "request_len",
            "req_body_len",
        ],
    )
    totlen_bwd_pkts = _get_int(
        record,
        [
            "totlen_bwd_pkts",
            "bytes_bwd",
            "bytes_backward",
            "backward_bytes",
            # Zeek http.log style
            "response_body_len",
            "response_len",
            "resp_body_len",
        ],
    )

    flow_duration = _get_duration_us(record)
    flow_byts_s = _get_float(record, ["flow_byts_s", "bytes_per_sec", "bps"])
    flow_pkts_s = _get_float(record, ["flow_pkts_s", "pkts_per_sec", "pps"])

    # Zeek http.log records often lack packet counts and duration. For v1 standalone,
    # synthesize minimal values so we don't silently drop high-signal HTTP events.
    source_type = _get_str(record, ["source_type"])
    source_id = _get_str(record, ["source_id"])
    alert_type = _get_str(record, ["alert_type"])
    is_zeek_http = (source_type == "zeek") and (source_id == "http.log" or alert_type == "http")
    if is_zeek_http:
        if totlen_fwd_pkts is None:
            totlen_fwd_pkts = _get_int(record, ["request_body_len", "request_len", "req_body_len"]) or 0
        if totlen_bwd_pkts is None:
            totlen_bwd_pkts = _get_int(record, ["response_body_len", "response_len", "resp_body_len"]) or 0
        if tot_fwd_pkts is None:
            tot_fwd_pkts = 1
        if tot_bwd_pkts is None:
            tot_bwd_pkts = 1
        if flow_duration is None:
            flow_duration = 0.0

        # Clamp synthesized lengths to non-negative.
        totlen_fwd_pkts = max(int(totlen_fwd_pkts), 0)
        totlen_bwd_pkts = max(int(totlen_bwd_pkts), 0)

    # Generic Zeek fallback: for some Zeek-derived records (e.g. rdp.json.gz) we may have
    # 5-tuple but no byte/packet counters. Synthesize minimal metrics so we can route the
    # event through the flow pipeline deterministically.
    is_zeek = source_type == "zeek"
    if is_zeek:
        if flow_duration is None:
            flow_duration = 0.0
        if tot_fwd_pkts is None:
            tot_fwd_pkts = 1
        if tot_bwd_pkts is None:
            tot_bwd_pkts = 1
        if totlen_fwd_pkts is None:
            totlen_fwd_pkts = 0
        if totlen_bwd_pkts is None:
            totlen_bwd_pkts = 0

    # Validate required fields
    required = {
        "src_ip": src_ip,
        "dst_ip": dst_ip,
        "src_port": src_port,
        "dst_port": dst_port,
        "protocol": protocol,
        "tot_fwd_pkts": tot_fwd_pkts,
        "tot_bwd_pkts": tot_bwd_pkts,
        "totlen_fwd_pkts": totlen_fwd_pkts,
        "totlen_bwd_pkts": totlen_bwd_pkts,
        "flow_duration": flow_duration,
    }

    for key, value in required.items():
        if value is None:
            errors.append(f"missing {key}")

    if errors:
        return None, errors

    if flow_duration is not None and flow_duration < 0:
        return None, ["invalid flow_duration (negative)"]

    if not (0 <= src_port <= 65535) or not (0 <= dst_port <= 65535):
        return None, ["invalid port range"]

    # Compute rates if missing
    duration_sec = max(flow_duration / 1_000_000.0, 0.0) if flow_duration else 0.0
    if flow_byts_s is None and duration_sec > 0:
        flow_byts_s = (totlen_fwd_pkts + totlen_bwd_pkts) / duration_sec
    if flow_pkts_s is None and duration_sec > 0:
        flow_pkts_s = (tot_fwd_pkts + tot_bwd_pkts) / duration_sec
    if flow_byts_s is None:
        flow_byts_s = 0.0
    if flow_pkts_s is None:
        flow_pkts_s = 0.0

    features = NetworkFlowFeaturesV1(
        src_ip=src_ip,
        dst_ip=dst_ip,
        src_port=src_port,
        dst_port=dst_port,
        protocol=protocol,
        tot_fwd_pkts=tot_fwd_pkts,
        tot_bwd_pkts=tot_bwd_pkts,
        totlen_fwd_pkts=totlen_fwd_pkts,
        totlen_bwd_pkts=totlen_bwd_pkts,
        flow_duration=float(flow_duration),
        flow_byts_s=float(flow_byts_s),
        flow_pkts_s=float(flow_pkts_s),
    )

    # Optional fields
    features.flow_id = _get_str(record, ["flow_id"])
    features.syn_flag_cnt = _get_int(record, ["syn_flag_cnt", "syn"]) or 0
    features.ack_flag_cnt = _get_int(record, ["ack_flag_cnt", "ack"]) or 0
    features.rst_flag_cnt = _get_int(record, ["rst_flag_cnt", "rst"]) or 0
    features.pps = _get_float(record, ["pps"]) or features.flow_pkts_s
    features.bps = _get_float(record, ["bps"]) or features.flow_byts_s
    features.syn_ack_ratio = _get_float(record, ["syn_ack_ratio"])
    features.unique_dst_ports_batch = _get_int(record, ["unique_dst_ports_batch", "unique_ports"])

    return features, []


def _get_str(record: Dict[str, Any], keys: List[str]) -> Optional[str]:
    for key in keys:
        if key in record and record[key] not in (None, ""):
            return str(record[key])
    return None


def _get_int(record: Dict[str, Any], keys: List[str]) -> Optional[int]:
    for key in keys:
        if key in record and record[key] not in (None, ""):
            try:
                return int(float(record[key]))
            except (ValueError, TypeError):
                return None
    return None


def _get_float(record: Dict[str, Any], keys: List[str]) -> Optional[float]:
    for key in keys:
        if key in record and record[key] not in (None, ""):
            try:
                return float(record[key])
            except (ValueError, TypeError):
                return None
    return None


def _get_proto(record: Dict[str, Any], keys: List[str]) -> Optional[int]:
    for key in keys:
        if key in record and record[key] not in (None, ""):
            value = record[key]
            if isinstance(value, str):
                lower = value.strip().lower()
                if lower == "tcp":
                    return 6
                if lower == "udp":
                    return 17
                if lower == "icmp":
                    return 1
            try:
                return int(float(value))
            except (ValueError, TypeError):
                return None
    return None


def _get_ip(record: Dict[str, Any], keys: List[str]) -> Optional[str]:
    for key in keys:
        if key in record and record[key] not in (None, ""):
            value = str(record[key]).strip()
            try:
                ipaddress.ip_address(value)
                return value
            except ValueError:
                return None
    return None


def _get_duration_us(record: Dict[str, Any]) -> Optional[float]:
    if "duration_us" in record:
        return _get_float(record, ["duration_us"])
    if "flow_duration" in record:
        return _get_float(record, ["flow_duration"])
    if "duration_ms" in record:
        value = _get_float(record, ["duration_ms"])
        return value * 1000 if value is not None else None
    if "duration_s" in record:
        value = _get_float(record, ["duration_s"])
        return value * 1_000_000 if value is not None else None
    if "duration" in record:
        # Heuristic: assume milliseconds if value < 1e6, else microseconds
        value = _get_float(record, ["duration"])
        if value is None:
            return None
        return value * 1000 if value < 1_000_000 else value
    return None
