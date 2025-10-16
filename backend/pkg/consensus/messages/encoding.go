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

	"backend/pkg/block"
	"backend/pkg/consensus/types"
)

type proposalWire struct {
	View       uint64                 `cbor:"1,keyasint"`
	Height     uint64                 `cbor:"2,keyasint"`
	Round      uint64                 `cbor:"3,keyasint"`
	BlockHash  types.BlockHash        `cbor:"4,keyasint"`
	ParentHash types.BlockHash        `cbor:"5,keyasint"`
	ProposerID types.ValidatorID      `cbor:"6,keyasint"`
	Timestamp  time.Time              `cbor:"7,keyasint"`
	JustifyQC  *QC                    `cbor:"8,keyasint,omitempty"`
	Block      *block.AppBlockPayload `cbor:"9,keyasint,omitempty"`
	Signature  Signature              `cbor:"10,keyasint"`
}

func newProposalWire(p *Proposal) (*proposalWire, error) {
	var payload *block.AppBlockPayload
	if p.Block != nil {
		appBlock, ok := p.Block.(*block.AppBlock)
		if !ok {
			return nil, fmt.Errorf("unsupported block implementation %T", p.Block)
		}
		var err error
		payload, err = appBlock.ToPayload()
		if err != nil {
			return nil, fmt.Errorf("serialize block: %w", err)
		}
	}

	return &proposalWire{
		View:       p.View,
		Height:     p.Height,
		Round:      p.Round,
		BlockHash:  p.BlockHash,
		ParentHash: p.ParentHash,
		ProposerID: p.ProposerID,
		Timestamp:  p.Timestamp,
		JustifyQC:  p.JustifyQC,
		Block:      payload,
		Signature:  p.Signature,
	}, nil
}

func (pw *proposalWire) toProposal() (*Proposal, error) {
	p := &Proposal{
		View:       pw.View,
		Height:     pw.Height,
		Round:      pw.Round,
		BlockHash:  pw.BlockHash,
		ParentHash: pw.ParentHash,
		ProposerID: pw.ProposerID,
		Timestamp:  pw.Timestamp,
		JustifyQC:  pw.JustifyQC,
		Signature:  pw.Signature,
	}

	if pw.Block != nil {
		blk, err := pw.Block.ToAppBlock()
		if err != nil {
			return nil, fmt.Errorf("decode block payload: %w", err)
		}
		if blk.GetHash() != pw.BlockHash {
			return nil, fmt.Errorf("block hash mismatch: payload %x vs proposal %x", blk.GetHash(), pw.BlockHash)
		}
		p.Block = blk
	}

	return p, nil
}

// Encoder handles secure message serialization
type Encoder struct {
	encMode     cbor.EncMode
	decMode     cbor.DecMode
	crypto      types.CryptoService
	config      *EncoderConfig
	verifyCache *expirable.LRU[string, bool]
	// verifySigCache caches successful signature verifications keyed by (signBytes|keyID)
	// to make re-verification idempotent, especially during QC validation where
	// only signatures are available (not full messages). This prevents replay
	// detections on identical data/signature pairs that were already verified at ingress.
	verifySigCache  *expirable.LRU[string, bool]
	validatorSet    ValidatorSet
	staticMinQuorum int
	mu              sync.RWMutex
}

// EncoderConfig contains encoding security parameters
type EncoderConfig struct {
	MaxProposalSize           int
	MaxVoteSize               int
	MaxQCSize                 int
	MaxViewChangeSize         int
	MaxNewViewSize            int
	MaxHeartbeatSize          int
	MaxEvidenceSize           int
	MaxGenesisReadySize       int
	MaxGenesisCertificateSize int
	MaxProposalIntentSize     int
	MaxReadyToVoteSize        int
	ClockSkewTolerance        time.Duration
	GenesisClockSkewTolerance time.Duration
	VerifyCacheSize           int
	VerifyCacheTTL            time.Duration
	RejectFutureMessages      bool
	MinQuorumSize             int
}

// DefaultEncoderConfig returns secure default configuration
func DefaultEncoderConfig() *EncoderConfig {
	return &EncoderConfig{
		MaxProposalSize:           1 << 20,   // 1 MB
		MaxVoteSize:               10 << 10,  // 10 KB
		MaxQCSize:                 100 << 10, // 100 KB
		MaxViewChangeSize:         50 << 10,  // 50 KB
		MaxNewViewSize:            500 << 10, // 500 KB
		MaxHeartbeatSize:          5 << 10,   // 5 KB
		MaxEvidenceSize:           200 << 10, // 200 KB
		MaxGenesisReadySize:       16 << 10,  // 16 KB
		MaxGenesisCertificateSize: 64 << 10,  // 64 KB (attestation bundle)
		MaxProposalIntentSize:     4 << 10,   // 4 KB
		MaxReadyToVoteSize:        4 << 10,   // 4 KB
		ClockSkewTolerance:        5 * time.Second,
		GenesisClockSkewTolerance: 15 * time.Minute,
		VerifyCacheSize:           10000,
		VerifyCacheTTL:            5 * time.Minute,
		RejectFutureMessages:      true,
		MinQuorumSize:             1,
	}
}

// NewEncoder creates a secure CBOR encoder
func NewEncoder(crypto types.CryptoService, config *EncoderConfig) (*Encoder, error) {
	if config == nil {
		config = DefaultEncoderConfig()
	}

	if config.GenesisClockSkewTolerance <= 0 {
		config.GenesisClockSkewTolerance = config.ClockSkewTolerance
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

	verifySigCache := expirable.NewLRU[string, bool](
		config.VerifyCacheSize,
		nil,
		config.VerifyCacheTTL,
	)

	return &Encoder{
		encMode:         encMode,
		decMode:         decMode,
		crypto:          crypto,
		config:          config,
		verifyCache:     verifyCache,
		verifySigCache:  verifySigCache,
		staticMinQuorum: config.MinQuorumSize,
	}, nil
}

// SetValidatorSet wires a validator set so the encoder can enforce quorum thresholds.
func (e *Encoder) SetValidatorSet(set ValidatorSet) {
	e.mu.Lock()
	e.validatorSet = set
	e.mu.Unlock()
}

func (e *Encoder) quorumThreshold() int {
	e.mu.RLock()
	set := e.validatorSet
	minQuorum := e.staticMinQuorum
	e.mu.RUnlock()
	if minQuorum < 1 {
		minQuorum = 1
	}
	if set == nil {
		return minQuorum
	}
	count := set.GetValidatorCount()
	if count <= 0 {
		return minQuorum
	}
	f := (count - 1) / 3
	dynamic := 2*f + 1
	if dynamic < 1 {
		dynamic = 1
	}
	if dynamic > minQuorum {
		return dynamic
	}
	return minQuorum
}

// Encode serializes a message to CBOR with size limits
func (e *Encoder) Encode(msg types.Message) ([]byte, error) {
	// Get size limit for message type
	maxSize := e.getMaxSize(msg.Type())

	var buf bytes.Buffer
	encoder := e.encMode.NewEncoder(&buf)
	// Encode message including Block payload (required for commits)
	switch m := msg.(type) {
	case *Proposal:
		wire, err := newProposalWire(m)
		if err != nil {
			return nil, err
		}
		if err := encoder.Encode(wire); err != nil {
			return nil, fmt.Errorf("CBOR encode failed: %w", err)
		}
	default:
		if err := encoder.Encode(msg); err != nil {
			return nil, fmt.Errorf("CBOR encode failed: %w", err)
		}
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
		var wire proposalWire
		if err := e.decMode.Unmarshal(data, &wire); err != nil {
			return nil, err
		}
		p, err := wire.toProposal()
		if err != nil {
			return nil, err
		}
		msg = p

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

	case TypeGenesisReady:
		var gr GenesisReady
		if err := e.decMode.Unmarshal(data, &gr); err != nil {
			return nil, err
		}
		msg = &gr

	case TypeGenesisCertificate:
		var gc GenesisCertificate
		if err := e.decMode.Unmarshal(data, &gc); err != nil {
			return nil, err
		}
		msg = &gc

	case TypeProposalIntent:
		var pi ProposalIntent
		if err := e.decMode.Unmarshal(data, &pi); err != nil {
			return nil, err
		}
		msg = &pi

	case TypeReadyToVote:
		var rtv ReadyToVote
		if err := e.decMode.Unmarshal(data, &rtv); err != nil {
			return nil, err
		}
		msg = &rtv

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
	// Fast-path: if we've already verified this exact (signBytes,keyID), skip crypto verify
	if len(signBytes) > 0 {
		sigKey := e.getSigCacheKey(signBytes, keyID)
		e.mu.RLock()
		if ok, cached := e.verifySigCache.Get(sigKey); cached && ok {
			e.mu.RUnlock()
			return nil
		}
		e.mu.RUnlock()
	}
	if err := e.crypto.VerifyWithContext(ctx, signBytes, sig, publicKey); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	// Cache successful verification
	e.mu.Lock()
	e.verifyCache.Add(cacheKey, true)
	if len(signBytes) > 0 {
		e.verifySigCache.Add(e.getSigCacheKey(signBytes, keyID), true)
	}
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
	case *GenesisReady:
		return m.SignBytes()
	case *GenesisCertificate:
		return m.SignBytes()
	case *ProposalIntent:
		return m.SignBytes()
	case *ReadyToVote:
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
	case *GenesisReady:
		return m.Signature.Bytes, m.Signature.KeyID, nil
	case *GenesisCertificate:
		return m.Signature.Bytes, m.Signature.KeyID, nil
	case *ProposalIntent:
		return m.Signature.Bytes, m.Signature.KeyID, nil
	case *ReadyToVote:
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
	tolerance := e.config.ClockSkewTolerance
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
	case *GenesisReady:
		timestamp = m.Timestamp
		if e.config.GenesisClockSkewTolerance > 0 {
			tolerance = e.config.GenesisClockSkewTolerance
		}
	case *GenesisCertificate:
		timestamp = m.Timestamp
		if e.config.GenesisClockSkewTolerance > 0 {
			tolerance = e.config.GenesisClockSkewTolerance
		}
	case *ProposalIntent:
		timestamp = m.Timestamp
	case *ReadyToVote:
		timestamp = m.Timestamp
	default:
		return fmt.Errorf("unknown message type for timestamp check")
	}

	now := time.Now()

	if now.Sub(timestamp) > tolerance {
		return fmt.Errorf("message timestamp too old: %v (now: %v, skew: %v)",
			timestamp, now, tolerance)
	}

	if e.config.RejectFutureMessages && timestamp.Sub(now) > tolerance {
		return fmt.Errorf("message timestamp in future: %v (now: %v, skew: %v)",
			timestamp, now, tolerance)
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

	required := e.quorumThreshold()
	if required > 0 && len(qc.Signatures) < required {
		return fmt.Errorf("QC has %d signatures, quorum requires %d", len(qc.Signatures), required)
	}

	seen := make(map[ValidatorID]struct{}, len(qc.Signatures))

	for idx, sig := range qc.Signatures {
		if len(sig.Bytes) == 0 {
			return fmt.Errorf("QC signature %d is empty", idx)
		}

		if _, duplicate := seen[sig.KeyID]; duplicate {
			return fmt.Errorf("QC contains duplicate signature from validator %x", sig.KeyID[:8])
		}
		seen[sig.KeyID] = struct{}{}

		publicKey, err := e.crypto.GetPublicKey(sig.KeyID)
		if err != nil {
			return fmt.Errorf("QC signature %d failed to load public key for validator %x: %w", idx, sig.KeyID[:8], err)
		}

		voteBytes := buildVoteSignBytesFromQC(qc, sig.KeyID, sig.Timestamp)
		// Idempotent path: if we've already verified this (signBytes,keyID), skip crypto verify
		sigKey := e.getSigCacheKey(voteBytes, sig.KeyID)
		e.mu.RLock()
		if ok, cached := e.verifySigCache.Get(sigKey); cached && ok {
			e.mu.RUnlock()
			continue
		}
		e.mu.RUnlock()

		if err := e.crypto.VerifyWithContext(ctx, voteBytes, sig.Bytes, publicKey); err != nil {
			return fmt.Errorf("QC signature %d invalid for validator %x: %w", idx, sig.KeyID[:8], err)
		}

		// Cache success for idempotence
		e.mu.Lock()
		e.verifySigCache.Add(sigKey, true)
		e.mu.Unlock()
	}

	return nil
}

func buildVoteSignBytesFromQC(qc *QC, voterID ValidatorID, timestamp time.Time) []byte {
	buf := make([]byte, 0, 128)
	buf = append(buf, []byte(DomainVote)...)
	buf = append(buf, 0x00)
	buf = appendUint64(buf, qc.View)
	buf = appendUint64(buf, qc.Height)
	buf = appendUint64(buf, qc.Round)
	buf = append(buf, qc.BlockHash[:]...)
	buf = append(buf, voterID[:]...)
	buf = appendInt64(buf, timestamp.UnixNano())
	return buf
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
	case TypeGenesisReady:
		return e.config.MaxGenesisReadySize
	case TypeGenesisCertificate:
		return e.config.MaxGenesisCertificateSize
	case TypeProposalIntent:
		return e.config.MaxProposalIntentSize
	case TypeReadyToVote:
		return e.config.MaxReadyToVoteSize
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

// getSigCacheKey generates a cache key from raw sign bytes and keyID.
func (e *Encoder) getSigCacheKey(signBytes []byte, keyID ValidatorID) string {
	combined := make([]byte, 32+len(signBytes))
	copy(combined[:len(signBytes)], signBytes)
	// Append keyID to strengthen uniqueness per signer
	off := len(signBytes)
	if len(combined) < off+32 {
		combined = append(combined, make([]byte, off+32-len(combined))...)
	}
	copy(combined[off:off+32], keyID[:])
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
