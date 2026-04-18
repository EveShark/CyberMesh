# Audit Reason Taxonomy

Status: Approved for Phase 0
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: `PRD_SECURITY_IDENTITY_AUTHZ_TENANCY_AUDIT.md`

## 1. Purpose
This document freezes the normalized reason code vocabulary used by authorization decisions and audit events. Reason codes must be stable enough for alerting, dashboards, and incident review.

## 2. Core Rules
1. Every allow, deny, or error decision emits exactly one primary reason code.
2. Reason codes are machine-readable and dot-delimited.
3. Human-readable detail may be added in `reason_detail`, but does not replace `reason_code`.
4. New reason codes require security review because they affect observability and audit semantics.
5. Any cross-language time fields emitted alongside these reasons must use RFC3339 UTC.

## 3. Reason Code Families
### 3.1 AuthN
- `authn.allow.user_verified`
- `authn.allow.service_verified`
- `authn.deny.missing_token`
- `authn.deny.invalid_token`
- `authn.deny.expired_token`
- `authn.deny.invalid_issuer`
- `authn.deny.invalid_audience`
- `authn.deny.workload_identity_invalid`

### 3.2 Access Resolution
- `access.allow.single_access_resolved`
- `access.allow.selector_resolved`
- `access.allow.delegation_resolved`
- `access.allow.global_resource_authorized`
- `access.deny.no_access_context`
- `access.deny.ambiguous_access`
- `access.deny.selector_unauthorized`
- `access.deny.selector_invalid`
- `access.deny.global_resource_not_allowed`
- `access.deny.service_access_missing`
- `access.deny.service_access_mismatch`

### 3.3 AuthZ
- `authz.allow.openfga_allowed`
- `authz.allow.delegation_allowed`
- `authz.deny.openfga_denied`
- `authz.deny.no_policy_path`
- `authz.error.openfga_unavailable`
- `authz.error.policy_evaluation_failed`

### 3.4 Workload Authorization
- `workload.allow.action_permitted`
- `workload.deny.identity_not_allowlisted`
- `workload.deny.action_not_allowed`
- `workload.deny.resource_scope_not_allowed`
- `workload.deny.access_scope_not_allowed`

### 3.5 Command Scope
- `command.allow.scope_validated`
- `command.deny.scope_mismatch`
- `command.deny.target_class_not_allowed`
- `command.deny.command_action_not_allowed`

### 3.6 Delegated Support and Break Glass
- `support.allow.delegation_valid`
- `support.deny.delegation_missing`
- `support.deny.delegation_expired`
- `support.deny.delegation_revoked`
- `break_glass.allow.override_used`
- `break_glass.deny.disabled`
- `break_glass.deny.approval_missing`

### 3.7 Audit Pipeline
- `audit.allow.event_recorded`
- `audit.error.write_failed`
- `audit.error.integrity_sign_failed`
- `audit.error.chain_verification_failed`
- `audit.deny.read_not_authorized`

## 4. Usage Rules
1. The final enforcement point emits the terminal reason code for the request decision.
2. Pre-decision intermediate checks may log internal diagnostics, but the audit event carries the terminal reason code.
3. Support and break-glass decisions must include delegation or approval metadata when applicable.
4. Error reason codes are not converted into allow decisions.

## 5. Phase 0 Frozen Decisions
1. No additional business-specific reason code families are added in v1 beyond the families in this document.
2. Audit read denials map to warning-severity alerts; audit write and integrity failures map to critical-severity alerts.
3. Break-glass remains disabled by default in the initial rollout and uses the existing `break_glass.*` namespace only for controlled future activation.
