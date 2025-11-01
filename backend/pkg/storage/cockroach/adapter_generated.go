package cockroach

import (
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
	if _, err := a.stmtUpsertMeta.ExecContext(ctx, height, hash, qc); err != nil {
		return fmt.Errorf("save committed block metadata: %w", err)
	}
	return nil
}

func (a *adapter) LoadLastCommitted(ctx context.Context) (uint64, []byte, []byte, error) {
	stop := a.recordQuery("load_last_committed")
	defer stop()
	row := a.stmtGetMeta.QueryRowContext(ctx)
	var height sql.NullInt64
	var hash []byte
	var qc []byte
	if err := row.Scan(&height, &hash, &qc); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil, nil, nil
		}
		return 0, nil, nil, fmt.Errorf("load metadata: %w", err)
	}
	if !height.Valid {
		return 0, nil, nil, nil
	}
	if len(hash) > 0 && len(hash) != 32 {
		return 0, nil, nil, fmt.Errorf("load metadata: invalid hash length %d", len(hash))
	}
	return uint64(height.Int64), hash, qc, nil
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

func (a *adapter) LoadGenesisCertificate(ctx context.Context) ([]byte, bool, error) {
	stop := a.recordQuery("load_genesis_certificate")
	defer stop()
	row := a.stmtLoadGenesisCert.QueryRowContext(ctx)
	var cert []byte
	if err := row.Scan(&cert); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load genesis certificate: %w", err)
	}
	return cert, true, nil
}

func (a *adapter) SaveGenesisCertificate(ctx context.Context, data []byte) error {
	stop := a.recordQuery("save_genesis_certificate")
	defer stop()
	if len(data) == 0 {
		return fmt.Errorf("save genesis certificate: data required")
	}
	if _, err := a.stmtUpsertGenesisCert.ExecContext(ctx, data); err != nil {
		return fmt.Errorf("save genesis certificate: %w", err)
	}
	return nil
}

func (a *adapter) DeleteGenesisCertificate(ctx context.Context) error {
	stop := a.recordQuery("delete_genesis_certificate")
	defer stop()
	if _, err := a.stmtDeleteGenesisCert.ExecContext(ctx); err != nil {
		return fmt.Errorf("delete genesis certificate: %w", err)
	}
	return nil
}
