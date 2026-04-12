package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const activeAccessCookieName = "cybermesh_access_id"

type trustedMembershipSnapshot struct {
	PrimaryAccessID  string
	AllowedAccessIDs []string
}

func (s *Server) loadTrustedMembershipSnapshot(ctx context.Context, principalID string) (trustedMembershipSnapshot, error) {
	return s.loadTrustedMembershipSnapshotWithMode(ctx, principalID, false)
}

func (s *Server) loadTrustedMembershipSnapshotStrict(ctx context.Context, principalID string) (trustedMembershipSnapshot, error) {
	return s.loadTrustedMembershipSnapshotWithMode(ctx, principalID, true)
}

func (s *Server) loadTrustedMembershipSnapshotWithMode(ctx context.Context, principalID string, strict bool) (trustedMembershipSnapshot, error) {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return trustedMembershipSnapshot{}, nil
	}

	db, err := s.getDB()
	if err != nil {
		if strict {
			return trustedMembershipSnapshot{}, fmt.Errorf("load trusted access memberships: %w", err)
		}
		return trustedMembershipSnapshot{}, nil
	}

	const query = `
		SELECT access_id, is_primary
		FROM auth_access_memberships
		WHERE principal_id = $1
		  AND status = 'active'
		ORDER BY is_primary DESC, access_id ASC
	`

	rows, err := db.QueryContext(ctx, query, principalID)
	if err != nil {
		return trustedMembershipSnapshot{}, err
	}
	defer rows.Close()

	var snapshot trustedMembershipSnapshot
	for rows.Next() {
		var accessID string
		var isPrimary bool
		if scanErr := rows.Scan(&accessID, &isPrimary); scanErr != nil {
			return trustedMembershipSnapshot{}, scanErr
		}
		accessID = strings.TrimSpace(accessID)
		if accessID == "" || accessListContains(snapshot.AllowedAccessIDs, accessID) {
			continue
		}
		snapshot.AllowedAccessIDs = append(snapshot.AllowedAccessIDs, accessID)
		if isPrimary && snapshot.PrimaryAccessID == "" {
			snapshot.PrimaryAccessID = accessID
		}
	}
	if err := rows.Err(); err != nil {
		return trustedMembershipSnapshot{}, err
	}

	if snapshot.PrimaryAccessID == "" && len(snapshot.AllowedAccessIDs) == 1 {
		snapshot.PrimaryAccessID = snapshot.AllowedAccessIDs[0]
	}
	s.queuePrincipalTupleSync(principalID)
	return snapshot, nil
}

func (s *Server) resolveTrustedMembershipSnapshot(r *http.Request, principalID string) (trustedMembershipSnapshot, error) {
	if r == nil {
		return trustedMembershipSnapshot{}, nil
	}
	if cached, ok := r.Context().Value(ctxKeyTrustedMemberships).(trustedMembershipSnapshot); ok {
		return cached, nil
	}
	return s.loadTrustedMembershipSnapshot(r.Context(), principalID)
}

func (s *Server) resolveTrustedMembershipSnapshotStrict(r *http.Request, principalID string) (trustedMembershipSnapshot, error) {
	if r == nil {
		return trustedMembershipSnapshot{}, nil
	}
	if cached, ok := r.Context().Value(ctxKeyTrustedMemberships).(trustedMembershipSnapshot); ok {
		return cached, nil
	}
	return s.loadTrustedMembershipSnapshotStrict(r.Context(), principalID)
}

func readActiveAccessCookie(r *http.Request) string {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(activeAccessCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func writeActiveAccessCookie(w http.ResponseWriter, r *http.Request, accessID string) {
	if w == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     activeAccessCookieName,
		Value:    strings.TrimSpace(accessID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestUsesTLS(r),
	})
}

func clearActiveAccessCookie(w http.ResponseWriter, r *http.Request) {
	if w == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     activeAccessCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestUsesTLS(r),
	})
}

func requestUsesTLS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func resolveActiveAccessSelection(snapshot trustedMembershipSnapshot, requested string) (string, bool) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		if len(snapshot.AllowedAccessIDs) == 1 {
			return snapshot.AllowedAccessIDs[0], true
		}
		if snapshot.PrimaryAccessID != "" && accessListContains(snapshot.AllowedAccessIDs, snapshot.PrimaryAccessID) {
			return snapshot.PrimaryAccessID, true
		}
		return "", false
	}
	if accessListContains(snapshot.AllowedAccessIDs, requested) {
		return requested, true
	}
	return "", false
}

func normalizeAllowedAccessIDs(values []string) []string {
	out := append([]string(nil), values...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.Compare(strings.ToLower(out[i]), strings.ToLower(out[j])) < 0
	})
	deduped := make([]string, 0, len(out))
	var lastNormalized string
	for _, value := range out {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized := strings.ToLower(trimmed)
		if normalized == lastNormalized {
			continue
		}
		deduped = append(deduped, trimmed)
		lastNormalized = normalized
	}
	return deduped
}

func accessListContains(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

type accessSelectRequest struct {
	AccessID string `json:"access_id"`
}

type accessSelectResponse struct {
	PrincipalID      string   `json:"principal_id"`
	AllowedAccessIDs []string `json:"allowed_access_ids"`
	ActiveAccessID   string   `json:"active_access_id,omitempty"`
}

func decodeAccessSelectRequest(r *http.Request) (accessSelectRequest, error) {
	var req accessSelectRequest
	if r == nil || r.Body == nil {
		return req, nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return req, nil
		}
		return accessSelectRequest{}, err
	}
	return req, nil
}
