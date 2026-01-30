# CyberMesh

Distributed threat detection and policy enforcement using cryptographic signing and BFT consensus.

This repository contains multiple services (Go/Python/TypeScript) plus architecture/design documentation.
The deployment manifests under `k8s_gke/` are the source of truth for the current Kubernetes topology.

## Table of Contents

1. Project Overview
2. What CyberMesh Does (Problem -> Solution)
3. Key Concepts (Anomaly, Evidence, Policy, Commit, Validator)
4. Architecture (Quick)
   - System Overview
   - Core Components
   - Data Flow (E2E)
5. Services
   - Backend Validators (Go)
   - AI Service (Python)
   - Enforcement Agent (Go)
   - Frontend (React/TypeScript)
6. Protobuf / Wire Contracts
7. Signing / Verification
8. Consensus (HotStuff 2-chain)
9. State Machine (Deterministic Execution + State Root)
10. Feedback Loop (Lifecycle + Calibration)
11. Deployment (GKE / `k8s_gke/`)
12. Local Development (Dev Workflow)
13. Observability
14. Quick Access
15. Prerequisites
16. Deployment
17. Secrets & Credentials

## 1. Project Overview

CyberMesh is a distributed platform that turns raw telemetry into signed detections, reaches validator consensus on those detections, and pushes enforcement policies to agents and infrastructure.

## 2. What CyberMesh Does (Problem -> Solution)

Security detections are noisy and easy to spoof. CyberMesh addresses this by:

- cryptographically signing detections/evidence at the producer,
- validating and reaching BFT consensus on what is accepted,
- producing policy commits that enforcement agents can apply consistently.

## 3. Key Concepts

- Anomaly: a detection event produced by the AI service from telemetry/features.
- Evidence: supporting data used to justify an anomaly (features/summaries) and enable verification.
- Policy: a desired enforcement action or configuration derived from accepted detections.
- Commit: a consensus-accepted decision (and its ordering) produced by the validator set.
- Validator: a backend node participating in consensus and deterministic execution.

## 4. Architecture (Quick)

### System Overview

High-level E2E flow (conceptual):

```text
Telemetry -> AI Service -> (signed protobuf messages) -> Kafka -> Validators (HotStuff)
         -> deterministic state machine -> (commits/policies) -> Kafka -> Enforcement Agent
                                                         -> Frontend queries Validators
```

### Core Components

- AI Service: produces signed anomalies/evidence; consumes validator feedback to calibrate thresholds.
- Backend Validators: verify signatures, order inputs via HotStuff 2-chain, execute deterministically, persist state.
- Enforcement Agent: consumes policy commits and applies enforcement on nodes / infrastructure surfaces.
- Frontend: operator UI for monitoring, review, and querying system state.

### Data Flow (E2E)

For the detailed, code-aligned E2E flow, see [DATA_FLOW.md](docs/design/DATA_FLOW.md).

## 5. Services

CyberMesh consists of 4 main services:

- [Backend Validators](backend/README.md) - Go service implementing HotStuff consensus and state machine
- [AI Service](ai-service/README.md) - Python ML pipeline for threat detection with ensemble voting
- [Enforcement Agent](enforcement-agent/README.md) - Go DaemonSet applying policies via iptables/nftables/K8s
- [Frontend](cybermesh-frontend/README.md) - React/TypeScript dashboard for monitoring

## 6. Protobuf / Wire Contracts

CyberMesh uses Protobuf for cross-service wire contracts (messages carried over Kafka and internal APIs).

- [Kafka Message Bus](docs/architecture/04_kafka_message_bus.md) - Topic architecture and message schemas
- [HLD](docs/design/HLD.md) - High-level system design and data flow

## 7. Signing / Verification

CyberMesh uses Ed25519 signatures to authenticate messages and mitigate spoofing/replay at the edges.

- [Security Model](docs/architecture/09_security_model.md) - Cryptographic signing and verification architecture

## 8. Consensus (HotStuff 2-chain)

The backend validator set orders and finalizes inputs using HotStuff 2-chain BFT (typical deployment uses 5 validators).

- [HotStuff Consensus](docs/architecture/03_hotstuff_consensus.md) - BFT consensus protocol implementation

## 9. State Machine (Deterministic Execution + State Root)

Accepted inputs are applied through deterministic execution to produce a verifiable state root.

- [State Machine](docs/architecture/05_state_machine.md) - Deterministic execution and state verification

## 10. Feedback Loop (Lifecycle + Calibration)

Validator outcomes drive feedback to the AI service to adjust thresholds/calibration over time.

- [Feedback Loop](docs/architecture/06_feedback_loop.md) - Anomaly lifecycle and threshold calibration

## 11. Deployment (GKE / `k8s_gke/`)

Kubernetes manifests (namespaces, services, workloads) live in `k8s_gke/`.

- [DEPLOYMENT](docs/design/DEPLOYMENT.md) - Complete deployment guide with manifest application order
- [GKE Deployment](docs/architecture/12_gke_deployment.md) - Deployment topology and architecture

## 12. Local Development (Dev Workflow)

Local workflows vary by service. Start with the service README:

- [Backend Validators](backend/README.md) - Go development setup and workflow
- [AI Service](ai-service/README.md) - Python environment and model training
- [Enforcement Agent](enforcement-agent/README.md) - Go DaemonSet local testing
- [Frontend](cybermesh-frontend/README.md) - React/TypeScript development server

## 13. Observability

Services expose health/metrics endpoints and are wired in Kubernetes manifests.

- [DEPLOYMENT](docs/design/DEPLOYMENT.md) - Health probes and metrics configuration

## 14. Quick Access

### 📊 Live System
- **Frontend Dashboard:** https://cybermesh.qzz.io
- **Backend API:** https://api.cybermesh.qzz.io

### 🏗️ Architecture Documentation

**Entry Points:**
- [ARCHITECTURE.md](docs/ARCHITECTURE.md) - Architecture portal
- [ARCHITECTURE_INDEX.md](docs/ARCHITECTURE_INDEX.md) - Complete index

**Deep Dives:**
- [System Overview](docs/architecture/01_system_overview.md)
- [AI Detection Pipeline](docs/architecture/02_ai_detection_pipeline.md)
- [HotStuff Consensus](docs/architecture/03_hotstuff_consensus.md)
- [Kafka Message Bus](docs/architecture/04_kafka_message_bus.md)
- [State Machine](docs/architecture/05_state_machine.md)
- [Feedback Loop](docs/architecture/06_feedback_loop.md)
- [Genesis Bootstrap](docs/architecture/07_genesis_bootstrap.md)
- [CockroachDB Persistence](docs/architecture/08_cockroach_persistence.md)
- [Security Model](docs/architecture/09_security_model.md)
- [P2P Networking](docs/architecture/10_p2p_networking.md)
- [System Timeline](docs/architecture/11_system_timeline.md)
- [GKE Deployment](docs/architecture/12_gke_deployment.md)

### 📐 Design Documentation
- [HLD](docs/design/HLD.md) - High-level design
- [Data Flow](docs/design/DATA_FLOW.md) - End-to-end system flow
- [Deployment](docs/design/DEPLOYMENT.md) - GKE deployment guide
- [Backend LLD](docs/design/LLD-backend.md) - Backend validators
- [AI Service LLD](docs/design/LLD-ai-service.md) - Detection pipeline
- [Enforcement Agent LLD](docs/design/LLD-enforcement-agent.md) - Policy enforcement
- [Frontend LLD](docs/design/LLD-frontend.md) - Dashboard UI

### 🔧 Service READMEs
- [Backend Validators](backend/README.md) - Go service
- [AI Service](ai-service/README.md) - Python ML pipeline
- [Enforcement Agent](enforcement-agent/README.md) - Go DaemonSet
- [Frontend](cybermesh-frontend/README.md) - React/TypeScript

---

## 15. Prerequisites

To work with CyberMesh, you need:

- Access to GKE cluster in `us-central1`
- `kubectl` configured for the `cybermesh` namespace
- Access to Confluent Cloud Kafka cluster
- Access to CockroachDB Cloud instance
- Network access to deployment environments

---

## 16. Deployment

Kubernetes manifests are in `k8s_gke/`.

**For deployment procedures and topology, see:**
- [DEPLOYMENT.md](docs/design/DEPLOYMENT.md) - Complete deployment guide with manifest application order
- [GKE Deployment Architecture](docs/architecture/12_gke_deployment.md) - Topology and architecture

---

## 17. Secrets & Credentials

All production credentials are managed via Kubernetes secrets in the `cybermesh` namespace.

**See:** [DEPLOYMENT.md - Configuration & Secrets](docs/design/DEPLOYMENT.md#6-configuration-and-secrets) for details.

**⚠️ Never commit secrets to this repository.**

