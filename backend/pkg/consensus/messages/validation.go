package messages

import (
	"context"
	"fmt"
	"time"
)

// Logger defines the interface for validation logging
type Logger interface {
	WarnContext(ctx context.Context, msg string, args ...interface{})
}

// Validator handles consensus message validation
type Validator struct {
	validatorSet ValidatorSet
	state        ConsensusState
	config       *ValidationConfig
	encoder      *Encoder
	logger       Logger
}

// ValidationConfig contains validation parameters
type ValidationConfig struct {
	MaxViewJump         uint64
	MaxHeightJump       uint64
	RequireJustifyQC    bool
	StrictMonotonicity  bool
	MinQuorumSize       int
	MaxQuorumSize       int
	AllowSelfVoting     bool
	MaxViewChangeProofs int
	MaxEvidenceAge      time.Duration
}

// ValidatorSet defines the interface for validator management
type ValidatorSet interface {
	IsValidator(keyID KeyID) bool
	GetValidatorCount() int
	GetValidator(keyID KeyID) (*ValidatorInfo, error)
	IsActiveInView(keyID KeyID, view uint64) bool
	GetLeaderForView(view uint64) (KeyID, error)
}

// ValidatorInfo contains validator metadata
type ValidatorInfo struct {
	KeyID      KeyID
	PublicKey  []byte
	Reputation float64
	IsActive   bool
	JoinedAt   uint64
}

// ConsensusState tracks current consensus state
type ConsensusState interface {
	GetCurrentView() uint64
	GetCurrentHeight() uint64
	GetLastCommittedQC() *QC
	HasSeenMessage(hash [32]byte) bool
	MarkMessageSeen(hash [32]byte)
	GetHighestQC() *QC
}

// DefaultValidationConfig returns secure default validation config
func DefaultValidationConfig() *ValidationConfig {
	return &ValidationConfig{
		MaxViewJump:         100,
		MaxHeightJump:       10,
		RequireJustifyQC:    true,
		StrictMonotonicity:  true,
		MinQuorumSize:       1,
		MaxQuorumSize:       10000,
		AllowSelfVoting:     true, // MUST be true for HotStuff - leaders vote for own proposals
		MaxViewChangeProofs: 1000,
		MaxEvidenceAge:      24 * time.Hour,
	}
}

// NewValidator creates a new message validator
func NewValidator(
	validatorSet ValidatorSet,
	state ConsensusState,
	encoder *Encoder,
	config *ValidationConfig,
	logger Logger,
) *Validator {
	if config == nil {
		config = DefaultValidationConfig()
	}

	return &Validator{
		validatorSet: validatorSet,
		state:        state,
		encoder:      encoder,
		config:       config,
		logger:       logger,
	}
}

// ValidateProposal validates a proposal message
func (v *Validator) ValidateProposal(ctx context.Context, p *Proposal) error {
	// Check if already seen (replay protection)
	if v.state.HasSeenMessage(p.Hash()) {
		return fmt.Errorf("duplicate proposal: already seen")
	}

	// Verify proposer is a validator
	if !v.validatorSet.IsValidator(p.ProposerID) {
		v.logger.WarnContext(ctx, "proposal validation failed: proposer not a validator",
			"proposer_id", fmt.Sprintf("%x", p.ProposerID[:8]),
			"view", p.View,
			"height", p.Height)
		return fmt.Errorf("proposer %x is not a validator", p.ProposerID)
	}

	// Verify proposer is active in this view
	if !v.validatorSet.IsActiveInView(p.ProposerID, p.View) {
		v.logger.WarnContext(ctx, "proposal validation failed: proposer not active",
			"proposer_id", fmt.Sprintf("%x", p.ProposerID[:8]),
			"view", p.View)
		return fmt.Errorf("proposer %x is not active in view %d", p.ProposerID, p.View)
	}

	// Verify proposer is the leader for this view
	expectedLeader, err := v.validatorSet.GetLeaderForView(p.View)
	if err != nil {
		return fmt.Errorf("failed to get leader for view %d: %w", p.View, err)
	}
	if p.ProposerID != expectedLeader {
		v.logger.WarnContext(ctx, "proposal validation failed: wrong leader",
			"proposer_id", fmt.Sprintf("%x", p.ProposerID[:8]),
			"expected_leader", fmt.Sprintf("%x", expectedLeader[:8]),
			"view", p.View,
			"height", p.Height)
		return fmt.Errorf("proposer %x is not the leader for view %d (expected %x)",
			p.ProposerID, p.View, expectedLeader)
	}

	// Check view progression
	currentView := v.state.GetCurrentView()
	if v.config.StrictMonotonicity && p.View < currentView {
		v.logger.WarnContext(ctx, "proposal validation failed: view regression",
			"proposal_view", p.View,
			"current_view", currentView)
		return fmt.Errorf("proposal view %d is less than current view %d", p.View, currentView)
	}
	if p.View > currentView+v.config.MaxViewJump {
		return fmt.Errorf("proposal view %d exceeds max jump from current view %d",
			p.View, currentView)
	}

	// Check height progression
	currentHeight := v.state.GetCurrentHeight()
	if v.config.StrictMonotonicity && p.Height < currentHeight {
		return fmt.Errorf("proposal height %d is less than current height %d",
			p.Height, currentHeight)
	}
	if p.Height > currentHeight+v.config.MaxHeightJump {
		return fmt.Errorf("proposal height %d exceeds max jump from current height %d",
			p.Height, currentHeight)
	}

	// Validate JustifyQC if present
	if p.JustifyQC != nil {
		if err := v.ValidateQC(ctx, p.JustifyQC); err != nil {
			return fmt.Errorf("invalid JustifyQC: %w", err)
		}

		// JustifyQC must justify the parent block
		if p.JustifyQC.BlockHash != p.ParentHash {
			v.logger.WarnContext(ctx, "proposal validation failed: JustifyQC/ParentHash mismatch",
				"justifyqc_blockhash", fmt.Sprintf("%x", p.JustifyQC.BlockHash[:8]),
				"parent_hash", fmt.Sprintf("%x", p.ParentHash[:8]),
				"view", p.View,
				"height", p.Height)
			return fmt.Errorf("JustifyQC block hash %x does not match parent hash %x",
				p.JustifyQC.BlockHash, p.ParentHash)
		}

		// JustifyQC view must be less than proposal view
		if p.JustifyQC.View >= p.View {
			v.logger.WarnContext(ctx, "proposal validation failed: JustifyQC view not less than proposal view",
				"justifyqc_view", p.JustifyQC.View,
				"proposal_view", p.View,
				"height", p.Height)
			return fmt.Errorf("JustifyQC view %d must be less than proposal view %d",
				p.JustifyQC.View, p.View)
		}
	} else if v.config.RequireJustifyQC && p.Height > 1 {
		// CRITICAL: Genesis block (height 1) does NOT require JustifyQC
		// Only blocks at height 2+ require justification from previous blocks
		return fmt.Errorf("proposal missing required JustifyQC at height %d", p.Height)
	}

	// Mark as seen
	v.state.MarkMessageSeen(p.Hash())

	return nil
}

// ValidateVote validates a vote message
func (v *Validator) ValidateVote(ctx context.Context, vote *Vote) error {
	// Check if already seen
	if v.state.HasSeenMessage(vote.Hash()) {
		return fmt.Errorf("duplicate vote: already seen")
	}

	// Verify voter is a validator
	if !v.validatorSet.IsValidator(vote.VoterID) {
		return fmt.Errorf("voter %x is not a validator", vote.VoterID)
	}

	// Verify voter is active in this view
	if !v.validatorSet.IsActiveInView(vote.VoterID, vote.View) {
		return fmt.Errorf("voter %x is not active in view %d", vote.VoterID, vote.View)
	}

	// Check if voter is voting for their own proposal (if not allowed)
	if !v.config.AllowSelfVoting {
		leader, err := v.validatorSet.GetLeaderForView(vote.View)
		if err == nil && vote.VoterID == leader {
			return fmt.Errorf("self-voting not allowed: voter %x is the leader", vote.VoterID)
		}
	}

	// Check view is reasonable
	currentView := v.state.GetCurrentView()
	// CRITICAL: Avoid underflow when currentView is 0
	minView := uint64(0)
	if currentView > v.config.MaxViewJump {
		minView = currentView - v.config.MaxViewJump
	}
	if vote.View < minView {
		return fmt.Errorf("vote view %d is too old (current: %d, min: %d)", vote.View, currentView, minView)
	}
	if vote.View > currentView+v.config.MaxViewJump {
		return fmt.Errorf("vote view %d is too far in future (current: %d)", vote.View, currentView)
	}

	// Mark as seen
	v.state.MarkMessageSeen(vote.Hash())

	return nil
}

// ValidateQC validates a Quorum Certificate
func (v *Validator) ValidateQC(ctx context.Context, qc *QC) error {
	if qc == nil {
		return fmt.Errorf("QC is nil")
	}

	// Check signature count
	sigCount := len(qc.Signatures)
	if sigCount < v.config.MinQuorumSize {
		return fmt.Errorf("QC has %d signatures, minimum required: %d",
			sigCount, v.config.MinQuorumSize)
	}
	if sigCount > v.config.MaxQuorumSize {
		return fmt.Errorf("QC has %d signatures, maximum allowed: %d",
			sigCount, v.config.MaxQuorumSize)
	}

	// Calculate required quorum size: 2f+1 where f = (N-1)/3
	totalValidators := v.validatorSet.GetValidatorCount()
	f := (totalValidators - 1) / 3
	requiredQuorum := 2*f + 1

	if sigCount < requiredQuorum {
		return fmt.Errorf("QC has %d signatures, quorum requires %d (N=%d, f=%d)",
			sigCount, requiredQuorum, totalValidators, f)
	}

	// Verify all signatures are from valid validators
	seenValidators := make(map[KeyID]bool)
	for i, sig := range qc.Signatures {
		// Check for duplicate signers
		if seenValidators[sig.KeyID] {
			return fmt.Errorf("QC contains duplicate signature from validator %x", sig.KeyID)
		}
		seenValidators[sig.KeyID] = true

		// Verify signer is a validator
		if !v.validatorSet.IsValidator(sig.KeyID) {
			return fmt.Errorf("QC signature %d from non-validator %x", i, sig.KeyID)
		}

		// Verify signer was active in the QC's view
		if !v.validatorSet.IsActiveInView(sig.KeyID, qc.View) {
			return fmt.Errorf("QC signature %d from inactive validator %x in view %d",
				i, sig.KeyID, qc.View)
		}
	}

	// Verify all signatures cryptographically
	if err := v.encoder.VerifyQC(ctx, qc); err != nil {
		return fmt.Errorf("QC signature verification failed: %w", err)
	}

	return nil
}

// ValidateViewChange validates a view change message
func (v *Validator) ValidateViewChange(ctx context.Context, vc *ViewChange) error {
	// Check if already seen
	if v.state.HasSeenMessage(vc.Hash()) {
		return fmt.Errorf("duplicate view change: already seen")
	}

	// Verify sender is a validator
	if !v.validatorSet.IsValidator(vc.SenderID) {
		return fmt.Errorf("view change sender %x is not a validator", vc.SenderID)
	}

	// Verify sender is active
	if !v.validatorSet.IsActiveInView(vc.SenderID, vc.OldView) {
		return fmt.Errorf("view change sender %x is not active in view %d",
			vc.SenderID, vc.OldView)
	}

	// Check view transition is valid
	if vc.NewView <= vc.OldView {
		return fmt.Errorf("new view %d must be greater than old view %d",
			vc.NewView, vc.OldView)
	}

	// Check view jump is reasonable
	if vc.NewView > vc.OldView+v.config.MaxViewJump {
		return fmt.Errorf("view change jump too large: %d -> %d (max: %d)",
			vc.OldView, vc.NewView, v.config.MaxViewJump)
	}

	// Validate HighestQC if present
	if vc.HighestQC != nil {
		if err := v.ValidateQC(ctx, vc.HighestQC); err != nil {
			return fmt.Errorf("invalid HighestQC in view change: %w", err)
		}

		// HighestQC view must be <= old view
		if vc.HighestQC.View > vc.OldView {
			return fmt.Errorf("HighestQC view %d exceeds old view %d",
				vc.HighestQC.View, vc.OldView)
		}
	}

	// Mark as seen
	v.state.MarkMessageSeen(vc.Hash())

	return nil
}

// ValidateNewView validates a new view message
func (v *Validator) ValidateNewView(ctx context.Context, nv *NewView) error {
	// Check if already seen
	if v.state.HasSeenMessage(nv.Hash()) {
		return fmt.Errorf("duplicate new view: already seen")
	}

	// Verify sender is a validator
	if !v.validatorSet.IsValidator(nv.LeaderID) {
		return fmt.Errorf("new view leader %x is not a validator", nv.LeaderID)
	}

	// Verify sender is the leader for this view
	expectedLeader, err := v.validatorSet.GetLeaderForView(nv.View)
	if err != nil {
		return fmt.Errorf("failed to get leader for view %d: %w", nv.View, err)
	}
	if nv.LeaderID != expectedLeader {
		return fmt.Errorf("new view sender %x is not the leader for view %d (expected %x)",
			nv.LeaderID, nv.View, expectedLeader)
	}

	// Check ViewChange proof count
	vcCount := len(nv.ViewChanges)
	if vcCount == 0 {
		return fmt.Errorf("new view has no ViewChange proofs")
	}
	if vcCount > v.config.MaxViewChangeProofs {
		return fmt.Errorf("new view has too many ViewChange proofs: %d (max: %d)",
			vcCount, v.config.MaxViewChangeProofs)
	}

	// Calculate required quorum
	totalValidators := v.validatorSet.GetValidatorCount()
	f := (totalValidators - 1) / 3
	requiredQuorum := 2*f + 1

	if vcCount < requiredQuorum {
		return fmt.Errorf("new view has %d ViewChanges, quorum requires %d",
			vcCount, requiredQuorum)
	}

	// Validate each ViewChange
	seenSenders := make(map[KeyID]bool)
	var highestQC *QC

	for i, vc := range nv.ViewChanges {
		// Check for duplicates
		if seenSenders[vc.SenderID] {
			return fmt.Errorf("new view contains duplicate ViewChange from %x", vc.SenderID)
		}
		seenSenders[vc.SenderID] = true

		// Validate ViewChange
		if err := v.ValidateViewChange(ctx, &vc); err != nil {
			return fmt.Errorf("invalid ViewChange %d in new view: %w", i, err)
		}

		// All ViewChanges must target the same new view
		if vc.NewView != nv.View {
			return fmt.Errorf("ViewChange %d targets view %d, expected %d",
				i, vc.NewView, nv.View)
		}

		// Track highest QC
		if vc.HighestQC != nil {
			if highestQC == nil || vc.HighestQC.View > highestQC.View {
				highestQC = vc.HighestQC
			}
		}
	}

	// Verify HighestQC matches the max from ViewChanges
	if nv.HighestQC != nil {
		if highestQC == nil {
			return fmt.Errorf("new view has HighestQC but no ViewChanges contain QCs")
		}
		if nv.HighestQC.Hash() != highestQC.Hash() {
			return fmt.Errorf("new view HighestQC does not match max QC from ViewChanges")
		}

		// Validate the HighestQC
		if err := v.ValidateQC(ctx, nv.HighestQC); err != nil {
			return fmt.Errorf("invalid HighestQC in new view: %w", err)
		}
	}

	// Mark as seen
	v.state.MarkMessageSeen(nv.Hash())

	return nil
}

// ValidateHeartbeat validates a heartbeat message
func (v *Validator) ValidateHeartbeat(ctx context.Context, hb *Heartbeat) error {
	// Check if already seen
	if v.state.HasSeenMessage(hb.Hash()) {
		return fmt.Errorf("duplicate heartbeat: already seen")
	}

	// Verify sender is a validator
	if !v.validatorSet.IsValidator(hb.LeaderID) {
		return fmt.Errorf("heartbeat sender %x is not a validator", hb.LeaderID)
	}

	// Verify sender is active
	if !v.validatorSet.IsActiveInView(hb.LeaderID, hb.View) {
		return fmt.Errorf("heartbeat sender %x is not active in view %d",
			hb.LeaderID, hb.View)
	}

	// Verify sender is the leader for this view
	expectedLeader, err := v.validatorSet.GetLeaderForView(hb.View)
	if err != nil {
		return fmt.Errorf("failed to get leader for view %d: %w", hb.View, err)
	}
	if hb.LeaderID != expectedLeader {
		return fmt.Errorf("heartbeat sender %x is not the leader for view %d (expected %x)",
			hb.LeaderID, hb.View, expectedLeader)
	}

	// Check view is reasonable
	currentView := v.state.GetCurrentView()
	if hb.View < currentView {
		return fmt.Errorf("heartbeat view %d is less than current view %d",
			hb.View, currentView)
	}
	if hb.View > currentView+v.config.MaxViewJump {
		return fmt.Errorf("heartbeat view %d exceeds max jump from current view %d",
			hb.View, currentView)
	}

	// Mark as seen
	v.state.MarkMessageSeen(hb.Hash())

	return nil
}

// ValidateEvidence validates an evidence message
func (v *Validator) ValidateEvidence(ctx context.Context, ev *Evidence) error {
	// Check if already seen
	if v.state.HasSeenMessage(ev.Hash()) {
		return fmt.Errorf("duplicate evidence: already seen")
	}

	// Verify reporter is a validator
	if !v.validatorSet.IsValidator(ev.ReporterID) {
		return fmt.Errorf("evidence reporter %x is not a validator", ev.ReporterID)
	}

	// Verify offender is a validator
	if !v.validatorSet.IsValidator(ev.OffenderID) {
		return fmt.Errorf("evidence offender %x is not a validator", ev.OffenderID)
	}

	// Check evidence age
	age := time.Since(ev.Timestamp)
	if age > v.config.MaxEvidenceAge {
		return fmt.Errorf("evidence is too old: age %v exceeds max %v",
			age, v.config.MaxEvidenceAge)
	}

	// Check view is reasonable
	currentView := v.state.GetCurrentView()
	if ev.View > currentView+v.config.MaxViewJump {
		return fmt.Errorf("evidence view %d exceeds max jump from current view %d",
			ev.View, currentView)
	}

	// Verify proof is not empty
	if len(ev.Proof) == 0 {
		return fmt.Errorf("evidence has empty proof")
	}

	// TODO: Validate specific evidence type proof
	// This would decode and verify the conflicting messages in ev.Proof

	// Mark as seen
	v.state.MarkMessageSeen(ev.Hash())

	return nil
}
