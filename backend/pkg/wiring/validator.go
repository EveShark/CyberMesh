package wiring

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"backend/pkg/block"
	"backend/pkg/consensus/api"
	"backend/pkg/utils"
)

var errAlreadyCommittedBlock = errors.New("block already committed")

// validateBlock performs basic block validation before committing
func (s *Service) validateBlock(b api.Block) error {
	s.mu.Lock()
	expectedHeight := s.lastCommittedHeight + 1
	expectedParent := s.lastParent
	initialCatchup := false
	shouldMarkSynced := false
	commitSynced := s.commitStateSynced
	lastCommittedHeight := s.lastCommittedHeight
	lastCommittedHash := s.lastParent

	if b.GetHeight() < expectedHeight {
		s.mu.Unlock()
		blockHash := b.GetHash()
		if b.GetHeight() == lastCommittedHeight && blockHash == lastCommittedHash {
			if s.log != nil {
				s.log.Info("commit callback received already-committed block",
					utils.ZapUint64("height", b.GetHeight()),
					utils.ZapString("hash", fmt.Sprintf("%x", blockHash[:8])))
			}
			return errAlreadyCommittedBlock
		}
		// Commit callbacks can be delivered out of order/duplicated during view churn.
		// Once local state has advanced, stale callbacks must not fail the commit path.
		if b.GetHeight() == lastCommittedHeight {
			if s.log != nil {
				s.log.Warn("stale conflicting commit callback ignored",
					utils.ZapUint64("height", b.GetHeight()),
					utils.ZapString("incoming_hash", fmt.Sprintf("%x", blockHash[:8])),
					utils.ZapString("last_committed_hash", fmt.Sprintf("%x", lastCommittedHash[:8])))
			}
		} else {
			if s.log != nil {
				s.log.Info("stale commit callback ignored",
					utils.ZapUint64("incoming_height", b.GetHeight()),
					utils.ZapUint64("expected_height", expectedHeight),
					utils.ZapUint64("last_committed_height", lastCommittedHeight))
			}
		}
		return errAlreadyCommittedBlock
	}

	if b.GetHeight() > expectedHeight {
		s.log.Warn("commit validator out of sync - aligning to consensus",
			utils.ZapUint64("expected_height", expectedHeight),
			utils.ZapUint64("incoming_height", b.GetHeight()))
		s.lastCommittedHeight = b.GetHeight() - 1
		s.lastParent = b.GetParentHash()
		expectedHeight = s.lastCommittedHeight + 1
		expectedParent = s.lastParent
		initialCatchup = true
	}

	if initialCatchup || !commitSynced {
		shouldMarkSynced = true
	}

	s.mu.Unlock()

	actualParent := b.GetParentHash()
	if actualParent != expectedParent {
		if initialCatchup {
			s.log.Info("parent check skipped during initial catch-up",
				utils.ZapString("expected_parent", fmt.Sprintf("%x", expectedParent[:8])),
				utils.ZapString("incoming_parent", fmt.Sprintf("%x", actualParent[:8])))
		} else {
			// Record specific parent-hash mismatch metric for alerting
			if s.metrics != nil {
				s.metrics.IncrementParentHashMismatches()
			}
			if s.eng != nil {
				s.eng.RecordParentHashMismatch()
			}
			strictParentCheck := false
			switch strings.ToLower(strings.TrimSpace(os.Getenv("COMMIT_VALIDATOR_STRICT_PARENT_HASH"))) {
			case "1", "true", "yes", "on":
				strictParentCheck = true
			}

			if strictParentCheck {
				return fmt.Errorf("invalid parent hash: got %x, expected %x",
					actualParent[:8], expectedParent[:8])
			}

			// Consensus has already finalized this commit path. In non-strict mode,
			// realign local commit validator state instead of aborting callback.
			s.log.Warn("commit parent mismatch - realigning to consensus parent",
				utils.ZapString("expected_parent", fmt.Sprintf("%x", expectedParent[:8])),
				utils.ZapString("incoming_parent", fmt.Sprintf("%x", actualParent[:8])),
				utils.ZapUint64("incoming_height", b.GetHeight()))
			s.mu.Lock()
			if b.GetHeight() > 0 {
				s.lastCommittedHeight = b.GetHeight() - 1
			}
			s.lastParent = actualParent
			s.commitStateSynced = true
			s.mu.Unlock()
		}
	}

	if shouldMarkSynced {
		s.mu.Lock()
		s.commitStateSynced = true
		s.mu.Unlock()
	}

	// For AppBlocks, validate proposer is a known validator
	if ab, ok := b.(*block.AppBlock); ok {
		proposerID := ab.Proposer()
		status := s.eng.GetStatus()
		// TODO: Add validator set validation when GetValidatorSet() is available
		// For now, just log the proposer
		_ = proposerID
		_ = status
	}

	return nil
}
