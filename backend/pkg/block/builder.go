package block

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"backend/pkg/consensus/types"
	"backend/pkg/mempool"
	"backend/pkg/state"
	"backend/pkg/utils"
)

type Builder struct {
	cfg                   Config
	mp                    *mempool.Mempool
	store                 state.StateStore
	log                   *utils.Logger
	p0ReservedPickTotal   uint64
	fairSharePickTotal    uint64
	fairShareRoundsTotal  uint64
	fairShareStarvedTotal uint64
}

type BuilderStats struct {
	P0ReservedPickTotal   uint64
	FairSharePickTotal    uint64
	FairShareRoundsTotal  uint64
	FairShareStarvedTotal uint64
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
	p0Floor := b.cfg.P0PriorityMinTxs

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
	if p0Floor < 0 {
		p0Floor = 0
	}
	if maxCount > 0 && policyFloor > maxCount {
		policyFloor = maxCount
	}
	if maxCount > 0 && p0Floor > maxCount {
		p0Floor = maxCount
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

	if p0Floor > 0 {
		pickedP0 := 0
		for _, tx := range candidates {
			if !isP0CriticalTx(tx) {
				continue
			}
			if appendTx(tx) {
				pickedP0++
				atomic.AddUint64(&b.p0ReservedPickTotal, 1)
				if pickedP0 >= p0Floor {
					break
				}
			}
		}
	}

	if b.cfg.FairShareEnabled {
		picks, rounds, starved := appendFairShare(candidates, appendTx, maxCount, b.cfg.FairShareMaxPerProducer)
		atomic.AddUint64(&b.fairSharePickTotal, uint64(picks))
		atomic.AddUint64(&b.fairShareRoundsTotal, uint64(rounds))
		atomic.AddUint64(&b.fairShareStarvedTotal, uint64(starved))
	} else {
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

func appendFairShare(candidates []state.Transaction, appendTx func(state.Transaction) bool, maxCount int, maxPerProducer int) (int, int, int) {
	byProducer := make(map[string][]state.Transaction)
	order := make([]string, 0, 8)
	selectedPerProducer := make(map[string]int)
	totalPicks := 0
	rounds := 0
	starved := 0
	for _, tx := range candidates {
		if tx == nil || tx.Envelope() == nil {
			continue
		}
		key := hex.EncodeToString(tx.Envelope().ProducerID)
		if _, ok := byProducer[key]; !ok {
			order = append(order, key)
		}
		byProducer[key] = append(byProducer[key], tx)
	}
	if len(order) == 0 {
		return 0, 0, 0
	}
	for {
		rounds++
		progress := false
		for _, producer := range order {
			quota := 1
			if maxPerProducer > 0 {
				quota = maxPerProducer
			}
			for i := 0; i < quota; i++ {
				queue := byProducer[producer]
				if len(queue) == 0 {
					break
				}
				// Try bounded rotation within this producer queue so a blocked head
				// does not prevent appendable tail transactions from being considered.
				attempts := len(queue)
				picked := false
				for attempts > 0 {
					tx := queue[0]
					queue = queue[1:]
					if appendTx(tx) {
						progress = true
						picked = true
						totalPicks++
						selectedPerProducer[producer]++
						byProducer[producer] = queue
						if maxCount > 0 {
							maxCount--
							if maxCount <= 0 {
								for _, p := range order {
									if len(byProducer[p]) > 0 && selectedPerProducer[p] == 0 {
										starved++
									}
								}
								return totalPicks, rounds, starved
							}
						}
						break
					}
					// Requeue failed candidate at tail for later rounds.
					queue = append(queue, tx)
					attempts--
				}
				if !picked {
					byProducer[producer] = queue
				}
			}
		}
		if !progress {
			return totalPicks, rounds, starved
		}
	}
}

func isP0CriticalTx(tx state.Transaction) bool {
	if tx == nil || tx.Type() != state.TxPolicy {
		return false
	}
	payload := tx.Payload()
	if len(payload) == 0 {
		return false
	}
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return false
	}
	lookup := func(m map[string]any, key string) string {
		if m == nil {
			return ""
		}
		if s, ok := m[key].(string); ok {
			return strings.ToLower(strings.TrimSpace(s))
		}
		return ""
	}
	action := lookup(root, "action")
	controlAction := lookup(root, "control_action")
	if nested, ok := root["params"].(map[string]any); ok {
		if action == "" {
			action = lookup(nested, "action")
		}
		if controlAction == "" {
			controlAction = lookup(nested, "control_action")
		}
	}
	switch controlAction {
	case "drop", "reject", "freeze", "block", "remove":
		return true
	}
	switch action {
	case "drop", "reject", "freeze", "block", "remove":
		return true
	}
	return false
}

func (b *Builder) Stats() BuilderStats {
	if b == nil {
		return BuilderStats{}
	}
	return BuilderStats{
		P0ReservedPickTotal:   atomic.LoadUint64(&b.p0ReservedPickTotal),
		FairSharePickTotal:    atomic.LoadUint64(&b.fairSharePickTotal),
		FairShareRoundsTotal:  atomic.LoadUint64(&b.fairShareRoundsTotal),
		FairShareStarvedTotal: atomic.LoadUint64(&b.fairShareStarvedTotal),
	}
}
