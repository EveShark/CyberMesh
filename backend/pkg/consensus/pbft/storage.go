package pbft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"backend/pkg/consensus/messages"
	"github.com/fxamacker/cbor/v2"
)

// Storage manages consensus state persistence with replay window
type Storage struct {
	config *StorageConfig
	audit  AuditLogger
	logger Logger

	// In-memory storage (replay window)
	proposals map[BlockHash]*messages.Proposal
	votes     map[uint64][]*messages.Vote // view -> votes
	qcs       map[BlockHash]QC
	evidence  map[BlockHash]*messages.Evidence
	blocks    map[uint64]Block // height -> block

	// Committed state
	lastCommitted   uint64
	lastQC          QC
	committedBlocks map[uint64]BlockHash // height -> hash

	// Persistent storage backend
	backend StorageBackend

	encMode cbor.EncMode
	decMode cbor.DecMode
	limits  *messages.EncoderConfig

	mu sync.RWMutex
}

type voteBackend interface {
	SaveVote(view uint64, key []byte, data []byte) error
}

// StorageConfig contains storage parameters
type StorageConfig struct {
	ReplayWindowSize  int
	MaxProposals      int
	MaxVotesPerView   int
	MaxQCs            int
	MaxEvidence       int
	EnablePersistence bool
	PersistInterval   time.Duration
	AutoPrune         bool
	MinRetainBlocks   int
}

// DefaultStorageConfig returns secure defaults
func DefaultStorageConfig() *StorageConfig {
	return &StorageConfig{
		ReplayWindowSize:  100,
		MaxProposals:      1000,
		MaxVotesPerView:   10000,
		MaxQCs:            1000,
		MaxEvidence:       10000,
		EnablePersistence: true,
		PersistInterval:   10 * time.Second,
		AutoPrune:         true,
		MinRetainBlocks:   10,
	}
}

// NewStorage creates a new consensus storage
func NewStorage(
	backend StorageBackend,
	audit AuditLogger,
	logger Logger,
	config *StorageConfig,
) *Storage {
	if config == nil {
		config = DefaultStorageConfig()
	}

	var encMode cbor.EncMode
	if em, err := cbor.CanonicalEncOptions().EncMode(); err == nil {
		encMode = em
	} else if logger != nil {
		logger.WarnContext(context.Background(), "failed to initialize cbor encoder", "error", err)
	}

	var decMode cbor.DecMode
	decOpts := cbor.DecOptions{
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		IndefLength:      cbor.IndefLengthForbidden,
		IntDec:           cbor.IntDecConvertNone,
		MaxArrayElements: 10000,
		MaxMapPairs:      1000,
		MaxNestedLevels:  16,
	}
	if dm, err := decOpts.DecMode(); err == nil {
		decMode = dm
	} else if logger != nil {
		logger.WarnContext(context.Background(), "failed to initialize cbor decoder", "error", err)
	}

	limits := messages.DefaultEncoderConfig()

	return &Storage{
		config:          config,
		audit:           audit,
		logger:          logger,
		proposals:       make(map[BlockHash]*messages.Proposal),
		votes:           make(map[uint64][]*messages.Vote),
		qcs:             make(map[BlockHash]QC),
		evidence:        make(map[BlockHash]*messages.Evidence),
		blocks:          make(map[uint64]Block),
		committedBlocks: make(map[uint64]BlockHash),
		backend:         backend,
		encMode:         encMode,
		decMode:         decMode,
		limits:          limits,
	}
}

// Start initializes storage and loads persisted state
func (s *Storage) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load last committed state from backend
	if s.config.EnablePersistence && s.backend != nil {
		lastHeight, qcData, err := s.backend.LoadLastCommitted()
		if err != nil {
			s.logger.WarnContext(ctx, "failed to load last committed state", "error", err)
		} else {
			s.lastCommitted = lastHeight
			if len(qcData) > 0 {
				qc, decodeErr := s.decodeQC(qcData)
				if decodeErr != nil {
					s.logger.WarnContext(ctx, "failed to decode last committed QC", "error", decodeErr)
				} else {
					s.lastQC = qc
				}
			}
			s.logger.InfoContext(ctx, "loaded committed state",
				"height", lastHeight,
			)
		}
	}

	// Start persistence worker
	if s.config.EnablePersistence {
		go s.persistenceWorker(ctx)
	}

	s.logger.InfoContext(ctx, "storage started",
		"replay_window", s.config.ReplayWindowSize,
		"last_committed", s.lastCommitted,
	)

	return nil
}

// Stop halts storage operations
func (s *Storage) Stop() error {
	// Final persistence flush
	if s.config.EnablePersistence {
		// Would signal persistence worker to stop
	}
	return nil
}

// StoreProposal stores a proposal in the replay window
func (s *Storage) StoreProposal(p *messages.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if within replay window
	if !s.isWithinWindow(p.Height) {
		return fmt.Errorf("proposal height %d outside replay window [%d, %d]",
			p.Height, s.lastCommitted+1, s.lastCommitted+uint64(s.config.ReplayWindowSize))
	}

	// Check capacity
	if len(s.proposals) >= s.config.MaxProposals {
		return fmt.Errorf("proposal storage full: %d", len(s.proposals))
	}

	// Store in memory
	s.proposals[p.BlockHash] = p

	// Persist if enabled
	if s.config.EnablePersistence && s.backend != nil {
		data, err := s.encodeProposal(p)
		if err != nil {
			return fmt.Errorf("encode proposal: %w", err)
		}
		if err := s.backend.SaveProposal(p.BlockHash[:], data); err != nil {
			return fmt.Errorf("persist proposal: %w", err)
		}
		if s.audit != nil {
			_ = s.audit.Info("proposal_persisted", map[string]interface{}{
				"height": p.Height,
				"view":   p.View,
				"hash":   fmt.Sprintf("%x", p.BlockHash[:8]),
			})
		}
	}

	return nil
}

// GetProposal retrieves a proposal by block hash
func (s *Storage) GetProposal(hash BlockHash) *messages.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.proposals[hash]
}

// StoreVote stores a vote
func (s *Storage) StoreVote(v *messages.Vote) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check capacity
	if len(s.votes[v.View]) >= s.config.MaxVotesPerView {
		return fmt.Errorf("vote storage full for view %d", v.View)
	}

	// Store in memory
	s.votes[v.View] = append(s.votes[v.View], v)

	// Persist if backend supports vote storage
	if s.config.EnablePersistence && s.backend != nil {
		if vb, ok := s.backend.(voteBackend); ok {
			data, err := s.encodeVote(v)
			if err != nil {
				return fmt.Errorf("encode vote: %w", err)
			}
			hash := v.Hash()
			if err := vb.SaveVote(v.View, hash[:], data); err != nil {
				return fmt.Errorf("persist vote: %w", err)
			}
			if s.audit != nil {
				_ = s.audit.Info("vote_persisted", map[string]interface{}{
					"view":   v.View,
					"height": v.Height,
					"hash":   fmt.Sprintf("%x", hash[:8]),
				})
			}
		}
	}

	return nil
}

// GetVotesByView retrieves all votes for a view
func (s *Storage) GetVotesByView(view uint64) []*messages.Vote {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.votes[view]
}

// StoreQC stores a quorum certificate
func (s *Storage) StoreQC(qc QC) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check capacity
	if len(s.qcs) >= s.config.MaxQCs {
		return fmt.Errorf("QC storage full: %d", len(s.qcs))
	}

	// Store in memory
	qcHash := qc.Hash()
	s.qcs[qcHash] = qc

	// Update last QC if newer
	if s.lastQC == nil || qc.GetView() > s.lastQC.GetView() {
		s.lastQC = qc
	}

	// Persist if enabled
	if s.config.EnablePersistence && s.backend != nil {
		data, err := s.encodeQC(qc)
		if err != nil {
			return fmt.Errorf("encode qc: %w", err)
		}
		if err := s.backend.SaveQC(qcHash[:], data); err != nil {
			return fmt.Errorf("persist qc: %w", err)
		}
		if s.audit != nil {
			_ = s.audit.Info("qc_persisted", map[string]interface{}{
				"view":   qc.GetView(),
				"height": qc.GetHeight(),
				"hash":   fmt.Sprintf("%x", qcHash[:8]),
			})
		}
	}

	return nil
}

// GetQC retrieves a QC by hash
func (s *Storage) GetQC(hash BlockHash) QC {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.qcs[hash]
}

// StoreEvidence stores Byzantine evidence
func (s *Storage) StoreEvidence(e *messages.Evidence) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check capacity
	if len(s.evidence) >= s.config.MaxEvidence {
		return fmt.Errorf("evidence storage full: %d", len(s.evidence))
	}

	// Store in memory
	evidenceHash := e.Hash()
	s.evidence[evidenceHash] = e

	// Persist if enabled
	if s.config.EnablePersistence && s.backend != nil {
		// Would serialize and save
	}

	s.audit.Security("evidence_stored", map[string]interface{}{
		"type":     e.Type,
		"view":     e.View,
		"offender": fmt.Sprintf("%x", e.OffenderID[:]),
		"reporter": fmt.Sprintf("%x", e.ReporterID[:]),
	})

	return nil
}

// GetAllEvidence retrieves all stored evidence
func (s *Storage) GetAllEvidence() []*messages.Evidence {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*messages.Evidence, 0, len(s.evidence))
	for _, e := range s.evidence {
		result = append(result, e)
	}

	return result
}

// CommitBlock marks a block as committed
func (s *Storage) CommitBlock(hash BlockHash, height uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update last committed
	if height > s.lastCommitted {
		s.lastCommitted = height
	}

	// Store committed block
	s.committedBlocks[height] = hash

	// Persist if enabled
	if s.config.EnablePersistence && s.backend != nil {
		if err := s.backend.SaveCommittedBlock(height, hash); err != nil {
			return fmt.Errorf("failed to persist committed block: %w", err)
		}
	}

	s.audit.Info("block_committed_storage", map[string]interface{}{
		"height": height,
		"hash":   fmt.Sprintf("%x", hash[:]),
	})

	// Auto-prune if enabled
	if s.config.AutoPrune {
		s.pruneOldDataUnsafe(height)
	}

	return nil
}

// GetLastCommittedHeight returns the last committed height
func (s *Storage) GetLastCommittedHeight() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lastCommitted
}

// GetLastCommittedQC returns the last committed QC
func (s *Storage) GetLastCommittedQC() QC {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lastQC
}

// IsCommitted checks if a block at height is committed
func (s *Storage) IsCommitted(height uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.committedBlocks[height]
	return exists
}

// GetCommittedBlockHash returns the hash of a committed block
func (s *Storage) GetCommittedBlockHash(height uint64) (BlockHash, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hash, exists := s.committedBlocks[height]
	return hash, exists
}

func (s *Storage) encodeProposal(p *messages.Proposal) ([]byte, error) {
	if s.encMode == nil {
		return nil, fmt.Errorf("cbor encoder not initialized")
	}
	data, err := s.encMode.Marshal(p)
	if err != nil {
		return nil, err
	}
	if s.limits != nil && len(data) > s.limits.MaxProposalSize {
		return nil, fmt.Errorf("proposal size %d exceeds limit %d", len(data), s.limits.MaxProposalSize)
	}
	return data, nil
}

func (s *Storage) encodeVote(v *messages.Vote) ([]byte, error) {
	if s.encMode == nil {
		return nil, fmt.Errorf("cbor encoder not initialized")
	}
	data, err := s.encMode.Marshal(v)
	if err != nil {
		return nil, err
	}
	if s.limits != nil && len(data) > s.limits.MaxVoteSize {
		return nil, fmt.Errorf("vote size %d exceeds limit %d", len(data), s.limits.MaxVoteSize)
	}
	return data, nil
}

func (s *Storage) encodeQC(qc QC) ([]byte, error) {
	if s.encMode == nil {
		return nil, fmt.Errorf("cbor encoder not initialized")
	}
	messagesQC, ok := qc.(*messages.QC)
	if !ok {
		return nil, fmt.Errorf("unsupported qc type %T", qc)
	}
	data, err := s.encMode.Marshal(messagesQC)
	if err != nil {
		return nil, err
	}
	if s.limits != nil && len(data) > s.limits.MaxQCSize {
		return nil, fmt.Errorf("qc size %d exceeds limit %d", len(data), s.limits.MaxQCSize)
	}
	return data, nil
}

func (s *Storage) encodeEvidence(e *messages.Evidence) ([]byte, error) {
	if s.encMode == nil {
		return nil, fmt.Errorf("cbor encoder not initialized")
	}
	data, err := s.encMode.Marshal(e)
	if err != nil {
		return nil, err
	}
	if s.limits != nil && len(data) > s.limits.MaxEvidenceSize {
		return nil, fmt.Errorf("evidence size %d exceeds limit %d", len(data), s.limits.MaxEvidenceSize)
	}
	return data, nil
}

func (s *Storage) decodeQC(data []byte) (QC, error) {
	if s.decMode == nil {
		return nil, fmt.Errorf("cbor decoder not initialized")
	}
	var qc messages.QC
	if err := s.decMode.Unmarshal(data, &qc); err != nil {
		return nil, err
	}
	return &qc, nil
}

// isWithinWindow checks if a height is within the replay window
func (s *Storage) isWithinWindow(height uint64) bool {
	windowStart := s.lastCommitted + 1
	windowEnd := s.lastCommitted + uint64(s.config.ReplayWindowSize)
	return height >= windowStart && height <= windowEnd
}

// pruneOldDataUnsafe removes data outside replay window (must hold lock)
func (s *Storage) pruneOldDataUnsafe(currentHeight uint64) {
	if currentHeight <= uint64(s.config.MinRetainBlocks) {
		return
	}

	pruneHeight := currentHeight - uint64(s.config.MinRetainBlocks)

	// Prune proposals
	for hash, p := range s.proposals {
		if p.Height < pruneHeight {
			delete(s.proposals, hash)
		}
	}

	// Prune votes (by view, approximating with height)
	for view := range s.votes {
		if view < pruneHeight {
			delete(s.votes, view)
		}
	}

	// Prune QCs
	for hash, qc := range s.qcs {
		if qc.GetHeight() < pruneHeight {
			delete(s.qcs, hash)
		}
	}

	// Prune old committed blocks (keep MinRetainBlocks)
	for height := range s.committedBlocks {
		if height < pruneHeight {
			delete(s.committedBlocks, height)
		}
	}

	// Persist pruning
	if s.config.EnablePersistence && s.backend != nil {
		if err := s.backend.DeleteBefore(pruneHeight); err != nil {
			s.logger.WarnContext(context.Background(), "failed to prune backend", "error", err)
		}
	}
}

// Prune manually triggers pruning (public API)
func (s *Storage) Prune(currentHeight uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneOldDataUnsafe(currentHeight)
}

// persistenceWorker periodically persists state
func (s *Storage) persistenceWorker(ctx context.Context) {
	ticker := time.NewTicker(s.config.PersistInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.persistState(ctx)
		}
	}
}

// persistState persists current state to backend
func (s *Storage) persistState(ctx context.Context) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.backend == nil {
		return
	}

	// Persist last committed state
	if s.lastQC != nil {
		if err := s.backend.SaveCommittedBlock(s.lastCommitted, s.lastQC.GetBlockHash()); err != nil {
			s.logger.ErrorContext(ctx, "failed to persist state", "error", err)
		}
	}
}

// GetStats returns storage statistics
func (s *Storage) GetStats() StorageStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalVotes := 0
	for _, votes := range s.votes {
		totalVotes += len(votes)
	}

	return StorageStats{
		ProposalCount:       len(s.proposals),
		VoteCount:           totalVotes,
		QCCount:             len(s.qcs),
		EvidenceCount:       len(s.evidence),
		CommittedBlockCount: len(s.committedBlocks),
		LastCommittedHeight: s.lastCommitted,
		ReplayWindowSize:    s.config.ReplayWindowSize,
	}
}

// StorageStats contains storage statistics
type StorageStats struct {
	ProposalCount       int
	VoteCount           int
	QCCount             int
	EvidenceCount       int
	CommittedBlockCount int
	LastCommittedHeight uint64
	ReplayWindowSize    int
}
