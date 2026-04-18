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
	"backend/pkg/control/policyoutbox"
	"backend/pkg/control/policytrace"
	"backend/pkg/ingest/kafka"
	"backend/pkg/mempool"
	"backend/pkg/p2p"
	backendsecurity "backend/pkg/security"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"

	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type PolicyOutboxStatsProvider interface {
	GetPolicyOutboxDispatcherStats() (policyoutbox.DispatcherStats, bool)
	GetPolicyOutboxBacklogStats(ctx context.Context) (policyoutbox.BacklogStats, bool)
	GetCommitPathStats() (CommitPathStats, bool)
	GetPolicyAckConsumerStats() (PolicyAckConsumerStats, bool)
	GetPolicyAckCausalStats() (PolicyAckCausalStats, bool)
	GetPolicyPublisherStats() (PolicyPublisherStats, bool)
	NotifyPolicyOutboxDispatcher()
}

type PolicyRuntimeTraceProvider interface {
	GetPolicyRuntimeTrace(policyID string) []policytrace.Marker
}

type CommitPathStats struct {
	LogSampleEvery                       uint64
	LogsSuppressed                       uint64
	BackfillPending                      uint64
	BackfillDropped                      uint64
	ReplayRejectedAdmit                  uint64
	ReplayRejectedBuild                  uint64
	ReplayFilterSize                     uint64
	ReplayFilterEvictions                uint64
	PersistEnqueueCommitProposer         uint64
	PersistEnqueueCommitNonProposer      uint64
	PersistEnqueueBackfillProposer       uint64
	PersistEnqueueBackfillNonProposer    uint64
	PersistEnqueueDroppedNonProposer     uint64
	PersistEnqueueErrors                 uint64
	PersistEnqueueTimeouts               uint64
	PersistEnqueueTimeoutsCommit         uint64
	PersistEnqueueTimeoutsBackfill       uint64
	PersistEnqueueWaitCommitSumMs        uint64
	PersistEnqueueWaitCommitCount        uint64
	PersistEnqueueWaitBackfillSumMs      uint64
	PersistEnqueueWaitBackfillCount      uint64
	PersistDirectFallbackAttempts        uint64
	PersistDirectFallbackSuccess         uint64
	PersistDirectFallbackFailures        uint64
	PersistDirectFallbackThrottled       uint64
	PersistDirectFallbackSkippedPressure uint64
	PersistDirectFallbackSkippedDistress uint64
	PersistDirectFallbackInFlight        uint64
	PersistWriterTakeoverActivations     uint64
	BackfillOldestPendingAgeMs           uint64
	BackfillNonRetryQuarantined          uint64
	PersistTotalBudgetExhausted          uint64
	PersistAttemptTimeoutCapped          uint64
	PersistOnPersistedDelaySumMs         uint64
	PersistOnPersistedDelayCount         uint64
	PersistMetadataUpdateSumMs           uint64
	PersistMetadataUpdateCount           uint64
	PersistMetadataUpdateFailures        uint64
	PersistExecuteProposer               uint64
	PersistExecuteNonProposer            uint64
	PersistExecuteDroppedNonOwner        uint64
	LifecyclePreAdmitAllowed             uint64
	LifecycleMempoolAddSuccess           uint64
	LifecycleMempoolAddFailure           uint64
	LifecycleCompactionRejected          uint64
	LifecyclePostAdmitEvicted            uint64
	LifecycleCompactionSuperseded        uint64
	BuilderP0ReservedPickTotal           uint64
	BuilderFairSharePickTotal            uint64
	BuilderFairShareRoundsTotal          uint64
	BuilderFairShareStarvedTotal         uint64
	ApplyBlockRuns                       uint64
	ApplyBlockEventTxs                   uint64
	ApplyBlockEvidenceTxs                uint64
	ApplyBlockPolicyTxs                  uint64
	ApplyBlockTotalBuckets               []utils.HistogramBucket
	ApplyBlockTotalCount                 uint64
	ApplyBlockTotalSumMs                 float64
	ApplyBlockTotalP95Ms                 float64
	ApplyBlockValidateBuckets            []utils.HistogramBucket
	ApplyBlockValidateCount              uint64
	ApplyBlockValidateSumMs              float64
	ApplyBlockValidateP95Ms              float64
	ApplyBlockNonceCheckBuckets          []utils.HistogramBucket
	ApplyBlockNonceCheckCount            uint64
	ApplyBlockNonceCheckSumMs            float64
	ApplyBlockNonceCheckP95Ms            float64
	ApplyBlockReducerEventBuckets        []utils.HistogramBucket
	ApplyBlockReducerEventCount          uint64
	ApplyBlockReducerEventSumMs          float64
	ApplyBlockReducerEventP95Ms          float64
	ApplyBlockReducerEvidenceBuckets     []utils.HistogramBucket
	ApplyBlockReducerEvidenceCount       uint64
	ApplyBlockReducerEvidenceSumMs       float64
	ApplyBlockReducerEvidenceP95Ms       float64
	ApplyBlockReducerPolicyBuckets       []utils.HistogramBucket
	ApplyBlockReducerPolicyCount         uint64
	ApplyBlockReducerPolicySumMs         float64
	ApplyBlockReducerPolicyP95Ms         float64
	ApplyBlockCommitStateBuckets         []utils.HistogramBucket
	ApplyBlockCommitStateCount           uint64
	ApplyBlockCommitStateSumMs           float64
	ApplyBlockCommitStateP95Ms           float64
}

type PolicyAckConsumerStats struct {
	ProcessedTotal          uint64
	RejectedTotal           uint64
	StoreRetryAttempts      uint64
	StoreRetryExhausted     uint64
	DLQPublishedTotal       uint64
	DLQPublishFailures      uint64
	LoopErrors              uint64
	WorkQueueWaits          uint64
	SoftThrottleActivations uint64
	LogsThrottled           uint64
	PartitionAssignments    uint64
	PartitionRevocations    uint64
	PartitionsOwnedCurrent  uint64
}

type PolicyAckCausalStats struct {
	SkewCorrectionsTotal       uint64
	CorrelationCommand         uint64
	CorrelationExact           uint64
	CorrelationFallbackHash    uint64
	CorrelationFallbackTrace   uint64
	CorrelationNoMatch         uint64
	CorrelationErrors          uint64
	AIEventUnitCorrections     uint64
	AIEventInvalidTotal        uint64
	SourceEventUnitCorrections uint64
	SourceEventInvalidTotal    uint64
	AIToAckBuckets             []utils.HistogramBucket
	AIToAckCount               uint64
	AIToAckSumMs               float64
	AIToAckP95Ms               float64
	SourceToAckBuckets         []utils.HistogramBucket
	SourceToAckCount           uint64
	SourceToAckSumMs           float64
	SourceToAckP95Ms           float64
	PublishToAckBuckets        []utils.HistogramBucket
	PublishToAckCount          uint64
	PublishToAckSumMs          float64
	PublishToAckP95Ms          float64
	PublishToAppliedBuckets    []utils.HistogramBucket
	PublishToAppliedCount      uint64
	PublishToAppliedSumMs      float64
	PublishToAppliedP95Ms      float64
	AppliedToAckBuckets        []utils.HistogramBucket
	AppliedToAckCount          uint64
	AppliedToAckSumMs          float64
	AppliedToAckP95Ms          float64
	CorrelationLatencyBuckets  []utils.HistogramBucket
	CorrelationLatencyCount    uint64
	CorrelationLatencySumMs    float64
	CorrelationLatencyP95Ms    float64
}

type PolicyPublisherStats struct {
	DedupeSuppressedByReason map[string]uint64
}

// Server provides read-only API access to backend state
type Server struct {
	config     *config.APIConfig
	logger     *utils.Logger
	audit      *utils.AuditLogger
	httpServer *http.Server

	// Backend components
	storage         cockroach.Adapter
	stateStore      state.StateStore
	mempool         *mempool.Mempool
	engine          *consapi.ConsensusEngine
	p2pRouter       *p2p.Router
	kafkaProd       *kafka.Producer
	kafkaCons       *kafka.Consumer
	redisClient     *redis.Client
	redisMetrics    *redisTelemetry
	outboxStats     PolicyOutboxStatsProvider
	traceStats      PolicyRuntimeTraceProvider
	controlSafeMode *atomic.Bool

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
	dashboardMetricsMu   sync.RWMutex
	dashboardMetrics     map[string]*dashboardSectionMetric

	// Middleware components
	rateLimiter   *RateLimiter
	ipAllowlist   *utils.IPAllowlist
	sem           chan struct{}
	routeLimiters map[rateLimitKey]*routeLimiter

	aiClient     *http.Client
	aiBaseURL    string
	aiAuthToken  string
	bearerAuth   bearerTokenValidator
	authorizer   backendsecurity.Authorizer
	tupleManager *openFGATupleManager

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

	controlMutationsSafeMode            atomic.Bool
	controlMutationsKillSwitch          atomic.Bool
	controlBreakers                     *controlBreakerRegistry
	controlMutationBlockedSafeMode      atomic.Uint64
	controlMutationBlockedKillSwitch    atomic.Uint64
	controlMutationBlockedConsensus     atomic.Uint64
	controlMutationBlockedTenantScope   atomic.Uint64
	controlMutationTimeoutTotal         atomic.Uint64
	controlAPITimeoutTotal              atomic.Uint64
	controlBreakerOpenTotal             atomic.Uint64
	controlMutationRateLimitedTotal     atomic.Uint64
	controlMutationCooldownBlockedTotal atomic.Uint64
	controlMutationLimiter              *RateLimiter
	controlMutationLastActionMu         sync.Mutex
	controlMutationLastActionByTarget   map[string]time.Time
	controlGateStateMu                  sync.Mutex
	controlGateLastSampleAt             time.Time
	tupleSyncInFlight                   sync.Map
	tupleSyncLastQueued                 sync.Map
	controlGateLastPublishedRows        int64
	controlGateLastAckedRows            int64
	controlGateLastOldestPendingAgeMs   int64
	controlGateContinuousPassSince      time.Time
	controlGateLastIntegrityTotal       uint64
	controlGateLastTxMismatchTotal      uint64

	consensusLivelockMu              sync.Mutex
	consensusLivelockLastSampleAt    time.Time
	consensusLivelockLastCommitted   uint64
	consensusLivelockLastViewChanges uint64
	consensusLivelockSuspectSince    time.Time
	consensusLivelockActive          atomic.Bool
	consensusLivelockDetectedTotal   atomic.Uint64
	consensusLivelockLastReason      atomic.Value
	consensusLivelockNoCommitMs      atomic.Uint64
	consensusLivelockPersistMu       sync.Mutex
	consensusLivelockLastPersistAt   time.Time
	consensusLivelockLastPersistSet  atomic.Bool
	consensusLivelockLastPersistFlag atomic.Bool
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
	method      string
	path        string
	statusClass string
}

type routeMetric struct {
	totalRequests atomic.Uint64
	errorRequests atomic.Uint64
	bytesTotal    atomic.Uint64
	latencyMicros atomic.Uint64
	cacheHits     atomic.Uint64
	cacheMisses   atomic.Uint64
	latencyHist   *utils.LatencyHistogram
}

type routeMetricSnapshot struct {
	method         string
	path           string
	statusClass    string
	requests       uint64
	errors         uint64
	bytes          uint64
	latencyMicros  uint64
	cacheHits      uint64
	cacheMisses    uint64
	latencyBuckets []utils.HistogramBucket
	latencyCount   uint64
	latencySumMs   float64
	latencyP95Ms   float64
	latencyP99Ms   float64
}

type dashboardSectionMetric struct {
	hist *utils.LatencyHistogram
}

type dashboardSectionMetricSnapshot struct {
	section string
	buckets []utils.HistogramBucket
	count   uint64
	sumMs   float64
	p95Ms   float64
	p99Ms   float64
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
	Config          *config.APIConfig
	Logger          *utils.Logger
	AuditLogger     *utils.AuditLogger
	Storage         cockroach.Adapter
	StateStore      state.StateStore
	Mempool         *mempool.Mempool
	Engine          *consapi.ConsensusEngine
	P2PRouter       *p2p.Router
	KafkaProd       *kafka.Producer
	KafkaCons       *kafka.Consumer
	OutboxStats     PolicyOutboxStatsProvider
	TraceStats      PolicyRuntimeTraceProvider
	ControlSafeMode *atomic.Bool
	NodeAliases     map[string]string
	NodeAliasList   []string
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
		outboxStats:      deps.OutboxStats,
		traceStats:       deps.TraceStats,
		controlSafeMode:  deps.ControlSafeMode,
		stopCh:           make(chan struct{}),
		processStartTime: time.Now().Unix(),
		routeMetrics:     make(map[requestMetricKey]*routeMetric),
		dashboardMetrics: make(map[string]*dashboardSectionMetric),
	}
	s.controlMutationsSafeMode.Store(deps.Config.ControlMutationsSafeMode)
	if deps.ControlSafeMode != nil {
		deps.ControlSafeMode.Store(deps.Config.ControlMutationsSafeMode)
	}
	if deps.Config.ControlAPIBreakerEnabled {
		s.controlBreakers = newControlBreakerRegistry(deps.Config.ControlAPIBreakerErrorThreshold, deps.Config.ControlAPIBreakerCooldown)
	}
	if deps.Config.ControlMutationMaxPerMinute > 0 {
		s.controlMutationLimiter = NewRateLimiter(RateLimiterConfig{
			RequestsPerMinute: deps.Config.ControlMutationMaxPerMinute,
			Burst:             deps.Config.ControlMutationMaxPerMinute,
			Logger:            deps.Logger,
		})
	}
	s.controlMutationLastActionByTarget = make(map[string]time.Time)
	s.consensusLivelockLastReason.Store("")

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

	if validator, err := newUserAuthnZitadel(deps.Config); err != nil {
		return nil, fmt.Errorf("failed to initialize ZITADEL JWT validator: %w", err)
	} else {
		s.bearerAuth = validator
	}
	s.authorizer = newAuthzOpenFGA(deps.Config, deps.Logger)
	s.tupleManager = newOpenFGATupleManager(deps.Config, deps.Logger)

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

	// Start background cache warmer
	if s.config.DashboardCacheTTL > 0 {
		s.wg.Add(1)
		go s.runCacheWarmer()
	}
	if s.tupleManager != nil && s.config.OpenFGATupleReconcileEnabled {
		s.wg.Add(1)
		go s.runOpenFGATupleReconciler()
	}
	if s.config.ConsensusLivelockDetectorEnabled {
		s.wg.Add(1)
		go s.runConsensusLivelockMonitor()
	}

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
		if closer, ok := s.bearerAuth.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				s.logger.Warn("bearer auth validator close error", utils.ZapError(err))
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
	key := requestMetricKey{
		method:      strings.ToUpper(method),
		path:        s.normalizeRoutePath(path),
		statusClass: normalizeStatusClass(status),
	}
	metric := s.getOrCreateRouteMetric(key)
	metric.totalRequests.Add(1)
	if status >= 400 {
		metric.errorRequests.Add(1)
	}
	if duration > 0 {
		metric.latencyMicros.Add(uint64(duration / time.Microsecond))
		metric.latencyHist.Observe(float64(duration) / float64(time.Millisecond))
	}
	if bytes > 0 {
		metric.bytesTotal.Add(uint64(bytes))
	}
}

func (s *Server) recordRouteCacheHit(method, path string) {
	key := requestMetricKey{
		method:      strings.ToUpper(method),
		path:        s.normalizeRoutePath(path),
		statusClass: "all",
	}
	metric := s.getOrCreateRouteMetric(key)
	metric.cacheHits.Add(1)
}

func (s *Server) recordRouteCacheMiss(method, path string) {
	key := requestMetricKey{
		method:      strings.ToUpper(method),
		path:        s.normalizeRoutePath(path),
		statusClass: "all",
	}
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

func (s *Server) runCacheWarmer() {
	defer s.wg.Done()

	ttl := s.config.DashboardCacheTTL
	refreshBefore := ttl * 80 / 100
	ticker := time.NewTicker(refreshBefore / 2)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.maybeRefreshDashboardCache()
		}
	}
}

func (s *Server) maybeRefreshDashboardCache() {
	s.dashboardCacheMu.RLock()
	entry := s.dashboardCache
	s.dashboardCacheMu.RUnlock()

	if entry.snapshot == nil {
		return
	}

	age := time.Since(entry.generatedAt)
	threshold := s.config.DashboardCacheTTL * 80 / 100

	if age < threshold {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.RequestTimeout)
	defer cancel()

	snapshot := s.buildDashboardSnapshot(ctx)
	if snapshot != nil {
		s.setDashboardCache(snapshot, time.Now())
	}
}

func (s *Server) buildDashboardSnapshot(ctx context.Context) *DashboardOverviewResponse {
	snapshot, _ := s.buildDashboardSnapshotMeasured(ctx)
	return snapshot
}

func (s *Server) buildDashboardSnapshotMeasured(ctx context.Context) (*DashboardOverviewResponse, map[string]time.Duration) {
	totalStart := time.Now()
	sectionDurations := make(map[string]time.Duration, 8)
	recordSection := func(name string, start time.Time) {
		d := time.Since(start)
		sectionDurations[name] = d
		s.recordDashboardSectionMetric(name, d)
	}

	backendStart := time.Now()
	stats := s.buildStatsResponse(ctx)
	backendMetrics := s.buildDashboardBackendMetrics(stats)
	backendDerived := s.buildDashboardBackendDerived(stats)
	backendHistory := s.snapshotBackendHistory(stats, backendMetrics)
	readiness, _ := s.buildReadinessResponse(ctx)
	health := healthResponseFromReadiness(readiness)
	recordSection("backend", backendStart)

	var consensusMetrics consapi.MetricsSnapshot
	if s.engine != nil {
		consensusMetrics = s.engine.GetMetrics()
	}

	var (
		networkOverview   *NetworkOverviewResponse
		consensusOverview *ConsensusOverviewResponse
		blocksSection     DashboardBlocksSection
		threatsSection    DashboardThreatsSection
		aiSection         DashboardAISection
		ledgerSection     *DashboardLedgerSection
		validatorSection  *DashboardValidatorsSection
	)

	g, gctx := errgroup.WithContext(ctx)
	var sectionMu sync.Mutex

	if s.engine != nil {
		g.Go(func() error {
			start := time.Now()
			if overview, err := s.buildNetworkOverview(gctx, time.Now().UTC()); err == nil {
				networkOverview = overview
			} else if s.logger != nil {
				s.logger.WarnContext(gctx, "failed to build network overview", utils.ZapError(err))
			}
			sectionMu.Lock()
			recordSection("network", start)
			sectionMu.Unlock()
			return nil
		})
		g.Go(func() error {
			start := time.Now()
			if overview, err := s.buildConsensusOverview(gctx, time.Now().UTC()); err == nil {
				consensusOverview = overview
			} else if s.logger != nil {
				s.logger.WarnContext(gctx, "failed to build consensus overview", utils.ZapError(err))
			}
			sectionMu.Lock()
			recordSection("consensus", start)
			sectionMu.Unlock()
			return nil
		})
	}

	g.Go(func() error {
		start := time.Now()
		blocksSection = s.buildDashboardBlocks(gctx, stats, consensusMetrics)
		sectionMu.Lock()
		recordSection("blocks", start)
		sectionMu.Unlock()
		return nil
	})

	g.Go(func() error {
		start := time.Now()
		threatsSection = s.buildDashboardThreats(gctx)
		sectionMu.Lock()
		recordSection("threats", start)
		sectionMu.Unlock()
		return nil
	})

	g.Go(func() error {
		start := time.Now()
		aiSection = s.buildDashboardAI(gctx)
		sectionMu.Lock()
		recordSection("ai", start)
		sectionMu.Unlock()
		return nil
	})

	g.Go(func() error {
		start := time.Now()
		ledgerSection = s.buildDashboardLedger(gctx, stats)
		sectionMu.Lock()
		recordSection("ledger", start)
		sectionMu.Unlock()
		return nil
	})

	g.Go(func() error {
		start := time.Now()
		validatorSection = s.buildDashboardValidators(gctx)
		sectionMu.Lock()
		recordSection("validators", start)
		sectionMu.Unlock()
		return nil
	})

	_ = g.Wait()

	snapshot := &DashboardOverviewResponse{
		Timestamp: time.Now().UnixMilli(),
		Backend: DashboardBackendSection{
			Health:    health,
			Readiness: readiness,
			Stats:     stats,
			Metrics:   backendMetrics,
			Derived:   backendDerived,
			History:   backendHistory,
		},
		Ledger:     ledgerSection,
		Validators: validatorSection,
		Network:    networkOverview,
		Consensus:  consensusOverview,
		Blocks:     blocksSection,
		Threats:    threatsSection,
		AI:         aiSection,
	}
	recordSection("total", totalStart)
	return snapshot, sectionDurations
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

	metric := &routeMetric{
		latencyHist: utils.NewLatencyHistogram([]float64{1, 5, 20, 50, 100, 250, 500, 1000, 2500, 5000, 10000}),
	}
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
		buckets, count, sumMs := metric.latencyHist.Snapshot()
		snapshots = append(snapshots, routeMetricSnapshot{
			method:         key.method,
			path:           key.path,
			statusClass:    key.statusClass,
			requests:       metric.totalRequests.Load(),
			errors:         metric.errorRequests.Load(),
			bytes:          metric.bytesTotal.Load(),
			latencyMicros:  metric.latencyMicros.Load(),
			cacheHits:      metric.cacheHits.Load(),
			cacheMisses:    metric.cacheMisses.Load(),
			latencyBuckets: buckets,
			latencyCount:   count,
			latencySumMs:   sumMs,
			latencyP95Ms:   metric.latencyHist.Quantile(0.95),
			latencyP99Ms:   metric.latencyHist.Quantile(0.99),
		})
	}
	return snapshots
}

func normalizeStatusClass(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500 && status < 600:
		return "5xx"
	default:
		return "unknown"
	}
}

func (s *Server) recordDashboardSectionMetric(section string, duration time.Duration) {
	if s == nil || duration < 0 {
		return
	}
	metric := s.getOrCreateDashboardSectionMetric(section)
	metric.hist.Observe(float64(duration) / float64(time.Millisecond))
}

func (s *Server) getOrCreateDashboardSectionMetric(section string) *dashboardSectionMetric {
	s.dashboardMetricsMu.RLock()
	if metric, ok := s.dashboardMetrics[section]; ok {
		s.dashboardMetricsMu.RUnlock()
		return metric
	}
	s.dashboardMetricsMu.RUnlock()

	metric := &dashboardSectionMetric{
		hist: utils.NewLatencyHistogram([]float64{1, 5, 20, 50, 100, 250, 500, 1000, 2500, 5000, 10000}),
	}
	s.dashboardMetricsMu.Lock()
	if existing, ok := s.dashboardMetrics[section]; ok {
		s.dashboardMetricsMu.Unlock()
		return existing
	}
	s.dashboardMetrics[section] = metric
	s.dashboardMetricsMu.Unlock()
	return metric
}

func (s *Server) snapshotDashboardSectionMetrics() []dashboardSectionMetricSnapshot {
	s.dashboardMetricsMu.RLock()
	defer s.dashboardMetricsMu.RUnlock()
	snapshots := make([]dashboardSectionMetricSnapshot, 0, len(s.dashboardMetrics))
	for section, metric := range s.dashboardMetrics {
		buckets, count, sumMs := metric.hist.Snapshot()
		snapshots = append(snapshots, dashboardSectionMetricSnapshot{
			section: section,
			buckets: buckets,
			count:   count,
			sumMs:   sumMs,
			p95Ms:   metric.hist.Quantile(0.95),
			p99Ms:   metric.hist.Quantile(0.99),
		})
	}
	return snapshots
}

func (s *Server) normalizeRoutePath(path string) string {
	if s == nil || s.config == nil {
		return path
	}
	basePath := strings.TrimSuffix(s.config.BasePath, "/")
	if basePath == "" || !strings.HasPrefix(path, basePath) {
		return path
	}
	trimmed := strings.TrimPrefix(path, basePath)
	switch {
	case trimmed == "/blocks/latest":
		return basePath + "/blocks/latest"
	case trimmed == "/blocks":
		return basePath + "/blocks"
	case strings.HasPrefix(trimmed, "/blocks/"):
		return basePath + "/blocks/{height}"
	case trimmed == "/state/root":
		return basePath + "/state/root"
	case strings.HasPrefix(trimmed, "/state/"):
		return basePath + "/state/{key}"
	case strings.HasPrefix(trimmed, "/policies/acks/"):
		return basePath + "/policies/acks/{id}"
	case strings.HasSuffix(trimmed, ":revoke") && strings.HasPrefix(trimmed, "/policies/"):
		return basePath + "/policies/{id}:revoke"
	case strings.HasSuffix(trimmed, ":approve") && strings.HasPrefix(trimmed, "/policies/"):
		return basePath + "/policies/{id}:approve"
	case strings.HasSuffix(trimmed, ":reject") && strings.HasPrefix(trimmed, "/policies/"):
		return basePath + "/policies/{id}:reject"
	case strings.HasSuffix(trimmed, "/coverage") && strings.HasPrefix(trimmed, "/policies/"):
		return basePath + "/policies/{id}/coverage"
	case strings.HasPrefix(trimmed, "/policies/"):
		return basePath + "/policies/{id}"
	case trimmed == "/control/outbox/backlog":
		return basePath + "/control/outbox/backlog"
	case strings.HasSuffix(trimmed, ":retry") && strings.HasPrefix(trimmed, "/control/outbox/"):
		return basePath + "/control/outbox/{id}:retry"
	case strings.HasSuffix(trimmed, ":requeue") && strings.HasPrefix(trimmed, "/control/outbox/"):
		return basePath + "/control/outbox/{id}:requeue"
	case strings.HasSuffix(trimmed, ":mark-terminal") && strings.HasPrefix(trimmed, "/control/outbox/"):
		return basePath + "/control/outbox/{id}:mark-terminal"
	case strings.HasSuffix(trimmed, ":revoke") && strings.HasPrefix(trimmed, "/control/outbox/"):
		return basePath + "/control/outbox/{id}:revoke"
	case strings.HasPrefix(trimmed, "/control/outbox/"):
		return basePath + "/control/outbox/{id}"
	case strings.HasPrefix(trimmed, "/control/trace/"):
		return basePath + "/control/trace/{policyId}"
	default:
		return path
	}
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
