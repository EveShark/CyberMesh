package policy

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const FastMitigationDomain = "control.fast_mitigation.v1"
const fastMitigationMaxTTLSeconds = 60

type FastMitigationEnvelope struct {
	SchemaVersion string          `json:"schema_version"`
	MitigationID  string          `json:"mitigation_id"`
	PolicyID      string          `json:"policy_id"`
	Action        string          `json:"action"`
	RuleType      string          `json:"rule_type"`
	Timestamp     int64           `json:"timestamp"`
	Payload       json.RawMessage `json:"payload"`
	ContentHash   string          `json:"content_hash"`
	Nonce         string          `json:"nonce"`
	Signature     string          `json:"signature"`
	Pubkey        string          `json:"pubkey"`
	ProducerID    string          `json:"producer_id"`
	Alg           string          `json:"alg"`
}

func (t *TrustedKeys) VerifyAndParseFast(raw []byte) (Event, error) {
	if t == nil {
		return Event{}, errors.New("policy: trusted store nil")
	}
	var env FastMitigationEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Event{}, fmt.Errorf("policy: decode fast mitigation: %w", err)
	}
	if env.SchemaVersion != FastMitigationDomain {
		return Event{}, fmt.Errorf("policy: unexpected fast mitigation schema_version %q", env.SchemaVersion)
	}
	if env.Alg != "Ed25519" {
		return Event{}, fmt.Errorf("policy: unsupported fast mitigation alg %q", env.Alg)
	}
	if env.MitigationID == "" || env.PolicyID == "" || env.Action == "" || env.RuleType == "" {
		return Event{}, errors.New("policy: fast mitigation missing required fields")
	}
	pubBytes, err := hex.DecodeString(env.Pubkey)
	if err != nil {
		return Event{}, fmt.Errorf("policy: decode fast mitigation pubkey: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return Event{}, errors.New("policy: fast mitigation pubkey size invalid")
	}
	pub, ok := t.keys[string(pubBytes)]
	if !ok {
		return Event{}, errors.New("policy: unknown fast mitigation signer")
	}
	if env.ProducerID == "" {
		return Event{}, errors.New("policy: fast mitigation producer_id required")
	}
	if env.ProducerID != env.Pubkey {
		return Event{}, errors.New("policy: fast mitigation producer_id mismatch")
	}
	sigBytes, err := hex.DecodeString(env.Signature)
	if err != nil {
		return Event{}, fmt.Errorf("policy: decode fast mitigation signature: %w", err)
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return Event{}, errors.New("policy: fast mitigation signature size invalid")
	}
	contentHashBytes, err := hex.DecodeString(env.ContentHash)
	if err != nil {
		return Event{}, fmt.Errorf("policy: decode fast mitigation content hash: %w", err)
	}
	if len(contentHashBytes) != sha256.Size {
		return Event{}, errors.New("policy: fast mitigation content hash size invalid")
	}
	nonceBytes, err := hex.DecodeString(env.Nonce)
	if err != nil {
		return Event{}, fmt.Errorf("policy: decode fast mitigation nonce: %w", err)
	}
	if len(nonceBytes) != 16 {
		return Event{}, errors.New("policy: fast mitigation nonce size invalid")
	}
	if env.Timestamp <= 0 {
		return Event{}, errors.New("policy: fast mitigation timestamp invalid")
	}
	payloadBytes, err := canonicalizeFastPayload(env.Payload)
	if err != nil {
		return Event{}, err
	}
	sum := sha256.Sum256(payloadBytes)
	if !equalBytes(sum[:], contentHashBytes) {
		return Event{}, errors.New("policy: fast mitigation content hash mismatch")
	}
	signBytes := buildFastSignBytes(
		env.MitigationID,
		env.PolicyID,
		env.Action,
		env.RuleType,
		env.Timestamp,
		pubBytes,
		nonceBytes,
		contentHashBytes,
	)
	msg := append([]byte(FastMitigationDomain), signBytes...)
	if !ed25519.Verify(pub, msg, sigBytes) {
		return Event{}, errors.New("policy: fast mitigation signature verification failed")
	}

	spec, err := ParseSpec(env.RuleType, payloadBytes, 0, 0, env.Timestamp)
	if err != nil {
		return Event{}, err
	}
	if spec.ID != env.PolicyID {
		return Event{}, errors.New("policy: fast mitigation policy_id mismatch")
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err == nil {
		spec.Raw = payload
	}
	spec.Timestamp = time.Unix(env.Timestamp, 0).UTC()
	if err := validateFastPayloadAndSpec(payload, spec, env.Action); err != nil {
		return Event{}, err
	}

	fast := &FastMitigationEnvelope{
		SchemaVersion: env.SchemaVersion,
		MitigationID:  env.MitigationID,
		PolicyID:      env.PolicyID,
		Action:        env.Action,
		RuleType:      env.RuleType,
		Timestamp:     env.Timestamp,
		Payload:       append([]byte(nil), payloadBytes...),
		ContentHash:   env.ContentHash,
		Nonce:         env.Nonce,
		Signature:     env.Signature,
		Pubkey:        env.Pubkey,
		ProducerID:    env.ProducerID,
		Alg:           env.Alg,
	}
	return Event{Fast: fast, Spec: spec}, nil
}

func validateFastPayloadAndSpec(payload map[string]any, spec PolicySpec, action string) error {
	if strings.ToLower(strings.TrimSpace(spec.RuleType)) != "block" {
		return errors.New("policy: fast mitigation rule_type must be block")
	}
	normalizedAction := strings.ToLower(strings.TrimSpace(action))
	if normalizedAction != "drop" && normalizedAction != "rate_limit" {
		return fmt.Errorf("policy: fast mitigation action %q not allowed", action)
	}
	if strings.ToLower(strings.TrimSpace(spec.Action)) != normalizedAction {
		return errors.New("policy: fast mitigation action mismatch")
	}

	metadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		return errors.New("policy: fast mitigation metadata missing")
	}
	if traceID, ok := metadata["trace_id"].(string); !ok || strings.TrimSpace(traceID) == "" {
		return errors.New("policy: fast mitigation metadata.trace_id required")
	}
	if anomalyID, ok := metadata["anomaly_id"].(string); !ok || strings.TrimSpace(anomalyID) == "" {
		return errors.New("policy: fast mitigation metadata.anomaly_id required")
	}
	for _, key := range []string{"source_event_id", "sentinel_event_id"} {
		if value, exists := metadata[key]; exists && value != nil {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("policy: fast mitigation metadata.%s must be string", key)
			}
		}
	}

	scope := strings.ToLower(strings.TrimSpace(spec.Target.Scope))
	if scope != "cluster" && scope != "namespace" {
		return fmt.Errorf("policy: fast mitigation scope %q not allowed", spec.Target.Scope)
	}
	if strings.TrimSpace(spec.Target.Tenant) != "" || strings.TrimSpace(spec.Tenant) != "" {
		return errors.New("policy: fast mitigation tenant targeting not allowed")
	}
	if strings.TrimSpace(spec.Target.Region) != "" || strings.TrimSpace(spec.Region) != "" {
		return errors.New("policy: fast mitigation region targeting not allowed")
	}
	if len(spec.Target.IPs) == 0 && len(spec.Target.CIDRs) == 0 {
		return errors.New("policy: fast mitigation requires ips or cidrs")
	}
	for key := range spec.Target.Selectors {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey != "namespace" && normalizedKey != "kubernetes_namespace" {
			return fmt.Errorf("policy: fast mitigation selector %q not allowed", key)
		}
	}
	if scope != "namespace" && len(spec.Target.Selectors) > 0 {
		return errors.New("policy: fast mitigation selectors only allowed for namespace scope")
	}
	if spec.Guardrails.TTLSeconds <= 0 || spec.Guardrails.TTLSeconds > fastMitigationMaxTTLSeconds {
		return fmt.Errorf("policy: fast mitigation ttl_seconds must be between 1 and %d", fastMitigationMaxTTLSeconds)
	}
	return nil
}

func canonicalizeFastPayload(raw json.RawMessage) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("policy: decode fast mitigation payload: %w", err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("policy: encode fast mitigation payload: %w", err)
	}
	return encoded, nil
}

func buildFastSignBytes(mitigationID, policyID, action, ruleType string, timestamp int64, pubkey, nonce, contentHash []byte) []byte {
	mitigationIDBytes := []byte(mitigationID)
	policyIDBytes := []byte(policyID)
	actionBytes := []byte(action)
	ruleTypeBytes := []byte(ruleType)
	out := make([]byte, 0, 1+2+len(mitigationIDBytes)+2+len(policyIDBytes)+2+len(actionBytes)+2+len(ruleTypeBytes)+8+2+len(pubkey)+len(nonce)+len(contentHash))
	out = append(out, 0x01)
	out = appendU16(out, len(mitigationIDBytes))
	out = append(out, mitigationIDBytes...)
	out = appendU16(out, len(policyIDBytes))
	out = append(out, policyIDBytes...)
	out = appendU16(out, len(actionBytes))
	out = append(out, actionBytes...)
	out = appendU16(out, len(ruleTypeBytes))
	out = append(out, ruleTypeBytes...)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(timestamp))
	out = append(out, ts[:]...)
	out = appendU16(out, len(pubkey))
	out = append(out, pubkey...)
	out = append(out, nonce...)
	out = append(out, contentHash...)
	return out
}

func appendU16(dst []byte, v int) []byte {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], uint16(v))
	return append(dst, buf[:]...)
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
