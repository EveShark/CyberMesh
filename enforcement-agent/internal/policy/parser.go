package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"time"
)

// ParseSpec parses and validates rule data for the supported policy types.
func ParseSpec(ruleType string, ruleData []byte, effectiveHeight, expirationHeight int64, timestamp int64) (PolicySpec, error) {
	var raw map[string]any
	if err := json.Unmarshal(ruleData, &raw); err != nil {
		return PolicySpec{}, fmt.Errorf("policy: decode rule_data: %w", err)
	}

	var spec PolicySpec
	spec.RuleType = ruleType
	spec.Raw = raw
	spec.EffectiveHeight = effectiveHeight
	spec.ExpirationHeight = expirationHeight
	spec.Timestamp = time.Unix(timestamp, 0).UTC()

	switch ruleType {
	case "block":
		return parseBlockPolicy(raw, spec)
	default:
		return PolicySpec{}, fmt.Errorf("policy: unsupported rule type %s", ruleType)
	}
}

func parseBlockPolicy(raw map[string]any, spec PolicySpec) (PolicySpec, error) {
	policyID, err := getString(raw, "policy_id")
	if err != nil {
		return PolicySpec{}, fmt.Errorf("policy: block policy_id: %w", err)
	}
	spec.ID = policyID

	typeVal, err := getString(raw, "rule_type")
	if err == nil && typeVal != "block" {
		return PolicySpec{}, fmt.Errorf("policy: block rule_type must be 'block', got %s", typeVal)
	}

	action, err := getString(raw, "action")
	if err != nil {
		return PolicySpec{}, fmt.Errorf("policy: block action: %w", err)
	}
	spec.Action = action

	// target
	targetRaw, ok := raw["target"].(map[string]any)
	if !ok {
		return PolicySpec{}, errors.New("policy: block target must be object")
	}

	target, err := parseTarget(targetRaw)
	if err != nil {
		return PolicySpec{}, err
	}
	spec.Target = target

	criteria, err := parseCriteria(raw["criteria"])
	if err != nil {
		return PolicySpec{}, err
	}
	spec.Criteria = criteria

	guardrails, err := parseGuardrails(raw["guardrails"])
	if err != nil {
		return PolicySpec{}, err
	}
	spec.Guardrails = guardrails

	if tenantVal, ok := raw["tenant"]; ok {
		tenant, terr := toString(tenantVal)
		if terr != nil {
			return PolicySpec{}, fmt.Errorf("policy: tenant: %w", terr)
		}
		spec.Tenant = strings.TrimSpace(tenant)
	}
	if regionVal, ok := raw["region"]; ok {
		region, rerr := toString(regionVal)
		if rerr != nil {
			return PolicySpec{}, fmt.Errorf("policy: region: %w", rerr)
		}
		spec.Region = strings.TrimSpace(region)
	}

	if guardrails.MaxTargets != nil {
		total := int64(len(target.IPs) + len(target.CIDRs))
		if total > *guardrails.MaxTargets {
			return PolicySpec{}, fmt.Errorf("policy: target count %d exceeds guardrails.max_targets %d", total, *guardrails.MaxTargets)
		}
	}
	if guardrails.FastPathCanaryScope && target.Scope == "" {
		return PolicySpec{}, errors.New("policy: guardrails.fast_path_canary_scope requires target.scope")
	}
	if guardrails.CIDRMaxPrefixLen != nil {
		for _, cidr := range target.CIDRs {
			_, network, _ := net.ParseCIDR(cidr)
			ones, _ := network.Mask.Size()
			if int64(ones) > *guardrails.CIDRMaxPrefixLen {
				return PolicySpec{}, fmt.Errorf("policy: target CIDR %s exceeds guardrails.cidr_max_prefix_len /%d", cidr, *guardrails.CIDRMaxPrefixLen)
			}
		}
	}
	if guardrails.CanaryScope && target.Scope == "" {
		return PolicySpec{}, errors.New("policy: guardrails.canary_scope requires target.scope")
	}
	if guardrails.PreConsensusTTLSeconds != nil && guardrails.TTLSeconds > 0 {
		if *guardrails.PreConsensusTTLSeconds > guardrails.TTLSeconds {
			return PolicySpec{}, errors.New("policy: guardrails.pre_consensus_ttl_seconds cannot exceed ttl_seconds")
		}
	}
	if guardrails.RollbackIfNoCommitAfter != nil && guardrails.TTLSeconds > 0 {
		if *guardrails.RollbackIfNoCommitAfter > guardrails.TTLSeconds {
			return PolicySpec{}, errors.New("policy: guardrails.rollback_if_no_commit_after_s cannot exceed ttl_seconds")
		}
	}

	audit, err := parseAudit(raw["audit"])
	if err != nil {
		return PolicySpec{}, err
	}
	spec.Audit = audit

	return spec, nil
}

func parseTarget(raw map[string]any) (Target, error) {
	var target Target
	if raw == nil {
		return target, errors.New("policy: block target missing")
	}

	if ipsRaw, ok := raw["ips"].([]any); ok {
		for _, ipVal := range ipsRaw {
			ip, err := toString(ipVal)
			if err != nil {
				return target, fmt.Errorf("policy: target.ips: %w", err)
			}
			if net.ParseIP(ip) == nil {
				return target, fmt.Errorf("policy: target.ips contains invalid ip %s", ip)
			}
			target.IPs = append(target.IPs, ip)
		}
	}

	if cidrsRaw, ok := raw["cidrs"].([]any); ok {
		for _, cidrVal := range cidrsRaw {
			cidr, err := toString(cidrVal)
			if err != nil {
				return target, fmt.Errorf("policy: target.cidrs: %w", err)
			}
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return target, fmt.Errorf("policy: target.cidrs invalid %s", cidr)
			}
			target.CIDRs = append(target.CIDRs, cidr)
		}
	}

	if selectorsRaw, ok := raw["selectors"].(map[string]any); ok {
		target.Selectors = make(map[string]string, len(selectorsRaw))
		for key, val := range selectorsRaw {
			str, err := toString(val)
			if err != nil {
				return target, fmt.Errorf("policy: target.selectors: %w", err)
			}
			target.Selectors[key] = str
		}
	}

	if nsVal, ok := raw["namespace"]; ok {
		ns, err := toString(nsVal)
		if err != nil {
			return target, fmt.Errorf("policy: target.namespace: %w", err)
		}
		target.Namespace = canonicalNamespace(ns)
		if target.Selectors == nil {
			target.Selectors = make(map[string]string, 1)
		}
		target.Selectors["namespace"] = target.Namespace
	}

	if ns, ok := target.Selectors["namespace"]; ok {
		target.Namespace = canonicalNamespace(ns)
		target.Selectors["namespace"] = target.Namespace
	}

	if portsRaw, ok := raw["ports"]; ok {
		ports, err := parsePorts(portsRaw)
		if err != nil {
			return target, fmt.Errorf("policy: target.ports: %w", err)
		}
		target.Ports = ports
	}

	if protRaw, ok := raw["protocols"]; ok {
		protocols, err := parseProtocols(protRaw)
		if err != nil {
			return target, fmt.Errorf("policy: target.protocols: %w", err)
		}
		target.Protocols = protocols
	} else if singleProto, ok := raw["protocol"]; ok {
		proto, err := toString(singleProto)
		if err != nil {
			return target, fmt.Errorf("policy: target.protocol: %w", err)
		}
		target.Protocols = []string{strings.ToUpper(strings.TrimSpace(proto))}
	}

	if tenantVal, ok := raw["tenant"]; ok {
		tenant, err := toString(tenantVal)
		if err != nil {
			return target, fmt.Errorf("policy: target.tenant: %w", err)
		}
		target.Tenant = strings.TrimSpace(tenant)
	}
	if regionVal, ok := raw["region"]; ok {
		region, err := toString(regionVal)
		if err != nil {
			return target, fmt.Errorf("policy: target.region: %w", err)
		}
		target.Region = strings.TrimSpace(region)
	}

	if len(target.IPs) == 0 && len(target.CIDRs) == 0 && len(target.Selectors) == 0 {
		return target, errors.New("policy: block target requires ips, cidrs, or selectors")
	}

	if dir, err := getString(raw, "direction"); err == nil && dir != "" {
		if dir != "ingress" && dir != "egress" {
			return target, fmt.Errorf("policy: target.direction invalid %s", dir)
		}
		target.Direction = dir
	}

	if scope, err := getString(raw, "scope"); err == nil && scope != "" {
		normalized := strings.ToLower(strings.TrimSpace(scope))
		switch normalized {
		case "cluster", "namespace", "node", "tenant", "region", "global":
			target.Scope = normalized
		default:
			return target, fmt.Errorf("policy: target.scope invalid %s", scope)
		}
	}

	return target, nil
}

func canonicalNamespace(ns string) string {
	return strings.ToLower(strings.TrimSpace(ns))
}

func parseCriteria(raw any) (Criteria, error) {
	var criteria Criteria
	if raw == nil {
		return criteria, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return criteria, errors.New("policy: criteria must be object")
	}

	if val, ok := m["min_confidence"]; ok {
		f, err := toFloat(val)
		if err != nil {
			return criteria, fmt.Errorf("policy: criteria.min_confidence: %w", err)
		}
		if f < 0 || f > 1 || math.IsNaN(f) {
			return criteria, errors.New("policy: criteria.min_confidence must be between 0 and 1")
		}
		criteria.MinConfidence = ptrFloat(f)
	}

	if val, ok := m["attempts_per_window"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return criteria, errors.New("policy: criteria.attempts_per_window must be positive integer")
		}
		criteria.AttemptsPerWindow = ptrInt(i)
	}

	if val, ok := m["window_s"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return criteria, errors.New("policy: criteria.window_s must be positive integer")
		}
		criteria.WindowSeconds = ptrInt(i)
	}

	return criteria, nil
}

func parseGuardrails(raw any) (Guardrails, error) {
	var guardrails Guardrails
	m, ok := raw.(map[string]any)
	if !ok {
		return guardrails, errors.New("policy: guardrails must be object")
	}

	ttl, err := toInt(m["ttl_seconds"])
	if err != nil || ttl <= 0 {
		return guardrails, errors.New("policy: guardrails.ttl_seconds must be positive integer")
	}
	guardrails.TTLSeconds = ttl

	if val, ok := m["cidr_max_prefix_len"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 || i > 128 {
			return guardrails, errors.New("policy: guardrails.cidr_max_prefix_len must be between 1 and 128")
		}
		guardrails.CIDRMaxPrefixLen = ptrInt(i)
	}

	if val, ok := m["max_targets"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return guardrails, errors.New("policy: guardrails.max_targets must be positive integer")
		}
		guardrails.MaxTargets = ptrInt(i)
	}

	if val, ok := m["dry_run"]; ok {
		guardrails.DryRun = toBool(val)
	}
	if val, ok := m["canary_scope"]; ok {
		guardrails.CanaryScope = toBool(val)
	}
	if val, ok := m["approval_required"]; ok {
		guardrails.ApprovalRequired = toBool(val)
	}
	if val, ok := m["fast_path_canary_scope"]; ok {
		guardrails.FastPathCanaryScope = toBool(val)
	}
	if val, ok := m["rollback_if_no_commit_after_s"]; ok {
		i, err := toInt(val)
		if err == nil && i > 0 {
			guardrails.RollbackIfNoCommitAfter = ptrInt(i)
		}
	}
	if val, ok := m["pre_consensus_ttl_seconds"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return guardrails, errors.New("policy: guardrails.pre_consensus_ttl_seconds must be positive integer")
		}
		guardrails.PreConsensusTTLSeconds = ptrInt(i)
	}
	if val, ok := m["fast_path_enabled"]; ok {
		guardrails.FastPathEnabled = toBool(val)
	}
	if val, ok := m["fast_path_ttl_seconds"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return guardrails, errors.New("policy: guardrails.fast_path_ttl_seconds must be positive integer")
		}
		guardrails.FastPathTTLSeconds = ptrInt(i)
	}
	if val, ok := m["fast_path_signals_required"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return guardrails, errors.New("policy: guardrails.fast_path_signals_required must be positive integer")
		}
		guardrails.FastPathSignalsRequired = ptrInt(i)
	}
	if val, ok := m["fast_path_confidence_min"]; ok {
		f, err := toFloat(val)
		if err != nil || f < 0 || f > 1 || math.IsNaN(f) {
			return guardrails, errors.New("policy: guardrails.fast_path_confidence_min must be between 0 and 1")
		}
		guardrails.FastPathConfidenceMin = ptrFloat(f)
	}

	if rlRaw, ok := m["rate_limit"].(map[string]any); ok {
		rateLimit, err := parseRateLimit(rlRaw)
		if err != nil {
			return guardrails, err
		}
		guardrails.RateLimit = rateLimit
	}

	if val, ok := m["rate_limit_per_tenant"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return guardrails, errors.New("policy: guardrails.rate_limit_per_tenant must be positive integer")
		}
		guardrails.RateLimitPerTenant = ptrInt(i)
	}

	if val, ok := m["rate_limit_per_region"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return guardrails, errors.New("policy: guardrails.rate_limit_per_region must be positive integer")
		}
		guardrails.RateLimitPerRegion = ptrInt(i)
	}

	if val, ok := m["escalation_cooldown_s"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return guardrails, errors.New("policy: guardrails.escalation_cooldown_s must be positive integer")
		}
		guardrails.EscalationCooldown = ptrInt(i)
	}

	if allowlistRaw, ok := m["allowlist"].(map[string]any); ok {
		if ipsRaw, ok := allowlistRaw["ips"].([]any); ok {
			for _, ipVal := range ipsRaw {
				ip, err := toString(ipVal)
				if err != nil {
					return guardrails, fmt.Errorf("policy: guardrails.allowlist.ips: %w", err)
				}
				if net.ParseIP(ip) == nil {
					return guardrails, fmt.Errorf("policy: guardrails.allowlist invalid ip %s", ip)
				}
				guardrails.AllowlistIPs = append(guardrails.AllowlistIPs, ip)
			}
		}
		if cidrsRaw, ok := allowlistRaw["cidrs"].([]any); ok {
			for _, cidrVal := range cidrsRaw {
				cidr, err := toString(cidrVal)
				if err != nil {
					return guardrails, fmt.Errorf("policy: guardrails.allowlist.cidrs: %w", err)
				}
				if _, _, err := net.ParseCIDR(cidr); err != nil {
					return guardrails, fmt.Errorf("policy: guardrails.allowlist cidr invalid %s", cidr)
				}
				guardrails.AllowlistCIDRs = append(guardrails.AllowlistCIDRs, cidr)
			}
		}
		if namespacesRaw, ok := allowlistRaw["namespaces"].([]any); ok {
			for _, nsVal := range namespacesRaw {
				ns, err := toString(nsVal)
				if err != nil {
					return guardrails, fmt.Errorf("policy: guardrails.allowlist.namespaces: %w", err)
				}
				guardrails.AllowlistNamespaces = append(guardrails.AllowlistNamespaces, canonicalNamespace(ns))
			}
		}
	}

	if val, ok := m["max_active_policies"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return guardrails, errors.New("policy: guardrails.max_active_policies must be positive integer")
		}
		guardrails.MaxActivePolicies = ptrInt(i)
	}

	if val, ok := m["max_policies_per_minute"]; ok {
		i, err := toInt(val)
		if err != nil || i <= 0 {
			return guardrails, errors.New("policy: guardrails.max_policies_per_minute must be positive integer")
		}
		guardrails.MaxPoliciesPerMinute = ptrInt(i)
	}

	return guardrails, nil
}

func parseAudit(raw any) (Audit, error) {
	if raw == nil {
		return Audit{}, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return Audit{}, errors.New("policy: audit must be object")
	}

	var audit Audit
	if val, ok := m["reason_code"]; ok {
		str, err := toString(val)
		if err != nil {
			return Audit{}, fmt.Errorf("policy: audit.reason_code: %w", err)
		}
		audit.ReasonCode = str
	}
	if val, ok := m["evidence_refs"].([]any); ok {
		for _, v := range val {
			str, err := toString(v)
			if err != nil {
				return Audit{}, fmt.Errorf("policy: audit.evidence_refs: %w", err)
			}
			audit.EvidenceRefs = append(audit.EvidenceRefs, str)
		}
	}
	return audit, nil
}

func getString(raw map[string]any, key string) (string, error) {
	val, ok := raw[key]
	if !ok {
		return "", fmt.Errorf("missing %s", key)
	}
	return toString(val)
}

func toString(val any) (string, error) {
	switch v := val.(type) {
	case string:
		if v == "" {
			return "", errors.New("value cannot be empty")
		}
		return v, nil
	default:
		return "", fmt.Errorf("expected string, got %T", val)
	}
}

func toInt(val any) (int64, error) {
	switch v := val.(type) {
	case float64:
		return int64(v), nil
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case json.Number:
		i, err := v.Int64()
		return i, err
	case string:
		var num json.Number = json.Number(v)
		return num.Int64()
	case nil:
		return 0, fmt.Errorf("value missing")
	default:
		return 0, fmt.Errorf("expected number, got %T", val)
	}
}

func toFloat(val any) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case json.Number:
		return v.Float64()
	case string:
		var num json.Number = json.Number(v)
		return num.Float64()
	default:
		return 0, fmt.Errorf("expected float, got %T", val)
	}
}

func parsePorts(raw any) ([]PortRange, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, errors.New("ports must be array")
	}
	ranges := make([]PortRange, 0, len(arr))
	for _, entry := range arr {
		switch v := entry.(type) {
		case float64, json.Number, int64, int32, int:
			port, err := toInt(v)
			if err != nil {
				return nil, err
			}
			if port <= 0 || port > 65535 {
				return nil, errors.New("port out of range")
			}
			ranges = append(ranges, PortRange{From: int(port), To: int(port)})
		case map[string]any:
			fromVal, okFrom := v["from"]
			toVal, okTo := v["to"]
			if !okFrom || !okTo {
				return nil, errors.New("port range must include from and to")
			}
			from, err := toInt(fromVal)
			if err != nil {
				return nil, err
			}
			to, err := toInt(toVal)
			if err != nil {
				return nil, err
			}
			if from <= 0 || to <= 0 || from > 65535 || to > 65535 || from > to {
				return nil, errors.New("port range out of bounds")
			}
			ranges = append(ranges, PortRange{From: int(from), To: int(to)})
		default:
			return nil, fmt.Errorf("unsupported port entry %T", entry)
		}
	}
	return ranges, nil
}

func parseProtocols(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []any:
		protocols := make([]string, 0, len(v))
		for _, entry := range v {
			proto, err := toString(entry)
			if err != nil {
				return nil, err
			}
			protocols = append(protocols, normalizeProtocol(proto))
		}
		return protocols, nil
	case []string:
		protocols := make([]string, 0, len(v))
		for _, proto := range v {
			protocols = append(protocols, normalizeProtocol(proto))
		}
		return protocols, nil
	default:
		return nil, errors.New("protocols must be array")
	}
}

func normalizeProtocol(proto string) string {
	p := strings.ToUpper(strings.TrimSpace(proto))
	switch p {
	case "", "ANY":
		return "ANY"
	case "TCP", "UDP", "ICMP", "SCTP", "UDPLITE":
		return p
	default:
		return p
	}
}

func parseRateLimit(raw map[string]any) (*RateLimit, error) {
	windowVal, ok := raw["window_s"]
	if !ok {
		return nil, errors.New("policy: guardrails.rate_limit.window_s required")
	}
	window, err := toInt(windowVal)
	if err != nil || window <= 0 {
		return nil, errors.New("policy: guardrails.rate_limit.window_s must be positive integer")
	}

	maxVal, ok := raw["max_actions"]
	if !ok {
		return nil, errors.New("policy: guardrails.rate_limit.max_actions required")
	}
	maxActions, err := toInt(maxVal)
	if err != nil || maxActions <= 0 {
		return nil, errors.New("policy: guardrails.rate_limit.max_actions must be positive integer")
	}

	return &RateLimit{WindowSeconds: window, MaxActions: maxActions}, nil
}

func toBool(val any) bool {
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

func ptrInt(v int64) *int64       { return &v }
func ptrFloat(v float64) *float64 { return &v }
