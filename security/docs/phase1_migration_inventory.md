# Phase 1 Migration Inventory

Status: approved for Batch 4 backend hardening

## Purpose

This inventory records the live producer/runtime field behavior that Phase 1 must preserve while the backend moves onto the normalized security path.

## Backend

- Current request selector inputs:
  - `X-Tenant-ID`
  - `X-Tenant`
  - `?tenant=`
- Current correlation fields already live in the API path:
  - `request_id`
  - `command_id`
  - `workflow_id`
  - `trace_id`
  - `source_event_id`
  - `sentinel_event_id`
  - `policy_id`
  - `scope_identifier`
- Phase 1 Batch 3 bridge:
  - `backend/pkg/api/resolveTenantScope` now classifies the route, normalizes request fields, resolves access, builds a normalized authorization request, and emits a normalized audit event before returning the existing `tenantScope` bridge string.
- Phase 1 Batch 4 hardening:
  - mutation actor attribution now uses normalized principal plus resolved access, not raw `X-Tenant-*` headers
  - ACK query filtering no longer falls back to raw `?tenant=` when no normalized access scope exists
  - pre-access deny decisions now emit normalized audit events without inventing a fake access id
  - backend race validation is part of CI with `CGO_ENABLED=1`

## AI Service

Source files reviewed:
- `ai-service/src/service/manager.py`
- `ai-service/src/service/publisher.py`

Observed live behavior:
- Packet capture requests require `tenant_id`.
- AI policy payload validation still requires integer `schema_version`.
- AI policy/business object IDs remain UUIDv4-compatible.

Phase 1 rule:
- The shared security layer must accept `tenant_id`-only inputs and preserve UUIDv4 business IDs as opaque business identifiers.

## Enforcement-Agent

Source file reviewed:
- `enforcement-agent/cmd/agent/main.go`

Observed live behavior:
- Effective tenant precedence is:
  - `spec.target.tenant`
  - then `spec.tenant`
- The enforcement agent still emits and consumes business/runtime `tenant` fields.

Phase 1 rule:
- The future enforcement adapter must collapse the effective tenant first, then pass the resolved value into the shared access contract.
- The shared contract boundary must not receive both unresolved values when they differ.

## Telemetry Layer

Source files reviewed:
- `telemetry-layer/adapters/internal/spectra/validate.go`
- `telemetry-layer/pcap-service/internal/validate/request.go`

Observed live behavior:
- `tenant_id` is required.
- `trace_id` is required for request-category spectra envelopes.
- `source_event_id` is required, but spectra may synthesize it from `request_id`.
- PCAP validation rejects unknown tenants and missing `tenant_id`.

Phase 1 rule:
- The shared security layer must accept `tenant_id`-only selectors.
- Missing correlation fields must remain path-specific:
  - backend may generate `request_id`
  - telemetry request validation remains stricter

## Remaining Phase 2 Work

- Replace static Phase 1 authorization decisions with real OpenFGA checks.
- Add a trusted identity-bound access membership source so access-bound routes can authorize against real memberships instead of fail-closed placeholder empty sets.
- Add dedicated AI/enforcement adapter code instead of relying on the backend bridge only.
- Replace legacy `tenantScope` SQL bridge usage with explicit access-bound repository APIs.
- Replace coarse dev/legacy fallback principals with real identity-bound principals once Phase 2 AuthN lands.
