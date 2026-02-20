package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cybermesh/telemetry-layer/pcap-service/internal/capture"
	"cybermesh/telemetry-layer/pcap-service/internal/codec"
	"cybermesh/telemetry-layer/pcap-service/internal/dlq"
	"cybermesh/telemetry-layer/pcap-service/internal/kafka"
	"cybermesh/telemetry-layer/pcap-service/internal/registry"
	"cybermesh/telemetry-layer/pcap-service/internal/storage"
	"cybermesh/telemetry-layer/pcap-service/internal/validate"
	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
	"github.com/IBM/sarama"
)

type config struct {
	Encoding               string
	RequestTopic           string
	ResultTopic            string
	ResultDLQTopic         string
	RegistryEnabled        bool
	RegistryURL            string
	RegistryUser           string
	RegistryPassword       string
	RegistrySubjectRequest string
	RegistrySubjectResult  string
	RegistryAllowFallback  bool
	AllowedTenants         map[string]bool
	AllowedRequesters      map[string]bool
	MaxDuration            time.Duration
	MaxBytes               int64
	DryRun                 bool
	Store                  storage.Config
	Capture                capture.Config
	Kafka                  kafka.Config
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		panic(err)
	}

	fmt.Printf("pcap-service starting: request=%s result=%s dlq=%s encoding=%s registry=%t dry_run=%t\n",
		cfg.RequestTopic, cfg.ResultTopic, cfg.ResultDLQTopic, cfg.Encoding, cfg.RegistryEnabled, cfg.DryRun)

	kcfg, err := kafka.BuildSaramaConfig(cfg.Kafka)
	if err != nil {
		panic(err)
	}

	producer, err := sarama.NewSyncProducer(cfg.Kafka.Brokers, kcfg)
	if err != nil {
		panic(err)
	}
	defer producer.Close()

	consumer, err := sarama.NewConsumerGroup(cfg.Kafka.Brokers, cfg.Kafka.ClientID, kcfg)
	if err != nil {
		panic(err)
	}
	defer consumer.Close()

	registryClient := registry.New(registry.Config{
		Enabled:  cfg.RegistryEnabled,
		URL:      cfg.RegistryURL,
		Username: cfg.RegistryUser,
		Password: cfg.RegistryPassword,
		Timeout:  5 * time.Second,
		CacheTTL: 5 * time.Minute,
	})

	storeWriter, err := storage.NewWriter(cfg.Store)
	if err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler := &pcapHandler{
		cfg:            cfg,
		registryClient: registryClient,
		producer:       producer,
		store:          storeWriter,
	}

	for {
		if ctx.Err() != nil {
			return
		}
		_ = consumer.Consume(ctx, []string{cfg.RequestTopic}, handler)
		time.Sleep(500 * time.Millisecond)
	}
}

type pcapHandler struct {
	cfg            config
	registryClient *registry.Client
	producer       sarama.SyncProducer
	store          storage.Writer
}

func (h *pcapHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *pcapHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *pcapHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		ctx := context.Background()
		fmt.Printf("pcap-service received request offset=%d key=%s\n", msg.Offset, string(msg.Key))
		payload := msg.Value
		req, err := codec.DecodeRequest(payload, h.cfg.Encoding)
		if err != nil {
			h.sendDLQ("REQUEST_DECODE", err.Error(), payload)
			session.MarkMessage(msg, "")
			continue
		}
		if h.cfg.RegistryEnabled && h.cfg.Encoding != "json" {
			schemaID := extractSchemaID(msg.Headers)
			if schemaID == 0 && !h.cfg.RegistryAllowFallback {
				h.sendDLQ("SCHEMA_MISSING", "schema_id missing", payload)
				session.MarkMessage(msg, "")
				continue
			}
			if schemaID != 0 {
				latestID, err := h.registryClient.SubjectLatestID(context.Background(), h.cfg.RegistrySubjectRequest)
				if err != nil || schemaID != latestID {
					h.sendDLQ("SCHEMA_MISMATCH", "schema_id mismatch", payload)
					session.MarkMessage(msg, "")
					continue
				}
			}
		}

		policy := validate.Policy{
			AllowedTenants:    h.cfg.AllowedTenants,
			AllowedRequesters: h.cfg.AllowedRequesters,
			MaxDuration:       h.cfg.MaxDuration,
			MaxBytes:          h.cfg.MaxBytes,
			DryRun:            h.cfg.DryRun,
		}
		if err := validate.ValidateRequest(req, policy); err != nil {
			fmt.Printf("pcap-service request denied: %s\n", err.Error())
			h.sendResult(req, "DENIED", err.Error(), "", 0, "", "", 0)
			session.MarkMessage(msg, "")
			continue
		}

		// Dry-run capture placeholder.
		if h.cfg.DryRun {
			fmt.Printf("pcap-service dry-run OK: request_id=%s\n", req.RequestId)
			h.sendResult(req, "OK", "", "dryrun://pcap", 0, "application/vnd.tcpdump.pcap", "", 0)
			session.MarkMessage(msg, "")
			continue
		}

		if h.store == nil || !h.cfg.Store.Enabled {
			h.sendResult(req, "FAILED", "storage disabled", "", 0, "", "", 0)
			session.MarkMessage(msg, "")
			continue
		}

		maxBytes := resolveMaxBytes(req.MaxBytes, h.cfg.MaxBytes)
		captureResult, err := capture.Capture(ctx, h.cfg.Capture, req, maxBytes)
		if err != nil {
			h.sendResult(req, "FAILED", err.Error(), "", 0, "", "", 0)
			session.MarkMessage(msg, "")
			continue
		}
		defer captureResult.Reader.Close()

		key := storage.BuildKey(req, h.cfg.Store.Prefix, time.Now())
		retentionUntil := storage.RetentionUntil(h.cfg.Store.RetentionDays)
		h.cfg.Store.Tagging = storage.DefaultTags(h.cfg.Store, req.TenantId, req.RequestId)
		storeResult, err := h.store.Put(ctx, key, captureResult.Reader, captureResult.Size, captureResult.ContentType, retentionUntil)
		if err != nil {
			h.sendResult(req, "FAILED", err.Error(), "", 0, "", "", 0)
			session.MarkMessage(msg, "")
			continue
		}

		h.sendResult(req, "OK", "", storeResult.URI, storeResult.BytesWritten, storeResult.ContentType, storeResult.ChecksumSHA256, storeResult.RetentionUntil)
		session.MarkMessage(msg, "")
	}
	return nil
}

func (h *pcapHandler) sendResult(req *telemetrypb.PcapRequestV1, status, errorMessage, uri string, bytesWritten int64, contentType string, checksum string, retentionUntil int64) {
	result := &telemetrypb.PcapResultV1{
		Schema:       "pcap.result.v1",
		Ts:           time.Now().Unix(),
		TenantId:     req.TenantId,
		RequestId:    req.RequestId,
		SensorId:     req.SensorId,
		Status:       status,
		ErrorMessage: errorMessage,
		StorageUri:   uri,
		BytesWritten: bytesWritten,
		DurationMs:   req.DurationMs,
		ContentType:  contentType,
		ChecksumSha256: checksum,
		RetentionUntil: retentionUntil,
	}
	payload, err := codec.EncodeResult(result, h.cfg.Encoding)
	if err != nil {
		h.sendDLQ("RESULT_ENCODE", err.Error(), []byte(req.RequestId))
		return
	}
	headers := []sarama.RecordHeader{}
	if h.cfg.RegistryEnabled && h.cfg.Encoding != "json" {
		schemaID, err := h.registryClient.SubjectLatestID(context.Background(), h.cfg.RegistrySubjectResult)
		if err == nil {
			headers = append(headers, sarama.RecordHeader{Key: []byte("schema_id"), Value: []byte(fmt.Sprintf("%d", schemaID))})
		}
	}
	msg := &sarama.ProducerMessage{
		Topic:   h.cfg.ResultTopic,
		Key:     sarama.StringEncoder(req.RequestId),
		Value:   sarama.ByteEncoder(payload),
		Headers: headers,
	}
	if _, _, err := h.producer.SendMessage(msg); err != nil {
		fmt.Printf("pcap-service result send failed: %v\n", err)
	}
}

func (h *pcapHandler) sendDLQ(code, message string, payload []byte) {
	env := dlq.NewEnvelope("dlq.v1", "pcap-service", code, message, payload)
	body, _ := json.Marshal(env)
	msg := &sarama.ProducerMessage{
		Topic: h.cfg.ResultDLQTopic,
		Key:   sarama.StringEncoder(env.PayloadHash),
		Value: sarama.ByteEncoder(body),
	}
	if _, _, err := h.producer.SendMessage(msg); err != nil {
		fmt.Printf("pcap-service dlq send failed: %v\n", err)
	}
}

func extractSchemaID(headers []*sarama.RecordHeader) int {
	for _, h := range headers {
		if string(h.Key) == "schema_id" {
			var id int
			_, _ = fmt.Sscanf(string(h.Value), "%d", &id)
			return id
		}
	}
	return 0
}

func loadConfig() (config, error) {
	brokers := splitCSV(getEnv("KAFKA_BOOTSTRAP_SERVERS", getEnv("KAFKA_BROKERS", "")))
	if len(brokers) == 0 {
		return config{}, errors.New("KAFKA_BOOTSTRAP_SERVERS is required")
	}
	return config{
		Encoding:               getEnv("PCAP_ENCODING", "proto"),
		RequestTopic:           getEnv("PCAP_REQUEST_TOPIC", "pcap.request.v1"),
		ResultTopic:            getEnv("PCAP_RESULT_TOPIC", "pcap.result.v1"),
		ResultDLQTopic:         getEnv("PCAP_RESULT_DLQ_TOPIC", "pcap.result.v1.dlq"),
		RegistryEnabled:        getEnvBool("SCHEMA_REGISTRY_ENABLED", false),
		RegistryURL:            getEnv("SCHEMA_REGISTRY_URL", ""),
		RegistryUser:           getEnv("SCHEMA_REGISTRY_USERNAME", ""),
		RegistryPassword:       getEnv("SCHEMA_REGISTRY_PASSWORD", ""),
		RegistrySubjectRequest: getEnv("SCHEMA_REGISTRY_SUBJECT_PCAP_REQUEST", "pcap.request.v1"),
		RegistrySubjectResult:  getEnv("SCHEMA_REGISTRY_SUBJECT_PCAP_RESULT", "pcap.result.v1"),
		RegistryAllowFallback:  getEnvBool("SCHEMA_REGISTRY_ALLOW_FALLBACK", false),
		AllowedTenants:         parseAllowlist(getEnv("PCAP_ALLOWED_TENANTS", "")),
		AllowedRequesters:      parseAllowlist(getEnv("PCAP_ALLOWED_REQUESTERS", "")),
		MaxDuration:            time.Duration(getEnvInt("PCAP_MAX_DURATION_MS", 60000)) * time.Millisecond,
		MaxBytes:               int64(getEnvInt("PCAP_MAX_BYTES", 10485760)),
		DryRun:                 getEnvBool("PCAP_DRY_RUN", true),
		Store: storage.Config{
			Enabled:        getEnvBool("PCAP_STORE_ENABLED", false),
			Provider:       getEnv("PCAP_STORE_PROVIDER", "s3"),
			Bucket:         getEnv("PCAP_STORE_BUCKET", ""),
			Prefix:         getEnv("PCAP_STORE_PREFIX", "pcap"),
			Region:         getEnv("PCAP_STORE_REGION", ""),
			Endpoint:       getEnv("PCAP_STORE_ENDPOINT", ""),
			ForcePathStyle: getEnvBool("PCAP_STORE_FORCE_PATH_STYLE", false),
			ServerSideEnc:  getEnv("PCAP_STORE_SSE", ""),
			KMSKeyID:       getEnv("PCAP_STORE_SSE_KMS_KEY_ID", ""),
			AccessKey:      getEnv("PCAP_STORE_ACCESS_KEY", ""),
			SecretKey:      getEnv("PCAP_STORE_SECRET_KEY", ""),
			SessionToken:   getEnv("PCAP_STORE_SESSION_TOKEN", ""),
			ContentType:    getEnv("PCAP_STORE_CONTENT_TYPE", "application/vnd.tcpdump.pcap"),
			RetentionDays:  getEnvInt("PCAP_STORE_RETENTION_DAYS", 0),
			LegalHold:      getEnvBool("PCAP_STORE_LEGAL_HOLD", false),
			Timeout:        time.Duration(getEnvInt("PCAP_STORE_TIMEOUT_SEC", 30)) * time.Second,
			MaxRetries:     getEnvInt("PCAP_STORE_MAX_RETRIES", 0),
		},
		Capture: capture.Config{
			Mode:        getEnv("PCAP_CAPTURE_MODE", "mock"),
			FilePath:    getEnv("PCAP_CAPTURE_FILE", ""),
			DefaultSize: int64(getEnvInt("PCAP_CAPTURE_DEFAULT_BYTES", 4096)),
			TcpdumpPath: getEnv("PCAP_TCPDUMP_PATH", "tcpdump"),
			Interface:   getEnv("PCAP_CAPTURE_INTERFACE", "any"),
			Snaplen:     getEnvInt("PCAP_CAPTURE_SNAPLEN", 96),
			Promisc:     getEnvBool("PCAP_CAPTURE_PROMISC", false),
			LibpcapTimeoutSec: getEnvInt("PCAP_LIBPCAP_TIMEOUT_SEC", 1),
		},
		Kafka: kafka.Config{
			Brokers:       brokers,
			ClientID:      getEnv("KAFKA_CLIENT_ID", "telemetry-pcap-service"),
			RequiredAcks:  getEnv("KAFKA_REQUIRED_ACKS", "all"),
			Compression:   getEnv("KAFKA_COMPRESSION", "none"),
			TLSEnabled:    getEnvBool("KAFKA_TLS_ENABLED", false),
			TLSCAPath:     getEnv("KAFKA_TLS_CA_PATH", ""),
			TLSCertPath:   getEnv("KAFKA_TLS_CERT_PATH", ""),
			TLSKeyPath:    getEnv("KAFKA_TLS_KEY_PATH", ""),
			SASLEnabled:   getEnvBool("KAFKA_SASL_ENABLED", false),
			SASLMechanism: getEnv("KAFKA_SASL_MECHANISM", "plain"),
			SASLUser:      pickSASLUser(),
			SASLPassword:  getEnv("KAFKA_SASL_PASSWORD", ""),
		},
	}, nil
}

func pickSASLUser() string {
	if val := getEnv("KAFKA_SASL_USERNAME", ""); strings.TrimSpace(val) != "" {
		return val
	}
	return getEnv("KAFKA_SASL_USER", "")
}

func getEnv(key, def string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	switch strings.ToLower(val) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func getEnvInt(key string, def int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	if n, err := strconv.Atoi(val); err == nil {
		return n
	}
	return def
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	raw := strings.Split(value, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseAllowlist(value string) map[string]bool {
	items := splitCSV(value)
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

func resolveMaxBytes(requested int64, fallback int64) int64 {
	if requested > 0 {
		return requested
	}
	if fallback > 0 {
		return fallback
	}
	return 0
}
