# Configuration System - Implementation Summary

Military-grade configuration system with production gates and fail-closed validation.

---

## Structure

```
config/
├── __init__.py         # Public API exports
├── settings.py         # Aggregator + production gates
├── security.py         # Ed25519, JWT, mTLS, AES-256
└── kafka.py            # Producer/consumer + topics
```

---

## Modules

### security.py (200 lines)

**Purpose**: Security configuration with validation and production gates.

**Dataclasses**:
- `Ed25519Config` - Signing key path, key ID, domain separation
- `JWTConfig` - JWT secret, algorithm, expiration
- `MTLSConfig` - mTLS enable flag, CA/server cert paths
- `AESConfig` - AES-256-GCM key path for at-rest encryption
- `SecurityConfig` - Aggregates all security configs

**Validation Rules**:
- Ed25519 private key file permissions must be 0600
- JWT secret minimum 64 chars (128 in production)
- Key ID max 128 characters
- Production requires mTLS or strong JWT
- All certificate paths validated for existence
- Server key permissions must be 0600

**Environment Variables**:
```
AI_SIGNING_KEY_PATH
AI_SIGNING_KEY_ID
AI_DOMAIN_SEPARATION
JWT_SECRET
JWT_ALGORITHM
JWT_EXPIRATION_SECONDS
MTLS_ENABLED
MTLS_CA_CERT_PATH
MTLS_SERVER_CERT_PATH
MTLS_SERVER_KEY_PATH
AES_ENCRYPTION_ENABLED
AES_KEY_PATH
```

---

### kafka.py (320 lines)

**Purpose**: Kafka producer/consumer configuration with TLS 1.3 and SASL.

**Dataclasses**:
- `KafkaSecurityConfig` - TLS settings, SASL mechanism, credentials
- `KafkaTopicsConfig` - All topic names (8 topics)
- `KafkaProducerConfig` - Producer settings with idempotence
- `KafkaConsumerConfig` - Consumer settings with manual commits

**Producer Defaults**:
- `acks=all` (required in production)
- `idempotence_enabled=true` (required in production)
- `compression=snappy`
- `retries=10`
- `max_in_flight_requests=5`

**Consumer Defaults**:
- `enable_auto_commit=false` (required in production)
- `auto_offset_reset=earliest`
- `max_poll_records=500`
- `session_timeout_ms=10000`

**Security Validation**:
- TLS required in production
- SASL mechanism must be SCRAM-SHA-256 or SCRAM-SHA-512 (not PLAIN) in production
- SASL password minimum 16 chars (32 in production)
- CA cert required for TLS in production

**Environment Variables**:
```
KAFKA_BOOTSTRAP_SERVERS
KAFKA_TLS_ENABLED
KAFKA_SASL_MECHANISM
KAFKA_SASL_USERNAME
KAFKA_SASL_PASSWORD
KAFKA_CA_CERT_PATH
KAFKA_CLIENT_CERT_PATH
KAFKA_CLIENT_KEY_PATH
TOPIC_AI_ANOMALIES
TOPIC_AI_EVIDENCE
TOPIC_AI_POLICY
TOPIC_CONTROL_COMMITS
TOPIC_CONTROL_REPUTATION
TOPIC_CONTROL_POLICY
TOPIC_CONTROL_EVIDENCE
TOPIC_DLQ
KAFKA_PRODUCER_* (9 settings)
KAFKA_CONSUMER_* (9 settings)
```

---

### settings.py (216 lines)

**Purpose**: Main aggregator with production gates and validation.

**Dataclasses**:
- `APIConfig` - Admin API listen address, health/metrics paths
- `ModelConfig` - Model paths, hot-reload settings
- `DetectionConfig` - Pipeline confidence thresholds
- `Settings` - Complete aggregated settings

**Production Gates** (fail-closed):
1. Kafka TLS must be enabled
2. Kafka SASL cannot be PLAIN
3. Kafka producer acks must be 'all'
4. Kafka producer idempotence must be enabled
5. Kafka consumer auto-commit must be disabled
6. mTLS enabled OR JWT secret >= 128 chars
7. Ed25519 signing key ID must be set

**Environment Variables**:
```
NODE_ID
ENVIRONMENT (development|staging|production)
API_LISTEN_ADDR
API_HEALTH_PATH
API_METRICS_PATH
API_ADMIN_PATH_PREFIX
MODEL_DDOS_PATH
MODEL_MALWARE_PATH
MODEL_HOT_RELOAD_ENABLED
MODEL_RELOAD_CHECK_INTERVAL
DETECTION_DDOS_CONFIDENCE_THRESHOLD
DETECTION_MALWARE_CONFIDENCE_THRESHOLD
DETECTION_ENABLE_DDOS
DETECTION_ENABLE_MALWARE
```

---

## Production Gates Enforcement

**Fail-closed validation**: Any violation raises `ConfigError`.

### Security Gates
- Ed25519 key file exists with 0600 permissions
- JWT secret >= 128 characters
- mTLS enabled OR strong JWT
- SASL passwords >= 32 characters

### Kafka Gates
- TLS enabled (KAFKA_TLS_ENABLED=true)
- SASL mechanism is SCRAM (not PLAIN)
- Producer acks='all'
- Producer idempotence enabled
- Consumer auto-commit disabled
- All topics configured

### Model Gates
- DDoS and Malware model paths must exist
- Hot-reload interval >= 10 seconds
- Confidence thresholds in [0.0, 1.0]

---

## Usage

### Basic Loading
```python
from config import load_settings

settings = load_settings()

print(f"Node ID: {settings.node_id}")
print(f"Environment: {settings.environment}")
print(f"Kafka topics: {settings.kafka_topics.ai_anomalies}")
```

### Kafka Producer Config
```python
from kafka import KafkaProducer

producer = KafkaProducer(**settings.kafka_producer.to_kafka_config())
```

### Kafka Consumer Config
```python
from kafka import KafkaConsumer

consumer = KafkaConsumer(
    settings.kafka_topics.ai_anomalies,
    **settings.kafka_consumer.to_kafka_config()
)
```

### Security Components
```python
from utils import Signer, NonceManager

# Signer with config
signer = Signer(
    private_key_path=settings.security.ed25519.signing_key_path,
    key_id=settings.security.ed25519.signing_key_id,
    domain_separation=settings.security.ed25519.domain_separation
)

# Nonce manager
nonce_mgr = NonceManager(
    instance_id=int(settings.node_id),
    ttl_seconds=900  # From utils.limits.TIME_LIMITS
)
```

---

## Configuration Checklist

### Development
- [ ] `ENVIRONMENT=development`
- [ ] `NODE_ID=1`
- [ ] Ed25519 key generated and path set
- [ ] JWT secret >= 64 characters
- [ ] Kafka bootstrap servers configured
- [ ] `KAFKA_TLS_ENABLED=false` (optional)
- [ ] `KAFKA_SASL_MECHANISM=SCRAM-SHA-256`
- [ ] All topic names configured
- [ ] Model paths set (can be placeholder files)

### Production
- [ ] `ENVIRONMENT=production`
- [ ] `NODE_ID` unique per instance
- [ ] Ed25519 key with 0600 permissions
- [ ] JWT secret >= 128 characters
- [ ] mTLS enabled OR strong JWT
- [ ] `KAFKA_TLS_ENABLED=true`
- [ ] `KAFKA_SASL_MECHANISM=SCRAM-SHA-256` or `SCRAM-SHA-512`
- [ ] SASL password >= 32 characters
- [ ] Kafka CA certificate path set
- [ ] All topics exist in Kafka
- [ ] Producer acks='all', idempotence=true
- [ ] Consumer auto-commit=false
- [ ] Model files exist and validated
- [ ] Hot-reload interval appropriate for workload

---

## Error Handling

All validation errors raise `ConfigError` with descriptive messages:

```python
from config import load_settings, ConfigError

try:
    settings = load_settings()
except ConfigError as e:
    print(f"Configuration error: {e}")
    exit(1)
```

**Common errors**:
- "Required environment variable X is not set"
- "Production gate: Kafka TLS is required"
- "Signing key has insecure permissions. Expected 0600"
- "JWT secret too weak: N chars, minimum 64 required"
- "Production gate: PLAIN SASL not allowed"

---

## Total Environment Variables: 50+

**Critical (required)**: 15
- NODE_ID, ENVIRONMENT
- AI_SIGNING_KEY_PATH, AI_SIGNING_KEY_ID
- JWT_SECRET
- KAFKA_BOOTSTRAP_SERVERS, KAFKA_SASL_USERNAME, KAFKA_SASL_PASSWORD
- 8 topic names
- MODEL_DDOS_PATH, MODEL_MALWARE_PATH

**Optional with defaults**: 35+
- Security (7), Kafka producer (9), Kafka consumer (9), API (4), Models (2), Detection (4)

---

## Integration with Utils

Config system feeds into utils layer:

```python
from config import load_settings
from utils import (
    Signer,
    NonceManager,
    MultiTierRateLimiter,
    CircuitBreaker,
    SecureLogger,
    get_metrics_collector
)

settings = load_settings()

# Initialize security components
signer = Signer(
    private_key_path=settings.security.ed25519.signing_key_path,
    key_id=settings.security.ed25519.signing_key_id
)

nonce_mgr = NonceManager(instance_id=int(settings.node_id))

# Initialize metrics
metrics = get_metrics_collector()

# Initialize logging
logger = SecureLogger("ai.service")
```

---

## Next Steps

1. **Protobuf schemas** - Define message structures for topics
2. **Kafka producer wrapper** - Idempotent publishing with DLQ
3. **Kafka consumer wrapper** - Manual commit with circuit breaker
4. **Model loader** - Hot-reload with validation
5. **Detection pipelines** - DDoS and Malware detection with confidence thresholds
