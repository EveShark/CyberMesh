package api

import (
	"net/http"
	"strings"

	"backend/pkg/security/contracts"
)

type authMeResponse struct {
	Authenticated     bool                    `json:"authenticated"`
	AuthSource        string                  `json:"auth_source,omitempty"`
	PrincipalID       string                  `json:"principal_id"`
	PrincipalType     contracts.PrincipalType `json:"principal_type"`
	Subject           string                  `json:"subject,omitempty"`
	PreferredUsername string                  `json:"preferred_username,omitempty"`
	Email             string                  `json:"email,omitempty"`
	AllowedAccessIDs  []string                `json:"allowed_access_ids"`
	ActiveAccessID    string                  `json:"active_access_id,omitempty"`
	ActiveDelegation  *delegationResponse     `json:"active_delegation,omitempty"`
	Capabilities      authCapabilities        `json:"capabilities"`
}

type authCapabilities struct {
	CanSelectAccess         bool `json:"can_select_access"`
	RequiresAccessSelection bool `json:"requires_access_selection"`
	HasActiveAccess         bool `json:"has_active_access"`
	IsGlobalOnly            bool `json:"is_global_only"`
	AllowedAccessCount      int  `json:"allowed_access_count"`
	CanManageDelegations    bool `json:"can_manage_delegations"`
	BreakGlassEnabled       bool `json:"break_glass_enabled"`
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	principalID, _ := r.Context().Value(ctxKeyPrincipalID).(string)
	principalTypeValue, _ := r.Context().Value(ctxKeyPrincipalType).(string)
	if strings.TrimSpace(principalID) == "" {
		principalID = s.resolveLegacyPrincipalID(r)
	}
	principalType := contracts.PrincipalTypeUser
	if strings.EqualFold(strings.TrimSpace(principalTypeValue), string(contracts.PrincipalTypeService)) {
		principalType = contracts.PrincipalTypeService
	} else if principalTypeValue == "" {
		principalType = s.resolveLegacyPrincipalType(r)
	}

	subject, _ := r.Context().Value(ctxKeyAuthSubject).(string)
	username, _ := r.Context().Value(ctxKeyAuthUsername).(string)
	email, _ := r.Context().Value(ctxKeyAuthEmail).(string)
	authSource, _ := r.Context().Value(ctxKeyAuthSource).(string)
	memberships, err := s.resolveMembershipSnapshotForAuth(r, principalID, authSource)
	if err != nil {
		writeErrorResponse(w, r, "AUTHZ_CONTEXT_FAILED", "failed to load trusted access memberships", http.StatusInternalServerError)
		return
	}
	activeAccessID, ok := resolveActiveAccessSelection(memberships, readActiveAccessCookie(r))
	if !ok && readActiveAccessCookie(r) != "" {
		clearActiveAccessCookie(w, r)
	}
	allowedAccessIDs := normalizeAllowedAccessIDs(memberships.AllowedAccessIDs)
	activeDelegation, delegationErr := s.resolveTrustedDelegationContextStrict(r, principalID, principalType)
	if delegationErr != nil {
		writeErrorResponse(w, r, "AUTHZ_CONTEXT_FAILED", "failed to load active delegation context", http.StatusInternalServerError)
		return
	}
	canManageDelegations := s.canManageDelegationsForPrincipal(r, principalID)

	resp := authMeResponse{
		Authenticated:     authSource != "" && !strings.EqualFold(authSource, "developer_fallback"),
		AuthSource:        strings.TrimSpace(authSource),
		PrincipalID:       principalID,
		PrincipalType:     principalType,
		Subject:           strings.TrimSpace(subject),
		PreferredUsername: strings.TrimSpace(username),
		Email:             strings.TrimSpace(email),
		AllowedAccessIDs:  allowedAccessIDs,
		ActiveAccessID:    activeAccessID,
		ActiveDelegation:  delegationContextToResponse(principalID, principalType, activeDelegation),
		Capabilities: authCapabilities{
			CanSelectAccess:         len(allowedAccessIDs) > 0,
			RequiresAccessSelection: len(allowedAccessIDs) > 1 && activeAccessID == "",
			HasActiveAccess:         activeAccessID != "",
			IsGlobalOnly:            len(allowedAccessIDs) == 0,
			AllowedAccessCount:      len(allowedAccessIDs),
			CanManageDelegations:    canManageDelegations,
			BreakGlassEnabled:       s.config != nil && s.config.BreakGlassEnabled,
		},
	}

	writeJSONResponse(w, r, NewSuccessResponse(resp), http.StatusOK)
}

func (s *Server) handleAuthAccessSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only POST method allowed", http.StatusMethodNotAllowed)
		return
	}

	principalID, _ := r.Context().Value(ctxKeyPrincipalID).(string)
	if strings.TrimSpace(principalID) == "" {
		principalID = s.resolveLegacyPrincipalID(r)
	}

	req, err := decodeAccessSelectRequest(r)
	if err != nil {
		writeErrorResponse(w, r, "INVALID_REQUEST", "invalid access selection body", http.StatusBadRequest)
		return
	}

	authSource, _ := r.Context().Value(ctxKeyAuthSource).(string)
	memberships, err := s.resolveMembershipSnapshotForAuth(r, principalID, authSource)
	if err != nil {
		writeErrorResponse(w, r, "AUTHZ_CONTEXT_FAILED", "failed to load trusted access memberships", http.StatusInternalServerError)
		return
	}

	allowedAccessIDs := normalizeAllowedAccessIDs(memberships.AllowedAccessIDs)
	requestedAccessID := strings.TrimSpace(req.AccessID)
	if requestedAccessID == "" {
		clearActiveAccessCookie(w, r)
		writeJSONResponse(w, r, NewSuccessResponse(accessSelectResponse{
			PrincipalID:      principalID,
			AllowedAccessIDs: allowedAccessIDs,
		}), http.StatusOK)
		return
	}

	activeAccessID, ok := resolveActiveAccessSelection(memberships, requestedAccessID)
	if !ok {
		writeErrorResponse(w, r, "ACCESS_SELECTION_FORBIDDEN", "requested access_id is not allowed", http.StatusForbidden)
		return
	}

	writeActiveAccessCookie(w, r, activeAccessID)
	writeJSONResponse(w, r, NewSuccessResponse(accessSelectResponse{
		PrincipalID:      principalID,
		AllowedAccessIDs: allowedAccessIDs,
		ActiveAccessID:   activeAccessID,
	}), http.StatusOK)
}

func (s *Server) resolveMembershipSnapshotForAuth(r *http.Request, principalID, authSource string) (trustedMembershipSnapshot, error) {
	if strings.HasPrefix(strings.TrimSpace(authSource), "developer") {
		return s.resolveTrustedMembershipSnapshot(r, principalID)
	}
	return s.resolveTrustedMembershipSnapshotStrict(r, principalID)
}
