package config

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"backend/pkg/utils"
)

// APIConfig holds read-only API server configuration
type APIConfig struct {
	// Server settings
	ListenAddr string
	BasePath   string // e.g., "/api/v1"

	// TLS configuration (REQUIRED in production)
	TLSEnabled      bool
	TLSCertFile     string
	TLSKeyFile      string
	TLSClientCAFile string // For mTLS client verification
	TLSMinVersion   uint16 // Default: TLS 1.3

	// Timeouts
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration

	// Security
	RBACEnabled  bool                // Role-based access control
	IPAllowlist  []string            // Allowed client IPs/CIDRs
	AllowedRoles map[string][]string // Role -> allowed endpoints mapping
	// Auth gating (feature-flagged)
	RequireAuth  bool     // If true, require auth even outside production (e.g., staging)
	BearerTokens []string // Optional static bearer tokens accepted for auth

	// Rate limiting
	RateLimitEnabled   bool
	RateLimitPerMinute int
	RateLimitBurst     int
	RouteRateLimits    map[string]RateLimitOverride

	// Observability
	EnableMetrics          bool
	EnableAudit            bool
	EnablePprof            bool // Debug only, never in production
	MetricsAllowedPrefixes []string
	MetricsCompress        bool

	// Request limits
	MaxRequestSize      int64 // bytes
	MaxHeaderSize       int   // bytes
	MaxConcurrentReqs   int
	RequestTimeout      time.Duration
	DashboardBlockLimit int
	DashboardCacheTTL   time.Duration

	// Environment
	Environment string // "development", "staging", "production"

	// Readiness policy
	// If true, /ready will only hard-gate on core control-plane dependencies and
	// report auxiliary dependencies (Kafka/Redis/AI) as degraded without blocking.
	AllowDegradedBootstrap bool

	// AI service integration
	AIServiceBaseURL string
	AIServiceTimeout time.Duration
	AIServiceToken   string

	// Redis integration (optional)
	RedisEnabled    bool
	RedisHost       string
	RedisPort       int
	RedisPassword   string
	RedisDB         int
	RedisTLSEnabled bool

	// Optional node alias configuration for telemetry overlays
	NodeAliasMap  map[string]string
	NodeAliasList []string
}

// RateLimitOverride configures per-route rate limiting policies.
type RateLimitOverride struct {
	RequestsPerMinute int `json:"rpm"`
	Burst             int `json:"burst"`
}

// DefaultAPIConfig returns secure defaults
func DefaultAPIConfig() *APIConfig {
	return &APIConfig{
		ListenAddr:         ":8443",
		BasePath:           "/api/v1",
		TLSEnabled:         true,
		TLSMinVersion:      tls.VersionTLS13,
		ReadTimeout:        10 * time.Second,
		WriteTimeout:       30 * time.Second,
		IdleTimeout:        60 * time.Second,
		ShutdownTimeout:    30 * time.Second,
		RBACEnabled:        true,
		RateLimitEnabled:   true,
		RateLimitPerMinute: 100,
		RateLimitBurst:     10,
		RouteRateLimits:    make(map[string]RateLimitOverride),
		EnableMetrics:      true,
		EnableAudit:        true,
		EnablePprof:        false,
		MetricsAllowedPrefixes: []string{
			"process_",
			"go_",
			"cybermesh_",
			"consensus_",
			"kafka_",
			"redis_",
			"cockroach_",
			"p2p_",
			"api_",
			"storage_",
			"state_",
			"mempool_",
		},
		MetricsCompress:        true,
		MaxRequestSize:         1024 * 1024, // 1MB
		MaxHeaderSize:          1024 * 1024, // 1MB
		MaxConcurrentReqs:      100,
		RequestTimeout:         30 * time.Second,
		DashboardBlockLimit:    50,
		DashboardCacheTTL:      3 * time.Second,
		Environment:            "production",
		AllowDegradedBootstrap: false,
		AIServiceTimeout:       2 * time.Second,
		AllowedRoles:           defaultRoleMapping(),
	}
}

// LoadAPIConfig loads API configuration from environment
func LoadAPIConfig(cm *utils.ConfigManager) (*APIConfig, error) {
	cfg := DefaultAPIConfig()

	// Server settings
	if addr := cm.GetString("API_LISTEN_ADDR", ""); addr != "" {
		cfg.ListenAddr = addr
	}

	if basePath := cm.GetString("API_BASE_PATH", ""); basePath != "" {
		cfg.BasePath = basePath
	}

	// TLS configuration
	cfg.TLSEnabled = cm.GetBool("API_TLS_ENABLED", true)
	cfg.TLSCertFile = cm.GetString("API_TLS_CERT_FILE", "")
	cfg.TLSKeyFile = cm.GetString("API_TLS_KEY_FILE", "")
	cfg.TLSClientCAFile = cm.GetString("API_TLS_CLIENT_CA_FILE", "")

	if tlsVersion := cm.GetString("API_TLS_MIN_VERSION", ""); tlsVersion != "" {
		switch tlsVersion {
		case "1.2":
			cfg.TLSMinVersion = tls.VersionTLS12
		case "1.3":
			cfg.TLSMinVersion = tls.VersionTLS13
		default:
			return nil, fmt.Errorf("invalid TLS version: %s (must be 1.2 or 1.3)", tlsVersion)
		}
	}

	// Timeouts
	if timeout := cm.GetDuration("API_READ_TIMEOUT", 0); timeout > 0 {
		cfg.ReadTimeout = timeout
	}
	if timeout := cm.GetDuration("API_WRITE_TIMEOUT", 0); timeout > 0 {
		cfg.WriteTimeout = timeout
	}
	if timeout := cm.GetDuration("API_IDLE_TIMEOUT", 0); timeout > 0 {
		cfg.IdleTimeout = timeout
	}
	if timeout := cm.GetDuration("API_SHUTDOWN_TIMEOUT", 0); timeout > 0 {
		cfg.ShutdownTimeout = timeout
	}
	if timeout := cm.GetDuration("API_REQUEST_TIMEOUT", 0); timeout > 0 {
		cfg.RequestTimeout = timeout
	}
	if ttl := cm.GetDuration("DASHBOARD_CACHE_TTL", 0); ttl > 0 {
		cfg.DashboardCacheTTL = ttl
	}

	// Security
	cfg.RBACEnabled = cm.GetBool("API_RBAC_ENABLED", true)

	if allowlist := cm.GetString("API_IP_ALLOWLIST", ""); allowlist != "" {
		cfg.IPAllowlist = parseCommaSeparated(allowlist)
	}

	// Feature-flagged auth gating
	cfg.RequireAuth = cm.GetBool("API_REQUIRE_AUTH", false)
	if tokens := cm.GetString("API_BEARER_TOKENS", ""); tokens != "" {
		cfg.BearerTokens = parseCommaSeparated(tokens)
	}

	// Rate limiting
	cfg.RateLimitEnabled = cm.GetBool("API_RATE_LIMIT_ENABLED", true)
	if rpm := cm.GetInt("API_RATE_LIMIT_PER_MINUTE", 0); rpm > 0 {
		cfg.RateLimitPerMinute = rpm
	}
	if burst := cm.GetInt("API_RATE_LIMIT_BURST", 0); burst > 0 {
		cfg.RateLimitBurst = burst
	}
	if overrides := cm.GetString("API_RATE_LIMIT_OVERRIDES", ""); overrides != "" {
		parsed := make(map[string]RateLimitOverride)
		if err := json.Unmarshal([]byte(overrides), &parsed); err != nil {
			return nil, fmt.Errorf("invalid API_RATE_LIMIT_OVERRIDES: %w", err)
		}
		cfg.RouteRateLimits = parsed
	}

	// Observability
	cfg.EnableMetrics = cm.GetBool("API_ENABLE_METRICS", true)
	cfg.EnableAudit = cm.GetBool("API_ENABLE_AUDIT", true)
	cfg.EnablePprof = cm.GetBool("API_ENABLE_PPROF", false)
	if prefixes := cm.GetString("API_METRICS_ALLOWED_PREFIXES", ""); prefixes != "" {
		cfg.MetricsAllowedPrefixes = parseCommaSeparated(prefixes)
	}
	cfg.MetricsCompress = cm.GetBool("API_METRICS_COMPRESS", true)

	// Request limits
	if maxSize := cm.GetInt("API_MAX_REQUEST_SIZE", 0); maxSize > 0 {
		cfg.MaxRequestSize = int64(maxSize)
	}
	if maxHeader := cm.GetInt("API_MAX_HEADER_SIZE", 0); maxHeader > 0 {
		cfg.MaxHeaderSize = maxHeader
	}
	if maxReqs := cm.GetInt("API_MAX_CONCURRENT_REQUESTS", 0); maxReqs > 0 {
		cfg.MaxConcurrentReqs = maxReqs
	}
	if blockLimit := cm.GetInt("API_DASHBOARD_BLOCK_LIMIT", 0); blockLimit > 0 {
		cfg.DashboardBlockLimit = blockLimit
	}

	// Environment
	if env := cm.GetString("ENVIRONMENT", ""); env != "" {
		cfg.Environment = env
	}

	// Readiness policy
	// Only allow degraded bootstrap outside production.
	if strings.EqualFold(cfg.Environment, "production") {
		cfg.AllowDegradedBootstrap = false
	} else {
		cfg.AllowDegradedBootstrap = cm.GetBool("ALLOW_DEGRADED_BOOTSTRAP", false)
	}

	// AI service integration
	if base := cm.GetString("AI_SERVICE_API_BASE", ""); base != "" {
		cfg.AIServiceBaseURL = strings.TrimRight(base, "/")
	}

	if timeout := cm.GetDuration("AI_SERVICE_API_TIMEOUT", 0); timeout > 0 {
		cfg.AIServiceTimeout = timeout
	}

	if token := cm.GetString("AI_SERVICE_API_TOKEN", ""); token != "" {
		cfg.AIServiceToken = token
	}

	// Redis configuration (optional)
	redisHost := cm.GetString("REDIS_HOST", "")
	redisPort := cm.GetInt("REDIS_PORT", 6379)
	if redisHost != "" {
		cfg.RedisEnabled = true
		cfg.RedisHost = redisHost
		cfg.RedisPort = redisPort
		cfg.RedisPassword = cm.GetString("REDIS_PASSWORD", "")
		cfg.RedisDB = cm.GetInt("REDIS_DB", 0)
		cfg.RedisTLSEnabled = cm.GetBool("REDIS_TLS_ENABLED", false)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("API config validation failed: %w", err)
	}

	if aliasMapEnv := cm.GetString("NETWORK_NODE_ALIASES", ""); aliasMapEnv != "" {
		cfg.NodeAliasMap = parseAliasMap(aliasMapEnv)
	}

	if aliasListEnv := cm.GetString("NETWORK_NODE_ALIAS_LIST", ""); aliasListEnv != "" {
		cfg.NodeAliasList = parseCommaSeparated(aliasListEnv)
	}

	return cfg, nil
}

func parseAliasMap(input string) map[string]string {
	result := make(map[string]string)
	if input == "" {
		return result
	}
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "{") {
		var jsonMap map[string]string
		if err := json.Unmarshal([]byte(trimmed), &jsonMap); err == nil {
			for k, v := range jsonMap {
				if key := strings.TrimSpace(k); key != "" {
					result[strings.ToLower(key)] = strings.TrimSpace(v)
				}
			}
			return result
		}
	}
	parts := strings.Split(trimmed, ",")
	for _, part := range parts {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		value := strings.TrimSpace(kv[1])
		if key != "" && value != "" {
			result[key] = value
		}
	}
	return result
}

// Validate checks configuration for security compliance
func (c *APIConfig) Validate() error {
	// Environment validation
	if c.Environment != "development" && c.Environment != "staging" && c.Environment != "production" {
		return fmt.Errorf("invalid environment: %s (must be development, staging, or production)", c.Environment)
	}

	// TLS validation (required in production/staging)
	if c.Environment == "production" || c.Environment == "staging" {
		if !c.TLSEnabled {
			return &SecurityError{
				Field:  "TLSEnabled",
				Reason: "TLS must be enabled in production/staging",
			}
		}

		if c.TLSCertFile == "" {
			return &SecurityError{
				Field:  "TLSCertFile",
				Reason: "TLS certificate file required in production/staging",
			}
		}

		if c.TLSKeyFile == "" {
			return &SecurityError{
				Field:  "TLSKeyFile",
				Reason: "TLS key file required in production/staging",
			}
		}

		// Verify cert/key files exist
		if _, err := os.Stat(c.TLSCertFile); os.IsNotExist(err) {
			return &SecurityError{
				Field:  "TLSCertFile",
				Reason: fmt.Sprintf("certificate file does not exist: %s", c.TLSCertFile),
			}
		}

		if _, err := os.Stat(c.TLSKeyFile); os.IsNotExist(err) {
			return &SecurityError{
				Field:  "TLSKeyFile",
				Reason: fmt.Sprintf("key file does not exist: %s", c.TLSKeyFile),
			}
		}

		// TLS 1.3 required in production
		if c.Environment == "production" && c.TLSMinVersion < tls.VersionTLS13 {
			return &SecurityError{
				Field:  "TLSMinVersion",
				Reason: "TLS 1.3 required in production",
			}
		}
	}

	// mTLS validation (client CA required for RBAC)
	if c.RBACEnabled && c.TLSEnabled {
		if c.TLSClientCAFile == "" {
			return &SecurityError{
				Field:  "TLSClientCAFile",
				Reason: "Client CA file required for RBAC (mTLS)",
			}
		}

		if _, err := os.Stat(c.TLSClientCAFile); os.IsNotExist(err) {
			return &SecurityError{
				Field:  "TLSClientCAFile",
				Reason: fmt.Sprintf("client CA file does not exist: %s", c.TLSClientCAFile),
			}
		}
	}

	// Timeout validation
	if c.ReadTimeout <= 0 {
		return fmt.Errorf("read timeout must be positive")
	}
	if c.WriteTimeout <= 0 {
		return fmt.Errorf("write timeout must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}

	// Rate limit validation
	if c.RateLimitEnabled {
		if c.RateLimitPerMinute <= 0 {
			return fmt.Errorf("rate limit per minute must be positive")
		}
		if c.RateLimitBurst < 0 {
			return fmt.Errorf("rate limit burst cannot be negative")
		}
	}

	// Request size validation
	if c.MaxRequestSize <= 0 {
		return fmt.Errorf("max request size must be positive")
	}
	if c.MaxHeaderSize <= 0 {
		return fmt.Errorf("max header size must be positive")
	}
	if c.MaxConcurrentReqs <= 0 {
		return fmt.Errorf("max concurrent requests must be positive")
	}
	if c.DashboardBlockLimit <= 0 || c.DashboardBlockLimit > 50 {
		return fmt.Errorf("dashboard block limit must be between 1 and 50")
	}
	if c.DashboardCacheTTL < 0 {
		return fmt.Errorf("dashboard cache TTL cannot be negative")
	}
	for key, override := range c.RouteRateLimits {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("route rate limit key cannot be empty")
		}
		if override.RequestsPerMinute <= 0 {
			return fmt.Errorf("route rate limit rpm must be positive for key %s", key)
		}
		if override.Burst < 0 {
			return fmt.Errorf("route rate limit burst cannot be negative for key %s", key)
		}
	}

	// Security validation (audit required in production)
	if c.Environment == "production" && !c.EnableAudit {
		return &SecurityError{
			Field:  "EnableAudit",
			Reason: "Audit logging must be enabled in production",
		}
	}

	// Pprof validation (forbidden in production)
	if c.Environment == "production" && c.EnablePprof {
		return &SecurityError{
			Field:  "EnablePprof",
			Reason: "Pprof profiling forbidden in production (security risk)",
		}
	}

	if c.AIServiceBaseURL != "" {
		u, err := url.Parse(c.AIServiceBaseURL)
		if err != nil || !u.IsAbs() {
			return fmt.Errorf("invalid AI service base URL: %s", c.AIServiceBaseURL)
		}
		if c.Environment == "production" && u.Scheme != "https" {
			return fmt.Errorf("AI service base URL must use https in production")
		}
	}

	if c.RedisEnabled {
		if c.RedisHost == "" {
			return fmt.Errorf("redis host must be provided when Redis is enabled")
		}
		if c.RedisPort <= 0 || c.RedisPort > 65535 {
			return fmt.Errorf("redis port must be between 1 and 65535")
		}
		if c.RedisDB < 0 {
			return fmt.Errorf("redis db index cannot be negative")
		}
	}

	return nil
}

// IsDevelopment returns true if in development mode
func (c *APIConfig) IsDevelopment() bool {
	return c.Environment == "development"
}

// IsProduction returns true if in production mode
func (c *APIConfig) IsProduction() bool {
	return c.Environment == "production"
}

// RequiresMTLS returns true if mTLS is required
func (c *APIConfig) RequiresMTLS() bool {
	return c.TLSEnabled && c.TLSClientCAFile != "" && c.RBACEnabled
}

// defaultRoleMapping returns default RBAC role mappings
func defaultRoleMapping() map[string][]string {
	return map[string][]string{
		"admin": {
			"/health",
			"/ready",
			"/metrics",
			"/blocks",
			"/state",
			"/validators",
			"/stats",
		},
		"block_reader": {
			"/health",
			"/ready",
			"/blocks",
		},
		"state_reader": {
			"/health",
			"/ready",
			"/state",
		},
		"validator_reader": {
			"/health",
			"/ready",
			"/validators",
		},
		"stats_reader": {
			"/health",
			"/ready",
			"/stats",
		},
		"metrics_reader": {
			"/health",
			"/ready",
			"/metrics",
		},
	}
}

// parseCommaSeparated splits comma-separated string into slice
func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}

	var result []string
	for _, item := range splitAndTrim(s, ",") {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

// splitAndTrim splits string and trims whitespace
func splitAndTrim(s, sep string) []string {
	parts := []string{}
	for _, p := range split(s, sep) {
		trimmed := trim(p)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func split(s, sep string) []string {
	if s == "" {
		return nil
	}

	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep[0] {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trim(s string) string {
	start := 0
	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}
