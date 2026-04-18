package cockroach

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/block"
	"backend/pkg/consensus/types"
	"backend/pkg/control/lifecycleaudit"
	"backend/pkg/control/policyoutbox"
	"backend/pkg/control/policystate"
	"backend/pkg/state"
	"backend/pkg/utils"
	"github.com/google/uuid"
	"github.com/jackc/pgconn"
	"github.com/lib/pq"
)

// Storage errors
var (
	ErrBlockNotFound       = errors.New("storage: block not found")
	ErrTransactionNotFound = errors.New("storage: transaction not found")
	ErrSnapshotNotFound    = errors.New("storage: snapshot not found")
	ErrIntegrityViolation  = errors.New("storage: integrity violation detected")
	ErrConflictDetected    = errors.New("storage: conflict detected on upsert")
	ErrInvalidData         = errors.New("storage: invalid data format")
)

func runPolicyStateRefreshMany(ctx context.Context, db *sql.DB, policyIDs []string) error {
	return policystate.RefreshMany(ctx, db, db, policyIDs)
}

var refreshPolicyStateMany = runPolicyStateRefreshMany

// Adapter defines the interface for CockroachDB persistence operations
type Adapter interface {
	// PersistBlock atomically persists a block, its transactions, and state snapshot
	// Uses configured transaction isolation (default SERIALIZABLE)
	// Idempotent: re-persisting same block succeeds if data matches
	PersistBlock(ctx context.Context, blk *block.AppBlock, receipts []state.Receipt, stateRoot [32]byte) error

	// GetBlock retrieves a block by height
	GetBlock(ctx context.Context, height uint64) (*block.AppBlock, error)

	// GetLatestHeight returns the maximum block height from the database
	GetLatestHeight(ctx context.Context) (uint64, error)

	// GetMinHeight returns the minimum block height from the database
	GetMinHeight(ctx context.Context) (uint64, error)

	// GetTransactionByContentHash retrieves a transaction by its content hash
	GetTransactionByContentHash(ctx context.Context, contentHash [32]byte) (*TxRow, error)

	// GetSnapshot retrieves a state snapshot by version
	GetSnapshot(ctx context.Context, version uint64) (*SnapshotRow, error)

	// ListTransactionsByBlock returns lightweight transaction metadata for a block
	ListTransactionsByBlock(ctx context.Context, height uint64) ([]TxMeta, error)

	// Consensus persistence operations (adapter satisfies types.StorageBackend)
	SaveProposal(ctx context.Context, hash []byte, height uint64, view uint64, proposer []byte, data []byte) error
	LoadProposal(ctx context.Context, hash []byte) ([]byte, error)
	ListProposals(ctx context.Context, minHeight uint64, limit int) ([]types.ProposalRecord, error)
	SaveQC(ctx context.Context, hash []byte, height uint64, view uint64, data []byte) error
	LoadQC(ctx context.Context, hash []byte) ([]byte, error)
	ListQCs(ctx context.Context, minHeight uint64, limit int) ([]types.QCRecord, error)
	SaveVote(ctx context.Context, view uint64, height uint64, voter []byte, blockHash []byte, voteHash []byte, data []byte) error
	ListVotes(ctx context.Context, minHeight uint64, limit int) ([]types.VoteRecord, error)
	SaveEvidence(ctx context.Context, hash []byte, height uint64, data []byte) error
	LoadEvidence(ctx context.Context, hash []byte) ([]byte, error)
	ListEvidence(ctx context.Context, minHeight uint64, limit int) ([]types.EvidenceRecord, error)
	SaveCommittedBlock(ctx context.Context, height uint64, hash []byte, qc []byte) error
	LoadLastCommitted(ctx context.Context) (uint64, []byte, []byte, error)
	LoadGenesisCertificate(ctx context.Context) ([]byte, bool, error)
	SaveGenesisCertificate(ctx context.Context, data []byte) error
	DeleteGenesisCertificate(ctx context.Context) error
	DeleteBefore(ctx context.Context, height uint64) error

	// Ping checks database liveness
	Ping(ctx context.Context) error

	// Close closes the database connection
	Close() error

	// Metrics returns aggregated latency metrics for diagnostics
	Metrics() MetricsSnapshot
}

func isUniqueViolation(err error, constraint string) bool {
	// lib/pq error
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		if string(pqErr.Code) != "23505" {
			return false
		}
		if constraint == "" {
			return true
		}
		return pqErr.Constraint == constraint
	}
	// pgx/pgconn error
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code != "23505" {
			return false
		}
		if constraint == "" {
			return true
		}
		return pgErr.ConstraintName == constraint
	}
	// Fallback: message contains SQLSTATE 23505 or duplicate key text
	msg := err.Error()
	if strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(strings.ToLower(msg), "duplicate key value violates unique constraint") {
		if constraint == "" {
			return true
		}
		return strings.Contains(msg, constraint)
	}
	return false
}

// adapter implements the Adapter interface
type adapter struct {
	db                  *sql.DB
	logger              *utils.Logger
	auditLogger         *utils.AuditLogger
	txIsolation         sql.IsolationLevel
	txIsolationLabel    string
	outboxNotifyMu      sync.RWMutex
	outboxDurableNotify func(context.Context, uint64, int)
	policyRefreshSem    chan struct{}
	policyRefreshAsync  bool
	policyRefreshMaxIDs int
	policyRefreshTTL    time.Duration
	policyRefreshFailureStreak atomic.Uint32
	policyRefreshCooldownUntil atomic.Int64
	metrics             *dbMetrics
	anomalyStoreEnabled bool
	txMismatchSeenMu    sync.Mutex
	txMismatchSeen      map[[32]byte]time.Time
	txMismatchSeenTTL   time.Duration
	txMismatchSeenMax   int
	perf                adapterPerformanceConfig
	txBatchCanary       *batchCanaryState
	outboxBatchCanary   *batchCanaryState
	batchTuner          *batchTunerState
	sqlTpl              sqlTemplateCache

	// Prepared statements for queries
	stmtGetBlock    *sql.Stmt
	stmtGetTxByHash *sql.Stmt
	stmtGetSnapshot *sql.Stmt
	stmtListTxByBlk *sql.Stmt

	// Consensus persistence prepared statements
	stmtUpsertProposal        *sql.Stmt
	stmtGetProposal           *sql.Stmt
	stmtListProposals         *sql.Stmt
	stmtUpsertQC              *sql.Stmt
	stmtGetQC                 *sql.Stmt
	stmtListQCs               *sql.Stmt
	stmtUpsertVote            *sql.Stmt
	stmtListVotes             *sql.Stmt
	stmtUpsertEvidence        *sql.Stmt
	stmtGetEvidence           *sql.Stmt
	stmtListEvidence          *sql.Stmt
	stmtUpsertMeta            *sql.Stmt
	stmtGetMeta               *sql.Stmt
	stmtGetCommitted          *sql.Stmt
	stmtLoadGenesisCert       *sql.Stmt
	stmtUpsertGenesisCert     *sql.Stmt
	stmtDeleteGenesisCert     *sql.Stmt
	stmtDeleteProposalsBefore *sql.Stmt
	stmtDeleteVotesBefore     *sql.Stmt
	stmtDeleteQCsBefore       *sql.Stmt
	stmtDeleteEvidenceBefore  *sql.Stmt
}

type sqlTemplateCache struct {
	txInsert         sync.Map // map[int]string
	txVerify         sync.Map // map[int]string
	anomalyUpsert    sync.Map // map[int]string
	outboxInsert     sync.Map // map[int]string
	outboxVerify     sync.Map // map[int]string
	outboxActive     sync.Map // map[int]string
	outboxActiveRows sync.Map // map[int]string
}

type activeSemanticOutboxRow struct {
	SemanticKey string
	PolicyID    string
	RuleHash    []byte
	Status      string
}

type outboxPrepared struct {
	blockHeight     uint64
	blockTS         int64
	txIndex         int
	policyID        string
	ruleHash        [32]byte
	semanticKey     string
	dispatchShard   string
	payload         []byte
	requestID       string
	commandID       string
	workflowID      string
	traceID         string
	aiEventTsMs     int64
	sourceEventID   string
	sourceEventTsMs int64
	sentinelEventID string
}

type batchTunerState struct {
	txCurrent       atomic.Int64
	outboxCurrent   atomic.Int64
	txScaleUp       atomic.Uint64
	txScaleDown     atomic.Uint64
	outboxScaleUp   atomic.Uint64
	outboxScaleDown atomic.Uint64
}

type batchCanaryState struct {
	mu                  sync.Mutex
	samples             []bool
	idx                 int
	filled              int
	bad                 int
	fallbackUntilUnixMs int64
	fallbackActivations atomic.Uint64
}

type policyStageMarker struct {
	policyID string
	traceID  string
	txIndex  int
}

type persistAttemptContextKey struct{}

// WithPersistAttempt attaches persistence attempt metadata to context for stage correlation.
func WithPersistAttempt(ctx context.Context, attempt int) context.Context {
	if attempt <= 0 {
		return ctx
	}
	return context.WithValue(ctx, persistAttemptContextKey{}, attempt)
}

// PersistAttemptFromContext returns persistence attempt metadata if present.
func PersistAttemptFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	v := ctx.Value(persistAttemptContextKey{})
	if n, ok := v.(int); ok && n > 0 {
		return n
	}
	return 0
}

const (
	txUpsertBatchSize      = 64
	outboxUpsertBatchSize  = 64
	anomalyUpsertBatchSize = 64
	maxUpsertBatchSize     = 1024
	// Cockroach automatic retry buffering is limited; keep outbox RETURNING payload
	// comfortably below that threshold with a conservative estimate.
	outboxReturningSafetyBudgetBytes = 12 * 1024
	// INSERT .. RETURNING currently returns id::STRING and tx_index.
	outboxReturningEstimatedRowBytes = 96
	// Keep a small commit reserve so optional anomaly writes do not consume the
	// remaining transaction deadline and fail-close durable policy persistence.
	anomalyPersistCommitReserve = 1500 * time.Millisecond
	// Keep a commit reserve for optional lifecycle audit journal writes so
	// best-effort audit work cannot consume the remaining durable commit budget.
	lifecycleAuditCommitReserve = 1200 * time.Millisecond
	// Async policy-state refresh should back off quickly after repeated DB timeouts
	// so it does not amplify pressure on the hot persist/outbox path.
	policyRefreshFailureStreakForCooldown = 1
	policyRefreshFailureCooldown          = 5 * time.Second
)

// AdapterPerformanceConfig controls persistence hot-path batching and guardrails.
type AdapterPerformanceConfig struct {
	TxBatchEnabled         bool
	TxBatchEnabledSet      bool
	TxBatchSize            int
	TxBatchAdaptive        bool
	TxBatchAdaptiveSet     bool
	TxBatchMinSize         int
	TxBatchScaleStep       int
	OutboxBatchEnabled     bool
	OutboxBatchEnabledSet  bool
	OutboxBatchSize        int
	OutboxBatchAdaptive    bool
	OutboxBatchAdaptiveSet bool
	OutboxBatchMinSize     int
	OutboxBatchScaleStep   int
	TxStoreFullPayload     bool
	TxStoreFullPayloadSet  bool
	CanaryAutoFallback     bool
	CanaryAutoFallbackSet  bool
	CanaryWindowSize       int
	CanaryMinSamples       int
	CanaryMaxErrorRate     float64
	CanaryFallbackCooldown time.Duration
	PersistTxRetryMax      int
	PersistTxRetryBaseMS   int
	PersistTxRetryMaxMS    int
	TxVerifyUseTx          bool
	TxVerifyUseTxSet       bool
	TxVerifyChunkSize      int
	OutboxDispatchShards   int
	OutboxDispatchMode     string
	ClusterShardingMode    string
	ClusterShardBuckets    int
}

type adapterPerformanceConfig struct {
	txBatchEnabled         bool
	txBatchSize            int
	txBatchAdaptive        bool
	txBatchMinSize         int
	txBatchScaleStep       int
	outboxBatchEnabled     bool
	outboxBatchSize        int
	outboxBatchAdaptive    bool
	outboxBatchMinSize     int
	outboxBatchScaleStep   int
	txStoreFullPayload     bool
	canaryAutoFallback     bool
	canaryWindowSize       int
	canaryMinSamples       int
	canaryMaxErrorRate     float64
	canaryFallbackCooldown time.Duration
	persistTxRetryMax      int
	persistTxRetryBase     time.Duration
	persistTxRetryCap      time.Duration
	txVerifyUseTx          bool
	txVerifyChunkSize      int
	outboxDispatchShards   int
	outboxDispatchMode     string
	clusterShardingMode    string
	clusterShardBuckets    int
}

// AdapterConfig holds configuration for the adapter
type AdapterConfig struct {
	DB                               *sql.DB
	Logger                           *utils.Logger
	AuditLogger                      *utils.AuditLogger
	PersistTxIsolation               string
	AllowReadCommittedIsolation      bool
	Performance                      AdapterPerformanceConfig
	PolicyStateRefreshAsyncEnabled   bool
	PolicyStateRefreshAsyncTimeout   time.Duration
	PolicyStateRefreshAsyncMaxPolicy int
}

// SetOutboxDurableNotifier registers a best-effort callback fired immediately
// after a block transaction commits durably and outbox rows are known durable.
// It can be used to trigger low-latency dispatcher wake-ups.
func (a *adapter) SetOutboxDurableNotifier(fn func(context.Context, uint64, int)) {
	if a == nil {
		return
	}
	a.outboxNotifyMu.Lock()
	a.outboxDurableNotify = fn
	a.outboxNotifyMu.Unlock()
}

// NewAdapter creates a new CockroachDB adapter
func NewAdapter(ctx context.Context, cfg *AdapterConfig) (Adapter, error) {
	if cfg == nil || cfg.DB == nil {
		return nil, errors.New("storage: adapter config with DB is required")
	}

	policyRefreshEnabled := true
	if !cfg.PolicyStateRefreshAsyncEnabled && (cfg.PolicyStateRefreshAsyncTimeout > 0 || cfg.PolicyStateRefreshAsyncMaxPolicy > 0) {
		policyRefreshEnabled = false
	} else if cfg.PolicyStateRefreshAsyncEnabled {
		policyRefreshEnabled = true
	}
	policyRefreshTimeout := cfg.PolicyStateRefreshAsyncTimeout
	if policyRefreshTimeout <= 0 {
		policyRefreshTimeout = 5 * time.Second
	}
	policyRefreshMaxIDs := cfg.PolicyStateRefreshAsyncMaxPolicy
	if policyRefreshMaxIDs <= 0 {
		policyRefreshMaxIDs = 32
	}
	txIsolation, txIsolationLabel, txIsolationValid := parsePersistTxIsolation(cfg.PersistTxIsolation, cfg.AllowReadCommittedIsolation)

	a := &adapter{
		db:                  cfg.DB,
		logger:              cfg.Logger,
		auditLogger:         cfg.AuditLogger,
		txIsolation:         txIsolation,
		txIsolationLabel:    txIsolationLabel,
		policyRefreshSem:    make(chan struct{}, 1),
		policyRefreshAsync:  policyRefreshEnabled,
		policyRefreshMaxIDs: policyRefreshMaxIDs,
		policyRefreshTTL:    policyRefreshTimeout,
		metrics:             newDBMetrics(),
		perf:                sanitizePerformanceConfig(cfg.Performance),
		txMismatchSeen:      make(map[[32]byte]time.Time),
		txMismatchSeenTTL:   6 * time.Hour,
		txMismatchSeenMax:   100000,
	}
	a.txBatchCanary = newBatchCanaryState(a.perf.canaryWindowSize)
	a.outboxBatchCanary = newBatchCanaryState(a.perf.canaryWindowSize)
	a.batchTuner = newBatchTunerState(a.perf.txBatchSize, a.perf.outboxBatchSize)

	// Prepare statements
	if err := a.prepareStatements(ctx); err != nil {
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	a.anomalyStoreEnabled = a.detectTableExists(ctx, "anomalies")

	if a.logger != nil {
		a.logger.InfoContext(ctx, "CockroachDB adapter initialized")
		a.logger.InfoContext(ctx, "persistence transaction isolation configured",
			utils.ZapString("requested", strings.TrimSpace(cfg.PersistTxIsolation)),
			utils.ZapString("effective", a.txIsolationLabel))
		if !txIsolationValid && strings.TrimSpace(cfg.PersistTxIsolation) != "" {
			a.logger.WarnContext(ctx, "unsupported or disallowed DB_PERSIST_TX_ISOLATION value; falling back to serializable",
				utils.ZapString("value", cfg.PersistTxIsolation))
		}
		if !a.perf.txStoreFullPayload {
			a.logger.WarnContext(ctx, "transaction payload persistence is in minimal mode; full tx JSON is not stored in transactions.payload")
		}
		if !a.anomalyStoreEnabled {
			a.logger.WarnContext(ctx, "anomaly persistence is disabled because the anomalies table is unavailable")
		}
	}

	return a, nil
}

func parsePersistTxIsolation(raw string, allowReadCommitted bool) (sql.IsolationLevel, string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "serializable":
		return sql.LevelSerializable, "serializable", true
	case "read_committed", "read-committed", "read committed":
		if !allowReadCommitted {
			return sql.LevelSerializable, "serializable", false
		}
		return sql.LevelReadCommitted, "read_committed", true
	default:
		return sql.LevelSerializable, "serializable", false
	}
}

func (a *adapter) persistTxIsolationLevel() sql.IsolationLevel {
	if a != nil && a.txIsolation != 0 {
		return a.txIsolation
	}
	return sql.LevelSerializable
}

func (a *adapter) persistTxOptions() *sql.TxOptions {
	return &sql.TxOptions{
		Isolation: a.persistTxIsolationLevel(),
	}
}

func (a *adapter) detectTableExists(ctx context.Context, tableName string) bool {
	if a == nil || a.db == nil || strings.TrimSpace(tableName) == "" {
		return false
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var exists bool
	err := a.db.QueryRowContext(lookupCtx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name = $1
		)
	`, tableName).Scan(&exists)
	if err != nil {
		if a.logger != nil {
			a.logger.WarnContext(ctx, "failed to detect table support",
				utils.ZapString("table", tableName),
				utils.ZapError(err))
		}
		return false
	}
	return exists
}

func sanitizePerformanceConfig(cfg AdapterPerformanceConfig) adapterPerformanceConfig {
	perf := adapterPerformanceConfig{
		txBatchEnabled:         true,
		txBatchSize:            txUpsertBatchSize,
		txBatchAdaptive:        true,
		txBatchMinSize:         32,
		txBatchScaleStep:       16,
		outboxBatchEnabled:     true,
		outboxBatchSize:        outboxUpsertBatchSize,
		outboxBatchAdaptive:    true,
		outboxBatchMinSize:     32,
		outboxBatchScaleStep:   16,
		txStoreFullPayload:     true,
		canaryAutoFallback:     true,
		canaryWindowSize:       50,
		canaryMinSamples:       20,
		canaryMaxErrorRate:     0.20,
		canaryFallbackCooldown: 5 * time.Minute,
		persistTxRetryMax:      4,
		persistTxRetryBase:     50 * time.Millisecond,
		persistTxRetryCap:      time.Second,
		txVerifyUseTx:          true,
		txVerifyChunkSize:      maxUpsertBatchSize,
		outboxDispatchShards:   1,
		outboxDispatchMode:     policyoutbox.DispatchShardTargetHashV1,
		clusterShardingMode:    "off",
		clusterShardBuckets:    1,
	}

	if cfg.TxBatchSize > 0 {
		perf.txBatchSize = cfg.TxBatchSize
	}
	if cfg.OutboxBatchSize > 0 {
		perf.outboxBatchSize = cfg.OutboxBatchSize
	}
	if perf.txBatchSize > maxUpsertBatchSize {
		perf.txBatchSize = maxUpsertBatchSize
	}
	if perf.txBatchSize < 1 {
		perf.txBatchSize = 1
	}
	if perf.outboxBatchSize > maxUpsertBatchSize {
		perf.outboxBatchSize = maxUpsertBatchSize
	}
	if perf.outboxBatchSize < 1 {
		perf.outboxBatchSize = 1
	}
	if cfg.TxBatchEnabledSet {
		perf.txBatchEnabled = cfg.TxBatchEnabled
	}
	if cfg.TxBatchAdaptiveSet {
		perf.txBatchAdaptive = cfg.TxBatchAdaptive
	}
	if cfg.OutboxBatchEnabledSet {
		perf.outboxBatchEnabled = cfg.OutboxBatchEnabled
	}
	if cfg.OutboxBatchAdaptiveSet {
		perf.outboxBatchAdaptive = cfg.OutboxBatchAdaptive
	}
	if cfg.TxBatchMinSize > 0 {
		perf.txBatchMinSize = cfg.TxBatchMinSize
	}
	if cfg.TxBatchScaleStep > 0 {
		perf.txBatchScaleStep = cfg.TxBatchScaleStep
	}
	if cfg.OutboxBatchMinSize > 0 {
		perf.outboxBatchMinSize = cfg.OutboxBatchMinSize
	}
	if cfg.OutboxBatchScaleStep > 0 {
		perf.outboxBatchScaleStep = cfg.OutboxBatchScaleStep
	}
	if cfg.TxStoreFullPayloadSet {
		perf.txStoreFullPayload = cfg.TxStoreFullPayload
	}
	if cfg.CanaryWindowSize > 0 {
		perf.canaryWindowSize = cfg.CanaryWindowSize
	}
	if cfg.CanaryMinSamples > 0 {
		perf.canaryMinSamples = cfg.CanaryMinSamples
	}
	if cfg.CanaryMaxErrorRate > 0 {
		perf.canaryMaxErrorRate = cfg.CanaryMaxErrorRate
	}
	if cfg.CanaryFallbackCooldown > 0 {
		perf.canaryFallbackCooldown = cfg.CanaryFallbackCooldown
	}
	if cfg.PersistTxRetryMax > 0 {
		perf.persistTxRetryMax = cfg.PersistTxRetryMax
	}
	if cfg.PersistTxRetryBaseMS > 0 {
		perf.persistTxRetryBase = time.Duration(cfg.PersistTxRetryBaseMS) * time.Millisecond
	}
	if cfg.PersistTxRetryMaxMS > 0 {
		perf.persistTxRetryCap = time.Duration(cfg.PersistTxRetryMaxMS) * time.Millisecond
	}
	if cfg.TxVerifyUseTxSet {
		perf.txVerifyUseTx = cfg.TxVerifyUseTx
	}
	if cfg.TxVerifyChunkSize > 0 {
		perf.txVerifyChunkSize = cfg.TxVerifyChunkSize
	}
	if cfg.OutboxDispatchShards > 0 {
		perf.outboxDispatchShards = cfg.OutboxDispatchShards
	}
	if strings.TrimSpace(cfg.OutboxDispatchMode) != "" {
		perf.outboxDispatchMode = strings.ToLower(strings.TrimSpace(cfg.OutboxDispatchMode))
	}
	if strings.TrimSpace(cfg.ClusterShardingMode) != "" {
		perf.clusterShardingMode = strings.ToLower(strings.TrimSpace(cfg.ClusterShardingMode))
	}
	if cfg.ClusterShardBuckets > 0 {
		perf.clusterShardBuckets = cfg.ClusterShardBuckets
	}
	if perf.persistTxRetryMax < 1 {
		perf.persistTxRetryMax = 1
	}
	if perf.persistTxRetryBase <= 0 {
		perf.persistTxRetryBase = 10 * time.Millisecond
	}
	if perf.persistTxRetryCap < perf.persistTxRetryBase {
		perf.persistTxRetryCap = perf.persistTxRetryBase
	}
	if perf.txVerifyChunkSize < 1 {
		perf.txVerifyChunkSize = 1
	}
	if perf.txVerifyChunkSize > maxUpsertBatchSize {
		perf.txVerifyChunkSize = maxUpsertBatchSize
	}
	if perf.clusterShardBuckets < 1 {
		perf.clusterShardBuckets = 1
	}
	if perf.outboxDispatchShards <= 0 {
		if perf.clusterShardBuckets > 1 {
			perf.outboxDispatchShards = perf.clusterShardBuckets
		} else {
			perf.outboxDispatchShards = 1
		}
	}
	if perf.canaryWindowSize < 1 {
		perf.canaryWindowSize = 1
	}
	if perf.canaryMinSamples < 1 {
		perf.canaryMinSamples = 1
	}
	if perf.canaryMinSamples > perf.canaryWindowSize {
		perf.canaryMinSamples = perf.canaryWindowSize
	}
	if perf.canaryMaxErrorRate <= 0 || perf.canaryMaxErrorRate > 1 {
		perf.canaryMaxErrorRate = 0.20
	}
	if perf.txBatchMinSize < 1 {
		perf.txBatchMinSize = 1
	}
	if perf.txBatchMinSize > perf.txBatchSize {
		perf.txBatchMinSize = perf.txBatchSize
	}
	if perf.txBatchScaleStep < 1 {
		perf.txBatchScaleStep = 1
	}
	if perf.outboxBatchMinSize < 1 {
		perf.outboxBatchMinSize = 1
	}
	if perf.outboxBatchMinSize > perf.outboxBatchSize {
		perf.outboxBatchMinSize = perf.outboxBatchSize
	}
	if perf.outboxBatchScaleStep < 1 {
		perf.outboxBatchScaleStep = 1
	}
	if cfg.CanaryAutoFallbackSet {
		perf.canaryAutoFallback = cfg.CanaryAutoFallback
	}
	return perf
}

func newBatchCanaryState(window int) *batchCanaryState {
	if window < 1 {
		window = 1
	}
	return &batchCanaryState{
		samples: make([]bool, window),
	}
}

func newBatchTunerState(txInitial, outboxInitial int) *batchTunerState {
	if txInitial < 1 {
		txInitial = txUpsertBatchSize
	}
	if outboxInitial < 1 {
		outboxInitial = outboxUpsertBatchSize
	}
	s := &batchTunerState{}
	s.txCurrent.Store(int64(txInitial))
	s.outboxCurrent.Store(int64(outboxInitial))
	return s
}

func (b *batchTunerState) txSize(defaultSize int) int {
	if b == nil {
		return defaultSize
	}
	v := int(b.txCurrent.Load())
	if v < 1 {
		return defaultSize
	}
	return v
}

func (b *batchTunerState) outboxSize(defaultSize int) int {
	if b == nil {
		return defaultSize
	}
	v := int(b.outboxCurrent.Load())
	if v < 1 {
		return defaultSize
	}
	return v
}

func (b *batchTunerState) setTxSize(size int) {
	if b == nil || size < 1 {
		return
	}
	b.txCurrent.Store(int64(size))
}

func (b *batchTunerState) setOutboxSize(size int) {
	if b == nil || size < 1 {
		return
	}
	b.outboxCurrent.Store(int64(size))
}

func (b *batchTunerState) scaleDownTx() {
	if b != nil {
		b.txScaleDown.Add(1)
	}
}

func (b *batchTunerState) scaleUpTx() {
	if b != nil {
		b.txScaleUp.Add(1)
	}
}

func (b *batchTunerState) scaleDownOutbox() {
	if b != nil {
		b.outboxScaleDown.Add(1)
	}
}

func (b *batchTunerState) scaleUpOutbox() {
	if b != nil {
		b.outboxScaleUp.Add(1)
	}
}

func (b *batchCanaryState) isFallbackActive(now time.Time) bool {
	if b == nil {
		return false
	}
	return now.UnixMilli() < atomic.LoadInt64(&b.fallbackUntilUnixMs)
}

func (b *batchCanaryState) fallbackUntilUnixMilli() int64 {
	if b == nil {
		return 0
	}
	return atomic.LoadInt64(&b.fallbackUntilUnixMs)
}

func (b *batchCanaryState) activationCount() uint64 {
	if b == nil {
		return 0
	}
	return b.fallbackActivations.Load()
}

func (b *batchCanaryState) recordSample(now time.Time, bad bool, minSamples int, maxErrorRate float64, cooldown time.Duration) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.filled == len(b.samples) {
		if b.samples[b.idx] {
			b.bad--
		}
	} else {
		b.filled++
	}

	b.samples[b.idx] = bad
	if bad {
		b.bad++
	}
	b.idx = (b.idx + 1) % len(b.samples)

	if b.filled < minSamples {
		return false
	}
	rate := float64(b.bad) / float64(b.filled)
	if rate <= maxErrorRate {
		return false
	}

	until := now.Add(cooldown).UnixMilli()
	previous := atomic.LoadInt64(&b.fallbackUntilUnixMs)
	if until > previous {
		atomic.StoreInt64(&b.fallbackUntilUnixMs, until)
		b.fallbackActivations.Add(1)
		return true
	}
	return false
}

// prepareStatements prepares SQL statements for reuse
func (a *adapter) prepareStatements(ctx context.Context) error {
	var err error

	// Prepare GetBlock statement
	a.stmtGetBlock, err = a.db.PrepareContext(ctx, `
		SELECT height, block_hash, parent_hash, state_root, tx_root, proposer_id, view_number, 
		       timestamp, tx_count, qc_view, qc_signatures, committed_at
		FROM blocks
		WHERE height = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get block: %w", err)
	}

	// Prepare GetTransactionByContentHash statement
	a.stmtGetTxByHash, err = a.db.PrepareContext(ctx, `
		SELECT tx_hash, block_height, tx_index, tx_type, producer_id, nonce, content_hash,
		       algorithm, public_key, signature, payload, custody_chain, status, error_msg,
		       submitted_at, executed_at
		FROM transactions
		WHERE content_hash = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get tx by hash: %w", err)
	}

	// Prepare GetSnapshot statement
	a.stmtGetSnapshot, err = a.db.PrepareContext(ctx, `
		SELECT version, state_root, block_height, block_hash, tx_count, 
		       reputation_changes, policy_changes, quarantine_changes, created_at
		FROM state_versions
		WHERE version = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get snapshot: %w", err)
	}

	// Prepare ListTransactionsByBlock statement (lightweight projection)
	a.stmtListTxByBlk, err = a.db.PrepareContext(ctx, `
		SELECT tx_hash, tx_index, tx_type, length(payload::text) as size_bytes
		FROM transactions
		WHERE block_height = $1
		ORDER BY tx_index ASC
	`)
	if err != nil {
		return fmt.Errorf("prepare list tx by block: %w", err)
	}

	// Consensus persistence statements
	a.stmtUpsertProposal, err = a.db.PrepareContext(ctx, `
		UPSERT INTO consensus_proposals (block_hash, height, view_number, proposer_id, proposal_cbor)
		VALUES ($1, $2, $3, $4, $5)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert proposal: %w", err)
	}

	a.stmtGetProposal, err = a.db.PrepareContext(ctx, `
		SELECT proposal_cbor
		FROM consensus_proposals
		WHERE block_hash = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get proposal: %w", err)
	}

	a.stmtListProposals, err = a.db.PrepareContext(ctx, `
		SELECT block_hash, height, view_number, proposer_id, proposal_cbor
		FROM consensus_proposals
		WHERE height >= $1
		ORDER BY height ASC
		LIMIT $2
	`)
	if err != nil {
		return fmt.Errorf("prepare list proposals: %w", err)
	}

	a.stmtUpsertQC, err = a.db.PrepareContext(ctx, `
		UPSERT INTO consensus_qcs (block_hash, height, view_number, qc_cbor)
		VALUES ($1, $2, $3, $4)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert qc: %w", err)
	}

	a.stmtGetQC, err = a.db.PrepareContext(ctx, `
		SELECT qc_cbor
		FROM consensus_qcs
		WHERE block_hash = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get qc: %w", err)
	}

	a.stmtListQCs, err = a.db.PrepareContext(ctx, `
		SELECT block_hash, height, view_number, qc_cbor
		FROM consensus_qcs
		WHERE height >= $1
		ORDER BY height ASC
		LIMIT $2
	`)
	if err != nil {
		return fmt.Errorf("prepare list qcs: %w", err)
	}

	a.stmtUpsertVote, err = a.db.PrepareContext(ctx, `
		UPSERT INTO consensus_votes (vote_hash, view_number, height, voter_id, block_hash, vote_cbor)
		VALUES ($1, $2, $3, $4, $5, $6)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert vote: %w", err)
	}

	a.stmtListVotes, err = a.db.PrepareContext(ctx, `
		SELECT vote_hash, view_number, height, voter_id, block_hash, vote_cbor
		FROM consensus_votes
		WHERE height >= $1
		ORDER BY height ASC, vote_hash ASC
		LIMIT $2
	`)
	if err != nil {
		return fmt.Errorf("prepare list votes: %w", err)
	}

	a.stmtUpsertEvidence, err = a.db.PrepareContext(ctx, `
		UPSERT INTO consensus_evidence (evidence_hash, height, evidence_cbor)
		VALUES ($1, $2, $3)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert evidence: %w", err)
	}

	a.stmtGetEvidence, err = a.db.PrepareContext(ctx, `
		SELECT evidence_cbor
		FROM consensus_evidence
		WHERE evidence_hash = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get evidence: %w", err)
	}

	a.stmtListEvidence, err = a.db.PrepareContext(ctx, `
		SELECT evidence_hash, height, evidence_cbor
		FROM consensus_evidence
		WHERE height >= $1
		ORDER BY height ASC
		LIMIT $2
	`)
	if err != nil {
		return fmt.Errorf("prepare list evidence: %w", err)
	}

	// Match UPSERT constraint (id=1) to ensure consistent genesis loading
	a.stmtLoadGenesisCert, err = a.db.PrepareContext(ctx, `
		SELECT certificate
		FROM genesis_certificates
		WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("prepare load genesis certificate: %w", err)
	}

	a.stmtUpsertGenesisCert, err = a.db.PrepareContext(ctx, `
		UPSERT INTO genesis_certificates (id, certificate)
		VALUES (1, $1)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert genesis certificate: %w", err)
	}

	a.stmtDeleteGenesisCert, err = a.db.PrepareContext(ctx, `
		DELETE FROM genesis_certificates WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete genesis certificate: %w", err)
	}

	a.stmtUpsertMeta, err = a.db.PrepareContext(ctx, `
		INSERT INTO consensus_metadata (key, height, block_hash, qc_cbor, updated_at)
		VALUES ('last_committed', $1, $2, $3, NOW())
		ON CONFLICT (key) DO UPDATE
		SET height = excluded.height,
		    block_hash = excluded.block_hash,
		    qc_cbor = excluded.qc_cbor,
		    updated_at = excluded.updated_at
		WHERE excluded.height >= consensus_metadata.height
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert metadata: %w", err)
	}

	a.stmtGetMeta, err = a.db.PrepareContext(ctx, `
		SELECT height, block_hash, qc_cbor
		FROM consensus_metadata
		WHERE key = 'last_committed'
	`)
	if err != nil {
		return fmt.Errorf("prepare get metadata: %w", err)
	}

	a.stmtGetCommitted, err = a.db.PrepareContext(ctx, `
		SELECT block_hash
		FROM blocks
		WHERE height = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get committed hash: %w", err)
	}

	a.stmtDeleteProposalsBefore, err = a.db.PrepareContext(ctx, `
		DELETE FROM consensus_proposals
		WHERE height < $1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete proposals before: %w", err)
	}

	a.stmtDeleteVotesBefore, err = a.db.PrepareContext(ctx, `
		DELETE FROM consensus_votes
		WHERE height < $1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete votes before: %w", err)
	}

	a.stmtDeleteQCsBefore, err = a.db.PrepareContext(ctx, `
		DELETE FROM consensus_qcs
		WHERE height < $1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete qcs before: %w", err)
	}

	a.stmtDeleteEvidenceBefore, err = a.db.PrepareContext(ctx, `
		DELETE FROM consensus_evidence
		WHERE height < $1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete evidence before: %w", err)
	}

	return nil
}

// PersistBlock atomically persists block, transactions, and state snapshot
func (a *adapter) PersistBlock(ctx context.Context, blk *block.AppBlock, receipts []state.Receipt, stateRoot [32]byte) (retErr error) {
	stop := a.recordTxn("persist_block")
	defer stop()
	persistStart := time.Now()
	txBodyStart := time.Time{}
	persistAttempt := PersistAttemptFromContext(ctx)
	defer func() {
		if a.metrics == nil {
			if a.logger != nil && blk != nil {
				total := time.Since(persistStart)
				if total >= 2*time.Second {
					a.logger.Warn("persist attempt summary",
						utils.ZapUint64("height", blk.GetHeight()),
						utils.ZapInt("attempt", persistAttempt),
						utils.ZapFloat64("total_ms", float64(total)/float64(time.Millisecond)),
						utils.ZapString("result", map[bool]string{true: "error", false: "ok"}[retErr != nil]))
				}
			}
			if retErr != nil && blk != nil {
				a.logPolicyStageMarkers(collectPolicyStageMarkers(blk), blk.GetHeight(), "t_outbox_row_materialize_failed", time.Now().UnixMilli(), persistAttempt)
			}
			return
		}
		a.metrics.observePersistStage("persist_attempt_total", time.Since(persistStart))
		if !txBodyStart.IsZero() {
			a.metrics.observePersistStage("tx_body_exec", time.Since(txBodyStart))
		}
		if retErr != nil {
			a.metrics.observePersistFailureClass("persist_attempt_total", retErr)
		}
		if a.logger != nil && blk != nil {
			total := time.Since(persistStart)
			if total >= 2*time.Second {
				result := "ok"
				class := "none"
				if retErr != nil {
					result = "error"
					class = classifyPersistError(retErr)
				}
				a.logger.Warn("persist attempt summary",
					utils.ZapUint64("height", blk.GetHeight()),
					utils.ZapInt("attempt", persistAttempt),
					utils.ZapFloat64("total_ms", float64(total)/float64(time.Millisecond)),
					utils.ZapString("result", result),
					utils.ZapString("class", class))
			}
		}
		if retErr != nil && blk != nil {
			a.logPolicyStageMarkers(collectPolicyStageMarkers(blk), blk.GetHeight(), "t_outbox_row_materialize_failed", time.Now().UnixMilli(), persistAttempt)
		}
	}()
	if blk == nil {
		retErr = fmt.Errorf("%w: block is nil", ErrInvalidData)
		return retErr
	}

	// Audit the persistence attempt (without payload)
	if a.auditLogger != nil {
		bh := blk.GetHash()
		_ = a.auditLogger.Info("block_persist_attempt", map[string]interface{}{
			"height":   blk.GetHeight(),
			"hash":     fmt.Sprintf("%x", bh[:8]),
			"tx_count": blk.GetTransactionCount(),
		})
	}

	maxAttempts := a.perf.persistTxRetryMax
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var txErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := retryBackoff(attempt-1, a.perf.persistTxRetryBase, a.perf.persistTxRetryCap)
			delay = withRetryJitter(delay)
			if !canRunPersistInternalRetry(ctx, delay) {
				retErr = txErr
				return retErr
			}
			if a.metrics != nil {
				a.metrics.observePersistStage("persist_internal_retry_delay", delay)
			}
			if !waitForContext(ctx, delay) {
				if txErr != nil && IsRetryable(txErr) {
					retErr = txErr
				} else {
					retErr = fmt.Errorf("persist internal retry canceled: %w", ctx.Err())
				}
				return retErr
			}
		}
		txErr = a.persistBlockOnce(ctx, blk, receipts, stateRoot, persistStart, &txBodyStart)
		if txErr == nil {
			retErr = nil
			return nil
		}
		if !IsRetryable(txErr) {
			retErr = txErr
			return retErr
		}
		if a.metrics != nil {
			a.metrics.observePersistFailureClass("persist_internal_retry", txErr)
			if classifyPersistError(txErr) == "retry_serialization" {
				a.metrics.observePersistContentionSignal("serialization_retry")
			}
		}
	}
	retErr = txErr
	return retErr
}

func (a *adapter) persistBlockOnce(ctx context.Context, blk *block.AppBlock, receipts []state.Receipt, stateRoot [32]byte, _ time.Time, txBodyStart *time.Time) error {
	// Start configured isolation (defaults to SERIALIZABLE for strict linearizability).
	beginStart := time.Now()
	tx, err := a.db.BeginTx(ctx, a.persistTxOptions())
	beginWait := time.Since(beginStart)
	if a.metrics != nil {
		a.metrics.observePersistStage("tx_begin_wait", beginWait)
		if beginWait >= 250*time.Millisecond {
			a.metrics.observePersistContentionSignal("tx_begin_lock_wait")
		}
	}
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	*txBodyStart = time.Now()
	defer tx.Rollback() // Safe to call even after commit
	persistAttempt := PersistAttemptFromContext(ctx)
	policyMarkers := collectPolicyStageMarkers(blk)
	policyIDs := collectPolicyIDsFromMarkers(policyMarkers)
	a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_db_tx_begin", time.Now().UnixMilli(), persistAttempt)

	// 1. UPSERT block
	stageStart := time.Now()
	if err := a.upsertBlock(ctx, tx, blk, stateRoot); err != nil {
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_block", time.Since(stageStart))
			a.metrics.observePersistFailureClass("upsert_block", err)
		}
		a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_db_upsert_block_failed", time.Now().UnixMilli(), persistAttempt)
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("block_persist_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("upsert block: %w", err)
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("upsert_block", time.Since(stageStart))
	}
	a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_db_upsert_block_done", time.Now().UnixMilli(), persistAttempt)

	// 2. UPSERT transactions
	stageStart = time.Now()
	upsertTransactionsCallStart := time.Now()
	if err := a.upsertTransactions(ctx, tx, blk, receipts, persistAttempt); err != nil {
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_call_wall", time.Since(upsertTransactionsCallStart))
			a.metrics.observePersistStage("upsert_transactions", time.Since(stageStart))
			a.metrics.observePersistFailureClass("upsert_transactions", err)
		}
		a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_db_upsert_transactions_failed", time.Now().UnixMilli(), persistAttempt)
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("transactions_persist_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("upsert transactions: %w", err)
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("upsert_transactions_call_wall", time.Since(upsertTransactionsCallStart))
		a.metrics.observePersistStage("upsert_transactions", time.Since(stageStart))
	}
	a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_db_upsert_transactions_done", time.Now().UnixMilli(), persistAttempt)

	// 3. UPSERT state snapshot
	stageStart = time.Now()
	if err := a.upsertSnapshot(ctx, tx, blk, stateRoot, len(receipts)); err != nil {
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_snapshot", time.Since(stageStart))
			a.metrics.observePersistFailureClass("upsert_snapshot", err)
		}
		a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_db_upsert_snapshot_failed", time.Now().UnixMilli(), persistAttempt)
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("snapshot_persist_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("upsert snapshot: %w", err)
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("upsert_snapshot", time.Since(stageStart))
	}
	a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_db_upsert_snapshot_done", time.Now().UnixMilli(), persistAttempt)

	// Commit transaction
	stageStart = time.Now()
	if err := tx.Commit(); err != nil {
		if a.metrics != nil {
			a.metrics.observePersistStage("commit", time.Since(stageStart))
			a.metrics.observePersistFailureClass("commit", err)
		}
		a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_db_tx_commit_failed", time.Now().UnixMilli(), persistAttempt)
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("block_persist_commit_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("commit transaction: %w", err)
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("commit", time.Since(stageStart))
	}
	a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_db_tx_commit_done", time.Now().UnixMilli(), persistAttempt)
	a.logPolicyStageMarkers(policyMarkers, blk.GetHeight(), "t_outbox_row_durable", time.Now().UnixMilli(), persistAttempt)
	a.notifyOutboxDurableCommitted(ctx, blk.GetHeight(), len(policyMarkers))
	a.refreshPolicyStateAsync(policyIDs, blk.GetHeight(), persistAttempt)

	// Success audit
	if a.auditLogger != nil {
		bh2 := blk.GetHash()
		_ = a.auditLogger.Info("block_persisted", map[string]interface{}{
			"height":     blk.GetHeight(),
			"hash":       fmt.Sprintf("%x", bh2[:8]),
			"tx_count":   blk.GetTransactionCount(),
			"state_root": fmt.Sprintf("%x", stateRoot[:8]),
		})
	}

	if a.logger != nil {
		a.logger.InfoContext(ctx, "block persisted successfully",
			utils.ZapUint64("height", blk.GetHeight()),
			utils.ZapInt("tx_count", blk.GetTransactionCount()))
	}

	return nil
}

func (a *adapter) notifyOutboxDurableCommitted(ctx context.Context, height uint64, policyRows int) {
	if a == nil || policyRows <= 0 {
		return
	}
	a.outboxNotifyMu.RLock()
	fn := a.outboxDurableNotify
	a.outboxNotifyMu.RUnlock()
	if fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil && a.logger != nil {
			a.logger.ErrorContext(ctx, "outbox durable notifier panic",
				utils.ZapAny("panic", r),
				utils.ZapUint64("height", height),
				utils.ZapInt("policy_rows", policyRows))
		}
	}()
	fn(ctx, height, policyRows)
}

func retryBackoff(attempt int, base, max time.Duration) time.Duration {
	if base <= 0 {
		base = 10 * time.Millisecond
	}
	if max < base {
		max = base
	}
	delay := base
	for i := 0; i < attempt; i++ {
		if delay >= max/2 {
			return max
		}
		delay *= 2
	}
	if delay > max {
		return max
	}
	return delay
}

func waitForContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func canRunPersistInternalRetry(ctx context.Context, delay time.Duration) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		return true
	}
	// Keep a small execution budget for the retrying SQL transaction body itself.
	const minRetryExecBudget = 100 * time.Millisecond
	return time.Until(deadline) > delay+minRetryExecBudget
}

func withRetryJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	maxJitter := base / 5 // +20% max jitter
	if maxJitter <= 0 {
		return base
	}
	extra := time.Duration(time.Now().UnixNano() % int64(maxJitter+1))
	return base + extra
}

// upsertBlock inserts or verifies existing block
func (a *adapter) upsertBlock(ctx context.Context, tx *sql.Tx, blk *block.AppBlock, stateRoot [32]byte) error {
	blockHash := blk.GetHash()
	parentHash := blk.GetParentHash()
	proposerID := blk.Proposer()

	// Try INSERT first
	bHash := blockHash[:]
	pHash := parentHash[:]
	pID := proposerID[:]
	sRoot := stateRoot[:]

	// Persist canonical tx_root from block header (already computed deterministically)
	txRootArr := blk.TxRoot()
	txRoot := txRootArr[:]

	// INSERT with conflict-avoidance to keep the transaction alive on duplicates
	res, err := tx.ExecContext(ctx, `
        INSERT INTO blocks (
            height, block_hash, parent_hash, state_root, proposer_id, view_number,
            timestamp, tx_count, tx_root, qc_view, qc_signatures, committed_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
        ON CONFLICT DO NOTHING
    `,
		blk.GetHeight(),
		bHash,
		pHash,
		sRoot,
		pID,
		uint64(0),          // view_number - TODO: get from block if available
		blk.GetTimestamp(), // Already time.Time
		blk.GetTransactionCount(),
		txRoot,
		uint64(0), // qc_view - TODO: get from QC if available
		[]byte{},  // qc_signatures - TODO: get from QC if available
	)
	if err != nil {
		return fmt.Errorf("insert block: %w", err)
	}

	if rows, _ := res.RowsAffected(); rows > 0 {
		return nil // Insert succeeded
	}

	// Conflict occurred. Verify idempotency: existing block must match exactly.
	var existingHash []byte
	err = tx.QueryRowContext(ctx, `SELECT block_hash FROM blocks WHERE height = $1`, blk.GetHeight()).Scan(&existingHash)
	if err == sql.ErrNoRows {
		// No row at this height; check if the same block_hash exists at a different height (corruption)
		var existingHeight uint64
		err2 := tx.QueryRowContext(ctx, `SELECT height FROM blocks WHERE block_hash = $1`, bHash).Scan(&existingHeight)
		if err2 == nil {
			if a.auditLogger != nil {
				_ = a.auditLogger.Security("block_hash_height_mismatch_detected", map[string]interface{}{
					"attempt_height":  blk.GetHeight(),
					"existing_height": existingHeight,
					"hash":            fmt.Sprintf("%x", blockHash[:]),
				})
			}
			return fmt.Errorf("%w: block hash already exists at different height (have %d, attempted %d)", ErrIntegrityViolation, existingHeight, blk.GetHeight())
		}
		return fmt.Errorf("%w: upsert conflict but no existing row found", ErrConflictDetected)
	}
	if err != nil {
		return fmt.Errorf("failed to verify existing block: %w", err)
	}

	if len(existingHash) != 32 {
		return fmt.Errorf("%w: existing block hash invalid length", ErrIntegrityViolation)
	}
	var existingHash32 [32]byte
	copy(existingHash32[:], existingHash)
	if blockHash != existingHash32 {
		if a.auditLogger != nil {
			_ = a.auditLogger.Security("block_hash_mismatch_detected", map[string]interface{}{
				"height":        blk.GetHeight(),
				"new_hash":      fmt.Sprintf("%x", blockHash[:]),
				"existing_hash": fmt.Sprintf("%x", existingHash32[:]),
			})
		}
		return fmt.Errorf("%w: block hash mismatch at height %d", ErrIntegrityViolation, blk.GetHeight())
	}

	// Idempotent insert
	return nil
}

type persistedTxRecord struct {
	txHash       [32]byte
	blockHeight  uint64
	txIndex      int
	txType       string
	producerID   []byte
	nonce        []byte
	contentHash  [32]byte
	algorithm    string
	publicKey    []byte
	signature    []byte
	payloadJSON  []byte
	custodyChain []byte
	status       string
	errorMsg     string
	submittedAt  time.Time
}

type persistedAnomalyRecord struct {
	anomalyID       string
	threatType      string
	severityValue   float64
	confidence      float64
	title           string
	description     string
	source          string
	modelVersion    string
	flowKey         string
	flowID          string
	sensorID        string
	validatorID     string
	scopeIdentifier string
	sourceEventID   string
	sourceEventTsMs int64
	sentinelEventID string
	blockHeight     uint64
	txHash          string
	detectedAt      int64
	rawPayload      []byte
}

type existingTxRecord struct {
	contentHash [32]byte
	producerID  []byte
	nonce       []byte
	blockHeight uint64
	txIndex     int
	algorithm   string
	publicKey   []byte
	signature   []byte
}

type policyOutboxInput struct {
	blockHeight uint64
	blockTS     int64
	txIndex     int
	txTS        int64
	payload     []byte
}

type persistedAnomalyPayload struct {
	AnomalyID          string                 `json:"anomaly_id"`
	ThreatType         string                 `json:"threat_type"`
	Severity           float64                `json:"severity"`
	Confidence         float64                `json:"confidence"`
	DetectionTimestamp int64                  `json:"detection_timestamp"`
	SourceToken        string                 `json:"source_token"`
	TargetToken        string                 `json:"target_token"`
	FlowKey            string                 `json:"flow_key"`
	ModelVersion       string                 `json:"model_version"`
	Description        string                 `json:"description"`
	Title              string                 `json:"title"`
	SourceEventID      string                 `json:"source_event_id"`
	SourceEventTsMs    int64                  `json:"source_event_ts_ms"`
	SentinelEventID    string                 `json:"sentinel_event_id"`
	Metadata           map[string]interface{} `json:"metadata"`
	Trace              map[string]interface{} `json:"trace"`
}

type policySemanticFingerprintContext struct {
	inMetadata bool
	inTrace    bool
	atRoot     bool
}

func (a *adapter) txInsertTemplate(rows int) string {
	if rows < 1 {
		rows = 1
	}
	if cached, ok := a.sqlTpl.txInsert.Load(rows); ok {
		if s, ok := cached.(string); ok && s != "" {
			return s
		}
	}
	var q strings.Builder
	q.Grow(rows * 80)
	q.WriteString(`
			INSERT INTO transactions (
				tx_hash, block_height, tx_index, tx_type, producer_id, nonce, content_hash,
				algorithm, public_key, signature, payload, custody_chain, status, error_msg,
				submitted_at, executed_at
			) VALUES
	`)
	for i := 0; i < rows; i++ {
		if i > 0 {
			q.WriteString(",")
		}
		base := i * 15
		fmt.Fprintf(&q, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,NOW())",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11, base+12, base+13, base+14, base+15)
	}
	q.WriteString(`
			ON CONFLICT (tx_hash) DO NOTHING
			RETURNING tx_hash
	`)
	sql := q.String()
	a.sqlTpl.txInsert.Store(rows, sql)
	return sql
}

func (a *adapter) txVerifyTemplate(rows int) string {
	if rows < 1 {
		rows = 1
	}
	if cached, ok := a.sqlTpl.txVerify.Load(rows); ok {
		if s, ok := cached.(string); ok && s != "" {
			return s
		}
	}
	var q strings.Builder
	q.Grow(rows * 8)
	q.WriteString(`
			SELECT tx_hash, content_hash, producer_id, nonce, block_height, tx_index, algorithm, public_key, signature
			FROM transactions
			WHERE tx_hash IN (
	`)
	for i := 0; i < rows; i++ {
		if i > 0 {
			q.WriteString(",")
		}
		fmt.Fprintf(&q, "$%d", i+1)
	}
	q.WriteString(")")
	sql := q.String()
	a.sqlTpl.txVerify.Store(rows, sql)
	return sql
}

func (a *adapter) outboxInsertTemplate(rows int) string {
	if rows < 1 {
		rows = 1
	}
	if cached, ok := a.sqlTpl.outboxInsert.Load(rows); ok {
		if s, ok := cached.(string); ok && s != "" {
			return s
		}
	}
	var q strings.Builder
	q.Grow(rows * 90)
	q.WriteString(`
			INSERT INTO control_policy_outbox (
				block_height, block_ts, tx_index, policy_id, rule_hash, semantic_key, dispatch_shard, payload, request_id, command_id, workflow_id, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, sentinel_event_id, status, next_retry_at, created_at, updated_at
			) VALUES
	`)
	for i := 0; i < rows; i++ {
		if i > 0 {
			q.WriteString(",")
		}
		base := i * 16
		fmt.Fprintf(&q, "($%d,$%d,$%d,$%d,$%d,NULLIF($%d,''),NULLIF($%d,''),$%d,NULLIF($%d,''),NULLIF($%d,''),NULLIF($%d,''),$%d,$%d,NULLIF($%d,''),NULLIF($%d,0),NULLIF($%d,''),'pending',NOW(),NOW(),NOW())",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11, base+12, base+13, base+14, base+15, base+16)
	}
	q.WriteString(`
			ON CONFLICT DO NOTHING
			RETURNING id::STRING, tx_index
	`)
	sql := q.String()
	a.sqlTpl.outboxInsert.Store(rows, sql)
	return sql
}

func (a *adapter) outboxActiveTemplate(rows int) string {
	if rows < 1 {
		rows = 1
	}
	if cached, ok := a.sqlTpl.outboxActive.Load(rows); ok {
		if s, ok := cached.(string); ok && s != "" {
			return s
		}
	}
	var q strings.Builder
	q.Grow(rows * 16)
	q.WriteString(`
			SELECT semantic_key
			FROM control_policy_outbox
			WHERE semantic_key IN (
	`)
	for i := 0; i < rows; i++ {
		if i > 0 {
			q.WriteString(",")
		}
		fmt.Fprintf(&q, "$%d", i+1)
	}
	q.WriteString(`
			)
			  AND status IN ('pending', 'retry', 'publishing', 'published')
	`)
	sql := q.String()
	a.sqlTpl.outboxActive.Store(rows, sql)
	return sql
}

func (a *adapter) outboxActiveRowsTemplate(rows int) string {
	if rows < 1 {
		rows = 1
	}
	if cached, ok := a.sqlTpl.outboxActiveRows.Load(rows); ok {
		if s, ok := cached.(string); ok && s != "" {
			return s
		}
	}
	var q strings.Builder
	q.Grow(rows * 32)
	q.WriteString(`
			SELECT semantic_key, policy_id, rule_hash, status
			FROM control_policy_outbox
			WHERE semantic_key IN (
	`)
	for i := 0; i < rows; i++ {
		if i > 0 {
			q.WriteString(",")
		}
		fmt.Fprintf(&q, "$%d", i+1)
	}
	q.WriteString(`
			)
			  AND status IN ('pending', 'retry', 'publishing', 'published')
	`)
	sql := q.String()
	a.sqlTpl.outboxActiveRows.Store(rows, sql)
	return sql
}

func (a *adapter) outboxVerifyTemplate(rows int) string {
	if rows < 1 {
		rows = 1
	}
	if cached, ok := a.sqlTpl.outboxVerify.Load(rows); ok {
		if s, ok := cached.(string); ok && s != "" {
			return s
		}
	}
	var q strings.Builder
	q.Grow(rows * 24)
	q.WriteString(`
			SELECT block_height, tx_index, policy_id, rule_hash
			FROM control_policy_outbox
			WHERE
	`)
	for i := 0; i < rows; i++ {
		if i > 0 {
			q.WriteString(" OR ")
		}
		base := i * 2
		fmt.Fprintf(&q, "(block_height = $%d AND tx_index = $%d)", base+1, base+2)
	}
	sql := q.String()
	a.sqlTpl.outboxVerify.Store(rows, sql)
	return sql
}

func (a *adapter) anomalyUpsertTemplate(rows int) string {
	if rows <= 0 {
		rows = 1
	}
	if rows > maxUpsertBatchSize {
		rows = maxUpsertBatchSize
	}
	if cached, ok := a.sqlTpl.anomalyUpsert.Load(rows); ok {
		return cached.(string)
	}

	const colsPerRow = 20
	var q strings.Builder
	q.WriteString(`
		INSERT INTO anomalies (
			anomaly_id, threat_type, severity_value, confidence, title, description,
			source, model_version, flow_key, flow_id, sensor_id, validator_id, scope_identifier, source_event_id, source_event_ts_ms,
			sentinel_event_id, block_height, tx_hash, detected_at, raw_payload,
			created_at, updated_at
		) VALUES
	`)
	for i := 0; i < rows; i++ {
		if i > 0 {
			q.WriteByte(',')
		}
		base := i*colsPerRow + 1
		q.WriteString(fmt.Sprintf(`
			($%d, $%d, $%d, $%d, NULLIF($%d, ''), NULLIF($%d, ''), $%d, NULLIF($%d, ''),
			 NULLIF($%d, ''), NULLIF($%d, ''), NULLIF($%d, ''), NULLIF($%d, ''), NULLIF($%d, ''), NULLIF($%d, ''), NULLIF($%d, 0), NULLIF($%d, ''),
			 $%d, $%d, $%d, $%d, NOW(), NOW())`,
			base, base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9,
			base+10, base+11, base+12, base+13, base+14, base+15, base+16, base+17, base+18, base+19,
		))
	}
	q.WriteString(`
		ON CONFLICT (anomaly_id) DO UPDATE SET
			threat_type = EXCLUDED.threat_type,
			severity_value = EXCLUDED.severity_value,
			confidence = EXCLUDED.confidence,
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			source = EXCLUDED.source,
			model_version = EXCLUDED.model_version,
			flow_key = EXCLUDED.flow_key,
			flow_id = EXCLUDED.flow_id,
			sensor_id = EXCLUDED.sensor_id,
			validator_id = EXCLUDED.validator_id,
			scope_identifier = EXCLUDED.scope_identifier,
			source_event_id = EXCLUDED.source_event_id,
			source_event_ts_ms = EXCLUDED.source_event_ts_ms,
			sentinel_event_id = EXCLUDED.sentinel_event_id,
			block_height = EXCLUDED.block_height,
			tx_hash = EXCLUDED.tx_hash,
			detected_at = EXCLUDED.detected_at,
			raw_payload = EXCLUDED.raw_payload,
			updated_at = NOW()
	`)

	sql := q.String()
	a.sqlTpl.anomalyUpsert.Store(rows, sql)
	return sql
}

func (a *adapter) marshalStoredTxPayload(stateTx state.Transaction) ([]byte, error) {
	if a == nil || a.perf.txStoreFullPayload {
		return json.Marshal(stateTx)
	}
	env := stateTx.Envelope()
	const payloadCap = 256
	buf := make([]byte, 0, payloadCap)
	buf = append(buf, []byte(`{"tx_type":"`)...)
	buf = append(buf, []byte(string(stateTx.Type()))...)
	buf = append(buf, []byte(`","payload_sha256":"`)...)
	buf = append(buf, []byte(hex.EncodeToString(env.ContentHash[:]))...)
	buf = append(buf, []byte(`","ts":`)...)
	buf = append(buf, []byte(strconv.FormatInt(stateTx.Timestamp(), 10))...)
	buf = append(buf, []byte(`}`)...)
	return buf, nil
}

func parsePersistedAnomaly(payload []byte, txHash [32]byte, blockHeight uint64, defaultTimestamp int64) (persistedAnomalyRecord, bool, error) {
	if len(payload) == 0 {
		return persistedAnomalyRecord{}, false, nil
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return persistedAnomalyRecord{}, false, err
	}

	doc := raw
	if nested, ok := raw["payload"].(map[string]interface{}); ok && len(nested) > 0 {
		doc = nested
	}

	schemaVersion := extractString(doc, "schema_version")
	eventID := extractString(doc, "event_id")
	anomalyID := extractString(doc, "anomaly_id")
	if anomalyID == "" {
		anomalyID = extractString(doc, "id")
	}
	if anomalyID == "" {
		anomalyID = deterministicAnomalyIDFromEventID(eventID)
	}
	threatType := extractString(doc, "threat_type")
	if threatType == "" {
		threatType = extractString(doc, "type")
	}
	sourceToken := extractString(doc, "source_token")
	targetToken := extractString(doc, "target_token")
	flowKey := extractString(doc, "flow_key")
	if flowKey == "" {
		flowKey = extractString(doc, "flow_id")
	}
	source := extractString(doc, "source")
	modelVersion := extractString(doc, "model_version")
	if modelVersion == "" {
		modelVersion = schemaVersion
	}
	title := extractString(doc, "title")
	description := extractString(doc, "description")
	detectedAt := extractInt64(doc, "detection_timestamp")
	if detectedAt <= 0 {
		detectedAt = extractInt64(doc, "ts")
	}
	severityValue := extractFloat64(doc, "severity")
	confidence := extractFloat64(doc, "confidence")

	if anomalyID == "" &&
		threatType == "" &&
		sourceToken == "" &&
		flowKey == "" &&
		targetToken == "" &&
		source == "" {
		return persistedAnomalyRecord{}, false, nil
	}

	sourceEventID := extractString(doc, "source_event_id")
	sourceEventTsMs := extractInt64(doc, "source_event_ts_ms")
	sentinelEventID := extractString(doc, "sentinel_event_id")
	metadata, _ := doc["metadata"].(map[string]interface{})
	labels, _ := doc["labels"].(map[string]interface{})
	analysis, _ := doc["analysis"].(map[string]interface{})
	input, _ := doc["input"].(map[string]interface{})
	if metadata != nil {
		if sourceEventID == "" {
			sourceEventID = extractString(metadata, "source_event_id")
		}
		if sourceEventID == "" {
			sourceEventID = extractString(metadata, "telemetry_event_id")
		}
		if sourceEventTsMs <= 0 {
			sourceEventTsMs = extractInt64(metadata, "source_event_ts_ms")
		}
		if sourceEventTsMs <= 0 {
			sourceEventTsMs = extractInt64(metadata, "telemetry_event_ts_ms")
		}
		if sentinelEventID == "" {
			sentinelEventID = extractString(metadata, "sentinel_event_id")
		}
	}
	trace, _ := doc["trace"].(map[string]interface{})
	if trace != nil {
		if sourceEventID == "" {
			sourceEventID = extractString(trace, "source_event_id")
		}
		if sourceEventTsMs <= 0 {
			sourceEventTsMs = extractInt64(trace, "source_event_ts_ms")
		}
		if sentinelEventID == "" {
			sentinelEventID = extractString(trace, "sentinel_event_id")
		}
	}
	if sourceEventID == "" {
		sourceEventID = extractString(labels, "source_event_id")
	}
	if sourceEventID == "" {
		sourceEventID = extractString(input, "id")
	}
	if sourceEventTsMs > 0 {
		if normalized, _, valid := utils.NormalizeUnixMillis(sourceEventTsMs); valid {
			sourceEventTsMs = normalized
		} else {
			sourceEventTsMs = 0
		}
	}
	if sourceEventTsMs <= 0 {
		if seconds := extractFloat64(input, "timestamp"); seconds > 0 {
			sourceEventTsMs = int64(seconds * 1000)
		}
	}
	if sentinelEventID == "" {
		sentinelEventID = eventID
	}
	flowID := flowKey
	if flowID == "" {
		flowID = extractString(labels, "flow_id")
	}
	if flowKey == "" {
		flowKey = flowID
	}
	sensorID := extractRootString(payload, "sensor_id")
	if sensorID == "" {
		sensorID = extractString(metadata, "sensor_id")
	}
	if sensorID == "" {
		sensorID = extractString(trace, "sensor_id")
	}
	if sensorID == "" {
		sensorID = extractString(labels, "source_id")
	}
	validatorID := extractRootString(payload, "validator_id")
	if validatorID == "" {
		validatorID = extractString(metadata, "validator_id")
	}
	if validatorID == "" {
		validatorID = extractString(trace, "validator_id")
	}
	scopeIdentifier := extractRootString(payload, "scope_identifier")
	if scopeIdentifier == "" {
		scopeIdentifier = extractString(metadata, "scope_identifier")
	}
	if scopeIdentifier == "" {
		scopeIdentifier = extractString(trace, "scope_identifier")
	}
	if scopeIdentifier == "" {
		scopeIdentifier = extractString(doc, "tenant_id")
	}

	if threatType == "" {
		if modality := strings.TrimSpace(extractString(input, "modality")); modality == "network_flow" {
			threatType = "network_intrusion"
		}
	}
	if threatType == "" {
		if level := strings.TrimSpace(extractString(analysis, "threat_level")); level != "" {
			if idx := strings.LastIndex(level, "."); idx >= 0 && idx < len(level)-1 {
				level = level[idx+1:]
			}
			threatType = strings.ToLower(level)
		}
	}
	if threatType == "" && schemaVersion != "" {
		threatType = strings.ReplaceAll(strings.TrimSpace(schemaVersion), ".", "_")
	}

	if detectedAt <= 0 {
		detectedAt = defaultTimestamp
	}

	source = strings.TrimSpace(source)
	if source == "" {
		source = strings.TrimSpace(sourceToken)
	}
	if source == "" {
		source = strings.TrimSpace(flowKey)
	}
	if source == "" {
		source = strings.TrimSpace(targetToken)
	}
	if source == "" {
		source = "ai-service"
	}

	if title == "" {
		title = formatPersistedThreatTitle(threatType)
	}
	if description == "" {
		description = buildPersistedAnomalyDescription(threatType, confidence, modelVersion)
	}

	if anomalyID == "" {
		anomalyID = hex.EncodeToString(txHash[:])
	}

	return persistedAnomalyRecord{
		anomalyID:       anomalyID,
		threatType:      strings.TrimSpace(threatType),
		severityValue:   severityValue,
		confidence:      confidence,
		title:           title,
		description:     description,
		source:          source,
		modelVersion:    strings.TrimSpace(modelVersion),
		flowKey:         strings.TrimSpace(flowKey),
		flowID:          flowID,
		sensorID:        sensorID,
		validatorID:     validatorID,
		scopeIdentifier: scopeIdentifier,
		sourceEventID:   sourceEventID,
		sourceEventTsMs: sourceEventTsMs,
		sentinelEventID: sentinelEventID,
		blockHeight:     blockHeight,
		txHash:          hex.EncodeToString(txHash[:]),
		detectedAt:      detectedAt,
		rawPayload:      append([]byte(nil), payload...),
	}, true, nil
}

func extractFloat64(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0
		}
		return n
	case float32:
		f := float64(n)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0
		}
		return f
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		if f, err := n.Float64(); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	}
	return 0
}

func formatPersistedThreatTitle(threatType string) string {
	clean := strings.TrimSpace(strings.ReplaceAll(threatType, "_", " "))
	if clean == "" {
		return "Threat Detected"
	}
	parts := strings.Fields(clean)
	for i, part := range parts {
		lower := strings.ToLower(part)
		if lower == "" {
			continue
		}
		parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(parts, " ")
}

func deterministicAnomalyIDFromEventID(eventID string) string {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(eventID))
	raw := make([]byte, 16)
	copy(raw, digest[:16])
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return uuid.UUID(raw).String()
}

func buildPersistedAnomalyDescription(threatType string, confidence float64, modelVersion string) string {
	title := formatPersistedThreatTitle(threatType)
	var builder strings.Builder
	builder.WriteString(title)
	builder.WriteString(" detection")
	if confidence > 0 {
		fmt.Fprintf(&builder, " (confidence %.1f%%)", confidence*100)
	}
	if strings.TrimSpace(modelVersion) != "" {
		fmt.Fprintf(&builder, " using model %s", strings.TrimSpace(modelVersion))
	}
	return builder.String()
}

func (a *adapter) upsertAnomalies(ctx context.Context, tx *sql.Tx, rows []persistedAnomalyRecord) error {
	if a == nil || tx == nil || !a.anomalyStoreEnabled || len(rows) == 0 {
		return nil
	}
	prepared := make([]persistedAnomalyRecord, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.anomalyID) != "" {
			prepared = append(prepared, row)
		}
	}
	if len(prepared) == 0 {
		return nil
	}

	chunkSize := anomalyUpsertBatchSize
	if chunkSize < 1 {
		chunkSize = 1
	}
	if chunkSize > maxUpsertBatchSize {
		chunkSize = maxUpsertBatchSize
	}

	for start := 0; start < len(prepared); start += chunkSize {
		end := start + chunkSize
		if end > len(prepared) {
			end = len(prepared)
		}
		chunk := prepared[start:end]

		args := make([]interface{}, 0, len(chunk)*20)
		for _, row := range chunk {
			args = append(args,
				row.anomalyID,
				row.threatType,
				row.severityValue,
				row.confidence,
				row.title,
				row.description,
				row.source,
				row.modelVersion,
				row.flowKey,
				row.flowID,
				row.sensorID,
				row.validatorID,
				row.scopeIdentifier,
				row.sourceEventID,
				row.sourceEventTsMs,
				row.sentinelEventID,
				row.blockHeight,
				row.txHash,
				row.detectedAt,
				row.rawPayload,
			)
		}

		if _, err := tx.ExecContext(ctx, a.anomalyUpsertTemplate(len(chunk)), args...); err != nil {
			return err
		}
	}
	return nil
}

func estimateAnomalyPersistBudget(rowCount int) time.Duration {
	if rowCount <= 0 {
		return 0
	}
	// Conservative wall-time estimate for batched anomaly writes.
	// Base accounts for planning/network overhead; per-row covers payload cost.
	budget := 400*time.Millisecond + time.Duration(rowCount)*12*time.Millisecond
	if budget > 10*time.Second {
		budget = 10 * time.Second
	}
	return budget
}

func shouldSkipAnomalyPersistForDeadline(ctx context.Context, rowCount int) bool {
	if rowCount <= 0 {
		return false
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return false
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return true
	}
	required := estimateAnomalyPersistBudget(rowCount) + anomalyPersistCommitReserve
	return remaining < required
}

func estimateLifecycleAuditBudget(eventCount int) time.Duration {
	if eventCount <= 0 {
		return 0
	}
	// Conservative estimate for batched lifecycle journal insert.
	budget := 80*time.Millisecond + time.Duration(eventCount)*6*time.Millisecond
	if budget > 3*time.Second {
		budget = 3 * time.Second
	}
	return budget
}

func shouldSkipLifecycleAuditForDeadline(ctx context.Context, eventCount int) bool {
	if eventCount <= 0 {
		return false
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return false
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return true
	}
	required := estimateLifecycleAuditBudget(eventCount) + lifecycleAuditCommitReserve
	return remaining < required
}

func extractRootString(payload []byte, key string) string {
	if len(payload) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return ""
	}
	return extractString(raw, key)
}

func (a *adapter) txBatchRuntime() (enabled bool, chunkSize int, mode string) {
	if a == nil {
		return true, txUpsertBatchSize, "batch"
	}
	if !a.perf.txBatchEnabled {
		return false, 1, "disabled"
	}
	if a.perf.canaryAutoFallback && a.txBatchCanary != nil && a.txBatchCanary.isFallbackActive(time.Now()) {
		return false, 1, "fallback_single"
	}
	size := a.perf.txBatchSize
	if a.perf.txBatchAdaptive && a.batchTuner != nil {
		size = a.batchTuner.txSize(a.perf.txBatchSize)
	}
	if size < 1 {
		size = 1
	}
	if a.perf.txBatchAdaptive {
		return true, size, "adaptive_batch"
	}
	return true, size, "batch"
}

func (a *adapter) outboxBatchRuntime() (enabled bool, chunkSize int, mode string) {
	if a == nil {
		return true, outboxUpsertBatchSize, "batch"
	}
	if !a.perf.outboxBatchEnabled {
		return false, 1, "disabled"
	}
	if a.perf.canaryAutoFallback && a.outboxBatchCanary != nil && a.outboxBatchCanary.isFallbackActive(time.Now()) {
		return false, 1, "fallback_single"
	}
	size := a.perf.outboxBatchSize
	if a.perf.outboxBatchAdaptive && a.batchTuner != nil {
		size = a.batchTuner.outboxSize(a.perf.outboxBatchSize)
	}
	if size < 1 {
		size = 1
	}
	if a.perf.outboxBatchAdaptive {
		return true, size, "adaptive_batch"
	}
	return true, size, "batch"
}

func classifyCanaryReason(err error) string {
	if err == nil {
		return "none"
	}
	switch {
	case errors.Is(err, ErrIntegrityViolation):
		return "integrity"
	case errors.Is(err, ErrInvalidData):
		return "invalid"
	case IsRetryable(err):
		return "retryable"
	default:
		return "other"
	}
}

func (a *adapter) recordTxBatchCanaryOutcome(err error, usedBatch bool) {
	if a == nil || a.txBatchCanary == nil || !a.perf.canaryAutoFallback || !usedBatch {
		return
	}
	reason := classifyCanaryReason(err)
	if a.metrics != nil && reason != "none" {
		a.metrics.observeTxBatchCanaryBad(reason)
	}
	// Security and availability split:
	// - integrity/invalid errors still fail closed
	// - only transient retryable errors trigger auto-fallback mode
	bad := reason == "retryable"
	activated := a.txBatchCanary.recordSample(
		time.Now(),
		bad,
		a.perf.canaryMinSamples,
		a.perf.canaryMaxErrorRate,
		a.perf.canaryFallbackCooldown,
	)
	if activated && a.logger != nil {
		a.logger.Warn("tx upsert switched to safe single-row mode",
			utils.ZapFloat64("max_error_rate", a.perf.canaryMaxErrorRate),
			utils.ZapInt("window_size", a.perf.canaryWindowSize),
			utils.ZapInt("min_samples", a.perf.canaryMinSamples),
			utils.ZapDuration("cooldown", a.perf.canaryFallbackCooldown))
	}
}

func (a *adapter) recordOutboxBatchCanaryOutcome(err error, usedBatch bool) {
	if a == nil || a.outboxBatchCanary == nil || !a.perf.canaryAutoFallback || !usedBatch {
		return
	}
	reason := classifyCanaryReason(err)
	if a.metrics != nil && reason != "none" {
		a.metrics.observeOutboxBatchCanaryBad(reason)
	}
	// Security and availability split:
	// - integrity/invalid errors still fail closed
	// - only transient retryable errors trigger auto-fallback mode
	bad := reason == "retryable"
	activated := a.outboxBatchCanary.recordSample(
		time.Now(),
		bad,
		a.perf.canaryMinSamples,
		a.perf.canaryMaxErrorRate,
		a.perf.canaryFallbackCooldown,
	)
	if activated && a.logger != nil {
		a.logger.Warn("outbox upsert switched to safe single-row mode",
			utils.ZapFloat64("max_error_rate", a.perf.canaryMaxErrorRate),
			utils.ZapInt("window_size", a.perf.canaryWindowSize),
			utils.ZapInt("min_samples", a.perf.canaryMinSamples),
			utils.ZapDuration("cooldown", a.perf.canaryFallbackCooldown))
	}
}

func (a *adapter) tuneBatchSizes(txConflicts, txRows int, txErr error, outboxRows int, outboxErr error) {
	if a == nil || a.batchTuner == nil {
		return
	}
	if a.perf.txBatchAdaptive {
		current := a.batchTuner.txSize(a.perf.txBatchSize)
		next := current
		switch {
		case txErr != nil && IsRetryable(txErr):
			next = maxInt(a.perf.txBatchMinSize, current/2)
		case txRows > 0 && txConflicts*5 > txRows: // >20% conflicts
			next = maxInt(a.perf.txBatchMinSize, current/2)
		case txErr == nil && txConflicts*20 < maxInt(txRows, 1): // <5% conflicts
			next = minInt(a.perf.txBatchSize, current+a.perf.txBatchScaleStep)
		}
		if next < current {
			a.batchTuner.scaleDownTx()
		}
		if next > current {
			a.batchTuner.scaleUpTx()
		}
		a.batchTuner.setTxSize(next)
	}
	if a.perf.outboxBatchAdaptive {
		current := a.batchTuner.outboxSize(a.perf.outboxBatchSize)
		next := current
		switch {
		case outboxErr != nil && IsRetryable(outboxErr):
			next = maxInt(a.perf.outboxBatchMinSize, current/2)
		case outboxErr == nil && outboxRows > 0:
			next = minInt(a.perf.outboxBatchSize, current+a.perf.outboxBatchScaleStep)
		}
		if next < current {
			a.batchTuner.scaleDownOutbox()
		}
		if next > current {
			a.batchTuner.scaleUpOutbox()
		}
		a.batchTuner.setOutboxSize(next)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a *adapter) upsertTransactions(ctx context.Context, tx *sql.Tx, blk *block.AppBlock, receipts []state.Receipt, persistAttempt int) (retErr error) {
	transactions := blk.Transactions()
	if len(transactions) != len(receipts) {
		return fmt.Errorf("%w: transaction count mismatch", ErrInvalidData)
	}
	if len(transactions) == 0 {
		return nil
	}
	upsertTransactionsStart := time.Now()
	beforeVerifyStart := upsertTransactionsStart
	afterVerifyStart := time.Time{}
	verifyStarted := false
	verifyCompleted := false
	defer func() {
		if a == nil || a.metrics == nil {
			return
		}
		totalDur := time.Since(upsertTransactionsStart)
		a.metrics.observePersistStage("upsert_transactions_total_inner", totalDur)
		if verifyCompleted && !afterVerifyStart.IsZero() {
			a.metrics.observePersistStage("upsert_transactions_after_verify", time.Since(afterVerifyStart))
		}
		if !verifyStarted {
			a.metrics.observePersistDiagnosticSignal("upsert_transactions_verify_skipped")
		}
		if retErr == nil {
			a.metrics.observePersistDiagnosticSignal("upsert_transactions_return_success")
			a.metrics.observePersistStage("upsert_transactions_return_success", totalDur)
			return
		}
		if errors.Is(retErr, ErrIntegrityViolation) {
			a.metrics.observePersistDiagnosticSignal("upsert_transactions_return_integrity_error")
			a.metrics.observePersistStage("upsert_transactions_return_integrity_error", totalDur)
			return
		}
		a.metrics.observePersistDiagnosticSignal("upsert_transactions_return_other_error")
		a.metrics.observePersistStage("upsert_transactions_return_other_error", totalDur)
	}()
	_, txChunkSize, txMode := a.txBatchRuntime()
	outboxBatchEnabled, outboxChunkSize, outboxMode := a.outboxBatchRuntime()
	usedTxBatch := txMode == "batch" || txMode == "adaptive_batch"
	usedOutboxBatch := outboxMode == "batch" || outboxMode == "adaptive_batch"
	totalConflicts := 0
	totalPayloadBytes := 0
	rowCount := len(transactions)
	policyRowCount := 0
	var outboxErr error
	defer func() {
		if a.metrics != nil {
			a.metrics.observeTxUpsertStats(rowCount, totalConflicts, totalPayloadBytes)
			a.metrics.observeTxBatchMode(txMode)
			a.metrics.observeOutboxBatchMode(outboxMode)
		}
		a.tuneBatchSizes(totalConflicts, rowCount, retErr, policyRowCount, outboxErr)
		a.recordTxBatchCanaryOutcome(retErr, usedTxBatch)
		a.recordOutboxBatchCanaryOutcome(outboxErr, usedOutboxBatch)
	}()

	records := make([]persistedTxRecord, 0, len(transactions))
	policyRows := make([]policyOutboxInput, 0, len(transactions))
	anomalyRows := make([]persistedAnomalyRecord, 0, len(transactions))
	eventTxCount := 0
	anomalyParseFailures := 0
	seenTxHash := make(map[[32]byte]persistedTxRecord, len(transactions))
	prepareStart := time.Now()
	blockTS := blk.GetTimestamp().Unix()
	for i, receipt := range receipts {
		if i >= len(transactions) {
			return fmt.Errorf("%w: receipt index out of bounds", ErrInvalidData)
		}
		stateTx := transactions[i]
		envelope := stateTx.Envelope()

		payloadJSON, err := a.marshalStoredTxPayload(stateTx)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction payload: %w", err)
		}

		var custodyChainJSON []byte
		if evidenceTx, ok := stateTx.(*state.EvidenceTx); ok && len(evidenceTx.CoC) > 0 {
			custodyChainJSON, err = json.Marshal(evidenceTx.CoC)
			if err != nil {
				return fmt.Errorf("failed to marshal custody chain: %w", err)
			}
		}

		status := "success"
		errorMsg := ""
		if receipt.Error != "" {
			status = "failed"
			errorMsg = receipt.Error
		}

		contentHash := envelope.ContentHash
		var domain string
		switch stateTx.Type() {
		case state.TxEvent:
			domain = state.DomainEventTx
		case state.TxEvidence:
			domain = state.DomainEvidenceTx
		case state.TxPolicy:
			domain = state.DomainPolicyTx
		default:
			return fmt.Errorf("%w: unknown tx type %q", ErrInvalidData, stateTx.Type())
		}
		signBytes, err := state.BuildSignBytes(domain, stateTx.Timestamp(), envelope.ProducerID, envelope.Nonce, contentHash)
		if err != nil {
			return fmt.Errorf("build tx id: %w", err)
		}
		txHash := state.HashBytes(signBytes)

		rec := persistedTxRecord{
			txHash:       txHash,
			blockHeight:  blk.GetHeight(),
			txIndex:      i,
			txType:       string(stateTx.Type()),
			producerID:   envelope.ProducerID,
			nonce:        envelope.Nonce,
			contentHash:  contentHash,
			algorithm:    envelope.Alg,
			publicKey:    envelope.PubKey,
			signature:    envelope.Signature,
			payloadJSON:  payloadJSON,
			custodyChain: custodyChainJSON,
			status:       status,
			errorMsg:     errorMsg,
			submittedAt:  time.Unix(stateTx.Timestamp(), 0),
		}
		totalPayloadBytes += len(payloadJSON) + len(custodyChainJSON)
		// Fail closed if the same tx_hash appears more than once in a block.
		// In batch mode, duplicate hashes could otherwise be misclassified as inserted and
		// skip location verification for the later duplicate row.
		if prev, exists := seenTxHash[rec.txHash]; exists {
			sameLocation := prev.blockHeight == rec.blockHeight && prev.txIndex == rec.txIndex
			sameEnvelope := bytes.Equal(prev.producerID, rec.producerID) &&
				bytes.Equal(prev.nonce, rec.nonce) &&
				prev.contentHash == rec.contentHash &&
				prev.algorithm == rec.algorithm &&
				bytes.Equal(prev.publicKey, rec.publicKey) &&
				bytes.Equal(prev.signature, rec.signature)
			if !sameLocation || !sameEnvelope {
				if a.auditLogger != nil {
					_ = a.auditLogger.Security("duplicate_tx_hash_in_block_detected", map[string]interface{}{
						"height":           blk.GetHeight(),
						"tx_hash":          fmt.Sprintf("%x", rec.txHash[:]),
						"prev_tx_index":    prev.txIndex,
						"current_tx_index": rec.txIndex,
					})
				}
				return fmt.Errorf("%w: duplicate tx_hash in block at height %d indexes %d and %d", ErrIntegrityViolation, blk.GetHeight(), prev.txIndex, rec.txIndex)
			}
			return fmt.Errorf("%w: duplicate tx_hash in block at height %d index %d", ErrIntegrityViolation, blk.GetHeight(), rec.txIndex)
		}
		seenTxHash[rec.txHash] = rec
		if stateTx.Type() == state.TxPolicy {
			policyTx, ok := stateTx.(*state.PolicyTx)
			if !ok || len(policyTx.Data) == 0 {
				return fmt.Errorf("%w: policy tx payload missing", ErrInvalidData)
			}
			policyRows = append(policyRows, policyOutboxInput{
				blockHeight: blk.GetHeight(),
				blockTS:     blockTS,
				txIndex:     i,
				txTS:        policyTx.Timestamp(),
				payload:     policyTx.Data,
			})
			policyRowCount++
		}
		if a.anomalyStoreEnabled && stateTx.Type() == state.TxEvent {
			eventTxCount++
			if eventTx, ok := stateTx.(*state.EventTx); ok && len(eventTx.Data) > 0 {
				if anomaly, recognized, anomalyErr := parsePersistedAnomaly(eventTx.Data, txHash, blk.GetHeight(), eventTx.Timestamp()); anomalyErr != nil {
					anomalyParseFailures++
					if a.logger != nil {
						a.logger.WarnContext(ctx, "failed to parse event tx for anomaly persistence",
							utils.ZapUint64("height", blk.GetHeight()),
							utils.ZapInt("tx_index", i),
							utils.ZapString("tx_hash", hex.EncodeToString(txHash[:])),
							utils.ZapError(anomalyErr))
					}
				} else if recognized {
					anomalyRows = append(anomalyRows, anomaly)
				}
			}
		}
		records = append(records, rec)
	}
	if a.logger != nil && a.anomalyStoreEnabled && eventTxCount > 0 {
		a.logger.InfoContext(ctx, "anomaly persistence scan completed",
			utils.ZapUint64("height", blk.GetHeight()),
			utils.ZapInt("event_tx_count", eventTxCount),
			utils.ZapInt("recognized_anomaly_rows", len(anomalyRows)),
			utils.ZapInt("parse_failures", anomalyParseFailures))
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("upsert_transactions_prepare", time.Since(prepareStart))
	}

	insertTotal := time.Duration(0)
	verifyTotal := time.Duration(0)
	conflictListBuildTotal := time.Duration(0)
	conflictsAll := make([]persistedTxRecord, 0, len(records))
	for start := 0; start < len(records); start += txChunkSize {
		end := start + txChunkSize
		if end > len(records) {
			end = len(records)
		}
		chunk := records[start:end]

		args := make([]interface{}, 0, len(chunk)*15)
		for _, rec := range chunk {
			args = append(args,
				rec.txHash[:],
				rec.blockHeight,
				rec.txIndex,
				rec.txType,
				rec.producerID,
				rec.nonce,
				rec.contentHash[:],
				rec.algorithm,
				rec.publicKey,
				rec.signature,
				rec.payloadJSON,
				rec.custodyChain,
				rec.status,
				rec.errorMsg,
				rec.submittedAt,
			)
		}

		inserted := make(map[[32]byte]struct{}, len(chunk))
		insertQueryStart := time.Now()
		insertRows, err := tx.QueryContext(ctx, a.txInsertTemplate(len(chunk)), args...)
		insertQueryDur := time.Since(insertQueryStart)
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_insert_query", insertQueryDur)
		}
		if err != nil {
			if IsRetryable(err) && len(chunk) > 1 {
				if a.logger != nil {
					a.logger.WarnContext(ctx, "batch tx insert timed out/retryable; splitting chunk for fallback",
						utils.ZapUint64("height", blk.GetHeight()),
						utils.ZapInt("chunk_size", len(chunk)),
						utils.ZapError(err))
				}
				fallbackInserted, fallbackErr := a.insertTxChunkAdaptive(ctx, tx, chunk)
				if fallbackErr != nil {
					return fmt.Errorf("failed to batch insert transactions at height %d: %w", blk.GetHeight(), fallbackErr)
				}
				inserted = fallbackInserted
			} else {
				return fmt.Errorf("failed to batch insert transactions at height %d: %w", blk.GetHeight(), err)
			}
		} else {
			insertScanStart := time.Now()
			for insertRows.Next() {
				var b []byte
				if scanErr := insertRows.Scan(&b); scanErr != nil {
					insertRows.Close()
					if a.metrics != nil {
						a.metrics.observePersistStage("upsert_transactions_insert_scan", time.Since(insertScanStart))
					}
					return fmt.Errorf("failed scanning inserted tx hash: %w", scanErr)
				}
				if len(b) != 32 {
					insertRows.Close()
					if a.metrics != nil {
						a.metrics.observePersistStage("upsert_transactions_insert_scan", time.Since(insertScanStart))
					}
					return fmt.Errorf("%w: inserted tx hash invalid length", ErrIntegrityViolation)
				}
				var h [32]byte
				copy(h[:], b)
				inserted[h] = struct{}{}
			}
			if err := insertRows.Err(); err != nil {
				insertRows.Close()
				if a.metrics != nil {
					a.metrics.observePersistStage("upsert_transactions_insert_scan", time.Since(insertScanStart))
				}
				return fmt.Errorf("failed reading inserted tx hashes: %w", err)
			}
			insertRows.Close()
			insertScanDur := time.Since(insertScanStart)
			if a.metrics != nil {
				a.metrics.observePersistStage("upsert_transactions_insert_scan", insertScanDur)
			}
			insertTotal += insertQueryDur + insertScanDur
		}

		conflictListBuildStart := time.Now()
		conflicts := make([]persistedTxRecord, 0, len(chunk))
		for _, rec := range chunk {
			if _, ok := inserted[rec.txHash]; !ok {
				conflicts = append(conflicts, rec)
			}
		}
		conflictListBuildTotal += time.Since(conflictListBuildStart)
		if len(conflicts) == 0 {
			continue
		}
		totalConflicts += len(conflicts)

		conflictsAll = append(conflictsAll, conflicts...)
	}
	if len(conflictsAll) > 0 {
		if a.metrics != nil {
			a.metrics.observePersistContentionSignal("verify_conflicts")
			a.metrics.observePersistDiagnosticSignal("upsert_transactions_verify_called")
		}
		verifyStarted = true
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_before_verify", time.Since(beforeVerifyStart))
		}
		verifyStart := time.Now()
		if err := a.verifyTxConflicts(ctx, tx, blk, conflictsAll); err != nil {
			return err
		}
		verifyTotal += time.Since(verifyStart)
		afterVerifyStart = time.Now()
		verifyCompleted = true
		if a.metrics != nil && verifyTotal >= 500*time.Millisecond {
			a.metrics.observePersistContentionSignal("verify_conflicts_slow")
		}
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("upsert_transactions_insert_chunks", insertTotal)
		a.metrics.observePersistStage("upsert_transactions_conflict_list_build", conflictListBuildTotal)
		if verifyTotal > 0 {
			a.metrics.observePersistStage("upsert_transactions_verify_conflicts", verifyTotal)
		}
	}

	if len(policyRows) > 0 {
		outboxStart := time.Now()
		if outboxBatchEnabled {
			if err := a.upsertPolicyOutboxWithAdaptiveFallback(ctx, tx, policyRows, outboxChunkSize, persistAttempt, blk.GetHeight()); err != nil {
				outboxErr = err
				return fmt.Errorf("upsert policy outbox: %w", err)
			}
		} else {
			for _, row := range policyRows {
				if err := a.upsertPolicyOutbox(ctx, tx, row.blockHeight, row.blockTS, row.txTS, row.txIndex, row.payload, persistAttempt); err != nil {
					outboxErr = err
					return fmt.Errorf("upsert policy outbox: %w", err)
				}
			}
		}
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_outbox_batch", time.Since(outboxStart))
		}
	}
	if len(anomalyRows) > 0 {
		if a.logger != nil {
			a.logger.InfoContext(ctx, "persisting anomaly rows",
				utils.ZapUint64("height", blk.GetHeight()),
				utils.ZapInt("anomaly_row_count", len(anomalyRows)))
		}
		if shouldSkipAnomalyPersistForDeadline(ctx, len(anomalyRows)) {
			if a.logger != nil {
				deadline, _ := ctx.Deadline()
				a.logger.WarnContext(ctx, "skipping anomaly persistence to preserve durable policy commit budget",
					utils.ZapUint64("height", blk.GetHeight()),
					utils.ZapInt("anomaly_row_count", len(anomalyRows)),
					utils.ZapDuration("estimated_budget", estimateAnomalyPersistBudget(len(anomalyRows))),
					utils.ZapDuration("remaining", time.Until(deadline)))
			}
			if a.auditLogger != nil {
				_ = a.auditLogger.Warn("anomaly_persist_skipped_due_to_deadline_budget", map[string]interface{}{
					"height":          blk.GetHeight(),
					"anomaly_rows":    len(anomalyRows),
					"estimated_ms":    estimateAnomalyPersistBudget(len(anomalyRows)).Milliseconds(),
					"commit_reserve":  anomalyPersistCommitReserve.Milliseconds(),
					"persist_attempt": persistAttempt,
				})
			}
			return nil
		}
		if err := a.upsertAnomalies(ctx, tx, anomalyRows); err != nil {
			return fmt.Errorf("upsert anomalies: %w", err)
		}
	}
	return nil
}

func (a *adapter) upsertPolicyOutboxWithAdaptiveFallback(ctx context.Context, tx *sql.Tx, rows []policyOutboxInput, initialChunkSize int, persistAttempt int, height uint64) error {
	if len(rows) == 0 {
		return nil
	}
	chunkSize := initialChunkSize
	if chunkSize < 1 {
		chunkSize = outboxUpsertBatchSize
	}
	if chunkSize > maxUpsertBatchSize {
		chunkSize = maxUpsertBatchSize
	}
	const minAdaptiveChunk = 8
	for {
		err := a.upsertPolicyOutboxBatch(ctx, tx, rows, chunkSize, persistAttempt)
		if err == nil {
			return nil
		}
		if !IsRetryable(err) {
			return err
		}
		if chunkSize <= minAdaptiveChunk {
			if a.logger != nil {
				a.logger.WarnContext(ctx, "outbox batch upsert retryable failure at minimum adaptive chunk; falling back to single-row upsert",
					utils.ZapUint64("height", height),
					utils.ZapInt("policy_rows", len(rows)),
					utils.ZapInt("chunk_size", chunkSize),
					utils.ZapError(err))
			}
			break
		}
		nextChunk := chunkSize / 2
		if nextChunk < minAdaptiveChunk {
			nextChunk = minAdaptiveChunk
		}
		if a.logger != nil {
			a.logger.WarnContext(ctx, "outbox batch upsert retryable failure; reducing chunk size",
				utils.ZapUint64("height", height),
				utils.ZapInt("policy_rows", len(rows)),
				utils.ZapInt("from_chunk", chunkSize),
				utils.ZapInt("to_chunk", nextChunk),
				utils.ZapError(err))
		}
		chunkSize = nextChunk
	}
	for _, row := range rows {
		if err := a.upsertPolicyOutbox(ctx, tx, row.blockHeight, row.blockTS, row.txTS, row.txIndex, row.payload, persistAttempt); err != nil {
			return err
		}
	}
	return nil
}

func (a *adapter) insertTxChunkAdaptive(ctx context.Context, tx *sql.Tx, chunk []persistedTxRecord) (map[[32]byte]struct{}, error) {
	inserted := make(map[[32]byte]struct{}, len(chunk))
	if len(chunk) == 0 {
		return inserted, nil
	}

	args := make([]interface{}, 0, len(chunk)*15)
	for _, rec := range chunk {
		args = append(args,
			rec.txHash[:],
			rec.blockHeight,
			rec.txIndex,
			rec.txType,
			rec.producerID,
			rec.nonce,
			rec.contentHash[:],
			rec.algorithm,
			rec.publicKey,
			rec.signature,
			rec.payloadJSON,
			rec.custodyChain,
			rec.status,
			rec.errorMsg,
			rec.submittedAt,
		)
	}

	rows, err := tx.QueryContext(ctx, a.txInsertTemplate(len(chunk)), args...)
	if err != nil {
		if IsRetryable(err) && len(chunk) > 1 {
			mid := len(chunk) / 2
			left, leftErr := a.insertTxChunkAdaptive(ctx, tx, chunk[:mid])
			if leftErr != nil {
				return nil, leftErr
			}
			right, rightErr := a.insertTxChunkAdaptive(ctx, tx, chunk[mid:])
			if rightErr != nil {
				return nil, rightErr
			}
			for h := range left {
				inserted[h] = struct{}{}
			}
			for h := range right {
				inserted[h] = struct{}{}
			}
			return inserted, nil
		}
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var b []byte
		if scanErr := rows.Scan(&b); scanErr != nil {
			return nil, fmt.Errorf("failed scanning inserted tx hash: %w", scanErr)
		}
		if len(b) != 32 {
			return nil, fmt.Errorf("%w: inserted tx hash invalid length", ErrIntegrityViolation)
		}
		var h [32]byte
		copy(h[:], b)
		inserted[h] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed reading inserted tx hashes: %w", err)
	}
	return inserted, nil
}

func (a *adapter) verifyTxConflicts(ctx context.Context, tx *sql.Tx, blk *block.AppBlock, conflicts []persistedTxRecord) error {
	if len(conflicts) == 0 {
		return nil
	}
	verifyTotalStart := time.Now()
	defer func() {
		if a != nil && a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_verify_total", time.Since(verifyTotalStart))
		}
	}()
	verifyChunkSize := maxUpsertBatchSize
	if a != nil && a.perf.txVerifyChunkSize > 0 {
		verifyChunkSize = a.perf.txVerifyChunkSize
	}
	queryRunner := interface {
		QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	}(tx)
	useOutOfTx := false
	if a != nil && !a.perf.txVerifyUseTx {
		queryRunner = a.db
		useOutOfTx = true
		if a.metrics != nil {
			a.metrics.observePersistContentionSignal("verify_mode_out_of_tx")
		}
	}
	for start := 0; start < len(conflicts); start += verifyChunkSize {
		end := start + verifyChunkSize
		if end > len(conflicts) {
			end = len(conflicts)
		}
		chunk := conflicts[start:end]
		verifyArgs := make([]interface{}, 0, len(chunk))
		for _, rec := range chunk {
			verifyArgs = append(verifyArgs, rec.txHash[:])
		}

		existing := make(map[[32]byte]existingTxRecord, len(chunk))
		verifyQueryStart := time.Now()
		rows, err := queryRunner.QueryContext(ctx, a.txVerifyTemplate(len(chunk)), verifyArgs...)
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_verify_query", time.Since(verifyQueryStart))
		}
		if err != nil {
			return fmt.Errorf("failed to batch verify existing transactions: %w", err)
		}
		verifyScanStart := time.Now()
		for rows.Next() {
			var txHashRaw []byte
			var contentHashRaw []byte
			var producerID []byte
			var nonce []byte
			var blockHeight uint64
			var txIndex int
			var algorithm string
			var publicKey []byte
			var signature []byte
			if scanErr := rows.Scan(&txHashRaw, &contentHashRaw, &producerID, &nonce, &blockHeight, &txIndex, &algorithm, &publicKey, &signature); scanErr != nil {
				rows.Close()
				if a.metrics != nil {
					a.metrics.observePersistStage("upsert_transactions_verify_scan", time.Since(verifyScanStart))
				}
				return fmt.Errorf("failed scanning existing transaction row: %w", scanErr)
			}
			if len(txHashRaw) != 32 || len(contentHashRaw) != 32 {
				rows.Close()
				if a.metrics != nil {
					a.metrics.observePersistStage("upsert_transactions_verify_scan", time.Since(verifyScanStart))
				}
				return fmt.Errorf("%w: existing tx hash/content_hash invalid length", ErrIntegrityViolation)
			}
			var txHash [32]byte
			var contentHash [32]byte
			copy(txHash[:], txHashRaw)
			copy(contentHash[:], contentHashRaw)
			existing[txHash] = existingTxRecord{
				contentHash: contentHash,
				producerID:  producerID,
				nonce:       nonce,
				blockHeight: blockHeight,
				txIndex:     txIndex,
				algorithm:   algorithm,
				publicKey:   publicKey,
				signature:   signature,
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			if a.metrics != nil {
				a.metrics.observePersistStage("upsert_transactions_verify_scan", time.Since(verifyScanStart))
			}
			return fmt.Errorf("failed reading existing transaction rows: %w", err)
		}
		rows.Close()
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_verify_scan", time.Since(verifyScanStart))
		}

		verifyCompareStart := time.Now()
		for _, rec := range chunk {
			ex, ok := existing[rec.txHash]
			if !ok && useOutOfTx {
				// Out-of-transaction verify can miss rows that are only visible inside the active tx.
				// Recheck once in-tx before failing integrity.
				recheckStart := time.Now()
				exInTx, foundInTx, recheckErr := a.recheckTxConflictInTx(ctx, tx, rec.txHash)
				if a.metrics != nil {
					a.metrics.observePersistStage("upsert_transactions_verify_recheck_in_tx", time.Since(recheckStart))
				}
				if recheckErr != nil {
					if a.metrics != nil {
						a.metrics.observePersistDiagnosticSignal("verify_compare_error")
						a.metrics.observePersistStage("upsert_transactions_verify_compare_error", time.Since(verifyCompareStart))
						a.metrics.observePersistStage("upsert_transactions_verify_compare_total", time.Since(verifyCompareStart))
					}
					return recheckErr
				}
				if foundInTx {
					ex = exInTx
					ok = true
					if a.metrics != nil {
						a.metrics.observePersistContentionSignal("verify_out_of_tx_fallback_hit")
					}
				}
			}
			if !ok {
				if useOutOfTx && a.metrics != nil {
					a.metrics.observePersistContentionSignal("verify_out_of_tx_fallback_miss")
				}
				if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("verify_compare_error")
					a.metrics.observePersistDiagnosticSignal("verify_existing_row_missing")
					a.metrics.observePersistIntegrityKind("missing_after_conflict")
					a.metrics.observePersistStage("upsert_transactions_verify_compare_error", time.Since(verifyCompareStart))
					a.metrics.observePersistStage("upsert_transactions_verify_compare_total", time.Since(verifyCompareStart))
				}
				return fmt.Errorf("%w: missing transaction row after tx_hash conflict at height %d, index %d", ErrIntegrityViolation, blk.GetHeight(), rec.txIndex)
			}
			if rec.contentHash != ex.contentHash {
				if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("verify_compare_error")
					a.metrics.observePersistDiagnosticSignal("verify_content_mismatch")
					a.metrics.observePersistIntegrityKind("content_hash_mismatch")
					a.metrics.observePersistStage("upsert_transactions_verify_compare_error", time.Since(verifyCompareStart))
					a.metrics.observePersistStage("upsert_transactions_verify_compare_total", time.Since(verifyCompareStart))
				}
				if a.auditLogger != nil {
					_ = a.auditLogger.Security("transaction_hash_mismatch_detected", map[string]interface{}{
						"height":        blk.GetHeight(),
						"tx_index":      rec.txIndex,
						"new_hash":      fmt.Sprintf("%x", rec.contentHash[:]),
						"existing_hash": fmt.Sprintf("%x", ex.contentHash[:]),
					})
				}
				return fmt.Errorf("%w: transaction content_hash mismatch at height %d, index %d", ErrIntegrityViolation, blk.GetHeight(), rec.txIndex)
			}
			if len(ex.producerID) == 0 || len(ex.nonce) == 0 {
				if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("verify_compare_error")
					a.metrics.observePersistIntegrityKind("producer_nonce_invalid_existing")
					a.metrics.observePersistStage("upsert_transactions_verify_compare_error", time.Since(verifyCompareStart))
					a.metrics.observePersistStage("upsert_transactions_verify_compare_total", time.Since(verifyCompareStart))
				}
				return fmt.Errorf("%w: existing tx producer_id/nonce invalid", ErrIntegrityViolation)
			}
			if !bytes.Equal(ex.producerID, rec.producerID) {
				if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("verify_compare_error")
					a.metrics.observePersistIntegrityKind("producer_id_mismatch")
					a.metrics.observePersistStage("upsert_transactions_verify_compare_error", time.Since(verifyCompareStart))
					a.metrics.observePersistStage("upsert_transactions_verify_compare_total", time.Since(verifyCompareStart))
				}
				return fmt.Errorf("%w: transaction producer_id mismatch at height %d, index %d", ErrIntegrityViolation, blk.GetHeight(), rec.txIndex)
			}
			if !bytes.Equal(ex.nonce, rec.nonce) {
				if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("verify_compare_error")
					a.metrics.observePersistIntegrityKind("nonce_mismatch")
					a.metrics.observePersistStage("upsert_transactions_verify_compare_error", time.Since(verifyCompareStart))
					a.metrics.observePersistStage("upsert_transactions_verify_compare_total", time.Since(verifyCompareStart))
				}
				return fmt.Errorf("%w: transaction nonce mismatch at height %d, index %d", ErrIntegrityViolation, blk.GetHeight(), rec.txIndex)
			}
			if ex.blockHeight != rec.blockHeight || ex.txIndex != rec.txIndex {
				locationMismatchStart := time.Now()
				kind := txLocationMismatchKind(ex.blockHeight != rec.blockHeight, ex.txIndex != rec.txIndex)
				if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("verify_compare_error")
					a.metrics.observePersistDiagnosticSignal("verify_location_mismatch_entered")
					a.metrics.observeTxLocationMismatch("upsert_transactions_verify_conflicts", kind)
					a.metrics.observePersistIntegrityKind("location_mismatch_" + kind)
				}
				// Tolerate exact replay conflicts only when the tx is already anchored in an older block.
				// This prevents stale/replayed transactions from fail-closing new block persistence while
				// preserving fail-closed behavior for same-height or backward-location conflicts.
				if ex.blockHeight < rec.blockHeight {
					if a.metrics != nil {
						a.metrics.observePersistDiagnosticSignal("verify_location_mismatch_replay_tolerated")
					}
					if a.logger != nil {
						a.logger.Warn("transaction replay already anchored in prior block; keeping existing location",
							utils.ZapString("tx_hash", fmt.Sprintf("%x", rec.txHash[:])),
							utils.ZapUint64("incoming_block_height", rec.blockHeight),
							utils.ZapInt("incoming_tx_index", rec.txIndex),
							utils.ZapUint64("existing_block_height", ex.blockHeight),
							utils.ZapInt("existing_tx_index", ex.txIndex),
							utils.ZapString("mismatch_kind", kind))
					}
					if a.metrics != nil {
						a.metrics.observePersistStage("upsert_transactions_verify_location_mismatch", time.Since(locationMismatchStart))
					}
					continue
				}
				if a.auditLogger != nil {
					auditStart := time.Now()
					_ = a.auditLogger.Security("transaction_location_mismatch_observed", map[string]interface{}{
						"tx_hash":               fmt.Sprintf("%x", rec.txHash[:]),
						"expected_block_height": rec.blockHeight,
						"expected_tx_index":     rec.txIndex,
						"existing_block_height": ex.blockHeight,
						"existing_tx_index":     ex.txIndex,
						"kind":                  kind,
					})
					if a.metrics != nil {
						a.metrics.observePersistStage("upsert_transactions_verify_location_mismatch_audit", time.Since(auditStart))
					}
				}
				if a.shouldLogTxLocationMismatch(rec.txHash) {
					if a.metrics != nil {
						a.metrics.observePersistDiagnosticSignal("verify_forensics_invoked")
					}
					a.logTxLocationMismatchForensics(ctx, tx, blk, rec.txHash, rec.blockHeight, rec.txIndex, ex, kind)
				} else if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("verify_forensics_skipped_dedup")
				}
				if a.metrics != nil {
					a.metrics.observePersistStage("upsert_transactions_verify_location_mismatch", time.Since(locationMismatchStart))
					a.metrics.observePersistStage("upsert_transactions_verify_compare_error", time.Since(verifyCompareStart))
					a.metrics.observePersistStage("upsert_transactions_verify_compare_total", time.Since(verifyCompareStart))
				}
				return fmt.Errorf("%w: transaction location mismatch at height %d, index %d", ErrIntegrityViolation, blk.GetHeight(), rec.txIndex)
			}
			if ex.algorithm != rec.algorithm || !bytes.Equal(ex.publicKey, rec.publicKey) || !bytes.Equal(ex.signature, rec.signature) {
				if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("verify_compare_error")
					a.metrics.observePersistDiagnosticSignal("verify_envelope_mismatch")
					a.metrics.observePersistIntegrityKind("envelope_mismatch")
					a.metrics.observePersistStage("upsert_transactions_verify_compare_error", time.Since(verifyCompareStart))
					a.metrics.observePersistStage("upsert_transactions_verify_compare_total", time.Since(verifyCompareStart))
				}
				return fmt.Errorf("%w: transaction envelope mismatch at height %d, index %d", ErrIntegrityViolation, blk.GetHeight(), rec.txIndex)
			}
		}
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_verify_compare", time.Since(verifyCompareStart))
			a.metrics.observePersistStage("upsert_transactions_verify_compare_total", time.Since(verifyCompareStart))
		}
	}
	return nil
}

func (a *adapter) recheckTxConflictInTx(ctx context.Context, tx *sql.Tx, txHash [32]byte) (existingTxRecord, bool, error) {
	var zero existingTxRecord
	if tx == nil {
		return zero, false, nil
	}
	rows, err := tx.QueryContext(ctx, a.txVerifyTemplate(1), txHash[:])
	if err != nil {
		return zero, false, fmt.Errorf("failed to recheck existing transaction in tx: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return zero, false, fmt.Errorf("failed reading tx recheck rows: %w", err)
		}
		return zero, false, nil
	}

	var txHashRaw []byte
	var contentHashRaw []byte
	var producerID []byte
	var nonce []byte
	var blockHeight uint64
	var txIndex int
	var algorithm string
	var publicKey []byte
	var signature []byte
	if scanErr := rows.Scan(&txHashRaw, &contentHashRaw, &producerID, &nonce, &blockHeight, &txIndex, &algorithm, &publicKey, &signature); scanErr != nil {
		return zero, false, fmt.Errorf("failed scanning tx recheck row: %w", scanErr)
	}
	if len(txHashRaw) != 32 || len(contentHashRaw) != 32 {
		return zero, false, fmt.Errorf("%w: existing tx hash/content_hash invalid length", ErrIntegrityViolation)
	}
	var txHashOut [32]byte
	copy(txHashOut[:], txHashRaw)
	if txHashOut != txHash {
		return zero, false, fmt.Errorf("%w: tx recheck hash mismatch", ErrIntegrityViolation)
	}
	var contentHash [32]byte
	copy(contentHash[:], contentHashRaw)

	return existingTxRecord{
		contentHash: contentHash,
		producerID:  producerID,
		nonce:       nonce,
		blockHeight: blockHeight,
		txIndex:     txIndex,
		algorithm:   algorithm,
		publicKey:   publicKey,
		signature:   signature,
	}, true, nil
}

func parsePolicyID(raw []byte) string {
	var payload struct {
		PolicyID string `json:"policy_id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.PolicyID)
}

func txLocationMismatchKind(heightDiff, indexDiff bool) string {
	switch {
	case heightDiff && indexDiff:
		return "height_and_index"
	case heightDiff:
		return "height"
	case indexDiff:
		return "index"
	default:
		return "unknown"
	}
}

func (a *adapter) shouldLogTxLocationMismatch(txHash [32]byte) bool {
	if a == nil {
		return false
	}
	now := time.Now()
	a.txMismatchSeenMu.Lock()
	defer a.txMismatchSeenMu.Unlock()
	a.pruneTxMismatchSeenLocked(now)
	if _, exists := a.txMismatchSeen[txHash]; exists {
		return false
	}
	a.txMismatchSeen[txHash] = now
	a.pruneTxMismatchSeenLocked(now)
	return true
}

func (a *adapter) pruneTxMismatchSeenLocked(now time.Time) {
	if a == nil {
		return
	}
	if a.txMismatchSeen == nil {
		a.txMismatchSeen = make(map[[32]byte]time.Time)
	}
	if a.txMismatchSeenTTL > 0 {
		cutoff := now.Add(-a.txMismatchSeenTTL)
		for hash, seenAt := range a.txMismatchSeen {
			if seenAt.Before(cutoff) {
				delete(a.txMismatchSeen, hash)
			}
		}
	}
	if a.txMismatchSeenMax <= 0 || len(a.txMismatchSeen) <= a.txMismatchSeenMax {
		return
	}
	drop := len(a.txMismatchSeen) - a.txMismatchSeenMax
	for hash := range a.txMismatchSeen {
		delete(a.txMismatchSeen, hash)
		drop--
		if drop <= 0 {
			break
		}
	}
}

func (a *adapter) logTxLocationMismatchForensics(ctx context.Context, tx *sql.Tx, blk *block.AppBlock, txHash [32]byte, incomingHeight uint64, incomingIndex int, ex existingTxRecord, kind string) {
	if a == nil || a.logger == nil || blk == nil {
		return
	}
	forensicsStart := time.Now()
	defer func() {
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_verify_forensics", time.Since(forensicsStart))
		}
	}()
	bh := blk.GetHash()
	incomingBlockHash := fmt.Sprintf("%x", bh[:])
	incomingProposer := fmt.Sprintf("%x", blk.Proposer())
	existingBlockHash, existingProposer := a.loadBlockLocationDetails(ctx, tx, ex.blockHeight)
	a.logger.Warn("transaction location mismatch forensic",
		utils.ZapString("stage", "upsert_transactions_verify_conflicts"),
		utils.ZapString("tx_hash", fmt.Sprintf("%x", txHash[:])),
		utils.ZapString("mismatch_kind", kind),
		utils.ZapUint64("incoming_block_height", incomingHeight),
		utils.ZapInt("incoming_tx_index", incomingIndex),
		utils.ZapString("incoming_block_hash", incomingBlockHash),
		utils.ZapString("incoming_block_proposer", incomingProposer),
		utils.ZapUint64("existing_block_height", ex.blockHeight),
		utils.ZapInt("existing_tx_index", ex.txIndex),
		utils.ZapString("existing_block_hash", existingBlockHash),
		utils.ZapString("existing_block_proposer", existingProposer),
		utils.ZapInt("persist_attempt", PersistAttemptFromContext(ctx)))
}

func (a *adapter) loadBlockLocationDetails(ctx context.Context, tx *sql.Tx, height uint64) (string, string) {
	lookupStart := time.Now()
	defer func() {
		if a != nil && a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions_verify_forensics_block_lookup", time.Since(lookupStart))
		}
	}()
	if tx == nil {
		return "", ""
	}
	var hashRaw []byte
	var proposerRaw []byte
	if err := tx.QueryRowContext(ctx, `SELECT block_hash, proposer_id FROM blocks WHERE height = $1`, height).Scan(&hashRaw, &proposerRaw); err != nil {
		return "", ""
	}
	return fmt.Sprintf("%x", hashRaw), fmt.Sprintf("%x", proposerRaw)
}

func parsePolicyTrace(raw []byte) (string, int64, string, int64, string) {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", 0, "", 0, ""
	}

	traceID, aiEventTsMs, sourceEventID, sourceEventTsMs, sentinelEventID := extractTraceFields(payload)

	// Some publishers wrap effective policy payload under "params".
	if (traceID == "" || aiEventTsMs <= 0 || sourceEventID == "" || sourceEventTsMs <= 0 || sentinelEventID == "") && payload != nil {
		if params, ok := payload["params"].(map[string]interface{}); ok {
			pTraceID, pAiTs, pSourceID, pSourceTs, pSentinelEventID := extractTraceFields(params)
			if traceID == "" {
				traceID = pTraceID
			}
			if aiEventTsMs <= 0 {
				aiEventTsMs = pAiTs
			}
			if sourceEventID == "" {
				sourceEventID = pSourceID
			}
			if sourceEventTsMs <= 0 {
				sourceEventTsMs = pSourceTs
			}
			if sentinelEventID == "" {
				sentinelEventID = pSentinelEventID
			}
		}
	}

	if aiEventTsMs > 0 {
		if normalized, _, valid := utils.NormalizeUnixMillis(aiEventTsMs); valid {
			aiEventTsMs = normalized
		} else {
			aiEventTsMs = 0
		}
	}
	if sourceEventTsMs > 0 {
		if normalized, _, valid := utils.NormalizeUnixMillis(sourceEventTsMs); valid {
			sourceEventTsMs = normalized
		} else {
			sourceEventTsMs = 0
		}
	}
	return traceID, aiEventTsMs, sourceEventID, sourceEventTsMs, sentinelEventID
}

func parsePolicyRequestID(raw []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	requestID := extractRequestIDField(payload)
	if requestID == "" && payload != nil {
		if params, ok := payload["params"].(map[string]interface{}); ok {
			requestID = extractRequestIDField(params)
		}
	}
	return requestID
}

func parsePolicyCommandID(raw []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	commandID := extractCommandIDField(payload)
	if commandID == "" && payload != nil {
		if params, ok := payload["params"].(map[string]interface{}); ok {
			commandID = extractCommandIDField(params)
		}
	}
	return commandID
}

func parsePolicyWorkflowID(raw []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	workflowID := extractWorkflowIDField(payload)
	if workflowID == "" && payload != nil {
		if params, ok := payload["params"].(map[string]interface{}); ok {
			workflowID = extractWorkflowIDField(params)
		}
	}
	return workflowID
}

func extractTraceFields(payload map[string]interface{}) (string, int64, string, int64, string) {
	if payload == nil {
		return "", 0, "", 0, ""
	}
	var traceID string
	topLevelTraceID := extractString(payload, "trace_id")
	qcRef := extractString(payload, "qc_reference")
	sourceEventID := extractString(payload, "source_event_id")
	sentinelEventID := extractString(payload, "sentinel_event_id")

	var aiEventTsMs int64
	var sourceEventTsMs int64
	if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
		traceID = extractString(metadata, "trace_id")
		if sourceEventID == "" {
			sourceEventID = extractString(metadata, "source_event_id")
		}
		if sourceEventID == "" {
			sourceEventID = extractString(metadata, "telemetry_event_id")
		}
		aiEventTsMs = extractInt64(metadata, "ai_event_ts_ms")
		if aiEventTsMs <= 0 {
			aiEventTsMs = extractInt64(metadata, "ai_event_timestamp_ms")
		}
		sourceEventTsMs = extractInt64(metadata, "source_event_ts_ms")
		if sourceEventTsMs <= 0 {
			sourceEventTsMs = extractInt64(metadata, "telemetry_event_ts_ms")
		}
		if sentinelEventID == "" {
			sentinelEventID = extractString(metadata, "sentinel_event_id")
		}
	}

	if trace, ok := payload["trace"].(map[string]interface{}); ok {
		if traceID == "" {
			traceID = extractString(trace, "id")
		}
		if sourceEventID == "" {
			sourceEventID = extractString(trace, "source_event_id")
		}
		if aiEventTsMs <= 0 {
			aiEventTsMs = extractInt64(trace, "ai_event_ts_ms")
		}
		if sourceEventTsMs <= 0 {
			sourceEventTsMs = extractInt64(trace, "source_event_ts_ms")
		}
		if sentinelEventID == "" {
			sentinelEventID = extractString(trace, "sentinel_event_id")
		}
	}
	if traceID == "" {
		traceID = topLevelTraceID
	}
	if sourceEventID == "" {
		sourceEventID = extractString(payload, "telemetry_event_id")
	}
	if traceID == "" {
		traceID = qcRef
	}
	return strings.TrimSpace(traceID), aiEventTsMs, strings.TrimSpace(sourceEventID), sourceEventTsMs, strings.TrimSpace(sentinelEventID)
}

func extractRequestIDField(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	requestID := extractString(payload, "request_id")
	if requestID == "" {
		if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
			requestID = extractString(metadata, "request_id")
		}
	}
	if requestID == "" {
		if trace, ok := payload["trace"].(map[string]interface{}); ok {
			requestID = extractString(trace, "request_id")
		}
	}
	return strings.TrimSpace(requestID)
}

func extractCommandIDField(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	commandID := extractString(payload, "command_id")
	if commandID == "" {
		if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
			commandID = extractString(metadata, "command_id")
		}
	}
	if commandID == "" {
		if trace, ok := payload["trace"].(map[string]interface{}); ok {
			commandID = extractString(trace, "command_id")
		}
	}
	return strings.TrimSpace(commandID)
}

func extractWorkflowIDField(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	workflowID := extractString(payload, "workflow_id")
	if workflowID == "" {
		if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
			workflowID = extractString(metadata, "workflow_id")
		}
	}
	if workflowID == "" {
		if trace, ok := payload["trace"].(map[string]interface{}); ok {
			workflowID = extractString(trace, "workflow_id")
		}
	}
	return strings.TrimSpace(workflowID)
}

func extractString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func extractInt64(m map[string]interface{}, key string) int64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0
		}
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
		if f, err := n.Float64(); err == nil {
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return 0
			}
			return int64(f)
		}
	}
	return 0
}

func policyOutboxSemanticFingerprint(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var payload any
	if err := dec.Decode(&payload); err != nil {
		return ""
	}
	var b strings.Builder
	writePolicyFingerprintJSON(&b, payload, policySemanticFingerprintContext{atRoot: true})
	return b.String()
}

func (a *adapter) derivePolicyOutboxDispatchShard(raw []byte) string {
	shards := 1
	dispatchMode := policyoutbox.DispatchShardTargetHashV1
	mode := "off"
	buckets := 1
	if a != nil {
		if a.perf.outboxDispatchShards > 0 {
			shards = a.perf.outboxDispatchShards
		}
		if strings.TrimSpace(a.perf.outboxDispatchMode) != "" {
			dispatchMode = a.perf.outboxDispatchMode
		}
		if strings.TrimSpace(a.perf.clusterShardingMode) != "" {
			mode = a.perf.clusterShardingMode
		}
		if a.perf.clusterShardBuckets > 0 {
			buckets = a.perf.clusterShardBuckets
		}
	}
	return policyoutbox.DeriveDispatchShard(raw, shards, policyoutbox.RoutingOptions{
		ClusterShardingMode: mode,
		ClusterShardBuckets: buckets,
		DispatchShardMode:   dispatchMode,
	})
}

func writePolicyFingerprintJSON(b *strings.Builder, v any, ctx policySemanticFingerprintContext) {
	switch val := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if val {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		enc, _ := json.Marshal(val)
		b.Write(enc)
	case json.Number:
		b.WriteString(val.String())
	case float64:
		enc, _ := json.Marshal(val)
		b.Write(enc)
	case int:
		b.WriteString(strconv.Itoa(val))
	case int64:
		b.WriteString(strconv.FormatInt(val, 10))
	case []interface{}:
		b.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				b.WriteByte(',')
			}
			writePolicyFingerprintJSON(b, item, ctx)
		}
		b.WriteByte(']')
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			if skipPolicyFingerprintKey(k, ctx) {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			keyJSON, _ := json.Marshal(k)
			b.Write(keyJSON)
			b.WriteByte(':')
			childCtx := policySemanticFingerprintContext{
				inMetadata: ctx.inMetadata,
				inTrace:    ctx.inTrace,
				atRoot:     false,
			}
			if k == "metadata" {
				childCtx.inMetadata = true
			}
			if k == "trace" {
				childCtx.inTrace = true
			}
			writePolicyFingerprintJSON(b, val[k], childCtx)
		}
		b.WriteByte('}')
	default:
		enc, _ := json.Marshal(val)
		b.Write(enc)
	}
}

func skipPolicyFingerprintKey(key string, ctx policySemanticFingerprintContext) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return true
	}
	switch key {
	case "policy_id":
		return true
	case "trace_id":
		return ctx.atRoot || ctx.inMetadata || ctx.inTrace
	case "ai_event_ts_ms", "ai_event_timestamp_ms", "source_event_ts_ms", "telemetry_event_ts_ms":
		return ctx.atRoot || ctx.inMetadata || ctx.inTrace
	case "source_event_id", "telemetry_event_id", "sentinel_event_id":
		return ctx.atRoot || ctx.inMetadata || ctx.inTrace
	case "request_id":
		return ctx.atRoot || ctx.inMetadata || ctx.inTrace
	case "command_id":
		return ctx.atRoot || ctx.inMetadata || ctx.inTrace
	case "workflow_id":
		return ctx.atRoot || ctx.inMetadata || ctx.inTrace
	}
	return false
}

func coalescePolicyOutboxInputs(rows []policyOutboxInput) []policyOutboxInput {
	if len(rows) <= 1 {
		return rows
	}
	sortedRows := append([]policyOutboxInput(nil), rows...)
	sort.SliceStable(sortedRows, func(i, j int) bool {
		if sortedRows[i].blockHeight != sortedRows[j].blockHeight {
			return sortedRows[i].blockHeight < sortedRows[j].blockHeight
		}
		return sortedRows[i].txIndex < sortedRows[j].txIndex
	})
	coalesced := make([]policyOutboxInput, 0, len(rows))
	seen := make(map[uint64]map[string]struct{}, len(rows))
	for _, row := range sortedRows {
		fp := policyOutboxSemanticFingerprint(row.payload)
		if fp == "" {
			coalesced = append(coalesced, row)
			continue
		}
		blockSeen := seen[row.blockHeight]
		if blockSeen == nil {
			blockSeen = make(map[string]struct{})
			seen[row.blockHeight] = blockSeen
		}
		if _, exists := blockSeen[fp]; exists {
			continue
		}
		blockSeen[fp] = struct{}{}
		coalesced = append(coalesced, row)
	}
	return coalesced
}

func boundedPhaseTimeoutFromP95(ctx context.Context, p95Ms float64, fallback, minBudget, maxBudget, reserve time.Duration) time.Duration {
	target := fallback
	if target <= 0 {
		target = minBudget
	}
	if p95Ms > 0 && !math.IsNaN(p95Ms) && !math.IsInf(p95Ms, 0) {
		adaptive := time.Duration((p95Ms*1.5)+100) * time.Millisecond
		if adaptive > target {
			target = adaptive
		}
	}
	if target < minBudget {
		target = minBudget
	}
	if maxBudget > 0 && target > maxBudget {
		target = maxBudget
	}
	if ctx != nil {
		if dl, ok := ctx.Deadline(); ok {
			remaining := time.Until(dl) - reserve
			if remaining <= 0 {
				remaining = minBudget
			}
			if target > remaining {
				target = remaining
			}
		}
	}
	if target < minBudget {
		target = minBudget
	}
	if maxBudget > 0 && target > maxBudget {
		target = maxBudget
	}
	return target
}

func (a *adapter) outboxUpsertBudget(ctx context.Context, rows int) time.Duration {
	fallback := 1500 * time.Millisecond
	if rows > 128 {
		fallback = 2 * time.Second
	}
	p95Ms := 0.0
	if a != nil && a.metrics != nil {
		p95Ms = a.metrics.persistStageQuantile("upsert_transactions_outbox_batch", 0.95)
	}
	return boundedPhaseTimeoutFromP95(ctx, p95Ms, fallback, 500*time.Millisecond, 5*time.Second, 100*time.Millisecond)
}

func (a *adapter) outboxConflictVerifyBudget(ctx context.Context, rows int) time.Duration {
	fallback := time.Second
	if rows > 128 {
		fallback = 1500 * time.Millisecond
	}
	p95Ms := 0.0
	if a != nil && a.metrics != nil {
		p95Ms = a.metrics.persistStageQuantile("upsert_transactions_verify_query", 0.95)
	}
	return boundedPhaseTimeoutFromP95(ctx, p95Ms, fallback, 400*time.Millisecond, 4*time.Second, 80*time.Millisecond)
}

func (a *adapter) semanticLookupBudget(ctx context.Context, keys int) time.Duration {
	fallback := 900 * time.Millisecond
	if keys > 128 {
		fallback = 1200 * time.Millisecond
	}
	p95Ms := 0.0
	if a != nil && a.metrics != nil {
		p95Ms = a.metrics.persistStageQuantile("upsert_policy_outbox_semantic_lookup", 0.95)
	}
	return boundedPhaseTimeoutFromP95(ctx, p95Ms, fallback, 300*time.Millisecond, 3*time.Second, 80*time.Millisecond)
}

func (a *adapter) upsertPolicyOutbox(ctx context.Context, tx *sql.Tx, blockHeight uint64, blockTS int64, txTS int64, txIndex int, payload []byte, persistAttempt int) error {
	if len(payload) == 0 {
		return fmt.Errorf("%w: empty policy payload", ErrInvalidData)
	}

	ruleHash := sha256.Sum256(payload)
	semanticKey := policyOutboxSemanticFingerprint(payload)
	dispatchShard := a.derivePolicyOutboxDispatchShard(payload)
	policyID := parsePolicyID(payload)
	if policyID == "" {
		policyID = "invalid:" + hex.EncodeToString(ruleHash[:8])
	}
	traceID, aiEventTsMs, sourceEventID, sourceEventTsMs, sentinelEventID := parsePolicyTrace(payload)
	requestID := parsePolicyRequestID(payload)
	commandID := parsePolicyCommandID(payload)
	workflowID := parsePolicyWorkflowID(payload)
	if aiEventTsMs <= 0 && txTS > 0 {
		aiEventTsMs = txTS * 1000
	}
	if traceID == "" {
		traceID = fmt.Sprintf("trace:%s:%s", policyID, hex.EncodeToString(ruleHash[:8]))
	}
	var createdOutboxID string
	insertCtx, insertCancel := context.WithTimeout(ctx, a.outboxUpsertBudget(ctx, 1))
	err := tx.QueryRowContext(insertCtx, `
		INSERT INTO control_policy_outbox (
			block_height, block_ts, tx_index, policy_id, rule_hash, semantic_key, dispatch_shard, payload, request_id, command_id, workflow_id, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, sentinel_event_id, status, next_retry_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), $12, $13, NULLIF($14, ''), NULLIF($15, 0), NULLIF($16, ''), 'pending', NOW(), NOW(), NOW()
		)
		ON CONFLICT DO NOTHING
		RETURNING id::STRING
	`, blockHeight, blockTS, txIndex, policyID, ruleHash[:], semanticKey, dispatchShard, payload, requestID, commandID, workflowID, traceID, aiEventTsMs, sourceEventID, sourceEventTsMs, sentinelEventID).Scan(&createdOutboxID)
	insertCancel()
	if err == sql.ErrNoRows {
		createdOutboxID = ""
	} else if err != nil {
		if a.metrics != nil && isTimeoutOrCanceledError(err) {
			a.metrics.observePersistDiagnosticSignal("outbox_upsert_timeout")
		}
		return err
	}
	if createdOutboxID != "" {
		a.logPolicyStage(policyID, traceID, "t_outbox_row_created", time.Now().UnixMilli(), blockHeight, txIndex, persistAttempt)
		if shouldSkipLifecycleAuditForDeadline(ctx, 1) {
			if a.metrics != nil {
				a.metrics.observePersistDiagnosticSignal("lifecycle_audit_deferred")
			}
			if a.logger != nil {
				deadline, _ := ctx.Deadline()
				a.logger.WarnContext(ctx, "skipping lifecycle audit for created outbox row to preserve durable commit budget",
					utils.ZapString("policy_id", policyID),
					utils.ZapString("trace_id", traceID),
					utils.ZapDuration("estimated_budget", estimateLifecycleAuditBudget(1)),
					utils.ZapDuration("remaining", time.Until(deadline)))
			}
			if a.auditLogger != nil {
				_ = a.auditLogger.Warn("lifecycle_audit_skipped_due_to_deadline_budget", map[string]interface{}{
					"height":          blockHeight,
					"tx_index":        txIndex,
					"event_count":     1,
					"estimated_ms":    estimateLifecycleAuditBudget(1).Milliseconds(),
					"commit_reserve":  lifecycleAuditCommitReserve.Milliseconds(),
					"persist_attempt": persistAttempt,
				})
			}
			return nil
		}
		if _, auditErr := lifecycleaudit.InsertOutboxEvent(ctx, tx, lifecycleaudit.OutboxEvent{
			ActionType:  lifecycleaudit.ActionPolicyCreated,
			OutboxID:    createdOutboxID,
			PolicyID:    policyID,
			WorkflowID:  workflowID,
			RequestID:   requestID,
			ReasonCode:  "auto.policy_created",
			ReasonText:  "durable outbox row created",
			AfterStatus: "pending",
		}); auditErr != nil {
			if !lifecycleaudit.IsBestEffortErr(auditErr) {
				return fmt.Errorf("insert lifecycle audit for created outbox row: %w", auditErr)
			}
			if a.logger != nil {
				a.logger.WarnContext(ctx, "policy lifecycle audit degraded to best-effort for created outbox row",
					utils.ZapError(auditErr),
					utils.ZapString("policy_id", policyID),
					utils.ZapString("trace_id", traceID))
			}
		}
		return nil
	}
	var existingPolicyID string
	var existingRuleHash []byte
	verifyCtx, verifyCancel := context.WithTimeout(ctx, a.outboxConflictVerifyBudget(ctx, 1))
	err = tx.QueryRowContext(verifyCtx, `
		SELECT policy_id, rule_hash
		FROM control_policy_outbox
		WHERE block_height = $1
		  AND tx_index = $2
	`, blockHeight, txIndex).Scan(&existingPolicyID, &existingRuleHash)
	verifyCancel()
	if err != nil {
		if a.metrics != nil && isTimeoutOrCanceledError(err) {
			a.metrics.observePersistDiagnosticSignal("outbox_conflict_verify_timeout")
		}
		if err == sql.ErrNoRows {
			if semanticKey != "" {
				activeRows, activeErr := a.lookupActivePolicyOutboxSemanticRows(ctx, tx, []string{semanticKey})
				if activeErr != nil {
					return fmt.Errorf("lookup active semantic outbox rows after conflict: %w", activeErr)
				}
				if activeRow, ok := activeRows[semanticKey]; ok {
					refreshed, refreshErr := a.refreshActivePolicyOutboxRow(ctx, tx, activeRow, outboxPrepared{
						blockHeight:     blockHeight,
						blockTS:         blockTS,
						txIndex:         txIndex,
						policyID:        policyID,
						ruleHash:        ruleHash,
						semanticKey:     semanticKey,
						dispatchShard:   dispatchShard,
						payload:         payload,
						requestID:       requestID,
						commandID:       commandID,
						workflowID:      workflowID,
						traceID:         traceID,
						aiEventTsMs:     aiEventTsMs,
						sourceEventID:   sourceEventID,
						sourceEventTsMs: sourceEventTsMs,
						sentinelEventID: sentinelEventID,
					}, persistAttempt)
					if refreshErr != nil {
						return refreshErr
					}
					if refreshed {
						return nil
					}
					a.logPolicyStage(policyID, traceID, "t_outbox_row_reused", time.Now().UnixMilli(), blockHeight, txIndex, persistAttempt)
					return nil
				}
			}
			return fmt.Errorf("%w: missing outbox row after tx-identity conflict at height %d index %d", ErrIntegrityViolation, blockHeight, txIndex)
		}
		return fmt.Errorf("verify existing outbox row: %w", err)
	}
	if strings.TrimSpace(existingPolicyID) != policyID {
		return fmt.Errorf("%w: outbox policy_id mismatch at height %d index %d", ErrIntegrityViolation, blockHeight, txIndex)
	}
	if !bytes.Equal(existingRuleHash, ruleHash[:]) {
		return fmt.Errorf("%w: outbox rule_hash mismatch at height %d index %d", ErrIntegrityViolation, blockHeight, txIndex)
	}
	return nil
}

func (a *adapter) upsertPolicyOutboxBatch(ctx context.Context, tx *sql.Tx, rows []policyOutboxInput, chunkSize int, persistAttempt int) error {
	if len(rows) == 0 {
		return nil
	}
	if chunkSize < 1 {
		chunkSize = outboxUpsertBatchSize
	}
	if chunkSize > maxUpsertBatchSize {
		chunkSize = maxUpsertBatchSize
	}
	if safeChunk := outboxReturningSafeChunkLimit(); chunkSize > safeChunk {
		if a.metrics != nil {
			a.metrics.observePersistDiagnosticSignal("outbox_returning_guardrail_applied")
		}
		if a.logger != nil {
			a.logger.InfoContext(ctx, "outbox returning guardrail reduced chunk size",
				utils.ZapInt("from_chunk", chunkSize),
				utils.ZapInt("to_chunk", safeChunk),
				utils.ZapInt("persist_attempt", persistAttempt))
		}
		chunkSize = safeChunk
	}
	rows = coalescePolicyOutboxInputs(rows)
	baseHeight := rows[0].blockHeight
	for _, row := range rows[1:] {
		if row.blockHeight != baseHeight {
			return fmt.Errorf("%w: mixed block heights in outbox batch (%d,%d)", ErrInvalidData, baseHeight, row.blockHeight)
		}
	}
	prepared := make([]outboxPrepared, 0, len(rows))
	for _, row := range rows {
		if len(row.payload) == 0 {
			return fmt.Errorf("%w: empty policy payload", ErrInvalidData)
		}
		ruleHash := sha256.Sum256(row.payload)
		semanticKey := policyOutboxSemanticFingerprint(row.payload)
		dispatchShard := a.derivePolicyOutboxDispatchShard(row.payload)
		policyID := parsePolicyID(row.payload)
		if policyID == "" {
			policyID = "invalid:" + hex.EncodeToString(ruleHash[:8])
		}
		traceID, aiEventTsMs, sourceEventID, sourceEventTsMs, sentinelEventID := parsePolicyTrace(row.payload)
		requestID := parsePolicyRequestID(row.payload)
		commandID := parsePolicyCommandID(row.payload)
		workflowID := parsePolicyWorkflowID(row.payload)
		if aiEventTsMs <= 0 && row.txTS > 0 {
			aiEventTsMs = row.txTS * 1000
		}
		if traceID == "" {
			traceID = fmt.Sprintf("trace:%s:%s", policyID, hex.EncodeToString(ruleHash[:8]))
		}
		prepared = append(prepared, outboxPrepared{
			blockHeight:     row.blockHeight,
			blockTS:         row.blockTS,
			txIndex:         row.txIndex,
			policyID:        policyID,
			ruleHash:        ruleHash,
			semanticKey:     semanticKey,
			dispatchShard:   dispatchShard,
			payload:         row.payload,
			requestID:       requestID,
			commandID:       commandID,
			workflowID:      workflowID,
			traceID:         traceID,
			aiEventTsMs:     aiEventTsMs,
			sourceEventID:   sourceEventID,
			sourceEventTsMs: sourceEventTsMs,
			sentinelEventID: sentinelEventID,
		})
	}

	type outboxKey struct {
		blockHeight uint64
		txIndex     int
	}
	type existingOutbox struct {
		policyID string
		ruleHash []byte
	}
	conflictsAll := make([]outboxPrepared, 0, len(prepared))
	for start := 0; start < len(prepared); start += chunkSize {
		end := start + chunkSize
		if end > len(prepared) {
			end = len(prepared)
		}
		chunk := prepared[start:end]

		args := make([]interface{}, 0, len(chunk)*16)
		filteredChunk := make([]outboxPrepared, 0, len(chunk))
		for _, row := range chunk {
			filteredChunk = append(filteredChunk, row)
			args = append(args,
				row.blockHeight,
				row.blockTS,
				row.txIndex,
				row.policyID,
				row.ruleHash[:],
				row.semanticKey,
				row.dispatchShard,
				row.payload,
				row.requestID,
				row.commandID,
				row.workflowID,
				row.traceID,
				row.aiEventTsMs,
				row.sourceEventID,
				row.sourceEventTsMs,
				row.sentinelEventID,
			)
		}
		if len(filteredChunk) == 0 {
			continue
		}

		insertedByTxIndex := make(map[int]string, len(filteredChunk))
		insertedRowsCount := 0
		returningBytesEstimate := 0
		logRetryableOutboxInsert := func(err error) {
			if err == nil || !IsRetryable(err) {
				return
			}
			state := extractSQLState(err)
			if a.metrics != nil {
				a.metrics.observeOutboxInsertRetry(state)
			}
			if a.logger != nil {
				a.logger.WarnContext(ctx, "outbox batch insert retryable failure",
					utils.ZapError(err),
					utils.ZapString("sqlstate", state),
					utils.ZapInt("chunk_size", len(filteredChunk)),
					utils.ZapInt("returned_rows", insertedRowsCount),
					utils.ZapInt("returning_estimated_bytes", returningBytesEstimate),
					utils.ZapInt("persist_attempt", persistAttempt))
			}
		}
		insertCtx, insertCancel := context.WithTimeout(ctx, a.outboxUpsertBudget(ctx, len(filteredChunk)))
		rowsInserted, err := tx.QueryContext(insertCtx, a.outboxInsertTemplate(len(filteredChunk)), args...)
		if err != nil {
			insertCancel()
			if a.metrics != nil && isTimeoutOrCanceledError(err) {
				a.metrics.observePersistDiagnosticSignal("outbox_upsert_timeout")
			}
			logRetryableOutboxInsert(err)
			return err
		}
		for rowsInserted.Next() {
			var outboxID string
			var i int
			if scanErr := rowsInserted.Scan(&outboxID, &i); scanErr != nil {
				rowsInserted.Close()
				return fmt.Errorf("scan inserted outbox rows: %w", scanErr)
			}
			insertedByTxIndex[i] = outboxID
			insertedRowsCount++
			returningBytesEstimate += estimateOutboxReturningRowBytes(outboxID)
		}
		if err := rowsInserted.Err(); err != nil {
			rowsInserted.Close()
			insertCancel()
			readErr := fmt.Errorf("read inserted outbox rows: %w", err)
			logRetryableOutboxInsert(readErr)
			return readErr
		}
		rowsInserted.Close()
		insertCancel()
		if a.metrics != nil {
			a.metrics.observeOutboxReturning(insertedRowsCount, returningBytesEstimate)
			if returningBytesEstimate >= outboxReturningSafetyBudgetBytes {
				a.metrics.observePersistDiagnosticSignal("outbox_returning_near_safety_budget")
			}
		}

		conflicts := make([]outboxPrepared, 0, len(filteredChunk))
		lifecycleEvents := make([]lifecycleaudit.OutboxEvent, 0, len(filteredChunk))
		nowMs := time.Now().UnixMilli()
		for _, row := range filteredChunk {
			if outboxID, ok := insertedByTxIndex[row.txIndex]; ok {
				a.logPolicyStage(row.policyID, row.traceID, "t_outbox_row_created", nowMs, row.blockHeight, row.txIndex, persistAttempt)
				lifecycleEvents = append(lifecycleEvents, lifecycleaudit.OutboxEvent{
					ActionType:  lifecycleaudit.ActionPolicyCreated,
					OutboxID:    outboxID,
					PolicyID:    row.policyID,
					WorkflowID:  row.workflowID,
					RequestID:   row.requestID,
					ReasonCode:  "auto.policy_created",
					ReasonText:  "durable outbox row created",
					AfterStatus: "pending",
				})
				continue
			}
			conflicts = append(conflicts, row)
		}
		if len(lifecycleEvents) > 0 {
			if shouldSkipLifecycleAuditForDeadline(ctx, len(lifecycleEvents)) {
				if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("lifecycle_audit_deferred")
				}
				if a.logger != nil {
					deadline, _ := ctx.Deadline()
					a.logger.WarnContext(ctx, "skipping lifecycle audit for created outbox batch to preserve durable commit budget",
						utils.ZapInt("batch_size", len(lifecycleEvents)),
						utils.ZapDuration("estimated_budget", estimateLifecycleAuditBudget(len(lifecycleEvents))),
						utils.ZapDuration("remaining", time.Until(deadline)))
				}
				if a.auditLogger != nil {
					_ = a.auditLogger.Warn("lifecycle_audit_skipped_due_to_deadline_budget", map[string]interface{}{
						"height":          chunk[0].blockHeight,
						"event_count":     len(lifecycleEvents),
						"estimated_ms":    estimateLifecycleAuditBudget(len(lifecycleEvents)).Milliseconds(),
						"commit_reserve":  lifecycleAuditCommitReserve.Milliseconds(),
						"persist_attempt": persistAttempt,
					})
				}
			} else if err := lifecycleaudit.InsertOutboxEvents(ctx, tx, lifecycleEvents); err != nil {
				if !lifecycleaudit.IsBestEffortErr(err) {
					return fmt.Errorf("insert lifecycle audit for created outbox rows: %w", err)
				}
				if a.logger != nil {
					a.logger.WarnContext(ctx, "policy lifecycle audit degraded to best-effort for created outbox batch",
						utils.ZapError(err),
						utils.ZapInt("batch_size", len(lifecycleEvents)))
				}
			}
		}
		conflictsAll = append(conflictsAll, conflicts...)
	}
	if len(conflictsAll) > 0 {
		verifyChunkSize := maxUpsertBatchSize
		for start := 0; start < len(conflictsAll); start += verifyChunkSize {
			end := start + verifyChunkSize
			if end > len(conflictsAll) {
				end = len(conflictsAll)
			}
			chunk := conflictsAll[start:end]
			verifyArgs := make([]interface{}, 0, len(chunk)*2)
			for _, row := range chunk {
				verifyArgs = append(verifyArgs, row.blockHeight, row.txIndex)
			}
			existing := make(map[outboxKey]existingOutbox, len(chunk))
			verifyCtx, verifyCancel := context.WithTimeout(ctx, a.outboxConflictVerifyBudget(ctx, len(chunk)))
			verifyRows, err := tx.QueryContext(verifyCtx, a.outboxVerifyTemplate(len(chunk)), verifyArgs...)
			if err != nil {
				verifyCancel()
				if a.metrics != nil && isTimeoutOrCanceledError(err) {
					a.metrics.observePersistDiagnosticSignal("outbox_conflict_verify_timeout")
				}
				return fmt.Errorf("verify existing outbox row: %w", err)
			}
			for verifyRows.Next() {
				var h uint64
				var idx int
				var pid string
				var rh []byte
				if scanErr := verifyRows.Scan(&h, &idx, &pid, &rh); scanErr != nil {
					verifyRows.Close()
					verifyCancel()
					return fmt.Errorf("scan existing outbox row: %w", scanErr)
				}
				existing[outboxKey{blockHeight: h, txIndex: idx}] = existingOutbox{policyID: strings.TrimSpace(pid), ruleHash: rh}
			}
			if err := verifyRows.Err(); err != nil {
				verifyRows.Close()
				verifyCancel()
				return fmt.Errorf("read existing outbox rows: %w", err)
			}
			verifyRows.Close()
			verifyCancel()
			for _, row := range chunk {
				key := outboxKey{blockHeight: row.blockHeight, txIndex: row.txIndex}
				ex, ok := existing[key]
				if ok {
					if ex.policyID != row.policyID {
						return fmt.Errorf("%w: outbox policy_id mismatch at height %d index %d", ErrIntegrityViolation, row.blockHeight, row.txIndex)
					}
					if !bytes.Equal(ex.ruleHash, row.ruleHash[:]) {
						return fmt.Errorf("%w: outbox rule_hash mismatch at height %d index %d", ErrIntegrityViolation, row.blockHeight, row.txIndex)
					}
				}
			}

			missingSemanticKeys := make([]string, 0, len(chunk))
			missingSemanticSeen := make(map[string]struct{}, len(chunk))
			for _, row := range chunk {
				key := outboxKey{blockHeight: row.blockHeight, txIndex: row.txIndex}
				if _, ok := existing[key]; ok {
					continue
				}
				semanticKey := strings.TrimSpace(row.semanticKey)
				if semanticKey == "" {
					continue
				}
				if _, seen := missingSemanticSeen[semanticKey]; seen {
					continue
				}
				missingSemanticSeen[semanticKey] = struct{}{}
				missingSemanticKeys = append(missingSemanticKeys, semanticKey)
			}
			activeMissingSemantic := make(map[string]activeSemanticOutboxRow)
			if len(missingSemanticKeys) > 0 {
				activeRows, activeErr := a.lookupActivePolicyOutboxSemanticRows(ctx, tx, missingSemanticKeys)
				if activeErr != nil {
					return fmt.Errorf("lookup active semantic outbox rows after batch conflict: %w", activeErr)
				}
				activeMissingSemantic = activeRows
			}

			for _, row := range chunk {
				key := outboxKey{blockHeight: row.blockHeight, txIndex: row.txIndex}
				if _, ok := existing[key]; ok {
					continue
				}
				if row.semanticKey != "" {
					if activeRow, present := activeMissingSemantic[row.semanticKey]; present {
						refreshed, refreshErr := a.refreshActivePolicyOutboxRow(ctx, tx, activeRow, row, persistAttempt)
						if refreshErr != nil {
							return refreshErr
						}
						if refreshed {
							continue
						}
						a.logPolicyStage(row.policyID, row.traceID, "t_outbox_row_reused", time.Now().UnixMilli(), row.blockHeight, row.txIndex, persistAttempt)
						continue
					}
				}
				return fmt.Errorf("%w: missing outbox row after tx-identity conflict at height %d index %d", ErrIntegrityViolation, row.blockHeight, row.txIndex)
			}
		}
	}
	return nil
}

func outboxReturningSafeChunkLimit() int {
	limit := outboxReturningSafetyBudgetBytes / outboxReturningEstimatedRowBytes
	if limit < 1 {
		return 1
	}
	if limit > maxUpsertBatchSize {
		return maxUpsertBatchSize
	}
	return limit
}

func estimateOutboxReturningRowBytes(outboxID string) int {
	// RETURNING payload fields:
	// - id::STRING (UUID text; typically 36 chars)
	// - tx_index (INT)
	// Include protocol/value overhead with a conservative floor.
	bytes := len(strings.TrimSpace(outboxID)) + 24
	if bytes < 64 {
		bytes = 64
	}
	return bytes
}

func (a *adapter) lookupActivePolicyOutboxSemanticKeys(ctx context.Context, tx *sql.Tx, keys []string) (map[string]struct{}, error) {
	rows, err := a.lookupActivePolicyOutboxSemanticRows(ctx, tx, keys)
	if err != nil {
		return nil, err
	}
	active := make(map[string]struct{})
	for key := range rows {
		active[key] = struct{}{}
	}
	return active, nil
}

func (a *adapter) lookupOutboxRowID(ctx context.Context, tx *sql.Tx, blockHeight uint64, txIndex int) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
		SELECT id::STRING
		FROM control_policy_outbox
		WHERE block_height = $1
		  AND tx_index = $2
	`, blockHeight, txIndex).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (a *adapter) lookupActivePolicyOutboxSemanticRows(ctx context.Context, tx *sql.Tx, keys []string) (map[string]activeSemanticOutboxRow, error) {
	active := make(map[string]activeSemanticOutboxRow)
	if len(keys) == 0 {
		return active, nil
	}
	start := time.Now()
	args := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}
	lookupCtx, lookupCancel := context.WithTimeout(ctx, a.semanticLookupBudget(ctx, len(keys)))
	rows, err := tx.QueryContext(lookupCtx, a.outboxActiveRowsTemplate(len(keys)), args...)
	if err != nil {
		lookupCancel()
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_policy_outbox_semantic_lookup", time.Since(start))
			if isTimeoutOrCanceledError(err) {
				a.metrics.observePersistDiagnosticSignal("outbox_semantic_lookup_timeout")
			}
		}
		return nil, err
	}
	for rows.Next() {
		var row activeSemanticOutboxRow
		if scanErr := rows.Scan(&row.SemanticKey, &row.PolicyID, &row.RuleHash, &row.Status); scanErr != nil {
			rows.Close()
			lookupCancel()
			return nil, fmt.Errorf("scan active semantic outbox row: %w", scanErr)
		}
		row.SemanticKey = strings.TrimSpace(row.SemanticKey)
		row.PolicyID = strings.TrimSpace(row.PolicyID)
		row.Status = strings.TrimSpace(strings.ToLower(row.Status))
		if row.SemanticKey != "" {
			active[row.SemanticKey] = row
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		lookupCancel()
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_policy_outbox_semantic_lookup", time.Since(start))
			if isTimeoutOrCanceledError(err) {
				a.metrics.observePersistDiagnosticSignal("outbox_semantic_lookup_timeout")
			}
		}
		return nil, fmt.Errorf("read active semantic outbox rows: %w", err)
	}
	rows.Close()
	lookupCancel()
	if a.metrics != nil {
		a.metrics.observePersistStage("upsert_policy_outbox_semantic_lookup", time.Since(start))
	}
	return active, nil
}

func (a *adapter) refreshActivePolicyOutboxRow(ctx context.Context, tx *sql.Tx, existing activeSemanticOutboxRow, incoming outboxPrepared, persistAttempt int) (bool, error) {
	if strings.TrimSpace(incoming.semanticKey) == "" {
		return false, nil
	}
	if existing.PolicyID == "" {
		return false, nil
	}
	if bytes.Equal(existing.RuleHash, incoming.ruleHash[:]) {
		if a.metrics != nil {
			a.metrics.observePersistDiagnosticSignal("outbox_semantic_refresh_skipped_same_hash")
		}
		return false, nil
	}
	if existing.Status == "publishing" || existing.Status == "published" || existing.Status == "acked" {
		if a.metrics != nil {
			a.metrics.observePersistDiagnosticSignal("outbox_semantic_refresh_skipped_nonmutable_status")
		}
		return false, nil
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE control_policy_outbox
		SET policy_id = $1,
		    rule_hash = $2,
		    payload = $3,
		    request_id = NULLIF($4, ''),
		    command_id = NULLIF($5, ''),
		    workflow_id = NULLIF($6, ''),
		    trace_id = $7,
		    ai_event_ts_ms = $8,
		    source_event_id = NULLIF($9, ''),
		    source_event_ts_ms = NULLIF($10, 0),
		    sentinel_event_id = NULLIF($11, ''),
		    status = 'pending',
		    retries = 0,
		    next_retry_at = NOW(),
		    last_error = NULL,
		    lease_holder = NULL,
		    lease_epoch = 0,
		    kafka_topic = NULL,
		    kafka_partition = NULL,
		    kafka_offset = NULL,
		    published_at = NULL,
		    ack_result = NULL,
		    ack_reason = NULL,
		    ack_controller = NULL,
		    acked_at = NULL,
		    updated_at = NOW()
		WHERE semantic_key = $12
		  AND status IN ('pending', 'retry')
	`, incoming.policyID, incoming.ruleHash[:], incoming.payload, incoming.requestID, incoming.commandID, incoming.workflowID, incoming.traceID, incoming.aiEventTsMs, incoming.sourceEventID, incoming.sourceEventTsMs, incoming.sentinelEventID, incoming.semanticKey)
	if err != nil {
		return false, fmt.Errorf("refresh active semantic outbox row: %w", err)
	}
	rows, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return false, fmt.Errorf("refresh active semantic outbox row rows affected: %w", rowsErr)
	}
	if rows == 0 {
		if a.metrics != nil {
			a.metrics.observePersistDiagnosticSignal("outbox_semantic_refresh_no_rows")
		}
		return false, nil
	}
	if a.metrics != nil {
		a.metrics.observePersistDiagnosticSignal("outbox_semantic_refresh_applied")
	}
	a.logPolicyStage(incoming.policyID, incoming.traceID, "t_outbox_row_refreshed", time.Now().UnixMilli(), incoming.blockHeight, incoming.txIndex, persistAttempt)
	return true, nil
}

func collectPolicyIDsFromMarkers(markers []policyStageMarker) []string {
	if len(markers) == 0 {
		return nil
	}
	out := make([]string, 0, len(markers))
	seen := make(map[string]struct{}, len(markers))
	for _, marker := range markers {
		id := strings.TrimSpace(marker.policyID)
		if id == "" || strings.HasPrefix(id, "invalid:") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func dedupePolicyIDList(policyIDs []string) []string {
	if len(policyIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(policyIDs))
	seen := make(map[string]struct{}, len(policyIDs))
	for _, raw := range policyIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (a *adapter) refreshPolicyStateAsync(policyIDs []string, blockHeight uint64, persistAttempt int) {
	if a == nil || a.db == nil || len(policyIDs) == 0 {
		return
	}
	if !a.policyRefreshAsync {
		return
	}
	if untilNs := a.policyRefreshCooldownUntil.Load(); untilNs > 0 && time.Now().UnixNano() < untilNs {
		if a.metrics != nil {
			a.metrics.observePersistDiagnosticSignal("policy_state_refresh_async_skipped_cooldown")
		}
		return
	}
	sem := a.policyRefreshSem
	if sem == nil {
		sem = make(chan struct{}, 1)
		a.policyRefreshSem = sem
	}
	select {
	case sem <- struct{}{}:
	default:
		if a.metrics != nil {
			a.metrics.observePersistDiagnosticSignal("policy_state_refresh_async_skipped_busy")
		}
		if a.logger != nil {
			a.logger.Warn("policy state async refresh skipped",
				utils.ZapUint64("height", blockHeight),
				utils.ZapInt("policy_count", len(policyIDs)),
				utils.ZapInt("persist_attempt", persistAttempt))
		}
		return
	}
	ids := dedupePolicyIDList(policyIDs)
	if len(ids) == 0 {
		<-sem
		return
	}
	if a.policyRefreshMaxIDs > 0 && len(ids) > a.policyRefreshMaxIDs {
		ids = ids[:a.policyRefreshMaxIDs]
	}
	go func() {
		defer func() { <-sem }()
		start := time.Now()
		timeout := a.policyRefreshTTL
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		err := refreshPolicyStateMany(ctx, a.db, ids)
		if a.metrics != nil {
			a.metrics.observePersistStage("policy_state_refresh_async", time.Since(start))
			if err != nil {
				a.metrics.observePersistFailureClass("policy_state_refresh_async", err)
			}
		}
		if err == nil {
			a.policyRefreshFailureStreak.Store(0)
			a.policyRefreshCooldownUntil.Store(0)
		} else if isTimeoutOrCanceledError(err) {
			streak := a.policyRefreshFailureStreak.Add(1)
			if streak >= policyRefreshFailureStreakForCooldown {
				a.policyRefreshCooldownUntil.Store(time.Now().Add(policyRefreshFailureCooldown).UnixNano())
				if a.metrics != nil {
					a.metrics.observePersistDiagnosticSignal("policy_state_refresh_async_cooldown_opened")
				}
			}
		}
		if err != nil && a.logger != nil {
			a.logger.Warn("policy state async refresh failed",
				utils.ZapError(err),
				utils.ZapUint64("height", blockHeight),
				utils.ZapInt("policy_count", len(ids)),
				utils.ZapInt("persist_attempt", persistAttempt))
		}
	}()
}

func collectPolicyStageMarkers(blk *block.AppBlock) []policyStageMarker {
	if blk == nil {
		return nil
	}
	txs := blk.Transactions()
	markers := make([]policyStageMarker, 0, len(txs))
	for idx, tx := range txs {
		if tx == nil || tx.Type() != state.TxPolicy {
			continue
		}
		payload := tx.Payload()
		if len(payload) == 0 {
			continue
		}
		ruleHash := sha256.Sum256(payload)
		policyID := parsePolicyID(payload)
		if policyID == "" {
			policyID = "invalid:" + hex.EncodeToString(ruleHash[:8])
		}
		traceID, _, _, _, _ := parsePolicyTrace(payload)
		if traceID == "" {
			traceID = fmt.Sprintf("trace:%s:%s", policyID, hex.EncodeToString(ruleHash[:8]))
		}
		markers = append(markers, policyStageMarker{
			policyID: policyID,
			traceID:  traceID,
			txIndex:  idx,
		})
	}
	return markers
}

func (a *adapter) logPolicyStageMarkers(markers []policyStageMarker, blockHeight uint64, stage string, tMs int64, persistAttempt int) {
	if a == nil || a.logger == nil || stage == "" || len(markers) == 0 {
		return
	}
	for _, marker := range markers {
		a.logPolicyStage(marker.policyID, marker.traceID, stage, tMs, blockHeight, marker.txIndex, persistAttempt)
	}
}

func (a *adapter) logPolicyStage(policyID, traceID, stage string, tMs int64, blockHeight uint64, txIndex int, persistAttempt int) {
	if a == nil || a.logger == nil || stage == "" || policyID == "" {
		return
	}
	if persistAttempt > 0 {
		a.logger.Info("policy stage marker",
			utils.ZapString("stage", stage),
			utils.ZapString("policy_id", policyID),
			utils.ZapString("trace_id", traceID),
			utils.ZapInt64("t_ms", tMs),
			utils.ZapUint64("height", blockHeight),
			utils.ZapInt("tx_index", txIndex),
			utils.ZapInt("persist_attempt", persistAttempt),
		)
		return
	}
	a.logger.Info("policy stage marker",
		utils.ZapString("stage", stage),
		utils.ZapString("policy_id", policyID),
		utils.ZapString("trace_id", traceID),
		utils.ZapInt64("t_ms", tMs),
		utils.ZapUint64("height", blockHeight),
		utils.ZapInt("tx_index", txIndex),
	)
}

func (a *adapter) logPolicyStageForPersist(blk *block.AppBlock, stage string, tMs int64) {
	if blk == nil || stage == "" {
		return
	}
	a.logPolicyStageMarkers(collectPolicyStageMarkers(blk), blk.GetHeight(), stage, tMs, 0)
}

func (a *adapter) logPolicyStageFromPayload(payload []byte, stage string, tMs int64, blockHeight uint64, txIndex int) {
	if len(payload) == 0 {
		return
	}
	policyID := parsePolicyID(payload)
	if policyID == "" {
		ruleHash := sha256.Sum256(payload)
		policyID = "invalid:" + hex.EncodeToString(ruleHash[:8])
	}
	traceID, _, _, _, _ := parsePolicyTrace(payload)
	if traceID == "" {
		ruleHash := sha256.Sum256(payload)
		traceID = fmt.Sprintf("trace:%s:%s", policyID, hex.EncodeToString(ruleHash[:8]))
	}
	a.logPolicyStage(policyID, traceID, stage, tMs, blockHeight, txIndex, 0)
}

// upsertSnapshot inserts or verifies state snapshot
func (a *adapter) upsertSnapshot(ctx context.Context, tx *sql.Tx, blk *block.AppBlock, stateRoot [32]byte, txCount int) error {
	blockHash := blk.GetHash()
	bHash := blockHash[:]
	sRoot := stateRoot[:]

	// Try INSERT with conflict-avoidance to keep transaction alive
	res, err := tx.ExecContext(ctx, `
		INSERT INTO state_versions (
			version, state_root, block_height, block_hash, tx_count,
			reputation_changes, policy_changes, quarantine_changes, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (version) DO NOTHING
	`,
		blk.GetHeight(), // version = height for now
		sRoot,
		blk.GetHeight(),
		bHash,
		txCount,
		0, // reputation_changes - TODO: calculate from receipts
		0, // policy_changes
		0, // quarantine_changes
	)

	if err != nil {
		return fmt.Errorf("failed to insert snapshot: %w", err)
	}

	if rows, _ := res.RowsAffected(); rows > 0 {
		return nil // Insert succeeded
	}

	// Conflict occurred (rows == 0), verify state_root matches
	var existingStateRoot []byte
	err = tx.QueryRowContext(ctx, `
		SELECT state_root FROM state_versions WHERE version = $1
	`, blk.GetHeight()).Scan(&existingStateRoot)

	if err != nil {
		return fmt.Errorf("failed to verify existing snapshot: %w", err)
	}

	if len(existingStateRoot) != 32 {
		return fmt.Errorf("%w: existing state_root invalid length", ErrIntegrityViolation)
	}

	var existingRoot [32]byte
	copy(existingRoot[:], existingStateRoot)

	if stateRoot != existingRoot {
		if a.auditLogger != nil {
			_ = a.auditLogger.Security("state_root_mismatch_detected", map[string]interface{}{
				"version":       blk.GetHeight(),
				"new_root":      fmt.Sprintf("%x", stateRoot[:]),
				"existing_root": fmt.Sprintf("%x", existingRoot[:]),
			})
		}
		return fmt.Errorf("%w: state_root mismatch at version %d", ErrIntegrityViolation, blk.GetHeight())
	}

	// State root matches, idempotent insert
	return nil
}

// GetBlock retrieves a block by height
func (a *adapter) GetBlock(ctx context.Context, height uint64) (*block.AppBlock, error) {
	stop := a.recordQuery("get_block")
	defer stop()
	var row BlockRow

	// Use prepared statement
	err := a.stmtGetBlock.QueryRowContext(ctx, height).Scan(
		&row.Height,
		&row.BlockHash,
		&row.ParentHash,
		&row.StateRoot,
		&row.TxRoot,
		&row.ProposerID,
		&row.ViewNumber,
		&row.Timestamp,
		&row.TxCount,
		&row.QCView,
		&row.QCSignatures,
		&row.CommittedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: height=%d", ErrBlockNotFound, height)
	}
	if err != nil {
		// SECURITY: Only log height, not data
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "failed to get block",
				utils.ZapError(err),
				utils.ZapUint64("height", height))
		}
		return nil, fmt.Errorf("query block: %w", err)
	}

	// Reconstruct block header (without transactions)
	if len(row.BlockHash) != 32 || len(row.ParentHash) != 32 || len(row.StateRoot) != 32 || len(row.ProposerID) != 32 || len(row.TxRoot) != 32 {
		return nil, fmt.Errorf("%w: invalid hash/id length in database", ErrInvalidData)
	}

	var blockHash, parentHash, stateRoot [32]byte
	var proposerID [32]byte
	var txRoot [32]byte
	copy(blockHash[:], row.BlockHash)
	copy(parentHash[:], row.ParentHash)
	copy(stateRoot[:], row.StateRoot)
	copy(proposerID[:], row.ProposerID)
	copy(txRoot[:], row.TxRoot)

	// Create block header without fetching transactions (performance optimization)
	// Transactions can be fetched separately if needed
	blkHeader := block.NewAppBlockHeader(
		row.Height,
		parentHash,
		txRoot,
		stateRoot,
		proposerID,
		row.Timestamp,
		row.TxCount,
	)

	return blkHeader, nil
}

// GetLatestHeight returns the maximum block height from the database
// Returns 0 if no blocks exist
func (a *adapter) GetLatestHeight(ctx context.Context) (uint64, error) {
	stop := a.recordQuery("get_latest_height")
	defer stop()
	var maxHeight sql.NullInt64

	query := `SELECT MAX(height) FROM blocks`
	err := a.db.QueryRowContext(ctx, query).Scan(&maxHeight)

	if err != nil {
		if a.logger != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				a.logger.DebugContext(ctx, "latest height probe timed out", utils.ZapError(err))
			} else {
				a.logger.ErrorContext(ctx, "failed to get latest height", utils.ZapError(err))
			}
		}
		return 0, fmt.Errorf("query latest height: %w", err)
	}

	// If no blocks exist, return 0
	if !maxHeight.Valid {
		return 0, nil
	}

	return uint64(maxHeight.Int64), nil
}

// GetMinHeight returns the minimum block height in the database
// Returns 0 if no blocks exist
func (a *adapter) GetMinHeight(ctx context.Context) (uint64, error) {
	stop := a.recordQuery("get_min_height")
	defer stop()
	var minHeight sql.NullInt64

	query := `SELECT MIN(height) FROM blocks`
	err := a.db.QueryRowContext(ctx, query).Scan(&minHeight)

	if err != nil {
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "failed to get min height", utils.ZapError(err))
		}
		return 0, fmt.Errorf("query min height: %w", err)
	}

	// If no blocks exist, return 0
	if !minHeight.Valid {
		return 0, nil
	}

	return uint64(minHeight.Int64), nil
}

// GetTransactionByContentHash retrieves a transaction by content hash
func (a *adapter) GetTransactionByContentHash(ctx context.Context, contentHash [32]byte) (*TxRow, error) {
	stop := a.recordQuery("get_tx_by_hash")
	defer stop()
	var row TxRow

	// Use prepared statement
	err := a.stmtGetTxByHash.QueryRowContext(ctx, contentHash[:]).Scan(
		&row.TxHash,
		&row.BlockHeight,
		&row.TxIndex,
		&row.TxType,
		&row.ProducerID,
		&row.Nonce,
		&row.ContentHash,
		&row.Algorithm,
		&row.PublicKey,
		&row.Signature,
		&row.Payload,
		&row.CustodyChain,
		&row.Status,
		&row.ErrorMsg,
		&row.SubmittedAt,
		&row.ExecutedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: content_hash=%x", ErrTransactionNotFound, contentHash[:8])
	}
	if err != nil {
		// SECURITY: Only log hash prefix, not payload
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "failed to get transaction",
				utils.ZapError(err),
				utils.ZapString("content_hash", fmt.Sprintf("%x", contentHash[:8])))
		}
		return nil, fmt.Errorf("query transaction: %w", err)
	}

	return &row, nil
}

// GetSnapshot retrieves a state snapshot by version
func (a *adapter) GetSnapshot(ctx context.Context, version uint64) (*SnapshotRow, error) {
	stop := a.recordQuery("get_snapshot")
	defer stop()
	var row SnapshotRow

	// Use prepared statement
	err := a.stmtGetSnapshot.QueryRowContext(ctx, version).Scan(
		&row.Version,
		&row.StateRoot,
		&row.BlockHeight,
		&row.BlockHash,
		&row.TxCount,
		&row.ReputationChanges,
		&row.PolicyChanges,
		&row.QuarantineChanges,
		&row.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: version=%d", ErrSnapshotNotFound, version)
	}
	if err != nil {
		// SECURITY: Only log version, not data
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "failed to get snapshot",
				utils.ZapError(err),
				utils.ZapUint64("version", version))
		}
		return nil, fmt.Errorf("query snapshot: %w", err)
	}

	return &row, nil
}

// ListTransactionsByBlock returns minimal transaction metadata for a block
func (a *adapter) ListTransactionsByBlock(ctx context.Context, height uint64) ([]TxMeta, error) {
	stop := a.recordQuery("list_transactions_by_block")
	defer stop()
	rows, err := a.stmtListTxByBlk.QueryContext(ctx, height)
	if err != nil {
		return nil, fmt.Errorf("query tx by block: %w", err)
	}
	defer rows.Close()

	metas := make([]TxMeta, 0)
	for rows.Next() {
		var m TxMeta
		if err := rows.Scan(&m.TxHash, &m.TxIndex, &m.TxType, &m.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan tx meta: %w", err)
		}
		metas = append(metas, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tx meta: %w", err)
	}
	return metas, nil
}

// Ping checks database liveness
func (a *adapter) Ping(ctx context.Context) error {
	stop := a.recordQuery("ping")
	defer stop()
	return a.db.PingContext(ctx)
}

// Close closes the database connection and prepared statements
func (a *adapter) Close() error {
	a.txMismatchSeenMu.Lock()
	a.txMismatchSeen = nil
	a.txMismatchSeenMu.Unlock()

	// Close prepared statements
	if a.stmtGetBlock != nil {
		a.stmtGetBlock.Close()
	}
	if a.stmtGetTxByHash != nil {
		a.stmtGetTxByHash.Close()
	}
	if a.stmtGetSnapshot != nil {
		a.stmtGetSnapshot.Close()
	}
	if a.stmtListTxByBlk != nil {
		a.stmtListTxByBlk.Close()
	}
	if a.stmtUpsertProposal != nil {
		a.stmtUpsertProposal.Close()
	}
	if a.stmtGetProposal != nil {
		a.stmtGetProposal.Close()
	}
	if a.stmtListProposals != nil {
		a.stmtListProposals.Close()
	}
	if a.stmtUpsertQC != nil {
		a.stmtUpsertQC.Close()
	}
	if a.stmtGetQC != nil {
		a.stmtGetQC.Close()
	}
	if a.stmtListQCs != nil {
		a.stmtListQCs.Close()
	}
	if a.stmtUpsertVote != nil {
		a.stmtUpsertVote.Close()
	}
	if a.stmtListVotes != nil {
		a.stmtListVotes.Close()
	}
	if a.stmtUpsertEvidence != nil {
		a.stmtUpsertEvidence.Close()
	}
	if a.stmtGetEvidence != nil {
		a.stmtGetEvidence.Close()
	}
	if a.stmtListEvidence != nil {
		a.stmtListEvidence.Close()
	}
	if a.stmtUpsertMeta != nil {
		a.stmtUpsertMeta.Close()
	}
	if a.stmtGetMeta != nil {
		a.stmtGetMeta.Close()
	}
	if a.stmtGetCommitted != nil {
		a.stmtGetCommitted.Close()
	}
	if a.stmtLoadGenesisCert != nil {
		a.stmtLoadGenesisCert.Close()
	}
	if a.stmtUpsertGenesisCert != nil {
		a.stmtUpsertGenesisCert.Close()
	}
	if a.stmtDeleteGenesisCert != nil {
		a.stmtDeleteGenesisCert.Close()
	}
	if a.stmtDeleteProposalsBefore != nil {
		a.stmtDeleteProposalsBefore.Close()
	}
	if a.stmtDeleteVotesBefore != nil {
		a.stmtDeleteVotesBefore.Close()
	}
	if a.stmtDeleteQCsBefore != nil {
		a.stmtDeleteQCsBefore.Close()
	}
	if a.stmtDeleteEvidenceBefore != nil {
		a.stmtDeleteEvidenceBefore.Close()
	}

	// Close database connection
	if a.db != nil {
		return a.db.Close()
	}

	return nil
}

// GetDB returns the underlying database connection for direct queries
// This is used by the API layer for statistics calculations
func (a *adapter) GetDB() *sql.DB {
	return a.db
}

// Metrics returns a snapshot of CockroachDB latency metrics.
func (a *adapter) Metrics() MetricsSnapshot {
	if a == nil || a.metrics == nil {
		return MetricsSnapshot{}
	}
	snap := a.metrics.snapshot()
	snap.BatchTxConfiguredSize = a.perf.txBatchSize
	snap.BatchOutboxConfiguredSize = a.perf.outboxBatchSize
	snap.TxStoreFullPayload = a.perf.txStoreFullPayload
	snap.BatchCanaryEnabled = a.perf.canaryAutoFallback
	snap.TxVerifyUseTx = a.perf.txVerifyUseTx
	snap.TxVerifyChunkSize = a.perf.txVerifyChunkSize
	snap.PersistTxIsolation = a.txIsolationLabel
	if a.txBatchCanary != nil {
		snap.BatchFallbackUntilUnixMs = a.txBatchCanary.fallbackUntilUnixMilli()
		snap.BatchFallbackActivations = a.txBatchCanary.activationCount()
		snap.BatchFallbackActive = a.txBatchCanary.isFallbackActive(time.Now())
		snap.TxBatchFallbackUntilUnixMs = snap.BatchFallbackUntilUnixMs
		snap.TxBatchFallbackActivations = snap.BatchFallbackActivations
		snap.TxBatchFallbackActive = snap.BatchFallbackActive
	}
	if a.outboxBatchCanary != nil {
		snap.OutboxBatchFallbackUntilUnixMs = a.outboxBatchCanary.fallbackUntilUnixMilli()
		snap.OutboxBatchFallbackActivations = a.outboxBatchCanary.activationCount()
		snap.OutboxBatchFallbackActive = a.outboxBatchCanary.isFallbackActive(time.Now())
	}
	if a.batchTuner != nil {
		snap.BatchTxCurrentSize = a.batchTuner.txSize(a.perf.txBatchSize)
		snap.BatchOutboxCurrentSize = a.batchTuner.outboxSize(a.perf.outboxBatchSize)
		snap.TxBatchAdaptiveScaleUpTotal = a.batchTuner.txScaleUp.Load()
		snap.TxBatchAdaptiveScaleDownTotal = a.batchTuner.txScaleDown.Load()
		snap.OutboxBatchAdaptiveScaleUpTotal = a.batchTuner.outboxScaleUp.Load()
		snap.OutboxBatchAdaptiveScaleDownTotal = a.batchTuner.outboxScaleDown.Load()
		// Backward compatibility aggregate.
		snap.BatchAdaptiveScaleUpTotal = snap.TxBatchAdaptiveScaleUpTotal + snap.OutboxBatchAdaptiveScaleUpTotal
		snap.BatchAdaptiveScaleDownTotal = snap.TxBatchAdaptiveScaleDownTotal + snap.OutboxBatchAdaptiveScaleDownTotal
	}
	return snap
}

func (a *adapter) recordQuery(label string) func() {
	if a == nil || a.metrics == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		a.metrics.observeQuery(label, time.Since(start))
	}
}

func (a *adapter) recordTxn(label string) func() {
	if a == nil || a.metrics == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		a.metrics.observeTxn(label, time.Since(start))
	}
}

// MetricsSnapshot contains aggregated latency and slow operation counters.
type MetricsSnapshot struct {
	QueryBuckets                      []utils.HistogramBucket
	QueryCount                        uint64
	QuerySumMs                        float64
	QueryP95Ms                        float64
	TxnBuckets                        []utils.HistogramBucket
	TxnCount                          uint64
	TxnSumMs                          float64
	TxnP95Ms                          float64
	SlowQueryCount                    uint64
	SlowTransactionCount              uint64
	SlowQueries                       map[string]uint64
	SlowTransactions                  map[string]uint64
	PersistStageBuckets               map[string][]utils.HistogramBucket
	PersistStageCount                 map[string]uint64
	PersistStageSumMs                 map[string]float64
	PersistStageP95Ms                 map[string]float64
	PersistFailureClassTotals         map[string]uint64
	PersistIntegrityKindTotals        map[string]uint64
	PersistContentionSignalTotals     map[string]uint64
	PersistDiagnosticSignalTotals     map[string]uint64
	TxUpsertBlocks                    uint64
	TxUpsertRows                      uint64
	TxUpsertConflicts                 uint64
	TxUpsertPayloadBytes              uint64
	TxUpsertConflictRatio             float64
	TxLocationMismatchTotal           uint64
	TxLocationMismatchBySource        map[string]uint64
	TxLocationMismatchByKind          map[string]uint64
	TxLocationMismatchByLabel         map[TxLocationMismatchLabel]uint64
	TxBatchModeTotals                 map[string]uint64
	OutboxBatchModeTotals             map[string]uint64
	TxBatchCanaryBadByReason          map[string]uint64
	OutboxBatchCanaryBadByReason      map[string]uint64
	BatchFallbackActive               bool
	BatchFallbackUntilUnixMs          int64
	BatchFallbackActivations          uint64
	TxBatchFallbackActive             bool
	TxBatchFallbackUntilUnixMs        int64
	TxBatchFallbackActivations        uint64
	OutboxBatchFallbackActive         bool
	OutboxBatchFallbackUntilUnixMs    int64
	OutboxBatchFallbackActivations    uint64
	BatchTxConfiguredSize             int
	BatchOutboxConfiguredSize         int
	BatchTxCurrentSize                int
	BatchOutboxCurrentSize            int
	BatchAdaptiveScaleUpTotal         uint64
	BatchAdaptiveScaleDownTotal       uint64
	TxBatchAdaptiveScaleUpTotal       uint64
	TxBatchAdaptiveScaleDownTotal     uint64
	OutboxBatchAdaptiveScaleUpTotal   uint64
	OutboxBatchAdaptiveScaleDownTotal uint64
	OutboxReturningRowsTotal          uint64
	OutboxReturningEstimatedBytes     uint64
	OutboxInsertRetriesTotal          uint64
	OutboxInsertRetrySerialization    uint64
	PersistTxIsolation                string
	TxStoreFullPayload                bool
	BatchCanaryEnabled                bool
	TxVerifyUseTx                     bool
	TxVerifyChunkSize                 int
}

type TxLocationMismatchLabel struct {
	Source string
	Kind   string
}

type dbMetrics struct {
	queryLatency                *utils.LatencyHistogram
	txnLatency                  *utils.LatencyHistogram
	slowQueryThresholdMs        float64
	slowTxnThresholdMs          float64
	slowQueryCount              atomic.Uint64
	slowTxnCount                atomic.Uint64
	slowQueryLabels             sync.Map
	slowTxnLabels               sync.Map
	persistStages               sync.Map
	persistFailureClasses       sync.Map
	persistIntegrityKinds       sync.Map
	persistContentionSignals    sync.Map
	persistDiagnosticSignals    sync.Map
	txUpsertBlocks              atomic.Uint64
	txUpsertRows                atomic.Uint64
	txUpsertConflicts           atomic.Uint64
	txUpsertPayloadBytes        atomic.Uint64
	txLocationMismatchTotal     atomic.Uint64
	txLocationMismatchBySource  sync.Map
	txLocationMismatchByKind    sync.Map
	txLocationMismatchByLabel   sync.Map
	txBatchModes                sync.Map
	outboxBatchModes            sync.Map
	txBatchCanaryBadReasons     sync.Map
	outboxBatchCanaryBadReasons sync.Map
	outboxReturningRowsTotal    atomic.Uint64
	outboxReturningBytesTotal   atomic.Uint64
	outboxInsertRetriesTotal    atomic.Uint64
	outboxInsertRetry40001Total atomic.Uint64
}

func newDBMetrics() *dbMetrics {
	return &dbMetrics{
		queryLatency:         utils.NewLatencyHistogram([]float64{1, 5, 20, 50, 100, 250, 500, 1000, 2500, 5000}),
		txnLatency:           utils.NewLatencyHistogram([]float64{5, 20, 50, 100, 250, 500, 1000, 2500, 5000, 10000}),
		slowQueryThresholdMs: 250,
		slowTxnThresholdMs:   500,
	}
}

func (m *dbMetrics) observeQuery(label string, d time.Duration) {
	if m == nil {
		return
	}
	ms := float64(d) / float64(time.Millisecond)
	m.queryLatency.Observe(ms)
	if ms > m.slowQueryThresholdMs {
		m.slowQueryCount.Add(1)
		incrementLabel(&m.slowQueryLabels, label)
	}
}

func (m *dbMetrics) observeTxn(label string, d time.Duration) {
	if m == nil {
		return
	}
	ms := float64(d) / float64(time.Millisecond)
	m.txnLatency.Observe(ms)
	if ms > m.slowTxnThresholdMs {
		m.slowTxnCount.Add(1)
		incrementLabel(&m.slowTxnLabels, label)
	}
}

type stageMetric struct {
	hist *utils.LatencyHistogram
}

func (m *dbMetrics) observePersistStage(label string, d time.Duration) {
	if m == nil {
		return
	}
	if label == "" {
		label = "unknown"
	}
	val, ok := m.persistStages.Load(label)
	if !ok {
		created := &stageMetric{
			hist: utils.NewLatencyHistogram([]float64{1, 5, 20, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 15000}),
		}
		actual, loaded := m.persistStages.LoadOrStore(label, created)
		if loaded {
			val = actual
		} else {
			val = created
		}
	}
	if sm, ok := val.(*stageMetric); ok && sm.hist != nil {
		sm.hist.Observe(float64(d) / float64(time.Millisecond))
	}
}

func (m *dbMetrics) persistStageQuantile(label string, q float64) float64 {
	if m == nil || label == "" {
		return 0
	}
	val, ok := m.persistStages.Load(label)
	if !ok {
		return 0
	}
	sm, ok := val.(*stageMetric)
	if !ok || sm.hist == nil {
		return 0
	}
	return sm.hist.Quantile(q)
}

func (m *dbMetrics) observePersistFailureClass(stage string, err error) {
	if m == nil || err == nil {
		return
	}
	class := classifyPersistError(err)
	key := stage + ":" + class
	incrementLabel(&m.persistFailureClasses, key)
}

func (m *dbMetrics) observePersistIntegrityKind(kind string) {
	if m == nil {
		return
	}
	if kind == "" {
		kind = "unknown"
	}
	incrementLabel(&m.persistIntegrityKinds, kind)
}

func (m *dbMetrics) observePersistContentionSignal(signal string) {
	if m == nil {
		return
	}
	if signal == "" {
		signal = "unknown"
	}
	incrementLabel(&m.persistContentionSignals, signal)
}

func (m *dbMetrics) observePersistDiagnosticSignal(signal string) {
	if m == nil {
		return
	}
	if signal == "" {
		signal = "unknown"
	}
	incrementLabel(&m.persistDiagnosticSignals, signal)
}

func (m *dbMetrics) observeTxUpsertStats(rows, conflicts, payloadBytes int) {
	if m == nil {
		return
	}
	if rows > 0 {
		m.txUpsertBlocks.Add(1)
		m.txUpsertRows.Add(uint64(rows))
	}
	if conflicts > 0 {
		m.txUpsertConflicts.Add(uint64(conflicts))
	}
	if payloadBytes > 0 {
		m.txUpsertPayloadBytes.Add(uint64(payloadBytes))
	}
}

func (m *dbMetrics) observeTxLocationMismatch(source, kind string) {
	if m == nil {
		return
	}
	if source == "" {
		source = "unknown"
	}
	if kind == "" {
		kind = "unknown"
	}
	m.txLocationMismatchTotal.Add(1)
	incrementLabel(&m.txLocationMismatchBySource, source)
	incrementLabel(&m.txLocationMismatchByKind, kind)
	incrementLabel(&m.txLocationMismatchByLabel, TxLocationMismatchLabel{Source: source, Kind: kind})
}

func (m *dbMetrics) observeTxBatchMode(mode string) {
	if m == nil {
		return
	}
	incrementLabel(&m.txBatchModes, mode)
}

func (m *dbMetrics) observeOutboxBatchMode(mode string) {
	if m == nil {
		return
	}
	incrementLabel(&m.outboxBatchModes, mode)
}

func (m *dbMetrics) observeTxBatchCanaryBad(reason string) {
	if m == nil {
		return
	}
	incrementLabel(&m.txBatchCanaryBadReasons, reason)
}

func (m *dbMetrics) observeOutboxBatchCanaryBad(reason string) {
	if m == nil {
		return
	}
	incrementLabel(&m.outboxBatchCanaryBadReasons, reason)
}

func (m *dbMetrics) observeOutboxReturning(rows, estimatedBytes int) {
	if m == nil {
		return
	}
	if rows > 0 {
		m.outboxReturningRowsTotal.Add(uint64(rows))
	}
	if estimatedBytes > 0 {
		m.outboxReturningBytesTotal.Add(uint64(estimatedBytes))
	}
}

func (m *dbMetrics) observeOutboxInsertRetry(sqlstate string) {
	if m == nil {
		return
	}
	m.outboxInsertRetriesTotal.Add(1)
	if sqlstate == "40001" {
		m.outboxInsertRetry40001Total.Add(1)
	}
}

func (m *dbMetrics) snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	queryBuckets, qCount, qSum := m.queryLatency.Snapshot()
	txnBuckets, tCount, tSum := m.txnLatency.Snapshot()
	snapshot := MetricsSnapshot{
		QueryBuckets:                   queryBuckets,
		QueryCount:                     qCount,
		QuerySumMs:                     qSum,
		QueryP95Ms:                     m.queryLatency.Quantile(0.95),
		TxnBuckets:                     txnBuckets,
		TxnCount:                       tCount,
		TxnSumMs:                       tSum,
		TxnP95Ms:                       m.txnLatency.Quantile(0.95),
		SlowQueryCount:                 m.slowQueryCount.Load(),
		SlowTransactionCount:           m.slowTxnCount.Load(),
		SlowQueries:                    make(map[string]uint64),
		SlowTransactions:               make(map[string]uint64),
		PersistStageBuckets:            make(map[string][]utils.HistogramBucket),
		PersistStageCount:              make(map[string]uint64),
		PersistStageSumMs:              make(map[string]float64),
		PersistStageP95Ms:              make(map[string]float64),
		PersistFailureClassTotals:      make(map[string]uint64),
		PersistIntegrityKindTotals:     make(map[string]uint64),
		PersistContentionSignalTotals:  make(map[string]uint64),
		PersistDiagnosticSignalTotals:  make(map[string]uint64),
		TxUpsertBlocks:                 m.txUpsertBlocks.Load(),
		TxUpsertRows:                   m.txUpsertRows.Load(),
		TxUpsertConflicts:              m.txUpsertConflicts.Load(),
		TxUpsertPayloadBytes:           m.txUpsertPayloadBytes.Load(),
		TxLocationMismatchTotal:        m.txLocationMismatchTotal.Load(),
		TxLocationMismatchBySource:     make(map[string]uint64),
		TxLocationMismatchByKind:       make(map[string]uint64),
		TxLocationMismatchByLabel:      make(map[TxLocationMismatchLabel]uint64),
		TxBatchModeTotals:              make(map[string]uint64),
		OutboxBatchModeTotals:          make(map[string]uint64),
		TxBatchCanaryBadByReason:       make(map[string]uint64),
		OutboxBatchCanaryBadByReason:   make(map[string]uint64),
		OutboxReturningRowsTotal:       m.outboxReturningRowsTotal.Load(),
		OutboxReturningEstimatedBytes:  m.outboxReturningBytesTotal.Load(),
		OutboxInsertRetriesTotal:       m.outboxInsertRetriesTotal.Load(),
		OutboxInsertRetrySerialization: m.outboxInsertRetry40001Total.Load(),
	}
	if snapshot.TxUpsertRows > 0 {
		snapshot.TxUpsertConflictRatio = float64(snapshot.TxUpsertConflicts) / float64(snapshot.TxUpsertRows)
	}
	m.slowQueryLabels.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			snapshot.SlowQueries[key.(string)] = counter.Load()
		}
		return true
	})
	m.txLocationMismatchBySource.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if source, ok := key.(string); ok {
				snapshot.TxLocationMismatchBySource[source] = counter.Load()
			}
		}
		return true
	})
	m.txLocationMismatchByKind.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if kind, ok := key.(string); ok {
				snapshot.TxLocationMismatchByKind[kind] = counter.Load()
			}
		}
		return true
	})
	m.txLocationMismatchByLabel.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if label, ok := key.(TxLocationMismatchLabel); ok {
				snapshot.TxLocationMismatchByLabel[label] = counter.Load()
			}
		}
		return true
	})
	m.slowTxnLabels.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			snapshot.SlowTransactions[key.(string)] = counter.Load()
		}
		return true
	})
	m.persistStages.Range(func(key, value any) bool {
		name, ok := key.(string)
		if !ok {
			return true
		}
		sm, ok := value.(*stageMetric)
		if !ok || sm.hist == nil {
			return true
		}
		buckets, count, sum := sm.hist.Snapshot()
		snapshot.PersistStageBuckets[name] = buckets
		snapshot.PersistStageCount[name] = count
		snapshot.PersistStageSumMs[name] = sum
		snapshot.PersistStageP95Ms[name] = sm.hist.Quantile(0.95)
		return true
	})
	m.persistFailureClasses.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if name, ok := key.(string); ok {
				snapshot.PersistFailureClassTotals[name] = counter.Load()
			}
		}
		return true
	})
	m.persistIntegrityKinds.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if name, ok := key.(string); ok {
				snapshot.PersistIntegrityKindTotals[name] = counter.Load()
			}
		}
		return true
	})
	m.persistContentionSignals.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if name, ok := key.(string); ok {
				snapshot.PersistContentionSignalTotals[name] = counter.Load()
			}
		}
		return true
	})
	m.persistDiagnosticSignals.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if name, ok := key.(string); ok {
				snapshot.PersistDiagnosticSignalTotals[name] = counter.Load()
			}
		}
		return true
	})
	m.txBatchModes.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if name, ok := key.(string); ok {
				snapshot.TxBatchModeTotals[name] = counter.Load()
			}
		}
		return true
	})
	m.outboxBatchModes.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if name, ok := key.(string); ok {
				snapshot.OutboxBatchModeTotals[name] = counter.Load()
			}
		}
		return true
	})
	m.txBatchCanaryBadReasons.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if name, ok := key.(string); ok {
				snapshot.TxBatchCanaryBadByReason[name] = counter.Load()
			}
		}
		return true
	})
	m.outboxBatchCanaryBadReasons.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if name, ok := key.(string); ok {
				snapshot.OutboxBatchCanaryBadByReason[name] = counter.Load()
			}
		}
		return true
	})
	return snapshot
}

func classifyPersistError(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, ErrIntegrityViolation) {
		return "integrity"
	}
	if errors.Is(err, ErrInvalidData) {
		return "invalid_data"
	}
	if state := extractSQLState(err); state != "" {
		switch state {
		case "40001":
			return "retry_serialization"
		case "40P01":
			return "retry_deadlock"
		case "55P03":
			return "retry_lock_not_available"
		case "57014":
			return "timeout_query_canceled"
		case "23505":
			return "constraint_unique"
		case "23503":
			return "constraint_foreign_key"
		case "23514":
			return "constraint_check"
		case "23502":
			return "constraint_not_null"
		case "22001", "22003", "22007", "22008", "22P02":
			return "invalid_data"
		case "08000", "08001", "08003", "08004", "08006":
			return "connection"
		}
		return "sqlstate_" + strings.ToLower(state)
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "restart transaction"), strings.Contains(msg, "40001"), strings.Contains(msg, "serialization"):
		return "retry_serialization"
	case strings.Contains(msg, "40p01"), strings.Contains(msg, "deadlock"):
		return "retry_deadlock"
	case strings.Contains(msg, "55p03"), strings.Contains(msg, "lock_not_available"), strings.Contains(msg, "lock timeout"):
		return "retry_lock_not_available"
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "timeout"):
		return "timeout"
	case strings.Contains(msg, "fenced/no-op"):
		return "fenced"
	case strings.Contains(msg, "23505"), strings.Contains(msg, "duplicate key"), strings.Contains(msg, "unique constraint"):
		return "constraint_unique"
	case strings.Contains(msg, "23503"), strings.Contains(msg, "foreign key"):
		return "constraint_foreign_key"
	case strings.Contains(msg, "23514"), strings.Contains(msg, "check constraint"):
		return "constraint_check"
	case strings.Contains(msg, "23502"), strings.Contains(msg, "not-null"):
		return "constraint_not_null"
	case strings.Contains(msg, "22p02"), strings.Contains(msg, "invalid input syntax"):
		return "invalid_data"
	case strings.Contains(msg, "connection reset"), strings.Contains(msg, "connection refused"), strings.Contains(msg, "broken pipe"):
		return "connection"
	default:
		return "other"
	}
}

func isTimeoutOrCanceledError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	state := extractSQLState(err)
	if state == "57014" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "query execution canceled") ||
		strings.Contains(msg, "timeout")
}

func extractSQLState(err error) string {
	if err == nil {
		return ""
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func incrementLabel(store *sync.Map, label any) {
	if label == nil {
		label = "unknown"
	}
	if val, ok := store.Load(label); ok {
		if counter, ok2 := val.(*atomic.Uint64); ok2 {
			counter.Add(1)
			return
		}
	}
	counter := &atomic.Uint64{}
	existing, loaded := store.LoadOrStore(label, counter)
	if loaded {
		if c, ok := existing.(*atomic.Uint64); ok {
			c.Add(1)
			return
		}
	}
	counter.Add(1)
}
