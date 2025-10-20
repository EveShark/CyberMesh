package types

import (
	"context"
	"time"
)

// CryptoService handles all cryptographic operations
// SINGLE definition used across all packages
type CryptoService interface {
	// Signing operations
	SignWithContext(ctx context.Context, data []byte) ([]byte, error)
	VerifyWithContext(ctx context.Context, data, signature, publicKey []byte, allowOldTimestamps bool) error

	// Key management
	GetKeyID() ValidatorID
	GetPublicKey(keyID ValidatorID) ([]byte, error)
	GetKeyVersion() (string, error)

	// Encryption operations (for future use)
	EncryptWithContext(ctx context.Context, plaintext []byte) ([]byte, error)
	DecryptWithContext(ctx context.Context, ciphertext []byte) ([]byte, error)
}

// AuditLogger provides tamper-evident audit logging
// SINGLE definition used across all packages
type AuditLogger interface {
	// Log levels
	Info(event string, fields map[string]interface{}) error
	Warn(event string, fields map[string]interface{}) error
	Error(event string, fields map[string]interface{}) error
	Security(event string, fields map[string]interface{}) error

	// Context-aware logging
	LogContext(ctx context.Context, event string, severity string, fields map[string]interface{}) error
}

// Logger provides structured logging
// SINGLE definition used across all packages
type Logger interface {
	// Context-aware logging
	InfoContext(ctx context.Context, msg string, fields ...interface{})
	WarnContext(ctx context.Context, msg string, fields ...interface{})
	ErrorContext(ctx context.Context, msg string, fields ...interface{})
	DebugContext(ctx context.Context, msg string, fields ...interface{})

	// Field helpers (for compatibility with different logging backends)
	With(fields ...interface{}) Logger
}

// ConfigManager provides type-safe configuration access
// SINGLE definition used across all packages
type ConfigManager interface {
	// Basic getters
	GetString(key, defaultValue string) string
	GetStringRequired(key string) (string, error)
	GetInt(key string, defaultValue int) int
	GetFloat64(key string, defaultValue float64) float64
	GetBool(key string, defaultValue bool) bool
	GetDuration(key string, defaultValue time.Duration) time.Duration

	// Advanced getters
	GetStringSlice(key string, defaultValue []string) []string
	GetIntRange(key string, defaultValue, min, max int) int

	// Secret management
	GetSecret(key string) (string, error)
}

// IPAllowlist validates network connections
// SINGLE definition used across all packages
type IPAllowlist interface {
	IsAllowed(ip string) bool
	IsAddrAllowed(addr string) error
	ValidateBeforeConnect(ctx context.Context, addr string) error
}

// ValidatorInfo contains validator metadata
type ValidatorInfo struct {
	ID         ValidatorID
	PublicKey  []byte
	Reputation float64
	IsActive   bool
	JoinedView uint64
	LastSeen   time.Time
}

// ValidatorSet provides validator information and management
// SINGLE definition used across all packages
type ValidatorSet interface {
	// Validator queries
	IsValidator(id ValidatorID) bool
	GetValidator(id ValidatorID) (*ValidatorInfo, error)
	GetValidators() []ValidatorInfo
	GetValidatorCount() int

	// Active status
	IsActive(id ValidatorID) bool
	IsActiveInView(id ValidatorID, view uint64) bool
}

// QuarantineManager tracks quarantined validators
type QuarantineManager interface {
	IsQuarantined(id ValidatorID) bool
	GetQuarantineExpiry(id ValidatorID) (time.Time, bool)
	GetQuarantinedCount() int
	Quarantine(id ValidatorID, duration time.Duration, reason string) error
	Release(id ValidatorID) error
}

// ProposalRecord represents a persisted proposal entry
type ProposalRecord struct {
	Hash     []byte
	Height   uint64
	View     uint64
	Proposer []byte
	Data     []byte
}

// QCRecord represents a persisted quorum certificate entry
type QCRecord struct {
	Hash   []byte
	Height uint64
	View   uint64
	Data   []byte
}

// VoteRecord represents a persisted vote entry
type VoteRecord struct {
	Hash      []byte
	View      uint64
	Height    uint64
	Voter     []byte
	BlockHash []byte
	Data      []byte
}

// EvidenceRecord represents a persisted evidence entry
type EvidenceRecord struct {
	Hash   []byte
	Height uint64
	Data   []byte
}

// StorageBackend defines the interface for persistent storage
type StorageBackend interface {
	// Proposal operations
	SaveProposal(ctx context.Context, hash []byte, height uint64, view uint64, proposer []byte, data []byte) error
	LoadProposal(ctx context.Context, hash []byte) ([]byte, error)
	ListProposals(ctx context.Context, minHeight uint64, limit int) ([]ProposalRecord, error)

	// QC operations
	SaveQC(ctx context.Context, hash []byte, height uint64, view uint64, data []byte) error
	LoadQC(ctx context.Context, hash []byte) ([]byte, error)
	ListQCs(ctx context.Context, minHeight uint64, limit int) ([]QCRecord, error)

	// Vote operations
	SaveVote(ctx context.Context, view uint64, height uint64, voter []byte, blockHash []byte, voteHash []byte, data []byte) error
	ListVotes(ctx context.Context, minHeight uint64, limit int) ([]VoteRecord, error)

	// Committed block operations
	SaveCommittedBlock(ctx context.Context, height uint64, hash []byte, qcData []byte) error
	LoadLastCommitted(ctx context.Context) (uint64, []byte, []byte, error)

	// Evidence operations
	SaveEvidence(ctx context.Context, hash []byte, height uint64, data []byte) error
	LoadEvidence(ctx context.Context, hash []byte) ([]byte, error)
	ListEvidence(ctx context.Context, minHeight uint64, limit int) ([]EvidenceRecord, error)

	// Genesis certificate operations
	LoadGenesisCertificate(ctx context.Context) ([]byte, bool, error)
	SaveGenesisCertificate(ctx context.Context, data []byte) error
	DeleteGenesisCertificate(ctx context.Context) error

	// Cleanup operations
	DeleteBefore(ctx context.Context, height uint64) error
	Close() error
}

// ConsensusCallbacks defines callbacks for consensus events
type ConsensusCallbacks interface {
	OnCommit(block Block, qc QC) error
	OnProposal(proposal interface{}) error
	OnVote(vote interface{}) error
	OnQCFormed(qc QC) error
}

// PacemakerCallbacks defines callbacks for pacemaker events
type PacemakerCallbacks interface {
	OnViewChange(newView uint64, highestQC QC) error
	OnNewView(newView uint64, newViewMsg interface{}) error
	OnTimeout(view uint64) error
}

// HeartbeatCallbacks defines callbacks for heartbeat events
type HeartbeatCallbacks interface {
	OnHeartbeatTimeout(view uint64, lastSeen time.Time) error
	OnLeaderAlive(view uint64, leaderID ValidatorID) error
}

// LeaderRotation handles leader selection
type LeaderRotation interface {
	SelectLeader(ctx context.Context, view uint64) (*ValidatorInfo, error)
	GetLeaderForView(ctx context.Context, view uint64) (ValidatorID, error)
	IsLeader(ctx context.Context, validatorID ValidatorID, view uint64) (bool, error)
	GetEligibleCount(ctx context.Context, view uint64) int
	IsLeaderEligibleToPropose(ctx context.Context, validatorID ValidatorID, view uint64) (bool, string)
}

// ReadinessOracle exposes validator readiness state to leader rotation and other components.
type ReadinessOracle interface {
	IsValidatorReady(id ValidatorID) bool
}

// PeerObserver provides peer connectivity metrics from the network layer.
type PeerObserver interface {
	GetConnectedPeerCount() int
	GetActivePeerCount(since time.Duration) int
	GetPeersSeenSinceStartup() int
}

// Pacemaker manages view timing and synchronization
type Pacemaker interface {
	Start(ctx context.Context) error
	Stop() error

	// View management
	GetCurrentView() uint64
	GetCurrentHeight() uint64
	SetCurrentHeight(height uint64)
	GetHighestQC() QC
	GetCurrentTimeout() time.Duration

	// Event handlers
	OnProposal(ctx context.Context)
	OnQC(ctx context.Context, qc QC)
	TriggerViewChange(ctx context.Context) error
	OnViewChange(ctx context.Context, vcMsg interface{}) error
	OnNewView(ctx context.Context, nvMsg interface{}) error
}

// MessageEncoder handles message serialization and verification
type MessageEncoder interface {
	Encode(msg Message) ([]byte, error)
	VerifyAndDecode(ctx context.Context, data []byte, msgType MessageType) (Message, error)
	VerifyQC(ctx context.Context, qc QC) error
	ClearCache()
	GetCacheStats() (size, capacity int)
}

// MessageValidator validates message business logic
type MessageValidator interface {
	ValidateProposal(ctx context.Context, p interface{}) error
	ValidateVote(ctx context.Context, v interface{}) error
	ValidateQC(ctx context.Context, qc QC) error
	ValidateViewChange(ctx context.Context, vc interface{}) error
	ValidateNewView(ctx context.Context, nv interface{}) error
	ValidateHeartbeat(ctx context.Context, hb interface{}) error
	ValidateEvidence(ctx context.Context, ev interface{}) error
}

// QuorumVerifier verifies quorum certificates
type QuorumVerifier interface {
	HasQuorum(votes map[ValidatorID]interface{}) bool
	VerifyQC(ctx context.Context, qc QC) error
	GetQuorumThreshold() int
	GetByzantineTolerance() int
	ValidateQuorumMath() error
	AggregateVotes(ctx context.Context, votes map[ValidatorID]interface{}, aggregatorID ValidatorID) (QC, error)
}

// Storage manages consensus state persistence
type Storage interface {
	Start(ctx context.Context) error
	Stop() error

	// Proposal operations
	StoreProposal(p interface{}) error
	GetProposal(hash BlockHash) interface{}

	// Vote operations
	StoreVote(v interface{}) error
	GetVotesByView(view uint64) []interface{}

	// QC operations
	StoreQC(qc QC) error
	GetQC(hash BlockHash) QC

	// Evidence operations
	StoreEvidence(e interface{}) error
	GetAllEvidence() []interface{}

	// Committed state
	CommitBlock(hash BlockHash, height uint64) error
	GetLastCommittedHeight() uint64
	GetLastCommittedQC() QC
	IsCommitted(height uint64) bool
	GetCommittedBlockHash(height uint64) (BlockHash, bool)

	// Maintenance
	Prune(currentHeight uint64)
	GetStats() interface{}
}

// HotStuffEngine defines the consensus engine interface
type HotStuffEngine interface {
	Start(ctx context.Context) error
	Stop() error

	// Consensus operations
	OnProposal(ctx context.Context, proposal interface{}) error
	OnVote(ctx context.Context, vote interface{}) error
	CreateProposal(ctx context.Context, block Block) (interface{}, error)

	// State queries
	GetCurrentView() uint64
	GetCurrentHeight() uint64
	GetLockedQC() QC
}
