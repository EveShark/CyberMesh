package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/config"
	consapi "backend/pkg/consensus/api"
	"backend/pkg/ingest/kafka"
	"backend/pkg/mempool"
	"backend/pkg/p2p"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"

	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Server provides read-only API access to backend state
type Server struct {
	config     *config.APIConfig
	logger     *utils.Logger
	audit      *utils.AuditLogger
	httpServer *http.Server

	// Backend components
	storage      cockroach.Adapter
	stateStore   state.StateStore
	mempool      *mempool.Mempool
	engine       *consapi.ConsensusEngine
	p2pRouter    *p2p.Router
	kafkaProd    *kafka.Producer
	kafkaCons    *kafka.Consumer
	redisClient  *redis.Client
	redisMetrics *redisTelemetry

	nodeAliases   map[string]string
	nodeAliasList []string

	metricsMu            sync.Mutex
	storageLatencyMs     float64
	kafkaReadyLatencyMs  float64
	p2pThroughputMu      sync.Mutex
	p2pLastBytesReceived uint64
	p2pLastBytesSent     uint64
	p2pLastSample        time.Time
	apiRequestsTotal     atomic.Uint64
	apiRequestErrors     atomic.Uint64
	routeMetricsMu       sync.RWMutex
	routeMetrics         map[requestMetricKey]*routeMetric

	// Middleware components
	rateLimiter   *RateLimiter
	ipAllowlist   *utils.IPAllowlist
	sem           chan struct{}
	routeLimiters map[rateLimitKey]*routeLimiter

	aiClient    *http.Client
	aiBaseURL   string
	aiAuthToken string

	// State
	running          atomic.Bool
	closeOnce        sync.Once
	stopCh           chan struct{}
	wg               sync.WaitGroup
	processStartTime int64 // Unix timestamp when server started

	metricsHistory      []DashboardBackendHistorySample
	lastHistorySnapshot *backendHistorySnapshot
	dashboardCacheMu    sync.RWMutex
	dashboardCache      dashboardCacheEntry

	loopStatus            atomic.Value
	loopStatusUpdated     atomic.Int64
	loopBlocking          atomic.Bool
	loopIssues            atomic.Value
	threatDataSource      atomic.Value
	threatSourceUpdated   atomic.Int64
	threatFallbackCount   atomic.Uint64
	threatFallbackUpdated atomic.Int64
	threatFallbackReason  atomic.Value
}

type rateLimitKey struct {
	method string
	path   string
}

type routeLimiter struct {
	config  RateLimiterConfig
	limiter *RateLimiter
}

type requestMetricKey struct {
	method string
	path   string
}

type routeMetric struct {
	totalRequests atomic.Uint64
	errorRequests atomic.Uint64
	bytesTotal    atomic.Uint64
	latencyMicros atomic.Uint64
	cacheHits     atomic.Uint64
	cacheMisses   atomic.Uint64
}

type routeMetricSnapshot struct {
	method        string
	path          string
	requests      uint64
	errors        uint64
	bytes         uint64
	latencyMicros uint64
	cacheHits     uint64
	cacheMisses   uint64
}

type backendHistorySnapshot struct {
	timestamp            int64
	cpuSecondsTotal      float64
	networkBytesSent     uint64
	networkBytesReceived uint64
	mempoolSizeBytes     int64
}

type dashboardCacheEntry struct {
	snapshot    *DashboardOverviewResponse
	generatedAt time.Time
}

// Dependencies holds server dependencies
type Dependencies struct {
	Config        *config.APIConfig
	Logger        *utils.Logger
	AuditLogger   *utils.AuditLogger
	Storage       cockroach.Adapter
	StateStore    state.StateStore
	Mempool       *mempool.Mempool
	Engine        *consapi.ConsensusEngine
	P2PRouter     *p2p.Router
	KafkaProd     *kafka.Producer
	KafkaCons     *kafka.Consumer
	NodeAliases   map[string]string
	NodeAliasList []string
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
		config:           deps.Config,
		logger:           deps.Logger,
		audit:            deps.AuditLogger,
		storage:          deps.Storage,
		stateStore:       deps.StateStore,
		mempool:          deps.Mempool,
		engine:           deps.Engine,
		p2pRouter:        deps.P2PRouter,
		kafkaProd:        deps.KafkaProd,
		kafkaCons:        deps.KafkaCons,
		stopCh:           make(chan struct{}),
		processStartTime: time.Now().Unix(),
		routeMetrics:     make(map[requestMetricKey]*routeMetric),
	}

	s.loopStatus.Store("unknown")
	s.loopIssues.Store("")
	s.threatDataSource.Store("unknown")
	s.threatFallbackReason.Store("")

	if deps.Logger != nil {
		if deps.Config.DashboardCacheTTL > 0 {
			deps.Logger.Info("dashboard cache enabled",
				utils.ZapDuration("dashboard_cache_ttl", deps.Config.DashboardCacheTTL))
		} else {
			deps.Logger.Info("dashboard cache disabled",
				utils.ZapDuration("dashboard_cache_ttl", deps.Config.DashboardCacheTTL))
		}
	}

	if deps.Config.MaxConcurrentReqs > 0 {
		s.sem = make(chan struct{}, deps.Config.MaxConcurrentReqs)
	}

	if len(deps.NodeAliases) > 0 {
		s.nodeAliases = make(map[string]string, len(deps.NodeAliases))
		for key, value := range deps.NodeAliases {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				s.nodeAliases[strings.ToLower(strings.TrimSpace(key))] = trimmed
			}
		}
	}

	if len(deps.NodeAliasList) > 0 {
		s.nodeAliasList = make([]string, 0, len(deps.NodeAliasList))
		for _, alias := range deps.NodeAliasList {
			if trimmed := strings.TrimSpace(alias); trimmed != "" {
				s.nodeAliasList = append(s.nodeAliasList, trimmed)
			}
		}
	}

	// Initialize rate limiter if enabled
	if deps.Config.RateLimitEnabled {
		s.rateLimiter = NewRateLimiter(RateLimiterConfig{
			RequestsPerMinute: deps.Config.RateLimitPerMinute,
			Burst:             deps.Config.RateLimitBurst,
			Logger:            deps.Logger,
		})
		s.routeLimiters = make(map[rateLimitKey]*routeLimiter)
		for rawKey, override := range deps.Config.RouteRateLimits {
			key := parseRateLimitKey(rawKey)
			if key.path == "" {
				continue
			}
			config := RateLimiterConfig{
				RequestsPerMinute: override.RequestsPerMinute,
				Burst:             override.Burst,
				Logger:            deps.Logger,
			}
			s.routeLimiters[key] = &routeLimiter{
				config:  config,
				limiter: NewRateLimiter(config),
			}
		}
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

	}

	// Create HTTP server
	if err := s.createHTTPServer(); err != nil {
		return nil, fmt.Errorf("failed to create HTTP server: %w", err)
	}

	if deps.Config.AIServiceBaseURL != "" {
		s.aiBaseURL = deps.Config.AIServiceBaseURL
		timeout := deps.Config.AIServiceTimeout
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		s.aiClient = &http.Client{Timeout: timeout}
		s.aiAuthToken = deps.Config.AIServiceToken
	}

	if deps.Config.RedisEnabled {
		opts := &redis.Options{
			Addr:     fmt.Sprintf("%s:%d", deps.Config.RedisHost, deps.Config.RedisPort),
			Password: deps.Config.RedisPassword,
			DB:       deps.Config.RedisDB,
		}
		if deps.Config.RedisTLSEnabled {
			opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		s.redisClient = redis.NewClient(opts)
		s.redisMetrics = newRedisTelemetry()
		s.redisClient.AddHook(&redisTelemetryHook{telemetry: s.redisMetrics})
		if err := s.redisClient.Ping(context.Background()).Err(); err != nil {
			deps.Logger.Warn("Redis ping failed", utils.ZapError(err))
		} else {
			deps.Logger.Info("Redis client initialized",
				utils.ZapString("host", deps.Config.RedisHost),
				utils.ZapInt("port", deps.Config.RedisPort),
				utils.ZapBool("tls_enabled", deps.Config.RedisTLSEnabled))
		}
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

func parseRateLimitKey(raw string) rateLimitKey {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return rateLimitKey{}
	}
	method := "*"
	path := raw
	if idx := strings.Index(raw, " "); idx >= 0 {
		methodCandidate := strings.TrimSpace(raw[:idx])
		if methodCandidate != "" {
			method = strings.ToUpper(methodCandidate)
		}
		path = strings.TrimSpace(raw[idx+1:])
	}
	return rateLimitKey{method: method, path: path}
}

func (s *Server) lookupRouteLimiter(method, path string) *routeLimiter {
	if s.routeLimiters == nil {
		return nil
	}
	method = strings.ToUpper(method)
	if rl, ok := s.routeLimiters[rateLimitKey{method: method, path: path}]; ok {
		return rl
	}
	if rl, ok := s.routeLimiters[rateLimitKey{method: "*", path: path}]; ok {
		return rl
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

		if s.redisClient != nil {
			if err := s.redisClient.Close(); err != nil {
				s.logger.Warn("Redis client close error", utils.ZapError(err))
			}
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

func (s *Server) recordStorageLatency(latency float64) {
	s.metricsMu.Lock()
	s.storageLatencyMs = latency
	s.metricsMu.Unlock()
}

func (s *Server) recordKafkaLatency(latency float64) {
	s.metricsMu.Lock()
	s.kafkaReadyLatencyMs = latency
	s.metricsMu.Unlock()
}

func (s *Server) recordAPIRequest(status int) {
	s.apiRequestsTotal.Add(1)
	if status >= 400 {
		s.apiRequestErrors.Add(1)
	}
}

func (s *Server) recordRouteMetrics(method, path string, status int, duration time.Duration, bytes int64) {
	key := requestMetricKey{method: strings.ToUpper(method), path: path}
	metric := s.getOrCreateRouteMetric(key)
	metric.totalRequests.Add(1)
	if status >= 400 {
		metric.errorRequests.Add(1)
	}
	if duration > 0 {
		metric.latencyMicros.Add(uint64(duration / time.Microsecond))
	}
	if bytes > 0 {
		metric.bytesTotal.Add(uint64(bytes))
	}
}

func (s *Server) recordRouteCacheHit(method, path string) {
	key := requestMetricKey{method: strings.ToUpper(method), path: path}
	metric := s.getOrCreateRouteMetric(key)
	metric.cacheHits.Add(1)
}

func (s *Server) recordRouteCacheMiss(method, path string) {
	key := requestMetricKey{method: strings.ToUpper(method), path: path}
	metric := s.getOrCreateRouteMetric(key)
	metric.cacheMisses.Add(1)
}

func (s *Server) getDashboardCache(now time.Time) (*DashboardOverviewResponse, bool) {
	if s == nil || s.config == nil || s.config.DashboardCacheTTL <= 0 {
		return nil, false
	}

	s.dashboardCacheMu.RLock()
	entry := s.dashboardCache
	s.dashboardCacheMu.RUnlock()

	if entry.snapshot == nil {
		return nil, false
	}

	if now.Sub(entry.generatedAt) > s.config.DashboardCacheTTL {
		return nil, false
	}

	return entry.snapshot, true
}

func (s *Server) setDashboardCache(snapshot *DashboardOverviewResponse, now time.Time) {
	if s == nil || s.config == nil || s.config.DashboardCacheTTL <= 0 || snapshot == nil {
		return
	}

	s.dashboardCacheMu.Lock()
	s.dashboardCache = dashboardCacheEntry{
		snapshot:    snapshot,
		generatedAt: now,
	}
	s.dashboardCacheMu.Unlock()
}

func (s *Server) recordLoopStatus(ctx context.Context, status string, blocking bool, issues []string) {
	if s == nil {
		return
	}
	normalized := strings.ToLower(strings.TrimSpace(status))
	if normalized == "" {
		normalized = "unknown"
	}
	prev := "unknown"
	if val := s.loopStatus.Load(); val != nil {
		if cast, ok := val.(string); ok && cast != "" {
			prev = cast
		}
	}
	issuesSummary := ""
	if len(issues) > 0 {
		issuesSummary = strings.Join(issues, "; ")
		s.loopIssues.Store(issuesSummary)
	} else {
		s.loopIssues.Store("")
	}
	prevBlocking := s.loopBlocking.Load()
	s.loopBlocking.Store(blocking)
	now := time.Now().Unix()

	if prev != normalized {
		s.loopStatus.Store(normalized)
		s.loopStatusUpdated.Store(now)
		if s.logger != nil {
			fields := []zap.Field{
				utils.ZapString("from", prev),
				utils.ZapString("to", normalized),
				utils.ZapBool("blocking", blocking),
			}
			if issuesSummary != "" {
				fields = append(fields, utils.ZapString("issues", issuesSummary))
			}
			if normalized == "critical" || normalized == "stopped" || blocking {
				s.logger.WarnContext(ctx, "ai detection loop status changed", fields...)
			} else {
				s.logger.InfoContext(ctx, "ai detection loop status changed", fields...)
			}
		}
		return
	}

	if blocking && !prevBlocking {
		s.loopStatusUpdated.Store(now)
		if s.logger != nil {
			fields := []zap.Field{utils.ZapString("status", normalized)}
			if issuesSummary != "" {
				fields = append(fields, utils.ZapString("issues", issuesSummary))
			}
			s.logger.WarnContext(ctx, "ai detection loop entered blocking state", fields...)
		}
		return
	}

	if issuesSummary != "" && s.logger != nil && (normalized == "degraded" || normalized == "critical" || normalized == "stopped") {
		s.logger.WarnContext(ctx, "ai detection loop issues detected",
			utils.ZapString("status", normalized),
			utils.ZapString("issues", issuesSummary))
	}
}

func (s *Server) recordThreatSource(ctx context.Context, source string, fallbackReason string) {
	if s == nil {
		return
	}
	normalized := strings.ToLower(strings.TrimSpace(source))
	if normalized == "" {
		normalized = "unknown"
	}
	prev := "unknown"
	if val := s.threatDataSource.Load(); val != nil {
		if cast, ok := val.(string); ok && cast != "" {
			prev = cast
		}
	}
	now := time.Now().Unix()

	if prev != normalized {
		s.threatDataSource.Store(normalized)
		s.threatSourceUpdated.Store(now)
		if normalized == "feed_fallback" || normalized == "empty" {
			s.threatFallbackCount.Add(1)
			s.threatFallbackUpdated.Store(now)
			s.threatFallbackReason.Store(fallbackReason)
			if s.logger != nil {
				s.logger.WarnContext(ctx, "threat dashboard using fallback data",
					utils.ZapString("source", normalized),
					utils.ZapString("reason", fallbackReason))
			}
		} else if s.logger != nil && (prev == "feed_fallback" || prev == "empty") {
			s.logger.InfoContext(ctx, "threat dashboard restored to history source",
				utils.ZapString("source", normalized))
		}
		return
	}

	if (normalized == "feed_fallback" || normalized == "empty") && fallbackReason != "" {
		s.threatFallbackReason.Store(fallbackReason)
	}
}

func (s *Server) getOrCreateRouteMetric(key requestMetricKey) *routeMetric {
	s.routeMetricsMu.RLock()
	if metric, ok := s.routeMetrics[key]; ok {
		s.routeMetricsMu.RUnlock()
		return metric
	}
	s.routeMetricsMu.RUnlock()

	metric := &routeMetric{}
	s.routeMetricsMu.Lock()
	if existing, ok := s.routeMetrics[key]; ok {
		s.routeMetricsMu.Unlock()
		return existing
	}
	s.routeMetrics[key] = metric
	s.routeMetricsMu.Unlock()
	return metric
}

func (s *Server) snapshotRouteMetrics() []routeMetricSnapshot {
	s.routeMetricsMu.RLock()
	defer s.routeMetricsMu.RUnlock()
	snapshots := make([]routeMetricSnapshot, 0, len(s.routeMetrics))
	for key, metric := range s.routeMetrics {
		snapshots = append(snapshots, routeMetricSnapshot{
			method:        key.method,
			path:          key.path,
			requests:      metric.totalRequests.Load(),
			errors:        metric.errorRequests.Load(),
			bytes:         metric.bytesTotal.Load(),
			latencyMicros: metric.latencyMicros.Load(),
			cacheHits:     metric.cacheHits.Load(),
			cacheMisses:   metric.cacheMisses.Load(),
		})
	}
	return snapshots
}

func (s *Server) getStorageLatencyMs() float64 {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	return s.storageLatencyMs
}

func (s *Server) getKafkaReadyLatencyMs() float64 {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	return s.kafkaReadyLatencyMs
}

func (s *Server) computeP2PThroughput(now time.Time, stats p2p.RouterStats) (float64, float64) {
	s.p2pThroughputMu.Lock()
	defer s.p2pThroughputMu.Unlock()

	if s.p2pLastSample.IsZero() {
		s.p2pLastSample = now
		s.p2pLastBytesReceived = stats.BytesReceived
		s.p2pLastBytesSent = stats.BytesSent
		return 0, 0
	}

	elapsed := now.Sub(s.p2pLastSample).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}

	var inbound float64
	if stats.BytesReceived >= s.p2pLastBytesReceived {
		inbound = float64(stats.BytesReceived-s.p2pLastBytesReceived) / elapsed
	}

	var outbound float64
	if stats.BytesSent >= s.p2pLastBytesSent {
		outbound = float64(stats.BytesSent-s.p2pLastBytesSent) / elapsed
	}

	s.p2pLastSample = now
	s.p2pLastBytesReceived = stats.BytesReceived
	s.p2pLastBytesSent = stats.BytesSent

	return inbound, outbound
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

func (s *Server) redisSnapshot() ([]utils.HistogramBucket, uint64, float64, uint64) {
	if s.redisMetrics == nil {
		return nil, 0, 0, 0
	}
	return s.redisMetrics.snapshot()
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
