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
	votesSent     map[uint64]bool // Tracks views where we voted (safety)

	// Proposal tracking
	pendingVotes map[types.BlockHash]map[types.ValidatorID]*messages.Vote // blockHash -> votes

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
		votesSent:    make(map[uint64]bool),
		pendingVotes: make(map[types.BlockHash]map[types.ValidatorID]*messages.Vote),
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
	defer hs.mu.Unlock()

	hs.logger.InfoContext(ctx, "received proposal",
		"view", proposal.View,
		"height", proposal.Height,
		"block_hash", fmt.Sprintf("%x", proposal.BlockHash[:8]),
		"proposer", fmt.Sprintf("%x", proposal.ProposerID[:8]),
	)

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
		return fmt.Errorf("proposal validation failed: %w", err)
	}

	// Check if we already voted in this view (safety rule)
	if hs.votesSent[proposal.View] {
		hs.logger.WarnContext(ctx, "already voted in this view",
			"view", proposal.View,
		)
		return fmt.Errorf("already voted in view %d", proposal.View)
	}

	// Validate justifyQC
	if proposal.JustifyQC != nil {
		if err := hs.validator.ValidateQC(ctx, proposal.JustifyQC); err != nil {
			return fmt.Errorf("invalid justifyQC: %w", err)
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
		return fmt.Errorf("proposal conflicts with locked QC")
	}

	// Validate block content (if validator configured)
	if hs.config.BlockValidationFunc != nil {
		if err := hs.config.BlockValidationFunc(ctx, proposal.Block); err != nil {
			return fmt.Errorf("block validation failed: %w", err)
		}
	}

	// Store proposal
	if err := hs.storage.StoreProposal(proposal); err != nil {
		return fmt.Errorf("failed to store proposal: %w", err)
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
		return hs.sendVote(ctx, proposal)
	}

	return nil
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

// sendVote creates and sends a vote for a proposal
func (hs *HotStuff) sendVote(ctx context.Context, proposal *messages.Proposal) error {
	vote := &messages.Vote{
		View:      proposal.View,
		Height:    proposal.Height,
		Round:     proposal.Round,
		BlockHash: proposal.BlockHash,
		VoterID:   messages.KeyID(hs.crypto.GetKeyID()),
		Timestamp: time.Now(),
	}

	// Sign vote
	signBytes := vote.SignBytes()
	signature, err := hs.crypto.SignWithContext(ctx, signBytes)
	if err != nil {
		return fmt.Errorf("failed to sign vote: %w", err)
	}

	vote.Signature = messages.Signature{
		Bytes:     signature,
		KeyID:     hs.crypto.GetKeyID(),
		Timestamp: time.Now(),
	}

	// Mark that we voted in this view
	hs.votesSent[proposal.View] = true

	// Store our vote
	if err := hs.storage.StoreVote(vote); err != nil {
		return fmt.Errorf("failed to store vote: %w", err)
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

	// If we're the leader, process our own vote (MUST be called while holding lock from OnProposal)
	isLeader, _ := hs.rotation.IsLeader(ctx, hs.crypto.GetKeyID(), proposal.View)
	hs.logger.InfoContext(ctx, "leader check", "is_leader", isLeader, "view", proposal.View)
	if isLeader {
		hs.logger.InfoContext(ctx, "calling onVoteInternal as leader")
		err := hs.onVoteInternal(ctx, vote)
		if err != nil {
			hs.logger.ErrorContext(ctx, "onVoteInternal failed", "error", err)
		}
		return err
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
	return nil
}

// OnVote processes a received vote (leader path) - external entry point with locking
func (hs *HotStuff) OnVote(ctx context.Context, vote *messages.Vote) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	return hs.onVoteInternal(ctx, vote)
}

// onVoteInternal processes a vote without locking (called internally when lock is already held)
func (hs *HotStuff) onVoteInternal(ctx context.Context, vote *messages.Vote) error {

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
		return fmt.Errorf("vote validation failed: %w", err)
	}
	hs.logger.InfoContext(ctx, "OnVote: validation PASSED")

	// Check for equivocation (double voting)
	hs.logger.InfoContext(ctx, "OnVote: checking equivocation")
	if err := hs.detectVoteEquivocation(ctx, vote); err != nil {
		hs.logger.ErrorContext(ctx, "OnVote: equivocation detected", "error", err)
		return err
	}
	hs.logger.InfoContext(ctx, "OnVote: no equivocation")

	// Store vote
	hs.logger.InfoContext(ctx, "OnVote: storing vote")
	if err := hs.storage.StoreVote(vote); err != nil {
		hs.logger.ErrorContext(ctx, "OnVote: storage FAILED", "error", err)
		return fmt.Errorf("failed to store vote: %w", err)
	}
	hs.logger.InfoContext(ctx, "OnVote: vote stored")

	// Add to pending votes
	if hs.pendingVotes[vote.BlockHash] == nil {
		hs.pendingVotes[vote.BlockHash] = make(map[types.ValidatorID]*messages.Vote)
	}
	hs.pendingVotes[vote.BlockHash][types.ValidatorID(vote.VoterID)] = vote

	// DEBUG: Log quorum check
	voteCount := len(hs.pendingVotes[vote.BlockHash])
	validatorCount := hs.validatorSet.GetValidatorCount()
	quorumThreshold := hs.quorum.GetQuorumThreshold()
	hs.logger.InfoContext(ctx, "quorum check",
		"vote_count", voteCount,
		"validator_count", validatorCount,
		"quorum_threshold", quorumThreshold,
		"block_hash", fmt.Sprintf("%x", vote.BlockHash[:8]),
	)

	// Check if we have quorum
	if hs.quorum.HasQuorum(hs.pendingVotes[vote.BlockHash]) {
		hs.logger.InfoContext(ctx, "QUORUM REACHED - forming QC")
		return hs.formQC(ctx, vote.BlockHash, vote.View)
	}

	hs.logger.WarnContext(ctx, "quorum NOT reached", "need", quorumThreshold, "have", voteCount)
	return nil
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

// formQC aggregates votes into a Quorum Certificate
func (hs *HotStuff) formQC(ctx context.Context, blockHash [32]byte, view uint64) error {
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
		return fmt.Errorf("QC validation failed: %w", err)
	}

	// Store QC
	if err := hs.storage.StoreQC(qc); err != nil {
		return fmt.Errorf("failed to store QC: %w", err)
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

	// Notify pacemaker
	hs.pacemaker.OnQC(ctx, qc)

	// Check commit rule (2-chain)
	if err := hs.checkCommitRule(ctx, qc); err != nil {
		hs.logger.ErrorContext(ctx, "commit check failed", "error", err)
	}

	// Callback
	if hs.callbacks != nil {
		if err := hs.callbacks.OnQCFormed(qc); err != nil {
			hs.logger.ErrorContext(ctx, "QC callback failed", "error", err)
		}
	}

	// Cleanup pending votes
	delete(hs.pendingVotes, blockHash)

	return nil
}

// checkCommitRule implements HotStuff 2-chain commit rule
func (hs *HotStuff) checkCommitRule(ctx context.Context, qc types.QC) error {
	// 2-chain rule: commit if we have QC(view N) and QC(view N-1) for consecutive blocks
	// QC → parentQC (consecutive views = commit)

	// CRITICAL FIX (BUG-015): Single-node mode immediate commit
	// In single-node mode (1 validator), there's no Byzantine fault risk.
	// Commit immediately when QC is formed to avoid getting stuck.
	validatorCount := hs.validatorSet.GetValidatorCount()
	if validatorCount == 1 {
		// Single-node mode: commit immediately
		proposal := hs.storage.GetProposal(qc.GetBlockHash())
		if proposal == nil {
			return fmt.Errorf("cannot find proposal for QC block")
		}
		
		hs.logger.InfoContext(ctx, "single-node mode: committing immediately",
			"height", proposal.Block.GetHeight(),
			"view", qc.GetView(),
		)
		
		return hs.commitBlock(ctx, proposal.Block, qc)
	}

	// Multi-node mode: use 2-chain rule
	if hs.lockedQC == nil {
		hs.lockedQC = qc
		return nil
	}

	// Check if this QC and previous QC form a 2-chain
	if qc.GetView() == hs.lockedQC.GetView()+1 {
		// Consecutive views - commit the block from lockedQC
		proposal := hs.storage.GetProposal(hs.lockedQC.GetBlockHash())
		if proposal == nil {
			return fmt.Errorf("cannot find proposal for committed block")
		}

		return hs.commitBlock(ctx, proposal.Block, hs.lockedQC)
	}

	// Update lockedQC if newer
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
	hs.mu.Lock()
	defer hs.mu.Unlock()

	// Verify we're the leader
	currentView := hs.pacemaker.GetCurrentView()
	isLeader, err := hs.rotation.IsLeader(ctx, hs.crypto.GetKeyID(), currentView)
	if err != nil {
		return nil, fmt.Errorf("failed to check leader status: %w", err)
	}
	if !isLeader {
		return nil, fmt.Errorf("not the leader for view %d", currentView)
	}

	// Get highest QC as justification
	justifyQC := hs.pacemaker.GetHighestQC()

	// Handle nil QC (genesis block or single-node mode)
	var justifyQCMsg *messages.QC
	if justifyQC != nil {
		justifyQCMsg = justifyQC.(*messages.QC)
	}

	proposal := &messages.Proposal{
		View:       currentView,
		Height:     hs.currentHeight,
		Round:      0, // Single round per view in HotStuff
		BlockHash:  block.GetHash(),
		ParentHash: getParentHash(block),
		ProposerID: hs.crypto.GetKeyID(),
		Timestamp:  time.Now(),
		JustifyQC:  justifyQCMsg, // Can be nil for genesis block or single-node mode
		Block:      block,
	}

	// Sign proposal
	signBytes := proposal.SignBytes()
	signature, err := hs.crypto.SignWithContext(ctx, signBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to sign proposal: %w", err)
	}

	proposal.Signature = messages.Signature{
		Bytes:     signature,
		KeyID:     hs.crypto.GetKeyID(),
		Timestamp: time.Now(),
	}

	// Store proposal
	if err := hs.storage.StoreProposal(proposal); err != nil {
		return nil, fmt.Errorf("failed to store proposal: %w", err)
	}

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
