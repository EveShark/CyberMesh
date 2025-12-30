package cockroach

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"backend/pkg/consensus/types"
)

func (a *adapter) SaveProposal(ctx context.Context, hash []byte, height uint64, view uint64, proposer []byte, data []byte) error {
	stop := a.recordQuery("save_proposal")
	defer stop()
	if len(hash) != 32 {
		return fmt.Errorf("save proposal: invalid hash length %d", len(hash))
	}
	if len(proposer) == 0 {
		return fmt.Errorf("save proposal: proposer id required")
	}
	_, err := a.stmtUpsertProposal.ExecContext(ctx, hash, height, view, proposer, data)
	if err != nil {
		return fmt.Errorf("save proposal: %w", err)
	}
	return nil
}

func (a *adapter) LoadProposal(ctx context.Context, hash []byte) ([]byte, error) {
	stop := a.recordQuery("load_proposal")
	defer stop()
	row := a.stmtGetProposal.QueryRowContext(ctx, hash)
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBlockNotFound
		}
		return nil, fmt.Errorf("load proposal: %w", err)
	}
	return payload, nil
}

func (a *adapter) SaveQC(ctx context.Context, hash []byte, height uint64, view uint64, data []byte) error {
	stop := a.recordQuery("save_qc")
	defer stop()
	if len(hash) != 32 {
		return fmt.Errorf("save qc: invalid hash length %d", len(hash))
	}
	_, err := a.stmtUpsertQC.ExecContext(ctx, hash, height, view, data)
	if err != nil {
		return fmt.Errorf("save qc: %w", err)
	}
	return nil
}

func (a *adapter) LoadQC(ctx context.Context, hash []byte) ([]byte, error) {
	stop := a.recordQuery("load_qc")
	defer stop()
	row := a.stmtGetQC.QueryRowContext(ctx, hash)
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBlockNotFound
		}
		return nil, fmt.Errorf("load qc: %w", err)
	}
	return payload, nil
}

func (a *adapter) ListProposals(ctx context.Context, minHeight uint64, limit int) ([]types.ProposalRecord, error) {
	stop := a.recordQuery("list_proposals")
	defer stop()
	rows, err := a.stmtListProposals.QueryContext(ctx, minHeight, limit)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()

	var records []types.ProposalRecord
	for rows.Next() {
		var rec types.ProposalRecord
		if err := rows.Scan(&rec.Hash, &rec.Height, &rec.View, &rec.Proposer, &rec.Data); err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proposals: %w", err)
	}
	return records, nil
}

func (a *adapter) ListQCs(ctx context.Context, minHeight uint64, limit int) ([]types.QCRecord, error) {
	stop := a.recordQuery("list_qcs")
	defer stop()
	rows, err := a.stmtListQCs.QueryContext(ctx, minHeight, limit)
	if err != nil {
		return nil, fmt.Errorf("list qcs: %w", err)
	}
	defer rows.Close()

	var records []types.QCRecord
	for rows.Next() {
		var rec types.QCRecord
		if err := rows.Scan(&rec.Hash, &rec.Height, &rec.View, &rec.Data); err != nil {
			return nil, fmt.Errorf("scan qc: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate qcs: %w", err)
	}
	return records, nil
}

func (a *adapter) SaveVote(ctx context.Context, view uint64, height uint64, voter []byte, blockHash []byte, voteHash []byte, data []byte) error {
	stop := a.recordQuery("save_vote")
	defer stop()
	if len(voteHash) != 32 {
		return fmt.Errorf("save vote: invalid vote hash length %d", len(voteHash))
	}
	if len(blockHash) != 32 {
		return fmt.Errorf("save vote: invalid block hash length %d", len(blockHash))
	}
	_, err := a.stmtUpsertVote.ExecContext(ctx, voteHash, view, height, voter, blockHash, data)
	if err != nil {
		return fmt.Errorf("save vote: %w", err)
	}
	return nil
}

func (a *adapter) ListVotes(ctx context.Context, minHeight uint64, limit int) ([]types.VoteRecord, error) {
	stop := a.recordQuery("list_votes")
	defer stop()
	rows, err := a.stmtListVotes.QueryContext(ctx, minHeight, limit)
	if err != nil {
		return nil, fmt.Errorf("list votes: %w", err)
	}
	defer rows.Close()

	var records []types.VoteRecord
	for rows.Next() {
		var rec types.VoteRecord
		if err := rows.Scan(&rec.Hash, &rec.View, &rec.Height, &rec.Voter, &rec.BlockHash, &rec.Data); err != nil {
			return nil, fmt.Errorf("scan vote: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate votes: %w", err)
	}
	return records, nil
}

func (a *adapter) SaveEvidence(ctx context.Context, hash []byte, height uint64, data []byte) error {
	stop := a.recordQuery("save_evidence")
	defer stop()
	if len(hash) != 32 {
		return fmt.Errorf("save evidence: invalid hash length %d", len(hash))
	}
	_, err := a.stmtUpsertEvidence.ExecContext(ctx, hash, height, data)
	if err != nil {
		return fmt.Errorf("save evidence: %w", err)
	}
	return nil
}

func (a *adapter) LoadEvidence(ctx context.Context, hash []byte) ([]byte, error) {
	stop := a.recordQuery("load_evidence")
	defer stop()
	row := a.stmtGetEvidence.QueryRowContext(ctx, hash)
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBlockNotFound
		}
		return nil, fmt.Errorf("load evidence: %w", err)
	}
	return payload, nil
}

func (a *adapter) ListEvidence(ctx context.Context, minHeight uint64, limit int) ([]types.EvidenceRecord, error) {
	stop := a.recordQuery("list_evidence")
	defer stop()
	rows, err := a.stmtListEvidence.QueryContext(ctx, minHeight, limit)
	if err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}
	defer rows.Close()

	var records []types.EvidenceRecord
	for rows.Next() {
		var rec types.EvidenceRecord
		if err := rows.Scan(&rec.Hash, &rec.Height, &rec.Data); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate evidence: %w", err)
	}
	return records, nil
}

func (a *adapter) SaveCommittedBlock(ctx context.Context, height uint64, hash []byte, qc []byte) error {
	stop := a.recordQuery("save_committed_block")
	defer stop()
	if len(hash) != 32 {
		return fmt.Errorf("save committed block: invalid block hash length %d", len(hash))
	}
	// Safety: only allow updating last_committed metadata to durable truth.
	// This prevents foreign writers or buggy call paths from pushing metadata ahead of persisted blocks.
	var durable sql.NullInt64
	if err := a.db.QueryRowContext(ctx, `SELECT MAX(height) FROM blocks`).Scan(&durable); err != nil {
		return fmt.Errorf("save committed block: load durable height: %w", err)
	}
	if !durable.Valid || durable.Int64 <= 0 {
		if height > 0 {
			if a.auditLogger != nil {
				_ = a.auditLogger.Security("consensus_metadata_write_rejected_no_blocks", map[string]interface{}{
					"requested_height": height,
				})
			}
			return fmt.Errorf("save committed block: no durable blocks; refusing metadata write")
		}
		return nil
	}
	durableHeight := uint64(durable.Int64)
	if height != durableHeight {
		if a.auditLogger != nil {
			_ = a.auditLogger.Security("consensus_metadata_write_rejected_height_mismatch", map[string]interface{}{
				"requested_height": height,
				"durable_height":   durableHeight,
			})
		}
		return fmt.Errorf("save committed block: requested height=%d does not match durable height=%d", height, durableHeight)
	}
	var durableHash []byte
	if err := a.db.QueryRowContext(ctx, `SELECT block_hash FROM blocks WHERE height = $1`, durableHeight).Scan(&durableHash); err != nil {
		return fmt.Errorf("save committed block: load durable hash: %w", err)
	}
	if len(durableHash) != 32 {
		return fmt.Errorf("save committed block: invalid durable hash length %d", len(durableHash))
	}
	if !bytes.Equal(durableHash, hash) {
		if a.auditLogger != nil {
			_ = a.auditLogger.Security("consensus_metadata_write_rejected_hash_mismatch", map[string]interface{}{
				"height": durableHeight,
			})
		}
		return fmt.Errorf("save committed block: hash mismatch for durable height=%d", durableHeight)
	}
	if _, err := a.stmtUpsertMeta.ExecContext(ctx, durableHeight, durableHash, qc); err != nil {
		return fmt.Errorf("save committed block metadata: %w", err)
	}
	return nil
}

func (a *adapter) LoadLastCommitted(ctx context.Context) (uint64, []byte, []byte, error) {
	stop := a.recordQuery("load_last_committed")
	defer stop()
	// Derive committed height from durable blocks table, not consensus_metadata.
	// This prevents restart corruption when metadata gets ahead of persisted blocks.
	var maxHeight sql.NullInt64
	if err := a.db.QueryRowContext(ctx, `SELECT MAX(height) FROM blocks`).Scan(&maxHeight); err != nil {
		return 0, nil, nil, fmt.Errorf("load last committed from blocks: %w", err)
	}
	if !maxHeight.Valid || maxHeight.Int64 <= 0 {
		// Genesis / empty durable state. If metadata exists, it's stale/corrupt (likely foreign writer).
		var metaHeight sql.NullInt64
		err := a.db.QueryRowContext(ctx, `SELECT height FROM consensus_metadata WHERE key = 'last_committed'`).Scan(&metaHeight)
		switch {
		case err == nil && metaHeight.Valid && metaHeight.Int64 > 0:
			if a.auditLogger != nil {
				_ = a.auditLogger.Security("consensus_height_drift_detected", map[string]interface{}{
					"metadata_height": uint64(metaHeight.Int64),
					"blocks_height":   uint64(0),
					"drift":           uint64(metaHeight.Int64),
					"action":          "delete_metadata_on_empty_blocks",
				})
			}
			if _, delErr := a.db.ExecContext(ctx, `DELETE FROM consensus_metadata WHERE key = 'last_committed'`); delErr != nil {
				return 0, nil, nil, fmt.Errorf("delete stale consensus_metadata on empty blocks: %w", delErr)
			}
		case errors.Is(err, sql.ErrNoRows):
			// no metadata
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			// Ignore metadata read failures; blocks table is the source of truth.
		}
		return 0, nil, nil, nil
	}
	height := uint64(maxHeight.Int64)

	var hash []byte
	if err := a.db.QueryRowContext(ctx, `SELECT block_hash FROM blocks WHERE height = $1`, height).Scan(&hash); err != nil {
		return 0, nil, nil, fmt.Errorf("load last committed hash from blocks: %w", err)
	}
	if len(hash) != 32 {
		return 0, nil, nil, fmt.Errorf("load last committed: invalid hash length %d", len(hash))
	}

	// Repair consensus_metadata.last_committed to durable truth.
	// SECURITY: metadata ahead of durable blocks indicates corruption or foreign writer.
	var metaHeight sql.NullInt64
	metaErr := a.db.QueryRowContext(ctx, `SELECT height FROM consensus_metadata WHERE key = 'last_committed'`).Scan(&metaHeight)
	metaHeightU := uint64(0)
	metaFound := false
	switch {
	case metaErr == nil && metaHeight.Valid:
		metaFound = true
		metaHeightU = uint64(metaHeight.Int64)
	case errors.Is(metaErr, sql.ErrNoRows):
		metaFound = false
	case metaErr != nil:
		// Ignore metadata read failures; blocks table is the source of truth.
	}
	if metaFound && metaHeightU != height {
		drift := uint64(0)
		if metaHeightU > height {
			drift = metaHeightU - height
			if a.auditLogger != nil {
				_ = a.auditLogger.Security("consensus_height_drift_detected", map[string]interface{}{
					"metadata_height": metaHeightU,
					"blocks_height":   height,
					"drift":           drift,
					"action":          "repair_metadata_down_to_durable",
				})
			}
		} else {
			drift = height - metaHeightU
			if a.auditLogger != nil {
				_ = a.auditLogger.Info("consensus_height_drift_detected", map[string]interface{}{
					"metadata_height": metaHeightU,
					"blocks_height":   height,
					"drift":           drift,
					"action":          "repair_metadata_up_to_durable",
				})
			}
		}
	}

	// Best-effort: if we have a persisted QC for last_committed, return it.
	// This is not guessing; it's only using data we already stored.
	var qcBytes []byte
	{
		var metaH sql.NullInt64
		var metaHash []byte
		var metaQC []byte
		err := a.db.QueryRowContext(ctx, `SELECT height, block_hash, qc_cbor FROM consensus_metadata WHERE key = 'last_committed'`).Scan(&metaH, &metaHash, &metaQC)
		switch {
		case err == nil:
			if metaH.Valid && uint64(metaH.Int64) == height && bytes.Equal(metaHash, hash) && len(metaQC) > 0 {
				qcBytes = metaQC
			}
		case errors.Is(err, sql.ErrNoRows):
			// no metadata yet
		default:
			// Ignore metadata read failures; blocks table is the source of truth.
		}
	}

	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO consensus_metadata (key, height, block_hash, qc_cbor, updated_at)
		VALUES ('last_committed', $1, $2, $3, NOW())
		ON CONFLICT (key) DO UPDATE
		SET height = EXCLUDED.height,
		    block_hash = EXCLUDED.block_hash,
		    qc_cbor = EXCLUDED.qc_cbor,
		    updated_at = NOW()
	`, height, hash, qcBytes); err != nil {
		// Drift repair failure should fail fast; continuing tends to cause view loops / proposal collisions.
		if a.auditLogger != nil {
			_ = a.auditLogger.Security("consensus_metadata_repair_failed", map[string]interface{}{
				"height": height,
				"error":  err.Error(),
			})
		}
		return 0, nil, nil, fmt.Errorf("consensus metadata repair failed: %w", err)
	}

	return height, hash, qcBytes, nil
}

func (a *adapter) GetCommittedBlockHash(ctx context.Context, height uint64) ([]byte, bool, error) {
	stop := a.recordQuery("get_committed_block_hash")
	defer stop()
	row := a.stmtGetCommitted.QueryRowContext(ctx, height)
	var hash []byte
	if err := row.Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get committed block hash: %w", err)
	}
	return hash, true, nil
}

func (a *adapter) DeleteBefore(ctx context.Context, height uint64) error {
	stop := a.recordQuery("delete_before_height")
	defer stop()
	if _, err := a.stmtDeleteProposalsBefore.ExecContext(ctx, height); err != nil {
		return fmt.Errorf("delete proposals before: %w", err)
	}
	if _, err := a.stmtDeleteVotesBefore.ExecContext(ctx, height); err != nil {
		return fmt.Errorf("delete votes before: %w", err)
	}
	if _, err := a.stmtDeleteQCsBefore.ExecContext(ctx, height); err != nil {
		return fmt.Errorf("delete qcs before: %w", err)
	}
	if _, err := a.stmtDeleteEvidenceBefore.ExecContext(ctx, height); err != nil {
		return fmt.Errorf("delete evidence before: %w", err)
	}
	return nil
}

func (a *adapter) LoadGenesisCertificate(ctx context.Context, networkID string, configHash [32]byte, peerHash [32]byte) ([]byte, bool, error) {
	stop := a.recordQuery("load_genesis_certificate")
	defer stop()
	if networkID == "" {
		return nil, false, fmt.Errorf("load genesis certificate: network_id required")
	}
	row := a.stmtLoadGenesisCert.QueryRowContext(ctx, networkID, configHash[:], peerHash[:])
	var cert []byte
	if err := row.Scan(&cert); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load genesis certificate: %w", err)
	}
	return cert, true, nil
}

func (a *adapter) SaveGenesisCertificate(ctx context.Context, networkID string, configHash [32]byte, peerHash [32]byte, data []byte) error {
	stop := a.recordQuery("save_genesis_certificate")
	defer stop()
	if networkID == "" {
		return fmt.Errorf("save genesis certificate: network_id required")
	}
	if len(data) == 0 {
		return fmt.Errorf("save genesis certificate: data required")
	}
	if _, err := a.stmtUpsertGenesisCert.ExecContext(ctx, networkID, configHash[:], peerHash[:], data); err != nil {
		return fmt.Errorf("save genesis certificate: %w", err)
	}
	return nil
}

func (a *adapter) DeleteGenesisCertificate(ctx context.Context, networkID string, configHash [32]byte, peerHash [32]byte) error {
	stop := a.recordQuery("delete_genesis_certificate")
	defer stop()
	if networkID == "" {
		return fmt.Errorf("delete genesis certificate: network_id required")
	}
	if _, err := a.stmtDeleteGenesisCert.ExecContext(ctx, networkID, configHash[:], peerHash[:]); err != nil {
		return fmt.Errorf("delete genesis certificate: %w", err)
	}
	return nil
}
