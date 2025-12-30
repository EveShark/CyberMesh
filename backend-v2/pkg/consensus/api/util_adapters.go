package api

import (
	"context"
	"crypto/sha256"
	"fmt"

	"backend/pkg/consensus/types"
	"backend/pkg/utils"
	"go.uber.org/zap"
	"net"
	"sync"
)

// cryptoAdapter bridges utils.CryptoService to types.CryptoService.
type cryptoAdapter struct {
	c *utils.CryptoService
	// registry maps validator IDs to their public keys for signature verification
	registry map[types.ValidatorID][]byte
	mu       sync.RWMutex
}

// NewCryptoAdapter builds a crypto adapter with a public key registry from the validator set.
func NewCryptoAdapter(c *utils.CryptoService, vs types.ValidatorSet) types.CryptoService {
	reg := make(map[types.ValidatorID][]byte)
	if vs != nil {
		for _, v := range vs.GetValidators() {
			if len(v.PublicKey) > 0 {
				key := make([]byte, len(v.PublicKey))
				copy(key, v.PublicKey)
				reg[v.ID] = key
			}
		}
	}
	return &cryptoAdapter{c: c, registry: reg}
}

func (a *cryptoAdapter) SignWithContext(ctx context.Context, data []byte) ([]byte, error) {
	return a.c.SignWithContext(ctx, data)
}

func (a *cryptoAdapter) VerifyWithContext(ctx context.Context, data, signature, publicKey []byte, allowOldTimestamps bool) error {
	return a.c.VerifyWithContext(ctx, data, signature, publicKey, allowOldTimestamps)
}

func (a *cryptoAdapter) GetKeyID() types.ValidatorID {
	pub, err := a.c.GetPublicKey()
	if err != nil {
		return types.ValidatorID{}
	}
	h := sha256.Sum256(pub)
	var id types.ValidatorID
	copy(id[:], h[:])
	return id
}

func (a *cryptoAdapter) GetPublicKey(keyID types.ValidatorID) ([]byte, error) {
	// Lookup by validator ID first
	a.mu.RLock()
	if pub, ok := a.registry[keyID]; ok {
		out := make([]byte, len(pub))
		copy(out, pub)
		a.mu.RUnlock()
		return out, nil
	}
	a.mu.RUnlock()

	// Fallback: if keyID matches local, return local public key
	localPub, err := a.c.GetPublicKey()
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(localPub)
	var localID types.ValidatorID
	copy(localID[:], h[:])
	if localID == keyID {
		out := make([]byte, len(localPub))
		copy(out, localPub)
		return out, nil
	}

	return nil, fmt.Errorf("unknown validator public key for id %x", keyID[:8])
}

func (a *cryptoAdapter) GetKeyVersion() (string, error) {
	v, err := a.c.GetKeyVersion()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", v), nil
}

func (a *cryptoAdapter) EncryptWithContext(ctx context.Context, plaintext []byte) ([]byte, error) {
	return a.c.EncryptWithContext(ctx, plaintext)
}

func (a *cryptoAdapter) DecryptWithContext(ctx context.Context, ciphertext []byte) ([]byte, error) {
	return a.c.DecryptWithContext(ctx, ciphertext)
}

// loggerAdapter bridges utils.Logger to types.Logger with simple field mapping.
type loggerAdapter struct{ l *utils.Logger }

func NewLoggerAdapter(l *utils.Logger) types.Logger { return &loggerAdapter{l: l} }

func (a *loggerAdapter) InfoContext(ctx context.Context, msg string, fields ...interface{}) {
	a.l.InfoContext(ctx, msg, toZapFields(fields...)...)
}

func (a *loggerAdapter) WarnContext(ctx context.Context, msg string, fields ...interface{}) {
	a.l.WarnContext(ctx, msg, toZapFields(fields...)...)
}

func (a *loggerAdapter) ErrorContext(ctx context.Context, msg string, fields ...interface{}) {
	a.l.ErrorContext(ctx, msg, toZapFields(fields...)...)
}

func (a *loggerAdapter) DebugContext(ctx context.Context, msg string, fields ...interface{}) {
	a.l.DebugContext(ctx, msg, toZapFields(fields...)...)
}

func (a *loggerAdapter) With(fields ...interface{}) types.Logger { return a }

func toZapFields(fields ...interface{}) []zap.Field {
	// Convert key/value pairs into zap fields via utils helpers.
	out := make([]zap.Field, 0, len(fields))
	n := len(fields)
	for i := 0; i < n; i += 2 {
		if i+1 >= n {
			break
		}
		k, ok := fields[i].(string)
		if !ok {
			// Fallback: stringify key
			k = fmt.Sprintf("%v", fields[i])
		}
		out = append(out, utils.ZapAny(k, fields[i+1]))
	}
	return out
}

// ipAllowlistAdapter bridges utils.IPAllowlist to types.IPAllowlist.
type ipAllowlistAdapter struct{ a *utils.IPAllowlist }

func NewIPAllowlistAdapter(a *utils.IPAllowlist) types.IPAllowlist { return &ipAllowlistAdapter{a: a} }

func (a *ipAllowlistAdapter) IsAllowed(ip string) bool {
	if ip == "" || a.a == nil {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return a.a.IsAllowed(parsed)
}

func (a *ipAllowlistAdapter) IsAddrAllowed(addr string) error {
	if a.a == nil {
		return fmt.Errorf("allowlist not configured")
	}
	return a.a.IsAddrAllowed(addr)
}

func (a *ipAllowlistAdapter) ValidateBeforeConnect(ctx context.Context, addr string) error {
	if a.a == nil {
		return fmt.Errorf("allowlist not configured")
	}
	return a.a.ValidateBeforeConnect(ctx, addr)
}
