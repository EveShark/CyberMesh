package leader

import (
	"context"
	"fmt"
	"sync"
	"time"

	"backend/pkg/consensus/types"
)

// Rotation handles deterministic leader selection with eligibility gating
type Rotation struct {
	validatorSet types.ValidatorSet
	quarantine   types.QuarantineManager
	config       *RotationConfig
	audit        AuditLogger
	logger       Logger
	mu           sync.RWMutex
}

// RotationConfig contains leader selection parameters
type RotationConfig struct {
	MinReputation       float64
	EnableQuarantine    bool
	EnableReputation    bool
	SelectionSeed       uint64
	FallbackToAll       bool
	MaxQuarantinedRatio float64
	AuditSelections     bool
}

// DefaultRotationConfig returns secure defaults
func DefaultRotationConfig() *RotationConfig {
	return &RotationConfig{
		MinReputation:       0.7,
		EnableQuarantine:    true,
		EnableReputation:    true,
		SelectionSeed:       0, // Set from config or random
		FallbackToAll:       true,
		MaxQuarantinedRatio: 0.33, // Don't allow >33% quarantined
		AuditSelections:     true,
	}
}

// NewRotation creates a new leader rotation manager
func NewRotation(
	validatorSet types.ValidatorSet,
	quarantine types.QuarantineManager,
	audit AuditLogger,
	logger Logger,
	config *RotationConfig,
) *Rotation {
	if config == nil {
		config = DefaultRotationConfig()
	}

	if quarantine == nil {
		quarantine = noopQuarantine{}
	}

	return &Rotation{
		validatorSet: validatorSet,
		quarantine:   quarantine,
		config:       config,
		audit:        audit,
		logger:       logger,
	}
}

type noopQuarantine struct{}

func (noopQuarantine) IsQuarantined(types.ValidatorID) bool { return false }

func (noopQuarantine) GetQuarantineExpiry(types.ValidatorID) (time.Time, bool) {
	return time.Time{}, false
}

func (noopQuarantine) GetQuarantinedCount() int { return 0 }

func (noopQuarantine) Quarantine(types.ValidatorID, time.Duration, string) error { return nil }

func (noopQuarantine) Release(types.ValidatorID) error { return nil }

// SelectLeader returns the leader for a given view using deterministic round-robin
func (r *Rotation) SelectLeader(ctx context.Context, view uint64) (*ValidatorInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get all validators
	allValidators := r.validatorSet.GetValidators()
	if len(allValidators) == 0 {
		return nil, fmt.Errorf("no validators in set")
	}

	// Filter eligible validators
	eligible := r.filterEligible(ctx, allValidators, view)

	// Handle empty eligible set
	if len(eligible) == 0 {
		return r.handleNoEligible(ctx, allValidators, view)
	}

	// Deterministic selection with seed
	index := r.selectIndex(view, len(eligible))
	leader := &eligible[index]

	// Audit the selection
	if r.config.AuditSelections {
		r.auditSelection(ctx, view, leader, len(eligible), len(allValidators))
	}

	return leader, nil
}

// filterEligible applies reputation and quarantine filters
func (r *Rotation) filterEligible(ctx context.Context, validators []ValidatorInfo, view uint64) []ValidatorInfo {
	eligible := make([]ValidatorInfo, 0, len(validators))

	for _, v := range validators {
		// Check if active
		if !v.IsActive {
			continue
		}

		// Check if joined before this view
		if v.JoinedView > view {
			continue
		}

		// Check quarantine status
		if r.config.EnableQuarantine && r.quarantine.IsQuarantined(v.ID) {
			r.logger.InfoContext(ctx, "validator quarantined, skipping",
				"validator", fmt.Sprintf("%x", v.ID[:8]),
				"view", view,
			)
			continue
		}

		// Check reputation threshold
		if r.config.EnableReputation && v.Reputation < r.config.MinReputation {
			r.logger.InfoContext(ctx, "validator below reputation threshold",
				"validator", fmt.Sprintf("%x", v.ID[:8]),
				"reputation", v.Reputation,
				"threshold", r.config.MinReputation,
				"view", view,
			)
			continue
		}

		eligible = append(eligible, v)
	}

	return eligible
}

// handleNoEligible handles the case when no validators are eligible
func (r *Rotation) handleNoEligible(ctx context.Context, allValidators []ValidatorInfo, view uint64) (*ValidatorInfo, error) {
	// Check quarantine ratio
	quarantinedCount := r.quarantine.GetQuarantinedCount()
	totalCount := len(allValidators)
	quarantinedRatio := float64(quarantinedCount) / float64(totalCount)

	// Log critical warning
	r.logger.WarnContext(ctx, "no eligible validators for leader selection",
		"view", view,
		"total_validators", totalCount,
		"quarantined", quarantinedCount,
		"quarantine_ratio", quarantinedRatio,
	)

	r.audit.Warn("no_eligible_leaders", map[string]interface{}{
		"view":              view,
		"total_validators":  totalCount,
		"quarantined_count": quarantinedCount,
		"quarantine_ratio":  quarantinedRatio,
	})

	// Check if too many quarantined
	if quarantinedRatio > r.config.MaxQuarantinedRatio {
		r.audit.Security("excessive_quarantine_ratio", map[string]interface{}{
			"view":             view,
			"quarantine_ratio": quarantinedRatio,
			"max_allowed":      r.config.MaxQuarantinedRatio,
		})
		return nil, fmt.Errorf("excessive quarantine ratio: %.2f exceeds max %.2f",
			quarantinedRatio, r.config.MaxQuarantinedRatio)
	}

	// Fallback to all active validators if configured
	if !r.config.FallbackToAll {
		return nil, fmt.Errorf("no eligible validators and fallback disabled")
	}

	// Use only active validators as fallback
	activeValidators := make([]ValidatorInfo, 0, len(allValidators))
	for _, v := range allValidators {
		if v.IsActive && v.JoinedView <= view {
			activeValidators = append(activeValidators, v)
		}
	}

	if len(activeValidators) == 0 {
		return nil, fmt.Errorf("no active validators available")
	}

	// Select from active validators
	index := r.selectIndex(view, len(activeValidators))
	leader := &activeValidators[index]

	r.audit.Warn("fallback_leader_selected", map[string]interface{}{
		"view":      view,
		"leader_id": fmt.Sprintf("%x", leader.ID[:]),
		"reason":    "no_eligible_validators",
	})

	return leader, nil
}

// selectIndex performs deterministic index selection
func (r *Rotation) selectIndex(view uint64, count int) int {
	// Combine view with seed for determinism
	// This ensures same view always selects same leader across all nodes
	combined := view + r.config.SelectionSeed

	// Use modulo for round-robin rotation
	return int(combined % uint64(count))
}

// GetLeaderForView returns the leader ID for a specific view (lightweight version)
func (r *Rotation) GetLeaderForView(ctx context.Context, view uint64) (ValidatorID, error) {
	leader, err := r.SelectLeader(ctx, view)
	if err != nil {
		return ValidatorID{}, err
	}
	return leader.ID, nil
}

// IsLeader checks if a given validator is the leader for a view
func (r *Rotation) IsLeader(ctx context.Context, validatorID ValidatorID, view uint64) (bool, error) {
	leaderID, err := r.GetLeaderForView(ctx, view)
	if err != nil {
		return false, err
	}
	return leaderID == validatorID, nil
}

// GetEligibleCount returns the number of eligible validators
func (r *Rotation) GetEligibleCount(ctx context.Context, view uint64) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	allValidators := r.validatorSet.GetValidators()
	eligible := r.filterEligible(ctx, allValidators, view)
	return len(eligible)
}

// auditSelection logs leader selection for audit trail
func (r *Rotation) auditSelection(ctx context.Context, view uint64, leader *ValidatorInfo, eligibleCount, totalCount int) {
	// Check if quarantined (shouldn't happen, but defensive)
	isQuarantined := r.quarantine.IsQuarantined(leader.ID)

	fields := map[string]interface{}{
		"view":           view,
		"leader_id":      fmt.Sprintf("%x", leader.ID[:]),
		"reputation":     leader.Reputation,
		"eligible_count": eligibleCount,
		"total_count":    totalCount,
		"is_quarantined": isQuarantined,
	}

	if isQuarantined {
		// This should never happen - log as security event
		r.audit.Security("quarantined_leader_selected", fields)
	} else {
		r.audit.Info("leader_selected", fields)
	}
}

// UpdateConfig allows runtime configuration updates
func (r *Rotation) UpdateConfig(config *RotationConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = config
}

// GetConfig returns current configuration (copy)
func (r *Rotation) GetConfig() RotationConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return *r.config
}

// ValidateRotation performs sanity checks on rotation configuration
func (r *Rotation) ValidateRotation(ctx context.Context) error {
	totalValidators := r.validatorSet.GetValidatorCount()

	if totalValidators == 0 {
		return fmt.Errorf("validator set is empty")
	}

	quarantinedCount := r.quarantine.GetQuarantinedCount()
	quarantinedRatio := float64(quarantinedCount) / float64(totalValidators)

	if quarantinedRatio > r.config.MaxQuarantinedRatio {
		r.logger.WarnContext(ctx, "high quarantine ratio detected",
			"ratio", quarantinedRatio,
			"max", r.config.MaxQuarantinedRatio,
		)
	}

	// Calculate f (byzantine fault tolerance)
	f := (totalValidators - 1) / 3
	minRequired := 2*f + 1

	eligibleCount := r.GetEligibleCount(ctx, 0) // View 0 as reference
	if eligibleCount < minRequired {
		return fmt.Errorf("insufficient eligible validators: have %d, need %d (N=%d, f=%d)",
			eligibleCount, minRequired, totalValidators, f)
	}

	return nil
}
