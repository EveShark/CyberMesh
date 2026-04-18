# SPIFFE ID Catalog

Status: Approved for Phase 0
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: `security/docs/workload_authorization_matrix.md`

## 1. Purpose
This document freezes the initial SPIFFE ID naming scheme used by CyberMesh workloads. Exact deployment bindings may evolve, but the namespace and workload naming pattern are fixed here so service authorization can be designed against stable identifiers.

## 2. Canonical Pattern
`spiffe://<trust-domain>/ns/<namespace>/sa/<service-account>`

## 3. Initial IDs
| Workload | SPIFFE ID |
| --- | --- |
| backend-control-plane | `spiffe://cybermesh.internal/ns/platform/sa/backend` |
| enforcement-agent | `spiffe://cybermesh.internal/ns/runtime/sa/enforcement-agent` |
| ai-service | `spiffe://cybermesh.internal/ns/platform/sa/ai-service` |
| audit-pipeline | `spiffe://cybermesh.internal/ns/platform/sa/audit-pipeline` |

## 4. Rules
1. SPIFFE IDs are workload identity only; authorization still comes from the workload authorization matrix.
2. Service account reuse across workloads is not allowed.
3. A new protected workload path requires a new reviewed SPIFFE ID mapping.
4. Access binding is never encoded only in the SPIFFE ID string; it is enforced by workload policy and explicit access context.
