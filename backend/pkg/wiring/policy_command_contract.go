package wiring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const enforcementCommandSchemaVersion = "enforcement.command.v1"

type commandContract struct {
	SchemaVersion  string
	CommandID      string
	RequestID      string
	TraceID        string
	WorkflowID     string
	PolicyID       string
	ActionType     string
	ControlAction  string
	RuleType       string
	RuleHashHex    string
	IdempotencyKey string
	Tenant         string
	Region         string
	ScopeID        string
	TTLSeconds     int64
	IssuedAtMs     int64
	FallbackUsed   bool
}

type commandContractOptions struct {
	Strict bool
}

func buildCommandContract(raw []byte, payload policyParams, policyID, controlAction, action, ruleType string, blockTime time.Time) (commandContract, error) {
	c := commandContract{
		SchemaVersion: enforcementCommandSchemaVersion,
		PolicyID:      strings.TrimSpace(policyID),
		ActionType:    strings.ToLower(strings.TrimSpace(action)),
		ControlAction: strings.ToLower(strings.TrimSpace(controlAction)),
		RuleType:      strings.ToLower(strings.TrimSpace(ruleType)),
		Tenant:        strings.TrimSpace(payload.Tenant),
		Region:        strings.TrimSpace(payload.Region),
		TTLSeconds:    int64(payload.getTTLSeconds()),
		IssuedAtMs:    blockTime.UTC().UnixMilli(),
	}

	hash := sha256.Sum256(raw)
	c.RuleHashHex = hex.EncodeToString(hash[:])

	ids := extractContractIDs(raw)
	c.TraceID = ids.TraceID
	c.RequestID = ids.RequestID
	c.CommandID = ids.CommandID
	c.WorkflowID = ids.WorkflowID
	c.ScopeID = strings.TrimSpace(extractStringFromAny(raw, "scope_identifier"))
	if c.ScopeID == "" {
		c.ScopeID = strings.TrimSpace(extractStringFromAny(raw, "scope_id"))
	}

	if c.CommandID == "" {
		c.CommandID = synthesizeDeterministicID("cmd", c.PolicyID, c.ActionType, c.TraceID, c.RuleHashHex)
		c.FallbackUsed = true
	}
	if c.RequestID == "" {
		c.RequestID = synthesizeDeterministicID("req", c.PolicyID, c.TraceID, c.CommandID)
		c.FallbackUsed = true
	}
	if c.TraceID == "" {
		c.TraceID = synthesizeDeterministicID("trace", c.PolicyID, c.CommandID, c.RuleHashHex)
		c.FallbackUsed = true
	}
	if c.IdempotencyKey == "" {
		c.IdempotencyKey = synthesizeDeterministicID("idem", c.PolicyID, c.CommandID, c.ActionType, c.RuleHashHex)
	}
	return c, nil
}

func validateCommandContract(c commandContract, opts commandContractOptions) error {
	if strings.TrimSpace(c.SchemaVersion) == "" {
		return fmt.Errorf("schema_version_missing")
	}
	if strings.TrimSpace(c.PolicyID) == "" {
		return fmt.Errorf("policy_id_missing")
	}
	if strings.TrimSpace(c.ActionType) == "" {
		return fmt.Errorf("action_type_missing")
	}
	if strings.TrimSpace(c.ControlAction) == "" {
		return fmt.Errorf("control_action_missing")
	}
	if strings.TrimSpace(c.RuleType) == "" {
		return fmt.Errorf("rule_type_missing")
	}
	if strings.TrimSpace(c.CommandID) == "" {
		return fmt.Errorf("command_id_missing")
	}
	if strings.TrimSpace(c.RequestID) == "" {
		return fmt.Errorf("request_id_missing")
	}
	if strings.TrimSpace(c.TraceID) == "" {
		return fmt.Errorf("trace_id_missing")
	}
	if strings.TrimSpace(c.IdempotencyKey) == "" {
		return fmt.Errorf("idempotency_key_missing")
	}
	if opts.Strict && c.FallbackUsed {
		return fmt.Errorf("contract_ids_fallback_used_in_strict_mode")
	}
	return nil
}

func commandContractHeaders(c commandContract) map[string]string {
	h := map[string]string{
		"command_schema_version": c.SchemaVersion,
		"command_id":             c.CommandID,
		"request_id":             c.RequestID,
		"trace_id":               c.TraceID,
		"workflow_id":            c.WorkflowID,
		"policy_id":              c.PolicyID,
		"action_type":            c.ActionType,
		"control_action":         c.ControlAction,
		"rule_type":              c.RuleType,
		"rule_hash_hex":          c.RuleHashHex,
		"idempotency_key":        c.IdempotencyKey,
		"tenant":                 c.Tenant,
		"region":                 c.Region,
		"scope_identifier":       c.ScopeID,
		"contract_fallback_used": boolToString(c.FallbackUsed),
	}
	if c.TTLSeconds > 0 {
		h["ttl_seconds"] = fmt.Sprintf("%d", c.TTLSeconds)
	}
	if c.IssuedAtMs > 0 {
		h["issued_at_ms"] = fmt.Sprintf("%d", c.IssuedAtMs)
	}
	return h
}

func boolToString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func synthesizeDeterministicID(prefix string, parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(filtered, "|")))
	return prefix + "-" + hex.EncodeToString(sum[:8])
}

type contractIDs struct {
	TraceID    string
	RequestID  string
	CommandID  string
	WorkflowID string
}

func extractContractIDs(raw []byte) contractIDs {
	return contractIDs{
		TraceID:    strings.TrimSpace(extractStringFromAny(raw, "trace_id")),
		RequestID:  strings.TrimSpace(extractStringFromAny(raw, "request_id")),
		CommandID:  strings.TrimSpace(extractStringFromAny(raw, "command_id")),
		WorkflowID: strings.TrimSpace(extractStringFromAny(raw, "workflow_id")),
	}
}

func extractStringFromAny(raw []byte, key string) string {
	if len(raw) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if v := extractStringHierarchy(obj, key); v != "" {
		return v
	}
	if params, ok := obj["params"].(map[string]interface{}); ok {
		if v := extractStringHierarchy(params, key); v != "" {
			return v
		}
	}
	return ""
}

func extractStringHierarchy(obj map[string]interface{}, key string) string {
	if obj == nil {
		return ""
	}
	if v, ok := obj[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if meta, ok := obj["metadata"].(map[string]interface{}); ok {
		if v, ok := meta[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if trace, ok := obj["trace"].(map[string]interface{}); ok {
		if v, ok := trace[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		if key == "trace_id" {
			if v, ok := trace["id"].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

