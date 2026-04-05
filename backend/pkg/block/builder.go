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
	return b.BuildWithExclusions(height, parent, proposer, now, nil)
}

// BuildWithExclusions builds a block while excluding specific transaction content hashes.
// This is used by proposer-side anti-replay hold logic to avoid re-proposing the same tx
// across rapid view changes before commit cleanup runs.
func (b *Builder) BuildWithExclusions(height uint64, parent types.BlockHash, proposer types.ValidatorID, now time.Time, exclusions map[[32]byte]struct{}) *AppBlock {
	count, _, oldest := b.mp.StatsDetailed()
	maxCount := b.cfg.MaxTxsPerBlock
	maxBytes := b.cfg.MaxBlockBytes
	if maxBytes > 0 && b.cfg.ProposalSizeReserveBytes > 0 && b.cfg.ProposalSizeReserveBytes < maxBytes {
		maxBytes -= b.cfg.ProposalSizeReserveBytes
	}
	policyFloor := b.cfg.PolicyPriorityMinTxs

	backpressureThreshold := int(float64(b.cfg.MempoolCapacity) * b.cfg.BackpressureThreshold)

	if count > backpressureThreshold {
		maxCount = min(count, b.cfg.MaxTxsBackpressure)
	}

	if oldest > 0 && now.Unix()-oldest > b.cfg.LatencyThresholdSeconds {
		maxCount = min(count, b.cfg.MaxTxsLatency)
	}
	if policyFloor < 0 {
		policyFloor = 0
	}
	if maxCount > 0 && policyFloor > maxCount {
		policyFloor = maxCount
	}

	// Fetch with unlimited count, then apply exclusion and per-block cap locally.
	// This avoids starvation when top-ranked txs are temporarily excluded.
	candidates := b.mp.Select(0, maxBytes)
	txs := make([]state.Transaction, 0, len(candidates))
	selected := make(map[[32]byte]struct{}, len(candidates))
	currentBytes := 0

	appendTx := func(tx state.Transaction) bool {
		if tx == nil {
			return false
		}
		env := tx.Envelope()
		if env == nil {
			return false
		}
		if _, exists := selected[env.ContentHash]; exists {
			return false
		}
		if len(exclusions) > 0 {
			if _, blocked := exclusions[env.ContentHash]; blocked {
				return false
			}
		}
		size := len(tx.Payload())
		if maxBytes > 0 && currentBytes+size > maxBytes {
			return false
		}
		if maxCount > 0 && len(txs) >= maxCount {
			return false
		}
		txs = append(txs, tx)
		selected[env.ContentHash] = struct{}{}
		currentBytes += size
		return true
	}

	if policyFloor > 0 {
		pickedPolicies := 0
		for _, tx := range candidates {
			if tx == nil || tx.Type() != state.TxPolicy {
				continue
			}
			if appendTx(tx) {
				pickedPolicies++
				if pickedPolicies >= policyFloor {
					break
				}
			}
		}
	}

	for _, tx := range candidates {
		if !appendTx(tx) {
			if maxCount > 0 && len(txs) >= maxCount {
				break
			}
			continue
		}
		if maxCount > 0 && len(txs) >= maxCount {
			break
		}
	}

	// Prepare roots
	txRoot := computeTxRoot(txs)
	var hint types.BlockHash
	if root, ok := b.store.Root(b.store.Latest()); ok {
		copy(hint[:], root[:])
	}
	blk := NewAppBlock(height, parent, txRoot, hint, proposer, now, txs)
	return blk
}
