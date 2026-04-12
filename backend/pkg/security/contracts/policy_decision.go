package contracts

// DecisionEffect is the final authorization outcome.
type DecisionEffect string

const (
	PolicyDecisionSchemaVersionV1 string = "security.policy_decision.v1"

	DecisionEffectAllow DecisionEffect = "allow"
	DecisionEffectDeny  DecisionEffect = "deny"
	DecisionEffectError DecisionEffect = "error"
)

// DecisionSource identifies which policy system produced the decision.
type DecisionSource string

const (
	DecisionSourceOpenFGA  DecisionSource = "openfga"
	DecisionSourceStatic   DecisionSource = "static"
	DecisionSourceDelegate DecisionSource = "delegation"
)

// ObligationType defines a post-decision constraint that callers must enforce.
type ObligationType string

const (
	ObligationTypeMaskFields       ObligationType = "mask_fields"
	ObligationTypeLimitAccessScope ObligationType = "limit_access_scope"
	ObligationTypeRequireStepUp    ObligationType = "require_step_up"
	ObligationTypeReadOnly         ObligationType = "read_only"
)

// DecisionObligation carries an additional enforcement constraint returned with a decision.
type DecisionObligation struct {
	Type    ObligationType
	Value   string
	Values  []string
	Message string
}

// PolicyDecision is the normalized AuthZ decision contract.
type PolicyDecision struct {
	SchemaVersion string
	DecisionID    string
	Effect        DecisionEffect
	Source        DecisionSource
	PolicyStore   string
	PolicyModel   string
	PrincipalID   string
	PrincipalType PrincipalType
	Action        Action
	Resource      ResourceRef
	AccessID      string
	ReasonCode    string
	ReasonDetail  string
	RequestID     string
	TraceID       string
	Obligations   []DecisionObligation
	// ExpiresAt must be RFC3339 UTC when present.
	ExpiresAt string
	Cached    bool
}
