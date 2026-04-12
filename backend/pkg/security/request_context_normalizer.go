package security

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"backend/pkg/security/contracts"
)

// LegacyRequestContextInput captures the live request/runtime fields that must
// normalize into the shared authorization context without inventing new names.
type LegacyRequestContextInput struct {
	RequestID       string
	CommandID       string
	WorkflowID      string
	TraceID         string
	SourceEventID   string
	SentinelEventID string
	PolicyID        string
	PolicyVersion   string
	ResourceVersion string
	ScopeIdentifier string
	RequestPath     string
	HTTPMethod      string
	ClientIP        string
	WorkloadID      string
	SupportReason   string
	BreakGlass      bool
	SessionAccessID string
	HeaderAccessID  string
	QueryAccessID   string
	Tenant          string
	TenantID        string
}

// NormalizeLegacyRequestContext returns the Phase 0 authorization context plus
// a single normalized selector access ID derived from the current prod fields.
func NormalizeLegacyRequestContext(in LegacyRequestContextInput) (contracts.AuthorizationContext, error) {
	requestID, ok := normalizeOrGenerateUUID(in.RequestID)
	if !ok {
		return contracts.AuthorizationContext{}, fmt.Errorf("request_id must be a UUID")
	}

	commandID, err := normalizeOptionalUUID("command_id", in.CommandID)
	if err != nil {
		return contracts.AuthorizationContext{}, err
	}
	workflowID, err := normalizeOptionalUUID("workflow_id", in.WorkflowID)
	if err != nil {
		return contracts.AuthorizationContext{}, err
	}

	selectorAccessID, err := normalizeAccessSelector(
		namedValue{name: "session_access_id", value: in.SessionAccessID},
		namedValue{name: "header_access_id", value: in.HeaderAccessID},
		namedValue{name: "query_access_id", value: in.QueryAccessID},
		namedValue{name: "tenant", value: in.Tenant},
		namedValue{name: "tenant_id", value: in.TenantID},
	)
	if err != nil {
		return contracts.AuthorizationContext{}, err
	}

	return contracts.AuthorizationContext{
		RequestID:        requestID,
		TraceID:          strings.TrimSpace(in.TraceID),
		SourceEventID:    strings.TrimSpace(in.SourceEventID),
		SentinelEventID:  strings.TrimSpace(in.SentinelEventID),
		CommandID:        commandID,
		WorkflowID:       workflowID,
		PolicyID:         strings.TrimSpace(in.PolicyID),
		PolicyVersion:    strings.TrimSpace(in.PolicyVersion),
		ResourceVersion:  strings.TrimSpace(in.ResourceVersion),
		ScopeIdentifier:  strings.TrimSpace(in.ScopeIdentifier),
		RequestPath:      strings.TrimSpace(in.RequestPath),
		HTTPMethod:       strings.TrimSpace(in.HTTPMethod),
		ClientIP:         strings.TrimSpace(in.ClientIP),
		WorkloadID:       strings.TrimSpace(in.WorkloadID),
		SupportReason:    strings.TrimSpace(in.SupportReason),
		BreakGlass:       in.BreakGlass,
		SelectorAccessID: selectorAccessID,
	}, nil
}

type namedValue struct {
	name  string
	value string
}

func normalizeAccessSelector(values ...namedValue) (string, error) {
	var selected string
	var selectedName string
	for _, item := range values {
		current := strings.TrimSpace(item.value)
		if current == "" {
			continue
		}
		if selected == "" {
			selected = current
			selectedName = item.name
			continue
		}
		if !strings.EqualFold(selected, current) {
			return "", fmt.Errorf("%s conflicts with %s", item.name, selectedName)
		}
	}
	return selected, nil
}

func normalizeOptionalUUID(field, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%s must be a UUID", field)
	}
	return parsed.String(), nil
}

func normalizeOrGenerateUUID(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return uuid.NewString(), true
		}
		return id.String(), true
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return "", false
	}
	return parsed.String(), true
}
