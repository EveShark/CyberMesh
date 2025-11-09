"""
Kafka consumer for receiving backend control messages.

Uses confluent-kafka (production-grade, wraps librdkafka).

Topics:
- control.commits.v1
- control.reputation.v1
- control.policy.v1
- control.evidence.v1
"""
import os
from confluent_kafka import Consumer, Producer, KafkaException, KafkaError as ConfluentKafkaError
import threading
import time
from typing import Callable, Dict, Optional, List
from ..contracts import (
    CommitEvent,
    ReputationEvent,
    PolicyUpdateEvent,
    EvidenceRequestEvent,
)
from ..utils.errors import ContractError, KafkaError, ValidationError, StorageError, TimestampSkewError
from ..utils.metrics import get_metrics_collector
import hashlib
import json
from ..config.settings import Settings, FeedbackConfig
from ..feedback.storage import RedisStorage
from ..feedback.tracker import AnomalyLifecycleTracker, AnomalyState


class AIConsumer:
    """
    Kafka consumer for Backend → AI messages.
    
    Uses confluent-kafka for:
    - High performance (librdkafka in C)
    - Better consumer group management
    - Automatic partition rebalancing
    
    Security:
    - TLS encryption
    - Ed25519 signature verification on all messages
    - Manual offset management for reliability
    """
    
    def __init__(
        self,
        config: Settings,
        tracker: Optional[AnomalyLifecycleTracker] = None,
        logger=None,
        *,
        storage: Optional[RedisStorage] = None,
        feedback_config: Optional[FeedbackConfig] = None,
        disable_persistence: Optional[bool] = None,
        topics: Optional[List[str]] = None,
    ):
        self.config = config
        self.logger = logger
        
        # Initialize lifecycle tracker
        feedback_cfg = feedback_config or getattr(config, "feedback", None)

        effective_disable = disable_persistence
        if feedback_cfg is None:
            if effective_disable is None:
                env_flag = os.getenv("FEEDBACK_DISABLE_PERSISTENCE")
                if env_flag is None:
                    effective_disable = False
                else:
                    effective_disable = env_flag.lower() in ("true", "1", "yes", "on")
            feedback_cfg = FeedbackConfig(disable_persistence=bool(effective_disable))
        else:
            if effective_disable is None:
                effective_disable = bool(getattr(feedback_cfg, "disable_persistence", False))

        if tracker is None:
            storage = storage or RedisStorage(disabled=effective_disable)
            tracker = AnomalyLifecycleTracker(storage, feedback_cfg, logger)
        else:
            if storage is None:
                storage = getattr(tracker, "storage", None)
                if storage is None:
                    storage = RedisStorage(disabled=effective_disable)
                    tracker.storage = storage
            else:
                tracker.storage = storage

        self.storage = storage
        self.tracker = tracker
        
        # Topics from settings
        default_topics = [
            config.kafka_topics.control_commits,
            config.kafka_topics.control_reputation,
            config.kafka_topics.control_policy,
            config.kafka_topics.control_evidence,
        ]
        self.topic_list = topics if topics is not None else default_topics
        
        # Build confluent-kafka config
        kafka_config = self._build_config(config)
        
        self.consumer = Consumer(kafka_config)
        self.consumer.subscribe(self.topic_list)
        
        # Manual commit tracking
        self._auto_commit_enabled = config.kafka_consumer.enable_auto_commit

        # DLQ producer (initialized lazily on first use)
        self._dlq_producer: Optional[Producer] = None
        self._metrics = get_metrics_collector()
        
        # Message handlers (can be overridden)
        self.handlers: Dict[str, Callable] = {
            "evidence_request": self._handle_evidence_request,
            "commit": self._handle_commit,
        }
        
        # Consumer thread
        self._running = False
        self._thread = None
        
        # Metrics
        self._messages_received = 0
        self._messages_processed = 0
        self._messages_failed = 0
        self._evidence_accepted = 0
        self._evidence_rejected = 0
        self._commits_processed = 0
        self._tracker_updates = 0
        self._tracker_errors = 0
    
    def _build_config(self, settings: Settings) -> dict:
        """Build confluent-kafka Consumer configuration."""
        consumer_cfg = settings.kafka_consumer
        security_cfg = consumer_cfg.security
        
        config = {
            # Connection
            'bootstrap.servers': consumer_cfg.bootstrap_servers,
            'group.id': consumer_cfg.group_id,
            
            # Consumer behavior
            'auto.offset.reset': consumer_cfg.auto_offset_reset,
            'enable.auto.commit': consumer_cfg.enable_auto_commit,
            'max.poll.interval.ms': consumer_cfg.max_poll_interval_ms,
            'session.timeout.ms': consumer_cfg.session_timeout_ms,
            'heartbeat.interval.ms': consumer_cfg.heartbeat_interval_ms,
            
            # Performance
            'fetch.min.bytes': consumer_cfg.fetch_min_bytes,
            # Note: confluent-kafka uses different property names (librdkafka)
            'fetch.wait.max.ms': consumer_cfg.fetch_max_wait_ms,
            
            # Reliability
            'enable.partition.eof': False,
            'isolation.level': 'read_committed',  # Only read committed messages (transactional)
        }
        
        # Security (TLS + SASL)
        if security_cfg.sasl_mechanism == "NONE":
            config['security.protocol'] = 'SSL' if security_cfg.tls_enabled else 'PLAINTEXT'
        else:
            config['security.protocol'] = 'SASL_SSL' if security_cfg.tls_enabled else 'SASL_PLAINTEXT'
            config['sasl.mechanism'] = security_cfg.sasl_mechanism
            config['sasl.username'] = security_cfg.sasl_username
            config['sasl.password'] = security_cfg.sasl_password
        
        if security_cfg.tls_enabled and security_cfg.ca_cert_path:
            config['ssl.ca.location'] = security_cfg.ca_cert_path
        if security_cfg.client_cert_path:
            config['ssl.certificate.location'] = security_cfg.client_cert_path
        if security_cfg.client_key_path:
            config['ssl.key.location'] = security_cfg.client_key_path
        
        return config
    
    def register_handler(self, message_type: str, handler: Callable):
        """
        Register handler for message type.
        
        Args:
            message_type: "commit", "reputation", "policy_update", "evidence_request"
            handler: Callable(message) -> None
        """
        self.handlers[message_type] = handler
    
    def start(self):
        """Start consumer in background thread"""
        if self._running:
            return
        
        self._running = True
        self._thread = threading.Thread(target=self._consume_loop, daemon=True)
        self._thread.start()
        
        if self.logger:
            self.logger.info("Kafka consumer started")
    
    def stop(self):
        """Stop consumer gracefully"""
        self._running = False
        
        if self._thread:
            self._thread.join(timeout=10)
        
        try:
            self.consumer.close()
        except Exception as e:
            if self.logger:
                self.logger.error(f"Error closing consumer: {e}")
        
        if self.logger:
            self.logger.info("Kafka consumer stopped")
    
    def _consume_loop(self):
        """Main consumer loop"""
        while self._running:
            try:
                # Poll for messages (timeout in seconds)
                msg = self.consumer.poll(timeout=1.0)
                
                if msg is None:
                    continue
                
                if msg.error():
                    if msg.error().code() == ConfluentKafkaError._PARTITION_EOF:
                        # End of partition, not an error
                        continue
                    else:
                        raise KafkaException(msg.error())
                
                # Process message
                self._messages_received += 1
                self._process_message(msg.topic(), msg.value())
                
                # Manual commit if auto-commit disabled
                if not self._auto_commit_enabled:
                    self.consumer.commit(message=msg, asynchronous=False)
                
            except KafkaException as e:
                if self.logger:
                    self.logger.error(f"Kafka consumer error: {e}")
                time.sleep(1)
                
            except Exception as e:
                if self.logger:
                    self.logger.error(f"Consumer loop error: {e}", exc_info=True)
                time.sleep(1)
    
    def _process_message(self, topic: str, data: bytes):
        """Process single message with signature verification"""
        try:
            # Parse and verify message based on topic
            if topic == self.config.kafka_topics.control_commits:
                # CommitEvent auto-verifies signature in __init__ (no param needed)
                msg = CommitEvent.from_bytes(data)
                handler = self.handlers.get("commit")
                    
            elif topic == self.config.kafka_topics.control_reputation:
                msg = ReputationEvent.from_bytes(data, verify_signature=True)
                handler = self.handlers.get("reputation")
                    
            elif topic == self.config.kafka_topics.control_policy:
                # PolicyUpdateEvent auto-verifies signature in __init__ (no param needed)
                msg = PolicyUpdateEvent.from_bytes(data)
                handler = self.handlers.get("policy_update")
                    
            elif topic == self.config.kafka_topics.control_evidence:
                msg = EvidenceRequestEvent.from_bytes(data, verify_signature=True)
                handler = self.handlers.get("evidence_request")
            else:
                raise ContractError(f"Unknown topic: {topic}")
            
            # Execute handler if registered
            if handler:
                handler(msg)
            else:
                if self.logger:
                    self.logger.warning(f"No handler registered for topic: {topic}")
            
            self._messages_processed += 1
            
        except ContractError as e:
            # Identify timestamp skew via structured error type in the cause chain
            cause = getattr(e, "__cause__", None)
            is_skew = isinstance(cause, TimestampSkewError)
            if is_skew:
                reason = "timestamp_skew"
                if self.logger:
                    self.logger.warning(
                        f"Dropping message due to timestamp skew from {topic}: {cause}"
                    )
                # In production, emit to DLQ with structured payload and record metrics
                try:
                    if getattr(self.config, "environment", "development") == "production":
                        self._send_to_dlq(topic, data, reason, str(cause))
                    self._metrics.record_dlq_message(topic=topic, reason=reason)
                except Exception:
                    # Metrics/DLQ issues must not crash consumer loop
                    if self.logger:
                        self.logger.error("Failed to emit DLQ/metrics for timestamp skew", exc_info=True)
                # Treat as processed (skip) by returning without raising; caller will commit offset.
                return
            # Other validation errors are counted as failures and re-raised
            self._messages_failed += 1
            if self.logger:
                self.logger.error(f"Message validation failed from {topic}: {e}")
            # Don't commit - will be reprocessed
            raise
            
        except Exception as e:
            self._messages_failed += 1
            if self.logger:
                self.logger.error(f"Message processing failed from {topic}: {e}", exc_info=True)
            # Don't commit - will be reprocessed
            raise
    
    def _handle_evidence_request(self, msg: EvidenceRequestEvent):
        """
        Handle evidence request/validation from backend.
        
        Updates anomaly lifecycle:
        - evidence_accepted=true → ADMITTED (validators accepted)
        - evidence_accepted=false → REJECTED (validators rejected)
        """
        try:
            anomaly_id = msg.anomaly_id
            
            if msg.evidence_accepted:
                # Anomaly admitted to mempool
                self.tracker.record_admitted(
                    anomaly_id=anomaly_id,
                    validator_id=msg.producer_id.hex() if msg.producer_id else "unknown",
                    timestamp=float(msg.timestamp)
                )
                self._evidence_accepted += 1
                
                if self.logger:
                    self.logger.info(
                        "Anomaly admitted",
                        extra={
                            "anomaly_id": anomaly_id,
                            "evidence_id": msg.evidence_id,
                            "quality_score": msg.evidence_quality_score,
                            "block_height": msg.block_height
                        }
                    )
            else:
                # Anomaly rejected by validators
                signature = msg.signature.hex() if msg.signature else ""
                self.tracker.record_rejected(
                    anomaly_id=anomaly_id,
                    signature=signature,
                    timestamp=float(msg.timestamp)
                )
                self._evidence_rejected += 1
                
                if self.logger:
                    self.logger.warning(
                        "Anomaly rejected",
                        extra={
                            "anomaly_id": anomaly_id,
                            "evidence_id": msg.evidence_id,
                            "rejection_reason": msg.rejection_reason,
                            "block_height": msg.block_height
                        }
                    )
            
            self._tracker_updates += 1
            
        except ValidationError as e:
            self._tracker_errors += 1
            if self.logger:
                self.logger.error(
                    f"Tracker validation error for anomaly {msg.anomaly_id}: {e}"
                )
            raise
            
        except StorageError as e:
            self._tracker_errors += 1
            if self.logger:
                self.logger.error(
                    f"Tracker storage error for anomaly {msg.anomaly_id}: {e}"
                )
            raise
    
    def _handle_commit(self, msg: CommitEvent):
        """
        Handle block commit from backend.
        
        Updates metrics and marks anomalies as committed.
        Fix: Now processes individual anomaly_ids to enable COMMITTED state.
        """
        try:
            self._commits_processed += 1
            
            # Fix: Gap 2 - Track individual anomalies to COMMITTED state
            committed_count = 0
            if hasattr(msg, 'anomaly_ids') and msg.anomaly_ids:
                for anomaly_id in msg.anomaly_ids:
                    try:
                        self.tracker.record_committed(
                            anomaly_id=anomaly_id,
                            block_height=int(msg.height),
                            signature=msg.signature.hex() if msg.signature else "",
                            timestamp=float(msg.timestamp)
                        )
                        committed_count += 1
                    except Exception as e:
                        # Log but don't fail on individual anomaly errors
                        if self.logger:
                            self.logger.warning(
                                f"Failed to record committed state for anomaly {anomaly_id}: {e}"
                            )
                        self._tracker_errors += 1
                
                if self.logger:
                    self.logger.info(
                        f"Marked {committed_count}/{len(msg.anomaly_ids)} anomalies as COMMITTED"
                    )
            
            if self.logger:
                self.logger.info(
                    "Block committed",
                    extra={
                        "height": msg.height,
                        "block_hash": msg.block_hash.hex()[:16],
                        "tx_count": msg.tx_count,
                        "anomaly_count": msg.anomaly_count,
                        "anomalies_tracked": committed_count,
                        "timestamp": msg.timestamp
                    }
                )
            
        except Exception as e:
            if self.logger:
                self.logger.error(f"Error handling commit event: {e}")
            raise
    
    def get_metrics(self) -> dict:
        """Get consumer metrics"""
        return {
            "messages_received": self._messages_received,
            "messages_processed": self._messages_processed,
            "messages_failed": self._messages_failed,
            "evidence_accepted": self._evidence_accepted,
            "evidence_rejected": self._evidence_rejected,
            "commits_processed": self._commits_processed,
            "tracker_updates": self._tracker_updates,
            "tracker_errors": self._tracker_errors,
        }

    def _ensure_dlq_producer(self):
        if self._dlq_producer is not None:
            return
        # Build minimal producer config from settings.kafka_producer
        producer_cfg = self.config.kafka_producer
        security_cfg = producer_cfg.security
        cfg = {
            'bootstrap.servers': producer_cfg.bootstrap_servers,
        }
        if security_cfg.sasl_mechanism == "NONE":
            cfg['security.protocol'] = 'SSL' if security_cfg.tls_enabled else 'PLAINTEXT'
        else:
            cfg['security.protocol'] = 'SASL_SSL' if security_cfg.tls_enabled else 'SASL_PLAINTEXT'
            cfg['sasl.mechanism'] = security_cfg.sasl_mechanism
            cfg['sasl.username'] = security_cfg.sasl_username
            cfg['sasl.password'] = security_cfg.sasl_password
        if security_cfg.tls_enabled and security_cfg.ca_cert_path:
            cfg['ssl.ca.location'] = security_cfg.ca_cert_path
        if security_cfg.client_cert_path:
            cfg['ssl.certificate.location'] = security_cfg.client_cert_path
        if security_cfg.client_key_path:
            cfg['ssl.key.location'] = security_cfg.client_key_path
        self._dlq_producer = Producer(cfg)

    def _send_to_dlq(self, original_topic: str, data: bytes, reason: str, error: str):
        self._ensure_dlq_producer()
        dlq_topic = self.config.kafka_topics.dlq
        payload = {
            "original_topic": original_topic,
            "reason": reason,
            "error": error,
            "timestamp": int(time.time()),
            "node_id": getattr(self.config, "node_id", "unknown"),
            "message_hash": hashlib.sha256(data or b'').hexdigest(),
            "data_size": len(data or b''),
        }
        encoded = json.dumps(payload, separators=(",", ":")).encode()
        key = hashlib.sha256((original_topic + payload["message_hash"]).encode()).digest()
        try:
            self._dlq_producer.produce(dlq_topic, value=encoded, key=key)
            self._dlq_producer.flush(5)
        except Exception as ex:
            if self.logger:
                self.logger.error(f"Failed to send DLQ message: {ex}")
