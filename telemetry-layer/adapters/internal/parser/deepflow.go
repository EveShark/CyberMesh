package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"cybermesh/telemetry-layer/adapters/internal/model"
)

func ParseZeekJSON(line []byte, tenantID, sensorID, sourceType string) (model.DeepFlowEvent, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(line, &payload); err != nil {
		return model.DeepFlowEvent{}, err
	}

	event := model.DeepFlowEvent{
		Schema:     "deepflow.v1",
		TenantID:   tenantID,
		SensorID:   sensorID,
		SourceType: sourceType,
		Metadata:   make(map[string]string),
	}

	if ts, ok := payload["ts"]; ok {
		event.Timestamp = parseDeepflowTimestamp(ts)
	}
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	event.SrcIP = getString(payload, "id.orig_h")
	event.DstIP = getString(payload, "id.resp_h")
	event.SrcPort = getInt(payload, "id.orig_p")
	event.DstPort = getInt(payload, "id.resp_p")
	event.Proto = parseDeepflowProto(getString(payload, "proto"))

	// Zeek JSON logs are multi-type: conn/http/dns/ssl/notice/weird/etc. Many are not "alerts".
	// We still emit them as DeepFlow events; alert_type becomes the high-level Zeek log kind.
	event.AlertType = pickFirstString(payload, "note", "event_type", "alert", "signature")
	event.AlertCategory = pickFirstString(payload, "category", "service", "sub", "sub_event")
	event.Severity = normalizeSeverity(getString(payload, "severity"))
	event.Signature = pickFirstString(payload, "signature", "notice")
	event.SignatureID = getString(payload, "signature_id")
	event.FlowID = getString(payload, "uid")

	for k, v := range payload {
		if _, exists := event.Metadata[k]; exists {
			continue
		}
		if str, ok := toString(v); ok {
			event.Metadata[k] = str
		}
	}

	applyDeepflowDerivations(&event)

	// Infer type for non-alert Zeek logs (e.g., conn.log) so we don't DLQ normal telemetry.
	if event.AlertType == "" {
		switch {
		case getString(payload, "query") != "":
			event.AlertType = "dns"
		case getString(payload, "method") != "" || getString(payload, "uri") != "":
			event.AlertType = "http"
		case getString(payload, "server_name") != "" || getString(payload, "cipher") != "":
			event.AlertType = "tls"
		case getString(payload, "name") != "" && getString(payload, "addl") != "":
			event.AlertType = "weird"
		default:
			event.AlertType = "connection"
		}
	}
	return event, nil
}

func ParseSuricataEVE(line []byte, tenantID, sensorID, sourceType string) (model.DeepFlowEvent, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(line, &payload); err != nil {
		return model.DeepFlowEvent{}, err
	}

	event := model.DeepFlowEvent{
		Schema:     "deepflow.v1",
		TenantID:   tenantID,
		SensorID:   sensorID,
		SourceType: sourceType,
		Metadata:   make(map[string]string),
	}

	event.Timestamp = parseDeepflowTimestamp(getValue(payload, "timestamp"))
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	event.SrcIP = getString(payload, "src_ip")
	event.DstIP = getString(payload, "dest_ip")
	event.SrcPort = getInt(payload, "src_port")
	event.DstPort = getInt(payload, "dest_port")
	event.Proto = parseDeepflowProto(getString(payload, "proto"))

	alert, _ := payload["alert"].(map[string]interface{})
	event.AlertType = pickFirstString(payload, "event_type", "alert_type")
	event.AlertCategory = getString(alert, "category")
	event.Severity = normalizeSeverity(getString(alert, "severity"))
	event.Signature = getString(alert, "signature")
	event.SignatureID = toStringOrEmpty(alert, "signature_id")

	if event.AlertType == "" {
		event.AlertType = getString(alert, "signature")
	}

	event.FlowID = toStringOrEmpty(payload, "flow_id")

	for k, v := range payload {
		if str, ok := toString(v); ok {
			event.Metadata[k] = str
		}
	}

	applyDeepflowDerivations(&event)

	if event.AlertType == "" {
		return model.DeepFlowEvent{}, errors.New("missing alert_type")
	}
	return event, nil
}

func parseDeepflowTimestamp(value interface{}) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return int64(f)
		}
	case string:
		if v == "" {
			return 0
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t.Unix()
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return int64(f)
		}
	}
	return 0
}

func parseDeepflowProto(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "tcp":
		return 6
	case "udp":
		return 17
	case "icmp":
		return 1
	default:
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return 0
}

func normalizeSeverity(value string) string {
	if value == "" {
		return "info"
	}
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "1", "low":
		return "low"
	case "2", "medium", "med":
		return "medium"
	case "3", "high":
		return "high"
	case "4", "critical":
		return "critical"
	default:
		return lower
	}
}

func applyDeepflowDerivations(event *model.DeepFlowEvent) {
	if event == nil {
		return
	}
	if event.Metadata == nil {
		event.Metadata = make(map[string]string)
	}
	score := severityScore(event.Severity)
	if score > 0 {
		event.Metadata["severity_score"] = strconv.Itoa(score)
	}
	if event.AlertType != "" || event.Signature != "" {
		sum := sha256.Sum256([]byte(event.AlertType + "|" + event.Signature))
		event.Metadata["alert_key_hash"] = hex.EncodeToString(sum[:])
	}
}

func severityScore(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "1":
		return 1
	case "medium", "med", "2":
		return 2
	case "high", "3":
		return 3
	case "critical", "4":
		return 4
	default:
		return 0
	}
}

func pickFirstString(payload map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := getString(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func getValue(payload map[string]interface{}, key string) interface{} {
	if payload == nil {
		return nil
	}
	return payload[key]
}

func getString(payload map[string]interface{}, key string) string {
	if payload == nil {
		return ""
	}
	if value, ok := payload[key]; ok {
		if str, ok := toString(value); ok {
			return str
		}
	}
	return ""
}

func getInt(payload map[string]interface{}, key string) int {
	if payload == nil {
		return 0
	}
	value, ok := payload[key]
	if !ok {
		return 0
	}
	switch v := value.(type) {
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

func toString(value interface{}) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

func toStringOrEmpty(payload map[string]interface{}, key string) string {
	if payload == nil {
		return ""
	}
	if value, ok := payload[key]; ok {
		if str, ok := toString(value); ok {
			return str
		}
	}
	return ""
}
