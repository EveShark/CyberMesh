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

	// State endpoints
	mux.HandleFunc(basePath+"/state/root", s.handleStateRoot)
	mux.HandleFunc(basePath+"/state/", s.handleState)

	// Validator endpoints
	mux.HandleFunc(basePath+"/validators", s.handleValidators)

	// Statistics
	mux.HandleFunc(basePath+"/stats", s.handleStats)

	// P2P info (for peer discovery) - DISABLED: handler method missing
	// mux.HandleFunc(basePath+"/p2p/info", s.handleP2PInfo)

	s.logger.Info("routes registered",
		utils.ZapString("base_path", basePath),
		utils.ZapInt("endpoint_count", 9))
}

// middlewareChain applies middleware in order
func (s *Server) middlewareChain(handler http.Handler) http.Handler {
	// Apply middleware in reverse order (outermost first)

	// 1. Panic recovery (outermost - catches all panics)
	handler = s.middlewarePanicRecovery(handler)

	// 2. Request logging
	handler = s.middlewareLogging(handler)

	// 3. Request ID
	handler = s.middlewareRequestID(handler)

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

	// 6. Authentication & RBAC (if enabled) - TODO: implement in auth.go
	// if s.config.RBACEnabled && s.config.TLSEnabled {
	//	handler = s.middlewareAuth(handler)
	// }

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
	}

	for _, endpoint := range publicEndpoints {
		if path == endpoint {
			return true
		}
	}

	return false
}

// getRequiredRole returns the required role for an endpoint
func (s *Server) getRequiredRole(path string) string {
	basePath := s.config.BasePath

	// Admin has access to everything
	if strings.HasPrefix(path, basePath+"/blocks") {
		return "block_reader"
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
	if strings.HasPrefix(path, basePath+"/metrics") {
		return "metrics_reader"
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

	// Check role mapping from config
	if allowedEndpoints, exists := s.config.AllowedRoles[clientRole]; exists {
		for _, endpoint := range allowedEndpoints {
			if strings.HasPrefix(requiredRole, endpoint) {
				return true
			}
		}
	}

	return false
}
