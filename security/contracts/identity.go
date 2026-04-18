package contracts

// PrincipalType identifies the normalized caller class.
type PrincipalType string

const (
	IdentitySchemaVersionV1 string = "security.identity.v1"

	PrincipalTypeUser    PrincipalType = "user"
	PrincipalTypeService PrincipalType = "service"
)

// IdentityClaims is the normalized identity view emitted by AuthN.
type IdentityClaims struct {
	SchemaVersion     string
	PrincipalID       string
	PrincipalType     PrincipalType
	AccessID          string
	Issuer            string
	Audience          []string
	Subject           string
	Roles             []string
	Groups            []string
	AllowedAccessIDs  []string
	InternalAdmin     bool
	SupportDelegated  bool
	DelegationID      string
	SessionID         string
	TokenID           string
	AuthenticationRef string
}
