package policyack

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/control/lifecycleaudit"
	"backend/pkg/control/policystate"
	"backend/pkg/utils"
	pb "backend/proto"
)

type Store struct {
	db          *sql.DB
	stats       ackStoreStats
	schemaMu    sync.Mutex
	schemaReady bool
	eventsCols  ackOptionalColumns
	acksCols    ackOptionalColumns
}

type ackOptionalColumns struct {
	ackEventID      bool
	requestID       bool
	commandID       bool
	workflowID      bool
	traceID         bool
	sourceEventID   bool
	sentinelEventID bool
}

type ackStoreStats struct {
	skewCorrections          atomic.Uint64
	correlationExact         atomic.Uint64
	correlationFallbackHash  atomic.Uint64
	correlationFallbackTrace atomic.Uint64
	correlationNoMatch       atomic.Uint64
	correlationErrors        atomic.Uint64
	aiEventUnitFixes         atomic.Uint64
	aiEventInvalid           atomic.Uint64
	sourceEventUnitFixes     atomic.Uint64
	sourceEventInvalid       atomic.Uint64
	aiToAckLatency           *utils.LatencyHistogram
	sourceToAckLatency       *utils.LatencyHistogram
	publishToAckLatency      *utils.LatencyHistogram
	publishToAppliedLatency  *utils.LatencyHistogram
	appliedToAckLatency      *utils.LatencyHistogram
}

type StoreStats struct {
	SkewCorrectionsTotal       uint64
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
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("policy ack store: db required")
	}
	return &Store{
		db: db,
		eventsCols: ackOptionalColumns{
			ackEventID:      true,
			requestID:       true,
			commandID:       true,
			workflowID:      true,
			traceID:         true,
			sourceEventID:   true,
			sentinelEventID: true,
		},
		acksCols: ackOptionalColumns{
			ackEventID:      true,
			requestID:       true,
			commandID:       true,
			workflowID:      true,
			traceID:         true,
			sourceEventID:   true,
			sentinelEventID: true,
		},
		schemaReady: false,
		stats: ackStoreStats{
			aiToAckLatency: utils.NewLatencyHistogram([]float64{
				10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
			}),
			sourceToAckLatency: utils.NewLatencyHistogram([]float64{
				10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
			}),
			publishToAckLatency: utils.NewLatencyHistogram([]float64{
				10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
			}),
			publishToAppliedLatency: utils.NewLatencyHistogram([]float64{
				10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
			}),
			appliedToAckLatency: utils.NewLatencyHistogram([]float64{
				10, 25, 50, 100, 250, 500, 1000, 2000, 5000, 10000, 30000,
			}),
		},
	}, nil
}

func (s *Store) Upsert(ctx context.Context, evt *pb.PolicyAckEvent, observedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy ack store: not initialized")
	}
	if evt == nil {
		return fmt.Errorf("policy ack store: event nil")
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("policy ack store: ensure schema failed: %w", err)
	}

	var appliedAt sql.NullTime
	if ts, ok := eventTime(evt.AppliedAtMs, evt.AppliedAt); ok {
		appliedAt = sql.NullTime{Time: ts, Valid: true}
	}
	var ackedAt sql.NullTime
	if ts, ok := eventTime(evt.AckedAtMs, evt.AckedAt); ok {
		ackedAt = sql.NullTime{Time: ts, Valid: true}
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	if err := s.insertEvent(ctx, evt, appliedAt, ackedAt, observedAt.UTC()); err != nil {
		return fmt.Errorf("policy ack store: insert event failed: %w", err)
	}

	ackQuery, ackArgs := s.buildPolicyAcksUpsert(evt, appliedAt, ackedAt, observedAt.UTC())
	_, err := s.db.ExecContext(ctx, ackQuery, ackArgs...)
	if err != nil {
		return fmt.Errorf("policy ack store: upsert failed: %w", err)
	}

	aiEventTsMs, sourceEventTsMs, publishedAt, ackedAtRow, mode, corrErr := s.correlateOutbox(ctx, evt, ackedAt, observedAt.UTC())
	if corrErr != nil {
		if isOutboxMissingErr(corrErr) {
			return nil
		}
		s.stats.correlationErrors.Add(1)
		return fmt.Errorf("policy ack store: correlate outbox failed: %w", corrErr)
	}
	switch mode {
	case correlationExact:
		s.stats.correlationExact.Add(1)
	case correlationFallbackHash:
		s.stats.correlationFallbackHash.Add(1)
	case correlationFallbackTrace:
		s.stats.correlationFallbackTrace.Add(1)
	default:
		s.stats.correlationNoMatch.Add(1)
	}

	// AI/source causal latencies are only considered strong when correlation included trace linkage.
	strongCausalMatch := mode == correlationExact || mode == correlationFallbackTrace

	if strongCausalMatch && ackedAtRow.Valid && aiEventTsMs.Valid && aiEventTsMs.Int64 > 0 {
		normalizedAIEvent, corrected, valid := utils.NormalizeUnixMillis(aiEventTsMs.Int64)
		if !valid {
			s.stats.aiEventInvalid.Add(1)
		} else {
			if corrected {
				s.stats.aiEventUnitFixes.Add(1)
			}
			latencyMs := ackedAtRow.Time.UnixMilli() - normalizedAIEvent
			if latencyMs < 0 {
				latencyMs = 0
				s.stats.skewCorrections.Add(1)
			}
			s.stats.aiToAckLatency.Observe(float64(latencyMs))
		}
	}
	if strongCausalMatch && ackedAtRow.Valid && sourceEventTsMs.Valid && sourceEventTsMs.Int64 > 0 {
		normalizedSourceEvent, corrected, valid := utils.NormalizeUnixMillis(sourceEventTsMs.Int64)
		if !valid {
			s.stats.sourceEventInvalid.Add(1)
		} else {
			if corrected {
				s.stats.sourceEventUnitFixes.Add(1)
			}
			latencyMs := ackedAtRow.Time.UnixMilli() - normalizedSourceEvent
			if latencyMs < 0 {
				latencyMs = 0
				s.stats.skewCorrections.Add(1)
			}
			s.stats.sourceToAckLatency.Observe(float64(latencyMs))
		}
	}
	if ackedAtRow.Valid && publishedAt.Valid {
		latencyMs := ackedAtRow.Time.UnixMilli() - publishedAt.Time.UnixMilli()
		if latencyMs < 0 {
			latencyMs = 0
			s.stats.skewCorrections.Add(1)
		}
		s.stats.publishToAckLatency.Observe(float64(latencyMs))
	}
	if appliedAt.Valid && publishedAt.Valid {
		latencyMs := appliedAt.Time.UnixMilli() - publishedAt.Time.UnixMilli()
		if latencyMs < 0 {
			latencyMs = 0
			s.stats.skewCorrections.Add(1)
		}
		s.stats.publishToAppliedLatency.Observe(float64(latencyMs))
	}
	if appliedAt.Valid && ackedAtRow.Valid {
		latencyMs := ackedAtRow.Time.UnixMilli() - appliedAt.Time.UnixMilli()
		if latencyMs < 0 {
			latencyMs = 0
			s.stats.skewCorrections.Add(1)
		}
		s.stats.appliedToAckLatency.Observe(float64(latencyMs))
	}

	if err := policystate.Refresh(ctx, s.db, s.db, evt.PolicyId); err != nil {
		return fmt.Errorf("policy ack store: refresh policy state failed: %w", err)
	}
	if meta, metaErr := s.loadAckedLifecycleMeta(ctx, evt); metaErr == nil && strings.TrimSpace(meta.OutboxID) != "" {
		workflowID := meta.WorkflowID
		if workflowID == "" {
			workflowID = strings.TrimSpace(evt.WorkflowId)
		}
		requestID := meta.RequestID
		if requestID == "" {
			requestID = strings.TrimSpace(evt.RequestId)
		}
		if _, auditErr := lifecycleaudit.InsertOutboxEvent(ctx, s.db, lifecycleaudit.OutboxEvent{
			ActionType:  lifecycleaudit.ActionPolicyAcked,
			OutboxID:    meta.OutboxID,
			PolicyID:    evt.PolicyId,
			WorkflowID:  workflowID,
			RequestID:   requestID,
			ReasonCode:  "auto.policy_acked",
			ReasonText:  ackReasonText(evt),
			AfterStatus: "acked",
			TenantScope: strings.TrimSpace(evt.Tenant),
		}); auditErr != nil {
			// Best-effort: lifecycle audit should not block ACK persistence.
		}
	}

	return nil
}

func eventTime(unixMs, unixSec int64) (time.Time, bool) {
	if unixMs > 0 {
		return time.UnixMilli(unixMs).UTC(), true
	}
	if unixSec > 0 {
		return time.Unix(unixSec, 0).UTC(), true
	}
	return time.Time{}, false
}

func (s *Store) insertEvent(ctx context.Context, evt *pb.PolicyAckEvent, appliedAt, ackedAt sql.NullTime, observedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy ack store: not initialized")
	}
	query, args := s.buildPolicyAckEventsInsert(evt, appliedAt, ackedAt, observedAt.UTC())
	_, err := s.db.ExecContext(ctx, query, args...)
	if isAckEventDuplicateErr(err) {
		return nil
	}
	return err
}

func isAckEventDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "idx_policy_ack_events_ack_event_id_unique") ||
		(strings.Contains(msg, "duplicate key value") && strings.Contains(msg, "ack_event_id"))
}

func (s *Store) Stats() StoreStats {
	if s == nil {
		return StoreStats{}
	}
	aiBuckets, aiCount, aiSum := s.stats.aiToAckLatency.Snapshot()
	sourceBuckets, sourceCount, sourceSum := s.stats.sourceToAckLatency.Snapshot()
	pubBuckets, pubCount, pubSum := s.stats.publishToAckLatency.Snapshot()
	pubApplyBuckets, pubApplyCount, pubApplySum := s.stats.publishToAppliedLatency.Snapshot()
	applyAckBuckets, applyAckCount, applyAckSum := s.stats.appliedToAckLatency.Snapshot()
	return StoreStats{
		SkewCorrectionsTotal:       s.stats.skewCorrections.Load(),
		CorrelationExact:           s.stats.correlationExact.Load(),
		CorrelationFallbackHash:    s.stats.correlationFallbackHash.Load(),
		CorrelationFallbackTrace:   s.stats.correlationFallbackTrace.Load(),
		CorrelationNoMatch:         s.stats.correlationNoMatch.Load(),
		CorrelationErrors:          s.stats.correlationErrors.Load(),
		AIEventUnitCorrections:     s.stats.aiEventUnitFixes.Load(),
		AIEventInvalidTotal:        s.stats.aiEventInvalid.Load(),
		SourceEventUnitCorrections: s.stats.sourceEventUnitFixes.Load(),
		SourceEventInvalidTotal:    s.stats.sourceEventInvalid.Load(),
		AIToAckBuckets:             aiBuckets,
		AIToAckCount:               aiCount,
		AIToAckSumMs:               aiSum,
		AIToAckP95Ms:               s.stats.aiToAckLatency.Quantile(0.95),
		SourceToAckBuckets:         sourceBuckets,
		SourceToAckCount:           sourceCount,
		SourceToAckSumMs:           sourceSum,
		SourceToAckP95Ms:           s.stats.sourceToAckLatency.Quantile(0.95),
		PublishToAckBuckets:        pubBuckets,
		PublishToAckCount:          pubCount,
		PublishToAckSumMs:          pubSum,
		PublishToAckP95Ms:          s.stats.publishToAckLatency.Quantile(0.95),
		PublishToAppliedBuckets:    pubApplyBuckets,
		PublishToAppliedCount:      pubApplyCount,
		PublishToAppliedSumMs:      pubApplySum,
		PublishToAppliedP95Ms:      s.stats.publishToAppliedLatency.Quantile(0.95),
		AppliedToAckBuckets:        applyAckBuckets,
		AppliedToAckCount:          applyAckCount,
		AppliedToAckSumMs:          applyAckSum,
		AppliedToAckP95Ms:          s.stats.appliedToAckLatency.Quantile(0.95),
	}
}

type correlationMode string

const (
	correlationNone          correlationMode = "none"
	correlationExact         correlationMode = "exact"
	correlationFallbackHash  correlationMode = "fallback_hash"
	correlationFallbackTrace correlationMode = "fallback_trace"
)

const (
	hashCorrelationMaxAge  = 5 * time.Minute
	traceCorrelationMaxAge = 30 * time.Minute
	exactCorrelationMaxAge = 30 * time.Minute
	// Clamp untrusted event ackedAt to a narrow window around trusted server time.
	correlationAckMaxPastSkew   = 2 * time.Minute
	correlationAckMaxFutureSkew = 30 * time.Second
)

func (s *Store) correlateOutbox(ctx context.Context, evt *pb.PolicyAckEvent, ackedAt sql.NullTime, trustedNow time.Time) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, correlationMode, error) {
	traceID := strings.TrimSpace(evt.TraceId)
	if traceID == "" {
		traceID = strings.TrimSpace(evt.QcReference)
	}
	hasRuleHash := len(evt.RuleHash) > 0
	hashCutoff := correlationCutoff(ackedAt, trustedNow, hashCorrelationMaxAge)
	traceCutoff := correlationCutoff(ackedAt, trustedNow, traceCorrelationMaxAge)
	exactCutoff := correlationCutoff(ackedAt, trustedNow, exactCorrelationMaxAge)

	if traceID != "" && hasRuleHash {
		aiTs, sourceTs, pubAt, ackAt, err := s.correlateExact(ctx, evt.PolicyId, evt.RuleHash, traceID, evt.Result, evt.Reason, evt.ControllerInstance, ackedAt, exactCutoff)
		if err == nil {
			return aiTs, sourceTs, pubAt, ackAt, correlationExact, nil
		}
		if err == sql.ErrNoRows && ackedAt.Valid {
			aiTs, sourceTs, pubAt, ackAt, err = s.correlateExactNoCutoff(ctx, evt.PolicyId, evt.RuleHash, traceID, evt.Result, evt.Reason, evt.ControllerInstance, ackedAt)
			if err == nil {
				return aiTs, sourceTs, pubAt, ackAt, correlationExact, nil
			}
		}
		if err != sql.ErrNoRows {
			return sql.NullInt64{}, sql.NullInt64{}, sql.NullTime{}, sql.NullTime{}, correlationNone, err
		}
	}

	if traceID != "" {
		aiTs, sourceTs, pubAt, ackAt, err := s.correlateByTraceID(ctx, evt.PolicyId, traceID, evt.Result, evt.Reason, evt.ControllerInstance, ackedAt, traceCutoff)
		if err == nil {
			return aiTs, sourceTs, pubAt, ackAt, correlationFallbackTrace, nil
		}
		if err == sql.ErrNoRows && ackedAt.Valid {
			aiTs, sourceTs, pubAt, ackAt, err = s.correlateByTraceIDNoCutoff(ctx, evt.PolicyId, traceID, evt.Result, evt.Reason, evt.ControllerInstance, ackedAt)
			if err == nil {
				return aiTs, sourceTs, pubAt, ackAt, correlationFallbackTrace, nil
			}
		}
		if err != sql.ErrNoRows {
			return sql.NullInt64{}, sql.NullInt64{}, sql.NullTime{}, sql.NullTime{}, correlationNone, err
		}
	}

	if hasRuleHash {
		aiTs, sourceTs, pubAt, ackAt, err := s.correlateByRuleHash(ctx, evt.PolicyId, evt.RuleHash, evt.Result, evt.Reason, evt.ControllerInstance, ackedAt, hashCutoff)
		if err == nil {
			return aiTs, sourceTs, pubAt, ackAt, correlationFallbackHash, nil
		}
		if err != sql.ErrNoRows {
			return sql.NullInt64{}, sql.NullInt64{}, sql.NullTime{}, sql.NullTime{}, correlationNone, err
		}
	}

	return sql.NullInt64{}, sql.NullInt64{}, sql.NullTime{}, sql.NullTime{}, correlationNone, nil
}

func (s *Store) correlateExact(ctx context.Context, policyID string, ruleHash []byte, traceID, result, reason, controller string, ackedAt sql.NullTime, cutoff time.Time) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var sourceEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, source_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND rule_hash = $2
			  AND trace_id = $3
			  AND status IN ('publishing', 'published', 'acked')
			  AND created_at >= $8
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$4,
			ack_reason=$5,
			ack_controller=$6,
			published_at=COALESCE(published_at, COALESCE($7, now())),
			acked_at=COALESCE($7, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT source_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, ruleHash, traceID, result, reason, controller, ackedAt, cutoff).Scan(&aiEventTsMs, &sourceEventTsMs, &publishedAt, &ackedAtRow)
	if err != nil && isSourceEventColumnMissingErr(err) {
		return s.correlateExactLegacy(ctx, policyID, ruleHash, traceID, result, reason, controller, ackedAt, cutoff)
	}
	return aiEventTsMs, sourceEventTsMs, publishedAt, ackedAtRow, err
}

func (s *Store) correlateExactNoCutoff(ctx context.Context, policyID string, ruleHash []byte, traceID, result, reason, controller string, ackedAt sql.NullTime) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var sourceEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, source_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND rule_hash = $2
			  AND trace_id = $3
			  AND status IN ('publishing', 'published', 'acked')
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$4,
			ack_reason=$5,
			ack_controller=$6,
			published_at=COALESCE(published_at, COALESCE($7, now())),
			acked_at=COALESCE($7, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT source_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, ruleHash, traceID, result, reason, controller, ackedAt).Scan(&aiEventTsMs, &sourceEventTsMs, &publishedAt, &ackedAtRow)
	if err != nil && isSourceEventColumnMissingErr(err) {
		return s.correlateExactLegacyNoCutoff(ctx, policyID, ruleHash, traceID, result, reason, controller, ackedAt)
	}
	return aiEventTsMs, sourceEventTsMs, publishedAt, ackedAtRow, err
}

func (s *Store) correlateByRuleHash(ctx context.Context, policyID string, ruleHash []byte, result, reason, controller string, ackedAt sql.NullTime, cutoff time.Time) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var sourceEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, source_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND rule_hash = $2
			  AND status IN ('publishing', 'published', 'acked')
			  AND created_at >= $7
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
			published_at=COALESCE(published_at, COALESCE($6, now())),
			acked_at=COALESCE($6, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT source_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, ruleHash, result, reason, controller, ackedAt, cutoff).Scan(&aiEventTsMs, &sourceEventTsMs, &publishedAt, &ackedAtRow)
	if err != nil && isSourceEventColumnMissingErr(err) {
		return s.correlateByRuleHashLegacy(ctx, policyID, ruleHash, result, reason, controller, ackedAt, cutoff)
	}
	return aiEventTsMs, sourceEventTsMs, publishedAt, ackedAtRow, err
}

func (s *Store) correlateByTraceID(ctx context.Context, policyID, traceID, result, reason, controller string, ackedAt sql.NullTime, cutoff time.Time) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var sourceEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, source_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND trace_id = $2
			  AND status IN ('publishing', 'published', 'acked')
			  AND created_at >= $7
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
			published_at=COALESCE(published_at, COALESCE($6, now())),
			acked_at=COALESCE($6, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT source_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, traceID, result, reason, controller, ackedAt, cutoff).Scan(&aiEventTsMs, &sourceEventTsMs, &publishedAt, &ackedAtRow)
	if err != nil && isSourceEventColumnMissingErr(err) {
		return s.correlateByTraceIDLegacy(ctx, policyID, traceID, result, reason, controller, ackedAt, cutoff)
	}
	return aiEventTsMs, sourceEventTsMs, publishedAt, ackedAtRow, err
}

func (s *Store) correlateByTraceIDNoCutoff(ctx context.Context, policyID, traceID, result, reason, controller string, ackedAt sql.NullTime) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var sourceEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, source_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND trace_id = $2
			  AND status IN ('publishing', 'published', 'acked')
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
			published_at=COALESCE(published_at, COALESCE($6, now())),
			acked_at=COALESCE($6, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT source_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, traceID, result, reason, controller, ackedAt).Scan(&aiEventTsMs, &sourceEventTsMs, &publishedAt, &ackedAtRow)
	if err != nil && isSourceEventColumnMissingErr(err) {
		return s.correlateByTraceIDLegacyNoCutoff(ctx, policyID, traceID, result, reason, controller, ackedAt)
	}
	return aiEventTsMs, sourceEventTsMs, publishedAt, ackedAtRow, err
}

func (s *Store) correlateExactLegacy(ctx context.Context, policyID string, ruleHash []byte, traceID, result, reason, controller string, ackedAt sql.NullTime, cutoff time.Time) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND rule_hash = $2
			  AND trace_id = $3
			  AND status IN ('publishing', 'published', 'acked')
			  AND created_at >= $8
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$4,
			ack_reason=$5,
			ack_controller=$6,
			published_at=COALESCE(published_at, COALESCE($7, now())),
			acked_at=COALESCE($7, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, ruleHash, traceID, result, reason, controller, ackedAt, cutoff).Scan(&aiEventTsMs, &publishedAt, &ackedAtRow)
	return aiEventTsMs, sql.NullInt64{}, publishedAt, ackedAtRow, err
}

func (s *Store) correlateExactLegacyNoCutoff(ctx context.Context, policyID string, ruleHash []byte, traceID, result, reason, controller string, ackedAt sql.NullTime) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND rule_hash = $2
			  AND trace_id = $3
			  AND status IN ('publishing', 'published', 'acked')
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$4,
			ack_reason=$5,
			ack_controller=$6,
			published_at=COALESCE(published_at, COALESCE($7, now())),
			acked_at=COALESCE($7, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, ruleHash, traceID, result, reason, controller, ackedAt).Scan(&aiEventTsMs, &publishedAt, &ackedAtRow)
	return aiEventTsMs, sql.NullInt64{}, publishedAt, ackedAtRow, err
}

func (s *Store) correlateByRuleHashLegacy(ctx context.Context, policyID string, ruleHash []byte, result, reason, controller string, ackedAt sql.NullTime, cutoff time.Time) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND rule_hash = $2
			  AND status IN ('publishing', 'published', 'acked')
			  AND created_at >= $7
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
			published_at=COALESCE(published_at, COALESCE($6, now())),
			acked_at=COALESCE($6, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, ruleHash, result, reason, controller, ackedAt, cutoff).Scan(&aiEventTsMs, &publishedAt, &ackedAtRow)
	return aiEventTsMs, sql.NullInt64{}, publishedAt, ackedAtRow, err
}

func (s *Store) correlateByTraceIDLegacy(ctx context.Context, policyID, traceID, result, reason, controller string, ackedAt sql.NullTime, cutoff time.Time) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND trace_id = $2
			  AND status IN ('publishing', 'published', 'acked')
			  AND created_at >= $7
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
			published_at=COALESCE(published_at, COALESCE($6, now())),
			acked_at=COALESCE($6, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, traceID, result, reason, controller, ackedAt, cutoff).Scan(&aiEventTsMs, &publishedAt, &ackedAtRow)
	return aiEventTsMs, sql.NullInt64{}, publishedAt, ackedAtRow, err
}

func (s *Store) correlateByTraceIDLegacyNoCutoff(ctx context.Context, policyID, traceID, result, reason, controller string, ackedAt sql.NullTime) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, error) {
	var aiEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		WITH chosen AS (
			SELECT id, ai_event_ts_ms, published_at
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND trace_id = $2
			  AND status IN ('publishing', 'published', 'acked')
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
			published_at=COALESCE(published_at, COALESCE($6, now())),
			acked_at=COALESCE($6, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, traceID, result, reason, controller, ackedAt).Scan(&aiEventTsMs, &publishedAt, &ackedAtRow)
	return aiEventTsMs, sql.NullInt64{}, publishedAt, ackedAtRow, err
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy ack store: not initialized")
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.schemaReady {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name IN ('policy_ack_events', 'policy_acks')
		  AND column_name IN ('ack_event_id', 'request_id', 'command_id', 'workflow_id', 'trace_id', 'source_event_id', 'sentinel_event_id')
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var eventsCols ackOptionalColumns
	var acksCols ackOptionalColumns
	for rows.Next() {
		var tableName string
		var columnName string
		if scanErr := rows.Scan(&tableName, &columnName); scanErr != nil {
			return scanErr
		}
		target := &eventsCols
		if tableName == "policy_acks" {
			target = &acksCols
		}
		switch columnName {
		case "ack_event_id":
			target.ackEventID = true
		case "request_id":
			target.requestID = true
		case "command_id":
			target.commandID = true
		case "workflow_id":
			target.workflowID = true
		case "trace_id":
			target.traceID = true
		case "source_event_id":
			target.sourceEventID = true
		case "sentinel_event_id":
			target.sentinelEventID = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.eventsCols = eventsCols
	s.acksCols = acksCols
	s.schemaReady = true
	return nil
}

func (s *Store) buildPolicyAckEventsInsert(evt *pb.PolicyAckEvent, appliedAt, ackedAt sql.NullTime, observedAt time.Time) (string, []any) {
	cols := []string{"policy_id", "controller_instance"}
	args := []any{evt.PolicyId, evt.ControllerInstance}
	if s.eventsCols.ackEventID {
		cols = append(cols, "ack_event_id")
		args = append(args, evt.AckEventId)
	}
	if s.eventsCols.requestID {
		cols = append(cols, "request_id")
		args = append(args, evt.RequestId)
	}
	if s.eventsCols.commandID {
		cols = append(cols, "command_id")
		args = append(args, evt.CommandId)
	}
	if s.eventsCols.workflowID {
		cols = append(cols, "workflow_id")
		args = append(args, evt.WorkflowId)
	}
	cols = append(cols, "scope_identifier", "tenant", "region", "result", "reason", "error_code", "applied_at", "acked_at", "qc_reference")
	args = append(args, evt.ScopeIdentifier, evt.Tenant, evt.Region, evt.Result, evt.Reason, evt.ErrorCode, appliedAt, ackedAt, evt.QcReference)
	if s.eventsCols.traceID {
		cols = append(cols, "trace_id")
		args = append(args, evt.TraceId)
	}
	if s.eventsCols.sourceEventID {
		cols = append(cols, "source_event_id")
		args = append(args, evt.SourceEventId)
	}
	if s.eventsCols.sentinelEventID {
		cols = append(cols, "sentinel_event_id")
		args = append(args, evt.SentinelEventId)
	}
	cols = append(cols, "fast_path", "rule_hash", "producer_id", "observed_at")
	args = append(args, evt.FastPath, evt.RuleHash, evt.ProducerId, observedAt.UTC())
	return insertQuery("policy_ack_events", cols), args
}

func (s *Store) buildPolicyAcksUpsert(evt *pb.PolicyAckEvent, appliedAt, ackedAt sql.NullTime, observedAt time.Time) (string, []any) {
	cols := []string{"policy_id", "controller_instance"}
	args := []any{evt.PolicyId, evt.ControllerInstance}
	if s.acksCols.ackEventID {
		cols = append(cols, "ack_event_id")
		args = append(args, evt.AckEventId)
	}
	if s.acksCols.requestID {
		cols = append(cols, "request_id")
		args = append(args, evt.RequestId)
	}
	if s.acksCols.commandID {
		cols = append(cols, "command_id")
		args = append(args, evt.CommandId)
	}
	if s.acksCols.workflowID {
		cols = append(cols, "workflow_id")
		args = append(args, evt.WorkflowId)
	}
	cols = append(cols, "scope_identifier", "tenant", "region", "result", "reason", "error_code", "applied_at", "acked_at", "qc_reference")
	args = append(args, evt.ScopeIdentifier, evt.Tenant, evt.Region, evt.Result, evt.Reason, evt.ErrorCode, appliedAt, ackedAt, evt.QcReference)
	if s.acksCols.traceID {
		cols = append(cols, "trace_id")
		args = append(args, evt.TraceId)
	}
	if s.acksCols.sourceEventID {
		cols = append(cols, "source_event_id")
		args = append(args, evt.SourceEventId)
	}
	if s.acksCols.sentinelEventID {
		cols = append(cols, "sentinel_event_id")
		args = append(args, evt.SentinelEventId)
	}
	cols = append(cols, "fast_path", "rule_hash", "producer_id", "observed_at")
	args = append(args, evt.FastPath, evt.RuleHash, evt.ProducerId, observedAt.UTC())

	updateClauses := []string{
		"controller_instance = EXCLUDED.controller_instance",
		"scope_identifier = COALESCE(NULLIF(EXCLUDED.scope_identifier, ''), policy_acks.scope_identifier)",
		"tenant = COALESCE(NULLIF(EXCLUDED.tenant, ''), policy_acks.tenant)",
		"region = COALESCE(NULLIF(EXCLUDED.region, ''), policy_acks.region)",
		"result = EXCLUDED.result",
		"reason = EXCLUDED.reason",
		"error_code = EXCLUDED.error_code",
		"applied_at = COALESCE(EXCLUDED.applied_at, policy_acks.applied_at)",
		"acked_at = COALESCE(EXCLUDED.acked_at, policy_acks.acked_at)",
		"qc_reference = COALESCE(NULLIF(EXCLUDED.qc_reference, ''), policy_acks.qc_reference)",
		"fast_path = EXCLUDED.fast_path",
		"rule_hash = CASE WHEN octet_length(EXCLUDED.rule_hash) > 0 THEN EXCLUDED.rule_hash ELSE policy_acks.rule_hash END",
		"producer_id = CASE WHEN octet_length(EXCLUDED.producer_id) > 0 THEN EXCLUDED.producer_id ELSE policy_acks.producer_id END",
		"observed_at = EXCLUDED.observed_at",
	}
	if s.acksCols.ackEventID {
		updateClauses = append(updateClauses, "ack_event_id = EXCLUDED.ack_event_id")
	}
	if s.acksCols.requestID {
		updateClauses = append(updateClauses, "request_id = COALESCE(NULLIF(EXCLUDED.request_id, ''), policy_acks.request_id)")
	}
	if s.acksCols.commandID {
		updateClauses = append(updateClauses, "command_id = COALESCE(NULLIF(EXCLUDED.command_id, ''), policy_acks.command_id)")
	}
	if s.acksCols.workflowID {
		updateClauses = append(updateClauses, "workflow_id = COALESCE(NULLIF(EXCLUDED.workflow_id, ''), policy_acks.workflow_id)")
	}
	if s.acksCols.traceID {
		updateClauses = append(updateClauses, "trace_id = COALESCE(NULLIF(EXCLUDED.trace_id, ''), policy_acks.trace_id)")
	}
	if s.acksCols.sourceEventID {
		updateClauses = append(updateClauses, "source_event_id = COALESCE(NULLIF(EXCLUDED.source_event_id, ''), policy_acks.source_event_id)")
	}
	if s.acksCols.sentinelEventID {
		updateClauses = append(updateClauses, "sentinel_event_id = COALESCE(NULLIF(EXCLUDED.sentinel_event_id, ''), policy_acks.sentinel_event_id)")
	}

	return insertQuery("policy_acks", cols) + `
	ON CONFLICT (policy_id, controller_instance) DO UPDATE SET
		` + strings.Join(updateClauses, ",\n\t\t"), args
}

func insertQuery(table string, cols []string) string {
	placeholders := make([]string, 0, len(cols))
	for i := range cols {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
}

func correlationCutoff(ackedAt sql.NullTime, trustedNow time.Time, maxAge time.Duration) time.Time {
	if maxAge <= 0 {
		return trustedNow.UTC()
	}
	ref := trustedNow.UTC()
	if ackedAt.Valid {
		eventAckedAt := ackedAt.Time.UTC()
		lower := ref.Add(-correlationAckMaxPastSkew)
		upper := ref.Add(correlationAckMaxFutureSkew)
		switch {
		case eventAckedAt.Before(lower):
			ref = lower
		case eventAckedAt.After(upper):
			ref = upper
		default:
			ref = eventAckedAt
		}
	}
	return ref.Add(-maxAge)
}

func isOutboxMissingErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "control_policy_outbox") && strings.Contains(msg, "does not exist")
}

func isSourceEventColumnMissingErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "source_event_ts_ms") && strings.Contains(msg, "does not exist")
}

type ackLifecycleMeta struct {
	OutboxID   string
	WorkflowID string
	RequestID  string
}

func (s *Store) loadAckedLifecycleMeta(ctx context.Context, evt *pb.PolicyAckEvent) (ackLifecycleMeta, error) {
	if evt == nil {
		return ackLifecycleMeta{}, nil
	}
	traceID := strings.TrimSpace(evt.TraceId)
	if traceID == "" {
		traceID = strings.TrimSpace(evt.QcReference)
	}
	if traceID != "" {
		meta, err := s.queryAckedLifecycleMeta(ctx, `
			SELECT id::STRING, COALESCE(workflow_id, ''), COALESCE(request_id, '')
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND trace_id = $2
			  AND status = 'acked'
			ORDER BY updated_at DESC
			LIMIT 1
		`, evt.PolicyId, traceID)
		if err == nil || err != sql.ErrNoRows {
			return meta, err
		}
	}
	if len(evt.RuleHash) > 0 {
		meta, err := s.queryAckedLifecycleMeta(ctx, `
			SELECT id::STRING, COALESCE(workflow_id, ''), COALESCE(request_id, '')
			FROM control_policy_outbox
			WHERE policy_id = $1
			  AND rule_hash = $2
			  AND status = 'acked'
			ORDER BY updated_at DESC
			LIMIT 1
		`, evt.PolicyId, evt.RuleHash)
		if err == nil || err != sql.ErrNoRows {
			return meta, err
		}
	}
	return s.queryAckedLifecycleMeta(ctx, `
		SELECT id::STRING, COALESCE(workflow_id, ''), COALESCE(request_id, '')
		FROM control_policy_outbox
		WHERE policy_id = $1
		  AND status = 'acked'
		ORDER BY updated_at DESC
		LIMIT 1
	`, evt.PolicyId)
}

func (s *Store) queryAckedLifecycleMeta(ctx context.Context, query string, args ...any) (ackLifecycleMeta, error) {
	var meta ackLifecycleMeta
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&meta.OutboxID, &meta.WorkflowID, &meta.RequestID)
	if err != nil {
		return ackLifecycleMeta{}, err
	}
	return meta, nil
}

func ackReasonText(evt *pb.PolicyAckEvent) string {
	if evt == nil {
		return "policy ACK recorded"
	}
	result := strings.TrimSpace(evt.Result)
	reason := strings.TrimSpace(evt.Reason)
	switch {
	case result != "" && reason != "":
		return fmt.Sprintf("policy ACK recorded: %s | %s", result, reason)
	case result != "":
		return fmt.Sprintf("policy ACK recorded: %s", result)
	case reason != "":
		return fmt.Sprintf("policy ACK recorded: %s", reason)
	default:
		return "policy ACK recorded"
	}
}
