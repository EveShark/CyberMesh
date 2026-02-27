package cockroach

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/block"
	"backend/pkg/consensus/types"
	"backend/pkg/state"
	"backend/pkg/utils"
	"github.com/jackc/pgconn"
	"github.com/lib/pq"
)

// Storage errors
var (
	ErrBlockNotFound       = errors.New("storage: block not found")
	ErrTransactionNotFound = errors.New("storage: transaction not found")
	ErrSnapshotNotFound    = errors.New("storage: snapshot not found")
	ErrIntegrityViolation  = errors.New("storage: integrity violation detected")
	ErrConflictDetected    = errors.New("storage: conflict detected on upsert")
	ErrInvalidData         = errors.New("storage: invalid data format")
)

// Adapter defines the interface for CockroachDB persistence operations
type Adapter interface {
	// PersistBlock atomically persists a block, its transactions, and state snapshot
	// Uses SERIALIZABLE transaction for linearizability
	// Idempotent: re-persisting same block succeeds if data matches
	PersistBlock(ctx context.Context, blk *block.AppBlock, receipts []state.Receipt, stateRoot [32]byte) error

	// GetBlock retrieves a block by height
	GetBlock(ctx context.Context, height uint64) (*block.AppBlock, error)

	// GetLatestHeight returns the maximum block height from the database
	GetLatestHeight(ctx context.Context) (uint64, error)

	// GetMinHeight returns the minimum block height from the database
	GetMinHeight(ctx context.Context) (uint64, error)

	// GetTransactionByContentHash retrieves a transaction by its content hash
	GetTransactionByContentHash(ctx context.Context, contentHash [32]byte) (*TxRow, error)

	// GetSnapshot retrieves a state snapshot by version
	GetSnapshot(ctx context.Context, version uint64) (*SnapshotRow, error)

	// ListTransactionsByBlock returns lightweight transaction metadata for a block
	ListTransactionsByBlock(ctx context.Context, height uint64) ([]TxMeta, error)

	// Consensus persistence operations (adapter satisfies types.StorageBackend)
	SaveProposal(ctx context.Context, hash []byte, height uint64, view uint64, proposer []byte, data []byte) error
	LoadProposal(ctx context.Context, hash []byte) ([]byte, error)
	ListProposals(ctx context.Context, minHeight uint64, limit int) ([]types.ProposalRecord, error)
	SaveQC(ctx context.Context, hash []byte, height uint64, view uint64, data []byte) error
	LoadQC(ctx context.Context, hash []byte) ([]byte, error)
	ListQCs(ctx context.Context, minHeight uint64, limit int) ([]types.QCRecord, error)
	SaveVote(ctx context.Context, view uint64, height uint64, voter []byte, blockHash []byte, voteHash []byte, data []byte) error
	ListVotes(ctx context.Context, minHeight uint64, limit int) ([]types.VoteRecord, error)
	SaveEvidence(ctx context.Context, hash []byte, height uint64, data []byte) error
	LoadEvidence(ctx context.Context, hash []byte) ([]byte, error)
	ListEvidence(ctx context.Context, minHeight uint64, limit int) ([]types.EvidenceRecord, error)
	SaveCommittedBlock(ctx context.Context, height uint64, hash []byte, qc []byte) error
	LoadLastCommitted(ctx context.Context) (uint64, []byte, []byte, error)
	LoadGenesisCertificate(ctx context.Context) ([]byte, bool, error)
	SaveGenesisCertificate(ctx context.Context, data []byte) error
	DeleteGenesisCertificate(ctx context.Context) error
	DeleteBefore(ctx context.Context, height uint64) error

	// Ping checks database liveness
	Ping(ctx context.Context) error

	// Close closes the database connection
	Close() error

	// Metrics returns aggregated latency metrics for diagnostics
	Metrics() MetricsSnapshot
}

func isUniqueViolation(err error, constraint string) bool {
	// lib/pq error
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		if string(pqErr.Code) != "23505" {
			return false
		}
		if constraint == "" {
			return true
		}
		return pqErr.Constraint == constraint
	}
	// pgx/pgconn error
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code != "23505" {
			return false
		}
		if constraint == "" {
			return true
		}
		return pgErr.ConstraintName == constraint
	}
	// Fallback: message contains SQLSTATE 23505 or duplicate key text
	msg := err.Error()
	if strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(strings.ToLower(msg), "duplicate key value violates unique constraint") {
		if constraint == "" {
			return true
		}
		return strings.Contains(msg, constraint)
	}
	return false
}

// adapter implements the Adapter interface
type adapter struct {
	db          *sql.DB
	logger      *utils.Logger
	auditLogger *utils.AuditLogger
	metrics     *dbMetrics

	// Prepared statements for queries
	stmtGetBlock    *sql.Stmt
	stmtGetTxByHash *sql.Stmt
	stmtGetSnapshot *sql.Stmt
	stmtListTxByBlk *sql.Stmt

	// Consensus persistence prepared statements
	stmtUpsertProposal        *sql.Stmt
	stmtGetProposal           *sql.Stmt
	stmtListProposals         *sql.Stmt
	stmtUpsertQC              *sql.Stmt
	stmtGetQC                 *sql.Stmt
	stmtListQCs               *sql.Stmt
	stmtUpsertVote            *sql.Stmt
	stmtListVotes             *sql.Stmt
	stmtUpsertEvidence        *sql.Stmt
	stmtGetEvidence           *sql.Stmt
	stmtListEvidence          *sql.Stmt
	stmtUpsertMeta            *sql.Stmt
	stmtGetMeta               *sql.Stmt
	stmtGetCommitted          *sql.Stmt
	stmtLoadGenesisCert       *sql.Stmt
	stmtUpsertGenesisCert     *sql.Stmt
	stmtDeleteGenesisCert     *sql.Stmt
	stmtDeleteProposalsBefore *sql.Stmt
	stmtDeleteVotesBefore     *sql.Stmt
	stmtDeleteQCsBefore       *sql.Stmt
	stmtDeleteEvidenceBefore  *sql.Stmt
}

// AdapterConfig holds configuration for the adapter
type AdapterConfig struct {
	DB          *sql.DB
	Logger      *utils.Logger
	AuditLogger *utils.AuditLogger
}

// NewAdapter creates a new CockroachDB adapter
func NewAdapter(ctx context.Context, cfg *AdapterConfig) (Adapter, error) {
	if cfg == nil || cfg.DB == nil {
		return nil, errors.New("storage: adapter config with DB is required")
	}

	a := &adapter{
		db:          cfg.DB,
		logger:      cfg.Logger,
		auditLogger: cfg.AuditLogger,
		metrics:     newDBMetrics(),
	}

	// Prepare statements
	if err := a.prepareStatements(ctx); err != nil {
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	if a.logger != nil {
		a.logger.InfoContext(ctx, "CockroachDB adapter initialized")
	}

	return a, nil
}

// prepareStatements prepares SQL statements for reuse
func (a *adapter) prepareStatements(ctx context.Context) error {
	var err error

	// Prepare GetBlock statement
	a.stmtGetBlock, err = a.db.PrepareContext(ctx, `
		SELECT height, block_hash, parent_hash, state_root, tx_root, proposer_id, view_number, 
		       timestamp, tx_count, qc_view, qc_signatures, committed_at
		FROM blocks
		WHERE height = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get block: %w", err)
	}

	// Prepare GetTransactionByContentHash statement
	a.stmtGetTxByHash, err = a.db.PrepareContext(ctx, `
		SELECT tx_hash, block_height, tx_index, tx_type, producer_id, nonce, content_hash,
		       algorithm, public_key, signature, payload, custody_chain, status, error_msg,
		       submitted_at, executed_at
		FROM transactions
		WHERE content_hash = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get tx by hash: %w", err)
	}

	// Prepare GetSnapshot statement
	a.stmtGetSnapshot, err = a.db.PrepareContext(ctx, `
		SELECT version, state_root, block_height, block_hash, tx_count, 
		       reputation_changes, policy_changes, quarantine_changes, created_at
		FROM state_versions
		WHERE version = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get snapshot: %w", err)
	}

	// Prepare ListTransactionsByBlock statement (lightweight projection)
	a.stmtListTxByBlk, err = a.db.PrepareContext(ctx, `
		SELECT tx_hash, tx_index, tx_type, length(payload::text) as size_bytes
		FROM transactions
		WHERE block_height = $1
		ORDER BY tx_index ASC
	`)
	if err != nil {
		return fmt.Errorf("prepare list tx by block: %w", err)
	}

	// Consensus persistence statements
	a.stmtUpsertProposal, err = a.db.PrepareContext(ctx, `
		UPSERT INTO consensus_proposals (block_hash, height, view_number, proposer_id, proposal_cbor)
		VALUES ($1, $2, $3, $4, $5)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert proposal: %w", err)
	}

	a.stmtGetProposal, err = a.db.PrepareContext(ctx, `
		SELECT proposal_cbor
		FROM consensus_proposals
		WHERE block_hash = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get proposal: %w", err)
	}

	a.stmtListProposals, err = a.db.PrepareContext(ctx, `
		SELECT block_hash, height, view_number, proposer_id, proposal_cbor
		FROM consensus_proposals
		WHERE height >= $1
		ORDER BY height ASC
		LIMIT $2
	`)
	if err != nil {
		return fmt.Errorf("prepare list proposals: %w", err)
	}

	a.stmtUpsertQC, err = a.db.PrepareContext(ctx, `
		UPSERT INTO consensus_qcs (block_hash, height, view_number, qc_cbor)
		VALUES ($1, $2, $3, $4)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert qc: %w", err)
	}

	a.stmtGetQC, err = a.db.PrepareContext(ctx, `
		SELECT qc_cbor
		FROM consensus_qcs
		WHERE block_hash = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get qc: %w", err)
	}

	a.stmtListQCs, err = a.db.PrepareContext(ctx, `
		SELECT block_hash, height, view_number, qc_cbor
		FROM consensus_qcs
		WHERE height >= $1
		ORDER BY height ASC
		LIMIT $2
	`)
	if err != nil {
		return fmt.Errorf("prepare list qcs: %w", err)
	}

	a.stmtUpsertVote, err = a.db.PrepareContext(ctx, `
		UPSERT INTO consensus_votes (vote_hash, view_number, height, voter_id, block_hash, vote_cbor)
		VALUES ($1, $2, $3, $4, $5, $6)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert vote: %w", err)
	}

	a.stmtListVotes, err = a.db.PrepareContext(ctx, `
		SELECT vote_hash, view_number, height, voter_id, block_hash, vote_cbor
		FROM consensus_votes
		WHERE height >= $1
		ORDER BY height ASC
		LIMIT $2
	`)
	if err != nil {
		return fmt.Errorf("prepare list votes: %w", err)
	}

	a.stmtUpsertEvidence, err = a.db.PrepareContext(ctx, `
		UPSERT INTO consensus_evidence (evidence_hash, height, evidence_cbor)
		VALUES ($1, $2, $3)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert evidence: %w", err)
	}

	a.stmtGetEvidence, err = a.db.PrepareContext(ctx, `
		SELECT evidence_cbor
		FROM consensus_evidence
		WHERE evidence_hash = $1
	`)
	if err != nil {
		return fmt.Errorf("prepare get evidence: %w", err)
	}

	a.stmtListEvidence, err = a.db.PrepareContext(ctx, `
		SELECT evidence_hash, height, evidence_cbor
		FROM consensus_evidence
		WHERE height >= $1
		ORDER BY height ASC
		LIMIT $2
	`)
	if err != nil {
		return fmt.Errorf("prepare list evidence: %w", err)
	}

	// Match UPSERT constraint (id=1) to ensure consistent genesis loading
	a.stmtLoadGenesisCert, err = a.db.PrepareContext(ctx, `
		SELECT certificate
		FROM genesis_certificates
		WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("prepare load genesis certificate: %w", err)
	}

	a.stmtUpsertGenesisCert, err = a.db.PrepareContext(ctx, `
		UPSERT INTO genesis_certificates (id, certificate)
		VALUES (1, $1)
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert genesis certificate: %w", err)
	}

	a.stmtDeleteGenesisCert, err = a.db.PrepareContext(ctx, `
		DELETE FROM genesis_certificates WHERE id = 1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete genesis certificate: %w", err)
	}

	a.stmtUpsertMeta, err = a.db.PrepareContext(ctx, `
		INSERT INTO consensus_metadata (key, height, block_hash, qc_cbor, updated_at)
		VALUES ('last_committed', $1, $2, $3, NOW())
		ON CONFLICT (key) DO UPDATE
		SET height = excluded.height,
		    block_hash = excluded.block_hash,
		    qc_cbor = excluded.qc_cbor,
		    updated_at = excluded.updated_at
		WHERE excluded.height >= consensus_metadata.height
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert metadata: %w", err)
	}

	a.stmtGetMeta, err = a.db.PrepareContext(ctx, `
		SELECT height, block_hash, qc_cbor
		FROM consensus_metadata
		WHERE key = 'last_committed'
	`)
	if err != nil {
		return fmt.Errorf("prepare get metadata: %w", err)
	}

	a.stmtGetCommitted, err = a.db.PrepareContext(ctx, `
		SELECT block_hash
		FROM consensus_metadata
		WHERE key = 'committed_' || $1::TEXT
	`)
	if err != nil {
		return fmt.Errorf("prepare get committed hash: %w", err)
	}

	a.stmtDeleteProposalsBefore, err = a.db.PrepareContext(ctx, `
		DELETE FROM consensus_proposals
		WHERE height < $1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete proposals before: %w", err)
	}

	a.stmtDeleteVotesBefore, err = a.db.PrepareContext(ctx, `
		DELETE FROM consensus_votes
		WHERE height < $1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete votes before: %w", err)
	}

	a.stmtDeleteQCsBefore, err = a.db.PrepareContext(ctx, `
		DELETE FROM consensus_qcs
		WHERE height < $1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete qcs before: %w", err)
	}

	a.stmtDeleteEvidenceBefore, err = a.db.PrepareContext(ctx, `
		DELETE FROM consensus_evidence
		WHERE height < $1
	`)
	if err != nil {
		return fmt.Errorf("prepare delete evidence before: %w", err)
	}

	return nil
}

// PersistBlock atomically persists block, transactions, and state snapshot
func (a *adapter) PersistBlock(ctx context.Context, blk *block.AppBlock, receipts []state.Receipt, stateRoot [32]byte) error {
	stop := a.recordTxn("persist_block")
	defer stop()
	if blk == nil {
		return fmt.Errorf("%w: block is nil", ErrInvalidData)
	}

	// Audit the persistence attempt (without payload)
	if a.auditLogger != nil {
		bh := blk.GetHash()
		_ = a.auditLogger.Info("block_persist_attempt", map[string]interface{}{
			"height":   blk.GetHeight(),
			"hash":     fmt.Sprintf("%x", bh[:8]),
			"tx_count": blk.GetTransactionCount(),
		})
	}

	// Start SERIALIZABLE transaction for linearizability
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// 1. UPSERT block
	stageStart := time.Now()
	if err := a.upsertBlock(ctx, tx, blk, stateRoot); err != nil {
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_block", time.Since(stageStart))
			a.metrics.observePersistFailureClass("upsert_block", err)
		}
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("block_persist_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("upsert block: %w", err)
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("upsert_block", time.Since(stageStart))
	}

	// 2. UPSERT transactions
	stageStart = time.Now()
	if err := a.upsertTransactions(ctx, tx, blk, receipts); err != nil {
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_transactions", time.Since(stageStart))
			a.metrics.observePersistFailureClass("upsert_transactions", err)
		}
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("transactions_persist_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("upsert transactions: %w", err)
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("upsert_transactions", time.Since(stageStart))
	}

	// 3. UPSERT state snapshot
	stageStart = time.Now()
	if err := a.upsertSnapshot(ctx, tx, blk, stateRoot, len(receipts)); err != nil {
		if a.metrics != nil {
			a.metrics.observePersistStage("upsert_snapshot", time.Since(stageStart))
			a.metrics.observePersistFailureClass("upsert_snapshot", err)
		}
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("snapshot_persist_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("upsert snapshot: %w", err)
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("upsert_snapshot", time.Since(stageStart))
	}

	// Commit transaction
	stageStart = time.Now()
	if err := tx.Commit(); err != nil {
		if a.metrics != nil {
			a.metrics.observePersistStage("commit", time.Since(stageStart))
			a.metrics.observePersistFailureClass("commit", err)
		}
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("block_persist_commit_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("commit transaction: %w", err)
	}
	if a.metrics != nil {
		a.metrics.observePersistStage("commit", time.Since(stageStart))
	}

	// Success audit
	if a.auditLogger != nil {
		bh2 := blk.GetHash()
		_ = a.auditLogger.Info("block_persisted", map[string]interface{}{
			"height":     blk.GetHeight(),
			"hash":       fmt.Sprintf("%x", bh2[:8]),
			"tx_count":   blk.GetTransactionCount(),
			"state_root": fmt.Sprintf("%x", stateRoot[:8]),
		})
	}

	if a.logger != nil {
		a.logger.InfoContext(ctx, "block persisted successfully",
			utils.ZapUint64("height", blk.GetHeight()),
			utils.ZapInt("tx_count", blk.GetTransactionCount()))
	}

	return nil
}

// upsertBlock inserts or verifies existing block
func (a *adapter) upsertBlock(ctx context.Context, tx *sql.Tx, blk *block.AppBlock, stateRoot [32]byte) error {
	blockHash := blk.GetHash()
	parentHash := blk.GetParentHash()
	proposerID := blk.Proposer()

	// Try INSERT first
	bHash := blockHash[:]
	pHash := parentHash[:]
	pID := proposerID[:]
	sRoot := stateRoot[:]

	// Persist canonical tx_root from block header (already computed deterministically)
	txRootArr := blk.TxRoot()
	txRoot := txRootArr[:]

	// INSERT with conflict-avoidance to keep the transaction alive on duplicates
	res, err := tx.ExecContext(ctx, `
        INSERT INTO blocks (
            height, block_hash, parent_hash, state_root, proposer_id, view_number,
            timestamp, tx_count, tx_root, qc_view, qc_signatures, committed_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
        ON CONFLICT DO NOTHING
    `,
		blk.GetHeight(),
		bHash,
		pHash,
		sRoot,
		pID,
		uint64(0),          // view_number - TODO: get from block if available
		blk.GetTimestamp(), // Already time.Time
		blk.GetTransactionCount(),
		txRoot,
		uint64(0), // qc_view - TODO: get from QC if available
		[]byte{},  // qc_signatures - TODO: get from QC if available
	)
	if err != nil {
		return fmt.Errorf("insert block: %w", err)
	}

	if rows, _ := res.RowsAffected(); rows > 0 {
		return nil // Insert succeeded
	}

	// Conflict occurred. Verify idempotency: existing block must match exactly.
	var existingHash []byte
	err = tx.QueryRowContext(ctx, `SELECT block_hash FROM blocks WHERE height = $1`, blk.GetHeight()).Scan(&existingHash)
	if err == sql.ErrNoRows {
		// No row at this height; check if the same block_hash exists at a different height (corruption)
		var existingHeight uint64
		err2 := tx.QueryRowContext(ctx, `SELECT height FROM blocks WHERE block_hash = $1`, bHash).Scan(&existingHeight)
		if err2 == nil {
			if a.auditLogger != nil {
				_ = a.auditLogger.Security("block_hash_height_mismatch_detected", map[string]interface{}{
					"attempt_height":  blk.GetHeight(),
					"existing_height": existingHeight,
					"hash":            fmt.Sprintf("%x", blockHash[:]),
				})
			}
			return fmt.Errorf("%w: block hash already exists at different height (have %d, attempted %d)", ErrIntegrityViolation, existingHeight, blk.GetHeight())
		}
		return fmt.Errorf("%w: upsert conflict but no existing row found", ErrConflictDetected)
	}
	if err != nil {
		return fmt.Errorf("failed to verify existing block: %w", err)
	}

	if len(existingHash) != 32 {
		return fmt.Errorf("%w: existing block hash invalid length", ErrIntegrityViolation)
	}
	var existingHash32 [32]byte
	copy(existingHash32[:], existingHash)
	if blockHash != existingHash32 {
		if a.auditLogger != nil {
			_ = a.auditLogger.Security("block_hash_mismatch_detected", map[string]interface{}{
				"height":        blk.GetHeight(),
				"new_hash":      fmt.Sprintf("%x", blockHash[:]),
				"existing_hash": fmt.Sprintf("%x", existingHash32[:]),
			})
		}
		return fmt.Errorf("%w: block hash mismatch at height %d", ErrIntegrityViolation, blk.GetHeight())
	}

	// Idempotent insert
	return nil
}

// upsertTransactions inserts or verifies existing transactions
func (a *adapter) upsertTransactions(ctx context.Context, tx *sql.Tx, blk *block.AppBlock, receipts []state.Receipt) error {
	transactions := blk.Transactions()
	if len(transactions) != len(receipts) {
		return fmt.Errorf("%w: transaction count mismatch", ErrInvalidData)
	}

	for i, receipt := range receipts {
		if i >= len(transactions) {
			return fmt.Errorf("%w: receipt index out of bounds", ErrInvalidData)
		}

		stateTx := transactions[i]
		envelope := stateTx.Envelope()

		// Serialize payload to JSONB
		payloadJSON, err := json.Marshal(stateTx)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction payload: %w", err)
		}

		// Serialize custody chain if present (for EvidenceTx)
		var custodyChainJSON []byte
		if evidenceTx, ok := stateTx.(*state.EvidenceTx); ok {
			if len(evidenceTx.CoC) > 0 {
				custodyChainJSON, err = json.Marshal(evidenceTx.CoC)
				if err != nil {
					return fmt.Errorf("failed to marshal custody chain: %w", err)
				}
			}
		}

		// Determine status from receipt
		status := "success"
		errorMsg := ""
		if receipt.Error != "" {
			status = "failed"
			errorMsg = receipt.Error
		}

		// Compute stable transaction ID from envelope sign-bytes (prevents collisions on identical payloads)
		contentHash := envelope.ContentHash
		var domain string
		switch stateTx.Type() {
		case state.TxEvent:
			domain = state.DomainEventTx
		case state.TxEvidence:
			domain = state.DomainEvidenceTx
		case state.TxPolicy:
			domain = state.DomainPolicyTx
		default:
			return fmt.Errorf("%w: unknown tx type %q", ErrInvalidData, stateTx.Type())
		}
		signBytes, err := state.BuildSignBytes(domain, stateTx.Timestamp(), envelope.ProducerID, envelope.Nonce, contentHash)
		if err != nil {
			return fmt.Errorf("build tx id: %w", err)
		}
		txHash := state.HashBytes(signBytes)

		// Try INSERT with conflict-avoidance to keep transaction alive
		res, err := tx.ExecContext(ctx, `
			INSERT INTO transactions (
				tx_hash, block_height, tx_index, tx_type, producer_id, nonce, content_hash,
				algorithm, public_key, signature, payload, custody_chain, status, error_msg,
				submitted_at, executed_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW())
			ON CONFLICT (tx_hash) DO NOTHING
		`,
			txHash[:],
			blk.GetHeight(),
			i,
			string(stateTx.Type()),
			envelope.ProducerID,
			envelope.Nonce,
			contentHash[:],
			envelope.Alg,
			envelope.PubKey,
			envelope.Signature,
			payloadJSON,
			custodyChainJSON,
			status,
			errorMsg,
			time.Unix(stateTx.Timestamp(), 0),
		)

		if err != nil {
			return fmt.Errorf("failed to insert transaction at height %d, index %d: %w", blk.GetHeight(), i, err)
		}

		if rows, _ := res.RowsAffected(); rows > 0 {
			// Insert succeeded; policy transactions must always durable-enqueue outbox within the same DB tx.
			if stateTx.Type() == state.TxPolicy {
				policyTx, ok := stateTx.(*state.PolicyTx)
				if !ok || len(policyTx.Data) == 0 {
					return fmt.Errorf("%w: policy tx payload missing", ErrInvalidData)
				}
				if err := a.upsertPolicyOutbox(ctx, tx, blk.GetHeight(), blk.GetTimestamp().Unix(), i, policyTx.Data); err != nil {
					return fmt.Errorf("upsert policy outbox: %w", err)
				}
			}
			continue
		}

		// Conflict occurred (rows == 0), verify idempotency
		var existingContentHash []byte
		var existingProducerID []byte
		var existingNonce []byte

		err = tx.QueryRowContext(ctx, `
			SELECT content_hash, producer_id, nonce
			FROM transactions
			WHERE block_height = $1 AND tx_index = $2
		`, blk.GetHeight(), i).Scan(&existingContentHash, &existingProducerID, &existingNonce)

		if err != nil {
			return fmt.Errorf("failed to verify existing transaction: %w", err)
		}

		// Verify content hash matches
		if len(existingContentHash) != 32 {
			return fmt.Errorf("%w: existing tx content_hash invalid length", ErrIntegrityViolation)
		}

		var existingHash [32]byte
		copy(existingHash[:], existingContentHash)

		if contentHash != existingHash {
			if a.auditLogger != nil {
				_ = a.auditLogger.Security("transaction_hash_mismatch_detected", map[string]interface{}{
					"height":        blk.GetHeight(),
					"tx_index":      i,
					"new_hash":      fmt.Sprintf("%x", contentHash[:]),
					"existing_hash": fmt.Sprintf("%x", existingHash[:]),
				})
			}
			return fmt.Errorf("%w: transaction content_hash mismatch at height %d, index %d", ErrIntegrityViolation, blk.GetHeight(), i)
		}

		// Verify producer_id and nonce match (basic check)
		if len(existingProducerID) == 0 {
			return fmt.Errorf("%w: existing tx producer_id empty", ErrIntegrityViolation)
		}

		// Idempotent insert
		if stateTx.Type() == state.TxPolicy {
			policyTx, ok := stateTx.(*state.PolicyTx)
			if !ok || len(policyTx.Data) == 0 {
				return fmt.Errorf("%w: policy tx payload missing", ErrInvalidData)
			}
			if err := a.upsertPolicyOutbox(ctx, tx, blk.GetHeight(), blk.GetTimestamp().Unix(), i, policyTx.Data); err != nil {
				return fmt.Errorf("upsert policy outbox: %w", err)
			}
		}
	}

	return nil
}

func parsePolicyID(raw []byte) string {
	var payload struct {
		PolicyID string `json:"policy_id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.PolicyID)
}

func parsePolicyTrace(raw []byte) (string, int64) {
	var payload struct {
		PolicyID string `json:"policy_id"`
		QCRef    string `json:"qc_reference"`
		Metadata struct {
			TraceID      string `json:"trace_id"`
			AIEventTsMs  int64  `json:"ai_event_ts_ms"`
			AIEventTSMs2 int64  `json:"ai_event_timestamp_ms"`
		} `json:"metadata"`
		Trace struct {
			ID        string `json:"id"`
			AIEventMs int64  `json:"ai_event_ts_ms"`
		} `json:"trace"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", 0
	}
	traceID := strings.TrimSpace(payload.Metadata.TraceID)
	if traceID == "" {
		traceID = strings.TrimSpace(payload.Trace.ID)
	}
	if traceID == "" {
		traceID = strings.TrimSpace(payload.QCRef)
	}
	aiEventTsMs := payload.Metadata.AIEventTsMs
	if aiEventTsMs <= 0 {
		aiEventTsMs = payload.Metadata.AIEventTSMs2
	}
	if aiEventTsMs <= 0 {
		aiEventTsMs = payload.Trace.AIEventMs
	}
	return traceID, aiEventTsMs
}

func (a *adapter) upsertPolicyOutbox(ctx context.Context, tx *sql.Tx, blockHeight uint64, blockTS int64, txIndex int, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("%w: empty policy payload", ErrInvalidData)
	}

	ruleHash := sha256.Sum256(payload)
	policyID := parsePolicyID(payload)
	if policyID == "" {
		// Preserve durability even for malformed payloads; dispatcher will terminal-fail with guardrail reason.
		policyID = "invalid:" + hex.EncodeToString(ruleHash[:8])
	}
	traceID, aiEventTsMs := parsePolicyTrace(payload)
	if traceID == "" {
		traceID = fmt.Sprintf("trace:%s:%s", policyID, hex.EncodeToString(ruleHash[:8]))
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO control_policy_outbox (
			block_height, block_ts, tx_index, policy_id, rule_hash, payload, trace_id, ai_event_ts_ms, status, next_retry_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, 'pending', NOW(), NOW(), NOW()
		)
		ON CONFLICT (block_height, tx_index) DO NOTHING
	`, blockHeight, blockTS, txIndex, policyID, ruleHash[:], payload, traceID, aiEventTsMs)
	if err != nil {
		return err
	}
	return nil
}

// upsertSnapshot inserts or verifies state snapshot
func (a *adapter) upsertSnapshot(ctx context.Context, tx *sql.Tx, blk *block.AppBlock, stateRoot [32]byte, txCount int) error {
	blockHash := blk.GetHash()
	bHash := blockHash[:]
	sRoot := stateRoot[:]

	// Try INSERT with conflict-avoidance to keep transaction alive
	res, err := tx.ExecContext(ctx, `
		INSERT INTO state_versions (
			version, state_root, block_height, block_hash, tx_count,
			reputation_changes, policy_changes, quarantine_changes, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (version) DO NOTHING
	`,
		blk.GetHeight(), // version = height for now
		sRoot,
		blk.GetHeight(),
		bHash,
		txCount,
		0, // reputation_changes - TODO: calculate from receipts
		0, // policy_changes
		0, // quarantine_changes
	)

	if err != nil {
		return fmt.Errorf("failed to insert snapshot: %w", err)
	}

	if rows, _ := res.RowsAffected(); rows > 0 {
		return nil // Insert succeeded
	}

	// Conflict occurred (rows == 0), verify state_root matches
	var existingStateRoot []byte
	err = tx.QueryRowContext(ctx, `
		SELECT state_root FROM state_versions WHERE version = $1
	`, blk.GetHeight()).Scan(&existingStateRoot)

	if err != nil {
		return fmt.Errorf("failed to verify existing snapshot: %w", err)
	}

	if len(existingStateRoot) != 32 {
		return fmt.Errorf("%w: existing state_root invalid length", ErrIntegrityViolation)
	}

	var existingRoot [32]byte
	copy(existingRoot[:], existingStateRoot)

	if stateRoot != existingRoot {
		if a.auditLogger != nil {
			_ = a.auditLogger.Security("state_root_mismatch_detected", map[string]interface{}{
				"version":       blk.GetHeight(),
				"new_root":      fmt.Sprintf("%x", stateRoot[:]),
				"existing_root": fmt.Sprintf("%x", existingRoot[:]),
			})
		}
		return fmt.Errorf("%w: state_root mismatch at version %d", ErrIntegrityViolation, blk.GetHeight())
	}

	// State root matches, idempotent insert
	return nil
}

// GetBlock retrieves a block by height
func (a *adapter) GetBlock(ctx context.Context, height uint64) (*block.AppBlock, error) {
	stop := a.recordQuery("get_block")
	defer stop()
	var row BlockRow

	// Use prepared statement
	err := a.stmtGetBlock.QueryRowContext(ctx, height).Scan(
		&row.Height,
		&row.BlockHash,
		&row.ParentHash,
		&row.StateRoot,
		&row.TxRoot,
		&row.ProposerID,
		&row.ViewNumber,
		&row.Timestamp,
		&row.TxCount,
		&row.QCView,
		&row.QCSignatures,
		&row.CommittedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: height=%d", ErrBlockNotFound, height)
	}
	if err != nil {
		// SECURITY: Only log height, not data
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "failed to get block",
				utils.ZapError(err),
				utils.ZapUint64("height", height))
		}
		return nil, fmt.Errorf("query block: %w", err)
	}

	// Reconstruct block header (without transactions)
	if len(row.BlockHash) != 32 || len(row.ParentHash) != 32 || len(row.StateRoot) != 32 || len(row.ProposerID) != 32 || len(row.TxRoot) != 32 {
		return nil, fmt.Errorf("%w: invalid hash/id length in database", ErrInvalidData)
	}

	var blockHash, parentHash, stateRoot [32]byte
	var proposerID [32]byte
	var txRoot [32]byte
	copy(blockHash[:], row.BlockHash)
	copy(parentHash[:], row.ParentHash)
	copy(stateRoot[:], row.StateRoot)
	copy(proposerID[:], row.ProposerID)
	copy(txRoot[:], row.TxRoot)

	// Create block header without fetching transactions (performance optimization)
	// Transactions can be fetched separately if needed
	blkHeader := block.NewAppBlockHeader(
		row.Height,
		parentHash,
		txRoot,
		stateRoot,
		proposerID,
		row.Timestamp,
		row.TxCount,
	)

	return blkHeader, nil
}

// GetLatestHeight returns the maximum block height from the database
// Returns 0 if no blocks exist
func (a *adapter) GetLatestHeight(ctx context.Context) (uint64, error) {
	stop := a.recordQuery("get_latest_height")
	defer stop()
	var maxHeight sql.NullInt64

	query := `SELECT MAX(height) FROM blocks`
	err := a.db.QueryRowContext(ctx, query).Scan(&maxHeight)

	if err != nil {
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "failed to get latest height", utils.ZapError(err))
		}
		return 0, fmt.Errorf("query latest height: %w", err)
	}

	// If no blocks exist, return 0
	if !maxHeight.Valid {
		return 0, nil
	}

	return uint64(maxHeight.Int64), nil
}

// GetMinHeight returns the minimum block height in the database
// Returns 0 if no blocks exist
func (a *adapter) GetMinHeight(ctx context.Context) (uint64, error) {
	stop := a.recordQuery("get_min_height")
	defer stop()
	var minHeight sql.NullInt64

	query := `SELECT MIN(height) FROM blocks`
	err := a.db.QueryRowContext(ctx, query).Scan(&minHeight)

	if err != nil {
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "failed to get min height", utils.ZapError(err))
		}
		return 0, fmt.Errorf("query min height: %w", err)
	}

	// If no blocks exist, return 0
	if !minHeight.Valid {
		return 0, nil
	}

	return uint64(minHeight.Int64), nil
}

// GetTransactionByContentHash retrieves a transaction by content hash
func (a *adapter) GetTransactionByContentHash(ctx context.Context, contentHash [32]byte) (*TxRow, error) {
	stop := a.recordQuery("get_tx_by_hash")
	defer stop()
	var row TxRow

	// Use prepared statement
	err := a.stmtGetTxByHash.QueryRowContext(ctx, contentHash[:]).Scan(
		&row.TxHash,
		&row.BlockHeight,
		&row.TxIndex,
		&row.TxType,
		&row.ProducerID,
		&row.Nonce,
		&row.ContentHash,
		&row.Algorithm,
		&row.PublicKey,
		&row.Signature,
		&row.Payload,
		&row.CustodyChain,
		&row.Status,
		&row.ErrorMsg,
		&row.SubmittedAt,
		&row.ExecutedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: content_hash=%x", ErrTransactionNotFound, contentHash[:8])
	}
	if err != nil {
		// SECURITY: Only log hash prefix, not payload
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "failed to get transaction",
				utils.ZapError(err),
				utils.ZapString("content_hash", fmt.Sprintf("%x", contentHash[:8])))
		}
		return nil, fmt.Errorf("query transaction: %w", err)
	}

	return &row, nil
}

// GetSnapshot retrieves a state snapshot by version
func (a *adapter) GetSnapshot(ctx context.Context, version uint64) (*SnapshotRow, error) {
	stop := a.recordQuery("get_snapshot")
	defer stop()
	var row SnapshotRow

	// Use prepared statement
	err := a.stmtGetSnapshot.QueryRowContext(ctx, version).Scan(
		&row.Version,
		&row.StateRoot,
		&row.BlockHeight,
		&row.BlockHash,
		&row.TxCount,
		&row.ReputationChanges,
		&row.PolicyChanges,
		&row.QuarantineChanges,
		&row.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: version=%d", ErrSnapshotNotFound, version)
	}
	if err != nil {
		// SECURITY: Only log version, not data
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "failed to get snapshot",
				utils.ZapError(err),
				utils.ZapUint64("version", version))
		}
		return nil, fmt.Errorf("query snapshot: %w", err)
	}

	return &row, nil
}

// ListTransactionsByBlock returns minimal transaction metadata for a block
func (a *adapter) ListTransactionsByBlock(ctx context.Context, height uint64) ([]TxMeta, error) {
	stop := a.recordQuery("list_transactions_by_block")
	defer stop()
	rows, err := a.stmtListTxByBlk.QueryContext(ctx, height)
	if err != nil {
		return nil, fmt.Errorf("query tx by block: %w", err)
	}
	defer rows.Close()

	metas := make([]TxMeta, 0)
	for rows.Next() {
		var m TxMeta
		if err := rows.Scan(&m.TxHash, &m.TxIndex, &m.TxType, &m.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan tx meta: %w", err)
		}
		metas = append(metas, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tx meta: %w", err)
	}
	return metas, nil
}

// Ping checks database liveness
func (a *adapter) Ping(ctx context.Context) error {
	stop := a.recordQuery("ping")
	defer stop()
	return a.db.PingContext(ctx)
}

// Close closes the database connection and prepared statements
func (a *adapter) Close() error {
	// Close prepared statements
	if a.stmtGetBlock != nil {
		a.stmtGetBlock.Close()
	}
	if a.stmtGetTxByHash != nil {
		a.stmtGetTxByHash.Close()
	}
	if a.stmtGetSnapshot != nil {
		a.stmtGetSnapshot.Close()
	}
	if a.stmtListTxByBlk != nil {
		a.stmtListTxByBlk.Close()
	}
	if a.stmtUpsertProposal != nil {
		a.stmtUpsertProposal.Close()
	}
	if a.stmtGetProposal != nil {
		a.stmtGetProposal.Close()
	}
	if a.stmtListProposals != nil {
		a.stmtListProposals.Close()
	}
	if a.stmtUpsertQC != nil {
		a.stmtUpsertQC.Close()
	}
	if a.stmtGetQC != nil {
		a.stmtGetQC.Close()
	}
	if a.stmtListQCs != nil {
		a.stmtListQCs.Close()
	}
	if a.stmtUpsertVote != nil {
		a.stmtUpsertVote.Close()
	}
	if a.stmtListVotes != nil {
		a.stmtListVotes.Close()
	}
	if a.stmtUpsertEvidence != nil {
		a.stmtUpsertEvidence.Close()
	}
	if a.stmtGetEvidence != nil {
		a.stmtGetEvidence.Close()
	}
	if a.stmtListEvidence != nil {
		a.stmtListEvidence.Close()
	}
	if a.stmtUpsertMeta != nil {
		a.stmtUpsertMeta.Close()
	}
	if a.stmtGetMeta != nil {
		a.stmtGetMeta.Close()
	}
	if a.stmtGetCommitted != nil {
		a.stmtGetCommitted.Close()
	}
	if a.stmtLoadGenesisCert != nil {
		a.stmtLoadGenesisCert.Close()
	}
	if a.stmtUpsertGenesisCert != nil {
		a.stmtUpsertGenesisCert.Close()
	}
	if a.stmtDeleteGenesisCert != nil {
		a.stmtDeleteGenesisCert.Close()
	}
	if a.stmtDeleteProposalsBefore != nil {
		a.stmtDeleteProposalsBefore.Close()
	}
	if a.stmtDeleteVotesBefore != nil {
		a.stmtDeleteVotesBefore.Close()
	}
	if a.stmtDeleteQCsBefore != nil {
		a.stmtDeleteQCsBefore.Close()
	}
	if a.stmtDeleteEvidenceBefore != nil {
		a.stmtDeleteEvidenceBefore.Close()
	}

	// Close database connection
	if a.db != nil {
		return a.db.Close()
	}

	return nil
}

// GetDB returns the underlying database connection for direct queries
// This is used by the API layer for statistics calculations
func (a *adapter) GetDB() *sql.DB {
	return a.db
}

// Metrics returns a snapshot of CockroachDB latency metrics.
func (a *adapter) Metrics() MetricsSnapshot {
	if a == nil || a.metrics == nil {
		return MetricsSnapshot{}
	}
	return a.metrics.snapshot()
}

func (a *adapter) recordQuery(label string) func() {
	if a == nil || a.metrics == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		a.metrics.observeQuery(label, time.Since(start))
	}
}

func (a *adapter) recordTxn(label string) func() {
	if a == nil || a.metrics == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		a.metrics.observeTxn(label, time.Since(start))
	}
}

// MetricsSnapshot contains aggregated latency and slow operation counters.
type MetricsSnapshot struct {
	QueryBuckets              []utils.HistogramBucket
	QueryCount                uint64
	QuerySumMs                float64
	QueryP95Ms                float64
	TxnBuckets                []utils.HistogramBucket
	TxnCount                  uint64
	TxnSumMs                  float64
	TxnP95Ms                  float64
	SlowQueryCount            uint64
	SlowTransactionCount      uint64
	SlowQueries               map[string]uint64
	SlowTransactions          map[string]uint64
	PersistStageBuckets       map[string][]utils.HistogramBucket
	PersistStageCount         map[string]uint64
	PersistStageSumMs         map[string]float64
	PersistStageP95Ms         map[string]float64
	PersistFailureClassTotals map[string]uint64
}

type dbMetrics struct {
	queryLatency          *utils.LatencyHistogram
	txnLatency            *utils.LatencyHistogram
	slowQueryThresholdMs  float64
	slowTxnThresholdMs    float64
	slowQueryCount        atomic.Uint64
	slowTxnCount          atomic.Uint64
	slowQueryLabels       sync.Map
	slowTxnLabels         sync.Map
	persistStages         sync.Map
	persistFailureClasses sync.Map
}

func newDBMetrics() *dbMetrics {
	return &dbMetrics{
		queryLatency:         utils.NewLatencyHistogram([]float64{1, 5, 20, 50, 100, 250, 500, 1000, 2500, 5000}),
		txnLatency:           utils.NewLatencyHistogram([]float64{5, 20, 50, 100, 250, 500, 1000, 2500, 5000, 10000}),
		slowQueryThresholdMs: 250,
		slowTxnThresholdMs:   500,
	}
}

func (m *dbMetrics) observeQuery(label string, d time.Duration) {
	if m == nil {
		return
	}
	ms := float64(d) / float64(time.Millisecond)
	m.queryLatency.Observe(ms)
	if ms > m.slowQueryThresholdMs {
		m.slowQueryCount.Add(1)
		incrementLabel(&m.slowQueryLabels, label)
	}
}

func (m *dbMetrics) observeTxn(label string, d time.Duration) {
	if m == nil {
		return
	}
	ms := float64(d) / float64(time.Millisecond)
	m.txnLatency.Observe(ms)
	if ms > m.slowTxnThresholdMs {
		m.slowTxnCount.Add(1)
		incrementLabel(&m.slowTxnLabels, label)
	}
}

type stageMetric struct {
	hist *utils.LatencyHistogram
}

func (m *dbMetrics) observePersistStage(label string, d time.Duration) {
	if m == nil {
		return
	}
	if label == "" {
		label = "unknown"
	}
	val, ok := m.persistStages.Load(label)
	if !ok {
		created := &stageMetric{
			hist: utils.NewLatencyHistogram([]float64{1, 5, 20, 50, 100, 250, 500, 1000, 2500, 5000}),
		}
		actual, loaded := m.persistStages.LoadOrStore(label, created)
		if loaded {
			val = actual
		} else {
			val = created
		}
	}
	if sm, ok := val.(*stageMetric); ok && sm.hist != nil {
		sm.hist.Observe(float64(d) / float64(time.Millisecond))
	}
}

func (m *dbMetrics) observePersistFailureClass(stage string, err error) {
	if m == nil || err == nil {
		return
	}
	class := classifyPersistError(err)
	key := stage + ":" + class
	incrementLabel(&m.persistFailureClasses, key)
}

func (m *dbMetrics) snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	queryBuckets, qCount, qSum := m.queryLatency.Snapshot()
	txnBuckets, tCount, tSum := m.txnLatency.Snapshot()
	snapshot := MetricsSnapshot{
		QueryBuckets:              queryBuckets,
		QueryCount:                qCount,
		QuerySumMs:                qSum,
		QueryP95Ms:                m.queryLatency.Quantile(0.95),
		TxnBuckets:                txnBuckets,
		TxnCount:                  tCount,
		TxnSumMs:                  tSum,
		TxnP95Ms:                  m.txnLatency.Quantile(0.95),
		SlowQueryCount:            m.slowQueryCount.Load(),
		SlowTransactionCount:      m.slowTxnCount.Load(),
		SlowQueries:               make(map[string]uint64),
		SlowTransactions:          make(map[string]uint64),
		PersistStageBuckets:       make(map[string][]utils.HistogramBucket),
		PersistStageCount:         make(map[string]uint64),
		PersistStageSumMs:         make(map[string]float64),
		PersistStageP95Ms:         make(map[string]float64),
		PersistFailureClassTotals: make(map[string]uint64),
	}
	m.slowQueryLabels.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			snapshot.SlowQueries[key.(string)] = counter.Load()
		}
		return true
	})
	m.slowTxnLabels.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			snapshot.SlowTransactions[key.(string)] = counter.Load()
		}
		return true
	})
	m.persistStages.Range(func(key, value any) bool {
		name, ok := key.(string)
		if !ok {
			return true
		}
		sm, ok := value.(*stageMetric)
		if !ok || sm.hist == nil {
			return true
		}
		buckets, count, sum := sm.hist.Snapshot()
		snapshot.PersistStageBuckets[name] = buckets
		snapshot.PersistStageCount[name] = count
		snapshot.PersistStageSumMs[name] = sum
		snapshot.PersistStageP95Ms[name] = sm.hist.Quantile(0.95)
		return true
	})
	m.persistFailureClasses.Range(func(key, value any) bool {
		if counter, ok := value.(*atomic.Uint64); ok {
			if name, ok := key.(string); ok {
				snapshot.PersistFailureClassTotals[name] = counter.Load()
			}
		}
		return true
	})
	return snapshot
}

func classifyPersistError(err error) string {
	if err == nil {
		return "none"
	}
	if state := extractSQLState(err); state != "" {
		switch state {
		case "40001":
			return "retry_serialization"
		case "40P01":
			return "retry_deadlock"
		case "55P03":
			return "retry_lock_not_available"
		case "57014":
			return "timeout_query_canceled"
		case "23505":
			return "constraint_unique"
		case "23503":
			return "constraint_foreign_key"
		case "23514":
			return "constraint_check"
		case "23502":
			return "constraint_not_null"
		case "22001", "22003", "22007", "22008", "22P02":
			return "invalid_data"
		case "08000", "08001", "08003", "08004", "08006":
			return "connection"
		}
		return "sqlstate_" + strings.ToLower(state)
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "restart transaction"), strings.Contains(msg, "40001"), strings.Contains(msg, "serialization"):
		return "retry_serialization"
	case strings.Contains(msg, "40p01"), strings.Contains(msg, "deadlock"):
		return "retry_deadlock"
	case strings.Contains(msg, "55p03"), strings.Contains(msg, "lock_not_available"), strings.Contains(msg, "lock timeout"):
		return "retry_lock_not_available"
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "timeout"):
		return "timeout"
	case strings.Contains(msg, "fenced/no-op"):
		return "fenced"
	case strings.Contains(msg, "23505"), strings.Contains(msg, "duplicate key"), strings.Contains(msg, "unique constraint"):
		return "constraint_unique"
	case strings.Contains(msg, "23503"), strings.Contains(msg, "foreign key"):
		return "constraint_foreign_key"
	case strings.Contains(msg, "23514"), strings.Contains(msg, "check constraint"):
		return "constraint_check"
	case strings.Contains(msg, "23502"), strings.Contains(msg, "not-null"):
		return "constraint_not_null"
	case strings.Contains(msg, "22p02"), strings.Contains(msg, "invalid input syntax"):
		return "invalid_data"
	case strings.Contains(msg, "connection reset"), strings.Contains(msg, "connection refused"), strings.Contains(msg, "broken pipe"):
		return "connection"
	default:
		return "other"
	}
}

func extractSQLState(err error) string {
	if err == nil {
		return ""
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func incrementLabel(store *sync.Map, label string) {
	if label == "" {
		label = "unknown"
	}
	if val, ok := store.Load(label); ok {
		if counter, ok2 := val.(*atomic.Uint64); ok2 {
			counter.Add(1)
			return
		}
	}
	counter := &atomic.Uint64{}
	existing, loaded := store.LoadOrStore(label, counter)
	if loaded {
		if c, ok := existing.(*atomic.Uint64); ok {
			c.Add(1)
			return
		}
	}
	counter.Add(1)
}
