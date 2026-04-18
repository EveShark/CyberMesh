package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	backendsecurity "backend/pkg/security"
	"backend/pkg/security/contracts"
)

type normalizedSecurityEnvelope struct {
	Identity contracts.IdentityClaims
	Access   contracts.AccessContext
	Request  contracts.AuthorizationRequest
	Decision contracts.PolicyDecision
}

func (s *Server) resolveNormalizedSecurityEnvelope(r *http.Request) (normalizedSecurityEnvelope, *tenantScopeError) {
	scope, err := classifyRequestScope(s.config.BasePath, r.Method, r.URL.Path)
	if err != nil {
		return normalizedSecurityEnvelope{}, &tenantScopeError{
			Code:       "RESOURCE_SCOPE_UNKNOWN",
			Message:    "resource scope could not be classified",
			HTTPStatus: http.StatusForbidden,
		}
	}
	scope.Resource.ID = s.normalizeRoutePath(r.URL.Path)

	requestID := getRequestID(r.Context())
	if requestID == "unknown" {
		requestID = ""
	}
	requestCtx, reqErr := backendsecurity.NormalizeLegacyRequestContext(backendsecurity.LegacyRequestContextInput{
		RequestID:       requestID,
		RequestPath:     r.URL.Path,
		HTTPMethod:      r.Method,
		ClientIP:        getClientIP(r),
		SessionAccessID: readActiveAccessCookie(r),
		HeaderAccessID:  r.Header.Get("X-Tenant-ID"),
		QueryAccessID:   r.URL.Query().Get("tenant"),
		Tenant:          r.Header.Get("X-Tenant"),
		TenantID:        r.URL.Query().Get("tenant_id"),
		WorkflowID:      r.URL.Query().Get("workflow_id"),
		PolicyID:        r.URL.Query().Get("policy_id"),
		ScopeIdentifier: r.URL.Query().Get("scope_identifier"),
	})
	if reqErr != nil {
		return normalizedSecurityEnvelope{}, &tenantScopeError{
			Code:       "TENANT_SCOPE_CONFLICT",
			Message:    reqErr.Error(),
			HTTPStatus: http.StatusForbidden,
		}
	}

	clientRole := s.resolveLegacyClientRole(r)
	principalID := s.resolveLegacyPrincipalID(r)
	principalType := s.resolveLegacyPrincipalType(r)
	requireScope := s.requireAccessScope()
	if roleErr := s.validateBridgeRole(clientRole, scope); roleErr != nil {
		identity, _ := backendsecurity.NormalizeLegacyIdentity(backendsecurity.LegacyIdentityInput{
			PrincipalID:   principalID,
			PrincipalType: principalType,
			CertificateCN: s.resolveLegacyCertificateCN(r),
			Roles:         roleList(clientRole),
		})
		authReq := contracts.AuthorizationRequest{
			SchemaVersion: contracts.AuthorizationRequestSchemaVersionV1,
			PrincipalID:   identity.PrincipalID,
			PrincipalType: identity.PrincipalType,
			Action:        inferAuthorizationAction(r.Method, r.URL.Path),
			Resource:      scope.Resource,
			Context:       requestCtx,
		}
		decision, _ := backendsecurity.StaticAuthorizer{
			Effect:     contracts.DecisionEffectDeny,
			ReasonCode: "authz.denied_required_role",
		}.Authorize(r.Context(), authReq)
		s.emitNormalizedSecurityAuditAsync(identity, contracts.AccessContext{}, decision, authReq, "denied")
		return normalizedSecurityEnvelope{}, roleErr
	}
	trustedMemberships, membershipErr := s.resolveTrustedMembershipSnapshot(r, principalID)
	if membershipErr != nil {
		return normalizedSecurityEnvelope{}, &tenantScopeError{
			Code:       "AUTHZ_CONTEXT_FAILED",
			Message:    "failed to load trusted access memberships",
			HTTPStatus: http.StatusInternalServerError,
		}
	}
	r = r.WithContext(context.WithValue(r.Context(), ctxKeyTrustedMemberships, trustedMemberships))
	delegation, delegationErr := s.resolveTrustedDelegationContextStrict(r, principalID, principalType)
	if delegationErr != nil {
		return normalizedSecurityEnvelope{}, &tenantScopeError{
			Code:       "AUTHZ_CONTEXT_FAILED",
			Message:    "failed to load trusted delegation",
			HTTPStatus: http.StatusInternalServerError,
		}
	}
	if delegation != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxKeyTrustedDelegation, delegation))
	}
	identity, idErr := backendsecurity.NormalizeLegacyIdentity(backendsecurity.LegacyIdentityInput{
		PrincipalID:      principalID,
		PrincipalType:    principalType,
		CertificateCN:    s.resolveLegacyCertificateCN(r),
		Roles:            roleList(clientRole),
		AccessID:         trustedMemberships.PrimaryAccessID,
		AllowedAccessIDs: trustedMemberships.AllowedAccessIDs,
		SupportDelegated: delegation != nil,
		DelegationID:     delegationIDOf(delegation),
	})
	if idErr != nil {
		return normalizedSecurityEnvelope{}, &tenantScopeError{
			Code:       "AUTHN_CONTEXT_INVALID",
			Message:    idErr.Error(),
			HTTPStatus: http.StatusUnauthorized,
		}
	}
	access, accessErr := s.resolveAccessContext(r, identity, requestCtx, scope, requireScope)
	if accessErr != nil {
		return normalizedSecurityEnvelope{}, accessErr
	}
	if access.Delegation != nil {
		requestCtx.SupportReason = strings.TrimSpace(access.Delegation.Reason)
		requestCtx.BreakGlass = access.Delegation.BreakGlass
	}

	authReq := contracts.AuthorizationRequest{
		SchemaVersion: contracts.AuthorizationRequestSchemaVersionV1,
		PrincipalID:   identity.PrincipalID,
		PrincipalType: identity.PrincipalType,
		Access:        access,
		Action:        inferAuthorizationAction(r.Method, r.URL.Path),
		Resource:      scope.Resource,
		Context:       requestCtx,
	}

	decision, authErr := s.authorizeNormalizedRequest(r, clientRole, scope, authReq)
	if authErr != nil {
		s.emitNormalizedSecurityAuditAsync(identity, access, decision, authReq, "denied")
		return normalizedSecurityEnvelope{}, authErr
	}

	s.emitNormalizedSecurityAuditAsync(identity, access, decision, authReq, "authorized")

	return normalizedSecurityEnvelope{
		Identity: identity,
		Access:   access,
		Request:  authReq,
		Decision: decision,
	}, nil
}

func (s *Server) resolveAccessContext(r *http.Request, identity contracts.IdentityClaims, requestCtx contracts.AuthorizationContext, scope backendsecurity.ScopeDescriptor, requireScope bool) (contracts.AccessContext, *tenantScopeError) {
	delegation := s.resolveLegacyDelegation(r)
	if scope.ScopeClass == backendsecurity.ScopeClassGlobal {
		access, err := backendsecurity.ResolveAccessContext(backendsecurity.ResolveAccessInput{
			PrincipalID:             identity.PrincipalID,
			PrincipalType:           identity.PrincipalType,
			AccessID:                identity.AccessID,
			AllowedAccessIDs:        identity.AllowedAccessIDs,
			SelectorAccessID:        requestCtx.SelectorAccessID,
			IsInternalAdmin:         identity.InternalAdmin,
			IsSupportDelegated:      identity.SupportDelegated,
			Delegation:              delegation,
			IsGlobalResourceRequest: true,
		})
		if err != nil {
			return contracts.AccessContext{}, &tenantScopeError{
				Code:       "TENANT_SCOPE_FORBIDDEN",
				Message:    err.Error(),
				HTTPStatus: http.StatusForbidden,
			}
		}
		return access, nil
	}

	if requestCtx.SelectorAccessID == "" && !requireScope && delegation == nil {
		return contracts.AccessContext{
			SchemaVersion:           contracts.AccessContextSchemaVersionV1,
			PrincipalID:             identity.PrincipalID,
			PrincipalType:           identity.PrincipalType,
			AccessID:                identity.AccessID,
			AllowedAccessIDs:        identity.AllowedAccessIDs,
			AccessSource:            contracts.AccessSourceClaim,
			AccessResolutionReason:  contracts.AccessResolutionReasonClaimSingleAccess,
			IsInternalAdmin:         identity.InternalAdmin,
			IsSupportDelegated:      identity.SupportDelegated,
			IsGlobalResourceRequest: false,
		}, nil
	}

	access, err := backendsecurity.ResolveAccessContext(backendsecurity.ResolveAccessInput{
		PrincipalID:             identity.PrincipalID,
		PrincipalType:           identity.PrincipalType,
		AccessID:                identity.AccessID,
		AllowedAccessIDs:        identity.AllowedAccessIDs,
		SelectorAccessID:        requestCtx.SelectorAccessID,
		IsInternalAdmin:         identity.InternalAdmin,
		IsSupportDelegated:      identity.SupportDelegated,
		Delegation:              delegation,
		IsGlobalResourceRequest: false,
	})
	if err != nil {
		code := "TENANT_SCOPE_FORBIDDEN"
		status := http.StatusForbidden
		if strings.Contains(err.Error(), "could not be resolved") {
			code = "TENANT_SCOPE_REQUIRED"
			status = http.StatusBadRequest
		}
		return contracts.AccessContext{}, &tenantScopeError{
			Code:       code,
			Message:    err.Error(),
			HTTPStatus: status,
		}
	}
	return access, nil
}

func (s *Server) authorizeNormalizedRequest(r *http.Request, _ string, _ backendsecurity.ScopeDescriptor, req contracts.AuthorizationRequest) (contracts.PolicyDecision, *tenantScopeError) {
	ctx := r.Context()
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		timeout := 2 * time.Second
		if s != nil && s.config != nil && s.config.RequestTimeout > 0 && s.config.RequestTimeout < timeout {
			timeout = s.config.RequestTimeout
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	decision, err := s.currentAuthorizer().Authorize(ctx, req)
	if err != nil {
		return contracts.PolicyDecision{}, &tenantScopeError{
			Code:       "AUTHZ_DECISION_FAILED",
			Message:    "failed to evaluate authorization request",
			HTTPStatus: http.StatusInternalServerError,
		}
	}
	if decision.Effect != contracts.DecisionEffectAllow {
		return decision, &tenantScopeError{
			Code:       "AUTHZ_FORBIDDEN",
			Message:    "authorization denied",
			HTTPStatus: http.StatusForbidden,
		}
	}
	return decision, nil
}

func (s *Server) emitNormalizedSecurityAuditAsync(identity contracts.IdentityClaims, access contracts.AccessContext, decision contracts.PolicyDecision, req contracts.AuthorizationRequest, outcome string) {
	if s == nil || s.audit == nil {
		return
	}
	go func() {
		emitCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if emitErr := (backendsecurity.AuditEmitter{Logger: s.audit}).BuildAndEmit(identity, access, decision, req, outcome); emitErr != nil && s.logger != nil {
			s.logger.WarnContext(emitCtx, "failed to emit normalized security audit")
		}
	}()
}

func (s *Server) currentAuthorizer() backendsecurity.Authorizer {
	if s.authorizer != nil {
		return s.authorizer
	}
	return backendsecurity.Phase1Authorizer{
		RequireResolvedAccess: s.requireAccessScope(),
	}
}

func (s *Server) validateBridgeRole(clientRole string, scope backendsecurity.ScopeDescriptor) *tenantScopeError {
	if !s.requireAccessScope() || scope.RequiredRole == "" {
		return nil
	}
	// JWT callers enter the bridge as authenticated_user and rely on the
	// authorizer for the final decision on policy/workflow/audit read paths.
	// Allow those requests to reach the authorizer instead of failing at the
	// coarse legacy role gate.
	if strings.EqualFold(strings.TrimSpace(clientRole), "authenticated_user") {
		switch strings.TrimSpace(scope.RequiredRole) {
		case "policy_reader", "audit_reader":
			return nil
		}
	}
	if s.hasRole(clientRole, scope.RequiredRole) {
		return nil
	}
	return &tenantScopeError{
		Code:       "AUTHZ_FORBIDDEN",
		Message:    "insufficient permissions",
		HTTPStatus: http.StatusForbidden,
	}
}

func (s *Server) requireAccessScope() bool {
	return strings.EqualFold(strings.TrimSpace(s.config.Environment), "production") || s.config.RequireAuth
}

func (s *Server) resolveLegacyClientRole(r *http.Request) string {
	role, _ := r.Context().Value(ctxKeyClientRole).(string)
	return strings.TrimSpace(role)
}

func (s *Server) resolveLegacyPrincipalID(r *http.Request) string {
	if principalID, _ := r.Context().Value(ctxKeyPrincipalID).(string); strings.TrimSpace(principalID) != "" {
		return strings.TrimSpace(principalID)
	}
	if cn := s.resolveLegacyCertificateCN(r); cn != "" {
		return "cert:" + cn
	}
	if role := s.resolveLegacyClientRole(r); role != "" {
		return "role:" + role
	}
	if !s.requireAccessScope() {
		return "dev:local"
	}
	return "legacy:unauthenticated"
}

func (s *Server) resolveLegacyPrincipalType(r *http.Request) contracts.PrincipalType {
	if principalType, _ := r.Context().Value(ctxKeyPrincipalType).(string); strings.EqualFold(strings.TrimSpace(principalType), string(contracts.PrincipalTypeService)) {
		return contracts.PrincipalTypeService
	}
	if s.resolveLegacyCertificateCN(r) != "" {
		return contracts.PrincipalTypeService
	}
	return contracts.PrincipalTypeUser
}

func (s *Server) resolveLegacyCertificateCN(r *http.Request) string {
	if r == nil || r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	return strings.TrimSpace(r.TLS.PeerCertificates[0].Subject.CommonName)
}

func (s *Server) resolveLegacyDelegation(r *http.Request) *contracts.DelegationContext {
	if r == nil {
		return nil
	}
	if delegation, ok := r.Context().Value(ctxKeyTrustedDelegation).(*contracts.DelegationContext); ok {
		return delegation
	}
	return nil
}

func delegationIDOf(delegation *contracts.DelegationContext) string {
	if delegation == nil {
		return ""
	}
	return delegation.DelegationID
}

func roleList(role string) []string {
	role = strings.TrimSpace(role)
	if role == "" {
		return nil
	}
	return []string{role}
}

func inferAuthorizationAction(method, path string) contracts.Action {
	switch {
	case strings.EqualFold(method, http.MethodGet) && isCollectionPath(path):
		return contracts.ActionList
	case strings.HasSuffix(path, ":approve"):
		return contracts.ActionApprove
	case strings.HasSuffix(path, ":reject"):
		return contracts.ActionReject
	case strings.EqualFold(method, http.MethodPost):
		return contracts.ActionWrite
	case strings.EqualFold(method, http.MethodDelete):
		return contracts.ActionDelete
	default:
		return contracts.ActionRead
	}
}

func isCollectionPath(path string) bool {
	trimmed := strings.TrimSuffix(strings.TrimSpace(path), "/")
	for _, suffix := range []string{
		"/policies",
		"/workflows",
		"/audit",
		"/audit/export",
		"/control/outbox",
		"/control/trace",
		"/control/acks",
		"/control/leases",
	} {
		if strings.HasSuffix(trimmed, suffix) {
			return true
		}
	}
	return false
}

func classifyRequestScope(basePath, method, path string) (backendsecurity.ScopeDescriptor, error) {
	scope, err := backendsecurity.ClassifyHTTPResource(basePath, method, path)
	if err == nil {
		return scope, nil
	}
	inferredBase := inferBasePath(path)
	if inferredBase != "" && !strings.EqualFold(strings.TrimSpace(basePath), inferredBase) {
		return backendsecurity.ClassifyHTTPResource(inferredBase, method, path)
	}
	return backendsecurity.ScopeDescriptor{}, err
}

func inferBasePath(path string) string {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) >= 2 && strings.EqualFold(parts[0], "api") {
		return "/" + parts[0] + "/" + parts[1]
	}
	return ""
}
