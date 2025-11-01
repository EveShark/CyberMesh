package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"backend/pkg/utils"
)

// Middleware context keys
type contextKey string

const (
	ctxKeyRequestID  contextKey = "request_id"
	ctxKeyClientIP   contextKey = "client_ip"
	ctxKeyClientCert contextKey = "client_cert"
	ctxKeyClientRole contextKey = "client_role"
	ctxKeyStartTime  contextKey = "start_time"
)

// middlewareRequestID adds a unique request ID to each request
func (s *Server) middlewareRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client provided request ID
		requestID := r.Header.Get(HeaderRequestID)

		// Generate if not provided or invalid
		if requestID == "" || !isValidRequestID(requestID) {
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
		// Use client cert fingerprint or IP as identifier
		clientID := getClientIdentifier(r)

		allowed, resetTime := s.rateLimiter.Allow(clientID)

		// Set rate limit headers
		w.Header().Set(HeaderRateLimitLimit, fmt.Sprintf("%d", s.config.RateLimitPerMinute))
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

			writeErrorResponse(w, r, "RATE_LIMIT_EXCEEDED", "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// middlewareCORS adds CORS headers if needed
func (s *Server) middlewareCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// In production, CORS should be tightly controlled
		// For now, we'll skip CORS as this is API-to-API communication with mTLS

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

// generateRequestID generates a random request ID
func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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

		// Method 1: mTLS Certificate Authentication (production)
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			cert := r.TLS.PeerCertificates[0]
			// Extract role from certificate CN or SAN
			clientRole = extractRoleFromCert(cert.Subject.CommonName)
			authenticated = true

			s.logger.InfoContext(r.Context(), "client authenticated via mTLS",
				utils.ZapString("role", clientRole),
				utils.ZapString("cn", cert.Subject.CommonName))
		}

		// Method 2: JWT Token Authentication (fallback)
		if !authenticated {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				_ = strings.TrimPrefix(authHeader, "Bearer ") // token for future JWT validation
				// TODO: Implement JWT validation with token
				// For now, accept any token in development mode
				if s.config.Environment != "production" {
					authenticated = true
					clientRole = "developer"
					s.logger.WarnContext(r.Context(), "JWT auth not fully implemented, accepting token in dev mode")
				}
			}
		}

		// Method 3: API Key Authentication (fallback)
		if !authenticated {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey != "" {
				// TODO: Implement API key validation
				// For now, accept any key in development mode
				if s.config.Environment != "production" {
					authenticated = true
					clientRole = "api_client"
					s.logger.WarnContext(r.Context(), "API key auth not fully implemented, accepting key in dev mode")
				}
			}
		}

		// SECURITY: Block unauthenticated requests in production
		if !authenticated {
			if s.config.Environment == "production" {
				s.logger.WarnContext(r.Context(), "unauthenticated request blocked",
					utils.ZapString("path", r.URL.Path),
					utils.ZapString("client_ip", getClientIP(r)))

				writeErrorFromUtils(w, r, NewUnauthorizedError("authentication required"))
				return
			} else {
				// Development mode: Allow but log warning
				s.logger.WarnContext(r.Context(), "unauthenticated request allowed in dev mode",
					utils.ZapString("path", r.URL.Path))
				authenticated = true
				clientRole = "developer"
			}
		}

		// Check role-based access control
		// In development mode, skip RBAC checks (allow all authenticated requests)
		if s.config.Environment != "production" {
			// Development mode: Allow all roles
			ctx := context.WithValue(r.Context(), ctxKeyClientRole, clientRole)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Production mode: Enforce RBAC
		requiredRole := s.getRequiredRole(r.URL.Path)
		if requiredRole != "" && !s.hasRole(clientRole, requiredRole) {
			s.logger.WarnContext(r.Context(), "access denied - insufficient role",
				utils.ZapString("path", r.URL.Path),
				utils.ZapString("client_role", clientRole),
				utils.ZapString("required_role", requiredRole))

			writeErrorFromUtils(w, r, NewForbiddenError("insufficient permissions"))
			return
		}

		// Add role to context
		ctx := context.WithValue(r.Context(), ctxKeyClientRole, clientRole)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
