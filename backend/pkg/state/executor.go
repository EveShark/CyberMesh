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
func ApplyBlock(store StateStore, now time.Time, skew time.Duration, txs []Transaction) (uint64, [32]byte, []Receipt, error) {
	var zeroRoot [32]byte
	receipts := make([]Receipt, len(txs))

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
		// Pre-compute content hash if available via envelope; fall back to hashing payload
		if env := tx.Envelope(); env != nil {
			r.ContentHash = env.ContentHash
		} else {
			if h, err := tx.Hash(); err == nil {
				r.ContentHash = h
			}
		}

		if err := tx.Validate(now, skew); err != nil {
			r.Code, r.Error = ExecCodeInvalid, err.Error()
			receipts[i] = r
			return 0, zeroRoot, receipts, err
		}

		// Enforce nonce uniqueness at execution time (defense-in-depth)
		if env := tx.Envelope(); env != nil {
			if v, ok := txn.Get(keyNonce(env.ProducerID, env.Nonce)); ok && len(v) > 0 {
				r.Code, r.Error = ExecCodeInvalid, ErrNonceReplay.Error()
				receipts[i] = r
				return 0, zeroRoot, receipts, ErrNonceReplay
			}
		}

		// Apply deterministic reducer
		switch tx.Type() {
		case TxEvent:
			if err := ApplyEvent(tx.(*EventTx), txn); err != nil {
				r.Code, r.Error = ExecCodeReducerError, err.Error()
				receipts[i] = r
				return 0, zeroRoot, receipts, err
			}
		case TxEvidence:
			if err := ApplyEvidence(tx.(*EvidenceTx), txn); err != nil {
				r.Code, r.Error = ExecCodeReducerError, err.Error()
				receipts[i] = r
				return 0, zeroRoot, receipts, err
			}
		case TxPolicy:
			if err := ApplyPolicy(tx.(*PolicyTx), txn); err != nil {
				r.Code, r.Error = ExecCodeReducerError, err.Error()
				receipts[i] = r
				return 0, zeroRoot, receipts, err
			}
		default:
			r.Code, r.Error = ExecCodeInvalid, "unknown tx type"
			receipts[i] = r
			return 0, zeroRoot, receipts, errors.New("unknown tx type")
		}

		r.Code = ExecCodeOK
		receipts[i] = r
	}

	// Commit atomically to next version and compute new root
	newVersion := latest + 1
	root, err := txn.Commit(newVersion)
	if err != nil {
		return 0, zeroRoot, receipts, err
	}
	return newVersion, root, receipts, nil
}
