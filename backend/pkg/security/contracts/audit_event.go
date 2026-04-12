package contracts

// AuditDecision records the enforcement result captured in an audit event.
type AuditDecision string

const (
	AuditEventSchemaVersionV1 string = "security.audit_event.v1"

	AuditDecisionAllow AuditDecision = "allow"
	AuditDecisionDeny  AuditDecision = "deny"
	AuditDecisionError AuditDecision = "error"
)

// AuditActor describes who initiated the action.
type AuditActor struct {
	PrincipalID       string
	PrincipalType     PrincipalType
	WorkloadID        string
	SessionID         string
	DelegationID      string
	ApprovalReference string
	SupportReason     string
	BreakGlass        bool
}

// AuditTarget captures the resource and access boundary of the event.
type AuditTarget struct {
	AccessID            string
	ResourceType        string
	ResourceID          string
	IsGlobalResource    bool
	SelectorAccessID    string
	AuthorizedAccessIDs []string
}

// AuditEvent is the normalized event emitted from enforcement points.
type AuditEvent struct {
	AuditEventID    string
	DecisionID      string
	RequestID       string
	TraceID         string
	SourceEventID   string
	SentinelEventID string
	PolicyID        string
	ScopeIdentifier string
	// Timestamp must be RFC3339 UTC.
	Timestamp     string
	SchemaVersion string
	Decision      AuditDecision
	ReasonCode    string
	ReasonDetail  string
	PolicySource  string
	PolicyStore   string
	PolicyModel   string
	Action        Action
	Actor         AuditActor
	Target        AuditTarget
	RequestPath   string
	HTTPMethod    string
	Outcome       string
}
