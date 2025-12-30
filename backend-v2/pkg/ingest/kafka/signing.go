package kafka

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"backend/pkg/utils"
	pb "backend/proto"
	"google.golang.org/protobuf/proto"
)

// CommitSignerConfig holds configuration for commit message signing.
type CommitSignerConfig struct {
	KeyPath    string
	KeyID      string
	Domain     string
	ProducerID string
	Logger     *utils.Logger
}

// CommitSigner handles Ed25519 signing for commit events.
type CommitSigner struct {
	keyID      string
	domain     string
	producerID []byte
	privKey    ed25519.PrivateKey
	pubKey     ed25519.PublicKey
	logger     *utils.Logger
}

// NewCommitSigner loads an Ed25519 key from disk and returns a signer.
func NewCommitSigner(cfg CommitSignerConfig) (*CommitSigner, error) {
	if strings.TrimSpace(cfg.KeyPath) == "" {
		return nil, fmt.Errorf("commit signer: key path is required")
	}

	pemData, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("commit signer: failed to read key: %w", err)
	}

	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("commit signer: invalid PEM encoding in %s", cfg.KeyPath)
	}

	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("commit signer: failed to parse PKCS#8 key: %w", err)
	}

	privKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("commit signer: key is not Ed25519: %T", parsedKey)
	}

	pubKey := privKey.Public().(ed25519.PublicKey)

	domain := strings.TrimSpace(cfg.Domain)
	if domain == "" {
		domain = "control.commits.v1"
	}

	keyID := strings.TrimSpace(cfg.KeyID)
	if keyID == "" {
		keyID = "unknown"
	}

	producerBytes := []byte(strings.TrimSpace(cfg.ProducerID))
	if len(producerBytes) == 0 {
		producerBytes = make([]byte, len(pubKey))
		copy(producerBytes, pubKey)
	}

	signer := &CommitSigner{
		keyID:      keyID,
		domain:     domain,
		producerID: producerBytes,
		privKey:    privKey,
		pubKey:     pubKey,
		logger:     cfg.Logger,
	}

	if signer.logger != nil {
		signer.logger.Info("Commit signer initialized",
			utils.ZapString("key_id", signer.keyID),
			utils.ZapString("domain", signer.domain))
	}

	return signer, nil
}

// ProducerID returns the configured producer identifier bytes.
func (s *CommitSigner) ProducerID() []byte {
	id := make([]byte, len(s.producerID))
	copy(id, s.producerID)
	return id
}

// PublicKey returns the public key bytes.
func (s *CommitSigner) PublicKey() []byte {
	pk := make([]byte, len(s.pubKey))
	copy(pk, s.pubKey)
	return pk
}

// Sign attaches signature and public key fields to the commit event.
func (s *CommitSigner) Sign(evt *pb.CommitEvent) error {
	if evt == nil {
		return fmt.Errorf("commit signer: event is nil")
	}

	evt.ProducerId = s.ProducerID()

	signMsg := &pb.CommitEvent{
		Height:        evt.Height,
		BlockHash:     evt.BlockHash,
		StateRoot:     evt.StateRoot,
		TxCount:       evt.TxCount,
		AnomalyCount:  evt.AnomalyCount,
		EvidenceCount: evt.EvidenceCount,
		PolicyCount:   evt.PolicyCount,
		AnomalyIds:    append([]string(nil), evt.AnomalyIds...),
		Timestamp:     evt.Timestamp,
		ProducerId:    evt.ProducerId,
	}

	payload, err := proto.Marshal(signMsg)
	if err != nil {
		return fmt.Errorf("commit signer: failed to marshal sign payload: %w", err)
	}

	message := append([]byte(s.domain), payload...)
	signature := ed25519.Sign(s.privKey, message)

	evt.Signature = signature
	evt.Pubkey = s.PublicKey()
	evt.Alg = "Ed25519"

	return nil
}

// SignPolicy attaches signature and public key fields to the policy update event.
func (s *CommitSigner) SignPolicy(evt *pb.PolicyUpdateEvent) error {
	if evt == nil {
		return fmt.Errorf("commit signer: policy event is nil")
	}

	evt.ProducerId = s.ProducerID()

	signMsg := &pb.PolicyUpdateEvent{
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

	payload, err := proto.Marshal(signMsg)
	if err != nil {
		return fmt.Errorf("commit signer: failed to marshal policy sign payload: %w", err)
	}

	message := append([]byte(s.domain), payload...)
	signature := ed25519.Sign(s.privKey, message)

	evt.Signature = signature
	evt.Pubkey = s.PublicKey()
	evt.Alg = "Ed25519"

	return nil
}
