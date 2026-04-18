# Phase 1 Adapter Rules

Status: Approved
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: current production code paths plus `PRD_SECURITY_IDENTITY_AUTHZ_TENANCY_AUDIT.md`

## Purpose

This document defines the single translation boundary allowed in Phase 1.

## Rules

1. Only one translation is allowed:
   - current runtime/product fields -> shared security contracts
2. Business IDs remain business IDs:
   - `policy_id`
   - `workflow_id`
   - `anomaly_id`
   - `evidence_id`
3. Correlation IDs remain correlation IDs:
   - `request_id`
   - `command_id`
   - `trace_id`
   - `source_event_id`
   - `sentinel_event_id`
4. Security boundary fields normalize into:
   - `access_id`
   - `allowed_access_ids`
   - `active_access_id`
5. Conflicting access-boundary candidates fail closed.
6. Missing `request_id` may be generated only on backend request ingress.
7. Invalid `command_id` or `workflow_id` fail normalization if provided.
8. Integer business `schema_version` values are tolerated only in legacy business payload paths, not in shared security contracts.

## Backend Adapter Expectations

1. Identity normalization must derive a stable `principal_id` from the strongest available source.
2. Access selection must treat `X-Tenant-ID` and query `tenant` as selector inputs only.
3. Mutation actor strings built from raw role/header state are legacy and must not remain the long-term principal source.

## AI / Enforcement / Telemetry Compatibility Expectations

1. AI `tenant_id` remains a business/runtime field until its protected operations are wired into the shared security path.
2. Enforcement `target.Tenant` currently overrides `spec.Tenant`; Phase 1 compatibility must preserve that precedence.
3. Telemetry `tenant_id` remains mandatory for the existing ingest and PCAP validation paths.
