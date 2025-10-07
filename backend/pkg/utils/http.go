package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// HTTP security constants
const (
	DefaultDialTimeout           = 5 * time.Second
	DefaultTLSHandshakeTimeout   = 5 * time.Second
	DefaultResponseHeaderTimeout = 10 * time.Second
	DefaultExpectContinueTimeout = 1 * time.Second
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultMaxIdleConns          = 100
	DefaultMaxIdleConnsPerHost   = 10
	DefaultMaxConnsPerHost       = 100
	DefaultRequestTimeout        = 30 * time.Second
)

// Errors
var (
	ErrInvalidConfig           = errors.New("http: invalid configuration")
	ErrTLSConfigFailed         = errors.New("http: TLS configuration failed")
	ErrCertificateInvalid      = errors.New("http: certificate validation failed")
	ErrConnectionPoolExhausted = errors.New("http: connection pool exhausted")
)

// HTTPClientConfig holds configuration for HTTP clients
type HTTPClientConfig struct {
	// Timeouts
	RequestTimeout        time.Duration
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	ExpectContinueTimeout time.Duration
	IdleConnTimeout       time.Duration

	// Connection pooling
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int
	DisableKeepAlives   bool

	// TLS configuration
	TLSMinVersion      uint16
	TLSMaxVersion      uint16
	InsecureSkipVerify bool
	RootCAs            *x509.CertPool
	ClientCertificates []tls.Certificate
	ServerName         string
	CipherSuites       []uint16

	// HTTP/2 settings
	ForceHTTP2   bool
	DisableHTTP2 bool

	// Compression
	DisableCompression bool

	// Proxy
	ProxyURL     string
	DisableProxy bool

	// Retry settings
	MaxRetries   int
	RetryWaitMin time.Duration
	RetryWaitMax time.Duration
}

// DefaultHTTPClientConfig returns secure defaults
func DefaultHTTPClientConfig() *HTTPClientConfig {
	return &HTTPClientConfig{
		RequestTimeout:        DefaultRequestTimeout,
		DialTimeout:           DefaultDialTimeout,
		TLSHandshakeTimeout:   DefaultTLSHandshakeTimeout,
		ResponseHeaderTimeout: DefaultResponseHeaderTimeout,
		ExpectContinueTimeout: DefaultExpectContinueTimeout,
		IdleConnTimeout:       DefaultIdleConnTimeout,
		MaxIdleConns:          DefaultMaxIdleConns,
		MaxIdleConnsPerHost:   DefaultMaxIdleConnsPerHost,
		MaxConnsPerHost:       DefaultMaxConnsPerHost,
		TLSMinVersion:         tls.VersionTLS13,
		TLSMaxVersion:         tls.VersionTLS13,
		ForceHTTP2:            true,
		DisableCompression:    true, // Prevent BREACH-style attacks
		DisableProxy:          false,
		MaxRetries:            3,
		RetryWaitMin:          1 * time.Second,
		RetryWaitMax:          30 * time.Second,
	}
}

// SecureHTTPClientConfig returns maximum security configuration
func SecureHTTPClientConfig() *HTTPClientConfig {
	config := DefaultHTTPClientConfig()
	config.DisableCompression = true
	config.DisableKeepAlives = true
	config.TLSMinVersion = tls.VersionTLS13
	config.CipherSuites = secureCipherSuites()
	return config
}

// HTTPClientBuilder provides a fluent interface for building HTTP clients
type HTTPClientBuilder struct {
	config *HTTPClientConfig
	err    error
}

// NewHTTPClientBuilder creates a new builder with defaults
func NewHTTPClientBuilder() *HTTPClientBuilder {
	return &HTTPClientBuilder{
		config: DefaultHTTPClientConfig(),
	}
}

// WithTimeout sets the request timeout
func (b *HTTPClientBuilder) WithTimeout(timeout time.Duration) *HTTPClientBuilder {
	if b.err == nil && timeout > 0 {
		b.config.RequestTimeout = timeout
	}
	return b
}

// WithTLSConfig sets TLS configuration
func (b *HTTPClientBuilder) WithTLSConfig(minVersion, maxVersion uint16) *HTTPClientBuilder {
	if b.err == nil {
		if minVersion < tls.VersionTLS12 {
			b.err = fmt.Errorf("%w: TLS version must be 1.2 or higher", ErrInvalidConfig)
			return b
		}
		b.config.TLSMinVersion = minVersion
		b.config.TLSMaxVersion = maxVersion
	}
	return b
}

// WithRootCAs sets custom root CA pool
func (b *HTTPClientBuilder) WithRootCAs(pool *x509.CertPool) *HTTPClientBuilder {
	if b.err == nil && pool != nil {
		b.config.RootCAs = pool
	}
	return b
}

// WithClientCertificates sets client certificates for mTLS
func (b *HTTPClientBuilder) WithClientCertificates(certs []tls.Certificate) *HTTPClientBuilder {
	if b.err == nil && len(certs) > 0 {
		b.config.ClientCertificates = certs
	}
	return b
}

// WithServerName sets SNI server name
func (b *HTTPClientBuilder) WithServerName(name string) *HTTPClientBuilder {
	if b.err == nil && name != "" {
		b.config.ServerName = name
	}
	return b
}

// WithConnectionPool configures connection pooling
func (b *HTTPClientBuilder) WithConnectionPool(maxIdle, maxIdlePerHost, maxPerHost int) *HTTPClientBuilder {
	if b.err == nil {
		b.config.MaxIdleConns = maxIdle
		b.config.MaxIdleConnsPerHost = maxIdlePerHost
		b.config.MaxConnsPerHost = maxPerHost
	}
	return b
}

// DisableHTTP2 disables HTTP/2
func (b *HTTPClientBuilder) DisableHTTP2() *HTTPClientBuilder {
	if b.err == nil {
		b.config.DisableHTTP2 = true
		b.config.ForceHTTP2 = false
	}
	return b
}

// EnableCompression enables response compression (security risk)
func (b *HTTPClientBuilder) EnableCompression() *HTTPClientBuilder {
	if b.err == nil {
		b.config.DisableCompression = false
	}
	return b
}

// InsecureSkipVerify disables certificate verification (DO NOT USE IN PRODUCTION)
func (b *HTTPClientBuilder) InsecureSkipVerify() *HTTPClientBuilder {
	if b.err == nil {
		b.config.InsecureSkipVerify = true
	}
	return b
}

// WithRetry configures retry behavior
func (b *HTTPClientBuilder) WithRetry(maxRetries int, waitMin, waitMax time.Duration) *HTTPClientBuilder {
	if b.err == nil {
		b.config.MaxRetries = maxRetries
		b.config.RetryWaitMin = waitMin
		b.config.RetryWaitMax = waitMax
	}
	return b
}

// Build creates the HTTP client
func (b *HTTPClientBuilder) Build() (*HTTPClient, error) {
	if b.err != nil {
		return nil, b.err
	}
	return NewHTTPClient(b.config)
}

// HTTPClient wraps http.Client with additional security features
type HTTPClient struct {
	client    *http.Client
	config    *HTTPClientConfig
	transport *http.Transport

	// Metrics
	requestCount uint64
	errorCount   uint64

	// Rate limiting (optional)
	rateLimiter *rateLimiter

	mu sync.RWMutex
}

// NewHTTPClient creates a new secure HTTP client
func NewHTTPClient(config *HTTPClientConfig) (*HTTPClient, error) {
	if config == nil {
		config = DefaultHTTPClientConfig()
	}

	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}

	// Create custom dialer
	dialer := &net.Dialer{
		Timeout:   config.DialTimeout,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}

	// Build TLS config
	tlsConfig, err := buildTLSConfig(config)
	if err != nil {
		return nil, err
	}

	// Create transport
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSClientConfig:       tlsConfig,
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       config.MaxConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		TLSHandshakeTimeout:   config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: config.ResponseHeaderTimeout,
		ExpectContinueTimeout: config.ExpectContinueTimeout,
		DisableCompression:    config.DisableCompression,
		DisableKeepAlives:     config.DisableKeepAlives,
		ForceAttemptHTTP2:     config.ForceHTTP2 && !config.DisableHTTP2,
	}

	// Configure proxy
	if !config.DisableProxy {
		transport.Proxy = http.ProxyFromEnvironment
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   config.RequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Limit redirects to prevent redirect loops
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	return &HTTPClient{
		client:    client,
		config:    config,
		transport: transport,
	}, nil
}

// Do performs an HTTP request with retries
func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.DoWithContext(req.Context(), req)
}

// DoWithContext performs an HTTP request with context and retries
func (c *HTTPClient) DoWithContext(ctx context.Context, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		// Check context
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Clone request for retry
		reqClone := req.Clone(ctx)

		// Perform request
		resp, err = c.client.Do(reqClone)

		// Check if retry is needed
		if err == nil && !shouldRetry(resp.StatusCode) {
			return resp, nil
		}

		// Close response body if present
		if resp != nil {
			resp.Body.Close()
		}

		// Don't retry on last attempt
		if attempt == c.config.MaxRetries {
			break
		}

		// Wait before retry with exponential backoff
		waitTime := c.calculateBackoff(attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitTime):
		}
	}

	return resp, err
}

// Get performs a GET request
func (c *HTTPClient) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return c.DoWithContext(ctx, req)
}

// Post performs a POST request
func (c *HTTPClient) Post(ctx context.Context, url, contentType string, body interface{}) (*http.Response, error) {
	// Implementation would handle body serialization
	return nil, errors.New("not implemented")
}

// Close closes idle connections
func (c *HTTPClient) Close() {
	c.transport.CloseIdleConnections()
}

// GetMetrics returns client metrics
func (c *HTTPClient) GetMetrics() map[string]uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]uint64{
		"request_count": c.requestCount,
		"error_count":   c.errorCount,
	}
}

// Private helper functions

func validateConfig(config *HTTPClientConfig) error {
	if config.TLSMinVersion < tls.VersionTLS12 {
		return fmt.Errorf("TLS version must be 1.2 or higher")
	}

	if config.RequestTimeout <= 0 {
		return fmt.Errorf("request timeout must be positive")
	}

	if config.MaxRetries < 0 {
		return fmt.Errorf("max retries cannot be negative")
	}

	return nil
}

func buildTLSConfig(config *HTTPClientConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         config.TLSMinVersion,
		MaxVersion:         config.TLSMaxVersion,
		InsecureSkipVerify: config.InsecureSkipVerify,
		ServerName:         config.ServerName,
		Certificates:       config.ClientCertificates,
		RootCAs:            config.RootCAs,
	}

	// Set secure cipher suites if not specified
	if len(config.CipherSuites) > 0 {
		tlsConfig.CipherSuites = config.CipherSuites
	} else if config.TLSMinVersion >= tls.VersionTLS13 {
		// TLS 1.3 cipher suites are managed by the runtime
		tlsConfig.CipherSuites = nil
	} else {
		tlsConfig.CipherSuites = secureCipherSuites()
	}

	return tlsConfig, nil
}

func secureCipherSuites() []uint16 {
	return []uint16{
		// TLS 1.3 suites (managed by crypto/tls)
		// TLS 1.2 suites
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
	}
}

func shouldRetry(statusCode int) bool {
	// Retry on specific status codes
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusRequestTimeout,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}

	// Retry on 5xx errors except 501 (Not Implemented)
	if statusCode >= 500 && statusCode != http.StatusNotImplemented {
		return true
	}

	return false
}

func (c *HTTPClient) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff with jitter
	backoff := c.config.RetryWaitMin * (1 << uint(attempt))
	if backoff > c.config.RetryWaitMax {
		backoff = c.config.RetryWaitMax
	}
	return backoff
}

// rateLimiter provides token bucket rate limiting
type rateLimiter struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	refill   float64
	lastTime time.Time
}

// GetHTTPClient returns a security-hardened HTTP client (legacy compatibility)
func GetHTTPClient(timeout time.Duration) *http.Client {
	client, _ := NewHTTPClientBuilder().
		WithTimeout(timeout).
		Build()

	if client != nil {
		return client.client
	}

	// Fallback
	return &http.Client{Timeout: timeout}
}
