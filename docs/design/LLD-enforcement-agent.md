# CyberMesh Enforcement Agent - Low-Level Design (LLD)

**Version:** 2.0.0  
**Last Updated:** 2026-01-30

---

## 📑 Navigation

**Quick Links:**
- [🏗️ Module Architecture](#2-module-architecture)
- [🎛️ Controller](#3-controller-internalcontroller)
- [⚙️ Enforcer](#4-enforcer-internalenforcer)
- [📋 Policy Spec](#5-policy-specification-internalpolicy)
- [✅ ACK System](#6-ack-system-internalack)

---

## 1. Overview

The Enforcement Agent is a **Go-based DaemonSet** that consumes policy messages from Kafka and enforces them using iptables, nftables, or Kubernetes NetworkPolicies.

> [!IMPORTANT]
> The agent runs as a **DaemonSet** with `hostNetwork: true` and requires `NET_ADMIN` capability for iptables/nftables access.

---

## 2. Module Architecture

### 2.1 Package Structure

```mermaid
graph TB
    subgraph cmd["cmd/"]
        main[main.go<br/>Entry point]
    end
    
    subgraph internal["internal/"]
        controller[controller/]
        enforcer[enforcer/]
        kafka[kafka/]
        policy[policy/]
        ack[ack/]
        ratelimit[ratelimit/]
        reconciler[reconciler/]
        scheduler[scheduler/]
        state[state/]
        config[config/]
        metrics[metrics/]
        control[control/]
        ledger[ledger/]
    end
    
    main --> controller
    main --> config
    main --> kafka
    main --> metrics
    main --> state
    main --> enforcer
    main --> reconciler
    main --> scheduler
    main --> control
    main --> ledger

    kafka --> controller
    controller --> policy
    controller --> enforcer
    controller --> ratelimit
    controller --> ack
    reconciler --> state
    scheduler --> state
    
    classDef entry fill:#e3f2fd,stroke:#1565c0,color:#000;
    classDef module fill:#fff9c4,stroke:#f57f17,color:#000;
    
    class main entry;
    class controller,enforcer,kafka,policy,ack,ratelimit,reconciler,scheduler,state,config,metrics,control,ledger module;
```

---

## 3. Controller (`internal/controller/`)

### 3.1 Class Diagram

```mermaid
classDiagram
    class Controller {
        -trust TrustedKeys
        -store Store
        -enforcer Enforcer
        -rate Coordinator
        -killSwitch KillSwitch
        -acks Publisher
        +HandleMessage(ctx, msg) error
    }
    
    class FastPath {
        +EvaluateFastPath(spec, cfg) FastPathEligibility
    }
    
    Controller --> FastPath
```

### 3.2 Control Loop

```mermaid
flowchart TB
    subgraph Input
        K[Kafka Message<br/>control.policy.v1]
    end
    
    subgraph Validation
        U[Unmarshal Protobuf]
        V[Verify Signature<br/>Hash and Trusted Key]
        P[Parse PolicySpec<br/>from rule_data]
    end
    
    subgraph Decision
        D[Dedupe Check<br/>policy_id, scope, rule_hash]
        A{Approval required?}
        G[Guardrails<br/>allowlist, rate limits, cooldowns]
    end
    
    subgraph Execution
        EN[Enforcer.Apply]
        ST[Persist state<br/>Store.Upsert]
    end
    
    subgraph Output
        ACK[Ack Publisher<br/>optional]
    end
    
    K --> U --> V --> P --> D --> A
    A -->|Yes| HOLD[Persist pending<br/>approval] --> END[Wait]
    A -->|No| G --> EN --> ST --> ACK
    
    style V fill:#e3f2fd,stroke:#1565c0,color:#000;
    style EN fill:#c8e6c9,stroke:#2e7d32,color:#000;
    style HOLD fill:#fff3e0,stroke:#f57f17,color:#000;
```

---

## 4. Enforcer (`internal/enforcer/`)

### 4.1 Backend Interface

```mermaid
classDiagram
    class Enforcer {
        <<interface>>
        +Apply(policy) error
        +Remove(policyID) error
        +List() []Policy
    }
    
    class IPTablesEnforcer {
        -chain string
        +Apply(policy) error
        +Remove(policyID) error
    }
    
    class NFTablesEnforcer {
        -table string
        -chain string
        +Apply(policy) error
        +Remove(policyID) error
    }
    
    class KubernetesEnforcer {
        -client kubernetes.Interface
        -namespace string
        +Apply(policy) error
        +Remove(policyID) error
    }
    
    Enforcer <|.. IPTablesEnforcer
    Enforcer <|.. NFTablesEnforcer
    Enforcer <|.. KubernetesEnforcer
```

### 4.2 Backend Selection

```mermaid
flowchart TB
    START[Start] --> CFG{ENFORCEMENT_BACKEND}
    CFG -->|iptables| IPT[iptables Enforcer]
    CFG -->|nftables| NFT[nftables Enforcer]
    CFG -->|kubernetes/k8s| K8S[Kubernetes Enforcer]
    CFG -->|noop| NOOP[No-op Enforcer]
    
    style IPT fill:#e3f2fd,stroke:#1565c0,color:#000;
    style NFT fill:#fff9c4,stroke:#f57f17,color:#000;
    style K8S fill:#c8e6c9,stroke:#2e7d32,color:#000;
```

---

## 5. Policy Specification (`internal/policy/`)

### 5.1 Policy Parser

```mermaid
classDiagram
    class PolicySpec {
        +ID string
        +RuleType string
        +Action string
        +Target Target
        +Criteria Criteria
        +Guardrails Guardrails
        +Audit Audit
        +Tenant string
        +Region string
    }
    
    class Target {
        +IPs []string
        +CIDRs []string
        +Direction string
        +Scope string
        +Selectors map
        +Namespace string
        +Ports []PortRange
        +Protocols []string
        +Tenant string
        +Region string
    }
    
    class Criteria {
        +MinConfidence *float64
        +AttemptsPerWindow *int64
        +WindowSeconds *int64
    }

    class Guardrails {
        +TTLSeconds int64
        +DryRun bool
        +MaxTargets *int64
        +CIDRMaxPrefixLen *int64
        +ApprovalRequired bool
        +MaxActivePolicies *int64
        +MaxPoliciesPerMinute *int64
        +RateLimit *RateLimit
        +RateLimitPerTenant *int64
        +RateLimitPerRegion *int64
        +AllowlistIPs []string
        +AllowlistCIDRs []string
        +AllowlistNamespaces []string
        +RollbackIfNoCommitAfter *int64
        +PreConsensusTTLSeconds *int64
        +FastPathEnabled bool
        +FastPathTTLSeconds *int64
        +FastPathSignalsRequired *int64
        +FastPathConfidenceMin *float64
        +EscalationCooldown *int64
    }
    
    PolicySpec --> Target
    PolicySpec --> Criteria
    PolicySpec --> Guardrails
```

### 5.2 Policy Types

| Type | Action | Example |
|------|--------|---------|
| `block` | `DROP`/`REJECT` | Block IP/CIDR or selector-scoped targets |

---

## 6. ACK System (`internal/ack/`)

### 6.1 Acknowledgment Flow

```mermaid
sequenceDiagram
    participant C as Controller
    participant B as BatchingPublisher
    participant R as RetryingPublisher
    participant Q as Durable Queue (bbolt)
    participant KP as KafkaPublisher
    participant Kafka as control.policy.ack.v1
    
    C->>R: Publish(payload)
    R->>Q: Enqueue(payload)
    
    opt Batching enabled
        C->>B: Publish(payload)
        B->>R: PublishBatch(payloads)
    end
    
    loop Background drain
        R->>Q: Peek()
        R->>KP: Publish(payload)
        KP->>Kafka: Produce message
        R->>Q: Delete(id)
    end
```

### 6.2 ACK Publisher

```mermaid
classDiagram
    class Publisher {
        <<interface>>
        +Publish(ctx, payload) error
        +PublishBatch(ctx, payloads) error
        +Close(ctx) error
    }
    
    class KafkaPublisher {
        -producer SyncProducer
        -topic string
        -signer Signer
        +Publish(ctx, payload) error
    }
    
    class RetryingPublisher {
        -queue Queue
        -backend Publisher
        +Publish(ctx, payload) error
    }

    class BatchingPublisher {
        -backend Publisher
        +Publish(ctx, payload) error
    }
    
    Publisher <|.. KafkaPublisher
    Publisher <|.. RetryingPublisher
    Publisher <|.. BatchingPublisher
    RetryingPublisher --> KafkaPublisher
```

---

## 7. Rate Limiting (`internal/ratelimit/`)

### 7.1 Rate Limiter Types

```mermaid
classDiagram
    class Coordinator {
        <<interface>>
        +Reserve(ctx, scope, window, limit, now) Reservation
    }
    
    class Reservation {
        <<interface>>
        +Commit(ctx) error
        +Release(ctx) error
        +Count() int64
    }
    
    class LocalCoordinator {
        -counter ScopedCounter
        +Reserve(...) Reservation
    }
    
    class RedisCoordinator {
        -client RedisClient
        -scriptSha string
        +Reserve(...) Reservation
    }
    
    Coordinator <|.. LocalCoordinator
    Coordinator <|.. RedisCoordinator
    RedisCoordinator --> Reservation
    LocalCoordinator --> Reservation
```

---

## 8. State Store (`internal/state/`)

### 8.1 Local State

```mermaid
classDiagram
    class StateStore {
        -records map~string~Record
        -history map~string~[]time
        -pending map~string~[]byte
        -persistPath string
        -lockPath string
        +Load() error
        +Upsert(spec, appliedAt) error
        +Remove(policyID) error
        +Expired(now) []Record
        +ReconcileLedger(snapshot, appliedAt) error
        +SavePendingApproval(key, payload) error
        +PendingApproval(key) []byte, bool
    }
    
    class Record {
        +Spec PolicySpec
        +AppliedAt time
        +ExpiresAt time
        +PreConsensus time
        +Rollback time
        +PendingConsensus bool
    }
    
    StateStore --> Record
```

---

## 9. iptables/nftables Commands

### 9.1 iptables Example

```bash
# Block source IP
iptables -A INPUT -s 1.2.3.4 -j DROP

# Rate limit
iptables -A INPUT -p tcp --dport 80 -m limit --limit 100/sec -j ACCEPT
iptables -A INPUT -p tcp --dport 80 -j DROP

# Remove rule (by line number)
iptables -D INPUT 5
```

### 9.2 nftables Example

```bash
# Block source IP
nft add rule ip cybermesh input ip saddr 1.2.3.4 drop

# Rate limit
nft add rule ip cybermesh input tcp dport 80 limit rate 100/second accept

# Remove rule (by handle)
nft delete rule ip cybermesh input handle 15
```

---

## 10. Kubernetes NetworkPolicy

### 10.1 Generated Policy

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: cybermesh-block-1-2-3-4
  namespace: default
spec:
  podSelector: {}
  policyTypes:
    - Ingress
  ingress:
    - from:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 1.2.3.4/32
```

---

## 11. Key Files Reference

| File | Purpose | Lines |
|------|---------|-------|
| `cmd/agent/main.go` | Entrypoint wiring | ~440 |
| `internal/config/config.go` | Env config parsing | ~320 |
| `internal/kafka/consumer.go` | Kafka consumer-group wrapper | ~220 |
| `internal/controller/controller.go` | Policy handler | ~770 |
| `internal/enforcer/enforcer.go` | Backend factory + interfaces | ~120 |
| `internal/enforcer/iptables/iptables_linux.go` | iptables backend | ~240 |
| `internal/enforcer/nftables/nftables_linux.go` | nftables backend | ~520 |
| `internal/enforcer/kubernetes/kubernetes.go` | Kubernetes NetworkPolicy backend | ~520 |
| `internal/state/store.go` | Persistent state store | ~690 |
| `internal/reconciler/reconciler.go` | Re-apply + ledger reconciliation | ~250 |
| `internal/scheduler/scheduler.go` | TTL/rollback expiration loop | ~200 |
| `internal/ack/publisher.go` | Kafka ACK publisher | ~170 |
| `internal/metrics/metrics.go` | Prometheus metrics | ~390 |

---

## 12. Related Documents

### Design Documents
- [HLD](./HLD.md) - High-level design
- [Data Flow](./DATA_FLOW.md) - System data flow

### Source Code
- [Build Summary](../../enforcement-agent/BUILD_SUMMARY.md)

---

**[⬆️ Back to Top](#-navigation)**
