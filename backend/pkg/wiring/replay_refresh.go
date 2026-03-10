package wiring

import (
	"context"
	"time"

	"backend/pkg/utils"
)

func (s *Service) runReplayFilterRefresh(ctx context.Context) {
	if s == nil || !s.replayFilterEnabled || !s.replayRefreshEnabled || s.persistWorker == nil {
		return
	}

	s.refreshReplayFilterFromDB(ctx)
	ticker := time.NewTicker(s.replayRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.refreshReplayFilterFromDB(ctx)
		}
	}
}

func (s *Service) refreshReplayFilterFromDB(ctx context.Context) {
	if s == nil || !s.replayFilterEnabled || !s.replayRefreshEnabled || s.persistWorker == nil {
		return
	}
	// In proposer-only persistence mode, a single refresh reader is enough.
	// Non-leaders skip refresh to avoid N-way background DB fan-out.
	if s.replayRefreshLeaderOnly && s.persistCommitProposerOnly && s.eng != nil {
		status := s.eng.GetStatus()
		if !status.IsLeader {
			return
		}
	}
	adapter := s.persistWorker.GetAdapter()
	if adapter == nil {
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	latest, err := adapter.GetLatestHeight(checkCtx)
	cancel()
	if err != nil {
		if s.log != nil {
			s.log.Debug("replay refresh latest-height check failed", utils.ZapError(err))
		}
		return
	}
	if latest == 0 {
		return
	}

	windowStart := uint64(1)
	if latest > s.replayRefreshHeightWindow {
		windowStart = latest - s.replayRefreshHeightWindow + 1
	}
	if s.replayRefreshNext < windowStart || s.replayRefreshNext > latest {
		s.replayRefreshNext = windowStart
	}
	start := s.replayRefreshNext
	end := latest
	maxHeights := s.replayRefreshMaxHeightsTick
	if maxHeights <= 0 {
		maxHeights = 32
	}
	if span := uint64(maxHeights); start+span-1 < end {
		end = start + span - 1
	}

	now := time.Now()
	for h := start; h <= end; h++ {
		listCtx, listCancel := context.WithTimeout(ctx, 2*time.Second)
		metas, err := adapter.ListTransactionsByBlock(listCtx, h)
		listCancel()
		if err != nil {
			if s.log != nil {
				s.log.Debug("replay refresh list-transactions failed",
					utils.ZapError(err),
					utils.ZapUint64("height", h))
			}
			continue
		}
		for _, meta := range metas {
			if len(meta.TxHash) != 32 {
				continue
			}
			var txh [32]byte
			copy(txh[:], meta.TxHash)
			s.rememberCommittedTxHash(txh, now)
		}
	}
	if end >= latest {
		s.replayRefreshNext = windowStart
		return
	}
	s.replayRefreshNext = end + 1
}
