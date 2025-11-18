package common

import (
	"fmt"
	"strings"

	"github.com/CyberMesh/enforcement-agent/internal/policy"
)

// ValidateScope ensures target scopes include the appropriate metadata.
func ValidateScope(spec policy.PolicySpec) error {
	scope := strings.ToLower(strings.TrimSpace(spec.Target.Scope))
	switch scope {
	case "", "global", "any", "both", "cluster":
		return nil
	case "node":
		if strings.TrimSpace(spec.Target.Selectors["node"]) != "" {
			return nil
		}
		return fmt.Errorf("enforcer: node scope requires target selector 'node'")
	case "namespace":
		if strings.TrimSpace(spec.Target.Namespace) != "" {
			return nil
		}
		if ns, ok := spec.Target.Selectors["namespace"]; ok && strings.TrimSpace(ns) != "" {
			return nil
		}
		return fmt.Errorf("enforcer: namespace scope requires target.namespace or namespace selector")
	case "tenant":
		if strings.TrimSpace(spec.Target.Tenant) != "" || strings.TrimSpace(spec.Tenant) != "" {
			return nil
		}
		return fmt.Errorf("enforcer: tenant scope requires tenant metadata")
	case "region":
		if strings.TrimSpace(spec.Target.Region) != "" || strings.TrimSpace(spec.Region) != "" {
			return nil
		}
		return fmt.Errorf("enforcer: region scope requires region metadata")
	default:
		return fmt.Errorf("enforcer: unsupported scope %s", spec.Target.Scope)
	}
}
