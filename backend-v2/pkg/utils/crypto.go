package utils

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Security constants
const (
	MaxDataSize           = 10 << 20 // 10MB max data size
	Ed25519SignatureSize  = 64
	Ed25519PrivateKeySize = 64
	Ed25519PublicKeySize  = 32
	NonceSize             = 16
	AESKeySize            = 32 // AES-256
	GCMNonceSize          = 12
	GCMTagSize            = 16
	KeyVersionSize        = 4
	TimestampSize         = 8

	// Key management
	MaxKeyHistorySize = 10
	DefaultKeyTTL     = 24 * time.Hour
	KeyRotationWindow = 1 * time.Hour

	// Entropy requirements
	MinEntropyBytes   = 32
	EntropyTestRounds = 3
)

// Errors
var (
	ErrInvalidKeySize         = errors.New("crypto: invalid key size")
	ErrCryptoInvalidSignature = errors.New("crypto: signature verification failed")
	ErrDataTooLarge           = errors.New("crypto: data exceeds maximum size")
	ErrKeyNotFound            = errors.New("crypto: key not found")
	ErrKeyExpired             = errors.New("crypto: key has expired")
	ErrInsufficientEntropy    = errors.New("crypto: insufficient entropy")
	ErrReplayDetected         = errors.New("crypto: replay attack detected")
	ErrInvalidCiphertext      = errors.New("crypto: invalid ciphertext format")
	ErrInvalidNonce           = errors.New("crypto: invalid nonce")
	ErrKeyVersionMismatch     = errors.New("crypto: key version mismatch")
	ErrContextCanceled        = errors.New("crypto: operation canceled")
	ErrDecryptionFailed       = errors.New("crypto: decryption failed")
	ErrEncryptionFailed       = errors.New("crypto: encryption failed")
)

// KeyVersion represents a versioned cryptographic key
type KeyVersion struct {
	Version       uint32
	SigningKey    ed25519.PrivateKey
	PublicKey     ed25519.PublicKey
	EncryptionKey []byte
	CreatedAt     time.Time
	ExpiresAt     time.Time
	Active        bool
	KeyID         string
}

// KeyStore manages multiple key versions for rotation
type KeyStore struct {
	mu          sync.RWMutex
	activeKey   *KeyVersion
	keyHistory  map[uint32]*KeyVersion
	nextVersion uint32
	maxHistory  int
}

// NewKeyStore creates a new key store
func NewKeyStore(maxHistory int) *KeyStore {
	if maxHistory <= 0 {
		maxHistory = MaxKeyHistorySize
	}
	return &KeyStore{
		keyHistory:  make(map[uint32]*KeyVersion),
		nextVersion: 1,
		maxHistory:  maxHistory,
	}
}

// AddKey adds a new key version to the store
func (ks *KeyStore) AddKey(key *KeyVersion) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if key.Version == 0 {
		key.Version = ks.nextVersion
		ks.nextVersion++
	}

	ks.keyHistory[key.Version] = key

	// Set as active if it's the first or explicitly active
	if ks.activeKey == nil || key.Active {
		ks.activeKey = key
		key.Active = true
	}

	// Prune old keys
	ks.pruneOldKeysLocked()

	return nil
}

// GetActiveKey returns the current active key
func (ks *KeyStore) GetActiveKey() (*KeyVersion, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	if ks.activeKey == nil {
		return nil, ErrKeyNotFound
	}

	if time.Now().After(ks.activeKey.ExpiresAt) {
		return nil, ErrKeyExpired
	}

	return ks.activeKey, nil
}

// GetKeyByVersion retrieves a specific key version
func (ks *KeyStore) GetKeyByVersion(version uint32) (*KeyVersion, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	key, exists := ks.keyHistory[version]
	if !exists {
		return nil, ErrKeyNotFound
	}

	return key, nil
}

// DeriveAuditKey derives a separate key for audit log signing
// This ensures audit logs use a different key than encryption
func (ks *KeyStore) DeriveAuditKey() ([]byte, error) {
	key, err := ks.GetActiveKey()
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(append(key.EncryptionKey, []byte("audit-hmac")...))
	return h[:], nil
}

// RotateKey creates and activates a new key version
func (ks *KeyStore) RotateKey(ttl time.Duration, generator KeyGenerator) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	newKey, err := generator.GenerateKey(ks.nextVersion, ttl)
	if err != nil {
		return fmt.Errorf("failed to generate new key: %w", err)
	}

	// Deactivate current active key
	if ks.activeKey != nil {
		ks.activeKey.Active = false
	}

	// Add and activate new key
	newKey.Active = true
	ks.keyHistory[newKey.Version] = newKey
	ks.activeKey = newKey
	ks.nextVersion++

	ks.pruneOldKeysLocked()

	return nil
}

// pruneOldKeysLocked removes expired and excess keys (caller must hold lock)
func (ks *KeyStore) pruneOldKeysLocked() {
	if len(ks.keyHistory) <= ks.maxHistory {
		return
	}

	// Collect versions sorted by creation time
	type versionTime struct {
		version uint32
		created time.Time
	}

	versions := make([]versionTime, 0, len(ks.keyHistory))
	for v, k := range ks.keyHistory {
		if k.Active {
			continue // Never prune active key
		}
		versions = append(versions, versionTime{v, k.CreatedAt})
	}

	// Sort by creation time (oldest first)
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].created.Before(versions[j].created)
	})

	// Remove oldest keys beyond max history
	toRemove := len(ks.keyHistory) - ks.maxHistory
	for i := 0; i < toRemove && i < len(versions); i++ {
		key := ks.keyHistory[versions[i].version]
		zeroKey(key.SigningKey)
		zeroKey(key.EncryptionKey)
		delete(ks.keyHistory, versions[i].version)
	}
}

// ZeroAllKeys securely wipes all keys
func (ks *KeyStore) ZeroAllKeys() {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	for _, key := range ks.keyHistory {
		zeroKey(key.SigningKey)
		zeroKey(key.EncryptionKey)
	}
	ks.keyHistory = make(map[uint32]*KeyVersion)
	ks.activeKey = nil
}

// KeyGenerator defines the interface for generating keys
type KeyGenerator interface {
	GenerateKey(version uint32, ttl time.Duration) (*KeyVersion, error)
}

// EntropyValidator validates random entropy quality
type EntropyValidator interface {
	ValidateEntropy() error
}

// CryptoConfig holds configuration for the crypto service
type CryptoConfig struct {
	// Key management
	KeyTTL           time.Duration
	MaxKeyHistory    int
	AutoRotate       bool
	RotationInterval time.Duration

	// Security settings
	EnableReplayProtection bool
	ReplayWindowSize       int
	MaxSignatureAge        time.Duration

	// Audit
	EnableAuditLog bool
	AuditLogger    CryptoAuditLogger

	// Key loading
	KeyLoader KeyLoader

	// Entropy validation
	EntropyValidator EntropyValidator
}

// DefaultCryptoConfig returns secure defaults
func DefaultCryptoConfig() *CryptoConfig {
	return &CryptoConfig{
		KeyTTL:                 DefaultKeyTTL,
		MaxKeyHistory:          MaxKeyHistorySize,
		AutoRotate:             false,
		RotationInterval:       24 * time.Hour,
		EnableReplayProtection: true,
		ReplayWindowSize:       10000,
		MaxSignatureAge:        24 * time.Hour,
		EnableAuditLog:         true,
		EntropyValidator:       &defaultEntropyValidator{},
	}
}

// KeyLoader loads keys from external sources
type KeyLoader interface {
	LoadKey() (*KeyVersion, error)
}

// CryptoAuditLogger logs cryptographic operations for the crypto service
type CryptoAuditLogger interface {
	LogSign(ctx context.Context, keyID string, dataSize int)
	LogVerify(ctx context.Context, keyID string, success bool)
	LogEncrypt(ctx context.Context, keyID string, dataSize int)
	LogDecrypt(ctx context.Context, keyID string, success bool)
	LogKeyRotation(ctx context.Context, oldVersion, newVersion uint32)
}

// CryptoService provides cryptographic operations
type CryptoService struct {
	config    *CryptoConfig
	keyStore  *KeyStore
	generator *defaultKeyGenerator

	// Replay protection
	replayCache *replayCache

	// Metrics
	signCount    uint64
	verifyCount  uint64
	encryptCount uint64
	decryptCount uint64
	rotateCount  uint64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewCryptoService creates a new crypto service
func NewCryptoService(config *CryptoConfig) (*CryptoService, error) {
	if config == nil {
		config = DefaultCryptoConfig()
	}

	// Validate entropy
	if config.EntropyValidator != nil {
		if err := config.EntropyValidator.ValidateEntropy(); err != nil {
			return nil, fmt.Errorf("entropy validation failed: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	cs := &CryptoService{
		config:    config,
		keyStore:  NewKeyStore(config.MaxKeyHistory),
		generator: &defaultKeyGenerator{entropy: config.EntropyValidator},
		ctx:       ctx,
		cancel:    cancel,
	}

	if config.EnableReplayProtection {
		cs.replayCache = newReplayCache(config.ReplayWindowSize)
		cs.startReplayCacheCleanup()
	}

	// Load initial key
	if config.KeyLoader != nil {
		key, err := config.KeyLoader.LoadKey()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to load key: %w", err)
		}
		if err := cs.keyStore.AddKey(key); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to add key: %w", err)
		}
	} else {
		// Generate initial key if no loader provided
		if err := cs.keyStore.RotateKey(config.KeyTTL, cs.generator); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to generate initial key: %w", err)
		}
	}

	// Start auto-rotation if enabled
	if config.AutoRotate {
		cs.startKeyRotation()
	}

	return cs, nil
}

// SignWithContext signs data with the active key
func (cs *CryptoService) SignWithContext(ctx context.Context, data []byte) ([]byte, error) {
	if len(data) > MaxDataSize {
		return nil, ErrDataTooLarge
	}

	select {
	case <-ctx.Done():
		return nil, ErrContextCanceled
	default:
	}

	key, err := cs.keyStore.GetActiveKey()
	if err != nil {
		return nil, err
	}

	// Build signature package: [version(4)][timestamp(8)][nonce(16)][signature(64)]
	var buf bytes.Buffer

	// Write version
	if err := binary.Write(&buf, binary.BigEndian, key.Version); err != nil {
		return nil, fmt.Errorf("failed to write version: %w", err)
	}

	// Write timestamp
	timestamp := time.Now().Unix()
	if err := binary.Write(&buf, binary.BigEndian, timestamp); err != nil {
		return nil, fmt.Errorf("failed to write timestamp: %w", err)
	}

	// Generate and write nonce if replay protection is enabled
	var nonce []byte
	if cs.config.EnableReplayProtection {
		nonce = make([]byte, NonceSize)
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return nil, fmt.Errorf("failed to generate nonce: %w", err)
		}
		buf.Write(nonce)
	}

	// Prepare data to sign: version + timestamp + nonce + data
	dataToSign := append(buf.Bytes(), data...)

	// Sign
	signature := ed25519.Sign(key.SigningKey, dataToSign)
	buf.Write(signature)

	atomic.AddUint64(&cs.signCount, 1)

	if cs.config.EnableAuditLog && cs.config.AuditLogger != nil {
		cs.config.AuditLogger.LogSign(ctx, key.KeyID, len(data))
	}

	return buf.Bytes(), nil
}

// VerifyWithContext verifies a signature
func (cs *CryptoService) VerifyWithContext(ctx context.Context, data, signedData []byte, pubKey []byte, allowOldTimestamps bool) error {
	if len(data) > MaxDataSize {
		return ErrDataTooLarge
	}

	select {
	case <-ctx.Done():
		return ErrContextCanceled
	default:
	}

	buf := bytes.NewReader(signedData)

	// Read version
	var version uint32
	if err := binary.Read(buf, binary.BigEndian, &version); err != nil {
		return ErrCryptoInvalidSignature
	}

	// Read timestamp
	var timestamp int64
	if err := binary.Read(buf, binary.BigEndian, &timestamp); err != nil {
		return ErrCryptoInvalidSignature
	}

	// Validate timestamp freshness unless explicitly skipped (used for disk restores)
	if cs.config.EnableReplayProtection && !allowOldTimestamps {
		signTime := time.Unix(timestamp, 0)
		if time.Since(signTime) > cs.config.MaxSignatureAge {
			return errors.New("signature timestamp too old")
		}
		if time.Until(signTime) > time.Minute {
			return errors.New("signature timestamp in future")
		}
	}

	// Read nonce if replay protection is enabled
	var nonce []byte
	if cs.config.EnableReplayProtection {
		nonce = make([]byte, NonceSize)
		if _, err := io.ReadFull(buf, nonce); err != nil {
			return ErrInvalidNonce
		}
	}

	// Read signature
	signature := make([]byte, Ed25519SignatureSize)
	if _, err := io.ReadFull(buf, signature); err != nil {
		return ErrCryptoInvalidSignature
	}

	// Reconstruct signed data: version + timestamp + nonce (if enabled) + data
	// This must match exactly what was signed in SignWithContext
	var signedDataBuf bytes.Buffer
	binary.Write(&signedDataBuf, binary.BigEndian, version)
	binary.Write(&signedDataBuf, binary.BigEndian, timestamp)
	if cs.config.EnableReplayProtection {
		signedDataBuf.Write(nonce)
	}
	signedDataBuf.Write(data)

	var payloadHash [32]byte
	if cs.config.EnableReplayProtection {
		payloadHash = sha256.Sum256(signedDataBuf.Bytes())
		if err := cs.replayCache.Check(nonce, payloadHash); err != nil {
			return err
		}
	}

	// Get verification key
	var verifyKey ed25519.PublicKey
	if len(pubKey) > 0 {
		if len(pubKey) != Ed25519PublicKeySize {
			return ErrInvalidKeySize
		}
		verifyKey = pubKey
	} else {
		key, err := cs.keyStore.GetKeyByVersion(version)
		if err != nil {
			return fmt.Errorf("key version %d: %w", version, err)
		}
		verifyKey = key.PublicKey
	}

	// Verify signature
	if !ed25519.Verify(verifyKey, signedDataBuf.Bytes(), signature) {
		atomic.AddUint64(&cs.verifyCount, 1)
		if cs.config.EnableAuditLog && cs.config.AuditLogger != nil {
			cs.config.AuditLogger.LogVerify(ctx, "", false)
		}
		return ErrCryptoInvalidSignature
	}

	// Add to replay cache
	if cs.config.EnableReplayProtection && nonce != nil {
		cs.replayCache.Add(nonce, payloadHash)
	}

	atomic.AddUint64(&cs.verifyCount, 1)

	if cs.config.EnableAuditLog && cs.config.AuditLogger != nil {
		cs.config.AuditLogger.LogVerify(ctx, "", true)
	}

	return nil
}

// EncryptWithContext encrypts data with versioned key
func (cs *CryptoService) EncryptWithContext(ctx context.Context, plaintext []byte) ([]byte, error) {
	if len(plaintext) > MaxDataSize {
		return nil, ErrDataTooLarge
	}

	select {
	case <-ctx.Done():
		return nil, ErrContextCanceled
	default:
	}

	key, err := cs.keyStore.GetActiveKey()
	if err != nil {
		return nil, err
	}

	if len(key.EncryptionKey) != AESKeySize {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryptionFailed, err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryptionFailed, err)
	}

	nonce := make([]byte, GCMNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: nonce generation failed: %v", ErrEncryptionFailed, err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Build result: [version(4)][nonce(12)][ciphertext]
	result := make([]byte, KeyVersionSize+GCMNonceSize+len(ciphertext))
	binary.BigEndian.PutUint32(result[0:], key.Version)
	copy(result[KeyVersionSize:], nonce)
	copy(result[KeyVersionSize+GCMNonceSize:], ciphertext)

	atomic.AddUint64(&cs.encryptCount, 1)

	if cs.config.EnableAuditLog && cs.config.AuditLogger != nil {
		cs.config.AuditLogger.LogEncrypt(ctx, key.KeyID, len(plaintext))
	}

	return result, nil
}

// DecryptWithContext decrypts data with automatic key version detection
func (cs *CryptoService) DecryptWithContext(ctx context.Context, ciphertext []byte) ([]byte, error) {
	minSize := KeyVersionSize + GCMNonceSize + GCMTagSize
	if len(ciphertext) < minSize {
		return nil, ErrInvalidCiphertext
	}

	select {
	case <-ctx.Done():
		return nil, ErrContextCanceled
	default:
	}

	// Extract version
	version := binary.BigEndian.Uint32(ciphertext[0:KeyVersionSize])

	// Get key by version
	key, err := cs.keyStore.GetKeyByVersion(version)
	if err != nil {
		return nil, fmt.Errorf("key version %d: %w", version, err)
	}

	if len(key.EncryptionKey) != AESKeySize {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptionFailed, err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryptionFailed, err)
	}

	// Extract nonce and encrypted data
	nonce := ciphertext[KeyVersionSize : KeyVersionSize+GCMNonceSize]
	encrypted := ciphertext[KeyVersionSize+GCMNonceSize:]

	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		atomic.AddUint64(&cs.decryptCount, 1)
		if cs.config.EnableAuditLog && cs.config.AuditLogger != nil {
			cs.config.AuditLogger.LogDecrypt(ctx, key.KeyID, false)
		}
		return nil, fmt.Errorf("%w: %v", ErrDecryptionFailed, err)
	}

	atomic.AddUint64(&cs.decryptCount, 1)

	if cs.config.EnableAuditLog && cs.config.AuditLogger != nil {
		cs.config.AuditLogger.LogDecrypt(ctx, key.KeyID, true)
	}

	return plaintext, nil
}

// Simple interfaces
func (cs *CryptoService) Sign(data []byte) ([]byte, error) {
	return cs.SignWithContext(context.Background(), data)
}

func (cs *CryptoService) Verify(data, signature []byte) bool {
	return cs.VerifyWithContext(context.Background(), data, signature, nil, false) == nil
}

func (cs *CryptoService) Encrypt(plaintext []byte) ([]byte, error) {
	return cs.EncryptWithContext(context.Background(), plaintext)
}

func (cs *CryptoService) Decrypt(ciphertext []byte) ([]byte, error) {
	return cs.DecryptWithContext(context.Background(), ciphertext)
}

// RotateKeys performs key rotation
func (cs *CryptoService) RotateKeys(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ErrContextCanceled
	default:
	}

	oldKey, _ := cs.keyStore.GetActiveKey()

	if err := cs.keyStore.RotateKey(cs.config.KeyTTL, cs.generator); err != nil {
		return err
	}

	newKey, _ := cs.keyStore.GetActiveKey()

	atomic.AddUint64(&cs.rotateCount, 1)

	if cs.config.EnableAuditLog && cs.config.AuditLogger != nil && oldKey != nil && newKey != nil {
		cs.config.AuditLogger.LogKeyRotation(ctx, oldKey.Version, newKey.Version)
	}

	return nil
}

// GetPublicKey returns the active public key
func (cs *CryptoService) GetPublicKey() ([]byte, error) {
	key, err := cs.keyStore.GetActiveKey()
	if err != nil {
		return nil, err
	}

	pubKey := make([]byte, len(key.PublicKey))
	copy(pubKey, key.PublicKey)
	return pubKey, nil
}

// GetKeyVersion returns the active key version
func (cs *CryptoService) GetKeyVersion() (uint32, error) {
	key, err := cs.keyStore.GetActiveKey()
	if err != nil {
		return 0, err
	}
	return key.Version, nil
}

// GetKeyStore returns the key store (for audit key derivation)
func (cs *CryptoService) GetKeyStore() *KeyStore {
	return cs.keyStore
}

// GetMetrics returns service metrics
func (cs *CryptoService) GetMetrics() map[string]uint64 {
	return map[string]uint64{
		"sign_count":    atomic.LoadUint64(&cs.signCount),
		"verify_count":  atomic.LoadUint64(&cs.verifyCount),
		"encrypt_count": atomic.LoadUint64(&cs.encryptCount),
		"decrypt_count": atomic.LoadUint64(&cs.decryptCount),
		"rotate_count":  atomic.LoadUint64(&cs.rotateCount),
	}
}

// HealthCheck performs service health check
func (cs *CryptoService) HealthCheck() error {
	_, err := cs.keyStore.GetActiveKey()
	return err
}

// Shutdown gracefully shuts down the service
func (cs *CryptoService) Shutdown() error {
	cs.cancel()
	cs.wg.Wait()
	cs.keyStore.ZeroAllKeys()
	return nil
}

// Private methods

func (cs *CryptoService) startKeyRotation() {
	cs.wg.Add(1)
	go func() {
		defer cs.wg.Done()
		ticker := time.NewTicker(cs.config.RotationInterval)
		defer ticker.Stop()

		for {
			select {
			case <-cs.ctx.Done():
				return
			case <-ticker.C:
				if err := cs.RotateKeys(cs.ctx); err != nil {
					// Log error (would use logger here)
					continue
				}
			}
		}
	}()
}

func (cs *CryptoService) startReplayCacheCleanup() {
	cs.wg.Add(1)
	go func() {
		defer cs.wg.Done()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-cs.ctx.Done():
				return
			case <-ticker.C:
				cs.replayCache.Cleanup()
			}
		}
	}()
}

// replayCache provides thread-safe replay attack prevention
type replayCache struct {
	mu      sync.RWMutex
	cache   map[string]replayEntry
	maxSize int
}

type replayEntry struct {
	timestamp time.Time
	hash      [32]byte
}

func newReplayCache(maxSize int) *replayCache {
	return &replayCache{
		cache:   make(map[string]replayEntry),
		maxSize: maxSize,
	}
}

func (rc *replayCache) Check(nonce []byte, hash [32]byte) error {
	rc.mu.RLock()
	entry, exists := rc.cache[string(nonce)]
	rc.mu.RUnlock()
	if !exists {
		return nil
	}
	if entry.hash != hash {
		return ErrReplayDetected
	}
	return nil
}

func (rc *replayCache) Add(nonce []byte, hash [32]byte) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	key := string(nonce)
	rc.cache[key] = replayEntry{
		timestamp: time.Now(),
		hash:      hash,
	}

	if len(rc.cache) > rc.maxSize {
		rc.cleanupLocked()
	}
}

func (rc *replayCache) Cleanup() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.cleanupLocked()
}

func (rc *replayCache) cleanupLocked() {
	cutoff := time.Now().Add(-5 * time.Minute)
	for key, entry := range rc.cache {
		if entry.timestamp.Before(cutoff) {
			delete(rc.cache, key)
		}
	}
}

// defaultKeyGenerator generates Ed25519 and AES keys
type defaultKeyGenerator struct {
	entropy EntropyValidator
}

func (g *defaultKeyGenerator) GenerateKey(version uint32, ttl time.Duration) (*KeyVersion, error) {
	if g.entropy != nil {
		if err := g.entropy.ValidateEntropy(); err != nil {
			return nil, err
		}
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate signing key: %w", err)
	}

	encKey := make([]byte, AESKeySize)
	if _, err := io.ReadFull(rand.Reader, encKey); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}

	now := time.Now()
	return &KeyVersion{
		Version:       version,
		SigningKey:    priv,
		PublicKey:     pub,
		EncryptionKey: encKey,
		CreatedAt:     now,
		ExpiresAt:     now.Add(ttl),
		Active:        false,
		KeyID:         generateKeyID(pub),
	}, nil
}

// defaultEntropyValidator validates system entropy
type defaultEntropyValidator struct{}

func (v *defaultEntropyValidator) ValidateEntropy() error {
	for i := 0; i < EntropyTestRounds; i++ {
		test := make([]byte, MinEntropyBytes)
		if _, err := io.ReadFull(rand.Reader, test); err != nil {
			return fmt.Errorf("entropy read failed: %w", err)
		}

		// Chi-square test for uniformity
		if !isEntropyGood(test) {
			return ErrInsufficientEntropy
		}
	}
	return nil
}

func isEntropyGood(data []byte) bool {
	freq := make([]int, 256)
	for _, b := range data {
		freq[b]++
	}

	expected := float64(len(data)) / 256.0
	chiSquare := 0.0

	for _, count := range freq {
		diff := float64(count) - expected
		chiSquare += (diff * diff) / expected
	}

	// Chi-square critical value for 255 degrees of freedom at 0.05 significance
	criticalValue := 293.25
	return chiSquare < criticalValue
}

// Utility functions

func zeroKey(key []byte) {
	for i := range key {
		key[i] = 0
	}
}

func generateKeyID(pubKey []byte) string {
	hash := sha256.Sum256(pubKey)
	return hex.EncodeToString(hash[:8])
}

// ConstantTimeCompare performs timing-safe comparison
func ConstantTimeCompare(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// Canonical data functions for distributed consensus

// CanonicalProposalBytes builds deterministic bytes for signing
func CanonicalProposalBytes(
	id string,
	term uint64,
	proposerID string,
	timestamp time.Time,
	requestID string,
	nonce string,
	payload []byte,
	metadata map[string]interface{},
) []byte {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "%s|%d|%s|%d|%s|%s|%x",
		id, term, proposerID, timestamp.UnixNano(), requestID, nonce, payload)

	if metadata != nil {
		keys := make([]string, 0, len(metadata))
		for k := range metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&buf, "|%s=%v", k, metadata[k])
		}
	}

	return buf.Bytes()
}

// HashProposal computes SHA-256 hash of canonical proposal
func HashProposal(
	id string,
	term uint64,
	proposerID string,
	timestamp time.Time,
	requestID string,
	nonce string,
	payload []byte,
	metadata map[string]interface{},
) string {
	data := CanonicalProposalBytes(id, term, proposerID, timestamp, requestID, nonce, payload, metadata)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// CanonicalThreatProposalBytes builds deterministic bytes for threat proposals
func CanonicalThreatProposalBytes(
	id string,
	term uint64,
	proposerID string,
	timestamp time.Time,
	requestID string,
	nonce string,
	severity string,
	confidence float64,
	threatType string,
	sourceNode string,
	threatDataJSON []byte,
	metadata map[string]interface{},
) []byte {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "%s|%d|%s|%d|%s|%s|%s|%.6f|%s|%s|%x",
		id, term, proposerID, timestamp.UnixNano(),
		requestID, nonce, severity, confidence,
		threatType, sourceNode, threatDataJSON)

	if metadata != nil {
		keys := make([]string, 0, len(metadata))
		for k := range metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&buf, "|%s=%v", k, metadata[k])
		}
	}

	return buf.Bytes()
}

// HashThreatProposal computes SHA-256 hash of canonical threat proposal
func HashThreatProposal(
	id string,
	term uint64,
	proposerID string,
	timestamp time.Time,
	requestID string,
	nonce string,
	severity string,
	confidence float64,
	threatType string,
	sourceNode string,
	threatDataJSON []byte,
	metadata map[string]interface{},
) string {
	data := CanonicalThreatProposalBytes(id, term, proposerID, timestamp, requestID, nonce, severity, confidence, threatType, sourceNode, threatDataJSON, metadata)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HMAC-based message authentication

// ComputeHMAC computes HMAC-SHA256 of data
func ComputeHMAC(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// ValidateHMAC validates HMAC in constant time
func ValidateHMAC(key, data, mac []byte) bool {
	expected := ComputeHMAC(key, data)
	return hmac.Equal(mac, expected)
}
