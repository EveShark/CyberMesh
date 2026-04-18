# Security Field Mapping

Status: Approved for Phase 0
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: `PRD_SECURITY_IDENTITY_AUTHZ_TENANCY_AUDIT.md`

This document freezes the one allowed translation boundary between existing product fields and the Phase 0 security contracts.

## Canonical security fields

| Security contract field | Meaning |
| --- | --- |
| `principal_id` | Canonical authenticated caller identifier |
| `principal_type` | Normalized caller class: `user` or `service` |
| `access_id` | Canonical access boundary identifier when asserted directly by identity or binding state |
| `allowed_access_ids[]` | Authoritative access set available to the caller |
| `active_access_id` | Access boundary selected for the current request |
| `access_resolution_reason` | Exact rule used to resolve the active access context |
| `delegation_id` | Explicit delegated support grant identifier |
| `decision_id` | Unique authorization decision identifier |
| `audit_event_id` | Unique audit record identifier |

## Product-to-security mapping

| Existing product field | Canonical security field | Notes |
| --- | --- | --- |
| `tenant` | `access_id` or `active_access_id` | Runtime/business field name only; do not expose as security contract vocabulary |
| `tenant_id` | `access_id` or `active_access_id` | Same business boundary as `tenant`, normalized at the security boundary |
| `sub` | `principal_id` | Used for human identities after AuthN normalization |
| SPIFFE workload ID | `principal_id` | Used for service identities after workload auth normalization |
| support grant UUID | `delegation_id` | Must stay time-bound and auditable |

## Business/resource identifiers

These remain business identifiers and are not renamed into security-core fields:

- `policy_id`
- `workflow_id`
- `anomaly_id`
- `evidence_id`

## Correlation identifiers

These remain lineage/join identifiers and are not renamed into security-core fields:

- `request_id`
- `command_id`
- `trace_id`
- `source_event_id`
- `sentinel_event_id`

## Boundary rule

Only one translation is allowed:

`product/runtime fields -> security contract fields`

No chained adapters, bootstrap shims, or intermediate naming layers are allowed in the Phase 0 model.
