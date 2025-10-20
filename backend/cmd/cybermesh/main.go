package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"backend/pkg/block"
	"backend/pkg/config"
	"backend/pkg/consensus/api"
	"backend/pkg/consensus/genesis"
	ctypes "backend/pkg/consensus/types"
	"backend/pkg/ingest/kafka"
	"backend/pkg/mempool"
	"backend/pkg/p2p"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
	"backend/pkg/wiring"
)

type validatorSet struct {
	index   map[ctypes.ValidatorID]ctypes.ValidatorInfo
	ordered []ctypes.ValidatorInfo
}

func newValidatorSet(infos []ctypes.ValidatorInfo) *validatorSet {
	idx := make(map[ctypes.ValidatorID]ctypes.ValidatorInfo, len(infos))
	ord := make([]ctypes.ValidatorInfo, 0, len(infos))
	for _, info := range infos {
		idx[info.ID] = info
		ord = append(ord, info)
	}
	return &validatorSet{index: idx, ordered: ord}
}

func (s *validatorSet) IsValidator(id ctypes.ValidatorID) bool {
	_, ok := s.index[id]
	return ok
}

func (s *validatorSet) GetValidator(id ctypes.ValidatorID) (*ctypes.ValidatorInfo, error) {
	if v, ok := s.index[id]; ok {
		copy := v
		return &copy, nil
	}
	return nil, fmt.Errorf("validator not found")
}

func (s *validatorSet) GetValidators() []ctypes.ValidatorInfo {
	out := make([]ctypes.ValidatorInfo, len(s.ordered))
	copy(out, s.ordered)
	return out
}

func (s *validatorSet) GetValidatorCount() int {
	return len(s.ordered)
}

func (s *validatorSet) IsActive(id ctypes.ValidatorID) bool {
	return s.IsValidator(id)
}

func (s *validatorSet) IsActiveInView(id ctypes.ValidatorID, _ uint64) bool {
	return s.IsValidator(id)
}

func main() {
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("CyberMesh Backend - Full Wiring Bootstrap")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println()

	// Try multiple .env paths (Load doesn't overwrite existing env vars)
	envPaths := []string{".env", "../../../.env", "../../.env", "../.env"}
	envLoaded := false
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			envLoaded = true
			fmt.Printf("[INFO] Loaded environment from: %s\n", path)
			break
		}
	}
	if !envLoaded {
		fmt.Println("[WARN] .env not found or failed to load; continuing with environment variables")
	}

	cfgMgr, err := utils.NewConfigManager(&utils.ConfigManagerConfig{
		SensitiveKeys: []string{
			"jwt_secret",
			"kafka_sasl_password",
			"db_dsn",
			"encryption_key",
			"secret_key",
			"salt",
		},
		RedactMode: utils.RedactFull,
	})
	if err != nil {
		log.Fatalf("config manager init failed: %v", err)
	}

	if err := config.InitializeTopology(); err != nil {
		log.Fatalf("topology initialization failed: %v", err)
	}

	nodeID, err := resolveLocalNodeID(cfgMgr)
	if err != nil {
		log.Fatalf("resolve node id failed: %v", err)
	}

	logger, err := utils.NewLogger(&utils.LogConfig{
		Level:       cfgMgr.GetString("LOG_LEVEL", "info"),
		Development: cfgMgr.GetBool("LOG_DEVELOPMENT", false),
	})
	if err != nil {
		log.Fatalf("logger init failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cryptoSvc, err := buildCryptoService(cfgMgr, logger)
	if err != nil {
		log.Fatalf("crypto service init failed: %v", err)
	}
	defer func() {
		if shutdownErr := cryptoSvc.Shutdown(); shutdownErr != nil {
			logger.Error("crypto shutdown failed", utils.ZapError(shutdownErr))
		}
	}()

	allowlist, err := utils.NewIPAllowlist(utils.DefaultIPAllowlistConfig())
	if err != nil {
		log.Fatalf("ip allowlist init failed: %v", err)
	}

	// Create logger adapter now; crypto adapter will be built after validator set is loaded
	loggerAdapter := api.NewLoggerAdapter(logger)
	allowlistAdapter := api.NewIPAllowlistAdapter(allowlist)

	localPublicKey, err := cryptoSvc.GetPublicKey()
	if err != nil {
		log.Fatalf("fetch local public key failed: %v", err)
	}

	validatorInfos, err := loadValidatorInfos(cfgMgr, config.ConsensusNodes, nodeID, localPublicKey)
	if err != nil {
		log.Fatalf("load validator infos failed: %v", err)
	}

	vSet := newValidatorSet(validatorInfos)
	// Build crypto adapter with validator registry so verification can use peers' public keys (fixes BUG-032)
	cryptoAdapter := api.NewCryptoAdapter(cryptoSvc, vSet)
	localValidatorID := deriveValidatorID(localPublicKey)
	if !vSet.IsValidator(localValidatorID) {
		log.Fatalf("local validator id %x is not present in consensus set", localValidatorID[:8])
	}

	store := state.NewMemStore()

	memCfg := mempool.Config{
		MaxTxs:        cfgMgr.GetInt("MEMPOOL_MAX_TXS", 1000),
		MaxBytes:      cfgMgr.GetInt("MEMPOOL_MAX_BYTES", 10*1024*1024),
		NonceTTL:      cfgMgr.GetDuration("MEMPOOL_NONCE_TTL", 15*time.Minute),
		Skew:          cfgMgr.GetDuration("MEMPOOL_SKEW_TOLERANCE", 5*time.Minute),
		RatePerSecond: cfgMgr.GetInt("MEMPOOL_RATE_PER_SECOND", 1000),
	}
	mp := mempool.New(memCfg, logger)

	// Initialize audit logger for production
	auditConfig := utils.DefaultAuditConfig()
	auditConfig.FilePath = cfgMgr.GetString("AUDIT_LOG_PATH", "./logs/audit.log")
	auditConfig.NodeID = fmt.Sprintf("validator-%d", cfgMgr.GetInt("NODE_ID", 1))
	auditConfig.Component = "consensus"
	auditConfig.EnableSigning = cfgMgr.GetBool("AUDIT_ENABLE_SIGNING", true)
	if signingKey := cfgMgr.GetString("AUDIT_SIGNING_KEY", ""); signingKey != "" && len(signingKey) >= 32 {
		auditConfig.SigningKey = []byte(signingKey)
	}

	auditLogger, err := utils.NewAuditLogger(auditConfig)
	var auditAdapter *utils.AuditLoggerAdapter
	if err != nil {
		logger.Warn("audit logger init failed, continuing without audit logging", utils.ZapError(err))
		auditLogger = nil
		auditAdapter = nil
	} else {
		logger.Info("audit logger initialized", utils.ZapString("path", auditConfig.FilePath))
		auditAdapter = utils.NewAuditLoggerAdapter(auditLogger)
	}

	blockCfg := block.Config{
		MaxTxsPerBlock: cfgMgr.GetInt("BLOCK_MAX_TXS", 500),
		MaxBlockBytes:  cfgMgr.GetInt("BLOCK_MAX_BYTES", 4*1024*1024),
		MinMempoolTxs:  cfgMgr.GetInt("BLOCK_MIN_MEMPOOL_TXS", 1),
		BuildInterval:  cfgMgr.GetDuration("BLOCK_BUILD_INTERVAL", 500*time.Millisecond),
	}
	builder := block.NewBuilder(blockCfg, mp, store, logger)

	engineConfig := &api.EngineConfig{
		NodeID:              localValidatorID,
		NodeType:            "validator",
		EnableProposing:     true,
		EnableVoting:        true,
		BlockTimeout:        cfgMgr.GetDuration("CONSENSUS_BLOCK_TIMEOUT", 5*time.Second),
		MetricsEnabled:      cfgMgr.GetBool("CONSENSUS_METRICS_ENABLED", true),
		HealthCheckInterval: cfgMgr.GetDuration("CONSENSUS_HEALTH_CHECK_INTERVAL", 10*time.Second),
		// Topics: nil - use default subtopics (consensus/proposal, consensus/heartbeat, etc.)
	}

	consensusEngine, err := api.NewConsensusEngine(
		cryptoAdapter,
		auditAdapter,
		loggerAdapter,
		cfgMgr,
		allowlistAdapter,
		vSet,
		engineConfig,
		nil, // genesisBackend will be set later after persistence worker is initialized
	)
	if err != nil {
		log.Fatalf("consensus engine init failed: %v", err)
	}

	wiringConfig, dbConn, dbAdapter, p2pRouter, err := buildWiringConfig(ctx, cfgMgr, logger, auditLogger)
	if err != nil {
		log.Fatalf("wiring config init failed: %v", err)
	}
	defer func() {
		if p2pRouter != nil {
			if closeErr := p2pRouter.Close(); closeErr != nil {
				logger.Warn("p2p router close failed", utils.ZapError(closeErr))
			}
		}
		if dbAdapter != nil {
			if closeErr := dbAdapter.Close(); closeErr != nil {
				logger.Warn("cockroach adapter close failed", utils.ZapError(closeErr))
			}
		}
		if dbConn != nil {
			if closeErr := dbConn.Close(); closeErr != nil {
				logger.Warn("cockroach connection close failed", utils.ZapError(closeErr))
			}
		}
	}()

	service, err := wiring.NewService(wiringConfig, consensusEngine, mp, builder, store, logger)
	if err != nil {
		log.Fatalf("wiring service init failed: %v", err)
	}

	// Ensure the engine has a peer observer for activation gating in multi-node setups.
	// The wiring service will also set this when P2P is enabled; this early wiring is safe and idempotent.
	if p2pRouter != nil {
		consensusEngine.SetPeerObserver(p2pRouter)
	}

	validatorsSnapshot := vSet.GetValidators()
	configHash := computeGenesisConfigHash(engineConfig, cfgMgr, validatorsSnapshot)
	peerHash := computeValidatorSetHash(validatorsSnapshot)
	statePath := cfgMgr.GetString("GENESIS_STATE_PATH", "/app/data/genesis_state.json")
	if statePath != "" {
		statePath = filepath.Clean(statePath)
	}
	runtimeSkew := cfgMgr.GetDuration("CONSENSUS_CLOCK_SKEW_TOLERANCE", 5*time.Second)
	if runtimeSkew <= 0 {
		runtimeSkew = 5 * time.Second
	}
	genesisSkew := cfgMgr.GetDuration("CONSENSUS_GENESIS_CLOCK_SKEW_TOLERANCE", 0)
	if genesisSkew <= 0 {
		genesisSkew = cfgMgr.GetDuration("GENESIS_CLOCK_SKEW_TOLERANCE", 15*time.Minute)
	}
	genesisCfg := genesis.Config{
		ReadyTimeout:              cfgMgr.GetDuration("GENESIS_READY_TIMEOUT", 30*time.Second),
		CertificateTimeout:        cfgMgr.GetDuration("GENESIS_CERTIFICATE_TIMEOUT", 60*time.Second),
		ClockSkewTolerance:        runtimeSkew,
		GenesisClockSkewTolerance: genesisSkew,
		ReadyRefreshInterval:      cfgMgr.GetDuration("GENESIS_READY_REFRESH_INTERVAL", 30*time.Second),
		ConfigHash:                configHash,
		PeerHash:                  peerHash,
		StatePath:                 statePath,
	}
	var genesisBackend ctypes.StorageBackend
	if persistenceWorker := service.PersistenceWorker(); persistenceWorker != nil {
		genesisBackend = persistenceWorker.GetStorageBackend()
	}
	genesisCoord, err := genesis.NewCoordinator(genesisCfg, localValidatorID, vSet, consensusEngine.LeaderRotation(), cryptoAdapter, consensusEngine, p2pRouter, loggerAdapter, auditAdapter, genesisBackend)
	if err != nil {
		log.Fatalf("genesis coordinator init failed: %v", err)
	}
	consensusEngine.SetGenesisCoordinator(genesisCoord)

	// CRITICAL: Allow P2P subscriptions to fully propagate before starting consensus
	// This prevents the 1ms race where proposals are published before all validators subscribe
	// Root cause fix for validators missing view 0 messages (Point 3 from diagnostic plan)
	logger.Info("[DIAGNOSTIC] P2P handlers attached, waiting for subscription propagation...",
		utils.ZapInt("local_node_id", nodeID))
	syncStart := time.Now()
	time.Sleep(100 * time.Millisecond)
	logger.Info("[DIAGNOSTIC] P2P subscription sync complete, starting consensus",
		utils.ZapDuration("sync_delay", time.Since(syncStart)))

	if err := consensusEngine.Start(ctx); err != nil {
		log.Fatalf("consensus engine start failed: %v", err)
	}

	// START SERVICE BEFORE GENESIS - Proposer needs to run regardless of genesis completion
	if err := service.Start(ctx); err != nil {
		log.Fatalf("service start failed: %v", err)
	}

	if err := genesisCoord.Start(ctx); err != nil {
		log.Fatalf("genesis ceremony start failed: %v", err)
	}

	// Wait for genesis asynchronously - don't block proposer
	go func() {
		for {
			cert, err := genesisCoord.WaitForCertificate(ctx)
			if err == nil {
				logger.Info("genesis ceremony completed; consensus timers activated",
					utils.ZapInt("attestations", len(cert.Attestations)))
				break
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				logger.Warn("genesis ceremony aborted", utils.ZapError(err))
				break
			}
			logger.Warn("genesis wait interrupted; retrying", utils.ZapError(err))
		}
	}()

	fmt.Println("Startup complete. Kafka ingest, mempool, and consensus wiring are active.")
	fmt.Println("Press Ctrl+C to initiate shutdown.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutdown requested, stopping components...")
	cancel()

	if err := service.StopWithTimeout(30 * time.Second); err != nil {
		logger.Warn("service stop encountered error", utils.ZapError(err))
	}

	if err := consensusEngine.Stop(); err != nil {
		logger.Warn("consensus engine stop encountered error", utils.ZapError(err))
	}

	fmt.Println("Shutdown complete.")
}

func computeGenesisConfigHash(engineConfig *api.EngineConfig, cfgMgr *utils.ConfigManager, validators []ctypes.ValidatorInfo) [32]byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "node_type=%s\n", engineConfig.NodeType)
	fmt.Fprintf(&buf, "enable_proposing=%t\n", engineConfig.EnableProposing)
	fmt.Fprintf(&buf, "enable_voting=%t\n", engineConfig.EnableVoting)
	fmt.Fprintf(&buf, "block_timeout=%s\n", engineConfig.BlockTimeout)

	durations := []struct {
		key   string
		value time.Duration
	}{
		{"CONSENSUS_BASE_TIMEOUT", cfgMgr.GetDuration("CONSENSUS_BASE_TIMEOUT", 10*time.Second)},
		{"CONSENSUS_MAX_TIMEOUT", cfgMgr.GetDuration("CONSENSUS_MAX_TIMEOUT", 60*time.Second)},
		{"CONSENSUS_MIN_TIMEOUT", cfgMgr.GetDuration("CONSENSUS_MIN_TIMEOUT", 5*time.Second)},
		{"CONSENSUS_TIMEOUT_DECREASE_DELTA", cfgMgr.GetDuration("CONSENSUS_TIMEOUT_DECREASE_DELTA", 200*time.Millisecond)},
		{"CONSENSUS_VIEWCHANGE_TIMEOUT", cfgMgr.GetDuration("CONSENSUS_VIEWCHANGE_TIMEOUT", 30*time.Second)},
		{"CONSENSUS_HEARTBEAT_INTERVAL", cfgMgr.GetDuration("CONSENSUS_HEARTBEAT_INTERVAL", 500*time.Millisecond)},
		{"CONSENSUS_MAX_IDLE_TIME", cfgMgr.GetDuration("CONSENSUS_MAX_IDLE_TIME", 3*time.Second)},
	}
	for _, item := range durations {
		fmt.Fprintf(&buf, "%s=%s\n", item.key, item.value)
	}

	bools := []struct {
		key   string
		value bool
	}{
		{"CONSENSUS_ENABLE_AIMD", cfgMgr.GetBool("CONSENSUS_ENABLE_AIMD", true)},
		{"CONSENSUS_ENABLE_REPUTATION", cfgMgr.GetBool("CONSENSUS_ENABLE_REPUTATION", true)},
		{"CONSENSUS_ENABLE_QUARANTINE", cfgMgr.GetBool("CONSENSUS_ENABLE_QUARANTINE", true)},
	}
	for _, item := range bools {
		fmt.Fprintf(&buf, "%s=%t\n", item.key, item.value)
	}

	floats := []struct {
		key   string
		value float64
	}{
		{"CONSENSUS_TIMEOUT_INCREASE_FACTOR", cfgMgr.GetFloat64("CONSENSUS_TIMEOUT_INCREASE_FACTOR", 1.5)},
		{"CONSENSUS_HEARTBEAT_JITTER", cfgMgr.GetFloat64("CONSENSUS_HEARTBEAT_JITTER", 0.05)},
		{"CONSENSUS_MIN_LEADER_REPUTATION", cfgMgr.GetFloat64("CONSENSUS_MIN_LEADER_REPUTATION", 0.7)},
	}
	for _, item := range floats {
		fmt.Fprintf(&buf, "%s=%.6f\n", item.key, item.value)
	}

	ints := []struct {
		key   string
		value int
	}{
		{"CONSENSUS_MISSED_HEARTBEATS", cfgMgr.GetInt("CONSENSUS_MISSED_HEARTBEATS", 6)},
	}
	for _, item := range ints {
		fmt.Fprintf(&buf, "%s=%d\n", item.key, item.value)
	}

	validatorIDs := make([]string, len(validators))
	for i := range validators {
		validatorIDs[i] = fmt.Sprintf("%x", validators[i].ID[:])
	}
	sort.Strings(validatorIDs)
	for _, id := range validatorIDs {
		fmt.Fprintf(&buf, "validator=%s\n", id)
	}

	return sha256.Sum256(buf.Bytes())
}

func computeValidatorSetHash(validators []ctypes.ValidatorInfo) [32]byte {
	ids := make([]string, len(validators))
	for i := range validators {
		ids[i] = fmt.Sprintf("%x", validators[i].ID[:])
	}
	sort.Strings(ids)
	var buf bytes.Buffer
	for _, id := range ids {
		buf.WriteString(id)
		buf.WriteByte('\n')
	}
	return sha256.Sum256(buf.Bytes())
}

func resolveLocalNodeID(cfgMgr *utils.ConfigManager) (int, error) {
	raw := strings.TrimSpace(cfgMgr.GetString("NODE_ID", ""))
	if raw == "" {
		return 0, fmt.Errorf("NODE_ID not set")
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid NODE_ID %q: %w", raw, err)
	}
	if id <= 0 {
		return 0, fmt.Errorf("NODE_ID must be positive, got %d", id)
	}
	return id, nil
}

func buildCryptoService(cfgMgr *utils.ConfigManager, logger *utils.Logger) (*utils.CryptoService, error) {
	cryptoConfig := utils.DefaultCryptoConfig()
	cryptoConfig.EnableAuditLog = cfgMgr.GetBool("CRYPTO_AUDIT_ENABLED", false)
	cryptoConfig.AutoRotate = false // Disable auto-rotation to avoid Windows entropy issues

	loader, err := newEnvKeyLoader(cfgMgr)
	if err != nil {
		return nil, err
	}
	if loader != nil {
		cryptoConfig.KeyLoader = loader
		// Disable entropy validation when loading keys from env (Windows compatibility)
		cryptoConfig.EntropyValidator = nil
	}

	return utils.NewCryptoService(cryptoConfig)
}

func newEnvKeyLoader(cfgMgr *utils.ConfigManager) (utils.KeyLoader, error) {
	encKey := strings.TrimSpace(cfgMgr.GetString("ENCRYPTION_KEY", ""))
	if encKey == "" {
		return nil, nil
	}

	signingHex := strings.TrimSpace(cfgMgr.GetString("CRYPTO_SIGNING_KEY_HEX", ""))
	signingPath := strings.TrimSpace(cfgMgr.GetString("CRYPTO_SIGNING_KEY_PATH", ""))
	if signingPath == "" {
		signingPath = strings.TrimSpace(cfgMgr.GetString("CONTROL_SIGNING_KEY_PATH", ""))
	}

	env := strings.ToLower(strings.TrimSpace(cfgMgr.GetString("ENVIRONMENT", "")))
	ttl := cfgMgr.GetDuration("CRYPTO_STATIC_KEY_TTL", 365*24*time.Hour)
	if ttl <= 0 {
		ttl = 365 * 24 * time.Hour
	}

	loader := &envKeyLoader{
		encryptionKeyHex: encKey,
		signingKeyHex:    signingHex,
		signingKeyPath:   signingPath,
		ttl:              ttl,
		environment:      env,
	}

	if err := loader.validateConfig(); err != nil {
		return nil, err
	}

	return loader, nil
}

type envKeyLoader struct {
	encryptionKeyHex string
	signingKeyHex    string
	signingKeyPath   string
	ttl              time.Duration
	environment      string
}

func (e *envKeyLoader) validateConfig() error {
	if e.encryptionKeyHex == "" {
		return fmt.Errorf("ENCRYPTION_KEY is required")
	}
	if e.signingKeyHex == "" && e.signingKeyPath == "" {
		if e.environment == "development" || e.environment == "test" || e.environment == "testing" {
			return fmt.Errorf("missing signing key configuration; set CRYPTO_SIGNING_KEY_PATH or CRYPTO_SIGNING_KEY_HEX")
		}
		return fmt.Errorf("missing signing key configuration for environment %s; set CRYPTO_SIGNING_KEY_PATH or CRYPTO_SIGNING_KEY_HEX", e.environment)
	}
	return nil
}

func (e *envKeyLoader) LoadKey() (*utils.KeyVersion, error) {
	encryptionKey, err := hex.DecodeString(e.encryptionKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid ENCRYPTION_KEY hex: %w", err)
	}
	if len(encryptionKey) != utils.AESKeySize {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be %d bytes, got %d", utils.AESKeySize, len(encryptionKey))
	}

	signingKey, err := e.loadSigningKey()
	if err != nil {
		return nil, err
	}

	pubKey := signingKey.Public().(ed25519.PublicKey)
	pubCopy := make(ed25519.PublicKey, len(pubKey))
	copy(pubCopy, pubKey)

	encCopy := make([]byte, len(encryptionKey))
	copy(encCopy, encryptionKey)

	hash := sha256.Sum256(pubCopy)
	keyID := hex.EncodeToString(hash[:8])

	createdAt := time.Now()
	expiresAt := createdAt.Add(e.ttl)

	return &utils.KeyVersion{
		Version:       1,
		SigningKey:    signingKey,
		PublicKey:     pubCopy,
		EncryptionKey: encCopy,
		CreatedAt:     createdAt,
		ExpiresAt:     expiresAt,
		Active:        true,
		KeyID:         keyID,
	}, nil
}

func (e *envKeyLoader) loadSigningKey() (ed25519.PrivateKey, error) {
	if e.signingKeyHex != "" {
		return parseSigningKeyHex(e.signingKeyHex)
	}

	path := filepath.Clean(e.signingKeyPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read signing key from %s: %w", path, err)
	}

	if block, _ := pem.Decode(data); block != nil {
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse PKCS#8 signing key %s: %w", path, err)
		}
		priv, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("signing key at %s is not an Ed25519 key", path)
		}
		return clonePrivateKey(priv), nil
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed != "" {
		return parseSigningKeyHex(trimmed)
	}

	return nil, fmt.Errorf("unsupported signing key format in %s", path)
}

func parseSigningKeyHex(value string) (ed25519.PrivateKey, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid signing key hex: %w", err)
	}

	switch len(decoded) {
	case ed25519.SeedSize:
		key := ed25519.NewKeyFromSeed(decoded)
		return clonePrivateKey(key), nil
	case ed25519.PrivateKeySize:
		key := make(ed25519.PrivateKey, len(decoded))
		copy(key, decoded)
		return key, nil
	default:
		return nil, fmt.Errorf("signing key must be %d or %d bytes, got %d", ed25519.SeedSize, ed25519.PrivateKeySize, len(decoded))
	}
}

func clonePrivateKey(key ed25519.PrivateKey) ed25519.PrivateKey {
	out := make(ed25519.PrivateKey, len(key))
	copy(out, key)
	return out
}

func loadValidatorInfos(cfgMgr *utils.ConfigManager, consensusNodes []int, localNodeID int, localPublicKey ed25519.PublicKey) ([]ctypes.ValidatorInfo, error) {
	if len(consensusNodes) == 0 {
		return nil, fmt.Errorf("consensus node list is empty")
	}

	if err := ensureValidatorEnvCompleteness(cfgMgr, len(consensusNodes)); err != nil {
		return nil, err
	}

	infos := make([]ctypes.ValidatorInfo, 0, len(consensusNodes))
	seen := make(map[ctypes.ValidatorID]struct{})

	for _, node := range consensusNodes {
		pubKey, err := resolveValidatorPublicKey(cfgMgr, node, localNodeID, localPublicKey)
		if err != nil {
			return nil, err
		}
		if node == localNodeID {
			if err := ensureLocalPublicKeyConsistency(cfgMgr, node, pubKey); err != nil {
				return nil, err
			}
		}
		id := deriveValidatorID(pubKey)
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate validator id derived for node %d", node)
		}
		seen[id] = struct{}{}
		infos = append(infos, ctypes.ValidatorInfo{
			ID:         id,
			PublicKey:  append([]byte(nil), pubKey...),
			Reputation: 1.0,
			IsActive:   true,
			JoinedView: 0,
		})
	}

	return infos, nil
}

func ensureLocalPublicKeyConsistency(cfgMgr *utils.ConfigManager, node int, pubKey []byte) error {
	envKey := fmt.Sprintf("VALIDATOR_%d_PUBKEY_HEX", node)
	raw := strings.TrimSpace(cfgMgr.GetString(envKey, ""))
	if raw == "" {
		return nil
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("invalid hex in %s: %w", envKey, err)
	}
	if !bytes.Equal(decoded, pubKey) {
		derived := hex.EncodeToString(pubKey)
		return fmt.Errorf("FATAL: Public key mismatch for NODE_ID=%d\n  Derived from CRYPTO_SIGNING_KEY_HEX_%d: %s\n  Expected from %s:   %s\nACTION: Regenerate validator-pubkeys Secret or fix CRYPTO_SIGNING_KEY_HEX_%d",
			node,
			node,
			strings.ToUpper(derived),
			envKey,
			strings.ToUpper(raw),
			node,
		)
	}
	return nil
}

func ensureValidatorEnvCompleteness(cfgMgr *utils.ConfigManager, validatorCount int) error {
	for i := 1; i <= validatorCount; i++ {
		envKey := fmt.Sprintf("VALIDATOR_%d_PUBKEY_HEX", i)
		if strings.TrimSpace(cfgMgr.GetString(envKey, "")) == "" {
			return fmt.Errorf("missing required %s environment variable", envKey)
		}
	}
	return nil
}

func resolveValidatorPublicKey(cfgMgr *utils.ConfigManager, node int, localNodeID int, localPublicKey ed25519.PublicKey) ([]byte, error) {
	if node == localNodeID {
		return append([]byte(nil), localPublicKey...), nil
	}

	envKey := fmt.Sprintf("VALIDATOR_%d_PUBKEY_HEX", node)
	raw := strings.TrimSpace(cfgMgr.GetString(envKey, ""))
	if raw == "" {
		return nil, fmt.Errorf("missing %s for validator node %d", envKey, node)
	}

	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid hex in %s: %w", envKey, err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%s must be %d bytes, got %d", envKey, ed25519.PublicKeySize, len(decoded))
	}
	return decoded, nil
}

func deriveValidatorID(pub []byte) ctypes.ValidatorID {
	sum := sha256.Sum256(pub)
	var id ctypes.ValidatorID
	copy(id[:], sum[:])
	return id
}

func buildWiringConfig(
	ctx context.Context,
	cfgMgr *utils.ConfigManager,
	logger *utils.Logger,
	auditLogger *utils.AuditLogger,
) (wiring.Config, *sql.DB, cockroach.Adapter, *p2p.Router, error) {
	enableKafka := cfgMgr.GetBool("ENABLE_KAFKA", true)
	kafkaBrokers := strings.Split(cfgMgr.GetString("KAFKA_BROKERS", "localhost:9092"), ",")
	for i := range kafkaBrokers {
		kafkaBrokers[i] = strings.TrimSpace(kafkaBrokers[i])
	}

	consumerTopics := strings.Split(cfgMgr.GetString("KAFKA_INPUT_TOPICS", "ai.anomalies.v1,ai.evidence.v1,ai.policy.v1"), ",")
	for i := range consumerTopics {
		consumerTopics[i] = strings.TrimSpace(consumerTopics[i])
	}

	wiringCfg := wiring.Config{
		BuildInterval:     cfgMgr.GetDuration("BLOCK_BUILD_INTERVAL", 500*time.Millisecond),
		MinMempoolTxs:     cfgMgr.GetInt("BLOCK_MIN_MEMPOOL_TXS", 1),
		TimestampSkew:     cfgMgr.GetDuration("STATE_TIMESTAMP_SKEW", 30*time.Second),
		GenesisHash:       [32]byte{},
		BlockTimeout:      cfgMgr.GetDuration("CONSENSUS_BLOCK_TIMEOUT", 5*time.Second),
		EnablePersistence: false,
		EnableKafka:       enableKafka,
		ConfigManager:     cfgMgr,
		AuditLogger:       auditLogger,
	}

	if enableKafka {
		// Each node uses a unique consumer group to consume ALL partitions
		// This ensures all nodes see all transactions for consensus
		baseGroupID := cfgMgr.GetString("KAFKA_CONSUMER_GROUP_ID", "cybermesh-consensus")
		nodeID := cfgMgr.GetString("NODE_ID", "")
		consumerGroupID := baseGroupID
		if nodeID != "" {
			consumerGroupID = fmt.Sprintf("%s-node-%s", baseGroupID, nodeID)
		}

		wiringCfg.KafkaConsumerCfg = kafka.ConsumerConfig{
			Brokers:  kafkaBrokers,
			GroupID:  consumerGroupID,
			Topics:   consumerTopics,
			DLQTopic: cfgMgr.GetString("KAFKA_DLQ_TOPIC", "ai.dlq.v1"),
			VerifierCfg: kafka.VerifierConfig{
				MaxTimestampSkew: cfgMgr.GetDuration("KAFKA_MAX_TIMESTAMP_SKEW", 5*time.Minute),
			},
		}

		logger.Info("Kafka consumer configured",
			utils.ZapString("group_id", consumerGroupID),
			utils.ZapStringArray("topics", consumerTopics),
			utils.ZapString("strategy", "all-partitions-per-node"))

		wiringCfg.KafkaProducerCfg = kafka.ProducerConfig{
			Brokers: wiringCfg.KafkaConsumerCfg.Brokers,
			Topics: kafka.ProducerTopics{
				Commits:    cfgMgr.GetString("CONTROL_COMMITS_TOPIC", "control.commits.v1"),
				Reputation: cfgMgr.GetString("CONTROL_REPUTATION_TOPIC", ""),
				Policy:     cfgMgr.GetString("CONTROL_POLICY_TOPIC", ""),
				Evidence:   cfgMgr.GetString("CONTROL_EVIDENCE_TOPIC", ""),
			},
		}
	}

	var dbConn *sql.DB
	var adapter cockroach.Adapter

	dsn := strings.TrimSpace(cfgMgr.GetString("DB_DSN", ""))
	if dsn != "" {
		conn, err := cockroach.NewConnection(ctx, &cockroach.ConnectionConfig{
			ConfigManager: cfgMgr,
			Logger:        logger,
			DSN:           dsn,
		})
		if err != nil {
			return wiring.Config{}, nil, nil, nil, fmt.Errorf("cockroach connection failed: %w", err)
		}

		adapterCfg := &cockroach.AdapterConfig{
			DB:     conn,
			Logger: logger,
		}
		dbAdapter, err := cockroach.NewAdapter(ctx, adapterCfg)
		if err != nil {
			conn.Close()
			return wiring.Config{}, nil, nil, nil, fmt.Errorf("cockroach adapter init failed: %w", err)
		}

		wiringCfg.EnablePersistence = true
		wiringCfg.DBAdapter = dbAdapter
		wiringCfg.PersistenceWorker = wiring.PersistenceWorkerConfig{
			QueueSize:       cfgMgr.GetInt("PERSIST_QUEUE_SIZE", 1024),
			RetryMax:        cfgMgr.GetInt("PERSIST_RETRY_MAX", 3),
			RetryBackoffMS:  cfgMgr.GetInt("PERSIST_RETRY_BACKOFF_MS", 100),
			MaxBackoffMS:    cfgMgr.GetInt("PERSIST_MAX_BACKOFF_MS", 5000),
			WorkerCount:     cfgMgr.GetInt("PERSIST_WORKERS", 1),
			ShutdownTimeout: cfgMgr.GetDuration("PERSIST_SHUTDOWN_TIMEOUT", 30*time.Second),
		}

		dbConn = conn
		adapter = dbAdapter
	}

	if cfgMgr.GetBool("ENABLE_API", false) {
		apiCfg, err := config.LoadAPIConfig(cfgMgr)
		if err != nil {
			return wiring.Config{}, nil, nil, nil, fmt.Errorf("api config invalid: %w", err)
		}
		wiringCfg.EnableAPI = true
		wiringCfg.APIConfig = apiCfg
	}

	// Initialize P2P networking for multi-node consensus
	var p2pRouter *p2p.Router
	if cfgMgr.GetBool("ENABLE_P2P", false) {
		// Load node config using actual structure
		nodeCfg := &config.NodeConfig{
			NodeID:      cfgMgr.GetInt("NODE_ID", 1),
			NodeType:    cfgMgr.GetString("NODE_TYPE", "validator"),
			Version:     cfgMgr.GetString("NODE_VERSION", "1.0.0"),
			Environment: cfgMgr.GetString("ENVIRONMENT", "development"),
			Region:      cfgMgr.GetString("REGION", "local"),
		}

		// Load or create security config
		secCfg := &config.SecurityConfig{
			TLSCertPath: cfgMgr.GetString("TLS_CERT_PATH", ""),
			TLSKeyPath:  cfgMgr.GetString("TLS_KEY_PATH", ""),
		}

		// P2P topics for consensus messages
		topics := strings.Split(cfgMgr.GetString("P2P_TOPICS", "consensus,blocks,txs"), ",")
		for i := range topics {
			topics[i] = strings.TrimSpace(topics[i])
		}

		// Bootstrap peers
		var bootstrapPeers []string
		if peers := cfgMgr.GetString("P2P_BOOTSTRAP_PEERS", ""); peers != "" {
			bootstrapPeers = strings.Split(peers, ",")
			for i := range bootstrapPeers {
				bootstrapPeers[i] = strings.TrimSpace(bootstrapPeers[i])
			}
		}

		routerOpts := p2p.RouterOptions{
			Topics:         topics,
			Rendezvous:     cfgMgr.GetString("RENDEZVOUS_NS", "cybermesh/prod"),
			BootstrapAddrs: bootstrapPeers,
			EnableMDNS:     cfgMgr.GetBool("P2P_ENABLE_MDNS", true), // Enable mDNS for local discovery
		}

		// Create P2P state with correct signature
		p2pState := p2p.NewState(ctx, logger, nodeCfg, cfgMgr, nil) // nil metrics for now

		var routerErr error
		p2pRouter, routerErr = p2p.NewRouter(ctx, nodeCfg, secCfg, p2pState, logger, cfgMgr, routerOpts)
		if routerErr != nil {
			logger.Warn("p2p router init failed, continuing without P2P", utils.ZapError(routerErr))
			p2pRouter = nil
		} else {
			logger.Info("P2P router initialized",
				utils.ZapInt("node_id", nodeCfg.NodeID),
				utils.ZapString("rendezvous", routerOpts.Rendezvous),
				utils.ZapInt("topics", len(topics)))
			wiringCfg.EnableP2P = true
			wiringCfg.P2PRouter = p2pRouter
		}
	}

	return wiringCfg, dbConn, adapter, p2pRouter, nil
}
