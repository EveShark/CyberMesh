# CyberMesh AI Service

## 1. Introduction

The CyberMesh AI Service is a **production-grade threat detection engine** implementing multiple AI algorithms for real-time cybersecurity analysis. This service operates as the **intelligence layer** of the CyberMesh platform, processing network traffic, system logs, and behavioral patterns through ensemble machine learning models to identify DDoS attacks, malware signatures, and advanced persistent threats.

The service provides **cryptographically secured** threat verdicts to the backend consensus layer via Kafka streams, supporting **distributed deployment** across a 10-node cluster with automatic peer discovery, mathematical validation, and hot-reload capabilities for model updates without service interruption.

**Key Capabilities:**
- **Multi-Engine Detection**: Mathematical, ML-based, and threat-intelligence engines
- **Distributed Coordination**: 10-node cluster with peer management and gossip protocols  
- **Real-time Processing**: Sub-second threat classification with batch processing support
- **Secure Communication**: TLS 1.3, Ed25519 signatures, AES-256 encryption for all external interfaces
- **Production Monitoring**: Prometheus metrics, structured logging, health endpoints

---

## 2. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    CYBERMESH ARCHITECTURE                       │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐    ┌──────────────┐    ┌─────────────────────┐ │
│  │   FRONTEND  │    │   BACKEND    │    │      STORAGE        │ │
│  │  Dashboard  │◄──►│  (Go PBFT)   │◄──►│   Blockchain +      │ │
│  │   React     │    │  Consensus   │    │   PostgreSQL +      │ │
│  │             │    │  P2P LibP2P  │    │   Redis Cache       │ │
│  └─────────────┘    └──────────────┘    └─────────────────────┘ │
│           ▲                  ▲                        ▲         │
│           │                  │                        │         │
│           ▼                  ▼                        ▼         │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                  AI SERVICE (Python)                       │ │
│  │  ┌──────────────┐  ┌─────────────┐  ┌─────────────────────┐ │ │
│  │  │   FastAPI    │  │   Pipeline  │  │   Detection         │ │ │
│  │  │   REST/WS    │◄─┤Orchestrator │◄─┤   Engines           │ │ │
│  │  │   Endpoints  │  │             │  │  • ML Engine        │ │ │
│  │  └──────────────┘  └─────────────┘  │  • Threat Engine    │ │ │
│  │           ▲                         │  • Math Engine      │ │ │
│  │           │                         │  • Ensemble         │ │ │
│  │           ▼                         └─────────────────────┘ │ │
│  │  ┌──────────────────────────────────────────────────────────┐ │ │
│  │  │            DISTRIBUTED COORDINATION                     │ │ │
│  │  │  ┌─────────────┐  ┌────────────┐  ┌──────────────────┐ │ │ │
│  │  │  │Kafka Service│  │Peer Manager│  │ Message Bus      │ │ │ │
│  │  │  │Producer/    │◄►│P2P Gossip  │◄►│Ed25519 Signatures│ │ │ │
│  │  │  │Consumer     │  │Discovery   │  │HMAC Validation   │ │ │ │
│  │  │  └─────────────┘  └────────────┘  └──────────────────┘ │ │ │
│  │  └──────────────────────────────────────────────────────────┘ │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. AI Pipeline Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                       AI DETECTION PIPELINE                         │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  REST API / Kafka Input                                              │
│           │                                                          │
│           ▼                                                          │
│  ┌─────────────────┐     ┌──────────────────────────────────────────┐ │
│  │   CONTROLLERS   │     │          SECURITY LAYER                  │ │
│  │ ┌─────────────┐ │     │ ┌──────────┐ ┌──────────┐ ┌──────────── │ │
│  │ │   Threat    │ │◄───►│ │   JWT    │ │  Input   │ │   HMAC     │ │ │
│  │ │ Controller  │ │     │ │  Auth    │ │Sanitizer │ │ Signatures │ │ │
│  │ │             │ │     │ └──────────┘ └──────────┘ └────────────┘ │ │
│  │ └─────────────┘ │     └──────────────────────────────────────────┘ │
│  │ ┌─────────────┐ │                                                  │
│  │ │  Training   │ │                                                  │
│  │ │ Controller  │ │                                                  │
│  │ └─────────────┘ │                                                  │
│  │ ┌─────────────┐ │                                                  │
│  │ │   System    │ │                                                  │
│  │ │ Controller  │ │                                                  │
│  │ └─────────────┘ │                                                  │
│  └─────────────────┘                                                  │
│           │                                                          │
│           ▼                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                   CORE PIPELINE                                 │ │
│  │  ┌─────────────────┐    ┌─────────────────┐    ┌──────────────┐ │ │
│  │  │   Data Prep     │───►│   Validation    │───►│    Engine    │ │ │
│  │  │  • Sanitization │    │ • Schema Check  │    │  Delegation  │ │ │
│  │  │  • Normalization│    │ • Type Safety   │    │              │ │ │
│  │  └─────────────────┘    └─────────────────┘    └──────────────┘ │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                     │                                │
│                                     ▼                                │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │                 DETECTION ENGINES                               │ │
│  │                                                                 │ │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────────┐ │ │
│  │  │   ML ENGINE    │  │  THREAT ENGINE │  │  MATHEMATICAL      │ │ │
│  │  │                │  │                │  │     ENGINE         │ │ │
│  │  │ • XGBoost      │  │ • Rule-based   │  │                    │ │ │
│  │  │ • Scikit-learn │  │ • Adaptive     │  │ • System Uptime    │ │ │
│  │  │ • Model Cache  │  │   Thresholds   │  │ • Detection Rate   │ │ │
│  │  │ • Hot Reload   │  │ • Pattern      │  │ • Confidence Calc  │ │ │
│  │  │                │  │   Matching     │  │ • Resilience       │ │ │
│  │  └────────────────┘  └────────────────┘  └────────────────────┘ │ │
│  │                                │                                │ │
│  │                                ▼                                │ │
│  │  ┌─────────────────────────────────────────────────────────────┐ │ │
│  │  │                 ENSEMBLE ORCHESTRATOR                       │ │ │
│  │  │                                                             │ │ │
│  │  │  • Weighted Voting (Majority, Average, Max)                │ │ │
│  │  │  • Cross-engine Validation                                 │ │ │
│  │  │  • Confidence Aggregation                                  │ │ │
│  │  │  • Final Threat Classification                             │ │ │
│  │  └─────────────────────────────────────────────────────────────┘ │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                     │                                │
│                                     ▼                                │
│  ┌─────────────────────────────────────────────────────────────────┐ │
│  │            DISTRIBUTED COORDINATOR                              │ │
│  │                                                                 │ │
│  │  • Ed25519 Result Signing                                      │ │
│  │  • Cache Integration (Redis)                                   │ │
│  │  • Kafka Publishing                                            │ │
│  │  • Backend Consensus Coordination                              │ │
│  └─────────────────────────────────────────────────────────────────┘ │
│                                     │                                │
│                                     ▼                                │
│                      JSON Response + Kafka Event                     │
└──────────────────────────────────────────────────────────────────────┘
```

---

## 5. Integration Flow – Kafka & Backend

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    KAFKA & BACKEND INTEGRATION FLOW                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  PHASE 1: THREAT DETECTION                                                  │
│  ┌─────────────────┐    ┌─────────────────┐    ┌────────────────────────┐   │
│  │  Network Input  │───►│   AI Service    │───►│    Kafka Producer      │   │
│  │  • REST API     │    │  • Pipeline     │    │  Topic:                │   │
│  │  • WebSocket    │    │  • 3x Engines   │    │  cybermesh-detections  │   │
│  │  • Traffic Inj  │    │  • Ensemble     │    │  (Detection Results)   │   │
│  └─────────────────┘    └─────────────────┘    └────────────────────────┘   │
│                                   │                         │               │
│                                   │            ┌────────────▼──────────────┐│
│                                   │            │     Kafka Cluster        ││
│                                   │            │                           ││
│                                   ▼            │ Topics:                   ││
│  ┌─────────────────────────────────────────────│ • cybermesh-detections   ││
│  │              Redis Cache                    │ • cybermesh-consensus    ││
│  │  • Detection Results (TTL: 1h)             │ • cybermesh-feedback     ││
│  │  • Model Performance Metrics               │ • cybermesh-alerts       ││
│  │  • Mathematical Validation Scores          │                           ││
│  │  • Training Session Coordination           │ Partitions: 10 (per node) ││
│  └─────────────────────────────────────────────│ Replication: 3           ││
│                                                └───────────────────────────┘│
│                                                             │               │
│  PHASE 2: BACKEND CONSENSUS                                 │               │
│                                                             ▼               │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                        Backend Service (Go)                             ││
│  │                                                                         ││
│  │  ┌────────────────┐  ┌─────────────────┐  ┌─────────────────────────┐  ││
│  │  │ Kafka Consumer │  │ PBFT Consensus  │  │   Blockchain Ledger     │  ││
│  │  │                │  │                 │  │                         │  ││
│  │  │ • Detection    │─►│ • AI Results    │─►│ • Hash Chain            │  ││
│  │  │   Results      │  │   Aggregation   │  │ • Ed25519 Signatures    │  ││
│  │  │ • Math Scores  │  │ • Cross-Node    │  │ • Immutable Logging     │  ││
│  │  │ • Node Health  │  │   Validation    │  │ • Consensus Tracking    │  ││
│  │  │                │  │ • 7/10 Quorum   │  │                         │  ││
│  │  └────────────────┘  └─────────────────┘  └─────────────────────────┘  ││
│  │                               │                          │              ││
│  │                               ▼                          ▼              ││
│  │  ┌─────────────────────────────────────────────────────────────────────┐││
│  │  │                    P2P Network (LibP2P)                             │││
│  │  │                                                                     │││
│  │  │  • Node Discovery & Health Monitoring                              │││
│  │  │  • Consensus Message Broadcasting                                  │││
│  │  │  • Mathematical Validation Coordination                            │││
│  │  │  • Byzantine Fault Tolerance                                       │││
│  │  └─────────────────────────────────────────────────────────────────────┘││
│  └─────────────────────────────────────────────────────────────────────────┘│
│                                   │                                         │
│                                   ▼                                         │
│  PHASE 3: FEEDBACK LOOP                                                     │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │  ┌────────────────┐    ┌────────────────┐    ┌─────────────────────────┐││
│  │  │ Kafka Producer │───►│ Feedback Topic │───►│   AI Service Training   │││
│  │  │ (Backend)      │    │ cybermesh-     │    │                         │││
│  │  │                │    │ feedback       │    │ • Model Updates         │││
│  │  │ • Consensus    │    │                │    │ • Threshold Adjustments │││
│  │  │   Results      │    │ • True/False   │    │ • Performance Tuning    │││
│  │  │ • Model        │    │   Positives    │    │ • Ensemble Rebalancing  │││
│  │  │   Feedback     │    │ • Accuracy     │    │                         │││
│  │  │ • Performance  │    │   Metrics      │    │                         │││
│  │  │   Analytics    │    │ • Improvement  │    │                         │││
│  │  │                │    │   Suggestions  │    │                         │││
│  │  └────────────────┘    └────────────────┘    └─────────────────────────┘││
│  └─────────────────────────────────────────────────────────────────────────┘│
│                                                                             │
│  FLOW SUMMARY:                                                              │
│  1. AI Service processes network data → Kafka (detections)                 │
│  2. Backend consumes detections → PBFT consensus → Blockchain              │
│  3. Backend publishes feedback → Kafka → AI Service training updates       │
│  4. Mathematical validation ensures cross-node consistency                  │
│  5. P2P network maintains distributed system health                        │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 6. Repository Structure

```
ai-service/
├── cmd/                                    # Service entry points
│   └── main.py                            # Production service launcher
├── app/                                    # Core application modules
│   ├── config/                            # Configuration management
│   │   ├── ai_detection.py               # AI engine configuration
│   │   ├── infrastructure.py             # Infrastructure discovery
│   │   ├── schema_validation.py          # Input validation schemas
│   │   ├── schema_validator.py           # Validation engine
│   │   ├── secrets_loader.py             # Secure credential loading
│   │   ├── security_module.py            # Cryptographic operations
│   │   └── settings.py                   # Environment-based settings
│   ├── controllers/                       # API request controllers
│   │   ├── system_controller.py          # System health and metrics
│   │   ├── threat_controller.py          # Threat detection endpoints
│   │   └── training_controller.py        # Model training management
│   ├── core/                             # Core pipeline logic
│   │   └── pipeline.py                   # Main processing orchestrator
│   ├── distributed/                      # Cluster coordination
│   │   ├── distributed_coordinator.py   # Node coordination manager
│   │   ├── distributed_validator.py     # Cross-node validation
│   │   ├── message_bus.py               # Inter-node messaging
│   │   ├── peer_manager.py              # Peer discovery and health
│   │   └── sync_manager.py              # State synchronization
│   ├── engines/                          # AI processing engines
│   │   ├── mathematical_engine.py       # Statistical analysis engine
│   │   ├── ml_engine.py                 # Machine learning models
│   │   └── threat_engine.py             # Threat intelligence engine
│   ├── ensemble/                         # Verdict aggregation
│   │   └── ensemble.py                  # Multi-engine consensus
│   ├── services/                         # Infrastructure services
│   │   ├── api_service.py               # FastAPI service definition
│   │   ├── cache_service.py             # Redis caching layer
│   │   ├── hot_reload_service.py        # Dynamic configuration updates
│   │   └── kafka_service.py             # Kafka producer/consumer
│   ├── training/                         # Model management
│   │   ├── feedback_consumer.py         # Training feedback processor
│   │   ├── trainer.py                   # Model training engine
│   │   └── updater.py                   # Model deployment manager
│   └── utils/                            # Shared utilities
│       ├── network_utils.py             # Network configuration helpers
│       └── security_utils.py            # Security validation utilities
├── models/                               # AI model storage
├── dataset/                              # Training data repository
├── logs/                                 # Service log storage
├── scripts/                              # Operational scripts
│   └── monitoring/                       # Monitoring automation
└── tests/                                # Test suites
    └── services/                         # Service-level tests
```

---

## 7. API Reference

### **Detection Endpoints**

### **Mathematical Engine Endpoints**

### **Training & Model Management**

### **Infrastructure & System**

### **Configuration & Hot Reload**


---

## 8. Configuration Overview

### **Core Configuration Files**

- **`ai_detection.py`**: ML model parameters, detection thresholds, performance profiles
- **`infrastructure.py`**: Kafka brokers, P2P networking, static rule configurations  
- **`security_module.py`**: Cryptographic settings (Ed25519, AES-256, JWT)
- **`secrets_loader.py`**: Secure credential management with environment variable loading

### **Security Configuration**
- TLS 1.3 certificate paths for external communications
- Ed25519 key pairs for message signing and node authentication  
- AES-256 encryption keys for sensitive data protection
- SASL authentication credentials for Kafka broker connections

---

## 9. Deployment

### **Deployment Options**
- **Standalone**: Python 3.11+ runtime with dependency installation and environment configuration
- **Docker**: Container-based deployment with horizontal scaling capabilities
- **Kubernetes**: Orchestrated cluster deployment with automated service management

### **Environment Requirements**
- Python 3.11+ runtime environment
- Kafka cluster connectivity for message streaming
- Redis instance for caching and session management
- Sufficient computational resources for ML model inference
- TLS certificates for secure communications

---

## 10. Usage & Validation Flow

### **Service Startup Validation**
1. **Initialize Service**: Launch AI service with distributed coordination and full engine integration
2. **Verify Health**: Confirm all detection engines operational and infrastructure dependencies connected
3. **Check Cluster**: Validate peer discovery across distributed nodes and coordination status
4. **Confirm Messaging**: Verify Kafka connectivity and topic availability through service monitoring

### **Traffic Injection & Processing**
1. **API Testing**: Submit threat detection requests through REST endpoints for real-time analysis
2. **Batch Processing**: Validate bulk threat analysis capabilities with multiple sample submissions
3. **Pipeline Testing**: Execute comprehensive analysis workflows with full engine coordination
4. **Message Verification**: Monitor secure verdict publication with encryption and signature validation

### **Health & Log Monitoring**
1. **Performance Monitoring**: Track processing metrics, latency distributions, and accuracy statistics
2. **Component Health**: Monitor individual engine health and distributed system coordination
3. **Log Analysis**: Review structured security events, processing errors, and performance data
4. **Cluster Status**: Verify distributed coordination through peer management and system health

### **Model Updates & Hot Reload**
1. **Configuration Management**: Dynamic configuration updates without service restart
2. **Model Deployment**: Zero-downtime model updates with validation and rollback capabilities
3. **Performance Validation**: Post-deployment accuracy verification and performance benchmarking

---

## 11. Metrics & Observability

### **Prometheus Metrics**
- **Detection Analytics**: Processing latency, accuracy rates, and false positive ratio monitoring
- **Engine Performance**: Individual engine resource utilization and throughput analysis
- **Messaging Health**: Message production rates, consumer performance, and connection stability
- **Cluster Coordination**: Peer health monitoring, heartbeat tracking, and failover event analysis

### **Key Performance Indicators**
- **Threat Detection Latency**: P50, P95, P99 response times for threat classification requests
- **Model Accuracy**: True positive rates, false positive rates, and confidence score distributions
- **System Throughput**: Requests per second processing capacity across all detection engines
- **Cluster Health**: Node availability percentage, successful peer discoveries, and coordination stability

### **Structured Logging**
- **JSON Log Format**: All logs output in structured JSON with timestamp, level, component, trace ID, and message fields  
- **Security Events**: Authentication failures, invalid signatures, replay attacks, and unauthorized access attempts
- **Performance Tracking**: Request processing times, database query performance, and resource utilization patterns
- **Error Classification**: Categorized error logging with severity levels and automated alerting thresholds

### **Alerting & Monitoring Integration**
- **Health Check Integration**: Kubernetes liveness/readiness probes for automated restart on service failures
- **Metric Collection**: Prometheus scraping configuration for comprehensive metric collection and retention
- **Alert Definitions**: Predefined alerts for high error rates, degraded performance, and security incidents
- **Dashboard Integration**: Grafana-compatible metrics for real-time operational visibility

---

## 12. Security Model

### **Cryptographic Standards**
- **Message Signing**: Ed25519 signatures for all Kafka messages ensuring authentication and non-repudiation
- **Data Encryption**: AES-256-CBC encryption for sensitive data at rest and in transit with PBKDF2 key derivation
- **Transport Security**: TLS 1.3 for all external communications including API endpoints and peer-to-peer coordination
- **Key Management**: Secure key rotation with automated certificate management and cryptographic material isolation

### **Authentication & Authorization**
- **API Security**: JWT-based authentication with HS256 signatures and configurable token expiration policies
- **Peer Authentication**: Mutual TLS authentication for inter-node communications with certificate-based identity verification
- **Service Authorization**: Role-based access control for administrative endpoints and sensitive operations
- **Input Validation**: Comprehensive input sanitization and schema validation preventing injection attacks

### **Network Security**
- **Kafka Security**: SASL/SSL authentication with encrypted broker connections and topic-level access controls
- **Redis Security**: Authentication-required Redis connections with encrypted data storage and access logging
- **API Rate Limiting**: Request rate limiting and DDoS protection for all public-facing endpoints
- **Network Isolation**: Service operates within secured network segments with firewall-controlled access

### **Operational Security**
- **Fail-Closed Principle**: Service fails securely by denying requests when security components are unavailable
- **Audit Logging**: Comprehensive security event logging with tamper-evident log storage and monitoring
- **Secret Management**: Encrypted secret storage with environment-based key derivation and access controls
- **Security Monitoring**: Real-time security event detection with automated incident response capabilities

### **Compliance & Hardening**
- **Data Protection**: Sensitive data encryption meeting enterprise security standards with secure deletion
- **Access Controls**: Principle of least privilege with role-based permissions and activity monitoring
- **Vulnerability Management**: Regular security assessments and dependency vulnerability scanning
- **Incident Response**: Automated security incident detection with configurable response procedures

---

