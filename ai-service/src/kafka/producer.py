"""
Kafka producer for sending AI messages to backend.

Uses confluent-kafka (production-grade, wraps librdkafka).

Topics:
- ai.anomalies.v1
- ai.evidence.v1
- ai.policy.v1
- control.fast_mitigation.v1
"""
from confluent_kafka import Producer, KafkaException
import time
import hashlib
import threading
import os
from typing import Optional
from ..contracts import AnomalyMessage, EvidenceMessage, FastMitigationMessage, PolicyMessage
from ..utils.errors import KafkaError
from ..utils.circuit_breaker import CircuitBreaker
from ..utils.backoff import ExponentialBackoff
from ..utils.metrics import get_metrics_collector
from ..config.settings import Settings


class AIProducer:
    """
    Kafka producer for AI → Backend messages.
    
    Uses confluent-kafka for:
    - High performance (librdkafka in C)
    - Exactly-once semantics (enable.idempotence)
    - Better reliability
    
    Security:
    - TLS encryption
    - Messages pre-signed with Ed25519
    - Circuit breaker protection
    - Exponential backoff retry
    """
    
    def __init__(self, config: Settings, circuit_breaker: CircuitBreaker):
        self.config = config
        self.circuit_breaker = circuit_breaker
        self._backoff_base_delay = 1.0
        self._backoff_max_delay = 60.0
        self._backoff_max_attempts = 5
        
        # Topics from settings
        self.topics = config.kafka_topics
        
        # Convert to confluent-kafka config format
        kafka_config = self._build_config(config)
        
        self.producer = Producer(kafka_config)
        self.sync_flush = os.getenv("AI_PRODUCER_SYNC_FLUSH", "false").lower() in ("1", "true", "yes", "on")
        self.policy_sync_flush = os.getenv("AI_PRODUCER_POLICY_SYNC_FLUSH", "true").lower() in ("1", "true", "yes", "on")
        self.flush_interval_ms = max(1, int(os.getenv("AI_PRODUCER_FLUSH_INTERVAL_MS", "50")))
        self._last_flush_at = time.monotonic()
        self._metrics_collector = get_metrics_collector()
        
        # Metrics
        self._messages_sent = 0
        self._messages_failed = 0
        self._bytes_sent = 0
        self._lock = threading.Lock()
        
        # Track in-flight messages for error handling
        self._delivery_errors = []

    def _new_backoff(self) -> ExponentialBackoff:
        return ExponentialBackoff(
            base_delay=self._backoff_base_delay,
            max_delay=self._backoff_max_delay,
            max_attempts=self._backoff_max_attempts,
        )
    
    def _build_config(self, settings: Settings) -> dict:
        """Build confluent-kafka Producer configuration."""
        producer_cfg = settings.kafka_producer
        security_cfg = producer_cfg.security
        
        config = {
            # Connection
            'bootstrap.servers': producer_cfg.bootstrap_servers,
            
            # Reliability (CRITICAL for production)
            'acks': producer_cfg.acks,
            'retries': producer_cfg.retries,
            'enable.idempotence': True,  # Exactly-once semantics
            'max.in.flight.requests.per.connection': producer_cfg.max_in_flight_requests,
            
            # Performance
            'compression.type': producer_cfg.compression_type,
            'batch.size': producer_cfg.batch_size,
            'linger.ms': producer_cfg.linger_ms,
            # Note: confluent-kafka uses different property names (librdkafka)
            'queue.buffering.max.kbytes': producer_cfg.buffer_memory // 1024,  # bytes to kbytes
            'request.timeout.ms': producer_cfg.request_timeout_ms,
            
            # Delivery guarantees
            'delivery.timeout.ms': 120000,  # 2 minutes max
            'queue.buffering.max.messages': 100000,
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
    
    def _delivery_callback(self, err, msg):
        """Callback for async delivery reports."""
        if err:
            with self._lock:
                self._messages_failed += 1
                self._delivery_errors.append(str(err))
        else:
            with self._lock:
                self._messages_sent += 1
                self._bytes_sent += len(msg.value() or b'')

    def _maybe_flush(self, force: bool = False, timeout: float = 0.2):
        if self.sync_flush:
            self.producer.flush(timeout=timeout if force else 10)
            return

        self.producer.poll(0)
        now = time.monotonic()
        elapsed_ms = int((now - self._last_flush_at) * 1000)
        if force or elapsed_ms >= self.flush_interval_ms:
            self.producer.flush(timeout=timeout)
            self._last_flush_at = now
    
    def send_anomaly(self, msg: AnomalyMessage) -> bool:
        """Send anomaly message to ai.anomalies.v1"""
        return self._send_message(msg, "anomaly")
    
    def send_evidence(self, msg: EvidenceMessage) -> bool:
        """Send evidence message to ai.evidence.v1"""
        return self._send_message(msg, "evidence")

    def send_policy(self, msg: PolicyMessage) -> bool:
        """Send policy message to ai.policy.v1"""
        return self._send_message(msg, "policy", force_flush=self.policy_sync_flush)

    def send_fast_mitigation(self, msg: FastMitigationMessage) -> bool:
        """Send fast mitigation message to control.fast_mitigation.v1."""
        return self._send_message(
            msg,
            "fast_mitigation",
            force_flush=False,
            max_attempts=1,
            retry_enabled=False,
            send_to_dlq=False,
            raise_on_failure=False,
        )

    def send_pcap_request(self, payload: bytes, *, key: Optional[str] = None) -> bool:
        """Send raw protobuf PCAP request to pcap.request.v1."""
        return self._send_bytes(
            self.topics.pcap_request,
            payload,
            key=key,
            force_flush=True,
        )

    def _send_bytes(
        self,
        topic: str,
        data: bytes,
        *,
        key: Optional[str] = None,
        force_flush: bool = False,
        metric_type: Optional[str] = None,
        max_attempts: Optional[int] = None,
        retry_enabled: bool = True,
        send_to_dlq: bool = True,
        raise_on_failure: bool = True,
    ) -> bool:
        last_error = None
        start = time.monotonic()
        backoff = self._new_backoff()
        attempts = 0
        configured_max_attempts = max(1, int(max_attempts)) if max_attempts is not None else None
        while True:
            attempts += 1
            attempt_start = time.monotonic()
            try:
                def _send():
                    self.producer.produce(
                        topic,
                        value=data,
                        key=key,
                        callback=self._delivery_callback
                    )
                    self.producer.poll(0)
                    self._maybe_flush(force=force_flush)

                self.circuit_breaker.call(_send)

                with self._lock:
                    if self._delivery_errors:
                        error = self._delivery_errors.pop(0)
                        raise KafkaError(f"Delivery failed: {error}")
                self._metrics_collector.record_kafka_publish_attempt_latency(
                    topic,
                    time.monotonic() - attempt_start,
                    "success",
                )
                if metric_type:
                    self._metrics_collector.record_message_published(metric_type, "success", len(data))
                self._metrics_collector.record_kafka_publish_latency(topic, time.monotonic() - start)
                return True

            except Exception as e:
                last_error = e
                self._metrics_collector.record_kafka_publish_attempt_latency(
                    topic,
                    time.monotonic() - attempt_start,
                    "error",
                )
                if retry_enabled:
                    self._metrics_collector.record_kafka_retry(topic, type(e).__name__)

                if configured_max_attempts is not None and attempts >= configured_max_attempts:
                    break

                if not retry_enabled:
                    break

                try:
                    delay = backoff.next_delay()
                    time.sleep(delay)
                except Exception:
                    break

        with self._lock:
            self._messages_failed += 1
        if metric_type:
            self._metrics_collector.record_message_published(metric_type, "failed", len(data))
        self._metrics_collector.record_kafka_publish_latency(topic, time.monotonic() - start)
        if send_to_dlq:
            self._send_to_dlq(data, topic, str(last_error))
        if raise_on_failure:
            raise KafkaError(f"Failed to send after max retries: {last_error}")
        return False
    
    def _send_message(
        self,
        msg,
        msg_type: str,
        force_flush: bool = False,
        max_attempts: Optional[int] = None,
        retry_enabled: bool = True,
        send_to_dlq: bool = True,
        raise_on_failure: bool = True,
    ) -> bool:
        """Send message with circuit breaker and retry"""
        # Get topic from settings
        topic_map = {
            "anomaly": self.topics.ai_anomalies,
            "evidence": self.topics.ai_evidence,
            "policy": self.topics.ai_policy,
            "fast_mitigation": self.topics.control_fast_mitigation,
        }
        topic = topic_map[msg_type]
        data = msg.to_bytes()
        return self._send_bytes(
            topic,
            data,
            force_flush=force_flush,
            metric_type=msg_type,
            max_attempts=max_attempts,
            retry_enabled=retry_enabled,
            send_to_dlq=send_to_dlq,
            raise_on_failure=raise_on_failure,
        )
    
    def flush(self, timeout: float = 30.0):
        """
        Flush buffered messages.
        
        Args:
            timeout: Max seconds to wait (default: 30)
        """
        remaining = self.producer.flush(timeout=timeout)
        if remaining > 0:
            raise KafkaError(f"{remaining} messages not delivered within timeout")
    
    def _send_to_dlq(self, data: bytes, msg_type: str, error: str):
        """Send failed message to dead letter queue"""
        try:
            dlq_topic = self.topics.dlq
            import json
            dlq_data = json.dumps({
                "original_topic": msg_type,
                "error": error,
                "timestamp": int(time.time()),
                "data_size": len(data),
            }).encode()
            
            key = hashlib.sha256(data).digest()
            
            self.producer.produce(
                dlq_topic,
                value=dlq_data,
                key=key,
                callback=lambda err, msg: None  # Ignore DLQ errors
            )
            self.producer.flush(timeout=5)
            self._metrics_collector.record_dlq_message(dlq_topic, reason=msg_type)
            
        except Exception as e:
            # DLQ send failed, just log
            print(f"Failed to send to DLQ: {e}")
    
    def close(self):
        """Close producer and flush pending messages"""
        try:
            self._maybe_flush(force=True, timeout=30)
        finally:
            # Producer doesn't have explicit close in confluent-kafka
            pass
    
    def get_metrics(self) -> dict:
        """Get producer metrics"""
        with self._lock:
            return {
                "messages_sent": self._messages_sent,
                "messages_failed": self._messages_failed,
                "bytes_sent": self._bytes_sent,
            }
