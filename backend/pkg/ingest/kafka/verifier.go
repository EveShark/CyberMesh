package kafka

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"backend/pkg/state"
	"backend/pkg/utils"
)

const (
	domainAnomaly  = "ai.anomaly.v1"
	domainEvidence = "ai.evidence.v1"
	domainPolicy   = "ai.policy.v1"
)

// Security domains for AI message signatures

// VerifierConfig holds configurable verification limits
type VerifierConfig struct {
	MaxTimestampSkew time.Duration // Max clock skew allowed (default: 5m)
}

// DefaultVerifierConfig returns default verification configuration
func DefaultVerifierConfig() VerifierConfig {
	return VerifierConfig{
		MaxTimestampSkew: 5 * time.Minute,
	}
}

// VerifyAnomalyMsg verifies signature, content hash, and timestamp of AnomalyMsg
func VerifyAnomalyMsg(msg *AnomalyMsg, cfg VerifierConfig, log *utils.Logger) (*state.EventTx, error) {
	// Validate message structure
	if err := ValidateAnomalyMsg(msg); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Verify timestamp skew
	now := time.Now().Unix()
	skewSeconds := int64(cfg.MaxTimestampSkew.Seconds())
	if msg.TS > now+skewSeconds || msg.TS < now-skewSeconds {
		return nil, fmt.Errorf("timestamp skew exceeded: %d (now: %d, skew: %d)", msg.TS, now, skewSeconds)
	}

	// Check payload size BEFORE hashing (DoS protection)
	if len(msg.Payload) > MaxPayloadSize {
		return nil, fmt.Errorf("payload too large: %d bytes (max: %d)", len(msg.Payload), MaxPayloadSize)
	}

	// Verify ContentHash = SHA256(payload) - mempool enforces this
	actualContentHash := sha256.Sum256(msg.Payload)
	if msg.ContentHash != actualContentHash {
		return nil, fmt.Errorf("content hash mismatch: expected %x, got %x", msg.ContentHash[:8], actualContentHash[:8])
	}
	// PayloadHash field removed (was redundant with ContentHash)

	// Build canonical sign bytes for AI service messages
	// AI service Signer.sign() prepends domainAnomaly automatically
	// So AI signs: domainAnomaly + (ts||producer_id_len||producer_id||nonce||content_hash)
	// We need to build the SAME bytes to verify

	// Build payload WITHOUT domain (Signer already added it)
	payloadBytes := make([]byte, 0, 8+2+len(msg.ProducerID)+16+32)

	// Timestamp (8 bytes, big-endian)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(msg.TS))
	payloadBytes = append(payloadBytes, ts[:]...)

	// Producer ID length (2 bytes) + producer ID
	var pidLen [2]byte
	binary.BigEndian.PutUint16(pidLen[:], uint16(len(msg.ProducerID)))
	payloadBytes = append(payloadBytes, pidLen[:]...)
	payloadBytes = append(payloadBytes, msg.ProducerID...)

	// Nonce (16 bytes)
	payloadBytes = append(payloadBytes, msg.Nonce...)

	// Content hash (32 bytes)
	payloadBytes = append(payloadBytes, msg.ContentHash[:]...)

	// AI Signer prepends domainAnomaly
	signBytes := append([]byte(domainAnomaly), payloadBytes...)

	// Verify Ed25519 signature
	if !ed25519.Verify(msg.PubKey, signBytes, msg.Signature) {
		return nil, fmt.Errorf("signature verification failed")
	}

	severityFromPayload, confidenceFromPayload, err := extractPriorityMetadata(msg.Payload)
	if err != nil {
		return nil, fmt.Errorf("priority metadata validation failed: %w", err)
	}
	if severityFromPayload != msg.Severity {
		return nil, fmt.Errorf("priority metadata mismatch: severity")
	}
	if math.IsNaN(confidenceFromPayload) || math.IsNaN(msg.Confidence) {
		return nil, fmt.Errorf("priority metadata invalid: confidence is NaN")
	}
	if math.Abs(confidenceFromPayload-msg.Confidence) > 1e-6 {
		return nil, fmt.Errorf("priority metadata mismatch: confidence")
	}

	// Convert to state.EventTx
	// Note: EventTx.Data will contain the full message payload
	// When protobuf is available, this will be the serialized protobuf message
	tx := &state.EventTx{
		Ts:   msg.TS,
		Data: msg.Payload, // Full message payload
		Env: state.Envelope{
			ProducerID:  msg.ProducerID,
			Nonce:       msg.Nonce,
			Signature:   msg.Signature,
			PubKey:      msg.PubKey,
			Alg:         msg.Alg,
			ContentHash: msg.ContentHash,
		},
	}

	return tx, nil
}

func extractPriorityMetadata(payload []byte) (uint8, float64, error) {
	var meta struct {
		Severity   *float64 `json:"severity"`
		Confidence *float64 `json:"confidence"`
	}

	if err := json.Unmarshal(payload, &meta); err != nil {
		return 0, 0, fmt.Errorf("payload decode: %w", err)
	}

	if meta.Severity == nil {
		return 0, 0, fmt.Errorf("payload missing severity")
	}
	if meta.Confidence == nil {
		return 0, 0, fmt.Errorf("payload missing confidence")
	}

	sev := *meta.Severity
	if math.IsNaN(sev) {
		return 0, 0, fmt.Errorf("severity NaN")
	}
	if sev < 0 || sev > 10 {
		return 0, 0, fmt.Errorf("severity out of range: %.2f", sev)
	}
	if math.Mod(sev, 1.0) != 0 {
		return 0, 0, fmt.Errorf("severity must be integer: %.2f", sev)
	}

	conf := *meta.Confidence
	if math.IsNaN(conf) {
		return 0, 0, fmt.Errorf("confidence NaN")
	}
	if conf < 0 || conf > 1 {
		return 0, 0, fmt.Errorf("confidence out of range: %.4f", conf)
	}

	return uint8(sev), conf, nil
}

// VerifyEvidenceMsg verifies signature, content hash, and timestamp of EvidenceMsg
func VerifyEvidenceMsg(msg *EvidenceMsg, cfg VerifierConfig, log *utils.Logger) (*state.EvidenceTx, error) {
	// Validate message structure
	if err := ValidateEvidenceMsg(msg); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Verify timestamp skew
	now := time.Now().Unix()
	skewSeconds := int64(cfg.MaxTimestampSkew.Seconds())
	if msg.TS > now+skewSeconds || msg.TS < now-skewSeconds {
		return nil, fmt.Errorf("timestamp skew exceeded: %d (now: %d, skew: %d)", msg.TS, now, skewSeconds)
	}

	// Check proof size BEFORE hashing (DoS protection)
	if len(msg.ProofBlob) > MaxProofSize {
		return nil, fmt.Errorf("proof blob too large: %d bytes (max: %d)", len(msg.ProofBlob), MaxProofSize)
	}

	// Verify ContentHash = SHA256(ProofBlob) - mempool enforces this
	actualContentHash := sha256.Sum256(msg.ProofBlob)
	if msg.ContentHash != actualContentHash {
		return nil, fmt.Errorf("content hash mismatch: expected %x, got %x", msg.ContentHash[:8], actualContentHash[:8])
	}

	// Build canonical sign bytes with AI service domain
	// AI service Signer uses domainEvidence for evidence signatures
	signBytes, err := state.BuildSignBytes(domainEvidence, msg.TS, msg.ProducerID, msg.Nonce, msg.ContentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to build sign bytes: %w", err)
	}

	// Verify Ed25519 signature
	if !ed25519.Verify(msg.PubKey, signBytes, msg.Signature) {
		return nil, fmt.Errorf("signature verification failed")
	}

	// Convert CoC entries
	var coc []state.CoCEntry
	for _, entry := range msg.CoC {
		coc = append(coc, state.CoCEntry{
			RefHash:   entry.RefHash,
			ActorID:   entry.ActorID,
			Ts:        entry.TS,
			Signature: entry.Signature,
		})
	}

	// Convert to state.EvidenceTx
	// Note: EvidenceTx.Data will contain the full message payload
	tx := &state.EvidenceTx{
		Ts:   msg.TS,
		Data: msg.ProofBlob, // Evidence proof data
		Env: state.Envelope{
			ProducerID:  msg.ProducerID,
			Nonce:       msg.Nonce,
			Signature:   msg.Signature,
			PubKey:      msg.PubKey,
			Alg:         msg.Alg,
			ContentHash: msg.ContentHash,
		},
		CoC: coc,
	}

	return tx, nil
}

// VerifyPolicyMsg verifies signature, content hash, and timestamp of PolicyMsg
func VerifyPolicyMsg(msg *PolicyMsg, cfg VerifierConfig, log *utils.Logger) (*state.PolicyTx, error) {
	// Validate message structure
	if err := ValidatePolicyMsg(msg); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Verify timestamp skew
	now := time.Now().Unix()
	skewSeconds := int64(cfg.MaxTimestampSkew.Seconds())
	if msg.TS > now+skewSeconds || msg.TS < now-skewSeconds {
		return nil, fmt.Errorf("timestamp skew exceeded: %d (now: %d, skew: %d)", msg.TS, now, skewSeconds)
	}

	// Check params size BEFORE hashing (DoS protection)
	if len(msg.Params) > MaxPayloadSize {
		return nil, fmt.Errorf("params too large: %d bytes (max: %d)", len(msg.Params), MaxPayloadSize)
	}

	// Verify ContentHash = SHA256(Params) - mempool enforces this
	actualContentHash := sha256.Sum256(msg.Params)
	if msg.ContentHash != actualContentHash {
		return nil, fmt.Errorf("content hash mismatch: expected %x, got %x", msg.ContentHash[:8], actualContentHash[:8])
	}

	// Build canonical sign bytes with proper policy domain
	signBytes, err := state.BuildSignBytes(domainPolicy, msg.TS, msg.ProducerID, msg.Nonce, msg.ContentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to build sign bytes: %w", err)
	}

	// Verify Ed25519 signature
	if !ed25519.Verify(msg.PubKey, signBytes, msg.Signature) {
		return nil, fmt.Errorf("signature verification failed")
	}

	// Convert to state.PolicyTx
	// Note: PolicyTx.Data will contain the policy parameters
	tx := &state.PolicyTx{
		Ts:   msg.TS,
		Data: msg.Params, // Policy parameters
		Env: state.Envelope{
			ProducerID:  msg.ProducerID,
			Nonce:       msg.Nonce,
			Signature:   msg.Signature,
			PubKey:      msg.PubKey,
			Alg:         msg.Alg,
			ContentHash: msg.ContentHash,
		},
	}

	return tx, nil
}
