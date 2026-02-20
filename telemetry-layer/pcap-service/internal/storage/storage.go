package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
)

type Config struct {
	Enabled         bool
	Provider        string
	Bucket          string
	Prefix          string
	Region          string
	Endpoint        string
	ForcePathStyle  bool
	ServerSideEnc   string
	KMSKeyID        string
	AccessKey       string
	SecretKey       string
	SessionToken    string
	ContentType     string
	RetentionDays   int
	LegalHold       bool
	Tagging         map[string]string
	Timeout         time.Duration
	MaxRetries      int
}

type Result struct {
	URI            string
	ChecksumSHA256 string
	BytesWritten   int64
	RetentionUntil int64
	ContentType    string
}

type Writer interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string, retentionUntil int64) (Result, error)
}

func ValidateConfig(cfg Config) error {
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		return errors.New("PCAP_STORE_PROVIDER required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return errors.New("PCAP_STORE_BUCKET required")
	}
	if cfg.RetentionDays <= 0 && !cfg.LegalHold {
		return errors.New("PCAP_STORE_RETENTION_DAYS must be > 0 or LEGAL_HOLD enabled")
	}
	return nil
}

func NewWriter(cfg Config) (Writer, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "s3", "s3_compat", "minio":
		return NewS3Writer(cfg)
	default:
		return nil, fmt.Errorf("unsupported storage provider %q", cfg.Provider)
	}
}

func BuildKey(req *telemetrypb.PcapRequestV1, prefix string, ts time.Time) string {
	safePrefix := strings.Trim(prefix, "/")
	base := fmt.Sprintf("%s/%s/%s/%d.pcap", req.TenantId, req.SensorId, req.RequestId, ts.Unix())
	if safePrefix == "" {
		return base
	}
	return fmt.Sprintf("%s/%s", safePrefix, base)
}

func RetentionUntil(days int) int64 {
	if days <= 0 {
		return 0
	}
	return time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
}

func DefaultTags(cfg Config, tenantID string, requestID string) map[string]string {
	tags := map[string]string{
		"tenant_id":   tenantID,
		"request_id":  requestID,
		"legal_hold":  fmt.Sprintf("%t", cfg.LegalHold),
		"retention_days": fmt.Sprintf("%d", cfg.RetentionDays),
	}
	for k, v := range cfg.Tagging {
		if strings.TrimSpace(k) == "" {
			continue
		}
		tags[k] = v
	}
	return tags
}
