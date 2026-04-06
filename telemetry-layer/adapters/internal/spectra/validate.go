package spectra

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

var (
	errBadSignature       = errors.New("bad signature")
	errMissingSignature   = errors.New("missing signature")
	errMissingTimestamp   = errors.New("missing signature timestamp")
	errExpiredSignature   = errors.New("signature timestamp out of window")
	errMissingEventID     = errors.New("source_event_id is required")
	errMissingSourceType  = errors.New("source_type is required")
	errMissingCategory    = errors.New("event_category is required")
	errMissingEventName   = errors.New("event_name is required")
	errMissingAppID       = errors.New("app_id is required")
	errMissingTenantID    = errors.New("tenant_id is required")
	errMissingTraceID     = errors.New("trace_id is required")
	errInvalidCategory    = errors.New("unsupported event_category")
	errInvalidModality    = errors.New("unsupported modality")
	errMissingModality    = errors.New("modality is required when features are provided")
	errMissingFeatures    = errors.New("features is required for modality events")
	errRequestNeedsNet    = errors.New("request category requires network payload")
	errInvalidNetworkIP   = errors.New("invalid network ip")
	errInvalidNetworkPort = errors.New("invalid network port")
)

var allowedCategories = map[string]bool{
	"request":  true,
	"auth":     true,
	"admin":    true,
	"business": true,
	"security": true,
}

var allowedModalities = map[string]bool{
	"action_event":     true,
	"mcp_runtime":      true,
	"exfil_event":      true,
	"resilience_event": true,
}

func verifySignature(secret string, body []byte, sigHeader, tsHeader string, replayWindow time.Duration) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	sigHeader = strings.TrimSpace(sigHeader)
	tsHeader = strings.TrimSpace(tsHeader)
	if sigHeader == "" {
		return errMissingSignature
	}
	if tsHeader == "" {
		return errMissingTimestamp
	}
	tsSec, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: parse timestamp", errBadSignature)
	}
	now := time.Now().Unix()
	window := int64(replayWindow.Seconds())
	if window <= 0 {
		window = 300
	}
	if tsSec < now-window || tsSec > now+window {
		return errExpiredSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(tsHeader))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	got := strings.ToLower(sigHeader)
	got = strings.TrimPrefix(got, "sha256=")
	if !hmac.Equal([]byte(expected), []byte(got)) {
		return errBadSignature
	}
	return nil
}

func normalize(in Envelope, cfg Config) (Envelope, error) {
	out := in
	out.AppID = strings.TrimSpace(out.AppID)
	out.SourceType = strings.TrimSpace(strings.ToLower(out.SourceType))
	out.Modality = strings.TrimSpace(strings.ToLower(out.Modality))
	out.EventCategory = strings.TrimSpace(strings.ToLower(out.EventCategory))
	out.EventName = strings.TrimSpace(out.EventName)
	out.SourceEventID = strings.TrimSpace(out.SourceEventID)
	out.TraceID = strings.TrimSpace(strings.ToLower(out.TraceID))
	out.RequestID = strings.TrimSpace(out.RequestID)
	out.TenantID = strings.TrimSpace(out.TenantID)
	out.UserID = strings.TrimSpace(out.UserID)
	out.SourceID = strings.TrimSpace(out.SourceID)
	if out.AppID == "" {
		out.AppID = strings.TrimSpace(cfg.AppIDDefault)
	}
	if out.SourceID == "" {
		out.SourceID = strings.TrimSpace(cfg.SourceIDDefault)
	}
	if out.AppID == "" {
		return Envelope{}, errMissingAppID
	}
	if out.SourceType == "" {
		return Envelope{}, errMissingSourceType
	}
	if out.EventCategory == "" {
		return Envelope{}, errMissingCategory
	}
	if !allowedCategories[out.EventCategory] {
		return Envelope{}, errInvalidCategory
	}
	if out.EventName == "" {
		return Envelope{}, errMissingEventName
	}
	if out.SourceEventID == "" {
		// request_id can act as source_event_id if caller did not send both.
		out.SourceEventID = out.RequestID
	}
	if out.SourceEventID == "" {
		return Envelope{}, errMissingEventID
	}
	if out.TimestampMs <= 0 {
		out.TimestampMs = time.Now().UnixMilli()
	}
	if out.EventCategory == "request" {
		if out.Network == nil {
			return Envelope{}, errRequestNeedsNet
		}
		if out.TenantID == "" {
			return Envelope{}, errMissingTenantID
		}
		if out.TraceID == "" {
			return Envelope{}, errMissingTraceID
		}
		if net.ParseIP(strings.TrimSpace(out.Network.SrcIP)) == nil || net.ParseIP(strings.TrimSpace(out.Network.DstIP)) == nil {
			return Envelope{}, errInvalidNetworkIP
		}
		if out.Network.SrcPort < 0 || out.Network.SrcPort > 65535 || out.Network.DstPort < 0 || out.Network.DstPort > 65535 {
			return Envelope{}, errInvalidNetworkPort
		}
		if out.Modality != "" {
			return Envelope{}, errInvalidModality
		}
		return out, nil
	}

	if out.Modality == "" {
		if len(out.Features) > 0 {
			return Envelope{}, errMissingModality
		}
		return out, nil
	}
	if !allowedModalities[out.Modality] {
		return Envelope{}, errInvalidModality
	}
	if out.TenantID == "" {
		return Envelope{}, errMissingTenantID
	}
	if out.TraceID == "" {
		return Envelope{}, errMissingTraceID
	}
	if len(out.Features) == 0 {
		return Envelope{}, errMissingFeatures
	}
	if err := validateModalityFeatures(out.Modality, out.Features); err != nil {
		return Envelope{}, err
	}
	return out, nil
}

func validateModalityFeatures(modality string, features map[string]any) error {
	has := func(key string) bool {
		v, ok := features[key]
		if !ok {
			return false
		}
		switch t := v.(type) {
		case string:
			return strings.TrimSpace(t) != ""
		default:
			return v != nil
		}
	}

	switch modality {
	case "action_event":
		if !has("event_name") {
			return fmt.Errorf("%w: action_event.event_name", errInvalidModality)
		}
	case "mcp_runtime":
		if !has("method") || !has("direction") {
			return fmt.Errorf("%w: mcp_runtime.method and mcp_runtime.direction", errInvalidModality)
		}
	case "exfil_event":
		if !has("destination") || !has("bytes_out") {
			return fmt.Errorf("%w: exfil_event.destination and exfil_event.bytes_out", errInvalidModality)
		}
	case "resilience_event":
		if !has("component") || !has("status") {
			return fmt.Errorf("%w: resilience_event.component and resilience_event.status", errInvalidModality)
		}
	}
	return nil
}
