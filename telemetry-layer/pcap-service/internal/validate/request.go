package validate

import (
	"errors"
	"strings"
	"time"

	telemetrypb "cybermesh/telemetry-layer/proto/gen/go"
)

type Policy struct {
	AllowedTenants    map[string]bool
	AllowedRequesters map[string]bool
	MaxDuration       time.Duration
	MaxBytes          int64
	DryRun            bool
}

func ValidateRequest(req *telemetrypb.PcapRequestV1, policy Policy) error {
	if req == nil {
		return errors.New("request required")
	}
	if req.TenantId == "" {
		return errors.New("tenant_id required")
	}
	if req.RequestId == "" {
		return errors.New("request_id required")
	}
	if req.Requester == "" {
		return errors.New("requester required")
	}
	if req.DurationMs <= 0 {
		return errors.New("duration_ms required")
	}
	if policy.MaxDuration > 0 && time.Duration(req.DurationMs)*time.Millisecond > policy.MaxDuration {
		return errors.New("duration exceeds max")
	}
	if req.MaxBytes < 0 {
		return errors.New("max_bytes invalid")
	}
	if policy.MaxBytes > 0 && req.MaxBytes > policy.MaxBytes {
		return errors.New("max_bytes exceeds max")
	}
	if len(policy.AllowedTenants) > 0 && !policy.AllowedTenants[req.TenantId] {
		return errors.New("tenant not allowed")
	}
	if len(policy.AllowedRequesters) > 0 && !policy.AllowedRequesters[req.Requester] {
		return errors.New("requester not allowed")
	}
	if strings.TrimSpace(req.SensorId) == "" {
		return errors.New("sensor_id required")
	}
	return nil
}
