"""
Kafka consumer for receiving backend control messages.

Uses confluent-kafka (production-grade, wraps librdkafka).

Topics:
- control.commits.v1
- control.reputation.v1
- control.policy.v1
- control.evidence.v1
"""
from confluent_kafka import Consumer, KafkaException, KafkaError as ConfluentKafkaError
import threading
import time
from typing import Callable, Dict, Optional
from ..contracts import (
    CommitEvent,
    ReputationEvent,
    PolicyUpdateEvent,
    EvidenceRequestEvent,
)
from ..utils.errors import ContractError, KafkaError, ValidationError, StorageError
from ..config.settings import Settings
from ..feedback.storage import RedisStorage
from ..feedback.tracker import AnomalyLifecycleTracker, AnomalyState
from ..config.settings import FeedbackConfig


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
        logger=None
    ):
        self.config = config
        self.logger = logger
        
        # Initialize lifecycle tracker
        if tracker is None:
            storage = RedisStorage()
            feedback_config = FeedbackConfig()
            tracker = AnomalyLifecycleTracker(storage, feedback_config, logger)
        self.tracker = tracker
        
        # Topics from settings
        self.topic_list = [
            config.kafka_topics.control_commits,
            config.kafka_topics.control_reputation,
            config.kafka_topics.control_policy,
            config.kafka_topics.control_evidence,
        ]
        
        # Build confluent-kafka config
        kafka_config = self._build_config(config)
        
        self.consumer = Consumer(kafka_config)
        self.consumer.subscribe(self.topic_list)
        
        # Manual commit tracking
        self._auto_commit_enabled = config.kafka_consumer.enable_auto_commit
        
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
                msg = CommitEvent.from_bytes(data, verify_signature=True)
                handler = self.handlers.get("commit")
                    
            elif topic == self.config.kafka_topics.control_reputation:
                msg = ReputationEvent.from_bytes(data, verify_signature=True)
                handler = self.handlers.get("reputation")
                    
            elif topic == self.config.kafka_topics.control_policy:
                msg = PolicyUpdateEvent.from_bytes(data, verify_signature=True)
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
        Note: We don't have individual anomaly IDs in CommitEvent,
        so we rely on evidence_accepted messages for individual tracking.
        """
        try:
            self._commits_processed += 1
            
            if self.logger:
                self.logger.info(
                    "Block committed",
                    extra={
                        "height": msg.height,
                        "block_hash": msg.block_hash.hex()[:16],
                        "tx_count": msg.tx_count,
                        "anomaly_count": msg.anomaly_count,
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
