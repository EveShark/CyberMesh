package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	redis "github.com/redis/go-redis/v9"

	"github.com/CyberMesh/enforcement-agent/internal/ack"
	"github.com/CyberMesh/enforcement-agent/internal/config"
	"github.com/CyberMesh/enforcement-agent/internal/control"
	"github.com/CyberMesh/enforcement-agent/internal/controller"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/iptables"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/kubernetes"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer/nftables"
	"github.com/CyberMesh/enforcement-agent/internal/kafka"
	"github.com/CyberMesh/enforcement-agent/internal/ledger"
	"github.com/CyberMesh/enforcement-agent/internal/metrics"
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

	trust, err := policy.LoadTrustedKeys(cfg.TrustedKeysDir)
	if err != nil {
		logger.Fatal("failed to load trusted keys", zap.Error(err))
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	recorder := metrics.NewRecorder(registry)
	recorder.SetBackend(cfg.EnforcementBackend)

	backend, err := enforcer.Factory(enforcer.Options{
		Backend: cfg.EnforcementBackend,
		DryRun:  cfg.DryRun,
		Logger:  logger,
		IPTables: iptables.Config{
			Binary:             cfg.IPTablesBinary,
			NamespaceSetPrefix: cfg.SelectorNamespacePrefix,
			NodeSetPrefix:      cfg.SelectorNodePrefix,
		},
		NFTables: nftables.Config{
			Binary:             cfg.NFTBinary,
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
	})
	if err != nil {
		logger.Fatal("failed to initialize enforcer", zap.Error(err))
	}

	store := state.NewStore(state.Options{
		PersistPath:      cfg.StatePath,
		HistoryRetention: cfg.StateHistoryRetention,
		EnableChecksum:   cfg.StateChecksum,
		LockTimeout:      cfg.StateLockTimeout,
	})
	if err := store.Load(); err != nil {
		logger.Warn("failed to load persisted policy state", zap.Error(err))
	}
	recorder.SetActivePolicies(store.ActiveCount())

	rateCoord, cleanup := buildRateCoordinator(ctx, cfg, store, logger)
	if cleanup != nil {
		defer cleanup()
	}

	killSwitch := control.NewKillSwitch(cfg.KillSwitchEnabled)
	recorder.SetKillSwitch(killSwitch.Enabled())

	ackPublisher, ackCloser := buildAckPublisher(ctx, cfg, recorder, logger)
	if ackCloser != nil {
		defer ackCloser()
	}

	ctrl := controller.New(trust, store, backend, recorder, rateCoord, killSwitch, logger, controller.Options{
		FastPathEnabled:       cfg.FastPathEnabled,
		FastPathMinConfidence: cfg.FastPathMinConfidence,
		FastPathSignals:       cfg.FastPathSignalsRequired,
		AckPublisher:          ackPublisher,
		ControllerInstanceID:  controllerInstanceID(cfg.Ack.ClientID),
	})

	var ledgerProvider reconciler.LedgerProvider
	if cfg.LedgerSnapshotPath != "" {
		ledgerProvider = ledger.NewFileProvider(cfg.LedgerSnapshotPath, cfg.LedgerDriftGrace)
	}

	consumer, err := kafka.NewConsumer(kafka.Config{
		Brokers:       cfg.Kafka.Brokers,
		GroupID:       cfg.Kafka.GroupID,
		Topic:         cfg.Kafka.Topic,
		TLS:           cfg.Kafka.TLS,
		TLSCAPath:     cfg.Kafka.TLSCAPath,
		TLSCertPath:   cfg.Kafka.TLSCertPath,
		TLSKeyPath:    cfg.Kafka.TLSKeyPath,
		SASLEnabled:   cfg.Kafka.SASLEnabled,
		SASLMechanism: cfg.Kafka.SASLMechanism,
		SASLUsername:  cfg.Kafka.SASLUsername,
		SASLPassword:  cfg.Kafka.SASLPassword,
		Metrics:       recorder,
	}, ctrl)
	if err != nil {
		logger.Fatal("failed to create kafka consumer", zap.Error(err))
	}
	defer consumer.Close()

	reconciler := reconciler.New(store, backend, cfg.ReconcileInterval, cfg.ReconcilerMaxBackoff, ledgerProvider, cfg.LedgerDriftGrace, killSwitch, logger, recorder)
	reconciler.RunOnce(ctx)
	go reconciler.Run(ctx)

	sched := scheduler.New(store, backend, cfg.ExpirationCheckFreq, cfg.SchedulerMaxBackoff, killSwitch, logger, recorder)
	go sched.Run(ctx)

	metricsServer := buildHTTPServer(cfg.MetricsAddr, registry, backend, killSwitch, recorder, logger)
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

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	metricsServer.Shutdown(shutdownCtx) //nolint:errcheck
	logger.Info("agent shutdown complete")
}

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

func buildHTTPServer(addr string, registry *prometheus.Registry, backend enforcer.Enforcer, kill *control.KillSwitch, recorder *metrics.Recorder, logger *zap.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler(registry))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		doHealth(w, r, backend, false, logger)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		doHealth(w, r, backend, true, logger)
	})
	mux.HandleFunc("/control/kill-switch", func(w http.ResponseWriter, r *http.Request) {
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
	return &http.Server{Addr: addr, Handler: mux}
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

func buildAckPublisher(ctx context.Context, cfg config.Config, recorder *metrics.Recorder, logger *zap.Logger) (ack.Publisher, func()) {
	if !cfg.Ack.Enabled {
		return nil, nil
	}
	saramaCfg := sarama.NewConfig()
	saramaCfg.Version = sarama.V3_6_0_0
	saramaCfg.ClientID = cfg.Ack.ClientID
	saramaCfg.Producer.Return.Successes = true
	saramaCfg.Producer.RequiredAcks = sarama.WaitForAll
	saramaCfg.Producer.Idempotent = true
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
	producer, err := sarama.NewSyncProducer(cfg.Ack.Brokers, saramaCfg)
	if err != nil {
		logger.Fatal("failed to create ACK producer", zap.Error(err))
	}
	var signer ack.Signer
	if cfg.Ack.Signing.Enabled {
		if cfg.Ack.Signing.KeyPath == "" {
			producer.Close()
			logger.Fatal("ACK signing enabled but ACK_SIGNING_KEY_PATH is empty")
		}
		signer, err = ack.NewEd25519Signer(cfg.Ack.Signing.KeyPath)
		if err != nil {
			producer.Close()
			logger.Fatal("failed to initialize ACK signer", zap.Error(err))
		}
		logger.Info("ACK signing enabled", zap.String("algorithm", "ed25519"))
	}
	basePublisher, err := ack.NewKafkaPublisher(ack.Options{
		Producer:     producer,
		Topic:        cfg.Ack.Topic,
		RetryMax:     cfg.Ack.RetryMax,
		RetryBackoff: cfg.Ack.RetryBackoff,
		Logger:       logger,
		Metrics:      recorder,
		Signer:       signer,
	})
	if err != nil {
		producer.Close()
		logger.Fatal("failed to initialize ACK publisher", zap.Error(err))
	}
	queue, err := ack.OpenQueue(ack.QueueOptions{Path: cfg.Ack.QueuePath, MaxSize: cfg.Ack.QueueMaxSize})
	if err != nil {
		basePublisher.Close(ctx)
		producer.Close()
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
		Queue:    queue,
		Backend:  basePublisher,
		Metrics:  recorder,
		Logger:   logger,
		Interval: cfg.Ack.RetryBackoff,
	})
	if err != nil {
		queue.Close()
		basePublisher.Close(ctx)
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
	return publisher, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, closer := range closers {
			_ = closer(ctx)
		}
	}
}

func kafkaTLSConfig(cfg config.Config) (*tls.Config, error) {
	caCertPool := x509.NewCertPool()
	if cfg.Kafka.TLSCAPath != "" {
		caBytes, err := os.ReadFile(cfg.Kafka.TLSCAPath)
		if err != nil {
			return nil, err
		}
		if ok := caCertPool.AppendCertsFromPEM(caBytes); !ok {
			return nil, fmt.Errorf("invalid ACK CA cert")
		}
	}
	var certs []tls.Certificate
	if cfg.Kafka.TLSCertPath != "" && cfg.Kafka.TLSKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(cfg.Kafka.TLSCertPath, cfg.Kafka.TLSKeyPath)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	return &tls.Config{
		RootCAs:      caCertPool,
		Certificates: certs,
		MinVersion:   tls.VersionTLS12,
	}, nil
}
