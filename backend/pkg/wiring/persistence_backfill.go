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
	// firstFailAt marks the first time this height entered pending backfill.
	// For enqueue-timeout reasons we allow a single fast-lane probe before
	// falling back to the normal stale window.
	firstFailAt       time.Time
	reason            pendingPersistenceReason
	fastLaneProbeUsed bool
	nonRetryAttempts  uint32
	nextEligibleAt    time.Time
}

type pendingPersistenceReason string

const (
	pendingReasonUnknown                 pendingPersistenceReason = "unknown"
	pendingReasonNonProposer             pendingPersistenceReason = "non_proposer"
	pendingReasonCommitEnqueueError      pendingPersistenceReason = "commit_enqueue_error"
	pendingReasonCommitEnqueueTimeout    pendingPersistenceReason = "commit_enqueue_timeout"
	pendingReasonDirectFallbackFailed    pendingPersistenceReason = "direct_fallback_failed"
	pendingReasonDirectFallbackNonRetry  pendingPersistenceReason = "direct_fallback_non_retryable_failed"
	pendingReasonDirectFallbackThrottled pendingPersistenceReason = "direct_fallback_throttled"
	pendingReasonDirectFallbackPressure  pendingPersistenceReason = "direct_fallback_pressure"
	pendingReasonDirectFallbackDistress  pendingPersistenceReason = "direct_fallback_distress"
)

const (
	nonRetryBackfillMaxAttempts = 3
	nonRetryBackfillBaseDelay   = 30 * time.Second
	nonRetryBackfillMaxDelay    = 10 * time.Minute
)

func (s *Service) rememberPendingPersistence(task *PersistenceTask, reason pendingPersistenceReason) {
	if s == nil || task == nil || task.Block == nil || !s.persistCommitProposerOnly || !s.persistBackfillEnabled {
		return
	}
	if reason == "" {
		reason = pendingReasonUnknown
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
	now := time.Now()
	if existing, exists := s.persistBackfillPending[h]; exists {
		existing.task = task
		if reason != "" && existing.reason != reason {
			existing.reason = reason
			if reason == pendingReasonDirectFallbackNonRetry {
				existing.nonRetryAttempts = 0
				existing.nextEligibleAt = time.Time{}
			}
		}
		s.persistBackfillPending[h] = existing
		s.persistBackfillMu.Unlock()
		return
	}
	if _, exists := s.persistBackfillPending[h]; !exists {
		s.persistBackfillPending[h] = pendingPersistenceEntry{
			task:              task,
			seenAt:            now,
			firstFailAt:       now,
			reason:            reason,
			fastLaneProbeUsed: false,
			nonRetryAttempts:  0,
			nextEligibleAt:    time.Time{},
		}
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
			if entry.reason == pendingReasonDirectFallbackNonRetry {
				s.recordNonRetryBackfillFailure(height)
			}
			if s.log != nil {
				s.log.Warn("persistence backfill enqueue failed",
					utils.ZapError(err),
					utils.ZapUint64("height", height),
					utils.ZapString("reason", string(entry.reason)))
			}
			continue
		}

		s.deletePersistBackfill(height)
		if s.log != nil {
			s.log.Info("persistence backfill enqueued",
				utils.ZapUint64("height", height),
				utils.ZapUint64("latest_durable_height", latest),
				utils.ZapUint64("latest_committed_height", committed),
				utils.ZapString("reason", string(entry.reason)))
		}
		if s.audit != nil {
			_ = s.audit.Warn("persistence_backfill_enqueued", map[string]interface{}{
				"height":                height,
				"latest_durable_height": latest,
				"latest_committed":      committed,
				"reason":                string(entry.reason),
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
		ready, consumedFastLane := s.shouldBackfillNow(entry, now)
		if !ready {
			continue
		}
		if consumedFastLane {
			entry.fastLaneProbeUsed = true
			s.persistBackfillPending[h] = entry
		}
		heights = append(heights, h)
	}

	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	if len(heights) > s.persistBackfillMaxPerCycle {
		heights = heights[:s.persistBackfillMaxPerCycle]
	}
	return heights
}

func (s *Service) shouldBackfillNow(entry pendingPersistenceEntry, now time.Time) (bool, bool) {
	if s == nil {
		return false, false
	}
	if entry.seenAt.IsZero() {
		return true, false
	}
	if entry.firstFailAt.IsZero() {
		entry.firstFailAt = entry.seenAt
	}
	if entry.reason == pendingReasonDirectFallbackNonRetry {
		if !entry.nextEligibleAt.IsZero() && now.Before(entry.nextEligibleAt) {
			return false, false
		}
	}
	if s.persistBackfillFastLaneEnabled &&
		entry.reason == pendingReasonCommitEnqueueTimeout &&
		!entry.fastLaneProbeUsed {
		if now.Sub(entry.firstFailAt) >= s.persistBackfillFastLaneMaxAge {
			return true, true
		}
	}
	return now.Sub(entry.seenAt) >= s.persistBackfillStaleAfter, false
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

func (s *Service) recordNonRetryBackfillFailure(height uint64) {
	if s == nil {
		return
	}
	attempt := uint32(0)
	nextEligible := time.Time{}
	quarantined := false
	s.persistBackfillMu.Lock()
	entry, ok := s.persistBackfillPending[height]
	if !ok {
		s.persistBackfillMu.Unlock()
		return
	}
	entry.nonRetryAttempts++
	attempt = entry.nonRetryAttempts
	if entry.nonRetryAttempts > nonRetryBackfillMaxAttempts {
		delete(s.persistBackfillPending, height)
		quarantined = true
		atomic.AddUint64(&s.persistBackfillDropped, 1)
	} else {
		backoff := nextNonRetryBackoff(entry.nonRetryAttempts)
		entry.nextEligibleAt = time.Now().Add(backoff)
		nextEligible = entry.nextEligibleAt
		s.persistBackfillPending[height] = entry
	}
	s.persistBackfillMu.Unlock()

	if quarantined {
		atomic.AddUint64(&s.persistBackfillNonRetryQuarantined, 1)
		if s.log != nil {
			s.log.Error("persistence backfill quarantined non-retryable entry after repeated failures",
				utils.ZapUint64("height", height),
				utils.ZapUint64("attempts", uint64(attempt)))
		}
		if s.audit != nil {
			_ = s.audit.Error("persistence_backfill_non_retryable_quarantined", map[string]interface{}{
				"height":   height,
				"attempts": attempt,
			})
		}
		return
	}
	if s.log != nil {
		s.log.Warn("persistence backfill scheduling cooldown for non-retryable entry",
			utils.ZapUint64("height", height),
			utils.ZapUint64("attempt", uint64(attempt)),
			utils.ZapTime("next_eligible_at", nextEligible))
	}
}

func nextNonRetryBackoff(attempt uint32) time.Duration {
	if attempt == 0 {
		return nonRetryBackfillBaseDelay
	}
	backoff := nonRetryBackfillBaseDelay * time.Duration(1<<uint(attempt-1))
	if backoff > nonRetryBackfillMaxDelay {
		backoff = nonRetryBackfillMaxDelay
	}
	return backoff
}
