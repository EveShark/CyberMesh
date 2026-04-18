# OpenFGA Tuple Lifecycle

Status: Approved for Phase 0
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: `PRD_SECURITY_IDENTITY_AUTHZ_TENANCY_AUDIT.md`

## 1. Purpose
This document freezes how OpenFGA relationship tuples are owned, written, revoked, audited, and reconciled. Tuples are runtime authorization state and must never be treated as deploy-time configuration outside dev bootstrap.

## 2. Core Rules
1. OpenFGA tuples are runtime state.
2. `security/policies/openfga/dev_seed_tuples.json` is allowed for local bootstrap only.
3. Each tuple family has exactly one source-of-truth writer.
4. Tuple writes and deletes must be idempotent.
5. Tuple mutations must be audited.
6. Authorization enforcement fails closed if required tuples are missing or stale.

## 3. Object Families and Ownership
| Object family | Example tuples | Source of truth | Writer service | Notes |
| --- | --- | --- | --- | --- |
| access membership | `user:u1 member access:a1` | Customer/user membership state in platform app | backend control plane | Driven by access membership changes |
| access roles | `user:u1 admin access:a1` | Customer role assignment state | backend control plane | Role updates must remove old role tuples |
| support delegation | `user:u1 support_delegate access:a1` | Delegated support grant object | backend control plane | Time-bound and auditable |
| platform admin | `user:u1 admin platform:cybermesh` | Internal admin assignment state | backend security admin flow | Separate from access admin |
| access-owned resource linkage | `policy:p1 access access:a1` | Resource metadata in app DB | backend control plane | Resource creation/removal drives tuple lifecycle |
| workflow linkage | `workflow:w1 access access:a1` | Resource metadata in app DB | backend control plane | Immutable access binding after creation |
| anomaly linkage | `anomaly:a1 access access:a1` | AI/backend persisted metadata | backend control plane publish ingest path | AI emits business artifacts, backend owns authorization tuple writes |
| evidence linkage | `evidence:e1 access access:a1` | AI/backend persisted metadata | backend control plane publish ingest path | AI emits business artifacts, backend owns authorization tuple writes |

## 4. Write Triggers
Tuple writes occur on:
1. access onboarding
2. user membership assignment
3. user role change
4. support delegation approval
5. support delegation revocation or expiry processing
6. resource creation for access-owned objects
7. resource deletion or archival when authorization surface is removed

## 5. Delete and Revoke Triggers
Tuple deletes occur on:
1. membership removal
2. role removal or role replacement
3. support delegation expiry or revocation
4. resource deletion when object authorization should disappear
5. access deprovisioning

Delete behavior requirements:
- deletes must be idempotent
- role replacement must not leave overlapping stale role tuples
- revoked grants must be removed before access is considered revoked-complete

## 6. Reconciliation Rules
1. A reconciliation job must compare authoritative app state to OpenFGA tuple state.
2. Reconciliation must be able to repair missing tuples and remove stale tuples.
3. Reconciliation results must be auditable.
4. Reconciliation must run after incidents, migrations, or store recovery.
5. Reconciliation does not replace synchronous write-path correctness.

## 7. Failure Behavior
1. If authoritative state changes but tuple write fails, the owning service must mark the change as incomplete and retry.
2. Protected access may not rely on optimistic tuple existence after failed writes.
3. Enforcement remains fail-closed if required tuples are absent.
4. Outage handling may queue tuple updates, but queued state is not treated as authorization success.

## 8. Consistency and Caching
1. Decision caching must use bounded TTL.
2. Membership and role changes must trigger cache invalidation where available.
3. Support delegation writes and revokes must have aggressive invalidation because time-bounded access is security sensitive.
4. Model version changes must be rolled out deliberately and tracked in audit and deployment records.

## 9. Audit Requirements
Each tuple mutation must record:
- actor principal
- affected access/resource
- tuple relation
- operation type: write or delete
- request id
- decision id or change correlation id
- timestamp
- reason code for role change, delegation, onboarding, or revocation

## 10. Phase 0 Frozen Decisions
1. Backend control plane is the single writer for all OpenFGA tuple mutations in v1.
2. AI service never writes OpenFGA tuples directly; it publishes artifacts to backend-owned ingest paths that write tuples.
3. Support delegation is modeled as a first-class DB object plus derived tuples.
4. Reconciliation runs on a scheduled cadence of every 15 minutes and on-demand after incident recovery, with Platform Security as operational owner.
