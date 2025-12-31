package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	consensusapi "backend/pkg/consensus/api"
	"backend/pkg/utils"
)

const (
	apiVersion = "1.0.0"
)

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	readiness, _ := s.buildReadinessResponse(ctx)
	response := healthResponseFromReadiness(readiness)

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

func healthResponseFromReadiness(readiness ReadinessResponse) HealthResponse {
	status := "healthy"
	if !readiness.Ready {
		status = "degraded"
	} else if len(readiness.Checks) == 0 {
		status = "unknown"
	} else {
		for _, check := range readiness.Checks {
			if !isReadinessPassing(check.Status) {
				status = "degraded"
				break
			}
		}
	}

	return HealthResponse{
		Status:    status,
		Timestamp: time.Now().Unix(),
		Version:   apiVersion,
	}
}

// handleReadiness handles GET /ready
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response, statusCode := s.buildReadinessResponse(ctx)
	writeJSONResponse(w, r, NewSuccessResponse(response), statusCode)
}

func (s *Server) buildReadinessResponse(ctx context.Context) (ReadinessResponse, int) {
	checks := make(map[string]ReadinessCheckResult)
	details := make(map[string]interface{})
	phase := ""
	// Readiness policy:
	// - In strict mode, any failing check blocks readiness.
	// - In degraded bootstrap mode, only core control-plane checks block readiness;
	//   auxiliary dependencies (Kafka/Redis/AI) are reported but do not block.
	allowDegraded := s.config != nil && s.config.AllowDegradedBootstrap
	strict := !allowDegraded
	ready := true
	warnings := make([]string, 0, 4)

	mergeDetail := func(key string, val interface{}) {
		if val == nil {
			return
		}
		if key == "" {
			if m, ok := val.(map[string]interface{}); ok {
				for k, v := range m {
					details[k] = v
				}
			}
			return
		}
		details[key] = val
	}

	mark := func(name string, res ReadinessCheckResult, detailKey string, detailVal interface{}, required bool) {
		checks[name] = res
		if required && !isReadinessPassing(res.Status) {
			ready = false
		}
		switch res.Status {
		case "genesis", "single_node", "degraded", "warning":
			warning := res.Message
			if warning == "" {
				warning = fmt.Sprintf("%s status: %s", name, res.Status)
			} else {
				warning = fmt.Sprintf("%s: %s", name, warning)
			}
			warnings = append(warnings, warning)
		}
		if detailVal != nil {
			mergeDetail(detailKey, detailVal)
		} else if detailKey != "" && res.Message != "" {
			mergeDetail(detailKey, res.Message)
		}
		if res.Message != "" && detailKey == "" {
			mergeDetail(name+"_message", res.Message)
		}
	}

	storageResult, storageDetail := s.runStorageCheck(ctx)
	mark("storage", storageResult, "storage_error", nil, true)
	checks["cockroach"] = storageResult
	if storageDetail != nil {
		details["storage"] = storageDetail
		details["cockroach"] = storageDetail
	}
	if storageResult.Message != "" {
		details["cockroach_message"] = storageResult.Message
	}

	stateResult, stateDetail := s.runStateStoreCheck(ctx)
	mark("state", stateResult, "state_error", stateDetail, true)
	if stateResult.Status == "ok" && s.stateStore != nil {
		details["state_version"] = s.stateStore.Latest()
	}

	mark("mempool", s.runMempoolCheck(ctx), "", nil, true)

	consensusResult, consensusDetails, consensusPhase := s.runConsensusCheck(ctx)
	mark("consensus", consensusResult, "", consensusDetails, true)
	if consensusResult.Message != "" {
		details["consensus_error"] = consensusResult.Message
	}
	if consensusPhase != "" {
		phase = consensusPhase
	}

	p2pResult, p2pDetails := s.runP2PCheck(ctx)
	mark("p2p_quorum", p2pResult, "", p2pDetails, true)
	if p2pResult.Message != "" {
		details["p2p_quorum_error"] = p2pResult.Message
	}

	kafkaResult := s.runKafkaCheck(ctx)
	mark("kafka", kafkaResult, "kafka_error", nil, strict)

	redisResult, redisDetail := s.runRedisCheck(ctx)
	mark("redis", redisResult, "redis_error", nil, strict)
	if redisDetail != nil {
		details["redis"] = redisDetail
	}

	aiResult := s.runAIServiceCheck(ctx)
	mark("ai_service", aiResult, "ai_error", nil, strict)

	if phase == "" && s.engine != nil {
		if s.engine.IsConsensusActive() {
			phase = "active"
		} else {
			phase = "genesis"
		}
	}

	response := ReadinessResponse{
		Ready:     ready,
		Checks:    checks,
		Timestamp: time.Now().Unix(),
		Details:   details,
		Phase:     phase,
	}

	statusCode := http.StatusOK
	if !ready {
		statusCode = http.StatusServiceUnavailable
	}

	if len(warnings) > 0 {
		response.Warnings = warnings
	}

	return response, statusCode
}

func isReadinessPassing(status string) bool {
	switch status {
	case "ok", "degraded", "warning":
		return true
	case "not configured":
		return false
	default:
		return false
	}
}

func buildCheckResult(start time.Time, status string, err error) ReadinessCheckResult {
	res := ReadinessCheckResult{
		Status:    status,
		LatencyMs: float64(time.Since(start).Milliseconds()),
	}
	if err != nil {
		res.Message = err.Error()
	}
	return res
}

func (s *Server) runStorageCheck(ctx context.Context) (ReadinessCheckResult, map[string]interface{}) {
	start := time.Now()
	if s.storage == nil {
		return ReadinessCheckResult{Status: "not configured", LatencyMs: 0, Message: "storage adapter not initialized"}, nil
	}

	err := s.checkStorage(ctx)
	status := "ok"
	if err != nil {
		status = "not ready"
	}

	res := buildCheckResult(start, status, err)
	s.recordStorageLatency(res.LatencyMs)

	if err != nil {
		return res, map[string]interface{}{"error": err.Error()}
	}

	info := s.collectCockroachInfo(ctx)
	if len(info) == 0 {
		return res, nil
	}
	return res, info
}

func (s *Server) collectCockroachInfo(parentCtx context.Context) map[string]interface{} {
	provider, ok := s.storage.(interface{ GetDB() *sql.DB })
	if !ok {
		return nil
	}

	db := provider.GetDB()
	if db == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(parentCtx, 2*time.Second)
	defer cancel()

	info := make(map[string]interface{})

	var dbName sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&dbName); err == nil && dbName.Valid {
		info["database"] = dbName.String
	} else if err != nil && s.logger != nil {
		s.logger.Debug("Failed to fetch current database for readiness detail", utils.ZapError(err))
	}

	var version sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT version()").Scan(&version); err == nil && version.Valid {
		parts := strings.Fields(version.String)
		if len(parts) >= 2 {
			info["version"] = parts[1]
		}
		info["version_full"] = version.String
	} else if err != nil && s.logger != nil {
		s.logger.Debug("Failed to fetch Cockroach version", utils.ZapError(err))
	}

	var nodeID sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT node_id FROM crdb_internal.node_id()").Scan(&nodeID); err == nil && nodeID.Valid {
		info["node_id"] = nodeID.Int64
	} else if err != nil && s.logger != nil {
		s.logger.Debug("Failed to fetch Cockroach node id", utils.ZapError(err))
	}

	if len(info) == 0 {
		return nil
	}

	return info
}

func (s *Server) runStateStoreCheck(ctx context.Context) (ReadinessCheckResult, interface{}) {
	start := time.Now()
	if s.stateStore == nil {
		return ReadinessCheckResult{Status: "not configured", LatencyMs: 0, Message: "state store not initialized"}, nil
	}

	err := s.checkStateStore(ctx)
	status := "ok"
	var detail interface{}
	if err != nil {
		status = "not ready"
		detail = err.Error()
	}

	return buildCheckResult(start, status, err), detail
}

func (s *Server) runMempoolCheck(ctx context.Context) ReadinessCheckResult {
	start := time.Now()
	if s.mempool == nil {
		return ReadinessCheckResult{Status: "not configured", LatencyMs: 0, Message: "mempool disabled"}
	}
	return buildCheckResult(start, "ok", nil)
}

func (s *Server) runConsensusCheck(ctx context.Context) (ReadinessCheckResult, interface{}, string) {
	start := time.Now()
	if s.engine == nil {
		return ReadinessCheckResult{Status: "not configured", LatencyMs: 0, Message: "consensus engine not attached"}, nil, ""
	}

	status := s.engine.GetStatus()
	activation := consensusapi.PrivateGetActivationStatus(s.engine)

	details := map[string]interface{}{
		"consensus_ready_validators": activation.ReadyValidators,
		"consensus_required_ready":   activation.RequiredReady,
		"consensus_peers_seen":       activation.PeersSeen,
		"consensus_seen_quorum":      activation.SeenQuorum,
	}

	var message string
	var phase string

	switch {
	case activation.GenesisPhase:
		phase = "genesis"
		details["consensus_phase"] = "genesis"
		return buildCheckResult(start, "genesis", nil), details, phase
	case status.Running && s.engine.IsConsensusActive() && activation.HasQuorum:
		phase = "active"
		return buildCheckResult(start, "ok", nil), details, phase
	default:
		phase = "inactive"
		switch {
		case !status.Running:
			message = "engine not running"
		case !s.engine.IsConsensusActive():
			message = "consensus not active"
		case !activation.HasQuorum:
			message = "activation quorum not met"
		default:
			message = "unknown consensus degradation"
		}
		res := buildCheckResult(start, "not ready", errors.New(message))
		return res, details, phase
	}
}

func (s *Server) runP2PCheck(ctx context.Context) (ReadinessCheckResult, interface{}) {
	start := time.Now()
	if s.p2pRouter == nil || s.engine == nil {
		return ReadinessCheckResult{Status: "not configured", LatencyMs: 0, Message: "p2p router unavailable"}, nil
	}

	activation := consensusapi.PrivateGetActivationStatus(s.engine)
	details := map[string]interface{}{
		"p2p_connected_peers": activation.ConnectedPeers,
		"p2p_active_peers":    activation.ActivePeers,
		"p2p_required_peers":  activation.RequiredPeers,
		"p2p_quorum_2f+1":     activation.RequiredReady,
	}
	totalValidators := len(s.engine.ListValidators())
	details["p2p_total_validators"] = totalValidators

	switch {
	case activation.GenesisPhase:
		res := buildCheckResult(start, "genesis", nil)
		res.Message = "consensus still in genesis activation phase"
		return res, details
	case totalValidators <= 1:
		res := buildCheckResult(start, "single_node", nil)
		res.Message = "single-node topology; quorum disabled"
		return res, details
	case activation.ConnectedPeers >= activation.RequiredPeers:
		return buildCheckResult(start, "ok", nil), details
	case activation.ConnectedPeers == 0:
		msg := fmt.Sprintf("need %d peers, have 0", activation.RequiredPeers)
		return buildCheckResult(start, "no_peers", errors.New(msg)), details
	default:
		msg := fmt.Sprintf("connected %d, need %d (quorum %d/%d validators)", activation.ConnectedPeers, activation.RequiredPeers, activation.RequiredReady, totalValidators)
		return buildCheckResult(start, "insufficient", errors.New(msg)), details
	}
}

func (s *Server) runKafkaCheck(ctx context.Context) ReadinessCheckResult {
	start := time.Now()
	if s.kafkaProd == nil {
		return ReadinessCheckResult{Status: "not configured", LatencyMs: 0, Message: "kafka integration disabled"}
	}
	err := s.kafkaProd.HealthCheck(ctx)
	status := "ok"
	if err != nil {
		status = "not ready"
	}
	res := buildCheckResult(start, status, err)
	s.recordKafkaLatency(res.LatencyMs)
	if err == nil {
		kafkaLatency := s.getKafkaReadyLatencyMs()
		if res.LatencyMs > 1000 || kafkaLatency > 2000 {
			res.Status = "degraded"
			if res.Message == "" {
				res.Message = fmt.Sprintf("kafka latency %.0fms", math.Max(res.LatencyMs, kafkaLatency))
			}
		}
	}
	return res
}

func (s *Server) runRedisCheck(ctx context.Context) (ReadinessCheckResult, map[string]interface{}) {
	start := time.Now()
	if s.redisClient == nil {
		return ReadinessCheckResult{Status: "not configured", LatencyMs: 0, Message: "redis client not initialized"}, nil
	}

	err := s.redisClient.Ping(ctx).Err()
	status := "ok"
	if err != nil {
		status = "not ready"
	}

	res := buildCheckResult(start, status, err)
	if err != nil {
		return res, map[string]interface{}{"error": err.Error()}
	}

	info := s.collectRedisInfo(ctx)
	if err == nil {
		if res.LatencyMs > 500 {
			res.Status = "degraded"
			if res.Message == "" {
				res.Message = fmt.Sprintf("redis ping %.0fms", res.LatencyMs)
			}
		} else {
			var opsPerSec float64
			switch v := info["ops_per_sec"].(type) {
			case int:
				opsPerSec = float64(v)
			case int64:
				opsPerSec = float64(v)
			case float64:
				opsPerSec = v
			}

			var clients float64
			switch v := info["connected_clients"].(type) {
			case int:
				clients = float64(v)
			case int64:
				clients = float64(v)
			case float64:
				clients = v
			}

			if clients > 0 && opsPerSec == 0 {
				res.Status = "degraded"
				if res.Message == "" {
					res.Message = "redis ops/sec stalled"
				}
			}
		}
	}

	if len(info) == 0 {
		return res, nil
	}
	return res, info
}

func (s *Server) collectRedisInfo(parentCtx context.Context) map[string]interface{} {
	ctx, cancel := context.WithTimeout(parentCtx, 2*time.Second)
	defer cancel()

	infoStr, err := s.redisClient.Info(ctx, "server").Result()
	if err != nil {
		if s.logger != nil {
			s.logger.Debug("Failed to fetch Redis INFO", utils.ZapError(err))
		}
		return nil
	}

	info := make(map[string]interface{})
	for _, line := range strings.Split(infoStr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]
		switch key {
		case "redis_version":
			info["version"] = value
		case "redis_mode":
			info["mode"] = value
		case "role":
			info["role"] = value
		case "connected_clients":
			if v, err := strconv.Atoi(value); err == nil {
				info["connected_clients"] = v
			}
		case "uptime_in_seconds":
			if v, err := strconv.ParseInt(value, 10, 64); err == nil {
				info["uptime_seconds"] = v
			}
		}
	}

	statsStr, err := s.redisClient.Info(ctx, "stats").Result()
	if err == nil {
		for _, line := range strings.Split(statsStr, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			switch parts[0] {
			case "total_connections_received":
				if v, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					info["total_connections_received"] = v
				}
			case "total_commands_processed":
				if v, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					info["total_commands_processed"] = v
				}
			case "instantaneous_ops_per_sec":
				if v, err := strconv.Atoi(parts[1]); err == nil {
					info["ops_per_sec"] = v
				}
			}
		}
	} else if s.logger != nil {
		s.logger.Debug("Failed to fetch Redis stats INFO", utils.ZapError(err))
	}

	if len(info) == 0 {
		return nil
	}

	return info
}

func (s *Server) runAIServiceCheck(ctx context.Context) ReadinessCheckResult {
	start := time.Now()
	if s.aiClient == nil || s.aiBaseURL == "" {
		return ReadinessCheckResult{Status: "not configured", LatencyMs: 0, Message: "ai service integration disabled"}
	}

	ready, msg, err := s.pingAIService(ctx)
	status := "ok"
	var finalErr error
	if err != nil {
		status = "not ready"
		finalErr = err
	} else if !ready {
		status = "not ready"
		if msg == "" {
			msg = "ai service not ready"
		}
		finalErr = errors.New(msg)
	}

	res := buildCheckResult(start, status, finalErr)
	if finalErr == nil && msg != "" {
		res.Message = msg
	}

	if finalErr == nil {
		metricsCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		aiMetrics, degraded := s.loadAIMetricsSnapshot(metricsCtx)
		cancel()
		if degraded {
			status = "degraded"
			if aiMetrics != nil && aiMetrics.Message != "" {
				res.Message = aiMetrics.Message
			} else if res.Message == "" {
				res.Message = "ai service metrics degraded"
			}
		}
	}

	res.Status = status
	return res
}

func (s *Server) pingAIService(ctx context.Context) (bool, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/ready", s.aiBaseURL), nil)
	if err != nil {
		return false, "", err
	}
	if s.aiAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.aiAuthToken)
	}

	resp, err := s.aiClient.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("ai ready status %d", resp.StatusCode), nil
	}

	var payload struct {
		Ready bool   `json:"ready"`
		State string `json:"state"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false, "", fmt.Errorf("invalid ai readiness response: %w", err)
	}

	if !payload.Ready {
		msg := payload.State
		if msg == "" {
			msg = payload.Error
		}
		if msg == "" {
			msg = "ai service not ready"
		}
		return false, msg, nil
	}

	return true, "", nil
}

// checkStorage verifies storage is accessible
func (s *Server) checkStorage(ctx context.Context) error {
	if s.storage == nil {
		return NewUnavailableError("storage not initialized")
	}
	if err := s.storage.Ping(ctx); err != nil {
		return NewUnavailableError("storage ping failed")
	}
	return nil
}

// checkStateStore verifies state store is accessible
func (s *Server) checkStateStore(ctx context.Context) error {
	if s.stateStore == nil {
		return NewUnavailableError("state store not initialized")
	}

	// Verify we can get the latest version
	latest := s.stateStore.Latest()

	// Verify we can get the root for the latest version
	_, exists := s.stateStore.Root(latest)
	if !exists {
		return NewUnavailableError("state root not found")
	}

	return nil
}

// Helper functions for JSON responses

// writeJSONResponse writes a successful JSON response
func writeJSONResponse(w http.ResponseWriter, r *http.Request, response *Response, statusCode int) {
	requestID := getRequestID(r.Context())
	if requestID != "" {
		if response.Meta == nil {
			response.Meta = &MetaDTO{}
		}
		response.Meta.RequestID = requestID
	}

	payload, err := json.Marshal(response)
	if err != nil {
		fallback := NewErrorResponseSimple("ENCODING_ERROR", fmt.Sprintf("failed to encode response: %v", err), requestID)
		fallbackPayload, marshalErr := json.Marshal(fallback)
		if marshalErr != nil {
			fallbackPayload = []byte("{\"success\":false,\"error\":{\"code\":\"ENCODING_ERROR\",\"message\":\"failed to encode response\"}}")
		}
		w.Header().Set(HeaderContentType, ContentTypeJSON)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(fallbackPayload)
		return
	}

	w.Header().Set(HeaderContentType, ContentTypeJSON)
	w.WriteHeader(statusCode)
	w.Write(payload)
}

// writeErrorResponse writes an error JSON response
func writeErrorResponse(w http.ResponseWriter, r *http.Request, code, message string, statusCode int) {
	requestID := getRequestID(r.Context())
	response := NewErrorResponseSimple(code, message, requestID)

	w.Header().Set(HeaderContentType, ContentTypeJSON)
	w.WriteHeader(statusCode)

	encoder := json.NewEncoder(w)
	encoder.Encode(response)
}

// writeErrorFromUtils writes an error response from utils.Error
func writeErrorFromUtils(w http.ResponseWriter, r *http.Request, err *utils.Error) {
	requestID := getRequestID(r.Context())
	response := NewErrorResponse(err, requestID)

	statusCode := err.GetHTTPStatus()
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}

	w.Header().Set(HeaderContentType, ContentTypeJSON)
	w.WriteHeader(statusCode)

	encoder := json.NewEncoder(w)
	encoder.Encode(response)
}
