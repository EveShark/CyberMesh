package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backend/pkg/utils"
)

func (s *Server) collectAIMetrics(ctx context.Context) (*AIMetricsResponse, error) {
	if s.aiClient == nil || strings.TrimSpace(s.aiBaseURL) == "" {
		return nil, fmt.Errorf("ai service integration disabled")
	}

	payload, err := s.fetchAIDetectionStats(ctx)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, fmt.Errorf("ai service returned empty payload")
	}

	response := &AIMetricsResponse{
		Status:   payload.Status,
		State:    payload.State,
		Message:  payload.Message,
		Engines:  mapAiEngineMetrics(payload.Engines),
		Variants: mapAiVariantMetrics(payload.Variants),
	}

	if payload.DetectionLoop != nil {
		var issues []string
		if len(payload.DetectionLoop.Issues) > 0 {
			issues = append([]string(nil), payload.DetectionLoop.Issues...)
		}
		response.Loop = &AiDetectionLoopSummary{
			Running:                   payload.DetectionLoop.Running,
			Status:                    payload.DetectionLoop.Status,
			Message:                   payload.DetectionLoop.Message,
			Issues:                    issues,
			Blocking:                  payload.DetectionLoop.Blocking,
			Healthy:                   payload.DetectionLoop.Healthy,
			AvgLatencyMs:              payload.DetectionLoop.AvgLatencyMs,
			LastLatencyMs:             payload.DetectionLoop.LastLatencyMs,
			SecondsSinceLastDetection: payload.DetectionLoop.SecondsSinceLastDetection,
			SecondsSinceLastIteration: payload.DetectionLoop.SecondsSinceLastIteration,
			CacheAgeSeconds:           payload.DetectionLoop.CacheAgeSeconds,
		}
		if payload.DetectionLoop.Metrics != nil {
			response.Loop.Counters = &AiDetectionLoopCounters{
				DetectionsTotal:       payload.DetectionLoop.Metrics.DetectionsTotal,
				DetectionsPublished:   payload.DetectionLoop.Metrics.DetectionsPublished,
				DetectionsRateLimited: payload.DetectionLoop.Metrics.DetectionsRateLimited,
				Errors:                payload.DetectionLoop.Metrics.Errors,
				LoopIterations:        payload.DetectionLoop.Metrics.LoopIterations,
			}
		}
	}

	if payload.Derived != nil {
		response.Derived = &AiMetricsDerived{
			DetectionsPerMinute:  payload.Derived.DetectionsPerMinute,
			PublishRatePerMinute: payload.Derived.PublishRatePerMinute,
			IterationsPerMinute:  payload.Derived.IterationsPerMinute,
			ErrorRatePerHour:     payload.Derived.ErrorRatePerHour,
			PublishSuccessRatio:  payload.Derived.PublishSuccessRatio,
		}
	}

	return response, nil
}

func (s *Server) handleAIMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	metrics, err := s.collectAIMetrics(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "failed to fetch AI detection stats",
				utils.ZapError(err))
		}
		writeErrorResponse(w, r, "AI_SERVICE_UNAVAILABLE", "failed to fetch detection stats", http.StatusBadGateway)
		return
	}

	writeJSONResponse(w, r, NewSuccessResponse(metrics), http.StatusOK)
}

func (s *Server) handleAIVariants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.aiClient == nil || strings.TrimSpace(s.aiBaseURL) == "" {
		writeErrorResponse(w, r, "AI_SERVICE_DISABLED", "ai service integration disabled", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	payload, err := s.fetchAIDetectionStats(ctx)
	if err != nil {
		writeErrorResponse(w, r, "AI_SERVICE_UNAVAILABLE", "failed to fetch detection stats", http.StatusBadGateway)
		return
	}

	variants := mapAiVariantMetrics(payload.Variants)
	writeJSONResponse(w, r, NewSuccessResponse(variants), http.StatusOK)
}

func (s *Server) handleAIDetectionHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.aiClient == nil || strings.TrimSpace(s.aiBaseURL) == "" {
		writeErrorResponse(w, r, "AI_SERVICE_DISABLED", "ai service integration disabled", http.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query()
	limit := 50
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		if parsed, err := parseInt(raw); err == nil {
			limit = parsed
		}
	}
	limit = clampIntValue(limit, 1, 200)

	since := strings.TrimSpace(query.Get("since"))
	validatorID := strings.TrimSpace(query.Get("validator_id"))

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	payload, err := s.fetchAIDetectionHistory(ctx, limit, since, validatorID)
	if err != nil {
		writeErrorResponse(w, r, "AI_SERVICE_UNAVAILABLE", "failed to fetch detection history", http.StatusBadGateway)
		return
	}

	writeJSONResponse(w, r, NewSuccessResponse(payload), http.StatusOK)
}

func (s *Server) handleAISuspiciousNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.aiClient == nil || strings.TrimSpace(s.aiBaseURL) == "" {
		writeErrorResponse(w, r, "AI_SERVICE_DISABLED", "ai service integration disabled", http.StatusServiceUnavailable)
		return
	}

	limit := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := parseInt(raw); err == nil {
			limit = parsed
		}
	}
	limit = clampIntValue(limit, 1, 50)

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	payload, err := s.fetchAISuspiciousNodesDetailed(ctx, limit)
	if err != nil {
		writeErrorResponse(w, r, "AI_SERVICE_UNAVAILABLE", "failed to fetch suspicious nodes", http.StatusBadGateway)
		return
	}

	writeJSONResponse(w, r, NewSuccessResponse(payload), http.StatusOK)
}

func (s *Server) fetchAIDetectionHistory(ctx context.Context, limit int, since, validatorID string) (*AiDetectionHistoryResponse, error) {
	resp, err := s.performAIRequest(ctx, http.MethodGet, "/detections/history", url.Values{
		"limit": {strconv.Itoa(limit)},
		"since": func() []string {
			if since == "" {
				return nil
			}
			return []string{since}
		}(),
		"validator_id": func() []string {
			if validatorID == "" {
				return nil
			}
			return []string{validatorID}
		}(),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ai service history request failed: status=%d", resp.StatusCode)
	}

	var payload AiDetectionHistoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode ai history response: %w", err)
	}

	if payload.UpdatedAt == "" {
		payload.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	return &payload, nil
}

func (s *Server) fetchAISuspiciousNodesDetailed(ctx context.Context, limit int) (*AiSuspiciousNodesResponse, error) {
	resp, err := s.performAIRequest(ctx, http.MethodGet, "/detections/suspicious-nodes", url.Values{
		"limit": {strconv.Itoa(limit)},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ai service suspicious nodes request failed: status=%d", resp.StatusCode)
	}

	var payload AiSuspiciousNodesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode ai suspicious nodes response: %w", err)
	}

	if payload.UpdatedAt == "" {
		payload.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	return &payload, nil
}

func (s *Server) performAIRequest(ctx context.Context, method, path string, query url.Values) (*http.Response, error) {
	if s.aiClient == nil {
		return nil, fmt.Errorf("ai client not configured")
	}

	base := strings.TrimRight(s.aiBaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("ai base url not configured")
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	endpoint := base + path
	if len(query) > 0 {
		filtered := url.Values{}
		for key, values := range query {
			if len(values) == 0 || values[0] == "" {
				continue
			}
			filtered[key] = values
		}
		if len(filtered) > 0 {
			endpoint = endpoint + "?" + filtered.Encode()
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	if s.aiAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.aiAuthToken)
	}

	return s.aiClient.Do(req)
}

func mapAiEngineMetrics(payload []aiDetectionEngineEntry) []AiEngineMetric {
	metrics := make([]AiEngineMetric, 0, len(payload))
	for _, item := range payload {
		var avgConfidence *float64
		if item.AvgConfidence != nil {
			v := *item.AvgConfidence
			avgConfidence = &v
		}
		var lastLatency *float64
		if item.LastLatencyMs != nil {
			v := *item.LastLatencyMs
			lastLatency = &v
		}
		var lastUpdated *float64
		if item.LastUpdated != nil {
			v := *item.LastUpdated
			lastUpdated = &v
		}

		metrics = append(metrics, AiEngineMetric{
			Engine:              item.Engine,
			Ready:               item.Ready,
			Candidates:          item.Candidates,
			Published:           item.Published,
			PublishRatio:        item.PublishRatio,
			ThroughputPerMinute: item.ThroughputPerMinute,
			AvgConfidence:       avgConfidence,
			ThreatTypes:         append([]string(nil), item.ThreatTypes...),
			LastLatencyMs:       lastLatency,
			LastUpdated:         lastUpdated,
		})
	}
	return metrics
}

func mapAiVariantMetrics(payload []aiDetectionVariantEntry) []AiVariantMetric {
	variants := make([]AiVariantMetric, 0, len(payload))
	for _, item := range payload {
		var avgConfidence *float64
		if item.AvgConfidence != nil {
			v := *item.AvgConfidence
			avgConfidence = &v
		}
		var lastUpdated *float64
		if item.LastUpdated != nil {
			v := *item.LastUpdated
			lastUpdated = &v
		}

		variants = append(variants, AiVariantMetric{
			Variant:             item.Variant,
			Engines:             append([]string(nil), item.Engines...),
			Total:               item.Total,
			Published:           item.Published,
			PublishRatio:        item.PublishRatio,
			ThroughputPerMinute: item.ThroughputPerMinute,
			AvgConfidence:       avgConfidence,
			ThreatTypes:         append([]string(nil), item.ThreatTypes...),
			LastUpdated:         lastUpdated,
		})
	}
	return variants
}

func mapAIDetectionLoopCounters(metrics map[string]float64) *AiDetectionLoopCounters {
	if len(metrics) == 0 {
		return nil
	}

	var counters AiDetectionLoopCounters
	found := false

	if value, ok := extractUintCounter(metrics, "detections_total"); ok {
		counters.DetectionsTotal = value
		found = true
	}
	if value, ok := extractUintCounter(metrics, "detections_published"); ok {
		counters.DetectionsPublished = value
		found = true
	}
	if value, ok := extractUintCounter(metrics, "detections_rate_limited"); ok {
		counters.DetectionsRateLimited = value
		found = true
	}
	if value, ok := extractUintCounter(metrics, "errors"); ok {
		counters.Errors = value
		found = true
	}
	if value, ok := extractUintCounter(metrics, "loop_iterations"); ok {
		counters.LoopIterations = value
		found = true
	}

	if !found {
		return nil
	}

	return &counters
}

func extractUintCounter(metrics map[string]float64, key string) (uint64, bool) {
	value, ok := metrics[key]
	if !ok || math.IsNaN(value) {
		return 0, false
	}
	if value < 0 {
		return 0, false
	}
	return uint64(math.Trunc(value + 1e-9)), true
}

func clampIntValue(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
