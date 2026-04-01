package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

const (
	namespaceScopePrefix = "namespace:"
	nodeScopePrefix      = "node:"
	clusterScopeKey      = "cluster"
	globalScopeKey       = "global"
)

// ScopeIdentifier returns the semantic enforcement scope for a policy.
func ScopeIdentifier(spec PolicySpec) string {
	scope := strings.ToLower(strings.TrimSpace(spec.Target.Scope))
	switch scope {
	case "namespace":
		if ns := effectiveNamespace(spec); ns != "" {
			return namespaceScopePrefix + ns
		}
		return globalScopeKey
	case "tenant":
		if tenant := effectiveTenant(spec); tenant != "" {
			return "tenant:" + tenant
		}
		return globalScopeKey
	case "region":
		if region := effectiveRegion(spec); region != "" {
			return "region:" + region
		}
		return globalScopeKey
	case "node":
		if node := effectiveNode(spec); node != "" {
			return nodeScopePrefix + node
		}
		return globalScopeKey
	case "cluster":
		return clusterScopeKey
	case "global", "", "any", "both":
		return globalScopeKey
	default:
		return scope
	}
}

// RateScope returns the guardrail throttling bucket for a policy.
// Cluster/global-scoped target-specific policies use a refined bucket so
// unrelated IP/CIDR actions do not all collide in the same "cluster" limiter.
func RateScope(spec PolicySpec) string {
	base := ScopeIdentifier(spec)
	switch base {
	case clusterScopeKey, globalScopeKey:
		if suffix := rateScopeSuffix(spec); suffix != "" {
			return base + ":" + suffix
		}
	}
	return base
}

// WindowScope returns the throttling bucket for windowed rate limits.
func WindowScope(spec PolicySpec) string {
	return "window:" + RateScope(spec)
}

func rateScopeSuffix(spec PolicySpec) string {
	parts := make([]string, 0, 4)
	if tenant := effectiveTenant(spec); tenant != "" {
		parts = append(parts, "tenant", tenant)
	}
	if region := effectiveRegion(spec); region != "" {
		parts = append(parts, "region", region)
	}
	if target := targetDiscriminator(spec); target != "" {
		parts = append(parts, target)
	}
	return strings.Join(parts, ":")
}

func targetDiscriminator(spec PolicySpec) string {
	direction := strings.ToLower(strings.TrimSpace(spec.Target.Direction))
	ips := normalizeSorted(spec.Target.IPs)
	cidrs := normalizeSorted(spec.Target.CIDRs)
	protocols := normalizeSorted(spec.Target.Protocols)
	ports := normalizePorts(spec.Target.Ports)

	if len(ips) == 1 && len(cidrs) == 0 {
		if direction != "" {
			return "ip:" + ips[0] + ":dir:" + direction
		}
		return "ip:" + ips[0]
	}
	if len(cidrs) == 1 && len(ips) == 0 {
		if direction != "" {
			return "cidr:" + cidrs[0] + ":dir:" + direction
		}
		return "cidr:" + cidrs[0]
	}
	if ns := effectiveNamespace(spec); ns != "" {
		if direction != "" {
			return "namespace:" + ns + ":dir:" + direction
		}
		return "namespace:" + ns
	}
	if node := effectiveNode(spec); node != "" {
		if direction != "" {
			return "node:" + node + ":dir:" + direction
		}
		return "node:" + node
	}

	parts := make([]string, 0, 5)
	if direction != "" {
		parts = append(parts, "dir="+direction)
	}
	if len(ips) > 0 {
		parts = append(parts, "ips="+strings.Join(ips, ","))
	}
	if len(cidrs) > 0 {
		parts = append(parts, "cidrs="+strings.Join(cidrs, ","))
	}
	if len(protocols) > 0 {
		parts = append(parts, "protocols="+strings.Join(protocols, ","))
	}
	if len(ports) > 0 {
		parts = append(parts, "ports="+strings.Join(ports, ","))
	}
	if len(parts) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "target:" + hex.EncodeToString(sum[:8])
}

func effectiveNamespace(spec PolicySpec) string {
	if spec.Target.Namespace != "" {
		return canonicalNamespace(spec.Target.Namespace)
	}
	if ns, ok := spec.Target.Selectors["namespace"]; ok {
		return canonicalNamespace(ns)
	}
	if ns, ok := spec.Target.Selectors["kubernetes_namespace"]; ok {
		return canonicalNamespace(ns)
	}
	return ""
}

func effectiveTenant(spec PolicySpec) string {
	if spec.Target.Tenant != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Tenant))
	}
	if spec.Tenant != "" {
		return strings.ToLower(strings.TrimSpace(spec.Tenant))
	}
	return ""
}

func effectiveRegion(spec PolicySpec) string {
	if spec.Target.Region != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Region))
	}
	if spec.Region != "" {
		return strings.ToLower(strings.TrimSpace(spec.Region))
	}
	return ""
}

func effectiveNode(spec PolicySpec) string {
	if node, ok := spec.Target.Selectors["node"]; ok {
		return strings.ToLower(strings.TrimSpace(node))
	}
	return ""
}

func normalizeSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizePorts(ports []PortRange) []string {
	if len(ports) == 0 {
		return nil
	}
	out := make([]string, 0, len(ports))
	for _, port := range ports {
		out = append(out, strconv.Itoa(port.From)+"-"+strconv.Itoa(port.To))
	}
	sort.Strings(out)
	return out
}
