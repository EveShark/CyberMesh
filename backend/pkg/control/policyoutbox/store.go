package policyoutbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("policy outbox: db required")
	}
	return &Store{db: db}, nil
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

	var holder string
	var epoch int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO control_dispatcher_leases (lease_key, holder_id, epoch, lease_until, updated_at)
		VALUES ($1, $2, 1, now() + $3::INTERVAL, now())
		ON CONFLICT (lease_key) DO UPDATE
		SET holder_id = CASE
				WHEN control_dispatcher_leases.holder_id = $2 OR control_dispatcher_leases.lease_until < now()
				THEN $2
				ELSE control_dispatcher_leases.holder_id
			END,
			epoch = CASE
				WHEN control_dispatcher_leases.holder_id = $2 THEN control_dispatcher_leases.epoch
				WHEN control_dispatcher_leases.lease_until < now() THEN control_dispatcher_leases.epoch + 1
				ELSE control_dispatcher_leases.epoch
			END,
			lease_until = CASE
				WHEN control_dispatcher_leases.holder_id = $2 OR control_dispatcher_leases.lease_until < now()
				THEN now() + $3::INTERVAL
				ELSE control_dispatcher_leases.lease_until
			END,
			updated_at = now()
		RETURNING holder_id, epoch
	`, leaseKey, holderID, ttl.String()).Scan(&holder, &epoch)
	if err != nil {
		return false, 0, fmt.Errorf("policy outbox: acquire lease: %w", err)
	}
	return holder == holderID, epoch, nil
}

func (s *Store) ClaimPending(ctx context.Context, holderID string, epoch int64, limit int, reclaimAfter time.Duration) ([]Row, error) {
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

	rows, err := s.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT o.id
			FROM control_policy_outbox o
			WHERE (
					o.status IN ('pending', 'retry')
					AND (o.next_retry_at IS NULL OR o.next_retry_at <= now())
				)
				OR (
					o.status = 'publishing'
					AND (
						o.lease_holder IS NULL
						OR o.updated_at < now() - $4::INTERVAL
						OR NOT EXISTS (
							SELECT 1
							FROM control_dispatcher_leases l
							WHERE l.holder_id = o.lease_holder
							  AND l.epoch = o.lease_epoch
							  AND l.lease_until >= now()
						)
					)
				)
			ORDER BY created_at ASC
			LIMIT $1
		)
		UPDATE control_policy_outbox
		SET status='publishing',
			lease_holder=$2,
			lease_epoch=$3,
			updated_at=now()
		WHERE id IN (SELECT id FROM candidates)
		RETURNING id::STRING, block_height, block_ts, tx_index, policy_id, COALESCE(trace_id, ''), COALESCE(ai_event_ts_ms, 0), rule_hash, payload, status, retries, lease_epoch
	`, limit, holderID, epoch, reclaimAfter.String())
	if err != nil {
		return nil, fmt.Errorf("policy outbox: claim pending: %w", err)
	}
	defer rows.Close()

	out := make([]Row, 0, limit)
	for rows.Next() {
		var r Row
		if scanErr := rows.Scan(
			&r.ID, &r.BlockHeight, &r.BlockTS, &r.TxIndex, &r.PolicyID, &r.TraceID, &r.AIEventTsMs, &r.RuleHash, &r.Payload, &r.Status, &r.Retries, &r.LeaseEpoch,
		); scanErr != nil {
			return nil, fmt.Errorf("policy outbox: claim scan: %w", scanErr)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("policy outbox: claim rows: %w", err)
	}
	return out, nil
}

func (s *Store) MarkPublished(ctx context.Context, id, holderID string, epoch int64, topic string, partition int32, offset int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("policy outbox: store not initialized")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE control_policy_outbox
		SET status=CASE WHEN status='acked' THEN 'acked' ELSE 'published' END,
			published_at=now(),
			kafka_topic=$2,
			kafka_partition=$3,
			kafka_offset=$4,
			last_error=NULL,
			updated_at=now()
		WHERE id::STRING=$1
		  AND lease_holder=$5
		  AND lease_epoch=$6
		  AND status IN ('publishing', 'acked')
	`, id, topic, partition, offset, holderID, epoch)
	if err != nil {
		return fmt.Errorf("policy outbox: mark published: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return fmt.Errorf("policy outbox: mark published fenced/no-op id=%s holder=%s epoch=%d", id, holderID, epoch)
	}
	return nil
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
		WHERE id::STRING=$1
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
		WHERE id::STRING=$1
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
			acked_at=$5,
			updated_at=now()
		WHERE id IN (SELECT id FROM latest)
	`, policyID, result, reason, controller, ackedAt.UTC())
	if err != nil {
		return fmt.Errorf("policy outbox: correlate ack: %w", err)
	}
	return nil
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
			COUNT(*) AS total_rows,
			COALESCE(SUM(CASE WHEN status='published' THEN 1 ELSE 0 END), 0) AS published_rows,
			COALESCE(SUM(CASE WHEN status='acked' THEN 1 ELSE 0 END), 0) AS acked_rows,
			COALESCE(SUM(CASE WHEN status='terminal_failed' THEN 1 ELSE 0 END), 0) AS terminal_rows,
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
	`).Scan(
		&stats.Pending,
		&stats.Retry,
		&stats.Publishing,
		&stats.TotalRows,
		&stats.PublishedRows,
		&stats.AckedRows,
		&stats.TerminalRows,
		&stats.OldestPendingAge,
	)
	if err != nil {
		return BacklogStats{}, fmt.Errorf("policy outbox: backlog stats: %w", err)
	}

	return stats, nil
}
