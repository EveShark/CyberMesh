"""
Sentinel Kafka adapter for AI-service integration.

Consumes Sentinel verdict envelopes and forwards high-risk detections into
the existing AI anomaly publish path.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import threading
import time
import uuid
from dataclasses import dataclass
from typing import Any, Dict, Optional

from confluent_kafka import Consumer, KafkaException

from ..config.settings import Settings
from ..contracts.generated.sentinel_result_pb2 import SentinelResultEvent
from ..feedback.tracker import AnomalyLifecycleTracker
from .policy_emitter import PolicyContext, build_policy_candidate
from .publisher import MessagePublisher
from ..utils.errors import StorageError, ValidationError


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
        )


class SentinelKafkaAdapter:
    """Bridge Sentinel verdict stream into ai.anomalies.v1 publisher path."""

    def __init__(
        self,
        settings: Settings,
        publisher: MessagePublisher,
        logger,
        tracker: Optional[AnomalyLifecycleTracker] = None,
    ):
        self._settings = settings
        self._publisher = publisher
        self._logger = logger
        self._tracker = tracker
        self._cfg = SentinelAdapterConfig.from_env(settings)
        self._policy_cfg = settings.policy_publishing

        self._running = False
        self._thread: Optional[threading.Thread] = None
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
        }

        self._consumer = Consumer(self._build_consumer_config())
        self._consumer.subscribe([self._cfg.input_topic])

    @property
    def enabled(self) -> bool:
        return self._cfg.enabled

    def start(self) -> None:
        if not self._cfg.enabled or self._running:
            return
        self._running = True
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
        try:
            self._consumer.close()
        except Exception:
            pass
        self._logger.info("Sentinel adapter stopped", extra={"metrics": dict(self._metrics)})

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
                    continue
                if msg.error():
                    continue
                self._metrics["received"] += 1
                self._handle(msg.value() or b"")
                self._consumer.commit(message=msg, asynchronous=False)
            except KafkaException as exc:
                self._logger.error("Sentinel adapter Kafka error", extra={"error": str(exc)})
                time.sleep(1)
            except Exception as exc:  # defensive: never crash background thread
                self._logger.error("Sentinel adapter loop error", extra={"error": str(exc)}, exc_info=True)
                time.sleep(1)

    def _handle(self, raw: bytes) -> None:
        if len(raw) > self._cfg.max_message_bytes:
            self._metrics["invalid"] += 1
            self._logger.warning("Sentinel envelope exceeds max size", extra={"size": len(raw)})
            return
        envelope = self._decode_envelope(raw)
        if envelope is None:
            self._metrics["invalid"] += 1
            self._logger.warning("Sentinel envelope decode failed", extra={"encoding": self._cfg.input_encoding})
            return
        if not isinstance(envelope, dict):
            self._metrics["invalid"] += 1
            return

        schema_version = str(envelope.get("schema_version") or "")
        if self._cfg.require_schema_version and schema_version != self._cfg.require_schema_version:
            self._metrics["invalid"] += 1
            self._logger.warning(
                "Sentinel schema mismatch",
                extra={"got": schema_version, "want": self._cfg.require_schema_version},
            )
            return

        event_id = str(envelope.get("event_id") or "")
        if not event_id:
            self._metrics["invalid"] += 1
            return
        if self._is_duplicate(event_id):
            self._metrics["deduped"] += 1
            return

        payload = envelope.get("payload") or {}
        analysis = payload.get("analysis") if isinstance(payload, dict) else {}
        if not isinstance(analysis, dict):
            self._metrics["invalid"] += 1
            return

        threat_level = str(analysis.get("threat_level") or "").lower()
        if threat_level in ("clean", "unknown", ""):
            self._metrics["skipped_clean"] += 1
            return

        anomaly_type = self._map_anomaly_type(analysis)
        confidence = self._safe_float(analysis.get("confidence"), default=0.75)
        severity = self._map_severity(analysis)
        source = self._extract_source(envelope)

        anomaly_id = self._stable_uuid4(event_id)
        payload_bytes = self._build_anomaly_payload(
            envelope,
            severity=severity,
            confidence=confidence,
            max_size=self._cfg.max_message_bytes,
        )
        now = int(time.time())

        if self._cfg.mode == "shadow":
            self._metrics["forwarded"] += 1
            self._logger.info(
                "Sentinel shadow detection observed",
                extra={"event_id": event_id, "anomaly_type": anomaly_type, "severity": severity},
            )
            return

        try:
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
            )
            self._metrics["forwarded"] += 1
        except (ValidationError, StorageError) as exc:
            self._metrics["publish_failed"] += 1
            self._logger.error("Sentinel adapter tracker error", extra={"error": str(exc)})
        except Exception as exc:
            self._metrics["publish_failed"] += 1
            self._logger.error("Sentinel adapter publish failed", extra={"error": str(exc)}, exc_info=True)

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
    def _map_anomaly_type(analysis: Dict[str, Any]) -> str:
        findings = analysis.get("findings") or []
        if isinstance(findings, list):
            joined = " ".join(str(f.get("description", "")) for f in findings if isinstance(f, dict)).lower()
            if "pps" in joined or "bps" in joined or "ddos" in joined:
                return "ddos"
            if "malware" in joined or "yara" in joined:
                return "malware"
            if "port" in joined and "scan" in joined:
                return "port_scan"
        return "anomaly"

    def _maybe_publish_policy(
        self,
        *,
        event_id: str,
        anomaly_id: str,
        anomaly_type: str,
        severity: int,
        confidence: float,
        envelope: Dict[str, Any],
    ) -> None:
        if not self._cfg.policy_enabled or self._cfg.mode != "prod":
            return

        payload = envelope.get("payload") if isinstance(envelope, dict) else {}
        analysis = payload.get("analysis") if isinstance(payload, dict) else {}
        findings = analysis.get("findings") if isinstance(analysis, dict) else []
        if not isinstance(findings, list):
            findings = []

        network_context = self._extract_network_context(payload, findings)
        labels = envelope.get("labels") if isinstance(envelope, dict) else None
        if not isinstance(labels, dict):
            labels = {}
        metadata = {
            "tenant_id": envelope.get("tenant_id"),
            "region": envelope.get("region"),
            "source": envelope.get("source"),
            "profile_mode": labels.get("profile_mode"),
            "scenario": labels.get("scenario"),
            "sample_count": labels.get("sample_count"),
            "false_positive_rate": labels.get("false_positive_rate"),
            "harmful_fp_rate": labels.get("harmful_fp_rate"),
            "acceptance_rate": labels.get("acceptance_rate"),
            "findings_count": len(findings),
        }

        decision = build_policy_candidate(
            PolicyContext(
                anomaly_id=anomaly_id,
                anomaly_type=anomaly_type,
                severity=severity,
                confidence=confidence,
                network_context=network_context,
                metadata=metadata,
            ),
            self._policy_cfg,
        )

        if decision.candidate is None:
            self._metrics["policy_skipped"] += 1
            reason = decision.reason or "unknown"
            if reason in ("severity_below_threshold", "confidence_below_threshold"):
                self._metrics["policy_skipped_threshold"] += 1
            if reason == "missing_target":
                self._metrics["policy_skipped_missing_target"] += 1
            self._logger.info(
                "Sentinel policy skipped",
                extra={"event_id": event_id, "anomaly_id": anomaly_id, "reason": reason},
            )
            return

        candidate = decision.candidate
        target = network_context.get("src_ip") or network_context.get("source_ip") or ""
        policy_id = self._stable_uuid4(f"{event_id}|{candidate.rule_type}|{candidate.action}|{target}")
        policy_payload = dict(candidate.payload)
        policy_payload["policy_id"] = policy_id

        self._metrics["policy_candidates"] += 1
        try:
            self._publisher.publish_policy_violation(
                policy_id=policy_id,
                rule_type=candidate.rule_type,
                enforcement_action=candidate.action,
                payload=policy_payload,
            )
            self._metrics["policy_published"] += 1
            if self._tracker:
                self._tracker.record_policy_dispatched(
                    anomaly_id=anomaly_id,
                    policy_id=policy_id,
                    ttl_seconds=getattr(self._policy_cfg, "ttl_seconds", None),
                    requires_ack=getattr(self._policy_cfg, "requires_ack", None),
                    fast_path=bool(getattr(self._policy_cfg, "canary_scope", False)),
                    timestamp=time.time(),
                )
        except Exception as exc:  # pragma: no cover - defensive
            self._metrics["policy_failed"] += 1
            self._logger.error(
                "Sentinel policy publish failed",
                extra={"event_id": event_id, "anomaly_id": anomaly_id, "error": str(exc)},
                exc_info=True,
            )

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
    def _extract_network_context(payload: Any, findings: list[Dict[str, Any]]) -> Dict[str, Any]:
        network_context: Dict[str, Any] = {}
        input_part = payload.get("input") if isinstance(payload, dict) else {}
        if isinstance(input_part, dict):
            for key in ("src_ip", "source_ip", "dst_ip", "destination_ip"):
                value = input_part.get(key)
                if isinstance(value, str) and value:
                    network_context[key] = value

        for finding in findings:
            if not isinstance(finding, dict):
                continue
            desc = str(finding.get("description") or "")
            match = re.search(r"\b(?:\d{1,3}\.){3}\d{1,3}\b", desc)
            if match:
                network_context.setdefault("src_ip", match.group(0))
                break
        return network_context

    @staticmethod
    def _safe_float(value: Any, default: Optional[float]) -> Optional[float]:
        if value is None:
            return default
        try:
            return float(value)
        except (TypeError, ValueError):
            return default

    @staticmethod
    def _stable_uuid4(event_id: str) -> str:
        digest = bytearray(hashlib.sha256(event_id.encode("utf-8")).digest()[:16])
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
