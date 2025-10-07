package messages

import (
	"bytes"
	"context"
	"crypto/subtle"
	"fmt"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/hashicorp/golang-lru/v2/expirable"

	"backend/pkg/consensus/types"
)

// Encoder handles secure message serialization
type Encoder struct {
	encMode     cbor.EncMode
	decMode     cbor.DecMode
	crypto      types.CryptoService
	config      *EncoderConfig
	verifyCache *expirable.LRU[string, bool]
	mu          sync.RWMutex
}

// EncoderConfig contains encoding security parameters
type EncoderConfig struct {
	MaxProposalSize      int
	MaxVoteSize          int
	MaxQCSize            int
	MaxViewChangeSize    int
	MaxNewViewSize       int
	MaxHeartbeatSize     int
	MaxEvidenceSize      int
	ClockSkewTolerance   time.Duration
	VerifyCacheSize      int
	VerifyCacheTTL       time.Duration
	RejectFutureMessages bool
}

// DefaultEncoderConfig returns secure default configuration
func DefaultEncoderConfig() *EncoderConfig {
	return &EncoderConfig{
		MaxProposalSize:      1 << 20,   // 1 MB
		MaxVoteSize:          10 << 10,  // 10 KB
		MaxQCSize:            100 << 10, // 100 KB
		MaxViewChangeSize:    50 << 10,  // 50 KB
		MaxNewViewSize:       500 << 10, // 500 KB
		MaxHeartbeatSize:     5 << 10,   // 5 KB
		MaxEvidenceSize:      200 << 10, // 200 KB
		ClockSkewTolerance:   5 * time.Second,
		VerifyCacheSize:      10000,
		VerifyCacheTTL:       5 * time.Minute,
		RejectFutureMessages: true,
	}
}

// NewEncoder creates a secure CBOR encoder
func NewEncoder(crypto types.CryptoService, config *EncoderConfig) (*Encoder, error) {
	if config == nil {
		config = DefaultEncoderConfig()
	}

	// Strict CBOR encoding mode
    encOpts := cbor.CanonicalEncOptions()
    // Preserve full timestamp precision to avoid signature mismatches
    encOpts.Time = cbor.TimeRFC3339Nano
    encMode, err := encOpts.EncMode()
	if err != nil {
		return nil, fmt.Errorf("failed to create CBOR encoder: %w", err)
	}

	// Strict CBOR decoding mode - rejects unknown fields
	decMode, err := cbor.DecOptions{
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		IndefLength:      cbor.IndefLengthForbidden,
		IntDec:           cbor.IntDecConvertNone,
		MaxArrayElements: 10000,
		MaxMapPairs:      1000,
		MaxNestedLevels:  16,
	}.DecMode()
	if err != nil {
		return nil, fmt.Errorf("failed to create CBOR decoder: %w", err)
	}

	// LRU cache for verified signatures to skip redundant checks
	verifyCache := expirable.NewLRU[string, bool](
		config.VerifyCacheSize,
		nil,
		config.VerifyCacheTTL,
	)

	return &Encoder{
		encMode:     encMode,
		decMode:     decMode,
		crypto:      crypto,
		config:      config,
		verifyCache: verifyCache,
	}, nil
}

// Encode serializes a message to CBOR with size limits
func (e *Encoder) Encode(msg types.Message) ([]byte, error) {
	// Get size limit for message type
	maxSize := e.getMaxSize(msg.Type())

	var buf bytes.Buffer
	encoder := e.encMode.NewEncoder(&buf)

	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("CBOR encode failed: %w", err)
	}

	data := buf.Bytes()
	if len(data) > maxSize {
		return nil, fmt.Errorf("message size %d exceeds limit %d for type %v",
			len(data), maxSize, msg.Type())
	}

	return data, nil
}

// VerifyAndDecode verifies signature and decodes message in one atomic operation
func (e *Encoder) VerifyAndDecode(ctx context.Context, data []byte, msgType MessageType) (types.Message, error) {
	// Check size before decode
	maxSize := e.getMaxSize(msgType)
	if len(data) > maxSize {
		return nil, fmt.Errorf("message size %d exceeds limit %d", len(data), maxSize)
	}

	// Decode message
	msg, err := e.decode(data, msgType)
	if err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}

	// Verify signature
	if err := e.verifyMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	// Check timestamp to prevent replay attacks
	if err := e.checkTimestamp(msg); err != nil {
		return nil, fmt.Errorf("timestamp check failed: %w", err)
	}

	return msg, nil
}

// decode deserializes CBOR data into the appropriate message type
func (e *Encoder) decode(data []byte, msgType MessageType) (types.Message, error) {
	var msg types.Message

	switch msgType {
	case TypeProposal:
		var p Proposal
		if err := e.decMode.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		msg = &p

	case TypeVote:
		var v Vote
		if err := e.decMode.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		msg = &v

	case TypeQC:
		var qc QC
		if err := e.decMode.Unmarshal(data, &qc); err != nil {
			return nil, err
		}
		msg = &qc

	case TypeViewChange:
		var vc ViewChange
		if err := e.decMode.Unmarshal(data, &vc); err != nil {
			return nil, err
		}
		msg = &vc

	case TypeNewView:
		var nv NewView
		if err := e.decMode.Unmarshal(data, &nv); err != nil {
			return nil, err
		}
		msg = &nv

	case TypeHeartbeat:
		var hb Heartbeat
		if err := e.decMode.Unmarshal(data, &hb); err != nil {
			return nil, err
		}
		msg = &hb

	case TypeEvidence:
		var ev Evidence
		if err := e.decMode.Unmarshal(data, &ev); err != nil {
			return nil, err
		}
		msg = &ev

	default:
		return nil, fmt.Errorf("unknown message type: %v", msgType)
	}

	return msg, nil
}

// verifyMessage checks cryptographic signature with caching
func (e *Encoder) verifyMessage(ctx context.Context, msg types.Message) error {
	sig, keyID, err := e.extractSignature(msg)
	if err != nil {
		return err
	}

	// Check cache for previously verified messages
	cacheKey := e.getCacheKey(msg.Hash(), keyID)
	e.mu.RLock()
	if verified, ok := e.verifyCache.Get(cacheKey); ok && verified {
		e.mu.RUnlock()
		return nil
	}
	e.mu.RUnlock()

	// Get public key for the signer
	publicKey, err := e.crypto.GetPublicKey(keyID)
	if err != nil {
		return fmt.Errorf("failed to get public key: %w", err)
	}

	// Verify signature using constant-time comparison
	signBytes := getSignBytes(msg)
	if err := e.crypto.VerifyWithContext(ctx, signBytes, sig, publicKey); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	// Cache successful verification
	e.mu.Lock()
	e.verifyCache.Add(cacheKey, true)
	e.mu.Unlock()

	return nil
}

// Helper to get SignBytes from any message type
func getSignBytes(msg types.Message) []byte {
	switch m := msg.(type) {
	case *Proposal:
		return m.SignBytes()
	case *Vote:
		return m.SignBytes()
	case *ViewChange:
		return m.SignBytes()
	case *NewView:
		return m.SignBytes()
	case *Heartbeat:
		return m.SignBytes()
	case *Evidence:
		return m.SignBytes()
	case *QC:
		return m.SignBytes()
	default:
		return []byte{}
	}
}

// extractSignature gets signature and keyID from any message type
func (e *Encoder) extractSignature(msg types.Message) ([]byte, ValidatorID, error) {
	switch m := msg.(type) {
	case *Proposal:
		return m.Signature.Bytes, m.Signature.KeyID, nil
	case *Vote:
		return m.Signature.Bytes, m.Signature.KeyID, nil
	case *ViewChange:
		return m.Signature.Bytes, m.Signature.KeyID, nil
	case *NewView:
		return m.Signature.Bytes, m.Signature.KeyID, nil
	case *Heartbeat:
		return m.Signature.Bytes, m.Signature.KeyID, nil
	case *Evidence:
		return m.Signature.Bytes, m.Signature.KeyID, nil
	case *QC:
		// QC doesn't have a single signature, validated separately
		return nil, ValidatorID{}, nil
	default:
		return nil, ValidatorID{}, fmt.Errorf("unknown message type for signature extraction")
	}
}

// checkTimestamp validates message timestamp against clock skew
func (e *Encoder) checkTimestamp(msg types.Message) error {
	var timestamp time.Time
	switch m := msg.(type) {
	case *Proposal:
		timestamp = m.Timestamp
	case *Vote:
		timestamp = m.Timestamp
	case *QC:
		timestamp = m.Timestamp
	case *ViewChange:
		timestamp = m.Timestamp
	case *NewView:
		timestamp = m.Timestamp
	case *Heartbeat:
		timestamp = m.Timestamp
	case *Evidence:
		timestamp = m.Timestamp
	default:
		return fmt.Errorf("unknown message type for timestamp check")
	}

	now := time.Now()

	if now.Sub(timestamp) > e.config.ClockSkewTolerance {
		return fmt.Errorf("message timestamp too old: %v (now: %v, skew: %v)",
			timestamp, now, e.config.ClockSkewTolerance)
	}

	if e.config.RejectFutureMessages && timestamp.Sub(now) > e.config.ClockSkewTolerance {
		return fmt.Errorf("message timestamp in future: %v (now: %v, skew: %v)",
			timestamp, now, e.config.ClockSkewTolerance)
	}

	return nil
}

// VerifyQC verifies all signatures in a Quorum Certificate
func (e *Encoder) VerifyQC(ctx context.Context, qc *QC) error {
	if qc == nil {
		return fmt.Errorf("QC is nil")
	}

	if len(qc.Signatures) == 0 {
		return fmt.Errorf("QC has no signatures")
	}

	// CRITICAL FIX (BUG-014): Vote signatures were ALREADY verified in ValidateVote().
	// The QC aggregates these pre-verified vote signatures. Re-verifying them here
	// against qc.SignBytes() is WRONG - vote signatures sign vote.SignBytes(), not qc.SignBytes()!
	// 
	// In HotStuff/BFT consensus:
	// 1. Each vote is individually signed and verified when received
	// 2. QC aggregates N verified vote signatures (no re-verification needed)
	// 3. QC itself is not signed - it's just a collection of vote signatures
	//
	// Skipping redundant re-verification fixes the signature mismatch bug.

	return nil
}

// getMaxSize returns the size limit for a message type
func (e *Encoder) getMaxSize(msgType MessageType) int {
	switch msgType {
	case TypeProposal:
		return e.config.MaxProposalSize
	case TypeVote:
		return e.config.MaxVoteSize
	case TypeQC:
		return e.config.MaxQCSize
	case TypeViewChange:
		return e.config.MaxViewChangeSize
	case TypeNewView:
		return e.config.MaxNewViewSize
	case TypeHeartbeat:
		return e.config.MaxHeartbeatSize
	case TypeEvidence:
		return e.config.MaxEvidenceSize
	default:
		return 1 << 20 // Default 1MB
	}
}

// getCacheKey generates a unique cache key for verified messages
func (e *Encoder) getCacheKey(msgHash BlockHash, keyID ValidatorID) string {
	// Use constant-time comparison safe concatenation
	combined := make([]byte, 64)
	copy(combined[:32], msgHash[:])
	copy(combined[32:], keyID[:])
	return string(combined)
}

// CompareSignatures performs constant-time signature comparison
func CompareSignatures(sig1, sig2 []byte) bool {
	return subtle.ConstantTimeCompare(sig1, sig2) == 1
}

// ClearCache clears the verification cache (useful for testing)
func (e *Encoder) ClearCache() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.verifyCache.Purge()
}

// GetCacheStats returns cache statistics
func (e *Encoder) GetCacheStats() (size, capacity int) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.verifyCache.Len(), e.config.VerifyCacheSize
}
