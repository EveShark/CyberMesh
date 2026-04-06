"""
Sentinel Kafka adapter for AI-service integration.

Consumes Sentinel verdict envelopes and forwards high-risk detections into
the existing AI anomaly publish path.
"""

from __future__ import annotations

import hashlib
import ipaddress
import json
import os
import queue
import re
import threading
import time
import uuid
from dataclasses import dataclass
from typing import Any, Dict, Optional, Callable

from confluent_kafka import Consumer, KafkaException

from ..config.settings import Settings
from ..contracts.generated.sentinel_result_pb2 import SentinelResultEvent
from ..feedback.tracker import AnomalyLifecycleTracker
from .policy_emitter import PolicyContext, build_policy_candidate
from .policy_aggregation import PolicyAggregationManager
from .fast_mitigation import decide_fast_mitigation
from .app_events_policy_pack import AppEventsPolicyPack, AppEventsPolicyDecision
from .publisher import MessagePublisher
from ..utils.errors import StorageError, ValidationError
from ..utils.metrics import get_metrics_collector
from ..observability import start_span


def _env_bool(name: str, default: bool = False) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() in ("1", "true", "yes", "on")


def _env_int(name: str, default: int) -> int:
    value = os.getenv(name)
    if value is None:
        return default
    try:
        return int(value)
    except ValueError:
        return default


def _stage_markers_enabled() -> bool:
    return os.getenv("POLICY_STAGE_MARKERS_ENABLED", "false").strip().lower() in ("1", "true", "yes", "on")


def _normalize_event_ts_ms(value: Any) -> int:
    if value is None:
        return 0
    try:
        numeric = float(value)
    except (TypeError, ValueError):
        return 0
    if numeric <= 0:
        return 0
    if 946684800 <= numeric <= 4102444800:
        return int(numeric * 1000)
    if 946684800000 <= numeric <= 4102444800000:
        return int(numeric)
    if 946684800000000 <= numeric <= 4102444800000000:
        return int(numeric / 1000)
    if 946684800000000000 <= numeric <= 4102444800000000000:
        return int(numeric / 1_000_000)
    return 0


def _copy_operational_context(dst: Dict[str, Any], src: Dict[str, Any], *keys: str) -> None:
    for key in keys:
        value = src.get(key)
        if isinstance(value, str):
            value = value.strip()
        if value not in (None, "", [], {}):
            dst[key] = value


def _metrics_modality_token(raw: Any) -> str:
    token = str(raw or "").strip().lower()
    token = token.replace("-", "_").replace(".", "_").replace(" ", "_")
    if token in {"network_flow", "scan_findings", "action_event", "mcp_runtime", "exfil_event", "resilience_event"}:
        return token
    return "unknown"


@dataclass(frozen=True)
class SentinelAdapterConfig:
    enabled: bool
    mode: str
    input_topic: str
    consumer_group: str
    max_message_bytes: int
    dedupe_ttl_seconds: int
    require_schema_version: str
    input_encoding: str
    policy_enabled: bool
    commit_every: int
    commit_interval_ms: int
    commit_async: bool
    loop_error_backoff_ms: int
    policy_publish_async: bool
    policy_publish_workers: int
    policy_publish_queue_size: int

    @classmethod
    def from_env(cls, settings: Settings) -> "SentinelAdapterConfig":
        group_default = f"{settings.kafka_consumer.group_id}-sentinel"
        mode = os.getenv("SENTINEL_ADAPTER_MODE", "shadow").strip().lower()
        if mode not in ("shadow", "prod"):
            mode = "shadow"
        encoding = os.getenv("SENTINEL_INPUT_ENCODING", "protobuf").strip().lower()
        if encoding not in ("protobuf", "json", "auto"):
            encoding = "protobuf"
        return cls(
            enabled=_env_bool("SENTINEL_ADAPTER_ENABLED", False),
            mode=mode,
            input_topic=os.getenv("SENTINEL_INPUT_TOPIC", "sentinel.verdicts.v1").strip(),
            consumer_group=os.getenv("SENTINEL_CONSUMER_GROUP", group_default).strip(),
            max_message_bytes=max(1024, _env_int("SENTINEL_MAX_MESSAGE_BYTES", 131072)),
            dedupe_ttl_seconds=max(30, _env_int("SENTINEL_DEDUPE_TTL_SECONDS", 300)),
            require_schema_version=os.getenv("SENTINEL_REQUIRED_SCHEMA", "sentinel.result.v1").strip(),
            input_encoding=encoding,
            policy_enabled=_env_bool("SENTINEL_POLICY_ENABLED", False),
            commit_every=max(1, _env_int("SENTINEL_COMMIT_EVERY", 32)),
            commit_interval_ms=max(1, _env_int("SENTINEL_COMMIT_INTERVAL_MS", 200)),
            commit_async=_env_bool("SENTINEL_COMMIT_ASYNC", False),
            loop_error_backoff_ms=max(10, _env_int("SENTINEL_LOOP_ERROR_BACKOFF_MS", 250)),
            policy_publish_async=_env_bool("SENTINEL_POLICY_PUBLISH_ASYNC", True),
            policy_publish_workers=max(1, _env_int("SENTINEL_POLICY_PUBLISH_WORKERS", 2)),
            policy_publish_queue_size=max(1, _env_int("SENTINEL_POLICY_PUBLISH_QUEUE_SIZE", 512)),
        )


class SentinelKafkaAdapter:
    """Bridge Sentinel verdict stream into ai.anomalies.v1 publisher path."""

    def __init__(
        self,
        settings: Settings,
        publisher: MessagePublisher,
        logger,
        tracker: Optional[AnomalyLifecycleTracker] = None,
        event_recorder: Optional[Callable[..., None]] = None,
        policy_aggregator: Optional[PolicyAggregationManager] = None,
    ):
        self._settings = settings
        self._publisher = publisher
        self._logger = logger
        self._tracker = tracker
        self._event_recorder = event_recorder
        self._cfg = SentinelAdapterConfig.from_env(settings)
        self._policy_cfg = settings.policy_publishing
        self._policy_aggregator = policy_aggregator
        if self._policy_aggregator is None and self._policy_cfg is not None:
            self._policy_aggregator = PolicyAggregationManager(self._policy_cfg)
        self._app_events_pack = AppEventsPolicyPack.from_env()
        self._app_events_policy_bypass_clean_enabled = _env_bool(
            "APP_EVENTS_POLICY_BYPASS_CLEAN_ENABLED",
            False,
        )
        self._service_metrics = get_metrics_collector()
        self._validator_ip_map = dict(getattr(settings, "sentinel_validator_ip_map", {}) or {})

        self._running = False
        self._thread: Optional[threading.Thread] = None
        self._policy_workers: list[threading.Thread] = []
        self._policy_publish_queue: Optional[queue.Queue] = None
        self._seen: Dict[str, float] = {}
        self._metrics = {
            "received": 0,
            "forwarded": 0,
            "skipped_clean": 0,
            "deduped": 0,
            "invalid": 0,
            "publish_failed": 0,
            "policy_candidates": 0,
            "policy_published": 0,
            "policy_failed": 0,
            "policy_skipped": 0,
            "policy_skipped_missing_target": 0,
            "policy_skipped_threshold": 0,
            "policy_enqueued": 0,
            "policy_queue_overflow_sync": 0,
            "app_events_detected": 0,
            "app_events_promoted": 0,
            "app_events_suppressed": 0,
            "app_events_no_match": 0,
            "clean_bypassed_for_app_events": 0,
            "commit_batches": 0,
            "commit_errors": 0,
        }
        self._metrics_lock = threading.Lock()
        self._pending_commit_msg = None
        self._pending_commit_count = 0
        self._last_commit_at = time.monotonic()

        self._consumer = Consumer(self._build_consumer_config())
        self._consumer.subscribe([self._cfg.input_topic])

    @property
    def enabled(self) -> bool:
        return self._cfg.enabled

    def _metric_inc(self, key: str, value: int = 1) -> None:
        with self._metrics_lock:
            self._metrics[key] = int(self._metrics.get(key, 0)) + int(value)

    def _metrics_snapshot(self) -> Dict[str, int]:
        with self._metrics_lock:
            return dict(self._metrics)

    def get_metrics(self) -> Dict[str, Any]:
        return {
            "enabled": self._cfg.enabled,
            "mode": self._cfg.mode,
            "topic": self._cfg.input_topic,
            "policy_enabled": self._cfg.policy_enabled,
            "app_events_policy_pack_enabled": self._app_events_pack.enabled,
            "app_events_policy_bypass_clean_enabled": self._app_events_policy_bypass_clean_enabled,
            "metrics": self._metrics_snapshot(),
        }

    def start(self) -> None:
        if not self._cfg.enabled or self._running:
            return
        self._running = True
        if self._cfg.policy_enabled and self._cfg.policy_publish_async and self._policy_publish_queue is None:
            self._policy_publish_queue = queue.Queue(maxsize=self._cfg.policy_publish_queue_size)
        if self._policy_publish_queue is not None and not self._policy_workers:
            for idx in range(self._cfg.policy_publish_workers):
                worker = threading.Thread(
                    target=self._policy_publish_loop,
                    name=f"sentinel-policy-publish-{idx}",
                    daemon=True,
                )
                worker.start()
                self._policy_workers.append(worker)
        self._thread = threading.Thread(target=self._loop, name="sentinel-adapter", daemon=True)
        self._thread.start()
        self._logger.info(
            "Sentinel adapter started",
            extra={
                "topic": self._cfg.input_topic,
                "mode": self._cfg.mode,
                "group": self._cfg.consumer_group,
            },
        )

    def stop(self, timeout: float = 10.0) -> None:
        self._running = False
        if self._thread and self._thread.is_alive():
            self._thread.join(timeout=timeout)
        deadline = time.time() + max(1.0, timeout)
        for worker in self._policy_workers:
            remain = max(0.1, deadline - time.time())
            if worker.is_alive():
                worker.join(timeout=remain)
        self._policy_workers = []
        self._policy_publish_queue = None
        self._flush_commit(force=True)
        try:
            self._consumer.close()
        except Exception:
            pass
        self._logger.info("Sentinel adapter stopped", extra={"metrics": self._metrics_snapshot()})

    def _build_consumer_config(self) -> Dict[str, Any]:
        consumer_cfg = self._settings.kafka_consumer
        security_cfg = consumer_cfg.security
        config: Dict[str, Any] = {
            "bootstrap.servers": consumer_cfg.bootstrap_servers,
            "group.id": self._cfg.consumer_group,
            "auto.offset.reset": consumer_cfg.auto_offset_reset,
            "enable.auto.commit": False,
            "max.poll.interval.ms": consumer_cfg.max_poll_interval_ms,
            "session.timeout.ms": consumer_cfg.session_timeout_ms,
            "heartbeat.interval.ms": consumer_cfg.heartbeat_interval_ms,
            "fetch.min.bytes": consumer_cfg.fetch_min_bytes,
            "fetch.wait.max.ms": consumer_cfg.fetch_max_wait_ms,
            "enable.partition.eof": False,
            "isolation.level": "read_committed",
        }
        if security_cfg.sasl_mechanism == "NONE":
            config["security.protocol"] = "SSL" if security_cfg.tls_enabled else "PLAINTEXT"
        else:
            config["security.protocol"] = "SASL_SSL" if security_cfg.tls_enabled else "SASL_PLAINTEXT"
            config["sasl.mechanism"] = security_cfg.sasl_mechanism
            config["sasl.username"] = security_cfg.sasl_username
            config["sasl.password"] = security_cfg.sasl_password
        if security_cfg.tls_enabled and security_cfg.ca_cert_path:
            config["ssl.ca.location"] = security_cfg.ca_cert_path
        if security_cfg.client_cert_path:
            config["ssl.certificate.location"] = security_cfg.client_cert_path
        if security_cfg.client_key_path:
            config["ssl.key.location"] = security_cfg.client_key_path
        return config

    def _loop(self) -> None:
        while self._running:
            try:
                msg = self._consumer.poll(timeout=1.0)
                if msg is None:
                    self._flush_commit(force=False)
                    continue
                if msg.error():
                    continue
                self._metric_inc("received")
                self._handle(msg.value() or b"")
                self._mark_commit(msg)
                self._flush_commit(force=False)
            except KafkaException as exc:
                self._logger.error("Sentinel adapter Kafka error", extra={"error": str(exc)})
                self._flush_commit(force=False)
                time.sleep(float(self._cfg.loop_error_backoff_ms) / 1000.0)
            except Exception as exc:  # defensive: never crash background thread
                self._logger.error("Sentinel adapter loop error", extra={"error": str(exc)}, exc_info=True)
                self._flush_commit(force=False)
                time.sleep(float(self._cfg.loop_error_backoff_ms) / 1000.0)

    def _mark_commit(self, msg) -> None:
        self._pending_commit_msg = msg
        self._pending_commit_count += 1

    def _flush_commit(self, *, force: bool) -> None:
        if self._pending_commit_count <= 0 or self._pending_commit_msg is None:
            return
        now = time.monotonic()
        elapsed_ms = int((now - self._last_commit_at) * 1000)
        if (
            not force
            and self._pending_commit_count < self._cfg.commit_every
            and elapsed_ms < self._cfg.commit_interval_ms
        ):
            return
        try:
            self._consumer.commit(message=self._pending_commit_msg, asynchronous=self._cfg.commit_async)
            self._metric_inc("commit_batches")
            self._pending_commit_count = 0
            self._pending_commit_msg = None
            self._last_commit_at = now
        except Exception as exc:  # pragma: no cover - defensive
            self._metric_inc("commit_errors")
            self._logger.error("Sentinel adapter commit failed", extra={"error": str(exc)})

    def _policy_publish_loop(self) -> None:
        while self._running or (self._policy_publish_queue is not None and not self._policy_publish_queue.empty()):
            if self._policy_publish_queue is None:
                return
            try:
                task = self._policy_publish_queue.get(timeout=0.5)
            except queue.Empty:
                continue
            try:
                self._publish_policy_task(task)
            finally:
                self._policy_publish_queue.task_done()

    def _handle(self, raw: bytes) -> None:
        handle_start = time.monotonic()
        ai_consume_ts_ms = int(time.time() * 1000)
        handle_status = "ok"
        modality_token = "unknown"
        with start_span(
            "ai.sentinel.analyze",
            tracer_name="ai-service/sentinel",
            attributes={
                "messaging.system": "kafka",
                "messaging.destination": self._cfg.input_topic,
            },
        ) as span:
            if len(raw) > self._cfg.max_message_bytes:
                handle_status = "error"
                self._metric_inc("invalid")
                self._logger.warning("Sentinel envelope exceeds max size", extra={"size": len(raw)})
                span.set_attribute("error.class", "message_too_large")
                return
            try:
                envelope = self._decode_envelope(raw)
                if envelope is None:
                    handle_status = "error"
                    self._metric_inc("invalid")
                    self._logger.warning("Sentinel envelope decode failed", extra={"encoding": self._cfg.input_encoding})
                    span.set_attribute("error.class", "decode_failed")
                    return
                if not isinstance(envelope, dict):
                    handle_status = "error"
                    self._metric_inc("invalid")
                    span.set_attribute("error.class", "invalid_envelope")
                    return
    
                schema_version = str(envelope.get("schema_version") or "")
                if self._cfg.require_schema_version and schema_version != self._cfg.require_schema_version:
                    handle_status = "error"
                    self._metric_inc("invalid")
                    self._logger.warning(
                        "Sentinel schema mismatch",
                        extra={"got": schema_version, "want": self._cfg.require_schema_version},
                    )
                    return
    
                event_id = str(envelope.get("event_id") or "")
                if not event_id:
                    handle_status = "error"
                    self._metric_inc("invalid")
                    span.set_attribute("error.class", "missing_event_id")
                    return
                labels = envelope.get("labels") if isinstance(envelope, dict) else {}
                if not isinstance(labels, dict):
                    labels = {}
                if _stage_markers_enabled():
                    trace_id = str(labels.get("trace_id") or labels.get("source_event_id") or event_id).strip()
                    self._logger.info(
                        "policy stage marker",
                        extra={
                            "stage": "t_ai_sentinel_consume",
                            "trace_id": trace_id,
                            "event_id": event_id,
                            "t_ms": int(time.time() * 1000),
                        },
                    )
                if self._is_duplicate(event_id):
                    handle_status = "duplicate"
                    self._metric_inc("deduped")
                    span.set_attribute("ai.event.status", "duplicate")
                    return
    
                payload = envelope.get("payload") or {}
                analysis = payload.get("analysis") if isinstance(payload, dict) else {}
                if not isinstance(analysis, dict):
                    handle_status = "error"
                    self._metric_inc("invalid")
                    span.set_attribute("error.class", "invalid_analysis")
                    return
                input_part = payload.get("input") if isinstance(payload, dict) else {}
                if isinstance(input_part, dict):
                    modality_token = _metrics_modality_token(input_part.get("modality"))
    
                threat_level = self._normalize_threat_level_value(analysis.get("threat_level"))
                if threat_level in ("clean", "unknown", ""):
                    bypassed = False
                    if (
                        self._cfg.policy_enabled
                        and self._app_events_pack.enabled
                        and self._app_events_policy_bypass_clean_enabled
                    ):
                        bypassed = self._maybe_publish_policy(
                            event_id=event_id,
                            anomaly_id=self._deterministic_uuid4_from_seed(event_id),
                            anomaly_type=self._map_anomaly_type(analysis, envelope=envelope),
                            severity=self._map_severity(analysis),
                            confidence=self._safe_float(analysis.get("confidence"), default=0.5),
                            envelope=envelope,
                            ai_consume_ts_ms=ai_consume_ts_ms,
                        )
                    if bypassed:
                        handle_status = "app_events_bypass"
                        self._metric_inc("clean_bypassed_for_app_events")
                        try:
                            self._service_metrics.record_service_operation(
                                "sentinel_handle_clean_bypassed_for_app_events",
                                0.0,
                                status="ok",
                            )
                        except Exception:
                            self._logger.debug("Failed to record app-events clean bypass metric", exc_info=True)
                    else:
                        handle_status = "skipped"
                        self._metric_inc("skipped_clean")
                    span.set_attribute("ai.event.status", handle_status)
                    return
    
                anomaly_type = self._map_anomaly_type(analysis, envelope=envelope)
                confidence = self._safe_float(analysis.get("confidence"), default=0.75)
                severity = self._map_severity(analysis)
                source = self._extract_source(envelope)
    
                anomaly_id = self._deterministic_uuid4_from_seed(event_id)
                payload_bytes = self._build_anomaly_payload(
                    envelope,
                    severity=severity,
                    confidence=confidence,
                    max_size=self._cfg.max_message_bytes,
                )
                now = int(time.time())
    
                if self._cfg.mode == "shadow":
                    handle_status = "shadow"
                    self._metric_inc("forwarded")
                    self._logger.info(
                        "Sentinel shadow detection observed",
                        extra={"event_id": event_id, "anomaly_type": anomaly_type, "severity": severity},
                    )
                    return
    
                try:
                    source_event_id = str(labels.get("source_event_id") or "").strip()
                    if not source_event_id:
                        source_event_id = str(
                            (((payload.get("input") or {}) if isinstance(payload, dict) else {}).get("id") or "")
                        ).strip()
                    event_metadata = {
                        "timestamp": now,
                        "source_event_id": source_event_id or event_id,
                        "sentinel_event_id": event_id,
                        "source": source,
                        "anomaly_id": anomaly_id,
                    }
                    network_context = self._extract_network_context(payload, analysis.get("findings") or [])
                    for key in ("src_ip", "source_ip", "dst_ip", "destination_ip", "source_event_ts_ms"):
                        if key in network_context:
                            event_metadata[key] = network_context[key]
                    _copy_operational_context(event_metadata, network_context, "flow_id", "source_id", "source_type", "sensor_id")
                    for key in ("scenario", "profile_mode", "trace_id", "source_event_id", "flow_id", "source_id", "source_type", "sensor_id"):
                        value = labels.get(key)
                        if isinstance(value, str) and value:
                            event_metadata[key] = value
                    if self._event_recorder:
                        validator_id = self._resolve_validator_id(
                            envelope=envelope,
                            payload=payload,
                            network_context=network_context,
                        )
                        if validator_id:
                            event_metadata["validator_id"] = validator_id
                        self._event_recorder(
                            source="sentinel",
                            validator_id=validator_id,
                            threat_type=anomaly_type,
                            severity=severity,
                            confidence=confidence,
                            final_score=self._safe_float(analysis.get("final_score"), default=confidence) or confidence,
                            should_publish=True,
                            metadata=event_metadata,
                        )
                    if self._tracker:
                        self._tracker.record_detected(
                            anomaly_id=anomaly_id,
                            anomaly_type=anomaly_type,
                            confidence=confidence,
                            severity=severity,
                            raw_score=self._safe_float(analysis.get("final_score"), default=None),
                            timestamp=float(now),
                        )
                    self._publisher.publish_anomaly(
                        anomaly_id=anomaly_id,
                        anomaly_type=anomaly_type,
                        source=source,
                        severity=severity,
                        confidence=confidence,
                        payload=payload_bytes,
                        model_version="sentinel-kafka.v1",
                    )
                    if self._tracker:
                        self._tracker.record_published(anomaly_id=anomaly_id, timestamp=float(now))
                    self._maybe_publish_policy(
                        event_id=event_id,
                        anomaly_id=anomaly_id,
                        anomaly_type=anomaly_type,
                        severity=severity,
                        confidence=confidence,
                        envelope=envelope,
                        ai_consume_ts_ms=ai_consume_ts_ms,
                    )
                    self._metric_inc("forwarded")
                    span.set_attribute("ai.event.status", "forwarded")
                    span.set_attribute("ai.anomaly.severity", int(severity))
                    span.set_attribute("ai.anomaly.confidence", float(confidence))
                except (ValidationError, StorageError) as exc:
                    handle_status = "error"
                    self._metric_inc("publish_failed")
                    self._logger.error("Sentinel adapter tracker error", extra={"error": str(exc)})
                    span.set_attribute("error.class", "validation_or_storage_error")
                except Exception as exc:
                    handle_status = "error"
                    self._metric_inc("publish_failed")
                    self._logger.error("Sentinel adapter publish failed", extra={"error": str(exc)}, exc_info=True)
                    span.set_attribute("error.class", "unexpected_error")
                finally:
                    span.set_attribute("ai.event.result", handle_status)
                    span.set_attribute("ai.event.modality", modality_token)
            except Exception as exc:
                handle_status = "error"
                self._metric_inc("publish_failed")
                self._logger.error("Sentinel adapter processing failed", extra={"error": str(exc)}, exc_info=True)
                span.set_attribute("error.class", "unexpected_error")
                span.set_attribute("ai.event.result", handle_status)
                span.set_attribute("ai.event.modality", modality_token)
        try:
            elapsed = time.monotonic() - handle_start
            self._service_metrics.record_service_operation(
                "sentinel_handle",
                elapsed,
                status=handle_status,
            )
            self._service_metrics.record_service_operation(
                f"sentinel_handle_modality_{modality_token}",
                elapsed,
                status=handle_status,
            )
        except Exception:
            self._logger.debug("Failed to record sentinel handle metric", exc_info=True)

    def _is_duplicate(self, event_id: str) -> bool:
        now = time.time()
        ttl = self._cfg.dedupe_ttl_seconds
        stale = [k for k, ts in self._seen.items() if (now - ts) > ttl]
        for key in stale:
            self._seen.pop(key, None)
        if event_id in self._seen:
            return True
        self._seen[event_id] = now
        return False

    @staticmethod
    def _map_anomaly_type(analysis: Dict[str, Any], *, envelope: Optional[Dict[str, Any]] = None) -> str:
        # Prefer explicit structured hints first; this avoids depending only on
        # free-text findings that can be sparse in blob replay scenarios.
        hint_candidates = (
            analysis.get("anomaly_type"),
            analysis.get("threat_type"),
            analysis.get("attack_type"),
            analysis.get("verdict"),
            analysis.get("threat_level"),
            analysis.get("model_version"),
        )
        for hint in hint_candidates:
            normalized = SentinelKafkaAdapter._normalize_anomaly_hint(hint)
            if normalized != "anomaly":
                return normalized

        labels = envelope.get("labels") if isinstance(envelope, dict) else {}
        if isinstance(labels, dict):
            for hint in (
                labels.get("scenario"),
                labels.get("attack_type"),
                labels.get("threat_type"),
                labels.get("profile_mode"),
            ):
                normalized = SentinelKafkaAdapter._normalize_anomaly_hint(hint)
                if normalized != "anomaly":
                    return normalized

        payload = envelope.get("payload") if isinstance(envelope, dict) else {}
        if isinstance(payload, dict):
            input_part = payload.get("input")
            if isinstance(input_part, dict):
                modality = str(input_part.get("modality") or "").strip().lower()
                threat_level = SentinelKafkaAdapter._normalize_threat_level_value(analysis.get("threat_level"))
                if modality == "network_flow":
                    if threat_level in ("malicious", "critical"):
                        return "ddos"
                    if threat_level == "suspicious":
                        return "network_intrusion"
                input_labels = input_part.get("labels")
                if isinstance(input_labels, dict):
                    for hint in (
                        input_labels.get("scenario"),
                        input_labels.get("attack_type"),
                        input_labels.get("threat_type"),
                        input_labels.get("profile_mode"),
                    ):
                        normalized = SentinelKafkaAdapter._normalize_anomaly_hint(hint)
                        if normalized != "anomaly":
                            return normalized
                for hint in (
                    input_part.get("scenario"),
                    input_part.get("attack_type"),
                    input_part.get("threat_type"),
                    input_part.get("profile_mode"),
                    input_part.get("source"),
                    input_part.get("source_id"),
                    input_part.get("dataset"),
                ):
                    normalized = SentinelKafkaAdapter._normalize_anomaly_hint(hint)
                    if normalized != "anomaly":
                        return normalized

        findings = analysis.get("findings") or []
        if isinstance(findings, list):
            text_parts = []
            for finding in findings:
                if not isinstance(finding, dict):
                    continue
                text_parts.append(str(finding.get("category", "")))
                text_parts.append(str(finding.get("description", "")))
            normalized = SentinelKafkaAdapter._normalize_anomaly_hint(" ".join(text_parts))
            if normalized != "anomaly":
                return normalized
        return "anomaly"

    @staticmethod
    def _normalize_anomaly_hint(value: Any) -> str:
        token = str(value or "").strip().lower().replace("-", "_")
        if not token:
            return "anomaly"
        if "ddos" in token or token in {"dos", "syn_flood", "flood"}:
            return "ddos"
        if "port" in token and "scan" in token:
            return "port_scan"
        if "malware" in token or "yara" in token or "ransom" in token:
            return "malware"
        return "anomaly"

    @staticmethod
    def _normalize_threat_level_value(value: Any) -> str:
        token = str(value or "").strip().lower()
        if not token:
            return ""
        if "." in token:
            token = token.rsplit(".", 1)[-1]
        return token

    def _maybe_publish_policy(
        self,
        *,
        event_id: str,
        anomaly_id: str,
        anomaly_type: str,
        severity: int,
        confidence: float,
        envelope: Dict[str, Any],
        ai_consume_ts_ms: int,
    ) -> bool:
        if not self._cfg.policy_enabled or self._cfg.mode != "prod":
            return False

        payload = envelope.get("payload") if isinstance(envelope, dict) else {}
        modality_token = _metrics_modality_token(((payload or {}).get("input") or {}).get("modality"))
        analysis = payload.get("analysis") if isinstance(payload, dict) else {}
        findings = analysis.get("findings") if isinstance(analysis, dict) else []
        if not isinstance(findings, list):
            findings = []
        labels = envelope.get("labels") if isinstance(envelope, dict) else None
        if not isinstance(labels, dict):
            labels = {}
        network_context = self._extract_network_context(payload, findings, labels=labels)
        label_source_event_id = labels.get("source_event_id") or labels.get("trace_id") or ""
        if label_source_event_id and not network_context.get("source_event_id"):
            network_context["source_event_id"] = label_source_event_id
        label_source_ts_ms = _normalize_event_ts_ms(labels.get("source_event_ts_ms"))
        if label_source_ts_ms > 0:
            network_context["source_event_ts_ms"] = label_source_ts_ms
        label_ingest_ts_ms = _normalize_event_ts_ms(labels.get("telemetry_ingest_ts_ms"))
        if label_ingest_ts_ms > 0:
            network_context["telemetry_ingest_ts_ms"] = label_ingest_ts_ms
        for key in ("flow_id", "source_id", "source_type", "sensor_id"):
            value = labels.get(key)
            if isinstance(value, str) and value.strip() and not network_context.get(key):
                network_context[key] = value.strip()
        validator_id = self._resolve_validator_id(
            envelope=envelope,
            payload=payload if isinstance(payload, dict) else {},
            network_context=network_context,
        )
        if validator_id and not network_context.get("validator_id"):
            network_context["validator_id"] = validator_id
        metadata = {
            "tenant_id": envelope.get("tenant_id"),
            "region": envelope.get("region"),
            "source": envelope.get("source"),
            "anomaly_id": anomaly_id,
            "sentinel_event_id": event_id,
            "source_event_id": network_context.get("source_event_id") or "",
            "source_event_ts_ms": _normalize_event_ts_ms(network_context.get("source_event_ts_ms")),
            "telemetry_ingest_ts_ms": _normalize_event_ts_ms(network_context.get("telemetry_ingest_ts_ms")),
            "trace_id": labels.get("trace_id") or labels.get("source_event_id") or "",
            "t_sentinel_consume_ms": _normalize_event_ts_ms(labels.get("t_sentinel_consume_ms")),
            "t_sentinel_analysis_done_ms": _normalize_event_ts_ms(labels.get("t_sentinel_analysis_done_ms")),
            "t_sentinel_emit_ms": _normalize_event_ts_ms(labels.get("t_sentinel_emit_ms"))
            or _normalize_event_ts_ms(envelope.get("timestamp")),
            "t_ai_sentinel_consume_ms": ai_consume_ts_ms,
            "profile_mode": labels.get("profile_mode"),
            "scenario": labels.get("scenario"),
            "sample_count": labels.get("sample_count"),
            "false_positive_rate": labels.get("false_positive_rate"),
            "harmful_fp_rate": labels.get("harmful_fp_rate"),
            "acceptance_rate": labels.get("acceptance_rate"),
            "findings_count": len(findings),
        }
        if validator_id:
            metadata["validator_id"] = validator_id
        _copy_operational_context(metadata, network_context, "flow_id", "source_id", "source_type", "sensor_id")

        context = PolicyContext(
            anomaly_id=anomaly_id,
            anomaly_type=anomaly_type,
            severity=severity,
            confidence=confidence,
            network_context=network_context,
            metadata=metadata,
        )
        app_events_decision = self._evaluate_app_events_policy_pack(
            context=context,
            envelope=envelope,
            labels=labels,
            metadata=metadata,
        )
        app_events_applies = app_events_decision.applies
        if app_events_decision.applies:
            metadata["app_events"] = {
                "signal": app_events_decision.signal,
                "count_in_window": app_events_decision.count_in_window,
                "threshold": app_events_decision.threshold,
                "entity_key": app_events_decision.entity_key,
                "recommended_action": app_events_decision.recommended_action,
                "critical": app_events_decision.is_critical,
            }
            metadata["control_action_recommendation"] = app_events_decision.recommended_action
            if app_events_decision.should_suppress:
                self._metric_inc("policy_skipped")
                self._metric_inc("app_events_suppressed")
                self._logger.info(
                    "Sentinel policy suppressed by app-events cooldown",
                    extra={
                        "event_id": event_id,
                        "anomaly_id": anomaly_id,
                        "signal": app_events_decision.signal,
                        "reason": app_events_decision.suppress_reason or "app_events_cooldown",
                        "entity_key": app_events_decision.entity_key,
                    },
                )
                return app_events_applies
            severity = app_events_decision.effective_severity
            confidence = app_events_decision.effective_confidence
            context = PolicyContext(
                anomaly_id=anomaly_id,
                anomaly_type=anomaly_type,
                severity=severity,
                confidence=confidence,
                network_context=network_context,
                metadata=metadata,
            )
            self._metric_inc("app_events_promoted")
            try:
                self._service_metrics.record_service_operation(
                    f"app_events_policy_pack_{app_events_decision.signal}",
                    0.0,
                    status="ok",
                )
            except Exception:
                self._logger.debug("Failed to record app-events policy metric", exc_info=True)

        decision_start = time.monotonic()
        decision_status = "ok"
        try:
            decision = build_policy_candidate(context, self._policy_cfg)
        except Exception:
            decision_status = "error"
            raise
        finally:
            try:
                self._service_metrics.record_service_operation(
                    f"sentinel_decision_modality_{modality_token}",
                    time.monotonic() - decision_start,
                    status=decision_status,
                )
            except Exception:
                self._logger.debug("Failed to record sentinel decision modality metric", exc_info=True)

        if decision.candidate is None:
            self._metric_inc("policy_skipped")
            reason = decision.reason or "unknown"
            if reason in ("severity_below_threshold", "confidence_below_threshold"):
                self._metric_inc("policy_skipped_threshold")
            if reason == "missing_target":
                self._metric_inc("policy_skipped_missing_target")
            self._logger.info(
                "Sentinel policy skipped",
                extra={
                    "event_id": event_id,
                    "anomaly_id": anomaly_id,
                    "reason": reason,
                    "anomaly_type": anomaly_type,
                    "severity": severity,
                    "confidence": confidence,
                    "min_severity": decision.effective_severity_threshold,
                    "min_confidence": decision.effective_confidence_threshold,
                },
            )
            return app_events_applies

        candidate = decision.candidate
        # Preserve adapter-side metadata enrichments that are not owned by
        # policy_emitter's canonical metadata builder.
        self._merge_runtime_policy_metadata(candidate_payload=candidate.payload, metadata=metadata)
        aggregation_metadata = None
        aggregation = None
        if self._policy_aggregator is not None:
            aggregation = self._policy_aggregator.admit(candidate=candidate, context=context)
            if not aggregation.publish:
                self._metric_inc("policy_skipped")
                reason = aggregation.reason or "aggregation_suppressed"
                self._logger.info(
                    "Sentinel policy suppressed by aggregation",
                    extra={
                        "event_id": event_id,
                        "anomaly_id": anomaly_id,
                        "reason": reason,
                        "policy_id": candidate.policy_id,
                    },
                )
                return app_events_applies
            if aggregation.policy_id and aggregation.policy_id != candidate.policy_id:
                aggregation_metadata = {
                    "mode": aggregation.mode,
                    "reason": aggregation.reason,
                    "aggregation_key": aggregation.aggregation_key,
                    "signal_count": aggregation.signal_count,
                    "refresh_count": aggregation.refresh_count,
                }
                decision = build_policy_candidate(
                    context,
                    self._policy_cfg,
                    policy_id_override=aggregation.policy_id,
                    aggregation_metadata=aggregation_metadata,
                )
                if decision.candidate is None:
                    self._metric_inc("policy_skipped")
                    return app_events_applies
                candidate = decision.candidate
                self._merge_runtime_policy_metadata(candidate_payload=candidate.payload, metadata=metadata)
            elif aggregation.mode != "publish_new":
                aggregation_metadata = {
                    "mode": aggregation.mode,
                    "reason": aggregation.reason,
                    "aggregation_key": aggregation.aggregation_key,
                    "signal_count": aggregation.signal_count,
                    "refresh_count": aggregation.refresh_count,
                }
                policy_payload = dict(candidate.payload)
                metadata_obj = dict(policy_payload.get("metadata") or {})
                metadata_obj["aggregation"] = aggregation_metadata
                policy_payload["metadata"] = metadata_obj
                candidate = type(candidate)(
                    policy_id=candidate.policy_id,
                    rule_type=candidate.rule_type,
                    action=candidate.action,
                    payload=policy_payload,
                )

        aggregation_mode = str(
            (((candidate.payload.get("metadata") or {}).get("aggregation") or {}).get("mode"))
            or (aggregation.mode if aggregation is not None else "publish_new")
        )
        signal_count = int(
            (((candidate.payload.get("metadata") or {}).get("aggregation") or {}).get("signal_count"))
            or (aggregation.signal_count if aggregation is not None else 1)
        )
        fast_mitigation = decide_fast_mitigation(
            candidate=candidate,
            context=context,
            config=self._policy_cfg,
            aggregation_mode=aggregation_mode,
            signal_count=signal_count,
        )

        policy_id = str(candidate.policy_id)
        policy_payload = dict(candidate.payload)
        policy_payload["policy_id"] = policy_id
        if _env_bool("POLICY_STAGE_MARKERS_ENABLED", False):
            trace_id = (
                (policy_payload.get("trace") or {}).get("id")
                or (policy_payload.get("metadata") or {}).get("trace_id")
                or policy_payload.get("trace_id")
                or policy_payload.get("qc_reference")
                or ""
            )
            if trace_id:
                self._logger.info(
                    "policy stage marker",
                    extra={
                        "stage": "t_ai_decision_done",
                        "policy_id": policy_id,
                        "trace_id": str(trace_id),
                        "t_ms": int(time.time() * 1000),
                    },
                )

        self._metric_inc("policy_candidates")
        task = {
            "event_id": event_id,
            "anomaly_id": anomaly_id,
            "policy_id": policy_id,
            "rule_type": candidate.rule_type,
            "enforcement_action": candidate.action,
            "modality": modality_token,
            "policy_payload": policy_payload,
            "dispatch_mode": aggregation_mode,
            "fast_mitigation_publish": bool(fast_mitigation.publish),
            "fast_mitigation_id": str(fast_mitigation.mitigation_id or ""),
            "fast_mitigation_payload": fast_mitigation.payload,
        }
        if self._policy_publish_queue is not None:
            try:
                self._policy_publish_queue.put_nowait(task)
                self._metric_inc("policy_enqueued")
                return app_events_applies
            except queue.Full:
                # Preserve correctness over throughput: fallback to synchronous publish.
                self._metric_inc("policy_queue_overflow_sync")

        self._publish_policy_task(task)
        return app_events_applies

    def _publish_policy_task(self, task: Dict[str, Any]) -> None:
        publish_start = time.monotonic()
        modality_token = _metrics_modality_token(task.get("modality"))
        with start_span(
            "ai.policy.publish",
            tracer_name="ai-service/sentinel",
            attributes={
                "messaging.system": "kafka",
                "messaging.operation": "publish",
                "policy.id": str(task.get("policy_id") or ""),
                "policy.rule_type": str(task.get("rule_type") or ""),
                "policy.action": str(task.get("enforcement_action") or ""),
            },
        ) as span:
            try:
                publish_fast = getattr(self._publisher, "publish_fast_mitigation_async", None)
                if not callable(publish_fast):
                    publish_fast = getattr(self._publisher, "publish_fast_mitigation", None)
                if task.get("fast_mitigation_publish") and callable(publish_fast):
                    try:
                        fast_accepted = publish_fast(
                            mitigation_id=task["fast_mitigation_id"],
                            policy_id=task["policy_id"],
                            rule_type=task["rule_type"],
                            enforcement_action=task["enforcement_action"],
                            payload=task["fast_mitigation_payload"],
                        )
                        if fast_accepted is False:
                            self._metric_inc("fast_mitigation_dispatch_rejected")
                        else:
                            self._metric_inc("fast_mitigation_dispatch_requested")
                    except Exception as exc:
                        self._metric_inc("fast_mitigation_failed")
                        self._logger.warning(
                            "Sentinel fast mitigation publish failed; durable policy path continues",
                            extra={
                                "event_id": task["event_id"],
                                "anomaly_id": task["anomaly_id"],
                                "policy_id": task["policy_id"],
                                "error": str(exc),
                            },
                        )
                self._publisher.publish_policy_violation(
                    policy_id=task["policy_id"],
                    rule_type=task["rule_type"],
                    enforcement_action=task["enforcement_action"],
                    payload=task["policy_payload"],
                )
                self._metric_inc("policy_published")
                if self._tracker:
                    self._tracker.record_policy_dispatched(
                        anomaly_id=task["anomaly_id"],
                        policy_id=task["policy_id"],
                        mitigation_id=str(task.get("fast_mitigation_id") or "") or None,
                        dispatch_mode=str(task.get("dispatch_mode") or "publish_new"),
                        ttl_seconds=getattr(self._policy_cfg, "ttl_seconds", None),
                        requires_ack=getattr(self._policy_cfg, "requires_ack", None),
                        fast_path=bool(task.get("fast_mitigation_publish")),
                        timestamp=time.time(),
                    )
            except Exception as exc:  # pragma: no cover - defensive
                self._metric_inc("policy_failed")
                span.set_attribute("error.class", "policy_publish_failed")
                try:
                    elapsed = time.monotonic() - publish_start
                    self._service_metrics.record_service_operation(
                        "sentinel_publish_policy",
                        elapsed,
                        status="error",
                    )
                    self._service_metrics.record_service_operation(
                        f"sentinel_publish_policy_modality_{modality_token}",
                        elapsed,
                        status="error",
                    )
                except Exception:
                    self._logger.debug("Failed to record sentinel policy publish metric", exc_info=True)
                self._logger.error(
                    "Sentinel policy publish failed",
                    extra={"event_id": task["event_id"], "anomaly_id": task["anomaly_id"], "error": str(exc)},
                    exc_info=True,
                )
                return
            try:
                elapsed = time.monotonic() - publish_start
                self._service_metrics.record_service_operation(
                    "sentinel_publish_policy",
                    elapsed,
                    status="ok",
                )
                self._service_metrics.record_service_operation(
                    f"sentinel_publish_policy_modality_{modality_token}",
                    elapsed,
                    status="ok",
                )
            except Exception:
                self._logger.debug("Failed to record sentinel policy publish metric", exc_info=True)

    def _evaluate_app_events_policy_pack(
        self,
        *,
        context: PolicyContext,
        envelope: Dict[str, Any],
        labels: Dict[str, Any],
        metadata: Dict[str, Any],
    ) -> AppEventsPolicyDecision:
        if not self._app_events_pack.enabled:
            self._metric_inc("app_events_no_match")
            return AppEventsPolicyDecision(
                matched=False,
                signal="",
                threshold=0,
                count_in_window=0,
                effective_severity=context.severity,
                effective_confidence=context.confidence,
                recommended_action="",
                should_suppress=False,
                suppress_reason="disabled",
                entity_key="",
                is_critical=False,
            )

        decision = self._app_events_pack.evaluate(
            labels=labels if isinstance(labels, dict) else {},
            metadata=metadata if isinstance(metadata, dict) else {},
            network_context=context.network_context if isinstance(context.network_context, dict) else {},
            anomaly_type=context.anomaly_type,
            severity=context.severity,
            confidence=context.confidence,
        )
        if decision.applies:
            self._metric_inc("app_events_detected")
        else:
            self._metric_inc("app_events_no_match")
        return decision

    @staticmethod
    def _merge_runtime_policy_metadata(*, candidate_payload: Dict[str, Any], metadata: Dict[str, Any]) -> None:
        if not isinstance(candidate_payload, dict) or not isinstance(metadata, dict):
            return
        payload_metadata = candidate_payload.get("metadata")
        if not isinstance(payload_metadata, dict):
            payload_metadata = {}
            candidate_payload["metadata"] = payload_metadata
        if "app_events" in metadata:
            app_events = metadata.get("app_events")
            if isinstance(app_events, dict):
                payload_metadata["app_events"] = dict(app_events)
        recommendation = metadata.get("control_action_recommendation")
        if isinstance(recommendation, str) and recommendation.strip():
            payload_metadata["control_action_recommendation"] = recommendation.strip()

    @staticmethod
    def _map_severity(analysis: Dict[str, Any]) -> int:
        score = SentinelKafkaAdapter._safe_float(analysis.get("final_score"), default=0.5)
        if score >= 0.90:
            return 10
        if score >= 0.75:
            return 9
        if score >= 0.60:
            return 8
        if score >= 0.40:
            return 6
        return 4

    @staticmethod
    def _extract_source(envelope: Dict[str, Any]) -> str:
        payload = envelope.get("payload") or {}
        input_part = payload.get("input") if isinstance(payload, dict) else {}
        source = ""
        if isinstance(input_part, dict):
            source = str(input_part.get("source") or "")
        if not source:
            source = str(envelope.get("source") or "sentinel")
        return source[:256]

    @staticmethod
    def _extract_network_context(
        payload: Any,
        findings: list[Dict[str, Any]],
        *,
        labels: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        network_context: Dict[str, Any] = {}
        input_part = payload.get("input") if isinstance(payload, dict) else {}
        if isinstance(input_part, dict):
            source_id = input_part.get("id")
            if isinstance(source_id, str) and source_id:
                network_context["source_event_id"] = source_id
            for key in ("flow_id", "source_id", "source_type", "sensor_id"):
                value = input_part.get(key)
                if isinstance(value, str) and value.strip():
                    network_context[key] = value.strip()
            explicit_source_ts = _normalize_event_ts_ms(input_part.get("source_event_ts_ms"))
            if explicit_source_ts > 0:
                network_context["source_event_ts_ms"] = explicit_source_ts
            explicit_ingest_ts = _normalize_event_ts_ms(input_part.get("telemetry_ingest_ts_ms"))
            if explicit_ingest_ts > 0:
                network_context["telemetry_ingest_ts_ms"] = explicit_ingest_ts
            source_ts = input_part.get("timestamp")
            if "source_event_ts_ms" not in network_context and isinstance(source_ts, (int, float)) and source_ts > 0:
                network_context["source_event_ts_ms"] = int(float(source_ts) * 1000.0)
            for key in ("src_ip", "source_ip", "dst_ip", "destination_ip"):
                value = input_part.get(key)
                if isinstance(value, str) and value:
                    network_context[key] = value
        if isinstance(labels, dict):
            for key in ("src_ip", "source_ip", "dst_ip", "destination_ip"):
                value = labels.get(key)
                if isinstance(value, str) and value.strip():
                    network_context.setdefault(key, value.strip())
            for key in ("src_port", "dst_port", "proto"):
                value = labels.get(key)
                if value in (None, ""):
                    continue
                try:
                    parsed = int(str(value).strip())
                except (TypeError, ValueError):
                    continue
                network_context.setdefault(key, parsed)

        for finding in findings:
            if not isinstance(finding, dict):
                continue
            desc = str(finding.get("description") or "")
            match = re.search(r"\b(?:\d{1,3}\.){3}\d{1,3}\b", desc)
            if match:
                network_context.setdefault("src_ip", match.group(0))
                break
        return network_context

    def _resolve_validator_id(
        self,
        *,
        envelope: Dict[str, Any],
        payload: Dict[str, Any],
        network_context: Dict[str, Any],
    ) -> Optional[str]:
        """Resolve validator identity from explicit metadata first, then trusted IP mapping."""

        labels = envelope.get("labels") if isinstance(envelope, dict) else {}
        if not isinstance(labels, dict):
            labels = {}
        input_part = payload.get("input") if isinstance(payload, dict) else {}
        if not isinstance(input_part, dict):
            input_part = {}

        explicit_keys = (
            "validator_id",
            "validator",
            "node_id",
            "node",
            "producer_id",
        )
        for source in (labels, input_part, network_context):
            for key in explicit_keys:
                value = source.get(key)
                if isinstance(value, str) and value.strip():
                    return value.strip()

        for key in ("src_ip", "source_ip", "dst_ip", "destination_ip"):
            candidate = network_context.get(key) or input_part.get(key)
            normalized_ip = self._normalize_ip(candidate)
            if normalized_ip and normalized_ip in self._validator_ip_map:
                return self._validator_ip_map[normalized_ip]
        return None

    @staticmethod
    def _normalize_ip(value: Any) -> str:
        if not isinstance(value, str):
            return ""
        raw = value.strip()
        if not raw:
            return ""
        try:
            return str(ipaddress.ip_address(raw))
        except ValueError:
            return ""

    @staticmethod
    def _safe_float(value: Any, default: Optional[float]) -> Optional[float]:
        if value is None:
            return default
        try:
            return float(value)
        except (TypeError, ValueError):
            return default

    @staticmethod
    def _deterministic_uuid4_from_seed(seed: str) -> str:
        digest = bytearray(hashlib.sha256(seed.encode("utf-8")).digest()[:16])
        digest[6] = (digest[6] & 0x0F) | 0x40  # version 4
        digest[8] = (digest[8] & 0x3F) | 0x80  # RFC 4122 variant
        return str(uuid.UUID(bytes=bytes(digest)))

    @staticmethod
    def _build_anomaly_payload(
        envelope: Dict[str, Any],
        *,
        severity: int,
        confidence: float,
        max_size: int,
    ) -> bytes:
        payload = envelope.get("payload") if isinstance(envelope, dict) else {}
        analysis = payload.get("analysis") if isinstance(payload, dict) else {}
        findings = analysis.get("findings") if isinstance(analysis, dict) else []
        if not isinstance(findings, list):
            findings = []

        compact = {
            "schema_version": envelope.get("schema_version"),
            "event_id": envelope.get("event_id"),
            "tenant_id": envelope.get("tenant_id"),
            "source": envelope.get("source"),
            "labels": {
                key: value
                for key, value in ((envelope.get("labels") or {}) if isinstance(envelope, dict) else {}).items()
                if key in {"trace_id", "source_event_id", "flow_id", "source_id", "source_type", "sensor_id"}
            },
            "severity": severity,
            "confidence": float(confidence),
            "analysis": {
                "threat_level": analysis.get("threat_level"),
                "final_score": analysis.get("final_score"),
                "confidence": analysis.get("confidence"),
                "errors": analysis.get("errors") or [],
                "findings": findings[:20],
            },
        }
        if isinstance(payload, dict):
            input_part = payload.get("input")
            if isinstance(input_part, dict):
                compact["input"] = {
                    key: input_part.get(key)
                    for key in (
                        "id",
                        "source",
                        "modality",
                        "features_version",
                        "timestamp",
                        "flow_id",
                        "source_id",
                        "source_type",
                        "sensor_id",
                        "src_ip",
                        "dst_ip",
                    )
                    if input_part.get(key) not in (None, "", [], {})
                }
        encoded = json.dumps(compact, separators=(",", ":")).encode("utf-8")
        if len(encoded) <= max_size:
            return encoded
        compact["analysis"]["findings"] = []
        compact["analysis"]["truncated"] = True
        return json.dumps(compact, separators=(",", ":")).encode("utf-8")

    def _decode_envelope(self, raw: bytes) -> Optional[Dict[str, Any]]:
        if self._cfg.input_encoding == "json":
            return self._decode_json(raw)
        if self._cfg.input_encoding == "protobuf":
            return self._decode_protobuf(raw)
        # auto mode: protobuf-first then json
        decoded = self._decode_protobuf(raw)
        if decoded is not None:
            return decoded
        return self._decode_json(raw)

    @staticmethod
    def _decode_json(raw: bytes) -> Optional[Dict[str, Any]]:
        try:
            obj = json.loads(raw.decode("utf-8"))
            return obj if isinstance(obj, dict) else None
        except Exception:
            return None

    @staticmethod
    def _decode_protobuf(raw: bytes) -> Optional[Dict[str, Any]]:
        try:
            msg = SentinelResultEvent()
            msg.ParseFromString(raw)
        except Exception:
            return None

        findings = []
        for finding in msg.findings:
            findings.append(
                {
                    "id": finding.id,
                    "agent": finding.agent,
                    "category": finding.category,
                    "description": finding.description,
                    "severity": finding.severity,
                    "score": finding.score,
                }
            )

        return {
            "schema_version": msg.schema_version,
            "event_id": msg.event_id,
            "tenant_id": msg.tenant_id,
            "timestamp": msg.timestamp,
            "source": msg.source,
            "payload_type": msg.payload_type,
            "labels": dict(msg.labels),
            "payload": {
                "input": {
                    "id": msg.input.id,
                    "tenant_id": msg.input.tenant_id,
                    "source": msg.input.source,
                    "modality": msg.input.modality,
                    "features_version": msg.input.features_version,
                    "timestamp": msg.input.timestamp,
                },
                "analysis": {
                    "threat_level": msg.threat_level,
                    "final_score": msg.final_score,
                    "confidence": msg.confidence,
                    "findings": findings,
                    "errors": list(msg.errors),
                    "degraded": bool(msg.degraded),
                    "verdict": msg.verdict,
                    "model_version": msg.model_version,
                },
            },
        }
