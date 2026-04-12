package security

import (
	"fmt"
	"strings"
	"time"

	"backend/pkg/security/contracts"
	"backend/pkg/utils"
)

// AuditEmitter converts normalized security decisions into the shared audit contract
// and optionally mirrors them into the existing backend audit logger.
type AuditEmitter struct {
	Logger *utils.AuditLogger
}

func (e AuditEmitter) BuildEvent(identity contracts.IdentityClaims, access contracts.AccessContext, decision contracts.PolicyDecision, req contracts.AuthorizationRequest, outcome string) (contracts.AuditEvent, error) {
	if decision.DecisionID == "" {
		return contracts.AuditEvent{}, fmt.Errorf("decision_id is required")
	}
	if req.Context.RequestID == "" {
		return contracts.AuditEvent{}, fmt.Errorf("request_id is required")
	}
	if identity.PrincipalID == "" {
		return contracts.AuditEvent{}, fmt.Errorf("principal_id is required")
	}
	if identity.PrincipalType != contracts.PrincipalTypeUser && identity.PrincipalType != contracts.PrincipalTypeService {
		return contracts.AuditEvent{}, fmt.Errorf("principal_type is required")
	}
	if req.Action == "" {
		return contracts.AuditEvent{}, fmt.Errorf("action is required")
	}
	if req.Resource.Type == "" {
		return contracts.AuditEvent{}, fmt.Errorf("resource.type is required")
	}
	if !req.Resource.IsGlobalResource && access.ActiveAccessID == "" && decision.Effect != contracts.DecisionEffectDeny {
		return contracts.AuditEvent{}, fmt.Errorf("access_id is required for access-bound resources")
	}

	auditDecision := contracts.AuditDecisionError
	switch decision.Effect {
	case contracts.DecisionEffectAllow:
		auditDecision = contracts.AuditDecisionAllow
	case contracts.DecisionEffectDeny:
		auditDecision = contracts.AuditDecisionDeny
	}

	event := contracts.AuditEvent{
		AuditEventID:    mustUUID(),
		DecisionID:      decision.DecisionID,
		RequestID:       req.Context.RequestID,
		TraceID:         req.Context.TraceID,
		SourceEventID:   req.Context.SourceEventID,
		SentinelEventID: req.Context.SentinelEventID,
		PolicyID:        req.Context.PolicyID,
		ScopeIdentifier: req.Context.ScopeIdentifier,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		SchemaVersion:   contracts.AuditEventSchemaVersionV1,
		Decision:        auditDecision,
		ReasonCode:      decision.ReasonCode,
		ReasonDetail:    decision.ReasonDetail,
		PolicySource:    string(decision.Source),
		PolicyStore:     decision.PolicyStore,
		PolicyModel:     decision.PolicyModel,
		Action:          req.Action,
		Actor: contracts.AuditActor{
			PrincipalID:   identity.PrincipalID,
			PrincipalType: identity.PrincipalType,
			WorkloadID:    req.Context.WorkloadID,
			SessionID:     identity.SessionID,
			SupportReason: strings.TrimSpace(req.Context.SupportReason),
			BreakGlass:    req.Context.BreakGlass,
		},
		Target: contracts.AuditTarget{
			AccessID:            access.ActiveAccessID,
			ResourceType:        req.Resource.Type,
			ResourceID:          req.Resource.ID,
			IsGlobalResource:    req.Resource.IsGlobalResource,
			SelectorAccessID:    req.Context.SelectorAccessID,
			AuthorizedAccessIDs: access.AllowedAccessIDs,
		},
		RequestPath: req.Context.RequestPath,
		HTTPMethod:  req.Context.HTTPMethod,
		Outcome:     outcome,
	}
	if access.Delegation != nil {
		event.Actor.DelegationID = access.Delegation.DelegationID
		event.Actor.ApprovalReference = strings.TrimSpace(access.Delegation.ApprovalReference)
	}
	return event, nil
}

func (e AuditEmitter) Emit(event contracts.AuditEvent) error {
	if e.Logger == nil {
		return nil
	}
	severity := utils.AuditInfo
	switch event.Decision {
	case contracts.AuditDecisionDeny:
		severity = utils.AuditWarn
	case contracts.AuditDecisionError:
		severity = utils.AuditError
	}
	fields := map[string]interface{}{
		"audit_event_id":     event.AuditEventID,
		"decision_id":        event.DecisionID,
		"request_id":         event.RequestID,
		"trace_id":           event.TraceID,
		"source_event_id":    event.SourceEventID,
		"sentinel_event_id":  event.SentinelEventID,
		"policy_id":          event.PolicyID,
		"scope_identifier":   event.ScopeIdentifier,
		"schema_version":     event.SchemaVersion,
		"decision":           event.Decision,
		"reason_code":        event.ReasonCode,
		"reason_detail":      event.ReasonDetail,
		"policy_source":      event.PolicySource,
		"policy_store":       event.PolicyStore,
		"policy_model":       event.PolicyModel,
		"action":             event.Action,
		"principal_id":       event.Actor.PrincipalID,
		"principal_type":     event.Actor.PrincipalType,
		"workload_id":        event.Actor.WorkloadID,
		"delegation_id":      event.Actor.DelegationID,
		"approval_reference": event.Actor.ApprovalReference,
		"support_reason":     event.Actor.SupportReason,
		"break_glass":        event.Actor.BreakGlass,
		"access_id":          event.Target.AccessID,
		"resource_type":      event.Target.ResourceType,
		"resource_id":        event.Target.ResourceID,
		"is_global_resource": event.Target.IsGlobalResource,
		"selector_access_id": event.Target.SelectorAccessID,
		"allowed_access_ids": event.Target.AuthorizedAccessIDs,
		"request_path":       event.RequestPath,
		"http_method":        event.HTTPMethod,
		"outcome":            event.Outcome,
	}
	return e.Logger.Log("security.authorization_decision", severity, fields)
}

func (e AuditEmitter) BuildAndEmit(identity contracts.IdentityClaims, access contracts.AccessContext, decision contracts.PolicyDecision, req contracts.AuthorizationRequest, outcome string) error {
	event, err := e.BuildEvent(identity, access, decision, req, outcome)
	if err != nil {
		return err
	}
	return e.Emit(event)
}
