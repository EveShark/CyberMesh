package gateway

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

type guardrailMaterialized struct {
	protectedPrefixes   []netip.Prefix
	protectedNamespaces map[string]struct{}
}

type applyState struct {
	ruleHash  string
	targetKey string
	appliedAt time.Time
	action    string
	tenant    string
}

func buildGuardrailMaterialized(cfg GuardrailsConfig) (guardrailMaterialized, error) {
	out := guardrailMaterialized{
		protectedNamespaces: make(map[string]struct{}, len(cfg.ProtectedNamespaces)),
	}

	for _, ns := range cfg.ProtectedNamespaces {
		v := strings.ToLower(strings.TrimSpace(ns))
		if v == "" {
			continue
		}
		out.protectedNamespaces[v] = struct{}{}
	}

	prefixes, err := normalizeProtectedPrefixes(cfg.ProtectedIPs, cfg.ProtectedCIDRs)
	if err != nil {
		return guardrailMaterialized{}, err
	}
	out.protectedPrefixes = prefixes
	return out, nil
}

func normalizeProtectedPrefixes(ips []string, cidrs []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(ips)+len(cidrs))
	seen := map[string]struct{}{}

	for _, raw := range ips {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		addr, err := netip.ParseAddr(v)
		if err != nil {
			return nil, fmt.Errorf("gateway: invalid protected ip %s", v)
		}
		bits := 32
		if addr.Is6() {
			bits = 128
		}
		p := netip.PrefixFrom(addr, bits).Masked()
		key := p.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}

	for _, raw := range cidrs {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		p, err := netip.ParsePrefix(v)
		if err != nil {
			return nil, fmt.Errorf("gateway: invalid protected cidr %s", v)
		}
		masked := p.Masked()
		key := masked.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, masked)
	}
	return out, nil
}

func validateGatewayGuardrails(cfg GuardrailsConfig, mat guardrailMaterialized, spec policy.PolicySpec, activeCount int, activeTenantCount int, prev *applyState, now time.Time) error {
	tenantSpec := strings.ToLower(strings.TrimSpace(spec.Tenant))
	tenantTarget := strings.ToLower(strings.TrimSpace(spec.Target.Tenant))
	effectiveTenant := tenantSpec
	if effectiveTenant == "" {
		effectiveTenant = tenantTarget
	}

	if cfg.EnforceTenantMatch && tenantSpec != "" && tenantTarget != "" && tenantSpec != tenantTarget {
		return fmt.Errorf("gateway: cross-tenant policy mismatch spec_tenant=%s target_tenant=%s", tenantSpec, tenantTarget)
	}
	if cfg.RequireTenant && effectiveTenant == "" {
		return fmt.Errorf("gateway: tenant is required by guardrail")
	}

	targetCount := len(spec.Target.CIDRs)
	if int64(targetCount) > cfg.MaxTargetsPerPolicy {
		return fmt.Errorf("gateway: target count %d exceeds max %d", targetCount, cfg.MaxTargetsPerPolicy)
	}

	if int64(len(spec.Target.Ports)) > cfg.MaxPortsPerPolicy {
		return fmt.Errorf("gateway: port range count %d exceeds max %d", len(spec.Target.Ports), cfg.MaxPortsPerPolicy)
	}

	if prev == nil && int64(activeCount) >= cfg.MaxActivePolicies {
		return fmt.Errorf("gateway: active policy count %d reached max %d", activeCount, cfg.MaxActivePolicies)
	}
	if prev == nil && cfg.MaxActivePerTenant > 0 && int64(activeTenantCount) >= cfg.MaxActivePerTenant {
		return fmt.Errorf("gateway: tenant active policy count %d reached max %d", activeTenantCount, cfg.MaxActivePerTenant)
	}

	if spec.Guardrails.TTLSeconds > cfg.MaxTTLSeconds {
		return fmt.Errorf("gateway: ttl_seconds %d exceeds max %d", spec.Guardrails.TTLSeconds, cfg.MaxTTLSeconds)
	}

	if cfg.RequireCanary && !spec.Guardrails.CanaryScope {
		return fmt.Errorf("gateway: canary_scope required by guardrail")
	}

	if prev != nil && cfg.ApplyCooldown > 0 {
		if now.Sub(prev.appliedAt) < cfg.ApplyCooldown {
			return fmt.Errorf("gateway: apply cooldown active for policy %s", spec.ID)
		}
	}

	if _, protected := mat.protectedNamespaces[strings.ToLower(strings.TrimSpace(spec.Target.Namespace))]; protected {
		return fmt.Errorf("gateway: protected namespace target %s is not allowed", spec.Target.Namespace)
	}

	for _, raw := range spec.Target.CIDRs {
		p, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("gateway: invalid target cidr %s", raw)
		}
		masked := p.Masked()
		bits := masked.Bits()
		if cfg.DenyBroadCIDRs {
			if masked.Addr().Is4() && bits < cfg.MinIPv4Prefix {
				return fmt.Errorf("gateway: broad ipv4 cidr %s rejected (min /%d)", masked.String(), cfg.MinIPv4Prefix)
			}
			if masked.Addr().Is6() && bits < cfg.MinIPv6Prefix {
				return fmt.Errorf("gateway: broad ipv6 cidr %s rejected (min /%d)", masked.String(), cfg.MinIPv6Prefix)
			}
		}
		for _, protected := range mat.protectedPrefixes {
			if masked.Overlaps(protected) {
				return fmt.Errorf("gateway: target cidr %s overlaps protected cidr %s", masked.String(), protected.String())
			}
		}
	}

	return nil
}
