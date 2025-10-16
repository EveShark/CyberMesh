package wiring

import (
	"fmt"

	"backend/pkg/block"
	"backend/pkg/consensus/api"
	"backend/pkg/utils"
)

// validateBlock performs basic block validation before committing
func (s *Service) validateBlock(b api.Block) error {
	s.mu.Lock()
	expectedHeight := s.lastCommittedHeight + 1
	expectedParent := s.lastParent
	initialCatchup := false
	shouldMarkSynced := false
	commitSynced := s.commitStateSynced

	if b.GetHeight() < expectedHeight {
		s.mu.Unlock()
		return fmt.Errorf("invalid block height: got %d, expected %d",
			b.GetHeight(), expectedHeight)
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
			return fmt.Errorf("invalid parent hash: got %x, expected %x",
				actualParent[:8], expectedParent[:8])
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
