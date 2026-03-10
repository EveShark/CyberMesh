package policyack

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"backend/pkg/utils"
	pb "backend/proto"
)

type Store struct {
	db    *sql.DB
	stats ackStoreStats
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
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("policy ack store: db required")
	}
	return &Store{
		db: db,
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

	_, err := s.db.ExecContext(ctx, `
		UPSERT INTO policy_acks (
			policy_id, controller_instance,
			scope_identifier, tenant, region,
			result, reason, error_code,
			applied_at, acked_at,
			qc_reference, fast_path,
			rule_hash, producer_id,
			observed_at
		) VALUES (
			$1, $2,
			$3, $4, $5,
			$6, $7, $8,
			$9, $10,
			$11, $12,
			$13, $14,
			$15
		)
	`, evt.PolicyId, evt.ControllerInstance,
		evt.ScopeIdentifier, evt.Tenant, evt.Region,
		evt.Result, evt.Reason, evt.ErrorCode,
		appliedAt, ackedAt,
		evt.QcReference, evt.FastPath,
		evt.RuleHash, evt.ProducerId,
		observedAt.UTC(),
	)
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
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO policy_ack_events (
			policy_id, controller_instance,
			scope_identifier, tenant, region,
			result, reason, error_code,
			applied_at, acked_at,
			qc_reference, fast_path,
			rule_hash, producer_id,
			observed_at
		) VALUES (
			$1, $2,
			$3, $4, $5,
			$6, $7, $8,
			$9, $10,
			$11, $12,
			$13, $14,
			$15
		)
	`, evt.PolicyId, evt.ControllerInstance,
		evt.ScopeIdentifier, evt.Tenant, evt.Region,
		evt.Result, evt.Reason, evt.ErrorCode,
		appliedAt, ackedAt,
		evt.QcReference, evt.FastPath,
		evt.RuleHash, evt.ProducerId,
		observedAt.UTC(),
	)
	return err
}

func (s *Store) Stats() StoreStats {
	if s == nil {
		return StoreStats{}
	}
	aiBuckets, aiCount, aiSum := s.stats.aiToAckLatency.Snapshot()
	sourceBuckets, sourceCount, sourceSum := s.stats.sourceToAckLatency.Snapshot()
	pubBuckets, pubCount, pubSum := s.stats.publishToAckLatency.Snapshot()
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
	strongCorrelationMaxAge = 30 * time.Minute
	hashCorrelationMaxAge   = 5 * time.Minute
	// Clamp untrusted event ackedAt to a narrow window around trusted server time.
	correlationAckMaxPastSkew   = 2 * time.Minute
	correlationAckMaxFutureSkew = 30 * time.Second
)

func (s *Store) correlateOutbox(ctx context.Context, evt *pb.PolicyAckEvent, ackedAt sql.NullTime, trustedNow time.Time) (sql.NullInt64, sql.NullInt64, sql.NullTime, sql.NullTime, correlationMode, error) {
	traceID := strings.TrimSpace(evt.QcReference)
	hasRuleHash := len(evt.RuleHash) > 0
	strongCutoff := correlationCutoff(ackedAt, trustedNow, strongCorrelationMaxAge)
	hashCutoff := correlationCutoff(ackedAt, trustedNow, hashCorrelationMaxAge)

	if traceID != "" && hasRuleHash {
		aiTs, sourceTs, pubAt, ackAt, err := s.correlateExact(ctx, evt.PolicyId, evt.RuleHash, traceID, evt.Result, evt.Reason, evt.ControllerInstance, ackedAt, strongCutoff)
		if err == nil {
			return aiTs, sourceTs, pubAt, ackAt, correlationExact, nil
		}
		if err != sql.ErrNoRows {
			return sql.NullInt64{}, sql.NullInt64{}, sql.NullTime{}, sql.NullTime{}, correlationNone, err
		}
	}

	if traceID != "" {
		aiTs, sourceTs, pubAt, ackAt, err := s.correlateByTraceID(ctx, evt.PolicyId, traceID, evt.Result, evt.Reason, evt.ControllerInstance, ackedAt, strongCutoff)
		if err == nil {
			return aiTs, sourceTs, pubAt, ackAt, correlationFallbackTrace, nil
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
			  AND status IN ('publishing', 'published')
			  AND created_at >= $8
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$4,
			ack_reason=$5,
			ack_controller=$6,
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
			  AND status IN ('publishing', 'published')
			  AND created_at >= $7
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
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
			  AND status IN ('publishing', 'published')
			  AND created_at >= $7
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
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
			  AND status IN ('publishing', 'published')
			  AND created_at >= $8
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$4,
			ack_reason=$5,
			ack_controller=$6,
			acked_at=COALESCE($7, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, ruleHash, traceID, result, reason, controller, ackedAt, cutoff).Scan(&aiEventTsMs, &publishedAt, &ackedAtRow)
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
			  AND status IN ('publishing', 'published')
			  AND created_at >= $7
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
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
			  AND status IN ('publishing', 'published')
			  AND created_at >= $7
			ORDER BY created_at DESC
			LIMIT 1
		)
		UPDATE control_policy_outbox
		SET status='acked',
			ack_result=$3,
			ack_reason=$4,
			ack_controller=$5,
			acked_at=COALESCE($6, now()),
			updated_at=now()
		WHERE id IN (SELECT id FROM chosen)
		RETURNING (SELECT ai_event_ts_ms FROM chosen), (SELECT published_at FROM chosen), acked_at
	`, policyID, traceID, result, reason, controller, ackedAt, cutoff).Scan(&aiEventTsMs, &publishedAt, &ackedAtRow)
	return aiEventTsMs, sql.NullInt64{}, publishedAt, ackedAtRow, err
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
