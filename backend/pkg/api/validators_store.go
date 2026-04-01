package api

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"backend/pkg/consensus/types"
)

type validatorStoreSupport struct {
	enabled bool
}

var validatorStoreCache sync.Map // map[*sql.DB]validatorStoreSupport

func loadValidatorStoreSupport(ctx context.Context, db *sql.DB) (validatorStoreSupport, error) {
	if db == nil {
		return validatorStoreSupport{}, nil
	}
	if cached, ok := validatorStoreCache.Load(db); ok {
		return cached.(validatorStoreSupport), nil
	}
	support := validatorStoreSupport{}
	var reg sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('validators')::STRING`).Scan(&reg); err != nil {
		return support, err
	}
	support.enabled = reg.Valid && reg.String != ""
	validatorStoreCache.Store(db, support)
	return support, nil
}

func syncValidatorSnapshots(ctx context.Context, db *sql.DB, validators []types.ValidatorInfo) error {
	if db == nil || len(validators) == 0 {
		return nil
	}
	support, err := loadValidatorStoreSupport(ctx, db)
	if err != nil || !support.enabled {
		return err
	}
	now := time.Now().UTC()
	for _, info := range validators {
		lastSeen := any(nil)
		if !info.LastSeen.IsZero() {
			lastSeen = info.LastSeen.UTC()
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO validators (
				validator_id, public_key, peer_id, is_active, reputation,
				joined_height, last_seen, updated_at
			) VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, $8)
			ON CONFLICT (validator_id) DO UPDATE SET
				public_key = EXCLUDED.public_key,
				peer_id = EXCLUDED.peer_id,
				is_active = EXCLUDED.is_active,
				reputation = EXCLUDED.reputation,
				joined_height = EXCLUDED.joined_height,
				last_seen = EXCLUDED.last_seen,
				updated_at = EXCLUDED.updated_at
		`, info.ID[:], info.PublicKey, info.PeerID, info.IsActive, info.Reputation, info.JoinedView, lastSeen, now); err != nil {
			return err
		}
	}
	return nil
}
