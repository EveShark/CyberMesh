package wiring

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	apiserver "backend/pkg/api"
	"backend/pkg/block"
	"backend/pkg/config"
	"backend/pkg/consensus/api"
	"backend/pkg/consensus/messages"
	"backend/pkg/ingest/kafka"
	"backend/pkg/mempool"
	"backend/pkg/p2p"
	"backend/pkg/state"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
	"github.com/IBM/sarama"
)

type Config struct {
	BuildInterval     time.Duration
	MinMempoolTxs     int
	TimestampSkew     time.Duration // For state.ApplyBlock validation
	GenesisHash       [32]byte      // Initial parent hash for first block
	BlockTimeout      time.Duration // Consensus block timeout (controls leader retry cadence)
	AllowSoloProposal bool          // Allow proposals even if peer quorum not met (dev/single-node)

	// Persistence configuration
	EnablePersistence bool                    // Enable async persistence (default: true)
	DBAdapter         cockroach.Adapter       // Database adapter (optional, created if nil)
	PersistenceWorker PersistenceWorkerConfig // Persistence worker config
	AuditLogger       *utils.AuditLogger      // Audit logger for persistence events

	// Kafka configuration (optional)
	EnableKafka      bool                 // Enable Kafka integration
	KafkaConsumerCfg kafka.ConsumerConfig // Kafka consumer config
	KafkaProducerCfg kafka.ProducerConfig // Kafka producer config
	ConfigManager    *utils.ConfigManager // Config manager for Kafka

	// API server configuration (optional)
	EnableAPI bool              // Enable read-only API server
	APIConfig *config.APIConfig // API server configuration

	// P2P consensus networking (optional)
	EnableP2P bool
	P2PRouter *p2p.Router
}

type Service struct {
	cfg     Config
	eng     *api.ConsensusEngine
	mp      *mempool.Mempool
	builder *block.Builder
	store   state.StateStore
	log     *utils.Logger
	metrics *Metrics

	// Persistence (optional)
	persistWorker *PersistenceWorker

	// Kafka (optional)
	kafkaConsumer   *kafka.Consumer
	kafkaProducer   *kafka.Producer
	policyPublisher *policyPublisher

	// API server (optional)
	apiServer *apiserver.Server

	// P2P router (optional)
	router *p2p.Router

	mu                  sync.Mutex
	lastParent          [32]byte
	lastRoot            [32]byte // Last committed state root
	lastCommittedHeight uint64   // Last successfully committed block height
	commitStateSynced   bool     // Whether commit validator is aligned with consensus height
	lastProposedView    uint64   // Last view we proposed in (debug visibility)
	lastProposedHeight  uint64   // Last height we proposed (for cooldown logging)
	lastProposalTime    time.Time
	blockTimeout        time.Duration
	stopCh              chan struct{}

	// Genesis coordination
	startTime          time.Time     // Service start time for genesis delay calculation
	genesisGracePeriod time.Duration // Grace period before first proposal
}

func NewService(cfg Config, eng *api.ConsensusEngine, mp *mempool.Mempool, builder *block.Builder, store state.StateStore, log *utils.Logger) (*Service, error) {
	s := &Service{
		cfg:          cfg,
		eng:          eng,
		mp:           mp,
		builder:      builder,
		store:        store,
		log:          log,
		metrics:      &Metrics{},
		lastParent:   cfg.GenesisHash, // Initialize with genesis instead of zero
		stopCh:       make(chan struct{}),
		blockTimeout: cfg.BlockTimeout,
		// startTime will be set in Start() after genesis completes
		genesisGracePeriod: 60 * time.Second, // Covers observed pod startup spread
	}
	if s.blockTimeout <= 0 {
		s.blockTimeout = 5 * time.Second
	}

	if cfg.DBAdapter != nil {
		if persistenceWorker := s.persistWorker; persistenceWorker != nil {
			eng.SetPersistence(persistenceWorker)
		}
	}

	// Ensure first proposal in view 0 isn't suppressed by cooldown guard
	s.lastProposedView = ^uint64(0)
	s.lastProposedHeight = ^uint64(0)

	// Initialize Kafka producer if enabled (must be before persistence worker)
	var kafkaProducer *kafka.Producer
	if cfg.EnableKafka {
		// Only set up producer when a commits topic is configured
		if cfg.KafkaProducerCfg.Topics.Commits != "" {
			if cfg.ConfigManager == nil {
				return nil, fmt.Errorf("kafka producer requires config manager")
			}

			keyPath := cfg.ConfigManager.GetString("CONTROL_SIGNING_KEY_PATH", "")
			if keyPath == "" {
				env := cfg.ConfigManager.GetString("ENVIRONMENT", "production")
				if env == "production" || env == "staging" {
					return nil, fmt.Errorf("CONTROL_SIGNING_KEY_PATH is required when Kafka producer enabled in %s", env)
				}
				// Dev mode: skip producer if no key
				if log != nil {
					log.Warn("Kafka producer disabled: CONTROL_SIGNING_KEY_PATH not set (dev mode)")
				}
			} else {
				keyPath = filepath.Clean(keyPath)

				signerCfg := kafka.CommitSignerConfig{
					KeyPath:    keyPath,
					KeyID:      cfg.ConfigManager.GetString("CONTROL_SIGNING_KEY_ID", cfg.ConfigManager.GetString("NODE_ID", "")),
					Domain:     cfg.ConfigManager.GetString("CONTROL_SIGNING_DOMAIN", "control.commits.v1"),
					ProducerID: cfg.ConfigManager.GetString("CONTROL_PRODUCER_ID", ""),
					Logger:     log,
				}

				signer, err := kafka.NewCommitSigner(signerCfg)
				if err != nil {
					return nil, fmt.Errorf("failed to initialize commit signer: %w", err)
				}

				cfg.KafkaProducerCfg.Signer = signer

				policyKeyPath := cfg.ConfigManager.GetString("CONTROL_POLICY_SIGNING_KEY_PATH", keyPath)
				if policyKeyPath != "" {
					policyKeyPath = filepath.Clean(policyKeyPath)
					policySignerCfg := kafka.CommitSignerConfig{
						KeyPath:    policyKeyPath,
						KeyID:      cfg.ConfigManager.GetString("CONTROL_POLICY_SIGNING_KEY_ID", signerCfg.KeyID),
						Domain:     cfg.ConfigManager.GetString("CONTROL_POLICY_SIGNING_DOMAIN", "control.policy.v1"),
						ProducerID: cfg.ConfigManager.GetString("CONTROL_POLICY_PRODUCER_ID", cfg.ConfigManager.GetString("CONTROL_PRODUCER_ID", "")),
						Logger:     log,
					}

					policySigner, err := kafka.NewCommitSigner(policySignerCfg)
					if err != nil {
						return nil, fmt.Errorf("failed to initialize policy signer: %w", err)
					}
					cfg.KafkaProducerCfg.PolicySigner = policySigner
				} else if cfg.KafkaProducerCfg.Topics.Policy != "" && log != nil {
					log.Warn("Kafka policy publishing disabled: CONTROL_POLICY_SIGNING_KEY_PATH not set")
				}

				// Build sarama config
				saramaCfg, err := kafka.BuildSaramaConfig(context.Background(), cfg.ConfigManager, log, cfg.AuditLogger)
				if err != nil {
					return nil, fmt.Errorf("failed to build Kafka config: %w", err)
				}

				// Create producer
				kafkaProducer, err = kafka.NewProducer(context.Background(), cfg.KafkaProducerCfg, saramaCfg, log, cfg.AuditLogger)
				if err != nil {
					return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
				}
				s.kafkaProducer = kafkaProducer

				if cfg.KafkaProducerCfg.Topics.Policy != "" {
					pp, err := newPolicyPublisher(kafkaProducer, cfg.ConfigManager, cfg.KafkaProducerCfg.Topics.Policy, log, cfg.AuditLogger)
					if err != nil {
						kafkaProducer.Close()
						return nil, fmt.Errorf("failed to initialize policy publisher: %w", err)
					}
					s.policyPublisher = pp
				}
			}
		}
	}

	// Initialize persistence worker if enabled
	if cfg.EnablePersistence && cfg.DBAdapter != nil {
		// Wire Kafka producer as onSuccess callback
		// Fix: Gap 2 - Updated to pass anomaly IDs for COMMITTED state tracking
		if kafkaProducer != nil {
			cfg.PersistenceWorker.OnSuccess = func(ctx context.Context, height uint64, hash [32]byte, stateRoot [32]byte, txCount int, ts int64, anomalyCount int, evidenceCount int, policyCount int, anomalyIDs []string, policyPayloads [][]byte) {
				// Publish commit event to Kafka after successful persistence
				if err := kafkaProducer.PublishCommit(ctx, height, hash, stateRoot, txCount, ts, anomalyCount, evidenceCount, policyCount, anomalyIDs); err != nil {
					if log != nil {
						log.ErrorContext(ctx, "Failed to publish commit to Kafka",
							utils.ZapError(err),
							utils.ZapUint64("height", height))
					}
				}

				if s.policyPublisher != nil {
					s.policyPublisher.Publish(ctx, height, ts, policyCount, policyPayloads)
				}
			}
		}

		worker, err := NewPersistenceWorker(
			cfg.PersistenceWorker,
			cfg.DBAdapter,
			log,
			cfg.AuditLogger,
		)
		if err != nil {
			// Cleanup Kafka producer if persistence worker creation fails
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to create persistence worker: %w", err)
		}
		s.persistWorker = worker
		// Wire persistence into consensus engine now that worker exists
		if eng != nil {
			eng.AttachPersistence(worker)
		}
	}

	// Initialize Kafka consumer if enabled (after mempool is set)
	if cfg.EnableKafka && mp != nil {
		// Build sarama config
		saramaCfg, err := kafka.BuildSaramaConfig(context.Background(), cfg.ConfigManager, log, cfg.AuditLogger)
		if err != nil {
			// Cleanup on error
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to build Kafka config: %w", err)
		}

		// Create consumer
		kafkaConsumer, err := kafka.NewConsumer(context.Background(), cfg.KafkaConsumerCfg, saramaCfg, mp, log, cfg.AuditLogger)
		if err != nil {
			// Cleanup on error
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
		}
		s.kafkaConsumer = kafkaConsumer

		// Best-effort topic validation: warn if topics missing or have no partitions
		if len(cfg.KafkaConsumerCfg.Topics) > 0 {
			if client, err := sarama.NewClient(cfg.KafkaConsumerCfg.Brokers, saramaCfg); err == nil {
				for _, t := range cfg.KafkaConsumerCfg.Topics {
					parts, perr := client.Partitions(t)
					if perr != nil {
						if log != nil {
							log.Warn("Kafka topic not accessible", utils.ZapString("topic", t), utils.ZapError(perr))
						}
						continue
					}
					if len(parts) == 0 {
						if log != nil {
							log.Warn("Kafka topic has no partitions", utils.ZapString("topic", t))
						}
					} else {
						if log != nil {
							log.Info("Kafka topic partitions", utils.ZapString("topic", t), utils.ZapInt("partition_count", len(parts)))
						}
					}
				}
				_ = client.Close()
			} else if log != nil {
				log.Warn("Kafka topic validation skipped (client create failed)", utils.ZapError(err))
			}
		}
	}

	// Initialize API server if enabled
	if cfg.EnableAPI && cfg.APIConfig != nil {
		apiDeps := apiserver.Dependencies{
			Config:        cfg.APIConfig,
			Logger:        log,
			AuditLogger:   cfg.AuditLogger,
			Storage:       cfg.DBAdapter,
			StateStore:    store,
			Mempool:       mp,
			Engine:        eng,
			P2PRouter:     cfg.P2PRouter,
			KafkaProd:     s.kafkaProducer,
			KafkaCons:     s.kafkaConsumer,
			NodeAliases:   cfg.APIConfig.NodeAliasMap,
			NodeAliasList: cfg.APIConfig.NodeAliasList,
		}

		apiSrv, err := apiserver.NewServer(apiDeps)
		if err != nil {
			// Cleanup on error
			if s.kafkaConsumer != nil {
				s.kafkaConsumer.Stop()
			}
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to create API server: %w", err)
		}
		s.apiServer = apiSrv

		if log != nil {
			log.Info("API server initialized",
				utils.ZapString("listen_addr", cfg.APIConfig.ListenAddr),
				utils.ZapBool("tls_enabled", cfg.APIConfig.TLSEnabled))
		}
	}

	// Attach P2P consensus networking if enabled
	if cfg.EnableP2P {
		if cfg.P2PRouter == nil {
			if s.apiServer != nil {
				_ = s.apiServer.Stop()
			}
			if s.kafkaConsumer != nil {
				_ = s.kafkaConsumer.Stop()
			}
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("p2p enabled but router is nil")
		}

		types := []messages.MessageType{
			messages.TypeProposal,
			messages.TypeVote,
			messages.TypeViewChange,
			messages.TypeNewView,
			messages.TypeHeartbeat,
			messages.TypeEvidence,
			messages.TypeGenesisReady,
			messages.TypeGenesisCertificate,
			messages.TypeProposalIntent,
			messages.TypeReadyToVote,
		}
		topicMap := make(map[string]messages.MessageType, len(types))
		for _, mt := range types {
			topic := eng.TopicFor(mt)
			topicMap[topic] = mt
			s.log.Info("TopicMap entry", utils.ZapString("topic", topic), utils.ZapAny("msgType", mt))
		}

		if err := p2p.AttachConsensusHandlers(cfg.P2PRouter, eng, topicMap); err != nil {
			if s.apiServer != nil {
				_ = s.apiServer.Stop()
			}
			if s.kafkaConsumer != nil {
				_ = s.kafkaConsumer.Stop()
			}
			if kafkaProducer != nil {
				kafkaProducer.Close()
			}
			return nil, fmt.Errorf("failed to attach consensus handlers: %w", err)
		}

		s.router = cfg.P2PRouter
		s.eng.SetPeerObserver(cfg.P2PRouter)
	}

	return s, nil
}

// GetMetrics returns a snapshot of current metrics
func (s *Service) GetMetrics() Metrics {
	return s.metrics.GetSnapshot()
}

func (s *Service) Start(ctx context.Context) error {
	// Start persistence worker if configured
	if s.persistWorker != nil {
		if err := s.persistWorker.Start(ctx); err != nil {
			return fmt.Errorf("failed to start persistence worker: %w", err)
		}
	}

	// Start Kafka consumer if configured
	if s.kafkaConsumer != nil {
		if err := s.kafkaConsumer.Start(); err != nil {
			// Cleanup on error
			if s.persistWorker != nil {
				s.persistWorker.Stop()
			}
			return fmt.Errorf("failed to start Kafka consumer: %w", err)
		}
		if s.log != nil {
			s.log.InfoContext(ctx, "Kafka consumer started")
		}
	}

	// Start API server if configured
	if s.apiServer != nil {
		if err := s.apiServer.Start(ctx); err != nil {
			// Cleanup on error
			if s.kafkaConsumer != nil {
				s.kafkaConsumer.Stop()
			}
			if s.persistWorker != nil {
				s.persistWorker.Stop()
			}
			return fmt.Errorf("failed to start API server: %w", err)
		}
		if s.log != nil {
			s.log.InfoContext(ctx, "API server started")
		}
	}

	// Register commit callback
	s.eng.RegisterCommitCallback(func(cctx context.Context, b api.Block, qc api.QC) error {
		return s.onCommit(cctx, b, qc)
	})

	// Set startTime AFTER genesis completes to ensure grace period covers cluster formation
	s.startTime = time.Now()
	s.log.InfoContext(ctx, "service start time recorded for genesis grace period",
		utils.ZapDuration("grace_period", s.genesisGracePeriod))

	// Proposer loop
	go s.runProposer(ctx)

	return nil
}

func (s *Service) Stop() {
	s.log.Info("wiring service shutting down...")

	// Signal stop to all goroutines
	close(s.stopCh)

	// Stop API server first (stop accepting new requests)
	if s.apiServer != nil {
		if err := s.apiServer.Stop(); err != nil {
			s.log.Warn("API server stop error", utils.ZapError(err))
		} else {
			s.log.Info("API server stopped")
		}
	}

	// Stop Kafka consumer second (stop ingesting new messages)
	if s.kafkaConsumer != nil {
		if err := s.kafkaConsumer.Stop(); err != nil {
			s.log.Warn("Kafka consumer stop error", utils.ZapError(err))
		} else {
			s.log.Info("Kafka consumer stopped")
		}
	}

	// Stop persistence worker (drain pending blocks)
	if s.persistWorker != nil {
		if err := s.persistWorker.Stop(); err != nil {
			s.log.Warn("persistence worker stop error", utils.ZapError(err))
		}
	}

	// Stop Kafka producer last (after all persistence completes)
	if s.kafkaProducer != nil {
		if err := s.kafkaProducer.Close(); err != nil {
			s.log.Warn("Kafka producer close error", utils.ZapError(err))
		} else {
			s.log.Info("Kafka producer closed")
		}
	}

	// Close P2P router
	if s.router != nil {
		if err := s.router.Close(); err != nil {
			s.log.Warn("p2p router close error", utils.ZapError(err))
		} else {
			s.log.Info("p2p router closed")
		}
	}

	// Note: Consensus engine and other components are stopped by caller
	// This service just needs to stop its proposer loop

	s.log.Info("wiring service stopped gracefully")
}

// PersistenceWorker returns the active persistence worker (if any).
func (s *Service) PersistenceWorker() *PersistenceWorker {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistWorker
}

// StopWithTimeout stops the service with a timeout for graceful shutdown
func (s *Service) StopWithTimeout(timeout time.Duration) error {
	s.log.Info("wiring service shutting down with timeout...",
		utils.ZapDuration("timeout", timeout))

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("shutdown timeout exceeded")
	}
}
