"""Test Evidence message AI -> Backend flow"""
import sys
import os
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent / 'ai-service'))

from src.contracts import EvidenceMessage
from src.utils.signer import Signer
from src.utils.nonce import NonceManager
from src.kafka.producer import AIProducer
from src.config.loader import load_settings

print("=" * 70)
print("TEST: Evidence AI to Backend")
print("=" * 70)
print()

# Load config
settings = load_settings()
print(f"[1/5] Kafka: {settings.kafka.bootstrap_servers}")

# Crypto
signer = Signer('ai-service/config/keys/test_integration_key.pem', key_id='ai-node-1')
nonce_mgr = NonceManager(signer.public_key_bytes)
print("[2/5] Crypto: OK")

# Producer
producer = AIProducer(settings.kafka)
print("[3/5] Producer: OK")

# Create evidence
evidence = EvidenceMessage(
    evidence_id="550e8400-e29b-41d4-a716-446655440001",
    evidence_type="pcap",
    refs=[b'\x11' * 32, b'\x22' * 32],
    proof_blob=b"PCAP binary data here...",
    chain_of_custody=[
        (b'\xaa' * 32, b'\xbb' * 32, 1704067200, b'\xcc' * 64)
    ],
    timestamp=int(time.time()),
    signer=signer,
    nonce_manager=nonce_mgr,
)
print(f"[4/5] Evidence created: {evidence.evidence_id}")

# Publish
result = producer.publish_evidence(evidence)
print(f"[5/5] Published: partition={result.partition}, offset={result.offset}")
print()
print("=" * 70)
print("Check backend logs for: consumed, verified, admitted stats")
print("=" * 70)
