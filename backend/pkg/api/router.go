package api

import (
	"net/http"
	"strings"

	"backend/pkg/utils"
)

// setupRouter creates the HTTP handler with middleware and routes
func (s *Server) setupRouter() http.Handler {
	mux := http.NewServeMux()

	// Register routes
	s.registerRoutes(mux)

	// Apply middleware chain (order matters - innermost first)
	handler := s.middlewareChain(mux)

	return handler
}

// registerRoutes registers all API endpoints
func (s *Server) registerRoutes(mux *http.ServeMux) {
	basePath := s.config.BasePath

	// Health & Readiness (no auth required)
	mux.HandleFunc(basePath+"/health", s.handleHealth)
	mux.HandleFunc(basePath+"/ready", s.handleReadiness)

	// Metrics (requires auth if RBAC enabled)
	mux.HandleFunc(basePath+"/metrics", s.handleMetrics)

	// Block endpoints
	mux.HandleFunc(basePath+"/blocks/latest", s.handleBlockLatest)
	mux.HandleFunc(basePath+"/blocks", s.handleBlocks)
	mux.HandleFunc(basePath+"/blocks/", s.handleBlocks)

	// State endpoints
	mux.HandleFunc(basePath+"/state/root", s.handleStateRoot)
	mux.HandleFunc(basePath+"/state/", s.handleState)

	// Validator endpoints
	mux.HandleFunc(basePath+"/validators", s.handleValidators)

	// Statistics
	mux.HandleFunc(basePath+"/stats", s.handleStats)

	// Network & consensus overviews
	mux.HandleFunc(basePath+"/network/overview", s.handleNetworkOverview)
	mux.HandleFunc(basePath+"/consensus/overview", s.handleConsensusOverview)
	mux.HandleFunc(basePath+"/dashboard/overview", s.handleDashboardOverview)

	// Anomaly endpoints
	mux.HandleFunc(basePath+"/anomalies/stats", s.handleAnomalyStats)
	mux.HandleFunc(basePath+"/anomalies", s.handleAnomalies)
	mux.HandleFunc(basePath+"/anomalies/suspicious-nodes", s.handleSuspiciousNodes)

	// AI telemetry endpoints
	mux.HandleFunc(basePath+"/ai/metrics", s.handleAIMetrics)
	mux.HandleFunc(basePath+"/ai/variants", s.handleAIVariants)
	mux.HandleFunc(basePath+"/ai/history", s.handleAIDetectionHistory)
	mux.HandleFunc(basePath+"/ai/suspicious-nodes", s.handleAISuspiciousNodes)

	// Policy execution visibility (ACKs from enforcement-agent).
	mux.HandleFunc(basePath+"/policies", s.handlePoliciesList)
	mux.HandleFunc(basePath+"/policies/", s.handlePoliciesGet)
	mux.HandleFunc(basePath+"/policies/acks", s.handlePolicyAcks)
	mux.HandleFunc(basePath+"/policies/acks/", s.handlePolicyAcks)
	mux.HandleFunc(basePath+"/workflows", s.handleWorkflowsList)
	mux.HandleFunc(basePath+"/workflows/", s.handleWorkflowsGet)
	mux.HandleFunc(basePath+"/audit", s.handleAuditList)
	mux.HandleFunc(basePath+"/audit/export", s.handleAuditExport)

	// Frontend runtime config.
	mux.HandleFunc(basePath+"/frontend-config", s.handleFrontendConfig)
	mux.HandleFunc(basePath+"/auth/me", s.handleAuthMe)
	mux.HandleFunc(basePath+"/auth/access/select", s.handleAuthAccessSelect)
	mux.HandleFunc(basePath+"/auth/delegations", s.handleAuthDelegations)
	mux.HandleFunc(basePath+"/auth/delegations/", s.handleAuthDelegationMutation)

	// Control-plane APIs (Wave 1 read visibility).
	mux.HandleFunc(basePath+"/control/outbox/backlog", s.handleControlOutboxBacklog)
	mux.HandleFunc(basePath+"/control/outbox", s.handleControlOutboxList)
	mux.HandleFunc(basePath+"/control/outbox/", s.handleControlOutboxGet)
	mux.HandleFunc(basePath+"/control/trace", s.handleControlTraceList)
	mux.HandleFunc(basePath+"/control/trace/", s.handleControlTraceByPolicy)
	mux.HandleFunc(basePath+"/control/leases", s.handleControlLeases)
	mux.HandleFunc(basePath+"/control/leases:force-takeover", s.handleControlLeases)
	mux.HandleFunc(basePath+"/control/safe-mode:toggle", s.handleControlSafeModeToggle)
	mux.HandleFunc(basePath+"/control/kill-switch:toggle", s.handleControlKillSwitchToggle)
	mux.HandleFunc(basePath+"/control/runtime:repair", s.handleControlRuntimeRepair)
	mux.HandleFunc(basePath+"/control/acks", s.handleControlAcksList)

	// P2P info (for peer discovery) - DISABLED: handler method missing
	// mux.HandleFunc(basePath+"/p2p/info", s.handleP2PInfo)

	s.logger.Info("routes registered",
		utils.ZapString("base_path", basePath),
		utils.ZapInt("endpoint_count", 33))
}

// middlewareChain applies middleware in order
func (s *Server) middlewareChain(handler http.Handler) http.Handler {
	// Apply middleware in reverse order (outermost first)

	// 1. Panic recovery (outermost - catches all panics)
	handler = s.middlewarePanicRecovery(handler)

	// 2. Request logging
	handler = s.middlewareLogging(handler)

	// 3.5 Concurrency limit (if configured)
	if s.sem != nil {
		handler = s.middlewareConcurrencyLimit(handler)
	}

	// 4. IP allowlist (if configured)
	if s.ipAllowlist != nil {
		handler = s.middlewareIPAllowlist(handler)
	}

	// 5. Rate limiting (if enabled)
	if s.rateLimiter != nil {
		handler = s.middlewareRateLimit(handler)
	}

	// 6. SECURITY: Global authentication for all non-public endpoints
	// This middleware checks if endpoint requires auth and validates credentials
	handler = s.middlewareGlobalAuth(handler)

	// 6.5 Request ID must wrap auth/rate-limit/logging so every error response carries it.
	handler = s.middlewareRequestID(handler)

	// 7. CORS headers (if needed)
	handler = s.middlewareCORS(handler)

	// 8. Security headers
	handler = s.middlewareSecurityHeaders(handler)

	return handler
}

// matchRoute checks if request path matches a pattern
func matchRoute(path, pattern string) (bool, map[string]string) {
	params := make(map[string]string)

	// Remove trailing slashes
	path = strings.TrimSuffix(path, "/")
	pattern = strings.TrimSuffix(pattern, "/")

	pathParts := strings.Split(path, "/")
	patternParts := strings.Split(pattern, "/")

	if len(pathParts) != len(patternParts) {
		return false, nil
	}

	for i := range pathParts {
		if patternParts[i] == "" {
			continue
		}

		// Check for parameter (starts with :)
		if strings.HasPrefix(patternParts[i], ":") {
			paramName := strings.TrimPrefix(patternParts[i], ":")
			params[paramName] = pathParts[i]
			continue
		}

		// Exact match required
		if pathParts[i] != patternParts[i] {
			return false, nil
		}
	}

	return true, params
}

// extractRouteParam extracts a parameter from URL path
// Example: /blocks/42 with pattern /blocks/:height -> "42"
func extractRouteParam(path, basePath, param string) string {
	// Remove base path
	path = strings.TrimPrefix(path, basePath)
	path = strings.TrimPrefix(path, "/")

	// Split by /
	parts := strings.Split(path, "/")

	// For /blocks/:height pattern
	if len(parts) >= 2 {
		return parts[1]
	}

	return ""
}

// isPublicEndpoint checks if an endpoint doesn't require authentication
func (s *Server) isPublicEndpoint(path string) bool {
	publicEndpoints := []string{
		s.config.BasePath + "/health",
		s.config.BasePath + "/ready",
		s.config.BasePath + "/frontend-config",
	}

	for _, endpoint := range publicEndpoints {
		if path == endpoint {
			return true
		}
	}

	return false
}

// getRequiredRole returns the required role for an endpoint
func (s *Server) getRequiredRole(method, path string) string {
	basePath := s.config.BasePath

	// Admin has access to everything
	if strings.HasPrefix(path, basePath+"/blocks") {
		return "block_reader"
	}
	if path == basePath+"/auth/me" {
		return ""
	}
	if path == basePath+"/auth/access/select" {
		return ""
	}
	if path == basePath+"/auth/delegations" {
		return ""
	}
	if strings.HasPrefix(path, basePath+"/auth/delegations/") {
		if method == http.MethodPost && (strings.HasSuffix(path, ":approve") || strings.HasSuffix(path, ":revoke")) {
			return "control_lease_admin"
		}
		return ""
	}
	if strings.HasPrefix(path, basePath+"/state") {
		return "state_reader"
	}
	if strings.HasPrefix(path, basePath+"/validators") {
		return "validator_reader"
	}
	if strings.HasPrefix(path, basePath+"/stats") {
		return "stats_reader"
	}
	if strings.HasPrefix(path, basePath+"/dashboard/overview") {
		return "stats_reader"
	}
	if strings.HasPrefix(path, basePath+"/metrics") {
		return "metrics_reader"
	}
	if strings.HasPrefix(path, basePath+"/network/overview") {
		return "network_reader"
	}
	if strings.HasPrefix(path, basePath+"/consensus/overview") {
		return "consensus_reader"
	}
	if strings.HasPrefix(path, basePath+"/ai/") {
		return "ai_reader"
	}
	if strings.HasPrefix(path, basePath+"/anomalies/suspicious-nodes") {
		return "anomaly_reader"
	}
	if strings.HasPrefix(path, basePath+"/policies/acks") {
		return "policy_reader"
	}
	if strings.HasPrefix(path, basePath+"/policies") {
		if method == http.MethodPost {
			return "control_outbox_operator"
		}
		return "policy_reader"
	}
	if strings.HasPrefix(path, basePath+"/workflows") {
		if method == http.MethodPost {
			return "control_outbox_operator"
		}
		return "policy_reader"
	}
	if strings.HasPrefix(path, basePath+"/audit") {
		return "audit_reader"
	}
	if strings.HasPrefix(path, basePath+"/frontend-config") {
		return "stats_reader"
	}
	if strings.HasPrefix(path, basePath+"/control/outbox") {
		if method == http.MethodPost {
			return "control_outbox_operator"
		}
		return "control_outbox_reader"
	}
	if strings.HasPrefix(path, basePath+"/control/trace") {
		return "control_trace_reader"
	}
	if strings.HasPrefix(path, basePath+"/control/leases") {
		if method == http.MethodPost {
			return "control_lease_admin"
		}
		return "control_lease_reader"
	}
	if strings.HasPrefix(path, basePath+"/control/safe-mode") {
		return "control_lease_admin"
	}
	if strings.HasPrefix(path, basePath+"/control/runtime:repair") {
		return "control_lease_admin"
	}
	if strings.HasPrefix(path, basePath+"/control/kill-switch") {
		return "control_lease_admin"
	}
	if strings.HasPrefix(path, basePath+"/control/acks") {
		return "control_ack_reader"
	}

	// Default: no specific role required (public endpoint)
	return ""
}

// hasRole checks if a role has access to an endpoint
func (s *Server) hasRole(clientRole, requiredRole string) bool {
	// Admin has access to everything
	if clientRole == "admin" {
		return true
	}

	// Empty required role means public endpoint
	if requiredRole == "" {
		return true
	}

	// Exact role match
	if clientRole == requiredRole {
		return true
	}

	if s == nil || s.config == nil {
		return false
	}

	// If both roles exist in the configured endpoint map, grant access when the
	// client role covers every endpoint exposed by the required role.
	clientEndpoints, clientExists := s.config.AllowedRoles[clientRole]
	if !clientExists {
		return false
	}

	// Backward-compatible mode: explicit role aliases in AllowedRoles.
	if stringSetContainsFold(clientEndpoints, requiredRole) {
		return true
	}

	// Endpoint-coverage mode: if both roles are defined as endpoint sets, the
	// client role can satisfy the required role when it covers all endpoints.
	requiredEndpoints, requiredExists := s.config.AllowedRoles[requiredRole]
	if !requiredExists {
		return false
	}
	return endpointSetContainsAll(clientEndpoints, requiredEndpoints)
}

func endpointSetContainsAll(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	normalized := make(map[string]struct{}, len(have))
	for _, value := range have {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		normalized[key] = struct{}{}
	}
	for _, value := range want {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, ok := normalized[key]; !ok {
			return false
		}
	}
	return true
}

func stringSetContainsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}
