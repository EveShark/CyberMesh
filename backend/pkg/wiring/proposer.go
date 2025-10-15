package wiring

import (
	"context"
	"fmt"
	"time"

	"backend/pkg/utils"
)

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
		case <-t.C:
			s.tryPropose(ctx)
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

	s.mu.Lock()
	lastView := s.lastProposedView
	lastHeight := s.lastProposedHeight
	lastTime := s.lastProposalTime
	s.mu.Unlock()

	s.log.DebugContext(ctx, "tryPropose state",
		utils.ZapUint64("current_view", currentView),
		utils.ZapUint64("current_height", currentHeight),
		utils.ZapUint64("last_view", lastView),
		utils.ZapUint64("last_height", lastHeight),
		utils.ZapDuration("block_timeout", blockTimeout),
		utils.ZapTime("last_proposal_time", lastTime))

	if lastView == currentView && !lastTime.IsZero() {
		elapsed := time.Since(lastTime)
		if elapsed < blockTimeout {
			cooldown := blockTimeout - elapsed
			if isLeader, err := s.eng.IsLeader(ctx); err == nil && isLeader {
				s.log.DebugContext(ctx, "skipping proposal: leader cooldown active",
					utils.ZapUint64("view", currentView),
					utils.ZapUint64("height", currentHeight),
					utils.ZapUint64("last_height", lastHeight),
					utils.ZapDuration("cooldown_remaining", cooldown))
			} else {
				s.log.DebugContext(ctx, "skipping proposal: cooldown active",
					utils.ZapUint64("view", currentView),
					utils.ZapUint64("height", currentHeight),
					utils.ZapUint64("last_height", lastHeight),
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
			s.log.WarnContext(ctx, "skipping proposal: insufficient peer quorum",
				utils.ZapInt("connected_peers", connectedPeers),
				utils.ZapInt("required_peers", requiredPeers),
				utils.ZapInt("total_validators", totalValidators),
				utils.ZapInt("byzantine_f", (totalValidators-1)/3),
				utils.ZapInt("quorum_2f+1", requiredQuorum),
				utils.ZapUint64("view", currentView))
			return
		}

		// Verify peers are active, not just connected
		activePeers := s.router.GetActivePeerCount(20 * time.Second)
		if activePeers < requiredPeers {
			s.log.WarnContext(ctx, "skipping proposal: peers connected but inactive",
				utils.ZapInt("connected_peers", connectedPeers),
				utils.ZapInt("active_peers", activePeers),
				utils.ZapInt("required_peers", requiredPeers),
				utils.ZapInt("total_validators", totalValidators),
				utils.ZapUint64("view", currentView))
			return
		}

		s.log.InfoContext(ctx, "quorum check passed",
			utils.ZapInt("connected_peers", connectedPeers),
			utils.ZapInt("active_peers", activePeers),
			utils.ZapInt("required_quorum", requiredQuorum),
			utils.ZapInt("total_validators", totalValidators))
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
	blk := s.builder.Build(height, parent, s.eng.GetStatus().NodeID, time.Now())
	if blk == nil || blk.GetTransactionCount() == 0 {
		s.log.InfoContext(ctx, "[PROPOSER] block builder returned nil or empty - skipping")
		return
	}

	s.log.InfoContext(ctx, "[PROPOSER] block built successfully",
		utils.ZapInt("tx_count", blk.GetTransactionCount()))

	// Submit block with retry
	s.metrics.IncrementProposalsAttempted()
	start := time.Now()
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
		s.mu.Lock()
		// Update last proposal metadata so cooldown logic has accurate view
		s.lastProposedView = currentView
		s.lastProposedHeight = height
		s.lastProposalTime = now
		// Optional consistency improvement: optimistically set lastParent to the
		// just-submitted block hash so subsequent proposals reference it as parent
		// even before commit (will be confirmed/overwritten on commit).
		s.lastParent = blk.GetHash()
		s.mu.Unlock()
		s.metrics.IncrementProposalsSucceeded()
		s.metrics.RecordProposalDuration(time.Since(start))
		s.log.InfoContext(ctx, "block submitted successfully",
			utils.ZapUint64("height", height),
			utils.ZapInt("tx_count", blk.GetTransactionCount()))
		return
	}
}
