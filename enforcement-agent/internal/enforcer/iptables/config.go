package iptables

import (
	"fmt"
	"net"
	"strings"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// Config controls construction of the iptables backend.
type Config struct {
	Binary             string
	DryRun             bool
	Logger             Logger
	NamespaceSetPrefix string
	NodeSetPrefix      string
}

// Logger is the subset of zap.Logger we rely on to keep the package decoupled in tests.
type Logger interface {
	Debug(msg string, fields ...zap.Field)
	Info(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
}

type rule struct {
	Chain  string
	Args   []string
	Action string
}

type chainDescriptor struct {
	Chain    string
	AddrFlag string
}

type selectorConfig struct {
	namespacePrefix string
	nodePrefix      string
}

// buildRules converts a PolicySpec into concrete iptables rules.
func buildRules(spec policy.PolicySpec, selectors selectorConfig) ([]rule, error) {
	targets := append([]string{}, spec.Target.IPs...)
	targets = append(targets, spec.Target.CIDRs...)
	selectorMatches := selectorConstraint{}
	if len(targets) == 0 {
		if len(spec.Target.Selectors) == 0 {
			return nil, fmt.Errorf("iptables: policy %s missing target IPs/CIDRs", spec.ID)
		}
		match, err := buildSelectorConstraint(spec, selectors)
		if err != nil {
			return nil, err
		}
		selectorMatches = match
	}

	action := strings.ToUpper(strings.TrimSpace(spec.Action))
	if action == "" {
		action = "DROP"
	}
	switch action {
	case "DROP", "REJECT":
	default:
		return nil, fmt.Errorf("iptables: unsupported action %s", spec.Action)
	}

	descriptors := chainsForDirection(spec.Target.Direction)
	if len(descriptors) == 0 {
		return nil, fmt.Errorf("iptables: unsupported direction %s", spec.Target.Direction)
	}

	protocols := normaliseProtocols(spec.Target.Protocols, len(spec.Target.Ports) > 0)
	if len(protocols) == 0 {
		protocols = []string{""}
	}

	portRanges := spec.Target.Ports
	if len(portRanges) == 0 {
		portRanges = []policy.PortRange{{From: 0, To: 0}}
	}

	var rules []rule
	if len(targets) == 0 {
		for _, desc := range descriptors {
			for _, proto := range protocols {
				protoLower := strings.ToLower(proto)
				requiresPort := len(spec.Target.Ports) > 0
				if requiresPort && protoLower != "tcp" && protoLower != "udp" && protoLower != "sctp" && protoLower != "udplite" {
					return nil, fmt.Errorf("iptables: ports specified but protocol %s does not support ports", proto)
				}
				for _, pr := range portRanges {
					baseArgs, ok := selectorMatches.argsFor(desc)
					if !ok {
						continue
					}
					args := append([]string{}, baseArgs...)
					if protoLower != "" && protoLower != "any" {
						args = append(args, "-p", protoLower)
						if requiresPort && (protoLower == "tcp" || protoLower == "udp" || protoLower == "sctp" || protoLower == "udplite") {
							args = append(args, "-m", protoLower)
						}
					}
					if pr.From > 0 {
						if pr.From == pr.To {
							args = append(args, "--dport", fmt.Sprintf("%d", pr.From))
						} else {
							args = append(args, "--dport", fmt.Sprintf("%d:%d", pr.From, pr.To))
						}
					}
					if len(args) == len(baseArgs) {
						// no proto/port additions
					}
					rules = append(rules, rule{
						Chain:  desc.Chain,
						Args:   args,
						Action: action,
					})
				}
			}
		}
		return rules, nil
	}
	for _, desc := range descriptors {
		for _, target := range targets {
			for _, proto := range protocols {
				protoLower := strings.ToLower(proto)
				requiresPort := len(spec.Target.Ports) > 0
				if requiresPort && protoLower != "tcp" && protoLower != "udp" && protoLower != "sctp" && protoLower != "udplite" {
					return nil, fmt.Errorf("iptables: ports specified but protocol %s does not support ports", proto)
				}
				for _, pr := range portRanges {
					args := []string{desc.AddrFlag, canonicalTarget(target)}
					if protoLower != "" && protoLower != "any" {
						args = append(args, "-p", protoLower)
						if requiresPort && (protoLower == "tcp" || protoLower == "udp" || protoLower == "sctp" || protoLower == "udplite") {
							args = append(args, "-m", protoLower)
						}
					}
					if pr.From > 0 {
						if pr.From == pr.To {
							args = append(args, "--dport", fmt.Sprintf("%d", pr.From))
						} else {
							args = append(args, "--dport", fmt.Sprintf("%d:%d", pr.From, pr.To))
						}
					}
					rules = append(rules, rule{
						Chain:  desc.Chain,
						Args:   args,
						Action: action,
					})
				}
			}
		}
	}

	return rules, nil
}

type selectorConstraint struct {
	srcSets []string
	dstSets []string
}

func (c selectorConstraint) argsFor(desc chainDescriptor) ([]string, bool) {
	var sets []string
	switch desc.AddrFlag {
	case "-s":
		sets = c.srcSets
	case "-d":
		sets = c.dstSets
	default:
		return nil, false
	}
	if len(sets) == 0 {
		return nil, false
	}
	args := make([]string, 0, len(sets)*4)
	for _, set := range sets {
		args = append(args, "-m", "set", "--match-set", set)
		if desc.AddrFlag == "-s" {
			args = append(args, "src")
		} else {
			args = append(args, "dst")
		}
	}
	return args, true
}

func buildSelectorConstraint(spec policy.PolicySpec, cfg selectorConfig) (selectorConstraint, error) {
	var constraint selectorConstraint
	ns := strings.TrimSpace(spec.Target.Namespace)
	if ns == "" {
		if v, ok := spec.Target.Selectors["namespace"]; ok {
			ns = strings.TrimSpace(v)
		}
	}
	if ns != "" {
		if cfg.namespacePrefix == "" {
			return selectorConstraint{}, fmt.Errorf("iptables: namespace selector requires NamespaceSetPrefix")
		}
		setName := sanitizeSetName(cfg.namespacePrefix, ns)
		constraint.srcSets = append(constraint.srcSets, setName)
		constraint.dstSets = append(constraint.dstSets, setName)
	}

	node := strings.TrimSpace(spec.Target.Selectors["node"])
	if node != "" {
		if cfg.nodePrefix == "" {
			return selectorConstraint{}, fmt.Errorf("iptables: node selector requires NodeSetPrefix")
		}
		setName := sanitizeSetName(cfg.nodePrefix, node)
		constraint.srcSets = append(constraint.srcSets, setName)
		constraint.dstSets = append(constraint.dstSets, setName)
	}

	if len(constraint.srcSets) == 0 && len(constraint.dstSets) == 0 {
		return selectorConstraint{}, fmt.Errorf("iptables: selector-only policy %s unsupported", spec.ID)
	}

	return constraint, nil
}

func sanitizeSetName(prefix, value string) string {
	cleaned := make([]rune, 0, len(value))
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			cleaned = append(cleaned, r)
			continue
		}
		cleaned = append(cleaned, '_')
	}
	return prefix + string(cleaned)
}

func chainsForDirection(direction string) []chainDescriptor {
	dir := strings.ToLower(strings.TrimSpace(direction))
	switch dir {
	case "", "any", "both":
		return []chainDescriptor{{Chain: "INPUT", AddrFlag: "-s"}, {Chain: "OUTPUT", AddrFlag: "-d"}}
	case "ingress", "inbound":
		return []chainDescriptor{{Chain: "INPUT", AddrFlag: "-s"}}
	case "egress", "outbound":
		return []chainDescriptor{{Chain: "OUTPUT", AddrFlag: "-d"}}
	case "forward":
		return []chainDescriptor{{Chain: "FORWARD", AddrFlag: "-s"}}
	default:
		return nil
	}
}

func canonicalTarget(target string) string {
	t := strings.TrimSpace(target)
	if strings.Contains(t, "/") {
		return t
	}
	if ip := net.ParseIP(t); ip != nil {
		if ip.To4() != nil {
			return ip.String()
		}
		return ip.String()
	}
	return t
}

func normaliseProtocols(protocols []string, portsDefined bool) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(protocols))
	for _, proto := range protocols {
		upper := strings.ToUpper(strings.TrimSpace(proto))
		if upper == "" {
			continue
		}
		if upper == "ANY" {
			return []string{""}
		}
		if _, ok := seen[upper]; ok {
			continue
		}
		seen[upper] = struct{}{}
		result = append(result, upper)
	}
	if len(result) == 0 {
		if portsDefined {
			return []string{"TCP", "UDP"}
		}
		return []string{""}
	}
	return result
}
