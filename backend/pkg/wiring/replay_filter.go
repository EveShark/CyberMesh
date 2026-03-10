package wiring

import (
	"fmt"
	"sync/atomic"
	"time"

	"backend/pkg/block"
	"backend/pkg/consensus/types"
	"backend/pkg/state"
	"backend/pkg/utils"
)

func txIdentityHash(tx state.Transaction) ([32]byte, bool) {
	var zero [32]byte
	if tx == nil {
		return zero, false
	}
	env := tx.Envelope()
	if env == nil {
		return zero, false
	}

	var domain string
	switch tx.Type() {
	case state.TxEvent:
		domain = state.DomainEventTx
	case state.TxEvidence:
		domain = state.DomainEvidenceTx
	case state.TxPolicy:
		domain = state.DomainPolicyTx
	default:
		return zero, false
	}

	signBytes, err := state.BuildSignBytes(domain, tx.Timestamp(), env.ProducerID, env.Nonce, env.ContentHash)
	if err != nil {
		return zero, false
	}
	return state.HashBytes(signBytes), true
}

func (s *Service) rememberCommittedTransactions(blk *block.AppBlock, now time.Time) {
	if s == nil || blk == nil || !s.replayFilterEnabled {
		return
	}
	for _, tx := range blk.Transactions() {
		hash, ok := txIdentityHash(tx)
		if !ok {
			continue
		}
		s.rememberCommittedTxHash(hash, now)
	}
}

func (s *Service) shouldRejectReplayOnAdmit(tx state.Transaction, now time.Time) bool {
	if s == nil || !s.replayFilterEnabled || tx == nil {
		return false
	}
	hash, ok := txIdentityHash(tx)
	if !ok {
		return false
	}
	if s.isCommittedTxHash(hash, now) {
		atomic.AddUint64(&s.replayRejectedAdmit, 1)
		if s.log != nil {
			s.log.Debug("replay filter rejected tx at admission",
				utils.ZapString("tx_hash", fmt.Sprintf("%x", hash[:8])))
		}
		return true
	}
	return false
}

func (s *Service) rememberCommittedTxHash(hash [32]byte, now time.Time) {
	if s == nil || !s.replayFilterEnabled {
		return
	}
	exp := now.Add(s.replayFilterTTL)
	s.replayFilterMu.Lock()
	defer s.replayFilterMu.Unlock()

	s.pruneReplayFilterExpiredLocked(now)
	if s.replayFilterMax > 0 && len(s.replayCommittedTx) >= s.replayFilterMax {
		var oldestHash [32]byte
		var oldestExp time.Time
		found := false
		for h, expiry := range s.replayCommittedTx {
			if !found || expiry.Before(oldestExp) {
				oldestHash = h
				oldestExp = expiry
				found = true
			}
		}
		if found {
			delete(s.replayCommittedTx, oldestHash)
			atomic.AddUint64(&s.replayFilterEvictions, 1)
		}
	}

	s.replayCommittedTx[hash] = exp
}

func (s *Service) isCommittedTxHash(hash [32]byte, now time.Time) bool {
	if s == nil || !s.replayFilterEnabled {
		return false
	}
	s.replayFilterMu.RLock()
	expiry, ok := s.replayCommittedTx[hash]
	s.replayFilterMu.RUnlock()
	if !ok {
		return false
	}
	if !now.After(expiry) {
		return true
	}
	// Upgrade to write lock only when we need to evict an expired key.
	s.replayFilterMu.Lock()
	defer s.replayFilterMu.Unlock()
	expiry, ok = s.replayCommittedTx[hash]
	if !ok {
		return false
	}
	if now.After(expiry) {
		delete(s.replayCommittedTx, hash)
		return false
	}
	return true
}

func (s *Service) replayFilterSize() int {
	if s == nil || !s.replayFilterEnabled {
		return 0
	}
	s.replayFilterMu.Lock()
	defer s.replayFilterMu.Unlock()
	s.pruneReplayFilterExpiredLocked(time.Now())
	return len(s.replayCommittedTx)
}

func (s *Service) pruneReplayFilterExpiredLocked(now time.Time) {
	for h, expiry := range s.replayCommittedTx {
		if now.After(expiry) {
			delete(s.replayCommittedTx, h)
		}
	}
}

func (s *Service) filterCommittedTxFromBlock(blk *block.AppBlock, now time.Time) (*block.AppBlock, int) {
	if s == nil || blk == nil || !s.replayFilterEnabled {
		return blk, 0
	}
	txs := blk.Transactions()
	if len(txs) == 0 {
		return blk, 0
	}

	filtered := make([]state.Transaction, 0, len(txs))
	rejected := 0
	for _, tx := range txs {
		hash, ok := txIdentityHash(tx)
		if ok && s.isCommittedTxHash(hash, now) {
			rejected++
			continue
		}
		filtered = append(filtered, tx)
	}
	if rejected == 0 {
		return blk, 0
	}

	atomic.AddUint64(&s.replayRejectedBuild, uint64(rejected))
	txRoot := computeTxRootFromTransactions(filtered)
	rebuilt := block.NewAppBlock(
		blk.GetHeight(),
		blk.GetParentHash(),
		txRoot,
		blk.StateRootHint(),
		blk.Proposer(),
		blk.GetTimestamp(),
		filtered,
	)
	if s.log != nil {
		s.log.Debug("proposal txs filtered by replay guard",
			utils.ZapUint64("height", blk.GetHeight()),
			utils.ZapInt("removed", rejected),
			utils.ZapInt("remaining", rebuilt.GetTransactionCount()))
	}
	return rebuilt, rejected
}

func computeTxRootFromTransactions(txs []state.Transaction) types.BlockHash {
	if len(txs) == 0 {
		return types.BlockHash(state.HashBytes(nil))
	}
	pairs := make([]state.KVPair, 0, len(txs))
	for _, tx := range txs {
		if tx == nil || tx.Envelope() == nil {
			continue
		}
		ch := tx.Envelope().ContentHash
		pairs = append(pairs, state.KVPair{Key: ch[:], Value: nil})
	}
	root := state.BuildRoot(pairs)
	var out types.BlockHash
	copy(out[:], root[:])
	return out
}
