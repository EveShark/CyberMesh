"""
TEST: AI → Backend Kafka Integration

1. Backend is running (started manually)
2. AI publishes anomaly to ai.anomalies.v1
3. Backend should consume and process
4. Check backend logs for consumption

Run this WHILE backend is running in another terminal!
"""

import sys
import time
from pathlib import Path

# Ensure ai-service package on path (absolute to avoid CWD issues)
sys.path.insert(0, str(Path(r"B:/CyberMesh/ai-service")))

print('=' * 70)
print('TEST: AI to Backend Kafka Integration')
print('=' * 70)
print()

from src.config.loader import load_settings
from src.contracts import AnomalyMessage
from src.utils.signer import Signer
from src.utils.nonce import NonceManager
from src.kafka.producer import AIProducer
from src.utils.circuit_breaker import CircuitBreaker
import uuid
import json

# Load AI service settings
print('[1/5] Loading AI service configuration...')
settings = load_settings(env_file='ai-service/.env')
print(f'  Kafka cluster: {settings.kafka_producer.bootstrap_servers}')
print()

# Initialize crypto
print('[2/5] Initializing crypto components...')
keys_dir = Path('ai-service/keys')
signer = Signer(
    str(keys_dir / 'signing_key.pem'),
    key_id='ai-node-1',
    domain_separation='ai.v1'
)
nonce_mgr = NonceManager(instance_id=1, ttl_seconds=900)
print('  OK - Signer ready')
print()

# Initialize Kafka producer
print('[3/5] Initializing Kafka producer...')
circuit_breaker = CircuitBreaker(failure_threshold=5, timeout_seconds=30)
producer = AIProducer(settings, circuit_breaker)
print('  OK - Producer connected to Confluent Cloud')
print()

# Create test anomaly
print('[4/5] Creating test AnomalyMessage for backend...')
anomaly_id = str(uuid.uuid4())
timestamp = int(time.time())

# Test payload
payload_data = {
    'test': 'ai_to_backend_integration',
    'threat_type': 'ddos',
    'source_ip': '192.168.1.100',
    'target_ip': '10.0.0.50',
    'packets_per_second': 150000,
    'detection_engines': ['rules', 'math', 'ml'],
    'timestamp': timestamp
}
payload = json.dumps(payload_data).encode('utf-8')

anomaly_msg = AnomalyMessage(
    anomaly_id=anomaly_id,
    anomaly_type='ddos',
    source='ai_ml_pipeline',
    severity=9,
    confidence=0.98,
    timestamp=timestamp,
    payload=payload,
    model_version='1.0.0',
    signer=signer,
    nonce_manager=nonce_mgr
)

print(f'  OK - Anomaly created')
print(f'    - ID: {anomaly_id}')
print(f'    - Type: ddos')
print(f'    - Severity: 9/10')
print(f'    - Confidence: 0.98')
print()

# Publish to Confluent Cloud
print('[5/5] Publishing to ai.anomalies.v1...')
print()
print('  ** Backend should now receive this message! **')
print('  ** Check backend terminal for consumption logs **')
print()

try:
    success = producer.send_anomaly(anomaly_msg)
    
    if success:
        print('  OK - Message sent (async)')
        producer.flush(timeout=10)
        
        metrics = producer.get_metrics()
        print(f'  Delivered: {metrics["messages_sent"]} messages')
        print(f'  Failed: {metrics["messages_failed"]} messages')
        print(f'  Bytes: {metrics["bytes_sent"]} bytes')
        
        if metrics['messages_sent'] > 0 and metrics['messages_failed'] == 0:
            print()
            print('=' * 70)
            print('SUCCESS - Message published to Confluent Cloud!')
            print('=' * 70)
            print()
            print('What to check in backend terminal:')
            print('  1. "Kafka consumer started" message')
            print('  2. Stats showing consumed: 1+')
            print('  3. Stats showing admitted: 1+')
            print('  4. Mempool size: 1+')
            print()
            print(f'Search for anomaly ID in logs: {anomaly_id}')
        else:
            print()
            print('ERROR - Message not delivered!')
            sys.exit(1)
    else:
        print('  ERROR - Send failed')
        sys.exit(1)
        
except Exception as e:
    print(f'  FATAL - Publish failed: {e}')
    import traceback
    traceback.print_exc()
    sys.exit(1)
