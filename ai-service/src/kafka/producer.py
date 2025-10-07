"""
Kafka producer for sending AI messages to backend.

Uses confluent-kafka (production-grade, wraps librdkafka).

Topics:
- ai.anomalies.v1
- ai.evidence.v1
- ai.policy.v1
"""
from confluent_kafka import Producer, KafkaException
import time
import hashlib
import threading
from typing import Optional
from ..contracts import AnomalyMessage, EvidenceMessage, PolicyMessage
from ..utils.errors import KafkaError
from ..utils.circuit_breaker import CircuitBreaker
from ..utils.backoff import ExponentialBackoff
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
        self.backoff = ExponentialBackoff(
            base_delay=1.0,
            max_delay=60.0,
            max_attempts=5,
        )
        
        # Topics from settings
        self.topics = config.kafka_topics
        
        # Convert to confluent-kafka config format
        kafka_config = self._build_config(config)
        
        self.producer = Producer(kafka_config)
        
        # Metrics
        self._messages_sent = 0
        self._messages_failed = 0
        self._bytes_sent = 0
        self._lock = threading.Lock()
        
        # Track in-flight messages for error handling
        self._delivery_errors = []
    
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
    
    def send_anomaly(self, msg: AnomalyMessage) -> bool:
        """Send anomaly message to ai.anomalies.v1"""
        return self._send_message(msg, "anomaly")
    
    def send_evidence(self, msg: EvidenceMessage) -> bool:
        """Send evidence message to ai.evidence.v1"""
        return self._send_message(msg, "evidence")

    def send_policy(self, msg: PolicyMessage) -> bool:
        """Send policy message to ai.policy.v1"""
        return self._send_message(msg, "policy")
    
    def _send_message(self, msg, msg_type: str) -> bool:
        """Send message with circuit breaker and retry"""
        # Get topic from settings
        topic_map = {
            "anomaly": self.topics.ai_anomalies,
            "evidence": self.topics.ai_evidence,
            "policy": self.topics.ai_policy,
        }
        topic = topic_map[msg_type]
        data = msg.to_bytes()
        
        last_error = None
        
        while True:
            try:
                # CircuitBreaker.call() wraps the operation
                def _send():
                    self.producer.produce(
                        topic,
                        value=data,
                        callback=self._delivery_callback
                    )
                    # Process delivery callbacks
                    self.producer.poll(0)
                
                self.circuit_breaker.call(_send)
                
                # Flush to wait for delivery confirmation
                self.producer.flush(timeout=10)
                
                # Check for delivery errors
                with self._lock:
                    if self._delivery_errors:
                        error = self._delivery_errors.pop(0)
                        raise KafkaError(f"Delivery failed: {error}")
                
                return True
                
            except Exception as e:
                last_error = e
                
                try:
                    delay = self.backoff.next_delay()
                    time.sleep(delay)
                except Exception:
                    # Max attempts reached
                    break
        
        # Permanent failure - send to DLQ
        with self._lock:
            self._messages_failed += 1
        self._send_to_dlq(data, msg_type, str(last_error))
        raise KafkaError(f"Failed to send after max retries: {last_error}")
    
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
            
        except Exception as e:
            # DLQ send failed, just log
            print(f"Failed to send to DLQ: {e}")
    
    def close(self):
        """Close producer and flush pending messages"""
        try:
            self.producer.flush(timeout=30)
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
