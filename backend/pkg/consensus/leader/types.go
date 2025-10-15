package leader

import "backend/pkg/consensus/types"

// Re-export types from consensus/types for convenience within leader package
type (
	ValidatorID        = types.ValidatorID
	ValidatorInfo      = types.ValidatorInfo
	AuditLogger        = types.AuditLogger
	Logger             = types.Logger
	CryptoService      = types.CryptoService
	HeartbeatCallbacks = types.HeartbeatCallbacks
	QC                 = types.QC
	ValidatorSet       = types.ValidatorSet
	BlockHash          = types.BlockHash
)
