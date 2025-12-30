package leader

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"time"

	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
)

// Use domain separators from types to avoid drift

type catchupReason string

const (
	catchupReasonQCAdvance catchupReason = "qc_advance"
	catchupReasonStalePeer catchupReason = "stale_peer"
)

const (
	catchupResendBaseInterval = 1 * time.Second // Target <2s recovery
	maxCatchupRetries         = 5
	stalePeerWindow           = 30 * time.Second // Track recent stale peers
)

// viewRetryState tracks retry attempts per view for circuit breaking
type viewRetryState struct {
	attempts  int
	firstTry  time.Time
	lastRetry time.Time
}

// Pacemaker manages view synchronization and timing for HotStuff consensus
type Pacemaker struct {
	rotation     *Rotation
	validatorSet ValidatorSet
	config       *PacemakerConfig
	crypto       CryptoService
	audit        AuditLogger
	logger       Logger
	publisher    PacemakerPublisher

	// State
	currentView   uint64
	currentHeight uint64
	highestQC     QC
	viewTimer     *time.Timer
	timeout       time.Duration

	// View change tracking
	viewChanges          map[uint64]map[ValidatorID]*ViewChangeMsg
	lastCatchupBroadcast map[uint64]time.Time
	retryState           map[uint64]viewRetryState
	staleViewChanges     map[uint64]time.Time // Track when we receive stale view changes
	newViewSent          bool

	// Diagnostic tracking
	startTime          time.Time // Consensus start time for first proposal tracking
	lastViewTime       time.Time // Last view change time for progression rate
	circuitBreakerHits int       // Total circuit breaker triggers

	mu        sync.RWMutex
	stopCh    chan struct{}
	stopped   bool
	callbacks types.PacemakerCallbacks
}

// PacemakerConfig contains timing parameters
type PacemakerConfig struct {
	BaseTimeout           time.Duration
	MaxTimeout            time.Duration
	MinTimeout            time.Duration
	TimeoutIncreaseFactor float64
	TimeoutDecreaseDelta  time.Duration
	JitterPercent         float64
	ViewChangeTimeout     time.Duration
	EnableAIMD            bool
	RequireQuorum         bool
}

// PacemakerPublisher publishes view-change and new-view messages to the network
type PacemakerPublisher interface {
	PublishViewChange(ctx context.Context, vc *ViewChangeMsg) error
	PublishNewView(ctx context.Context, nv *NewViewMsg) error
}

// ViewChangeMsg represents a view change message
type ViewChangeMsg struct {
	OldView   uint64
	NewView   uint64
	HighestQC QC
	SenderID  ValidatorID
	Timestamp time.Time
	Signature []byte
}

// NewViewMsg represents a new view announcement
type NewViewMsg struct {
	View        uint64
	ViewChanges []*ViewChangeMsg
	HighestQC   QC
	LeaderID    ValidatorID
	Timestamp   time.Time
	Signature   []byte
}

// DefaultPacemakerConfig returns secure defaults for HotStuff
func DefaultPacemakerConfig() *PacemakerConfig {
	return &PacemakerConfig{
		BaseTimeout:           10 * time.Second, // Increased from 2s to 10s for container startup variance
		MaxTimeout:            60 * time.Second,
		MinTimeout:            5 * time.Second,        // Increased from 1s to 5s
		TimeoutIncreaseFactor: 1.5,                    // Multiplicative increase
		TimeoutDecreaseDelta:  200 * time.Millisecond, // Additive decrease
		JitterPercent:         0.1,                    // 10% jitter
		ViewChangeTimeout:     30 * time.Second,
		EnableAIMD:            true,
		RequireQuorum:         true,
	}
}

// NewPacemaker creates a new HotStuff pacemaker
func NewPacemaker(
	rotation *Rotation,
	validatorSet ValidatorSet,
	crypto CryptoService,
	audit AuditLogger,
	logger Logger,
	config *PacemakerConfig,
	callbacks types.PacemakerCallbacks,
	publisher PacemakerPublisher,
) *Pacemaker {
	if config == nil {
		config = DefaultPacemakerConfig()
	}

	return &Pacemaker{
		rotation:             rotation,
		validatorSet:         validatorSet,
		config:               config,
		crypto:               crypto,
		audit:                audit,
		logger:               logger,
		publisher:            publisher,
		currentView:          0,
		currentHeight:        0,
		timeout:              config.BaseTimeout,
		viewChanges:          make(map[uint64]map[ValidatorID]*ViewChangeMsg),
		lastCatchupBroadcast: make(map[uint64]time.Time),
		retryState:           make(map[uint64]viewRetryState),
		staleViewChanges:     make(map[uint64]time.Time),
		stopCh:               make(chan struct{}),
		callbacks:            callbacks,
	}
}

// Start begins the pacemaker timer
func (pm *Pacemaker) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.startTime = time.Now()
	pm.lastViewTime = pm.startTime

	pm.logger.InfoContext(ctx, "starting pacemaker",
		"view", pm.currentView,
		"timeout", pm.timeout,
		"start_time", pm.startTime,
	)

	pm.resetTimerLocked()

	go pm.run(ctx)

	return nil
}

// Stop halts the pacemaker
func (pm *Pacemaker) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.stopped {
		if pm.viewTimer != nil {
			pm.viewTimer.Stop()
		}
		return nil
	}

	pm.stopped = true
	close(pm.stopCh)
	if pm.viewTimer != nil {
		pm.viewTimer.Stop()
	}

	return nil
}

// run is the main pacemaker loop
func (pm *Pacemaker) run(ctx context.Context) {
	pm.logger.InfoContext(ctx, "[DEBUG-PACEMAKER] pm.run() STARTED")
	defer pm.logger.InfoContext(ctx, "[DEBUG-PACEMAKER] pm.run() EXITED")

	for {
		pm.mu.RLock()
		timer := pm.viewTimer
		pm.mu.RUnlock()

		var timerCh <-chan time.Time
		if timer != nil {
			timerCh = timer.C
		}

		select {
		case <-pm.stopCh:
			pm.logger.InfoContext(ctx, "[DEBUG-PACEMAKER] stopCh closed")
			return
		case <-timerCh:
			pm.logger.InfoContext(ctx, "[DEBUG-PACEMAKER] viewTimer fired")
			pm.handleTimeout(ctx)
		}
	}
}

// handleTimeout processes view timeout
func (pm *Pacemaker) handleTimeout(ctx context.Context) {
	pm.mu.Lock()
	view := pm.currentView
	pm.mu.Unlock()

	pm.logger.WarnContext(ctx, "view timeout",
		"view", view,
		"timeout", pm.timeout,
	)

	pm.audit.Warn("view_timeout", map[string]interface{}{
		"view":    view,
		"timeout": pm.timeout.String(),
	})

	// Increase timeout (AIMD)
	if pm.config.EnableAIMD {
		pm.increaseTimeout()
	}

	// Trigger view change
	if err := pm.TriggerViewChange(ctx); err != nil {
		pm.logger.ErrorContext(ctx, "failed to trigger view change",
			"error", err,
			"view", view,
		)
	}

	// Notify callback
	if pm.callbacks != nil {
		if err := pm.callbacks.OnTimeout(view); err != nil {
			pm.logger.ErrorContext(ctx, "timeout callback failed",
				"error", err,
				"view", view,
			)
		}
	}
}

// TriggerViewChange initiates a view change
func (pm *Pacemaker) TriggerViewChange(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	oldView := pm.currentView
	newView := oldView + 1

	pm.logger.InfoContext(ctx, "triggering view change",
		"old_view", oldView,
		"new_view", newView,
	)

	// Create view change message
	vcMsg, err := pm.createViewChangeMsg(ctx, oldView, newView)
	if err != nil {
		return fmt.Errorf("failed to create view change message: %w", err)
	}

	// Store our own view change
	if pm.viewChanges[newView] == nil {
		pm.viewChanges[newView] = make(map[ValidatorID]*ViewChangeMsg)
	}
	pm.viewChanges[newView][pm.crypto.GetKeyID()] = vcMsg

	// Publish view change
	if pm.publisher != nil {
		if err := pm.publisher.PublishViewChange(ctx, vcMsg); err != nil {
			pm.logger.WarnContext(ctx, "failed to publish view change", "error", err)
		} else {
			pm.lastCatchupBroadcast[newView] = time.Now()
			pm.logger.InfoContext(ctx, "view change published", "new_view", newView)
		}
	}

	pm.audit.Info("view_change_initiated", map[string]interface{}{
		"old_view": oldView,
		"new_view": newView,
	})

	// Reset timer to retry if quorum not reached
	pm.resetTimerLocked()

	return nil
}

// OnViewChange processes received view change messages
func (pm *Pacemaker) OnViewChange(ctx context.Context, vcMsgInterface interface{}) error {
	vcMsg, ok := vcMsgInterface.(*ViewChangeMsg)
	if !ok {
		return fmt.Errorf("OnViewChange: expected *ViewChangeMsg, got %T", vcMsgInterface)
	}

	pm.mu.Lock()

	pm.logger.InfoContext(ctx, "[CATCHUP-DEBUG] OnViewChange ENTERED",
		"sender", fmt.Sprintf("%x", vcMsg.SenderID[:8]),
		"old_view", vcMsg.OldView,
		"new_view", vcMsg.NewView,
		"my_current_view", pm.currentView)

	// Validate view change
	if vcMsg.NewView <= pm.currentView {
		current := pm.currentView
		if vcMsg.NewView < current {
			// Truly stale peer – track for conditional catchup forcing and broadcast
			pm.staleViewChanges[vcMsg.NewView] = time.Now()
			pm.mu.Unlock()
			pm.logger.InfoContext(ctx, "[CATCHUP-DEBUG] OnViewChange REJECTED - stale view",
				"stale_view", vcMsg.NewView,
				"current_view", current)
			if current > 0 {
				pm.broadcastCatchupViewChange(ctx, current-1, current, catchupReasonStalePeer, true)
			}
			return fmt.Errorf("view change for old view: %d < %d", vcMsg.NewView, current)
		}

		// Equal view – benign duplicate; ignore without triggering catchup storm
		pm.mu.Unlock()
		pm.logger.DebugContext(ctx, "[CATCHUP-DEBUG] OnViewChange ignored - equal view",
			"view", vcMsg.NewView,
			"current_view", current)
		return nil
	}

	// Store view change
	if pm.viewChanges[vcMsg.NewView] == nil {
		pm.viewChanges[vcMsg.NewView] = make(map[ValidatorID]*ViewChangeMsg)
	}
	pm.viewChanges[vcMsg.NewView][vcMsg.SenderID] = vcMsg

	total := len(pm.viewChanges[vcMsg.NewView])
	validatorCount := pm.validatorSet.GetValidatorCount()
	f := (validatorCount - 1) / 3
	quorum := 2*f + 1
	pm.logger.InfoContext(ctx, "[CATCHUP-DEBUG] ViewChange stored",
		"sender", fmt.Sprintf("%x", vcMsg.SenderID[:8]),
		"old_view", vcMsg.OldView,
		"new_view", vcMsg.NewView,
		"total_received", total,
		"quorum_needed", quorum,
		"has_quorum", total >= quorum)

	if !pm.hasViewChangeQuorum(vcMsg.NewView) {
		pm.mu.Unlock()
		return nil
	}

	isLeader, err := pm.rotation.IsLeader(ctx, pm.crypto.GetKeyID(), vcMsg.NewView)
	if err != nil {
		pm.mu.Unlock()
		return fmt.Errorf("failed to check leader status: %w", err)
	}

	if isLeader {
		pm.mu.Unlock()
		return pm.sendNewView(ctx, vcMsg.NewView)
	}

	// Followers: capture highest QC we know about, then advance locally outside the lock
	highestQC := pm.selectHighestQCForViewLocked(vcMsg.NewView)
	pm.logger.InfoContext(ctx, "view change quorum met",
		"new_view", vcMsg.NewView,
		"role", "follower",
		"highest_qc_view", getQCView(highestQC),
	)
	pm.mu.Unlock()

	if err := pm.advanceView(ctx, vcMsg.NewView, highestQC); err != nil {
		pm.logger.WarnContext(ctx, "failed to advance view after quorum",
			"error", err,
			"new_view", vcMsg.NewView,
		)
		return err
	}

	pm.logger.InfoContext(ctx, "view advanced after quorum",
		"new_view", vcMsg.NewView,
		"source", "view_change_quorum",
	)

	return nil
}

// hasViewChangeQuorum checks if we have 2f+1 view changes
func (pm *Pacemaker) hasViewChangeQuorum(view uint64) bool {
	if !pm.config.RequireQuorum {
		return true
	}

	totalValidators := pm.validatorSet.GetValidatorCount()
	f := (totalValidators - 1) / 3
	quorum := 2*f + 1

	received := len(pm.viewChanges[view])
	return received >= quorum
}

// sendNewView creates and broadcasts a NewView message
func (pm *Pacemaker) sendNewView(ctx context.Context, newView uint64) error {
	pm.mu.Lock()
	viewChangesMap := pm.viewChanges[newView]
	vcList := make([]*ViewChangeMsg, 0, len(viewChangesMap))
	var highestQC QC
	for _, vc := range viewChangesMap {
		vcList = append(vcList, vc)
		if !isNilQC(vc.HighestQC) {
			if isNilQC(highestQC) || vc.HighestQC.GetView() > highestQC.GetView() {
				highestQC = vc.HighestQC
			}
		}
	}
	if len(vcList) == 0 {
		pm.mu.Unlock()
		return fmt.Errorf("no view changes collected for new view %d", newView)
	}
	pm.newViewSent = true
	pm.mu.Unlock()

	nvMsg := &NewViewMsg{
		View:        newView,
		ViewChanges: vcList,
		HighestQC: func() QC {
			if isNilQC(highestQC) {
				return nil
			}
			return highestQC
		}(),
		LeaderID:  pm.crypto.GetKeyID(),
		Timestamp: time.Now(),
	}

	signBytes := pm.newViewSignBytes(nvMsg)
	signature, err := pm.crypto.SignWithContext(ctx, signBytes)
	if err != nil {
		return fmt.Errorf("failed to sign NewView: %w", err)
	}
	nvMsg.Signature = signature

	pm.audit.Info("new_view_sent", map[string]interface{}{
		"view":              newView,
		"view_change_count": len(vcList),
		"highest_qc_view":   getQCView(highestQC),
	})

	if pm.publisher != nil {
		_ = pm.publisher.PublishNewView(ctx, nvMsg)
	}

	if pm.callbacks != nil {
		if err := pm.callbacks.OnNewView(newView, nvMsg); err != nil {
			return fmt.Errorf("NewView callback failed: %w", err)
		}
	}

	return pm.advanceView(ctx, newView, highestQC)
}

// OnNewView processes received NewView messages
func (pm *Pacemaker) OnNewView(ctx context.Context, nvMsgInterface interface{}) error {
	pm.logger.InfoContext(ctx, "OnNewView called", "type", fmt.Sprintf("%T", nvMsgInterface))

	nvMsg, ok := nvMsgInterface.(*NewViewMsg)
	if !ok {
		pm.logger.WarnContext(ctx, "wrong type in OnNewView", "got", fmt.Sprintf("%T", nvMsgInterface))
		return fmt.Errorf("OnNewView: expected *NewViewMsg, got %T", nvMsgInterface)
	}

	pm.mu.Lock()
	pm.logger.InfoContext(ctx, "processing newview", "view", nvMsg.View, "current", pm.currentView)

	// LATE-JOINER FIX: Allow catching up to higher views
	// Old check: nvMsg.View <= pm.currentView (rejected catch-up)
	// New check: Only reject messages from the past, allow catch-up to future views
	if nvMsg.View < pm.currentView {
		pm.logger.WarnContext(ctx, "stale newview from past, ignoring", "view", nvMsg.View, "current", pm.currentView)
		pm.mu.Unlock()
		return fmt.Errorf("NewView for old view: %d < %d", nvMsg.View, pm.currentView)
	}

	// Already at this view, no need to advance
	if nvMsg.View == pm.currentView {
		pm.logger.InfoContext(ctx, "already at NewView target view, skipping",
			"view", nvMsg.View,
			"leader", fmt.Sprintf("%x", nvMsg.LeaderID[:8]))
		pm.mu.Unlock()
		return nil
	}

	// Late joiner case: catching up to higher view
	pm.logger.InfoContext(ctx, "late joiner catching up via NewView",
		"from_view", pm.currentView,
		"to_view", nvMsg.View,
		"leader", fmt.Sprintf("%x", nvMsg.LeaderID[:8]))

	// Verify sender is the leader
	isLeader, err := pm.rotation.IsLeader(ctx, nvMsg.LeaderID, nvMsg.View)
	if err != nil {
		pm.logger.ErrorContext(ctx, "failed to verify newview leader",
			"view", nvMsg.View,
			"leader", fmt.Sprintf("%x", nvMsg.LeaderID[:8]),
			"error", err,
		)
		pm.mu.Unlock()
		return fmt.Errorf("failed to verify leader: %w", err)
	}
	if !isLeader {
		pm.logger.WarnContext(ctx, "newview from non-leader",
			"view", nvMsg.View,
			"sender", fmt.Sprintf("%x", nvMsg.LeaderID[:8]),
		)
		pm.mu.Unlock()
		return fmt.Errorf("NewView from non-leader")
	}

	// Validate has quorum of ViewChanges
	if pm.config.RequireQuorum {
		totalValidators := pm.validatorSet.GetValidatorCount()
		f := (totalValidators - 1) / 3
		quorum := 2*f + 1

		if len(nvMsg.ViewChanges) < quorum {
			pm.logger.WarnContext(ctx, "newview quorum insufficient",
				"view", nvMsg.View,
				"have", len(nvMsg.ViewChanges),
				"required", quorum,
			)
			pm.mu.Unlock()
			return fmt.Errorf("NewView has insufficient ViewChanges: %d < %d",
				len(nvMsg.ViewChanges), quorum)
		}
	}

	pm.logger.InfoContext(ctx, "received valid NewView",
		"view", nvMsg.View,
		"leader", fmt.Sprintf("%x", nvMsg.LeaderID[:8]),
	)

	pm.mu.Unlock()

	// Advance to new view without holding the lock to avoid deadlocks with internal timeout adjustments
	return pm.advanceView(ctx, nvMsg.View, nvMsg.HighestQC)
}

// advanceView moves to a new view
func (pm *Pacemaker) advanceView(ctx context.Context, newView uint64, highestQC QC) error {
	now := time.Now()

	pm.mu.Lock()
	oldView := pm.currentView
	oldTime := pm.lastViewTime
	oldHighestQC := pm.highestQC
	oldHeight := pm.currentHeight
	oldNewViewSent := pm.newViewSent
	pm.currentView = newView
	pm.lastViewTime = now
	if !isNilQC(highestQC) {
		if isNilQC(pm.highestQC) || highestQC.GetView() > pm.highestQC.GetView() {
			pm.highestQC = highestQC
			pm.currentHeight = highestQC.GetHeight() + 1
		}
	}
	pm.newViewSent = false
	pm.mu.Unlock()

	if pm.callbacks != nil {
		if err := pm.callbacks.OnViewChange(newView, highestQC); err != nil {
			pm.mu.Lock()
			pm.currentView = oldView
			pm.lastViewTime = oldTime
			pm.highestQC = oldHighestQC
			pm.currentHeight = oldHeight
			pm.newViewSent = oldNewViewSent
			pm.mu.Unlock()
			return fmt.Errorf("view change callback failed: %w", err)
		}
	}

	pm.mu.RLock()
	height := pm.currentHeight
	pm.mu.RUnlock()

	viewDuration := now.Sub(oldTime)

	pm.logger.InfoContext(ctx, "advancing view",
		"from", oldView,
		"to", newView)

	if pm.config.EnableAIMD {
		pm.decreaseTimeout()
	}

	pm.resetTimer(ctx)

	pm.logger.InfoContext(ctx, "[DIAGNOSTIC] View advanced",
		"old_view", oldView,
		"new_view", newView,
		"view_duration", viewDuration,
		"height", height)

	if viewDuration > 30*time.Second {
		pm.logger.WarnContext(ctx, "[DIAGNOSTIC] Slow view progression detected",
			"view_duration", viewDuration,
			"old_view", oldView,
			"new_view", newView,
			"threshold", 30*time.Second)
	}

	pm.mu.RLock()
	timeoutStr := pm.timeout.String()
	pm.mu.RUnlock()

	pm.audit.Info("view_advanced", map[string]interface{}{
		"view":    newView,
		"height":  height,
		"timeout": timeoutStr,
	})

	return nil
}

// OnProposal resets timer when valid proposal received
func (pm *Pacemaker) OnProposal(ctx context.Context) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.resetTimerLocked()
}

// OnQC resets timer and may advance view when QC formed
func (pm *Pacemaker) OnQC(ctx context.Context, qc QC) {
	// Capture state updates under lock, then advance view outside the lock to avoid deadlocks
	pm.mu.Lock()

	// Update highest QC
	if pm.highestQC == nil || qc.GetView() > pm.highestQC.GetView() {
		pm.highestQC = qc
	}

	// Update currentHeight when QC is formed: QC@H => next height H+1
	newHeight := qc.GetHeight() + 1
	if newHeight > pm.currentHeight {
		pm.currentHeight = newHeight
		pm.logger.InfoContext(ctx, "pacemaker height advanced",
			"height", pm.currentHeight,
			"qc_height", qc.GetHeight(),
		)
	}

	// Next proposal must occur in the view immediately after the QC's view to satisfy
	// JustifyQC.View < proposal.View and ensure replicas converge on the same view
	oldView := qc.GetView()
	nextView := oldView + 1
	if nextView <= pm.currentView {
		pm.mu.Unlock()
		pm.logger.DebugContext(ctx, "stale QC ignored",
			"qc_view", oldView,
			"current_view", pm.currentView)
		return
	}
	pm.mu.Unlock()

	// Advance to the next view immediately using the QC as HighestQC.
	// This unblocks the leader from proposing again without waiting for a timeout-driven view change.
	// advanceView handles timer reset and callbacks; warn if it fails so we don't lose the error silently.
	if err := pm.advanceView(ctx, nextView, qc); err != nil {
		pm.logger.WarnContext(ctx, "failed to advance view after QC",
			"error", err,
			"next_view", nextView,
		)
	} else {
		pm.logger.InfoContext(ctx, "[CATCHUP-DEBUG] view advanced after QC - calling broadcastCatchupViewChange",
			"old_view", nextView-1,
			"new_view", nextView,
			"qc_height", qc.GetHeight())

		// Only force if we've seen lagging peers recently
		force := pm.hasRecentStalePeers(nextView)
		pm.logger.InfoContext(ctx, "[CATCHUP-DEBUG] QC catchup decision",
			"force", force,
			"has_lagging_peers", force)
		pm.broadcastCatchupViewChange(ctx, oldView, nextView, catchupReasonQCAdvance, force)
	}
}

func (pm *Pacemaker) broadcastCatchupViewChange(ctx context.Context, oldView, newView uint64, reason catchupReason, force bool) {
	localID := pm.crypto.GetKeyID()

	pm.logger.InfoContext(ctx, "[CATCHUP-DEBUG] broadcastCatchupViewChange CALLED",
		"old_view", oldView,
		"new_view", newView,
		"my_id", fmt.Sprintf("%x", localID[:8]),
		"force", force,
		"reason", reason)

	pm.mu.Lock()

	// Circuit breaker: check retry limit
	state := pm.retryState[newView]
	if state.attempts >= maxCatchupRetries {
		pm.circuitBreakerHits++
		pm.mu.Unlock()
		pm.logger.ErrorContext(ctx, "[DIAGNOSTIC] Circuit breaker triggered - max catchup retries exceeded",
			"view", newView,
			"attempts", state.attempts,
			"first_try", state.firstTry,
			"retry_duration", time.Since(state.firstTry),
			"total_breaker_hits", pm.circuitBreakerHits)
		return
	}

	if pm.viewChanges[newView] == nil {
		pm.viewChanges[newView] = make(map[ValidatorID]*ViewChangeMsg)
	}
	existing := pm.viewChanges[newView][localID]
	now := time.Now()

	currentTimeout := pm.timeout
	resendInterval := catchupResendIntervalFor(currentTimeout, state.attempts)

	// Throttle check (unless forced)
	if existing != nil && !force {
		if last, ok := pm.lastCatchupBroadcast[newView]; ok && !last.IsZero() && now.Sub(last) < resendInterval {
			pm.mu.Unlock()
			pm.logger.InfoContext(ctx, "[CATCHUP-DEBUG] broadcastCatchupViewChange SKIPPED - throttled",
				"old_view", oldView,
				"new_view", newView,
				"last_sent", last,
				"wait_remaining", resendInterval-now.Sub(last))
			return
		}
	}

	vcMsg := existing
	if vcMsg == nil {
		var err error
		vcMsg, err = pm.createViewChangeMsg(ctx, oldView, newView)
		if err != nil {
			pm.mu.Unlock()
			pm.logger.WarnContext(ctx, "failed to create catchup view change",
				"error", err,
				"old_view", oldView,
				"new_view", newView)
			return
		}
		pm.viewChanges[newView][localID] = vcMsg
	}

	// Update retry tracking
	if state.attempts == 0 {
		state.firstTry = now
	}
	state.attempts++
	state.lastRetry = now
	pm.retryState[newView] = state

	pm.lastCatchupBroadcast[newView] = now
	pm.mu.Unlock()

	if pm.publisher == nil {
		pm.logger.DebugContext(ctx, "no publisher for catchup view change",
			"old_view", oldView,
			"new_view", newView)
		return
	}

	if err := pm.publisher.PublishViewChange(ctx, vcMsg); err != nil {
		// On failure, backdate timestamp to allow immediate retry
		pm.mu.Lock()
		pm.lastCatchupBroadcast[newView] = now.Add(-resendInterval)
		pm.mu.Unlock()

		pm.logger.WarnContext(ctx, "[CATCHUP-DEBUG] broadcastCatchupViewChange PUBLISH FAILED",
			"error", err,
			"old_view", oldView,
			"new_view", newView,
			"next_retry_in", resendInterval)
		return
	}

	attempt := state.attempts
	var retryDuration time.Duration
	if !state.firstTry.IsZero() {
		retryDuration = time.Since(state.firstTry)
	}

	pm.mu.Lock()
	delete(pm.retryState, newView)
	pm.mu.Unlock()

	pm.logger.InfoContext(ctx, "[DIAGNOSTIC] Catchup broadcast SUCCESS",
		"old_view", oldView,
		"new_view", newView,
		"reason", reason,
		"attempt", attempt,
		"retry_duration", retryDuration,
		"backoff_interval", resendInterval,
		"next_allowed", now.Add(resendInterval))
}

// hasRecentStalePeers checks if we've seen lagging peers recently to enable conditional forcing
func (pm *Pacemaker) hasRecentStalePeers(currentView uint64) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	cutoff := time.Now().Add(-stalePeerWindow)
	for view, timestamp := range pm.staleViewChanges {
		if view < currentView && timestamp.After(cutoff) {
			return true
		}
	}
	return false
}

// catchupResendIntervalFor computes dynamic interval with exponential backoff
func catchupResendIntervalFor(currentTimeout time.Duration, attempts int) time.Duration {
	interval := catchupResendBaseInterval

	if attempts > 0 {
		backoff := time.Duration(1<<uint(attempts)) * catchupResendBaseInterval
		if backoff < currentTimeout {
			interval = backoff
		} else {
			interval = currentTimeout
		}
	}

	if maxInterval := currentTimeout / 2; interval > maxInterval {
		interval = maxInterval
	}

	return interval
}

// resetTimer resets the view timeout with jitter
func (pm *Pacemaker) resetTimer(ctx context.Context) {
	pm.mu.Lock()
	pm.resetTimerLocked()
	pm.mu.Unlock()
}

func (pm *Pacemaker) resetTimerLocked() {
	timeout := pm.addJitter(pm.timeout)
	if pm.viewTimer == nil {
		pm.viewTimer = time.NewTimer(timeout)
		return
	}

	if !pm.viewTimer.Stop() {
		select {
		case <-pm.viewTimer.C:
		default:
		}
	}

	pm.viewTimer.Reset(timeout)
}

// increaseTimeout implements multiplicative increase (AIMD)
func (pm *Pacemaker) increaseTimeout() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	newTimeout := time.Duration(float64(pm.timeout) * pm.config.TimeoutIncreaseFactor)
	if newTimeout > pm.config.MaxTimeout {
		newTimeout = pm.config.MaxTimeout
	}

	pm.timeout = newTimeout
}

// decreaseTimeout implements additive decrease (AIMD)
func (pm *Pacemaker) decreaseTimeout() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	newTimeout := pm.timeout - pm.config.TimeoutDecreaseDelta
	if newTimeout < pm.config.MinTimeout {
		newTimeout = pm.config.MinTimeout
	}

	pm.timeout = newTimeout
}

// addJitter adds randomness to prevent thundering herd
func (pm *Pacemaker) addJitter(duration time.Duration) time.Duration {
	if pm.config.JitterPercent <= 0 {
		return duration
	}

	jitter := float64(duration) * pm.config.JitterPercent
	offset := (math.Sin(float64(time.Now().UnixNano())) + 1) / 2 * jitter

	return duration + time.Duration(offset)
}

// Helper functions

func (pm *Pacemaker) createViewChangeMsg(ctx context.Context, oldView, newView uint64) (*ViewChangeMsg, error) {
	vcMsg := &ViewChangeMsg{
		OldView:   oldView,
		NewView:   newView,
		HighestQC: nil,
		SenderID:  pm.crypto.GetKeyID(),
		Timestamp: time.Now(),
	}
	if !isNilQC(pm.highestQC) {
		vcMsg.HighestQC = pm.highestQC
	}

	signBytes := pm.viewChangeSignBytes(vcMsg)
	signature, err := pm.crypto.SignWithContext(ctx, signBytes)
	if err != nil {
		return nil, err
	}
	vcMsg.Signature = signature

	return vcMsg, nil
}

// viewChangeSignBytes creates canonical bytes for ViewChange signature (FIX: Issue #11)
func (pm *Pacemaker) viewChangeSignBytes(vc *ViewChangeMsg) []byte {
	buf := make([]byte, 0, 128)

	// Domain separator
	buf = append(buf, []byte(types.DomainViewChange)...)
	buf = append(buf, 0x00) // Separator

	// View transition
	buf = appendUint64(buf, vc.OldView)
	buf = appendUint64(buf, vc.NewView)

	// Height field must be present to match messages.ViewChange.SignBytes.
	// Network publisher sets Height=0; include the same here for canonical bytes.
	buf = appendUint64(buf, 0)

	// Sender
	buf = append(buf, vc.SenderID[:]...)

	// Timestamp
	buf = appendInt64(buf, vc.Timestamp.UnixNano())

	// Do not include HighestQC in VC sign bytes because network message omits HighestQC.

	return buf
}

// newViewSignBytes creates canonical bytes for NewView signature (FIX: Issue #11)
func (pm *Pacemaker) newViewSignBytes(nv *NewViewMsg) []byte {
	buf := make([]byte, 0, 256)

	// Domain separator
	buf = append(buf, []byte(types.DomainNewView)...)
	buf = append(buf, 0x00) // Separator

	// View and Height (publisher sends Height=0)
	buf = appendUint64(buf, nv.View)
	buf = appendUint64(buf, 0)

	// Leader
	buf = append(buf, nv.LeaderID[:]...)

	// Timestamp
	buf = appendInt64(buf, nv.Timestamp.UnixNano())

	// Hash all ViewChanges
	for _, vc := range nv.ViewChanges {
		vcHash := hashViewChange(vc)
		buf = append(buf, vcHash[:]...)
	}

	// HighestQC hash if present
	if !isNilQC(nv.HighestQC) {
		qcHash := nv.HighestQC.Hash()
		buf = append(buf, qcHash[:]...)
	}

	return buf
}

// Helper to hash a ViewChange message
func hashViewChange(vc *ViewChangeMsg) [32]byte {
	buf := make([]byte, 0, 160)
	// Must mirror messages.ViewChange.SignBytes()
	buf = append(buf, []byte(types.DomainViewChange)...)
	buf = append(buf, 0x00)
	buf = appendUint64(buf, vc.OldView)
	buf = appendUint64(buf, vc.NewView)
	// Height is 0 in network messages; include it for canonical hash
	buf = appendUint64(buf, 0)
	buf = append(buf, vc.SenderID[:]...)
	buf = appendInt64(buf, vc.Timestamp.UnixNano())
	// Do not include HighestQC in hash to match on-wire ViewChange (HighestQC omitted).
	return sha256.Sum256(buf)
}

func getQCView(qc QC) uint64 {
	if qc == nil {
		return 0
	}
	return qc.GetView()
}

// isNilQC safely checks if a QC interface holds a typed-nil pointer
func isNilQC(qc QC) bool {
	if qc == nil {
		return true
	}
	switch v := qc.(type) {
	case *messages.QC:
		return v == nil
	}
	return false
}

// selectHighestQCForViewLocked picks the best QC we have observed for the given view.
// Caller must hold pm.mu.
func (pm *Pacemaker) selectHighestQCForViewLocked(view uint64) QC {
	best := pm.highestQC
	if vcs := pm.viewChanges[view]; vcs != nil {
		for _, vc := range vcs {
			if !isNilQC(vc.HighestQC) && (isNilQC(best) || vc.HighestQC.GetView() > best.GetView()) {
				best = vc.HighestQC
			}
		}
	}
	return best
}

// GetCurrentView returns the current view
func (pm *Pacemaker) GetCurrentView() uint64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.currentView
}

// GetCurrentHeight returns the current height
func (pm *Pacemaker) GetCurrentHeight() uint64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.currentHeight
}

// SetCurrentView updates the pacemaker's current view to match recovered HotStuff state.
func (pm *Pacemaker) SetCurrentView(view uint64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if view == pm.currentView {
		return
	}

	if view < pm.currentView {
		pm.logger.WarnContext(context.Background(), "ignoring request to decrease pacemaker view",
			"current_view", pm.currentView,
			"requested_view", view)
		return
	}

	oldView := pm.currentView
	pm.currentView = view
	pm.lastViewTime = time.Now()

	if pm.viewTimer != nil {
		pm.resetTimerLocked()
	}

	pm.logger.InfoContext(context.Background(), "pacemaker view updated",
		"old_view", oldView,
		"new_view", view)
}

// SetCurrentHeight updates the pacemaker's current height when recovering state.
func (pm *Pacemaker) SetCurrentHeight(height uint64) {
	pm.mu.Lock()
	oldHeight := pm.currentHeight
	if height > pm.currentHeight {
		pm.currentHeight = height
		pm.logger.InfoContext(context.Background(), "pacemaker height updated",
			"old_height", oldHeight,
			"new_height", height)
	}
	pm.mu.Unlock()
}

// GetHighestQC returns the highest known QC
func (pm *Pacemaker) GetHighestQC() QC {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.highestQC
}

// GetCurrentTimeout returns the current timeout value
func (pm *Pacemaker) GetCurrentTimeout() time.Duration {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.timeout
}

// SetHighestQC updates the highest QC (thread-safe)
func (pm *Pacemaker) SetHighestQC(qc QC) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.highestQC == nil || qc.GetView() > pm.highestQC.GetView() {
		pm.highestQC = qc
	}
}

// GetViewChangeCount returns number of view changes received for a view
func (pm *Pacemaker) GetViewChangeCount(view uint64) int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.viewChanges[view])
}

// CleanupOldViewChanges removes view change messages older than current view
func (pm *Pacemaker) CleanupOldViewChanges() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	currentView := pm.currentView
	for view := range pm.viewChanges {
		if view < currentView {
			delete(pm.viewChanges, view)
		}
	}
}
