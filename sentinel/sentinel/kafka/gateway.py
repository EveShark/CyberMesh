"""Kafka gateway worker for standalone Sentinel."""

from __future__ import annotations

import hashlib
import json
import os
import time
from dataclasses import asdict, dataclass, is_dataclass
from enum import Enum
from typing import Any, Dict, Optional, Protocol

from sentinel.agents import SentinelOrchestrator
from sentinel.contracts import CanonicalEvent, Modality
from sentinel.contracts.generated.sentinel_result_pb2 import SentinelFinding, SentinelResultEvent
from sentinel.logging import get_logger
from sentinel.utils.metrics import get_metrics_collector
from sentinel.utils.error_codes import (
    ERR_INVALID_FIELDS,
    ERR_INVALID_JSON,
    ERR_INVALID_SCHEMA,
    ERR_MISSING_EVENT,
    ERR_MISSING_TENANT,
    ERR_OVERSIZE,
    format_error,
)

from .config import KafkaWorkerConfig
from .telemetry_decoder import decode_telemetry_topic_message

logger = get_logger(__name__)
_metrics = get_metrics_collector()


class KafkaRecord(Protocol):
    """Protocol for consumed Kafka records."""

    topic: str
    value: bytes
    key: Optional[bytes]


class KafkaClient(Protocol):
    """Minimal Kafka client contract used by the gateway worker."""

    def poll(self, timeout_seconds: float) -> Optional[KafkaRecord]:
        """Return next record or None."""

    def produce(
        self,
        topic: str,
        value: bytes,
        key: Optional[bytes] = None,
        headers: Optional[Dict[str, str]] = None,
    ) -> None:
        """Publish one message."""

    def commit(self, record: KafkaRecord) -> None:
        """Commit consumed offset for a record."""

    def flush(self, timeout_seconds: float) -> None:
        """Flush producer and surface delivery failures."""

    def close(self) -> None:
        """Release client resources."""


@dataclass
class _ParsedEnvelope:
    event: CanonicalEvent
    envelope: Dict[str, Any]


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
    return obj


def _safe_json_dumps(payload: Dict[str, Any]) -> bytes:
    return json.dumps(_normalize(payload), sort_keys=True, separators=(",", ":")).encode("utf-8")


def _safe_float(value: Any, default: float = 0.0) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def _stage_markers_enabled() -> bool:
    return os.getenv("SENTINEL_STAGE_MARKERS_ENABLED", "false").strip().lower() in ("1", "true", "yes", "on")


def _emit_stage_marker(stage: str, event: CanonicalEvent) -> None:
    if not _stage_markers_enabled():
        return
    trace_id = ""
    if isinstance(event.labels, dict):
        trace_id = str(event.labels.get("trace_id") or event.labels.get("source_event_id") or "").strip()
    if not trace_id:
        trace_id = str(event.id)
    logger.info(
        "runtime stage marker",
        extra={
            "stage": stage,
            "trace_id": trace_id,
            "event_id": str(event.id),
            "tenant_id": str(event.tenant_id),
            "t_ms": int(time.time() * 1000),
            "source": "sentinel.kafka.gateway",
        },
    )


def _set_event_stage_label(event: CanonicalEvent, key: str, timestamp_ms: int) -> None:
    if timestamp_ms <= 0:
        return
    labels = dict(event.labels or {})
    labels[str(key)] = str(int(timestamp_ms))
    event.labels = labels


def _parse_modality(value: Any) -> Modality:
    if not isinstance(value, str):
        raise ValueError(format_error(ERR_INVALID_FIELDS, "payload.modality must be a string"))
    try:
        return Modality(value)
    except ValueError as exc:
        raise ValueError(format_error(ERR_INVALID_FIELDS, f"unsupported modality: {value}")) from exc


def _coerce_timestamp(value: Any) -> float:
    if value is None:
        return time.time()
    try:
        return float(value)
    except Exception as exc:  # pylint: disable=broad-except
        raise ValueError(format_error(ERR_INVALID_FIELDS, "timestamp must be numeric")) from exc


def _build_canonical_event(payload: Dict[str, Any], envelope: Dict[str, Any]) -> CanonicalEvent:
    event_id = payload.get("id") or envelope.get("event_id")
    if not event_id:
        raise ValueError(format_error(ERR_MISSING_EVENT, "missing event_id"))

    tenant_id = payload.get("tenant_id") or envelope.get("tenant_id")
    if not tenant_id:
        raise ValueError(format_error(ERR_MISSING_TENANT, "missing tenant_id"))

    source = payload.get("source") or envelope.get("source") or "kafka_ingest"
    features_version = payload.get("features_version")
    if not isinstance(features_version, str) or not features_version.strip():
        raise ValueError(format_error(ERR_INVALID_FIELDS, "missing payload.features_version"))

    features = payload.get("features")
    if not isinstance(features, dict):
        raise ValueError(format_error(ERR_INVALID_FIELDS, "payload.features must be an object"))

    raw_context = payload.get("raw_context") or {}
    if not isinstance(raw_context, dict):
        raise ValueError(format_error(ERR_INVALID_FIELDS, "payload.raw_context must be an object"))

    labels = payload.get("labels") or {}
    if not isinstance(labels, dict):
        raise ValueError(format_error(ERR_INVALID_FIELDS, "payload.labels must be an object"))

    modality = _parse_modality(payload.get("modality"))
    ts = _coerce_timestamp(payload.get("timestamp") or envelope.get("timestamp"))
    normalized_labels = {str(k): str(v) for k, v in labels.items()}
    normalized_labels.setdefault("trace_id", str(event_id))
    normalized_labels.setdefault("source_event_id", str(event_id))
    normalized_labels.setdefault("source_event_ts_ms", str(int(ts * 1000)))
    return CanonicalEvent(
        id=str(event_id),
        timestamp=ts,
        source=str(source),
        tenant_id=str(tenant_id),
        modality=modality,
        features_version=features_version,
        features=features,
        raw_context=raw_context,
        labels=normalized_labels,
    )


def parse_gateway_message(value: bytes, cfg: KafkaWorkerConfig) -> _ParsedEnvelope:
    """Parse and validate a Kafka message into a CanonicalEvent."""

    if len(value) > cfg.max_message_size:
        raise ValueError(format_error(ERR_OVERSIZE, f"message exceeds {cfg.max_message_size} bytes"))

    try:
        envelope = json.loads(value.decode("utf-8"))
    except Exception as exc:  # pylint: disable=broad-except
        raise ValueError(format_error(ERR_INVALID_JSON, f"invalid json: {exc}")) from exc

    if not isinstance(envelope, dict):
        raise ValueError(format_error(ERR_INVALID_SCHEMA, "top-level message must be an object"))

    payload = envelope.get("payload")
    if not isinstance(payload, dict):
        raise ValueError(format_error(ERR_INVALID_FIELDS, "missing payload object"))

    event = _build_canonical_event(payload, envelope)

    now = time.time()
    if abs(now - event.timestamp) > float(cfg.max_timestamp_skew_seconds):
        raise ValueError(
            format_error(
                ERR_INVALID_FIELDS,
                f"timestamp skew exceeds {cfg.max_timestamp_skew_seconds}s",
            )
        )

    return _ParsedEnvelope(event=event, envelope=envelope)


class KafkaGatewayWorker:
    """Consume canonical events from Kafka and publish Sentinel analysis results."""

    def __init__(
        self,
        config: KafkaWorkerConfig,
        kafka_client: KafkaClient,
        orchestrator: SentinelOrchestrator,
    ):
        self.config = config
        self.kafka = kafka_client
        self.orchestrator = orchestrator
        self.stats: Dict[str, int] = {
            "received": 0,
            "processed": 0,
            "published": 0,
            "dlq": 0,
            "commit_errors": 0,
            "flushes": 0,
            "flush_errors": 0,
            "pending_commits_max": 0,
            "backpressure_pauses": 0,
            "backpressure_sleep_ms_total": 0,
            "analyze_errors": 0,
            "validation_errors": 0,
        }
        self._pending_commits: list[KafkaRecord] = []
        self._last_flush_at = time.monotonic()

    def run(self, *, max_messages: int = 0, stop_on_idle: bool = False) -> Dict[str, int]:
        """Process records until max_messages is reached or no work is available."""

        processed = 0
        while True:
            progressed = self.run_once()
            if progressed:
                processed += 1
                if max_messages > 0 and processed >= max_messages:
                    break
            else:
                self._flush_pending_if_due(force=stop_on_idle)
                if stop_on_idle:
                    break
        self._flush_pending_if_due(force=True)
        return dict(self.stats)

    def run_once(self) -> bool:
        """Process a single record if available."""

        if len(self._pending_commits) >= self.config.producer_max_pending_commits:
            # Force a flush before consuming more to keep bounded memory and
            # preserve commit-after-produce ordering.
            self._flush_pending_if_due(force=True)

        record = self.kafka.poll(self.config.poll_timeout_seconds)
        if record is None:
            return False

        self.stats["received"] += 1
        topic_schema = self.config.topic_schema_map.get(record.topic, "canonical_event")
        if topic_schema == "canonical_event":
            validate_start = time.perf_counter()
            try:
                parsed = parse_gateway_message(record.value, self.config)
                event = parsed.event
                _metrics.record_operation("gateway_validate", time.perf_counter() - validate_start, "ok")
                _set_event_stage_label(event, "t_sentinel_consume_ms", int(time.time() * 1000))
                _emit_stage_marker("t_sentinel_consume", event)
            except Exception as exc:  # pylint: disable=broad-except
                _metrics.record_operation("gateway_validate", time.perf_counter() - validate_start, "error")
                self.stats["validation_errors"] += 1
                self._publish_dlq(record=record, error=str(exc), stage="validate")
                self._queue_commit(record)
                self._flush_pending_if_due(force=False)
                return True
        else:
            decode_start = time.perf_counter()
            try:
                event = decode_telemetry_topic_message(record.topic, record.value, self.config)
                _metrics.record_operation("gateway_decode", time.perf_counter() - decode_start, "ok")
                _set_event_stage_label(event, "t_sentinel_consume_ms", int(time.time() * 1000))
                _emit_stage_marker("t_sentinel_consume", event)
            except Exception as exc:  # pylint: disable=broad-except
                _metrics.record_operation("gateway_decode", time.perf_counter() - decode_start, "error")
                self.stats["validation_errors"] += 1
                self._publish_dlq(record=record, error=str(exc), stage="decode")
                self._queue_commit(record)
                self._flush_pending_if_due(force=False)
                return True

        analyze_start = time.perf_counter()
        try:
            result = self.orchestrator.analyze_event(event)
            _metrics.record_operation("gateway_analyze", time.perf_counter() - analyze_start, "ok")
            _set_event_stage_label(event, "t_sentinel_analysis_done_ms", int(time.time() * 1000))
            _emit_stage_marker("t_sentinel_analysis_done", event)
        except Exception as exc:  # pylint: disable=broad-except
            _metrics.record_operation("gateway_analyze", time.perf_counter() - analyze_start, "error")
            self.stats["analyze_errors"] += 1
            self._publish_dlq(record=record, error=f"analysis failed: {exc}", stage="analyze")
            self._queue_commit(record)
            self._flush_pending_if_due(force=False)
            return True

        publish_start = time.perf_counter()
        try:
            payload, schema_version = self._serialize_result(event, result)
            self.kafka.produce(
                topic=self.config.output_topic,
                key=event.id.encode("utf-8"),
                value=payload,
                headers={
                    "schema_version": schema_version,
                    "tenant_id": event.tenant_id,
                    "encoding": self.config.output_encoding,
                },
            )
            self.stats["published"] += 1
            _metrics.record_operation("gateway_publish", time.perf_counter() - publish_start, "ok")
            _emit_stage_marker("t_sentinel_publish_ack", event)
        except Exception as exc:  # pylint: disable=broad-except
            _metrics.record_operation("gateway_publish", time.perf_counter() - publish_start, "error")
            self._publish_dlq(record=record, error=f"publish failed: {exc}", stage="publish")
            self._queue_commit(record)
            self._flush_pending_if_due(force=False)
            return True

        self._queue_commit(record)
        self._flush_pending_if_due(force=False)
        self.stats["processed"] += 1
        return True

    def close(self) -> None:
        try:
            self._flush_pending_if_due(force=True)
        except Exception as exc:  # pylint: disable=broad-except
            logger.error("final kafka flush failed during close: %s", exc)
        finally:
            self.kafka.close()

    def _safe_commit(self, record: KafkaRecord) -> None:
        start = time.perf_counter()
        try:
            self.kafka.commit(record)
            _metrics.record_operation("gateway_commit", time.perf_counter() - start, "ok")
        except Exception as exc:  # pylint: disable=broad-except
            _metrics.record_operation("gateway_commit", time.perf_counter() - start, "error")
            self.stats["commit_errors"] += 1
            logger.error("kafka commit failed: %s", exc)

    def _queue_commit(self, record: KafkaRecord) -> None:
        self._pending_commits.append(record)
        pending = len(self._pending_commits)
        if pending > self.stats["pending_commits_max"]:
            self.stats["pending_commits_max"] = pending
        # Safety guard: avoid unbounded memory growth if flush path is degraded.
        if pending > (self.config.producer_max_pending_commits * 4):
            raise RuntimeError(
                "pending commit queue exceeded safety limit; stopping consumer to preserve at-least-once semantics"
            )

    def _flush_pending_if_due(self, *, force: bool) -> bool:
        if not self._pending_commits:
            return True

        now = time.monotonic()
        elapsed_ms = int((now - self._last_flush_at) * 1000)
        if (
            not force
            and len(self._pending_commits) < self.config.producer_max_pending_commits
            and elapsed_ms < self.config.producer_flush_interval_ms
        ):
            return True

        flush_start = time.perf_counter()
        try:
            self.kafka.flush(self.config.producer_flush_timeout_seconds)
        except Exception as exc:  # pylint: disable=broad-except
            _metrics.record_operation("gateway_flush", time.perf_counter() - flush_start, "error")
            self.stats["flush_errors"] += 1
            logger.error("kafka producer flush failed: %s", exc)
            self._apply_backpressure_pause()
            # Fail fast: keep offsets uncommitted and let process restart.
            raise RuntimeError("kafka producer flush failed") from exc
        _metrics.record_operation("gateway_flush", time.perf_counter() - flush_start, "ok")

        self._last_flush_at = now
        self.stats["flushes"] += 1
        pending = self._pending_commits
        self._pending_commits = []
        for record in pending:
            self._safe_commit(record)
        return True

    def _apply_backpressure_pause(self) -> None:
        sleep_ms = max(1, int(self.config.gateway_backpressure_sleep_ms))
        self.stats["backpressure_pauses"] += 1
        self.stats["backpressure_sleep_ms_total"] += sleep_ms
        time.sleep(float(sleep_ms) / 1000.0)

    def _publish_dlq(self, *, record: KafkaRecord, error: str, stage: str) -> None:
        start = time.perf_counter()
        payload = {
            "event_id": None,
            "tenant_id": None,
            "timestamp": time.time(),
            "source": "sentinel.kafka.gateway",
            "schema_version": "sentinel.dlq.v1",
            "payload_type": "sentinel_dlq",
            "payload": {
                "stage": stage,
                "error": str(error)[:1024],
                "input_topic": getattr(record, "topic", ""),
                "message_size": len(getattr(record, "value", b"") or b""),
                "message_sha256": hashlib.sha256(getattr(record, "value", b"") or b"").hexdigest(),
                "message_preview": (getattr(record, "value", b"") or b"")[:512].decode("utf-8", errors="ignore"),
            },
        }
        try:
            self.kafka.produce(
                topic=self.config.dlq_topic,
                value=_safe_json_dumps(payload),
                headers={"schema_version": "sentinel.dlq.v1"},
            )
            _metrics.record_operation("gateway_dlq_publish", time.perf_counter() - start, "ok")
            self.stats["dlq"] += 1
        except Exception as exc:  # pylint: disable=broad-except
            _metrics.record_operation("gateway_dlq_publish", time.perf_counter() - start, "error")
            logger.error("failed to publish DLQ message: %s", exc)

    @staticmethod
    def _result_envelope(event: CanonicalEvent, result: Dict[str, Any]) -> Dict[str, Any]:
        labels = dict(event.labels or {})
        # Sentinel-owned lineage/stage keys are authoritative on egress.
        labels["trace_id"] = str(event.id)
        labels["source_event_id"] = str(event.id)
        labels["source_event_ts_ms"] = str(labels.get("source_event_ts_ms") or int(float(event.timestamp) * 1000.0))
        labels["sentinel_source"] = "sentinel.kafka.gateway"
        labels["t_sentinel_emit_ms"] = str(int(time.time() * 1000))
        return {
            "event_id": event.id,
            "tenant_id": event.tenant_id,
            "timestamp": time.time(),
            "source": "sentinel.kafka.gateway",
            "schema_version": "sentinel.result.v1",
            "payload_type": "sentinel_result",
            "labels": labels,
            "payload": {
                "input": {
                    "id": event.id,
                    "tenant_id": event.tenant_id,
                    "source": event.source,
                    "modality": event.modality.value,
                    "features_version": event.features_version,
                    "timestamp": event.timestamp,
                },
                "analysis": _normalize(result),
            },
        }

    def _serialize_result(self, event: CanonicalEvent, result: Dict[str, Any]) -> tuple[bytes, str]:
        if self.config.output_encoding == "protobuf":
            return self._result_protobuf(event, result), "sentinel.result.v1"
        return _safe_json_dumps(self._result_envelope(event, result)), "sentinel.result.v1"

    @staticmethod
    def _result_protobuf(event: CanonicalEvent, result: Dict[str, Any]) -> bytes:
        metadata = result.get("metadata") if isinstance(result, dict) else {}
        if not isinstance(metadata, dict):
            metadata = {}

        msg = SentinelResultEvent(
            schema_version="sentinel.result.v1",
            event_id=event.id,
            tenant_id=event.tenant_id,
            timestamp=time.time(),
            source="sentinel.kafka.gateway",
            payload_type="sentinel_result",
            threat_level=str(result.get("threat_level") or ""),
            final_score=_safe_float(result.get("final_score"), 0.0),
            confidence=_safe_float(result.get("confidence"), 0.0),
            errors=[str(err) for err in (result.get("errors") or []) if err is not None],
            degraded=bool(metadata.get("degraded", False)),
            verdict=str(result.get("verdict") or ""),
            model_version=str(result.get("model_version") or "sentinel-kafka.v1"),
        )

        msg.input.id = event.id
        msg.input.tenant_id = event.tenant_id
        msg.input.source = event.source
        msg.input.modality = event.modality.value
        msg.input.features_version = event.features_version
        msg.input.timestamp = float(event.timestamp)
        result_labels = dict(event.labels or {})
        # Sentinel-owned lineage/stage keys are authoritative on egress.
        result_labels["trace_id"] = str(event.id)
        result_labels["source_event_id"] = str(event.id)
        result_labels["source_event_ts_ms"] = str(result_labels.get("source_event_ts_ms") or int(float(event.timestamp) * 1000.0))
        result_labels["sentinel_source"] = "sentinel.kafka.gateway"
        result_labels["t_sentinel_emit_ms"] = str(int(time.time() * 1000))
        if result_labels:
            msg.labels.update({str(k): str(v) for k, v in result_labels.items()})

        findings = result.get("findings") or []
        if isinstance(findings, list):
            for item in findings[:50]:
                if isinstance(item, dict):
                    msg.findings.append(
                        SentinelFinding(
                            id=str(item.get("id") or item.get("rule_id") or ""),
                            agent=str(item.get("agent") or item.get("source") or ""),
                            category=str(item.get("category") or ""),
                            description=str(item.get("description") or ""),
                            severity=str(item.get("severity") or ""),
                            score=_safe_float(item.get("score"), 0.0),
                        )
                    )
                elif item is not None:
                    msg.findings.append(SentinelFinding(description=str(item)))

        # Preserve source/destination IP context for downstream policy routing.
        # Sentinel adapter uses finding descriptions as a fallback target extractor.
        if isinstance(event.features, dict):
            src_ip = (
                event.features.get("src_ip")
                or event.features.get("source_ip")
                or event.features.get("sip")
            )
            dst_ip = (
                event.features.get("dst_ip")
                or event.features.get("destination_ip")
                or event.features.get("dip")
            )
            if isinstance(src_ip, str) and src_ip:
                desc = f"network context src_ip={src_ip}"
                if isinstance(dst_ip, str) and dst_ip:
                    desc += f" dst_ip={dst_ip}"
                msg.findings.append(
                    SentinelFinding(
                        id="context-network-ip",
                        agent="gateway",
                        category="network_context",
                        description=desc,
                        severity="info",
                        score=0.0,
                    )
                )

        return msg.SerializeToString()
