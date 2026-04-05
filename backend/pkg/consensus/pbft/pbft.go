package pbft

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"backend/pkg/block"
	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
	"backend/pkg/state"
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
	pendingVotes map[pendingVoteKey]map[types.ValidatorID]*messages.Vote // (view, blockHash) -> votes

	mu        sync.RWMutex
	stopCh    chan struct{}
	callbacks types.ConsensusCallbacks
}

type pendingVoteKey struct {
	view      uint64
	blockHash types.BlockHash
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
		pendingVotes: make(map[pendingVoteKey]map[types.ValidatorID]*messages.Vote),
		stopCh:       make(chan struct{}),
		callbacks:    callbacks,
	}
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
			hs.logger.InfoContext(ctx, "duplicate proposal received for view",
				"view", proposal.View,
				"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
			)
			return nil, nil // idempotent re-delivery: already processed this proposal
		}
		hs.logger.WarnContext(ctx, "already voted in this view",
			"view", proposal.View,
			"existing_block", fmt.Sprintf("%x", votedHash[:8]),
			"incoming_block", fmt.Sprintf("%x", proposal.BlockHash[:8]),
		)
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
		)
		return nil, fmt.Errorf("proposal conflicts with locked QC")
	}

	// Validate block content (if validator configured)
	if hs.config.BlockValidationFunc != nil {
		if err := hs.config.BlockValidationFunc(ctx, proposal.Block); err != nil {
			return nil, fmt.Errorf("block validation failed: %w", err)
		}
	}
	hs.emitPolicyStageForProposal(proposal, "t_replica_proposal_validated", time.Now().UnixMilli())

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
	hs.emitPolicyStageForProposal(proposal, "t_vote_generated", ts.UnixMilli())

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

	// Mark that we voted in this view
	hs.votesSent[proposal.View] = proposal.BlockHash

	// Store our vote
	if err := hs.storage.StoreVote(vote); err != nil {
		return nil, fmt.Errorf("failed to store vote: %w", err)
	}

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
	hs.emitPolicyStageForProposal(proposal, "t_vote_broadcast", time.Now().UnixMilli())

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

	// Add to pending votes bucket keyed by (view, block hash) to avoid cross-view mixing.
	key := pendingVoteKey{view: vote.View, blockHash: vote.BlockHash}
	if hs.pendingVotes[key] == nil {
		hs.pendingVotes[key] = make(map[types.ValidatorID]*messages.Vote)
	}
	// Defensive invariant: reject mixed height/round in the same QC bucket.
	for _, existing := range hs.pendingVotes[key] {
		if existing == nil {
			continue
		}
		if existing.Height != vote.Height || existing.Round != vote.Round {
			return nil, fmt.Errorf("invalid vote bucket invariant: mixed height/round for view %d block %x (existing h=%d r=%d, incoming h=%d r=%d)",
				vote.View, vote.BlockHash[:8], existing.Height, existing.Round, vote.Height, vote.Round)
		}
		break
	}
	hs.pendingVotes[key][types.ValidatorID(vote.VoterID)] = vote
	hs.emitPolicyStageForStoredProposal(vote.BlockHash, "t_vote_aggregated", time.Now().UnixMilli())

	// DEBUG: Log quorum check with detailed progress
	voteCount := len(hs.pendingVotes[key])
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
	if hs.quorum.HasQuorum(hs.pendingVotes[key]) {
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
	key := pendingVoteKey{view: view, blockHash: blockHash}
	votes := hs.pendingVotes[key]
	if len(votes) == 0 {
		return nil, fmt.Errorf("cannot form qc: no pending votes for view %d block %x", view, blockHash[:8])
	}

	hs.logger.InfoContext(ctx, "forming QC",
		"view", view,
		"block_hash", fmt.Sprintf("%x", blockHash[:8]),
		"vote_count", len(votes),
	)

	// Aggregate signatures and enforce strict per-QC vote invariants.
	firstVoter := getFirstVoter(votes)
	firstVote := votes[firstVoter]
	if firstVote == nil {
		delete(hs.pendingVotes, key)
		return nil, fmt.Errorf("cannot form qc: first vote is nil for view %d block %x", view, blockHash[:8])
	}
	signatures := make([]types.Signature, 0, len(votes))
	for _, vote := range votes {
		if vote == nil {
			delete(hs.pendingVotes, key)
			return nil, fmt.Errorf("cannot form qc: nil vote encountered for view %d block %x", view, blockHash[:8])
		}
		if vote.View != firstVote.View || vote.Height != firstVote.Height || vote.Round != firstVote.Round || vote.BlockHash != firstVote.BlockHash {
			delete(hs.pendingVotes, key)
			return nil, fmt.Errorf("cannot form qc: mixed vote set detected (expected v=%d h=%d r=%d hash=%x, got v=%d h=%d r=%d hash=%x)",
				firstVote.View, firstVote.Height, firstVote.Round, firstVote.BlockHash[:8],
				vote.View, vote.Height, vote.Round, vote.BlockHash[:8])
		}
		signatures = append(signatures, vote.Signature)
	}

	// Create QC
	qc := &QuorumCertificate{
		View:         firstVote.View,
		Height:       firstVote.Height,
		Round:        firstVote.Round,
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
	delete(hs.pendingVotes, key)

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
	hs.emitPolicyStageForStoredProposal(qc.GetBlockHash(), "t_2chain_commit_eval_start", time.Now().UnixMilli())

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

	// Check if this QC extends the lockedQC block via parent linkage.
	// Views can advance with timeouts; use the proposal's ParentHash chain rather than strict view adjacency.
	qcHash := qc.GetBlockHash()
	currentProposal := hs.storage.GetProposal(qcHash)
	if currentProposal == nil {
		hs.logger.WarnContext(ctx, "cannot evaluate commit rule: missing proposal for qc block",
			"qc_view", qc.GetView(),
			"qc_height", qc.GetHeight(),
			"block_hash", fmt.Sprintf("%x", qcHash[:8]))
	} else {
		lockedHash := hs.lockedQC.GetBlockHash()
		consecutiveHeight := qc.GetHeight() == hs.lockedQC.GetHeight()+1
		parentMatches := currentProposal.ParentHash == lockedHash
		hs.logger.InfoContext(ctx, "checking 2-chain by parent linkage",
			"qc_height", qc.GetHeight(),
			"locked_height", hs.lockedQC.GetHeight(),
			"consecutive_height", consecutiveHeight,
			"parent_matches_locked", parentMatches)

		if consecutiveHeight && parentMatches {
			// Commit the block from lockedQC (parent of current QC's block)
			hs.logger.InfoContext(ctx, "2-chain rule satisfied - committing lockedQC block",
				"commit_height", hs.lockedQC.GetHeight())
			hs.emitPolicyStageForStoredProposal(lockedHash, "t_2chain_commit_satisfied", time.Now().UnixMilli())
			proposal := hs.storage.GetProposal(lockedHash)
			if proposal == nil {
				hs.logger.ErrorContext(ctx, "cannot find proposal for committed block")
				return fmt.Errorf("cannot find proposal for committed block")
			}
			if proposal.Block == nil {
				hs.logger.ErrorContext(ctx, "proposal missing block payload",
					"view", hs.lockedQC.GetView(),
					"height", hs.lockedQC.GetHeight(),
					"block_hash", fmt.Sprintf("%x", lockedHash[:8]))
				return fmt.Errorf("proposal missing block payload for committed block")
			}

			return hs.commitBlock(ctx, proposal.Block, hs.lockedQC)
		}
	}

	// Update lockedQC if newer (prefer height monotonicity; break ties by view).
	hs.logger.InfoContext(ctx, "2-chain not satisfied - updating lockedQC",
		"old_locked_height", hs.lockedQC.GetHeight(),
		"new_qc_height", qc.GetHeight(),
		"old_locked_view", hs.lockedQC.GetView(),
		"new_qc_view", qc.GetView())
	if qc.GetHeight() > hs.lockedQC.GetHeight() || (qc.GetHeight() == hs.lockedQC.GetHeight() && qc.GetView() > hs.lockedQC.GetView()) {
		hs.lockedQC = qc
	}

	return nil
}

func (hs *HotStuff) emitPolicyStageForStoredProposal(blockHash types.BlockHash, stage string, tMs int64) {
	if hs == nil || hs.storage == nil {
		return
	}
	if proposal := hs.storage.GetProposal(blockHash); proposal != nil {
		hs.emitPolicyStageForProposal(proposal, stage, tMs)
	}
}

func (hs *HotStuff) emitPolicyStageForProposal(proposal *messages.Proposal, stage string, tMs int64) {
	if hs == nil || hs.logger == nil || proposal == nil || proposal.Block == nil || stage == "" {
		return
	}
	blk, ok := proposal.Block.(*block.AppBlock)
	if !ok || blk == nil {
		return
	}
	for _, tx := range blk.Transactions() {
		if tx == nil || tx.Type() != state.TxPolicy {
			continue
		}
		policyID, traceID := extractPolicyIdentityFromPayload(tx.Payload())
		if policyID == "" {
			continue
		}
		hs.logger.InfoContext(context.Background(), "policy stage marker",
			"stage", stage,
			"policy_id", policyID,
			"trace_id", traceID,
			"t_ms", tMs,
			"view", proposal.View,
			"height", proposal.Height,
			"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
		)
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

// commitBlock finalizes a block
func (hs *HotStuff) commitBlock(ctx context.Context, block types.Block, qc types.QC) error {
	h := block.GetHash()
	hs.logger.InfoContext(ctx, "committing block",
		"height", block.GetHeight(),
		"hash", fmt.Sprintf("%x", h[:8]),
		"view", qc.GetView(),
	)

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

	// Callback
	if hs.callbacks != nil {
		if err := hs.callbacks.OnCommit(block, qc); err != nil {
			return fmt.Errorf("commit callback failed: %w", err)
		}
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

		justifyQC, err = hs.sanitizeJustifyQC(ctx, justifyQC)
		if err != nil {
			return nil, fmt.Errorf("failed to sanitize highest QC: %w", err)
		}

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
		if justifyQC != nil {
			minHeight := justifyQC.GetHeight() + 1
			if proposalHeight < minHeight {
				proposalHeight = minHeight
			}
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
		} else if proposalHeight > 1 {
			if committedHash, ok := hs.storage.GetCommittedBlockHash(proposalHeight - 1); ok {
				parentHash = committedHash
			}
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

func (hs *HotStuff) sanitizeJustifyQC(ctx context.Context, qc types.QC) (types.QC, error) {
	if qc == nil {
		return nil, nil
	}
	if err := hs.validator.ValidateQC(ctx, qc); err == nil {
		return qc, nil
	}

	hs.logger.WarnContext(ctx, "highest qc failed validation; attempting repair from votes",
		"view", qc.GetView(),
		"height", qc.GetHeight(),
		"block_hash", func() string {
			hash := qc.GetBlockHash()
			return fmt.Sprintf("%x", hash[:8])
		}())

	rebuilt, err := hs.rebuildQCFromVotes(ctx, qc)
	if err != nil {
		if fallback := hs.bestValidatedFallbackQC(ctx, qc); fallback != nil {
			hs.logger.WarnContext(ctx, "using fallback justify qc after repair failure",
				"failed_qc_view", qc.GetView(),
				"failed_qc_height", qc.GetHeight(),
				"fallback_view", fallback.GetView(),
				"fallback_height", fallback.GetHeight(),
				"error", err)
			return fallback, nil
		}
		return nil, fmt.Errorf("rebuild qc from votes: %w", err)
	}
	if err := hs.validator.ValidateQC(ctx, rebuilt); err != nil {
		if fallback := hs.bestValidatedFallbackQC(ctx, qc); fallback != nil {
			hs.logger.WarnContext(ctx, "using fallback justify qc after rebuilt qc validation failure",
				"failed_qc_view", qc.GetView(),
				"failed_qc_height", qc.GetHeight(),
				"fallback_view", fallback.GetView(),
				"fallback_height", fallback.GetHeight(),
				"error", err)
			return fallback, nil
		}
		return nil, fmt.Errorf("rebuilt qc still invalid: %w", err)
	}

	if err := hs.storage.StoreQC(rebuilt); err != nil {
		hs.logger.WarnContext(ctx, "failed to persist repaired qc",
			"view", rebuilt.GetView(),
			"height", rebuilt.GetHeight(),
			"error", err)
	}

	hs.logger.InfoContext(ctx, "repaired highest qc from persisted votes",
		"view", rebuilt.GetView(),
		"height", rebuilt.GetHeight(),
		"block_hash", func() string {
			hash := rebuilt.GetBlockHash()
			return fmt.Sprintf("%x", hash[:8])
		}(),
		"signature_count", len(rebuilt.GetSignatures()))
	return rebuilt, nil
}

func (hs *HotStuff) bestValidatedFallbackQC(ctx context.Context, exclude types.QC) types.QC {
	if hs == nil || hs.validator == nil {
		return nil
	}

	hs.mu.RLock()
	locked := hs.lockedQC
	prepare := hs.prepareQC
	hs.mu.RUnlock()

	candidates := []types.QC{locked, prepare}
	if hs.storage != nil {
		candidates = append(candidates, hs.storage.GetLastCommittedQC())
	}

	var best types.QC
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if exclude != nil && candidate.GetView() == exclude.GetView() &&
			candidate.GetHeight() == exclude.GetHeight() &&
			candidate.GetBlockHash() == exclude.GetBlockHash() {
			continue
		}
		if err := hs.validator.ValidateQC(ctx, candidate); err != nil {
			continue
		}
		if best == nil ||
			candidate.GetHeight() > best.GetHeight() ||
			(candidate.GetHeight() == best.GetHeight() && candidate.GetView() > best.GetView()) {
			best = candidate
		}
	}
	return best
}

func (hs *HotStuff) rebuildQCFromVotes(ctx context.Context, qc types.QC) (types.QC, error) {
	if qc == nil {
		return nil, fmt.Errorf("qc is nil")
	}

	votesByValidator := make(map[types.ValidatorID]*messages.Vote)
	discardedVotes := 0
	discardedByReason := make(map[string]int)
	discardedSamples := make([]string, 0, 3)
	for _, vote := range hs.storage.GetVotesByView(qc.GetView()) {
		if vote == nil {
			continue
		}
		if vote.View != qc.GetView() || vote.Height != qc.GetHeight() || vote.BlockHash != qc.GetBlockHash() {
			continue
		}
		if err := hs.verifyPersistedVoteForQCRepair(ctx, vote); err != nil {
			discardedVotes++
			reason := classifyQCRepairVoteError(err)
			discardedByReason[reason]++
			if len(discardedSamples) < cap(discardedSamples) {
				discardedSamples = append(discardedSamples, fmt.Sprintf("voter=%x reason=%s", vote.VoterID[:8], reason))
			}
			continue
		}
		id := types.ValidatorID(vote.VoterID)
		if existing, ok := votesByValidator[id]; !ok || vote.Timestamp.Before(existing.Timestamp) {
			votesByValidator[id] = vote
		}
	}
	if !hs.quorum.HasQuorum(votesByValidator) {
		return nil, fmt.Errorf("have %d valid matching votes after discarding %d invalid votes, quorum requires %d (reasons=%v samples=%v)",
			len(votesByValidator), discardedVotes, hs.quorum.GetQuorumThreshold(), discardedByReason, discardedSamples)
	}
	if discardedVotes > 0 {
		hs.logger.WarnContext(ctx, "discarded invalid persisted votes during qc repair",
			"view", qc.GetView(),
			"height", qc.GetHeight(),
			"discarded", discardedVotes,
			"reason_counts", discardedByReason,
			"samples", discardedSamples)
	}

	rebuilt, err := hs.quorum.AggregateVotes(ctx, votesByValidator, hs.crypto.GetKeyID())
	if err != nil {
		return nil, err
	}

	sigs := append([]types.Signature(nil), rebuilt.GetSignatures()...)
	sort.SliceStable(sigs, func(i, j int) bool {
		return strings.Compare(fmt.Sprintf("%x", sigs[i].KeyID[:]), fmt.Sprintf("%x", sigs[j].KeyID[:])) < 0
	})

	switch typed := rebuilt.(type) {
	case *QuorumCertificate:
		typed.Signatures = sigs
		typed.Timestamp = latestVoteTimestamp(votesByValidator)
		return typed, nil
	case *messages.QC:
		typed.Signatures = sigs
		typed.Timestamp = latestVoteTimestamp(votesByValidator)
		return typed, nil
	default:
		return rebuilt, nil
	}
}

func classifyQCRepairVoteError(err error) string {
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "not in validator set"):
		return "validator_not_in_set"
	case strings.Contains(msg, "not active in view"):
		return "validator_inactive_in_view"
	case strings.Contains(msg, "signature key") && strings.Contains(msg, "does not match voter"):
		return "signature_key_mismatch"
	case strings.Contains(msg, "load public key"):
		return "public_key_load_failed"
	case strings.Contains(msg, "verify signature"):
		return "signature_verify_failed"
	default:
		return "other"
	}
}

func (hs *HotStuff) verifyPersistedVoteForQCRepair(ctx context.Context, vote *messages.Vote) error {
	if vote == nil {
		return fmt.Errorf("vote is nil")
	}
	if vote.Signature.KeyID != vote.VoterID {
		return fmt.Errorf("signature key %x does not match voter %x", vote.Signature.KeyID[:8], vote.VoterID[:8])
	}
	if !hs.validatorSet.IsValidator(vote.VoterID) {
		return fmt.Errorf("voter %x is not in validator set", vote.VoterID[:8])
	}
	publicKey, err := hs.crypto.GetPublicKey(vote.VoterID)
	if err != nil {
		return fmt.Errorf("load public key for voter %x: %w", vote.VoterID[:8], err)
	}
	// Persisted votes can legitimately be older than replay freshness windows.
	if err := hs.crypto.VerifyWithContext(ctx, vote.SignBytes(), vote.Signature.Bytes, publicKey, true); err != nil {
		return fmt.Errorf("verify signature for voter %x: %w", vote.VoterID[:8], err)
	}
	return nil
}

func latestVoteTimestamp(votes map[types.ValidatorID]*messages.Vote) time.Time {
	var latest time.Time
	for _, vote := range votes {
		if vote != nil && vote.Timestamp.After(latest) {
			latest = vote.Timestamp
		}
	}
	return latest
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

	// CRITICAL: Always initialize currentHeight for the next block to propose
	// This must be set even on fresh start (lastCommitted=0, lastQC=nil)
	// Replay window expects [lastCommitted+1, lastCommitted+100]
	hs.currentHeight = lastCommitted + 1

	if lastQC != nil {
		// Resuming from committed state
		hs.lockedQC = lastQC
		hs.prepareQC = lastQC
		hs.currentView = lastQC.GetView() + 1
	} else {
		// Fresh start: no committed blocks yet
		// Start from view 0, height 1
		hs.currentView = 0
	}

	hs.logger.InfoContext(ctx, "state loaded",
		"height", hs.currentHeight,
		"view", hs.currentView,
		"last_committed", lastCommitted,
	)

	return nil
}

// cleanupOldData removes old votes and proposals
func (hs *HotStuff) cleanupOldData(currentHeight uint64) {
	// Cleanup votes older than replay window
	if currentHeight > 100 {
		_ = currentHeight - 100 // cutoffHeight for potential future use
		for key := range hs.pendingVotes {
			delete(hs.pendingVotes, key)
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

// CanRestartBootstrap reports whether the next proposal may omit JustifyQC while
// re-establishing consensus from a durable committed tip.
func (hs *HotStuff) CanRestartBootstrap(parentHash types.BlockHash, proposalHeight uint64) bool {
	return hs.storage.AllowRestartBootstrap(parentHash, proposalHeight)
}

// AdvanceView advances the consensus to a new view
func (hs *HotStuff) AdvanceView(ctx context.Context, newView uint64, highestQC types.QC) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if newView <= hs.currentView {
		return fmt.Errorf("cannot advance to view %d, currently at view %d", newView, hs.currentView)
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
