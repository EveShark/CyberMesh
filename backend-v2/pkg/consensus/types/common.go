package types

import (
	"time"
)

// ValidatorID uniquely identifies a validator (32-byte identifier)
// This is the SINGLE source of truth for validator identification
type ValidatorID [32]byte

// BlockHash represents a cryptographic hash of a block
type BlockHash [32]byte

// Signature represents a cryptographic signature with metadata
type Signature struct {
	Bytes     []byte      `cbor:"1,keyasint"`
	KeyID     ValidatorID `cbor:"2,keyasint"`
	Timestamp time.Time   `cbor:"3,keyasint"`
}

// Block defines the interface for consensus blocks
type Block interface {
	GetHeight() uint64
	GetHash() BlockHash
	GetParentHash() BlockHash
	GetTransactionCount() int
	GetTimestamp() time.Time
}

// QC defines the interface for Quorum Certificates
// This allows different implementations while maintaining compatibility
type QC interface {
	GetView() uint64
	GetHeight() uint64
	GetBlockHash() BlockHash
	GetSignatures() []Signature
	GetTimestamp() time.Time
	Hash() BlockHash
}

// MessageType identifies the type of consensus message
type MessageType uint8

const (
	MessageTypeProposal MessageType = iota + 1
	MessageTypeVote
	MessageTypeQC
	MessageTypeViewChange
	MessageTypeNewView
	MessageTypeHeartbeat
	MessageTypeEvidence
	MessageTypeGenesisReady
	MessageTypeGenesisCertificate
	MessageTypeProposalIntent
	MessageTypeReadyToVote
)

// Message is the base interface for all consensus messages
type Message interface {
	Type() MessageType
	Hash() BlockHash
	GetView() uint64
	GetTimestamp() time.Time
}

// EvidenceType classifies Byzantine misbehavior
type EvidenceType uint8

const (
	EvidenceDoubleVote EvidenceType = iota + 1
	EvidenceDoublePropose
	EvidenceInvalidSignature
	EvidenceTimestampViolation
)

// Domain separators for signature security
const (
	DomainProposal           = "CONSENSUS_PROPOSAL_V1"
	DomainVote               = "CONSENSUS_VOTE_V1"
	DomainQC                 = "CONSENSUS_QC_V1"
	DomainViewChange         = "CONSENSUS_VIEWCHANGE_V1"
	DomainNewView            = "CONSENSUS_NEWVIEW_V1"
	DomainHeartbeat          = "CONSENSUS_HEARTBEAT_V1"
	DomainEvidence           = "CONSENSUS_EVIDENCE_V1"
	DomainGenesisReady       = "CONSENSUS_GENESIS_READY_V1"
	DomainGenesisCertificate = "CONSENSUS_GENESIS_CERT_V1"
	DomainProposalIntent     = "CONSENSUS_PROPOSAL_INTENT_V1"
	DomainReadyToVote        = "CONSENSUS_READY_TO_VOTE_V1"
)
