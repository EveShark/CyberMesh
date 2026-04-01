package api

import (
	"context"
	"database/sql"
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

	db, err := s.getDB()
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get anomaly database handle", utils.ZapError(err))
		writeErrorFromUtils(w, r, NewInternalError("failed to retrieve anomalies"))
		return
	}

	// Query anomalies from database
	anomalies, err := queryAnomalies(ctx, db, limit, severityFilter)
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

// queryAnomalies retrieves anomalies from the dedicated anomalies table.
func queryAnomalies(ctx context.Context, db *sql.DB, limit int, severityFilter string) ([]AnomalyResponse, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if db == nil {
		return nil, fmt.Errorf("db not available")
	}

	queryLimit := limit
	if severityFilter != "" {
		queryLimit = limit * 5
		if queryLimit > 5000 {
			queryLimit = 5000
		}
	}

	rows, err := db.QueryContext(queryCtx, `
		SELECT
			anomaly_id,
			threat_type,
			severity_value,
			title,
			description,
			source,
			block_height,
			detected_at,
			confidence,
			tx_hash
		FROM anomalies
		ORDER BY detected_at DESC, created_at DESC
		LIMIT $1
	`, queryLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to query anomalies: %w", err)
	}
	defer rows.Close()

	anomalies := make([]AnomalyResponse, 0, limit)
	for rows.Next() {
		var (
			anomalyID    string
			threatType   string
			severity     float64
			title        sql.NullString
			description  sql.NullString
			source       string
			blockHeight  uint64
			detectedAt   int64
			confidence   float64
			txHash       string
		)

		if err := rows.Scan(&anomalyID, &threatType, &severity, &title, &description, &source, &blockHeight, &detectedAt, &confidence, &txHash); err != nil {
			return nil, fmt.Errorf("failed to scan anomaly row: %w", err)
		}

		severityLabel := mapSeverity(severity)
		if severityLabel == "" {
			severityLabel = "medium"
		}
		if severityFilter != "" && severityLabel != severityFilter {
			continue
		}

		item := AnomalyResponse{
			ID:          strings.TrimSpace(anomalyID),
			Type:        strings.TrimSpace(threatType),
			Severity:    severityLabel,
			Title:       strings.TrimSpace(title.String),
			Description: strings.TrimSpace(description.String),
			Source:      strings.TrimSpace(source),
			BlockHeight: blockHeight,
			Timestamp:   detectedAt,
			Confidence:  confidence,
			TxHash:      strings.TrimSpace(txHash),
		}
		if item.ID == "" {
			item.ID = item.TxHash
		}
		if item.Title == "" {
			item.Title = formatThreatTitle(item.Type)
		}
		if item.Description == "" {
			item.Description = buildAnomalyDescription(&detectionPayload{
				ThreatType:   item.Type,
				Confidence:   item.Confidence,
				Description:  item.Description,
				ModelVersion: "",
			})
		}
		if item.Source == "" {
			item.Source = "ai-service"
		}

		anomalies = append(anomalies, item)
		if len(anomalies) >= limit {
			break
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anomaly rows: %w", err)
	}

	return anomalies, nil
}

func (s *Server) getAnomaliesFromDB(ctx context.Context, limit int, severityFilter string) ([]AnomalyResponse, error) {
	db, err := s.getDB()
	if err != nil {
		return nil, err
	}
	return queryAnomalies(ctx, db, limit, severityFilter)
}

// handleAnomalyStats handles GET /anomalies/stats
func (s *Server) handleAnomalyStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	db, err := s.getDB()
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get anomaly database handle", utils.ZapError(err))
		writeErrorFromUtils(w, r, NewInternalError("failed to retrieve anomaly statistics"))
		return
	}

	stats, err := queryAnomalyStats(ctx, db)
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

// queryAnomalyStats retrieves anomaly statistics from the dedicated anomalies table.
func queryAnomalyStats(ctx context.Context, db *sql.DB) (*AnomalyStatsResponse, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if db == nil {
		return nil, fmt.Errorf("db not available")
	}

	rows, err := db.QueryContext(queryCtx, `
		SELECT severity_value
		FROM anomalies
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query anomaly stats: %w", err)
	}
	defer rows.Close()

	stats := &AnomalyStatsResponse{}

	for rows.Next() {
		var severity float64
		if err := rows.Scan(&severity); err != nil {
			return nil, fmt.Errorf("failed to scan anomaly stats row: %w", err)
		}

		stats.TotalCount++
		switch mapSeverity(severity) {
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

func (s *Server) getAnomalyStats(ctx context.Context) (*AnomalyStatsResponse, error) {
	db, err := s.getDB()
	if err != nil {
		return nil, err
	}
	return queryAnomalyStats(ctx, db)
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
