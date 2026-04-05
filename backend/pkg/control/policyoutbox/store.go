package policyoutbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/control/lifecycleaudit"
	"github.com/jackc/pgconn"
	"github.com/lib/pq"
)

const (
	maxClaimAttemptsHard         = 3
	minClaimAttemptBudget        = 50 * time.Millisecond
	reclaimPhaseMinBudget        = 400 * time.Millisecond
	compatPhaseMaxBudget         = 300 * time.Millisecond
	markPublishedRecoveryBudget  = 30 * time.Second
	markPublishedRecoveryPerTry  = 10 * time.Second
	maxMarkPublishedRecoverTries = 6
	maxLegacyDispatchShardLabel  = 999
)

var errClaimBudgetExhausted = errors.New("policy outbox: claim budget exhausted")

type Store struct {
	db                *sql.DB
	hasRequestColumn  bool
	hasCommandColumn  bool
	hasWorkflowColumn bool
	hasSourceColumns  bool
	hasSentinelColumn bool
	hasDispatchShard  bool
	reclaimTimeouts   uint64
	backlogSummaryMu  sync.Mutex
	backlogSummaryAt  time.Time
	backlogSummaryTTL time.Duration
	backlogSummary    BacklogStats
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("policy outbox: db required")
	}
	return &Store{
		db:                db,
		backlogSummaryTTL: 30 * time.Second,
	}, nil
}

func (s *Store) ReclaimTimeoutsTotal() uint64 {
	if s == nil {
		return 0
	}
	return atomic.LoadUint64(&s.reclaimTimeouts)
}

// EnsureSchema verifies required outbox tables/columns exist before runtime.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy outbox: store not initialized")
	}

	var outboxReg, leaseReg sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT to_regclass('control_policy_outbox')::STRING, to_regclass('control_dispatcher_leases')::STRING
	`).Scan(&outboxReg, &leaseReg); err != nil {
		return fmt.Errorf("policy outbox: schema check failed: %w", err)
	}
	if !outboxReg.Valid || outboxReg.String == "" {
		return fmt.Errorf("policy outbox: required table control_policy_outbox missing")
	}
	if !leaseReg.Valid || leaseReg.String == "" {
		return fmt.Errorf("policy outbox: required table control_dispatcher_leases missing")
	}

	var traceCols int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'control_policy_outbox'
		  AND column_name IN ('trace_id', 'ai_event_ts_ms')
	`).Scan(&traceCols); err != nil {
		return fmt.Errorf("policy outbox: trace column check failed: %w", err)
	}
	if traceCols < 2 {
		return fmt.Errorf("policy outbox: required trace columns missing; ensure migration 008 applied")
	}
	var sourceCols int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'control_policy_outbox'
		  AND column_name IN ('source_event_id', 'source_event_ts_ms')
	`).Scan(&sourceCols); err != nil {
		return fmt.Errorf("policy outbox: source trace column check failed: %w", err)
	}
	s.hasSourceColumns = sourceCols >= 2
	var requestCols int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'control_policy_outbox'
		  AND column_name = 'request_id'
	`).Scan(&requestCols); err != nil {
		return fmt.Errorf("policy outbox: request_id column check failed: %w", err)
	}
	s.hasRequestColumn = requestCols >= 1
	var commandCols int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'control_policy_outbox'
		  AND column_name = 'command_id'
	`).Scan(&commandCols); err != nil {
		return fmt.Errorf("policy outbox: command_id column check failed: %w", err)
	}
	s.hasCommandColumn = commandCols >= 1
	var workflowCols int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'control_policy_outbox'
		  AND column_name = 'workflow_id'
	`).Scan(&workflowCols); err != nil {
		return fmt.Errorf("policy outbox: workflow_id column check failed: %w", err)
	}
	s.hasWorkflowColumn = workflowCols >= 1
	var sentinelCols int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'control_policy_outbox'
		  AND column_name = 'sentinel_event_id'
	`).Scan(&sentinelCols); err != nil {
		return fmt.Errorf("policy outbox: sentinel event column check failed: %w", err)
	}
	s.hasSentinelColumn = sentinelCols >= 1
	var dispatchShardCols int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'control_policy_outbox'
		  AND column_name = 'dispatch_shard'
	`).Scan(&dispatchShardCols); err != nil {
		return fmt.Errorf("policy outbox: dispatch shard column check failed: %w", err)
	}
	s.hasDispatchShard = dispatchShardCols >= 1
	if !s.hasDispatchShard {
		return fmt.Errorf("policy outbox: required dispatch_shard column missing; ensure migration 028 applied")
	}

	return nil
}

func (s *Store) TryAcquireLease(ctx context.Context, leaseKey, holderID string, ttl time.Duration) (bool, int64, error) {
	if s == nil || s.db == nil {
		return false, 0, fmt.Errorf("policy outbox: store not initialized")
	}
	if leaseKey == "" || holderID == "" {
		return false, 0, fmt.Errorf("policy outbox: lease key and holder required")
	}
	if ttl <= 0 {
		return false, 0, fmt.Errorf("policy outbox: ttl must be positive")
	}

	// Step 1: Create lease row if missing.
	insRows, err := s.db.QueryContext(ctx, `
		INSERT INTO control_dispatcher_leases (lease_key, holder_id, epoch, lease_until, updated_at)
		VALUES ($1, $2, 1, now() + $3::INTERVAL, now())
		ON CONFLICT (lease_key) DO NOTHING
		RETURNING holder_id, epoch
	`, leaseKey, holderID, ttl.String())
	if err != nil {
		return false, 0, fmt.Errorf("policy outbox: acquire lease insert: %w", err)
	}
	defer insRows.Close()

	var holder string
	var epoch int64
	if insRows.Next() {
		if scanErr := insRows.Scan(&holder, &epoch); scanErr != nil {
			return false, 0, fmt.Errorf("policy outbox: acquire lease insert scan: %w", scanErr)
		}
		if rowsErr := insRows.Err(); rowsErr != nil {
			return false, 0, fmt.Errorf("policy outbox: acquire lease insert rows: %w", rowsErr)
		}
		return true, epoch, nil
	}
	if rowsErr := insRows.Err(); rowsErr != nil {
		return false, 0, fmt.Errorf("policy outbox: acquire lease insert rows: %w", rowsErr)
	}

	// Step 2: Renew if already holder, or take over if expired.
	updRows, err := s.db.QueryContext(ctx, `
		UPDATE control_dispatcher_leases
		SET holder_id = $2,
			epoch = CASE
				WHEN control_dispatcher_leases.holder_id = $2 THEN control_dispatcher_leases.epoch
				ELSE control_dispatcher_leases.epoch + 1
			END,
			lease_until = now() + $3::INTERVAL,
			updated_at = now()
		WHERE lease_key = $1
		  AND (control_dispatcher_leases.holder_id = $2 OR control_dispatcher_leases.lease_until < now())
		RETURNING holder_id, epoch
	`, leaseKey, holderID, ttl.String())
	if err != nil {
		return false, 0, fmt.Errorf("policy outbox: acquire lease update: %w", err)
	}
	defer updRows.Close()
	if updRows.Next() {
		if scanErr := updRows.Scan(&holder, &epoch); scanErr != nil {
			return false, 0, fmt.Errorf("policy outbox: acquire lease update scan: %w", scanErr)
		}
		if rowsErr := updRows.Err(); rowsErr != nil {
			return false, 0, fmt.Errorf("policy outbox: acquire lease update rows: %w", rowsErr)
		}
		return holder == holderID, epoch, nil
	}
	if rowsErr := updRows.Err(); rowsErr != nil {
		return false, 0, fmt.Errorf("policy outbox: acquire lease update rows: %w", rowsErr)
	}

	// Step 3: Read current holder (no write for non-holders while lease is valid).
	err = s.db.QueryRowContext(ctx, `
		SELECT holder_id, epoch
		FROM control_dispatcher_leases
		WHERE lease_key = $1
	`, leaseKey).Scan(&holder, &epoch)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("policy outbox: acquire lease read current: %w", err)
	}
	return holder == holderID, epoch, nil
}

// ReleaseLease relinquishes a held lease early so another dispatcher holder can take over.
// This is best-effort and guarded by holder/epoch ownership checks.
func (s *Store) ReleaseLease(ctx context.Context, leaseKey, holderID string, epoch int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy outbox: store not initialized")
	}
	leaseKey = strings.TrimSpace(leaseKey)
	holderID = strings.TrimSpace(holderID)
	if leaseKey == "" || holderID == "" {
		return fmt.Errorf("policy outbox: lease key and holder required")
	}
	if epoch <= 0 {
		return fmt.Errorf("policy outbox: lease epoch required")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE control_dispatcher_leases
		SET lease_until = now() - INTERVAL '1s',
		    updated_at = now()
		WHERE lease_key = $1
		  AND holder_id = $2
		  AND epoch = $3
	`, leaseKey, holderID, epoch)
	if err != nil {
		return fmt.Errorf("policy outbox: release lease: %w", err)
	}
	return nil
}

func (s *Store) ClaimPending(ctx context.Context, holderID string, epoch int64, limit int, reclaimAfter time.Duration, leaseKey string, dispatchShard string, dispatchShardCount int, shardCompat bool) ([]Row, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("policy outbox: store not initialized")
	}
	if holderID == "" {
		return nil, fmt.Errorf("policy outbox: holder required")
	}
	if limit <= 0 {
		limit = 100
	}
	if reclaimAfter <= 0 {
		reclaimAfter = 30 * time.Second
	}
	leaseKey = strings.TrimSpace(leaseKey)
	if leaseKey == "" {
		return nil, fmt.Errorf("policy outbox: lease key required")
	}
	dispatchShard = strings.TrimSpace(dispatchShard)
	if dispatchShard == "" {
		dispatchShard = dispatchShardLabel(0)
	}
	if dispatchShardCount <= 1 {
		dispatchShardCount = 1
	}
	dispatchShardBucket := parseDispatchShardBucket(dispatchShard)
	strictPendingDispatchPredicate := strictDispatchShardPredicate("o.dispatch_shard", "$4")
	strictReclaimDispatchPredicate := strictDispatchShardPredicate("o.dispatch_shard", "$5")
	pendingArgs := []any{limit, holderID, epoch, dispatchShard}
	reclaimArgs := []any{limit, holderID, epoch, reclaimAfter.String(), dispatchShard, leaseKey}
	pendingCompatArgs := []any{limit, holderID, epoch}
	reclaimCompatArgs := []any{limit, holderID, epoch, reclaimAfter.String(), leaseKey}
	compatPendingDispatchPredicate := ""
	compatReclaimDispatchPredicate := ""
	compatNullPendingDispatchPredicate := ""
	compatNullReclaimDispatchPredicate := ""
	if shardCompat {
		compatPendingDispatchPredicate = compatibleDispatchShardPredicateLiteral("o.dispatch_shard", dispatchShardBucket, dispatchShardCount)
		compatReclaimDispatchPredicate = compatPendingDispatchPredicate
		compatNullPendingDispatchPredicate = compatibleNullDispatchShardPredicate("o.dispatch_shard", dispatchShardBucket)
		compatNullReclaimDispatchPredicate = compatNullPendingDispatchPredicate
	}

	pendingClause := `
					o.status IN ('pending', 'retry')
					AND (o.next_retry_at IS NULL OR o.next_retry_at <= now())
	`
	reclaimClauseStrict := `
					o.status = 'publishing'
					AND (
						o.updated_at < now() - $4::INTERVAL
						AND (
							(o.lease_holder = $2 AND o.lease_epoch = $3)
							OR
							o.lease_holder IS NULL
							OR NOT EXISTS (
							SELECT 1
							FROM control_dispatcher_leases l
							WHERE l.holder_id = o.lease_holder
							  AND l.epoch = o.lease_epoch
							  AND l.lease_key = $6
							  AND l.lease_until >= now()
							)
						)
					)
	`
	reclaimClauseCompat := `
					o.status = 'publishing'
					AND (
						o.updated_at < now() - $4::INTERVAL
						AND (
							(o.lease_holder = $2 AND o.lease_epoch = $3)
							OR
							o.lease_holder IS NULL
							OR NOT EXISTS (
							SELECT 1
							FROM control_dispatcher_leases l
							WHERE l.holder_id = o.lease_holder
							  AND l.epoch = o.lease_epoch
							  AND l.lease_key = $5
							  AND l.lease_until >= now()
							)
						)
					)
	`
	buildClaimQuery := func(fromClause string, dispatchPredicate string, whereClause string) string {
		return `
		WITH candidates AS (
			SELECT o.id
			FROM ` + fromClause + ` o
			WHERE ` + dispatchPredicate + `
			  AND (
` + whereClause + `
			  )
			ORDER BY o.created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE control_policy_outbox
		SET status='publishing',
			lease_holder=$2,
			lease_epoch=$3,
			updated_at=now()
		WHERE id IN (SELECT id FROM candidates)
	`
	}
	strictFrom := "control_policy_outbox@idx_control_policy_outbox_dispatch_claim"
	compatFrom := "control_policy_outbox"
	pendingQuery := buildClaimQuery(strictFrom, strictPendingDispatchPredicate, pendingClause)
	reclaimQuery := buildClaimQuery(strictFrom, strictReclaimDispatchPredicate, reclaimClauseStrict)
	pendingCompatQuery := ""
	reclaimCompatQuery := ""
	pendingNullCompatQuery := ""
	reclaimNullCompatQuery := ""
	if shardCompat {
		// Keep compatibility claims index-friendly by enumerating valid shard labels
		// for this bucket rather than evaluating modulo/regex expressions per row.
		pendingCompatQuery = buildClaimQuery(strictFrom, compatPendingDispatchPredicate, pendingClause)
		reclaimCompatQuery = buildClaimQuery(strictFrom, compatReclaimDispatchPredicate, reclaimClauseCompat)
		if compatNullPendingDispatchPredicate != "FALSE" {
			pendingNullCompatQuery = buildClaimQuery(compatFrom, compatNullPendingDispatchPredicate, pendingClause)
			reclaimNullCompatQuery = buildClaimQuery(compatFrom, compatNullReclaimDispatchPredicate, reclaimClauseCompat)
		}
	}
	requestSelect := "''"
	if s.hasRequestColumn {
		requestSelect = "COALESCE(request_id, '')"
	}
	commandSelect := "''"
	if s.hasCommandColumn {
		commandSelect = "COALESCE(command_id, '')"
	}
	workflowSelect := "''"
	if s.hasWorkflowColumn {
		workflowSelect = "COALESCE(workflow_id, '')"
	}
	returningClause := ""
	if s.hasSourceColumns {
		if s.hasSentinelColumn {
			returningClause = `
		RETURNING id::STRING, COALESCE(dispatch_shard, '` + dispatchShardLabel(0) + `'), block_height, block_ts, CAST(EXTRACT(EPOCH FROM created_at) * 1000 AS INT8), tx_index, policy_id, ` + requestSelect + `, ` + commandSelect + `, ` + workflowSelect + `, COALESCE(trace_id, ''), COALESCE(ai_event_ts_ms, 0), COALESCE(source_event_id, ''), COALESCE(source_event_ts_ms, 0), COALESCE(sentinel_event_id, ''), rule_hash, payload, status, retries, lease_epoch`
		} else {
			returningClause = `
		RETURNING id::STRING, COALESCE(dispatch_shard, '` + dispatchShardLabel(0) + `'), block_height, block_ts, CAST(EXTRACT(EPOCH FROM created_at) * 1000 AS INT8), tx_index, policy_id, ` + requestSelect + `, ` + commandSelect + `, ` + workflowSelect + `, COALESCE(trace_id, ''), COALESCE(ai_event_ts_ms, 0), COALESCE(source_event_id, ''), COALESCE(source_event_ts_ms, 0), rule_hash, payload, status, retries, lease_epoch`
		}
	} else {
		returningClause = `
		RETURNING id::STRING, COALESCE(dispatch_shard, '` + dispatchShardLabel(0) + `'), block_height, block_ts, CAST(EXTRACT(EPOCH FROM created_at) * 1000 AS INT8), tx_index, policy_id, ` + requestSelect + `, ` + commandSelect + `, ` + workflowSelect + `, COALESCE(trace_id, ''), COALESCE(ai_event_ts_ms, 0), rule_hash, payload, status, retries, lease_epoch`
	}
	pendingQuery += returningClause
	reclaimQuery += returningClause
	if shardCompat {
		pendingCompatQuery += returningClause
		reclaimCompatQuery += returningClause
	}
	totalBudget := remainingContextBudget(ctx)
	pendingBudget := time.Duration(0)
	compatBudget := time.Duration(0)
	reclaimBudget := time.Duration(0)
	if totalBudget > 0 {
		reclaimBudget = minDuration(reclaimPhaseMinBudget, totalBudget/3)
		if reclaimBudget < minClaimAttemptBudget {
			reclaimBudget = minDuration(minClaimAttemptBudget, totalBudget)
		}
		pendingBudget = totalBudget - reclaimBudget
		if pendingBudget < minClaimAttemptBudget {
			pendingBudget = minClaimAttemptBudget
		}
		if shardCompat {
			compatBudget = minDuration(compatPhaseMaxBudget, pendingBudget/3)
			if compatBudget < minClaimAttemptBudget {
				compatBudget = minClaimAttemptBudget
			}
			if compatBudget > pendingBudget {
				compatBudget = pendingBudget
			}
			pendingBudget -= compatBudget
		}
	}

	var claimRowsWithTimeout func(parent context.Context, query string, args []any, timeout time.Duration, detached bool) ([]Row, error)
	claimRowsWithTimeout = func(parent context.Context, query string, args []any, timeout time.Duration, detached bool) ([]Row, error) {
		attemptCtx := parent
		cancel := func() {}
		if timeout > 0 {
			root := parent
			if detached {
				// Only detach when the parent cannot provide the phase budget.
				// This preserves service-bound cancellation when enough budget exists.
				if deadline, ok := parent.Deadline(); !ok || time.Until(deadline) < timeout {
					// Detach per-phase claim budget from near-exhausted caller deadlines
					// while retaining context values for tracing/audit.
					// We still honor explicit parent cancellation (checked below).
					root = context.WithoutCancel(parent)
				}
			}
			attemptCtx, cancel = context.WithTimeout(root, timeout)
		}
		defer cancel()

		var lastErr error
		maxAttempts := claimMaxAttemptsForTimeout(timeout)
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if detached && parent != nil && errors.Is(parent.Err(), context.Canceled) {
				return nil, fmt.Errorf("policy outbox: claim pending: %w", context.Canceled)
			}
			if isBudgetNearlyExhausted(attemptCtx, minClaimAttemptBudget) {
				budgetErr := errors.Join(errClaimBudgetExhausted, context.DeadlineExceeded, lastErr)
				return nil, fmt.Errorf("policy outbox: claim pending: %w", budgetErr)
			}
			out, err := s.claimPendingOnce(attemptCtx, query, args...)
			if err == nil {
				return out, nil
			}
			lastErr = err
			// Under cloud Cockroach pressure, connect/dial timeouts are not
			// likely to recover within this claim phase budget. Fail fast so the
			// dispatcher can cooldown the shard and avoid retry-amplifying DB load.
			if isClaimConnectErr(err) {
				break
			}
			if !isRetryableClaimErr(err) || attempt == maxAttempts {
				break
			}
			backoff := time.Duration(attempt*25) * time.Millisecond
			if remaining := remainingContextBudget(attemptCtx); remaining > 0 {
				capBackoff := remaining / 2
				if capBackoff < 5*time.Millisecond {
					capBackoff = 5 * time.Millisecond
				}
				if backoff > capBackoff {
					backoff = capBackoff
				}
			}
			timer := time.NewTimer(backoff)
			select {
			case <-attemptCtx.Done():
				timer.Stop()
				return nil, fmt.Errorf("policy outbox: claim pending: %w", attemptCtx.Err())
			case <-timer.C:
			}
			if detached && parent != nil && errors.Is(parent.Err(), context.Canceled) {
				return nil, fmt.Errorf("policy outbox: claim pending: %w", context.Canceled)
			}
		}
		return nil, lastErr
	}
	claimRows := func(query string, args []any) ([]Row, error) {
		return claimRowsWithTimeout(ctx, query, args, pendingBudget, false)
	}
	claimCompatRows := func(query string, args []any) ([]Row, error) {
		return claimRowsWithTimeout(ctx, query, args, compatBudget, false)
	}

	setClaimLimit := func(args []any, n int) []any {
		cloned := append([]any(nil), args...)
		cloned[0] = n
		return cloned
	}

	freshRows, err := claimRows(pendingQuery, setClaimLimit(pendingArgs, limit))
	if err != nil {
		return nil, err
	}
	if len(freshRows) > 0 {
		return freshRows, nil
	}
	if shardCompat {
		compatFreshRows, compatErr := claimCompatRows(pendingCompatQuery, setClaimLimit(pendingCompatArgs, limit))
		if compatErr != nil {
			return nil, compatErr
		}
		if len(compatFreshRows) > 0 {
			return compatFreshRows, nil
		}
		if pendingNullCompatQuery != "" {
			compatFreshRows, compatErr = claimCompatRows(pendingNullCompatQuery, setClaimLimit(pendingCompatArgs, limit))
			if compatErr != nil {
				return nil, compatErr
			}
			if len(compatFreshRows) > 0 {
				return compatFreshRows, nil
			}
		}
	}

	reclaimRows, reclaimErr := claimRowsWithTimeout(ctx, reclaimQuery, setClaimLimit(reclaimArgs, limit), reclaimBudget, true)
	if reclaimErr != nil && !isClaimTimeoutErr(reclaimErr) {
		return nil, reclaimErr
	}
	if reclaimErr != nil {
		atomic.AddUint64(&s.reclaimTimeouts, 1)
		reclaimRows = nil
	}
	if len(reclaimRows) > 0 || !shardCompat {
		return reclaimRows, nil
	}

	compatReclaimRows, compatReclaimErr := claimRowsWithTimeout(ctx, reclaimCompatQuery, setClaimLimit(reclaimCompatArgs, limit), reclaimBudget, true)
	if compatReclaimErr != nil && !isClaimTimeoutErr(compatReclaimErr) {
		return nil, compatReclaimErr
	}
	if compatReclaimErr != nil {
		atomic.AddUint64(&s.reclaimTimeouts, 1)
		compatReclaimRows = nil
	}
	if len(compatReclaimRows) > 0 || reclaimNullCompatQuery == "" {
		return compatReclaimRows, nil
	}
	compatReclaimRows, compatReclaimErr = claimRowsWithTimeout(ctx, reclaimNullCompatQuery, setClaimLimit(reclaimCompatArgs, limit), reclaimBudget, true)
	if compatReclaimErr != nil && !isClaimTimeoutErr(compatReclaimErr) {
		return nil, compatReclaimErr
	}
	if compatReclaimErr != nil {
		atomic.AddUint64(&s.reclaimTimeouts, 1)
		return nil, nil
	}
	return compatReclaimRows, nil
}

func (s *Store) claimPendingOnce(ctx context.Context, query string, args ...any) ([]Row, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("policy outbox: claim pending: %w", err)
	}
	defer rows.Close()

	capHint := 100
	if len(args) > 0 {
		if n, ok := args[0].(int); ok && n > 0 {
			capHint = n
		}
	}
	out := make([]Row, 0, capHint)
	for rows.Next() {
		var r Row
		var scanErr error
		if s.hasSourceColumns {
			if s.hasSentinelColumn {
				scanErr = rows.Scan(
					&r.ID, &r.DispatchShard, &r.BlockHeight, &r.BlockTS, &r.CreatedAtMs, &r.TxIndex, &r.PolicyID, &r.RequestID, &r.CommandID, &r.WorkflowID, &r.TraceID, &r.AIEventTsMs, &r.SourceEventID, &r.SourceEventTsMs, &r.SentinelEventID, &r.RuleHash, &r.Payload, &r.Status, &r.Retries, &r.LeaseEpoch,
				)
			} else {
				scanErr = rows.Scan(
					&r.ID, &r.DispatchShard, &r.BlockHeight, &r.BlockTS, &r.CreatedAtMs, &r.TxIndex, &r.PolicyID, &r.RequestID, &r.CommandID, &r.WorkflowID, &r.TraceID, &r.AIEventTsMs, &r.SourceEventID, &r.SourceEventTsMs, &r.RuleHash, &r.Payload, &r.Status, &r.Retries, &r.LeaseEpoch,
				)
			}
		} else {
			scanErr = rows.Scan(
				&r.ID, &r.DispatchShard, &r.BlockHeight, &r.BlockTS, &r.CreatedAtMs, &r.TxIndex, &r.PolicyID, &r.RequestID, &r.CommandID, &r.WorkflowID, &r.TraceID, &r.AIEventTsMs, &r.RuleHash, &r.Payload, &r.Status, &r.Retries, &r.LeaseEpoch,
			)
		}
		if scanErr != nil {
			return nil, fmt.Errorf("policy outbox: claim scan: %w", scanErr)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("policy outbox: claim rows: %w", err)
	}
	return out, nil
}

func parseDispatchShardBucket(label string) int {
	label = strings.TrimSpace(label)
	if strings.HasPrefix(label, "shard:") {
		if bucket, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(label, "shard:"))); err == nil && bucket >= 0 {
			return bucket
		}
	}
	return 0
}

func strictDispatchShardPredicate(column string, shardParam string) string {
	return column + ` = ` + shardParam
}

func compatibleDispatchShardPredicate(column string, bucketParam string, countParam string) string {
	return `(CASE
		WHEN ` + column + ` IS NULL THEN 0
		WHEN ` + column + ` ~ '^shard:[0-9]{3}$' THEN mod(CAST(substr(` + column + `, 7, 3) AS INT8), ` + countParam + `::INT8)
		ELSE -1
	END = ` + bucketParam + `)`
}

func compatibleDispatchShardPredicateLiteral(column string, bucket int, shardCount int) string {
	labels := compatibleDispatchShardLabels(bucket, shardCount)
	if len(labels) == 0 {
		return "FALSE"
	}
	var b strings.Builder
	b.WriteString(column)
	b.WriteString(" IN (")
	for i, label := range labels {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("'")
		b.WriteString(label)
		b.WriteString("'")
	}
	b.WriteString(")")
	return b.String()
}

func compatibleDispatchShardLabels(bucket int, shardCount int) []string {
	if shardCount <= 1 {
		shardCount = 1
	}
	if bucket < 0 || bucket >= shardCount {
		return nil
	}
	labels := make([]string, 0, (maxLegacyDispatchShardLabel/shardCount)+1)
	for i := bucket; i <= maxLegacyDispatchShardLabel; i += shardCount {
		labels = append(labels, dispatchShardLabel(i))
	}
	return labels
}

func compatibleNullDispatchShardPredicate(column string, bucket int) string {
	if bucket != 0 {
		return "FALSE"
	}
	return column + " IS NULL"
}

func compatibleDispatchShardMatches(label string, bucket int, shardCount int) bool {
	if shardCount <= 1 {
		shardCount = 1
	}
	label = strings.TrimSpace(label)
	switch {
	case label == "":
		return bucket == 0
	case strings.HasPrefix(label, "shard:"):
		parsed := parseDispatchShardBucket(label)
		if label != dispatchShardLabel(parsed) {
			return false
		}
		return parsed%shardCount == bucket
	default:
		return false
	}
}

func remainingContextBudget(ctx context.Context) time.Duration {
	if ctx == nil {
		return 0
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	return remaining
}

func claimMaxAttemptsForTimeout(timeout time.Duration) int {
	switch {
	case timeout <= 0:
		return maxClaimAttemptsHard
	case timeout <= 400*time.Millisecond:
		return 1
	case timeout <= 2500*time.Millisecond:
		return 2
	default:
		return maxClaimAttemptsHard
	}
}

func isBudgetNearlyExhausted(ctx context.Context, minBudget time.Duration) bool {
	if ctx == nil {
		return false
	}
	if err := ctx.Err(); err != nil {
		return true
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return false
	}
	return time.Until(deadline) < minBudget
}

func isRetryableClaimErr(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && string(pgErr.Code) == "40001" {
		return true
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && string(pqErr.Code) == "40001" {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 40001") ||
		strings.Contains(msg, "restart transaction") ||
		strings.Contains(msg, "RETRY_SERIALIZABLE")
}

func isClaimTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "timeout")
}

func isClaimConnectErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "failed to connect") ||
		strings.Contains(msg, "dial error") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no route to host")
}

func (s *Store) MarkPublished(ctx context.Context, id, holderID string, epoch int64, leaseKey string, topic string, partition int32, offset int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy outbox: store not initialized")
	}
	leaseKey = strings.TrimSpace(leaseKey)
	if leaseKey == "" {
		return fmt.Errorf("policy outbox: mark published lease key required")
	}
	res, err := s.markPublishedOnce(ctx, id, holderID, epoch, topic, partition, offset)
	if err != nil && shouldRecoverMarkPublished(err, ctx) {
		recoveryUntil := time.Now().Add(markPublishedRecoveryBudget)
		backoff := 150 * time.Millisecond
		for attempt := 1; attempt <= maxMarkPublishedRecoverTries; attempt++ {
			if shouldAbortMarkPublishedRecovery(ctx) {
				break
			}
			if !time.Now().Before(recoveryUntil) {
				break
			}
			attemptBudget := minDuration(markPublishedRecoveryPerTry, time.Until(recoveryUntil))
			if attemptBudget <= 0 {
				break
			}
			recoveryRoot := ctx
			if recoveryRoot == nil {
				recoveryRoot = context.TODO()
			}
			// Detach only when the caller cannot satisfy the attempt budget.
			if deadline, ok := recoveryRoot.Deadline(); !ok || time.Until(deadline) < attemptBudget {
				// Preserve context values while detaching from near-exhausted deadlines;
				// explicit cancellation still honored via shouldAbortMarkPublishedRecovery.
				recoveryRoot = context.WithoutCancel(recoveryRoot)
			}
			recoveryCtx, cancel := context.WithTimeout(recoveryRoot, attemptBudget)
			res, err = s.markPublishedOnce(recoveryCtx, id, holderID, epoch, topic, partition, offset)
			cancel()
			if err == nil || !isRetryableMarkPublishedErr(err) {
				break
			}
			if attempt == maxMarkPublishedRecoverTries {
				break
			}
			if !waitForMarkPublishedRetry(ctx, backoff) {
				break
			}
			backoff = minDuration(backoff*2, time.Second)
		}
	}
	if err != nil && shouldRecoverMarkPublished(err, ctx) {
		finalizeRoot := ctx
		if finalizeRoot == nil {
			finalizeRoot = context.TODO()
		}
		finalizeBudget := minDuration(markPublishedRecoveryPerTry, markPublishedRecoveryBudget)
		if deadline, ok := finalizeRoot.Deadline(); !ok || time.Until(deadline) < finalizeBudget {
			finalizeRoot = context.WithoutCancel(finalizeRoot)
		}
		finalizeCtx, cancel := context.WithTimeout(finalizeRoot, finalizeBudget)
		res, err = s.markPublishedFinalizeOnce(finalizeCtx, id, holderID, epoch, leaseKey, topic, partition, offset)
		cancel()
	}
	if err != nil {
		return fmt.Errorf("policy outbox: mark published: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("policy outbox: mark published fenced/no-op id=%s holder=%s epoch=%d", id, holderID, epoch)
	}
	meta, err := s.loadLifecycleMeta(ctx, id)
	if err != nil {
		// Best-effort only: lifecycle audit must not block durable publish completion.
		return nil
	}
	if _, err := lifecycleaudit.InsertOutboxEvent(ctx, s.db, lifecycleaudit.OutboxEvent{
		ActionType:   lifecycleaudit.ActionPolicyPublished,
		OutboxID:     id,
		PolicyID:     meta.PolicyID,
		WorkflowID:   meta.WorkflowID,
		RequestID:    meta.RequestID,
		ReasonCode:   "auto.policy_published",
		ReasonText:   "durable outbox row published to Kafka",
		BeforeStatus: "publishing",
		AfterStatus:  meta.Status,
	}); err != nil {
		// Best-effort only: lifecycle audit must not block durable publish completion.
		return nil
	}
	return nil
}

func (s *Store) markPublishedOnce(ctx context.Context, id, holderID string, epoch int64, topic string, partition int32, offset int64) (sql.Result, error) {
	return s.db.ExecContext(ctx, `
		UPDATE control_policy_outbox
		SET status=CASE WHEN status='acked' THEN 'acked' ELSE 'published' END,
			published_at=COALESCE(published_at, now()),
			kafka_topic=COALESCE(kafka_topic, $2),
			kafka_partition=CASE WHEN kafka_partition IS NULL THEN $3 ELSE kafka_partition END,
			kafka_offset=CASE WHEN kafka_offset IS NULL THEN $4 ELSE kafka_offset END,
			last_error=NULL,
			updated_at=now()
		WHERE id=$1::UUID
		  AND lease_holder=$5
		  AND lease_epoch=$6
		  AND status IN ('publishing', 'published', 'acked')
	`, id, topic, partition, offset, holderID, epoch)
}

func (s *Store) markPublishedFinalizeOnce(ctx context.Context, id, holderID string, epoch int64, leaseKey string, topic string, partition int32, offset int64) (sql.Result, error) {
	return s.db.ExecContext(ctx, `
		UPDATE control_policy_outbox
		SET status=CASE WHEN status='acked' THEN 'acked' ELSE 'published' END,
			published_at=COALESCE(published_at, now()),
			kafka_topic=COALESCE(kafka_topic, $2),
			kafka_partition=CASE WHEN kafka_partition IS NULL THEN $3 ELSE kafka_partition END,
			kafka_offset=CASE WHEN kafka_offset IS NULL THEN $4 ELSE kafka_offset END,
			last_error=NULL,
			updated_at=now()
		WHERE id=$1::UUID
		  AND status IN ('publishing', 'published', 'acked')
		  AND (
			(lease_holder=$5 AND lease_epoch=$6)
			OR (
				status='publishing'
				AND kafka_partition IS NULL
				AND kafka_offset IS NULL
				AND (
					lease_holder IS NULL
					OR NOT EXISTS (
						SELECT 1
						FROM control_dispatcher_leases l
						WHERE l.holder_id = control_policy_outbox.lease_holder
						  AND l.epoch = control_policy_outbox.lease_epoch
						  AND l.lease_key = $7
						  AND l.lease_until >= now()
					)
				)
			)
		  )
	`, id, topic, partition, offset, holderID, epoch, leaseKey)
}

func shouldRecoverMarkPublished(err error, ctx context.Context) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return isRetryableMarkPublishedErr(err)
}

func isRetryableMarkPublishedErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr != nil && pgErr.Code == "40001" {
		return true
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && string(pqErr.Code) == "40001" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sqlstate 40001") ||
		strings.Contains(msg, "restart transaction") ||
		strings.Contains(msg, "retry_serializable") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "context canceled")
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= b {
		return a
	}
	return b
}

func shouldAbortMarkPublishedRecovery(ctx context.Context) bool {
	return ctx != nil && errors.Is(ctx.Err(), context.Canceled)
}

func waitForMarkPublishedRetry(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if shouldAbortMarkPublishedRecovery(ctx) {
		return false
	}
	if ctx == nil {
		time.Sleep(d)
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		// Keep retrying on deadline-expired per-op contexts, but stop promptly on hard cancellation.
		return !errors.Is(ctx.Err(), context.Canceled)
	}
}

func (s *Store) MarkRetry(ctx context.Context, id, holderID string, epoch int64, retries int, nextRetryAt time.Time, errMsg string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy outbox: store not initialized")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE control_policy_outbox
		SET status='retry',
			retries=$2,
			next_retry_at=$3,
			last_error=$4,
			updated_at=now()
		WHERE id=$1::UUID
		  AND lease_holder=$5
		  AND lease_epoch=$6
		  AND status='publishing'
	`, id, retries, nextRetryAt.UTC(), errMsg, holderID, epoch)
	if err != nil {
		return fmt.Errorf("policy outbox: mark retry: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("policy outbox: mark retry fenced/no-op id=%s holder=%s epoch=%d", id, holderID, epoch)
	}
	return nil
}

func (s *Store) MarkTerminal(ctx context.Context, id, holderID string, epoch int64, retries int, errMsg string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy outbox: store not initialized")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE control_policy_outbox
		SET status='terminal_failed',
			retries=$2,
			last_error=$3,
			updated_at=now()
		WHERE id=$1::UUID
		  AND lease_holder=$4
		  AND lease_epoch=$5
		  AND status='publishing'
	`, id, retries, errMsg, holderID, epoch)
	if err != nil {
		return fmt.Errorf("policy outbox: mark terminal: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("policy outbox: mark terminal fenced/no-op id=%s holder=%s epoch=%d", id, holderID, epoch)
	}
	meta, err := s.loadLifecycleMeta(ctx, id)
	if err != nil {
		// Best-effort only: lifecycle audit must not block durable terminal marking.
		return nil
	}
	if _, err := lifecycleaudit.InsertOutboxEvent(ctx, s.db, lifecycleaudit.OutboxEvent{
		ActionType:   lifecycleaudit.ActionPolicyFailed,
		OutboxID:     id,
		PolicyID:     meta.PolicyID,
		WorkflowID:   meta.WorkflowID,
		RequestID:    meta.RequestID,
		ReasonCode:   "auto.policy_terminal_failed",
		ReasonText:   terminalReasonText(errMsg),
		BeforeStatus: "publishing",
		AfterStatus:  "terminal_failed",
	}); err != nil {
		// Best-effort only: lifecycle audit must not block durable terminal marking.
		return nil
	}
	return nil
}

func (s *Store) CorrelateAck(ctx context.Context, policyID, result, reason, controller string, ackedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy outbox: store not initialized")
	}
	if policyID == "" {
		return nil
	}
	if ackedAt.IsZero() {
		ackedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		WITH latest AS (
			SELECT id
			FROM control_policy_outbox
			WHERE policy_id=$1
			  AND status IN ('published', 'retry', 'pending', 'publishing', 'terminal_failed')
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$2,
			ack_reason=$3,
			ack_controller=$4,
			published_at=COALESCE(published_at, COALESCE($5, now())),
			acked_at=$5,
			updated_at=now()
		WHERE id IN (SELECT id FROM latest)
	`, policyID, result, reason, controller, ackedAt.UTC())
	if err != nil {
		return fmt.Errorf("policy outbox: correlate ack: %w", err)
	}
	return nil
}

type lifecycleMeta struct {
	PolicyID   string
	WorkflowID string
	RequestID  string
	Status     string
}

func (s *Store) loadLifecycleMeta(ctx context.Context, id string) (lifecycleMeta, error) {
	var meta lifecycleMeta
	err := s.db.QueryRowContext(ctx, `
		SELECT policy_id, COALESCE(workflow_id, ''), COALESCE(request_id, ''), status
		FROM control_policy_outbox
		WHERE id = $1::UUID
	`, id).Scan(&meta.PolicyID, &meta.WorkflowID, &meta.RequestID, &meta.Status)
	if err != nil {
		return lifecycleMeta{}, err
	}
	return meta, nil
}

func terminalReasonText(errMsg string) string {
	base := "durable outbox row marked terminal failed"
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		return base
	}
	return base + ": " + errMsg
}

// BacklogStats returns queue depth and oldest publish-eligible row age.
func (s *Store) BacklogStats(ctx context.Context) (BacklogStats, error) {
	if s == nil || s.db == nil {
		return BacklogStats{}, fmt.Errorf("policy outbox: store not initialized")
	}

	var stats BacklogStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END), 0) AS pending_count,
			COALESCE(SUM(CASE WHEN status='retry' THEN 1 ELSE 0 END), 0) AS retry_count,
			COALESCE(SUM(CASE WHEN status='publishing' THEN 1 ELSE 0 END), 0) AS publishing_count,
			COALESCE(
				MAX(
					CASE
						WHEN status='pending'
							THEN CAST(EXTRACT(EPOCH FROM (now() - created_at)) * 1000 AS INT8)
						WHEN status='retry' AND next_retry_at IS NOT NULL AND next_retry_at <= now()
							THEN CAST(EXTRACT(EPOCH FROM (now() - next_retry_at)) * 1000 AS INT8)
						WHEN status='publishing'
							THEN CAST(EXTRACT(EPOCH FROM (now() - updated_at)) * 1000 AS INT8)
						ELSE NULL
					END
				), 0
			) AS oldest_pending_age_ms
		FROM control_policy_outbox
		WHERE status IN ('pending', 'retry', 'publishing')
	`).Scan(
		&stats.Pending,
		&stats.Retry,
		&stats.Publishing,
		&stats.OldestPendingAge,
	)
	if err != nil {
		return BacklogStats{}, fmt.Errorf("policy outbox: backlog stats: %w", err)
	}
	summary, err := s.backlogSummaryStats(ctx)
	if err != nil {
		return BacklogStats{}, err
	}
	stats.PublishedRows = summary.PublishedRows
	stats.AckedRows = summary.AckedRows
	stats.TerminalRows = summary.TerminalRows
	stats.BacklogCacheAgeMs = summary.BacklogCacheAgeMs
	stats.TotalRows = stats.Pending + stats.Retry + stats.Publishing + stats.PublishedRows + stats.AckedRows + stats.TerminalRows

	return stats, nil
}

func (s *Store) backlogSummaryStats(ctx context.Context) (BacklogStats, error) {
	now := time.Now()
	s.backlogSummaryMu.Lock()
	if s.backlogSummaryTTL > 0 && now.Before(s.backlogSummaryAt.Add(s.backlogSummaryTTL)) {
		snapshot := s.backlogSummary
		if !s.backlogSummaryAt.IsZero() {
			snapshot.BacklogCacheAgeMs = now.Sub(s.backlogSummaryAt).Milliseconds()
		}
		s.backlogSummaryMu.Unlock()
		return snapshot, nil
	}
	s.backlogSummaryMu.Unlock()

	var summary BacklogStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status='published' THEN 1 ELSE 0 END), 0) AS published_rows,
			COALESCE(SUM(CASE WHEN status='acked' THEN 1 ELSE 0 END), 0) AS acked_rows,
			COALESCE(SUM(CASE WHEN status='terminal_failed' THEN 1 ELSE 0 END), 0) AS terminal_rows
		FROM control_policy_outbox
		WHERE status IN ('published', 'acked', 'terminal_failed')
	`).Scan(
		&summary.PublishedRows,
		&summary.AckedRows,
		&summary.TerminalRows,
	)
	if err != nil {
		return BacklogStats{}, fmt.Errorf("policy outbox: backlog summary stats: %w", err)
	}

	s.backlogSummaryMu.Lock()
	s.backlogSummary = summary
	s.backlogSummaryAt = time.Now()
	snapshot := s.backlogSummary
	snapshot.BacklogCacheAgeMs = 0
	s.backlogSummaryMu.Unlock()
	return snapshot, nil
}
