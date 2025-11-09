package kafka

import (
	"encoding/binary"
	"fmt"
	"io"

	pb "backend/proto"
	"google.golang.org/protobuf/proto"
)

// Message size limits (DoS protection)
const (
	MaxMessageSize  = 1 * 1024 * 1024 // 1MB total message
	MaxPayloadSize  = 512 * 1024      // 512KB payload
	MaxProofSize    = 256 * 1024      // 256KB proof blob
	MaxIDLength     = 128             // Max ID string length
	MaxStringLength = 256             // Max string field length
	MaxRefsCount    = 1000            // Max evidence references
	MaxCoCEntries   = 100             // Max chain-of-custody entries
)

// AnomalyMsg represents ai.anomalies.v1 message from AI service
type AnomalyMsg struct {
	ID           string   // Anomaly unique identifier
	Type         string   // Anomaly type (e.g., "port_scan", "malware")
	Source       string   // Source IP/hostname
	Severity     uint8    // 1-10 severity level
	Confidence   float64  // 0.0-1.0 confidence score
	TS           int64    // Unix timestamp (seconds)
	Payload      []byte   // Binary payload data
	ModelVersion string   // AI model version
	ProducerID   []byte   // AI node public key (32 bytes Ed25519)
	Nonce        []byte   // Unique nonce (16 bytes - state.NonceSize)
	Signature    []byte   // Ed25519 signature (64 bytes)
	PubKey       []byte   // Public key (32 bytes Ed25519)
	Alg          string   // "Ed25519"
	ContentHash  [32]byte // SHA-256 of canonical content
	Raw          []byte   // Original protobuf bytes
}

// EvidenceMsg represents ai.evidence.v1 message from AI service
type EvidenceMsg struct {
	ID           string     // Evidence unique identifier
	EvidenceType string     // Evidence type (e.g., "pcap", "log")
	Refs         [][32]byte // References to related anomalies/evidence
	ProofBlob    []byte     // Binary proof data (PCAP, logs, etc.)
	TS           int64      // Unix timestamp (seconds)
	ProducerID   []byte     // AI node public key (32 bytes Ed25519)
	Nonce        []byte     // Unique nonce (16 bytes - state.NonceSize)
	Signature    []byte     // Ed25519 signature (64 bytes)
	PubKey       []byte     // Public key (32 bytes Ed25519)
	Alg          string     // "Ed25519"
	ContentHash  [32]byte   // SHA-256 of canonical content
	CoC          []CoCEntry // Chain-of-custody trail
}

// PolicyMsg represents ai.policy.v1 message from AI service
type PolicyMsg struct {
	ID          string   // Policy unique identifier
	Action      string   // Action (e.g., "block", "quarantine", "alert")
	Rule        string   // Rule definition
	Params      []byte   // JSON-encoded parameters
	TS          int64    // Unix timestamp (seconds)
	ProducerID  []byte   // AI node public key (32 bytes Ed25519)
	Nonce       []byte   // Unique nonce (16 bytes - state.NonceSize)
	Signature   []byte   // Ed25519 signature (64 bytes)
	PubKey      []byte   // Public key (32 bytes Ed25519)
	Alg         string   // "Ed25519"
	ContentHash [32]byte // SHA-256 of canonical content
}

// CoCEntry represents a chain-of-custody entry in evidence
type CoCEntry struct {
	RefHash   [32]byte // Reference hash
	ActorID   []byte   // Actor public key (32 bytes Ed25519)
	TS        int64    // Unix timestamp (seconds)
	Signature []byte   // Ed25519 signature (64 bytes)
}

// ValidateAnomalyMsg validates message size and required fields
func ValidateAnomalyMsg(msg *AnomalyMsg) error {
	if len(msg.ID) == 0 || len(msg.ID) > MaxIDLength {
		return fmt.Errorf("invalid ID length: %d (max: %d)", len(msg.ID), MaxIDLength)
	}
	if len(msg.Type) == 0 || len(msg.Type) > MaxStringLength {
		return fmt.Errorf("invalid Type length: %d (max: %d)", len(msg.Type), MaxStringLength)
	}
	if len(msg.Source) == 0 || len(msg.Source) > MaxStringLength {
		return fmt.Errorf("invalid Source length: %d (max: %d)", len(msg.Source), MaxStringLength)
	}
	if msg.Severity == 0 || msg.Severity > 10 {
		return fmt.Errorf("invalid Severity: %d (must be 1-10)", msg.Severity)
	}
	if msg.Confidence < 0.0 || msg.Confidence > 1.0 {
		return fmt.Errorf("invalid Confidence: %f (must be 0.0-1.0)", msg.Confidence)
	}
	if msg.TS == 0 {
		return fmt.Errorf("missing TS")
	}
	if len(msg.Payload) == 0 || len(msg.Payload) > MaxPayloadSize {
		return fmt.Errorf("invalid Payload size: %d (max: %d)", len(msg.Payload), MaxPayloadSize)
	}
	if len(msg.ModelVersion) == 0 || len(msg.ModelVersion) > MaxStringLength {
		return fmt.Errorf("invalid ModelVersion length: %d (max: %d)", len(msg.ModelVersion), MaxStringLength)
	}
	if len(msg.ProducerID) != 32 {
		return fmt.Errorf("invalid ProducerID length: %d (expected: 32)", len(msg.ProducerID))
	}
	if len(msg.Nonce) != 16 {
		return fmt.Errorf("invalid Nonce length: %d (expected: 16)", len(msg.Nonce))
	}
	if len(msg.Signature) != 64 {
		return fmt.Errorf("invalid Signature length: %d (expected: 64)", len(msg.Signature))
	}
	if len(msg.PubKey) != 32 {
		return fmt.Errorf("invalid PubKey length: %d (expected: 32)", len(msg.PubKey))
	}
	if msg.Alg != "Ed25519" {
		return fmt.Errorf("unsupported Alg: %s (expected: Ed25519)", msg.Alg)
	}
	return nil
}

// ValidateEvidenceMsg validates message size and required fields
func ValidateEvidenceMsg(msg *EvidenceMsg) error {
	if len(msg.ID) == 0 || len(msg.ID) > MaxIDLength {
		return fmt.Errorf("invalid ID length: %d (max: %d)", len(msg.ID), MaxIDLength)
	}
	if len(msg.EvidenceType) == 0 || len(msg.EvidenceType) > MaxStringLength {
		return fmt.Errorf("invalid EvidenceType length: %d (max: %d)", len(msg.EvidenceType), MaxStringLength)
	}
	if len(msg.Refs) > MaxRefsCount {
		return fmt.Errorf("too many Refs: %d (max: %d)", len(msg.Refs), MaxRefsCount)
	}
	if len(msg.ProofBlob) > MaxProofSize {
		return fmt.Errorf("invalid ProofBlob size: %d (max: %d)", len(msg.ProofBlob), MaxProofSize)
	}
	if msg.TS == 0 {
		return fmt.Errorf("missing TS")
	}
	if len(msg.ProducerID) != 32 {
		return fmt.Errorf("invalid ProducerID length: %d (expected: 32)", len(msg.ProducerID))
	}
	if len(msg.Nonce) != 16 {
		return fmt.Errorf("invalid Nonce length: %d (expected: 16)", len(msg.Nonce))
	}
	if len(msg.Signature) != 64 {
		return fmt.Errorf("invalid Signature length: %d (expected: 64)", len(msg.Signature))
	}
	if len(msg.PubKey) != 32 {
		return fmt.Errorf("invalid PubKey length: %d (expected: 32)", len(msg.PubKey))
	}
	if msg.Alg != "Ed25519" {
		return fmt.Errorf("unsupported Alg: %s (expected: Ed25519)", msg.Alg)
	}
	if len(msg.CoC) > MaxCoCEntries {
		return fmt.Errorf("too many CoC entries: %d (max: %d)", len(msg.CoC), MaxCoCEntries)
	}
	for i, entry := range msg.CoC {
		if len(entry.ActorID) != 32 {
			return fmt.Errorf("CoC[%d]: invalid ActorID length: %d (expected: 32)", i, len(entry.ActorID))
		}
		if len(entry.Signature) != 64 {
			return fmt.Errorf("CoC[%d]: invalid Signature length: %d (expected: 64)", i, len(entry.Signature))
		}
	}
	return nil
}

// ValidatePolicyMsg validates message size and required fields
func ValidatePolicyMsg(msg *PolicyMsg) error {
	if len(msg.ID) == 0 || len(msg.ID) > MaxIDLength {
		return fmt.Errorf("invalid ID length: %d (max: %d)", len(msg.ID), MaxIDLength)
	}
	if len(msg.Action) == 0 || len(msg.Action) > MaxStringLength {
		return fmt.Errorf("invalid Action length: %d (max: %d)", len(msg.Action), MaxStringLength)
	}
	if len(msg.Rule) == 0 || len(msg.Rule) > MaxStringLength {
		return fmt.Errorf("invalid Rule length: %d (max: %d)", len(msg.Rule), MaxStringLength)
	}
	if len(msg.Params) > MaxPayloadSize {
		return fmt.Errorf("invalid Params size: %d (max: %d)", len(msg.Params), MaxPayloadSize)
	}
	if msg.TS == 0 {
		return fmt.Errorf("missing TS")
	}
	if len(msg.ProducerID) != 32 {
		return fmt.Errorf("invalid ProducerID length: %d (expected: 32)", len(msg.ProducerID))
	}
	if len(msg.Nonce) != 16 {
		return fmt.Errorf("invalid Nonce length: %d (expected: 16)", len(msg.Nonce))
	}
	if len(msg.Signature) != 64 {
		return fmt.Errorf("invalid Signature length: %d (expected: 64)", len(msg.Signature))
	}
	if len(msg.PubKey) != 32 {
		return fmt.Errorf("invalid PubKey length: %d (expected: 32)", len(msg.PubKey))
	}
	if msg.Alg != "Ed25519" {
		return fmt.Errorf("unsupported Alg: %s (expected: Ed25519)", msg.Alg)
	}
	return nil
}

// Length-prefixed binary encoding utilities

// writeBytes writes length-prefixed byte slice
func writeBytes(w io.Writer, data []byte) error {
	if err := binary.Write(w, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// readBytes reads length-prefixed byte slice with size limit
func readBytes(r io.Reader, maxSize int) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	if int(length) > maxSize {
		return nil, fmt.Errorf("data too large: %d (max: %d)", length, maxSize)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

// writeString writes length-prefixed string
func writeString(w io.Writer, s string) error {
	return writeBytes(w, []byte(s))
}

// readString reads length-prefixed string with size limit
func readString(r io.Reader, maxSize int) (string, error) {
	data, err := readBytes(r, maxSize)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ============================================================================
// TODO: AI TEAM - PROTOBUF SCHEMA REQUIRED
// ============================================================================
// The decode functions below require .proto schema files from the AI team.
//
// Required files:
//   1. ai_anomaly.proto   - Defines AnomalyEvent message structure
//   2. ai_evidence.proto  - Defines EvidenceEvent message structure
//   3. ai_policy.proto    - Defines PolicyEvent message structure
//
// Schema must include ALL fields from the structs above:
//   - ID, Type, Source, Severity, Confidence, TS, Payload, ModelVersion
//   - ProducerID, Nonce, Signature, PubKey, Alg, ContentHash
//   - For Evidence: Refs, ProofBlob, CoC (chain-of-custody)
//   - For Policy: Action, Rule, Params
//
// Once provided, run:
//   protoc --go_out=. --go_opt=paths=source_relative ai_anomaly.proto
//   protoc --go_out=. --go_opt=paths=source_relative ai_evidence.proto
//   protoc --go_out=. --go_opt=paths=source_relative ai_policy.proto
//
// Then replace the functions below with proper protobuf deserialization.
// ============================================================================

// DecodeAnomalyMsg decodes ai.anomalies.v1 message from bytes
func DecodeAnomalyMsg(data []byte) (*AnomalyMsg, error) {
	if len(data) > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d (max: %d)", len(data), MaxMessageSize)
	}

	p := &pb.AnomalyEvent{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("protobuf unmarshal failed: %w", err)
	}

	raw := make([]byte, len(data))
	copy(raw, data)

	var contentHash [32]byte
	if len(p.ContentHash) == 32 {
		copy(contentHash[:], p.ContentHash)
	}

	msg := &AnomalyMsg{
		ID:           p.Id,
		Type:         p.Type,
		Source:       p.Source,
		Severity:     uint8(p.Severity),
		Confidence:   p.Confidence,
		TS:           p.Ts,
		Payload:      p.Payload,
		ModelVersion: p.ModelVersion,
		ProducerID:   p.ProducerId,
		Nonce:        p.Nonce,
		Signature:    p.Signature,
		PubKey:       p.Pubkey,
		Alg:          p.Alg,
		ContentHash:  contentHash,
		Raw:          raw,
	}
	return msg, nil
}

// DecodeEvidenceMsg decodes ai.evidence.v1 message from bytes
func DecodeEvidenceMsg(data []byte) (*EvidenceMsg, error) {
	if len(data) > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d (max: %d)", len(data), MaxMessageSize)
	}

	p := &pb.EvidenceEvent{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("protobuf unmarshal failed: %w", err)
	}

	refs := make([][32]byte, len(p.Refs))
	for i, r := range p.Refs {
		if len(r) != 32 {
			return nil, fmt.Errorf("invalid ref[%d] size: %d", i, len(r))
		}
		copy(refs[i][:], r)
	}

	coc := make([]CoCEntry, len(p.Coc))
	for i, e := range p.Coc {
		var refHash [32]byte
		if len(e.RefHash) == 32 {
			copy(refHash[:], e.RefHash)
		}
		coc[i] = CoCEntry{
			RefHash:   refHash,
			ActorID:   e.ActorId,
			TS:        e.Ts,
			Signature: e.Signature,
		}
	}

	var contentHash [32]byte
	if len(p.ContentHash) == 32 {
		copy(contentHash[:], p.ContentHash)
	}

	msg := &EvidenceMsg{
		ID:           p.Id,
		EvidenceType: p.EvidenceType,
		Refs:         refs,
		ProofBlob:    p.ProofBlob,
		TS:           p.Ts,
		ProducerID:   p.ProducerId,
		Nonce:        p.Nonce,
		Signature:    p.Signature,
		PubKey:       p.Pubkey,
		Alg:          p.Alg,
		ContentHash:  contentHash,
		CoC:          coc,
	}
	return msg, nil
}

// DecodePolicyMsg decodes ai.policy.v1 message from bytes
func DecodePolicyMsg(data []byte) (*PolicyMsg, error) {
	if len(data) > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d (max: %d)", len(data), MaxMessageSize)
	}

	p := &pb.PolicyEvent{}
	if err := proto.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("protobuf unmarshal failed: %w", err)
	}

	var contentHash [32]byte
	if len(p.ContentHash) == 32 {
		copy(contentHash[:], p.ContentHash)
	}

	msg := &PolicyMsg{
		ID:          p.Id,
		Action:      p.Action,
		Rule:        p.Rule,
		Params:      p.Params,
		TS:          p.Ts,
		ProducerID:  p.ProducerId,
		Nonce:       p.Nonce,
		Signature:   p.Signature,
		PubKey:      p.Pubkey,
		Alg:         p.Alg,
		ContentHash: contentHash,
	}
	return msg, nil
}
