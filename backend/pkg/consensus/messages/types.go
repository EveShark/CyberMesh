package messages

import (
	"crypto/sha256"
	"encoding/binary"
	"time"

	"backend/pkg/consensus/types"
)

// Re-export common types for convenience
type (
	ValidatorID = types.ValidatorID
	// KeyID is an alias used within this package; unify on ValidatorID underneath
	KeyID        = types.ValidatorID
	BlockHash    = types.BlockHash
	Signature    = types.Signature
	MessageType  = types.MessageType
	EvidenceType = types.EvidenceType
	Block        = types.Block
)

// Re-export message types
const (
	TypeProposal           = types.MessageTypeProposal
	TypeVote               = types.MessageTypeVote
	TypeQC                 = types.MessageTypeQC
	TypeViewChange         = types.MessageTypeViewChange
	TypeNewView            = types.MessageTypeNewView
	TypeHeartbeat          = types.MessageTypeHeartbeat
	TypeEvidence           = types.MessageTypeEvidence
	TypeGenesisReady       = types.MessageTypeGenesisReady
	TypeGenesisCertificate = types.MessageTypeGenesisCertificate
	TypeProposalIntent     = types.MessageTypeProposalIntent
	TypeReadyToVote        = types.MessageTypeReadyToVote
)

// Re-export domain separators
const (
	DomainProposal           = types.DomainProposal
	DomainVote               = types.DomainVote
	DomainQC                 = types.DomainQC
	DomainViewChange         = types.DomainViewChange
	DomainNewView            = types.DomainNewView
	DomainHeartbeat          = types.DomainHeartbeat
	DomainEvidence           = types.DomainEvidence
	DomainGenesisReady       = types.DomainGenesisReady
	DomainGenesisCertificate = types.DomainGenesisCertificate
	DomainProposalIntent     = types.DomainProposalIntent
	DomainReadyToVote        = types.DomainReadyToVote
)

// Re-export evidence types
const (
	EvidenceDoubleVote         = types.EvidenceDoubleVote
	EvidenceDoublePropose      = types.EvidenceDoublePropose
	EvidenceInvalidSignature   = types.EvidenceInvalidSignature
	EvidenceTimestampViolation = types.EvidenceTimestampViolation
)

// Proposal represents a block proposal from the leader
type Proposal struct {
	View       uint64      `cbor:"1,keyasint"`
	Height     uint64      `cbor:"2,keyasint"`
	Round      uint64      `cbor:"3,keyasint"`
	BlockHash  BlockHash   `cbor:"4,keyasint"`
	ParentHash BlockHash   `cbor:"5,keyasint"`
	ProposerID ValidatorID `cbor:"6,keyasint"`
	Timestamp  time.Time   `cbor:"7,keyasint"`
	JustifyQC  *QC         `cbor:"8,keyasint,omitempty"`
	Block      Block       `cbor:"9,keyasint,omitempty"` // FIX: Added Block field
	Signature  Signature   `cbor:"10,keyasint"`
}

// SignBytes returns the canonical bytes for signing a Proposal
func (p *Proposal) SignBytes() []byte {
	buf := make([]byte, 0, 256)

	// Domain separator
	buf = append(buf, []byte(DomainProposal)...)
	buf = append(buf, 0x00) // Separator

	// View, Height, Round
	buf = appendUint64(buf, p.View)
	buf = appendUint64(buf, p.Height)
	buf = appendUint64(buf, p.Round)

	// Block hashes
	buf = append(buf, p.BlockHash[:]...)
	buf = append(buf, p.ParentHash[:]...)

	// Proposer identity
	buf = append(buf, p.ProposerID[:]...)

	// Timestamp (Unix nanos for precision)
	buf = appendInt64(buf, p.Timestamp.UnixNano())

	// Do not include signature metadata in canonical bytes

	// JustifyQC hash if present
	if p.JustifyQC != nil {
		h := p.JustifyQC.Hash()
		buf = append(buf, h[:]...)
	}

	return buf
}

// Hash returns the content hash of the proposal
func (p *Proposal) Hash() BlockHash {
	return sha256.Sum256(p.SignBytes())
}

// Type returns the message type
func (p *Proposal) Type() MessageType {
	return TypeProposal
}

// GetView returns the view (implements types.Message interface)
func (p *Proposal) GetView() uint64 {
	return p.View
}

// GetTimestamp returns the timestamp (implements types.Message interface)
func (p *Proposal) GetTimestamp() time.Time {
	return p.Timestamp
}

// Vote represents a validator's vote on a proposal
type Vote struct {
	View      uint64    `cbor:"1,keyasint"`
	Height    uint64    `cbor:"2,keyasint"`
	Round     uint64    `cbor:"3,keyasint"`
	BlockHash BlockHash `cbor:"4,keyasint"`
	VoterID   KeyID     `cbor:"5,keyasint"`
	Timestamp time.Time `cbor:"6,keyasint"`
	Signature Signature `cbor:"7,keyasint"`
}

// SignBytes returns the canonical bytes for signing a Vote
func (v *Vote) SignBytes() []byte {
	buf := make([]byte, 0, 128)

	buf = append(buf, []byte(DomainVote)...)
	buf = append(buf, 0x00)

	buf = appendUint64(buf, v.View)
	buf = appendUint64(buf, v.Height)
	buf = appendUint64(buf, v.Round)
	buf = append(buf, v.BlockHash[:]...)
	buf = append(buf, v.VoterID[:]...)
	buf = appendInt64(buf, v.Timestamp.UnixNano())

	return buf
}

// Hash returns the content hash of the vote
func (v *Vote) Hash() BlockHash {
	return sha256.Sum256(v.SignBytes())
}

// Type returns the message type
func (v *Vote) Type() MessageType {
	return TypeVote
}

// GetView returns the view (implements types.Message interface)
func (v *Vote) GetView() uint64 {
	return v.View
}

// GetTimestamp returns the timestamp (implements types.Message interface)
func (v *Vote) GetTimestamp() time.Time {
	return v.Timestamp
}

// QC (Quorum Certificate) aggregates votes to prove consensus
type QC struct {
	View         uint64      `cbor:"1,keyasint"`
	Height       uint64      `cbor:"2,keyasint"`
	Round        uint64      `cbor:"3,keyasint"`
	BlockHash    BlockHash   `cbor:"4,keyasint"`
	Signatures   []Signature `cbor:"5,keyasint"`
	Timestamp    time.Time   `cbor:"6,keyasint"`
	AggregatorID ValidatorID `cbor:"7,keyasint"` // Who created this QC
}

// GetView implements types.QC interface
func (qc *QC) GetView() uint64 {
	return qc.View
}

// GetHeight implements types.QC interface
func (qc *QC) GetHeight() uint64 {
	return qc.Height
}

// GetBlockHash implements types.QC interface
func (qc *QC) GetBlockHash() BlockHash {
	return qc.BlockHash
}

// GetSignatures implements types.QC interface
func (qc *QC) GetSignatures() []Signature {
	return qc.Signatures
}

// GetTimestamp implements types.QC interface
func (qc *QC) GetTimestamp() time.Time {
	return qc.Timestamp
}

// SignBytes returns the canonical bytes for the QC vote hash
func (qc *QC) SignBytes() []byte {
	buf := make([]byte, 0, 128)

	buf = append(buf, []byte(DomainQC)...)
	buf = append(buf, 0x00)

	buf = appendUint64(buf, qc.View)
	buf = appendUint64(buf, qc.Height)
	buf = appendUint64(buf, qc.Round)
	buf = append(buf, qc.BlockHash[:]...)
	buf = appendInt64(buf, qc.Timestamp.UnixNano())

	return buf
}

// Hash returns the content hash of the QC
func (qc *QC) Hash() BlockHash {
	return sha256.Sum256(qc.SignBytes())
}

// Type returns the message type
func (qc *QC) Type() MessageType {
	return TypeQC
}

// ViewChange signals intent to move to a new view
type ViewChange struct {
	OldView   uint64      `cbor:"1,keyasint"`
	NewView   uint64      `cbor:"2,keyasint"`
	Height    uint64      `cbor:"3,keyasint"`
	HighestQC *QC         `cbor:"4,keyasint,omitempty"`
	SenderID  ValidatorID `cbor:"5,keyasint"`
	Timestamp time.Time   `cbor:"6,keyasint"`
	Signature Signature   `cbor:"7,keyasint"`
}

// SignBytes returns the canonical bytes for signing a ViewChange
func (vc *ViewChange) SignBytes() []byte {
	buf := make([]byte, 0, 128)

	buf = append(buf, []byte(DomainViewChange)...)
	buf = append(buf, 0x00)

	buf = appendUint64(buf, vc.OldView)
	buf = appendUint64(buf, vc.NewView)
	buf = appendUint64(buf, vc.Height)
	buf = append(buf, vc.SenderID[:]...)
	buf = appendInt64(buf, vc.Timestamp.UnixNano())

	if vc.HighestQC != nil {
		h := vc.HighestQC.Hash()
		buf = append(buf, h[:]...)
	}

	return buf
}

// Hash returns the content hash of the view change
func (vc *ViewChange) Hash() BlockHash {
	return sha256.Sum256(vc.SignBytes())
}

// Type returns the message type
func (vc *ViewChange) Type() MessageType {
	return TypeViewChange
}

// GetView returns the view (implements types.Message interface)
func (vc *ViewChange) GetView() uint64 {
	return vc.NewView // Use NewView since that's where we're transitioning to
}

// GetTimestamp returns the timestamp (implements types.Message interface)
func (vc *ViewChange) GetTimestamp() time.Time {
	return vc.Timestamp
}

// NewView announces the start of a new view by the new leader
type NewView struct {
	View        uint64       `cbor:"1,keyasint"`
	Height      uint64       `cbor:"2,keyasint"`
	ViewChanges []ViewChange `cbor:"3,keyasint"` // 2f+1 proof
	HighestQC   *QC          `cbor:"4,keyasint"`
	LeaderID    ValidatorID  `cbor:"5,keyasint"`
	Timestamp   time.Time    `cbor:"6,keyasint"`
	Signature   Signature    `cbor:"7,keyasint"`
}

// SignBytes returns the canonical bytes for signing a NewView
func (nv *NewView) SignBytes() []byte {
	buf := make([]byte, 0, 256)

	buf = append(buf, []byte(DomainNewView)...)
	buf = append(buf, 0x00)

	buf = appendUint64(buf, nv.View)
	buf = appendUint64(buf, nv.Height)
	buf = append(buf, nv.LeaderID[:]...)
	buf = appendInt64(buf, nv.Timestamp.UnixNano())

	// Hash all ViewChanges
	for _, vc := range nv.ViewChanges {
		h := vc.Hash()
		buf = append(buf, h[:]...)
	}

	if nv.HighestQC != nil {
		h := nv.HighestQC.Hash()
		buf = append(buf, h[:]...)
	}

	return buf
}

// Hash returns the content hash of the new view
func (nv *NewView) Hash() BlockHash {
	return sha256.Sum256(nv.SignBytes())
}

// Type returns the message type
func (nv *NewView) Type() MessageType {
	return TypeNewView
}

// GetView returns the view (implements types.Message interface)
func (nv *NewView) GetView() uint64 {
	return nv.View
}

// GetTimestamp returns the timestamp (implements types.Message interface)
func (nv *NewView) GetTimestamp() time.Time {
	return nv.Timestamp
}

// Heartbeat signals leader liveness
type Heartbeat struct {
	View      uint64      `cbor:"1,keyasint"`
	Height    uint64      `cbor:"2,keyasint"`
	LeaderID  ValidatorID `cbor:"3,keyasint"`
	Timestamp time.Time   `cbor:"4,keyasint"`
	Signature Signature   `cbor:"5,keyasint"`
}

// SignBytes returns the canonical bytes for signing a Heartbeat
func (hb *Heartbeat) SignBytes() []byte {
	buf := make([]byte, 0, 96)

	buf = append(buf, []byte(DomainHeartbeat)...)
	buf = append(buf, 0x00)

	buf = appendUint64(buf, hb.View)
	buf = appendUint64(buf, hb.Height)
	buf = append(buf, hb.LeaderID[:]...)
	// Use millisecond precision to avoid CBOR time precision loss across encode/decode
	buf = appendInt64(buf, hb.Timestamp.UnixMilli())

	return buf
}

// Hash returns the content hash of the heartbeat
func (hb *Heartbeat) Hash() BlockHash {
	return sha256.Sum256(hb.SignBytes())
}

// Type returns the message type
func (hb *Heartbeat) Type() MessageType {
	return TypeHeartbeat
}

// GetView returns the view (implements types.Message interface)
func (hb *Heartbeat) GetView() uint64 {
	return hb.View
}

// GetTimestamp returns the timestamp (implements types.Message interface)
func (hb *Heartbeat) GetTimestamp() time.Time {
	return hb.Timestamp
}

// Evidence proves Byzantine behavior
type Evidence struct {
	Kind       EvidenceType `cbor:"1,keyasint"`
	View       uint64       `cbor:"2,keyasint"`
	Height     uint64       `cbor:"3,keyasint"`
	OffenderID KeyID        `cbor:"4,keyasint"`
	Proof      []byte       `cbor:"5,keyasint"` // Serialized conflicting messages
	ReporterID KeyID        `cbor:"6,keyasint"`
	Timestamp  time.Time    `cbor:"7,keyasint"`
	Signature  Signature    `cbor:"8,keyasint"`
}

// SignBytes returns the canonical bytes for signing Evidence
func (e *Evidence) SignBytes() []byte {
	buf := make([]byte, 0, 128)

	buf = append(buf, []byte(DomainEvidence)...)
	buf = append(buf, 0x00)

	buf = append(buf, byte(e.Kind))
	buf = appendUint64(buf, e.View)
	buf = appendUint64(buf, e.Height)
	buf = append(buf, e.OffenderID[:]...)

	// Hash the proof instead of including full bytes
	proofHash := sha256.Sum256(e.Proof)
	buf = append(buf, proofHash[:]...)

	buf = append(buf, e.ReporterID[:]...)
	buf = appendInt64(buf, e.Timestamp.UnixNano())

	return buf
}

// Hash returns the content hash of the evidence (FIX: Issue #12)
func (e *Evidence) Hash() BlockHash {
	h := sha256.Sum256(e.SignBytes())
	var blockHash BlockHash
	copy(blockHash[:], h[:])
	return blockHash
}

// Type returns the message type
func (e *Evidence) Type() MessageType {
	return TypeEvidence
}

// GetView returns the view (implements types.Message interface)
func (e *Evidence) GetView() uint64 {
	return e.View
}

// GetTimestamp returns the timestamp (implements types.Message interface)
func (e *Evidence) GetTimestamp() time.Time {
	return e.Timestamp
}

// GenesisReady is a cryptographic attestation that a validator is ready to join consensus.
type GenesisReady struct {
	ValidatorID ValidatorID `cbor:"1,keyasint"`
	Timestamp   time.Time   `cbor:"2,keyasint"`
	ConfigHash  [32]byte    `cbor:"3,keyasint"`
	PeerHash    [32]byte    `cbor:"4,keyasint"`
	Nonce       [32]byte    `cbor:"5,keyasint"`
	Signature   Signature   `cbor:"6,keyasint"`
}

// SignBytes returns the canonical bytes for signing a GenesisReady attestation.
func (gr *GenesisReady) SignBytes() []byte {
	buf := make([]byte, 0, 160)
	buf = append(buf, []byte(DomainGenesisReady)...)
	buf = append(buf, 0x00)
	buf = append(buf, gr.ValidatorID[:]...)
	buf = appendInt64(buf, gr.Timestamp.UnixNano())
	buf = append(buf, gr.ConfigHash[:]...)
	buf = append(buf, gr.PeerHash[:]...)
	buf = append(buf, gr.Nonce[:]...)
	return buf
}

// Hash returns the content hash of the attestation.
func (gr *GenesisReady) Hash() BlockHash {
	return sha256.Sum256(gr.SignBytes())
}

// Type returns the message type.
func (gr *GenesisReady) Type() MessageType { return TypeGenesisReady }

// GetView returns the view; genesis messages are view-agnostic.
func (gr *GenesisReady) GetView() uint64 { return 0 }

// GetTimestamp returns the attestation timestamp.
func (gr *GenesisReady) GetTimestamp() time.Time { return gr.Timestamp }

// GenesisCertificate aggregates READY attestations proving quorum readiness.
type GenesisCertificate struct {
	Attestations []GenesisReady `cbor:"1,keyasint"`
	Aggregator   ValidatorID    `cbor:"2,keyasint"`
	Timestamp    time.Time      `cbor:"3,keyasint"`
	Signature    Signature      `cbor:"4,keyasint"`
}

// SignBytes returns the canonical bytes representing the certificate contents.
func (gc *GenesisCertificate) SignBytes() []byte {
	buf := make([]byte, 0, 256)
	buf = append(buf, []byte(DomainGenesisCertificate)...)
	buf = append(buf, 0x00)
	buf = append(buf, gc.Aggregator[:]...)
	buf = appendInt64(buf, gc.Timestamp.UnixNano())
	for _, att := range gc.Attestations {
		h := att.Hash()
		buf = append(buf, h[:]...)
	}
	return buf
}

// Hash returns the content hash of the certificate.
func (gc *GenesisCertificate) Hash() BlockHash {
	return sha256.Sum256(gc.SignBytes())
}

// Type returns the message type.
func (gc *GenesisCertificate) Type() MessageType { return TypeGenesisCertificate }

// GetView returns the view; certificates are view-agnostic.
func (gc *GenesisCertificate) GetView() uint64 { return 0 }

// GetTimestamp returns the timestamp of the certificate aggregation.
func (gc *GenesisCertificate) GetTimestamp() time.Time { return gc.Timestamp }

// ProposalIntent announces a leader's intent to propose and challenges replicas for readiness.
type ProposalIntent struct {
	View      uint64      `cbor:"1,keyasint"`
	Height    uint64      `cbor:"2,keyasint"`
	LeaderID  ValidatorID `cbor:"3,keyasint"`
	Nonce     [32]byte    `cbor:"4,keyasint"`
	Timestamp time.Time   `cbor:"5,keyasint"`
	Signature Signature   `cbor:"6,keyasint"`
}

func (pi *ProposalIntent) SignBytes() []byte {
	buf := make([]byte, 0, 128)
	buf = append(buf, []byte(DomainProposalIntent)...)
	buf = append(buf, 0x00)
	buf = appendUint64(buf, pi.View)
	buf = appendUint64(buf, pi.Height)
	buf = append(buf, pi.LeaderID[:]...)
	buf = append(buf, pi.Nonce[:]...)
	buf = appendInt64(buf, pi.Timestamp.UnixNano())
	return buf
}

func (pi *ProposalIntent) Hash() BlockHash {
	return sha256.Sum256(pi.SignBytes())
}

func (pi *ProposalIntent) Type() MessageType { return TypeProposalIntent }

func (pi *ProposalIntent) GetView() uint64 { return pi.View }

func (pi *ProposalIntent) GetTimestamp() time.Time { return pi.Timestamp }

// ReadyToVote acknowledges a leader's proposal intent and readiness to vote.
type ReadyToVote struct {
	View        uint64      `cbor:"1,keyasint"`
	Height      uint64      `cbor:"2,keyasint"`
	ValidatorID ValidatorID `cbor:"3,keyasint"`
	LeaderID    ValidatorID `cbor:"4,keyasint"`
	Nonce       [32]byte    `cbor:"5,keyasint"`
	Timestamp   time.Time   `cbor:"6,keyasint"`
	Signature   Signature   `cbor:"7,keyasint"`
}

func (rtv *ReadyToVote) SignBytes() []byte {
	buf := make([]byte, 0, 160)
	buf = append(buf, []byte(DomainReadyToVote)...)
	buf = append(buf, 0x00)
	buf = appendUint64(buf, rtv.View)
	buf = appendUint64(buf, rtv.Height)
	buf = append(buf, rtv.ValidatorID[:]...)
	buf = append(buf, rtv.LeaderID[:]...)
	buf = append(buf, rtv.Nonce[:]...)
	buf = appendInt64(buf, rtv.Timestamp.UnixNano())
	return buf
}

func (rtv *ReadyToVote) Hash() BlockHash {
	return sha256.Sum256(rtv.SignBytes())
}

func (rtv *ReadyToVote) Type() MessageType { return TypeReadyToVote }

func (rtv *ReadyToVote) GetView() uint64 { return rtv.View }

func (rtv *ReadyToVote) GetTimestamp() time.Time { return rtv.Timestamp }

// Message is a union type for all consensus messages
type Message interface {
	SignBytes() []byte
	Hash() [32]byte
	Type() MessageType
}

// Helper functions for canonical encoding
func appendUint64(buf []byte, val uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, val)
	return append(buf, b...)
}

func appendInt64(buf []byte, val int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(val))
	return append(buf, b...)
}
