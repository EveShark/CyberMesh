package api

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"backend/pkg/p2p"
)

func syncP2PStateSnapshots(ctx context.Context, db *sql.DB, snapshots map[peer.ID]p2p.PeerState, ttl time.Duration, latestHeight uint64) error {
	if db == nil || len(snapshots) == 0 {
		return nil
	}
	for id, snap := range snapshots {
		producerID := []byte(id)
		lastViolation := any(nil)
		expiresAt := any(nil)
		permanent := false
		if snap.Quarantined {
			lastViolation = snap.QuarantineAt.UTC()
			if ttl > 0 {
				expiresAt = snap.QuarantineAt.UTC().Add(ttl)
			} else {
				permanent = true
			}
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO state_reputation (
				producer_id, score, total_events, violations, last_violation, updated_height, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (producer_id) DO UPDATE SET
				score = EXCLUDED.score,
				total_events = EXCLUDED.total_events,
				violations = EXCLUDED.violations,
				last_violation = EXCLUDED.last_violation,
				updated_height = EXCLUDED.updated_height,
				updated_at = NOW()
		`, producerID, snap.Score, snap.MsgIn, quarantineViolations(snap), lastViolation, latestHeight); err != nil {
			return err
		}
		if snap.Quarantined {
			reason := strings.TrimSpace(snap.Labels["quarantine_reason"])
			if reason == "" {
				reason = "p2p-quarantined"
			}
			if _, err := db.ExecContext(ctx, `
				INSERT INTO state_quarantine (
					producer_id, reason, evidence_hash, quarantined_at, expires_at, permanent, applied_height
				) VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (producer_id) DO UPDATE SET
					reason = EXCLUDED.reason,
					evidence_hash = EXCLUDED.evidence_hash,
					quarantined_at = EXCLUDED.quarantined_at,
					expires_at = EXCLUDED.expires_at,
					permanent = EXCLUDED.permanent,
					applied_height = EXCLUDED.applied_height
			`, producerID, reason, producerID, snap.QuarantineAt.UTC(), expiresAt, permanent, latestHeight); err != nil {
				return err
			}
		} else {
			if _, err := db.ExecContext(ctx, `DELETE FROM state_quarantine WHERE producer_id = $1`, producerID); err != nil {
				return err
			}
		}
	}
	return nil
}

func quarantineViolations(snap p2p.PeerState) uint64 {
	if snap.Quarantined {
		return 1
	}
	return 0
}
