package pbft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"

	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
)

// QuorumVerifier handles quorum certificate verification
type QuorumVerifier struct {
	validatorSet ValidatorSet
	encoder      MessageEncoder
	config       *QuorumConfig
	audit        AuditLogger
	logger       Logger

	// QC cache for performance
	qcCache *expirable.LRU[BlockHash, bool]

	mu sync.RWMutex
}

// QuorumConfig contains quorum verification parameters
type QuorumConfig struct {
	EnableCache         bool
	CacheSize           int
	CacheTTL            int64 // seconds
	StrictValidation    bool
	RequireUniqueVoters bool
	AllowSelfVoting     bool
}

// DefaultQuorumConfig returns secure defaults
func DefaultQuorumConfig() *QuorumConfig {
	return &QuorumConfig{
		EnableCache:         true,
		CacheSize:           10000,
		CacheTTL:            300, // 5 minutes
		StrictValidation:    true,
		RequireUniqueVoters: true,
		AllowSelfVoting:     false,
	}
}

// NewQuorumVerifier creates a new quorum verifier
func NewQuorumVerifier(
	validatorSet ValidatorSet,
	encoder MessageEncoder,
	audit AuditLogger,
	logger Logger,
	config *QuorumConfig,
) *QuorumVerifier {
	if config == nil {
		config = DefaultQuorumConfig()
	}

	var cache *expirable.LRU[BlockHash, bool]
	if config.EnableCache {
		cache = expirable.NewLRU[BlockHash, bool](
			config.CacheSize,
			nil,
			time.Duration(config.CacheTTL)*time.Second,
		)
	}

	return &QuorumVerifier{
		validatorSet: validatorSet,
		encoder:      encoder,
		config:       config,
		audit:        audit,
		logger:       logger,
		qcCache:      cache,
	}
}

// HasQuorum checks if a set of votes forms a quorum
func (qv *QuorumVerifier) HasQuorum(votes map[ValidatorID]*messages.Vote) bool {
	totalValidators := qv.validatorSet.GetValidatorCount()
	voteCount := len(votes)

	// Calculate f and quorum threshold
	f := qv.calculateF(totalValidators)
	quorum := qv.calculateQuorum(f)

	return voteCount >= quorum
}

// VerifyQC verifies a Quorum Certificate
func (qv *QuorumVerifier) VerifyQC(ctx context.Context, qc QC) error {
	qv.mu.Lock()
	defer qv.mu.Unlock()

	// Check cache first
	if qv.config.EnableCache && qv.qcCache != nil {
		qcHash := qc.Hash()
		if verified, ok := qv.qcCache.Get(qcHash); ok && verified {
			return nil
		}
	}

	// Perform verification
	if err := qv.verifyQCInternal(ctx, qc); err != nil {
		return err
	}

	// Cache successful verification
	if qv.config.EnableCache && qv.qcCache != nil {
		qv.qcCache.Add(qc.Hash(), true)
	}

	return nil
}

// verifyQCInternal performs the actual QC verification
func (qv *QuorumVerifier) verifyQCInternal(ctx context.Context, qc QC) error {
	// Check QC is not nil
	if qc == nil {
		return fmt.Errorf("QC is nil")
	}

	// Get signatures
	signatures := qc.GetSignatures()
	if len(signatures) == 0 {
		return fmt.Errorf("QC has no signatures")
	}

	// Calculate required quorum
	totalValidators := qv.validatorSet.GetValidatorCount()
	f := qv.calculateF(totalValidators)
	requiredQuorum := qv.calculateQuorum(f)

	// Check signature count meets quorum
	sigCount := len(signatures)
	if sigCount < requiredQuorum {
		qv.audit.Warn("qc_insufficient_signatures", map[string]interface{}{
			"view":             qc.GetView(),
			"signature_count":  sigCount,
			"required_quorum":  requiredQuorum,
			"total_validators": totalValidators,
			"f":                f,
		})
		return fmt.Errorf("QC has %d signatures, quorum requires %d (N=%d, f=%d)",
			sigCount, requiredQuorum, totalValidators, f)
	}

	// Verify all signers are valid validators
	seenValidators := make(map[ValidatorID]bool)
	for i, sig := range signatures {
		// Check for duplicate signers (FIX: Issue #15 - create Evidence)
		if qv.config.RequireUniqueVoters && seenValidators[sig.KeyID] {
			qv.audit.Security("qc_duplicate_signer", map[string]interface{}{
				"view":      qc.GetView(),
				"signer":    fmt.Sprintf("%x", sig.KeyID[:]),
				"signature": i,
			})

			// FIX Issue #15: Create Evidence for duplicate signer
			evidence := qv.createDuplicateSignerEvidence(ctx, qc, sig.KeyID, i)
			if evidence != nil {
				qv.audit.Security("duplicate_signer_evidence_created", map[string]interface{}{
					"offender": fmt.Sprintf("%x", sig.KeyID[:]),
					"view":     qc.GetView(),
				})
			}

			return fmt.Errorf("QC contains duplicate signature from validator %x", sig.KeyID[:8])
		}
		seenValidators[sig.KeyID] = true

		// Verify signer is a validator
		if !qv.validatorSet.IsValidator(sig.KeyID) {
			qv.audit.Warn("qc_invalid_signer", map[string]interface{}{
				"view":      qc.GetView(),
				"signer":    fmt.Sprintf("%x", sig.KeyID[:]),
				"signature": i,
			})
			return fmt.Errorf("QC signature %d from non-validator %x", i, sig.KeyID[:8])
		}

		// Verify signer was active in the QC's view
		if !qv.validatorSet.IsActiveInView(sig.KeyID, qc.GetView()) {
			qv.audit.Warn("qc_inactive_signer", map[string]interface{}{
				"view":      qc.GetView(),
				"signer":    fmt.Sprintf("%x", sig.KeyID[:]),
				"signature": i,
			})
			return fmt.Errorf("QC signature %d from inactive validator %x in view %d",
				i, sig.KeyID[:8], qc.GetView())
		}
	}

	// Verify cryptographic signatures
	if err := qv.encoder.VerifyQC(ctx, qc); err != nil {
		return fmt.Errorf("QC signature verification failed: %w", err)
	}

	h := qc.GetBlockHash()
	qv.logger.InfoContext(ctx, "QC verified",
		"view", qc.GetView(),
		"block_hash", fmt.Sprintf("%x", h[:8]),
		"signature_count", sigCount,
	)

	return nil
}

// createDuplicateSignerEvidence creates Evidence for duplicate signers (FIX: Issue #15)
func (qv *QuorumVerifier) createDuplicateSignerEvidence(ctx context.Context, qc QC, offenderID ValidatorID, sigIndex int) *messages.Evidence {
	// In a real implementation, we would extract the conflicting signatures
	// and package them as proof. For now, we create a minimal evidence structure.

	evidence := &messages.Evidence{
		Kind:       types.EvidenceInvalidSignature,
		View:       qc.GetView(),
		Height:     qc.GetHeight(),
		OffenderID: offenderID,
		Proof:      []byte(fmt.Sprintf("duplicate_signer_in_qc:view=%d:index=%d", qc.GetView(), sigIndex)),
		ReporterID: ValidatorID{}, // Would be set by the reporter
		Timestamp:  time.Now(),
	}

	// Note: In production, the calling code should sign this evidence
	return evidence
}

// calculateF computes the maximum number of Byzantine faults
// f = (N - 1) / 3
func (qv *QuorumVerifier) calculateF(totalValidators int) int {
	if totalValidators <= 0 {
		return 0
	}
	return (totalValidators - 1) / 3
}

// calculateQuorum computes the required quorum size
// quorum = 2f + 1
func (qv *QuorumVerifier) calculateQuorum(f int) int {
	return 2*f + 1
}

// GetQuorumThreshold returns the current quorum threshold
func (qv *QuorumVerifier) GetQuorumThreshold() int {
	totalValidators := qv.validatorSet.GetValidatorCount()
	f := qv.calculateF(totalValidators)
	return qv.calculateQuorum(f)
}

// GetByzantineTolerance returns the number of Byzantine faults tolerated
func (qv *QuorumVerifier) GetByzantineTolerance() int {
	totalValidators := qv.validatorSet.GetValidatorCount()
	return qv.calculateF(totalValidators)
}

// ValidateQuorumMath checks if the validator set can form quorum
func (qv *QuorumVerifier) ValidateQuorumMath() error {
	totalValidators := qv.validatorSet.GetValidatorCount()

	if totalValidators < 4 {
		return fmt.Errorf("insufficient validators for BFT: need at least 4, have %d",
			totalValidators)
	}

	f := qv.calculateF(totalValidators)
	quorum := qv.calculateQuorum(f)

	if quorum > totalValidators {
		return fmt.Errorf("invalid quorum math: quorum %d exceeds total validators %d",
			quorum, totalValidators)
	}

	// Check we can actually tolerate f failures
	if totalValidators-f < quorum {
		return fmt.Errorf("cannot tolerate f=%d failures with N=%d validators",
			f, totalValidators)
	}

	return nil
}

// AggregateVotes creates a QC from a set of votes
func (qv *QuorumVerifier) AggregateVotes(
	ctx context.Context,
	votes map[ValidatorID]*messages.Vote,
	aggregatorID ValidatorID,
) (QC, error) {
	qv.mu.Lock()
	defer qv.mu.Unlock()

	// Check we have quorum
	if !qv.HasQuorum(votes) {
		return nil, fmt.Errorf("insufficient votes for quorum: have %d, need %d",
			len(votes), qv.GetQuorumThreshold())
	}

	// Extract common fields (all votes should match)
	var firstVote *messages.Vote
	for _, v := range votes {
		firstVote = v
		break
	}

	if firstVote == nil {
		return nil, fmt.Errorf("no votes provided")
	}

	// Verify all votes are for the same block
	for _, vote := range votes {
		if vote.BlockHash != firstVote.BlockHash {
			return nil, fmt.Errorf("votes for different blocks: %x vs %x",
				vote.BlockHash[:8], firstVote.BlockHash[:8])
		}
		if vote.View != firstVote.View {
			return nil, fmt.Errorf("votes for different views: %d vs %d",
				vote.View, firstVote.View)
		}
		if vote.Height != firstVote.Height {
			return nil, fmt.Errorf("votes for different heights: %d vs %d",
				vote.Height, firstVote.Height)
		}
	}

	// Aggregate signatures
	signatures := make([]Signature, 0, len(votes))
	for _, vote := range votes {
		signatures = append(signatures, vote.Signature)
	}

	// Create concrete QC (would import from messages in real implementation)
	// For now, return interface-compatible structure
	qc := &QuorumCertificate{
		View:         firstVote.View,
		Height:       firstVote.Height,
		Round:        firstVote.Round,
		BlockHash:    firstVote.BlockHash,
		Signatures:   signatures,
		Timestamp:    firstVote.Timestamp,
		AggregatorID: aggregatorID,
	}

	qv.logger.InfoContext(ctx, "QC aggregated",
		"view", qc.View,
		"block_hash", fmt.Sprintf("%x", qc.BlockHash[:8]),
		"vote_count", len(signatures),
	)

	qv.audit.Info("qc_aggregated", map[string]interface{}{
		"view":       qc.View,
		"height":     qc.Height,
		"block_hash": fmt.Sprintf("%x", qc.BlockHash[:]),
		"vote_count": len(signatures),
	})

	return qc, nil
}

// ClearCache clears the QC verification cache
func (qv *QuorumVerifier) ClearCache() {
	qv.mu.Lock()
	defer qv.mu.Unlock()

	if qv.qcCache != nil {
		qv.qcCache.Purge()
	}
}

// GetCacheStats returns cache statistics
func (qv *QuorumVerifier) GetCacheStats() (size, capacity int) {
	qv.mu.RLock()
	defer qv.mu.RUnlock()

	if qv.qcCache == nil {
		return 0, 0
	}

	return qv.qcCache.Len(), qv.config.CacheSize
}

// QuorumCertificate represents an aggregated quorum certificate
type QuorumCertificate struct {
	View         uint64
	Height       uint64
	Round        uint64
	BlockHash    BlockHash
	Signatures   []Signature
	Timestamp    time.Time
	AggregatorID ValidatorID
}

// GetView implements QC interface
func (qc *QuorumCertificate) GetView() uint64 {
	return qc.View
}

// GetHeight implements QC interface
func (qc *QuorumCertificate) GetHeight() uint64 {
	return qc.Height
}

// GetBlockHash implements QC interface
func (qc *QuorumCertificate) GetBlockHash() BlockHash {
	return qc.BlockHash
}

// GetSignatures implements QC interface
func (qc *QuorumCertificate) GetSignatures() []Signature {
	return qc.Signatures
}

// GetTimestamp implements QC interface
func (qc *QuorumCertificate) GetTimestamp() time.Time {
	return qc.Timestamp
}

// Hash returns the hash of the QC
func (qc *QuorumCertificate) Hash() BlockHash {
	// Simplified hash - in production use proper hashing
	return qc.BlockHash
}

// QuorumStats contains quorum statistics
type QuorumStats struct {
	TotalValidators    int
	ByzantineTolerance int
	QuorumThreshold    int
	CacheSize          int
	CacheCapacity      int
}

// GetStats returns current quorum statistics
func (qv *QuorumVerifier) GetStats() QuorumStats {
	totalValidators := qv.validatorSet.GetValidatorCount()
	f := qv.calculateF(totalValidators)
	quorum := qv.calculateQuorum(f)
	cacheSize, cacheCapacity := qv.GetCacheStats()

	return QuorumStats{
		TotalValidators:    totalValidators,
		ByzantineTolerance: f,
		QuorumThreshold:    quorum,
		CacheSize:          cacheSize,
		CacheCapacity:      cacheCapacity,
	}
}
