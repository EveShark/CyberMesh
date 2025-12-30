package pbft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
)

// Domain separators for consensus messages
const (
	DomainProposal = "CONSENSUS_PROPOSAL_V1"
	DomainVote     = "CONSENSUS_VOTE_V1"
	DomainEvidence = "CONSENSUS_EVIDENCE_V1"
)

const (
	createProposalMaxSyncRetries = 50
	createProposalRetryDelay     = 50 * time.Millisecond
)

// HotStuff implements the HotStuff consensus protocol (2-chain variant)
type HotStuff struct {
	validatorSet types.ValidatorSet
	rotation     types.LeaderRotation
	pacemaker    types.Pacemaker
	quorum       *QuorumVerifier
	storage      *Storage
	encoder      types.MessageEncoder
	validator    types.MessageValidator
	crypto       types.CryptoService
	config       *HotStuffConfig
	audit        types.AuditLogger
	logger       types.Logger

	// Consensus state
	currentView   uint64
	currentHeight uint64
	lockedQC      types.QC
	prepareQC     types.QC
	votesSent     map[uint64]types.BlockHash // Tracks block hash we voted for in each view (safety)

	// Proposal tracking
	pendingVotes map[types.BlockHash]map[types.ValidatorID]*messages.Vote // blockHash -> votes

	logLast map[string]time.Time

	mu        sync.RWMutex
	stopCh    chan struct{}
	callbacks types.ConsensusCallbacks
}

// HotStuffConfig contains consensus parameters
type HotStuffConfig struct {
	ValidatorID            types.ValidatorID
	EnableVoting           bool
	EnableProposing        bool
	MaxPendingVotes        int
	VoteTimeout            time.Duration
	BlockValidationFunc    func(ctx context.Context, block types.Block) error
	SafetyCheckStrict      bool
	AutoCleanupOldVotes    bool
	MaxConcurrentProposals int
}

// DefaultHotStuffConfig returns secure defaults
func DefaultHotStuffConfig() *HotStuffConfig {
	return &HotStuffConfig{
		EnableVoting:           true,
		EnableProposing:        true,
		MaxPendingVotes:        1000,
		VoteTimeout:            5 * time.Second,
		SafetyCheckStrict:      true,
		AutoCleanupOldVotes:    true,
		MaxConcurrentProposals: 10,
	}
}

// NewHotStuff creates a new HotStuff consensus engine
func NewHotStuff(
	validatorSet types.ValidatorSet,
	rotation types.LeaderRotation,
	pacemaker types.Pacemaker,
	quorum *QuorumVerifier,
	storage *Storage,
	encoder types.MessageEncoder,
	validator types.MessageValidator,
	crypto types.CryptoService,
	audit types.AuditLogger,
	logger types.Logger,
	config *HotStuffConfig,
	callbacks types.ConsensusCallbacks,
) *HotStuff {
	if config == nil {
		config = DefaultHotStuffConfig()
	}

	return &HotStuff{
		validatorSet: validatorSet,
		rotation:     rotation,
		pacemaker:    pacemaker,
		quorum:       quorum,
		storage:      storage,
		encoder:      encoder,
		validator:    validator,
		crypto:       crypto,
		config:       config,
		audit:        audit,
		logger:       logger,
		votesSent:    make(map[uint64]types.BlockHash),
		pendingVotes: make(map[types.BlockHash]map[types.ValidatorID]*messages.Vote),
		logLast:      make(map[string]time.Time),
		stopCh:       make(chan struct{}),
		callbacks:    callbacks,
	}
}

func (hs *HotStuff) shouldLogLocked(key string, every time.Duration) bool {
	now := time.Now()
	if last, ok := hs.logLast[key]; ok && now.Sub(last) < every {
		return false
	}
	hs.logLast[key] = now
	return true
}

func (hs *HotStuff) SetBlockValidationFunc(f func(ctx context.Context, block types.Block) error) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	if hs.config == nil {
		hs.config = DefaultHotStuffConfig()
	}
	hs.config.BlockValidationFunc = f
}

// Start initializes the consensus engine
func (hs *HotStuff) Start(ctx context.Context) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	// Load state from storage
	if err := hs.loadState(ctx); err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	hs.logger.InfoContext(ctx, "HotStuff consensus started",
		"view", hs.currentView,
		"height", hs.currentHeight,
		"validator_id", fmt.Sprintf("%x", hs.config.ValidatorID[:8]),
	)

	if hs.audit != nil {
		hs.audit.Info("consensus_started", map[string]interface{}{
			"view":         hs.currentView,
			"height":       hs.currentHeight,
			"validator_id": fmt.Sprintf("%x", hs.config.ValidatorID[:]),
		})
	}

	return nil
}

// Stop halts the consensus engine
func (hs *HotStuff) Stop() error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	close(hs.stopCh)

	hs.logger.InfoContext(context.Background(), "HotStuff consensus stopped")

	return nil
}

// OnProposal processes a received proposal (replica path)
func (hs *HotStuff) OnProposal(ctx context.Context, proposal *messages.Proposal) error {
	hs.mu.Lock()
	qc, err := hs.onProposalLocked(ctx, proposal)
	hs.mu.Unlock()

	if err != nil {
		hs.logger.WarnContext(ctx, "proposal handling failed",
			"view", proposal.View,
			"height", proposal.Height,
			"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
			"proposer", fmt.Sprintf("%x", proposal.ProposerID[:8]),
			"error", err.Error(),
		)
		return err
	}

	if qc != nil {
		hs.pacemaker.OnQC(ctx, qc)
		hs.handleQCFormedCallback(qc)
	}

	return nil
}

func (hs *HotStuff) onProposalLocked(ctx context.Context, proposal *messages.Proposal) (types.QC, error) {
	// Track first proposal for startup diagnostic
	if proposal.View == 0 && proposal.Height == 0 {
		hs.logger.InfoContext(ctx, "[DIAGNOSTIC] First proposal received",
			"view", proposal.View,
			"height", proposal.Height,
			"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
			"proposer", fmt.Sprintf("%x", proposal.ProposerID[:8]),
		)
	} else {
		hs.logger.InfoContext(ctx, "received proposal",
			"view", proposal.View,
			"height", proposal.Height,
			"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
			"proposer", fmt.Sprintf("%x", proposal.ProposerID[:8]),
		)
	}

	// Validate proposal structure
	if err := hs.validator.ValidateProposal(ctx, proposal); err != nil {
		hs.logger.WarnContext(ctx, "proposal rejected: invalid proposal",
			"view", proposal.View,
			"height", proposal.Height,
			"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
			"proposer", fmt.Sprintf("%x", proposal.ProposerID[:8]),
			"error", err.Error(),
		)
		if hs.audit != nil {
			hs.audit.Warn("invalid_proposal", map[string]interface{}{
				"view":     proposal.View,
				"height":   proposal.Height,
				"error":    err.Error(),
				"proposer": fmt.Sprintf("%x", proposal.ProposerID[:]),
			})
		}
		return nil, fmt.Errorf("proposal validation failed: %w", err)
	}

	// Check if we already voted in this view (safety rule)
	if votedHash, hasVoted := hs.votesSent[proposal.View]; hasVoted {
		if votedHash == proposal.BlockHash {
			if hs.logger != nil && hs.shouldLogLocked(fmt.Sprintf("dup_proposal_view_%d", proposal.View), 30*time.Second) {
				hs.logger.InfoContext(ctx, "duplicate proposal received for view",
					"view", proposal.View,
					"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
				)
			}
			return nil, nil // idempotent re-delivery: already processed this proposal
		}
		if hs.logger != nil && hs.shouldLogLocked(fmt.Sprintf("already_voted_view_%d", proposal.View), 30*time.Second) {
			hs.logger.WarnContext(ctx, "already voted in this view",
				"view", proposal.View,
				"existing_block", fmt.Sprintf("%x", votedHash[:8]),
				"incoming_block", fmt.Sprintf("%x", proposal.BlockHash[:8]),
			)
		}
		return nil, fmt.Errorf("already voted in view %d", proposal.View)
	}

	// Validate justifyQC
	if proposal.JustifyQC != nil {
		if err := hs.validator.ValidateQC(ctx, proposal.JustifyQC); err != nil {
			return nil, fmt.Errorf("invalid justifyQC: %w", err)
		}

		// Update lockedQC if justifyQC is higher
		if hs.lockedQC == nil || proposal.JustifyQC.GetView() > hs.lockedQC.GetView() {
			hs.lockedQC = proposal.JustifyQC
		}
	}

	// Check safety: can only vote if proposal extends from our locked block
	if !hs.isSafeToVote(ctx, proposal) {
		hs.logger.WarnContext(ctx, "unsafe to vote, proposal doesn't extend locked block",
			"proposal_view", proposal.View,
			"locked_view", getQCView(hs.lockedQC),
			"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
			"parent_hash", fmt.Sprintf("%x", proposal.ParentHash[:8]),
		)
		return nil, fmt.Errorf("proposal conflicts with locked QC")
	}

	// Validate block content (if validator configured)
	if hs.config.BlockValidationFunc != nil {
		if err := hs.config.BlockValidationFunc(ctx, proposal.Block); err != nil {
			hs.logger.WarnContext(ctx, "proposal rejected: block validation failed",
				"view", proposal.View,
				"height", proposal.Height,
				"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
				"proposer", fmt.Sprintf("%x", proposal.ProposerID[:8]),
				"error", err.Error(),
			)
			return nil, fmt.Errorf("block validation failed: %w", err)
		}
	}

	// Store proposal
	if err := hs.storage.StoreProposal(proposal); err != nil {
		return nil, fmt.Errorf("failed to store proposal: %w", err)
	}

	// Notify pacemaker of proposal
	hs.pacemaker.OnProposal(ctx)

	// Callback
	if hs.callbacks != nil {
		if err := hs.callbacks.OnProposal(*proposal); err != nil {
			hs.logger.ErrorContext(ctx, "proposal callback failed", "error", err)
		}
	}

	// Vote if enabled
	if hs.config.EnableVoting {
		hs.logger.InfoContext(ctx, "creating vote for proposal",
			"view", proposal.View,
			"height", proposal.Height,
			"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]))
		return hs.sendVoteLocked(ctx, proposal)
	}

	hs.logger.InfoContext(ctx, "voting disabled - skipping vote")
	return nil, nil
}

// isSafeToVote implements the HotStuff safety rule
func (hs *HotStuff) isSafeToVote(ctx context.Context, proposal *messages.Proposal) bool {
	if !hs.config.SafetyCheckStrict {
		return true
	}

	// Safety rule: can vote if proposal extends from locked QC
	// In HotStuff: vote if proposal.JustifyQC.view >= lockedQC.view
	if hs.lockedQC == nil {
		return true // No locked QC, safe to vote
	}

	if proposal.JustifyQC == nil {
		return false // No justifyQC but we have locked QC
	}

	return proposal.JustifyQC.GetView() >= hs.lockedQC.GetView()
}

// sendVoteLocked creates and sends a vote for a proposal. Caller must hold hs.mu.
func (hs *HotStuff) sendVoteLocked(ctx context.Context, proposal *messages.Proposal) (types.QC, error) {
	ts := time.Now()
	vote := &messages.Vote{
		View:      proposal.View,
		Height:    proposal.Height,
		Round:     proposal.Round,
		BlockHash: proposal.BlockHash,
		VoterID:   messages.KeyID(hs.crypto.GetKeyID()),
		Timestamp: ts,
	}

	// Sign vote
	signBytes := vote.SignBytes()
	signature, err := hs.crypto.SignWithContext(ctx, signBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to sign vote: %w", err)
	}

	vote.Signature = messages.Signature{
		Bytes:     signature,
		KeyID:     hs.crypto.GetKeyID(),
		Timestamp: ts,
	}

	// Store our vote
	if err := hs.storage.StoreVote(vote); err != nil {
		return nil, fmt.Errorf("failed to store vote: %w", err)
	}

	// Mark that we voted in this view
	hs.votesSent[proposal.View] = proposal.BlockHash

	hs.logger.InfoContext(ctx, "vote sent",
		"view", vote.View,
		"height", vote.Height,
		"block_hash", fmt.Sprintf("%x", vote.BlockHash[:8]),
	)

	if hs.audit != nil {
		hs.audit.Info("vote_sent", map[string]interface{}{
			"view":       vote.View,
			"height":     vote.Height,
			"block_hash": fmt.Sprintf("%x", vote.BlockHash[:]),
		})
	}

	// Callback
	if hs.callbacks != nil {
		if err := hs.callbacks.OnVote(*vote); err != nil {
			hs.logger.ErrorContext(ctx, "vote callback failed", "error", err)
		}
	}

	// If we're the leader, process our own vote (MUST be called while holding lock from OnProposal)
	isLeader, _ := hs.rotation.IsLeader(ctx, hs.crypto.GetKeyID(), proposal.View)
	hs.logger.InfoContext(ctx, "leader check", "is_leader", isLeader, "view", proposal.View)
	if isLeader {
		hs.logger.InfoContext(ctx, "calling onVoteInternal as leader")
		qc, err := hs.onVoteInternal(ctx, vote)
		if err != nil {
			hs.logger.ErrorContext(ctx, "onVoteInternal failed", "error", err)
			return nil, err
		}
		return qc, nil
	}

	// DEV-ONLY SAFETY: single-node self-delivery fallback
	// If the quorum threshold is 1 (e.g., QUORUM_SIZE=1 / single validator),
	// immediately deliver our vote back to ourselves to unblock dev mode.
	// This does NOT alter production behavior where quorum > 1.
	validatorCount := hs.validatorSet.GetValidatorCount()
	quorumThreshold := 0
	if hs.quorum != nil {
		quorumThreshold = hs.quorum.GetQuorumThreshold()
	}
	hs.logger.InfoContext(ctx, "single-node check", "validator_count", validatorCount, "quorum_threshold", quorumThreshold)
	if validatorCount == 1 || quorumThreshold == 1 {
		hs.logger.InfoContext(ctx, "calling onVoteInternal as single-node fallback")
		return hs.onVoteInternal(ctx, vote)
	}

	hs.logger.WarnContext(ctx, "vote NOT delivered - neither leader nor single-node")
	return nil, nil
}

// OnVote processes a received vote (leader path) - external entry point with locking
func (hs *HotStuff) OnVote(ctx context.Context, vote *messages.Vote) error {
	hs.mu.Lock()
	qc, err := hs.onVoteInternal(ctx, vote)
	hs.mu.Unlock()

	if err != nil {
		return err
	}

	if qc != nil {
		hs.pacemaker.OnQC(ctx, qc)
		hs.handleQCFormedCallback(qc)
	}

	return nil
}

// onVoteInternal processes a vote without locking (called internally when lock is already held)
func (hs *HotStuff) onVoteInternal(ctx context.Context, vote *messages.Vote) (types.QC, error) {

	hs.logger.InfoContext(ctx, "OnVote ENTERED",
		"view", vote.View,
		"voter", fmt.Sprintf("%x", vote.VoterID[:8]),
		"block_hash", fmt.Sprintf("%x", vote.BlockHash[:8]),
	)

	// Validate vote
	hs.logger.InfoContext(ctx, "OnVote: validating vote")
	if err := hs.validator.ValidateVote(ctx, vote); err != nil {
		hs.logger.ErrorContext(ctx, "OnVote: validation FAILED", "error", err)
		if hs.audit != nil {
			hs.audit.Warn("invalid_vote", map[string]interface{}{
				"view":  vote.View,
				"voter": fmt.Sprintf("%x", vote.VoterID[:]),
				"error": err.Error(),
			})
		}
		return nil, fmt.Errorf("vote validation failed: %w", err)
	}
	hs.logger.InfoContext(ctx, "OnVote: validation PASSED")

	// Check for equivocation (double voting)
	hs.logger.InfoContext(ctx, "OnVote: checking equivocation")
	if err := hs.detectVoteEquivocation(ctx, vote); err != nil {
		hs.logger.ErrorContext(ctx, "OnVote: equivocation detected", "error", err)
		return nil, err
	}
	hs.logger.InfoContext(ctx, "OnVote: no equivocation")

	// Store vote
	hs.logger.InfoContext(ctx, "OnVote: storing vote")
	if err := hs.storage.StoreVote(vote); err != nil {
		hs.logger.ErrorContext(ctx, "OnVote: storage FAILED", "error", err)
		return nil, fmt.Errorf("failed to store vote: %w", err)
	}
	hs.logger.InfoContext(ctx, "OnVote: vote stored")

	// Add to pending votes
	if hs.pendingVotes[vote.BlockHash] == nil {
		hs.pendingVotes[vote.BlockHash] = make(map[types.ValidatorID]*messages.Vote)
	}
	hs.pendingVotes[vote.BlockHash][types.ValidatorID(vote.VoterID)] = vote

	// DEBUG: Log quorum check with detailed progress
	voteCount := len(hs.pendingVotes[vote.BlockHash])
	validatorCount := hs.validatorSet.GetValidatorCount()
	quorumThreshold := hs.quorum.GetQuorumThreshold()
	remaining := quorumThreshold - voteCount
	if remaining < 0 {
		remaining = 0
	}
	hs.logger.InfoContext(ctx, "OnVote: quorum progress",
		"vote_count", voteCount,
		"validator_count", validatorCount,
		"quorum_threshold", quorumThreshold,
		"remaining", remaining,
		"block_hash", fmt.Sprintf("%x", vote.BlockHash[:8]),
		"view", vote.View,
	)

	// Check if we have quorum
	if hs.quorum.HasQuorum(hs.pendingVotes[vote.BlockHash]) {
		hs.logger.InfoContext(ctx, "QUORUM REACHED - forming QC",
			"block_hash", fmt.Sprintf("%x", vote.BlockHash[:8]),
			"view", vote.View,
			"height", vote.Height)
		return hs.formQC(ctx, vote.BlockHash, vote.View)
	}

	hs.logger.InfoContext(ctx, "quorum NOT reached yet",
		"needed", quorumThreshold,
		"have", voteCount,
		"remaining", remaining,
		"block_hash", fmt.Sprintf("%x", vote.BlockHash[:8]),
		"view", vote.View,
	)
	return nil, nil
}

// detectVoteEquivocation checks for double voting
func (hs *HotStuff) detectVoteEquivocation(ctx context.Context, newVote *messages.Vote) error {
	// Check if this voter already voted for a different block in this view
	existingVotes := hs.storage.GetVotesByView(newVote.View)

	for _, existingVote := range existingVotes {
		if existingVote.VoterID == newVote.VoterID &&
			existingVote.BlockHash != newVote.BlockHash {
			// Equivocation detected!
			hs.logger.WarnContext(ctx, "equivocation detected",
				"voter", fmt.Sprintf("%x", newVote.VoterID[:8]),
				"view", newVote.View,
			)

			evidence := hs.createEquivocationEvidence(ctx, existingVote, newVote)

			if hs.audit != nil {
				hs.audit.Security("vote_equivocation_detected", map[string]interface{}{
					"voter":  fmt.Sprintf("%x", newVote.VoterID[:]),
					"view":   newVote.View,
					"block1": fmt.Sprintf("%x", existingVote.BlockHash[:]),
					"block2": fmt.Sprintf("%x", newVote.BlockHash[:]),
				})
			}

			// Store evidence
			if err := hs.storage.StoreEvidence(evidence); err != nil {
				hs.logger.ErrorContext(ctx, "failed to store evidence", "error", err)
			}

			return fmt.Errorf("equivocation detected from voter %x", newVote.VoterID[:8])
		}
	}

	return nil
}

// createEquivocationEvidence creates evidence of Byzantine behavior
func (hs *HotStuff) createEquivocationEvidence(ctx context.Context, vote1, vote2 *messages.Vote) *messages.Evidence {
	// Encode both votes as proof
	proof1, _ := hs.encoder.Encode(vote1)
	proof2, _ := hs.encoder.Encode(vote2)
	proof := append(proof1, proof2...)

	evidence := &messages.Evidence{
		Kind:       messages.EvidenceDoubleVote,
		View:       vote1.View,
		Height:     vote1.Height,
		OffenderID: vote1.VoterID,
		Proof:      proof,
		ReporterID: hs.crypto.GetKeyID(),
		Timestamp:  time.Now(),
	}

	// Sign evidence
	signBytes := evidence.SignBytes()
	signature, _ := hs.crypto.SignWithContext(ctx, signBytes)
	evidence.Signature = messages.Signature{
		Bytes:     signature,
		KeyID:     hs.crypto.GetKeyID(),
		Timestamp: time.Now(),
	}

	return evidence
}

// formQC aggregates votes into a Quorum Certificate. Caller must hold hs.mu.
func (hs *HotStuff) formQC(ctx context.Context, blockHash [32]byte, view uint64) (types.QC, error) {
	votes := hs.pendingVotes[blockHash]

	hs.logger.InfoContext(ctx, "forming QC",
		"view", view,
		"block_hash", fmt.Sprintf("%x", blockHash[:8]),
		"vote_count", len(votes),
	)

	// Aggregate signatures
	signatures := make([]types.Signature, 0, len(votes))
	for _, vote := range votes {
		signatures = append(signatures, vote.Signature)
	}

	// Create QC
	qc := &QuorumCertificate{
		View:         view,
		Height:       votes[getFirstVoter(votes)].Height,
		Round:        votes[getFirstVoter(votes)].Round,
		BlockHash:    blockHash,
		Signatures:   signatures,
		Timestamp:    time.Now(),
		AggregatorID: hs.crypto.GetKeyID(),
	}

	// Validate QC
	if err := hs.validator.ValidateQC(ctx, qc); err != nil {
		hs.logger.ErrorContext(ctx, "qc validation failed",
			"view", view,
			"block_hash", fmt.Sprintf("%x", blockHash[:8]),
			"error", err,
		)
		return nil, fmt.Errorf("QC validation failed: %w", err)
	}

	// Store QC
	if err := hs.storage.StoreQC(qc); err != nil {
		hs.logger.ErrorContext(ctx, "qc storage failed",
			"view", view,
			"block_hash", fmt.Sprintf("%x", blockHash[:8]),
			"error", err,
		)
		return nil, fmt.Errorf("failed to store QC: %w", err)
	}

	if hs.audit != nil {
		hs.audit.Info("qc_formed", map[string]interface{}{
			"view":       view,
			"block_hash": fmt.Sprintf("%x", blockHash[:]),
			"vote_count": len(votes),
		})
	}

	// Update prepareQC
	hs.prepareQC = qc

	// Check commit rule (2-chain)
	hs.logger.InfoContext(ctx, "checking commit rule",
		"qc_view", qc.View,
		"qc_height", qc.Height)
	if err := hs.checkCommitRule(ctx, qc); err != nil {
		hs.logger.ErrorContext(ctx, "commit check failed", "error", err)
	} else {
		hs.logger.InfoContext(ctx, "commit rule check completed")
	}

	// Cleanup pending votes
	delete(hs.pendingVotes, blockHash)

	return qc, nil
}

// handleQCFormedCallback invokes the QC callback outside of the HotStuff mutex to avoid self-deadlocks.
func (hs *HotStuff) handleQCFormedCallback(qc types.QC) {
	if hs.callbacks == nil {
		return
	}

	if err := hs.callbacks.OnQCFormed(qc); err != nil {
		hs.logger.ErrorContext(context.Background(), "QC callback failed", "error", err)
	}
}

// checkCommitRule implements HotStuff 2-chain commit rule
func (hs *HotStuff) checkCommitRule(ctx context.Context, qc types.QC) error {
	// 2-chain rule: commit if we have QC(view N) and QC(view N-1) for consecutive blocks
	// QC → parentQC (consecutive views = commit)

	hs.logger.InfoContext(ctx, "checkCommitRule ENTERED",
		"qc_view", qc.GetView(),
		"qc_height", qc.GetHeight())

	// CRITICAL FIX (BUG-015): Single-node mode immediate commit
	// In single-node mode (1 validator), there's no Byzantine fault risk.
	// Commit immediately when QC is formed to avoid getting stuck.
	validatorCount := hs.validatorSet.GetValidatorCount()
	hs.logger.InfoContext(ctx, "checking validator count for single-node mode",
		"validator_count", validatorCount)
	if validatorCount == 1 {
		// Single-node mode: commit immediately
		hs.logger.InfoContext(ctx, "single-node mode detected - committing immediately")
		proposal := hs.storage.GetProposal(qc.GetBlockHash())
		if proposal == nil {
			hs.logger.ErrorContext(ctx, "cannot find proposal for QC block")
			return fmt.Errorf("cannot find proposal for QC block")
		}
		if proposal.Block == nil {
			hash := qc.GetBlockHash()
			hs.logger.ErrorContext(ctx, "proposal missing block payload",
				"view", qc.GetView(),
				"height", qc.GetHeight(),
				"block_hash", fmt.Sprintf("%x", hash[:8]))
			return fmt.Errorf("proposal missing block payload for QC view %d", qc.GetView())
		}

		hs.logger.InfoContext(ctx, "single-node mode: committing immediately",
			"height", proposal.Block.GetHeight(),
			"view", qc.GetView(),
		)

		return hs.commitBlock(ctx, proposal.Block, qc)
	}

	// Multi-node mode: use 2-chain rule
	hs.logger.InfoContext(ctx, "multi-node mode: checking 2-chain rule",
		"validator_count", validatorCount)
	if hs.lockedQC == nil {
		hs.logger.InfoContext(ctx, "no lockedQC yet - setting it",
			"qc_view", qc.GetView(),
			"qc_height", qc.GetHeight())
		hs.lockedQC = qc
		return nil
	}

	// Check if this QC and previous QC form a 2-chain
	hs.logger.InfoContext(ctx, "checking 2-chain consecutive views",
		"current_qc_view", qc.GetView(),
		"locked_qc_view", hs.lockedQC.GetView(),
		"consecutive", qc.GetView() == hs.lockedQC.GetView()+1)
	if qc.GetView() == hs.lockedQC.GetView()+1 {
		// Consecutive views - commit the block from lockedQC
		hs.logger.InfoContext(ctx, "2-chain rule satisfied - committing lockedQC block",
			"commit_height", hs.lockedQC.GetHeight())
		proposal := hs.storage.GetProposal(hs.lockedQC.GetBlockHash())
		if proposal == nil {
			hs.logger.ErrorContext(ctx, "cannot find proposal for committed block")
			return fmt.Errorf("cannot find proposal for committed block")
		}
		if proposal.Block == nil {
			hash := hs.lockedQC.GetBlockHash()
			hs.logger.ErrorContext(ctx, "proposal missing block payload",
				"view", hs.lockedQC.GetView(),
				"height", hs.lockedQC.GetHeight(),
				"block_hash", fmt.Sprintf("%x", hash[:8]))
			return fmt.Errorf("proposal missing block payload for committed block")
		}

		return hs.commitBlock(ctx, proposal.Block, hs.lockedQC)
	}

	// Update lockedQC if newer
	hs.logger.InfoContext(ctx, "2-chain not satisfied - updating lockedQC",
		"old_locked_view", hs.lockedQC.GetView(),
		"new_qc_view", qc.GetView())
	if qc.GetView() > hs.lockedQC.GetView() {
		hs.lockedQC = qc
	}

	return nil
}

// commitBlock finalizes a block
func (hs *HotStuff) commitBlock(ctx context.Context, block types.Block, qc types.QC) error {
	h := block.GetHash()
	hs.logger.InfoContext(ctx, "committing block",
		"height", block.GetHeight(),
		"hash", fmt.Sprintf("%x", h[:8]),
		"view", qc.GetView(),
	)

	// Fail-closed: apply/validate the committed block via callback before advancing
	// any in-memory committed height.
	if hs.callbacks != nil {
		if err := hs.callbacks.OnCommit(block, qc); err != nil {
			if hs.audit != nil {
				h2 := block.GetHash()
				hs.audit.Security("block_rejected_by_state_machine", map[string]interface{}{
					"height": block.GetHeight(),
					"hash":   fmt.Sprintf("%x", h2[:8]),
					"view":   qc.GetView(),
					"error":  err.Error(),
				})
			}
			return fmt.Errorf("commit callback failed: %w", err)
		}
	}

	// Mark as committed in storage
	if err := hs.storage.CommitBlock(block.GetHash(), block.GetHeight()); err != nil {
		return fmt.Errorf("failed to commit block: %w", err)
	}

	// Update state
	hs.currentHeight = block.GetHeight() + 1

	h2 := block.GetHash()
	if hs.audit != nil {
		hs.audit.Info("block_committed", map[string]interface{}{
			"height":   block.GetHeight(),
			"hash":     fmt.Sprintf("%x", h2[:]),
			"view":     qc.GetView(),
			"tx_count": block.GetTransactionCount(),
		})
	}

	// Cleanup old data
	if hs.config.AutoCleanupOldVotes {
		hs.cleanupOldData(block.GetHeight())
	}

	return nil
}

// CreateProposal creates a new proposal (leader path)
func (hs *HotStuff) CreateProposal(ctx context.Context, block types.Block) (*messages.Proposal, error) {
	var lastPacemakerView uint64
	var lastHotStuffView uint64
	for attempt := 0; attempt < createProposalMaxSyncRetries; attempt++ {
		currentView := hs.pacemaker.GetCurrentView()
		lastPacemakerView = currentView

		isLeader, err := hs.rotation.IsLeader(ctx, hs.crypto.GetKeyID(), currentView)
		if err != nil {
			return nil, fmt.Errorf("failed to check leader status: %w", err)
		}
		if !isLeader {
			return nil, fmt.Errorf("not the leader for view %d", currentView)
		}

		justifyQC := hs.pacemaker.GetHighestQC()
		proposalHeight := hs.pacemaker.GetCurrentHeight()

		hs.mu.RLock()
		hotStuffView := hs.currentView
		hotStuffHeight := hs.currentHeight
		hs.mu.RUnlock()

		lastHotStuffView = hotStuffView
		if hotStuffView != currentView {
			hs.logger.InfoContext(ctx, "[CREATE_PROPOSAL] view advanced before proposal assembled",
				"expected_view", currentView,
				"actual_view", hotStuffView,
				"attempt", attempt+1)
			time.Sleep(createProposalRetryDelay)
			continue
		}

		if proposalHeight < hotStuffHeight {
			proposalHeight = hotStuffHeight
		}

		var justifyQCMsg *messages.QC
		if justifyQC != nil {
			justifyQCMsg, err = convertToMessageQC(justifyQC)
			if err != nil {
				return nil, fmt.Errorf("failed to convert highest QC to message: %w", err)
			}
		}

		var parentHash [32]byte
		if justifyQCMsg != nil {
			parentHash = justifyQCMsg.BlockHash
		}

		hs.logger.InfoContext(ctx, "[CREATE_PROPOSAL] fetched height from pacemaker",
			"height", proposalHeight,
			"view", currentView)

		proposal := &messages.Proposal{
			View:       currentView,
			Height:     proposalHeight,
			Round:      0, // Single round per view in HotStuff
			BlockHash:  block.GetHash(),
			ParentHash: parentHash,
			ProposerID: hs.crypto.GetKeyID(),
			Timestamp:  time.Now(),
			JustifyQC:  justifyQCMsg, // Can be nil for genesis block or single-node mode
			Block:      block,
		}

		signBytes := proposal.SignBytes()
		signature, err := hs.crypto.SignWithContext(ctx, signBytes)
		if err != nil {
			hs.mu.Unlock()
			return nil, fmt.Errorf("failed to sign proposal: %w", err)
		}

		proposal.Signature = messages.Signature{
			Bytes:     signature,
			KeyID:     hs.crypto.GetKeyID(),
			Timestamp: time.Now(),
		}

		hs.logger.InfoContext(ctx, "[CREATE_PROPOSAL] storing proposal in storage",
			"height", proposal.Height,
			"view", proposal.View)
		if err := hs.storage.StoreProposal(proposal); err != nil {
			hs.logger.ErrorContext(ctx, "[CREATE_PROPOSAL] StoreProposal FAILED",
				"error", err,
				"height", proposal.Height,
				"storage_last_committed", hs.storage.GetLastCommittedHeight())
			return nil, fmt.Errorf("failed to store proposal: %w", err)
		}

		hs.logger.InfoContext(ctx, "[CREATE_PROPOSAL] proposal stored successfully",
			"height", proposal.Height)

		hs.logger.InfoContext(ctx, "proposal created",
			"view", proposal.View,
			"height", proposal.Height,
			"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
		)

		if hs.audit != nil {
			hs.audit.Info("proposal_created", map[string]interface{}{
				"view":       proposal.View,
				"height":     proposal.Height,
				"block_hash": fmt.Sprintf("%x", proposal.BlockHash[:]),
			})
		}
		return proposal, nil
	}

	return nil, fmt.Errorf("create proposal aborted: view sync timeout after %d attempts (pacemaker=%d hotstuff=%d)", createProposalMaxSyncRetries, lastPacemakerView, lastHotStuffView)
}

// convertToMessageQC converts any implementation of types.QC to the wire-format messages.QC.
// This is required because HotStuff internally uses pbft.QuorumCertificate while network
// messages expect *messages.QC. The fields between the two representations are equivalent.
func convertToMessageQC(qc types.QC) (*messages.QC, error) {
	switch typed := qc.(type) {
	case nil:
		return nil, nil
	case *messages.QC:
		return typed, nil
	case *QuorumCertificate:
		return &messages.QC{
			View:         typed.View,
			Height:       typed.Height,
			Round:        typed.Round,
			BlockHash:    typed.BlockHash,
			Signatures:   append([]types.Signature(nil), typed.Signatures...),
			Timestamp:    typed.Timestamp,
			AggregatorID: typed.AggregatorID,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported QC type %T", qc)
	}
}

// loadState loads consensus state from storage
func (hs *HotStuff) loadState(ctx context.Context) error {
	lastCommitted := hs.storage.GetLastCommittedHeight()
	lastQC := hs.storage.GetLastCommittedQC()

	// Initialize next height to propose.
	hs.currentHeight = lastCommitted + 1

	var lastQCView uint64
	if lastQC != nil {
		// Resuming from committed state
		hs.lockedQC = lastQC
		hs.prepareQC = lastQC
		hs.currentView = lastQC.GetView() + 1
		lastQCView = lastQC.GetView()
	} else {
		// Fresh start: no committed blocks yet
		// Start from view 0, height 1
		hs.currentView = 0
		lastQCView = 0
	}

	// Prune restored state that can poison restart liveness.
	// Keep our own vote history to avoid double-voting after restart.
	hs.storage.ClearStaleConsensusRecords(ctx, lastCommitted, lastQCView, hs.crypto.GetKeyID())

	hs.votesSent = hs.storage.GetVotesByVoterInReplayWindow(hs.crypto.GetKeyID())
	hs.pendingVotes = make(map[types.BlockHash]map[types.ValidatorID]*messages.Vote)

	hs.logger.InfoContext(ctx, "state loaded",
		"height", hs.currentHeight,
		"view", hs.currentView,
		"last_committed", lastCommitted,
		"last_committed_qc_view", lastQCView,
		"restored_local_vote_views", len(hs.votesSent),
	)

	return nil
}

// cleanupOldData removes old votes and proposals
func (hs *HotStuff) cleanupOldData(currentHeight uint64) {
	// Cleanup votes older than replay window
	if currentHeight > 100 {
		_ = currentHeight - 100 // cutoffHeight for potential future use
		for blockHash := range hs.pendingVotes {
			delete(hs.pendingVotes, blockHash)
		}
		// Storage will handle its own cleanup
	}

	// Cleanup old votesSent tracking
	for view := range hs.votesSent {
		if view < hs.currentView-100 {
			delete(hs.votesSent, view)
		}
	}
}

// GetCurrentView returns current view
func (hs *HotStuff) GetCurrentView() uint64 {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return hs.currentView
}

// GetCurrentHeight returns current height
func (hs *HotStuff) GetCurrentHeight() uint64 {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return hs.currentHeight
}

// GetLockedQC returns the locked QC
func (hs *HotStuff) GetLockedQC() types.QC {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return hs.lockedQC
}

// AdvanceView advances the consensus to a new view
func (hs *HotStuff) AdvanceView(ctx context.Context, newView uint64, highestQC types.QC) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if newView < hs.currentView {
		return fmt.Errorf("cannot advance to view %d, currently at view %d", newView, hs.currentView)
	}

	if newView == hs.currentView {
		// Idempotent: duplicate view-advance notifications can arrive via both OnViewChange and OnNewView.
		// Still accept QC/height updates.
		if highestQC != nil && (hs.lockedQC == nil || highestQC.GetView() > hs.lockedQC.GetView()) {
			hs.lockedQC = highestQC
			hs.prepareQC = highestQC
			if highestQC.GetHeight() >= hs.currentHeight {
				hs.currentHeight = highestQC.GetHeight() + 1
			}
		}
		return nil
	}

	oldView := hs.currentView
	hs.currentView = newView

	// Update locked QC if the new one is higher
	if highestQC != nil && (hs.lockedQC == nil || highestQC.GetView() > hs.lockedQC.GetView()) {
		hs.lockedQC = highestQC
		hs.prepareQC = highestQC
		// Advance height if needed
		if highestQC.GetHeight() >= hs.currentHeight {
			hs.currentHeight = highestQC.GetHeight() + 1
		}
	}

	hs.logger.InfoContext(ctx, "view advanced",
		"old_view", oldView,
		"new_view", newView,
		"height", hs.currentHeight,
		"locked_qc_view", getQCView(hs.lockedQC),
	)

	return nil
}

// Helper functions

func getQCView(qc types.QC) uint64 {
	if qc == nil {
		return 0
	}
	return qc.GetView()
}

func getFirstVoter(votes map[types.ValidatorID]*messages.Vote) types.ValidatorID {
	for id := range votes {
		return id
	}
	return types.ValidatorID{}
}

func getParentHash(block types.Block) [32]byte {
	// Blocks should have parent hash - this is a placeholder
	return [32]byte{}
}
