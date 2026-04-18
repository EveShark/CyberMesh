# Workload Authorization Matrix

Status: Approved for Phase 0
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: `PRD_SECURITY_IDENTITY_AUTHZ_TENANCY_AUDIT.md`

## 1. Purpose
This document freezes the workload-level authorization model that sits on top of workload identity. It defines what protected operations each workload class may perform after workload identity verification succeeds.

## 2. Core Rules
1. Workload identity alone is not authorization.
2. Every protected service-to-service call must map workload identity to an allowed action/resource set.
3. Workloads must be classified as `global` or `access-bound`.
4. Access-bound workloads must carry explicit access context on access-bound operations.
5. Global workloads still need explicit access context when operating on access-bound resources.
6. Protected service-to-service operations are deny-by-default.
7. Workload policy decisions may include obligations or expiry constraints that callers must enforce.

## 3. Initial Workload Classes
| Workload class | Example future identity | Scope class | Notes |
| --- | --- | --- | --- |
| backend-control-plane | `spiffe://cybermesh.internal/ns/platform/sa/backend` | global | Main policy enforcement point and tuple writer |
| enforcement-agent | `spiffe://cybermesh.internal/ns/runtime/sa/enforcement-agent` | access-bound for customer-targeted actions; may also execute global platform actions if explicitly approved later | Accepts commands only from authorized workloads |
| ai-service | `spiffe://cybermesh.internal/ns/platform/sa/ai-service` | global service with access-scoped publish operations | Must not implicitly inherit access context |
| audit-pipeline | `spiffe://cybermesh.internal/ns/platform/sa/audit-pipeline` | global | Append-only audit transport and verification path |

Exact SPIFFE IDs and trust domain are frozen in deployment/security design, but this matrix freezes the action model and scope semantics.

## 4. Protected Operations Matrix
| Caller workload | Target service/path class | Allowed actions | Access rules |
| --- | --- | --- | --- |
| backend-control-plane | backend internal tuple writer path | `write_tuple`, `delete_tuple`, `reconcile_tuple` | Access context required when tuple targets access-bound object |
| backend-control-plane | enforcement command ingress | `issue_command`, `reject_command` | Access context required for access-bound command targets |
| backend-control-plane | ai-service protected ingest/publish path | `request_analysis`, `receive_publish` | Access context explicit and validated per operation |
| enforcement-agent | backend ACK/reject ingest path | `publish_ack`, `publish_reject`, `report_outcome` | Access context required when command/resource is access-bound |
| ai-service | backend protected publish path | `publish_anomaly`, `publish_evidence`, `publish_policy_signal` | Access context required for all access-bound outputs |
| audit-pipeline | audit append/verify path | `append_audit`, `verify_chain` | No access expansion; source access metadata preserved from source event |

## 5. Explicit Denies
1. `enforcement-agent` may not issue customer authorization tuple changes.
2. `ai-service` may not mutate access membership or role relationships.
3. `backend-control-plane` may not omit access context when targeting access-bound downstream resources.
4. Any workload with a valid identity but no action mapping is denied.
5. Any workload attempting an access-bound operation with mismatched or missing access context is denied.

## 6. Caller-Specific Rules
### 6.1 backend-control-plane
- Acts as the primary service writer for membership, role, and resource-link tuples unless ownership is explicitly delegated.
- May invoke enforcement-agent only through approved command actions.
- May call AI service only through enumerated service APIs.

### 6.2 enforcement-agent
- Accepts commands only from allowed workload identities.
- Must validate command action, target class, and access/resource alignment before execution.
- May emit ACK/reject/outcome only to approved backend paths.

### 6.3 ai-service
- May publish only enumerated artifact types.
- Must include explicit access context when publishing access-bound artifacts.
- Must not treat upstream access context as trusted unless the receiving service revalidates it.

### 6.4 audit-pipeline
- May append and verify audit events.
- Must not change actor, access, action, or decision semantics in source events.

## 7. Required Protected Path Inventory Before Phase 1 Implementation
1. backend -> OpenFGA tuple management path
2. backend -> enforcement command path
3. backend -> ai-service request path
4. enforcement-agent -> backend ACK/reject path
5. ai-service -> backend publish path
6. audit append/verify path

## 8. Phase 0 Frozen Decisions
1. Trust domain is `cybermesh.internal`.
2. Initial service-account-to-workload mappings are frozen in `security/docs/spiffe_ids.md`.
3. No workload other than backend-control-plane may own tuple writes in v1.
4. Enforcement-agent has no platform-global action surface in v1 beyond explicitly authorized command execution and outcome reporting.
5. AI publish actions in v1 are `publish_anomaly`, `publish_evidence`, and `publish_policy_signal`.
