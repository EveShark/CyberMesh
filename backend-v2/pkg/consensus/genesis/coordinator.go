package genesis

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"backend/pkg/consensus/messages"
	"backend/pkg/consensus/types"
	"backend/pkg/utils"
	"github.com/fxamacker/cbor/v2"
)

// ReadyEvent enumerates lifecycle events for genesis readiness telemetry.
type ReadyEvent string

const (
	ReadyEventReceived      ReadyEvent = "received"
	ReadyEventAccepted      ReadyEvent = "accepted"
	ReadyEventDuplicate     ReadyEvent = "duplicate"
	ReadyEventReplayBlocked ReadyEvent = "replay_blocked"
	ReadyEventRejected      ReadyEvent = "rejected"
)

// Host defines the minimal hooks the coordinator needs from the consensus engine.
type Host interface {
	PublishGenesisMessage(ctx context.Context, msg types.Message) error
	OnGenesisCertificate(ctx context.Context, cert *messages.GenesisCertificate) error
	IsConsensusActive() bool
	MarkValidatorReady(id types.ValidatorID)
	RecordGenesisReady(ctx context.Context, event ReadyEvent, id types.ValidatorID, hash [32]byte, reason string)
	RecordGenesisCertificateDiscard(ctx context.Context)
}

// PeerHasher provides a deterministic cluster peer fingerprint.
type PeerHasher interface {
	PeerHash() ([32]byte, error)
}

// Config controls ceremony parameters.
type Config struct {
	ReadyTimeout              time.Duration
	CertificateTimeout        time.Duration
	ClockSkewTolerance        time.Duration
	GenesisClockSkewTolerance time.Duration
	RequiredQuorum            int
	// StateMode selects how genesis certificate state is restored/persisted.
	// Values: "db_only", "db_then_disk", "disk_only".
	StateMode string
	// DBRequired makes DB restore failures fatal.
	DBRequired           bool
	NetworkID            string
	ConfigHash           [32]byte
	PeerHash             [32]byte
	ReadyRefreshInterval time.Duration
	StatePath            string
}

const (
	stateModeDBOnly     = "db_only"
	stateModeDBThenDisk = "db_then_disk"
	stateModeDiskOnly   = "disk_only"
)

type persistedGenesisState struct {
	Certificate *messages.GenesisCertificate `json:"certificate"`
	SavedAt     time.Time                    `json:"saved_at"`
}

// Coordinator manages the genesis readiness ceremony.
type Coordinator struct {
	cfg          Config
	localID      types.ValidatorID
	validatorSet types.ValidatorSet
	rotation     types.LeaderRotation
	crypto       types.CryptoService
	host         Host
	peerHasher   PeerHasher
	logger       types.Logger
	audit        types.AuditLogger
	backend      types.StorageBackend

	mu                   sync.Mutex
	attestations         map[types.ValidatorID]*messages.GenesisReady
	verifying            map[types.ValidatorID][32]byte
	requirement          int
	aggregator           types.ValidatorID
	certificate          *messages.GenesisCertificate
	certificateBroadcast bool
	completed            bool
	doneCh               chan struct{}

	readyMu     sync.Mutex
	localReady  *messages.GenesisReady
	rebroadcast struct {
		mu       sync.Mutex
		base     time.Duration
		max      time.Duration
		current  time.Duration
		lastHash [32]byte
		lastSend time.Time
	}

	certificateOnce sync.Once
	startOnce       sync.Once
	discardCount    atomic.Uint32
}

// NewCoordinator constructs a genesis ceremony coordinator.
func NewCoordinator(
	cfg Config,
	localID types.ValidatorID,
	validatorSet types.ValidatorSet,
	rotation types.LeaderRotation,
	crypto types.CryptoService,
	host Host,
	peerHasher PeerHasher,
	logger types.Logger,
	audit types.AuditLogger,
	backend types.StorageBackend,
) (*Coordinator, error) {
	if validatorSet == nil || rotation == nil || crypto == nil || host == nil || logger == nil {
		return nil, fmt.Errorf("genesis coordinator: missing dependencies")
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = 30 * time.Second
	}
	if cfg.CertificateTimeout <= 0 {
		cfg.CertificateTimeout = 60 * time.Second
	}
	if cfg.ClockSkewTolerance <= 0 {
		cfg.ClockSkewTolerance = 5 * time.Second
	}
	if cfg.GenesisClockSkewTolerance <= 0 {
		cfg.GenesisClockSkewTolerance = 24 * time.Hour
	}
	if cfg.ReadyRefreshInterval < 0 {
		cfg.ReadyRefreshInterval = 0
	}

	coord := &Coordinator{
		cfg:          cfg,
		localID:      localID,
		validatorSet: validatorSet,
		rotation:     rotation,
		crypto:       crypto,
		host:         host,
		peerHasher:   peerHasher,
		logger:       logger,
		audit:        audit,
		backend:      backend,
		attestations: make(map[types.ValidatorID]*messages.GenesisReady),
		verifying:    make(map[types.ValidatorID][32]byte),
		doneCh:       make(chan struct{}),
	}
	coord.applyConfigDefaults()
	coord.rebroadcast.base = 5 * time.Second
	coord.rebroadcast.max = time.Minute
	if cfg.ReadyRefreshInterval > 0 {
		if cfg.ReadyRefreshInterval < coord.rebroadcast.max {
			coord.rebroadcast.max = cfg.ReadyRefreshInterval
		}
		if base := cfg.ReadyRefreshInterval / 2; base >= time.Second {
			coord.rebroadcast.base = base
		}
	}
	if coord.rebroadcast.base <= 0 {
		coord.rebroadcast.base = time.Second
	}
	if coord.rebroadcast.max < coord.rebroadcast.base {
		coord.rebroadcast.max = coord.rebroadcast.base
	}
	coord.rebroadcast.current = coord.rebroadcast.base
	return coord, nil
}

func (c *Coordinator) applyConfigDefaults() {
	mode := c.cfg.StateMode
	if mode == "" {
		mode = stateModeDBThenDisk
	}
	switch mode {
	case stateModeDBOnly, stateModeDBThenDisk, stateModeDiskOnly:
		// ok
	default:
		mode = stateModeDBThenDisk
	}
	c.cfg.StateMode = mode
	if c.cfg.NetworkID == "" {
		c.cfg.NetworkID = "cybermesh"
	}
}

// Start broadcasts the local READY attestation and prepares quorum tracking.
func (c *Coordinator) Start(ctx context.Context) error {
	var err error
	c.startOnce.Do(func() {
		err = c.initialize(ctx)
	})
	return err
}

func (c *Coordinator) initialize(ctx context.Context) error {
	aggregator, err := c.rotation.GetLeaderForView(ctx, 0)
	if err != nil {
		return fmt.Errorf("determine aggregator: %w", err)
	}
	c.mu.Lock()
	c.aggregator = aggregator
	if c.cfg.RequiredQuorum > 0 {
		c.requirement = c.cfg.RequiredQuorum
	} else {
		count := c.validatorSet.GetValidatorCount()
		if count == 0 {
			c.mu.Unlock()
			return fmt.Errorf("validator set empty")
		}
		f := (count - 1) / 3
		c.requirement = 2*f + 1
		if c.requirement < 1 {
			c.requirement = 1
		}
	}
	c.mu.Unlock()

	// Production-grade guardrails: db_only is the only safe mode for multi-node clusters.
	if c.cfg.StateMode == stateModeDiskOnly && c.validatorSet.GetValidatorCount() > 1 {
		return fmt.Errorf("genesis state mode %q is not allowed with %d validators", c.cfg.StateMode, c.validatorSet.GetValidatorCount())
	}

	c.logger.InfoContext(ctx, "genesis coordinator initialized",
		"aggregator", shortValidator(c.aggregator),
		"required_quorum", c.requirement,
		"config_hash", shortHash(c.cfg.ConfigHash),
		"peer_hash", shortHash(c.cfg.PeerHash))

	var restored bool
	switch c.cfg.StateMode {
	case stateModeDBOnly:
		if c.backend == nil {
			return fmt.Errorf("genesis state mode %q requires database backend", c.cfg.StateMode)
		}
		var err error
		restored, err = c.restoreFromDatabase(ctx)
		if err != nil {
			if c.cfg.DBRequired {
				return fmt.Errorf("restore genesis state from database (required): %w", err)
			}
			c.logger.WarnContext(ctx, "genesis database restore failed; proceeding with live ceremony",
				"error", err,
				"db_required", c.cfg.DBRequired)
			break
		}
		if restored {
			return nil
		}

	case stateModeDBThenDisk:
		if c.backend != nil {
			var err error
			restored, err = c.restoreFromDatabase(ctx)
			if err != nil {
				return fmt.Errorf("restore genesis state from database: %w", err)
			}
			if restored {
				return nil
			}
		}
		var err error
		restored, err = c.restoreFromDisk(ctx)
		if err != nil {
			return fmt.Errorf("restore genesis state from disk: %w", err)
		}
		if restored {
			return nil
		}

	case stateModeDiskOnly:
		var err error
		restored, err = c.restoreFromDisk(ctx)
		if err != nil {
			return fmt.Errorf("restore genesis state from disk: %w", err)
		}
		if restored {
			return nil
		}
	}

	// Start periodic rebroadcast until certificate received
	go c.rebroadcastLoop(ctx)

	return c.broadcastReady(ctx)
}

func (c *Coordinator) broadcastReady(ctx context.Context) error {
	ready, err := c.issueLocalReady(ctx, "local_broadcast")
	if err != nil {
		return err
	}
	if ready == nil {
		return nil
	}
	if err := c.host.PublishGenesisMessage(ctx, ready); err != nil {
		return fmt.Errorf("publish genesis ready: %w", err)
	}
	return nil
}

func (c *Coordinator) issueLocalReady(ctx context.Context, reason string) (*messages.GenesisReady, error) {
	ready, err := c.buildLocalReady(ctx)
	if err != nil {
		return nil, err
	}
	c.recordLocalReady(ctx, ready, reason)
	return ready, nil
}

func (c *Coordinator) buildLocalReady(ctx context.Context) (*messages.GenesisReady, error) {
	peerHash := c.cfg.PeerHash
	if c.peerHasher != nil {
		if ph, err := c.peerHasher.PeerHash(); err == nil {
			if !bytesEqual32(ph, peerHash) {
				c.logger.WarnContext(ctx, "peer hash from router differs from configured fingerprint; using configured value",
					"configured", fmt.Sprintf("%x", peerHash[:4]),
					"router", fmt.Sprintf("%x", ph[:4]))
			}
		} else {
			c.logger.WarnContext(ctx, "peer hash unavailable, using configured value", "error", err)
		}
	}
	c.mu.Lock()
	c.cfg.PeerHash = peerHash
	c.mu.Unlock()
	ts := time.Now().UTC()
	nonce := randomNonce()
	ready := &messages.GenesisReady{
		ValidatorID: c.localID,
		Timestamp:   ts,
		ConfigHash:  c.cfg.ConfigHash,
		PeerHash:    peerHash,
		Nonce:       nonce,
	}
	sigBytes, err := c.crypto.SignWithContext(ctx, ready.SignBytes())
	if err != nil {
		return nil, fmt.Errorf("sign ready attestation: %w", err)
	}
	ready.Signature = messages.Signature{
		Bytes:     sigBytes,
		KeyID:     c.localID,
		Timestamp: ts,
	}
	return ready, nil
}

func (c *Coordinator) resetRebroadcastState(hash [32]byte) {
	c.rebroadcast.mu.Lock()
	c.rebroadcast.lastHash = hash
	c.rebroadcast.current = c.rebroadcast.base
	c.rebroadcast.lastSend = time.Time{}
	c.rebroadcast.mu.Unlock()
}

func (c *Coordinator) rebroadcastInterval() time.Duration {
	c.rebroadcast.mu.Lock()
	interval := c.rebroadcast.current
	if interval <= 0 {
		interval = c.rebroadcast.base
		if interval <= 0 {
			interval = time.Second
		}
		c.rebroadcast.current = interval
	}
	c.rebroadcast.mu.Unlock()
	return interval
}

func (c *Coordinator) noteRebroadcast(hash [32]byte) {
	c.rebroadcast.mu.Lock()
	if c.rebroadcast.lastHash != hash {
		c.rebroadcast.lastHash = hash
		c.rebroadcast.current = c.rebroadcast.base
	} else if c.rebroadcast.current < c.rebroadcast.max {
		next := c.rebroadcast.current * 2
		if next > c.rebroadcast.max {
			next = c.rebroadcast.max
		}
		c.rebroadcast.current = next
	}
	c.rebroadcast.lastSend = time.Now()
	c.rebroadcast.mu.Unlock()
}

func (c *Coordinator) recordLocalReady(ctx context.Context, ready *messages.GenesisReady, label string) {
	readyHash := ready.Hash()
	nonceLabel := shortNonce(ready.Nonce)
	sigNonceLabel := shortSignatureNonce(ready.Signature.Bytes)
	ts := ready.Timestamp
	c.logger.InfoContext(ctx, "broadcasting local genesis ready",
		"validator", shortValidator(c.localID),
		"hash", shortHash(readyHash),
		"config_hash", shortHash(c.cfg.ConfigHash),
		"peer_hash", shortHash(c.cfg.PeerHash),
		"nonce", nonceLabel,
		"signature_nonce", sigNonceLabel,
		"timestamp", ts,
		"signature_timestamp", ready.Signature.Timestamp,
		"signature_key", shortValidator(ready.Signature.KeyID),
		"reason", label)
	receivedReason := fmt.Sprintf("ts=%s msg_nonce=%s sig_nonce=%s sig_ts=%s sig_key=%s %s",
		ts.UTC().Format(time.RFC3339Nano),
		nonceLabel,
		sigNonceLabel,
		ready.Signature.Timestamp.UTC().Format(time.RFC3339Nano),
		shortValidator(ready.Signature.KeyID),
		label)
	acceptedReason := fmt.Sprintf("%s msg_nonce=%s sig_nonce=%s", label, nonceLabel, sigNonceLabel)
	c.recordReadyEvent(ctx, ReadyEventReceived, c.localID, readyHash, receivedReason)

	c.readyMu.Lock()
	c.localReady = ready
	c.readyMu.Unlock()
	c.resetRebroadcastState(readyHash)

	c.mu.Lock()
	c.attestations[c.localID] = ready
	c.mu.Unlock()

	c.recordReadyEvent(ctx, ReadyEventAccepted, c.localID, readyHash, acceptedReason)

	if c.audit != nil {
		_ = c.audit.Info("genesis_ready_broadcast", map[string]interface{}{
			"validator": fmt.Sprintf("%x", c.localID[:4]),
			"timestamp": ts,
			"reason":    label,
		})
	}

	c.host.MarkValidatorReady(c.localID)
}

// OnGenesisReady handles an attestation from a peer.
func (c *Coordinator) OnGenesisReady(ctx context.Context, ready *messages.GenesisReady) {
	var (
		required        int
		audit           types.AuditLogger
		readyHash       = ready.Hash()
		verifyingMarked bool
	)

	validatorLabel := shortValidator(ready.ValidatorID)
	hashLabel := shortHash(readyHash)
	nonceLabel := shortNonce(ready.Nonce)
	sigNonceLabel := shortSignatureNonce(ready.Signature.Bytes)
	sigKeyLabel := shortValidator(ready.Signature.KeyID)
	reason := fmt.Sprintf("ts=%s msg_nonce=%s sig_nonce=%s sig_ts=%s sig_key=%s",
		ready.Timestamp.UTC().Format(time.RFC3339Nano),
		nonceLabel,
		sigNonceLabel,
		ready.Signature.Timestamp.UTC().Format(time.RFC3339Nano),
		sigKeyLabel,
	)
	c.recordReadyEvent(ctx, ReadyEventReceived, ready.ValidatorID, readyHash, reason)

	c.mu.Lock()
	if c.completed {
		c.mu.Unlock()
		return
	}

	c.logger.DebugContext(ctx, "genesis ready attestation received",
		"validator", validatorLabel,
		"hash", hashLabel,
		"timestamp", ready.Timestamp,
		"nonce", nonceLabel,
		"signature_nonce", sigNonceLabel,
		"signature_timestamp", ready.Signature.Timestamp,
		"signature_key", sigKeyLabel)

	if !c.validatorSet.IsValidator(ready.ValidatorID) {
		c.mu.Unlock()
		c.logger.WarnContext(ctx, "ignoring ready from non-validator",
			"validator", validatorLabel)
		c.recordReadyEvent(ctx, ReadyEventRejected, ready.ValidatorID, readyHash, "non_validator")
		return
	}

	if existing, ok := c.attestations[ready.ValidatorID]; ok {
		if existing.Hash() == readyHash {
			c.mu.Unlock()
			c.logger.DebugContext(ctx, "genesis ready attestation duplicate ignored",
				"validator", validatorLabel,
				"hash", hashLabel)
			c.recordReadyEvent(ctx, ReadyEventDuplicate, ready.ValidatorID, readyHash, fmt.Sprintf("already_recorded msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
			return
		}
		if !ready.Timestamp.After(existing.Timestamp) {
			c.mu.Unlock()
			c.logger.WarnContext(ctx, "stale ready attestation ignored",
				"validator", validatorLabel,
				"existing_timestamp", existing.Timestamp,
				"incoming_timestamp", ready.Timestamp)
			c.recordReadyEvent(ctx, ReadyEventDuplicate, ready.ValidatorID, readyHash, fmt.Sprintf("stale msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
			return
		}
		c.logger.InfoContext(ctx, "newer ready attestation received; replacing existing",
			"validator", validatorLabel,
			"previous_timestamp", existing.Timestamp,
			"new_timestamp", ready.Timestamp)
		delete(c.attestations, ready.ValidatorID)
	}

	if inflightHash, ok := c.verifying[ready.ValidatorID]; ok {
		if inflightHash == readyHash {
			c.mu.Unlock()
			c.logger.DebugContext(ctx, "ready attestation verification already in-flight",
				"validator", validatorLabel,
				"hash", hashLabel)
			c.recordReadyEvent(ctx, ReadyEventDuplicate, ready.ValidatorID, readyHash, fmt.Sprintf("verification_inflight msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
			return
		}
		c.mu.Unlock()
		c.logger.WarnContext(ctx, "conflicting ready attestation while verification in-flight",
			"validator", validatorLabel)
		c.recordReadyEvent(ctx, ReadyEventRejected, ready.ValidatorID, readyHash, fmt.Sprintf("conflicting_inflight msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
		return
	}

	if !bytesEqual32(ready.ConfigHash, c.cfg.ConfigHash) {
		c.mu.Unlock()
		c.logger.WarnContext(ctx, "ready with mismatched config hash rejected",
			"validator", validatorLabel)
		c.recordReadyEvent(ctx, ReadyEventRejected, ready.ValidatorID, readyHash, fmt.Sprintf("config_hash_mismatch msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
		return
	}
	if !bytesEqual32(ready.PeerHash, c.cfg.PeerHash) {
		c.mu.Unlock()
		c.logger.WarnContext(ctx, "ready with mismatched peer hash rejected",
			"validator", validatorLabel)
		c.recordReadyEvent(ctx, ReadyEventRejected, ready.ValidatorID, readyHash, fmt.Sprintf("peer_hash_mismatch msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
		return
	}

	tolerance := c.cfg.GenesisClockSkewTolerance
	if tolerance <= 0 {
		tolerance = c.cfg.ClockSkewTolerance
	}
	if skew := time.Since(ready.Timestamp); skew > tolerance || skew < -tolerance {
		c.mu.Unlock()
		c.logger.WarnContext(ctx, "ready attestation expired",
			"validator", validatorLabel,
			"skew", skew,
			"tolerance", tolerance)
		c.recordReadyEvent(ctx, ReadyEventRejected, ready.ValidatorID, readyHash, fmt.Sprintf("timestamp_skew msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
		return
	}

	c.verifying[ready.ValidatorID] = readyHash
	verifyingMarked = true
	audit = c.audit
	required = c.requirement
	c.mu.Unlock()

	defer func() {
		if verifyingMarked {
			c.mu.Lock()
			delete(c.verifying, ready.ValidatorID)
			c.mu.Unlock()
		}
	}()

	info, err := c.validatorSet.GetValidator(ready.ValidatorID)
	if err != nil || info == nil {
		c.logger.WarnContext(ctx, "ready attestation validator lookup failed",
			"validator", validatorLabel,
			"error", err)
		c.recordReadyEvent(ctx, ReadyEventRejected, ready.ValidatorID, readyHash, fmt.Sprintf("validator_lookup_failed msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
		return
	}

	if ready.Signature.KeyID != ready.ValidatorID {
		c.logger.WarnContext(ctx, "ready attestation signature key mismatch",
			"validator", validatorLabel)
		c.recordReadyEvent(ctx, ReadyEventRejected, ready.ValidatorID, readyHash, fmt.Sprintf("signature_key_mismatch msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
		return
	}

	c.logger.DebugContext(ctx, "verifying genesis ready attestation",
		"validator", validatorLabel,
		"hash", hashLabel)

	if err := c.crypto.VerifyWithContext(ctx, ready.SignBytes(), ready.Signature.Bytes, info.PublicKey, false); err != nil {
		c.logger.WarnContext(ctx, "ready attestation signature verification failed",
			"validator", validatorLabel,
			"error", err)
		// Replay detection inside crypto service will surface as verify error; record reason for observability.
		c.recordReadyEvent(ctx, ReadyEventReplayBlocked, ready.ValidatorID, readyHash, fmt.Sprintf("%s msg_nonce=%s sig_nonce=%s", err.Error(), nonceLabel, sigNonceLabel))
		return
	}

	c.mu.Lock()
	if c.completed {
		c.mu.Unlock()
		return
	}
	if existing, ok := c.attestations[ready.ValidatorID]; ok {
		if existing.Hash() == readyHash {
			c.mu.Unlock()
			c.logger.DebugContext(ctx, "ready attestation already recorded after verification",
				"validator", validatorLabel,
				"hash", hashLabel)
			c.recordReadyEvent(ctx, ReadyEventDuplicate, ready.ValidatorID, readyHash, fmt.Sprintf("already_recorded_post_verify msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
			return
		}
		c.mu.Unlock()
		c.logger.WarnContext(ctx, "duplicate conflicting ready attestation",
			"validator", validatorLabel)
		c.recordReadyEvent(ctx, ReadyEventRejected, ready.ValidatorID, readyHash, fmt.Sprintf("conflicting_post_verify msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
		return
	}

	c.attestations[ready.ValidatorID] = ready
	count := len(c.attestations)
	c.mu.Unlock()

	if audit != nil {
		_ = audit.Info("genesis_ready_received", map[string]interface{}{
			"validator": validatorLabel,
			"count":     count,
			"required":  required,
		})
	}

	c.logger.InfoContext(ctx, "genesis ready attestation accepted",
		"validator", validatorLabel,
		"hash", hashLabel,
		"count", count,
		"required", required,
		"nonce", nonceLabel,
		"signature_nonce", sigNonceLabel)
	c.recordReadyEvent(ctx, ReadyEventAccepted, ready.ValidatorID, readyHash, fmt.Sprintf("msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))

	c.host.MarkValidatorReady(ready.ValidatorID)

	c.mu.Lock()
	if c.completed {
		c.mu.Unlock()
		return
	}
	c.logger.InfoContext(ctx, "[GENESIS] checking quorum after acceptance",
		"total_attestations", len(c.attestations),
		"required", c.requirement,
		"am_aggregator", c.aggregator == c.localID)
	cert := c.checkQuorumLocked(ctx)
	c.mu.Unlock()

	if cert != nil {
		go c.publishCertificate(ctx, cert)
		c.setCertificate(ctx, cert)
	}
}

// OnGenesisCertificate processes a quorum certificate from the aggregator.
func (c *Coordinator) OnGenesisCertificate(ctx context.Context, cert *messages.GenesisCertificate) {
	c.mu.Lock()
	if c.completed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	if err := c.validateCertificate(ctx, cert, false); err != nil {
		c.logger.ErrorContext(ctx, "invalid genesis certificate", "error", err)
		return
	}

	c.setCertificate(ctx, cert)
}

// WaitForCertificate blocks until the ceremony completes or context times out.
func (c *Coordinator) WaitForCertificate(ctx context.Context) (*messages.GenesisCertificate, error) {
	timeout := c.cfg.CertificateTimeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	for {
		select {
		case <-c.doneCh:
			c.mu.Lock()
			cert := c.certificate
			c.mu.Unlock()
			if cert == nil {
				return nil, errors.New("genesis ceremony completed without certificate")
			}
			return cert, nil
		case <-time.After(timeout):
			c.logger.WarnContext(ctx, "genesis certificate still pending; continuing to wait",
				"timeout", timeout)
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *Coordinator) checkQuorumLocked(ctx context.Context) *messages.GenesisCertificate {
	if c.completed || c.aggregator != c.localID || c.certificateBroadcast {
		return nil
	}

	current := len(c.attestations)
	if current < c.requirement {
		// Log current state to trace missing attestations
		validators := make([]string, 0, current)
		for vid := range c.attestations {
			validators = append(validators, shortValidator(vid))
		}
		c.logger.InfoContext(ctx, "[GENESIS] quorum check - waiting for more attestations",
			"current", current,
			"required", c.requirement,
			"have_validators", validators,
			"aggregator", shortValidator(c.localID))
		return nil
	}

	att := make([]*messages.GenesisReady, 0, len(c.attestations))
	for _, v := range c.attestations {
		att = append(att, v)
	}

	cert, err := c.buildCertificate(ctx, att)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to build genesis certificate", "error", err)
		return nil
	}

	c.certificateBroadcast = true
	return cert
}

func (c *Coordinator) buildCertificate(ctx context.Context, attestations []*messages.GenesisReady) (*messages.GenesisCertificate, error) {
	if len(attestations) == 0 {
		return nil, fmt.Errorf("no attestations to aggregate")
	}
	c.logger.InfoContext(ctx, "building genesis certificate",
		"attestations", len(attestations),
		"aggregator", shortValidator(c.localID),
		"required_quorum", c.requirement)
	c.recordReadyEvent(ctx, ReadyEventReceived, c.localID, [32]byte{}, fmt.Sprintf("aggregate=%d aggregator=%s", len(attestations), shortValidator(c.localID)))
	// Sort attestations for deterministic ordering
	sort.Slice(attestations, func(i, j int) bool {
		return lessValidator(attestations[i].ValidatorID, attestations[j].ValidatorID)
	})

	pruned := make([]messages.GenesisReady, 0, len(attestations))
	seen := make(map[types.ValidatorID]struct{}, len(attestations))
	for _, att := range attestations {
		if _, ok := seen[att.ValidatorID]; ok {
			continue
		}
		pruned = append(pruned, *att)
		seen[att.ValidatorID] = struct{}{}
	}

	cert := &messages.GenesisCertificate{
		Attestations: pruned,
		Aggregator:   c.localID,
		Timestamp:    time.Now().UTC(),
	}
	sigBytes, err := c.crypto.SignWithContext(ctx, cert.SignBytes())
	if err != nil {
		return nil, fmt.Errorf("sign certificate: %w", err)
	}
	cert.Signature = messages.Signature{
		Bytes:     sigBytes,
		KeyID:     c.localID,
		Timestamp: cert.Timestamp,
	}
	return cert, nil
}

func (c *Coordinator) publishCertificate(ctx context.Context, cert *messages.GenesisCertificate) {
	c.logger.InfoContext(ctx, "broadcasting genesis certificate",
		"attestations", len(cert.Attestations),
		"aggregator", shortValidator(cert.Aggregator))
	if err := c.host.PublishGenesisMessage(ctx, cert); err != nil {
		c.logger.ErrorContext(ctx, "failed to publish genesis certificate", "error", err)
	}
}

func (c *Coordinator) validateCertificate(ctx context.Context, cert *messages.GenesisCertificate, fromDisk bool) error {
	// allowOldTimestamps relaxes freshness checks for persisted data.
	// NOTE: This must NEVER relax signature verification.
	allowOldTimestamps := fromDisk
	if cert == nil {
		return fmt.Errorf("nil certificate")
	}
	c.mu.Lock()
	if c.aggregator == (types.ValidatorID{}) {
		c.aggregator = cert.Aggregator
	}
	aggregator := c.aggregator
	c.mu.Unlock()
	if cert.Aggregator != aggregator {
		if fromDisk {
			c.logger.WarnContext(ctx, "persisted genesis certificate aggregator differs from rotation; adopting stored aggregator",
				"stored", shortValidator(cert.Aggregator),
				"expected", shortValidator(aggregator))
			c.mu.Lock()
			c.aggregator = cert.Aggregator
			aggregator = cert.Aggregator
			c.mu.Unlock()
		} else {
			return fmt.Errorf("unexpected aggregator %x (expected %x)", cert.Aggregator[:4], aggregator[:4])
		}
	}
	if len(cert.Attestations) < c.requirement {
		return fmt.Errorf("certificate attestation count %d below quorum %d", len(cert.Attestations), c.requirement)
	}

	tolerance := c.cfg.GenesisClockSkewTolerance
	if tolerance <= 0 {
		tolerance = c.cfg.ClockSkewTolerance
	}
	if !allowOldTimestamps {
		if skew := time.Since(cert.Timestamp); skew > tolerance || skew < -tolerance {
			return fmt.Errorf("certificate timestamp skew %s exceeds tolerance %s", skew, tolerance)
		}
	}

	c.logger.InfoContext(ctx, "validating genesis certificate",
		"attestations", len(cert.Attestations),
		"aggregator", shortValidator(cert.Aggregator))

	seen := make(map[types.ValidatorID]struct{}, len(cert.Attestations))
	for i := range cert.Attestations {
		att := cert.Attestations[i]
		if !c.validatorSet.IsValidator(att.ValidatorID) {
			return fmt.Errorf("certificate contains non-validator %x", att.ValidatorID[:4])
		}
		if _, ok := seen[att.ValidatorID]; ok {
			return fmt.Errorf("duplicate attestation for %x", att.ValidatorID[:4])
		}
		seen[att.ValidatorID] = struct{}{}
		if !bytesEqual32(att.ConfigHash, c.cfg.ConfigHash) {
			return fmt.Errorf("attestation config hash mismatch for %x", att.ValidatorID[:4])
		}
		if !bytesEqual32(att.PeerHash, c.cfg.PeerHash) {
			return fmt.Errorf("attestation peer hash mismatch for %x", att.ValidatorID[:4])
		}
		if !allowOldTimestamps {
			if skew := time.Since(att.Timestamp); skew > tolerance || skew < -tolerance {
				return fmt.Errorf("attestation timestamp skew %s exceeds tolerance %s for %x", skew, tolerance, att.ValidatorID[:4])
			}
		}

		attHash := att.Hash()
		validatorLabel := shortValidator(att.ValidatorID)
		hashLabel := shortHash(attHash)
		nonceLabel := shortNonce(att.Nonce)
		sigNonceLabel := shortSignatureNonce(att.Signature.Bytes)
		sigKeyLabel := shortValidator(att.Signature.KeyID)
		reason := fmt.Sprintf("ts=%s msg_nonce=%s sig_nonce=%s sig_ts=%s sig_key=%s certificate",
			att.Timestamp.UTC().Format(time.RFC3339Nano),
			nonceLabel,
			sigNonceLabel,
			att.Signature.Timestamp.UTC().Format(time.RFC3339Nano),
			sigKeyLabel,
		)

		c.mu.Lock()
		existing, ok := c.attestations[att.ValidatorID]
		if ok && existing.Hash() == attHash {
			c.mu.Unlock()
			c.logger.DebugContext(ctx, "certificate attestation already trusted",
				"validator", validatorLabel,
				"hash", hashLabel,
				"signature_nonce", sigNonceLabel)
			c.recordReadyEvent(ctx, ReadyEventDuplicate, att.ValidatorID, attHash, fmt.Sprintf("cert_already_trusted msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
			continue
		}
		if inflight, inflightOk := c.verifying[att.ValidatorID]; inflightOk {
			if inflight == attHash {
				c.mu.Unlock()
				c.logger.DebugContext(ctx, "certificate attestation verification already in-flight",
					"validator", validatorLabel,
					"hash", hashLabel,
					"nonce", nonceLabel,
					"signature_nonce", sigNonceLabel)
				c.recordReadyEvent(ctx, ReadyEventDuplicate, att.ValidatorID, attHash, fmt.Sprintf("cert_verification_inflight msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
				continue
			}
			c.mu.Unlock()
			c.recordReadyEvent(ctx, ReadyEventRejected, att.ValidatorID, attHash, fmt.Sprintf("cert_conflicting_inflight msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
			return fmt.Errorf("conflicting attestation for %x during certificate verification", att.ValidatorID[:4])
		}
		c.verifying[att.ValidatorID] = attHash
		c.mu.Unlock()

		c.logger.DebugContext(ctx, "verifying certificate attestation",
			"validator", validatorLabel,
			"hash", hashLabel,
			"nonce", nonceLabel,
			"signature_nonce", sigNonceLabel,
			"signature_key", sigKeyLabel)
		c.recordReadyEvent(ctx, ReadyEventReceived, att.ValidatorID, attHash, reason)

		info, err := c.validatorSet.GetValidator(att.ValidatorID)
		if err != nil || info == nil {
			c.mu.Lock()
			delete(c.verifying, att.ValidatorID)
			c.mu.Unlock()
			c.recordReadyEvent(ctx, ReadyEventRejected, att.ValidatorID, attHash, fmt.Sprintf("cert_validator_lookup_failed msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
			return fmt.Errorf("fetch validator info for %x: %w", att.ValidatorID[:4], err)
		}
		if att.Signature.KeyID != att.ValidatorID {
			c.mu.Lock()
			delete(c.verifying, att.ValidatorID)
			c.mu.Unlock()
			c.recordReadyEvent(ctx, ReadyEventRejected, att.ValidatorID, attHash, fmt.Sprintf("cert_key_mismatch msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
			return fmt.Errorf("attestation signature key mismatch for %x", att.ValidatorID[:4])
		}
		verifyErr := c.crypto.VerifyWithContext(ctx, att.SignBytes(), att.Signature.Bytes, info.PublicKey, allowOldTimestamps)
		if verifyErr != nil {
			c.mu.Lock()
			delete(c.verifying, att.ValidatorID)
			c.mu.Unlock()
			c.recordReadyEvent(ctx, ReadyEventReplayBlocked, att.ValidatorID, attHash, fmt.Sprintf("%s msg_nonce=%s sig_nonce=%s", verifyErr.Error(), nonceLabel, sigNonceLabel))
			return fmt.Errorf("verify attestation signature for %x: %w", att.ValidatorID[:4], verifyErr)
		} else {
			c.mu.Lock()
			delete(c.verifying, att.ValidatorID)
			c.mu.Unlock()
		}

		attClone := att
		c.mu.Lock()
		c.attestations[att.ValidatorID] = &attClone
		c.mu.Unlock()

		c.logger.InfoContext(ctx, "certificate attestation verified",
			"validator", validatorLabel,
			"hash", hashLabel,
			"nonce", nonceLabel,
			"signature_nonce", sigNonceLabel)
		c.recordReadyEvent(ctx, ReadyEventAccepted, att.ValidatorID, attHash, fmt.Sprintf("certificate msg_nonce=%s sig_nonce=%s", nonceLabel, sigNonceLabel))
	}

	// Verify the certificate signature itself (aggregator signature).
	aggInfo, err := c.validatorSet.GetValidator(cert.Aggregator)
	if err != nil || aggInfo == nil {
		return fmt.Errorf("fetch aggregator info for %x: %w", cert.Aggregator[:4], err)
	}
	if cert.Signature.KeyID != cert.Aggregator {
		return fmt.Errorf("certificate signature key mismatch for %x", cert.Aggregator[:4])
	}
	if err := c.crypto.VerifyWithContext(ctx, cert.SignBytes(), cert.Signature.Bytes, aggInfo.PublicKey, allowOldTimestamps); err != nil {
		return fmt.Errorf("verify certificate signature for %x: %w", cert.Aggregator[:4], err)
	}
	return nil
}

func (c *Coordinator) setCertificate(ctx context.Context, cert *messages.GenesisCertificate) {
	c.certificateOnce.Do(func() {
		c.mu.Lock()
		c.certificate = cert
		c.completed = true
		close(c.doneCh)
		c.mu.Unlock()

		c.persistCertificate(ctx, cert)

		c.logger.InfoContext(ctx, "genesis ceremony complete",
			"attestations", len(cert.Attestations),
			"aggregator", shortValidator(cert.Aggregator))

		for i := range cert.Attestations {
			c.host.MarkValidatorReady(cert.Attestations[i].ValidatorID)
		}

		if c.validatorSet != nil {
			validators := c.validatorSet.GetValidators()
			for i := range validators {
				c.host.MarkValidatorReady(validators[i].ID)
			}
		}

		if c.audit != nil {
			_ = c.audit.Info("genesis_certificate_received", map[string]interface{}{
				"aggregator":   fmt.Sprintf("%x", cert.Aggregator[:4]),
				"attestations": len(cert.Attestations),
			})
		}

		if err := c.host.OnGenesisCertificate(ctx, cert); err != nil {
			c.logger.ErrorContext(ctx, "failed to activate consensus after genesis", "error", err)
		}
	})
}

// Certificate returns the finalized genesis certificate (nil if not completed).
func (c *Coordinator) Certificate() *messages.GenesisCertificate {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.certificate
}

func randomNonce() [32]byte {
	var out [32]byte
	_, _ = rand.Read(out[:])
	return out
}

// rebroadcastLoop periodically rebroadcasts GenesisReady until certificate is received
// This ensures late-joining aggregators don't miss ephemeral gossip messages
func (c *Coordinator) rebroadcastLoop(ctx context.Context) {
	refresh := c.cfg.ReadyRefreshInterval
	for {
		interval := c.rebroadcastInterval()
		timer := time.NewTimer(interval)
		select {
		case <-c.doneCh:
			timer.Stop()
			return
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			c.readyMu.Lock()
			ready := c.localReady
			c.readyMu.Unlock()
			c.mu.Lock()
			completed := c.completed
			c.mu.Unlock()

			if completed {
				return
			}
			if ready == nil {
				continue
			}

			if refresh > 0 && time.Since(ready.Timestamp) >= refresh {
				c.logger.InfoContext(ctx, "refresh timer triggered; issuing new genesis ready",
					"validator", shortValidator(c.localID),
					"age", time.Since(ready.Timestamp))
				newReady, err := c.issueLocalReady(ctx, "refresh_timer")
				if err != nil {
					c.logger.ErrorContext(ctx, "failed to refresh genesis ready", "error", err)
					continue
				}
				ready = newReady
			}

			hash := ready.Hash()
			if err := c.host.PublishGenesisMessage(ctx, ready); err != nil {
				c.logger.WarnContext(ctx, "[GENESIS] rebroadcast failed", "error", err)
			} else {
				c.noteRebroadcast(hash)
				c.logger.InfoContext(ctx, "[GENESIS] rebroadcast successful",
					"validator", shortValidator(c.localID),
					"attempt", "periodic",
					"interval", interval)
			}
		}
	}
}

func (c *Coordinator) restoreFromDisk(ctx context.Context) (bool, error) {
	if c.cfg.StatePath == "" {
		return false, nil
	}
	data, err := os.ReadFile(c.cfg.StatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read persisted genesis state: %w", err)
	}
	var state persistedGenesisState
	if err := json.Unmarshal(data, &state); err != nil {
		return false, fmt.Errorf("decode persisted genesis state: %w", err)
	}
	if state.Certificate == nil {
		return false, fmt.Errorf("persisted genesis state missing certificate")
	}
	if err := c.validateCertificate(ctx, state.Certificate, true); err != nil {
		if c.logger != nil {
			c.logger.WarnContext(ctx, "discarding persisted genesis certificate from disk",
				"error", err,
				"path", c.cfg.StatePath)
		}
		if remErr := os.Remove(c.cfg.StatePath); remErr != nil && !errors.Is(remErr, os.ErrNotExist) {
			if c.logger != nil {
				c.logger.WarnContext(ctx, "failed to remove stale genesis state file", "error", remErr, "path", c.cfg.StatePath)
			}
		}
		if c.host != nil {
			c.host.RecordGenesisCertificateDiscard(ctx)
		}
		return false, nil
	}
	meta := map[string]interface{}{
		"path":     c.cfg.StatePath,
		"saved_at": state.SavedAt,
	}
	c.applyRestoredCertificate(ctx, state.Certificate, "disk", meta)
	return true, nil
}

func (c *Coordinator) restoreFromDatabase(ctx context.Context) (bool, error) {
	if c.backend == nil {
		return false, nil
	}
	data, found, err := c.backend.LoadGenesisCertificate(ctx, c.cfg.NetworkID, c.cfg.ConfigHash, c.cfg.PeerHash)
	if err != nil {
		return false, fmt.Errorf("load persisted genesis certificate: %w", err)
	}
	if !found || len(data) == 0 {
		return false, nil
	}
	var cert messages.GenesisCertificate
	if err := cbor.Unmarshal(data, &cert); err != nil {
		return false, fmt.Errorf("decode persisted genesis certificate: %w", err)
	}
	if err := c.validateCertificate(ctx, &cert, true); err != nil {
		c.handleInvalidPersistedCertificate(ctx, err, &cert)
		return false, nil
	}
	c.applyRestoredCertificate(ctx, &cert, "cockroachdb", nil)
	return true, nil
}

func (c *Coordinator) applyRestoredCertificate(ctx context.Context, cert *messages.GenesisCertificate, source string, meta map[string]interface{}) {
	if cert == nil {
		return
	}
	c.mu.Lock()
	c.attestations = make(map[types.ValidatorID]*messages.GenesisReady, len(cert.Attestations))
	for i := range cert.Attestations {
		attCopy := cert.Attestations[i]
		attPtr := &attCopy
		c.attestations[attCopy.ValidatorID] = attPtr
		if attCopy.ValidatorID == c.localID {
			c.readyMu.Lock()
			c.localReady = attPtr
			c.readyMu.Unlock()
		}
	}
	c.mu.Unlock()

	if c.logger != nil {
		fields := []interface{}{
			"source", source,
			"attestations", len(cert.Attestations),
		}
		if meta != nil {
			for k, v := range meta {
				fields = append(fields, k, v)
			}
		}
		c.logger.InfoContext(ctx, "restored genesis certificate", fields...)
	}

	c.setCertificate(ctx, cert)

	c.readyMu.Lock()
	readyCopy := c.localReady
	c.readyMu.Unlock()
	if readyCopy != nil && c.host != nil {
		if err := c.host.PublishGenesisMessage(ctx, readyCopy); err != nil {
			c.logger.WarnContext(ctx, "failed to rebroadcast restored genesis ready", "error", err)
		} else {
			hash := readyCopy.Hash()
			c.resetRebroadcastState(hash)
			c.logger.InfoContext(ctx, "rebroadcast restored genesis ready",
				"validator", shortValidator(readyCopy.ValidatorID),
				"hash", shortHash(hash))
		}
	}
	if c.aggregator == c.localID {
		go c.publishCertificate(ctx, cert)
	}
}

func (c *Coordinator) handleInvalidPersistedCertificate(ctx context.Context, cause error, cert *messages.GenesisCertificate) {
	count := c.discardCount.Add(1)
	if c.logger != nil {
		fields := []interface{}{
			"reason", cause.Error(),
			"discard_count", count,
		}
		if cert != nil {
			fields = append(fields,
				"aggregator", shortValidator(cert.Aggregator),
				"attestations", len(cert.Attestations),
			)
		}
		c.logger.WarnContext(ctx, "discarding persisted genesis certificate", fields...)
	}
}

func (c *Coordinator) persistCertificate(ctx context.Context, cert *messages.GenesisCertificate) {
	if cert == nil {
		return
	}

	// Single-writer semantics: only the genesis aggregator should persist to the shared database.
	if c.backend != nil && c.aggregator == c.localID {
		bytes, err := cbor.Marshal(cert)
		if err != nil {
			c.logger.WarnContext(ctx, "failed to marshal genesis certificate for database persistence", "error", err)
			return
		}
		if err := c.backend.SaveGenesisCertificate(ctx, c.cfg.NetworkID, c.cfg.ConfigHash, c.cfg.PeerHash, bytes); err != nil {
			c.logger.WarnContext(ctx, "failed to persist genesis certificate to database", "error", err)
			return
		}
	}

	// In db_only mode, never persist local disk state (avoid per-node divergence).
	if c.cfg.StateMode == stateModeDBOnly {
		c.logger.InfoContext(ctx, "skipping disk persistence for genesis certificate (db_only mode)")
		return
	}

	if c.cfg.StatePath != "" {
		dir := filepath.Dir(c.cfg.StatePath)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				c.logger.ErrorContext(ctx, "failed to create genesis state directory", "error", err, "path", dir)
				return
			}
		}
		state := persistedGenesisState{
			Certificate: cert,
			SavedAt:     time.Now().UTC(),
		}
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to encode persisted genesis state", "error", err)
			return
		}
		tmpPath := c.cfg.StatePath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
			c.logger.ErrorContext(ctx, "failed to write temporary genesis state", "error", err, "path", tmpPath)
			_ = os.Remove(tmpPath)
			return
		}
		if err := os.Rename(tmpPath, c.cfg.StatePath); err != nil {
			c.logger.ErrorContext(ctx, "failed to finalize genesis state file", "error", err, "from", tmpPath, "to", c.cfg.StatePath)
			_ = os.Remove(tmpPath)
			return
		}
	}

	c.logger.InfoContext(ctx, "persisted genesis certificate",
		"path", c.cfg.StatePath,
		"attestations", len(cert.Attestations))

}

func (c *Coordinator) recordReadyEvent(ctx context.Context, event ReadyEvent, id types.ValidatorID, hash [32]byte, reason string) {
	if c.host != nil {
		c.host.RecordGenesisReady(ctx, event, id, hash, reason)
	}

	fields := []interface{}{
		"event", string(event),
		"validator", shortValidator(id),
	}

	var zeroHash [32]byte
	if hash != zeroHash {
		fields = append(fields, "hash", shortHash(hash))
	}
	if reason != "" {
		fields = append(fields, "reason", reason)
	}

	if c.logger != nil {
		c.logger.InfoContext(ctx, "genesis ready telemetry", fields...)
	}
}

func shortValidator(id types.ValidatorID) string {
	return fmt.Sprintf("%x", id[:4])
}

func shortHash(hash [32]byte) string {
	return fmt.Sprintf("%x", hash[:4])
}

func shortNonce(nonce [32]byte) string {
	return fmt.Sprintf("%x", nonce[:4])
}

func shortSignatureNonce(sig []byte) string {
	offset := utils.KeyVersionSize + utils.TimestampSize
	if len(sig) < offset {
		return "short"
	}
	if len(sig) < offset+utils.NonceSize {
		return "none"
	}
	return fmt.Sprintf("%x", sig[offset:offset+4])
}

func lessValidator(a, b types.ValidatorID) bool {
	return bytes.Compare(a[:], b[:]) < 0
}

func bytesEqual32(a, b [32]byte) bool {
	return a == b
}
