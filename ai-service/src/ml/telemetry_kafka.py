"""
Kafka telemetry source for live CIC feature ingestion.

Consumes telemetry.features.v1 and returns flow dictionaries compatible with FlowFeatureExtractor.
"""

import hashlib
import json
import time
import os
import sys
from pathlib import Path
from typing import Dict, List, Optional
from collections import OrderedDict

from confluent_kafka import Consumer, Producer, KafkaError, KafkaException, TopicPartition

from ..logging import get_logger
from ..config.settings import Settings
from .features_flow import FlowFeatureExtractor
from .telemetry import TelemetrySource


def _parse_feature_mask(mask: str) -> int:
    try:
        return int(mask, 16)
    except Exception:
        return 0


def _apply_feature_mask(flow: Dict, feature_names: List[str], mask: str) -> Dict:
    if not mask:
        return flow
    bitmask = _parse_feature_mask(mask)
    if bitmask == 0:
        return flow

    for idx, name in enumerate(feature_names):
        if not (bitmask & (1 << idx)):
            flow[name] = None
    return flow


def _meets_coverage(flow: Dict, min_coverage: float) -> bool:
    coverage = flow.get("feature_coverage")
    if coverage is None:
        return True
    try:
        return float(coverage) >= float(min_coverage)
    except Exception:
        return False


def _validate_required(flow: Dict, required: List[str]) -> None:
    missing = [key for key in required if flow.get(key) in (None, "")]
    if missing:
        raise ValueError(f"missing required fields: {', '.join(missing)}")

def _decode_feature_proto(payload: bytes) -> Dict:
    proto_path = os.getenv("TELEMETRY_PROTO_PATH")
    if proto_path:
        path = Path(proto_path)
        if path.exists():
            sys.path.insert(0, str(path))
    try:
        from telemetry_feature_v1_pb2 import CicFeaturesV1  # type: ignore
    except Exception as exc:
        raise ValueError(f"protobuf module unavailable: {exc}") from exc
    msg = CicFeaturesV1()
    msg.ParseFromString(payload)
    flow = {
        "schema": msg.schema or "cic.v1",
        "ts": int(msg.ts or 0),
        "tenant_id": msg.tenant_id or "",
        "flow_id": msg.flow_id or "",
        "src_ip": msg.src_ip or "",
        "dst_ip": msg.dst_ip or "",
        "src_port": int(msg.src_port or 0),
        "dst_port": int(msg.dst_port or 0),
        "protocol": int(msg.protocol or 0),
        "feature_mask": msg.feature_mask.hex() if msg.feature_mask else "",
        "feature_coverage": float(msg.feature_coverage or 0.0),
        "source_type": msg.source_type or "",
        "source_id": msg.source_id or "",
        "source_event_ts_ms": int(getattr(msg, "source_event_ts_ms", 0) or 0),
        "telemetry_ingest_ts_ms": int(getattr(msg, "telemetry_ingest_ts_ms", 0) or 0),
    }
    feature_names = FlowFeatureExtractor.FEATURE_COLUMNS
    skip = {"src_port", "dst_port", "protocol"}
    for name in feature_names:
        if name in skip:
            continue
        if hasattr(msg, name):
            flow[name] = float(getattr(msg, name))
    return flow


class KafkaTelemetrySource(TelemetrySource):
    """Kafka telemetry source for feature ingestion."""

    REQUIRED_FIELDS = [
        "schema",
        "ts",
        "tenant_id",
        "flow_id",
        "src_ip",
        "dst_ip",
        "src_port",
        "dst_port",
        "protocol",
    ]

    def __init__(
        self,
        settings: Settings,
        topic: str,
        dlq_topic: str,
        *,
        consumer_group: Optional[str] = None,
        poll_timeout: float = 1.0,
        feature_mask_enabled: bool = True,
    ):
        self.settings = settings
        self.topic = topic
        self.dlq_topic = dlq_topic
        self.poll_timeout = max(0.1, float(poll_timeout))
        self.feature_mask_enabled = bool(feature_mask_enabled)
        self.min_feature_coverage = float(os.getenv("FEATURE_MIN_COVERAGE", getattr(settings, "min_feature_coverage", 0.0)))
        self.dedup_enabled = os.getenv("TELEMETRY_DEDUP_ENABLED", "true").lower() in ("1", "true", "yes", "on")
        self.dedup_ttl_sec = max(1, int(os.getenv("TELEMETRY_DEDUP_TTL_SEC", "120")))
        self.dedup_max_keys = max(1000, int(os.getenv("TELEMETRY_DEDUP_MAX_KEYS", "50000")))
        self.commit_mode = (os.getenv("AI_TELEMETRY_COMMIT_MODE", "sync") or "sync").strip().lower()
        if self.commit_mode not in ("sync", "batch"):
            self.commit_mode = "sync"
        self.commit_batch_size = max(1, int(os.getenv("AI_TELEMETRY_COMMIT_BATCH_SIZE", "1")))
        self.commit_interval_ms = max(1, int(os.getenv("AI_TELEMETRY_COMMIT_INTERVAL_MS", "1000")))
        self.poll_drain_max_messages = max(1, int(os.getenv("AI_TELEMETRY_DRAIN_MAX_MESSAGES", "1")))
        self.poll_drain_max_ms = max(0, int(os.getenv("AI_TELEMETRY_DRAIN_MAX_MS", "0")))
        self.dlq_sync_flush = os.getenv("AI_TELEMETRY_DLQ_SYNC_FLUSH", "true").lower() in ("1", "true", "yes", "on")
        self.dlq_flush_interval_ms = max(1, int(os.getenv("AI_TELEMETRY_DLQ_FLUSH_INTERVAL_MS", "500")))
        self._pending_commits = 0
        self._last_commit_at = time.monotonic()
        self._last_dlq_flush_at = time.monotonic()
        self._seen_flows: "OrderedDict[str, float]" = OrderedDict()
        self.logger = get_logger(__name__)
        self._buffer: List[Dict] = []
        self._prefetched_messages = []
        self._last_fetch_stats: Dict[str, object] = {}

        self._feature_names = FlowFeatureExtractor.FEATURE_COLUMNS
        self._consumer = Consumer(self._build_consumer_config(consumer_group))
        self._consumer.subscribe([self.topic])
        self._producer = Producer(self._build_producer_config())

        self.logger.info(
            "Initialized KafkaTelemetrySource",
            extra={
                "topic": self.topic,
                "dlq_topic": self.dlq_topic,
                "group": consumer_group or f"{self.settings.kafka_consumer.group_id}-telemetry",
            },
        )

    def get_network_flows(self, limit: int = 100, *, wait_policy: Optional[str] = None) -> List[Dict]:
        limit = max(1, int(limit))
        fetch_started = time.perf_counter()
        flows: List[Dict] = []
        wait_cfg = self._resolve_wait_policy(wait_policy)
        max_empty_polls = wait_cfg["max_empty_polls"]
        poll_timeout = wait_cfg["poll_timeout"]
        assign_timeout = wait_cfg["assign_timeout_sec"]
        empty_polls = 0
        rejected_count = 0
        coverage_reject_count = 0
        duplicate_count = 0
        accepted_count = 0
        assignment_polls = 0
        poll_count = 0
        reason = "success"
        seek_latest = os.getenv("TELEMETRY_SEEK_LATEST", "false").lower() in ("1", "true", "yes", "on")
        seek_ts_ms = os.getenv("TELEMETRY_SEEK_TIMESTAMP_MS")
        assignment_wait_ms = 0.0
        # Ensure assignments are established before applying any seek.
        # If we seek-to-latest before the assignment exists, the first seek may happen
        # *after* new messages arrive and skip them (common in E2E tests).
        if not getattr(self, "_assigned", False):
            assignment_wait_ms, assignment_polls, poll_count, assigned = self._prime_assignment(
                timeout_sec=assign_timeout,
                poll_count=poll_count,
                assignment_polls=assignment_polls,
            )
            if assigned:
                self._assigned = True
            elif assign_timeout > 0:
                reason = "no_assignment"

        if seek_ts_ms:
            try:
                self._apply_seek_timestamp(seek_ts_ms)
            except Exception:
                pass

        if seek_latest and getattr(self, "_assigned", False) and not getattr(self, "_seek_latest_applied", False):
            try:
                assignment = self._consumer.assignment() or []
                for tp in assignment:
                    try:
                        _low, high = self._consumer.get_watermark_offsets(tp, timeout=5.0)
                        self._consumer.seek(TopicPartition(tp.topic, tp.partition, high))
                    except Exception:
                        continue
                self._seek_latest_applied = True
            except Exception:
                pass

        while self._buffer and len(flows) < limit:
            flows.append(self._buffer.pop(0))

        while len(flows) < limit:
            if self._prefetched_messages:
                msg = self._prefetched_messages.pop(0)
            else:
                msg = None
            if not self._consumer.assignment():
                if seek_ts_ms:
                    try:
                        self._apply_seek_timestamp(seek_ts_ms)
                    except Exception:
                        pass
                if msg is None:
                    self._consumer.poll(0.1)
                    poll_count += 1
                    assignment_polls += 1
                if not self._consumer.assignment():
                    empty_polls += 1
                    if empty_polls >= max_empty_polls:
                        if reason == "success":
                            reason = "no_assignment"
                        break
                    continue
            if msg is None:
                msg = self._consumer.poll(poll_timeout)
                poll_count += 1
            if msg is None:
                empty_polls += 1
                if empty_polls >= max_empty_polls:
                    if reason == "success":
                        reason = "empty_topic_window"
                    break
                continue
            drained_messages, drain_polls = self._drain_messages(msg)
            poll_count += drain_polls
            for msg in drained_messages:
                if msg.error():
                    if msg.error().code() == KafkaError._PARTITION_EOF:
                        continue
                    self.logger.error(f"Kafka telemetry error: {msg.error()}")
                    continue

                payload = msg.value() or b""
                try:
                    try:
                        flow = json.loads(payload.decode("utf-8"))
                    except Exception:
                        flow = _decode_feature_proto(payload)
                    if flow.get("schema") != "cic.v1":
                        raise ValueError("invalid schema")
                    _validate_required(flow, self.REQUIRED_FIELDS)
                    if self.feature_mask_enabled:
                        flow = _apply_feature_mask(flow, self._feature_names, flow.get("feature_mask", ""))
                    if not _meets_coverage(flow, self.min_feature_coverage):
                        coverage_reject_count += 1
                        raise ValueError("feature coverage below threshold")
                    if self._is_duplicate(flow):
                        duplicate_count += 1
                        self._commit_processed(message=msg, processed=True)
                        continue
                except Exception as exc:
                    rejected_count += 1
                    self._emit_dlq("TELEMETRY_INVALID", str(exc), payload)
                    self._commit_processed(message=msg, processed=True)
                    continue

                flows.append(flow)
                accepted_count += 1
                self._commit_processed(message=msg, processed=True)
                if len(flows) >= limit:
                    break

        self._commit_processed(force=True)
        self._flush_dlq(force=True)
        if not flows:
            if rejected_count > 0 and duplicate_count == 0:
                reason = "all_rejected"
            elif duplicate_count > 0 and rejected_count == 0:
                reason = "all_duplicates"
            elif rejected_count > 0 or duplicate_count > 0:
                reason = "mixed_filtered"
            elif reason == "success":
                reason = "empty_topic_window"
        total_duration_ms = (time.perf_counter() - fetch_started) * 1000
        self._last_fetch_stats = {
            "wait_policy": wait_cfg["policy_name"],
            "limit": limit,
            "accepted_count": accepted_count,
            "rejected_count": rejected_count,
            "coverage_reject_count": coverage_reject_count,
            "duplicate_count": duplicate_count,
            "empty_polls": empty_polls,
            "assignment_polls": assignment_polls,
            "poll_count": poll_count,
            "assignment_wait_ms": round(assignment_wait_ms, 2),
            "poll_timeout_sec": poll_timeout,
            "max_empty_polls": max_empty_polls,
            "assign_timeout_sec": assign_timeout,
            "total_duration_ms": round(total_duration_ms, 2),
            "reason": reason,
        }
        if not flows:
            self.logger.info("Kafka telemetry fetch yielded no flows", extra=self._last_fetch_stats)
        return flows

    def get_last_fetch_stats(self) -> Dict:
        return dict(self._last_fetch_stats)

    def prime_assignment(self, timeout_sec: Optional[float] = None) -> Dict[str, object]:
        effective_timeout = timeout_sec
        if effective_timeout is None:
            effective_timeout = max(0.0, float(os.getenv("TELEMETRY_STARTUP_ASSIGN_TIMEOUT_SEC", "2.0")))
        assignment_wait_ms, assignment_polls, poll_count, assigned = self._prime_assignment(
            timeout_sec=float(effective_timeout),
            poll_count=0,
            assignment_polls=0,
        )
        return {
            "assigned": bool(assigned),
            "assignment_wait_ms": round(assignment_wait_ms, 2),
            "assignment_polls": int(assignment_polls),
            "poll_count": int(poll_count),
        }

    def get_files(self, limit: int = 50) -> List[Dict]:
        return []

    def has_data(self) -> bool:
        if self._buffer:
            return True
        seek_ts_ms = os.getenv("TELEMETRY_SEEK_TIMESTAMP_MS")
        if seek_ts_ms:
            try:
                self._apply_seek_timestamp(seek_ts_ms)
            except Exception:
                pass
        msg = self._consumer.poll(0.1)
        if msg is None or msg.error():
            return False
        payload = msg.value() or b""
        try:
            try:
                flow = json.loads(payload.decode("utf-8"))
            except Exception:
                flow = _decode_feature_proto(payload)
            if self.feature_mask_enabled:
                flow = _apply_feature_mask(flow, self._feature_names, flow.get("feature_mask", ""))
            if not _meets_coverage(flow, self.min_feature_coverage):
                raise ValueError("feature coverage below threshold")
            if self._is_duplicate(flow):
                return False
            self._buffer.append(flow)
            return True
        except Exception:
            self.logger.debug("telemetry rejected", extra={"error": "invalid json/proto"})
            self._emit_dlq("TELEMETRY_INVALID", "invalid json", payload)
            return False

    def close(self) -> None:
        try:
            self._commit_processed(force=True)
            self._flush_dlq(force=True)
            self._consumer.close()
        except Exception:
            pass

    def _emit_dlq(self, code: str, message: str, payload: bytes) -> None:
        try:
            envelope = {
                "schema": "dlq.v1",
                "timestamp": int(time.time()),
                "source_component": "ai-telemetry",
                "error_code": code,
                "error_message": message,
                "payload_hash": hashlib.sha256(payload).hexdigest(),
            }
            self._producer.produce(self.dlq_topic, json.dumps(envelope).encode("utf-8"))
            self._flush_dlq()
        except Exception:
            self.logger.warning("Failed to emit telemetry DLQ")

    def _flush_dlq(self, force: bool = False) -> None:
        if self.dlq_sync_flush:
            self._producer.flush(3.0)
            return

        self._producer.poll(0)
        now = time.monotonic()
        elapsed_ms = int((now - self._last_dlq_flush_at) * 1000)
        if force or elapsed_ms >= self.dlq_flush_interval_ms:
            self._producer.flush(0.2)
            self._last_dlq_flush_at = now

    def _commit_processed(self, *, message=None, processed: bool = False, force: bool = False) -> None:
        try:
            if self.commit_mode == "sync":
                if processed and message is not None:
                    self._consumer.commit(message=message, asynchronous=False)
                elif force:
                    self._consumer.commit(asynchronous=False)
                return

            if processed:
                self._pending_commits += 1

            if force and self._pending_commits == 0:
                return

            now = time.monotonic()
            elapsed_ms = int((now - self._last_commit_at) * 1000)
            if force or self._pending_commits >= self.commit_batch_size or elapsed_ms >= self.commit_interval_ms:
                self._consumer.commit(asynchronous=True)
                self._pending_commits = 0
                self._last_commit_at = now
        except KafkaException as exc:
            self.logger.warning("Kafka commit failed", extra={"error": str(exc)})

    def _drain_messages(self, first_msg) -> tuple[List, int]:
        batch = [first_msg]
        if self.poll_drain_max_messages <= 1 or self.poll_drain_max_ms <= 0:
            return batch, 0

        deadline = time.monotonic() + (float(self.poll_drain_max_ms) / 1000.0)
        poll_count = 0
        while len(batch) < self.poll_drain_max_messages:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                break
            msg = self._consumer.poll(min(remaining, 0.05))
            poll_count += 1
            if msg is None:
                break
            batch.append(msg)
        return batch, poll_count

    def _resolve_wait_policy(self, wait_policy: Optional[str]) -> Dict[str, object]:
        policy_name = (wait_policy or os.getenv("TELEMETRY_WAIT_POLICY", "bounded_wait")).strip().lower()
        if policy_name == "non_blocking":
            return {
                "policy_name": policy_name,
                "assign_timeout_sec": max(0.0, float(os.getenv("TELEMETRY_NON_BLOCKING_ASSIGN_TIMEOUT_SEC", "0.5"))),
                "max_empty_polls": max(1, int(os.getenv("TELEMETRY_NON_BLOCKING_MAX_EMPTY_POLLS", "1"))),
                "poll_timeout": max(0.05, float(os.getenv("TELEMETRY_NON_BLOCKING_POLL_TIMEOUT_SEC", "0.2"))),
            }
        if policy_name == "streaming_batch":
            return {
                "policy_name": policy_name,
                "assign_timeout_sec": max(0.0, float(os.getenv("TELEMETRY_STREAMING_ASSIGN_TIMEOUT_SEC", "2.0"))),
                "max_empty_polls": max(1, int(os.getenv("TELEMETRY_STREAMING_MAX_EMPTY_POLLS", "3"))),
                "poll_timeout": max(0.05, float(os.getenv("TELEMETRY_STREAMING_POLL_TIMEOUT_SEC", "0.5"))),
            }
        return {
            "policy_name": "bounded_wait",
            "assign_timeout_sec": max(0.0, float(os.getenv("TELEMETRY_ASSIGN_TIMEOUT_SEC", "8.0"))),
            "max_empty_polls": max(1, int(os.getenv("TELEMETRY_MAX_EMPTY_POLLS", "5"))),
            "poll_timeout": self.poll_timeout,
        }

    def _prime_assignment(self, *, timeout_sec: float, poll_count: int, assignment_polls: int) -> tuple[float, int, int, bool]:
        deadline = time.time() + max(0.0, timeout_sec)
        assignment = self._consumer.assignment() or []
        assign_started = time.perf_counter()
        while time.time() < deadline and not assignment:
            try:
                msg = self._consumer.poll(0.2)
                poll_count += 1
                assignment_polls += 1
                if msg is not None and not msg.error():
                    self._prefetched_messages.append(msg)
                assignment = self._consumer.assignment() or []
            except Exception:
                assignment = []
            if not assignment:
                time.sleep(0.05)
        assignment_wait_ms = (time.perf_counter() - assign_started) * 1000
        return assignment_wait_ms, assignment_polls, poll_count, bool(assignment)

    def _build_consumer_config(self, consumer_group: Optional[str]) -> dict:
        consumer_cfg = self.settings.kafka_consumer
        security_cfg = consumer_cfg.security

        group_id = consumer_group or f"{consumer_cfg.group_id}-telemetry"

        config = {
            "bootstrap.servers": consumer_cfg.bootstrap_servers,
            "group.id": group_id,
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

    def _build_producer_config(self) -> dict:
        producer_cfg = self.settings.kafka_producer
        security_cfg = producer_cfg.security

        config = {
            "bootstrap.servers": producer_cfg.bootstrap_servers,
            "client.id": "ai-telemetry-dlq",
            "enable.idempotence": True,
            "acks": "all",
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

    def _is_duplicate(self, flow: Dict) -> bool:
        if not self.dedup_enabled:
            return False
        key = self._flow_dedup_key(flow)
        now = time.time()
        self._evict_seen(now)
        if key in self._seen_flows:
            return True
        self._seen_flows[key] = now + float(self.dedup_ttl_sec)
        if len(self._seen_flows) > self.dedup_max_keys:
            self._seen_flows.popitem(last=False)
        return False

    def _flow_dedup_key(self, flow: Dict) -> str:
        flow_id = str(flow.get("flow_id", "") or "")
        ts = int(flow.get("ts", 0) or 0)
        source_type = str(flow.get("source_type", "") or "")
        source_id = str(flow.get("source_id", "") or "")
        return f"{flow_id}:{ts}:{source_type}:{source_id}"

    def _evict_seen(self, now: Optional[float] = None) -> None:
        if now is None:
            now = time.time()
        expired = []
        for key, expiry in self._seen_flows.items():
            if expiry < now:
                expired.append(key)
            else:
                break
        for key in expired:
            self._seen_flows.pop(key, None)

    def _apply_seek_timestamp(self, ts_ms: str) -> None:
        if getattr(self, "_seek_ts_applied", False):
            return
        try:
            target = int(ts_ms)
        except Exception:
            return
        meta = self._consumer.list_topics(self.topic, timeout=5.0)
        topic_meta = meta.topics.get(self.topic)
        if not topic_meta or not topic_meta.partitions:
            return
        partitions = [TopicPartition(self.topic, p, target) for p in topic_meta.partitions.keys()]
        offsets = self._consumer.offsets_for_times(partitions, timeout=5.0)
        offsets = [tp for tp in offsets if tp and tp.offset is not None and tp.offset >= 0]
        if not offsets:
            return
        self._consumer.assign(offsets)
        self._seek_ts_applied = True
