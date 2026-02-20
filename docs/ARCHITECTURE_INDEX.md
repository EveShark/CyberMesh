# CyberMesh Documentation Index

**Version:** 1 | **Last Updated:** 2026-02-20

---

## 🏛️ Core Architecture
*The foundational specifications of the system.*

*   **[00. Runtime Defaults](architecture/00_runtime_defaults.md)** - Source-of-truth env/topic/backend defaults.
*   **[01. System Overview](architecture/01_system_overview.md)** - 🌟 **Start Here**
*   **[02. AI Detection Pipeline](architecture/02_ai_detection_pipeline.md)**
*   **[03. HotStuff Consensus](architecture/03_hotstuff_consensus.md)**
*   **[04. Kafka Message Bus](architecture/04_kafka_message_bus.md)**
*   **[05. State Machine](architecture/05_state_machine.md)**
*   **[06. Feedback Loop](architecture/06_feedback_loop.md)**
*   **[07. Genesis Bootstrap](architecture/07_genesis_bootstrap.md)**
*   **[08. Cockroach Persistence](architecture/08_cockroach_persistence.md)**
*   **[09. Security Model](architecture/09_security_model.md)**
*   **[10. P2P Networking](architecture/10_p2p_networking.md)**
*   **[11. System Timeline](architecture/11_system_timeline.md)**
*   **[12. GKE Deployment](architecture/12_gke_deployment.md)**
*   **[13. Sentinel Integration](architecture/13_sentinel_integration.md)**

---

## 📐 Design & Specifications (HLD/LLD)
*Detailed technical designs and data flows.*

*   **[High-Level Design (HLD)](design/HLD.md)** - Logical architecture & containers.
*   **[Data Flow Specification](design/DATA_FLOW.md)** - Sequence diagrams for all flows.
*   **[Deployment Guide](design/DEPLOYMENT.md)** - Runtime topology, images, manifests, and rollout guidance.
*   **[LLD: Backend Validators](design/LLD-backend.md)** - Class diagrams & internal logic.
*   **[LLD: AI Service](design/LLD-ai-service.md)** - Pipeline architecture.
*   **[LLD: Sentinel](design/LLD-sentinel.md)** - Gateway worker, contracts, and adapter integration.
*   **[LLD: Telemetry Layer](design/LLD-telemetry-layer.md)** - Ingest, stream, features, PCAP.
*   **[LLD: Enforcement Agent](design/LLD-enforcement-agent.md)** - Policy application.
    Includes control-plane vs data-plane boundaries, L3/L4 behavior, and East-West/North-South backend placement.
*   **[LLD: Frontend](design/LLD-frontend.md)** - Component hierarchy.

---

## 🧭 Documentation Operations

*   **[Docs Map](DOCS_MAP.md)** - Fast navigation for developers and operators.
*   **[Definition Of Done](DEFINITION_OF_DONE.md)** - System-level release-readiness checklist.
