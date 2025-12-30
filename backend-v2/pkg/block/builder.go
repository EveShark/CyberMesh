package block

import (
	"context"
	"time"

	"backend/pkg/consensus/types"
	"backend/pkg/mempool"
	"backend/pkg/state"
	"backend/pkg/utils"
)

type Builder struct {
	cfg   Config
	mp    *mempool.Mempool
	store state.StateStore
	log   *utils.Logger
}

func NewBuilder(cfg Config, mp *mempool.Mempool, store state.StateStore, log *utils.Logger) *Builder {
	return &Builder{cfg: cfg, mp: mp, store: store, log: log}
}

// computeTxRoot builds a Merkle root from tx content hashes as keys (values empty)
func computeTxRoot(txs []state.Transaction) types.BlockHash {
	if len(txs) == 0 {
		return types.BlockHash(state.HashBytes(nil))
	}
	pairs := make([]state.KVPair, 0, len(txs))
	for _, tx := range txs {
		h := tx.Envelope().ContentHash
		pairs = append(pairs, state.KVPair{Key: h[:], Value: nil})
	}
	r := state.BuildRoot(pairs)
	var out types.BlockHash
	copy(out[:], r[:])
	return out
}

// Build deterministically selects transactions from the mempool under limits and produces an AppBlock.
func (b *Builder) Build(height uint64, parent types.BlockHash, proposer types.ValidatorID, now time.Time) *AppBlock {
	maxCount := b.cfg.MaxTxsPerBlock
	maxBytes := b.cfg.MaxBlockBytes
	txs := b.mp.Select(maxCount, maxBytes)

	// Filter out transactions that are already committed according to the deterministic state store.
	// This prevents nonce-replay stalls when Kafka replays messages or when mempool lags behind commits.
	version := b.store.Latest()
	seenNonce := make(map[string]struct{}, len(txs))
	filtered := make([]state.Transaction, 0, len(txs))
	var dropHashes [][32]byte
	dupInBlock := 0
	committedNonce := 0
	for _, tx := range txs {
		env := tx.Envelope()
		if env == nil {
			continue
		}
		k := string(state.NonceKey(env.ProducerID, env.Nonce))
		if _, ok := seenNonce[k]; ok {
			dupInBlock++
			dropHashes = append(dropHashes, env.ContentHash)
			continue
		}
		seenNonce[k] = struct{}{}

		if v, ok := b.store.Get(version, state.NonceKey(env.ProducerID, env.Nonce)); ok && len(v) > 0 {
			committedNonce++
			dropHashes = append(dropHashes, env.ContentHash)
			continue
		}
		filtered = append(filtered, tx)
	}
	if len(dropHashes) > 0 {
		if b.log != nil {
			b.log.InfoContext(context.Background(), "builder dropped txs with already-committed/duplicate nonces",
				utils.ZapUint64("height", height),
				utils.ZapInt("selected", len(txs)),
				utils.ZapInt("dropped", len(dropHashes)),
				utils.ZapInt("dropped_duplicate_in_block", dupInBlock),
				utils.ZapInt("dropped_already_committed_nonce", committedNonce),
				utils.ZapUint64("state_version", version),
			)
		}
		b.mp.Remove(dropHashes...)
	}
	txs = filtered
	// Prepare roots
	txRoot := computeTxRoot(txs)
	var hint types.BlockHash
	if root, ok := b.store.Root(version); ok {
		copy(hint[:], root[:])
	}
	blk := NewAppBlock(height, parent, txRoot, hint, proposer, now, txs)
	return blk
}
