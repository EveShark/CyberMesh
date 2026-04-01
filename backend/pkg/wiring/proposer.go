package wiring

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"backend/pkg/block"
	"backend/pkg/consensus/messages"
	"backend/pkg/control/policytrace"
	"backend/pkg/state"
	"backend/pkg/utils"
	"go.uber.org/zap"
)

func effectiveProposalCooldown(
	base time.Duration,
	backlogFast time.Duration,
	policyFast time.Duration,
	txCount int,
	backlogThreshold int,
	hasPolicy bool,
) time.Duration {
	effective := base
	if effective <= 0 {
		return effective
	}
	if backlogFast > 0 && txCount >= backlogThreshold && backlogThreshold > 0 && backlogFast < effective {
		effective = backlogFast
	}
	if policyFast > 0 && hasPolicy && policyFast < effective {
		effective = policyFast
	}
	return effective
}

func shouldEnforceActivePeerGate(requireActivePeers bool, activePeers, requiredPeers int) bool {
	if !requireActivePeers {
		return false
	}
	return activePeers < requiredPeers
}

func (s *Service) runProposer(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("panic in runProposer",
				utils.ZapString("panic", fmt.Sprintf("%v", r)))
		}
	}()

	interval := s.cfg.BuildInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	s.log.InfoContext(ctx, "[PROPOSER] runProposer started", utils.ZapDuration("interval", interval))
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-s.proposeTriggerCh:
			s.tryPropose(ctx)
		case <-t.C:
			s.tryPropose(ctx)
		}
	}
}

func (s *Service) notifyProposeTrigger(source string) {
	if s == nil || s.proposeTriggerCh == nil {
		return
	}
	select {
	case s.proposeTriggerCh <- struct{}{}:
	default:
		dropped := atomic.AddUint64(&s.proposeTriggerDropped, 1)
		if s.log != nil && s.proposeTriggerLogEvery > 0 && dropped%s.proposeTriggerLogEvery == 0 {
			s.log.Debug("proposer trigger dropped (channel full)",
				utils.ZapString("source", source),
				utils.ZapUint64("dropped", dropped))
		}
	}
}

func (s *Service) tryPropose(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("panic in tryPropose",
				utils.ZapString("panic", fmt.Sprintf("%v", r)))
		}
	}()

	// Avoid duplicate proposals in the same view (HotStuff votes once per view)
	currentView := s.eng.GetCurrentView()
	currentHeight := s.eng.GetCurrentHeight()
	blockTimeout := s.blockTimeout
	if blockTimeout <= 0 {
		blockTimeout = 5 * time.Second
	}
	cooldownWindow := s.proposalCooldown
	if cooldownWindow <= 0 {
		cooldownWindow = blockTimeout
	}

	if !s.eng.IsConsensusActive() {
		s.log.DebugContext(ctx, "skipping proposal: consensus not yet active",
			utils.ZapUint64("view", currentView),
			utils.ZapUint64("height", currentHeight))
		return
	}

	if !s.eng.IsLocalValidatorReady() {
		s.log.DebugContext(ctx, "skipping proposal: local validator not ready",
			utils.ZapUint64("view", currentView),
			utils.ZapUint64("height", currentHeight))
		return
	}

	txCount, _ := s.mp.Stats()
	hasPolicy := s.mp.HasPolicyTx()

	s.mu.Lock()
	lastView := s.lastProposedView
	lastHeight := s.lastProposedHeight
	lastTime := s.lastProposalTime
	s.mu.Unlock()
	effectiveCooldown := effectiveProposalCooldown(
		cooldownWindow,
		s.backlogCooldown,
		s.policyFastCooldown,
		txCount,
		s.backlogTxThreshold,
		hasPolicy,
	)

	s.log.DebugContext(ctx, "tryPropose state",
		utils.ZapUint64("current_view", currentView),
		utils.ZapUint64("current_height", currentHeight),
		utils.ZapUint64("last_view", lastView),
		utils.ZapUint64("last_height", lastHeight),
		utils.ZapDuration("block_timeout", blockTimeout),
		utils.ZapDuration("effective_cooldown", effectiveCooldown),
		utils.ZapInt("mempool_txs", txCount),
		utils.ZapBool("has_policy_tx", hasPolicy),
		utils.ZapTime("last_proposal_time", lastTime))

	if lastView == currentView && !lastTime.IsZero() {
		elapsed := time.Since(lastTime)
		if elapsed < effectiveCooldown {
			cooldown := effectiveCooldown - elapsed
			if isLeader, err := s.eng.IsLeader(ctx); err == nil && isLeader {
				s.log.DebugContext(ctx, "skipping proposal: leader cooldown active",
					utils.ZapUint64("view", currentView),
					utils.ZapUint64("height", currentHeight),
					utils.ZapUint64("last_height", lastHeight),
					utils.ZapDuration("configured_cooldown", cooldownWindow),
					utils.ZapDuration("effective_cooldown", effectiveCooldown),
					utils.ZapDuration("cooldown_remaining", cooldown))
			} else {
				s.log.DebugContext(ctx, "skipping proposal: cooldown active",
					utils.ZapUint64("view", currentView),
					utils.ZapUint64("height", currentHeight),
					utils.ZapUint64("last_height", lastHeight),
					utils.ZapDuration("configured_cooldown", cooldownWindow),
					utils.ZapDuration("effective_cooldown", effectiveCooldown),
					utils.ZapDuration("cooldown_remaining", cooldown),
					utils.ZapBool("leader_status_unknown", err != nil))
			}
			return
		}
	}

	s.log.InfoContext(ctx, "[PROPOSER] tryPropose called",
		utils.ZapUint64("view", currentView),
		utils.ZapUint64("height", currentHeight))

	// Wait for genesis grace period to allow cluster formation
	if currentHeight == 0 {
		elapsed := time.Since(s.startTime)
		if elapsed < s.genesisGracePeriod {
			remaining := s.genesisGracePeriod - elapsed
			s.log.InfoContext(ctx, "skipping proposal: genesis grace period active",
				utils.ZapDuration("elapsed", elapsed),
				utils.ZapDuration("grace_period", s.genesisGracePeriod),
				utils.ZapDuration("remaining", remaining))
			return
		}
		s.log.InfoContext(ctx, "genesis grace period complete",
			utils.ZapDuration("elapsed", elapsed))
	}

	// Verify Byzantine quorum before proposing
	if s.router != nil {
		// Get validator count to calculate Byzantine quorum
		validators := s.eng.ListValidators()
		totalValidators := len(validators)
		overrideQuorum := false

		// Calculate Byzantine fault tolerance: f = (N-1)/3, quorum = 2f+1
		var requiredQuorum int
		if totalValidators > 1 {
			f := (totalValidators - 1) / 3
			requiredQuorum = 2*f + 1
		} else {
			// Single validator deployment (dev/test only)
			requiredQuorum = 1
		}

		// For P2P connectivity, we need requiredQuorum-1 peers (since we don't count ourselves)
		requiredPeers := requiredQuorum - 1

		connectedPeers := s.router.GetConnectedPeerCount()
		if connectedPeers < requiredPeers {
			if s.cfg.AllowSoloProposal {
				s.log.WarnContext(ctx, "peer quorum override enabled - proceeding without required peers",
					utils.ZapInt("connected_peers", connectedPeers),
					utils.ZapInt("required_peers", requiredPeers),
					utils.ZapInt("total_validators", totalValidators),
					utils.ZapInt("byzantine_f", (totalValidators-1)/3),
					utils.ZapInt("quorum_2f+1", requiredQuorum),
					utils.ZapUint64("view", currentView))
				overrideQuorum = true
			} else {
				s.log.WarnContext(ctx, "skipping proposal: insufficient peer quorum",
					utils.ZapInt("connected_peers", connectedPeers),
					utils.ZapInt("required_peers", requiredPeers),
					utils.ZapInt("total_validators", totalValidators),
					utils.ZapInt("byzantine_f", (totalValidators-1)/3),
					utils.ZapInt("quorum_2f+1", requiredQuorum),
					utils.ZapUint64("view", currentView))
				return
			}
		}

		// Optionally require active peers in addition to connected peers.
		// Default is liveness-first: connected quorum is enough to keep proposals flowing.
		activePeers := s.router.GetActivePeerCount(20 * time.Second)
		if shouldEnforceActivePeerGate(s.cfg.RequireActivePeers, activePeers, requiredPeers) {
			if s.cfg.AllowSoloProposal {
				s.log.WarnContext(ctx, "peer activity override enabled - proceeding despite inactive peers",
					utils.ZapInt("connected_peers", connectedPeers),
					utils.ZapInt("active_peers", activePeers),
					utils.ZapInt("required_peers", requiredPeers),
					utils.ZapInt("total_validators", totalValidators),
					utils.ZapUint64("view", currentView))
				overrideQuorum = true
			} else {
				s.log.WarnContext(ctx, "skipping proposal: peers connected but inactive",
					utils.ZapInt("connected_peers", connectedPeers),
					utils.ZapInt("active_peers", activePeers),
					utils.ZapInt("required_peers", requiredPeers),
					utils.ZapInt("total_validators", totalValidators),
					utils.ZapUint64("view", currentView))
				return
			}
		} else if activePeers < requiredPeers {
			s.log.WarnContext(ctx, "active peer quorum below target but not enforced",
				utils.ZapInt("connected_peers", connectedPeers),
				utils.ZapInt("active_peers", activePeers),
				utils.ZapInt("required_peers", requiredPeers),
				utils.ZapInt("total_validators", totalValidators),
				utils.ZapBool("require_active_peers", s.cfg.RequireActivePeers),
				utils.ZapUint64("view", currentView))
		}

		msg := "quorum check passed"
		if overrideQuorum {
			msg = "quorum check override active"
		}
		s.log.InfoContext(ctx, msg,
			utils.ZapInt("connected_peers", connectedPeers),
			utils.ZapInt("active_peers", activePeers),
			utils.ZapInt("required_quorum", requiredQuorum),
			utils.ZapInt("total_validators", totalValidators),
			utils.ZapBool("override", overrideQuorum))
	}

	// Check leadership
	isLeader, err := s.eng.IsLeader(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "[PROPOSER] leadership check failed", utils.ZapError(err))
		return
	}
	s.log.InfoContext(ctx, "[PROPOSER] leadership check result",
		utils.ZapBool("is_leader", isLeader),
		utils.ZapUint64("view", currentView))
	if !isLeader {
		return
	}

	if eligible, reason := s.eng.IsLocalLeaderEligible(ctx, currentView); !eligible {
		s.log.WarnContext(ctx, "skipping proposal: leader ineligible to propose",
			utils.ZapUint64("view", currentView),
			utils.ZapUint64("height", currentHeight),
			utils.ZapString("reason", reason))
		return
	}

	if err := s.eng.EnsureProposalQuorum(ctx, currentView, currentHeight); err != nil {
		s.log.WarnContext(ctx, "skipping proposal: proposal quorum handshake incomplete",
			utils.ZapError(err),
			utils.ZapUint64("view", currentView),
			utils.ZapUint64("height", currentHeight))
		return
	}

	// Check mempool threshold and update metrics
	count, sizeBytes := s.mp.Stats()
	s.metrics.UpdateMempoolStats(count, sizeBytes)
	s.log.InfoContext(ctx, "[PROPOSER] I AM LEADER - checking mempool",
		utils.ZapUint64("view", currentView),
		utils.ZapUint64("last_proposed_view", lastView),
		utils.ZapInt("mempool_txs", count),
		utils.ZapInt("min_required", s.cfg.MinMempoolTxs))
	if s.cfg.MinMempoolTxs > 0 && count < s.cfg.MinMempoolTxs {
		s.log.InfoContext(ctx, "[PROPOSER] mempool below threshold - skipping proposal")
		return
	}

	// Determine next height and parent hash
	// Pacemaker already advances to QC.Height+1 in OnQC, so use current height directly
	height := currentHeight
	if height == 0 {
		height = s.eng.GetCurrentHeight()
	}
	s.mu.Lock()
	parent := s.lastParent
	s.mu.Unlock()

	s.log.InfoContext(ctx, "[PROPOSER] building block",
		utils.ZapUint64("height", height),
		utils.ZapString("parent_hash", fmt.Sprintf("%x", parent[:8])))
	exclusions := s.snapshotProposedTxExclusions(time.Now())
	blk := s.builder.BuildWithExclusions(height, parent, s.eng.GetStatus().NodeID, time.Now(), exclusions)
	if blk == nil || blk.GetTransactionCount() == 0 {
		s.log.InfoContext(ctx, "[PROPOSER] block builder returned nil or empty - skipping",
			utils.ZapInt("excluded_tx_count", len(exclusions)))
		return
	}
	blk, replayRejected := s.filterCommittedTxFromBlock(blk, time.Now())
	if replayRejected > 0 {
		s.log.InfoContext(ctx, "[PROPOSER] replay filter removed already-committed txs from proposal",
			utils.ZapInt("removed", replayRejected),
			utils.ZapUint64("height", height))
	}
	if blk == nil || blk.GetTransactionCount() == 0 {
		s.log.InfoContext(ctx, "[PROPOSER] replay-filtered block is empty - skipping",
			utils.ZapInt("removed", replayRejected),
			utils.ZapUint64("height", height))
		return
	}

	s.log.InfoContext(ctx, "[PROPOSER] block built successfully",
		utils.ZapInt("tx_count", blk.GetTransactionCount()))
	s.logPolicyStageForBlock("t_leader_selected", blk, currentView, height, 0)

	// Submit block with retry
	s.metrics.IncrementProposalsAttempted()
	start := time.Now()
	attemptTime := start
	// Record attempt metadata immediately so cooldown/dedup logic suppresses repeated retries in the same view
	s.mu.Lock()
	s.lastProposedView = currentView
	s.lastProposedHeight = height
	s.lastProposalTime = attemptTime
	s.mu.Unlock()
	s.log.InfoContext(ctx, "[PROPOSER] submitting block to consensus engine",
		utils.ZapUint64("height", height),
		utils.ZapInt("tx_count", blk.GetTransactionCount()))
	s.logPolicyStageForBlock("t_propose_start", blk, currentView, height, 0)
	const maxRetries = 3
	backoff := 50 * time.Millisecond
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := s.eng.SubmitBlock(ctx, blk); err != nil {
			if attempt < maxRetries-1 {
				s.log.WarnContext(ctx, "[PROPOSER] submit block failed, retrying",
					utils.ZapError(err),
					utils.ZapUint64("height", height),
					utils.ZapInt("attempt", attempt+1))
				time.Sleep(backoff)
				backoff *= 2 // exponential backoff
				continue
			}
			// Final attempt failed
			s.metrics.IncrementProposalsFailed()
			s.log.ErrorContext(ctx, "[PROPOSER] submit block failed after retries",
				utils.ZapError(err),
				utils.ZapUint64("height", height),
				utils.ZapInt("attempts", maxRetries))
			return
		}
		// Success
		now := time.Now()
		s.logPolicyStageForBlock("t_proposal_broadcast", blk, currentView, height, 0)
		s.log.InfoContext(ctx, "[PROPOSER] block submission SUCCESS",
			utils.ZapUint64("height", height),
			utils.ZapDuration("elapsed", now.Sub(start)))
		s.recordProposedTxHold(blk, now)
		s.mu.Lock()
		// Update last proposal metadata with the success timestamp only; committed parent updates happen in onCommit.
		s.lastProposalTime = now
		s.mu.Unlock()
		s.metrics.IncrementProposalsSucceeded()
		s.metrics.RecordProposalDuration(time.Since(start))
		s.log.InfoContext(ctx, "block submitted successfully",
			utils.ZapUint64("height", height),
			utils.ZapInt("tx_count", blk.GetTransactionCount()))
		return
	}
}

func (s *Service) logPolicyStageForBlock(stage string, blk *block.AppBlock, view uint64, height uint64, qcTsMs int64) {
	if s == nil || s.log == nil || blk == nil {
		return
	}
	nowMs := time.Now().UnixMilli()
	stageTsMs := nowMs
	if stage == "t_qc_formed" && qcTsMs > 0 {
		stageTsMs = qcTsMs
	}
	for _, tx := range blk.Transactions() {
		if tx == nil || tx.Type() != state.TxPolicy {
			continue
		}
		policyID, traceID := extractPolicyIdentityFromPayload(tx.Payload())
		if policyID == "" {
			continue
		}
		zf := []zap.Field{
			utils.ZapString("stage", stage),
			utils.ZapString("policy_id", policyID),
			utils.ZapString("trace_id", traceID),
			utils.ZapInt64("t_ms", stageTsMs),
			utils.ZapUint64("height", height),
			utils.ZapUint64("view", view),
		}
		if qcTsMs > 0 {
			zf = append(zf, utils.ZapInt64("qc_ts_ms", qcTsMs))
		}
		s.log.Info("policy stage marker", zf...)
		if s.policyTraceCollector != nil {
			s.policyTraceCollector.Record(policytrace.Marker{
				Stage:       stage,
				PolicyID:    policyID,
				TraceID:     traceID,
				TimestampMs: stageTsMs,
				Height:      height,
				View:        view,
				QCTsMs:      qcTsMs,
			})
		}
	}
}

func extractPolicyIdentityFromPayload(payload []byte) (string, string) {
	if len(payload) == 0 {
		return "", ""
	}
	var root map[string]interface{}
	if err := json.Unmarshal(payload, &root); err != nil {
		return "", ""
	}
	policyID := strings.TrimSpace(stringFromMap(root, "policy_id"))
	traceID := strings.TrimSpace(stringFromMap(root, "trace_id"))
	if traceID == "" {
		if metadata, ok := root["metadata"].(map[string]interface{}); ok {
			traceID = strings.TrimSpace(stringFromMap(metadata, "trace_id"))
		}
	}
	if traceID == "" {
		if trace, ok := root["trace"].(map[string]interface{}); ok {
			traceID = strings.TrimSpace(stringFromMap(trace, "id"))
		}
	}
	if wrapped, ok := root["params"].(map[string]interface{}); ok {
		if policyID == "" {
			policyID = strings.TrimSpace(stringFromMap(wrapped, "policy_id"))
		}
		if traceID == "" {
			traceID = strings.TrimSpace(stringFromMap(wrapped, "trace_id"))
		}
		if traceID == "" {
			if metadata, ok := wrapped["metadata"].(map[string]interface{}); ok {
				traceID = strings.TrimSpace(stringFromMap(metadata, "trace_id"))
			}
		}
		if traceID == "" {
			if trace, ok := wrapped["trace"].(map[string]interface{}); ok {
				traceID = strings.TrimSpace(stringFromMap(trace, "id"))
			}
		}
	}
	return policyID, traceID
}

func stringFromMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func (s *Service) snapshotProposedTxExclusions(now time.Time) map[[32]byte]struct{} {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.proposedTxHold) == 0 {
		return nil
	}
	for hash, until := range s.proposedTxHold {
		if now.After(until) {
			delete(s.proposedTxHold, hash)
		}
	}
	if len(s.proposedTxHold) == 0 {
		return nil
	}
	out := make(map[[32]byte]struct{}, len(s.proposedTxHold))
	for hash := range s.proposedTxHold {
		out[hash] = struct{}{}
	}
	return out
}

func (s *Service) recordProposedTxHold(blk *block.AppBlock, now time.Time) {
	if s == nil || blk == nil || s.proposalTxHoldDuration <= 0 {
		return
	}
	until := now.Add(s.proposalTxHoldDuration)
	s.recordTxHoldUntil(blk.Transactions(), until)
}

func (s *Service) recordObservedProposalTxHold(proposal interface{}, now time.Time) {
	if s == nil || s.proposalTxHoldDuration <= 0 || proposal == nil {
		return
	}

	var txs []state.Transaction
	switch p := proposal.(type) {
	case *messages.Proposal:
		if p != nil {
			txs = proposalTransactions(p.Block)
		}
	case messages.Proposal:
		txs = proposalTransactions(p.Block)
	}
	if len(txs) == 0 {
		return
	}

	s.recordTxHoldUntil(txs, now.Add(s.proposalTxHoldDuration))
}

func proposalTransactions(blk interface{}) []state.Transaction {
	if blk == nil {
		return nil
	}
	if appBlk, ok := blk.(*block.AppBlock); ok && appBlk != nil {
		return appBlk.Transactions()
	}
	if txSource, ok := blk.(interface{ Transactions() []state.Transaction }); ok {
		return txSource.Transactions()
	}
	return nil
}

func (s *Service) recordTxHoldUntil(txs []state.Transaction, until time.Time) {
	if s == nil || len(txs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tx := range txs {
		if tx == nil || tx.Envelope() == nil {
			continue
		}
		s.proposedTxHold[tx.Envelope().ContentHash] = until
	}
}

func (s *Service) clearProposedTxHold(hashes ...[32]byte) {
	if s == nil || len(hashes) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, hash := range hashes {
		delete(s.proposedTxHold, hash)
	}
}
