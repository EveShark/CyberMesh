package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"backend/pkg/config"
	backendsecurity "backend/pkg/security"
	"backend/pkg/security/contracts"
	"backend/pkg/utils"

	"github.com/google/uuid"
)

const openFGAPlatformObject = "platform:cybermesh"

type openFGAAuthorizer struct {
	baseURL string
	storeID string
	modelID string
	token   string
	client  *http.Client
}

type openFGACheckRequest struct {
	AuthorizationModelID string                  `json:"authorization_model_id"`
	TupleKey             openFGATupleKey         `json:"tuple_key"`
	ContextualTuples     openFGAContextualTuples `json:"contextual_tuples,omitempty"`
}

type openFGAContextualTuples struct {
	TupleKeys []openFGATupleKey `json:"tuple_keys,omitempty"`
}

type openFGATupleKey struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}

type openFGACheckResponse struct {
	Allowed bool `json:"allowed"`
}

type shadowLoggingAuthorizer struct {
	primary          backendsecurity.Authorizer
	shadow           backendsecurity.Authorizer
	logger           *utils.Logger
	timeout          time.Duration
	shadowSlots      chan struct{}
	shadowDropLogged atomic.Bool
}

type enforceLoggingAuthorizer struct {
	primary            backendsecurity.Authorizer
	enforce            backendsecurity.Authorizer
	logger             *utils.Logger
	enforcedTypes      map[string]struct{}
	enforceUnavailable string
}

func newConfiguredAuthorizer(cfg *config.APIConfig, logger *utils.Logger) backendsecurity.Authorizer {
	primary := backendsecurity.Phase1Authorizer{
		RequireResolvedAccess: strings.EqualFold(strings.TrimSpace(cfg.Environment), "production") || cfg.RequireAuth,
	}
	if !cfg.OpenFGAShadow && !cfg.OpenFGAEnforce {
		return primary
	}
	shadow, err := newOpenFGAAuthorizer(cfg)
	if err != nil {
		if logger != nil {
			logger.Warn("openfga shadow authorizer disabled",
				utils.ZapString("reason", err.Error()))
		}
		if cfg.OpenFGAEnforce {
			return &enforceLoggingAuthorizer{
				primary:            primary,
				logger:             logger,
				enforcedTypes:      normalizeEnforcedTypes(cfg.OpenFGAEnforceTypes),
				enforceUnavailable: err.Error(),
			}
		}
		return primary
	}
	if cfg.OpenFGAEnforce {
		return &enforceLoggingAuthorizer{
			primary:       primary,
			enforce:       shadow,
			logger:        logger,
			enforcedTypes: normalizeEnforcedTypes(cfg.OpenFGAEnforceTypes),
		}
	}
	if !cfg.OpenFGAShadow {
		return primary
	}
	return &shadowLoggingAuthorizer{
		primary:     primary,
		shadow:      shadow,
		logger:      logger,
		timeout:     cfg.OpenFGATimeout,
		shadowSlots: make(chan struct{}, cfg.OpenFGAShadowMaxInflight),
	}
}

func newOpenFGAAuthorizer(cfg *config.APIConfig) (backendsecurity.Authorizer, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.OpenFGAAPIURL), "/")
	storeID := strings.TrimSpace(cfg.OpenFGAStoreID)
	modelID := strings.TrimSpace(cfg.OpenFGAModelID)
	if baseURL == "" || storeID == "" || modelID == "" {
		return nil, fmt.Errorf("OpenFGA API URL, store ID, and model ID are required")
	}
	timeout := cfg.OpenFGATimeout
	if timeout <= 0 {
		timeout = 750 * time.Millisecond
	}
	return &openFGAAuthorizer{
		baseURL: baseURL,
		storeID: storeID,
		modelID: modelID,
		token:   strings.TrimSpace(cfg.OpenFGAToken),
		client:  &http.Client{Timeout: timeout},
	}, nil
}

func (a *shadowLoggingAuthorizer) Authorize(ctx context.Context, req contracts.AuthorizationRequest) (contracts.PolicyDecision, error) {
	primary, err := a.primary.Authorize(ctx, req)
	if err != nil {
		return contracts.PolicyDecision{}, err
	}
	if a.shadow == nil {
		return primary, nil
	}
	a.evaluateShadowAsync(req, primary)
	return primary, nil
}

func (a *shadowLoggingAuthorizer) evaluateShadowAsync(req contracts.AuthorizationRequest, primary contracts.PolicyDecision) {
	shadow := a.shadow
	logger := a.logger
	timeout := a.timeout
	if timeout <= 0 {
		timeout = 750 * time.Millisecond
	}
	if a.shadowSlots != nil {
		select {
		case a.shadowSlots <- struct{}{}:
		default:
			if logger != nil && a.shadowDropLogged.CompareAndSwap(false, true) {
				logger.Warn("openfga shadow evaluation skipped due to concurrency limit")
			}
			return
		}
	}

	go func() {
		if a.shadowSlots != nil {
			defer func() {
				<-a.shadowSlots
				a.shadowDropLogged.Store(false)
			}()
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		shadowDecision, shadowErr := shadow.Authorize(ctx, req)
		if logger == nil {
			return
		}
		switch {
		case shadowErr != nil:
			logger.WarnContext(ctx, "openfga shadow evaluation failed",
				utils.ZapString("request_id", req.Context.RequestID),
				utils.ZapString("principal_id", req.PrincipalID),
				utils.ZapString("resource_type", req.Resource.Type),
				utils.ZapString("action", string(req.Action)),
				utils.ZapString("error", shadowErr.Error()))
		case primary.Effect != shadowDecision.Effect || primary.ReasonCode != shadowDecision.ReasonCode:
			logger.WarnContext(ctx, "openfga shadow divergence",
				utils.ZapString("request_id", req.Context.RequestID),
				utils.ZapString("principal_id", req.PrincipalID),
				utils.ZapString("resource_type", req.Resource.Type),
				utils.ZapString("action", string(req.Action)),
				utils.ZapString("primary_effect", string(primary.Effect)),
				utils.ZapString("primary_reason", primary.ReasonCode),
				utils.ZapString("shadow_effect", string(shadowDecision.Effect)),
				utils.ZapString("shadow_reason", shadowDecision.ReasonCode))
		}
	}()
}

func (a *enforceLoggingAuthorizer) Authorize(ctx context.Context, req contracts.AuthorizationRequest) (contracts.PolicyDecision, error) {
	primary, err := a.primary.Authorize(ctx, req)
	if err != nil {
		return contracts.PolicyDecision{}, err
	}
	if !a.isEnforcedType(req.Resource.Type) {
		return primary, nil
	}
	if strings.TrimSpace(a.enforceUnavailable) != "" || a.enforce == nil {
		if a.logger != nil {
			a.logger.WarnContext(ctx, "openfga enforce unavailable; fail-closed decision",
				utils.ZapString("request_id", req.Context.RequestID),
				utils.ZapString("resource_type", req.Resource.Type),
				utils.ZapString("reason", a.enforceUnavailable))
		}
		return newOpenFGADecision(req, contracts.DecisionEffectDeny, "authz.enforce_openfga_fail_closed", strings.TrimSpace(a.enforceUnavailable), "", ""), nil
	}
	shadow, shadowErr := a.enforce.Authorize(ctx, req)
	if shadowErr != nil {
		if a.logger != nil {
			a.logger.WarnContext(ctx, "openfga enforce evaluation failed; fail-closed decision",
				utils.ZapString("request_id", req.Context.RequestID),
				utils.ZapString("resource_type", req.Resource.Type),
				utils.ZapString("error", shadowErr.Error()))
		}
		return newOpenFGADecision(req, contracts.DecisionEffectDeny, "authz.enforce_openfga_fail_closed", shadowErr.Error(), "", ""), nil
	}
	if shadow.Effect == contracts.DecisionEffectError {
		if a.logger != nil {
			a.logger.WarnContext(ctx, "openfga enforce returned error decision; fail-closed applied",
				utils.ZapString("request_id", req.Context.RequestID),
				utils.ZapString("resource_type", req.Resource.Type),
				utils.ZapString("reason_code", shadow.ReasonCode))
		}
		return newOpenFGADecision(req, contracts.DecisionEffectDeny, "authz.enforce_openfga_fail_closed", shadow.ReasonCode, shadow.PolicyStore, shadow.PolicyModel), nil
	}
	if a.logger != nil && (primary.Effect != shadow.Effect || primary.ReasonCode != shadow.ReasonCode) {
		a.logger.WarnContext(ctx, "openfga enforce divergence from local primary",
			utils.ZapString("request_id", req.Context.RequestID),
			utils.ZapString("resource_type", req.Resource.Type),
			utils.ZapString("action", string(req.Action)),
			utils.ZapString("primary_effect", string(primary.Effect)),
			utils.ZapString("primary_reason", primary.ReasonCode),
			utils.ZapString("enforce_effect", string(shadow.Effect)),
			utils.ZapString("enforce_reason", shadow.ReasonCode))
	}
	return shadow, nil
}

func (a *enforceLoggingAuthorizer) isEnforcedType(resourceType string) bool {
	if len(a.enforcedTypes) == 0 {
		return false
	}
	_, ok := a.enforcedTypes[strings.ToLower(strings.TrimSpace(resourceType))]
	return ok
}

func normalizeEnforcedTypes(values []string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		out[key] = struct{}{}
	}
	return out
}

func (a *openFGAAuthorizer) Authorize(ctx context.Context, req contracts.AuthorizationRequest) (contracts.PolicyDecision, error) {
	checkReq, reason, reasonDetail := buildOpenFGACheck(req, ctx)
	if reason != "" {
		return newOpenFGADecision(req, contracts.DecisionEffectError, reason, reasonDetail, a.storeID, a.modelID), nil
	}
	checkReq.AuthorizationModelID = a.modelID

	body, err := json.Marshal(checkReq)
	if err != nil {
		return contracts.PolicyDecision{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/stores/%s/check", a.baseURL, a.storeID), bytes.NewReader(body))
	if err != nil {
		return contracts.PolicyDecision{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.token)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return contracts.PolicyDecision{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return contracts.PolicyDecision{}, fmt.Errorf("OpenFGA check returned %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var checkResp openFGACheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&checkResp); err != nil {
		return contracts.PolicyDecision{}, err
	}

	effect := contracts.DecisionEffectDeny
	reason = "authz.shadow_openfga_deny"
	if checkResp.Allowed {
		effect = contracts.DecisionEffectAllow
		reason = "authz.shadow_openfga_allow"
	}
	return newOpenFGADecision(req, effect, reason, "", a.storeID, a.modelID), nil
}

func buildOpenFGACheck(req contracts.AuthorizationRequest, ctx context.Context) (*openFGACheckRequest, string, string) {
	principal, ok := openFGAUserString(req.PrincipalType, req.PrincipalID)
	if !ok {
		return nil, "authz.shadow_model_gap", "unsupported principal format for OpenFGA shadow"
	}

	clientRole, _ := ctx.Value(ctxKeyClientRole).(string)
	roleRelation, ok := mapClientRoleToOpenFGARelation(clientRole)
	if !ok {
		return nil, "authz.shadow_model_gap", "client role is not mapped to the OpenFGA model"
	}

	targetObject, targetRelation, contextualTuples, ok := mapRequestToOpenFGA(req, principal, roleRelation)
	if !ok {
		return nil, "authz.shadow_model_gap", "resource/action pair is not represented in the OpenFGA model"
	}

	return &openFGACheckRequest{
		AuthorizationModelID: "",
		TupleKey: openFGATupleKey{
			User:     principal,
			Relation: targetRelation,
			Object:   targetObject,
		},
		ContextualTuples: openFGAContextualTuples{TupleKeys: contextualTuples},
	}, "", ""
}

func mapRequestToOpenFGA(req contracts.AuthorizationRequest, principal, roleRelation string) (string, string, []openFGATupleKey, bool) {
	accessID := strings.TrimSpace(req.Access.ActiveAccessID)
	switch req.Resource.Type {
	case "policy":
		if accessID == "" {
			return "", "", nil, false
		}
		if req.Action == contracts.ActionList {
			return openFGAObject("access", accessID), "can_view", []openFGATupleKey{
				{User: principal, Relation: roleRelation, Object: openFGAObject("access", accessID)},
			}, true
		}
		resourceObject := openFGAObject("policy", req.Resource.ID)
		return resourceObject, openFGAPolicyRelation(req.Action), []openFGATupleKey{
			{User: openFGAObject("access", accessID), Relation: "access", Object: resourceObject},
			{User: principal, Relation: roleRelation, Object: openFGAObject("access", accessID)},
		}, true
	case "workflow":
		if accessID == "" {
			return "", "", nil, false
		}
		if req.Action == contracts.ActionList {
			return openFGAObject("access", accessID), "can_view", []openFGATupleKey{
				{User: principal, Relation: roleRelation, Object: openFGAObject("access", accessID)},
			}, true
		}
		resourceObject := openFGAObject("workflow", req.Resource.ID)
		return resourceObject, openFGAWorkflowRelation(req.Action), []openFGATupleKey{
			{User: openFGAObject("access", accessID), Relation: "access", Object: resourceObject},
			{User: principal, Relation: roleRelation, Object: openFGAObject("access", accessID)},
		}, true
	case "audit_scope":
		if accessID == "" {
			return "", "", nil, false
		}
		resourceObject := openFGAObject("audit_scope", accessID)
		return resourceObject, openFGAAuditRelation(req), []openFGATupleKey{
			{User: openFGAObject("access", accessID), Relation: "access", Object: resourceObject},
			{User: principal, Relation: roleRelation, Object: openFGAObject("access", accessID)},
		}, true
	case "platform_config":
		if roleRelation != "platform_admin" {
			return "", "", nil, false
		}
		return openFGAPlatformObject, "can_manage_global_config", []openFGATupleKey{
			{User: principal, Relation: "admin", Object: openFGAPlatformObject},
		}, true
	default:
		return "", "", nil, false
	}
}

func openFGAPolicyRelation(action contracts.Action) string {
	switch action {
	case contracts.ActionWrite:
		return "can_write"
	case contracts.ActionApprove, contracts.ActionReject:
		return "can_approve"
	case contracts.ActionDelete:
		return "can_delete"
	case contracts.ActionList:
		return "can_view"
	default:
		return "can_read"
	}
}

func openFGAWorkflowRelation(action contracts.Action) string {
	switch action {
	case contracts.ActionWrite:
		return "can_write"
	case contracts.ActionApprove, contracts.ActionReject:
		return "can_approve"
	case contracts.ActionApply:
		return "can_apply"
	case contracts.ActionList:
		return "can_view"
	default:
		return "can_read"
	}
}

func openFGAAuditRelation(req contracts.AuthorizationRequest) string {
	if strings.HasSuffix(strings.TrimSpace(req.Resource.ID), "/audit/export") {
		return "can_export"
	}
	return "can_read"
}

func openFGAUserString(principalType contracts.PrincipalType, principalID string) (string, bool) {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", false
	}
	switch principalType {
	case contracts.PrincipalTypeUser:
		if strings.HasPrefix(principalID, "user:") {
			return principalID, true
		}
		if strings.HasPrefix(principalID, "service:") {
			return "", false
		}
		return "user:" + principalID, true
	case contracts.PrincipalTypeService:
		if strings.HasPrefix(principalID, "service:") {
			return principalID, true
		}
		if strings.HasPrefix(principalID, "user:") {
			return "", false
		}
		return "service:" + principalID, true
	}
	return "", false
}

func mapClientRoleToOpenFGARelation(role string) (string, bool) {
	switch strings.TrimSpace(role) {
	case "admin":
		return "platform_admin", true
	case "control_outbox_operator":
		return "admin", true
	case "policy_reader", "control_ack_reader", "control_outbox_reader", "control_trace_reader", "authenticated_user", "api_client", "developer":
		return "viewer", true
	case "audit_reader":
		return "admin", true
	default:
		return "", false
	}
}

func openFGAObject(typ, id string) string {
	return typ + ":" + strings.TrimSpace(id)
}

func newOpenFGADecision(req contracts.AuthorizationRequest, effect contracts.DecisionEffect, reasonCode, reasonDetail, policyStore, policyModel string) contracts.PolicyDecision {
	decisionID, err := uuid.NewV7()
	if err != nil {
		decisionID = uuid.Must(uuid.NewRandom())
	}
	return contracts.PolicyDecision{
		SchemaVersion: contracts.PolicyDecisionSchemaVersionV1,
		DecisionID:    decisionID.String(),
		Effect:        effect,
		Source:        contracts.DecisionSourceOpenFGA,
		PolicyStore:   policyStore,
		PolicyModel:   policyModel,
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
