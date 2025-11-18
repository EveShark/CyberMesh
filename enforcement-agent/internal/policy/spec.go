package policy

import "time"

// PolicySpec represents an actionable policy derived from control.policy.v1 events.
type PolicySpec struct {
	ID               string
	RuleType         string
	Action           string
	Target           Target
	Criteria         Criteria
	Guardrails       Guardrails
	Audit            Audit
	EffectiveHeight  int64
	ExpirationHeight int64
	Timestamp        time.Time
	Raw              map[string]any
	Tenant           string
	Region           string
}

// Target describes the enforcement scope of a block policy.
type Target struct {
	IPs       []string
	CIDRs     []string
	Direction string
	Scope     string
	Selectors map[string]string
	Namespace string
	Ports     []PortRange
	Protocols []string
	Tenant    string
	Region    string
}

// PortRange represents an inclusive port range.
type PortRange struct {
	From int
	To   int
}

// Criteria encapsulates optional conditions.
type Criteria struct {
	MinConfidence     *float64
	AttemptsPerWindow *int64
	WindowSeconds     *int64
}

// Guardrails controls safety limits.
type Guardrails struct {
	TTLSeconds              int64
	CIDRMaxPrefixLen        *int64
	MaxTargets              *int64
	DryRun                  bool
	CanaryScope             bool
	RollbackIfNoCommitAfter *int64
	PreConsensusTTLSeconds  *int64
	FastPathEnabled         bool
	FastPathTTLSeconds      *int64
	FastPathSignalsRequired *int64
	FastPathConfidenceMin   *float64
	FastPathCanaryScope     bool
	AllowlistIPs            []string
	AllowlistCIDRs          []string
	AllowlistNamespaces     []string
	MaxActivePolicies       *int64
	MaxPoliciesPerMinute    *int64
	ApprovalRequired        bool
	RateLimit               *RateLimit
	RateLimitPerTenant      *int64
	RateLimitPerRegion      *int64
	EscalationCooldown      *int64
}

// Audit contains metadata for traceability.
type Audit struct {
	ReasonCode   string
	EvidenceRefs []string
}

// RateLimit captures guardrail-specific throttling configuration.
type RateLimit struct {
	WindowSeconds int64
	MaxActions    int64
}
