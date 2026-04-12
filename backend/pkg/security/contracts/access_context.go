package contracts

// AccessSource describes how active access context was resolved.
type AccessSource string

const (
	AccessContextSchemaVersionV1 string = "security.access_context.v1"

	AccessSourceClaim      AccessSource = "claim"
	AccessSourceSelector   AccessSource = "selector"
	AccessSourceDelegation AccessSource = "delegation"
	AccessSourceSystem     AccessSource = "system"
)

// AccessResolutionReason captures the exact rule used to resolve access context.
type AccessResolutionReason string

const (
	AccessResolutionReasonClaimSingleAccess AccessResolutionReason = "claim_single_access"
	AccessResolutionReasonSelector          AccessResolutionReason = "selector"
	AccessResolutionReasonDelegation        AccessResolutionReason = "delegation"
	AccessResolutionReasonSystemGlobal      AccessResolutionReason = "system_global"
)

// DelegationContext captures a verified support or break-glass grant.
type DelegationContext struct {
	DelegationID      string
	ApproverID        string
	ApprovalReference string
	Reason            string
	// StartTime and ExpiryTime must be RFC3339 UTC timestamps.
	StartTime  string
	ExpiryTime string
	AccessIDs  []string
	BreakGlass bool
}

// AccessContext is the normalized access boundary emitted before AuthZ.
type AccessContext struct {
	SchemaVersion           string
	PrincipalID             string
	PrincipalType           PrincipalType
	AccessID                string
	AllowedAccessIDs        []string
	ActiveAccessID          string
	AccessSource            AccessSource
	AccessResolutionReason  AccessResolutionReason
	IsInternalAdmin         bool
	IsSupportDelegated      bool
	Delegation              *DelegationContext
	IsGlobalResourceRequest bool
}
