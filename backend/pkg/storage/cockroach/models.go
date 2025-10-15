package cockroach

import (
	"time"
)

// BlockRow represents a row in the blocks table
type BlockRow struct {
	// Primary key
	Height uint64

	// Block identification
	BlockHash  []byte
	ParentHash []byte
	StateRoot  []byte
	TxRoot     []byte

	// Consensus metadata
	ProposerID []byte
	ViewNumber uint64
	Timestamp  time.Time

	// Block content
	TxCount int

	// BFT proof
	QCView       uint64
	QCSignatures []byte

	// Timestamps
	CommittedAt time.Time
}

// TxRow represents a row in the transactions table
type TxRow struct {
	// Primary key
	TxHash []byte

	// Block reference
	BlockHeight uint64
	TxIndex     int

	// Transaction type
	TxType string // "event", "evidence", "policy"

	// Envelope (mandatory)
	ProducerID  []byte
	Nonce       []byte
	ContentHash []byte
	Algorithm   string
	PublicKey   []byte
	Signature   []byte

	// Transaction payload
	Payload []byte // JSONB stored as bytes

	// Chain-of-custody (for EvidenceTx)
	CustodyChain []byte // JSONB stored as bytes, nullable

	// Execution result
	Status   string // "success", "failed", "skipped"
	ErrorMsg string // nullable

	// Timestamps
	SubmittedAt time.Time
	ExecutedAt  time.Time
}

// TxMeta is a lightweight projection for transaction listings
type TxMeta struct {
    TxHash    []byte
    TxIndex   int
    TxType    string
    SizeBytes int
}

// SnapshotRow represents a row in the state_versions table
type SnapshotRow struct {
	// Primary key
	Version uint64

	// State root
	StateRoot []byte

	// Block reference
	BlockHeight uint64
	BlockHash   []byte

	// Statistics
	TxCount           int
	ReputationChanges int
	PolicyChanges     int
	QuarantineChanges int

	// Timestamp
	CreatedAt time.Time
}

// ReputationRow represents a row in the state_reputation table
type ReputationRow struct {
	ProducerID    []byte
	Score         float64
	TotalEvents   uint64
	Violations    uint64
	LastViolation *time.Time // nullable
	UpdatedHeight uint64
	UpdatedAt     time.Time
}

// QuarantineRow represents a row in the state_quarantine table
type QuarantineRow struct {
	ProducerID    []byte
	Reason        string
	EvidenceHash  []byte
	QuarantinedAt time.Time
	ExpiresAt     *time.Time // nullable
	Permanent     bool
	AppliedHeight uint64
}

// PolicyRow represents a row in the state_policies table
type PolicyRow struct {
	PolicyID      []byte
	PolicyType    string // "allow", "deny", "rate_limit", "threshold"
	Target        string
	Rules         []byte // JSONB stored as bytes
	Active        bool
	Priority      int
	CreatedHeight uint64
	CreatedAt     time.Time
	UpdatedHeight *uint64    // nullable
	UpdatedAt     *time.Time // nullable
}

// AuditLogRow represents a row in the audit_logs table
type AuditLogRow struct {
	ID          int64
	EventType   string
	Severity    string
	Actor       string
	Action      string
	Resource    string
	Result      string
	Fields      []byte // JSONB stored as bytes
	SequenceNum uint64
	PrevHash    []byte
	RecordHash  []byte
	Timestamp   time.Time
}

// ValidatorRow represents a row in the validators table
type ValidatorRow struct {
	ValidatorID    []byte
	PublicKey      []byte
	PeerID         string
	IsActive       bool
	Reputation     float64
	JoinedHeight   uint64
	LastSeen       *time.Time // nullable
	BlocksProposed uint64
	BlocksVoted    uint64
	Address        string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
