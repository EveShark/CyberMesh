package wiring

import (
	"context"
	"errors"
	"sort"
	"sync/atomic"
	"time"

	"backend/pkg/utils"
)

type pendingPersistenceEntry struct {
	// Keep full task so recovered persistence can replay exact block/receipts/root.
	// This retains AppBlock references until processed/pruned; cap the map size.
	task   *PersistenceTask
	seenAt time.Time
}

func (s *Service) rememberPendingPersistence(task *PersistenceTask) {
	if s == nil || task == nil || task.Block == nil || !s.persistCommitProposerOnly || !s.persistBackfillEnabled {
		return
	}
	h := task.Block.GetHeight()
	s.persistBackfillMu.Lock()
	if s.persistBackfillPendingMax > 0 && len(s.persistBackfillPending) >= s.persistBackfillPendingMax {
		evictedHeight := uint64(0)
		oldestSeen := time.Now()
		for eh, entry := range s.persistBackfillPending {
			if entry.seenAt.Before(oldestSeen) || evictedHeight == 0 {
				evictedHeight = eh
				oldestSeen = entry.seenAt
			}
		}
		if evictedHeight != 0 {
			delete(s.persistBackfillPending, evictedHeight)
			atomic.AddUint64(&s.persistBackfillDropped, 1)
			if s.log != nil {
				s.log.Warn("persistence backfill pending map capped; evicting oldest entry",
					utils.ZapUint64("evicted_height", evictedHeight),
					utils.ZapInt("pending_max", s.persistBackfillPendingMax))
			}
			if s.audit != nil {
				_ = s.audit.Warn("persistence_backfill_pending_evicted", map[string]interface{}{
					"evicted_height": evictedHeight,
					"pending_max":    s.persistBackfillPendingMax,
				})
			}
		}
	}
	if _, exists := s.persistBackfillPending[h]; !exists {
		s.persistBackfillPending[h] = pendingPersistenceEntry{task: task, seenAt: time.Now()}
	}
	s.persistBackfillMu.Unlock()
}

func (s *Service) runPersistenceBackfill(ctx context.Context) {
	if s == nil || s.persistWorker == nil || !s.persistCommitProposerOnly || !s.persistBackfillEnabled {
		return
	}

	ticker := time.NewTicker(s.persistBackfillPollInterval)
	defer ticker.Stop()
	timeoutStreak := 0
	cooldownUntil := time.Time{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			if !cooldownUntil.IsZero() && now.Before(cooldownUntil) {
				continue
			}
			timedOut := s.tryPersistenceBackfill(ctx)
			if timedOut {
				timeoutStreak++
				cooldown := nextBackgroundProbeCooldown(timeoutStreak, s.persistBackfillPollInterval)
				cooldownUntil = now.Add(cooldown)
				if s.log != nil {
					s.log.Debug("persistence backfill probe cooling down after timeout",
						utils.ZapInt("timeout_streak", timeoutStreak),
						utils.ZapDuration("cooldown", cooldown))
				}
				continue
			}
			timeoutStreak = 0
			cooldownUntil = time.Time{}
		}
	}
}

func (s *Service) tryPersistenceBackfill(ctx context.Context) bool {
	if s == nil || s.persistWorker == nil || !s.persistCommitProposerOnly || !s.persistBackfillEnabled {
		return false
	}
	// In proposer-only persistence mode, limit background backfill DB reads to the
	// current leader to avoid N-way polling pressure under load.
	if s.eng != nil {
		status := s.eng.GetStatus()
		if !status.IsLeader {
			return false
		}
	}

	adapter := s.persistWorker.GetAdapter()
	if adapter == nil {
		return false
	}

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	latest, err := adapter.GetLatestHeight(checkCtx)
	cancel()
	if err != nil {
		if s.log != nil {
			s.log.Debug("persistence backfill latest height check failed", utils.ZapError(err))
		}
		return isBackgroundTimeoutErr(err)
	}

	s.mu.Lock()
	committed := s.lastCommittedHeight
	s.mu.Unlock()

	// Nothing to repair yet.
	if committed <= latest+s.persistBackfillLagThreshold {
		s.dropPersistBackfillUpTo(latest)
		return false
	}

	now := time.Now()
	candidates := s.collectPersistBackfillCandidates(latest, committed, now)
	for _, height := range candidates {
		entry, ok := s.pendingBackfillEntry(height)
		if !ok || entry.task == nil {
			continue
		}

		enqueueCtx, enqueueCancel := context.WithTimeout(ctx, 2*time.Second)
		decision := s.persistWriterDecisionForBlock(entry.task.Block)
		s.recordPersistEnqueueAttempt("backfill", decision.role == "proposer")
		err := s.enqueuePersistenceTask(enqueueCtx, entry.task, "backfill")
		enqueueCancel()
		if err != nil {
			if errors.Is(err, errPersistEnqueueNonProposer) {
				s.deletePersistBackfill(height)
				continue
			}
			if s.log != nil {
				s.log.Warn("persistence backfill enqueue failed",
					utils.ZapError(err),
					utils.ZapUint64("height", height))
			}
			continue
		}

		s.deletePersistBackfill(height)
		if s.log != nil {
			s.log.Info("persistence backfill enqueued",
				utils.ZapUint64("height", height),
				utils.ZapUint64("latest_durable_height", latest),
				utils.ZapUint64("latest_committed_height", committed))
		}
		if s.audit != nil {
			_ = s.audit.Warn("persistence_backfill_enqueued", map[string]interface{}{
				"height":                height,
				"latest_durable_height": latest,
				"latest_committed":      committed,
			})
		}
	}
	return false
}

func (s *Service) collectPersistBackfillCandidates(latest uint64, committed uint64, now time.Time) []uint64 {
	s.persistBackfillMu.Lock()
	defer s.persistBackfillMu.Unlock()

	heights := make([]uint64, 0, len(s.persistBackfillPending))
	for h, entry := range s.persistBackfillPending {
		if h <= latest {
			delete(s.persistBackfillPending, h)
			continue
		}
		if h > committed {
			continue
		}
		if now.Sub(entry.seenAt) < s.persistBackfillStaleAfter {
			continue
		}
		heights = append(heights, h)
	}

	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	if len(heights) > s.persistBackfillMaxPerCycle {
		heights = heights[:s.persistBackfillMaxPerCycle]
	}
	return heights
}

func (s *Service) dropPersistBackfillUpTo(height uint64) {
	s.persistBackfillMu.Lock()
	for h := range s.persistBackfillPending {
		if h <= height {
			delete(s.persistBackfillPending, h)
		}
	}
	s.persistBackfillMu.Unlock()
}

func (s *Service) pendingBackfillEntry(height uint64) (pendingPersistenceEntry, bool) {
	s.persistBackfillMu.RLock()
	defer s.persistBackfillMu.RUnlock()
	entry, ok := s.persistBackfillPending[height]
	return entry, ok
}

func (s *Service) deletePersistBackfill(height uint64) {
	s.persistBackfillMu.Lock()
	delete(s.persistBackfillPending, height)
	s.persistBackfillMu.Unlock()
}
