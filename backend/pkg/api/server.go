package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/config"
	consapi "backend/pkg/consensus/api"
	"backend/pkg/mempool"
	"backend/pkg/p2p"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
)

// Server provides read-only API access to backend state
type Server struct {
	config     *config.APIConfig
	logger     *utils.Logger
	audit      *utils.AuditLogger
	httpServer *http.Server

	// Backend components
	storage    cockroach.Adapter
	stateStore state.StateStore
	mempool    *mempool.Mempool
	engine     *consapi.ConsensusEngine
	p2pRouter  *p2p.Router

	// Middleware components
	rateLimiter *RateLimiter
	ipAllowlist *utils.IPAllowlist
	sem         chan struct{}

	// State
	running   atomic.Bool
	closeOnce sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// Dependencies holds server dependencies
type Dependencies struct {
	Config      *config.APIConfig
	Logger      *utils.Logger
	AuditLogger *utils.AuditLogger
	Storage     cockroach.Adapter
	StateStore  state.StateStore
	Mempool     *mempool.Mempool
	Engine      *consapi.ConsensusEngine
	P2PRouter   *p2p.Router
}

// NewServer creates a new API server
func NewServer(deps Dependencies) (*Server, error) {
	if deps.Config == nil {
		return nil, errors.New("config is required")
	}
	if deps.Logger == nil {
		return nil, errors.New("logger is required")
	}
	if deps.Storage == nil {
		return nil, errors.New("storage is required")
	}
	if deps.StateStore == nil {
		return nil, errors.New("state store is required")
	}

	// Validate configuration
	if err := deps.Config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Ensure RBAC is only enabled when mTLS client authentication is configured
	if deps.Config.RBACEnabled && deps.Config.TLSClientCAFile == "" {
		deps.Logger.Warn("RBAC requires mutual TLS client CA; disabling RBAC until certificates are provisioned")
		deps.Config.RBACEnabled = false
	}

	s := &Server{
		config:     deps.Config,
		logger:     deps.Logger,
		audit:      deps.AuditLogger,
		storage:    deps.Storage,
		stateStore: deps.StateStore,
		mempool:    deps.Mempool,
		engine:     deps.Engine,
		p2pRouter:  deps.P2PRouter,
		stopCh:     make(chan struct{}),
	}

	// Initialize rate limiter if enabled
	if deps.Config.RateLimitEnabled {
		s.rateLimiter = NewRateLimiter(RateLimiterConfig{
			RequestsPerMinute: deps.Config.RateLimitPerMinute,
			Burst:             deps.Config.RateLimitBurst,
			Logger:            deps.Logger,
		})
	}

	// Initialize IP allowlist if configured
	if len(deps.Config.IPAllowlist) > 0 {
		allowlistCfg := utils.DefaultIPAllowlistConfig()
		allowlistCfg.AllowedCIDRs = deps.Config.IPAllowlist
		allowlistCfg.Logger = deps.Logger

		var err error
		s.ipAllowlist, err = utils.NewIPAllowlist(allowlistCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create IP allowlist: %w", err)
		}

		if deps.Logger != nil {
			deps.Logger.Info("IP allowlist enabled",
				utils.ZapInt("cidr_count", len(deps.Config.IPAllowlist)),
				utils.ZapBool("dns_disabled", allowlistCfg.DisableDNS))
		}

		// Initialize concurrency limiter
		if deps.Config.MaxConcurrentReqs > 0 {
			s.sem = make(chan struct{}, deps.Config.MaxConcurrentReqs)
		}
	}

	// Create HTTP server
	if err := s.createHTTPServer(); err != nil {
		return nil, fmt.Errorf("failed to create HTTP server: %w", err)
	}

	return s, nil
}

// createHTTPServer creates and configures the HTTP server
func (s *Server) createHTTPServer() error {
	// Create router with middleware
	handler := s.setupRouter()

	// Configure TLS if enabled
	var tlsConfig *tls.Config
	if s.config.TLSEnabled {
		var err error
		tlsConfig, err = s.buildTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
	}

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:           s.config.ListenAddr,
		Handler:        handler,
		TLSConfig:      tlsConfig,
		ReadTimeout:    s.config.ReadTimeout,
		WriteTimeout:   s.config.WriteTimeout,
		IdleTimeout:    s.config.IdleTimeout,
		MaxHeaderBytes: s.config.MaxHeaderSize,

		// Security settings
		ReadHeaderTimeout: 10 * time.Second,
	}

	return nil
}

// buildTLSConfig creates TLS configuration
func (s *Server) buildTLSConfig() (*tls.Config, error) {
	// Load server certificate
	cert, err := tls.LoadX509KeyPair(s.config.TLSCertFile, s.config.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   s.config.TLSMinVersion,
		MaxVersion:   tls.VersionTLS13,
		CipherSuites: secureCipherSuites(),
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
		},
		PreferServerCipherSuites: true,
	}

	// Configure mTLS if client CA provided
	if s.config.TLSClientCAFile != "" {
		clientCAPool := x509.NewCertPool()
		clientCA, err := ioutil.ReadFile(s.config.TLSClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read client CA: %w", err)
		}

		if !clientCAPool.AppendCertsFromPEM(clientCA) {
			return nil, errors.New("failed to parse client CA certificate")
		}

		tlsConfig.ClientCAs = clientCAPool

		// Require client certificates if RBAC enabled
		if s.config.RBACEnabled {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
		}

		s.logger.Info("mTLS client authentication enabled",
			utils.ZapBool("rbac_required", s.config.RBACEnabled))
	}

	return tlsConfig, nil
}

// Start starts the API server
func (s *Server) Start(ctx context.Context) error {
	if s.running.Load() {
		return errors.New("server already running")
	}

	s.running.Store(true)

	// Start HTTP server
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		var err error
		if s.config.TLSEnabled {
			s.logger.Info("starting HTTPS server",
				utils.ZapString("addr", s.config.ListenAddr),
				utils.ZapString("tls_min_version", tlsVersionString(s.config.TLSMinVersion)))
			err = s.httpServer.ListenAndServeTLS("", "")
		} else {
			s.logger.Warn("starting HTTP server WITHOUT TLS (development only)",
				utils.ZapString("addr", s.config.ListenAddr))
			err = s.httpServer.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", utils.ZapError(err))
		}
	}()

	s.logger.Info("API server started",
		utils.ZapString("addr", s.config.ListenAddr),
		utils.ZapString("base_path", s.config.BasePath),
		utils.ZapBool("tls_enabled", s.config.TLSEnabled),
		utils.ZapBool("rbac_enabled", s.config.RBACEnabled),
		utils.ZapBool("rate_limit_enabled", s.config.RateLimitEnabled))

	// Audit server start
	if s.audit != nil {
		s.audit.Log("api.server.started", utils.AuditInfo, map[string]interface{}{
			"listen_addr":  s.config.ListenAddr,
			"tls_enabled":  s.config.TLSEnabled,
			"rbac_enabled": s.config.RBACEnabled,
			"environment":  s.config.Environment,
		})
	}

	return nil
}

// Stop gracefully stops the API server
func (s *Server) Stop() error {
	var stopErr error

	s.closeOnce.Do(func() {
		if !s.running.Load() {
			return
		}

		s.logger.Info("stopping API server...")

		// Signal shutdown
		close(s.stopCh)

		// Shutdown HTTP server with timeout
		ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
		defer cancel()

		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.logger.Error("HTTP server shutdown error", utils.ZapError(err))
			stopErr = err
		}

		// Wait for all goroutines
		s.wg.Wait()

		s.running.Store(false)

		s.logger.Info("API server stopped")

		// Audit server stop
		if s.audit != nil {
			s.audit.Log("api.server.stopped", utils.AuditInfo, map[string]interface{}{
				"listen_addr": s.config.ListenAddr,
			})
		}
	})

	return stopErr
}

// IsRunning returns true if server is running
func (s *Server) IsRunning() bool {
	return s.running.Load()
}

// GetMetrics returns server metrics
func (s *Server) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})

	metrics["running"] = s.running.Load()
	metrics["listen_addr"] = s.config.ListenAddr

	if s.rateLimiter != nil {
		metrics["rate_limiter"] = s.rateLimiter.GetMetrics()
	}

	if s.ipAllowlist != nil {
		metrics["ip_allowlist"] = s.ipAllowlist.GetMetrics()
	}

	return metrics
}

// Helper functions

func secureCipherSuites() []uint16 {
	return []uint16{
		// TLS 1.3 (managed by crypto/tls)
		// TLS 1.2
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
	}
}

func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "1.0"
	case tls.VersionTLS11:
		return "1.1"
	case tls.VersionTLS12:
		return "1.2"
	case tls.VersionTLS13:
		return "1.3"
	default:
		return "unknown"
	}
}
