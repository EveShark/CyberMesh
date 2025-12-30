package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"backend/pkg/utils"
)

// handleAnomalies handles GET /anomalies?limit=:limit&severity=:severity
func (s *Server) handleAnomalies(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	query := r.URL.Query()

	// Parse limit parameter
	limitStr := query.Get("limit")
	limit := 100 // default
	if limitStr != "" {
		parsedLimit, err := parseInt(limitStr)
		if err != nil {
			writeErrorFromUtils(w, r, NewInvalidParamsError("limit", "must be valid integer"))
			return
		}
		if parsedLimit > 0 && parsedLimit <= 1000 {
			limit = parsedLimit
		}
	}

	// Parse severity filter (optional)
	severityFilter := query.Get("severity")
	// Valid values: critical, high, medium, low, or empty for all

	// Query anomalies from database
	anomalies, err := s.getAnomaliesFromDB(ctx, limit, severityFilter)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get anomalies", utils.ZapError(err))
		writeErrorFromUtils(w, r, NewInternalError("failed to retrieve anomalies"))
		return
	}

	response := AnomalyListResponse{
		Anomalies: anomalies,
		Count:     len(anomalies),
	}

	writeJSONResponse(w, r, &Response{
		Success: true,
		Data:    response,
	}, http.StatusOK)
}

// getAnomaliesFromDB retrieves anomalies from transactions table
func (s *Server) getAnomaliesFromDB(ctx context.Context, limit int, severityFilter string) ([]AnomalyResponse, error) {
	// SECURITY: Add timeout protection
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	adapter, ok := s.storage.(interface {
		GetDB() *sql.DB
	})
	if !ok {
		return nil, fmt.Errorf("storage adapter does not support GetDB()")
	}

	queryLimit := limit
	if severityFilter != "" {
		queryLimit = limit * 5
		if queryLimit > 5000 {
			queryLimit = 5000
		}
	}

	rows, err := adapter.GetDB().QueryContext(queryCtx, `
		SELECT
			tx_hash,
			block_height,
			submitted_at,
			payload
		FROM transactions
		WHERE tx_type = 'event'
			AND payload ? 'Data'
		ORDER BY submitted_at DESC
		LIMIT $1
	`, queryLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to query anomalies: %w", err)
	}
	defer rows.Close()

	anomalies := make([]AnomalyResponse, 0, limit)
	for rows.Next() {
		var (
			txHashBytes []byte
			blockHeight uint64
			submittedAt time.Time
			payload     []byte
		)

		if err := rows.Scan(&txHashBytes, &blockHeight, &submittedAt, &payload); err != nil {
			s.logger.WarnContext(ctx, "failed to scan anomaly row", utils.ZapError(err))
			continue
		}

		anomaly, ok, err := s.parseAnomalyFromTransaction(hex.EncodeToString(txHashBytes), blockHeight, submittedAt.Unix(), payload)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to parse anomaly payload", utils.ZapError(err))
			continue
		}
		if !ok {
			continue
		}

		if severityFilter != "" && anomaly.Severity != severityFilter {
			continue
		}

		anomalies = append(anomalies, anomaly)
		if len(anomalies) >= limit {
			break
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anomaly rows: %w", err)
	}

	return anomalies, nil
}

// parseAnomalyFromTransaction extracts anomaly details from transaction payload
func (s *Server) parseAnomalyFromTransaction(txHash string, blockHeight uint64, defaultTimestamp int64, payload []byte) (AnomalyResponse, bool, error) {
	record, envelopeTs, err := decodeAnomalyPayload(payload)
	if err != nil {
		return AnomalyResponse{}, false, err
	}

	severityLabel := mapSeverity(record.Severity)
	if severityLabel == "" {
		severityLabel = "medium"
	}

	title := formatThreatTitle(record.ThreatType)
	description := buildAnomalyDescription(record)
	ts := record.DetectionTimestamp
	if ts == 0 {
		ts = envelopeTs
	}
	if ts == 0 {
		ts = defaultTimestamp
	}

	source := record.SourceToken
	if source == "" {
		source = record.FlowKey
	}
	if source == "" {
		source = record.TargetToken
	}
	if source == "" {
		source = "ai-service"
	}

	anomalyID := record.AnomalyID
	if anomalyID == "" {
		anomalyID = txHash
	}

	anomaly := AnomalyResponse{
		ID:          anomalyID,
		Type:        record.ThreatType,
		Severity:    severityLabel,
		Title:       title,
		Description: description,
		Source:      source,
		BlockHeight: blockHeight,
		Timestamp:   ts,
		Confidence:  record.Confidence,
		TxHash:      txHash,
	}

	return anomaly, true, nil
}

// handleAnomalyStats handles GET /anomalies/stats
func (s *Server) handleAnomalyStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	stats, err := s.getAnomalyStats(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get anomaly stats", utils.ZapError(err))
		writeErrorFromUtils(w, r, NewInternalError("failed to retrieve anomaly statistics"))
		return
	}

	writeJSONResponse(w, r, &Response{
		Success: true,
		Data:    stats,
	}, http.StatusOK)
}

// getAnomalyStats retrieves anomaly statistics
func (s *Server) getAnomalyStats(ctx context.Context) (*AnomalyStatsResponse, error) {
	// SECURITY: Add timeout protection
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Get concrete adapter to access database
	adapter, ok := s.storage.(interface {
		GetDB() *sql.DB
	})
	if !ok {
		return nil, fmt.Errorf("storage adapter does not support GetDB()")
	}

	rows, err := adapter.GetDB().QueryContext(queryCtx, `
		SELECT payload
		FROM transactions
		WHERE tx_type = 'event'
			AND payload ? 'Data'
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query anomaly stats: %w", err)
	}
	defer rows.Close()

	stats := &AnomalyStatsResponse{}

	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			s.logger.WarnContext(ctx, "failed to scan anomaly stats row", utils.ZapError(err))
			continue
		}

		record, _, err := decodeAnomalyPayload(payload)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to decode anomaly payload for stats", utils.ZapError(err))
			continue
		}

		stats.TotalCount++
		switch mapSeverity(record.Severity) {
		case "critical":
			stats.CriticalCount++
		case "high":
			stats.HighCount++
		case "medium":
			stats.MediumCount++
		default:
			stats.LowCount++
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anomaly stats rows: %w", err)
	}

	return stats, nil
}

type anomalyEnvelope struct {
	Ts   int64  `json:"Ts"`
	Data string `json:"Data"`
}

type detectionPayload struct {
	AnomalyID          string  `json:"anomaly_id"`
	ThreatType         string  `json:"threat_type"`
	Severity           float64 `json:"severity"`
	Confidence         float64 `json:"confidence"`
	FinalScore         float64 `json:"final_score"`
	LLR                float64 `json:"llr"`
	DetectionTimestamp int64   `json:"detection_timestamp"`
	SourceToken        string  `json:"source_token"`
	TargetToken        string  `json:"target_token"`
	FlowKey            string  `json:"flow_key"`
	ModelVersion       string  `json:"model_version"`
	Description        string  `json:"description"`
}

func decodeAnomalyPayload(payload []byte) (*detectionPayload, int64, error) {
	if len(payload) == 0 {
		return nil, 0, fmt.Errorf("anomaly payload empty")
	}

	var envelope anomalyEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, 0, fmt.Errorf("decode envelope: %w", err)
	}

	if strings.TrimSpace(envelope.Data) == "" {
		return nil, envelope.Ts, fmt.Errorf("anomaly payload missing data field")
	}

	decoded, err := base64.StdEncoding.DecodeString(envelope.Data)
	if err != nil {
		return nil, envelope.Ts, fmt.Errorf("decode anomaly data: %w", err)
	}

	var detection detectionPayload
	if err := json.Unmarshal(decoded, &detection); err != nil {
		return nil, envelope.Ts, fmt.Errorf("decode anomaly detection: %w", err)
	}

	return &detection, envelope.Ts, nil
}

func mapSeverity(raw float64) string {
	switch {
	case raw >= 8:
		return "critical"
	case raw >= 5:
		return "high"
	case raw >= 3:
		return "medium"
	case raw > 0:
		return "low"
	default:
		return ""
	}
}

func formatThreatTitle(threatType string) string {
	clean := strings.TrimSpace(strings.ReplaceAll(threatType, "_", " "))
	if clean == "" {
		return "Threat Detected"
	}
	parts := strings.Fields(clean)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		lower := strings.ToLower(part)
		parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(parts, " ")
}

func buildAnomalyDescription(record *detectionPayload) string {
	if record == nil {
		return "Anomaly detected by AI service"
	}

	if desc := strings.TrimSpace(record.Description); desc != "" {
		return desc
	}

	title := formatThreatTitle(record.ThreatType)
	if title == "" {
		title = "Threat"
	}

	var builder strings.Builder
	builder.WriteString(title)
	builder.WriteString(" detection")

	if record.FinalScore != 0 {
		fmt.Fprintf(&builder, " scored %.2f", record.FinalScore)
	}

	if record.Confidence > 0 {
		fmt.Fprintf(&builder, " (confidence %.1f%%)", record.Confidence*100)
	}

	if record.ModelVersion != "" {
		fmt.Fprintf(&builder, " using model %s", record.ModelVersion)
	}

	if record.LLR != 0 {
		fmt.Fprintf(&builder, ", LLR %.2f", record.LLR)
	}

	builder.WriteString(".")
	return builder.String()
}
