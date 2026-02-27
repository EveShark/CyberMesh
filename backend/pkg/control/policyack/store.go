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
	skewCorrections     atomic.Uint64
	aiToAckLatency      *utils.LatencyHistogram
	publishToAckLatency *utils.LatencyHistogram
}

type StoreStats struct {
	SkewCorrectionsTotal uint64
	AIToAckBuckets       []utils.HistogramBucket
	AIToAckCount         uint64
	AIToAckSumMs         float64
	AIToAckP95Ms         float64
	PublishToAckBuckets  []utils.HistogramBucket
	PublishToAckCount    uint64
	PublishToAckSumMs    float64
	PublishToAckP95Ms    float64
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
	if evt.AppliedAt > 0 {
		appliedAt = sql.NullTime{Time: time.Unix(evt.AppliedAt, 0).UTC(), Valid: true}
	}
	var ackedAt sql.NullTime
	if evt.AckedAt > 0 {
		ackedAt = sql.NullTime{Time: time.Unix(evt.AckedAt, 0).UTC(), Valid: true}
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
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

	// Correlate ACK with durable outbox: exact (policy_id+rule_hash+trace_id) first, fallback second.
	var ruleHash interface{}
	if len(evt.RuleHash) > 0 {
		ruleHash = evt.RuleHash
	}
	traceID := strings.TrimSpace(evt.QcReference)
	var aiEventTsMs sql.NullInt64
	var publishedAt sql.NullTime
	var ackedAtRow sql.NullTime
	exactErr := sql.ErrNoRows
	if traceID != "" {
		exactErr = s.db.QueryRowContext(ctx, `
			WITH chosen AS (
				SELECT id, ai_event_ts_ms, published_at
				FROM control_policy_outbox
				WHERE policy_id = $1
				  AND ($2::BYTES IS NULL OR rule_hash = $2)
				  AND trace_id = $3
				  AND status IN ('published', 'acked')
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
		`, evt.PolicyId, ruleHash, traceID, evt.Result, evt.Reason, evt.ControllerInstance, ackedAt).Scan(&aiEventTsMs, &publishedAt, &ackedAtRow)
	}
	if exactErr == nil {
		// Exact-path correlation succeeded.
	} else if exactErr != sql.ErrNoRows {
		msg := strings.ToLower(exactErr.Error())
		if strings.Contains(msg, "control_policy_outbox") && strings.Contains(msg, "does not exist") {
			return nil
		}
		return fmt.Errorf("policy ack store: correlate outbox exact failed: %w", exactErr)
	} else {
		err = s.db.QueryRowContext(ctx, `
			WITH chosen AS (
				SELECT id, ai_event_ts_ms, published_at
				FROM control_policy_outbox
				WHERE policy_id = $1
				  AND ($2::BYTES IS NULL OR rule_hash = $2)
				  AND status IN ('published', 'acked')
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
		`, evt.PolicyId, ruleHash, evt.Result, evt.Reason, evt.ControllerInstance, ackedAt).Scan(&aiEventTsMs, &publishedAt, &ackedAtRow)
		if err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "control_policy_outbox") && strings.Contains(msg, "does not exist") {
				return nil
			}
			if err == sql.ErrNoRows {
				return nil
			}
			return fmt.Errorf("policy ack store: correlate outbox fallback failed: %w", err)
		}
	}

	if ackedAtRow.Valid && aiEventTsMs.Valid && aiEventTsMs.Int64 > 0 {
		latencyMs := ackedAtRow.Time.UnixMilli() - aiEventTsMs.Int64
		if latencyMs < 0 {
			latencyMs = 0
			s.stats.skewCorrections.Add(1)
		}
		s.stats.aiToAckLatency.Observe(float64(latencyMs))
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

func (s *Store) Stats() StoreStats {
	if s == nil {
		return StoreStats{}
	}
	aiBuckets, aiCount, aiSum := s.stats.aiToAckLatency.Snapshot()
	pubBuckets, pubCount, pubSum := s.stats.publishToAckLatency.Snapshot()
	return StoreStats{
		SkewCorrectionsTotal: s.stats.skewCorrections.Load(),
		AIToAckBuckets:       aiBuckets,
		AIToAckCount:         aiCount,
		AIToAckSumMs:         aiSum,
		AIToAckP95Ms:         s.stats.aiToAckLatency.Quantile(0.95),
		PublishToAckBuckets:  pubBuckets,
		PublishToAckCount:    pubCount,
		PublishToAckSumMs:    pubSum,
		PublishToAckP95Ms:    s.stats.publishToAckLatency.Quantile(0.95),
	}
}
