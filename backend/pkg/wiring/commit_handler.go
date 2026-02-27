package wiring

import (
	"context"
	"fmt"
	"time"

	"backend/pkg/block"
	"backend/pkg/consensus/api"
	ctypes "backend/pkg/consensus/types"
	"backend/pkg/state"
	"backend/pkg/utils"
)

func (s *Service) onCommit(ctx context.Context, b api.Block, qc api.QC) error {
	logCommitInfo := s.shouldLogCommitInfo()
	if logCommitInfo {
		s.log.InfoContext(ctx, "onCommit called",
			utils.ZapUint64("height", b.GetHeight()),
			utils.ZapBool("has_persistWorker", s.persistWorker != nil))
	}

	start := time.Now()

	// Validate block before committing
	// NOTE: GetCurrentHeight returns NEXT height to propose (lastCommitted+1),
	// but we need to validate against the actual committed height sequence.
	// Since OnQC advances height before onCommit is called, we must check
	// block.Height against lastCommitted+1 from storage, not currentHeight.
	if err := s.validateBlock(b); err != nil {
		s.metrics.IncrementValidationFailures()
		s.log.ErrorContext(ctx, "block validation failed", utils.ZapError(err))
		return fmt.Errorf("block validation failed: %w", err)
	}

	// Expect our AppBlock (produced by Builder)
	ab, ok := b.(*block.AppBlock)
	if !ok {
		s.log.WarnContext(ctx, "received non-AppBlock commit, skipping state execution",
			utils.ZapString("block_type", fmt.Sprintf("%T", b)),
			utils.ZapUint64("height", b.GetHeight()))
		return nil
	}

	if logCommitInfo {
		s.log.InfoContext(ctx, "executing AppBlock in state machine",
			utils.ZapInt("tx_count", ab.GetTransactionCount()))
	}

	// Execute deterministically against state store (use block timestamp, not wall clock)
	blockTime := ab.GetTimestamp()
	skew := s.cfg.TimestampSkew
	if skew == 0 {
		skew = 30 * time.Second // default skew tolerance
	}

	oldest := blockTime
	for _, tx := range ab.Transactions() {
		ts := time.Unix(tx.Timestamp(), 0)
		if ts.Before(oldest) {
			oldest = ts
		}
	}

	if delta := blockTime.Sub(oldest); delta > skew {
		s.log.Info("expanding timestamp skew for backlog block",
			utils.ZapDuration("original_skew", skew),
			utils.ZapDuration("backlog_age", delta),
			utils.ZapUint64("height", ab.GetHeight()))
		skew = delta
	} else if blockLag := time.Since(blockTime); blockLag > skew {
		s.log.Info("expanding timestamp skew for catchup block",
			utils.ZapDuration("original_skew", skew),
			utils.ZapDuration("block_age", blockLag),
			utils.ZapUint64("height", ab.GetHeight()))
		skew = blockLag
	}
	version, root, receipts, err := state.ApplyBlock(s.store, blockTime, skew, ab.Transactions())
	if err != nil {
		s.metrics.IncrementCommitErrors()
		s.log.Error("apply block failed", utils.ZapError(err), utils.ZapUint64("version", version))
		return err
	}

	// Update metrics
	s.metrics.IncrementBlocksCommitted()
	s.metrics.AddTransactionsCommitted(uint64(ab.GetTransactionCount()))
	s.metrics.UpdateLastCommittedHeight(ab.GetHeight())
	s.metrics.UpdateLastCommittedVersion(version)
	s.metrics.RecordCommitDuration(time.Since(start))

	// Update last parent hash, state root, and committed height
	s.mu.Lock()
	s.lastParent = ab.GetHash()
	s.lastRoot = root
	s.lastCommittedHeight = ab.GetHeight()
	s.mu.Unlock()

	// Remove committed txs from mempool
	hashes := make([][32]byte, 0, len(receipts))
	for _, r := range receipts {
		hashes = append(hashes, r.ContentHash)
	}
	s.mp.Remove(hashes...)

	// Audit with state root
	if logCommitInfo {
		s.log.InfoContext(ctx, "block committed",
			utils.ZapUint64("height", ab.GetHeight()),
			utils.ZapInt("tx_count", ab.GetTransactionCount()),
			utils.ZapString("state_root", fmt.Sprintf("%x", root[:8])),
			utils.ZapUint64("version", version))
	}

	// Prune old state versions to prevent memory growth
	if memStore, ok := s.store.(*state.MemStore); ok {
		memStore.PruneRetain(s.cfg.StateRetainVersions)
	}

	// Check memory thresholds
	mempoolTxs, _ := s.mp.Stats()
	producerCount := s.mp.ProducerCount()
	s.memMon.Check(mempoolTxs, producerCount, 0, 0, 0, 0)

	// Enqueue async persistence if enabled
	if s.persistWorker != nil {
		if logCommitInfo {
			s.log.InfoContext(ctx, "enqueueing persistence task",
				utils.ZapUint64("height", ab.GetHeight()))
		}

		task := &PersistenceTask{
			Block:     ab,
			Receipts:  receipts,
			StateRoot: root,
			Attempt:   0,
		}
		if err := s.persistWorker.Enqueue(ctx, task); err != nil {
			// Log error but don't fail consensus (async persistence)
			s.log.WarnContext(ctx, "failed to enqueue persistence task",
				utils.ZapError(err),
				utils.ZapUint64("height", ab.GetHeight()))
		} else if logCommitInfo {
			s.log.InfoContext(ctx, "persistence task enqueued successfully")
		}

	} else {
		if s.shouldLogCommitWarn(&s.lastPersistNilWarnNs, s.commitWarnThrottle) {
			s.log.WarnContext(ctx, "persistWorker is nil, cannot persist to database")
		}
	}

	return nil
}

func shouldPublishPolicyFromCommit(commitEnabled bool, proposerOnly bool, localNodeID ctypes.ValidatorID, proposerID ctypes.ValidatorID) bool {
	if !commitEnabled {
		return false
	}
	if !proposerOnly {
		return true
	}
	return localNodeID == proposerID
}

func (s *Service) shouldPublishPolicyOnCommit(ab *block.AppBlock) bool {
	if s == nil || ab == nil || s.policyPublisher == nil {
		return false
	}
	status := s.eng.GetStatus()
	allowed := shouldPublishPolicyFromCommit(s.policyPublishOnCommit, s.policyCommitProposerOnly, status.NodeID, ab.Proposer())
	if !allowed && s.policyPublishOnCommit && s.policyCommitProposerOnly {
		s.log.Debug("Skipping commit policy publish on non-proposer validator",
			utils.ZapUint64("height", ab.GetHeight()))
	}
	return allowed
}

func (s *Service) logCommitPolicySummary(ab *block.AppBlock, policyCount int, publishEnabled bool, willPublish bool) {
	if s == nil || ab == nil || s.log == nil {
		return
	}
	status := s.eng.GetStatus()
	isProposer := status.NodeID == ab.Proposer()
	s.log.Info("commit policy summary",
		utils.ZapUint64("height", ab.GetHeight()),
		utils.ZapInt("policy_count", policyCount),
		utils.ZapBool("publish_enabled", publishEnabled),
		utils.ZapBool("commit_writer_proposer_only", s.policyCommitProposerOnly),
		utils.ZapBool("is_block_proposer", isProposer),
		utils.ZapBool("will_publish", willPublish))
}
