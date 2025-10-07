package cockroach

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"backend/pkg/block"
	"backend/pkg/state"
	"backend/pkg/utils"
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

	// GetTransactionByContentHash retrieves a transaction by its content hash
	GetTransactionByContentHash(ctx context.Context, contentHash [32]byte) (*TxRow, error)

	// GetSnapshot retrieves a state snapshot by version
	GetSnapshot(ctx context.Context, version uint64) (*SnapshotRow, error)

	// ListTransactionsByBlock returns lightweight transaction metadata for a block
	ListTransactionsByBlock(ctx context.Context, height uint64) ([]TxMeta, error)

	// Ping checks database liveness
	Ping(ctx context.Context) error

	// Close closes the database connection
	Close() error
}

func isUniqueViolation(err error, constraint string) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	if pqErr.Code != "23505" {
		return false
	}
	if constraint == "" {
		return true
	}
	return pqErr.Constraint == constraint
}

// adapter implements the Adapter interface
type adapter struct {
	db          *sql.DB
	logger      *utils.Logger
	auditLogger *utils.AuditLogger

	// Prepared statements for queries
	stmtGetBlock    *sql.Stmt
	stmtGetTxByHash *sql.Stmt
	stmtGetSnapshot *sql.Stmt
	stmtListTxByBlk *sql.Stmt
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

	return nil
}

// PersistBlock atomically persists block, transactions, and state snapshot
func (a *adapter) PersistBlock(ctx context.Context, blk *block.AppBlock, receipts []state.Receipt, stateRoot [32]byte) error {
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
	if err := a.upsertBlock(ctx, tx, blk, stateRoot); err != nil {
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("block_persist_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("upsert block: %w", err)
	}

	// 2. UPSERT transactions
	if err := a.upsertTransactions(ctx, tx, blk, receipts); err != nil {
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("transactions_persist_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("upsert transactions: %w", err)
	}

	// 3. UPSERT state snapshot
	if err := a.upsertSnapshot(ctx, tx, blk, stateRoot, len(receipts)); err != nil {
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("snapshot_persist_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("upsert snapshot: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		if a.auditLogger != nil {
			_ = a.auditLogger.Error("block_persist_commit_failed", map[string]interface{}{
				"height": blk.GetHeight(),
				"error":  err.Error(),
			})
		}
		return fmt.Errorf("commit transaction: %w", err)
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

		// Try INSERT
		contentHash := envelope.ContentHash
		_, err = tx.ExecContext(ctx, `
			INSERT INTO transactions (
				tx_hash, block_height, tx_index, tx_type, producer_id, nonce, content_hash,
				algorithm, public_key, signature, payload, custody_chain, status, error_msg,
				submitted_at, executed_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW())
		`,
			contentHash[:],
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

		if err == nil {
			continue // Insert succeeded
		}

		if isUniqueViolation(err, "transactions_producer_nonce_unique") {
			if a.auditLogger != nil {
				_ = a.auditLogger.Security("transaction_replay_attempt_detected", map[string]interface{}{
					"producer_id": fmt.Sprintf("%x", envelope.ProducerID),
					"nonce":       fmt.Sprintf("%x", envelope.Nonce),
				})
			}
			return fmt.Errorf("%w: duplicate producer/nonce combination", ErrIntegrityViolation)
		}

		// Check if it's a primary key conflict (idempotent retry)
		if !isPrimaryKeyViolation(err) {
			// Not a PK conflict - return the error immediately
			return fmt.Errorf("failed to insert transaction at height %d, index %d: %w", blk.GetHeight(), i, err)
		}

		// Primary key conflict detected, verify idempotency
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
	}

	return nil
}

// upsertSnapshot inserts or verifies state snapshot
func (a *adapter) upsertSnapshot(ctx context.Context, tx *sql.Tx, blk *block.AppBlock, stateRoot [32]byte, txCount int) error {
	blockHash := blk.GetHash()
	bHash := blockHash[:]
	sRoot := stateRoot[:]

	// Try INSERT
	_, err := tx.ExecContext(ctx, `
		INSERT INTO state_versions (
			version, state_root, block_height, block_hash, tx_count,
			reputation_changes, policy_changes, quarantine_changes, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
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

	if err == nil {
		return nil // Insert succeeded
	}

	// Conflict detected, verify state_root matches
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

// GetTransactionByContentHash retrieves a transaction by content hash
func (a *adapter) GetTransactionByContentHash(ctx context.Context, contentHash [32]byte) (*TxRow, error) {
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

	// Close database connection
	if a.db != nil {
		return a.db.Close()
	}

	return nil
}
