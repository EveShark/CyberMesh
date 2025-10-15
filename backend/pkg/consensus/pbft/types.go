package pbft

import "backend/pkg/consensus/types"

// Re-export types from consensus/types for convenience within pbft package
type (
	ValidatorID    = types.ValidatorID
	BlockHash      = types.BlockHash
	Signature      = types.Signature
	ValidatorSet   = types.ValidatorSet
	MessageEncoder = types.MessageEncoder
	AuditLogger    = types.AuditLogger
	Logger         = types.Logger
	QC             = types.QC
	Block          = types.Block
	StorageBackend = types.StorageBackend
)
