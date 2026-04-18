# Phase 1 Resource Scope Catalog

Status: Approved
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: current backend route surface in `backend/pkg/api/router.go` plus current scope-enforced handlers

## Purpose

This catalog freezes the initial backend route-to-scope classification used by Batch 2 resource scope logic.

## Access-bound families

These handler families already use request access scope in current backend code and are classified as access-bound in Batch 2:

| Route family | Resource type | Current evidence |
| --- | --- | --- |
| `/policies` | `policy` | `backend/pkg/api/policies.go` calls `resolveTenantScope` |
| `/policies/acks` | `policy_ack` | policy ACK reads filter by tenant/access scope |
| `/workflows` | `workflow` | `backend/pkg/api/workflows.go` calls `resolveTenantScope` |
| `/audit` | `audit_scope` | `backend/pkg/api/audit.go` filters on `tenant_scope` |
| `/control/outbox` | `control_outbox` | `backend/pkg/api/control_plane.go` calls `resolveTenantScope` |
| `/control/trace` | `control_trace` | `backend/pkg/api/control_plane.go` calls `resolveTenantScope` |
| `/control/acks` | `control_ack` | control ACK paths carry tenant/access filtering |
| `/control/leases` | `control_lease` | lease paths currently resolve tenant/access scope |

## Global families

These route families do not currently enforce request tenant/access scope in backend code and are treated as global in Batch 2:

| Route family | Resource type | Current evidence |
| --- | --- | --- |
| `/control/safe-mode:toggle` | `platform_config` | toggle is cluster/platform state |
| `/control/kill-switch:toggle` | `platform_config` | toggle is cluster/platform state |
| `/stats` | `stats` | no request access resolution in current handler |
| `/dashboard/overview` | `dashboard` | no request access resolution in current handler |
| `/network/overview` | `network_overview` | no request access resolution in current handler |
| `/consensus/overview` | `consensus_overview` | no request access resolution in current handler |
| `/ai/*` | `ai_metrics` | current backend handler family is not access-scoped |
| `/anomalies*` | `anomaly_overview` | current backend handler family is not access-scoped |
| `/frontend-config` | `frontend_config` | UI/platform config output |

## Fail-closed rule

Unknown routes are not classified implicitly. Batch 2 resource classification must return an error for paths not in this catalog.
