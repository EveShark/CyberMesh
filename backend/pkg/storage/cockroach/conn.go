package cockroach

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver

	"backend/pkg/utils"
)

// Security errors
var (
	ErrTLSRequired           = errors.New("storage: TLS is required in production/staging environments")
	ErrDSNRequired           = errors.New("storage: DB_DSN is required")
	ErrDSNInvalid            = errors.New("storage: DB_DSN is invalid")
	ErrConnectionFailed      = errors.New("storage: failed to establish connection")
	ErrConnectionPoolInvalid = errors.New("storage: connection pool configuration invalid")
)

// Environment indicates the deployment environment
type Environment string

const (
	EnvProduction  Environment = "production"
	EnvStaging     Environment = "staging"
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
)

// ConnectionConfig holds database connection configuration
type ConnectionConfig struct {
	// Connection
	DSN         string        // PostgreSQL connection string (REQUIRED, SENSITIVE)
	ConnTimeout time.Duration // Connection timeout (default: 5s)
	TLSEnabled  bool          // TLS enabled (REQUIRED in prod/staging)

	// Pool configuration
	MaxOpenConns int           // Maximum open connections (default: 50)
	MaxIdleConns int           // Maximum idle connections (default: 10)
	MaxLifetime  time.Duration // Maximum connection lifetime (default: 30m)

	// Environment
	Environment Environment // Deployment environment (default: production)

	// Infrastructure
	ConfigManager *utils.ConfigManager // Config source (optional, uses env if nil)
	Logger        *utils.Logger        // Logger (optional)
	AuditLogger   *utils.AuditLogger   // Audit logger (optional)
}

// NewConnection creates a secure database connection with military-grade security
//
// Security features:
// - TLS enforcement in prod/staging (fail-closed)
// - DSN validation and redaction
// - Connection pool limits
// - Audit logging
// - Secret protection (never logs DSN)
func NewConnection(ctx context.Context, cfg *ConnectionConfig) (*sql.DB, error) {
	if cfg == nil {
		return nil, errors.New("storage: connection config is required")
	}

	// Load configuration from environment if ConfigManager provided
	if cfg.ConfigManager != nil {
		if err := loadConfigFromManager(cfg); err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Apply defaults
	applyDefaults(cfg)

	// Validate configuration (security-critical)
	if err := validateConfig(cfg); err != nil {
		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Security("db_connection_rejected", map[string]interface{}{
				"error":       err.Error(),
				"environment": string(cfg.Environment),
			})
		}
		return nil, err
	}

	// Audit connection attempt (without secrets)
	auditConnectionAttempt(cfg)

	// Enforce TLS in production/staging
	if err := enforceTLS(cfg); err != nil {
		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Security("db_tls_enforcement_failed", map[string]interface{}{
				"error":       err.Error(),
				"environment": string(cfg.Environment),
				"tls_enabled": cfg.TLSEnabled,
			})
		}
		return nil, err
	}

	// Open connection with timeout
	db, err := openConnectionWithTimeout(ctx, cfg)
	if err != nil {
		if cfg.AuditLogger != nil {
			_ = cfg.AuditLogger.Warn("db_connection_failed", map[string]interface{}{
				"error": err.Error(),
			})
		}
		return nil, err
	}

	// Configure connection pool
	configurePool(db, cfg)

	// Verify connection
	if err := verifyConnection(ctx, db, cfg); err != nil {
		db.Close()
		return nil, err
	}

	// Success audit
	if cfg.AuditLogger != nil {
		_ = cfg.AuditLogger.Info("db_connection_established", map[string]interface{}{
			"environment":    string(cfg.Environment),
			"max_open_conns": cfg.MaxOpenConns,
			"max_idle_conns": cfg.MaxIdleConns,
			"tls_enabled":    cfg.TLSEnabled,
		})
	}

	if cfg.Logger != nil {
		cfg.Logger.InfoContext(ctx, "CockroachDB connection established",
			utils.ZapString("environment", string(cfg.Environment)),
			utils.ZapBool("tls_enabled", cfg.TLSEnabled),
			utils.ZapInt("max_open", cfg.MaxOpenConns),
			utils.ZapInt("max_idle", cfg.MaxIdleConns))
	}

	return db, nil
}

// loadConfigFromManager loads configuration from utils.ConfigManager
func loadConfigFromManager(cfg *ConnectionConfig) error {
	cm := cfg.ConfigManager

	// Load DSN (SENSITIVE - use GetSecret to mark as sensitive)
	dsn, err := cm.GetSecret("DB_DSN")
	if err == nil && dsn != "" {
		cfg.DSN = dsn
	}

	// Load connection timeout
	cfg.ConnTimeout = cm.GetDuration("DB_CONN_TIMEOUT", 5*time.Second)

	// Load TLS setting (must be true in prod/staging)
	cfg.TLSEnabled = cm.GetBool("DB_TLS", true)

	// Load pool configuration
	cfg.MaxOpenConns = cm.GetInt("DB_MAX_OPEN", 50)
	cfg.MaxIdleConns = cm.GetInt("DB_MAX_IDLE", 10)
	cfg.MaxLifetime = cm.GetDuration("DB_MAX_LIFETIME", 30*time.Minute)

	// Load environment
	envStr := cm.GetString("ENVIRONMENT", "production")
	cfg.Environment = Environment(strings.ToLower(envStr))

	return nil
}

// applyDefaults applies default values to configuration
func applyDefaults(cfg *ConnectionConfig) {
	if cfg.ConnTimeout == 0 {
		cfg.ConnTimeout = 5 * time.Second
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = 50
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 10
	}
	if cfg.MaxLifetime == 0 {
		cfg.MaxLifetime = 30 * time.Minute
	}
	if cfg.Environment == "" {
		cfg.Environment = EnvProduction
	}
}

// validateConfig validates configuration (security-critical)
func validateConfig(cfg *ConnectionConfig) error {
	// DSN is required
	if strings.TrimSpace(cfg.DSN) == "" {
		return ErrDSNRequired
	}

	// Validate DSN format (basic check without logging the actual DSN)
	if !strings.HasPrefix(cfg.DSN, "postgres://") && !strings.HasPrefix(cfg.DSN, "postgresql://") {
		return fmt.Errorf("%w: must start with postgres:// or postgresql://", ErrDSNInvalid)
	}

	// Validate pool configuration
	if cfg.MaxOpenConns < 1 || cfg.MaxOpenConns > 1000 {
		return fmt.Errorf("%w: max_open_conns must be between 1 and 1000", ErrConnectionPoolInvalid)
	}
	if cfg.MaxIdleConns < 0 || cfg.MaxIdleConns > cfg.MaxOpenConns {
		return fmt.Errorf("%w: max_idle_conns must be between 0 and max_open_conns", ErrConnectionPoolInvalid)
	}
	if cfg.MaxLifetime < 0 {
		return fmt.Errorf("%w: max_lifetime must be non-negative", ErrConnectionPoolInvalid)
	}
	if cfg.ConnTimeout < time.Second || cfg.ConnTimeout > time.Minute {
		return fmt.Errorf("%w: conn_timeout must be between 1s and 1m", ErrConnectionPoolInvalid)
	}

	return nil
}

// enforceTLS enforces TLS in production/staging environments (fail-closed security)
func enforceTLS(cfg *ConnectionConfig) error {
	// Production and staging MUST use TLS
	if cfg.Environment == EnvProduction || cfg.Environment == EnvStaging {
		if !cfg.TLSEnabled {
			return fmt.Errorf("%w: environment=%s", ErrTLSRequired, cfg.Environment)
		}

		// Verify DSN contains sslmode parameter
		if !strings.Contains(cfg.DSN, "sslmode=") {
			return fmt.Errorf("%w: DSN must contain sslmode parameter in %s", ErrTLSRequired, cfg.Environment)
		}

		// Ensure sslmode is not 'disable' in production/staging
		if strings.Contains(cfg.DSN, "sslmode=disable") {
			return fmt.Errorf("%w: sslmode=disable is forbidden in %s", ErrTLSRequired, cfg.Environment)
		}
	}

	return nil
}

// openConnectionWithTimeout opens database connection with timeout
func openConnectionWithTimeout(ctx context.Context, cfg *ConnectionConfig) (*sql.DB, error) {
	// Create context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.ConnTimeout)
	defer cancel()

	// Open connection using pgx driver
	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		// SECURITY: Never log the DSN
		return nil, fmt.Errorf("%w: %s", ErrConnectionFailed, "failed to open database")
	}

	// Ping to verify connection
	if err := db.PingContext(timeoutCtx); err != nil {
		db.Close()
		// SECURITY: Never log the DSN or detailed connection error that might expose credentials
		return nil, fmt.Errorf("%w: %s", ErrConnectionFailed, "failed to ping database")
	}

	return db, nil
}

// configurePool configures connection pool settings
func configurePool(db *sql.DB, cfg *ConnectionConfig) {
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.MaxLifetime)

	// Set max idle time to prevent stale connections
	db.SetConnMaxIdleTime(5 * time.Minute)
}

// verifyConnection verifies the database connection is healthy
func verifyConnection(ctx context.Context, db *sql.DB, cfg *ConnectionConfig) error {
	verifyCtx, cancel := context.WithTimeout(ctx, cfg.ConnTimeout)
	defer cancel()

	// Execute a simple query to verify connection
	var result int
	err := db.QueryRowContext(verifyCtx, "SELECT 1").Scan(&result)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.ErrorContext(ctx, "database connection verification failed",
				utils.ZapError(err))
		}
		return fmt.Errorf("%w: verification query failed", ErrConnectionFailed)
	}

	if result != 1 {
		return fmt.Errorf("%w: unexpected verification result", ErrConnectionFailed)
	}

	return nil
}

// auditConnectionAttempt logs connection attempt (without secrets)
func auditConnectionAttempt(cfg *ConnectionConfig) {
	if cfg.AuditLogger != nil {
		_ = cfg.AuditLogger.Info("db_connection_attempt", map[string]interface{}{
			"environment":    string(cfg.Environment),
			"tls_enabled":    cfg.TLSEnabled,
			"max_open_conns": cfg.MaxOpenConns,
			"max_idle_conns": cfg.MaxIdleConns,
			"conn_timeout":   cfg.ConnTimeout.String(),
			"dsn_provided":   cfg.DSN != "", // Only log presence, not value
		})
	}
}

// RedactDSN redacts sensitive information from a DSN for safe logging
// Returns the DSN with credentials replaced by "***REDACTED***"
func RedactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}

	// Parse the DSN
	u, err := url.Parse(dsn)
	if err != nil {
		return "***INVALID_DSN***"
	}

	// Redact password if present
	if u.User != nil {
		username := u.User.Username()
		u.User = url.UserPassword(username, "***REDACTED***")
	}

	return u.String()
}

// GetConnectionStats returns current connection pool statistics
func GetConnectionStats(db *sql.DB) map[string]interface{} {
	stats := db.Stats()
	return map[string]interface{}{
		"max_open_connections": stats.MaxOpenConnections,
		"open_connections":     stats.OpenConnections,
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration_ms":     stats.WaitDuration.Milliseconds(),
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_idle_time_closed": stats.MaxIdleTimeClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}
}
