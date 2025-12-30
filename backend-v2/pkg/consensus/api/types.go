package api

import "backend/pkg/consensus/types"

// Re-export types from consensus/types for convenience within api package
type (
	ValidatorID    = types.ValidatorID
	ValidatorInfo  = types.ValidatorInfo
	BlockHash      = types.BlockHash
	Block          = types.Block
	QC             = types.QC
	CryptoService  = types.CryptoService
	AuditLogger    = types.AuditLogger
	Logger         = types.Logger
	ConfigManager  = types.ConfigManager
	IPAllowlist    = types.IPAllowlist
	ValidatorSet   = types.ValidatorSet
	LeaderRotation = types.LeaderRotation
)
