package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// translateGatewaySpec normalizes and validates gateway policy semantics for deterministic enforcement.
func translateGatewaySpec(gatewayNS string, spec policy.PolicySpec) (policy.PolicySpec, error) {
	out := spec
	out.Action = strings.ToLower(strings.TrimSpace(out.Action))
	out.Target.Direction = strings.ToLower(strings.TrimSpace(out.Target.Direction))
	out.Target.Scope = strings.ToLower(strings.TrimSpace(out.Target.Scope))

	ns := strings.ToLower(strings.TrimSpace(out.Target.Namespace))
	if ns == "" {
		if raw, ok := out.Target.Selectors["namespace"]; ok {
			ns = strings.ToLower(strings.TrimSpace(raw))
		}
	}
	out.Target.Namespace = ns

	normalizedCIDRs, err := normalizeTargets(out.Target.IPs, out.Target.CIDRs)
	if err != nil {
		return policy.PolicySpec{}, err
	}
	out.Target.IPs = nil
	out.Target.CIDRs = normalizedCIDRs

	normalizedProtocols, err := normalizeProtocols(out.Target.Protocols)
	if err != nil {
		return policy.PolicySpec{}, err
	}
	out.Target.Protocols = normalizedProtocols

	normalizedPorts, err := normalizePorts(out.Target.Ports)
	if err != nil {
		return policy.PolicySpec{}, err
	}
	out.Target.Ports = normalizedPorts

	if len(out.Target.Ports) > 0 {
		if len(out.Target.Protocols) == 0 {
			return policy.PolicySpec{}, fmt.Errorf("gateway: ports specified without protocol")
		}
		for _, proto := range out.Target.Protocols {
			if proto != "TCP" && proto != "UDP" {
				return policy.PolicySpec{}, fmt.Errorf("gateway: ports specified with unsupported protocol %s", proto)
			}
		}
	}

	if out.Raw == nil {
		out.Raw = make(map[string]any, 4)
	}
	ruleHash := gatewayRuleHash(out)
	out.Raw["gateway_rule_hash"] = ruleHash
	out.Raw["gateway_rule_id"] = "gw-" + ruleHash[:12]
	out.Raw["gateway_rule_version"] = "v1"

	if err := validateGatewayProfile(gatewayNS, out); err != nil {
		return policy.PolicySpec{}, err
	}
	return out, nil
}

func normalizeTargets(ips []string, cidrs []string) ([]string, error) {
	uniq := make(map[string]struct{}, len(ips)+len(cidrs))

	for _, raw := range ips {
		val := strings.TrimSpace(raw)
		if val == "" {
			continue
		}
		addr, err := netip.ParseAddr(val)
		if err != nil {
			return nil, fmt.Errorf("gateway: invalid target ip %s", val)
		}
		bits := 32
		if addr.Is6() {
			bits = 128
		}
		prefix := netip.PrefixFrom(addr, bits)
		uniq[prefix.String()] = struct{}{}
	}

	for _, raw := range cidrs {
		val := strings.TrimSpace(raw)
		if val == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(val)
		if err != nil {
			return nil, fmt.Errorf("gateway: invalid target cidr %s", val)
		}
		uniq[prefix.Masked().String()] = struct{}{}
	}

	if len(uniq) == 0 {
		return nil, fmt.Errorf("gateway: no valid target IPs/CIDRs")
	}

	out := make([]string, 0, len(uniq))
	for c := range uniq {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeProtocols(protocols []string) ([]string, error) {
	if len(protocols) == 0 {
		return nil, nil
	}
	uniq := map[string]struct{}{}
	for _, raw := range protocols {
		p := strings.ToUpper(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		switch p {
		case "TCP", "UDP", "ICMP":
			uniq[p] = struct{}{}
		default:
			return nil, fmt.Errorf("gateway: unsupported protocol %s", p)
		}
	}
	if len(uniq) == 0 {
		return nil, nil
	}
	order := []string{"TCP", "UDP", "ICMP"}
	out := make([]string, 0, len(uniq))
	for _, p := range order {
		if _, ok := uniq[p]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func normalizePorts(ports []policy.PortRange) ([]policy.PortRange, error) {
	if len(ports) == 0 {
		return nil, nil
	}
	uniq := make(map[string]policy.PortRange, len(ports))
	for _, pr := range ports {
		from := pr.From
		to := pr.To
		if from <= 0 || from > 65535 {
			return nil, fmt.Errorf("gateway: invalid port %d", from)
		}
		if to == 0 {
			to = from
		}
		if to < from || to > 65535 {
			return nil, fmt.Errorf("gateway: invalid port range %d-%d", from, to)
		}
		key := strconv.Itoa(from) + "-" + strconv.Itoa(to)
		uniq[key] = policy.PortRange{From: from, To: to}
	}
	out := make([]policy.PortRange, 0, len(uniq))
	for _, pr := range uniq {
		out = append(out, pr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out, nil
}

func gatewayRuleHash(spec policy.PolicySpec) string {
	parts := []string{
		"rule_type=" + strings.ToLower(strings.TrimSpace(spec.RuleType)),
		"action=" + strings.ToLower(strings.TrimSpace(spec.Action)),
		"direction=" + strings.ToLower(strings.TrimSpace(spec.Target.Direction)),
		"scope=" + strings.ToLower(strings.TrimSpace(spec.Target.Scope)),
		"namespace=" + strings.ToLower(strings.TrimSpace(spec.Target.Namespace)),
		"tenant=" + strings.ToLower(strings.TrimSpace(spec.Tenant)),
		"region=" + strings.ToLower(strings.TrimSpace(spec.Region)),
	}
	parts = append(parts, "cidrs="+strings.Join(spec.Target.CIDRs, ","))
	parts = append(parts, "protocols="+strings.Join(spec.Target.Protocols, ","))

	portParts := make([]string, 0, len(spec.Target.Ports))
	for _, pr := range spec.Target.Ports {
		portParts = append(portParts, strconv.Itoa(pr.From)+"-"+strconv.Itoa(pr.To))
	}
	parts = append(parts, "ports="+strings.Join(portParts, ","))
	parts = append(parts, "ttl="+strconv.FormatInt(spec.Guardrails.TTLSeconds, 10))

	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

