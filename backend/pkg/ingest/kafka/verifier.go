package kafka

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
	MaxTimestampSkew      time.Duration       // Max clock skew allowed (default: 5m)
	PolicyPubKeyAllowlist map[string]struct{} // Hex-encoded Ed25519 pubkeys allowed to publish policies
	PolicyRequireTrace    bool                // Require trace contract fields in ai.policy payload
	PolicyRequireQCRef    bool                // Require an approval reference or explicit trace contract in ai.policy payload
	PolicyTraceFutureSkew time.Duration       // Max future skew for ai_event_ts_ms
}

// DefaultVerifierConfig returns default verification configuration
func DefaultVerifierConfig() VerifierConfig {
	return VerifierConfig{
		MaxTimestampSkew:      5 * time.Minute,
		PolicyPubKeyAllowlist: nil,
		PolicyRequireTrace:    false,
		PolicyRequireQCRef:    false,
		PolicyTraceFutureSkew: 5 * time.Minute,
	}
}

// VerifyAnomalyMsg verifies signature, content hash, and timestamp of AnomalyMsg
func VerifyAnomalyMsg(msg *AnomalyMsg, cfg VerifierConfig, log *utils.Logger) (*state.EventTx, error) {
	if log != nil {
		log.Info("[DEBUG] VerifyAnomalyMsg() ENTRY")
	}

	// Validate message structure
	if log != nil {
		log.Info("[DEBUG] Calling ValidateAnomalyMsg()")
	}
	if err := ValidateAnomalyMsg(msg); err != nil {
		if log != nil {
			log.Info("[DEBUG] ValidateAnomalyMsg() FAILED",
				utils.ZapError(err))
		}
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	if log != nil {
		log.Info("[DEBUG] ValidateAnomalyMsg() SUCCESS")
	}

	// Verify timestamp skew
	if log != nil {
		log.Info("[DEBUG] Verifying timestamp skew")
	}
	now := time.Now().Unix()
	skewSeconds := int64(cfg.MaxTimestampSkew.Seconds())
	if msg.TS > now+skewSeconds || msg.TS < now-skewSeconds {
		if log != nil {
			log.Info("[DEBUG] Timestamp skew FAILED",
				utils.ZapInt64("msg_ts", msg.TS),
				utils.ZapInt64("now", now),
				utils.ZapInt64("skew_seconds", skewSeconds))
		}
		return nil, fmt.Errorf("timestamp skew exceeded: %d (now: %d, skew: %d)", msg.TS, now, skewSeconds)
	}
	if log != nil {
		log.Info("[DEBUG] Timestamp skew OK")
	}

	// Check payload size BEFORE hashing (DoS protection)
	if log != nil {
		log.Info("[DEBUG] Checking payload size",
			utils.ZapInt("payload_bytes", len(msg.Payload)),
			utils.ZapInt("max_allowed", MaxPayloadSize))
	}
	if len(msg.Payload) > MaxPayloadSize {
		if log != nil {
			log.Info("[DEBUG] Payload size check FAILED")
		}
		return nil, fmt.Errorf("payload too large: %d bytes (max: %d)", len(msg.Payload), MaxPayloadSize)
	}
	if log != nil {
		log.Info("[DEBUG] Payload size OK")
	}

	// Verify ContentHash = SHA256(payload) - mempool enforces this
	if log != nil {
		log.Info("[DEBUG] Verifying content hash")
	}
	actualContentHash := sha256.Sum256(msg.Payload)
	if msg.ContentHash != actualContentHash {
		if log != nil {
			log.Info("[DEBUG] Content hash mismatch")
		}
		return nil, fmt.Errorf("content hash mismatch: expected %x, got %x", msg.ContentHash[:8], actualContentHash[:8])
	}
	if log != nil {
		log.Info("[DEBUG] Content hash OK")
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
	if log != nil {
		log.Info("[DEBUG] Verifying Ed25519 signature",
			utils.ZapInt("pubkey_bytes", len(msg.PubKey)),
			utils.ZapInt("signature_bytes", len(msg.Signature)),
			utils.ZapInt("sign_bytes", len(signBytes)))
	}
	if !ed25519.Verify(msg.PubKey, signBytes, msg.Signature) {
		if log != nil {
			log.Info("[DEBUG] Ed25519 signature verification FAILED")
		}
		return nil, fmt.Errorf("signature verification failed")
	}

	if !bytes.Equal(msg.ProducerID, msg.PubKey) {
		return nil, fmt.Errorf("producer/pubkey mismatch")
	}
	if log != nil {
		log.Info("[DEBUG] Ed25519 signature verification SUCCESS")
	}

	if log != nil {
		log.Info("[DEBUG] Extracting priority metadata from payload")
	}
	severityFromPayload, confidenceFromPayload, err := extractPriorityMetadata(msg.Payload)
	if err != nil {
		if log != nil {
			log.Info("[DEBUG] Priority metadata extraction FAILED",
				utils.ZapError(err))
		}
		return nil, fmt.Errorf("priority metadata validation failed: %w", err)
	}
	if log != nil {
		log.Info("[DEBUG] Priority metadata extraction SUCCESS")
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

	if log != nil {
		log.Info("[DEBUG] Creating state.EventTx")
	}

	// Preserve the original payload bytes validated above so the envelope
	// content hash remains intact for downstream state validation.
	commitPayload := append([]byte(nil), msg.Payload...)

	// Convert to state.EventTx
	tx := &state.EventTx{
		Ts:   msg.TS,
		Data: commitPayload,
		Env: state.Envelope{
			ProducerID:  msg.ProducerID,
			Nonce:       msg.Nonce,
			Signature:   msg.Signature,
			PubKey:      msg.PubKey,
			Alg:         msg.Alg,
			ContentHash: msg.ContentHash,
		},
	}

	if log != nil {
		log.Info("[DEBUG] VerifyAnomalyMsg() EXIT - SUCCESS")
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

	// Build canonical sign bytes consistent with AI Signer.sign(domain + data)
	// Layout: domainEvidence || ts(8B BE) || pid_len(2B BE) || producer_id || nonce(16B) || content_hash(32B)
	payloadBytes := make([]byte, 0, 8+2+len(msg.ProducerID)+16+32)

	var tsb [8]byte
	binary.BigEndian.PutUint64(tsb[:], uint64(msg.TS))
	payloadBytes = append(payloadBytes, tsb[:]...)

	var pidLen [2]byte
	binary.BigEndian.PutUint16(pidLen[:], uint16(len(msg.ProducerID)))
	payloadBytes = append(payloadBytes, pidLen[:]...)
	payloadBytes = append(payloadBytes, msg.ProducerID...)

	payloadBytes = append(payloadBytes, msg.Nonce...)
	payloadBytes = append(payloadBytes, msg.ContentHash[:]...)

	signBytes := append([]byte(domainEvidence), payloadBytes...)

	// Verify Ed25519 signature
	if !ed25519.Verify(msg.PubKey, signBytes, msg.Signature) {
		return nil, fmt.Errorf("signature verification failed")
	}

	if !bytes.Equal(msg.ProducerID, msg.PubKey) {
		return nil, fmt.Errorf("producer/pubkey mismatch")
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
	policyID, traceID := extractPolicyStageIdentityFromParams(msg.Params)
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

	if !bytes.Equal(msg.ProducerID, msg.PubKey) {
		return nil, fmt.Errorf("producer/pubkey mismatch")
	}
	if log != nil && policyID != "" {
		log.Info("policy stage marker",
			utils.ZapString("stage", "t_signature_domain_ok"),
			utils.ZapString("policy_id", policyID),
			utils.ZapString("trace_id", traceID),
			utils.ZapInt64("t_ms", time.Now().UnixMilli()))
	}

	if len(cfg.PolicyPubKeyAllowlist) > 0 {
		keyHex := hex.EncodeToString(msg.PubKey)
		if _, ok := cfg.PolicyPubKeyAllowlist[keyHex]; !ok {
			return nil, fmt.Errorf("policy producer %s not allowlisted", keyHex)
		}
	}
	if err := validatePolicyTraceContract(msg.Params, cfg); err != nil {
		return nil, fmt.Errorf("policy trace contract invalid: %w", err)
	}
	if log != nil && policyID != "" {
		log.Info("policy stage marker",
			utils.ZapString("stage", "t_trace_contract_ok"),
			utils.ZapString("policy_id", policyID),
			utils.ZapString("trace_id", traceID),
			utils.ZapInt64("t_ms", time.Now().UnixMilli()))
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

func extractPolicyStageIdentityFromParams(params []byte) (string, string) {
	if len(params) == 0 {
		return "", ""
	}
	var payload map[string]any
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", ""
	}
	get := func(m map[string]any, key string) string {
		if m == nil {
			return ""
		}
		if v, ok := m[key].(string); ok {
			return v
		}
		return ""
	}
	policyID := get(payload, "policy_id")
	traceID := get(payload, "trace_id")
	if meta, ok := payload["metadata"].(map[string]any); ok && traceID == "" {
		traceID = get(meta, "trace_id")
	}
	if tr, ok := payload["trace"].(map[string]any); ok && traceID == "" {
		traceID = get(tr, "id")
	}
	if nested, ok := payload["params"].(map[string]any); ok {
		if policyID == "" {
			policyID = get(nested, "policy_id")
		}
		if traceID == "" {
			traceID = get(nested, "trace_id")
		}
		if meta, ok := nested["metadata"].(map[string]any); ok && traceID == "" {
			traceID = get(meta, "trace_id")
		}
		if tr, ok := nested["trace"].(map[string]any); ok && traceID == "" {
			traceID = get(tr, "id")
		}
	}
	return policyID, traceID
}

func validatePolicyTraceContract(params []byte, cfg VerifierConfig) error {
	if !cfg.PolicyRequireTrace && !cfg.PolicyRequireQCRef {
		return nil
	}

	var payload map[string]any
	if err := json.Unmarshal(params, &payload); err != nil {
		return fmt.Errorf("params json decode failed: %w", err)
	}

	var (
		qcReference string
		traceID     string
		traceIDAlt  string
		aiEventTSMs int64
		traceTSMs   int64
	)
	extractNum := func(v any) int64 {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		default:
			return 0
		}
	}
	parseFrom := func(src map[string]any) {
		if src == nil {
			return
		}
		if rawQC, ok := src["qc_reference"].(string); ok && qcReference == "" {
			qcReference = rawQC
		}
		if metadata, ok := src["metadata"].(map[string]any); ok {
			if v, ok := metadata["trace_id"].(string); ok && traceID == "" {
				traceID = v
			}
			if aiEventTSMs == 0 {
				aiEventTSMs = extractNum(metadata["ai_event_ts_ms"])
			}
			if aiEventTSMs == 0 {
				aiEventTSMs = extractNum(metadata["ai_event_timestamp_ms"])
			}
		}
		if trace, ok := src["trace"].(map[string]any); ok {
			if v, ok := trace["id"].(string); ok && traceIDAlt == "" {
				traceIDAlt = v
			}
			if traceTSMs == 0 {
				traceTSMs = extractNum(trace["ai_event_ts_ms"])
			}
		}
		if v, ok := src["trace_id"].(string); ok && traceID == "" {
			traceID = v
		}
	}

	parseFrom(payload)
	if wrapped, ok := payload["params"].(map[string]any); ok {
		parseFrom(wrapped)
	}

	if cfg.PolicyRequireTrace {
		if traceID == "" && traceIDAlt == "" {
			return fmt.Errorf("missing trace_id")
		}
		if aiEventTSMs == 0 && traceTSMs == 0 {
			return fmt.Errorf("missing ai_event_ts_ms")
		}
	}
	if traceID != "" && traceIDAlt != "" && traceID != traceIDAlt {
		return fmt.Errorf("trace id mismatch between metadata.trace_id and trace.id")
	}
	if cfg.PolicyRequireQCRef && qcReference == "" {
		activeTraceID := traceID
		if activeTraceID == "" {
			activeTraceID = traceIDAlt
		}
		if activeTraceID == "" {
			return fmt.Errorf("missing qc_reference")
		}
	}

	effectiveEventTS := aiEventTSMs
	if effectiveEventTS == 0 {
		effectiveEventTS = traceTSMs
	}
	if effectiveEventTS > 0 {
		normalizedTS, _, valid := utils.NormalizeUnixMillis(effectiveEventTS)
		if !valid {
			return fmt.Errorf("ai_event_ts_ms invalid or out of allowed range")
		}
		effectiveEventTS = normalizedTS
		maxFutureSkew := cfg.PolicyTraceFutureSkew
		if maxFutureSkew <= 0 {
			maxFutureSkew = cfg.MaxTimestampSkew
		}
		if maxFutureSkew <= 0 {
			maxFutureSkew = 5 * time.Minute
		}
		if effectiveEventTS > time.Now().Add(maxFutureSkew).UnixMilli() {
			return fmt.Errorf("ai_event_ts_ms exceeds allowed future skew")
		}
	}

	return nil
}
