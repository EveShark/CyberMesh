package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"backend/pkg/security/contracts"
	"backend/pkg/utils"
	"github.com/google/uuid"
)

// Middleware context keys
type contextKey string

const (
	ctxKeyRequestID          contextKey = "request_id"
	ctxKeyClientIP           contextKey = "client_ip"
	ctxKeyClientCert         contextKey = "client_cert"
	ctxKeyClientRole         contextKey = "client_role"
	ctxKeyStartTime          contextKey = "start_time"
	ctxKeyPrincipalID        contextKey = "principal_id"
	ctxKeyPrincipalType      contextKey = "principal_type"
	ctxKeyAuthSubject        contextKey = "auth_subject"
	ctxKeyAuthUsername       contextKey = "auth_username"
	ctxKeyAuthEmail          contextKey = "auth_email"
	ctxKeyAuthSource         contextKey = "auth_source"
	ctxKeyTrustedMemberships contextKey = "trusted_memberships"
	ctxKeyTrustedDelegation  contextKey = "trusted_delegation"
)

var errBearerValidatorUnavailable = errors.New("bearer_validator_unavailable")

// middlewareRequestID adds a unique request ID to each request
func (s *Server) middlewareRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := ""
		if normalized, ok := normalizeRequestID(r.Header.Get(HeaderRequestID)); ok {
			requestID = normalized
		} else {
			requestID = generateRequestID()
		}

		// Add to context
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, requestID)
		ctx = context.WithValue(ctx, utils.ContextKeyRequestID, requestID)

		// Add to response headers
		w.Header().Set(HeaderRequestID, requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// middlewareLogging logs all requests
func (s *Server) middlewareLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := context.WithValue(r.Context(), ctxKeyStartTime, start)

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Process request
		next.ServeHTTP(wrapped, r.WithContext(ctx))

		// Log request
		duration := time.Since(start)
		requestID := getRequestID(r.Context())
		clientIP := getClientIP(r)

		// Log request
		if wrapped.statusCode >= 500 {
			s.logger.ErrorContext(r.Context(), "request error",
				utils.ZapString("request_id", requestID),
				utils.ZapString("method", r.Method),
				utils.ZapString("path", r.URL.Path),
				utils.ZapString("client_ip", clientIP),
				utils.ZapInt("status", wrapped.statusCode),
				utils.ZapInt64("duration_ms", duration.Milliseconds()))
		} else if wrapped.statusCode >= 400 {
			s.logger.WarnContext(r.Context(), "request client error",
				utils.ZapString("request_id", requestID),
				utils.ZapString("method", r.Method),
				utils.ZapString("path", r.URL.Path),
				utils.ZapString("client_ip", clientIP),
				utils.ZapInt("status", wrapped.statusCode),
				utils.ZapInt64("duration_ms", duration.Milliseconds()))
		} else {
			s.logger.InfoContext(r.Context(), "request completed",
				utils.ZapString("request_id", requestID),
				utils.ZapString("method", r.Method),
				utils.ZapString("path", r.URL.Path),
				utils.ZapString("client_ip", clientIP),
				utils.ZapInt("status", wrapped.statusCode),
				utils.ZapInt64("duration_ms", duration.Milliseconds()))
		}

		s.recordAPIRequest(wrapped.statusCode)
		s.recordRouteMetrics(r.Method, r.URL.Path, wrapped.statusCode, duration, wrapped.bytesWritten)

		// Audit logging for sensitive endpoints
		if s.audit != nil && !s.isPublicEndpoint(r.URL.Path) {
			s.audit.Log("api.request", utils.AuditInfo, map[string]interface{}{
				"request_id":  requestID,
				"method":      r.Method,
				"path":        r.URL.Path,
				"client_ip":   clientIP,
				"status":      wrapped.statusCode,
				"duration_ms": duration.Milliseconds(),
			})
		}
	})
}

// middlewarePanicRecovery recovers from panics and returns 500
func (s *Server) middlewarePanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				requestID := getRequestID(r.Context())

				// Log panic with stack trace
				s.logger.Error("panic recovered",
					utils.ZapString("request_id", requestID),
					utils.ZapString("path", r.URL.Path),
					utils.ZapAny("panic", err),
					utils.ZapString("stack", string(debug.Stack())))

				// Audit panic
				if s.audit != nil {
					s.audit.Log("api.panic", utils.AuditError, map[string]interface{}{
						"request_id": requestID,
						"path":       r.URL.Path,
						"panic":      fmt.Sprintf("%v", err),
					})
				}

				// Return 500 with JSON body
				writeErrorResponse(w, r, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// middlewareIPAllowlist checks IP allowlist
func (s *Server) middlewareIPAllowlist(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := getClientIP(r)
		ip := net.ParseIP(clientIP)

		if ip == nil {
			s.logger.Warn("invalid client IP",
				utils.ZapString("client_ip", clientIP))
			writeErrorResponse(w, r, "INVALID_IP", "invalid client IP", http.StatusUnauthorized)
			return
		}

		if s.ipAllowlist != nil && !s.ipAllowlist.IsAllowed(ip) {
			s.logger.Warn("IP not allowed",
				utils.ZapString("client_ip", clientIP))

			if s.audit != nil {
				s.audit.Log("api.ip_denied", utils.AuditWarn, map[string]interface{}{
					"client_ip":  clientIP,
					"path":       r.URL.Path,
					"request_id": getRequestID(r.Context()),
				})
			}

			writeErrorResponse(w, r, "IP_NOT_ALLOWED", "IP not allowed", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// middlewareRateLimit enforces rate limiting
func (s *Server) middlewareRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limiter := s.rateLimiter
		config := RateLimiterConfig{
			RequestsPerMinute: s.config.RateLimitPerMinute,
			Burst:             s.config.RateLimitBurst,
		}
		if route := s.lookupRouteLimiter(r.Method, r.URL.Path); route != nil {
			limiter = route.limiter
			config = route.config
		}
		// Use client cert fingerprint or IP as identifier
		clientID := getClientIdentifier(r)

		allowed, resetTime := limiter.Allow(clientID)

		// Set rate limit headers
		w.Header().Set(HeaderRateLimitLimit, fmt.Sprintf("%d", config.RequestsPerMinute))
		w.Header().Set(HeaderRateLimitReset, fmt.Sprintf("%d", resetTime))

		if !allowed {
			w.Header().Set(HeaderRateLimitRemaining, "0")

			s.logger.Warn("rate limit exceeded",
				utils.ZapString("client_id", clientID),
				utils.ZapString("path", r.URL.Path))

			if s.audit != nil {
				s.audit.Log("api.rate_limit_exceeded", utils.AuditWarn, map[string]interface{}{
					"client_id":  clientID,
					"path":       r.URL.Path,
					"request_id": getRequestID(r.Context()),
				})
			}

			retryAfter := resetTime - time.Now().Unix()
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			writeErrorResponse(w, r, "RATE_LIMIT_EXCEEDED", "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// middlewareCORS adds CORS headers if needed
func (s *Server) middlewareCORS(next http.Handler) http.Handler {
	allowedOrigins := os.Getenv("API_CORS_ALLOWED_ORIGINS")
	if allowedOrigins == "" {
		allowedOrigins = "*"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigins)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// middlewareSecurityHeaders adds security headers
func (s *Server) middlewareSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")

		next.ServeHTTP(w, r)
	})
}

// middlewareConcurrencyLimit limits concurrent in-flight requests
func (s *Server) middlewareConcurrencyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.sem != nil {
			select {
			case s.sem <- struct{}{}:
				defer func() { <-s.sem }()
			default:
				writeErrorResponse(w, r, "SERVICE_UNAVAILABLE", "too many concurrent requests", http.StatusServiceUnavailable)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Helper functions

// responseWriter wraps http.ResponseWriter to capture status code and size
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

// getRequestID extracts request ID from context
func getRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return id
	}
	return "unknown"
}

// getClientIP extracts client IP from request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (if behind proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Use remote addr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}

// getClientIdentifier returns a unique identifier for the client
func getClientIdentifier(r *http.Request) string {
	// Prefer client certificate fingerprint
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		cert := r.TLS.PeerCertificates[0]
		return fmt.Sprintf("cert:%x", cert.SerialNumber)
	}

	// Fallback to IP
	return fmt.Sprintf("ip:%s", getClientIP(r))
}

// generateRequestID generates a UUIDv7 request ID with UUIDv4 fallback.
func generateRequestID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// Fallback to UUIDv4 if v7 generation fails.
		return uuid.NewString()
	}
	return id.String()
}

// middlewareGlobalAuth enforces authentication for all non-public endpoints
// SECURITY: Single point of authentication control
func (s *Server) middlewareGlobalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if endpoint is public (doesn't require auth)
		if s.isPublicEndpoint(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// SECURITY: Authentication required for this endpoint
		authenticated := false
		clientRole := "anonymous"
		authSource := ""

		// Determine if auth is required based on environment and feature flag
		requireAuth := s.config.Environment == "production" || s.config.RequireAuth

		// Method 1: mTLS Certificate Authentication (production)
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			cert := r.TLS.PeerCertificates[0]
			// Extract role from certificate CN or SAN
			clientRole = extractRoleFromCert(cert.Subject.CommonName)
			authenticated = true
			authSource = "mtls"

			s.logger.InfoContext(r.Context(), "client authenticated via mTLS",
				utils.ZapString("role", clientRole),
				utils.ZapString("cn", cert.Subject.CommonName))
		}

		// Method 2: Bearer token / JWT Authentication
		if !authenticated {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
				if token == "" {
					s.recordBearerValidationFailure(r, errors.New("bearer token format invalid"))
					writeErrorFromUtils(w, r, NewUnauthorizedError("invalid bearer token"))
					return
				}
				if s.validateBearerToken(token) {
					authenticated = true
					principalID, parsedRole := staticBearerIdentity(token)
					clientRole = parsedRole
					authSource = "static_bearer"
					ctx := context.WithValue(r.Context(), ctxKeyPrincipalID, principalID)
					ctx = context.WithValue(ctx, ctxKeyPrincipalType, string(contracts.PrincipalTypeUser))
					ctx = context.WithValue(ctx, ctxKeyAuthSource, authSource)
					r = r.WithContext(ctx)
				} else if s.bearerAuth != nil {
					if identity, err := s.bearerAuth.ValidateToken(r.Context(), token); err == nil {
						authenticated = true
						clientRole = deriveJWTClientRole(identity)
						authSource = "jwt"
						ctx := context.WithValue(r.Context(), ctxKeyPrincipalID, identity.PrincipalID)
						ctx = context.WithValue(ctx, ctxKeyPrincipalType, string(identity.PrincipalType))
						ctx = context.WithValue(ctx, ctxKeyAuthSubject, identity.Subject)
						ctx = context.WithValue(ctx, ctxKeyAuthUsername, identity.Username)
						ctx = context.WithValue(ctx, ctxKeyAuthEmail, identity.Email)
						ctx = context.WithValue(ctx, ctxKeyAuthSource, authSource)
						r = r.WithContext(ctx)
					} else {
						s.recordBearerValidationFailure(r, err)
						writeErrorFromUtils(w, r, NewUnauthorizedError("invalid bearer token"))
						return
					}
				} else {
					s.recordBearerValidationFailure(r, errBearerValidatorUnavailable)
					writeErrorFromUtils(w, r, NewUnauthorizedError("invalid bearer token"))
					return
				}
			}
		}

		// Method 3: API Key Authentication (fallback)
		if !authenticated {
			apiKey := r.Header.Get("X-API-Key")
			if strings.TrimSpace(apiKey) != "" && s.validateAPIKey(apiKey) {
				authenticated = true
				clientRole = "api_client"
				authSource = "api_key"
			}
		}

		// SECURITY: Block unauthenticated requests when required
		if !authenticated {
			if requireAuth {
				s.logger.WarnContext(r.Context(), "unauthenticated request blocked",
					utils.ZapString("path", r.URL.Path),
					utils.ZapString("client_ip", getClientIP(r)))
				writeErrorFromUtils(w, r, NewUnauthorizedError("authentication required"))
				return
			}
			// Dev mode and auth not required: allow with warning
			s.logger.WarnContext(r.Context(), "unauthenticated request allowed (API_REQUIRE_AUTH=false)",
				utils.ZapString("path", r.URL.Path))
			authenticated = true
			clientRole = "developer"
			authSource = "developer_fallback"
		}

		// Enforce RBAC whenever it is enabled. Environment-specific bypasses make
		// live JWT auth behave differently from the configured permission model.
		requiredRole := s.getRequiredRole(r.Method, r.URL.Path)
		if s.config.RBACEnabled && requiredRole != "" && !s.hasRole(clientRole, requiredRole) {
			// JWT callers are normalized as authenticated_user and must reach the
			// security bridge for policy/audit reads where access membership and
			// authorizer checks make the final decision.
			if strings.EqualFold(strings.TrimSpace(clientRole), "authenticated_user") &&
				(requiredRole == "policy_reader" || requiredRole == "audit_reader") {
				ctx := context.WithValue(r.Context(), ctxKeyClientRole, clientRole)
				if authSource != "" {
					ctx = context.WithValue(ctx, ctxKeyAuthSource, authSource)
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			s.logger.WarnContext(r.Context(), "access denied - insufficient role",
				utils.ZapString("path", r.URL.Path),
				utils.ZapString("client_role", clientRole),
				utils.ZapString("required_role", requiredRole))

			writeErrorFromUtils(w, r, NewForbiddenError("insufficient permissions"))
			return
		}

		// Add role to context
		ctx := context.WithValue(r.Context(), ctxKeyClientRole, clientRole)
		if authSource != "" {
			ctx = context.WithValue(ctx, ctxKeyAuthSource, authSource)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) recordBearerValidationFailure(r *http.Request, err error) {
	if err == nil {
		return
	}
	requestID := getRequestID(r.Context())
	reason := classifyBearerValidationFailure(err)
	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		detail = "validation_failed"
	}
	detail = sanitizeBearerValidationDetail(detail)
	if s.logger != nil {
		s.logger.WarnContext(r.Context(), "bearer token validation failed",
			utils.ZapString("request_id", requestID),
			utils.ZapString("path", r.URL.Path),
			utils.ZapString("client_ip", getClientIP(r)),
			utils.ZapString("reason", reason),
			utils.ZapString("detail", detail))
	}
	if s.audit != nil {
		_ = s.audit.Log("api.auth.bearer_invalid", utils.AuditSecurity, map[string]interface{}{
			"request_id": requestID,
			"path":       r.URL.Path,
			"client_ip":  getClientIP(r),
			"reason":     reason,
			"detail":     detail,
		})
	}
}

func classifyBearerValidationFailure(err error) string {
	if err == nil {
		return "validation_failed"
	}
	if errors.Is(err, errBearerValidatorUnavailable) {
		return "validator_unavailable"
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "expired"):
		return "expired"
	case strings.Contains(msg, "issuer"):
		return "issuer_mismatch"
	case strings.Contains(msg, "audience"):
		return "audience_mismatch"
	case strings.Contains(msg, "signature"):
		return "invalid_signature"
	case strings.Contains(msg, "subject"):
		return "invalid_subject"
	case strings.Contains(msg, "format"), strings.Contains(msg, "malformed"), strings.Contains(msg, "parse"):
		return "invalid_format"
	default:
		return "validation_failed"
	}
}

func sanitizeBearerValidationDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return "validation_failed"
	}
	lower := strings.ToLower(detail)
	// Guard against accidentally logging raw credential/token fragments from validator errors.
	if strings.Contains(lower, "token ") ||
		strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "authorization:") ||
		containsTokenLikeFragment(detail) {
		return "[REDACTED]"
	}
	const maxDetailLen = 240
	if len(detail) > maxDetailLen {
		return detail[:maxDetailLen]
	}
	return detail
}

func containsTokenLikeFragment(detail string) bool {
	fields := strings.FieldsFunc(detail, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', ',', ';', ':', '=', '"', '\'', '(', ')', '[', ']', '{', '}':
			return true
		default:
			return false
		}
	})
	for _, field := range fields {
		f := strings.TrimSpace(field)
		if len(f) < 24 {
			continue
		}
		// JWT-like fragments.
		if strings.Count(f, ".") >= 2 {
			return true
		}
		if isBase64URLLike(f) {
			return true
		}
	}
	return false
}

func isBase64URLLike(value string) bool {
	if len(value) < 32 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func deriveJWTClientRole(identity *validatedBearerIdentity) string {
	if identity == nil {
		return "authenticated_user"
	}
	clientID := strings.ToLower(strings.TrimSpace(identity.ClientID))
	if clientID == "" {
		return "authenticated_user"
	}
	patterns := []struct {
		substr string
		role   string
	}{
		{".platform.admin.", "admin"},
		{".control.admin.", "control_lease_admin"},
		{".block.reader.", "block_reader"},
		{".state.reader.", "state_reader"},
		{".validator.reader.", "validator_reader"},
		{".stats.reader.", "stats_reader"},
		{".metrics.reader.", "metrics_reader"},
		{".policy.reader.", "policy_reader"},
		{".audit.reader.", "audit_reader"},
		{".network.reader.", "network_reader"},
		{".consensus.reader.", "consensus_reader"},
		{".ai.reader.", "ai_reader"},
		{".anomaly.reader.", "anomaly_reader"},
		{".outbox.reader.", "control_outbox_reader"},
		{".trace.reader.", "control_trace_reader"},
		{".lease.reader.", "control_lease_reader"},
		{".ack.reader.", "control_ack_reader"},
		{".outbox.operator.", "control_outbox_operator"},
		{".api.client.", "api_client"},
		{".developer.", "developer"},
		{".support.access.approver.", "control_lease_admin"},
	}
	for _, pattern := range patterns {
		if strings.Contains(clientID, pattern.substr) {
			return pattern.role
		}
	}
	return "authenticated_user"
}

// validateBearerToken checks provided token against configured static tokens (constant-time)
func (s *Server) validateBearerToken(token string) bool {
	return s.validateStaticToken(token)
}

// validateAPIKey checks provided API key against configured static tokens (constant-time).
func (s *Server) validateAPIKey(apiKey string) bool {
	return s.validateStaticToken(apiKey)
}

func (s *Server) validateStaticToken(token string) bool {
	if len(s.config.BearerTokens) == 0 || token == "" {
		return false
	}
	// Constant-time compare
	for _, t := range s.config.BearerTokens {
		if subtleConstantTimeCompare(token, strings.TrimSpace(t)) {
			return true
		}
	}
	return false
}

// subtleConstantTimeCompare performs constant-time string equality
func subtleConstantTimeCompare(a, b string) bool {
	if len(a) != len(b) {
		// still perform loop to avoid timing leakage on length
		var mismatch byte = 0
		la, lb := len(a), len(b)
		max := la
		if lb > max {
			max = lb
		}
		for i := 0; i < max; i++ {
			var ca, cb byte
			if i < la {
				ca = a[i]
			}
			if i < lb {
				cb = b[i]
			}
			mismatch |= ca ^ cb
		}
		return mismatch == 0
	}
	var v byte = 0
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func staticBearerIdentity(token string) (string, string) {
	token = strings.TrimSpace(token)
	role := "api_client"
	principalID := ""

	segments := strings.Split(token, ";")
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		key, value, ok := strings.Cut(segment, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "id", "principal_id", "principal":
			principalID = value
		case "role":
			if value != "" {
				role = value
			}
		}
	}

	if strings.TrimSpace(principalID) == "" {
		sum := sha256.Sum256([]byte(token))
		principalID = "user:static-" + fmt.Sprintf("%x", sum[:8])
	} else if !strings.Contains(principalID, ":") {
		principalID = "user:" + principalID
	}

	return principalID, role
}

// extractRoleFromCert extracts role from certificate CN
func extractRoleFromCert(cn string) string {
	// Parse CN format: "role:validator" or "service:api" or just "admin"
	parts := strings.Split(cn, ":")
	if len(parts) == 2 {
		return parts[1]
	}
	return cn
}

// writeJSON writes a JSON response
// (intentionally no generic writeJSON helper here; handlers use writeJSONResponse/writeErrorResponse)
