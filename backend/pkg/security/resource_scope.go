package security

import (
	"fmt"
	"strings"

	"backend/pkg/security/contracts"
)

// ScopeClass describes whether a resource is access-bound or platform-global.
type ScopeClass string

const (
	ScopeClassAccessBound ScopeClass = "access_bound"
	ScopeClassGlobal      ScopeClass = "global"
)

// ScopeDescriptor is the normalized classification for a backend route target.
type ScopeDescriptor struct {
	Resource      contracts.ResourceRef
	ScopeClass    ScopeClass
	RequiredRole  string
	HandlerFamily string
}

// ClassifyHTTPResource derives resource scope from the current backend route patterns.
// Unknown routes are rejected so Batch 2 does not silently classify new paths.
func ClassifyHTTPResource(basePath, method, path string) (ScopeDescriptor, error) {
	normalizedBase := strings.TrimSuffix(strings.TrimSpace(basePath), "/")
	normalizedPath := strings.TrimSpace(path)
	if normalizedBase != "" && strings.HasPrefix(normalizedPath, normalizedBase) {
		normalizedPath = strings.TrimPrefix(normalizedPath, normalizedBase)
	}
	if normalizedPath == "" {
		normalizedPath = "/"
	}
	if !strings.HasPrefix(normalizedPath, "/") {
		normalizedPath = "/" + normalizedPath
	}

	switch {
	case hasRoutePrefix(normalizedPath, "/policies/acks"):
		return descriptor("policy_ack", "policy_acks", normalizedPath, ScopeClassAccessBound, "policy_reader"), nil
	case hasRoutePrefix(normalizedPath, "/policies"):
		role := "policy_reader"
		if strings.EqualFold(method, "POST") {
			role = "control_outbox_operator"
		}
		return descriptor("policy", "policies", normalizedPath, ScopeClassAccessBound, role), nil
	case hasRoutePrefix(normalizedPath, "/workflows"):
		role := "policy_reader"
		if strings.EqualFold(method, "POST") {
			role = "control_outbox_operator"
		}
		return descriptor("workflow", "workflows", normalizedPath, ScopeClassAccessBound, role), nil
	case hasRoutePrefix(normalizedPath, "/audit"):
		return descriptor("audit_scope", "audit", normalizedPath, ScopeClassAccessBound, "audit_reader"), nil
	case hasRoutePrefix(normalizedPath, "/control/outbox"):
		role := "control_outbox_reader"
		if strings.EqualFold(method, "POST") {
			role = "control_outbox_operator"
		}
		return descriptor("control_outbox", "control_outbox", normalizedPath, ScopeClassAccessBound, role), nil
	case hasRoutePrefix(normalizedPath, "/control/trace"):
		return descriptor("control_trace", "control_trace", normalizedPath, ScopeClassAccessBound, "control_trace_reader"), nil
	case hasRoutePrefix(normalizedPath, "/control/acks"):
		return descriptor("control_ack", "control_acks", normalizedPath, ScopeClassAccessBound, "control_ack_reader"), nil
	case hasRoutePrefix(normalizedPath, "/control/leases"):
		role := "control_lease_reader"
		if strings.EqualFold(method, "POST") {
			role = "control_lease_admin"
		}
		return descriptor("control_lease", "control_leases", normalizedPath, ScopeClassAccessBound, role), nil
	case hasRoutePrefix(normalizedPath, "/control/safe-mode"):
		return descriptor("platform_config", "control_safe_mode", normalizedPath, ScopeClassGlobal, "control_lease_admin"), nil
	case hasRoutePrefix(normalizedPath, "/control/kill-switch"):
		return descriptor("platform_config", "control_kill_switch", normalizedPath, ScopeClassGlobal, "control_lease_admin"), nil
	case hasRoutePrefix(normalizedPath, "/stats"):
		return descriptor("stats", "stats", normalizedPath, ScopeClassGlobal, "stats_reader"), nil
	case hasRoutePrefix(normalizedPath, "/dashboard/overview"):
		return descriptor("dashboard", "dashboard", normalizedPath, ScopeClassGlobal, "stats_reader"), nil
	case hasRoutePrefix(normalizedPath, "/network/overview"):
		return descriptor("network_overview", "network_overview", normalizedPath, ScopeClassGlobal, "network_reader"), nil
	case hasRoutePrefix(normalizedPath, "/consensus/overview"):
		return descriptor("consensus_overview", "consensus_overview", normalizedPath, ScopeClassGlobal, "consensus_reader"), nil
	case hasRoutePrefix(normalizedPath, "/ai"):
		return descriptor("ai_metrics", "ai", normalizedPath, ScopeClassGlobal, "ai_reader"), nil
	case hasRoutePrefix(normalizedPath, "/anomalies"):
		return descriptor("anomaly_overview", "anomalies", normalizedPath, ScopeClassGlobal, "anomaly_reader"), nil
	case hasRoutePrefix(normalizedPath, "/frontend-config"):
		return descriptor("frontend_config", "frontend_config", normalizedPath, ScopeClassGlobal, "stats_reader"), nil
	default:
		return ScopeDescriptor{}, fmt.Errorf("resource scope unknown for path %q", path)
	}
}

func descriptor(resourceType, handlerFamily, id string, class ScopeClass, requiredRole string) ScopeDescriptor {
	return ScopeDescriptor{
		Resource: contracts.ResourceRef{
			Type:             resourceType,
			ID:               id,
			IsGlobalResource: class == ScopeClassGlobal,
		},
		ScopeClass:    class,
		RequiredRole:  requiredRole,
		HandlerFamily: handlerFamily,
	}
}

func hasRoutePrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	if strings.HasSuffix(prefix, "/") {
		return true
	}
	if len(path) <= len(prefix) {
		return false
	}
	switch path[len(prefix)] {
	case '/', ':', '?':
		return true
	default:
		return false
	}
}
