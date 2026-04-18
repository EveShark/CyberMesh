package security

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"backend/pkg/security/contracts"
)

// Authorizer is the authorization boundary for backend handlers.
type Authorizer interface {
	Authorize(context.Context, contracts.AuthorizationRequest) (contracts.PolicyDecision, error)
}

// DenyAuthorizer fails closed for all requests.
type DenyAuthorizer struct {
	ReasonCode string
}

func (a DenyAuthorizer) Authorize(_ context.Context, req contracts.AuthorizationRequest) (contracts.PolicyDecision, error) {
	reason := a.ReasonCode
	if reason == "" {
		reason = "authz.denied_by_default"
	}
	return newDecision(req, contracts.DecisionEffectDeny, contracts.DecisionSourceStatic, reason, ""), nil
}

// StaticAuthorizer always returns the configured effect and reason.
type StaticAuthorizer struct {
	Effect     contracts.DecisionEffect
	ReasonCode string
}

func (a StaticAuthorizer) Authorize(_ context.Context, req contracts.AuthorizationRequest) (contracts.PolicyDecision, error) {
	effect := a.Effect
	if effect == "" {
		effect = contracts.DecisionEffectAllow
	}
	reason := a.ReasonCode
	if reason == "" {
		if effect == contracts.DecisionEffectAllow {
			reason = "authz.allowed_static"
		} else {
			reason = "authz.denied_static"
		}
	}
	return newDecision(req, effect, contracts.DecisionSourceStatic, reason, ""), nil
}

// Phase1Authorizer is the low-latency local authorization boundary used when
// request handling stays on the in-process policy path.
type Phase1Authorizer struct {
	RequireResolvedAccess bool
}

func (a Phase1Authorizer) Authorize(_ context.Context, req contracts.AuthorizationRequest) (contracts.PolicyDecision, error) {
	if strings.TrimSpace(req.PrincipalID) == "" {
		return newDecision(req, contracts.DecisionEffectDeny, contracts.DecisionSourceStatic, "authz.denied_missing_principal", ""), nil
	}
	if req.PrincipalType != contracts.PrincipalTypeUser && req.PrincipalType != contracts.PrincipalTypeService {
		return newDecision(req, contracts.DecisionEffectDeny, contracts.DecisionSourceStatic, "authz.denied_invalid_principal_type", ""), nil
	}
	if strings.TrimSpace(string(req.Action)) == "" {
		return newDecision(req, contracts.DecisionEffectDeny, contracts.DecisionSourceStatic, "authz.denied_missing_action", ""), nil
	}
	if strings.TrimSpace(req.Resource.Type) == "" {
		return newDecision(req, contracts.DecisionEffectDeny, contracts.DecisionSourceStatic, "authz.denied_missing_resource_type", ""), nil
	}
	if a.RequireResolvedAccess && !req.Resource.IsGlobalResource && strings.TrimSpace(req.Access.ActiveAccessID) == "" {
		return newDecision(req, contracts.DecisionEffectDeny, contracts.DecisionSourceStatic, "authz.denied_missing_access_scope", ""), nil
	}
	return newDecision(req, contracts.DecisionEffectAllow, contracts.DecisionSourceStatic, "authz.allowed_phase1_local", ""), nil
}

// ShadowAuthorizer returns the primary decision while recording a shadow decision.
type ShadowAuthorizer struct {
	Primary       Authorizer
	Shadow        Authorizer
	ShadowResults *[]ShadowResult
	mu            sync.Mutex
}

type ShadowResult struct {
	Primary contracts.PolicyDecision
	Shadow  contracts.PolicyDecision
	Err     error
}

func (a *ShadowAuthorizer) Authorize(ctx context.Context, req contracts.AuthorizationRequest) (contracts.PolicyDecision, error) {
	if a.Primary == nil {
		return contracts.PolicyDecision{}, fmt.Errorf("primary authorizer is required")
	}
	primary, err := a.Primary.Authorize(ctx, req)
	if err != nil {
		return contracts.PolicyDecision{}, err
	}
	if a.Shadow == nil || a.ShadowResults == nil {
		return primary, nil
	}
	shadow, shadowErr := a.Shadow.Authorize(ctx, req)
	a.mu.Lock()
	*a.ShadowResults = append(*a.ShadowResults, ShadowResult{
		Primary: primary,
		Shadow:  shadow,
		Err:     shadowErr,
	})
	a.mu.Unlock()
	return primary, nil
}

func newDecision(req contracts.AuthorizationRequest, effect contracts.DecisionEffect, source contracts.DecisionSource, reasonCode, reasonDetail string) contracts.PolicyDecision {
	decisionID := mustUUID()
	return contracts.PolicyDecision{
		SchemaVersion: contracts.PolicyDecisionSchemaVersionV1,
		DecisionID:    decisionID,
		Effect:        effect,
		Source:        source,
		PrincipalID:   req.PrincipalID,
		PrincipalType: req.PrincipalType,
		Action:        req.Action,
		Resource:      req.Resource,
		AccessID:      req.Access.ActiveAccessID,
		ReasonCode:    reasonCode,
		ReasonDetail:  reasonDetail,
		RequestID:     req.Context.RequestID,
		TraceID:       req.Context.TraceID,
	}
}

func mustUUID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}
