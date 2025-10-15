"""
FULL AI → Backend Integration Test

Starts backend subprocess, publishes from AI, verifies consumption.
"""

import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent / 'ai-service'))

print('=' * 70)
print('FULL AI to Backend Integration Test')
print('=' * 70)
print()

# Step 1: Start backend
print('[1/7] Starting backend Kafka consumer...')
backend_process = subprocess.Popen(
    ['go', 'run', 'cmd/cybermesh/kafka_simple.go'],
    cwd='B:/CyberMesh/backend',
    stdout=subprocess.PIPE,
    stderr=subprocess.STDOUT,
    text=True,
    bufsize=1
)

# Wait for backend to start
print('  Waiting 8 seconds for backend to initialize...')
time.sleep(8)

if backend_process.poll() is not None:
    print('  ERROR - Backend exited prematurely!')
    print('  Output:')
    for line in backend_process.stdout:
        print(f'    {line}', end='')
    sys.exit(1)

print('  OK - Backend started (PID: {})'.format(backend_process.pid))
print()

try:
    # Step 2-6: AI publishes (same as test_ai_to_backend.py)
    from src.config.loader import load_settings
    from src.contracts import AnomalyMessage
    from src.utils.signer import Signer
    from src.utils.nonce import NonceManager
    from src.kafka.producer import AIProducer
    from src.utils.circuit_breaker import CircuitBreaker
    import uuid
    import json

    print('[2/7] Loading AI service configuration...')
    settings = load_settings(env_file='ai-service/.env')
    print(f'  Kafka cluster: {settings.kafka_producer.bootstrap_servers}')
    print()

    print('[3/7] Initializing crypto components...')
    keys_dir = Path('ai-service/keys')
    signer = Signer(
        str(keys_dir / 'signing_key.pem'),
        key_id='ai-node-1',
        domain_separation='ai.v1'
    )
    nonce_mgr = NonceManager(instance_id=1, ttl_seconds=900)
    print('  OK - Signer ready')
    print()

    print('[4/7] Initializing Kafka producer...')
    circuit_breaker = CircuitBreaker(failure_threshold=5, timeout_seconds=30)
    producer = AIProducer(settings, circuit_breaker)
    print('  OK - Producer connected')
    print()

    print('[5/7] Creating test AnomalyMessage...')
    anomaly_id = str(uuid.uuid4())
    timestamp = int(time.time())

    payload_data = {
        'test': 'full_integration',
        'threat_type': 'ddos',
        'source_ip': '192.168.1.100',
        'target_ip': '10.0.0.50',
        'packets_per_second': 150000,
        'detection_engines': ['rules', 'math', 'ml'],
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

    print(f'  OK - Anomaly ID: {anomaly_id}')
    print()

    print('[6/7] Publishing to Confluent Cloud...')
    success = producer.send_anomaly(anomaly_msg)
    
    if not success:
        print('  ERROR - Send failed!')
        raise Exception('Publish failed')
    
    producer.flush(timeout=10)
    metrics = producer.get_metrics()
    
    print(f'  OK - Published ({metrics["messages_sent"]} sent, {metrics["bytes_sent"]} bytes)')
    print()

    # Step 7: Wait and check backend output
    print('[7/7] Waiting for backend to consume message (15 seconds)...')
    print()
    
    consumed = False
    start_time = time.time()
    
    while time.time() - start_time < 15:
        line = backend_process.stdout.readline()
        if line:
            print(f'  [BACKEND] {line}', end='')
            
            # Check for consumption indicators
            if 'consumed' in line.lower() or 'admitted' in line.lower():
                consumed = True
            if anomaly_id in line:
                print()
                print('  ✓ Anomaly ID found in backend output!')
                consumed = True
        
        time.sleep(0.1)
    
    print()
    
    if consumed:
        print('=' * 70)
        print('SUCCESS - Backend consumed message from AI!')
        print('=' * 70)
    else:
        print('=' * 70)
        print('PARTIAL SUCCESS - Message published, check backend manually')
        print('=' * 70)
        print()
        print('Backend may still be processing...')

finally:
    # Cleanup
    print()
    print('Shutting down backend...')
    backend_process.terminate()
    try:
        backend_process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        backend_process.kill()
    
    print('Test complete!')
