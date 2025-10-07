# Phase P0.3 - Kafka Integration Layer

**Status:** Ready to Start
**Prerequisites:** ✅ P0.1 (Config/Utils) Complete, ✅ P0.2 (Contracts) Complete
**Duration:** 1-2 days
**Goal:** Wire contracts to Kafka for bidirectional communication with backend

---

## 🎯 OBJECTIVES

1. **Producer:** Send signed messages (anomaly/evidence/policy) to backend via Kafka
2. **Consumer:** Receive control messages (commits/reputation/policy updates) from backend
3. **Error Handling:** DLQ, retries, circuit breakers
4. **Monitoring:** Metrics for message flow
5. **Integration:** Connect with config layer for settings

---

## 📋 TASK BREAKDOWN

### Task 1: Kafka Producer (AI → Backend)

**File:** `src/kafka/producer.py`

**Responsibilities:**
- Send `AnomalyMessage` → `ai.anomalies.v1`
- Send `EvidenceMessage` → `ai.evidence.v1`
- Send `PolicyMessage` → `ai.policy.v1`
- Handle connection failures, retries
- Metrics: messages sent, failures, latency

**Implementation:**

```python
"""
Kafka producer for sending AI messages to backend.
"""
from kafka import KafkaProducer
from kafka.errors import KafkaError
import time
from typing import Optional
from ..contracts import AnomalyMessage, EvidenceMessage, PolicyMessage
from ..utils.errors import KafkaError as CyberMeshKafkaError
from ..utils.circuit_breaker import CircuitBreaker
from ..utils.backoff import ExponentialBackoff
from ..config import Config


class AIProducer:
    """
    Kafka producer for AI → Backend messages.
    
    Sends signed protobuf messages to:
    - ai.anomalies.v1
    - ai.evidence.v1
    - ai.policy.v1
    """
    
    TOPICS = {
        "anomaly": "ai.anomalies.v1",
        "evidence": "ai.evidence.v1",
        "policy": "ai.policy.v1",
    }
    
    def __init__(self, config: Config, circuit_breaker: CircuitBreaker):
        self.config = config
        self.circuit_breaker = circuit_breaker
        self.backoff = ExponentialBackoff(
            base_delay=1.0,
            max_delay=60.0,
            max_retries=5,
        )
        
        # Kafka configuration
        kafka_config = {
            'bootstrap_servers': config.get('KAFKA_BROKERS', 'localhost:9092').split(','),
            'acks': 'all',  # Wait for all replicas
            'retries': 3,
            'max_in_flight_requests_per_connection': 1,  # Ordering guarantee
            'compression_type': 'snappy',
            'value_serializer': lambda v: v,  # Already serialized to bytes
        }
        
        # TLS configuration
        if config.get_bool('KAFKA_TLS_ENABLED', True):
            kafka_config.update({
                'security_protocol': 'SSL',
                'ssl_cafile': config.get('KAFKA_CA_CERT'),
                'ssl_certfile': config.get('KAFKA_CLIENT_CERT'),
                'ssl_keyfile': config.get('KAFKA_CLIENT_KEY'),
            })
        
        self.producer = KafkaProducer(**kafka_config)
        
        # Metrics
        self._messages_sent = 0
        self._messages_failed = 0
        self._bytes_sent = 0
    
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
        """Internal: Send message with circuit breaker and retry"""
        topic = self.TOPICS[msg_type]
        
        # Check circuit breaker
        if not self.circuit_breaker.allow():
            raise CyberMeshKafkaError("Circuit breaker open, message rejected")
        
        # Serialize to protobuf bytes
        data = msg.to_bytes()
        
        # Send with retry
        retry_count = 0
        while retry_count < self.backoff.max_retries:
            try:
                future = self.producer.send(topic, value=data)
                record_metadata = future.get(timeout=10)
                
                # Success
                self.circuit_breaker.success()
                self._messages_sent += 1
                self._bytes_sent += len(data)
                
                return True
                
            except KafkaError as e:
                retry_count += 1
                self.circuit_breaker.failure()
                
                if retry_count >= self.backoff.max_retries:
                    self._messages_failed += 1
                    raise CyberMeshKafkaError(f"Failed to send after {retry_count} retries: {e}")
                
                # Exponential backoff
                delay = self.backoff.get_delay(retry_count)
                time.sleep(delay)
        
        return False
    
    def flush(self):
        """Flush any buffered messages"""
        self.producer.flush()
    
    def close(self):
        """Close producer connection"""
        self.producer.close()
    
    def get_metrics(self) -> dict:
        """Get producer metrics"""
        return {
            "messages_sent": self._messages_sent,
            "messages_failed": self._messages_failed,
            "bytes_sent": self._bytes_sent,
        }
```

**Metrics to track:**
- `kafka_messages_sent_total{topic, type}` - Counter
- `kafka_messages_failed_total{topic, type}` - Counter
- `kafka_send_latency_seconds{topic}` - Histogram
- `kafka_bytes_sent_total{topic}` - Counter

---

### Task 2: Kafka Consumer (Backend → AI)

**File:** `src/kafka/consumer.py`

**Responsibilities:**
- Receive `CommitEvent` from `control.commits.v1`
- Receive `ReputationEvent` from `control.reputation.v1`
- Receive `PolicyUpdateEvent` from `control.policy.v1`
- Receive `EvidenceRequestEvent` from `control.evidence.v1`
- Verify signatures on all incoming messages
- Handle message processing errors

**Implementation:**

```python
"""
Kafka consumer for receiving backend control messages.
"""
from kafka import KafkaConsumer
from kafka.errors import KafkaError
import threading
import time
from typing import Callable, Dict
from ..contracts import (
    CommitEvent, 
    ReputationEvent, 
    PolicyUpdateEvent, 
    EvidenceRequestEvent
)
from ..utils.errors import ContractError
from ..config import Config


class AIConsumer:
    """
    Kafka consumer for Backend → AI messages.
    
    Receives signed protobuf messages from:
    - control.commits.v1
    - control.reputation.v1
    - control.policy.v1
    - control.evidence.v1
    """
    
    TOPICS = [
        "control.commits.v1",
        "control.reputation.v1",
        "control.policy.v1",
        "control.evidence.v1",
    ]
    
    def __init__(self, config: Config):
        self.config = config
        
        # Kafka configuration
        kafka_config = {
            'bootstrap_servers': config.get('KAFKA_BROKERS', 'localhost:9092').split(','),
            'group_id': config.get('KAFKA_CONSUMER_GROUP', 'ai-service'),
            'auto_offset_reset': 'earliest',
            'enable_auto_commit': True,
            'max_poll_records': 100,
            'value_deserializer': lambda v: v,  # Raw bytes, will parse manually
        }
        
        # TLS configuration
        if config.get_bool('KAFKA_TLS_ENABLED', True):
            kafka_config.update({
                'security_protocol': 'SSL',
                'ssl_cafile': config.get('KAFKA_CA_CERT'),
                'ssl_certfile': config.get('KAFKA_CLIENT_CERT'),
                'ssl_keyfile': config.get('KAFKA_CLIENT_KEY'),
            })
        
        self.consumer = KafkaConsumer(*self.TOPICS, **kafka_config)
        
        # Message handlers
        self.handlers: Dict[str, Callable] = {}
        
        # Control
        self._running = False
        self._thread = None
        
        # Metrics
        self._messages_received = 0
        self._messages_processed = 0
        self._messages_failed = 0
    
    def register_handler(self, message_type: str, handler: Callable):
        """
        Register handler for message type.
        
        Args:
            message_type: "commit", "reputation", "policy_update", "evidence_request"
            handler: Callable that takes parsed message
        """
        self.handlers[message_type] = handler
    
    def start(self):
        """Start consumer in background thread"""
        if self._running:
            return
        
        self._running = True
        self._thread = threading.Thread(target=self._consume_loop, daemon=True)
        self._thread.start()
    
    def stop(self):
        """Stop consumer"""
        self._running = False
        if self._thread:
            self._thread.join(timeout=10)
        self.consumer.close()
    
    def _consume_loop(self):
        """Main consumer loop"""
        while self._running:
            try:
                messages = self.consumer.poll(timeout_ms=1000)
                
                for topic_partition, records in messages.items():
                    for record in records:
                        self._messages_received += 1
                        self._process_message(record.topic, record.value)
                        
            except Exception as e:
                # Log error but keep consuming
                time.sleep(1)
    
    def _process_message(self, topic: str, data: bytes):
        """Process single message"""
        try:
            # Parse based on topic
            if topic == "control.commits.v1":
                msg = CommitEvent.from_bytes(data, verify_signature=True)
                handler = self.handlers.get("commit")
                if handler:
                    handler(msg)
                    
            elif topic == "control.reputation.v1":
                msg = ReputationEvent.from_bytes(data, verify_signature=True)
                handler = self.handlers.get("reputation")
                if handler:
                    handler(msg)
                    
            elif topic == "control.policy.v1":
                msg = PolicyUpdateEvent.from_bytes(data, verify_signature=True)
                handler = self.handlers.get("policy_update")
                if handler:
                    handler(msg)
                    
            elif topic == "control.evidence.v1":
                msg = EvidenceRequestEvent.from_bytes(data, verify_signature=True)
                handler = self.handlers.get("evidence_request")
                if handler:
                    handler(msg)
            
            self._messages_processed += 1
            
        except ContractError as e:
            # Signature/validation failure
            self._messages_failed += 1
            # Log to audit
            
        except Exception as e:
            # Processing error
            self._messages_failed += 1
            # Log error
    
    def get_metrics(self) -> dict:
        """Get consumer metrics"""
        return {
            "messages_received": self._messages_received,
            "messages_processed": self._messages_processed,
            "messages_failed": self._messages_failed,
        }
```

**Metrics to track:**
- `kafka_messages_received_total{topic, type}` - Counter
- `kafka_messages_processed_total{topic, type}` - Counter
- `kafka_messages_failed_total{topic, type}` - Counter
- `kafka_processing_latency_seconds{topic}` - Histogram

---

### Task 3: Kafka Manager (Orchestration)

**File:** `src/kafka/manager.py`

**Purpose:** Coordinate producer and consumer, handle lifecycle

```python
"""
Kafka manager - orchestrates producer and consumer.
"""
from typing import Optional
from .producer import AIProducer
from .consumer import AIConsumer
from ..config import Config
from ..utils.circuit_breaker import CircuitBreaker
from ..utils.signer import Signer
from ..utils.nonce import NonceManager


class KafkaManager:
    """
    Manages Kafka producer and consumer lifecycle.
    """
    
    def __init__(
        self, 
        config: Config,
        signer: Signer,
        nonce_manager: NonceManager,
        circuit_breaker: CircuitBreaker,
    ):
        self.config = config
        self.signer = signer
        self.nonce_manager = nonce_manager
        
        self.producer = AIProducer(config, circuit_breaker)
        self.consumer = AIConsumer(config)
    
    def start(self):
        """Start consumer"""
        self.consumer.start()
    
    def stop(self):
        """Stop producer and consumer"""
        self.producer.flush()
        self.producer.close()
        self.consumer.stop()
    
    def get_producer(self) -> AIProducer:
        """Get producer instance"""
        return self.producer
    
    def get_consumer(self) -> AIConsumer:
        """Get consumer instance"""
        return self.consumer
```

---

### Task 4: Integration Layer

**File:** `src/kafka/__init__.py`

```python
"""
Kafka integration layer for AI ↔ Backend communication.
"""
from .producer import AIProducer
from .consumer import AIConsumer
from .manager import KafkaManager

__all__ = [
    "AIProducer",
    "AIConsumer",
    "KafkaManager",
]
```

---

### Task 5: Configuration

**Update:** `src/config/settings.py` or `.env`

Add Kafka configuration:

```bash
# Kafka Connection
KAFKA_BROKERS=localhost:9092,localhost:9093,localhost:9094
KAFKA_CONSUMER_GROUP=ai-service

# Kafka Topics (optional, defaults are built-in)
KAFKA_TOPIC_ANOMALY=ai.anomalies.v1
KAFKA_TOPIC_EVIDENCE=ai.evidence.v1
KAFKA_TOPIC_POLICY=ai.policy.v1

# Kafka TLS
KAFKA_TLS_ENABLED=true
KAFKA_CA_CERT=/etc/kafka/ca-cert.pem
KAFKA_CLIENT_CERT=/etc/kafka/client-cert.pem
KAFKA_CLIENT_KEY=/etc/kafka/client-key.pem

# Kafka Tuning
KAFKA_MAX_RETRIES=5
KAFKA_RETRY_BACKOFF_MS=1000
KAFKA_REQUEST_TIMEOUT_MS=30000
```

---

### Task 6: Error Handling & DLQ

**File:** `src/kafka/dlq.py`

**Purpose:** Handle messages that fail processing

```python
"""
Dead Letter Queue handler for failed messages.
"""
from kafka import KafkaProducer
import json
import time


class DLQHandler:
    """
    Handles failed messages by sending to DLQ topic.
    """
    
    def __init__(self, producer: KafkaProducer, dlq_topic: str = "ai.dlq"):
        self.producer = producer
        self.dlq_topic = dlq_topic
    
    def send_to_dlq(self, original_topic: str, data: bytes, error: str):
        """Send failed message to DLQ with error metadata"""
        dlq_message = {
            "original_topic": original_topic,
            "timestamp": int(time.time()),
            "error": str(error),
            "data": data.hex(),  # Hex-encoded for JSON
        }
        
        self.producer.send(
            self.dlq_topic,
            value=json.dumps(dlq_message).encode('utf-8')
        )
```

---

## 📦 DEPENDENCIES

Update `requirements.txt`:

```txt
kafka-python>=2.0.2
```

**Note:** `kafka-python` already in requirements.txt from P0.1 ✅

---

## 🧪 TESTING

### Unit Tests

**File:** `tests/kafka/test_producer.py`

```python
import pytest
from src.kafka.producer import AIProducer
from src.contracts import AnomalyMessage
from src.utils.signer import Signer
from src.utils.nonce import NonceManager
from src.config import Config
import uuid
import time


def test_producer_sends_anomaly(mock_kafka):
    """Test producer sends anomaly message"""
    config = Config()
    circuit_breaker = CircuitBreaker()
    producer = AIProducer(config, circuit_breaker)
    
    signer = Signer("test_key.pem", "test-node", "ai.anomaly.v1")
    nonce_mgr = NonceManager(instance_id=1)
    
    msg = AnomalyMessage(
        anomaly_id=str(uuid.uuid4()),
        anomaly_type="test",
        source="192.168.1.1",
        severity=5,
        confidence=0.9,
        timestamp=int(time.time()),
        payload=b"test payload",
        model_version="test-v1",
        signer=signer,
        nonce_manager=nonce_mgr,
    )
    
    result = producer.send_anomaly(msg)
    assert result == True
    
    metrics = producer.get_metrics()
    assert metrics["messages_sent"] == 1
```

### Integration Tests

**File:** `tests/integration/test_kafka_roundtrip.py`

Test full roundtrip: Producer → Kafka → Consumer

---

## 📊 METRICS & MONITORING

Integrate with existing metrics (`src/utils/metrics.py`):

```python
# Producer metrics
KAFKA_MESSAGES_SENT = Counter('kafka_messages_sent_total', 'Messages sent', ['topic', 'type'])
KAFKA_MESSAGES_FAILED = Counter('kafka_messages_failed_total', 'Messages failed', ['topic', 'type'])
KAFKA_SEND_LATENCY = Histogram('kafka_send_latency_seconds', 'Send latency', ['topic'])

# Consumer metrics
KAFKA_MESSAGES_RECEIVED = Counter('kafka_messages_received_total', 'Messages received', ['topic'])
KAFKA_MESSAGES_PROCESSED = Counter('kafka_messages_processed_total', 'Messages processed', ['topic'])
KAFKA_PROCESSING_LATENCY = Histogram('kafka_processing_latency_seconds', 'Processing latency', ['topic'])
```

---

## 🔐 SECURITY CHECKLIST

- ✅ TLS enabled for all Kafka connections
- ✅ All outgoing messages signed with Ed25519
- ✅ All incoming messages signature-verified
- ✅ Certificate validation enabled
- ✅ No credentials in logs
- ✅ Circuit breaker prevents DDoS
- ✅ Rate limiting on producer

---

## 📁 FILE STRUCTURE

```
ai-service/
├── src/
│   ├── kafka/              ← NEW
│   │   ├── __init__.py
│   │   ├── producer.py     # AIProducer
│   │   ├── consumer.py     # AIConsumer
│   │   ├── manager.py      # KafkaManager
│   │   └── dlq.py          # DLQHandler
│   ├── contracts/          ✅ Done
│   ├── utils/              ✅ Done
│   └── config/             ✅ Done
└── tests/
    └── kafka/              ← NEW
        ├── test_producer.py
        ├── test_consumer.py
        └── test_integration.py
```

---

## ✅ ACCEPTANCE CRITERIA

1. **Producer sends messages:** AI can send anomaly/evidence/policy to backend
2. **Consumer receives messages:** AI can receive commit/reputation/policy updates from backend
3. **Signatures work:** All messages signed and verified correctly
4. **Error handling:** Failed messages go to DLQ, retries work
5. **Metrics tracked:** All Kafka operations instrumented
6. **TLS works:** Connections secured
7. **Tests pass:** Unit and integration tests green

---

## 🚀 NEXT PHASE AFTER P0.3

**P0.4: Basic Detection**
- Simple anomaly detection logic
- Integration with Kafka producer
- Send first real anomaly to backend
- Verify full end-to-end flow

**Duration:** 1-2 days

**Goal:** First functional detection pipeline

---

## 📝 SUMMARY

**Phase P0.3 Duration:** 1-2 days
**Lines of Code:** ~800 lines
**Files Created:** 7 files (4 src + 3 tests)
**Dependencies:** kafka-python (already installed)

**After P0.3:**
- ✅ AI can send messages to backend
- ✅ AI can receive messages from backend  
- ✅ Full bidirectional Kafka communication
- ✅ Error handling and monitoring
- ✅ Ready for detection logic (P0.4)
