package ack

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
)

const (
	signingAlgoEd25519 = "ed25519"
)

// NewEd25519Signer loads an Ed25519 private key from disk.
func NewEd25519Signer(path string) (Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ack signer: read key: %w", err)
	}
	raw, err := parsePrivateKey(bytes.TrimSpace(data))
	if err != nil {
		return nil, err
	}
	switch len(raw) {
	case ed25519.SeedSize:
		key := ed25519.NewKeyFromSeed(raw)
		return &ed25519Signer{key: key}, nil
	case ed25519.PrivateKeySize:
		return &ed25519Signer{key: ed25519.PrivateKey(raw)}, nil
	default:
		return nil, fmt.Errorf("ack signer: unsupported key length %d", len(raw))
	}
}

func parsePrivateKey(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("ack signer: empty key")
	}
	if block, _ := pem.Decode(data); block != nil {
		if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			if ed, ok := key.(ed25519.PrivateKey); ok {
				return []byte(ed), nil
			}
		}
		data = block.Bytes
	}
	if decoded, err := hex.DecodeString(string(data)); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(string(data)); err == nil {
		return decoded, nil
	}
	return data, nil
}

type ed25519Signer struct {
	key ed25519.PrivateKey
}

func (s *ed25519Signer) Sign(_ context.Context, _ Payload, encoded []byte) (SignOutput, error) {
	signature := ed25519.Sign(s.key, encoded)
	return SignOutput{Signature: signature, Algorithm: signingAlgoEd25519}, nil
}
