package wiring

import (
	"context"
	"time"

	"backend/pkg/utils"
)

func (s *Service) runProposer(ctx context.Context) {
	interval := s.cfg.BuildInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-t.C:
			s.tryPropose(ctx)
		}
	}
}

func (s *Service) tryPropose(ctx context.Context) {
	// Check leadership
	isLeader, err := s.eng.IsLeader(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "failed to check leadership", utils.ZapError(err))
		return
	}
	if !isLeader {
		return
	}

	// Check mempool threshold and update metrics
	count, sizeBytes := s.mp.Stats()
	s.metrics.UpdateMempoolStats(count, sizeBytes)
	if s.cfg.MinMempoolTxs > 0 && count < s.cfg.MinMempoolTxs {
		return
	}

	// Determine next height and parent hash
	height := s.eng.GetCurrentHeight() + 1
	s.mu.Lock()
	parent := s.lastParent
	s.mu.Unlock()

	blk := s.builder.Build(height, parent, s.eng.GetStatus().NodeID, time.Now())
	if blk == nil || blk.GetTransactionCount() == 0 {
		return
	}

	// Submit block with retry
	s.metrics.IncrementProposalsAttempted()
	start := time.Now()
	const maxRetries = 3
	backoff := 50 * time.Millisecond
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := s.eng.SubmitBlock(ctx, blk); err != nil {
			if attempt < maxRetries-1 {
				s.log.WarnContext(ctx, "submit block failed, retrying",
					utils.ZapError(err),
					utils.ZapUint64("height", height),
					utils.ZapInt("attempt", attempt+1))
				time.Sleep(backoff)
				backoff *= 2 // exponential backoff
				continue
			}
			// Final attempt failed
			s.metrics.IncrementProposalsFailed()
			s.log.ErrorContext(ctx, "submit block failed after retries",
				utils.ZapError(err),
				utils.ZapUint64("height", height),
				utils.ZapInt("attempts", maxRetries))
			return
		}
		// Success
		s.metrics.IncrementProposalsSucceeded()
		s.metrics.RecordProposalDuration(time.Since(start))
		s.log.InfoContext(ctx, "block submitted successfully",
			utils.ZapUint64("height", height),
			utils.ZapInt("tx_count", blk.GetTransactionCount()))
		return
	}
}
