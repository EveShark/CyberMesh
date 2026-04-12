package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"backend/pkg/security/contracts"
)

func (s *Server) loadTrustedDelegationContext(ctx context.Context, principalID string, principalType contracts.PrincipalType) (*contracts.DelegationContext, error) {
	return s.loadTrustedDelegationContextWithMode(ctx, principalID, principalType, false)
}

func (s *Server) loadTrustedDelegationContextStrict(ctx context.Context, principalID string, principalType contracts.PrincipalType) (*contracts.DelegationContext, error) {
	return s.loadTrustedDelegationContextWithMode(ctx, principalID, principalType, true)
}

func (s *Server) loadTrustedDelegationContextWithMode(ctx context.Context, principalID string, principalType contracts.PrincipalType, strict bool) (*contracts.DelegationContext, error) {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return nil, nil
	}
	if principalType != contracts.PrincipalTypeUser && principalType != contracts.PrincipalTypeService {
		return nil, nil
	}
	if principalType == contracts.PrincipalTypeUser && !strings.HasPrefix(principalID, "user:") {
		return nil, nil
	}
	if principalType == contracts.PrincipalTypeService &&
		!strings.HasPrefix(principalID, "service:") &&
		!strings.HasPrefix(principalID, "cert:") {
		return nil, nil
	}

	db, err := s.getDB()
	if err != nil {
		if strict {
			return nil, fmt.Errorf("load trusted delegation: %w", err)
		}
		return nil, nil
	}

	const delegationQuery = `
		SELECT delegation_id,
		       approved_by_principal_id,
		       approval_reference,
		       reason_code,
		       reason_text,
		       break_glass,
		       starts_at,
		       expires_at
		FROM support_delegations
		WHERE principal_id = $1
		  AND principal_type = $2
		  AND status = 'active'
		  AND starts_at <= now()
		  AND expires_at > now()
		ORDER BY starts_at DESC, delegation_id ASC
		LIMIT 2
	`

	rows, err := db.QueryContext(ctx, delegationQuery, principalID, string(principalType))
	if err != nil {
		if !strict {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	type delegationRow struct {
		delegationID string
		approverID   string
		approvalRef  string
		reasonCode   string
		reasonText   string
		breakGlass   bool
		startsAt     time.Time
		expiresAt    time.Time
	}

	var matches []delegationRow
	for rows.Next() {
		var row delegationRow
		if err := rows.Scan(&row.delegationID, &row.approverID, &row.approvalRef, &row.reasonCode, &row.reasonText, &row.breakGlass, &row.startsAt, &row.expiresAt); err != nil {
			if !strict {
				return nil, nil
			}
			return nil, err
		}
		matches = append(matches, row)
	}
	if err := rows.Err(); err != nil {
		if !strict {
			return nil, nil
		}
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple active support delegations found for principal_id %q", principalID)
	}
	if matches[0].breakGlass {
		if s == nil || s.config == nil || !s.config.BreakGlassEnabled {
			return nil, fmt.Errorf("break-glass is disabled")
		}
		if strings.TrimSpace(matches[0].approvalRef) == "" {
			return nil, fmt.Errorf("break-glass approval reference is required")
		}
	}

	accessIDs, err := loadTrustedDelegationAccessIDs(ctx, db, matches[0].delegationID)
	if err != nil {
		if !strict {
			return nil, nil
		}
		return nil, err
	}
	if len(accessIDs) == 0 {
		return nil, fmt.Errorf("support delegation %q has no authorized access_ids", matches[0].delegationID)
	}

	reason := strings.TrimSpace(matches[0].reasonText)
	if reason == "" {
		reason = strings.TrimSpace(matches[0].reasonCode)
	}

	return &contracts.DelegationContext{
		DelegationID:      strings.TrimSpace(matches[0].delegationID),
		ApproverID:        strings.TrimSpace(matches[0].approverID),
		ApprovalReference: strings.TrimSpace(matches[0].approvalRef),
		Reason:            reason,
		StartTime:         matches[0].startsAt.UTC().Format(time.RFC3339),
		ExpiryTime:        matches[0].expiresAt.UTC().Format(time.RFC3339),
		AccessIDs:         accessIDs,
		BreakGlass:        matches[0].breakGlass,
	}, nil
}

func loadTrustedDelegationAccessIDs(ctx context.Context, db *sql.DB, delegationID string) ([]string, error) {
	const accessQuery = `
		SELECT access_id
		FROM support_delegation_accesses
		WHERE delegation_id = $1
		ORDER BY access_id ASC
	`

	rows, err := db.QueryContext(ctx, accessQuery, delegationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accessIDs []string
	for rows.Next() {
		var accessID string
		if err := rows.Scan(&accessID); err != nil {
			return nil, err
		}
		accessID = strings.TrimSpace(accessID)
		if accessID == "" || accessListContains(accessIDs, accessID) {
			continue
		}
		accessIDs = append(accessIDs, accessID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accessIDs, nil
}

func (s *Server) resolveTrustedDelegationContext(r *http.Request, principalID string, principalType contracts.PrincipalType) (*contracts.DelegationContext, error) {
	if r == nil {
		return nil, nil
	}
	if cached, ok := r.Context().Value(ctxKeyTrustedDelegation).(*contracts.DelegationContext); ok && cached != nil {
		return cached, nil
	}
	return s.loadTrustedDelegationContext(r.Context(), principalID, principalType)
}

func (s *Server) resolveTrustedDelegationContextStrict(r *http.Request, principalID string, principalType contracts.PrincipalType) (*contracts.DelegationContext, error) {
	if r == nil {
		return nil, nil
	}
	if cached, ok := r.Context().Value(ctxKeyTrustedDelegation).(*contracts.DelegationContext); ok && cached != nil {
		return cached, nil
	}
	return s.loadTrustedDelegationContextStrict(r.Context(), principalID, principalType)
}
