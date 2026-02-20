package policyack

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type TrustedKeys struct {
	keys []ed25519.PublicKey
}

func LoadTrustedKeys(dir string) (*TrustedKeys, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("policy ack: trust dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("policy ack: trust dir %s is not directory", dir)
	}

	keys := make([]ed25519.PublicKey, 0)
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
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
			return fmt.Errorf("policy ack: trust store invalid pem %s", path)
		}
		pub, parseErr := x509.ParsePKIXPublicKey(block.Bytes)
		if parseErr != nil {
			return fmt.Errorf("policy ack: parse pubkey %s: %w", path, parseErr)
		}
		edKey, ok := pub.(ed25519.PublicKey)
		if !ok {
			return fmt.Errorf("policy ack: key %s not ed25519", path)
		}
		copyKey := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(copyKey, edKey)
		keys = append(keys, copyKey)
		return nil
	}); err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return nil, errors.New("policy ack: trust store empty")
	}

	return &TrustedKeys{keys: keys}, nil
}

func (t *TrustedKeys) Verify(message, signature []byte) bool {
	if t == nil {
		return false
	}
	if len(signature) != ed25519.SignatureSize {
		return false
	}
	for _, pub := range t.keys {
		if len(pub) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(pub, message, signature) {
			return true
		}
	}
	return false
}

