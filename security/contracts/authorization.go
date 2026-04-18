package contracts

// Action names an application authorization operation.
type Action string

const (
	AuthorizationRequestSchemaVersionV1 string = "security.authorization_request.v1"

	ActionRead    Action = "read"
	ActionWrite   Action = "write"
	ActionDelete  Action = "delete"
	ActionApprove Action = "approve"
	ActionReject  Action = "reject"
	ActionApply   Action = "apply"
	ActionList    Action = "list"
)

// ResourceRef identifies the target resource under authorization.
type ResourceRef struct {
	Type             string
	ID               string
	IsGlobalResource bool
}

// AuthorizationContext carries request metadata relevant to policy evaluation.
type AuthorizationContext struct {
	RequestID        string
	TraceID          string
	SourceEventID    string
	SentinelEventID  string
	CommandID        string
	WorkflowID       string
	PolicyID         string
	PolicyVersion    string
	ResourceVersion  string
	ScopeIdentifier  string
	RequestPath      string
	HTTPMethod       string
	ClientIP         string
	WorkloadID       string
	SupportReason    string
	BreakGlass       bool
	SelectorAccessID string
}

// AuthorizationRequest is the normalized request sent to the PEP/PDP boundary.
type AuthorizationRequest struct {
	SchemaVersion string
	PrincipalID   string
	PrincipalType PrincipalType
	Access        AccessContext
	Action        Action
	Resource      ResourceRef
	Context       AuthorizationContext
}
