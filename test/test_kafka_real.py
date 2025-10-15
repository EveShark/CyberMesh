#!/usr/bin/env python3
"""
REAL Kafka Integration Test - NO MOCKS

Tests:
1. Load config from .env
2. Generate signing keys
3. Create real AnomalyMessage with signatures
4. Connect to REAL Kafka (Confluent Cloud)
5. Send message to ai.anomalies.v1
6. Verify message was sent
7. Try to consume from control.commits.v1 (if backend is running)

Requirements:
- .env must have valid KAFKA_SASL_USERNAME/PASSWORD
- Kafka topics must exist
- Network connectivity to Confluent Cloud
"""
import os
import sys
import uuid
import time

# Load .env from project root
from dotenv import load_dotenv
load_dotenv("B:/CyberMesh/.env")

sys.path.insert(0, "B:/CyberMesh/ai-service")

print("="*70)
print("REAL KAFKA INTEGRATION TEST - NO MOCKS")
print("="*70)

# Verify Kafka config
print("\n[STEP 1] Verify Kafka Configuration...")
kafka_brokers = os.getenv("KAFKA_BROKERS")
kafka_username = os.getenv("KAFKA_SASL_USERNAME")
kafka_password = os.getenv("KAFKA_SASL_PASSWORD")
kafka_mechanism = os.getenv("KAFKA_SASL_MECHANISM")

print(f"  Brokers: {kafka_brokers}")
print(f"  SASL Mechanism: {kafka_mechanism}")
print(f"  Username: {kafka_username}")
print(f"  Password: {'*' * len(kafka_password) if kafka_password else 'NOT SET'}")

if kafka_username == "CHANGE_ME" or kafka_password == "CHANGE_ME":
    print("\n  ERROR: Kafka credentials not set!")
    print("  Please update KAFKA_SASL_USERNAME and KAFKA_SASL_PASSWORD in .env")
    sys.exit(1)

print("  Config OK")

# Generate signing key
print("\n[STEP 2] Generate Ed25519 Signing Key...")
from src.utils.signer import generate_keypair, Signer
from src.utils.nonce import NonceManager

key_path = "B:/CyberMesh/test_integration_key.pem"
if os.path.exists(key_path):
    os.remove(key_path)

pub_bytes, priv_bytes = generate_keypair(key_path)
print(f"  Generated: {len(pub_bytes)} byte pubkey, {len(priv_bytes)} byte private key")

signer = Signer(key_path, "test-ai-node", domain_separation="ai.anomaly.v1")
nonce_mgr = NonceManager(instance_id=1)
print("  Signer ready")

# Create real anomaly message
print("\n[STEP 3] Create Real AnomalyMessage...")
from src.contracts import AnomalyMessage

anomaly_id = str(uuid.uuid4())
timestamp = int(time.time())

msg = AnomalyMessage(
    anomaly_id=anomaly_id,
    anomaly_type="test_ddos",
    source="192.168.1.100",
    severity=8,
    confidence=0.95,
    timestamp=timestamp,
    payload=b"REAL TEST: DDoS attack detected - integration test",
    model_version="test-v1.0.0",
    signer=signer,
    nonce_manager=nonce_mgr,
)

print(f"  ID: {msg.anomaly_id}")
print(f"  Type: {msg.anomaly_type}")
print(f"  Severity: {msg.severity}")
print(f"  Timestamp: {msg.timestamp}")
print(f"  Nonce: {len(msg.nonce)} bytes")
print(f"  Signature: {len(msg.signature)} bytes")
print(f"  Pubkey: {len(msg.pubkey)} bytes")

# Serialize
serialized = msg.to_bytes()
print(f"  Serialized: {len(serialized)} bytes")

# Verify roundtrip
msg_verify = AnomalyMessage.from_bytes(serialized, nonce_manager=NonceManager(2))
print(f"  Roundtrip verification: PASS")

# Connect to REAL Kafka
print("\n[STEP 4] Connect to Real Kafka (Confluent Cloud)...")
from kafka import KafkaProducer
from kafka.errors import KafkaError as KafkaLibError

kafka_config = {
    'bootstrap_servers': kafka_brokers.split(','),
    'security_protocol': 'SASL_SSL',
    'sasl_mechanism': kafka_mechanism,
    'sasl_plain_username': kafka_username,
    'sasl_plain_password': kafka_password,
    'acks': 'all',
    'retries': 3,
    'value_serializer': lambda v: v,
}

print("  Connecting...")
try:
    producer = KafkaProducer(**kafka_config)
    print("  Connected to Kafka!")
except Exception as e:
    print(f"  FAILED: {e}")
    print("\n  Possible issues:")
    print("    - Invalid credentials")
    print("    - Network connectivity")
    print("    - Firewall blocking port 9092")
    sys.exit(1)

# Send message
print("\n[STEP 5] Send AnomalyMessage to ai.anomalies.v1...")
topic = "ai.anomalies.v1"

try:
    future = producer.send(topic, value=serialized)
    record_metadata = future.get(timeout=10)
    
    print(f"  SUCCESS!")
    print(f"    Topic: {record_metadata.topic}")
    print(f"    Partition: {record_metadata.partition}")
    print(f"    Offset: {record_metadata.offset}")
    print(f"    Timestamp: {record_metadata.timestamp}")
    
except KafkaLibError as e:
    print(f"  FAILED: {e}")
    print("\n  Possible issues:")
    print("    - Topic doesn't exist")
    print("    - No permissions to write")
    print("    - Message too large")
    sys.exit(1)

producer.flush()
producer.close()

# Try to consume from control topic
print("\n[STEP 6] Try to consume from control.commits.v1...")
print("  (Will fail if backend is not running - that's OK)")

from kafka import KafkaConsumer

try:
    consumer = KafkaConsumer(
        'control.commits.v1',
        **{
            'bootstrap_servers': kafka_brokers.split(','),
            'security_protocol': 'SASL_SSL',
            'sasl_mechanism': kafka_mechanism,
            'sasl_plain_username': kafka_username,
            'sasl_plain_password': kafka_password,
            'group_id': 'test-consumer',
            'auto_offset_reset': 'latest',
            'value_deserializer': lambda v: v,
        }
    )
    
    print("  Connected to consumer")
    print("  Polling for 5 seconds...")
    
    messages = consumer.poll(timeout_ms=5000)
    if messages:
        print(f"  Received {sum(len(records) for records in messages.values())} messages")
        for topic_partition, records in messages.items():
            for record in records:
                print(f"    Offset {record.offset}: {len(record.value)} bytes")
                from src.contracts import CommitEvent
                try:
                    commit = CommitEvent.from_bytes(record.value, verify_signature=True)
                    print(f"      Block height: {commit.height}, tx_count: {commit.tx_count}")
                except Exception as e:
                    print(f"      Parse failed: {e}")
    else:
        print("  No messages (backend probably not running)")
    
    consumer.close()
    
except Exception as e:
    print(f"  Consumer error: {e}")
    print("  (This is OK if backend is not running)")

# Cleanup
os.remove(key_path)

print("\n" + "="*70)
print("INTEGRATION TEST COMPLETE")
print("="*70)
print("\nResults:")
print("  ✓ Config loaded from .env")
print("  ✓ Ed25519 signing key generated")
print("  ✓ AnomalyMessage created and signed")
print("  ✓ Connected to REAL Kafka (Confluent Cloud)")
print("  ✓ Message sent to ai.anomalies.v1")
print("  ✓ Serialization/deserialization works")
print("\nKafka Integration: VERIFIED AND WORKING")
print("\nNext: Start backend to see full AI → Backend → AI flow")
