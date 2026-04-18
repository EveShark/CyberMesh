# Principal to Access Resolution

Status: Approved for Phase 0
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: `PRD_SECURITY_IDENTITY_AUTHZ_TENANCY_AUDIT.md`

## 1. Purpose
This document freezes the principal-to-access resolution rules used by CyberMesh AuthN, AuthZ, and access isolation. It defines how a verified principal is normalized into access context before any authorization or access-bound data access is allowed.

All shared contracts derived from this model must carry an explicit `schema_version`.

## 2. Scope
In scope:
- user principals authenticated by ZITADEL
- service principals authenticated by workload identity in later phases
- access-bound resources
- global/system resources
- delegated support access
- break-glass interaction rules

Out of scope:
- runtime SDK implementation details
- SPIFFE deployment details
- UI/UX for access selection

## 3. Canonical Security IDs
These IDs are the Phase 0 security identifiers and must not overlap semantically.

The canonical model must keep business IDs and correlation IDs separate from security access IDs. Phase 0 does not overload security vocabulary with product runtime names.

| Field | Meaning | Source | Notes |
| --- | --- | --- | --- |
| `principal_id` | Canonical subject of the caller | ZITADEL `sub` for users; SPIFFE ID for services in later phases | Reuse existing authoritative subject identifiers |
| `principal_type` | Caller class | Derived from verified identity | Allowed values: `user`, `service` |
| `access_id` | Canonical authorized access boundary identifier | Derived from authoritative business tenant/org/access boundary | Security-layer name, not raw runtime field name |
| `allowed_access_ids[]` | Authorized access set for the principal | Verified claims, delegation grants, or workload binding | Authority comes from identity/grant state |
| `active_access_id` | Access boundary selected for the current request | Resolution algorithm in this document | Required for access-bound resources |
| `delegation_id` | Identifier for delegated support grant | New security object when support delegation exists | Not required for ordinary user access |
| `resource_type` | Authorized resource class | Business object type | Examples: `policy`, `workflow`, `audit_scope` |
| `resource_id` | Authorized object identifier | Existing resource ID for that object | Reuse existing business IDs |
| `action` | Authorized operation | Application action vocabulary | Examples: `read`, `write`, `apply`, `approve` |
| `decision_id` | Unique authorization decision correlation ID | New generated security ID | Used in decision and audit records |
| `audit_event_id` | Unique audit event identifier | New generated security ID | One ID per emitted event |
| `request_id` | Request correlation identifier | Existing request correlation path | Must remain distinct from `decision_id` |

## 4. Principal Classes
### 4.1 User principal
A human user authenticated by ZITADEL. `principal_id` is the verified user subject.

### 4.2 Service principal
A workload principal authenticated by service identity in later phases. `principal_id` is the verified workload identifier. Service principals must be classified as `global` or `access-bound`.

### 4.3 Internal admin
A user principal with platform-internal administration privileges. Internal admin status does not bypass access resolution for access-bound resources.

### 4.4 Delegated support principal
A user principal operating under an explicit time-bound support delegation grant. Delegated support must always be reason-bound and access-bound.

## 5. Resource Classes
### 5.1 Access-bound resource
A resource whose access must always resolve to a concrete access boundary.
Examples:
- policy
- workflow
- anomaly
- evidence
- enforcement command targeted at customer resources
- access-scoped audit data

### 5.2 Global/system resource
A resource intentionally modeled as platform-global.
Examples:
- global platform configuration approved as global in policy
- platform audit scope reserved for privileged internal access
- tenant directory metadata approved as global

Global/system access is never an implicit fallback when access resolution fails.

## 6. Claim and Grant Inputs
The following normalized inputs feed resolution:
- verified subject claims from ZITADEL
- verified group/role/internal-admin claims from ZITADEL
- verified access membership claims from ZITADEL
- explicit support delegation grant metadata
- verified service principal binding metadata for access-bound services
- explicit request access selector when present

Phase 0 freezes the normalized ZITADEL claim contract as:
- `sub`: canonical user subject -> `principal_id`
- `urn:cybermesh:access_ids`: string array of authorized access IDs -> `allowed_access_ids[]`
- `urn:cybermesh:internal_admin`: boolean -> `is_internal_admin`
- `urn:cybermesh:support_eligible`: boolean -> caller may request delegated support access, but does not itself grant access

Support delegation is not granted by token claims alone. Support delegation grants are first-class application objects looked up at request time and populate `is_support_delegated`, `delegation_id`, and access scope only within the grant's authorized bounds.

## 7. Resolution Algorithm
1. Verify caller identity and normalize `principal_id` and `principal_type`.
2. Build `allowed_access_ids[]` from verified claims, verified grants, or verified workload bindings.
3. Determine whether the requested resource is access-bound or global/system.
4. If a valid support delegation grant is present, resolve `active_access_id` from that grant's access scope.
5. Otherwise, if the request includes an access selector, accept it only if it is contained in `allowed_access_ids[]`.
6. Otherwise, if exactly one access ID exists in `allowed_access_ids[]`, use that access ID as `active_access_id`.
7. Otherwise, leave `active_access_id` unresolved.
8. If the resource is access-bound and `active_access_id` is unresolved, deny.
9. If the resource is global/system, require explicit global authorization and set `is_global_resource_request=true`.
10. Record `access_resolution_reason` as one of `claim_single_access`, `selector`, `delegation`, or `system_global`.

## 8. Selector Rules
1. Request access selectors are advisory only.
2. Accepted selector channels may include header, query, or body only if the API explicitly allows them.
3. Selectors can narrow access to an already authorized access boundary; they can never expand access.
4. Selector parsing happens after AuthN and before AuthZ.
5. Invalid, missing, or unauthorized selectors on access-bound requests result in deny.

## 9. Deny Rules
Deny the request when any of the following occurs:
1. Identity is not verified.
2. `allowed_access_ids[]` cannot be derived for an access-bound resource.
3. Multiple access IDs are authorized and no valid selector resolves an active access ID.
4. The selector access ID is not in `allowed_access_ids[]`.
5. The request targets an access-bound resource and no concrete `active_access_id` exists.
6. An internal admin attempts access-bound access without a concrete access ID.
7. A support delegation grant is missing, expired, revoked, or outside authorized access scope.
8. An access-bound service principal attempts an operation without explicit access context.
9. A global service principal attempts access-bound access without policy-authorized access context.
10. The request claims global access but the resource is not declared global by policy.

## 10. Principal-Specific Rules
### 10.1 Single-access user
- `allowed_access_ids[]` contains exactly one access ID.
- If no selector is provided, that access ID becomes `active_access_id`.
- If a selector is provided and does not match the sole allowed access ID, deny.

### 10.2 Multi-access user
- `allowed_access_ids[]` contains multiple access IDs.
- A valid per-request selector is required.
- No implicit default access ID is assumed.

### 10.3 Internal admin
- Internal admin status grants eligibility for privileged actions but does not remove access requirements.
- Internal admin access to access-bound resources must still resolve `active_access_id`.
- Internal admin access to global/system resources requires explicit global authorization.

### 10.4 Delegated support
- Delegation must include `delegation_id`, approver, reason, start time, expiry, and access scope.
- Delegation can reduce or replace access for the duration of the grant, but cannot expand beyond the approved grant scope.
- Expired or revoked delegations deny immediately.

### 10.5 Service principal: global
- The principal is allowed to act across platform-level operations defined by workload policy.
- Access to access-bound resources still requires explicit access context and action-specific authorization.

### 10.6 Service principal: access-bound
- The principal is pre-bound to one access ID or a constrained access set.
- Every protected access-bound operation must validate the carried access context against that binding.

## 11. Output Contract
The normalized access context must expose at least:
- `principal_id`
- `principal_type`
- `allowed_access_ids[]`
- `active_access_id`
- `access_source`
- `is_internal_admin`
- `is_support_delegated`
- `delegation_id`
- `is_global_resource_request`

## 12. Mapping Rule
In v1, `access_id` maps to the existing business tenant/org boundary used by the product. The security layer uses `access_*` names to avoid overloading business/runtime field names like `tenant` and `tenant_id`.

## 13. Phase 0 Frozen Decisions
1. ZITADEL user claims are normalized from `sub`, `urn:cybermesh:access_ids`, `urn:cybermesh:internal_admin`, and `urn:cybermesh:support_eligible`.
2. Multi-access selection is per request in v1.
3. Support delegation and break-glass are first-class application grant objects stored and validated outside the token.
4. Global/system resources in v1 are limited to `platform_config`, `global_audit_scope`, and `tenant_directory`.
5. Break-glass is disabled by default in v1 and may be enabled only through explicit operational configuration and audit controls.
