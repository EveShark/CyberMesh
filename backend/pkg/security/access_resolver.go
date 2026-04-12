package security

import (
	"fmt"
	"strings"

	"backend/pkg/security/contracts"
)

// ResolveAccessInput captures the minimum normalized data needed to build an
// access context from the current backend request path.
type ResolveAccessInput struct {
	PrincipalID             string
	PrincipalType           contracts.PrincipalType
	AccessID                string
	AllowedAccessIDs        []string
	SelectorAccessID        string
	IsInternalAdmin         bool
	IsSupportDelegated      bool
	Delegation              *contracts.DelegationContext
	IsGlobalResourceRequest bool
}

// ResolveAccessContext converts normalized identity and request selector data
// into the shared access contract.
func ResolveAccessContext(in ResolveAccessInput) (contracts.AccessContext, error) {
	principalID := strings.TrimSpace(in.PrincipalID)
	if principalID == "" {
		return contracts.AccessContext{}, fmt.Errorf("principal_id is required")
	}
	if in.PrincipalType != contracts.PrincipalTypeUser && in.PrincipalType != contracts.PrincipalTypeService {
		return contracts.AccessContext{}, fmt.Errorf("principal_type is required")
	}

	allowedAccessIDs := normalizeStringSet(in.AllowedAccessIDs)
	accessID := strings.TrimSpace(in.AccessID)
	if accessID != "" && !containsFold(allowedAccessIDs, accessID) {
		allowedAccessIDs = append([]string{accessID}, allowedAccessIDs...)
	}

	selectorAccessID := strings.TrimSpace(in.SelectorAccessID)
	if in.IsGlobalResourceRequest && selectorAccessID != "" {
		return contracts.AccessContext{}, fmt.Errorf("global resource request cannot carry selector_access_id")
	}

	source := contracts.AccessSourceClaim
	reason := contracts.AccessResolutionReasonClaimSingleAccess
	activeAccessID := ""

	if in.Delegation != nil {
		delegated := normalizeStringSet(in.Delegation.AccessIDs)
		if len(delegated) > 0 {
			allowedAccessIDs = delegated
			source = contracts.AccessSourceDelegation
			reason = contracts.AccessResolutionReasonDelegation
		}
	}

	switch {
	case selectorAccessID != "":
		if !containsFold(allowedAccessIDs, selectorAccessID) {
			return contracts.AccessContext{}, fmt.Errorf("selector_access_id is outside allowed_access_ids")
		}
		activeAccessID = selectorAccessID
		source = contracts.AccessSourceSelector
		reason = contracts.AccessResolutionReasonSelector
	case len(allowedAccessIDs) == 1:
		activeAccessID = allowedAccessIDs[0]
	case in.IsGlobalResourceRequest:
		source = contracts.AccessSourceSystem
		reason = contracts.AccessResolutionReasonSystemGlobal
	default:
		return contracts.AccessContext{}, fmt.Errorf("active_access_id could not be resolved")
	}

	return contracts.AccessContext{
		SchemaVersion:           contracts.AccessContextSchemaVersionV1,
		PrincipalID:             principalID,
		PrincipalType:           in.PrincipalType,
		AccessID:                accessID,
		AllowedAccessIDs:        allowedAccessIDs,
		ActiveAccessID:          activeAccessID,
		AccessSource:            source,
		AccessResolutionReason:  reason,
		IsInternalAdmin:         in.IsInternalAdmin,
		IsSupportDelegated:      in.IsSupportDelegated,
		Delegation:              in.Delegation,
		IsGlobalResourceRequest: in.IsGlobalResourceRequest,
	}, nil
}

func normalizeStringSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || containsFold(out, trimmed) {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
