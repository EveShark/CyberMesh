# Phase 1 Field Normalization Matrix

Status: Approved
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: current production code paths plus `PRD_SECURITY_IDENTITY_AUTHZ_TENANCY_AUDIT.md`

## Purpose

This document freezes the current live field shapes that Batch 1 must normalize before Phase 1 wiring starts.

## Backend

| Current field | Meaning in code today | Current behavior | Source |
| --- | --- | --- | --- |
| `X-Tenant-ID` | access selector | compared against query `tenant`; required in auth-required mode | `backend/pkg/api/control_plane.go:2698` |
| `tenant` query param | access selector | compared against `X-Tenant-ID`; same semantic path as header | `backend/pkg/api/control_plane.go:2700` |
| `request_id` | request correlation | generated as UUIDv7 with UUIDv4 fallback when absent | `backend/pkg/api/middleware.go:29` |
| `command_id` | operator command correlation | generated as UUIDv7 with UUIDv4 fallback | `backend/pkg/api/control_plane_mutations.go:1264` |
| `workflow_id` | workflow correlation | normalized from header/body or generated | `backend/pkg/api/control_plane_mutations.go:665` |
| `trace_id` | lineage correlation | carried through policy/audit paths, optional in backend | `backend/pkg/api/policies.go` |
| `source_event_id` | lineage correlation | carried when present | `backend/pkg/api/policies.go` |
| `sentinel_event_id` | lineage correlation | carried when present | `backend/pkg/api/policies.go` |
| `tenant_scope` | persisted mutation/audit access boundary | used for policy mutation and audit filtering | `backend/pkg/api/policies.go`, `backend/pkg/api/audit.go` |

## AI Service

| Current field | Meaning in code today | Current behavior | Source |
| --- | --- | --- | --- |
| `tenant_id` | business tenant/access boundary | required for packet-capture request path | `ai-service/src/service/manager.py:765` |
| `schema_version` | business payload version | integer in policy publisher path | `ai-service/src/service/publisher.py:77` |
| `target.scope=tenant` | policy targeting value | accepted as valid business scope | `ai-service/src/contracts/policy.py:503` |
| `Authorization: Bearer` | admin API auth | static bearer token auth | `ai-service/src/api/server.py:454` |

## Enforcement-Agent

| Current field | Meaning in code today | Current behavior | Source |
| --- | --- | --- | --- |
| `spec.Tenant` | policy business tenant | secondary tenant source | `enforcement-agent/cmd/agent/main.go:1029` |
| `target.Tenant` | policy target tenant | takes precedence over `spec.Tenant` | `enforcement-agent/cmd/agent/main.go:1029` |
| `tenant` | summarized output field | emitted from effective tenant helper | `enforcement-agent/cmd/agent/main.go:997` |
| `Authorization: Bearer` | control API auth | static bearer token auth | `enforcement-agent/cmd/agent/main.go:836` |

## Telemetry Layer

| Current field | Meaning in code today | Current behavior | Source |
| --- | --- | --- | --- |
| `tenant_id` | required tenant/access boundary | required in request and modality validation | `telemetry-layer/adapters/internal/spectra/validate.go:137` |
| `trace_id` | required lineage id | required in request and modality validation | `telemetry-layer/adapters/internal/spectra/validate.go:140` |
| `source_event_id` | source identity | required or synthesized from `request_id` | `telemetry-layer/adapters/internal/spectra/validate.go:123` |
| `request_id` | request correlation | required in PCAP validation, optional source-event fallback in spectra | `telemetry-layer/pcap-service/internal/validate/request.go:26` |

## Batch 1 Normalization Rules

1. Backend `X-Tenant-ID` and query `tenant` are treated as access selectors, not trusted identity.
2. Backend/AI/enforcement business `tenant` and `tenant_id` fields are not assumed identical across all services; they are normalized per path.
3. Correlation fields keep their current names.
4. Shared security contracts normalize only the security boundary field into `access_*`.
5. No chained adapters are allowed after this matrix.
