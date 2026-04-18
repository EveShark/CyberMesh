package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/pkg/security/contracts"

	"github.com/google/uuid"
)

var (
	errDelegationSelfApproval          = errors.New("delegation self approval forbidden")
	errDelegationPendingRequired       = errors.New("delegation pending status required")
	errDelegationActiveOrPendingOnly   = errors.New("delegation active or pending status required")
	errDelegationActiveAlreadyExists   = errors.New("active support delegation already exists for principal")
	errDelegationBreakGlassDisabled    = errors.New("break-glass is disabled")
	errDelegationBreakGlassApprovalRef = errors.New("break-glass approval reference is required")
	errDelegationScopeAllForbidden     = errors.New("delegation scope-all forbidden")
)

type delegationListResponse struct {
	PrincipalID  string               `json:"principal_id"`
	Delegations  []delegationResponse `json:"delegations"`
	ActiveGrant  *delegationResponse  `json:"active_grant,omitempty"`
	ActiveAccess string               `json:"active_access_id,omitempty"`
}

type delegationResponse struct {
	DelegationID          string                  `json:"delegation_id"`
	PrincipalID           string                  `json:"principal_id"`
	PrincipalType         contracts.PrincipalType `json:"principal_type"`
	Status                string                  `json:"status"`
	ApprovedByPrincipalID string                  `json:"approved_by_principal_id,omitempty"`
	ApprovalReference     string                  `json:"approval_reference,omitempty"`
	ReasonCode            string                  `json:"reason_code"`
	ReasonText            string                  `json:"reason_text"`
	BreakGlass            bool                    `json:"break_glass"`
	StartsAt              string                  `json:"starts_at"`
	ExpiresAt             string                  `json:"expires_at"`
	ApprovedAt            string                  `json:"approved_at,omitempty"`
	RevokedAt             string                  `json:"revoked_at,omitempty"`
	RevokedByPrincipalID  string                  `json:"revoked_by_principal_id,omitempty"`
	RevokedReasonCode     string                  `json:"revoked_reason_code,omitempty"`
	RevokedReasonText     string                  `json:"revoked_reason_text,omitempty"`
	AccessIDs             []string                `json:"access_ids"`
}

type delegationRequest struct {
	AccessIDs         []string `json:"access_ids"`
	ReasonCode        string   `json:"reason_code"`
	ReasonText        string   `json:"reason_text"`
	StartsAt          string   `json:"starts_at,omitempty"`
	ExpiresAt         string   `json:"expires_at"`
	BreakGlass        bool     `json:"break_glass,omitempty"`
	ApprovalReference string   `json:"approval_reference,omitempty"`
}

type delegationRevokeRequest struct {
	ReasonCode string `json:"reason_code"`
	ReasonText string `json:"reason_text"`
}

type delegationApproveRequest struct {
	ApprovalReference string `json:"approval_reference,omitempty"`
}

func (s *Server) handleAuthDelegations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAuthDelegationList(w, r)
	case http.MethodPost:
		s.handleAuthDelegationCreate(w, r)
	default:
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET and POST methods allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAuthDelegationMutation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only POST method allowed", http.StatusMethodNotAllowed)
		return
	}

	ref := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, strings.TrimSuffix(s.config.BasePath, "/")+"/auth/delegations/"))
	delegationID, action, ok := parseDelegationMutationRef(ref)
	if !ok {
		writeErrorResponse(w, r, "INVALID_DELEGATION_PATH", "path must end with :approve or :revoke", http.StatusBadRequest)
		return
	}

	switch action {
	case "approve":
		s.handleAuthDelegationApprove(w, r, delegationID)
	case "revoke":
		s.handleAuthDelegationRevoke(w, r, delegationID)
	default:
		writeErrorResponse(w, r, "INVALID_DELEGATION_PATH", "unsupported delegation action", http.StatusBadRequest)
	}
}

func (s *Server) handleAuthDelegationList(w http.ResponseWriter, r *http.Request) {
	principalID, principalType, ok := resolveAuthenticatedPrincipal(r, s)
	if !ok {
		writeErrorResponse(w, r, "AUTHN_CONTEXT_INVALID", "principal context is required", http.StatusUnauthorized)
		return
	}
	canManage := s.canManageDelegationsForPrincipal(r, principalID)
	listAll := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("scope")), "all")
	limit := parseDelegationListLimit(r)
	grants, err := s.listSupportDelegations(r.Context(), principalID, principalType, listAll, canManage, limit)
	if err != nil {
		if errors.Is(err, errDelegationScopeAllForbidden) {
			writeErrorResponse(w, r, "DELEGATION_SCOPE_FORBIDDEN", "delegation scope requires control_lease_admin", http.StatusForbidden)
			return
		}
		writeErrorResponse(w, r, "DELEGATION_LOOKUP_FAILED", "failed to load support delegations", http.StatusInternalServerError)
		return
	}
	activeGrant, activeGrantErr := s.resolveTrustedDelegationContextStrict(r, principalID, principalType)
	if activeGrantErr != nil {
		writeErrorResponse(w, r, "AUTHZ_CONTEXT_FAILED", "failed to load active delegation context", http.StatusInternalServerError)
		return
	}
	resp := delegationListResponse{
		PrincipalID: principalID,
		Delegations: grants,
	}
	if activeGrant != nil {
		resp.ActiveGrant = delegationContextToResponse(principalID, principalType, activeGrant)
		if len(activeGrant.AccessIDs) == 1 {
			resp.ActiveAccess = activeGrant.AccessIDs[0]
		}
	}
	writeJSONResponse(w, r, NewSuccessResponse(resp), http.StatusOK)
}

func (s *Server) handleAuthDelegationCreate(w http.ResponseWriter, r *http.Request) {
	principalID, principalType, ok := resolveAuthenticatedPrincipal(r, s)
	if !ok {
		writeErrorResponse(w, r, "AUTHN_CONTEXT_INVALID", "principal context is required", http.StatusUnauthorized)
		return
	}
	req, err := decodeDelegationRequest(r)
	if err != nil {
		writeErrorResponse(w, r, "INVALID_REQUEST", "invalid delegation request body", http.StatusBadRequest)
		return
	}
	if !reasonCodePattern.MatchString(req.ReasonCode) {
		writeErrorResponse(w, r, "INVALID_REASON_CODE", "reason_code format is invalid", http.StatusBadRequest)
		return
	}
	if len(req.ReasonText) == 0 || len(req.ReasonText) > 512 {
		writeErrorResponse(w, r, "INVALID_REASON_TEXT", "reason_text is required and must be <= 512 chars", http.StatusBadRequest)
		return
	}

	startsAt, expiresAt, timeErr := parseDelegationWindow(req.StartsAt, req.ExpiresAt)
	if timeErr != nil {
		writeErrorResponse(w, r, "INVALID_DELEGATION_WINDOW", timeErr.Error(), http.StatusBadRequest)
		return
	}
	if req.BreakGlass {
		if !s.config.BreakGlassEnabled {
			writeErrorResponse(w, r, "BREAK_GLASS_DISABLED", "break-glass is disabled", http.StatusForbidden)
			return
		}
		if s.config.BreakGlassMaxDuration > 0 && expiresAt.Sub(startsAt) > s.config.BreakGlassMaxDuration {
			writeErrorResponse(w, r, "INVALID_BREAK_GLASS_WINDOW", "break-glass duration exceeds maximum allowed window", http.StatusBadRequest)
			return
		}
	} else {
		req.ApprovalReference = ""
	}
	accessIDs := normalizeAllowedAccessIDs(req.AccessIDs)
	if len(accessIDs) == 0 {
		writeErrorResponse(w, r, "INVALID_ACCESS_SCOPE", "at least one access_id is required", http.StatusBadRequest)
		return
	}
	authSource, _ := r.Context().Value(ctxKeyAuthSource).(string)
	memberships, membershipErr := s.resolveMembershipSnapshotForAuth(r, principalID, authSource)
	if membershipErr != nil {
		writeErrorResponse(w, r, "AUTHZ_CONTEXT_FAILED", "failed to load trusted access memberships", http.StatusInternalServerError)
		return
	}
	for _, accessID := range accessIDs {
		if !accessListContains(memberships.AllowedAccessIDs, accessID) {
			writeErrorResponse(w, r, "INVALID_ACCESS_SCOPE", "requested access_ids must belong to the caller's trusted memberships", http.StatusForbidden)
			return
		}
	}

	created, err := s.createSupportDelegation(r.Context(), principalID, principalType, req.ReasonCode, req.ReasonText, startsAt, expiresAt, accessIDs, req.BreakGlass, strings.TrimSpace(req.ApprovalReference))
	if err != nil {
		writeErrorResponse(w, r, "DELEGATION_CREATE_FAILED", "failed to create support delegation", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, r, NewSuccessResponse(created), http.StatusCreated)
}

func (s *Server) handleAuthDelegationApprove(w http.ResponseWriter, r *http.Request, delegationID string) {
	approverID, _, ok := resolveAuthenticatedPrincipal(r, s)
	if !ok {
		writeErrorResponse(w, r, "AUTHN_CONTEXT_INVALID", "principal context is required", http.StatusUnauthorized)
		return
	}
	canManage := s.canManageDelegationsForPrincipal(r, approverID)
	if !canManage {
		writeErrorResponse(w, r, "DELEGATION_SCOPE_FORBIDDEN", "delegation approve requires control_lease_admin", http.StatusForbidden)
		return
	}
	req, err := decodeDelegationApproveRequest(r)
	if err != nil {
		writeErrorResponse(w, r, "INVALID_REQUEST", "invalid delegation approval body", http.StatusBadRequest)
		return
	}
	approved, err := s.approveSupportDelegation(r.Context(), delegationID, approverID, strings.TrimSpace(req.ApprovalReference))
	if err != nil {
		writeDelegationMutationError(w, r, err, "DELEGATION_APPROVE_FAILED")
		return
	}
	writeJSONResponse(w, r, NewSuccessResponse(approved), http.StatusOK)
}

func (s *Server) handleAuthDelegationRevoke(w http.ResponseWriter, r *http.Request, delegationID string) {
	actorID, _, ok := resolveAuthenticatedPrincipal(r, s)
	if !ok {
		writeErrorResponse(w, r, "AUTHN_CONTEXT_INVALID", "principal context is required", http.StatusUnauthorized)
		return
	}
	canManage := s.canManageDelegationsForPrincipal(r, actorID)
	if !canManage {
		writeErrorResponse(w, r, "DELEGATION_SCOPE_FORBIDDEN", "delegation revoke requires control_lease_admin", http.StatusForbidden)
		return
	}
	req, err := decodeDelegationRevokeRequest(r)
	if err != nil {
		writeErrorResponse(w, r, "INVALID_REQUEST", "invalid delegation revoke body", http.StatusBadRequest)
		return
	}
	if !reasonCodePattern.MatchString(req.ReasonCode) {
		writeErrorResponse(w, r, "INVALID_REASON_CODE", "reason_code format is invalid", http.StatusBadRequest)
		return
	}
	if len(req.ReasonText) == 0 || len(req.ReasonText) > 512 {
		writeErrorResponse(w, r, "INVALID_REASON_TEXT", "reason_text is required and must be <= 512 chars", http.StatusBadRequest)
		return
	}

	revoked, revokeErr := s.revokeSupportDelegation(r.Context(), delegationID, actorID, req.ReasonCode, req.ReasonText)
	if revokeErr != nil {
		writeDelegationMutationError(w, r, revokeErr, "DELEGATION_REVOKE_FAILED")
		return
	}
	writeJSONResponse(w, r, NewSuccessResponse(revoked), http.StatusOK)
}

func resolveAuthenticatedPrincipal(r *http.Request, s *Server) (string, contracts.PrincipalType, bool) {
	principalID, _ := r.Context().Value(ctxKeyPrincipalID).(string)
	principalTypeValue, _ := r.Context().Value(ctxKeyPrincipalType).(string)
	if strings.TrimSpace(principalID) == "" {
		principalID = s.resolveLegacyPrincipalID(r)
	}
	if strings.TrimSpace(principalID) == "" {
		return "", "", false
	}
	principalType := contracts.PrincipalTypeUser
	if strings.EqualFold(strings.TrimSpace(principalTypeValue), string(contracts.PrincipalTypeService)) {
		principalType = contracts.PrincipalTypeService
	} else if strings.TrimSpace(principalTypeValue) == "" {
		principalType = s.resolveLegacyPrincipalType(r)
	}
	return principalID, principalType, true
}

func parseDelegationMutationRef(ref string) (string, string, bool) {
	ref = strings.Trim(strings.TrimSpace(ref), "/")
	switch {
	case strings.HasSuffix(ref, ":approve"):
		return strings.TrimSpace(strings.TrimSuffix(ref, ":approve")), "approve", strings.TrimSpace(strings.TrimSuffix(ref, ":approve")) != ""
	case strings.HasSuffix(ref, ":revoke"):
		return strings.TrimSpace(strings.TrimSuffix(ref, ":revoke")), "revoke", strings.TrimSpace(strings.TrimSuffix(ref, ":revoke")) != ""
	default:
		return "", "", false
	}
}

func parseDelegationWindow(startsAtRaw, expiresAtRaw string) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	startsAt := now
	if strings.TrimSpace(startsAtRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(startsAtRaw))
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("starts_at must be RFC3339 UTC")
		}
		startsAt = parsed.UTC()
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresAtRaw))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("expires_at must be RFC3339 UTC")
	}
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(startsAt) {
		return time.Time{}, time.Time{}, fmt.Errorf("expires_at must be after starts_at")
	}
	return startsAt, expiresAt, nil
}

func parseDelegationListLimit(r *http.Request) int {
	const (
		defaultLimit = 100
		maxLimit     = 500
	)
	if r == nil {
		return defaultLimit
	}
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultLimit
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultLimit
	}
	if value > maxLimit {
		return maxLimit
	}
	return value
}

func decodeDelegationRequest(r *http.Request) (delegationRequest, error) {
	var req delegationRequest
	if r == nil || r.Body == nil {
		return req, nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if err == io.EOF {
			return req, nil
		}
		return delegationRequest{}, err
	}
	return req, nil
}

func decodeDelegationRevokeRequest(r *http.Request) (delegationRevokeRequest, error) {
	var req delegationRevokeRequest
	if r == nil || r.Body == nil {
		return req, nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if err == io.EOF {
			return req, nil
		}
		return delegationRevokeRequest{}, err
	}
	return req, nil
}

func writeDelegationMutationError(w http.ResponseWriter, r *http.Request, err error, fallbackCode string) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeErrorResponse(w, r, "DELEGATION_NOT_FOUND", "support delegation not found", http.StatusNotFound)
	case errors.Is(err, errDelegationBreakGlassDisabled):
		writeErrorResponse(w, r, "BREAK_GLASS_DISABLED", "break-glass is disabled", http.StatusForbidden)
	case errors.Is(err, errDelegationBreakGlassApprovalRef):
		writeErrorResponse(w, r, "BREAK_GLASS_APPROVAL_REQUIRED", "break-glass approval_reference is required", http.StatusBadRequest)
	case errors.Is(err, errDelegationSelfApproval):
		writeErrorResponse(w, r, "DELEGATION_SELF_APPROVAL_FORBIDDEN", "requester cannot approve their own delegation", http.StatusForbidden)
	case errors.Is(err, errDelegationPendingRequired):
		writeErrorResponse(w, r, "DELEGATION_STATUS_CONFLICT", err.Error(), http.StatusConflict)
	case errors.Is(err, errDelegationActiveAlreadyExists):
		writeErrorResponse(w, r, "DELEGATION_STATUS_CONFLICT", err.Error(), http.StatusConflict)
	case errors.Is(err, errDelegationActiveOrPendingOnly):
		writeErrorResponse(w, r, "DELEGATION_STATUS_CONFLICT", err.Error(), http.StatusConflict)
	default:
		writeErrorResponse(w, r, fallbackCode, "failed to mutate support delegation", http.StatusInternalServerError)
	}
}

func generateDelegationID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func (s *Server) createSupportDelegation(ctx context.Context, principalID string, principalType contracts.PrincipalType, reasonCode, reasonText string, startsAt, expiresAt time.Time, accessIDs []string, breakGlass bool, approvalReference string) (delegationResponse, error) {
	db, err := s.getDB()
	if err != nil {
		return delegationResponse{}, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return delegationResponse{}, err
	}
	defer tx.Rollback()

	delegationID := generateDelegationID()
	const insertDelegation = `
		INSERT INTO support_delegations (
			delegation_id, principal_id, principal_type, approved_by_principal_id,
			reason_code, reason_text, status, break_glass, starts_at, expires_at, approval_reference
		) VALUES ($1,$2,$3,$4,$5,$6,'pending',$7,$8,$9,$10)
	`
	if _, err := tx.ExecContext(ctx, insertDelegation, delegationID, principalID, string(principalType), "", reasonCode, reasonText, breakGlass, startsAt.UTC(), expiresAt.UTC(), approvalReference); err != nil {
		return delegationResponse{}, err
	}

	const insertAccess = `
		INSERT INTO support_delegation_accesses (delegation_id, access_id)
		VALUES ($1, $2)
	`
	for _, accessID := range accessIDs {
		if _, err := tx.ExecContext(ctx, insertAccess, delegationID, accessID); err != nil {
			return delegationResponse{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return delegationResponse{}, err
	}
	return delegationResponse{
		DelegationID:      delegationID,
		PrincipalID:       principalID,
		PrincipalType:     principalType,
		Status:            "pending",
		ApprovalReference: strings.TrimSpace(approvalReference),
		ReasonCode:        reasonCode,
		ReasonText:        reasonText,
		BreakGlass:        breakGlass,
		StartsAt:          startsAt.UTC().Format(time.RFC3339),
		ExpiresAt:         expiresAt.UTC().Format(time.RFC3339),
		AccessIDs:         accessIDs,
	}, nil
}

func currentClientRole(r *http.Request) string {
	if r == nil {
		return ""
	}
	role, _ := r.Context().Value(ctxKeyClientRole).(string)
	return strings.TrimSpace(role)
}

func (s *Server) canManageDelegationsForPrincipal(r *http.Request, principalID string) bool {
	role := currentClientRole(r)
	if strings.EqualFold(role, "control_lease_admin") {
		return true
	}
	if s != nil && s.hasRole(role, "control_lease_admin") {
		return true
	}
	authSource, _ := r.Context().Value(ctxKeyAuthSource).(string)
	snapshot, err := s.resolveMembershipSnapshotForAuth(r, principalID, authSource)
	if err != nil {
		return false
	}
	for _, role := range snapshot.AccessRoles {
		switch normalizeAccessRole(role) {
		case "admin", "platform_admin", "support_delegate":
			return true
		}
	}
	return false
}

func (s *Server) listSupportDelegations(ctx context.Context, principalID string, principalType contracts.PrincipalType, listAll bool, canManage bool, limit int) ([]delegationResponse, error) {
	if listAll && !canManage {
		return nil, errDelegationScopeAllForbidden
	}
	db, err := s.getDB()
	if err != nil {
		return nil, err
	}
	query := `
		SELECT delegation_id, principal_id, principal_type, status, approved_by_principal_id,
		       approval_reference, reason_code, reason_text, break_glass, starts_at, expires_at,
		       approved_at, revoked_at, revoked_by_principal_id, revoked_reason_code, revoked_reason_text
		FROM support_delegations
	`
	var rows *sql.Rows
	if listAll {
		query += `
		ORDER BY
			CASE status
				WHEN 'pending' THEN 0
				WHEN 'active' THEN 1
				WHEN 'revoked' THEN 2
				ELSE 3
			END,
			created_at DESC,
			delegation_id DESC
		LIMIT $1
		`
		rows, err = db.QueryContext(ctx, query, limit)
	} else {
		query += `
		WHERE principal_id = $1
		  AND principal_type = $2
		ORDER BY created_at DESC, delegation_id DESC
		`
		rows, err = db.QueryContext(ctx, query, principalID, string(principalType))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []delegationResponse
	for rows.Next() {
		item, err := scanDelegationRow(rows)
		if err != nil {
			return nil, err
		}
		item.AccessIDs, err = loadTrustedDelegationAccessIDs(ctx, db, item.DelegationID)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) approveSupportDelegation(ctx context.Context, delegationID, approverID, approvalReference string) (delegationResponse, error) {
	db, err := s.getDB()
	if err != nil {
		return delegationResponse{}, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return delegationResponse{}, err
	}
	defer tx.Rollback()

	item, err := loadDelegationForUpdate(ctx, tx, delegationID)
	if err != nil {
		return delegationResponse{}, err
	}
	if strings.EqualFold(strings.TrimSpace(item.PrincipalID), strings.TrimSpace(approverID)) {
		return delegationResponse{}, fmt.Errorf("%w", errDelegationSelfApproval)
	}
	if item.Status != "pending" {
		return delegationResponse{}, fmt.Errorf("%w: support delegation must be pending before approval", errDelegationPendingRequired)
	}
	const activeOverlapQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM support_delegations
			WHERE principal_id = $1
			  AND principal_type = $2
			  AND status = 'active'
			  AND starts_at <= now()
			  AND expires_at > now()
			  AND delegation_id <> $3
		)
	`
	var activeOverlap bool
	if err := tx.QueryRowContext(ctx, activeOverlapQuery, item.PrincipalID, string(item.PrincipalType), delegationID).Scan(&activeOverlap); err != nil {
		return delegationResponse{}, err
	}
	if activeOverlap {
		return delegationResponse{}, fmt.Errorf("%w", errDelegationActiveAlreadyExists)
	}
	if item.BreakGlass {
		if !s.config.BreakGlassEnabled {
			return delegationResponse{}, fmt.Errorf("%w", errDelegationBreakGlassDisabled)
		}
		if strings.TrimSpace(approvalReference) == "" && strings.TrimSpace(item.ApprovalReference) == "" {
			return delegationResponse{}, fmt.Errorf("%w", errDelegationBreakGlassApprovalRef)
		}
	}
	if strings.TrimSpace(approvalReference) == "" {
		approvalReference = item.ApprovalReference
	}

	const updateQuery = `
		UPDATE support_delegations
		SET status = 'active',
		    approved_by_principal_id = $2,
		    approved_at = now(),
		    approval_reference = $3,
		    updated_at = now()
		WHERE delegation_id = $1
	`
	if _, err := tx.ExecContext(ctx, updateQuery, delegationID, approverID, approvalReference); err != nil {
		return delegationResponse{}, err
	}

	item.Status = "active"
	item.ApprovedByPrincipalID = approverID
	item.ApprovalReference = strings.TrimSpace(approvalReference)
	item.ApprovedAt = time.Now().UTC().Format(time.RFC3339)
	item.AccessIDs, err = loadDelegationAccessIDsTx(ctx, tx, delegationID)
	if err != nil {
		return delegationResponse{}, err
	}

	if err := tx.Commit(); err != nil {
		return delegationResponse{}, err
	}
	s.queuePrincipalTupleSyncImmediate(item.PrincipalID)
	return item, nil
}

func (s *Server) revokeSupportDelegation(ctx context.Context, delegationID, actorID, reasonCode, reasonText string) (delegationResponse, error) {
	db, err := s.getDB()
	if err != nil {
		return delegationResponse{}, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return delegationResponse{}, err
	}
	defer tx.Rollback()

	item, err := loadDelegationForUpdate(ctx, tx, delegationID)
	if err != nil {
		return delegationResponse{}, err
	}
	if item.Status != "active" && item.Status != "pending" {
		return delegationResponse{}, fmt.Errorf("%w: support delegation must be active or pending before revoke", errDelegationActiveOrPendingOnly)
	}

	const updateQuery = `
		UPDATE support_delegations
		SET status = 'revoked',
		    revoked_at = now(),
		    revoked_by_principal_id = $2,
		    revoked_reason_code = $3,
		    revoked_reason_text = $4,
		    updated_at = now()
		WHERE delegation_id = $1
	`
	if _, err := tx.ExecContext(ctx, updateQuery, delegationID, actorID, reasonCode, reasonText); err != nil {
		return delegationResponse{}, err
	}

	item.Status = "revoked"
	item.RevokedAt = time.Now().UTC().Format(time.RFC3339)
	item.RevokedByPrincipalID = actorID
	item.RevokedReasonCode = reasonCode
	item.RevokedReasonText = reasonText
	item.AccessIDs, err = loadDelegationAccessIDsTx(ctx, tx, delegationID)
	if err != nil {
		return delegationResponse{}, err
	}

	if err := tx.Commit(); err != nil {
		return delegationResponse{}, err
	}
	s.queuePrincipalTupleSyncImmediate(item.PrincipalID)
	return item, nil
}

func loadDelegationForUpdate(ctx context.Context, tx *sql.Tx, delegationID string) (delegationResponse, error) {
	const query = `
		SELECT delegation_id, principal_id, principal_type, status, approved_by_principal_id,
		       approval_reference, reason_code, reason_text, break_glass, starts_at, expires_at,
		       approved_at, revoked_at, revoked_by_principal_id, revoked_reason_code, revoked_reason_text
		FROM support_delegations
		WHERE delegation_id = $1
		FOR UPDATE
	`
	row := tx.QueryRowContext(ctx, query, delegationID)
	return scanDelegationScanTarget(row)
}

func loadDelegationAccessIDsTx(ctx context.Context, tx *sql.Tx, delegationID string) ([]string, error) {
	const query = `
		SELECT access_id
		FROM support_delegation_accesses
		WHERE delegation_id = $1
		ORDER BY access_id ASC
	`
	rows, err := tx.QueryContext(ctx, query, delegationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var accessID string
		if err := rows.Scan(&accessID); err != nil {
			return nil, err
		}
		accessID = strings.TrimSpace(accessID)
		if accessID == "" || accessListContains(out, accessID) {
			continue
		}
		out = append(out, accessID)
	}
	return out, rows.Err()
}

func decodeDelegationApproveRequest(r *http.Request) (delegationApproveRequest, error) {
	var req delegationApproveRequest
	if r == nil || r.Body == nil {
		return req, nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if err == io.EOF {
			return req, nil
		}
		return delegationApproveRequest{}, err
	}
	return req, nil
}

func scanDelegationRow(rows *sql.Rows) (delegationResponse, error) {
	var (
		item              delegationResponse
		principalType     string
		startsAt          time.Time
		expiresAt         time.Time
		approvedAt        sql.NullTime
		revokedAt         sql.NullTime
		approvedBy        sql.NullString
		approvalReference sql.NullString
		revokedBy         sql.NullString
		revokedReasonCode sql.NullString
		revokedReasonText sql.NullString
	)
	if err := rows.Scan(
		&item.DelegationID,
		&item.PrincipalID,
		&principalType,
		&item.Status,
		&approvedBy,
		&approvalReference,
		&item.ReasonCode,
		&item.ReasonText,
		&item.BreakGlass,
		&startsAt,
		&expiresAt,
		&approvedAt,
		&revokedAt,
		&revokedBy,
		&revokedReasonCode,
		&revokedReasonText,
	); err != nil {
		return delegationResponse{}, err
	}
	item.PrincipalType = contracts.PrincipalType(strings.TrimSpace(principalType))
	item.ApprovedByPrincipalID = strings.TrimSpace(approvedBy.String)
	item.ApprovalReference = strings.TrimSpace(approvalReference.String)
	item.StartsAt = startsAt.UTC().Format(time.RFC3339)
	item.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	if approvedAt.Valid {
		item.ApprovedAt = approvedAt.Time.UTC().Format(time.RFC3339)
	}
	if revokedAt.Valid {
		item.RevokedAt = revokedAt.Time.UTC().Format(time.RFC3339)
	}
	item.RevokedByPrincipalID = strings.TrimSpace(revokedBy.String)
	item.RevokedReasonCode = strings.TrimSpace(revokedReasonCode.String)
	item.RevokedReasonText = strings.TrimSpace(revokedReasonText.String)
	return item, nil
}

func scanDelegationScanTarget(row *sql.Row) (delegationResponse, error) {
	var (
		item              delegationResponse
		principalType     string
		startsAt          time.Time
		expiresAt         time.Time
		approvedAt        sql.NullTime
		revokedAt         sql.NullTime
		approvedBy        sql.NullString
		approvalReference sql.NullString
		revokedBy         sql.NullString
		revokedReasonCode sql.NullString
		revokedReasonText sql.NullString
	)
	if err := row.Scan(
		&item.DelegationID,
		&item.PrincipalID,
		&principalType,
		&item.Status,
		&approvedBy,
		&approvalReference,
		&item.ReasonCode,
		&item.ReasonText,
		&item.BreakGlass,
		&startsAt,
		&expiresAt,
		&approvedAt,
		&revokedAt,
		&revokedBy,
		&revokedReasonCode,
		&revokedReasonText,
	); err != nil {
		return delegationResponse{}, err
	}
	item.PrincipalType = contracts.PrincipalType(strings.TrimSpace(principalType))
	item.ApprovedByPrincipalID = strings.TrimSpace(approvedBy.String)
	item.ApprovalReference = strings.TrimSpace(approvalReference.String)
	item.StartsAt = startsAt.UTC().Format(time.RFC3339)
	item.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	if approvedAt.Valid {
		item.ApprovedAt = approvedAt.Time.UTC().Format(time.RFC3339)
	}
	if revokedAt.Valid {
		item.RevokedAt = revokedAt.Time.UTC().Format(time.RFC3339)
	}
	item.RevokedByPrincipalID = strings.TrimSpace(revokedBy.String)
	item.RevokedReasonCode = strings.TrimSpace(revokedReasonCode.String)
	item.RevokedReasonText = strings.TrimSpace(revokedReasonText.String)
	return item, nil
}

func delegationContextToResponse(principalID string, principalType contracts.PrincipalType, delegation *contracts.DelegationContext) *delegationResponse {
	if delegation == nil {
		return nil
	}
	return &delegationResponse{
		DelegationID:  delegation.DelegationID,
		PrincipalID:   principalID,
		PrincipalType: principalType,
		Status:        "active",
		ReasonText:    delegation.Reason,
		BreakGlass:    delegation.BreakGlass,
		StartsAt:      delegation.StartTime,
		ExpiresAt:     delegation.ExpiryTime,
		AccessIDs:     append([]string(nil), delegation.AccessIDs...),
	}
}
