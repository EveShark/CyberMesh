package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/prometheus/client_golang/prometheus"
	kgo "github.com/segmentio/kafka-go"
	kgosasl "github.com/segmentio/kafka-go/sasl"
	kgoplain "github.com/segmentio/kafka-go/sasl/plain"
	kgoscram "github.com/segmentio/kafka-go/sasl/scram"
	"go.uber.org/zap"

	redis "github.com/redis/go-redis/v9"

	"github.com/CyberMesh/enforcement-agent/internal/ack"
	"github.com/CyberMesh/enforcement-agent/internal/config"
	"github.com/CyberMesh/enforcement-agent/internal/control"
	"github.com/CyberMesh/enforcement-agent/internal/controller"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer"
	enforcerapp "github.com/CyberMesh/enforcement-agent/internal/enforcer/app"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/cilium"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/gateway"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/iptables"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/kubernetes"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/nftables"
	"github.com/CyberMesh/enforcement-agent/internal/kafka"
	"github.com/CyberMesh/enforcement-agent/internal/ledger"
	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/observability"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
	"github.com/CyberMesh/enforcement-agent/internal/ratelimit"
	"github.com/CyberMesh/enforcement-agent/internal/reconciler"
	"github.com/CyberMesh/enforcement-agent/internal/scheduler"
	"github.com/CyberMesh/enforcement-agent/internal/state"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	logger, err := buildLogger(cfg.LogLevel)
	if err != nil {
		panic(err)
	}
	defer logger.Sync() //nolint:errcheck

	otelShutdown, otelErr := observability.InitFromEnv(ctx, "cybermesh-enforcement-agent")
	if otelErr != nil {
		logger.Warn("OpenTelemetry tracing disabled due to init error", zap.Error(otelErr))
	} else {
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := otelShutdown(shutdownCtx); err != nil {
				logger.Warn("OpenTelemetry shutdown failed", zap.Error(err))
			}
		}()
		logger.Info("OpenTelemetry tracing initialized")
	}
	logger.Info("enforcement consumption mode configured",
		zap.String("mode", cfg.Kafka.ConsumptionMode),
		zap.String("group_id", cfg.Kafka.GroupID),
		zap.String("node_name", cfg.NodeName),
		zap.String("backend", cfg.EnforcementBackend))
	logger.Info("enforcement effective resilience settings",
		zap.Duration("apply_timeout", cfg.ApplyTimeout),
		zap.Duration("apply_total_budget", cfg.ApplyTotalBudget),
		zap.Int("apply_retry_max", cfg.ApplyRetryMax),
		zap.Duration("apply_retry_backoff", cfg.ApplyRetryBackoff),
		zap.Duration("apply_retry_base_backoff", cfg.ApplyRetryBaseBackoff),
		zap.Duration("apply_retry_max_backoff", cfg.ApplyRetryMaxBackoff),
		zap.Float64("apply_retry_jitter_ratio", cfg.ApplyRetryJitterRatio),
		zap.Int("apply_timeout_breaker_threshold", cfg.ApplyTimeoutBreakerThreshold),
		zap.Duration("apply_timeout_breaker_cooldown", cfg.ApplyTimeoutBreakerCooldown),
		zap.Bool("expired_shedding_enabled", cfg.ExpiredSheddingEnabled),
		zap.Int("handler_workers", cfg.Kafka.HandlerWorkers),
		zap.Int("handler_queue", cfg.Kafka.HandlerQueue),
		zap.Duration("stripe_stall_threshold", cfg.Kafka.StripeStallThreshold),
		zap.Duration("admission_pause_on_saturation", cfg.Kafka.AdmissionPauseOnSaturation),
		zap.Duration("dispatch_max_wait", cfg.Kafka.DispatchMaxWait),
		zap.Duration("dispatch_pause", cfg.Kafka.DispatchPause),
		zap.Duration("consumer_handler_timeout", cfg.Kafka.HandlerTimeout),
		zap.Int("stripe_distress_threshold", cfg.Kafka.StripeDistressThreshold),
		zap.Bool("lifecycle_compaction_enabled", cfg.LifecycleCompactionEnabled),
		zap.Duration("lifecycle_compaction_window", cfg.LifecycleCompactionWindow),
		zap.Int("critical_lane_max_in_flight", cfg.CriticalLaneMaxInFlight),
		zap.Int("maintenance_lane_max_in_flight", cfg.MaintenanceLaneMaxInFlight),
		zap.Duration("lane_starvation_threshold", cfg.LaneStarvationThreshold),
		zap.Bool("ack_accepted_enabled", cfg.AckAcceptedEnabled),
		zap.String("lifecycle_class_critical_mode", cfg.LifecycleCriticalMode),
		zap.String("lifecycle_class_maintenance_mode", cfg.LifecycleMaintenanceMode),
		zap.Bool("lifecycle_promotion_gate_enabled", cfg.LifecyclePromotionGateEnabled),
		zap.Duration("lifecycle_promotion_min_window", cfg.LifecyclePromotionMinWindow),
		zap.Bool("nft_batch_atomic", cfg.NFTBatchAtomic),
	)
	logger.Info("enforcement timeout contract",
		zap.Bool("handler_timeout_cooperative_only", true),
		zap.String("note", "timeouts rely on cooperative context cancellation; non-cooperative backend calls can overrun until they return"))
	if cfg.ApplyTotalBudget > 0 {
		recommendedMin := cfg.ApplyTotalBudget + 250*time.Millisecond
		if cfg.Kafka.HandlerTimeout > 0 && cfg.Kafka.HandlerTimeout < recommendedMin {
			logger.Warn("consumer handler timeout is below recommended minimum for retry budget",
				zap.Duration("handler_timeout", cfg.Kafka.HandlerTimeout),
				zap.Duration("recommended_min", recommendedMin))
		}
	}

	trust, err := policy.LoadTrustedKeys(cfg.TrustedKeysDir)
	if err != nil {
		logger.Fatal("failed to load trusted keys", zap.Error(err))
	}
	var fastTrust *policy.TrustedKeys
	if cfg.FastMitigation.Enabled {
		fastTrust, err = policy.LoadTrustedKeys(cfg.FastMitigation.TrustedKeysDir)
		if err != nil {
			logger.Fatal("failed to load fast mitigation trusted keys", zap.Error(err))
		}
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	recorder := metrics.NewRecorder(registry)
	recorder.SetBackend(cfg.EnforcementBackend)

	backend, err := enforcer.Factory(enforcer.Options{
		Backend: cfg.EnforcementBackend,
		DryRun:  cfg.DryRun,
		Logger:  logger,
		Cilium: cilium.Config{
			KubeConfigPath:  cfg.KubeConfigPath,
			Context:         cfg.KubeContext,
			Namespace:       cfg.KubeNamespace,
			QPS:             cfg.KubeQPS,
			Burst:           cfg.KubeBurst,
			PolicyMode:      cfg.CiliumPolicyMode,
			PolicyNamespace: cfg.CiliumPolicyNamespace,
			LabelPrefix:     cfg.CiliumLabelPrefix,
		},
		Gateway: gateway.Config{
			GatewayNamespace: cfg.GatewayNamespace,
			Metrics:          recorder,
			Guardrails: gateway.GuardrailsConfig{
				MaxTargetsPerPolicy: cfg.GatewayGuardrails.MaxTargetsPerPolicy,
				MaxPortsPerPolicy:   cfg.GatewayGuardrails.MaxPortsPerPolicy,
				MaxActivePolicies:   cfg.GatewayGuardrails.MaxActivePolicies,
				MaxActivePerTenant:  cfg.GatewayGuardrails.MaxActivePerTenant,
				MaxTTLSeconds:       cfg.GatewayGuardrails.MaxTTLSeconds,
				RequireTenant:       cfg.GatewayGuardrails.RequireTenant,
				EnforceTenantMatch:  cfg.GatewayGuardrails.EnforceTenantMatch,
				ApplyCooldown:       cfg.GatewayGuardrails.ApplyCooldown,
				RequireCanary:       cfg.GatewayGuardrails.RequireCanary,
				DenyBroadCIDRs:      cfg.GatewayGuardrails.DenyBroadCIDRs,
				MinIPv4Prefix:       cfg.GatewayGuardrails.MinIPv4Prefix,
				MinIPv6Prefix:       cfg.GatewayGuardrails.MinIPv6Prefix,
				ProtectedCIDRs:      cfg.GatewayGuardrails.ProtectedCIDRs,
				ProtectedIPs:        cfg.GatewayGuardrails.ProtectedIPs,
				ProtectedNamespaces: cfg.GatewayGuardrails.ProtectedNamespaces,
			},
			Cilium: gateway.CiliumConfig{
				KubeConfigPath:  cfg.KubeConfigPath,
				Context:         cfg.KubeContext,
				Namespace:       cfg.KubeNamespace,
				QPS:             cfg.KubeQPS,
				Burst:           cfg.KubeBurst,
				PolicyMode:      cfg.CiliumPolicyMode,
				PolicyNamespace: cfg.CiliumPolicyNamespace,
				LabelPrefix:     cfg.CiliumLabelPrefix,
			},
		},
		IPTables: iptables.Config{
			Binary:             cfg.IPTablesBinary,
			NamespaceSetPrefix: cfg.SelectorNamespacePrefix,
			NodeSetPrefix:      cfg.SelectorNodePrefix,
			KubeConfigPath:     cfg.KubeConfigPath,
			Context:            cfg.KubeContext,
			KubeNamespace:      cfg.KubeNamespace,
			QPS:                cfg.KubeQPS,
			Burst:              cfg.KubeBurst,
			NodeName:           cfg.NodeName,
		},
		NFTables: nftables.Config{
			Binary:             cfg.NFTBinary,
			BatchAtomic:        cfg.NFTBatchAtomic,
			NamespaceSetPrefix: cfg.SelectorNamespacePrefix,
			NodeSetPrefix:      cfg.SelectorNodePrefix,
		},
		Kubernetes: kubernetes.Config{
			KubeConfigPath: cfg.KubeConfigPath,
			Context:        cfg.KubeContext,
			Namespace:      cfg.KubeNamespace,
			QPS:            cfg.KubeQPS,
			Burst:          cfg.KubeBurst,
		},
		App: enforcerapp.Config{
			BaseURL:        cfg.App.BaseURL,
			CommandPath:    cfg.App.CommandPath,
			HealthPath:     cfg.App.HealthPath,
			Timeout:        cfg.App.Timeout,
			BearerToken:    cfg.App.BearerToken,
			AllowedActions: append([]string(nil), cfg.App.AllowedActions...),
		},
	})
	if err != nil {
		logger.Fatal("failed to initialize enforcer", zap.Error(err))
	}

	store := state.NewStore(state.Options{
		PersistPath:      cfg.StatePath,
		HistoryRetention: cfg.StateHistoryRetention,
		EnableChecksum:   cfg.StateChecksum,
		LockTimeout:      cfg.StateLockTimeout,
		PersistMinGap:    cfg.StatePersistMinGap,
	})
	stateLoadedOK := true
	if err := store.Load(); err != nil {
		logger.Warn("failed to load persisted policy state", zap.Error(err))
		stateLoadedOK = false
	}
	recorder.SetActivePolicies(store.ActiveCount())
	if err := maybeFastForwardPolicyBacklog(ctx, cfg, store, stateLoadedOK, logger); err != nil {
		logger.Fatal("failed to fast-forward stale policy backlog", zap.Error(err))
	}

	rateCoord, cleanup := buildRateCoordinator(ctx, cfg, store, logger)
	if cleanup != nil {
		defer cleanup()
	}

	killSwitch := control.NewKillSwitch(cfg.KillSwitchEnabled)
	recorder.SetKillSwitch(killSwitch.Enabled())

	ackPublisher, ackStatus, ackCloser := buildAckPublisher(ctx, cfg, recorder, logger)
	if ackCloser != nil {
		defer ackCloser()
	}

	var commandRejectSink controller.CommandRejectSink = noopCommandRejectSinkShim{}
	commandRejectCfg, err := controller.BuildFastRejectSaramaConfig(
		cfg.Kafka.ProtocolVersion,
		cfg.Kafka.TLS,
		cfg.Kafka.TLSCAPath,
		cfg.Kafka.TLSCertPath,
		cfg.Kafka.TLSKeyPath,
		cfg.Kafka.SASLEnabled,
		cfg.Kafka.SASLMechanism,
		cfg.Kafka.SASLUsername,
		cfg.Kafka.SASLPassword,
	)
	if err != nil {
		logger.Warn("command reject sink disabled: failed to build producer config", zap.Error(err))
	} else {
		sink, sinkErr := controller.NewKafkaCommandRejectSink(commandRejectCfg, cfg.Kafka.Brokers, cfg.CommandRejectTopic, cfg.Kafka.GroupID, logger)
		if sinkErr != nil {
			logger.Warn("command reject sink disabled: failed to create sink", zap.Error(sinkErr))
		} else {
			commandRejectSink = sink
			defer commandRejectSink.Close()
		}
	}

	ctrl := controller.New(trust, store, backend, recorder, rateCoord, killSwitch, logger, controller.Options{
		FastPathEnabled:               cfg.FastPathEnabled,
		FastPathMinConfidence:         cfg.FastPathMinConfidence,
		FastPathSignals:               cfg.FastPathSignalsRequired,
		AckPublisher:                  ackPublisher,
		AckEnqueueTimeout:             cfg.Ack.EnqueueTimeout,
		ApplyTimeout:                  cfg.ApplyTimeout,
		ApplyTotalBudget:              cfg.ApplyTotalBudget,
		ApplyRetryMax:                 cfg.ApplyRetryMax,
		ApplyRetryBackoff:             cfg.ApplyRetryBackoff,
		ApplyRetryBaseBackoff:         cfg.ApplyRetryBaseBackoff,
		ApplyRetryMaxBackoff:          cfg.ApplyRetryMaxBackoff,
		ApplyRetryJitterRatio:         cfg.ApplyRetryJitterRatio,
		ApplyTimeoutBreakerThreshold:  cfg.ApplyTimeoutBreakerThreshold,
		ApplyTimeoutBreakerCooldown:   cfg.ApplyTimeoutBreakerCooldown,
		ExpiredShedding:               cfg.ExpiredSheddingEnabled,
		ControllerInstanceID:          controllerInstanceID(cfg.Ack.ClientID),
		ReplayWindow:                  cfg.Security.ReplayWindow,
		ReplayFutureSkew:              cfg.Security.ReplayFutureSkew,
		ReplayCacheMaxEntries:         cfg.Security.ReplayCacheMaxEntries,
		CommandRejectSink:             commandRejectSink,
		CommandTopic:                  cfg.Kafka.Topic,
		LifecycleCompactionEnabled:    cfg.LifecycleCompactionEnabled,
		LifecycleCompactionWindow:     cfg.LifecycleCompactionWindow,
		CriticalLaneMaxInFlight:       cfg.CriticalLaneMaxInFlight,
		MaintenanceLaneMaxInFlight:    cfg.MaintenanceLaneMaxInFlight,
		LaneStarvationThreshold:       cfg.LaneStarvationThreshold,
		EmitAcceptedAck:               cfg.AckAcceptedEnabled,
		CriticalMode:                  cfg.LifecycleCriticalMode,
		MaintenanceMode:               cfg.LifecycleMaintenanceMode,
		LifecyclePromotionGateEnabled: cfg.LifecyclePromotionGateEnabled,
		LifecyclePromotionMinWindow:   cfg.LifecyclePromotionMinWindow,
	})

	var ledgerProvider reconciler.LedgerProvider
	if cfg.LedgerSnapshotPath != "" {
		ledgerProvider = ledger.NewFileProvider(cfg.LedgerSnapshotPath, cfg.LedgerDriftGrace)
	}

	consumer, err := createKafkaConsumerWithRetry(ctx, logger, kafka.Config{
		Brokers:                 cfg.Kafka.Brokers,
		GroupID:                 cfg.Kafka.GroupID,
		Topic:                   cfg.Kafka.Topic,
		HandlerWorkers:          cfg.Kafka.HandlerWorkers,
		HandlerQueue:            cfg.Kafka.HandlerQueue,
		StallThreshold:          cfg.Kafka.StripeStallThreshold,
		AdmissionPause:          cfg.Kafka.AdmissionPauseOnSaturation,
		DispatchMaxWait:         cfg.Kafka.DispatchMaxWait,
		DispatchPause:           cfg.Kafka.DispatchPause,
		HandlerTimeout:          cfg.Kafka.HandlerTimeout,
		StripeDistressThreshold: cfg.Kafka.StripeDistressThreshold,
		CriticalMaxInFlight:     cfg.CriticalLaneMaxInFlight,
		MaintenanceMaxInFlight:  cfg.MaintenanceLaneMaxInFlight,
		ProtocolVersion:         cfg.Kafka.ProtocolVersion,
		TLS:                     cfg.Kafka.TLS,
		TLSCAPath:               cfg.Kafka.TLSCAPath,
		TLSCertPath:             cfg.Kafka.TLSCertPath,
		TLSKeyPath:              cfg.Kafka.TLSKeyPath,
		SASLEnabled:             cfg.Kafka.SASLEnabled,
		SASLMechanism:           cfg.Kafka.SASLMechanism,
		SASLUsername:            cfg.Kafka.SASLUsername,
		SASLPassword:            cfg.Kafka.SASLPassword,
		Metrics:                 recorder,
		Logger:                  logger,
	}, ctrl)
	if err != nil {
		logger.Fatal("failed to create kafka consumer after retries", zap.Error(err))
	}
	defer consumer.Close()

	var fastConsumer *kafka.Consumer
	var fastRejectSink controller.FastRejectSink = noopFastRejectSinkShim{}
	if cfg.FastMitigation.Enabled {
		rejectCfg, err := controller.BuildFastRejectSaramaConfig(
			cfg.Kafka.ProtocolVersion,
			cfg.Kafka.TLS,
			cfg.Kafka.TLSCAPath,
			cfg.Kafka.TLSCertPath,
			cfg.Kafka.TLSKeyPath,
			cfg.Kafka.SASLEnabled,
			cfg.Kafka.SASLMechanism,
			cfg.Kafka.SASLUsername,
			cfg.Kafka.SASLPassword,
		)
		if err != nil {
			logger.Fatal("failed to build fast mitigation reject producer config", zap.Error(err))
		}
		fastRejectSink, err = controller.NewKafkaFastRejectSink(rejectCfg, cfg.Kafka.Brokers, cfg.FastMitigation.RejectTopic, cfg.FastMitigation.GroupID, logger)
		if err != nil {
			logger.Fatal("failed to create fast mitigation reject sink", zap.Error(err))
		}
		defer fastRejectSink.Close()
		fastConsumer, err = createKafkaConsumerWithRetry(ctx, logger, kafka.Config{
			Brokers:                 cfg.Kafka.Brokers,
			GroupID:                 cfg.FastMitigation.GroupID,
			Topic:                   cfg.FastMitigation.Topic,
			HandlerWorkers:          cfg.Kafka.HandlerWorkers,
			HandlerQueue:            cfg.Kafka.HandlerQueue,
			StallThreshold:          cfg.Kafka.StripeStallThreshold,
			AdmissionPause:          cfg.Kafka.AdmissionPauseOnSaturation,
			DispatchMaxWait:         cfg.Kafka.DispatchMaxWait,
			DispatchPause:           cfg.Kafka.DispatchPause,
			HandlerTimeout:          cfg.Kafka.HandlerTimeout,
			StripeDistressThreshold: cfg.Kafka.StripeDistressThreshold,
			ProtocolVersion:         cfg.Kafka.ProtocolVersion,
			TLS:                     cfg.Kafka.TLS,
			TLSCAPath:               cfg.Kafka.TLSCAPath,
			TLSCertPath:             cfg.Kafka.TLSCertPath,
			TLSKeyPath:              cfg.Kafka.TLSKeyPath,
			SASLEnabled:             cfg.Kafka.SASLEnabled,
			SASLMechanism:           cfg.Kafka.SASLMechanism,
			SASLUsername:            cfg.Kafka.SASLUsername,
			SASLPassword:            cfg.Kafka.SASLPassword,
			Metrics:                 recorder,
			Logger:                  logger,
		}, controller.NewFastHandler(fastTrust, ctrl, recorder, fastRejectSink, logger))
		if err != nil {
			logger.Fatal("failed to create fast mitigation consumer after retries", zap.Error(err))
		}
		defer fastConsumer.Close()
	}

	reconciler := reconciler.New(store, backend, cfg.ReconcileInterval, cfg.ReconcilerMaxBackoff, ledgerProvider, cfg.LedgerDriftGrace, killSwitch, logger, recorder)
	reconciler.RunOnce(ctx)
	go reconciler.Run(ctx)

	sched := scheduler.New(store, backend, cfg.ExpirationCheckFreq, cfg.SchedulerMaxBackoff, killSwitch, logger, recorder)
	go sched.Run(ctx)
	if cfg.StateFlushInterval > 0 {
		go runStateFlushLoop(ctx, cfg.StateFlushInterval, store, logger)
	}

	metricsServer := buildHTTPServer(cfg.MetricsAddr, registry, backend, store, ctrl, killSwitch, ackStatus, recorder, logger)
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server failed", zap.Error(err))
		}
	}()

	go func() {
		if err := consumer.Run(ctx); err != nil {
			logger.Error("consumer stopped", zap.Error(err))
			cancel()
		}
	}()
	if fastConsumer != nil {
		go func() {
			if err := fastConsumer.Run(ctx); err != nil {
				logger.Error("fast mitigation consumer stopped", zap.Error(err))
				cancel()
			}
		}()
	}

	<-ctx.Done()
	if err := store.Flush(); err != nil {
		logger.Warn("state flush on shutdown failed", zap.Error(err))
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	metricsServer.Shutdown(shutdownCtx) //nolint:errcheck
	logger.Info("agent shutdown complete")
}

func runStateFlushLoop(ctx context.Context, interval time.Duration, store *state.Store, logger *zap.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := store.Flush(); err != nil && logger != nil {
				logger.Warn("state flush failed", zap.Error(err))
			}
		}
	}
}

type noopFastRejectSinkShim struct{}

func (noopFastRejectSinkShim) PublishReject(context.Context, *sarama.ConsumerMessage, error) error {
	return nil
}
func (noopFastRejectSinkShim) Close() error { return nil }

type noopCommandRejectSinkShim struct{}

func (noopCommandRejectSinkShim) PublishReject(context.Context, *sarama.ConsumerMessage, error) error {
	return nil
}
func (noopCommandRejectSinkShim) Close() error { return nil }

func buildRateCoordinator(ctx context.Context, cfg config.Config, store *state.Store, logger *zap.Logger) (ratelimit.Coordinator, func()) {
	backend := cfg.RateLimiter.Backend
	switch backend {
	case "redis":
		if cfg.RateLimiter.RedisAddr == "" {
			logger.Fatal("redis rate limiter selected but RATE_LIMIT_REDIS_ADDR is empty")
		}
		client := redis.NewClient(&redis.Options{
			Addr:     cfg.RateLimiter.RedisAddr,
			Username: cfg.RateLimiter.RedisUser,
			Password: cfg.RateLimiter.RedisPass,
			DB:       cfg.RateLimiter.RedisDB,
		})
		if err := client.Ping(ctx).Err(); err != nil {
			logger.Fatal("failed to connect to redis for rate limiter", zap.Error(err))
		}
		coord, err := ratelimit.Factory("redis", ratelimit.Options{
			Redis: &ratelimit.RedisOptions{
				Client:    ratelimit.NewRedisAdapter(client),
				KeyPrefix: cfg.RateLimiter.KeyPrefix,
			},
		})
		if err != nil {
			logger.Fatal("failed to create redis rate limiter", zap.Error(err))
		}
		return coord, func() { _ = client.Close() }
	default:
		coord, err := ratelimit.Factory("local", ratelimit.Options{
			Local: &ratelimit.LocalOptions{Counter: store},
		})
		if err != nil {
			logger.Fatal("failed to create local rate limiter", zap.Error(err))
		}
		return coord, nil
	}
}

func buildLogger(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	switch level {
	case "debug":
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "warn":
		cfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	cfg.EncoderConfig.TimeKey = "ts"
	return cfg.Build()
}

func createKafkaConsumerWithRetry(ctx context.Context, logger *zap.Logger, cfg kafka.Config, handler kafka.MessageHandler) (*kafka.Consumer, error) {
	backoff := 2 * time.Second
	maxBackoff := 30 * time.Second
	attempt := 0

	for {
		attempt++
		consumer, err := kafka.NewConsumer(cfg, handler)
		if err == nil {
			if attempt > 1 {
				logger.Info("kafka consumer connected after retry", zap.Int("attempt", attempt))
			}
			return consumer, nil
		}

		logger.Warn("kafka consumer init failed; retrying", zap.Int("attempt", attempt), zap.Error(err), zap.Duration("next_backoff", backoff))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

type policyBacklogCommitter func(context.Context, config.Config) (int, int64, error)

const (
	policyBacklogTokenStatePending = "pending"
	policyBacklogTokenStateApplied = "applied"
)

func maybeFastForwardPolicyBacklog(ctx context.Context, cfg config.Config, store *state.Store, stateLoadedOK bool, logger *zap.Logger) error {
	return maybeFastForwardPolicyBacklogWithCommitter(ctx, cfg, store, stateLoadedOK, logger, commitPolicyBacklogOffsets)
}

func maybeFastForwardPolicyBacklogWithCommitter(ctx context.Context, cfg config.Config, store *state.Store, stateLoadedOK bool, logger *zap.Logger, commit policyBacklogCommitter) error {
	if !cfg.Kafka.SkipStaleBacklogOnEmptyState {
		return nil
	}
	if !stateLoadedOK {
		if logger != nil {
			logger.Warn("skipping stale backlog fast-forward because persisted state did not load cleanly")
		}
		return nil
	}
	if cfg.Kafka.ConsumptionMode != "fanout_per_node" {
		if logger != nil {
			logger.Warn("skipping stale backlog fast-forward because consumption mode is not node-scoped fanout",
				zap.String("mode", cfg.Kafka.ConsumptionMode))
		}
		return nil
	}
	if cfg.Kafka.SkipStaleBacklogToken == "" {
		if logger != nil {
			logger.Warn("skipping stale backlog fast-forward because no reset token was provided")
		}
		return nil
	}
	if store == nil || store.ActiveCount() != 0 {
		return nil
	}
	tokenPath := policyBacklogFastForwardTokenPath(cfg.StatePath)
	tokenState, applied, err := readPolicyBacklogFastForwardTokenState(tokenPath, cfg.Kafka.SkipStaleBacklogToken)
	if err != nil {
		return fmt.Errorf("check stale backlog fast-forward token: %w", err)
	}
	if applied {
		if logger != nil {
			logger.Info("stale backlog fast-forward token already applied; skipping",
				zap.String("group_id", cfg.Kafka.GroupID),
				zap.String("topic", cfg.Kafka.Topic))
		}
		return nil
	}
	if tokenState == policyBacklogTokenStatePending {
		if logger != nil {
			logger.Warn("stale backlog fast-forward token is pending from a prior incomplete attempt; skipping automatic retry",
				zap.String("group_id", cfg.Kafka.GroupID),
				zap.String("topic", cfg.Kafka.Topic))
		}
		return nil
	}
	if err := writePolicyBacklogFastForwardTokenState(tokenPath, policyBacklogTokenStatePending, cfg.Kafka.SkipStaleBacklogToken); err != nil {
		return fmt.Errorf("persist stale backlog fast-forward pending token: %w", err)
	}
	partitions, totalSkipped, err := commit(ctx, cfg)
	if err != nil {
		if clearErr := clearPolicyBacklogFastForwardTokenState(tokenPath, cfg.Kafka.SkipStaleBacklogToken, policyBacklogTokenStatePending); clearErr != nil && logger != nil {
			logger.Warn("failed to clear pending stale backlog fast-forward token after commit error",
				zap.Error(clearErr),
				zap.String("group_id", cfg.Kafka.GroupID),
				zap.String("topic", cfg.Kafka.Topic))
		}
		return err
	}
	if err := writePolicyBacklogFastForwardTokenState(tokenPath, policyBacklogTokenStateApplied, cfg.Kafka.SkipStaleBacklogToken); err != nil {
		if logger != nil {
			logger.Error("stale backlog fast-forward completed but applied token could not be persisted; continuing startup",
				zap.Error(err),
				zap.String("group_id", cfg.Kafka.GroupID),
				zap.String("topic", cfg.Kafka.Topic))
		}
	}
	if logger != nil {
		logger.Warn("fast-forwarded stale policy backlog because local enforcement state is empty",
			zap.String("group_id", cfg.Kafka.GroupID),
			zap.String("topic", cfg.Kafka.Topic),
			zap.Int("partitions", partitions),
			zap.Int64("approx_next_offset_total", totalSkipped))
	}
	return nil
}

func commitPolicyBacklogOffsets(ctx context.Context, cfg config.Config) (int, int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	saramaCfg := sarama.NewConfig()
	versionStr := cfg.Kafka.ProtocolVersion
	if versionStr == "" {
		versionStr = "3.6.0"
	}
	ver, err := sarama.ParseKafkaVersion(versionStr)
	if err != nil {
		return 0, 0, fmt.Errorf("parse kafka version %q: %w", versionStr, err)
	}
	saramaCfg.Version = ver
	saramaCfg.Net.DialTimeout = 10 * time.Second
	saramaCfg.Net.ReadTimeout = 10 * time.Second
	saramaCfg.Net.WriteTimeout = 10 * time.Second

	if cfg.Kafka.TLS {
		tlsConfig, err := kafkaTLSConfig(cfg)
		if err != nil {
			return 0, 0, fmt.Errorf("build kafka tls config: %w", err)
		}
		saramaCfg.Net.TLS.Enable = true
		saramaCfg.Net.TLS.Config = tlsConfig
	}
	if cfg.Kafka.SASLEnabled {
		saramaCfg.Net.SASL.Enable = true
		saramaCfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		if cfg.Kafka.SASLMechanism == "SCRAM-SHA-256" {
			saramaCfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		} else if cfg.Kafka.SASLMechanism == "SCRAM-SHA-512" {
			saramaCfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		}
		saramaCfg.Net.SASL.User = cfg.Kafka.SASLUsername
		saramaCfg.Net.SASL.Password = cfg.Kafka.SASLPassword
	}

	client, err := sarama.NewClient(cfg.Kafka.Brokers, saramaCfg)
	if err != nil {
		return 0, 0, fmt.Errorf("create kafka client: %w", err)
	}
	defer client.Close()

	partitions, err := client.Partitions(cfg.Kafka.Topic)
	if err != nil {
		return 0, 0, fmt.Errorf("list topic partitions: %w", err)
	}
	offsetManager, err := sarama.NewOffsetManagerFromClient(cfg.Kafka.GroupID, client)
	if err != nil {
		return 0, 0, fmt.Errorf("create offset manager: %w", err)
	}
	defer offsetManager.Close()
	totalSkipped := int64(0)
	for _, partition := range partitions {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		nextOffset, err := client.GetOffset(cfg.Kafka.Topic, partition, sarama.OffsetNewest)
		if err != nil {
			return 0, 0, fmt.Errorf("get latest offset for partition %d: %w", partition, err)
		}
		pom, err := offsetManager.ManagePartition(cfg.Kafka.Topic, partition)
		if err != nil {
			return 0, 0, fmt.Errorf("manage offsets for partition %d: %w", partition, err)
		}
		pom.MarkOffset(nextOffset, "skip_stale_backlog_on_empty_state")
		if err := pom.Close(); err != nil {
			return 0, 0, fmt.Errorf("commit offset for partition %d: %w", partition, err)
		}
		totalSkipped += nextOffset
	}
	return len(partitions), totalSkipped, nil
}

func policyBacklogFastForwardTokenPath(statePath string) string {
	if strings.TrimSpace(statePath) == "" {
		return ""
	}
	return statePath + ".skip_stale_backlog.token"
}

func readPolicyBacklogFastForwardTokenState(path string, token string) (string, bool, error) {
	if path == "" || strings.TrimSpace(token) == "" {
		return "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		return "", false, nil
	}
	state, recordedToken, ok := strings.Cut(line, ":")
	if !ok {
		if line == token {
			return policyBacklogTokenStateApplied, true, nil
		}
		return "", false, nil
	}
	if recordedToken != token {
		return "", false, nil
	}
	return state, state == policyBacklogTokenStateApplied, nil
}

func writePolicyBacklogFastForwardTokenState(path string, state string, token string) error {
	if path == "" || strings.TrimSpace(token) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(state+":"+token+"\n"), 0o600)
}

func clearPolicyBacklogFastForwardTokenState(path string, token string, expectedState string) error {
	if path == "" || strings.TrimSpace(token) == "" {
		return nil
	}
	state, applied, err := readPolicyBacklogFastForwardTokenState(path, token)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if applied || state != expectedState {
		return nil
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type ackRuntimeStatus struct {
	Enabled       bool
	PublisherImpl string
	Topic         string
	QueuePath     string
	QueueMaxSize  int
	Queue         ack.Queue
}

type decisionLister interface {
	ListDecisions(limit int, policyID string, result string) []controller.DecisionRecord
}

func (s *ackRuntimeStatus) Snapshot(ctx context.Context) map[string]any {
	out := map[string]any{
		"enabled":        s != nil && s.Enabled,
		"publisher_impl": "",
		"topic":          "",
		"queue_path":     "",
		"queue_max_size": 0,
		"queue_depth":    nil,
	}
	if s == nil {
		return out
	}
	out["publisher_impl"] = s.PublisherImpl
	out["topic"] = s.Topic
	out["queue_path"] = s.QueuePath
	out["queue_max_size"] = s.QueueMaxSize
	if !s.Enabled {
		return out
	}
	if s.Queue == nil {
		out["queue_depth_error"] = "queue_not_initialized"
		return out
	}
	size, err := s.Queue.Len(ctx)
	if err != nil {
		out["queue_depth_error"] = err.Error()
		return out
	}
	out["queue_depth"] = size
	return out
}

func buildHTTPServer(addr string, registry *prometheus.Registry, backend enforcer.Enforcer, store *state.Store, ctrl decisionLister, kill *control.KillSwitch, ackStatus *ackRuntimeStatus, recorder *metrics.Recorder, logger *zap.Logger) *http.Server {
	mux := http.NewServeMux()
	controlToken := strings.TrimSpace(os.Getenv("ENFORCEMENT_API_TOKEN"))
	requireControlAuth := parseEnvBoolWithDefault(os.Getenv("ENFORCEMENT_API_REQUIRE_AUTH"), controlToken != "")
	requireAuth := func(w http.ResponseWriter, r *http.Request) bool {
		if !requireControlAuth {
			return true
		}
		if controlToken == "" {
			http.Error(w, "control api auth misconfigured", http.StatusServiceUnavailable)
			return false
		}
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if subtle.ConstantTimeCompare([]byte(token), []byte(controlToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.Handle("/metrics", metrics.Handler(registry))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		doHealth(w, r, backend, false, logger)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		doHealth(w, r, backend, true, logger)
	})
	mux.HandleFunc("/control/kill-switch", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		if kill == nil {
			http.Error(w, "kill switch not configured", http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			resp := map[string]bool{"enabled": kill.Enabled()}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case http.MethodPost:
			enabledParam := r.URL.Query().Get("enabled")
			if enabledParam == "" {
				http.Error(w, "missing enabled query parameter", http.StatusBadRequest)
				return
			}
			state, err := strconv.ParseBool(enabledParam)
			if err != nil {
				http.Error(w, "invalid enabled value", http.StatusBadRequest)
				return
			}
			kill.Set(state)
			if recorder != nil {
				recorder.SetKillSwitch(state)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/control/decisions", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if ctrl == nil {
			http.Error(w, "controller not configured", http.StatusServiceUnavailable)
			return
		}
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}
		policyID := strings.TrimSpace(r.URL.Query().Get("policy_id"))
		result := strings.TrimSpace(r.URL.Query().Get("result"))
		decisions := ctrl.ListDecisions(limit, policyID, result)
		writeJSON(w, http.StatusOK, map[string]any{
			"count":     len(decisions),
			"decisions": decisions,
		})
	})
	mux.HandleFunc("/control/effective-rules", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		var backendRules []policy.PolicySpec
		var backendErr string
		if backend != nil {
			rules, err := backend.List(ctx)
			if err != nil {
				backendErr = err.Error()
			} else {
				backendRules = rules
			}
		}

		storeRecords := []state.Record{}
		if store != nil {
			storeRecords = store.List()
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"backend_count": len(backendRules),
			"store_count":   len(storeRecords),
			"backend_error": backendErr,
			"backend_rules": summarizePolicySpecs(backendRules),
			"store_rules":   summarizeStoreRecords(storeRecords),
		})
	})
	mux.HandleFunc("/control/ack-status", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if ackStatus == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"enabled": false,
				"error":   "ack_status_not_initialized",
			})
			return
		}
		writeJSON(w, http.StatusOK, ackStatus.Snapshot(ctx))
	})
	return &http.Server{Addr: addr, Handler: mux}
}

func parseEnvBoolWithDefault(raw string, defaultValue bool) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return defaultValue
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func summarizePolicySpecs(specs []policy.PolicySpec) []map[string]any {
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		out = append(out, map[string]any{
			"policy_id": spec.ID,
			"action":    spec.Action,
			"rule_type": spec.RuleType,
			"scope":     strings.ToLower(strings.TrimSpace(spec.Target.Scope)),
			"tenant":    effectiveTenantFromSpec(spec),
			"region":    effectiveRegionFromSpec(spec),
			"dry_run":   spec.Guardrails.DryRun,
		})
	}
	return out
}

func summarizeStoreRecords(records []state.Record) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		out = append(out, map[string]any{
			"policy_id":          rec.Spec.ID,
			"action":             rec.Spec.Action,
			"rule_type":          rec.Spec.RuleType,
			"scope":              strings.ToLower(strings.TrimSpace(rec.Spec.Target.Scope)),
			"tenant":             effectiveTenantFromSpec(rec.Spec),
			"region":             effectiveRegionFromSpec(rec.Spec),
			"applied_at":         rec.AppliedAt,
			"expires_at":         rec.ExpiresAt,
			"pending_consensus":  rec.PendingConsensus,
			"fast_path_deadline": rec.FastPathDeadline,
		})
	}
	return out
}

func effectiveTenantFromSpec(spec policy.PolicySpec) string {
	if spec.Target.Tenant != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Tenant))
	}
	if spec.Tenant != "" {
		return strings.ToLower(strings.TrimSpace(spec.Tenant))
	}
	return ""
}

func effectiveRegionFromSpec(spec policy.PolicySpec) string {
	if spec.Target.Region != "" {
		return strings.ToLower(strings.TrimSpace(spec.Target.Region))
	}
	if spec.Region != "" {
		return strings.ToLower(strings.TrimSpace(spec.Region))
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func doHealth(w http.ResponseWriter, r *http.Request, backend enforcer.Enforcer, ready bool, logger *zap.Logger) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	var err error
	if ready {
		if rc, ok := backend.(enforcer.ReadyChecker); ok {
			err = rc.ReadyCheck(ctx)
		}
	} else {
		if hc, ok := backend.(enforcer.HealthChecker); ok {
			err = hc.HealthCheck(ctx)
		}
	}
	if err != nil {
		if logger != nil {
			logger.Warn("health check failed", zap.Bool("ready", ready), zap.Error(err))
		}
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func controllerInstanceID(clientID string) string {
	if clientID != "" {
		return clientID
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "controller"
}

func buildAckPublisher(ctx context.Context, cfg config.Config, recorder *metrics.Recorder, logger *zap.Logger) (ack.Publisher, *ackRuntimeStatus, func()) {
	status := &ackRuntimeStatus{
		Enabled:       cfg.Ack.Enabled,
		PublisherImpl: cfg.Ack.PublisherImpl,
		Topic:         cfg.Ack.Topic,
		QueuePath:     cfg.Ack.QueuePath,
		QueueMaxSize:  cfg.Ack.QueueMaxSize,
	}
	if !cfg.Ack.Enabled {
		return nil, status, nil
	}
	impl := strings.ToLower(strings.TrimSpace(cfg.Ack.PublisherImpl))
	if impl == "" {
		impl = "kafkago"
	}
	status.PublisherImpl = impl
	if impl == "sarama" {
		return buildAckPublisherSarama(ctx, cfg, recorder, logger, status)
	}
	return buildAckPublisherKafkaGo(ctx, cfg, recorder, logger, status)
}

func ackInnerRetryMax(configured int) int {
	// ACK delivery durability is provided by the local queue + retrier. Keep the
	// inner Kafka publisher to single-attempt behavior to avoid long head-of-line
	// blocking on one failing record.
	if configured <= 0 {
		return 1
	}
	return 1
}

func buildAckPublisherSarama(ctx context.Context, cfg config.Config, recorder *metrics.Recorder, logger *zap.Logger, status *ackRuntimeStatus) (ack.Publisher, *ackRuntimeStatus, func()) {
	saramaCfg := sarama.NewConfig()
	versionStr := cfg.Kafka.ProtocolVersion
	if versionStr == "" {
		versionStr = "3.6.0"
	}
	ver, err := sarama.ParseKafkaVersion(versionStr)
	if err != nil {
		logger.Fatal("invalid kafka protocol version", zap.String("version", versionStr), zap.Error(err))
	}
	saramaCfg.Version = ver
	saramaCfg.ClientID = cfg.Ack.ClientID
	saramaCfg.Producer.Return.Successes = true
	saramaCfg.Producer.Return.Errors = true
	saramaCfg.Producer.Idempotent = cfg.Ack.Idempotent
	if saramaCfg.Producer.Idempotent {
		saramaCfg.Producer.RequiredAcks = sarama.WaitForAll
		// Sarama requires MaxOpenRequests=1 when idempotency is enabled.
		saramaCfg.Net.MaxOpenRequests = 1
	} else {
		// For non-idempotent local/dev runs, WaitForLocal is sufficient and tends to be
		// more compatible across lightweight brokers (e.g. Redpanda single-node).
		saramaCfg.Producer.RequiredAcks = sarama.WaitForLocal
	}
	saramaCfg.Producer.Retry.Max = cfg.Ack.RetryMax
	saramaCfg.Producer.Retry.Backoff = cfg.Ack.RetryBackoff
	saramaCfg.Producer.Partitioner = sarama.NewHashPartitioner
	if cfg.Kafka.TLS {
		if cfg.Kafka.TLSCAPath != "" || cfg.Kafka.TLSCertPath != "" || cfg.Kafka.TLSKeyPath != "" {
			tlsConfig, err := kafkaTLSConfig(cfg)
			if err != nil {
				logger.Fatal("failed to build ACK Kafka TLS config", zap.Error(err))
			}
			saramaCfg.Net.TLS.Enable = true
			saramaCfg.Net.TLS.Config = tlsConfig
		}
	}
	client, err := sarama.NewClient(cfg.Ack.Brokers, saramaCfg)
	if err != nil {
		logger.Fatal("failed to create ACK client", zap.Error(err))
	}
	producer, err := sarama.NewSyncProducerFromClient(client)
	if err != nil {
		_ = client.Close()
		logger.Fatal("failed to create ACK producer", zap.Error(err))
	}
	var signer ack.Signer
	if cfg.Ack.Signing.Enabled {
		if cfg.Ack.Signing.KeyPath == "" {
			producer.Close()
			_ = client.Close()
			logger.Fatal("ACK signing enabled but ACK_SIGNING_KEY_PATH is empty")
		}
		signer, err = ack.NewEd25519Signer(cfg.Ack.Signing.KeyPath)
		if err != nil {
			producer.Close()
			_ = client.Close()
			logger.Fatal("failed to initialize ACK signer", zap.Error(err))
		}
		logger.Info("ACK signing enabled", zap.String("algorithm", "ed25519"))
	}
	basePublisher, err := ack.NewKafkaPublisher(ack.Options{
		Producer:     producer,
		Topic:        cfg.Ack.Topic,
		RetryMax:     ackInnerRetryMax(cfg.Ack.RetryMax),
		RetryBackoff: cfg.Ack.RetryBackoff,
		Logger:       logger,
		Metrics:      recorder,
		Signer:       signer,
	})
	if err != nil {
		producer.Close()
		_ = client.Close()
		logger.Fatal("failed to initialize ACK publisher", zap.Error(err))
	}
	queue, err := ack.OpenQueue(ack.QueueOptions{Path: cfg.Ack.QueuePath, MaxSize: cfg.Ack.QueueMaxSize})
	if err != nil {
		basePublisher.Close(ctx)
		producer.Close()
		_ = client.Close()
		logger.Fatal("failed to open ACK queue", zap.Error(err))
	}
	if recorder != nil {
		if size, err := queue.Len(ctx); err == nil {
			recorder.ObserveAckQueueDepth(size)
		} else if logger != nil {
			logger.Warn("failed to read ACK queue depth", zap.Error(err))
		}
	}
	retrier, err := ack.NewRetryingPublisher(ack.RetrierOptions{
		Queue:           queue,
		Backend:         basePublisher,
		Metrics:         recorder,
		Logger:          logger,
		Interval:        cfg.Ack.RetryBackoff,
		MaxHeadAttempts: cfg.Ack.Retrier.MaxHeadAttempts,
	})
	if err != nil {
		queue.Close()
		basePublisher.Close(ctx)
		producer.Close()
		_ = client.Close()
		logger.Fatal("failed to initialize ACK retrier", zap.Error(err))
	}
	if cfg.Ack.Batch.Enabled && logger != nil {
		logger.Info("ACK batching enabled", zap.Int("max_size", cfg.Ack.Batch.MaxSize), zap.Duration("interval", cfg.Ack.Batch.Interval))
	}
	var publisher ack.Publisher = retrier
	var closers []func(context.Context) error
	closers = append(closers, retrier.Close, basePublisher.Close)
	if cfg.Ack.Batch.Enabled {
		batcher, err := ack.NewBatchingPublisher(ack.BatchingOptions{
			Backend:   retrier,
			Metrics:   recorder,
			Logger:    logger,
			FlushSize: cfg.Ack.Batch.MaxSize,
			Interval:  cfg.Ack.Batch.Interval,
		})
		if err != nil {
			queue.Close()
			retrier.Close(ctx)
			basePublisher.Close(ctx)
			logger.Fatal("failed to initialize ACK batcher", zap.Error(err))
		}
		publisher = batcher
		closers = append([]func(context.Context) error{batcher.Close}, closers...)
	}
	status.Queue = queue
	return publisher, status, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, closer := range closers {
			_ = closer(ctx)
		}
		status.Queue = nil
		_ = client.Close()
	}
}

type kafkaGoWriter struct {
	w *kgo.Writer
}

func (kw kafkaGoWriter) WriteMessages(ctx context.Context, msgs ...kgo.Message) error {
	return kw.w.WriteMessages(ctx, msgs...)
}

func (kw kafkaGoWriter) Close() error { return kw.w.Close() }

func buildAckPublisherKafkaGo(ctx context.Context, cfg config.Config, recorder *metrics.Recorder, logger *zap.Logger, status *ackRuntimeStatus) (ack.Publisher, *ackRuntimeStatus, func()) {
	_ = ctx

	var tlsConfig *tls.Config
	if cfg.Kafka.TLS {
		// Confluent Cloud and most managed Kafka endpoints require TLS but typically do not
		// require client certificates. Using a nil TLS config results in plaintext dials to
		// :9092 and hard-to-debug timeouts.
		tc, err := kafkaTLSConfig(cfg)
		if err != nil {
			logger.Fatal("failed to build ACK Kafka TLS config", zap.Error(err))
		}
		tlsConfig = tc
	}

	var saslMech kgosasl.Mechanism
	if cfg.Kafka.SASLEnabled {
		mech := strings.ToLower(strings.TrimSpace(cfg.Kafka.SASLMechanism))
		switch {
		case mech == "", mech == "plain", mech == "plaintext":
			saslMech = kgoplain.Mechanism{Username: cfg.Kafka.SASLUsername, Password: cfg.Kafka.SASLPassword}
		case strings.Contains(mech, "scram"):
			algo := kgoscram.SHA512
			if strings.Contains(mech, "256") {
				algo = kgoscram.SHA256
			}
			m, err := kgoscram.Mechanism(algo, cfg.Kafka.SASLUsername, cfg.Kafka.SASLPassword)
			if err != nil {
				logger.Fatal("failed to build ACK Kafka SASL SCRAM mechanism", zap.Error(err))
			}
			saslMech = m
		default:
			logger.Fatal("unsupported ACK Kafka SASL mechanism", zap.String("mechanism", cfg.Kafka.SASLMechanism))
		}
	}

	requiredAcks := kgo.RequireOne
	if cfg.Ack.Idempotent {
		requiredAcks = kgo.RequireAll
	}
	writer := &kgo.Writer{
		Addr:         kgo.TCP(cfg.Ack.Brokers...),
		Topic:        cfg.Ack.Topic,
		Balancer:     &kgo.Hash{},
		RequiredAcks: requiredAcks,
		Transport: &kgo.Transport{
			TLS:      tlsConfig,
			SASL:     saslMech,
			ClientID: cfg.Ack.ClientID,
		},
		BatchTimeout: 50 * time.Millisecond,
	}

	var signer ack.Signer
	if cfg.Ack.Signing.Enabled {
		if cfg.Ack.Signing.KeyPath == "" {
			_ = writer.Close()
			logger.Fatal("ACK signing enabled but ACK_SIGNING_KEY_PATH is empty")
		}
		s, err := ack.NewEd25519Signer(cfg.Ack.Signing.KeyPath)
		if err != nil {
			_ = writer.Close()
			logger.Fatal("failed to initialize ACK signer", zap.Error(err))
		}
		signer = s
		logger.Info("ACK signing enabled", zap.String("algorithm", "ed25519"))
	}

	basePublisher, err := ack.NewKafkaGoPublisher(ack.KafkaGoOptions{
		Writer:       kafkaGoWriter{w: writer},
		Topic:        cfg.Ack.Topic,
		RetryMax:     ackInnerRetryMax(cfg.Ack.RetryMax),
		RetryBackoff: cfg.Ack.RetryBackoff,
		Logger:       logger,
		Metrics:      recorder,
		Signer:       signer,
	})
	if err != nil {
		_ = writer.Close()
		logger.Fatal("failed to initialize ACK publisher", zap.Error(err))
	}

	queue, err := ack.OpenQueue(ack.QueueOptions{Path: cfg.Ack.QueuePath, MaxSize: cfg.Ack.QueueMaxSize})
	if err != nil {
		basePublisher.Close(context.Background())
		_ = writer.Close()
		logger.Fatal("failed to open ACK queue", zap.Error(err))
	}
	if recorder != nil {
		if size, err := queue.Len(context.Background()); err == nil {
			recorder.ObserveAckQueueDepth(size)
		} else if logger != nil {
			logger.Warn("failed to read ACK queue depth", zap.Error(err))
		}
	}

	retrier, err := ack.NewRetryingPublisher(ack.RetrierOptions{
		Queue:           queue,
		Backend:         basePublisher,
		Metrics:         recorder,
		Logger:          logger,
		Interval:        cfg.Ack.RetryBackoff,
		MaxHeadAttempts: cfg.Ack.Retrier.MaxHeadAttempts,
	})
	if err != nil {
		queue.Close()
		basePublisher.Close(context.Background())
		_ = writer.Close()
		logger.Fatal("failed to initialize ACK retrier", zap.Error(err))
	}
	if cfg.Ack.Batch.Enabled && logger != nil {
		logger.Info("ACK batching enabled", zap.Int("max_size", cfg.Ack.Batch.MaxSize), zap.Duration("interval", cfg.Ack.Batch.Interval))
	}

	var publisher ack.Publisher = retrier
	var closers []func(context.Context) error
	closers = append(closers, retrier.Close, basePublisher.Close)
	if cfg.Ack.Batch.Enabled {
		batcher, err := ack.NewBatchingPublisher(ack.BatchingOptions{
			Backend:   retrier,
			Metrics:   recorder,
			Logger:    logger,
			FlushSize: cfg.Ack.Batch.MaxSize,
			Interval:  cfg.Ack.Batch.Interval,
		})
		if err != nil {
			queue.Close()
			retrier.Close(context.Background())
			basePublisher.Close(context.Background())
			logger.Fatal("failed to initialize ACK batcher", zap.Error(err))
		}
		publisher = batcher
		closers = append([]func(context.Context) error{batcher.Close}, closers...)
	}

	status.Queue = queue
	return publisher, status, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, closer := range closers {
			_ = closer(ctx)
		}
		status.Queue = nil
		queue.Close()
		_ = writer.Close()
	}
}

func kafkaTLSConfig(cfg config.Config) (*tls.Config, error) {
	caCertPool := x509.NewCertPool()
	hasCA := false
	if cfg.Kafka.TLSCAPath != "" {
		caBytes, err := os.ReadFile(cfg.Kafka.TLSCAPath)
		if err != nil {
			return nil, err
		}
		if ok := caCertPool.AppendCertsFromPEM(caBytes); !ok {
			return nil, fmt.Errorf("invalid ACK CA cert")
		}
		hasCA = true
	}
	var certs []tls.Certificate
	if cfg.Kafka.TLSCertPath != "" && cfg.Kafka.TLSKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(cfg.Kafka.TLSCertPath, cfg.Kafka.TLSKeyPath)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	tlsCfg := &tls.Config{
		Certificates: certs,
		MinVersion:   tls.VersionTLS12,
	}
	// If no custom CA is provided, fall back to system roots by leaving RootCAs nil.
	if hasCA {
		tlsCfg.RootCAs = caCertPool
	}
	return tlsCfg, nil
}
