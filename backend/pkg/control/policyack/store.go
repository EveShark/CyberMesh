package policyack

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	pb "backend/proto"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("policy ack store: db required")
	}
	return &Store{db: db}, nil
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
	return nil
}

