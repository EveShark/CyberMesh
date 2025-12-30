package wiring

import (
	"context"
	"fmt"
	"time"

	"backend/pkg/block"
	"backend/pkg/consensus/api"
	"backend/pkg/state"
	"backend/pkg/utils"
)

func (s *Service) onCommit(ctx context.Context, b api.Block, qc api.QC) error {
	s.log.InfoContext(ctx, "onCommit called",
		utils.ZapUint64("height", b.GetHeight()),
		utils.ZapBool("has_persistWorker", s.persistWorker != nil))

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

	s.log.InfoContext(ctx, "executing AppBlock in state machine",
		utils.ZapInt("tx_count", ab.GetTransactionCount()))

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
	s.log.InfoContext(ctx, "block committed",
		utils.ZapUint64("height", ab.GetHeight()),
		utils.ZapInt("tx_count", ab.GetTransactionCount()),
		utils.ZapString("state_root", fmt.Sprintf("%x", root[:8])),
		utils.ZapUint64("version", version))

	// Enqueue async persistence if enabled
	if s.persistWorker != nil {
		s.log.InfoContext(ctx, "enqueueing persistence task",
			utils.ZapUint64("height", ab.GetHeight()))

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
		} else {
			s.log.InfoContext(ctx, "persistence task enqueued successfully")
		}
	} else {
		s.log.WarnContext(ctx, "persistWorker is nil, cannot persist to database")
		if s.policyPublisher != nil {
			meta := extractCommitMetadata(ab, s.log)
			if meta.policyCount > 0 {
				s.policyPublisher.Publish(ctx, ab.GetHeight(), ab.GetTimestamp().Unix(), meta.policyCount, meta.policyPayloads)
			}
		}
	}

	return nil
}
