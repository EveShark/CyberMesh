package leader

import (
    "context"
    "crypto/sha256"
    "fmt"
    "math"
    "sync"
    "time"

    "backend/pkg/consensus/types"
)

// Use domain separators from types to avoid drift

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
	viewChanges map[uint64]map[ValidatorID]*ViewChangeMsg
	newViewSent bool

	mu        sync.RWMutex
	stopCh    chan struct{}
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
		BaseTimeout:           2 * time.Second,
		MaxTimeout:            60 * time.Second,
		MinTimeout:            1 * time.Second,
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
		rotation:      rotation,
		validatorSet:  validatorSet,
		config:        config,
		crypto:        crypto,
		audit:         audit,
		logger:        logger,
		publisher:     publisher,
		currentView:   0,
		currentHeight: 0,
		timeout:       config.BaseTimeout,
		viewChanges:   make(map[uint64]map[ValidatorID]*ViewChangeMsg),
		stopCh:        make(chan struct{}),
		callbacks:     callbacks,
	}
}

// Start begins the pacemaker timer
func (pm *Pacemaker) Start(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.logger.InfoContext(ctx, "starting pacemaker",
		"view", pm.currentView,
		"timeout", pm.timeout,
	)

	pm.resetTimer(ctx)

	go pm.run(ctx)

	return nil
}

// Stop halts the pacemaker
func (pm *Pacemaker) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	close(pm.stopCh)
	if pm.viewTimer != nil {
		pm.viewTimer.Stop()
	}

	return nil
}

// run is the main pacemaker loop
func (pm *Pacemaker) run(ctx context.Context) {
	for {
		select {
		case <-pm.stopCh:
			return
		case <-pm.viewTimer.C:
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
		_ = pm.publisher.PublishViewChange(ctx, vcMsg)
	}

	pm.audit.Info("view_change_initiated", map[string]interface{}{
		"old_view": oldView,
		"new_view": newView,
	})

	return nil
}

// OnViewChange processes received view change messages
func (pm *Pacemaker) OnViewChange(ctx context.Context, vcMsgInterface interface{}) error {
	vcMsg, ok := vcMsgInterface.(*ViewChangeMsg)
	if !ok {
		return fmt.Errorf("OnViewChange: expected *ViewChangeMsg, got %T", vcMsgInterface)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Validate view change
	if vcMsg.NewView <= pm.currentView {
		return fmt.Errorf("view change for old view: %d <= %d", vcMsg.NewView, pm.currentView)
	}

	// Store view change
	if pm.viewChanges[vcMsg.NewView] == nil {
		pm.viewChanges[vcMsg.NewView] = make(map[ValidatorID]*ViewChangeMsg)
	}
	pm.viewChanges[vcMsg.NewView][vcMsg.SenderID] = vcMsg

	pm.logger.InfoContext(ctx, "received view change",
		"sender", fmt.Sprintf("%x", vcMsg.SenderID[:8]),
		"old_view", vcMsg.OldView,
		"new_view", vcMsg.NewView,
		"total_received", len(pm.viewChanges[vcMsg.NewView]),
	)

	// Check if we have quorum
	if pm.hasViewChangeQuorum(vcMsg.NewView) {
		return pm.processViewChangeQuorum(ctx, vcMsg.NewView)
	}

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

// processViewChangeQuorum handles reaching view change quorum
func (pm *Pacemaker) processViewChangeQuorum(ctx context.Context, newView uint64) error {
	// Check if we're the new leader
	isLeader, err := pm.rotation.IsLeader(ctx, pm.crypto.GetKeyID(), newView)
	if err != nil {
		return fmt.Errorf("failed to check leader status: %w", err)
	}

	if !isLeader {
		// Wait for NewView from leader
		pm.logger.InfoContext(ctx, "view change quorum reached, waiting for new leader",
			"new_view", newView,
		)
		return nil
	}

	// We're the leader - create and broadcast NewView
	if pm.newViewSent {
		return nil // Already sent
	}

	return pm.sendNewView(ctx, newView)
}

// sendNewView creates and broadcasts a NewView message
func (pm *Pacemaker) sendNewView(ctx context.Context, newView uint64) error {
	viewChanges := pm.viewChanges[newView]

	// Collect ViewChange messages
	vcList := make([]*ViewChangeMsg, 0, len(viewChanges))
	var highestQC QC

	for _, vc := range viewChanges {
		vcList = append(vcList, vc)

		// Track highest QC
		if vc.HighestQC != nil {
			if highestQC == nil || vc.HighestQC.GetView() > highestQC.GetView() {
				highestQC = vc.HighestQC
			}
		}
	}

	// Create NewView message
	nvMsg := &NewViewMsg{
		View:        newView,
		ViewChanges: vcList,
		HighestQC:   highestQC,
		LeaderID:    pm.crypto.GetKeyID(),
		Timestamp:   time.Now(),
	}

	// Sign NewView
	signBytes := pm.newViewSignBytes(nvMsg)
	signature, err := pm.crypto.SignWithContext(ctx, signBytes)
	if err != nil {
		return fmt.Errorf("failed to sign NewView: %w", err)
	}
	nvMsg.Signature = signature

	pm.newViewSent = true

	pm.audit.Info("new_view_sent", map[string]interface{}{
		"view":              newView,
		"view_change_count": len(vcList),
		"highest_qc_view":   getQCView(highestQC),
	})

	// Publish NewView
	if pm.publisher != nil {
		_ = pm.publisher.PublishNewView(ctx, nvMsg)
	}

	// Notify callback
	if pm.callbacks != nil {
		if err := pm.callbacks.OnNewView(newView, nvMsg); err != nil {
			return fmt.Errorf("NewView callback failed: %w", err)
		}
	}

	// Advance to new view
	return pm.advanceView(ctx, newView, highestQC)
}

// OnNewView processes received NewView messages
func (pm *Pacemaker) OnNewView(ctx context.Context, nvMsgInterface interface{}) error {
	nvMsg, ok := nvMsgInterface.(*NewViewMsg)
	if !ok {
		return fmt.Errorf("OnNewView: expected *NewViewMsg, got %T", nvMsgInterface)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Validate NewView
	if nvMsg.View <= pm.currentView {
		return fmt.Errorf("NewView for old view: %d <= %d", nvMsg.View, pm.currentView)
	}

	// Verify sender is the leader
	isLeader, err := pm.rotation.IsLeader(ctx, nvMsg.LeaderID, nvMsg.View)
	if err != nil {
		return fmt.Errorf("failed to verify leader: %w", err)
	}
	if !isLeader {
		return fmt.Errorf("NewView from non-leader")
	}

	// Validate has quorum of ViewChanges
	if pm.config.RequireQuorum {
		totalValidators := pm.validatorSet.GetValidatorCount()
		f := (totalValidators - 1) / 3
		quorum := 2*f + 1

		if len(nvMsg.ViewChanges) < quorum {
			return fmt.Errorf("NewView has insufficient ViewChanges: %d < %d",
				len(nvMsg.ViewChanges), quorum)
		}
	}

	pm.logger.InfoContext(ctx, "received valid NewView",
		"view", nvMsg.View,
		"leader", fmt.Sprintf("%x", nvMsg.LeaderID[:8]),
	)

	// Advance to new view
	return pm.advanceView(ctx, nvMsg.View, nvMsg.HighestQC)
}

// advanceView moves to a new view
func (pm *Pacemaker) advanceView(ctx context.Context, newView uint64, highestQC QC) error {
	pm.currentView = newView
	if highestQC != nil {
		pm.highestQC = highestQC
		pm.currentHeight = highestQC.GetHeight() + 1
	}

	// Decrease timeout on successful view change (AIMD)
	if pm.config.EnableAIMD {
		pm.decreaseTimeout()
	}

	// Reset state
	pm.newViewSent = false
	pm.resetTimer(ctx)

	pm.audit.Info("view_advanced", map[string]interface{}{
		"view":    newView,
		"height":  pm.currentHeight,
		"timeout": pm.timeout.String(),
	})

	// Notify callback
	if pm.callbacks != nil {
		if err := pm.callbacks.OnViewChange(newView, highestQC); err != nil {
			return fmt.Errorf("view change callback failed: %w", err)
		}
	}

	return nil
}

// OnProposal resets timer when valid proposal received
func (pm *Pacemaker) OnProposal(ctx context.Context) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.resetTimer(ctx)
}

// OnQC resets timer and may advance view when QC formed
func (pm *Pacemaker) OnQC(ctx context.Context, qc QC) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Update highest QC
	if pm.highestQC == nil || qc.GetView() > pm.highestQC.GetView() {
		pm.highestQC = qc
	}

	// CRITICAL FIX (BUG-016): Update currentHeight when QC is formed
	// QC for height H means we can now propose height H+1
	newHeight := qc.GetHeight() + 1
	if newHeight > pm.currentHeight {
		pm.currentHeight = newHeight
		pm.logger.InfoContext(ctx, "pacemaker height advanced",
			"height", pm.currentHeight,
			"qc_height", qc.GetHeight(),
		)
	}

	pm.resetTimer(ctx)
}

// resetTimer resets the view timeout with jitter
func (pm *Pacemaker) resetTimer(ctx context.Context) {
	if pm.viewTimer != nil {
		pm.viewTimer.Stop()
	}

	timeout := pm.addJitter(pm.timeout)
	pm.viewTimer = time.NewTimer(timeout)
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
		HighestQC: pm.highestQC,
		SenderID:  pm.crypto.GetKeyID(),
		Timestamp: time.Now(),
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

	// View
	buf = appendUint64(buf, nv.View)

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
	if nv.HighestQC != nil {
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
