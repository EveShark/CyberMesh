package leader

import (
    "context"
    "fmt"
    "sync"
    "time"

    "backend/pkg/consensus/types"
)

// HeartbeatManager handles leader liveness via periodic heartbeats
type HeartbeatManager struct {
	rotation  *Rotation
	pacemaker *Pacemaker
	crypto    CryptoService
	config    *HeartbeatConfig
	audit     AuditLogger
	logger    Logger
	publisher HeartbeatPublisher

	// Liveness tracking
	lastHeartbeat time.Time
	lastProposal  time.Time

	// Sender state (for leader)
	sendTicker *time.Ticker

	// Receiver state (for replicas)
	checkTicker *time.Timer

	mu        sync.RWMutex
	stopCh    chan struct{}
	callbacks HeartbeatCallbacks
}

// HeartbeatConfig contains heartbeat parameters
type HeartbeatConfig struct {
	SendInterval     time.Duration
	MaxIdleTime      time.Duration // Max time without proposal despite heartbeats
	MissedBeforeFail int
	EnableSending    bool
	EnableChecking   bool
	JitterPercent    float64
}

// HeartbeatPublisher publishes leader heartbeat messages to the network
type HeartbeatPublisher interface {
	PublishHeartbeat(ctx context.Context, hb *HeartbeatMsg) error
}

// HeartbeatMsg represents a leader heartbeat
type HeartbeatMsg struct {
	View      uint64
	Height    uint64
	LeaderID  ValidatorID
	Timestamp time.Time
	Signature []byte
}

// DefaultHeartbeatConfig returns secure defaults
func DefaultHeartbeatConfig() *HeartbeatConfig {
	return &HeartbeatConfig{
		SendInterval:     500 * time.Millisecond,
		MaxIdleTime:      3 * time.Second, // 6x send interval
		MissedBeforeFail: 6,
		EnableSending:    true,
		EnableChecking:   true,
		JitterPercent:    0.05, // 5% jitter
	}
}

// NewHeartbeatManager creates a new heartbeat manager
func NewHeartbeatManager(
	rotation *Rotation,
	pacemaker *Pacemaker,
	crypto CryptoService,
	audit AuditLogger,
	logger Logger,
	config *HeartbeatConfig,
	callbacks HeartbeatCallbacks,
	publisher HeartbeatPublisher,
) *HeartbeatManager {
	if config == nil {
		config = DefaultHeartbeatConfig()
	}

	return &HeartbeatManager{
		rotation:      rotation,
		pacemaker:     pacemaker,
		crypto:        crypto,
		config:        config,
		audit:         audit,
		logger:        logger,
		publisher:     publisher,
		lastHeartbeat: time.Now(),
		lastProposal:  time.Time{},
		stopCh:        make(chan struct{}),
		callbacks:     callbacks,
	}
}

// Start begins heartbeat operations
func (hm *HeartbeatManager) Start(ctx context.Context) error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.logger.InfoContext(ctx, "starting heartbeat manager",
		"send_interval", hm.config.SendInterval,
		"max_idle_time", hm.config.MaxIdleTime,
	)

	// Start sender if enabled (for leaders)
	if hm.config.EnableSending {
		hm.sendTicker = time.NewTicker(hm.config.SendInterval)
		go hm.runSender(ctx)
	}

	// Start checker if enabled (for replicas)
	if hm.config.EnableChecking {
		hm.checkTicker = time.NewTimer(hm.config.MaxIdleTime)
		go hm.runChecker(ctx)
	}

	return nil
}

// Stop halts heartbeat operations
func (hm *HeartbeatManager) Stop() error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	close(hm.stopCh)

	if hm.sendTicker != nil {
		hm.sendTicker.Stop()
	}
	if hm.checkTicker != nil {
		hm.checkTicker.Stop()
	}

	return nil
}

// runSender periodically sends heartbeats (leader only)
func (hm *HeartbeatManager) runSender(ctx context.Context) {
	for {
		select {
		case <-hm.stopCh:
			return
		case <-hm.sendTicker.C:
			if err := hm.sendHeartbeat(ctx); err != nil {
				hm.logger.ErrorContext(ctx, "failed to send heartbeat",
					"error", err,
				)
			}
		}
	}
}

// runChecker monitors leader liveness (replicas only)
func (hm *HeartbeatManager) runChecker(ctx context.Context) {
	for {
		select {
		case <-hm.stopCh:
			return
		case <-hm.checkTicker.C:
			if err := hm.checkLiveness(ctx); err != nil {
				hm.logger.ErrorContext(ctx, "liveness check failed",
					"error", err,
				)
			}
			// Reset timer
			hm.checkTicker.Reset(hm.config.MaxIdleTime)
		}
	}
}

// sendHeartbeat creates and broadcasts a heartbeat message
func (hm *HeartbeatManager) sendHeartbeat(ctx context.Context) error {
    hm.mu.RLock()
    view := hm.pacemaker.GetCurrentView()
    height := hm.pacemaker.GetCurrentHeight()
    hm.mu.RUnlock()

	// Check if we're the leader
	isLeader, err := hm.rotation.IsLeader(ctx, hm.crypto.GetKeyID(), view)
	if err != nil {
		return fmt.Errorf("failed to check leader status: %w", err)
	}

	if !isLeader {
		// Not the leader, skip sending
		return nil
	}

	// Create heartbeat message
	hbMsg := &HeartbeatMsg{
		View:      view,
		Height:    height,
		LeaderID:  hm.crypto.GetKeyID(),
		Timestamp: time.Now(),
	}

	// Sign heartbeat
	signBytes := hm.heartbeatSignBytes(hbMsg)
	signature, err := hm.crypto.SignWithContext(ctx, signBytes)
	if err != nil {
		return fmt.Errorf("failed to sign heartbeat: %w", err)
	}
	hbMsg.Signature = signature

    hm.logger.InfoContext(ctx, "heartbeat sent",
		"view", view,
		"height", height,
	)

	// Publish heartbeat
	if hm.publisher != nil {
		_ = hm.publisher.PublishHeartbeat(ctx, hbMsg)
	}

    // Leader does not receive its own heartbeat via pubsub; update local liveness to avoid self-timeouts.
    hm.mu.Lock()
    hm.lastHeartbeat = time.Now()
    if hm.checkTicker != nil {
        hm.checkTicker.Reset(hm.config.MaxIdleTime)
    }
    hm.mu.Unlock()

	return nil
}

// OnHeartbeat processes received heartbeat messages
func (hm *HeartbeatManager) OnHeartbeat(ctx context.Context, hbMsg *HeartbeatMsg) error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	currentView := hm.pacemaker.GetCurrentView()

	// Validate heartbeat is for current view
	if hbMsg.View != currentView {
		return fmt.Errorf("heartbeat for wrong view: got %d, expected %d",
			hbMsg.View, currentView)
	}

	// Verify sender is the leader
	isLeader, err := hm.rotation.IsLeader(ctx, hbMsg.LeaderID, hbMsg.View)
	if err != nil {
		return fmt.Errorf("failed to verify leader: %w", err)
	}
	if !isLeader {
		return fmt.Errorf("heartbeat from non-leader: %x", hbMsg.LeaderID[:8])
	}

	// Update last heartbeat time
	hm.lastHeartbeat = time.Now()

	hm.logger.InfoContext(ctx, "heartbeat received",
		"view", hbMsg.View,
		"leader", fmt.Sprintf("%x", hbMsg.LeaderID[:8]),
	)

	// Reset check timer
	if hm.checkTicker != nil {
		hm.checkTicker.Reset(hm.config.MaxIdleTime)
	}

	// Notify callback
	if hm.callbacks != nil {
		if err := hm.callbacks.OnLeaderAlive(hbMsg.View, hbMsg.LeaderID); err != nil {
			hm.logger.ErrorContext(ctx, "leader alive callback failed",
				"error", err,
			)
		}
	}

	// CRITICAL: Even with heartbeat, check if we've had a proposal recently
	// This prevents a malicious/stuck leader from just sending heartbeats
	// without making progress
	return hm.checkProposalIdleness(ctx)
}

// OnProposal updates last proposal time (called by consensus layer)
func (hm *HeartbeatManager) OnProposal(ctx context.Context) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.lastProposal = time.Now()
	hm.lastHeartbeat = time.Now()

	hm.logger.InfoContext(ctx, "proposal received, liveness confirmed")

	// Reset check timer
	if hm.checkTicker != nil {
		hm.checkTicker.Reset(hm.config.MaxIdleTime)
	}
}

// checkLiveness verifies leader is making progress
func (hm *HeartbeatManager) checkLiveness(ctx context.Context) error {
    hm.mu.RLock()
    defer hm.mu.RUnlock()

    // If we are the current leader, skip heartbeat-missing timeout logic for self.
    // Leader does not receive its own heartbeat via pubsub; liveness should be
    // governed by proposal progress when leader.
    isLeader, err := hm.rotation.IsLeader(ctx, hm.crypto.GetKeyID(), hm.pacemaker.GetCurrentView())
    if err == nil && isLeader {
        return hm.checkProposalIdleness(ctx)
    }

    return hm.checkProposalIdleness(ctx)
}

// checkProposalIdleness verifies proposals are being made despite heartbeats
func (hm *HeartbeatManager) checkProposalIdleness(ctx context.Context) error {
	now := time.Now()
    // If no proposal has been observed in the current view, do not trigger idleness.
    if hm.lastProposal.IsZero() {
        return nil
    }
	timeSinceProposal := now.Sub(hm.lastProposal)
	timeSinceHeartbeat := now.Sub(hm.lastHeartbeat)

	// If we haven't seen a heartbeat at all, trigger timeout
    // Skip this condition if we are the current leader; leaders don't receive their own heartbeats.
    isLeader, _ := hm.rotation.IsLeader(ctx, hm.crypto.GetKeyID(), hm.pacemaker.GetCurrentView())
    if !isLeader && timeSinceHeartbeat > hm.config.MaxIdleTime {
		view := hm.pacemaker.GetCurrentView()

		hm.logger.WarnContext(ctx, "heartbeat timeout detected",
			"view", view,
			"time_since_heartbeat", timeSinceHeartbeat,
			"max_idle", hm.config.MaxIdleTime,
		)

		hm.audit.Warn("heartbeat_timeout", map[string]interface{}{
			"view":                 view,
			"time_since_heartbeat": timeSinceHeartbeat.String(),
			"max_idle_time":        hm.config.MaxIdleTime.String(),
		})

		// Notify callback
		if hm.callbacks != nil {
			if err := hm.callbacks.OnHeartbeatTimeout(view, hm.lastHeartbeat); err != nil {
				return fmt.Errorf("heartbeat timeout callback failed: %w", err)
			}
		}

        // Trigger view change via pacemaker
        return hm.pacemaker.TriggerViewChange(ctx)
	}

	// CRITICAL FIX: Even if heartbeats are recent, check if proposals are stale
	// This prevents a leader from keeping the view alive with just heartbeats
	// without making actual consensus progress
	if timeSinceProposal > hm.config.MaxIdleTime {
		view := hm.pacemaker.GetCurrentView()

		hm.logger.WarnContext(ctx, "proposal idleness detected despite heartbeats",
			"view", view,
			"time_since_proposal", timeSinceProposal,
			"time_since_heartbeat", timeSinceHeartbeat,
			"max_idle", hm.config.MaxIdleTime,
		)

		hm.audit.Warn("proposal_idleness", map[string]interface{}{
			"view":                 view,
			"time_since_proposal":  timeSinceProposal.String(),
			"time_since_heartbeat": timeSinceHeartbeat.String(),
			"max_idle_time":        hm.config.MaxIdleTime.String(),
		})

		// Notify callback
		if hm.callbacks != nil {
			if err := hm.callbacks.OnHeartbeatTimeout(view, hm.lastProposal); err != nil {
				return fmt.Errorf("proposal timeout callback failed: %w", err)
			}
		}

		// Trigger view change - leader is alive but not making progress
		return hm.pacemaker.TriggerViewChange(ctx)
	}

	return nil
}

// GetLastHeartbeat returns time of last heartbeat
func (hm *HeartbeatManager) GetLastHeartbeat() time.Time {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.lastHeartbeat
}

// GetLastProposal returns time of last proposal
func (hm *HeartbeatManager) GetLastProposal() time.Time {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return hm.lastProposal
}

// GetTimeSinceHeartbeat returns duration since last heartbeat
func (hm *HeartbeatManager) GetTimeSinceHeartbeat() time.Duration {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return time.Since(hm.lastHeartbeat)
}

// GetTimeSinceProposal returns duration since last proposal
func (hm *HeartbeatManager) GetTimeSinceProposal() time.Duration {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return time.Since(hm.lastProposal)
}

// IsLeaderLive checks if leader is currently considered live
func (hm *HeartbeatManager) IsLeaderLive() bool {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	timeSinceHeartbeat := time.Since(hm.lastHeartbeat)
	timeSinceProposal := time.Since(hm.lastProposal)

	// Leader is live if either:
	// 1. Recent heartbeat AND recent proposal
	// 2. Very recent heartbeat (within send interval, so proposal might be coming)

	heartbeatFresh := timeSinceHeartbeat < hm.config.SendInterval*2
	proposalFresh := timeSinceProposal < hm.config.MaxIdleTime

	return heartbeatFresh && proposalFresh
}

// ResetLiveness resets liveness timers (useful for view changes)
func (hm *HeartbeatManager) ResetLiveness() {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	now := time.Now()
	hm.lastHeartbeat = now
    // New view: mark as no proposal observed yet to avoid false-positive idleness
    hm.lastProposal = time.Time{}

	if hm.checkTicker != nil {
		hm.checkTicker.Reset(hm.config.MaxIdleTime)
	}
}

// UpdateConfig updates heartbeat configuration
func (hm *HeartbeatManager) UpdateConfig(config *HeartbeatConfig) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.config = config

	// Restart tickers with new intervals
	if hm.sendTicker != nil {
		hm.sendTicker.Stop()
		hm.sendTicker = time.NewTicker(config.SendInterval)
	}
}

// GetConfig returns current configuration (copy)
func (hm *HeartbeatManager) GetConfig() HeartbeatConfig {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return *hm.config
}

// heartbeatSignBytes returns canonical bytes for signing (FIX: proper domain separation)
func (hm *HeartbeatManager) heartbeatSignBytes(hb *HeartbeatMsg) []byte {
	buf := make([]byte, 0, 96)

	// Domain separator
    buf = append(buf, []byte(types.DomainHeartbeat)...)
	buf = append(buf, 0x00) // Separator

	// View and height
	buf = appendUint64(buf, hb.View)
	buf = appendUint64(buf, hb.Height)

	// Leader
	buf = append(buf, hb.LeaderID[:]...)

    // Timestamp: match messages.Heartbeat.SignBytes precision to ensure round-trip
    buf = appendInt64(buf, hb.Timestamp.UnixMilli())

	return buf
}

// GetLivenessStats returns liveness statistics for monitoring
func (hm *HeartbeatManager) GetLivenessStats() LivenessStats {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	return LivenessStats{
		LastHeartbeat:      hm.lastHeartbeat,
		LastProposal:       hm.lastProposal,
		TimeSinceHeartbeat: time.Since(hm.lastHeartbeat),
		TimeSinceProposal:  time.Since(hm.lastProposal),
		IsLive:             hm.isLeaderLiveUnsafe(),
		MaxIdleTime:        hm.config.MaxIdleTime,
	}
}

// LivenessStats contains liveness monitoring data
type LivenessStats struct {
	LastHeartbeat      time.Time
	LastProposal       time.Time
	TimeSinceHeartbeat time.Duration
	TimeSinceProposal  time.Duration
	IsLive             bool
	MaxIdleTime        time.Duration
}

// isLeaderLiveUnsafe checks liveness without lock (internal use)
func (hm *HeartbeatManager) isLeaderLiveUnsafe() bool {
	timeSinceHeartbeat := time.Since(hm.lastHeartbeat)
	timeSinceProposal := time.Since(hm.lastProposal)

	heartbeatFresh := timeSinceHeartbeat < hm.config.SendInterval*2
	proposalFresh := timeSinceProposal < hm.config.MaxIdleTime

	return heartbeatFresh && proposalFresh
}
