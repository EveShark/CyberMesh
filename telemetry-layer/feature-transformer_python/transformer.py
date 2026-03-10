from __future__ import annotations

import hashlib
import json
import time
import sys
from pathlib import Path
import sysconfig
import importlib.machinery
import importlib.util
from typing import Any, Dict, List, Optional, Tuple

base_dir = Path(__file__).resolve().parent
if str(base_dir) not in sys.path:
    sys.path.insert(0, str(base_dir))
proto_dir = base_dir.parent / "proto" / "gen" / "python"
if str(proto_dir) not in sys.path:
    sys.path.insert(0, str(proto_dir))

_stdlib = sysconfig.get_paths().get("stdlib", "")
_spec = importlib.machinery.PathFinder.find_spec("logging", [_stdlib])
if not _spec or not _spec.loader:
    raise ImportError("stdlib logging not found")
logging = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(logging)

from config import load_config
from ml.features_flow import FlowFeatureExtractor
from schema import load_flow_schema, load_cic_schema
from registry import RegistryClient, RegistryConfig


logger = logging.getLogger("feature-transformer")

def _init_metrics(port: int):
    try:
        from prometheus_client import Counter, Gauge, start_http_server
    except Exception:
        return None

    start_http_server(port)
    return {
        "events_total": Counter("telemetry_transformer_events_total", "Total events processed"),
        "events_invalid": Counter("telemetry_transformer_events_invalid_total", "Invalid events"),
        "events_dlq": Counter("telemetry_transformer_events_dlq_total", "DLQ events"),
        "events_published": Counter("telemetry_transformer_events_published_total", "Kafka publishes"),
        "events_postgres": Counter("telemetry_transformer_events_postgres_total", "Postgres writes"),
        "events_skipped": Counter("telemetry_transformer_events_skipped_total", "Skipped events"),
        "last_coverage": Gauge("telemetry_transformer_feature_coverage", "Last feature coverage"),
    }


def _now_ts() -> int:
    return int(time.time())


def _hash_payload(payload: bytes) -> str:
    return hashlib.sha256(payload).hexdigest()


def _dlq_envelope(code: str, message: str, payload: bytes) -> Dict[str, Any]:
    return {
        "schema": "dlq.v1",
        "timestamp": _now_ts(),
        "source_component": "feature-transformer",
        "error_code": code,
        "error_message": message,
        "payload_hash": _hash_payload(payload),
    }


def _validate_required(flow: Dict[str, Any], required: List[str]) -> None:
    missing = [key for key in required if flow.get(key) in (None, "")]
    if missing:
        raise ValueError(f"missing required fields: {', '.join(missing)}")


def _load_proto_modules():
    try:
        from telemetry_flow_v1_pb2 import FlowV1  # type: ignore
        from telemetry_feature_v1_pb2 import CicFeaturesV1  # type: ignore
        return FlowV1, CicFeaturesV1
    except Exception as exc:
        raise ImportError(f"protobuf modules unavailable: {exc}") from exc


def _flow_from_proto(flow_msg) -> Dict[str, Any]:
    identity = {}
    if getattr(flow_msg, "identity", None):
        identity = {
            "namespace": flow_msg.identity.namespace,
            "pod": flow_msg.identity.pod,
            "node": flow_msg.identity.node,
        }
    return {
        "schema": flow_msg.schema or "flow.v1",
        "ts": int(flow_msg.ts or 0),
        "tenant_id": flow_msg.tenant_id or "",
        "flow_id": flow_msg.flow_id or "",
        "src_ip": flow_msg.src_ip or "",
        "dst_ip": flow_msg.dst_ip or "",
        "src_port": int(flow_msg.src_port or 0),
        "dst_port": int(flow_msg.dst_port or 0),
        "proto": int(flow_msg.proto or 0),
        "bytes_fwd": int(flow_msg.bytes_fwd or 0),
        "bytes_bwd": int(flow_msg.bytes_bwd or 0),
        "pkts_fwd": int(flow_msg.pkts_fwd or 0),
        "pkts_bwd": int(flow_msg.pkts_bwd or 0),
        "duration_ms": int(flow_msg.duration_ms or 0),
        "identity": identity,
        "verdict": flow_msg.verdict or "",
        "metrics_known": bool(flow_msg.metrics_known),
        "source_type": int(flow_msg.source_type),
        "source_id": flow_msg.source_id or "",
        "source_event_ts_ms": int(getattr(flow_msg, "source_event_ts_ms", 0) or 0),
        "telemetry_ingest_ts_ms": int(getattr(flow_msg, "telemetry_ingest_ts_ms", 0) or 0),
        "feature_mask": flow_msg.feature_mask.hex() if flow_msg.feature_mask else "",
        "timing_known": bool(flow_msg.timing_known),
        "timing_derived": bool(flow_msg.timing_derived),
        "derivation_policy": flow_msg.derivation_policy or "",
        "flags_known": bool(flow_msg.flags_known),
        "flow_iat_mean": float(flow_msg.flow_iat_mean),
        "flow_iat_std": float(flow_msg.flow_iat_std),
        "flow_iat_max": float(flow_msg.flow_iat_max),
        "flow_iat_min": float(flow_msg.flow_iat_min),
        "fwd_iat_tot": float(flow_msg.fwd_iat_tot),
        "fwd_iat_mean": float(flow_msg.fwd_iat_mean),
        "fwd_iat_std": float(flow_msg.fwd_iat_std),
        "fwd_iat_max": float(flow_msg.fwd_iat_max),
        "fwd_iat_min": float(flow_msg.fwd_iat_min),
        "bwd_iat_tot": float(flow_msg.bwd_iat_tot),
        "bwd_iat_mean": float(flow_msg.bwd_iat_mean),
        "bwd_iat_std": float(flow_msg.bwd_iat_std),
        "bwd_iat_max": float(flow_msg.bwd_iat_max),
        "bwd_iat_min": float(flow_msg.bwd_iat_min),
        "active_mean": float(flow_msg.active_mean),
        "active_std": float(flow_msg.active_std),
        "active_max": float(flow_msg.active_max),
        "active_min": float(flow_msg.active_min),
        "idle_mean": float(flow_msg.idle_mean),
        "idle_std": float(flow_msg.idle_std),
        "idle_max": float(flow_msg.idle_max),
        "idle_min": float(flow_msg.idle_min),
        "fin_flag_cnt": float(flow_msg.fin_flag_cnt),
        "syn_flag_cnt": float(flow_msg.syn_flag_cnt),
        "rst_flag_cnt": float(flow_msg.rst_flag_cnt),
        "psh_flag_cnt": float(flow_msg.psh_flag_cnt),
        "ack_flag_cnt": float(flow_msg.ack_flag_cnt),
        "urg_flag_cnt": float(flow_msg.urg_flag_cnt),
        "cwe_flag_count": float(flow_msg.cwe_flag_count),
        "ece_flag_cnt": float(flow_msg.ece_flag_cnt),
    }

_SOURCE_TYPE_MAP = {
    0: "unknown",
    1: "k8s_cilium",
    2: "bare_metal",
    3: "gateway_sensor",
    4: "gcp_vpc",
    5: "aws_vpc",
    6: "azure_nsg",
    7: "pcap",
    8: "zeek",
    9: "suricata",
}


def _normalize_source_type(raw: Any) -> str:
    if raw is None:
        return ""
    if isinstance(raw, int):
        return _SOURCE_TYPE_MAP.get(raw, str(raw))
    val = str(raw).strip().lower()
    if not val:
        return ""
    if val.isdigit():
        return _SOURCE_TYPE_MAP.get(int(val), val)
    return val


def _decode_flow(payload: bytes, encoding: str) -> Dict[str, Any]:
    normalized = (encoding or "").strip().lower()
    if normalized in {"protobuf", "proto", "pb"}:
        FlowV1, _ = _load_proto_modules()
        msg = FlowV1()
        msg.ParseFromString(payload)
        return _flow_from_proto(msg)
    return json.loads(payload.decode("utf-8"))


def _should_process(flow: Dict[str, Any], role: str, edge_ids: List[str], edge_types: List[str]) -> bool:
    role_norm = (role or "central").strip().lower()
    source_id = str(flow.get("source_id") or "").strip()
    source_type = _normalize_source_type(flow.get("source_type"))
    normalized_edge_types = {_normalize_source_type(item) for item in edge_types if str(item).strip()}

    def matches() -> bool:
        if edge_ids and source_id in edge_ids:
            return True
        if normalized_edge_types and source_type in normalized_edge_types:
            return True
        return False

    if role_norm == "edge":
        if not edge_ids and not edge_types:
            return True
        return matches()
    if role_norm == "central":
        if not edge_ids and not edge_types:
            return True
        return not matches()
    return True


def _feature_to_proto(event: Dict[str, Any], feature_names: List[str]):
    _, CicFeaturesV1 = _load_proto_modules()
    msg = CicFeaturesV1()
    msg.schema = event.get("schema", "cic.v1")
    msg.ts = int(event.get("ts", 0) or 0)
    msg.tenant_id = event.get("tenant_id", "")
    msg.flow_id = event.get("flow_id", "")
    msg.src_ip = event.get("src_ip", "")
    msg.dst_ip = event.get("dst_ip", "")
    msg.src_port = int(event.get("src_port", 0) or 0)
    msg.dst_port = int(event.get("dst_port", 0) or 0)
    msg.protocol = int(event.get("protocol", 0) or 0)
    msg.source_type = str(event.get("source_type", "") or "")
    msg.source_id = str(event.get("source_id", "") or "")
    msg.source_event_ts_ms = int(event.get("source_event_ts_ms", 0) or 0)
    msg.telemetry_ingest_ts_ms = int(event.get("telemetry_ingest_ts_ms", 0) or 0)
    mask = event.get("feature_mask", "")
    if mask:
        try:
            msg.feature_mask = bytes.fromhex(mask)
        except ValueError:
            msg.feature_mask = b""
    msg.feature_coverage = float(event.get("feature_coverage", 0.0) or 0.0)

    skip_names = {"src_port", "dst_port", "protocol"}
    for name in feature_names:
        if name in skip_names:
            continue
        if name in event:
            setattr(msg, name, float(event.get(name, 0.0) or 0.0))
    return msg


def _encode_feature(event: Dict[str, Any], feature_names: List[str], encoding: str) -> bytes:
    normalized = (encoding or "").strip().lower()
    if normalized in {"protobuf", "proto", "pb"}:
        msg = _feature_to_proto(event, feature_names)
        return msg.SerializeToString()
    return json.dumps(event).encode("utf-8")


def _extract_schema_id(headers) -> Optional[int]:
    if not headers:
        return None
    for key, value in headers:
        if key == "schema_id":
            try:
                if isinstance(value, bytes):
                    value = value.decode("utf-8")
                return int(value)
            except Exception:
                return None
    return None


def _compute_features(flow: Dict[str, Any], feature_names: List[str]) -> Dict[str, Any]:
    values: Dict[str, Any] = {name: None for name in feature_names}

    # Coverage is security-significant: do not count missing fields as "present" just because
    # we can coerce them to 0.0. Only treat metrics/timing/flags as present when the payload
    # claims they are known and the specific field is actually provided.
    def _maybe_float(raw: Any) -> Optional[float]:
        if raw is None:
            return None
        if isinstance(raw, str) and raw.strip() == "":
            return None
        try:
            return float(raw)
        except Exception:
            return None

    metrics_known = bool(flow.get("metrics_known", True))

    duration_ms = 0
    duration_sec = 0.0
    bytes_fwd = 0
    bytes_bwd = 0
    pkts_fwd = 0
    pkts_bwd = 0
    total_bytes = 0
    total_pkts = 0

    values["src_port"] = int(flow.get("src_port", 0) or 0)
    values["dst_port"] = int(flow.get("dst_port", 0) or 0)
    values["protocol"] = int(flow.get("proto", 0) or 0)
    if metrics_known:
        duration_ms = int(flow.get("duration_ms", 0) or 0)
        duration_sec = duration_ms / 1000.0 if duration_ms > 0 else 0.0
        bytes_fwd = int(flow.get("bytes_fwd", 0) or 0)
        bytes_bwd = int(flow.get("bytes_bwd", 0) or 0)
        pkts_fwd = int(flow.get("pkts_fwd", 0) or 0)
        pkts_bwd = int(flow.get("pkts_bwd", 0) or 0)
        total_bytes = bytes_fwd + bytes_bwd
        total_pkts = pkts_fwd + pkts_bwd

        values["flow_duration"] = float(duration_ms)
        values["tot_fwd_pkts"] = float(pkts_fwd)
        values["tot_bwd_pkts"] = float(pkts_bwd)
        values["totlen_fwd_pkts"] = float(bytes_fwd)
        values["totlen_bwd_pkts"] = float(bytes_bwd)

        if duration_sec > 0:
            values["flow_byts_s"] = float(total_bytes) / duration_sec
            values["flow_pkts_s"] = float(total_pkts) / duration_sec
            values["fwd_pkts_s"] = float(pkts_fwd) / duration_sec
            values["bwd_pkts_s"] = float(pkts_bwd) / duration_sec

        if total_pkts > 0:
            values["pkt_size_avg"] = float(total_bytes) / float(total_pkts)

        if pkts_fwd > 0:
            values["fwd_pkt_len_mean"] = float(bytes_fwd) / float(pkts_fwd)
            values["fwd_seg_size_avg"] = float(bytes_fwd) / float(pkts_fwd)

        if pkts_bwd > 0:
            values["bwd_pkt_len_mean"] = float(bytes_bwd) / float(pkts_bwd)
            values["bwd_seg_size_avg"] = float(bytes_bwd) / float(pkts_bwd)

        if pkts_fwd > 0:
            values["down_up_ratio"] = float(pkts_bwd) / float(pkts_fwd)

    timing_known = bool(flow.get("timing_known"))
    if timing_known:
        values["flow_iat_mean"] = _maybe_float(flow.get("flow_iat_mean"))
        values["flow_iat_std"] = _maybe_float(flow.get("flow_iat_std"))
        values["flow_iat_max"] = _maybe_float(flow.get("flow_iat_max"))
        values["flow_iat_min"] = _maybe_float(flow.get("flow_iat_min"))
        values["fwd_iat_tot"] = _maybe_float(flow.get("fwd_iat_tot"))
        values["fwd_iat_mean"] = _maybe_float(flow.get("fwd_iat_mean"))
        values["fwd_iat_std"] = _maybe_float(flow.get("fwd_iat_std"))
        values["fwd_iat_max"] = _maybe_float(flow.get("fwd_iat_max"))
        values["fwd_iat_min"] = _maybe_float(flow.get("fwd_iat_min"))
        values["bwd_iat_tot"] = _maybe_float(flow.get("bwd_iat_tot"))
        values["bwd_iat_mean"] = _maybe_float(flow.get("bwd_iat_mean"))
        values["bwd_iat_std"] = _maybe_float(flow.get("bwd_iat_std"))
        values["bwd_iat_max"] = _maybe_float(flow.get("bwd_iat_max"))
        values["bwd_iat_min"] = _maybe_float(flow.get("bwd_iat_min"))
        values["active_mean"] = _maybe_float(flow.get("active_mean"))
        values["active_std"] = _maybe_float(flow.get("active_std"))
        values["active_max"] = _maybe_float(flow.get("active_max"))
        values["active_min"] = _maybe_float(flow.get("active_min"))
        values["idle_mean"] = _maybe_float(flow.get("idle_mean"))
        values["idle_std"] = _maybe_float(flow.get("idle_std"))
        values["idle_max"] = _maybe_float(flow.get("idle_max"))
        values["idle_min"] = _maybe_float(flow.get("idle_min"))

    flags_known = bool(flow.get("flags_known"))
    if flags_known:
        values["fin_flag_cnt"] = _maybe_float(flow.get("fin_flag_cnt"))
        values["syn_flag_cnt"] = _maybe_float(flow.get("syn_flag_cnt"))
        values["rst_flag_cnt"] = _maybe_float(flow.get("rst_flag_cnt"))
        values["psh_flag_cnt"] = _maybe_float(flow.get("psh_flag_cnt"))
        values["ack_flag_cnt"] = _maybe_float(flow.get("ack_flag_cnt"))
        values["urg_flag_cnt"] = _maybe_float(flow.get("urg_flag_cnt"))
        values["cwe_flag_count"] = _maybe_float(flow.get("cwe_flag_count"))
        values["ece_flag_cnt"] = _maybe_float(flow.get("ece_flag_cnt"))

    return values


def _feature_mask(values: Dict[str, Any], feature_names: List[str]) -> str:
    mask = 0
    for idx, name in enumerate(feature_names):
        if values.get(name) is not None:
            mask |= 1 << idx
    width = (len(feature_names) + 3) // 4
    return format(mask, f"0{width}x")


def _coverage_ratio(values: Dict[str, Any]) -> float:
    total = len(values)
    if total == 0:
        return 0.0
    present = sum(1 for value in values.values() if value is not None)
    return present / float(total)


def _impute(values: Dict[str, Any]) -> Dict[str, float]:
    out: Dict[str, float] = {}
    for key, value in values.items():
        if value is None:
            out[key] = 0.0
        else:
            out[key] = float(value)
    return out


def _output_modes(mode: str) -> Tuple[bool, bool]:
    normalized = (mode or "").strip().lower()
    if normalized == "both":
        return True, True
    if normalized == "postgres":
        return False, True
    return True, False


def _to_db_row(event: Dict[str, Any], feature_names: List[str]) -> Tuple[List[str], List[Any]]:
    columns = [
        "ts",
        "tenant_id",
        "src_ip",
        "dst_ip",
        "src_port",
        "dst_port",
        "protocol",
        "flow_id",
    ] + feature_names + ["feature_mask", "feature_coverage"]

    values: List[Any] = [
        int(event.get("ts", 0) or 0),
        event.get("tenant_id", ""),
        event.get("src_ip", ""),
        event.get("dst_ip", ""),
        int(event.get("src_port", 0) or 0),
        int(event.get("dst_port", 0) or 0),
        int(event.get("protocol", 0) or 0),
        event.get("flow_id", ""),
    ]
    for name in feature_names:
        values.append(event.get(name, 0.0))

    mask = event.get("feature_mask")
    if mask:
        try:
            values.append(bytes.fromhex(mask))
        except ValueError:
            values.append(None)
    else:
        values.append(None)

    values.append(float(event.get("feature_coverage", 0.0)))

    return columns, values


def _write_postgres(conn, table: str, event: Dict[str, Any], feature_names: List[str]) -> None:
    columns, values = _to_db_row(event, feature_names)
    placeholders = []
    for col in columns:
        if col == "ts":
            placeholders.append("to_timestamp(%s)")
        else:
            placeholders.append("%s")
    sql = f"INSERT INTO {table} ({', '.join(columns)}) VALUES ({', '.join(placeholders)})"
    with conn.cursor() as cur:
        cur.execute(sql, values)


def transform_flow(flow: Dict[str, Any], feature_names: List[str], mask_enabled: bool) -> Dict[str, Any]:
    values = _compute_features(flow, feature_names)
    coverage = _coverage_ratio(values)
    mask = _feature_mask(values, feature_names)
    features = _impute(values)

    event: Dict[str, Any] = {
        "schema": "cic.v1",
        "ts": int(flow.get("ts", 0) or 0),
        "tenant_id": flow.get("tenant_id", ""),
        "flow_id": flow.get("flow_id", ""),
        "src_ip": flow.get("src_ip", ""),
        "dst_ip": flow.get("dst_ip", ""),
        "src_port": int(flow.get("src_port", 0) or 0),
        "dst_port": int(flow.get("dst_port", 0) or 0),
        "protocol": int(flow.get("proto", 0) or 0),
        "source_type": _normalize_source_type(flow.get("source_type")),
        "source_id": str(flow.get("source_id", "") or ""),
        "source_event_ts_ms": int(flow.get("source_event_ts_ms", 0) or 0),
        "telemetry_ingest_ts_ms": int(flow.get("telemetry_ingest_ts_ms", 0) or 0),
    }
    if mask_enabled:
        event["feature_mask"] = mask
        event["feature_coverage"] = coverage
    event.update(features)
    return event


def run() -> None:
    from confluent_kafka import KafkaError
    from kafka_client import build_consumer, build_producer

    cfg = load_config()
    flow_schema = load_flow_schema()
    cic_schema = load_cic_schema()
    extractor = FlowFeatureExtractor()
    feature_names = extractor.get_feature_names()
    output_kafka, output_postgres = _output_modes(cfg.features.output_mode)

    if cfg.features.schema_version != cic_schema.name:
        raise ValueError(f"schema mismatch: {cfg.features.schema_version} != {cic_schema.name}")

    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    registry = RegistryClient(
        RegistryConfig(
            enabled=cfg.features.registry_enabled,
            url=cfg.features.registry_url,
            subject_flow=cfg.features.registry_subject_flow,
            subject_feature=cfg.features.registry_subject_feature,
            timeout_sec=cfg.features.registry_timeout_sec,
            allow_fallback=cfg.features.registry_allow_fallback,
            username=cfg.features.registry_username,
            password=cfg.features.registry_password,
        )
    )
    metrics = _init_metrics(cfg.features.metrics_port)
    if cfg.features.healthcheck_only:
        logger.info("Healthcheck-only mode enabled; exiting after init")
        return

    consumer = build_consumer(cfg.kafka)
    producer = build_producer(cfg.kafka)
    pg_conn = None
    if output_postgres and cfg.postgres.enabled:
        try:
            import psycopg2
            pg_conn = psycopg2.connect(cfg.postgres.dsn)
            pg_conn.autocommit = True
        except Exception as exc:
            raise ValueError(f"Postgres connection failed: {exc}")
    consumer.subscribe([cfg.kafka.input_topic])
    logger.info(f"Feature transformer started: {cfg.kafka.input_topic} -> {cfg.kafka.output_topic}")

    commit_mode = cfg.features.commit_mode if cfg.features.commit_mode in {"sync", "batch"} else "sync"
    pending_commits = 0
    last_commit_at = time.monotonic()
    last_producer_flush_at = time.monotonic()

    def _maybe_commit(*, message=None, processed: bool = False, force: bool = False) -> None:
        nonlocal pending_commits, last_commit_at
        if commit_mode == "sync":
            if processed and message is not None:
                consumer.commit(message=message, asynchronous=False)
            elif force:
                consumer.commit(asynchronous=False)
            return

        if processed:
            pending_commits += 1

        if force and pending_commits == 0:
            return

        now = time.monotonic()
        elapsed_ms = int((now - last_commit_at) * 1000)
        if force or pending_commits >= cfg.features.commit_batch_size or elapsed_ms >= cfg.features.commit_interval_ms:
            consumer.commit(asynchronous=True)
            pending_commits = 0
            last_commit_at = now

    def _maybe_flush_producer(force: bool = False) -> None:
        nonlocal last_producer_flush_at
        if cfg.features.producer_sync_flush:
            producer.flush(5.0)
            return

        producer.poll(0)
        now = time.monotonic()
        elapsed_ms = int((now - last_producer_flush_at) * 1000)
        if force or elapsed_ms >= cfg.features.producer_flush_interval_ms:
            producer.flush(0.2)
            last_producer_flush_at = now

    def _drain_batch(first_msg) -> List[Any]:
        batch = [first_msg]
        max_messages = max(1, cfg.features.consumer_drain_max_messages)
        max_ms = max(0, cfg.features.consumer_drain_max_ms)
        if max_messages <= 1 or max_ms <= 0:
            return batch

        deadline = time.monotonic() + (float(max_ms) / 1000.0)
        while len(batch) < max_messages:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            msg = consumer.poll(min(remaining, 0.05))
            if msg is None:
                break
            batch.append(msg)
        return batch

    try:
        processed = 0
        while True:
            msg = consumer.poll(cfg.features.consumer_poll_timeout_sec)
            if msg is None:
                continue
            for msg in _drain_batch(msg):
                if msg.error():
                    if msg.error().code() == KafkaError._PARTITION_EOF:
                        continue
                    logger.error(f"Kafka error: {msg.error()}")
                    if metrics:
                        metrics["events_invalid"].inc()
                    continue

                payload = msg.value() or b""
                try:
                    if registry.enabled() and cfg.features.input_encoding in {"protobuf", "proto", "pb"}:
                        schema_id = _extract_schema_id(msg.headers())
                        if schema_id is None:
                            if cfg.features.registry_allow_fallback:
                                schema_id = None
                            else:
                                raise ValueError("missing schema_id header")
                        if schema_id is not None:
                            latest_id = registry.subject_latest_id(cfg.features.registry_subject_flow)
                            if schema_id != latest_id:
                                raise ValueError("schema_id mismatch")
                    flow = _decode_flow(payload, cfg.features.input_encoding)
                    if not _should_process(flow, cfg.features.role, cfg.features.edge_source_ids, cfg.features.edge_source_types):
                        if metrics:
                            metrics["events_skipped"].inc()
                        _maybe_commit(message=msg, processed=True)
                        processed += 1
                        if cfg.features.max_messages > 0 and processed >= cfg.features.max_messages:
                            _maybe_commit(force=True)
                            _maybe_flush_producer(force=True)
                            return
                        continue
                    if flow.get("schema") != flow_schema.name:
                        raise ValueError("invalid flow schema")
                    _validate_required(flow, flow_schema.required)
                    event = transform_flow(flow, feature_names, cfg.features.feature_mask_enabled)
                    _validate_required(event, cic_schema.required)
                    if not event.get("flow_id"):
                        raise ValueError("missing flow_id")
                    if cfg.features.feature_mask_enabled:
                        coverage = float(event.get("feature_coverage", 0.0))
                        if coverage < cfg.features.min_feature_coverage:
                            raise ValueError("feature coverage below threshold")
                    if metrics:
                        metrics["events_total"].inc()
                        metrics["last_coverage"].set(float(event.get("feature_coverage", 0.0)))
                except Exception as exc:
                    # Include stack trace: transform failures are common during early bring-up and must not be silent.
                    logger.warning("Transform error: %s", exc, exc_info=True)
                    env = _dlq_envelope("TRANSFORM_ERROR", str(exc), payload)
                    if output_kafka:
                        producer.produce(cfg.kafka.dlq_topic, json.dumps(env).encode("utf-8"))
                        _maybe_flush_producer()
                    if metrics:
                        metrics["events_invalid"].inc()
                        metrics["events_dlq"].inc()
                    _maybe_commit(message=msg, processed=True)
                    processed += 1
                    if cfg.features.max_messages > 0 and processed >= cfg.features.max_messages:
                        _maybe_commit(force=True)
                        _maybe_flush_producer(force=True)
                        return
                    continue

                if output_postgres and pg_conn:
                    try:
                        if cfg.features.dry_run:
                            logger.info(
                                "Dry-run: Postgres write skipped",
                                extra={"table": cfg.postgres.table, "flow_id": event.get("flow_id", "")},
                            )
                        else:
                            _write_postgres(pg_conn, cfg.postgres.table, event, feature_names)
                        if metrics:
                            metrics["events_postgres"].inc()
                    except Exception as exc:
                        env = _dlq_envelope("POSTGRES_WRITE", str(exc), json.dumps(event).encode("utf-8"))
                        if output_kafka:
                            producer.produce(cfg.kafka.dlq_topic, json.dumps(env).encode("utf-8"))
                            _maybe_flush_producer()
                        if metrics:
                            metrics["events_dlq"].inc()
                if output_kafka:
                    encoded = _encode_feature(event, feature_names, cfg.features.output_encoding)
                    headers = None
                    if registry.enabled() and cfg.features.output_encoding in {"protobuf", "proto", "pb"}:
                        schema_id = registry.subject_latest_id(cfg.features.registry_subject_feature)
                        headers = [("schema_id", str(schema_id).encode("utf-8"))]
                    producer.produce(cfg.kafka.output_topic, encoded, key=event.get("flow_id", ""), headers=headers)
                    _maybe_flush_producer()
                    if metrics:
                        metrics["events_published"].inc()
                _maybe_commit(message=msg, processed=True)
                processed += 1
                if cfg.features.max_messages > 0 and processed >= cfg.features.max_messages:
                    _maybe_commit(force=True)
                    _maybe_flush_producer(force=True)
                    return
    finally:
        try:
            _maybe_commit(force=True)
        except Exception:
            pass
        try:
            _maybe_flush_producer(force=True)
        except Exception:
            pass
        consumer.close()
        if pg_conn:
            try:
                pg_conn.close()
            except Exception:
                pass


if __name__ == "__main__":
    run()
