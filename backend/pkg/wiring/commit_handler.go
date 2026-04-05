package wiring

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"backend/pkg/block"
	"backend/pkg/consensus/api"
	ctypes "backend/pkg/consensus/types"
	"backend/pkg/mempool"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
)

var errPersistEnqueueNonProposer = errors.New("persistence enqueue dropped on non-proposer in proposer-only mode")

type persistWriterDecision struct {
	allowed  bool
	role     string
	reason   string
	local    ctypes.ValidatorID
	proposer ctypes.ValidatorID
}

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
		if errors.Is(err, errAlreadyCommittedBlock) {
			s.log.InfoContext(ctx, "skipping duplicate already-committed block callback",
				utils.ZapUint64("height", b.GetHeight()))
			return nil
		}
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
	s.logPolicyStageForBlock("t_state_apply_done", ab, 0, ab.GetHeight(), 0)

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
	identityKeys := make([]mempool.ProducerNonce, 0, len(ab.Transactions()))
	for _, r := range receipts {
		hashes = append(hashes, r.ContentHash)
	}
	for _, tx := range ab.Transactions() {
		if tx == nil || tx.Envelope() == nil {
			continue
		}
		env := tx.Envelope()
		identityKeys = append(identityKeys, mempool.ProducerNonce{
			ProducerID: env.ProducerID,
			Nonce:      env.Nonce,
		})
	}
	s.rememberCommittedTransactions(ab, time.Now())
	s.mp.Remove(hashes...)
	s.mp.RemoveByProducerNonces(identityKeys)
	s.clearProposedTxHold(hashes...)

	// Audit with state root
	if logCommitInfo {
		s.log.InfoContext(ctx, "block committed",
			utils.ZapUint64("height", ab.GetHeight()),
			utils.ZapInt("tx_count", ab.GetTransactionCount()),
			utils.ZapString("state_root", fmt.Sprintf("%x", root[:8])),
			utils.ZapUint64("version", version))
	}
	qcTsMs := int64(0)
	qcView := uint64(0)
	var qcData []byte
	if qc != nil {
		qcTsMs = qc.GetTimestamp().UnixMilli()
		qcView = qc.GetView()
		var encodeErr error
		qcData, encodeErr = encodeCommittedQC(qc)
		if encodeErr != nil {
			s.log.Warn("failed to encode committed QC metadata",
				utils.ZapUint64("height", ab.GetHeight()),
				utils.ZapError(encodeErr))
		}
	}
	s.logPolicyStageForBlock("t_qc_formed", ab, qcView, ab.GetHeight(), qcTsMs)
	s.logPolicyStageForBlock("t_commit", ab, qcView, ab.GetHeight(), qcTsMs)

	// Prune old state versions to prevent memory growth
	if memStore, ok := s.store.(*state.MemStore); ok {
		memStore.PruneRetain(s.cfg.StateRetainVersions)
	}

	// Check memory thresholds
	mempoolTxs, _ := s.mp.Stats()
	producerCount := s.mp.ProducerCount()
	s.memMon.Check(mempoolTxs, producerCount, 0, 0, 0, 0)
	if mempoolTxs > 0 {
		// If backlog remains, wake proposer instead of waiting for next ticker tick.
		s.notifyProposeTrigger("post_commit_backlog")
	}

	// Enqueue async persistence if enabled
	if s.persistWorker != nil {
		task := &PersistenceTask{
			Block:     ab,
			Receipts:  receipts,
			StateRoot: root,
			QCData:    qcData,
			Attempt:   0,
		}
		decision := s.persistWriterDecisionForBlock(ab)
		s.recordPersistEnqueueAttempt("commit", decision.role == "proposer")
		if s.shouldPersistOnCommit(ab) {
			if logCommitInfo {
				s.log.InfoContext(ctx, "enqueueing persistence task",
					utils.ZapUint64("height", ab.GetHeight()))
			}
			if err := s.enqueuePersistenceTask(ctx, task, "commit"); err != nil {
				// Log error but don't fail consensus (async persistence)
				s.log.WarnContext(ctx, "failed to enqueue persistence task",
					utils.ZapError(err),
					utils.ZapUint64("height", ab.GetHeight()))
				if !errors.Is(err, errPersistEnqueueNonProposer) {
					reason := pendingReasonCommitEnqueueError
					if isEnqueueTimeoutErr(err) {
						reason = pendingReasonCommitEnqueueTimeout
					}
					// Commit-path enqueue failures should attempt bounded direct persistence
					// before entering slower backfill recovery.
					if ok, pendingReason := s.tryDirectPersistFallback(ctx, task, reason); !ok {
						s.rememberPendingPersistence(task, pendingReason)
					}
				}
			} else if logCommitInfo {
				s.log.InfoContext(ctx, "persistence task enqueued successfully")
			}
		} else {
			s.rememberPendingPersistence(task, pendingReasonNonProposer)
		}

	} else {
		if s.shouldLogCommitWarn(&s.lastPersistNilWarnNs, s.commitWarnThrottle) {
			s.log.WarnContext(ctx, "persistWorker is nil, cannot persist to database")
		}
	}

	return nil
}

func (s *Service) enqueuePersistenceTask(ctx context.Context, task *PersistenceTask, source string) error {
	if s == nil || s.persistWorker == nil || task == nil || task.Block == nil {
		return fmt.Errorf("persistence enqueue unavailable")
	}
	decision := s.persistWriterDecisionForBlock(task.Block)
	if !decision.allowed {
		atomic.AddUint64(&s.persistEnqueueDroppedNonProposer, 1)
		if s.log != nil {
			s.log.Warn("dropping non-proposer persistence enqueue in proposer-only mode",
				utils.ZapString("source", source),
				utils.ZapUint64("height", task.Block.GetHeight()),
				utils.ZapString("reason", decision.reason))
		}
		if s.audit != nil {
			_ = s.audit.Security("persistence_enqueue_dropped_non_proposer", map[string]interface{}{
				"height": task.Block.GetHeight(),
				"source": source,
				"reason": decision.reason,
			})
		}
		return errPersistEnqueueNonProposer
	}
	enqueueCtx := ctx
	cancel := func() {}
	if source == "commit" {
		// Commit-path persistence enqueue is durability-critical and should not
		// inherit a near-expired consensus callback context. Use a bounded,
		// detached enqueue timeout to avoid falling into slow backfill recovery.
		timeout := s.persistEnqueueTimeout
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		baseCtx := s.persistBaseContext()
		enqueueCtx, cancel = context.WithTimeout(baseCtx, timeout)
	}
	defer cancel()
	started := time.Now()
	if err := s.persistWorker.Enqueue(enqueueCtx, task); err != nil {
		waitMs := uint64(time.Since(started) / time.Millisecond)
		s.recordPersistEnqueueWait(source, waitMs)
		atomic.AddUint64(&s.persistEnqueueErrors, 1)
		if source == "commit" && isEnqueueTimeoutErr(err) {
			atomic.AddUint64(&s.persistEnqueueTimeouts, 1)
			atomic.AddUint64(&s.persistEnqueueTimeoutsCommit, 1)
			s.noteCommitEnqueueResult(true)
		}
		if source == "commit" && !isEnqueueTimeoutErr(err) {
			// Keep distress gate tied to consecutive timeout pressure only.
			s.noteCommitEnqueueResult(false)
		}
		if source == "backfill" && isEnqueueTimeoutErr(err) {
			atomic.AddUint64(&s.persistEnqueueTimeouts, 1)
			atomic.AddUint64(&s.persistEnqueueTimeoutsBackfill, 1)
		}
		return err
	}
	waitMs := uint64(time.Since(started) / time.Millisecond)
	s.recordPersistEnqueueWait(source, waitMs)
	if source == "commit" {
		s.noteCommitEnqueueResult(false)
	}
	return nil
}

func isEnqueueTimeoutErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func (s *Service) tryDirectPersistFallback(ctx context.Context, task *PersistenceTask, reason pendingPersistenceReason) (bool, pendingPersistenceReason) {
	if s == nil || task == nil || task.Block == nil || s.persistWorker == nil || !s.persistDirectFallbackEnabled {
		return false, reason
	}
	if blocked, pendingReason := s.shouldSkipDirectFallback(reason); blocked {
		return false, pendingReason
	}
	adapter := s.persistWorker.GetAdapter()
	if adapter == nil {
		return false, reason
	}
	if !s.acquireDirectFallbackSlot() {
		atomic.AddUint64(&s.persistDirectFallbackThrottled, 1)
		return false, pendingReasonDirectFallbackThrottled
	}
	atomic.AddUint64(&s.persistDirectFallbackAttempts, 1)
	go func(task *PersistenceTask, reason pendingPersistenceReason) {
		defer s.releaseDirectFallbackSlot()
		atomic.AddInt64(&s.persistDirectFallbackInFlight, 1)
		defer atomic.AddInt64(&s.persistDirectFallbackInFlight, -1)
		timeout := s.persistDirectFallbackTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		baseCtx := s.persistBaseContext()
		fallbackCtx, cancel := context.WithTimeout(baseCtx, timeout)
		defer cancel()
		if err := adapter.PersistBlock(fallbackCtx, task.Block, task.Receipts, task.StateRoot); err != nil {
			atomic.AddUint64(&s.persistDirectFallbackFailures, 1)
			retryable := cockroach.IsRetryable(err)
			pendingReason := pendingReasonDirectFallbackNonRetry
			if retryable {
				pendingReason = pendingReasonDirectFallbackFailed
			}
			// Fail closed for durability: always retain a recovery handoff.
			// Non-retryable failures are tagged separately for observability and triage.
			s.rememberPendingPersistence(task, pendingReason)
			if s.log != nil {
				s.log.WarnContext(ctx, "direct persistence fallback failed",
					utils.ZapUint64("height", task.Block.GetHeight()),
					utils.ZapString("reason", string(reason)),
					utils.ZapBool("retryable", retryable),
					utils.ZapBool("pending_enqueued", true),
					utils.ZapString("pending_reason", string(pendingReason)),
					utils.ZapError(err))
			}
			return
		}
		atomic.AddUint64(&s.persistDirectFallbackSuccess, 1)
		s.persistWorker.handlePersistSuccess(baseCtx, task, 1)
		if s.log != nil {
			s.log.InfoContext(ctx, "direct persistence fallback succeeded",
				utils.ZapUint64("height", task.Block.GetHeight()),
				utils.ZapString("reason", string(reason)))
		}
	}(task, reason)
	return true, ""
}

func (s *Service) shouldSkipDirectFallback(reason pendingPersistenceReason) (bool, pendingPersistenceReason) {
	if s == nil || s.persistWorker == nil || !s.persistDirectFallbackDisableOnQueuePressure {
		// keep evaluating distress gate below when pressure gate is disabled
	}
	// Distress guard applies only to enqueue-timeout storms.
	if reason != pendingReasonCommitEnqueueTimeout {
		return false, ""
	}
	if s.isDirectFallbackInDistressCooldown() {
		atomic.AddUint64(&s.persistDirectFallbackSkippedDistress, 1)
		return true, pendingReasonDirectFallbackDistress
	}
	if s == nil || s.persistWorker == nil || !s.persistDirectFallbackDisableOnQueuePressure {
		return false, ""
	}
	queueSize, queueCap := s.persistWorker.GetQueueStats()
	if queueCap <= 0 {
		return false, ""
	}
	thresholdPct := s.persistDirectFallbackQueuePressurePct
	if thresholdPct <= 0 {
		thresholdPct = 85
	}
	queuePct := (queueSize * 100) / queueCap
	if queuePct < thresholdPct {
		return false, ""
	}
	atomic.AddUint64(&s.persistDirectFallbackSkippedPressure, 1)
	if s.log != nil {
		s.log.Warn("skipping direct persistence fallback due to queue pressure",
			utils.ZapInt("queue_size", queueSize),
			utils.ZapInt("queue_cap", queueCap),
			utils.ZapInt("queue_pressure_pct", queuePct),
			utils.ZapInt("pressure_threshold_pct", thresholdPct))
	}
	return true, pendingReasonDirectFallbackPressure
}

func (s *Service) noteCommitEnqueueResult(timeout bool) {
	if s == nil {
		return
	}
	if !timeout {
		atomic.StoreUint64(&s.persistCommitEnqueueTimeoutStreak, 0)
		return
	}
	streak := atomic.AddUint64(&s.persistCommitEnqueueTimeoutStreak, 1)
	if s.persistDirectFallbackDistressThreshold <= 0 || int(streak) < s.persistDirectFallbackDistressThreshold {
		return
	}
	cooldown := s.persistDirectFallbackDistressCooldown
	if cooldown <= 0 {
		cooldown = 5 * time.Second
	}
	atomic.StoreInt64(&s.persistDirectFallbackDistressUntilNs, time.Now().Add(cooldown).UnixNano())
}

func (s *Service) isDirectFallbackInDistressCooldown() bool {
	if s == nil {
		return false
	}
	untilNs := atomic.LoadInt64(&s.persistDirectFallbackDistressUntilNs)
	if untilNs <= 0 {
		return false
	}
	return time.Now().UnixNano() < untilNs
}

func (s *Service) recordPersistEnqueueWait(source string, waitMs uint64) {
	if s == nil {
		return
	}
	switch source {
	case "commit":
		atomic.AddUint64(&s.persistEnqueueWaitCommitSumMs, waitMs)
		atomic.AddUint64(&s.persistEnqueueWaitCommitCount, 1)
	case "backfill":
		atomic.AddUint64(&s.persistEnqueueWaitBackfillSumMs, waitMs)
		atomic.AddUint64(&s.persistEnqueueWaitBackfillCount, 1)
	}
}

func (s *Service) acquireDirectFallbackSlot() bool {
	if s == nil {
		return false
	}
	if s.persistFallbackSlots == nil {
		return true
	}
	select {
	case s.persistFallbackSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) persistBaseContext() context.Context {
	if s == nil || s.persistDetachedCtx == nil {
		return context.Background()
	}
	return s.persistDetachedCtx
}

func (s *Service) releaseDirectFallbackSlot() {
	if s == nil || s.persistFallbackSlots == nil {
		return
	}
	select {
	case <-s.persistFallbackSlots:
	default:
	}
}

func (s *Service) recordPersistEnqueueAttempt(source string, proposer bool) {
	if s == nil {
		return
	}
	switch source {
	case "commit":
		if proposer {
			atomic.AddUint64(&s.persistEnqueueCommitProposer, 1)
		} else {
			atomic.AddUint64(&s.persistEnqueueCommitNonProposer, 1)
		}
	case "backfill":
		if proposer {
			atomic.AddUint64(&s.persistEnqueueBackfillProposer, 1)
		} else {
			atomic.AddUint64(&s.persistEnqueueBackfillNonProposer, 1)
		}
	}
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

func shouldPersistFromCommit(proposerOnly bool, localNodeID ctypes.ValidatorID, proposerID ctypes.ValidatorID) bool {
	return decidePersistFromCommit(proposerOnly, localNodeID, proposerID).allowed
}

func (s *Service) shouldPersistOnCommit(ab *block.AppBlock) bool {
	if s == nil || ab == nil || s.persistWorker == nil {
		return false
	}
	decision := s.persistWriterDecisionForBlock(ab)
	if !decision.allowed && s.persistCommitProposerOnly {
		if s.log != nil {
			s.log.Debug("Skipping full block persistence on non-proposer validator",
				utils.ZapUint64("height", ab.GetHeight()),
				utils.ZapString("reason", decision.reason))
		}
	}
	if s.audit != nil {
		mode := s.persistWriterMode
		if mode == "" {
			mode = "all"
			if s.persistCommitProposerOnly {
				mode = "proposer"
			}
		}
		_ = s.audit.Info("persist_writer_decision", map[string]interface{}{
			"height":       ab.GetHeight(),
			"source":       "commit",
			"local_node":   fmt.Sprintf("%x", decision.local[:8]),
			"proposer":     fmt.Sprintf("%x", decision.proposer[:8]),
			"mode":         mode,
			"role":         decision.role,
			"reason":       decision.reason,
			"will_persist": decision.allowed,
		})
	}
	return decision.allowed
}

func decidePersistFromCommit(proposerOnly bool, localNodeID ctypes.ValidatorID, proposerID ctypes.ValidatorID) persistWriterDecision {
	if isZeroValidatorID(proposerID) {
		return persistWriterDecision{
			allowed:  false,
			role:     "unknown",
			reason:   "missing_proposer_id",
			local:    localNodeID,
			proposer: proposerID,
		}
	}
	isProposer := localNodeID == proposerID
	role := "non_proposer"
	if isProposer {
		role = "proposer"
	}
	if !proposerOnly {
		return persistWriterDecision{
			allowed:  true,
			role:     role,
			reason:   "writer_mode_all",
			local:    localNodeID,
			proposer: proposerID,
		}
	}
	if !isProposer {
		return persistWriterDecision{
			allowed:  false,
			role:     role,
			reason:   "non_proposer_guard",
			local:    localNodeID,
			proposer: proposerID,
		}
	}
	return persistWriterDecision{
		allowed:  true,
		role:     "proposer",
		reason:   "owner_proposer",
		local:    localNodeID,
		proposer: proposerID,
	}
}

func (s *Service) decidePersistWithTakeover(ab *block.AppBlock, localNodeID ctypes.ValidatorID) persistWriterDecision {
	if s == nil || ab == nil {
		return persistWriterDecision{allowed: false, role: "unknown", reason: "invalid_task"}
	}
	base := decidePersistFromCommit(true, localNodeID, ab.Proposer())
	if base.allowed {
		base.reason = "owner_proposer_primary"
		return base
	}
	if base.reason == "missing_proposer_id" {
		return base
	}
	if s.persistWorker == nil {
		return persistWriterDecision{
			allowed:  false,
			role:     "non_proposer",
			reason:   "worker_unavailable",
			local:    localNodeID,
			proposer: ab.Proposer(),
		}
	}
	adapter := s.persistWorker.GetAdapter()
	if adapter == nil {
		return persistWriterDecision{
			allowed:  false,
			role:     "non_proposer",
			reason:   "adapter_unavailable",
			local:    localNodeID,
			proposer: ab.Proposer(),
		}
	}
	committed := uint64(0)
	s.mu.Lock()
	committed = s.lastCommittedHeight
	s.mu.Unlock()

	latestDurable, err := s.getTakeoverLatestDurable(adapter)
	if err != nil {
		return persistWriterDecision{
			allowed:  false,
			role:     "non_proposer",
			reason:   "takeover_probe_failed",
			local:    localNodeID,
			proposer: ab.Proposer(),
		}
	}
	lag := uint64(0)
	if committed > latestDurable {
		lag = committed - latestDurable
	}
	if lag < s.persistTakeoverLagThreshold {
		return persistWriterDecision{
			allowed:  false,
			role:     "non_proposer",
			reason:   "takeover_lag_below_threshold",
			local:    localNodeID,
			proposer: ab.Proposer(),
		}
	}
	if s.persistTakeoverDelay > 0 {
		now := time.Now()
		firstSeen := s.takeoverFirstSeen(ab.GetHeight(), now)
		if now.Sub(firstSeen) < s.persistTakeoverDelay {
			return persistWriterDecision{
				allowed:  false,
				role:     "non_proposer",
				reason:   "takeover_delay_guard",
				local:    localNodeID,
				proposer: ab.Proposer(),
			}
		}
	}
	atomic.AddUint64(&s.persistWriterTakeoverActivations, 1)
	return persistWriterDecision{
		allowed:  true,
		role:     "non_proposer",
		reason:   "takeover_allowed",
		local:    localNodeID,
		proposer: ab.Proposer(),
	}
}

func (s *Service) getTakeoverLatestDurable(adapter interface {
	GetLatestHeight(ctx context.Context) (uint64, error)
}) (uint64, error) {
	if s == nil || adapter == nil {
		return 0, fmt.Errorf("takeover durable probe unavailable")
	}
	now := time.Now()
	s.persistTakeoverMu.Lock()
	cached := s.persistTakeoverCached
	minInterval := s.persistTakeoverProbeMinInterval
	s.persistTakeoverMu.Unlock()
	if cached.set && minInterval > 0 && now.Sub(cached.at) <= minInterval {
		return cached.latest, nil
	}
	probeCtx, cancel := context.WithTimeout(s.persistBaseContext(), s.persistTakeoverProbeTimeout)
	latest, err := adapter.GetLatestHeight(probeCtx)
	cancel()
	if err != nil {
		return 0, err
	}
	s.persistTakeoverMu.Lock()
	s.persistTakeoverCached.latest = latest
	s.persistTakeoverCached.at = now
	s.persistTakeoverCached.set = true
	s.persistTakeoverMu.Unlock()
	return latest, nil
}

func (s *Service) takeoverFirstSeen(height uint64, now time.Time) time.Time {
	if s == nil {
		return now
	}
	s.persistTakeoverMu.Lock()
	defer s.persistTakeoverMu.Unlock()
	if first, ok := s.persistTakeoverSeen[height]; ok {
		return first
	}
	s.persistTakeoverSeen[height] = now
	// Keep map bounded while preserving recent takeover windows.
	if len(s.persistTakeoverSeen) > 4096 {
		cutoff := uint64(0)
		if height > 2048 {
			cutoff = height - 2048
		}
		for h := range s.persistTakeoverSeen {
			if h < cutoff {
				delete(s.persistTakeoverSeen, h)
			}
		}
	}
	return now
}

func (s *Service) persistWriterDecisionForBlock(ab *block.AppBlock) persistWriterDecision {
	if ab == nil {
		return persistWriterDecision{allowed: false, role: "unknown", reason: "nil_block"}
	}
	if s == nil {
		return persistWriterDecision{allowed: false, role: "unknown", reason: "nil_service", proposer: ab.Proposer()}
	}
	if s.persistStatusFn != nil {
		if nodeID, ok := s.persistStatusFn(); ok {
			switch s.persistWriterMode {
			case "all":
				return decidePersistFromCommit(false, nodeID, ab.Proposer())
			case "proposer_primary":
				return s.decidePersistWithTakeover(ab, nodeID)
			default:
				return decidePersistFromCommit(true, nodeID, ab.Proposer())
			}
		}
		return persistWriterDecision{allowed: false, role: "unknown", reason: "engine_unavailable", proposer: ab.Proposer()}
	}
	if s.eng == nil {
		return persistWriterDecision{allowed: false, role: "unknown", reason: "engine_unavailable", proposer: ab.Proposer()}
	}
	status := s.eng.GetStatus()
	switch s.persistWriterMode {
	case "all":
		return decidePersistFromCommit(false, status.NodeID, ab.Proposer())
	case "proposer_primary":
		return s.decidePersistWithTakeover(ab, status.NodeID)
	default:
		return decidePersistFromCommit(true, status.NodeID, ab.Proposer())
	}
}

func (s *Service) persistCommittedMetadataOnly(ctx context.Context, ab *block.AppBlock, qc ctypes.QC) error {
	if s == nil || ab == nil || s.persistWorker == nil {
		return nil
	}

	adapter := s.persistWorker.GetAdapter()
	if adapter == nil {
		return fmt.Errorf("persistence adapter not configured")
	}

	bh := ab.GetHash()
	qcData, err := encodeCommittedQC(qc)
	if err != nil {
		return fmt.Errorf("encode committed qc metadata: %w", err)
	}
	metaCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := adapter.SaveCommittedBlock(metaCtx, ab.GetHeight(), bh[:], qcData); err != nil {
		return fmt.Errorf("save committed block metadata: %w", err)
	}
	return nil
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
