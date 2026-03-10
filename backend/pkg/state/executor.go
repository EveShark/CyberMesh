package state

import (
	"errors"
	"time"
)

// Block-level limits (defense-in-depth; proposer should respect tighter policies)
const (
	MaxBlockTxs   = 5000
	MaxBlockBytes = 16 << 20 // 16 MiB aggregate payload
)

var (
	ErrBlockTooLarge = errors.New("block exceeds limits")
	ErrNonceReplay   = errors.New("nonce replay detected")
)

type ExecCode int

const (
	ExecCodeOK ExecCode = iota
	ExecCodeInvalid
	ExecCodeReducerError
)

type Receipt struct {
	Index       int
	Type        TxType
	ContentHash [32]byte
	Code        ExecCode
	Error       string
}

// ApplyBlock validates and applies a list of transactions atomically to the latest version of the store.
// On success, it commits version latest+1 and returns the new version, merkle root, and receipts for each tx.
// On any failure, no changes are committed and an error is returned (fail-closed).
func ApplyBlock(store StateStore, now time.Time, skew time.Duration, txs []Transaction) (newVersion uint64, root [32]byte, receipts []Receipt, err error) {
	start := time.Now()
	metrics := ApplyBlockMetrics{}
	var zeroRoot [32]byte
	receipts = make([]Receipt, len(txs))
	defer func() {
		metrics.Total = time.Since(start)
		reportApplyBlockMetrics(metrics)
	}()

	// Block-level bounds
	if len(txs) > MaxBlockTxs {
		return 0, zeroRoot, receipts, ErrBlockTooLarge
	}
	var aggBytes int
	for _, tx := range txs {
		aggBytes += len(tx.Payload())
	}
	if aggBytes > MaxBlockBytes {
		return 0, zeroRoot, receipts, ErrBlockTooLarge
	}

	// Begin transactional mutation on latest version
	latest := store.Latest()
	txn, err := store.Begin(latest)
	if err != nil {
		return 0, zeroRoot, receipts, err
	}

	// Validate and apply sequentially
	for i, tx := range txs {
		r := Receipt{Index: i, Type: tx.Type()}
		validateStart := time.Now()
		// Pre-compute content hash if available via envelope; fall back to hashing payload
		if env := tx.Envelope(); env != nil {
			r.ContentHash = env.ContentHash
		} else {
			if h, err := tx.Hash(); err == nil {
				r.ContentHash = h
			}
		}

		if err := tx.Validate(now, skew); err != nil {
			metrics.Validate += time.Since(validateStart)
			r.Code, r.Error = ExecCodeInvalid, err.Error()
			receipts[i] = r
			return 0, zeroRoot, receipts, err
		}
		metrics.Validate += time.Since(validateStart)

		// Enforce nonce uniqueness at execution time (defense-in-depth)
		if env := tx.Envelope(); env != nil {
			nonceStart := time.Now()
			if v, ok := txn.Get(keyNonce(env.ProducerID, env.Nonce)); ok && len(v) > 0 {
				metrics.NonceCheck += time.Since(nonceStart)
				// Check if this is a replay of an already-committed transaction
				var existingHash [32]byte
				copy(existingHash[:], v)
				if existingHash == env.ContentHash {
					// Transaction already applied - skip (idempotent replay)
					r.Code = ExecCodeOK
					receipts[i] = r
					continue
				}
				r.Code, r.Error = ExecCodeInvalid, ErrNonceReplay.Error()
				receipts[i] = r
				return 0, zeroRoot, receipts, ErrNonceReplay
			}
			metrics.NonceCheck += time.Since(nonceStart)
		}

		// Apply deterministic reducer
		switch tx.Type() {
		case TxEvent:
			metrics.EventTxs++
			reducerStart := time.Now()
			if err := ApplyEvent(tx.(*EventTx), txn); err != nil {
				metrics.ReducerEvent += time.Since(reducerStart)
				r.Code, r.Error = ExecCodeReducerError, err.Error()
				receipts[i] = r
				return 0, zeroRoot, receipts, err
			}
			metrics.ReducerEvent += time.Since(reducerStart)
		case TxEvidence:
			metrics.EvidenceTxs++
			reducerStart := time.Now()
			if err := ApplyEvidence(tx.(*EvidenceTx), txn); err != nil {
				metrics.ReducerEvidence += time.Since(reducerStart)
				r.Code, r.Error = ExecCodeReducerError, err.Error()
				receipts[i] = r
				return 0, zeroRoot, receipts, err
			}
			metrics.ReducerEvidence += time.Since(reducerStart)
		case TxPolicy:
			metrics.PolicyTxs++
			reducerStart := time.Now()
			if err := ApplyPolicy(tx.(*PolicyTx), txn); err != nil {
				metrics.ReducerPolicy += time.Since(reducerStart)
				r.Code, r.Error = ExecCodeReducerError, err.Error()
				receipts[i] = r
				return 0, zeroRoot, receipts, err
			}
			metrics.ReducerPolicy += time.Since(reducerStart)
		default:
			r.Code, r.Error = ExecCodeInvalid, "unknown tx type"
			receipts[i] = r
			return 0, zeroRoot, receipts, errors.New("unknown tx type")
		}

		r.Code = ExecCodeOK
		receipts[i] = r
	}

	// Commit atomically to next version and compute new root
	newVersion = latest + 1
	commitStart := time.Now()
	root, err = txn.Commit(newVersion)
	metrics.CommitState = time.Since(commitStart)
	if err != nil {
		return 0, zeroRoot, receipts, err
	}
	return newVersion, root, receipts, nil
}
