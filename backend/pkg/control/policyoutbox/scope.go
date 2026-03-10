package policyoutbox

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type scopePayload struct {
	Tenant string `json:"tenant"`
	Region string `json:"region"`
	Target struct {
		Scope     string         `json:"scope"`
		Namespace string         `json:"namespace"`
		Tenant    string         `json:"tenant"`
		Region    string         `json:"region"`
		Direction string         `json:"direction"`
		IPs       []string       `json:"ips"`
		CIDRs     []string       `json:"cidrs"`
		Protocols []string       `json:"protocols"`
		Ports     []any          `json:"ports"`
		Selectors map[string]any `json:"selectors"`
	} `json:"target"`
}

type RoutingOptions struct {
	ClusterShardingMode string
	ClusterShardBuckets int
}

const (
	ClusterShardingOff          = "off"
	ClusterShardingTargetHashV1 = "target_hash_v1"
	MaxClusterShardBuckets      = 256
)

// DeriveScopeIdentifier returns the enforcement routing scope derived from a raw policy payload.
// It mirrors the existing enforcement scope model closely enough for bounded publish-side metrics.
func DeriveScopeIdentifier(raw []byte) (kind string, identifier string, fallback bool) {
	return DeriveScopeIdentifierWithOptions(raw, RoutingOptions{})
}

func NormalizeRoutingOptions(opts RoutingOptions) (RoutingOptions, bool) {
	normalized := opts
	normalized.ClusterShardingMode = strings.ToLower(strings.TrimSpace(normalized.ClusterShardingMode))
	if normalized.ClusterShardingMode == "" {
		normalized.ClusterShardingMode = ClusterShardingOff
	}
	if normalized.ClusterShardBuckets < 1 {
		normalized.ClusterShardBuckets = 1
	}
	if normalized.ClusterShardBuckets > MaxClusterShardBuckets {
		normalized.ClusterShardBuckets = MaxClusterShardBuckets
	}
	unknownMode := false
	switch normalized.ClusterShardingMode {
	case ClusterShardingOff:
		normalized.ClusterShardBuckets = 1
	case ClusterShardingTargetHashV1:
		if normalized.ClusterShardBuckets <= 1 {
			normalized.ClusterShardingMode = ClusterShardingOff
			normalized.ClusterShardBuckets = 1
		}
	default:
		unknownMode = true
		normalized.ClusterShardingMode = ClusterShardingOff
		normalized.ClusterShardBuckets = 1
	}
	return normalized, unknownMode
}

// DeriveScopeIdentifierWithOptions returns the publish routing scope derived from a raw policy payload.
// Cluster-scope sharding only affects delivery routing; enforcement correctness/accounting can stay cluster-global.
func DeriveScopeIdentifierWithOptions(raw []byte, opts RoutingOptions) (kind string, identifier string, fallback bool) {
	opts, _ = NormalizeRoutingOptions(opts)
	if len(raw) == 0 {
		return "unknown", "global", true
	}
	var payload scopePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "unknown", "global", true
	}

	scope := strings.ToLower(strings.TrimSpace(payload.Target.Scope))
	switch scope {
	case "namespace":
		ns := normalizeNamespace(payload)
		if ns == "" {
			return "global", "global", true
		}
		return "namespace", "namespace:" + ns, false
	case "node":
		node := normalizeSelector(payload.Target.Selectors, "node")
		if node == "" {
			return "global", "global", true
		}
		return "node", "node:" + node, false
	case "tenant":
		tenant := normalizeTenant(payload)
		if tenant == "" {
			return "global", "global", true
		}
		return "tenant", "tenant:" + tenant, false
	case "region":
		region := normalizeRegion(payload)
		if region == "" {
			return "global", "global", true
		}
		return "region", "region:" + region, false
	case "cluster":
		if kind, id, ok := deriveClusterRoutingIdentifier(payload, opts); ok {
			return kind, id, false
		}
		return "cluster", "cluster", false
	case "", "global":
		return "global", "global", scope == ""
	default:
		return "global", "global", true
	}
}

func normalizeNamespace(payload scopePayload) string {
	if ns := strings.ToLower(strings.TrimSpace(payload.Target.Namespace)); ns != "" {
		return ns
	}
	return normalizeSelector(payload.Target.Selectors, "namespace")
}

func normalizeTenant(payload scopePayload) string {
	if tenant := strings.ToLower(strings.TrimSpace(payload.Target.Tenant)); tenant != "" {
		return tenant
	}
	return strings.ToLower(strings.TrimSpace(payload.Tenant))
}

func normalizeRegion(payload scopePayload) string {
	if region := strings.ToLower(strings.TrimSpace(payload.Target.Region)); region != "" {
		return region
	}
	return strings.ToLower(strings.TrimSpace(payload.Region))
}

func normalizeSelector(selectors map[string]any, key string) string {
	if len(selectors) == 0 {
		return ""
	}
	val, ok := selectors[key]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(str))
}

func deriveClusterRoutingIdentifier(payload scopePayload, opts RoutingOptions) (string, string, bool) {
	if node := normalizeSelector(payload.Target.Selectors, "node"); node != "" {
		return "node", "node:" + node, true
	}
	if namespace := normalizeNamespace(payload); namespace != "" {
		return "namespace", "namespace:" + namespace, true
	}
	if region := normalizeRegion(payload); region != "" {
		return "region", "region:" + region, true
	}

	if hasConcreteClusterTarget(payload) {
		mode := opts.ClusterShardingMode
		buckets := opts.ClusterShardBuckets
		if mode != "" && mode != ClusterShardingOff && buckets > 1 {
			switch mode {
			case ClusterShardingTargetHashV1:
				fingerprint := canonicalClusterTargetFingerprint(payload)
				sum := sha256.Sum256([]byte(fingerprint))
				bucket := int(binary.BigEndian.Uint64(sum[:8]) % uint64(buckets))
				return "cluster", fmt.Sprintf("cluster:%d", bucket), true
			}
		}
	}

	if tenant := normalizeTenant(payload); tenant != "" {
		return "tenant", "tenant:" + tenant, true
	}

	return "", "", false
}

func canonicalClusterTargetFingerprint(payload scopePayload) string {
	parts := []string{
		"scope=cluster",
		"namespace=" + normalizeNamespace(payload),
		"tenant=" + normalizeTenant(payload),
		"region=" + normalizeRegion(payload),
		"direction=" + strings.ToLower(strings.TrimSpace(payload.Target.Direction)),
		"ips=" + strings.Join(normalizeStrings(payload.Target.IPs), ","),
		"cidrs=" + strings.Join(normalizeStrings(payload.Target.CIDRs), ","),
		"protocols=" + strings.Join(normalizeStrings(payload.Target.Protocols), ","),
		"selectors=" + canonicalSelectors(payload.Target.Selectors),
		"ports=" + canonicalPortList(payload.Target.Ports),
	}
	return strings.Join(parts, "|")
}

func hasConcreteClusterTarget(payload scopePayload) bool {
	if len(normalizeStrings(payload.Target.IPs)) > 0 {
		return true
	}
	if len(normalizeStrings(payload.Target.CIDRs)) > 0 {
		return true
	}
	if len(normalizeStrings(payload.Target.Protocols)) > 0 {
		return true
	}
	if canonicalPortList(payload.Target.Ports) != "" {
		return true
	}
	if selectors := canonicalSelectors(payload.Target.Selectors); selectors != "" {
		return true
	}
	return false
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func canonicalSelectors(selectors map[string]any) string {
	if len(selectors) == 0 {
		return ""
	}
	keys := make([]string, 0, len(selectors))
	for key := range selectors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		val, ok := selectors[key].(string)
		if !ok {
			continue
		}
		parts = append(parts, strings.ToLower(strings.TrimSpace(key))+"="+strings.ToLower(strings.TrimSpace(val)))
	}
	return strings.Join(parts, ",")
}

func canonicalPortList(ports []any) string {
	if len(ports) == 0 {
		return ""
	}
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		b, err := json.Marshal(port)
		if err != nil {
			continue
		}
		values = append(values, string(b))
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
