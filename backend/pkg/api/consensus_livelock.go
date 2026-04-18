package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"backend/pkg/utils"
)

func (s *Server) evaluateConsensusLivelock(now time.Time) (bool, string, float64) {
	if s == nil || s.engine == nil || s.mempool == nil || !s.config.ConsensusLivelockDetectorEnabled {
		if s != nil {
			s.consensusLivelockActive.Store(false)
			s.consensusLivelockLastReason.Store("")
			s.consensusLivelockNoCommitMs.Store(0)
		}
		return false, "", 0
	}

	status := s.engine.GetStatus()
	if !status.Running || !s.engine.IsConsensusActive() {
		s.consensusLivelockMu.Lock()
		s.consensusLivelockSuspectSince = time.Time{}
		s.consensusLivelockLastSampleAt = now
		s.consensusLivelockLastCommitted = status.Metrics.BlocksCommitted
		s.consensusLivelockLastViewChanges = status.Metrics.ViewChanges
		s.consensusLivelockMu.Unlock()
		s.consensusLivelockActive.Store(false)
		s.consensusLivelockLastReason.Store("")
		s.consensusLivelockNoCommitMs.Store(0)
		_ = s.persistConsensusLivelockState(now, false, "consensus not active")
		return false, "", 0
	}

	mempoolCount, _, oldestTs := s.mempool.StatsDetailed()
	oldestAge := time.Duration(0)
	if oldestTs > 0 {
		oldestAge = now.Sub(time.Unix(oldestTs, 0))
		if oldestAge < 0 {
			oldestAge = 0
		}
	}

	s.consensusLivelockMu.Lock()
	defer s.consensusLivelockMu.Unlock()

	if s.consensusLivelockLastSampleAt.IsZero() {
		s.consensusLivelockLastSampleAt = now
		s.consensusLivelockLastCommitted = status.Metrics.BlocksCommitted
		s.consensusLivelockLastViewChanges = status.Metrics.ViewChanges
		s.consensusLivelockActive.Store(false)
		s.consensusLivelockLastReason.Store("")
		s.consensusLivelockNoCommitMs.Store(0)
		return false, "", 0
	}

	commitsDelta := uint64(0)
	if status.Metrics.BlocksCommitted >= s.consensusLivelockLastCommitted {
		commitsDelta = status.Metrics.BlocksCommitted - s.consensusLivelockLastCommitted
	}
	viewsDelta := uint64(0)
	if status.Metrics.ViewChanges >= s.consensusLivelockLastViewChanges {
		viewsDelta = status.Metrics.ViewChanges - s.consensusLivelockLastViewChanges
	}

	s.consensusLivelockLastSampleAt = now
	s.consensusLivelockLastCommitted = status.Metrics.BlocksCommitted
	s.consensusLivelockLastViewChanges = status.Metrics.ViewChanges

	if commitsDelta > 0 {
		s.consensusLivelockSuspectSince = time.Time{}
		s.consensusLivelockActive.Store(false)
		s.consensusLivelockLastReason.Store("")
		s.consensusLivelockNoCommitMs.Store(0)
		_ = s.persistConsensusLivelockState(now, false, "commit progress resumed")
		return false, "", 0
	}

	signal := viewsDelta >= s.config.ConsensusLivelockMinViewChanges &&
		mempoolCount >= s.config.ConsensusLivelockMinMempoolTxs &&
		oldestAge >= s.config.ConsensusLivelockMinOldestTxAge
	if !signal {
		s.consensusLivelockSuspectSince = time.Time{}
		s.consensusLivelockActive.Store(false)
		s.consensusLivelockLastReason.Store("")
		s.consensusLivelockNoCommitMs.Store(0)
		_ = s.persistConsensusLivelockState(now, false, "signal not sustained")
		return false, "", 0
	}

	if s.consensusLivelockSuspectSince.IsZero() {
		s.consensusLivelockSuspectSince = now
	}
	noCommitDuration := now.Sub(s.consensusLivelockSuspectSince)
	if noCommitDuration < s.config.ConsensusLivelockNoCommitWindow {
		noCommitSeconds := noCommitDuration.Seconds()
		if noCommitSeconds < 0 {
			noCommitSeconds = 0
		}
		s.consensusLivelockNoCommitMs.Store(uint64(noCommitDuration / time.Millisecond))
		return false, "", noCommitSeconds
	}

	reason := fmt.Sprintf(
		"no commits %.0fs with view_changes_delta=%d mempool_txs=%d oldest_tx_age=%.0fs",
		noCommitDuration.Seconds(),
		viewsDelta,
		mempoolCount,
		oldestAge.Seconds(),
	)
	wasActive := s.consensusLivelockActive.Load()
	s.consensusLivelockActive.Store(true)
	s.consensusLivelockLastReason.Store(reason)
	if !wasActive {
		s.consensusLivelockDetectedTotal.Add(1)
		if s.logger != nil {
			s.logger.Error("consensus livelock detected",
				utils.ZapString("reason", reason),
				utils.ZapUint64("height", status.Height),
				utils.ZapUint64("view", status.View),
				utils.ZapUint64("blocks_committed", status.Metrics.BlocksCommitted),
				utils.ZapUint64("view_changes", status.Metrics.ViewChanges))
		}
		if s.audit != nil {
			_ = s.audit.Critical("consensus_livelock_detected", map[string]interface{}{
				"reason":              reason,
				"height":              status.Height,
				"view":                status.View,
				"blocks_committed":    status.Metrics.BlocksCommitted,
				"view_changes":        status.Metrics.ViewChanges,
				"mempool_txs":         mempoolCount,
				"oldest_tx_age_secs":  oldestAge.Seconds(),
				"no_commit_window_ms": s.config.ConsensusLivelockNoCommitWindow.Milliseconds(),
			})
		}
	}
	_ = s.persistConsensusLivelockState(now, true, reason)
	s.consensusLivelockNoCommitMs.Store(uint64(noCommitDuration / time.Millisecond))
	return true, reason, noCommitDuration.Seconds()
}

func (s *Server) persistConsensusLivelockState(now time.Time, active bool, reason string) error {
	if s == nil || !s.config.ConsensusLivelockPersistState || s.storage == nil {
		return nil
	}
	interval := s.config.ConsensusLivelockPersistInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	stateKey := strings.TrimSpace(s.config.ConsensusLivelockStateKey)
	if stateKey == "" {
		return nil
	}

	s.consensusLivelockPersistMu.Lock()
	lastPersistAt := s.consensusLivelockLastPersistAt
	lastPersistSet := s.consensusLivelockLastPersistSet.Load()
	lastPersistFlag := s.consensusLivelockLastPersistFlag.Load()
	s.consensusLivelockPersistMu.Unlock()
	if lastPersistAt.Add(interval).After(now) && lastPersistSet && lastPersistFlag == active {
		return nil
	}
	db, err := s.getDB()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reasonCode := "consensus_livelock_clear"
	if active {
		reasonCode = "consensus_livelock_detected"
	}
	_, err = db.ExecContext(ctx, `
		UPSERT INTO control_runtime_state (state_key, enabled, reason_code, reason_text, updated_at)
		VALUES ($1, $2, $3, $4, now())
	`, stateKey, active, reasonCode, reason)
	if err != nil {
		return err
	}
	s.consensusLivelockPersistMu.Lock()
	s.consensusLivelockLastPersistAt = now
	s.consensusLivelockLastPersistSet.Store(true)
	s.consensusLivelockLastPersistFlag.Store(active)
	s.consensusLivelockPersistMu.Unlock()
	return nil
}

func (s *Server) consensusLivelockSnapshot(now time.Time) (bool, string, float64) {
	if s == nil {
		return false, "", 0
	}
	active := s.consensusLivelockActive.Load()
	reason, _ := s.consensusLivelockLastReason.Load().(string)
	noCommitSeconds := float64(s.consensusLivelockNoCommitMs.Load()) / 1000.0
	if !active {
		return false, "", noCommitSeconds
	}
	return true, reason, noCommitSeconds
}

func (s *Server) runConsensusLivelockMonitor() {
	defer s.wg.Done()
	interval := 1 * time.Second
	if s != nil && s.config != nil && s.config.ConsensusLivelockMonitorInterval > 0 {
		interval = s.config.ConsensusLivelockMonitorInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			_, _, _ = s.evaluateConsensusLivelock(now)
		}
	}
}
