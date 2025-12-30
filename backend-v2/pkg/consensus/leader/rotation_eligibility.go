package leader

import (
	"context"
	"fmt"
)

// IsLeaderEligibleToPropose checks if the selected leader can propose blocks.
// This is separate from leader selection to maintain deterministic election.
func (r *Rotation) IsLeaderEligibleToPropose(ctx context.Context, validatorID ValidatorID, view uint64) (bool, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get validator info
	allValidators := r.validatorSet.GetValidators()
	var validator *ValidatorInfo
	for i := range allValidators {
		if allValidators[i].ID == validatorID {
			validator = &allValidators[i]
			break
		}
	}

	if validator == nil {
		return false, "validator not in set"
	}

	// Check if active
	if !validator.IsActive {
		return false, "validator inactive"
	}

	// Check if joined before this view
	if validator.JoinedView > view {
		return false, fmt.Sprintf("validator joined at view %d, current view %d", validator.JoinedView, view)
	}

	// Check quarantine status
	if r.config.EnableQuarantine && r.quarantine.IsQuarantined(validator.ID) {
		return false, "validator quarantined"
	}

	// Check reputation threshold
	if r.config.EnableReputation && validator.Reputation < r.config.MinReputation {
		return false, fmt.Sprintf("reputation %.2f below threshold %.2f", validator.Reputation, r.config.MinReputation)
	}

	// Check readiness (enforced after genesis view 0)
	if r.readiness != nil && view > 0 && !r.readiness.IsValidatorReady(validator.ID) {
		return false, "validator not marked ready"
	}

	return true, ""
}

// GetLeaderEligibilityStatus returns detailed eligibility status for monitoring
func (r *Rotation) GetLeaderEligibilityStatus(ctx context.Context, view uint64) (*LeaderEligibilityStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	leader, err := r.SelectLeader(ctx, view)
	if err != nil {
		return nil, err
	}

	eligible, reason := r.IsLeaderEligibleToPropose(ctx, leader.ID, view)

	return &LeaderEligibilityStatus{
		View:       view,
		LeaderID:   leader.ID,
		Eligible:   eligible,
		Reason:     reason,
		Reputation: leader.Reputation,
		IsActive:   leader.IsActive,
		JoinedView: leader.JoinedView,
	}, nil
}

// LeaderEligibilityStatus contains detailed eligibility information
type LeaderEligibilityStatus struct {
	View       uint64
	LeaderID   ValidatorID
	Eligible   bool
	Reason     string
	Reputation float64
	IsActive   bool
	JoinedView uint64
}
