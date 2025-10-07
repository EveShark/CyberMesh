package block

import (
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
	// Prepare roots
	txRoot := computeTxRoot(txs)
	var hint types.BlockHash
	if root, ok := b.store.Root(b.store.Latest()); ok {
		copy(hint[:], root[:])
	}
	blk := NewAppBlock(height, parent, txRoot, hint, proposer, now, txs)
	return blk
}
