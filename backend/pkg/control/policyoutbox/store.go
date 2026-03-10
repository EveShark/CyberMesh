package policyoutbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Store struct {
	db               *sql.DB
	hasSourceColumns bool
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

	query := `
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
	`
	if s.hasSourceColumns {
		query += `
		RETURNING id::STRING, block_height, block_ts, tx_index, policy_id, COALESCE(trace_id, ''), COALESCE(ai_event_ts_ms, 0), COALESCE(source_event_id, ''), COALESCE(source_event_ts_ms, 0), rule_hash, payload, status, retries, lease_epoch`
	} else {
		query += `
		RETURNING id::STRING, block_height, block_ts, tx_index, policy_id, COALESCE(trace_id, ''), COALESCE(ai_event_ts_ms, 0), rule_hash, payload, status, retries, lease_epoch`
	}
	rows, err := s.db.QueryContext(ctx, query, limit, holderID, epoch, reclaimAfter.String())
	if err != nil {
		return nil, fmt.Errorf("policy outbox: claim pending: %w", err)
	}
	defer rows.Close()

	out := make([]Row, 0, limit)
	for rows.Next() {
		var r Row
		var scanErr error
		if s.hasSourceColumns {
			scanErr = rows.Scan(
				&r.ID, &r.BlockHeight, &r.BlockTS, &r.TxIndex, &r.PolicyID, &r.TraceID, &r.AIEventTsMs, &r.SourceEventID, &r.SourceEventTsMs, &r.RuleHash, &r.Payload, &r.Status, &r.Retries, &r.LeaseEpoch,
			)
		} else {
			scanErr = rows.Scan(
				&r.ID, &r.BlockHeight, &r.BlockTS, &r.TxIndex, &r.PolicyID, &r.TraceID, &r.AIEventTsMs, &r.RuleHash, &r.Payload, &r.Status, &r.Retries, &r.LeaseEpoch,
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
