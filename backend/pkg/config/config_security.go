package config

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/pkg/utils"
)

// Security validation constants
var (
	JWTSecretMinLength = 32
	ValidEnvironments  = []string{"development", "staging", "production"}
	ValidAuthMethods   = []string{"jwt", "oauth2", "mtls"}
)

// Security-specific error codes
const (
	ErrCodeInvalidTLSConfig   = utils.ErrorCode("INVALID_TLS_CONFIG")
	ErrCodeCertificateExpired = utils.ErrorCode("CERTIFICATE_EXPIRED")
	ErrCodeInvalidAuthMethod  = utils.ErrorCode("INVALID_AUTH_METHOD")
	ErrCodeMissingSecurityVar = utils.ErrorCode("MISSING_SECURITY_VAR")
	ErrCodeInsecureURL        = utils.ErrorCode("INSECURE_URL")
)

// SecurityConfig - integrated with utils security infrastructure
type SecurityConfig struct {
	// Security infrastructure (actual working components)
	CryptoService *utils.CryptoService `json:"-"`
	IPAllowlist   *utils.IPAllowlist   `json:"-"`
	HTTPClient    *utils.HTTPClient    `json:"-"`
	AuditLogger   *utils.AuditLogger   `json:"-"`

	// TLS configuration
	TLSConfig     *tls.Config `json:"-"`
	TLSCertPath   string      `json:"tls_cert_path"`
	TLSKeyPath    string      `json:"tls_key_path"`
	TLSMinVersion uint16      `json:"-"`

	// Key management (store version, not keys)
	KeyVersion uint32 `json:"key_version"`

	// Topology integration
	TrustedNodes []int    `json:"trusted_nodes"`
	AllowedCIDRs []string `json:"allowed_cidrs"`

	// Configuration
	P2PEncryptionEnabled bool              `json:"p2p_encryption_enabled"`
	AuditLogPath         string            `json:"audit_log_path"`
	AuthMethod           string            `json:"auth_method"`
	SecurityHeaders      map[string]string `json:"security_headers"`

	// JWT configuration
	JWTIssuer   string `json:"jwt_issuer"`
	JWTAudience string `json:"jwt_audience"`
}

// SecurityConfigBuilder builds security configuration with full utils integration
type SecurityConfigBuilder struct {
	configMgr   *utils.ConfigManager
	cryptoSvc   *utils.CryptoService
	allowlist   *utils.IPAllowlist
	auditLogger *utils.AuditLogger
	httpClient  *utils.HTTPClient
	logger      *utils.Logger
	err         error
}

// NewSecurityConfigBuilder creates a new security config builder
func NewSecurityConfigBuilder(configMgr *utils.ConfigManager) *SecurityConfigBuilder {
	return &SecurityConfigBuilder{
		configMgr: configMgr,
		logger:    utils.GetLogger(),
	}
}

// WithCryptoService sets a custom crypto service (optional)
func (b *SecurityConfigBuilder) WithCryptoService(cryptoSvc *utils.CryptoService) *SecurityConfigBuilder {
	if b.err == nil {
		b.cryptoSvc = cryptoSvc
	}
	return b
}

// Build creates the security configuration
func (b *SecurityConfigBuilder) Build(ctx context.Context) (*SecurityConfig, error) {
	if b.err != nil {
		return nil, b.err
	}

	b.logger.InfoContext(ctx, "Building enhanced security configuration")

	// Validate core security requirements
	if err := b.validateCoreSecurityVars(); err != nil {
		return nil, err
	}

	// Setup crypto service
	if err := b.setupCryptoService(ctx); err != nil {
		return nil, err
	}

	// Setup IP allowlist
	if err := b.setupIPAllowlist(ctx); err != nil {
		return nil, err
	}

	// Load and validate TLS configuration
	tlsConfig, tlsCertPath, tlsKeyPath, err := b.loadTLSConfiguration(ctx)
	if err != nil {
		return nil, err
	}

	// Setup HTTP client with TLS
	if err := b.setupHTTPClient(ctx, tlsConfig); err != nil {
		return nil, err
	}

	// Setup audit logger
	if err := b.setupAuditLogger(ctx); err != nil {
		return nil, err
	}

	// Validate JWT configuration
	jwtIssuer, jwtAudience, err := b.validateJWTConfig()
	if err != nil {
		return nil, err
	}

	// Validate auth method
	authMethod, err := b.validateAuthMethod()
	if err != nil {
		return nil, err
	}

	// Validate and parse trusted nodes
	trustedNodes, err := b.validateTrustedNodes()
	if err != nil {
		return nil, err
	}

	// Get allowed CIDRs
	allowedCIDRs := b.configMgr.GetStringSlice("ALLOWED_IPS", []string{})

	// Validate service URLs
	if err := b.validateServiceURLs(); err != nil {
		return nil, err
	}

	// Get audit log path
	auditLogPath := b.configMgr.GetString("AUDIT_LOG_PATH", "/var/log/security/audit.log")

	// Get key version
	keyVersion, err := b.cryptoSvc.GetKeyVersion()
	if err != nil {
		return nil, utils.WrapError(err, utils.CodeInternal, "failed to get crypto key version")
	}

	config := &SecurityConfig{
		CryptoService:        b.cryptoSvc,
		IPAllowlist:          b.allowlist,
		HTTPClient:           b.httpClient,
		AuditLogger:          b.auditLogger,
		TLSConfig:            tlsConfig,
		TLSCertPath:          tlsCertPath,
		TLSKeyPath:           tlsKeyPath,
		TLSMinVersion:        tlsConfig.MinVersion,
		KeyVersion:           keyVersion,
		TrustedNodes:         trustedNodes,
		AllowedCIDRs:         allowedCIDRs,
		P2PEncryptionEnabled: true,
		AuditLogPath:         auditLogPath,
		AuthMethod:           authMethod,
		JWTIssuer:            jwtIssuer,
		JWTAudience:          jwtAudience,
		SecurityHeaders:      buildSecurityHeaders(),
	}

	b.logger.InfoContext(ctx, "Security configuration built successfully",
		utils.ZapString("auth_method", config.AuthMethod),
		utils.ZapInt("trusted_nodes", len(config.TrustedNodes)),
		utils.ZapInt("allowed_cidrs", len(config.AllowedCIDRs)),
		utils.ZapUint32("key_version", config.KeyVersion))

	return config, nil
}

// Private builder methods

func (b *SecurityConfigBuilder) validateCoreSecurityVars() error {
	required := []string{
		"TLS_CERT_PATH",
		"TLS_KEY_PATH",
		"JWT_SECRET",
		"JWT_ISSUER",
		"JWT_AUDIENCE",
		"TRUSTED_NODES",
		"ALLOWED_IPS",
	}

	missing := make([]string, 0)
	for _, envVar := range required {
		if b.configMgr.GetString(envVar, "") == "" {
			missing = append(missing, envVar)
		}
	}

	if len(missing) > 0 {
		return utils.NewError(ErrCodeMissingSecurityVar,
			fmt.Sprintf("missing required security variables: %v", missing)).
			WithDetail("missing_vars", missing)
	}

	return nil
}

func (b *SecurityConfigBuilder) setupCryptoService(ctx context.Context) error {
	if b.cryptoSvc != nil {
		return nil // Already provided
	}

	// Create crypto service with secure defaults
	cryptoConfig := utils.DefaultCryptoConfig()
	cryptoConfig.EnableReplayProtection = true
	cryptoConfig.EnableAuditLog = true
	cryptoConfig.KeyTTL = 24 * time.Hour
	cryptoConfig.AutoRotate = false // Manual rotation for security-critical keys

	cryptoSvc, err := utils.NewCryptoService(cryptoConfig)
	if err != nil {
		return utils.WrapError(err, utils.CodeInternal,
			"failed to create crypto service")
	}

	b.cryptoSvc = cryptoSvc
	return nil
}

func (b *SecurityConfigBuilder) setupIPAllowlist(ctx context.Context) error {
	allowlistConfig := utils.DefaultIPAllowlistConfig()

	// Configure based on environment
	environment := b.configMgr.GetString("ENVIRONMENT", "production")
	if environment == "development" {
		allowlistConfig.AllowPrivate = true
		allowlistConfig.AllowLoopback = true
	}

	// Parse allowed CIDRs from config
	allowedIPs := b.configMgr.GetStringSlice("ALLOWED_IPS", []string{})
	if len(allowedIPs) > 0 {
		allowlistConfig.AllowedCIDRs = allowedIPs
	}

	allowlistConfig.Logger = b.logger
	allowlistConfig.DisableDNS = true // Prevent DNS rebinding attacks
	allowlistConfig.ValidateDNSOnUse = true

	allowlist, err := utils.NewIPAllowlist(allowlistConfig)
	if err != nil {
		return utils.WrapError(err, utils.CodeConfigInvalid,
			"failed to create IP allowlist")
	}

	b.allowlist = allowlist

	// Validate that all provided IPs/CIDRs are valid
	for _, ipStr := range allowedIPs {
		if ipStr == "" {
			continue
		}
		// This validates the IP/CIDR format
		if err := allowlist.IsAddrAllowed(ipStr); err != nil {
			return utils.WrapError(err, utils.CodeConfigInvalid,
				fmt.Sprintf("invalid IP/CIDR in ALLOWED_IPS: %s", ipStr))
		}
	}

	return nil
}

func (b *SecurityConfigBuilder) loadTLSConfiguration(ctx context.Context) (*tls.Config, string, string, error) {
	certPath, err := b.configMgr.GetStringRequired("TLS_CERT_PATH")
	if err != nil {
		return nil, "", "", utils.WrapError(err, ErrCodeInvalidTLSConfig,
			"TLS_CERT_PATH is required")
	}

	keyPath, err := b.configMgr.GetStringRequired("TLS_KEY_PATH")
	if err != nil {
		return nil, "", "", utils.WrapError(err, ErrCodeInvalidTLSConfig,
			"TLS_KEY_PATH is required")
	}

	// Validate certificate exists and is valid
	if err := b.validateTLSCertificate(certPath); err != nil {
		return nil, "", "", err
	}

	// Validate key exists
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return nil, "", "", utils.NewError(ErrCodeInvalidTLSConfig,
			fmt.Sprintf("TLS key file does not exist: %s", keyPath))
	}

	// Load certificate and key
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, "", "", utils.WrapError(err, ErrCodeInvalidTLSConfig,
			"failed to load TLS certificate and key")
	}

	// Create TLS config with secure defaults
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		PreferServerCipherSuites: true,
	}

	return tlsConfig, certPath, keyPath, nil
}

func (b *SecurityConfigBuilder) validateTLSCertificate(certPath string) error {
	// Check file exists
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return utils.WrapError(err, ErrCodeInvalidTLSConfig,
			"failed to read TLS certificate")
	}

	// Parse PEM
	block, _ := pem.Decode(certData)
	if block == nil {
		return utils.NewError(ErrCodeInvalidTLSConfig,
			"certificate is not in valid PEM format")
	}

	// Parse certificate
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return utils.WrapError(err, ErrCodeInvalidTLSConfig,
			"failed to parse certificate")
	}

	// Check expiration
	now := time.Now()
	if cert.NotAfter.Before(now) {
		return utils.NewError(ErrCodeCertificateExpired,
			fmt.Sprintf("certificate expired on %v", cert.NotAfter)).
			WithDetail("not_after", cert.NotAfter).
			WithDetail("current_time", now)
	}

	// Warn if expiring soon (within 30 days)
	if cert.NotAfter.Before(now.Add(30 * 24 * time.Hour)) {
		b.logger.Warn("TLS certificate expiring soon",
			utils.ZapTime("expires_at", cert.NotAfter),
			utils.ZapDuration("time_remaining", time.Until(cert.NotAfter)))
	}

	return nil
}

func (b *SecurityConfigBuilder) setupHTTPClient(ctx context.Context, tlsConfig *tls.Config) error {
	httpClient, err := utils.NewHTTPClientBuilder().
		WithTimeout(30*time.Second).
		WithTLSConfig(tls.VersionTLS13, tls.VersionTLS13).
		WithRetry(3, 1*time.Second, 10*time.Second).
		Build()
	if err != nil {
		return utils.WrapError(err, utils.CodeInternal,
			"failed to create HTTP client")
	}

	b.httpClient = httpClient
	return nil
}

func (b *SecurityConfigBuilder) setupAuditLogger(ctx context.Context) error {
	auditLogPath := b.configMgr.GetString("AUDIT_LOG_PATH", "/var/log/security/audit.log")

	// Validate audit log directory
	dir := filepath.Dir(auditLogPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return utils.WrapError(err, utils.CodeConfigInvalid,
			"failed to create audit log directory")
	}

	// Test write permissions
	testFile := filepath.Join(dir, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0640); err != nil {
		return utils.WrapError(err, utils.CodeConfigInvalid,
			"cannot write to audit log directory")
	}
	os.Remove(testFile)

	// Create audit logger
	auditConfig := utils.DefaultAuditConfig()
	auditConfig.FilePath = auditLogPath
	auditConfig.Component = "security"
	auditConfig.EnableSigning = true
	auditConfig.EnableRotation = true
	auditConfig.MaxSize = 100 // MB
	auditConfig.MaxBackups = 30
	auditConfig.MaxAge = 90 // days

	// Get signing key from crypto service
	pubKey, err := b.cryptoSvc.GetPublicKey()
	if err != nil {
		return utils.WrapError(err, utils.CodeInternal,
			"failed to get public key for audit signing")
	}
	auditConfig.SigningKey = pubKey[:32]

	auditLogger, err := utils.NewAuditLogger(auditConfig)
	if err != nil {
		return utils.WrapError(err, utils.CodeInternal,
			"failed to create audit logger")
	}

	b.auditLogger = auditLogger
	return nil
}

func (b *SecurityConfigBuilder) validateJWTConfig() (string, string, error) {
	jwtSecret := b.configMgr.GetString("JWT_SECRET", "")
	if len(jwtSecret) < JWTSecretMinLength {
		return "", "", utils.NewValidationError(
			fmt.Sprintf("JWT_SECRET must be at least %d characters", JWTSecretMinLength))
	}

	jwtIssuer, err := b.configMgr.GetStringRequired("JWT_ISSUER")
	if err != nil {
		return "", "", utils.WrapError(err, utils.CodeConfigInvalid,
			"JWT_ISSUER is required")
	}

	jwtAudience, err := b.configMgr.GetStringRequired("JWT_AUDIENCE")
	if err != nil {
		return "", "", utils.WrapError(err, utils.CodeConfigInvalid,
			"JWT_AUDIENCE is required")
	}

	return jwtIssuer, jwtAudience, nil
}

func (b *SecurityConfigBuilder) validateAuthMethod() (string, error) {
	authMethod := b.configMgr.GetString("AUTH_METHOD", "jwt")

	valid := false
	for _, method := range ValidAuthMethods {
		if authMethod == method {
			valid = true
			break
		}
	}

	if !valid {
		return "", utils.NewError(ErrCodeInvalidAuthMethod,
			fmt.Sprintf("invalid auth method '%s'", authMethod)).
			WithDetail("valid_methods", ValidAuthMethods)
	}

	return authMethod, nil
}

func (b *SecurityConfigBuilder) validateTrustedNodes() ([]int, error) {
	trustedNodesStr, err := b.configMgr.GetStringRequired("TRUSTED_NODES")
	if err != nil {
		return nil, utils.WrapError(err, utils.CodeConfigInvalid,
			"TRUSTED_NODES is required")
	}

	trustedNodes, err := parseIntList(trustedNodesStr, 1, ClusterSize)
	if err != nil {
		return nil, utils.WrapError(err, utils.CodeInvalidInput,
			"invalid TRUSTED_NODES format")
	}

	// Validate nodes exist in topology
	for _, nodeID := range trustedNodes {
		if _, exists := NodeRoles[nodeID]; !exists {
			return nil, utils.NewError(utils.CodeConfigInvalid,
				fmt.Sprintf("trusted node %d not found in cluster topology", nodeID)).
				WithDetail("node_id", nodeID).
				WithDetail("cluster_size", ClusterSize)
		}
	}

	return trustedNodes, nil
}

func (b *SecurityConfigBuilder) validateServiceURLs() error {
	environment := b.configMgr.GetString("ENVIRONMENT", "production")
	requireHTTPS := environment == "production" || environment == "staging"

	urls := map[string]string{
		"AI_API_URL":  b.configMgr.GetString("AI_API_URL", ""),
		"BACKEND_URL": b.configMgr.GetString("BACKEND_URL", ""),
		"WEBHOOK_URL": b.configMgr.GetString("WEBHOOK_URL", ""),
	}

	for key, urlStr := range urls {
		if urlStr == "" {
			continue
		}

		if requireHTTPS && !strings.HasPrefix(urlStr, "https://") {
			return utils.NewError(ErrCodeInsecureURL,
				fmt.Sprintf("%s must use HTTPS in %s environment", key, environment)).
				WithDetail("url", urlStr).
				WithDetail("environment", environment)
		}
	}

	return nil
}

// Public API

// BuildSecurityConfig creates a security configuration
func BuildSecurityConfig(ctx context.Context, configMgr *utils.ConfigManager) (*SecurityConfig, error) {
	builder := NewSecurityConfigBuilder(configMgr)
	return builder.Build(ctx)
}

// BuildSecurityConfigWithCrypto creates config with custom crypto service
func BuildSecurityConfigWithCrypto(ctx context.Context, configMgr *utils.ConfigManager, cryptoSvc *utils.CryptoService) (*SecurityConfig, error) {
	builder := NewSecurityConfigBuilder(configMgr).
		WithCryptoService(cryptoSvc)
	return builder.Build(ctx)
}

// Security config methods

// EncryptData encrypts data using the crypto service
func (c *SecurityConfig) EncryptData(ctx context.Context, plaintext []byte) ([]byte, error) {
	if c.CryptoService == nil {
		return nil, utils.NewError(utils.CodeInternal, "crypto service not configured")
	}
	return c.CryptoService.EncryptWithContext(ctx, plaintext)
}

// DecryptData decrypts data using the crypto service
func (c *SecurityConfig) DecryptData(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if c.CryptoService == nil {
		return nil, utils.NewError(utils.CodeInternal, "crypto service not configured")
	}
	return c.CryptoService.DecryptWithContext(ctx, ciphertext)
}

// SignData signs data using the crypto service
func (c *SecurityConfig) SignData(ctx context.Context, data []byte) ([]byte, error) {
	if c.CryptoService == nil {
		return nil, utils.NewError(utils.CodeInternal, "crypto service not configured")
	}
	return c.CryptoService.SignWithContext(ctx, data)
}

// VerifySignature verifies a signature using the crypto service
func (c *SecurityConfig) VerifySignature(ctx context.Context, data, signature []byte, pubKey []byte) error {
	if c.CryptoService == nil {
		return utils.NewError(utils.CodeInternal, "crypto service not configured")
	}
	return c.CryptoService.VerifyWithContext(ctx, data, signature, pubKey)
}

// ValidateAddress validates an address against the IP allowlist
func (c *SecurityConfig) ValidateAddress(ctx context.Context, address string) error {
	if c.IPAllowlist == nil {
		return nil // Allowlist not configured
	}
	return c.IPAllowlist.ValidateBeforeConnect(ctx, address)
}

// AuditSecurityEvent logs a security event
func (c *SecurityConfig) AuditSecurityEvent(ctx context.Context, event string, fields map[string]interface{}) error {
	if c.AuditLogger == nil {
		return nil // Audit logging not enabled
	}
	return c.AuditLogger.Security(event, fields)
}

// Close closes all resources
func (c *SecurityConfig) Close() error {
	if c.HTTPClient != nil {
		c.HTTPClient.Close()
	}
	if c.AuditLogger != nil {
		c.AuditLogger.Close()
	}
	if c.CryptoService != nil {
		c.CryptoService.Shutdown()
	}
	return nil
}

// Helper functions

func buildSecurityHeaders() map[string]string {
	return map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains; preload",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"X-XSS-Protection":          "1; mode=block",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Content-Security-Policy":   "default-src 'self'; script-src 'self'",
		"Permissions-Policy":        "geolocation=(), microphone=(), camera=()",
	}
}

// ValidateSystemSecurity validates security across all subsystems
func ValidateSystemSecurity(ctx context.Context, config *Config) error {
	logger := utils.GetLogger()
	logger.InfoContext(ctx, "Validating system-wide security configuration")

	// Validate topology security
	if config.Topology != nil {
		if config.Topology.ClusterSize < 4 {
			return utils.NewValidationError(
				"cluster size must be at least 4 for Byzantine fault tolerance")
		}

		minQuorum := (config.Topology.ClusterSize / 2) + 1
		if config.Topology.QuorumSize < minQuorum {
			return utils.NewValidationError(
				fmt.Sprintf("quorum size %d insufficient for cluster size %d (min: %d)",
					config.Topology.QuorumSize, config.Topology.ClusterSize, minQuorum))
		}
	}

	logger.InfoContext(ctx, "System-wide security validation completed successfully")
	return nil
}
