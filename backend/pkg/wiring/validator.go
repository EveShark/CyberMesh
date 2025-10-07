package wiring

import (
	"fmt"

	"backend/pkg/block"
	"backend/pkg/consensus/api"
)

// validateBlock performs basic block validation before committing
func (s *Service) validateBlock(b api.Block, expectedHeight uint64) error {
	// Check height sequence
	if b.GetHeight() != expectedHeight {
		return fmt.Errorf("invalid block height: got %d, expected %d",
			b.GetHeight(), expectedHeight)
	}

	// Validate parent hash
	s.mu.Lock()
	expectedParent := s.lastParent
	s.mu.Unlock()

	actualParent := b.GetParentHash()
	if actualParent != expectedParent {
		return fmt.Errorf("invalid parent hash: got %x, expected %x",
			actualParent[:8], expectedParent[:8])
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
