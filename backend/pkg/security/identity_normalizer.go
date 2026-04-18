package security

import (
	"fmt"
	"strings"

	"backend/pkg/security/contracts"
)

// LegacyIdentityInput captures the identity material currently available from
// the live backend auth paths before ZITADEL/SPIFFE integration lands.
type LegacyIdentityInput struct {
	PrincipalID       string
	PrincipalType     contracts.PrincipalType
	Subject           string
	WorkloadID        string
	CertificateCN     string
	Issuer            string
	Audience          []string
	Roles             []string
	Groups            []string
	AccessID          string
	AllowedAccessIDs  []string
	InternalAdmin     bool
	SupportDelegated  bool
	DelegationID      string
	SessionID         string
	TokenID           string
	AuthenticationRef string
}

// NormalizeLegacyIdentity builds the shared identity contract from the
// strongest currently available caller identity material.
func NormalizeLegacyIdentity(in LegacyIdentityInput) (contracts.IdentityClaims, error) {
	principalID := strings.TrimSpace(in.PrincipalID)
	principalType := in.PrincipalType

	if principalID == "" {
		switch {
		case strings.TrimSpace(in.Subject) != "":
			principalID = strings.TrimSpace(in.Subject)
			if principalType == "" {
				principalType = contracts.PrincipalTypeUser
			}
		case strings.TrimSpace(in.WorkloadID) != "":
			principalID = strings.TrimSpace(in.WorkloadID)
			if principalType == "" {
				principalType = contracts.PrincipalTypeService
			}
		case strings.TrimSpace(in.CertificateCN) != "":
			principalID = strings.TrimSpace(in.CertificateCN)
			if principalType == "" {
				principalType = contracts.PrincipalTypeService
			}
		}
	}

	if principalID == "" {
		return contracts.IdentityClaims{}, fmt.Errorf("principal_id could not be derived")
	}
	if principalType != contracts.PrincipalTypeUser && principalType != contracts.PrincipalTypeService {
		return contracts.IdentityClaims{}, fmt.Errorf("principal_type is required")
	}

	accessID := strings.TrimSpace(in.AccessID)
	allowedAccessIDs := normalizeStringSet(in.AllowedAccessIDs)
	if accessID != "" && !containsFold(allowedAccessIDs, accessID) {
		allowedAccessIDs = append([]string{accessID}, allowedAccessIDs...)
	}

	return contracts.IdentityClaims{
		SchemaVersion:     contracts.IdentitySchemaVersionV1,
		PrincipalID:       principalID,
		PrincipalType:     principalType,
		AccessID:          accessID,
		Issuer:            strings.TrimSpace(in.Issuer),
		Audience:          normalizeStringSet(in.Audience),
		Subject:           strings.TrimSpace(in.Subject),
		Roles:             normalizeStringSet(in.Roles),
		Groups:            normalizeStringSet(in.Groups),
		AllowedAccessIDs:  allowedAccessIDs,
		InternalAdmin:     in.InternalAdmin,
		SupportDelegated:  in.SupportDelegated,
		DelegationID:      strings.TrimSpace(in.DelegationID),
		SessionID:         strings.TrimSpace(in.SessionID),
		TokenID:           strings.TrimSpace(in.TokenID),
		AuthenticationRef: strings.TrimSpace(in.AuthenticationRef),
	}, nil
}
