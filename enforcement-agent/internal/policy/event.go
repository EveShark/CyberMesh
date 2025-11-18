package policy

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	pb "backend/proto"
	"google.golang.org/protobuf/proto"
)

// Event wraps a protobuf PolicyUpdateEvent plus computed spec.
type Event struct {
	Proto *pb.PolicyUpdateEvent
	Spec  PolicySpec
}

// TrustedKeys stores validator public keys keyed by raw bytes.
type TrustedKeys struct {
	keys map[string]ed25519.PublicKey
}

// NewTrustedKeys constructs an in-memory trust store from provided public keys.
func NewTrustedKeys(keys ...ed25519.PublicKey) *TrustedKeys {
	store := &TrustedKeys{keys: make(map[string]ed25519.PublicKey, len(keys))}
	for _, key := range keys {
		if len(key) != ed25519.PublicKeySize {
			continue
		}
		copyKey := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(copyKey, key)
		store.keys[string(copyKey)] = copyKey
	}
	return store
}

// LoadTrustedKeys loads all PEM-encoded Ed25519 public keys from dir.
func LoadTrustedKeys(dir string) (*TrustedKeys, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("policy: trust dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("policy: trust dir %s is not directory", dir)
	}

	store := &TrustedKeys{keys: make(map[string]ed25519.PublicKey)}
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".pem" {
			return nil
		}
		pemBytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		block, _ := pem.Decode(pemBytes)
		if block == nil {
			return fmt.Errorf("policy: trust store invalid pem %s", path)
		}
		pub, parseErr := x509.ParsePKIXPublicKey(block.Bytes)
		if parseErr != nil {
			return fmt.Errorf("policy: parse pubkey %s: %w", path, parseErr)
		}
		edKey, ok := pub.(ed25519.PublicKey)
		if !ok {
			return fmt.Errorf("policy: key %s not ed25519", path)
		}
		store.keys[string(edKey)] = edKey
		return nil
	}); err != nil {
		return nil, err
	}

	if len(store.keys) == 0 {
		return nil, errors.New("policy: trust store empty")
	}

	return store, nil
}

// VerifyAndParse validates signature and rule payload.
func (t *TrustedKeys) VerifyAndParse(evt *pb.PolicyUpdateEvent) (Event, error) {
	if t == nil {
		return Event{}, errors.New("policy: trusted store nil")
	}

	if evt == nil {
		return Event{}, errors.New("policy: event nil")
	}

	if len(evt.Pubkey) != ed25519.PublicKeySize {
		return Event{}, errors.New("policy: pubkey size invalid")
	}
	pub, ok := t.keys[string(evt.Pubkey)]
	if !ok {
		return Event{}, errors.New("policy: unknown signer")
	}

	if len(evt.Signature) != ed25519.SignatureSize {
		return Event{}, errors.New("policy: signature size invalid")
	}

	signed := pb.PolicyUpdateEvent{
		PolicyId:         evt.PolicyId,
		Action:           evt.Action,
		RuleType:         evt.RuleType,
		RuleData:         evt.RuleData,
		RuleHash:         evt.RuleHash,
		RequiresAck:      evt.RequiresAck,
		RollbackPolicyId: evt.RollbackPolicyId,
		Timestamp:        evt.Timestamp,
		EffectiveHeight:  evt.EffectiveHeight,
		ExpirationHeight: evt.ExpirationHeight,
		ProducerId:       evt.ProducerId,
	}

	// Domain separation identical to backend producer (control.policy.v1).
	bytes, err := proto.Marshal(&signed)
	if err != nil {
		return Event{}, fmt.Errorf("policy: marshal sign payload: %w", err)
	}
	message := append([]byte("control.policy.v1"), bytes...)
	if !ed25519.Verify(pub, message, evt.Signature) {
		return Event{}, errors.New("policy: signature verification failed")
	}

	if len(evt.RuleHash) != 32 {
		return Event{}, errors.New("policy: rule hash invalid length")
	}
	if !hashMatches(evt.RuleData, evt.RuleHash) {
		return Event{}, errors.New("policy: rule hash mismatch")
	}

	spec, err := ParseSpec(evt.RuleType, evt.RuleData, evt.EffectiveHeight, evt.ExpirationHeight, evt.Timestamp)
	if err != nil {
		return Event{}, err
	}

	return Event{Proto: evt, Spec: spec}, nil
}

func hashMatches(payload, expected []byte) bool {
	if len(expected) != 32 {
		return false
	}
	sum := sha256.Sum256(payload)
	for i := range sum {
		if sum[i] != expected[i] {
			return false
		}
	}
	return true
}
