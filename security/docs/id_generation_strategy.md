# ID Generation Strategy

Status: Approved for Phase 0
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: `PRD_SECURITY_IDENTITY_AUTHZ_TENANCY_AUDIT.md`

## 1. Purpose
This document freezes the ID format, generation policy, maximum lengths, and prefix rules for shared security contracts.

## 2. Core Rules
1. Different IDs must not overlap semantically, even if they share a string format.
2. Existing authoritative business IDs are reused where they already exist.
3. New security-generated IDs use canonical lowercase UUID strings.
4. Generated security IDs in v1 do not use textual prefixes. The field name provides the namespace.
5. IDs must be opaque. Authorization logic must not parse business meaning out of an ID string.
6. Shared security contracts must reuse the live lineage IDs already implemented in production instead of inventing substitute names.

## 3. Generated IDs
| Field | Generation rule | Format | Max length | Prefix |
| --- | --- | --- | --- | --- |
| `request_id` | Backend-generated UUIDv7 with UUIDv4 fallback | canonical UUID string | 36 | none |
| `decision_id` | New UUIDv7 with UUIDv4 fallback | canonical UUID string | 36 | none |
| `audit_event_id` | New UUIDv7 with UUIDv4 fallback | canonical UUID string | 36 | none |
| `command_id` | Existing backend-generated UUIDv7 with UUIDv4 fallback | canonical UUID string | 36 | none |
| `workflow_id` | Existing backend-generated UUIDv7 with UUIDv4 fallback | canonical UUID string | 36 | none |
| `delegation_id` | New UUIDv7 with UUIDv4 fallback | canonical UUID string | 36 | none |

## 4. Reused Authoritative IDs
| Field | Source | Max length | Notes |
| --- | --- | --- | --- |
| `principal_id` | ZITADEL `sub` for users, SPIFFE ID for services | 255 | Do not reformat authoritative subject IDs |
| `access_id` | Security-layer name for the existing canonical tenant/org access boundary | 128 | Stable and unique per authorized access boundary |
| `resource_id` | Existing business object ID | 255 | Reuse current object IDs such as policy/workflow/anomaly/evidence IDs |
| `resource_version` | Existing object version or state token | 128 | Opaque version token, not a generated UUID by default |

## 4.1 Live lineage IDs reused directly
These are already implemented across backend, AI, enforcement, and telemetry and must stay first-class in shared contracts:

| Field | Format | Owner | Notes |
| --- | --- | --- | --- |
| `trace_id` | 32 lowercase hex | telemetry ingress or upstream | end-to-end execution lineage |
| `source_event_id` | opaque string | telemetry/source | original upstream source record identity |
| `sentinel_event_id` | deterministic UUIDv5 preserved downstream | Sentinel | exported Sentinel analysis event identity |
| `policy_id` | UUIDv7 | AI policy emission | primary control-plane policy object ID |
| `scope_identifier` | opaque scoped string | dispatch/enforcement/ACK | exact enforcement scope, e.g. `tenant:acme` |
| `anomaly_id` | UUIDv4 | AI | operational object ID, not auth principal or tenant ID |

Rule:
- shared security contracts may introduce generic fields like `resource_id`, but they must not replace these live lineage IDs where those IDs already carry product meaning.

## 5. Prefix Policy
1. No prefixes are added to generated UUID-based security IDs in v1.
2. Dotted domain-style strings are used only for `schema_version`, not for object IDs.
3. If human-friendly external references are needed later, they must be separate display/reference IDs, not replacements for canonical security IDs.

## 6. Schema Version Convention
1. Shared contract `schema_version` fields use lowercase dotted strings ending in `.vN`.
2. Required pattern: `<domain>.<name>.vN`
3. Shared contract values in v1:
   - `security.identity.v1`
   - `security.access_context.v1`
   - `security.authorization_request.v1`
   - `security.policy_decision.v1`
   - `security.audit_event.v1`
4. Existing legacy payloads that use integer `schema_version` values remain legacy until migrated.
5. Adapters must normalize legacy payload versions at contract boundaries rather than leaking mixed version styles into shared contracts.

## 7. Time Format Convention
1. All cross-language time fields use RFC3339 UTC.
2. This applies to:
   - `timestamp`
   - `start_time`
   - `expiry_time`
   - `expires_at`

## 8. Validation Rules
1. UUID-generated IDs must be validated as canonical UUID strings.
2. `schema_version` strings must match the shared version pattern.
3. Empty strings are invalid for required IDs.
4. Max-length limits are enforced in JSON schemas.

## 9. Migration Notes
1. Current backend already generates UUIDv7 request, command, and workflow IDs with UUIDv4 fallback.
2. Current codebase still contains integer `schema_version` usage in some business payloads.
3. Shared contract canonical names must prefer already-live field names where the concept is stable across services:
   - keep `trace_id`
   - keep `source_event_id`
   - keep `sentinel_event_id`
   - keep `policy_id`
   - keep `scope_identifier`
   - normalize only true name mismatches such as `tenant` / `tenant_id` -> `access_id`
4. Phase 1 adapters must normalize:
   - `schema_version`
   - `is_global_resource`
   - `request_id`
   - `command_id`
   - `workflow_id`
   - `resource_version`
5. Shared security contracts are string-versioned even if surrounding legacy payloads are not yet migrated.
