package pbft

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	lastCommitted         uint64
	lastQC                QC
	committedBlocks       map[uint64]BlockHash // height -> hash
	allowRestartBootstrap bool

	// Persistent storage backend
	backend StorageBackend

	encMode cbor.EncMode
	decMode cbor.DecMode
	limits  *messages.EncoderConfig

	backgroundCtx context.Context

	// Replay-window recovery telemetry (protected by mu).
	replayWindowRejectCount          uint64
	replayWindowRecoverSuccessCount  uint64
	replayWindowRecoverFailureCount  uint64
	lastReplayWindowRecoverAttemptAt time.Time

	mu sync.RWMutex
}

const (
	replayRecoverThrottleWindow = 500 * time.Millisecond
	replayRecoverTimeoutFast    = 1500 * time.Millisecond
	replayRecoverTimeoutForce   = 5 * time.Second
)

type voteBackend interface {
	SaveVote(ctx context.Context, view uint64, height uint64, voter []byte, blockHash []byte, voteHash []byte, data []byte) error
}

type evidenceBackend interface {
	SaveEvidence(ctx context.Context, hash []byte, height uint64, data []byte) error
}

type durableTipBackend interface {
	GetLatestHeight(ctx context.Context) (uint64, error)
	GetCommittedBlockHash(ctx context.Context, height uint64) ([]byte, bool, error)
}

// StorageConfig contains storage parameters
type StorageConfig struct {
	ReplayWindowSize  int
	MaxProposals      int
	MaxVotesPerView   int
	MaxQCs            int
	MaxEvidence       int
	MaxProposalSize   int
	MaxVoteSize       int
	MaxQCSize         int
	MaxEvidenceSize   int
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
		MaxProposalSize:   1 << 20,
		MaxVoteSize:       10 << 10,
		MaxQCSize:         100 << 10,
		MaxEvidenceSize:   200 << 10,
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
	if config.MaxProposalSize > 0 {
		limits.MaxProposalSize = config.MaxProposalSize
	}
	if config.MaxVoteSize > 0 {
		limits.MaxVoteSize = config.MaxVoteSize
	}
	if config.MaxQCSize > 0 {
		limits.MaxQCSize = config.MaxQCSize
	}
	if config.MaxEvidenceSize > 0 {
		limits.MaxEvidenceSize = config.MaxEvidenceSize
	}

	backgroundCtx := context.Background()

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
		backgroundCtx:   backgroundCtx,
	}
}

// SetBackend attaches or replaces the persistent storage backend.
// Safe to call before Start(); after Start(), it will take effect for subsequent
// persistence operations. Restoration occurs in Start() when backend is present.
func (s *Storage) SetBackend(backend StorageBackend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backend = backend
}

// Start initializes storage and loads persisted state
func (s *Storage) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load persisted state from backend
	if s.config.EnablePersistence && s.backend != nil {
		restoreCommittedCtx, cancelCommitted := context.WithTimeout(ctx, 5*time.Second)
		if err := s.restoreCommittedState(restoreCommittedCtx); err != nil {
			s.logger.WarnContext(ctx, "failed to restore committed state", "error", err)
		}
		cancelCommitted()

		restoreReplayCtx, cancelReplay := context.WithTimeout(ctx, 8*time.Second)
		if err := s.restoreReplayWindow(restoreReplayCtx); err != nil {
			s.logger.WarnContext(ctx, "failed to restore replay window", "error", err)
		}
		cancelReplay()
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

func (s *Storage) restoreCommittedState(ctx context.Context) error {
	lastHeight, blockHashBytes, qcData, err := s.backend.LoadLastCommitted(ctx)
	if err != nil {
		return fmt.Errorf("load last committed: %w", err)
	}

	s.allowRestartBootstrap = false

	if tipBackend, ok := s.backend.(durableTipBackend); ok {
		durableHeight, tipErr := tipBackend.GetLatestHeight(ctx)
		if tipErr != nil {
			s.logger.WarnContext(ctx, "failed to query durable block tip during committed-state restore", "error", tipErr)
		} else if durableHeight > lastHeight {
			durableHash, found, hashErr := tipBackend.GetCommittedBlockHash(ctx, durableHeight)
			if hashErr != nil {
				s.logger.WarnContext(ctx, "failed to load durable block tip hash during committed-state restore",
					"height", durableHeight,
					"error", hashErr)
			} else if found && len(durableHash) == 32 {
				s.logger.WarnContext(ctx, "consensus metadata behind durable block tip; reconciling to durable chain",
					"metadata_height", lastHeight,
					"durable_height", durableHeight)
				lastHeight = durableHeight
				blockHashBytes = durableHash
				// The persisted QC payload belongs to the older metadata height, so discard it.
				qcData = nil
				s.allowRestartBootstrap = true
			}
		}
	}

	s.lastCommitted = lastHeight

	if len(blockHashBytes) == 32 {
		var bh BlockHash
		copy(bh[:], blockHashBytes)
		s.committedBlocks[lastHeight] = bh
	}

	if len(qcData) > 0 {
		qc, decodeErr := s.decodeQC(qcData)
		if decodeErr != nil {
			s.logger.WarnContext(ctx, "failed to decode last committed qc", "error", decodeErr)
		} else {
			s.lastQC = qc
			s.allowRestartBootstrap = false
		}
	}

	s.logger.InfoContext(ctx, "committed state restored",
		"height", lastHeight,
	)
	return nil
}

func (s *Storage) restoreReplayWindow(ctx context.Context) error {
	minHeight := uint64(0)
	if s.lastCommitted >= uint64(s.config.ReplayWindowSize) {
		minHeight = s.lastCommitted - uint64(s.config.ReplayWindowSize) + 1
	}

	if err := s.restoreProposals(ctx, minHeight); err != nil {
		return err
	}
	if err := s.restoreQCs(ctx, minHeight); err != nil {
		return err
	}
	if err := s.restoreVotes(ctx, minHeight); err != nil {
		return err
	}
	if err := s.restoreEvidence(ctx, minHeight); err != nil {
		return err
	}

	// If consensus_metadata did not include a QC payload, try to derive the lastQC from the
	// persisted replay window using the committed block hash.
	if s.lastQC == nil && s.lastCommitted > 0 {
		if committedHash, ok := s.committedBlocks[s.lastCommitted]; ok {
			if qc, exists := s.qcs[committedHash]; exists && qc != nil {
				s.lastQC = qc
				s.allowRestartBootstrap = false
			}
		}
	}

	s.logger.InfoContext(ctx, "replay window restored",
		"min_height", minHeight,
		"proposal_count", len(s.proposals),
		"vote_views", len(s.votes),
		"qc_count", len(s.qcs),
		"evidence_count", len(s.evidence),
	)
	return nil
}

func (s *Storage) restoreProposals(ctx context.Context, minHeight uint64) error {
	const pageSize = 256
	for offset := minHeight; ; offset += pageSize {
		records, err := s.backend.ListProposals(ctx, offset, pageSize)
		if err != nil {
			return fmt.Errorf("list proposals: %w", err)
		}
		if len(records) == 0 {
			break
		}
		for _, rec := range records {
			if len(rec.Hash) != 32 {
				s.logger.WarnContext(ctx, "skipping proposal with invalid hash length", "length", len(rec.Hash))
				continue
			}
			proposal, err := s.decodeProposal(rec.Data)
			if err != nil {
				s.logger.WarnContext(ctx, "failed to decode persisted proposal", "error", err)
				continue
			}
			var hash BlockHash
			copy(hash[:], rec.Hash)
			s.proposals[hash] = proposal
		}
		if len(records) < pageSize {
			break
		}
	}
	return nil
}

func (s *Storage) restoreQCs(ctx context.Context, minHeight uint64) error {
	const pageSize = 256
	for offset := minHeight; ; offset += pageSize {
		records, err := s.backend.ListQCs(ctx, offset, pageSize)
		if err != nil {
			return fmt.Errorf("list qcs: %w", err)
		}
		if len(records) == 0 {
			break
		}
		for _, rec := range records {
			if len(rec.Hash) != 32 {
				s.logger.WarnContext(ctx, "skipping qc with invalid hash length", "length", len(rec.Hash))
				continue
			}
			qc, err := s.decodeQC(rec.Data)
			if err != nil {
				s.logger.WarnContext(ctx, "failed to decode persisted qc", "error", err)
				continue
			}
			var hash BlockHash
			copy(hash[:], rec.Hash)
			s.qcs[hash] = qc
		}
		if len(records) < pageSize {
			break
		}
	}
	return nil
}

func (s *Storage) restoreVotes(ctx context.Context, minHeight uint64) error {
	const pageSize = 512
	nextMinHeight := minHeight
	for {
		records, err := s.backend.ListVotes(ctx, nextMinHeight, pageSize)
		if err != nil {
			return fmt.Errorf("list votes: %w", err)
		}
		if len(records) == 0 {
			break
		}
		maxHeightSeen := nextMinHeight
		for _, rec := range records {
			vote, err := s.decodeVote(rec.Data)
			if err != nil {
				s.logger.WarnContext(ctx, "failed to decode persisted vote", "error", err)
				continue
			}
			s.votes[rec.View] = append(s.votes[rec.View], vote)
			if rec.Height > maxHeightSeen {
				maxHeightSeen = rec.Height
			}
		}
		// Advance by durable height cursor rather than page size to avoid skipping sparse heights.
		// NOTE: With a min-height API we cannot paginate all rows at the exact same height beyond pageSize.
		// Stabilize ordering and emit a warning when this edge case is likely.
		if len(records) == pageSize && maxHeightSeen == nextMinHeight {
			s.logger.WarnContext(ctx, "vote restore page saturated at single height; additional same-height votes may require larger page",
				"height", nextMinHeight,
				"page_size", pageSize)
		}
		if maxHeightSeen == ^uint64(0) {
			break
		}
		nextMinHeight = maxHeightSeen + 1
		if len(records) < pageSize {
			break
		}
	}
	for view := range s.votes {
		s.sortVotesByTimestamp(view)
	}
	return nil
}

func (s *Storage) sortVotesByTimestamp(view uint64) {
	votes := s.votes[view]
	sort.SliceStable(votes, func(i, j int) bool {
		return votes[i].Timestamp.Before(votes[j].Timestamp)
	})
	s.votes[view] = votes
}

func (s *Storage) restoreEvidence(ctx context.Context, minHeight uint64) error {
	const pageSize = 256
	for offset := minHeight; ; offset += pageSize {
		records, err := s.backend.ListEvidence(ctx, offset, pageSize)
		if err != nil {
			return fmt.Errorf("list evidence: %w", err)
		}
		if len(records) == 0 {
			break
		}
		for _, rec := range records {
			if len(rec.Hash) != 32 {
				s.logger.WarnContext(ctx, "skipping evidence with invalid hash length", "length", len(rec.Hash))
				continue
			}
			ev, err := s.decodeEvidence(rec.Data)
			if err != nil {
				s.logger.WarnContext(ctx, "failed to decode persisted evidence", "error", err)
				continue
			}
			var hash BlockHash
			copy(hash[:], rec.Hash)
			s.evidence[hash] = ev
		}
		if len(records) < pageSize {
			break
		}
	}
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

	// Check if within replay window
	if !s.isWithinWindow(p.Height) {
		s.replayWindowRejectCount++
		windowSize := uint64(s.config.ReplayWindowSize)
		if windowSize == 0 {
			windowSize = 1
		}
		// If proposal height is outside the configured replay window, force a durable
		// tip reconcile immediately so we do not spin on stale in-memory committed height.
		forceRecover := p.Height > s.lastCommitted+windowSize
		recovered, recoverErr := s.tryReconcileReplayWindowLocked(p.Height, forceRecover)
		if recoverErr != nil {
			s.replayWindowRecoverFailureCount++
			if s.logger != nil {
				s.logger.WarnContext(s.backgroundCtx, "replay window reconcile failed",
					"proposal_height", p.Height,
					"last_committed", s.lastCommitted,
					"force", forceRecover,
					"error", recoverErr,
				)
			}
		}
		if recovered && s.isWithinWindow(p.Height) {
			s.replayWindowRecoverSuccessCount++
		} else if recovered {
			s.replayWindowRecoverFailureCount++
		}
		if !recovered && recoverErr == nil && s.logger != nil {
			s.logger.WarnContext(s.backgroundCtx, "replay window reconcile did not advance committed height",
				"proposal_height", p.Height,
				"last_committed", s.lastCommitted,
				"window_start", s.lastCommitted+1,
				"window_end", s.lastCommitted+windowSize,
				"force", forceRecover,
			)
		}
		if !s.isWithinWindow(p.Height) {
			s.mu.Unlock()
			return fmt.Errorf("proposal height %d outside replay window [%d, %d]",
				p.Height, s.lastCommitted+1, s.lastCommitted+uint64(s.config.ReplayWindowSize))
		}
	}

	// Check capacity
	if len(s.proposals) >= s.config.MaxProposals {
		s.mu.Unlock()
		return fmt.Errorf("proposal storage full: %d", len(s.proposals))
	}

	// Store in memory
	s.proposals[p.BlockHash] = p
	backend := s.backend
	enablePersistence := s.config.EnablePersistence
	s.mu.Unlock()

	// Persist if enabled
	if enablePersistence && backend != nil {
		data, err := s.encodeProposal(p)
		if err != nil {
			s.mu.Lock()
			delete(s.proposals, p.BlockHash)
			s.mu.Unlock()
			return fmt.Errorf("encode proposal: %w", err)
		}
		if err := backend.SaveProposal(s.backgroundCtx, p.BlockHash[:], p.Height, p.View, p.ProposerID[:], data); err != nil {
			s.mu.Lock()
			delete(s.proposals, p.BlockHash)
			s.mu.Unlock()
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

func (s *Storage) tryReconcileReplayWindowLocked(proposalHeight uint64, force bool) (bool, error) {
	if s.backend == nil {
		return false, nil
	}
	tipBackend, ok := s.backend.(durableTipBackend)
	if !ok {
		return false, nil
	}
	now := time.Now()
	// Guard repeated expensive backend lookups during hot reject loops.
	if !force && !s.lastReplayWindowRecoverAttemptAt.IsZero() && now.Sub(s.lastReplayWindowRecoverAttemptAt) < replayRecoverThrottleWindow {
		return false, nil
	}
	s.lastReplayWindowRecoverAttemptAt = now

	recoverCtx := s.backgroundCtx
	if recoverCtx == nil {
		recoverCtx = context.Background()
	}
	var cancel context.CancelFunc
	timeout := replayRecoverTimeoutFast
	if force {
		timeout = replayRecoverTimeoutForce
	}
	recoverCtx, cancel = context.WithTimeout(recoverCtx, timeout)
	defer cancel()

	durableHeight, err := tipBackend.GetLatestHeight(recoverCtx)
	if err != nil {
		return false, err
	}
	if durableHeight <= s.lastCommitted {
		return false, nil
	}
	durableHash, found, hashErr := tipBackend.GetCommittedBlockHash(recoverCtx, durableHeight)
	if hashErr != nil {
		return false, hashErr
	}
	if !found || len(durableHash) != 32 {
		return false, nil
	}

	prevCommitted := s.lastCommitted
	s.lastCommitted = durableHeight
	var bh BlockHash
	copy(bh[:], durableHash)
	s.committedBlocks[durableHeight] = bh

	if s.logger != nil {
		s.logger.WarnContext(s.backgroundCtx, "replay window reconciled from durable tip",
			"proposal_height", proposalHeight,
			"prev_last_committed", prevCommitted,
			"new_last_committed", durableHeight,
		)
	}
	return true, nil
}

// ReconcileReplayWindow force-refreshes in-memory replay-window committed height from
// durable storage tip. It returns true when committed height advanced.
func (s *Storage) ReconcileReplayWindow(proposalHeight uint64, force bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tryReconcileReplayWindowLocked(proposalHeight, force)
}

// GetProposal retrieves a proposal by block hash
func (s *Storage) GetProposal(hash BlockHash) *messages.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.proposals[hash]
}

// GetProposalOrRecover returns a proposal from memory, or attempts one bounded
// load from durable storage when missing. On successful recovery it rehydrates
// the replay cache for subsequent accesses.
func (s *Storage) GetProposalOrRecover(ctx context.Context, hash BlockHash) (*messages.Proposal, error) {
	if p := s.GetProposal(hash); p != nil {
		return p, nil
	}

	s.mu.RLock()
	backend := s.backend
	s.mu.RUnlock()
	if backend == nil {
		return nil, nil
	}

	recoverCtx := ctx
	if recoverCtx == nil {
		recoverCtx = context.Background()
	}
	if _, hasDeadline := recoverCtx.Deadline(); !hasDeadline {
		timeout := 2 * time.Second
		var cancel context.CancelFunc
		recoverCtx, cancel = context.WithTimeout(recoverCtx, timeout)
		defer cancel()
	}

	raw, err := backend.LoadProposal(recoverCtx, hash[:])
	if err != nil {
		return nil, fmt.Errorf("load proposal from backend: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}

	proposal, err := s.decodeProposal(raw)
	if err != nil {
		return nil, fmt.Errorf("decode recovered proposal: %w", err)
	}
	if proposal == nil {
		return nil, nil
	}
	if proposal.BlockHash != hash {
		return nil, fmt.Errorf("recovered proposal hash mismatch")
	}

	s.mu.Lock()
	// Another goroutine may have restored the same proposal while we were loading.
	if existing := s.proposals[hash]; existing != nil {
		s.mu.Unlock()
		return existing, nil
	}
	s.proposals[hash] = proposal
	s.mu.Unlock()
	return proposal, nil
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
			if err := vb.SaveVote(s.backgroundCtx, v.View, v.Height, v.VoterID[:], v.BlockHash[:], hash[:], data); err != nil {
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

// GetVotesForQCRepair returns votes that match the target QC tuple.
// It first checks in-memory replay-window votes; if below neededCount,
// it backfills from durable storage using height-based scans.
func (s *Storage) GetVotesForQCRepair(
	ctx context.Context,
	view uint64,
	height uint64,
	blockHash BlockHash,
	neededCount int,
) []*messages.Vote {
	s.mu.RLock()
	cached := append([]*messages.Vote(nil), s.votes[view]...)
	backend := s.backend
	decMode := s.decMode
	logger := s.logger
	s.mu.RUnlock()

	matches := make([]*messages.Vote, 0, len(cached))
	for _, vote := range cached {
		if vote == nil {
			continue
		}
		if vote.View == view && vote.Height == height && vote.BlockHash == blockHash {
			matches = append(matches, vote)
		}
	}
	if neededCount > 0 && len(matches) >= neededCount {
		return matches
	}
	if backend == nil || decMode == nil {
		return matches
	}

	limits := []int{2048, 8192, 32768, 131072}
	seen := make(map[[32]byte]struct{}, len(matches))
	for _, vote := range matches {
		if vote == nil {
			continue
		}
		vh := vote.Hash()
		seen[vh] = struct{}{}
	}

	for _, limit := range limits {
		records, err := backend.ListVotes(ctx, height, limit)
		if err != nil {
			if logger != nil {
				logger.WarnContext(ctx, "qc repair vote backfill failed",
					"view", view,
					"height", height,
					"limit", limit,
					"error", err)
			}
			break
		}
		if len(records) == 0 {
			break
		}

		added := 0
		for _, rec := range records {
			if rec.View != view || rec.Height != height {
				continue
			}
			if len(rec.BlockHash) != 32 {
				continue
			}
			var recHash BlockHash
			copy(recHash[:], rec.BlockHash)
			if recHash != blockHash {
				continue
			}

			var vote messages.Vote
			if err := decMode.Unmarshal(rec.Data, &vote); err != nil {
				continue
			}
			vh := vote.Hash()
			if _, exists := seen[vh]; exists {
				continue
			}
			seen[vh] = struct{}{}
			voteCopy := vote
			matches = append(matches, &voteCopy)
			added++
		}

		if logger != nil && added > 0 {
			logger.InfoContext(ctx, "qc repair vote backfill matched persisted votes",
				"view", view,
				"height", height,
				"limit", limit,
				"added", added,
				"total_matches", len(matches))
		}
		if neededCount > 0 && len(matches) >= neededCount {
			break
		}
		if len(records) < limit {
			break
		}
	}

	return matches
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
		if err := s.backend.SaveQC(s.backgroundCtx, qcHash[:], qc.GetHeight(), qc.GetView(), data); err != nil {
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

// GetQCsSnapshot returns a copy of currently loaded QCs.
func (s *Storage) GetQCsSnapshot() []QC {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]QC, 0, len(s.qcs))
	for _, qc := range s.qcs {
		if qc != nil {
			out = append(out, qc)
		}
	}
	return out
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
		if eb, ok := s.backend.(evidenceBackend); ok {
			data, err := s.encodeEvidence(e)
			if err != nil {
				return fmt.Errorf("encode evidence: %w", err)
			}
			if err := eb.SaveEvidence(s.backgroundCtx, evidenceHash[:], e.Height, data); err != nil {
				return fmt.Errorf("persist evidence: %w", err)
			}
		}
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

	// Fail closed on conflicting commits at the same height.
	// Idempotent re-commit of the same hash is allowed.
	if existing, ok := s.committedBlocks[height]; ok {
		if existing != hash {
			err := fmt.Errorf("conflicting committed block at height %d", height)
			if s.audit != nil {
				_ = s.audit.Security("block_commit_conflict_detected", map[string]interface{}{
					"height":        height,
					"existing_hash": fmt.Sprintf("%x", existing[:]),
					"incoming_hash": fmt.Sprintf("%x", hash[:]),
				})
			}
			return err
		}
		// Idempotent commit for already persisted block.
		return nil
	}

	// Update last committed
	if height > s.lastCommitted {
		s.lastCommitted = height
	}

	// Store committed block
	s.committedBlocks[height] = hash

	if s.audit != nil {
		_ = s.audit.Info("block_committed_storage", map[string]interface{}{
			"height": height,
			"hash":   fmt.Sprintf("%x", hash[:]),
		})
	}

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

// AllowRestartBootstrap reports whether the next proposal can legally omit a QC
// because storage reconciled to a durable committed tip without a persisted QC.
func (s *Storage) AllowRestartBootstrap(parentHash BlockHash, proposalHeight uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.allowRestartBootstrap || s.lastQC != nil || s.lastCommitted == 0 {
		return false
	}
	if proposalHeight != s.lastCommitted+1 {
		return false
	}
	committedHash, ok := s.committedBlocks[s.lastCommitted]
	if !ok {
		return false
	}
	return committedHash == parentHash
}

func (s *Storage) encodeProposal(p *messages.Proposal) ([]byte, error) {
	data, err := messages.EncodeProposalWire(s.encMode, p)
	if err != nil {
		return nil, err
	}
	if s.limits != nil && len(data) > s.limits.MaxProposalSize {
		return nil, fmt.Errorf("proposal size %d exceeds limit %d", len(data), s.limits.MaxProposalSize)
	}
	return data, nil
}

func (s *Storage) decodeProposal(data []byte) (*messages.Proposal, error) {
	proposal, err := messages.DecodeProposalWire(s.decMode, data)
	if err == nil {
		return proposal, nil
	}
	errText := err.Error()
	if strings.Contains(errText, "block hash mismatch") || strings.Contains(errText, "decode block payload") {
		legacyProposal, legacyErr := messages.DecodeProposalWireMetadataOnly(s.decMode, data)
		if legacyErr == nil {
			return legacyProposal, nil
		}
	}
	return nil, err
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

func (s *Storage) decodeVote(data []byte) (*messages.Vote, error) {
	if s.decMode == nil {
		return nil, fmt.Errorf("cbor decoder not initialized")
	}
	var vote messages.Vote
	if err := s.decMode.Unmarshal(data, &vote); err != nil {
		return nil, err
	}
	return &vote, nil
}

func (s *Storage) encodeQC(qc QC) ([]byte, error) {
	if s.encMode == nil {
		return nil, fmt.Errorf("cbor encoder not initialized")
	}
	// Support both wire-format (*messages.QC) and internal (*pbft.QuorumCertificate)
	var messagesQC *messages.QC
	switch typed := any(qc).(type) {
	case *messages.QC:
		messagesQC = typed
	case *QuorumCertificate:
		// Reuse converter from pbft package
		converted, err := convertToMessageQC(typed)
		if err != nil {
			return nil, fmt.Errorf("convert qc: %w", err)
		}
		messagesQC = converted
	default:
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

func (s *Storage) decodeEvidence(data []byte) (*messages.Evidence, error) {
	if s.decMode == nil {
		return nil, fmt.Errorf("cbor decoder not initialized")
	}
	var evidence messages.Evidence
	if err := s.decMode.Unmarshal(data, &evidence); err != nil {
		return nil, err
	}
	return &evidence, nil
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
		if err := s.backend.DeleteBefore(context.Background(), pruneHeight); err != nil {
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

	// Intentionally do not persist consensus_metadata here.
	// consensus_metadata is updated after durable block persistence (e.g., DB transaction) to avoid
	// advancing metadata ahead of blocks/state and causing restart drift.
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
		ProposalCount:                   len(s.proposals),
		VoteCount:                       totalVotes,
		QCCount:                         len(s.qcs),
		EvidenceCount:                   len(s.evidence),
		CommittedBlockCount:             len(s.committedBlocks),
		LastCommittedHeight:             s.lastCommitted,
		ReplayWindowSize:                s.config.ReplayWindowSize,
		ReplayWindowRejectCount:         s.replayWindowRejectCount,
		ReplayWindowRecoverSuccessCount: s.replayWindowRecoverSuccessCount,
		ReplayWindowRecoverFailureCount: s.replayWindowRecoverFailureCount,
	}
}

// StorageStats contains storage statistics
type StorageStats struct {
	ProposalCount                   int
	VoteCount                       int
	QCCount                         int
	EvidenceCount                   int
	CommittedBlockCount             int
	LastCommittedHeight             uint64
	ReplayWindowSize                int
	ReplayWindowRejectCount         uint64
	ReplayWindowRecoverSuccessCount uint64
	ReplayWindowRecoverFailureCount uint64
}
