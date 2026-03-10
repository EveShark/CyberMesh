package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"backend/pkg/control/policyoutbox"
	"backend/pkg/control/policytrace"
)

const (
	controlListDefaultLimit = 50
	controlListMaxLimit     = 200
)

type controlOutboxRow struct {
	ID              string
	BlockHeight     int64
	BlockTS         int64
	TxIndex         int
	PolicyID        string
	TraceID         sql.NullString
	AIEventTsMs     sql.NullInt64
	SourceEventID   sql.NullString
	SourceEventTsMs sql.NullInt64
	Payload         []byte
	Status          string
	Retries         int64
	NextRetryAt     sql.NullTime
	LastError       sql.NullString
	LeaseHolder     sql.NullString
	LeaseEpoch      int64
	KafkaTopic      sql.NullString
	KafkaPartition  sql.NullInt64
	KafkaOffset     sql.NullInt64
	PublishedAt     sql.NullTime
	AckResult       sql.NullString
	AckReason       sql.NullString
	AckController   sql.NullString
	AckedAt         sql.NullTime
	CreatedAt       time.Time
	UpdatedAt       time.Time
	RuleHash        []byte
}

type controlOutboxRowDTO struct {
	ID              string `json:"id"`
	BlockHeight     int64  `json:"block_height"`
	BlockTS         int64  `json:"block_ts"`
	TxIndex         int    `json:"tx_index"`
	PolicyID        string `json:"policy_id"`
	TraceID         string `json:"trace_id,omitempty"`
	AIEventTsMs     int64  `json:"ai_event_ts_ms,omitempty"`
	SourceEventID   string `json:"source_event_id,omitempty"`
	SourceEventTsMs int64  `json:"source_event_ts_ms,omitempty"`
	Status          string `json:"status"`
	Retries         int64  `json:"retries"`
	NextRetryAt     int64  `json:"next_retry_at,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	LeaseHolder     string `json:"lease_holder,omitempty"`
	LeaseEpoch      int64  `json:"lease_epoch"`
	KafkaTopic      string `json:"kafka_topic,omitempty"`
	KafkaPartition  int64  `json:"kafka_partition,omitempty"`
	KafkaOffset     int64  `json:"kafka_offset,omitempty"`
	PublishedAt     int64  `json:"published_at,omitempty"`
	AckResult       string `json:"ack_result,omitempty"`
	AckReason       string `json:"ack_reason,omitempty"`
	AckController   string `json:"ack_controller,omitempty"`
	AckedAt         int64  `json:"acked_at,omitempty"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	RuleHashHex     string `json:"rule_hash_hex,omitempty"`
}

type controlListMeta struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type outboxBacklogDTO struct {
	Pending            int64   `json:"pending"`
	Retry              int64   `json:"retry"`
	Publishing         int64   `json:"publishing"`
	PublishedRows      int64   `json:"published_rows"`
	AckedRows          int64   `json:"acked_rows"`
	TerminalRows       int64   `json:"terminal_rows"`
	TotalRows          int64   `json:"total_rows"`
	OldestPendingAgeMs int64   `json:"oldest_pending_age_ms"`
	AckClosureRatio    float64 `json:"ack_closure_ratio"`
}

type leaseRowDTO struct {
	LeaseKey   string `json:"lease_key"`
	HolderID   string `json:"holder_id"`
	Epoch      int64  `json:"epoch"`
	LeaseUntil int64  `json:"lease_until"`
	UpdatedAt  int64  `json:"updated_at"`
	IsActive   bool   `json:"is_active"`
	StaleByMs  int64  `json:"stale_by_ms,omitempty"`
	NowUnixMs  int64  `json:"now_unix_ms"`
}

type controlAcksListResponse struct {
	Rows       []policyAckPayload `json:"rows"`
	Pagination controlListMeta    `json:"pagination"`
}

type controlOutboxListResponse struct {
	Rows       []controlOutboxRowDTO `json:"rows"`
	Pagination controlListMeta       `json:"pagination"`
}

type controlTraceResponse struct {
	PolicyID       string                  `json:"policy_id"`
	Outbox         []controlOutboxRowDTO   `json:"outbox"`
	Acks           []policyAckPayload      `json:"acks"`
	RuntimeMarkers []runtimeTraceMarkerDTO `json:"runtime_markers,omitempty"`
	Materialized   *materializedTraceDTO   `json:"materialized,omitempty"`
}

type runtimeTraceMarkerDTO struct {
	Stage       string `json:"stage"`
	PolicyID    string `json:"policy_id"`
	TraceID     string `json:"trace_id,omitempty"`
	TimestampMs int64  `json:"t_ms"`
	Height      uint64 `json:"height,omitempty"`
	View        uint64 `json:"view,omitempty"`
	QCTsMs      int64  `json:"qc_ts_ms,omitempty"`
	OutboxID    string `json:"outbox_id,omitempty"`
	Partition   int32  `json:"partition,omitempty"`
	Offset      int64  `json:"offset,omitempty"`
}

type materializedTraceDTO struct {
	TraceID         string                        `json:"trace_id,omitempty"`
	SourceEventID   string                        `json:"source_event_id,omitempty"`
	SourceEventTsMs int64                         `json:"source_event_ts_ms,omitempty"`
	AIEventTsMs     int64                         `json:"ai_event_ts_ms,omitempty"`
	Stages          []materializedTraceStageDTO   `json:"stages,omitempty"`
	Latencies       []materializedTraceLatencyDTO `json:"latencies,omitempty"`
}

type materializedTraceStageDTO struct {
	Stage       string `json:"stage"`
	Source      string `json:"source"`
	TimestampMs int64  `json:"t_ms"`
}

type materializedTraceLatencyDTO struct {
	Name       string `json:"name"`
	DurationMs int64  `json:"duration_ms"`
}

type tenantScopeError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *tenantScopeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (s *Server) handleControlOutboxBacklog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	stats, err := s.loadOutboxBacklog(r.Context())
	if err != nil {
		writeErrorResponse(w, r, "OUTBOX_BACKLOG_QUERY_FAILED", "failed to load outbox backlog", http.StatusInternalServerError)
		return
	}
	resp := outboxBacklogDTO{
		Pending:            stats.Pending,
		Retry:              stats.Retry,
		Publishing:         stats.Publishing,
		PublishedRows:      stats.PublishedRows,
		AckedRows:          stats.AckedRows,
		TerminalRows:       stats.TerminalRows,
		TotalRows:          stats.TotalRows,
		OldestPendingAgeMs: stats.OldestPendingAge,
	}
	if stats.PublishedRows > 0 {
		resp.AckClosureRatio = float64(stats.AckedRows) / float64(stats.PublishedRows)
	}

	writeJSONResponse(w, r, NewSuccessResponse(resp), http.StatusOK)
}

func (s *Server) handleControlOutboxList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, err := parseControlLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeErrorResponse(w, r, "INVALID_LIMIT", err.Error(), http.StatusBadRequest)
		return
	}

	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		return
	}

	where := make([]string, 0, 8)
	args := make([]interface{}, 0, 16)
	where = append(where, "1=1")
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}
	if tenantScope != "" {
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM policy_acks pa WHERE pa.policy_id = control_policy_outbox.policy_id AND pa.tenant = $%d)", len(args)+1))
		args = append(args, tenantScope)
	}

	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if !isValidOutboxStatus(status) {
			writeErrorResponse(w, r, "INVALID_STATUS", "status must be one of pending,publishing,published,retry,terminal_failed,acked", http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, status)
	}
	if policyID := strings.TrimSpace(r.URL.Query().Get("policy_id")); policyID != "" {
		where = append(where, fmt.Sprintf("policy_id = $%d", len(args)+1))
		args = append(args, policyID)
	}
	if traceID := strings.TrimSpace(r.URL.Query().Get("trace_id")); traceID != "" {
		where = append(where, fmt.Sprintf("trace_id = $%d", len(args)+1))
		args = append(args, traceID)
	}
	if blockHeight := strings.TrimSpace(r.URL.Query().Get("block_height")); blockHeight != "" {
		h, parseErr := strconv.ParseInt(blockHeight, 10, 64)
		if parseErr != nil || h < 0 {
			writeErrorResponse(w, r, "INVALID_BLOCK_HEIGHT", "block_height must be a positive integer", http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("block_height = $%d", len(args)+1))
		args = append(args, h)
	}
	if fromRaw := strings.TrimSpace(r.URL.Query().Get("from")); fromRaw != "" {
		from, parseErr := parseControlTimeFilter(fromRaw)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_FROM", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("created_at >= $%d", len(args)+1))
		args = append(args, from)
	}
	if toRaw := strings.TrimSpace(r.URL.Query().Get("to")); toRaw != "" {
		to, parseErr := parseControlTimeFilter(toRaw)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_TO", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("created_at <= $%d", len(args)+1))
		args = append(args, to)
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("cursor")); cursor != "" {
		cursorAt, cursorID, parseErr := decodeOutboxCursor(cursor)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_CURSOR", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("(created_at < $%d OR (created_at = $%d AND id::STRING < $%d))", len(args)+1, len(args)+1, len(args)+2))
		args = append(args, cursorAt, cursorID)
	}

	fetchLimit := limit + 1
	query := fmt.Sprintf(`
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			trace_id, ai_event_ts_ms, status, retries, next_retry_at,
			last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at,
			ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`, strings.Join(where, " AND "), len(args)+1)
	args = append(args, fetchLimit)

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		writeErrorResponse(w, r, "OUTBOX_QUERY_FAILED", "failed to query outbox rows", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]controlOutboxRowDTO, 0, limit)
	for rows.Next() {
		var row controlOutboxRow
		scanErr := rows.Scan(
			&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
			&row.TraceID, &row.AIEventTsMs, &row.Status, &row.Retries, &row.NextRetryAt,
			&row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
			&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt,
			&row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
			&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
		)
		if scanErr != nil {
			writeErrorResponse(w, r, "OUTBOX_SCAN_FAILED", "failed to read outbox row", http.StatusInternalServerError)
			return
		}
		items = append(items, outboxRowToDTO(row))
	}
	if err := rows.Err(); err != nil {
		writeErrorResponse(w, r, "OUTBOX_ITERATION_FAILED", "failed to iterate outbox rows", http.StatusInternalServerError)
		return
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		nextCursor = encodeOutboxCursor(time.Unix(last.CreatedAt, 0).UTC(), last.ID)
		items = items[:limit]
	}

	writeJSONResponse(w, r, NewSuccessResponse(controlOutboxListResponse{
		Rows:       items,
		Pagination: controlListMeta{Limit: limit, NextCursor: nextCursor},
	}), http.StatusOK)
}

func (s *Server) handleControlOutboxGet(w http.ResponseWriter, r *http.Request) {
	prefix := strings.TrimSuffix(s.config.BasePath, "/") + "/control/outbox/"
	rowRef := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if rowRef == "" {
		writeErrorResponse(w, r, "INVALID_ROW_ID", "outbox row id is required", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodPost {
		s.handleControlOutboxMutation(w, r, rowRef)
		return
	}
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET/POST methods allowed", http.StatusMethodNotAllowed)
		return
	}

	rowID := rowRef
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}

	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	var row controlOutboxRow
	err = db.QueryRowContext(ctx, `
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			trace_id, ai_event_ts_ms, status, retries, next_retry_at,
			last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at,
			ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE id::STRING = $1
	`, rowID).Scan(
		&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
		&row.TraceID, &row.AIEventTsMs, &row.Status, &row.Retries, &row.NextRetryAt,
		&row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
		&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt,
		&row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
		&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
	)
	if err == sql.ErrNoRows {
		writeErrorResponse(w, r, "OUTBOX_ROW_NOT_FOUND", "outbox row not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeErrorResponse(w, r, "OUTBOX_QUERY_FAILED", "failed to query outbox row", http.StatusInternalServerError)
		return
	}
	if tenantScope != "" {
		var visible int
		visErr := db.QueryRowContext(ctx, `
			SELECT 1
			FROM policy_acks
			WHERE policy_id = $1 AND tenant = $2
			LIMIT 1
		`, row.PolicyID, tenantScope).Scan(&visible)
		if visErr == sql.ErrNoRows {
			writeErrorResponse(w, r, "OUTBOX_ROW_NOT_FOUND", "outbox row not found", http.StatusNotFound)
			return
		}
		if visErr != nil {
			writeErrorResponse(w, r, "OUTBOX_SCOPE_QUERY_FAILED", "failed to evaluate outbox row scope", http.StatusInternalServerError)
			return
		}
	}

	writeJSONResponse(w, r, NewSuccessResponse(outboxRowToDTO(row)), http.StatusOK)
}

func (s *Server) handleControlTraceList(w http.ResponseWriter, r *http.Request) {
	const endpointName = "control.trace.list"
	if err := s.checkControlBreaker(endpointName); err != nil {
		writeErrorResponse(w, r, "CONTROL_API_CIRCUIT_OPEN", "trace endpoint temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		s.recordControlBreakerSuccess(endpointName)
		return
	}
	policyID := strings.TrimSpace(r.URL.Query().Get("policy_id"))
	traceID := strings.TrimSpace(r.URL.Query().Get("trace_id"))
	if policyID == "" && traceID == "" {
		writeErrorResponse(w, r, "INDEXED_SELECTOR_REQUIRED", "trace lookup requires policy_id or trace_id", http.StatusBadRequest)
		return
	}
	limit, err := parseControlLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeErrorResponse(w, r, "INVALID_LIMIT", err.Error(), http.StatusBadRequest)
		return
	}
	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		s.recordControlBreakerFailure(endpointName)
		return
	}
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}

	where := []string{"1=1"}
	args := make([]interface{}, 0, 16)
	if tenantScope != "" {
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM policy_acks pa WHERE pa.policy_id = control_policy_outbox.policy_id AND pa.tenant = $%d)", len(args)+1))
		args = append(args, tenantScope)
	}
	if policyID != "" {
		where = append(where, fmt.Sprintf("policy_id = $%d", len(args)+1))
		args = append(args, policyID)
	}
	if traceID != "" {
		where = append(where, fmt.Sprintf("trace_id = $%d", len(args)+1))
		args = append(args, traceID)
	}
	if qcRef := strings.TrimSpace(r.URL.Query().Get("qc_reference")); qcRef != "" {
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM policy_acks qa WHERE qa.policy_id = control_policy_outbox.policy_id AND qa.qc_reference = $%d)", len(args)+1))
		args = append(args, qcRef)
	}
	if ruleHashHex := strings.TrimSpace(r.URL.Query().Get("rule_hash")); ruleHashHex != "" {
		ruleHashBytes, parseErr := hex.DecodeString(ruleHashHex)
		if parseErr != nil || len(ruleHashBytes) == 0 {
			writeErrorResponse(w, r, "INVALID_RULE_HASH", "rule_hash must be valid hex", http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("rule_hash = $%d", len(args)+1))
		args = append(args, ruleHashBytes)
	}
	if fromRaw := strings.TrimSpace(r.URL.Query().Get("from")); fromRaw != "" {
		from, parseErr := parseControlTimeFilter(fromRaw)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_FROM", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("created_at >= $%d", len(args)+1))
		args = append(args, from)
	}
	if toRaw := strings.TrimSpace(r.URL.Query().Get("to")); toRaw != "" {
		to, parseErr := parseControlTimeFilter(toRaw)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_TO", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("created_at <= $%d", len(args)+1))
		args = append(args, to)
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("cursor")); cursor != "" {
		cursorAt, cursorID, parseErr := decodeOutboxCursor(cursor)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_CURSOR", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("(created_at < $%d OR (created_at = $%d AND id::STRING < $%d))", len(args)+1, len(args)+1, len(args)+2))
		args = append(args, cursorAt, cursorID)
	}

	fetchLimit := limit + 1
	query := fmt.Sprintf(`
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			trace_id, ai_event_ts_ms, status, retries, next_retry_at,
			last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at,
			ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`, strings.Join(where, " AND "), len(args)+1)
	args = append(args, fetchLimit)

	ctx, cancel := context.WithTimeout(r.Context(), s.controlTraceTimeout())
	defer cancel()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		s.noteControlTimeout(err, false)
		writeErrorResponse(w, r, "TRACE_QUERY_FAILED", "failed to query trace rows", http.StatusInternalServerError)
		s.recordControlBreakerFailure(endpointName)
		return
	}
	defer rows.Close()

	items := make([]controlOutboxRowDTO, 0, limit)
	for rows.Next() {
		var row controlOutboxRow
		if scanErr := rows.Scan(
			&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
			&row.TraceID, &row.AIEventTsMs, &row.Status, &row.Retries, &row.NextRetryAt,
			&row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
			&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt,
			&row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
			&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
		); scanErr != nil {
			writeErrorResponse(w, r, "TRACE_SCAN_FAILED", "failed to read trace row", http.StatusInternalServerError)
			s.recordControlBreakerFailure(endpointName)
			return
		}
		items = append(items, outboxRowToDTO(row))
	}
	if err := rows.Err(); err != nil {
		s.noteControlTimeout(err, false)
		writeErrorResponse(w, r, "TRACE_ITERATION_FAILED", "failed to iterate trace rows", http.StatusInternalServerError)
		s.recordControlBreakerFailure(endpointName)
		return
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		nextCursor = encodeOutboxCursor(time.Unix(last.CreatedAt, 0).UTC(), last.ID)
		items = items[:limit]
	}

	writeJSONResponse(w, r, NewSuccessResponse(controlOutboxListResponse{
		Rows:       items,
		Pagination: controlListMeta{Limit: limit, NextCursor: nextCursor},
	}), http.StatusOK)
	s.recordControlBreakerSuccess(endpointName)
}

func (s *Server) handleControlTraceByPolicy(w http.ResponseWriter, r *http.Request) {
	const endpointName = "control.trace.by_policy"
	if err := s.checkControlBreaker(endpointName); err != nil {
		writeErrorResponse(w, r, "CONTROL_API_CIRCUIT_OPEN", "trace endpoint temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		s.recordControlBreakerSuccess(endpointName)
		return
	}
	prefix := strings.TrimSuffix(s.config.BasePath, "/") + "/control/trace/"
	policyID := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if policyID == "" {
		writeErrorResponse(w, r, "INVALID_POLICY_ID", "policy_id is required", http.StatusBadRequest)
		return
	}
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}

	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		s.recordControlBreakerFailure(endpointName)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.controlTraceTimeout())
	defer cancel()

	outboxWhere := "policy_id = $1"
	outboxArgs := []interface{}{policyID}
	if tenantScope != "" {
		outboxWhere = outboxWhere + fmt.Sprintf(" AND EXISTS (SELECT 1 FROM policy_acks pa WHERE pa.policy_id = control_policy_outbox.policy_id AND pa.tenant = $%d)", len(outboxArgs)+1)
		outboxArgs = append(outboxArgs, tenantScope)
	}
	outboxRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, payload, status, retries, next_retry_at,
			last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at,
			ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT 200
	`, outboxWhere), outboxArgs...)
	if err != nil {
		s.noteControlTimeout(err, false)
		writeErrorResponse(w, r, "TRACE_OUTBOX_QUERY_FAILED", "failed to query trace outbox rows", http.StatusInternalServerError)
		s.recordControlBreakerFailure(endpointName)
		return
	}
	defer outboxRows.Close()

	outboxRowsRaw := make([]controlOutboxRow, 0, 32)
	outbox := make([]controlOutboxRowDTO, 0, 32)
	for outboxRows.Next() {
		var row controlOutboxRow
		if scanErr := outboxRows.Scan(
			&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
			&row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.Payload, &row.Status, &row.Retries, &row.NextRetryAt,
			&row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
			&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt,
			&row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
			&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
		); scanErr != nil {
			writeErrorResponse(w, r, "TRACE_OUTBOX_SCAN_FAILED", "failed to read trace outbox row", http.StatusInternalServerError)
			s.recordControlBreakerFailure(endpointName)
			return
		}
		outboxRowsRaw = append(outboxRowsRaw, row)
		outbox = append(outbox, outboxRowToDTO(row))
	}

	ackWhere := "policy_id = $1"
	ackArgs := []interface{}{policyID}
	if tenantScope != "" {
		ackWhere = ackWhere + fmt.Sprintf(" AND tenant = $%d", len(ackArgs)+1)
		ackArgs = append(ackArgs, tenantScope)
	}
	ackRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			policy_id, controller_instance,
			scope_identifier, tenant, region,
			result, reason, error_code,
			applied_at, acked_at,
			qc_reference, fast_path,
			rule_hash, producer_id,
			observed_at
		FROM policy_ack_events
		WHERE %s
		ORDER BY acked_at DESC NULLS LAST, observed_at DESC
		LIMIT 200
	`, ackWhere), ackArgs...)
	if err != nil {
		s.noteControlTimeout(err, false)
		writeErrorResponse(w, r, "TRACE_ACKS_QUERY_FAILED", "failed to query trace acks", http.StatusInternalServerError)
		s.recordControlBreakerFailure(endpointName)
		return
	}
	defer ackRows.Close()

	ackRowsRaw := make([]policyAckRow, 0, 32)
	acks := make([]policyAckPayload, 0, 32)
	for ackRows.Next() {
		var row policyAckRow
		if scanErr := ackRows.Scan(
			&row.PolicyID, &row.ControllerInstance,
			&row.ScopeIdentifier, &row.Tenant, &row.Region,
			&row.Result, &row.Reason, &row.ErrorCode,
			&row.AppliedAt, &row.AckedAt,
			&row.QCReference, &row.FastPath,
			&row.RuleHash, &row.ProducerID,
			&row.ObservedAt,
		); scanErr != nil {
			writeErrorResponse(w, r, "TRACE_ACKS_SCAN_FAILED", "failed to read trace ack row", http.StatusInternalServerError)
			s.recordControlBreakerFailure(endpointName)
			return
		}
		ackRowsRaw = append(ackRowsRaw, row)
		acks = append(acks, policyAckToPayload(row))
	}

	runtimeMarkers := make([]runtimeTraceMarkerDTO, 0)
	if s.traceStats != nil {
		for _, marker := range filterScopedRuntimeMarkers(s.traceStats.GetPolicyRuntimeTrace(policyID), outbox, acks, tenantScope != "") {
			runtimeMarkers = append(runtimeMarkers, runtimeTraceMarkerToDTO(marker))
		}
	}

	writeJSONResponse(w, r, NewSuccessResponse(controlTraceResponse{
		PolicyID:       policyID,
		Outbox:         outbox,
		Acks:           acks,
		RuntimeMarkers: runtimeMarkers,
		Materialized:   materializeControlTrace(outboxRowsRaw, ackRowsRaw, runtimeMarkers),
	}), http.StatusOK)
	s.recordControlBreakerSuccess(endpointName)
}

func runtimeTraceMarkerToDTO(marker policytrace.Marker) runtimeTraceMarkerDTO {
	return runtimeTraceMarkerDTO{
		Stage:       marker.Stage,
		PolicyID:    marker.PolicyID,
		TraceID:     marker.TraceID,
		TimestampMs: marker.TimestampMs,
		Height:      marker.Height,
		View:        marker.View,
		QCTsMs:      marker.QCTsMs,
		OutboxID:    marker.OutboxID,
		Partition:   marker.Partition,
		Offset:      marker.Offset,
	}
}

func filterScopedRuntimeMarkers(markers []policytrace.Marker, outbox []controlOutboxRowDTO, acks []policyAckPayload, tenantScoped bool) []policytrace.Marker {
	if len(markers) == 0 {
		return nil
	}
	if !tenantScoped {
		return append([]policytrace.Marker(nil), markers...)
	}

	allowedTraceIDs := make(map[string]struct{}, len(outbox)+len(acks))
	for _, row := range outbox {
		if row.TraceID != "" {
			allowedTraceIDs[row.TraceID] = struct{}{}
		}
	}
	for _, ack := range acks {
		if ack.QCReference != "" {
			allowedTraceIDs[ack.QCReference] = struct{}{}
		}
	}
	if len(allowedTraceIDs) == 0 {
		return nil
	}

	filtered := make([]policytrace.Marker, 0, len(markers))
	for _, marker := range markers {
		if marker.TraceID == "" {
			continue
		}
		if _, ok := allowedTraceIDs[marker.TraceID]; ok {
			filtered = append(filtered, marker)
		}
	}
	return filtered
}

func materializeControlTrace(outbox []controlOutboxRow, acks []policyAckRow, runtime []runtimeTraceMarkerDTO) *materializedTraceDTO {
	if len(outbox) == 0 && len(acks) == 0 && len(runtime) == 0 {
		return nil
	}

	primaryOutbox, primaryAck, traceID := selectMaterializedTraceRows(outbox, acks, runtime)
	firstAck, latestAck := selectMaterializedAckBounds(acks, traceID)
	if latestAck != nil {
		primaryAck = latestAck
	}
	sourceEventID := ""
	sourceEventTsMs := int64(0)
	aiEventTsMs := int64(0)
	if primaryOutbox != nil {
		if primaryOutbox.SourceEventID.Valid {
			sourceEventID = primaryOutbox.SourceEventID.String
		}
		if primaryOutbox.SourceEventTsMs.Valid {
			sourceEventTsMs = primaryOutbox.SourceEventTsMs.Int64
		}
		if primaryOutbox.AIEventTsMs.Valid {
			aiEventTsMs = primaryOutbox.AIEventTsMs.Int64
		}
	}
	upstreamStages := parseMaterializedUpstreamStages(primaryOutbox)

	stages := make([]materializedTraceStageDTO, 0, len(runtime)+7)
	if sourceEventTsMs > 0 {
		stages = append(stages, materializedTraceStageDTO{Stage: "t_source_event", Source: "durable", TimestampMs: sourceEventTsMs})
	}
	appendUpstreamStage := func(stage string) {
		if ts := upstreamStages[stage]; ts > 0 {
			stages = append(stages, materializedTraceStageDTO{Stage: stage, Source: "payload_trace", TimestampMs: ts})
		}
	}
	appendUpstreamStage("t_telemetry_ingest")
	appendUpstreamStage("t_sentinel_consume")
	appendUpstreamStage("t_sentinel_analysis_done")
	appendUpstreamStage("t_sentinel_emit")
	appendUpstreamStage("t_ai_sentinel_consume")
	if aiEventTsMs > 0 {
		stages = append(stages, materializedTraceStageDTO{Stage: "t_ai_decision_done", Source: "durable", TimestampMs: aiEventTsMs})
	}
	if primaryOutbox != nil && !primaryOutbox.CreatedAt.IsZero() {
		stages = append(stages, materializedTraceStageDTO{Stage: "t_outbox_row_created", Source: "durable", TimestampMs: primaryOutbox.CreatedAt.UTC().UnixMilli()})
	}
	for _, marker := range runtime {
		if traceID != "" {
			markerTraceID := strings.TrimSpace(marker.TraceID)
			// Once a trace is selected, only keep runtime markers bound to that exact trace.
			if markerTraceID == "" || markerTraceID != traceID {
				continue
			}
		}
		stages = append(stages, materializedTraceStageDTO{Stage: marker.Stage, Source: "runtime", TimestampMs: marker.TimestampMs})
	}
	if primaryOutbox != nil && primaryOutbox.PublishedAt.Valid {
		stages = append(stages, materializedTraceStageDTO{Stage: "t_control_publish_ack", Source: "durable", TimestampMs: primaryOutbox.PublishedAt.Time.UTC().UnixMilli()})
	}
	if outboxAckTs := materializedOutboxAckTs(primaryOutbox); outboxAckTs > 0 {
		stages = append(stages, materializedTraceStageDTO{Stage: "t_outbox_acked", Source: "durable", TimestampMs: outboxAckTs})
	}
	if firstAckTs := materializedAckTs(firstAck); firstAckTs > 0 {
		stages = append(stages, materializedTraceStageDTO{Stage: "t_first_policy_ack", Source: "durable", TimestampMs: firstAckTs})
	}
	if ackTs := materializedAckTs(primaryAck); ackTs > 0 {
		stages = append(stages, materializedTraceStageDTO{Stage: "t_policy_ack", Source: "durable", TimestampMs: ackTs})
		stages = append(stages, materializedTraceStageDTO{Stage: "t_ack", Source: "durable", TimestampMs: ackTs})
	}

	sort.SliceStable(stages, func(i, j int) bool {
		if stages[i].TimestampMs == stages[j].TimestampMs {
			if stages[i].Source == stages[j].Source {
				return stages[i].Stage < stages[j].Stage
			}
			return stages[i].Source < stages[j].Source
		}
		return stages[i].TimestampMs < stages[j].TimestampMs
	})

	outboxCreatedMs := int64(0)
	publishedMs := int64(0)
	outboxAckMs := int64(0)
	if primaryOutbox != nil {
		if !primaryOutbox.CreatedAt.IsZero() {
			outboxCreatedMs = primaryOutbox.CreatedAt.UTC().UnixMilli()
		}
		if primaryOutbox.PublishedAt.Valid {
			publishedMs = primaryOutbox.PublishedAt.Time.UTC().UnixMilli()
		}
		outboxAckMs = materializedOutboxAckTs(primaryOutbox)
	}
	firstPolicyAckMs := materializedAckTs(firstAck)
	policyAckMs := materializedAckTs(primaryAck)
	latencies := make([]materializedTraceLatencyDTO, 0, 12)
	appendLatency := func(name string, start, end int64) {
		if start > 0 && end > 0 && end >= start {
			latencies = append(latencies, materializedTraceLatencyDTO{Name: name, DurationMs: end - start})
		}
	}
	appendLatency("source_to_ai_decision", sourceEventTsMs, aiEventTsMs)
	appendLatency("ai_decision_to_outbox_created", aiEventTsMs, outboxCreatedMs)
	appendLatency("outbox_created_to_published", outboxCreatedMs, publishedMs)
	appendLatency("published_to_outbox_ack", publishedMs, outboxAckMs)
	appendLatency("outbox_created_to_outbox_ack", outboxCreatedMs, outboxAckMs)
	appendLatency("published_to_first_policy_ack", publishedMs, firstPolicyAckMs)
	appendLatency("source_to_first_policy_ack", sourceEventTsMs, firstPolicyAckMs)
	appendLatency("ai_decision_to_first_policy_ack", aiEventTsMs, firstPolicyAckMs)
	appendLatency("published_to_policy_ack", publishedMs, policyAckMs)
	appendLatency("source_to_policy_ack", sourceEventTsMs, policyAckMs)
	appendLatency("ai_decision_to_policy_ack", aiEventTsMs, policyAckMs)
	appendLatency("published_to_ack", publishedMs, policyAckMs)
	appendLatency("source_to_ack", sourceEventTsMs, policyAckMs)
	appendLatency("ai_decision_to_ack", aiEventTsMs, policyAckMs)
	appendLatency("telemetry_ingest_to_ai_decision", upstreamStages["t_telemetry_ingest"], aiEventTsMs)
	appendLatency("telemetry_to_sentinel_emit", upstreamStages["t_telemetry_ingest"], upstreamStages["t_sentinel_emit"])
	appendLatency("sentinel_emit_to_ai_decision", upstreamStages["t_sentinel_emit"], aiEventTsMs)
	appendLatency("ai_decision_to_backend_consume", aiEventTsMs, stageTimestamp(stages, "t_backend_consume"))
	appendLatency("backend_consume_to_commit", stageTimestamp(stages, "t_backend_consume"), stageTimestamp(stages, "t_commit"))
	appendLatency("commit_to_publish", stageTimestamp(stages, "t_commit"), publishedMs)
	appendLatency("publish_to_ack", publishedMs, policyAckMs)

	return &materializedTraceDTO{
		TraceID:         traceID,
		SourceEventID:   sourceEventID,
		SourceEventTsMs: sourceEventTsMs,
		AIEventTsMs:     aiEventTsMs,
		Stages:          stages,
		Latencies:       latencies,
	}
}

func selectMaterializedAckBounds(acks []policyAckRow, traceID string) (*policyAckRow, *policyAckRow) {
	if strings.TrimSpace(traceID) == "" {
		return nil, nil
	}
	var first *policyAckRow
	var latest *policyAckRow
	for i := range acks {
		if traceID != "" {
			if !acks[i].QCReference.Valid || strings.TrimSpace(acks[i].QCReference.String) != traceID {
				continue
			}
		}
		ts := materializedAckTs(&acks[i])
		if ts <= 0 {
			continue
		}
		if first == nil || ts < materializedAckTs(first) {
			first = &acks[i]
		}
		if latest == nil || ts > materializedAckTs(latest) {
			latest = &acks[i]
		}
	}
	return first, latest
}

func parseMaterializedUpstreamStages(outbox *controlOutboxRow) map[string]int64 {
	stages := make(map[string]int64, 6)
	if outbox == nil || len(outbox.Payload) == 0 {
		return stages
	}
	var root map[string]interface{}
	if err := json.Unmarshal(outbox.Payload, &root); err != nil {
		return stages
	}
	trace, ok := root["trace"].(map[string]interface{})
	if !ok {
		return stages
	}
	rawStages, ok := trace["stages"].(map[string]interface{})
	if !ok {
		return stages
	}
	for _, name := range []string{
		"t_telemetry_ingest",
		"t_sentinel_consume",
		"t_sentinel_analysis_done",
		"t_sentinel_emit",
		"t_ai_sentinel_consume",
		"t_ai_decision_done",
	} {
		if ts := materializedStageTimestamp(rawStages[name]); ts > 0 {
			stages[name] = ts
		}
	}
	return stages
}

func materializedStageTimestamp(v interface{}) int64 {
	toMs := func(raw int64) int64 {
		switch {
		// already small relative ms (legacy/local traces)
		case raw > 0 && raw < 946684800:
			return raw
		// seconds
		case raw >= 946684800 && raw <= 4102444800:
			return raw * 1000
		// milliseconds
		case raw >= 946684800000 && raw <= 4102444800000:
			return raw
		// microseconds
		case raw >= 946684800000000 && raw <= 4102444800000000:
			return raw / 1000
		// nanoseconds
		case raw >= 946684800000000000 && raw <= 4102444800000000000:
			return raw / 1_000_000
		default:
			return 0
		}
	}

	switch n := v.(type) {
	case int64:
		return toMs(n)
	case float64:
		return toMs(int64(n))
	case json.Number:
		if parsed, err := n.Int64(); err == nil {
			return toMs(parsed)
		}
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
			return toMs(parsed)
		}
	}
	return 0
}

func stageTimestamp(stages []materializedTraceStageDTO, stage string) int64 {
	for _, item := range stages {
		if item.Stage == stage && item.TimestampMs > 0 {
			return item.TimestampMs
		}
	}
	return 0
}

func selectMaterializedTraceRows(outbox []controlOutboxRow, acks []policyAckRow, runtime []runtimeTraceMarkerDTO) (*controlOutboxRow, *policyAckRow, string) {
	traceID := ""
	for _, row := range outbox {
		if row.TraceID.Valid && strings.TrimSpace(row.TraceID.String) != "" {
			traceID = strings.TrimSpace(row.TraceID.String)
			break
		}
	}
	if traceID == "" {
		for _, ack := range acks {
			if ack.QCReference.Valid && strings.TrimSpace(ack.QCReference.String) != "" {
				traceID = strings.TrimSpace(ack.QCReference.String)
				break
			}
		}
	}
	if traceID == "" {
		for _, marker := range runtime {
			if strings.TrimSpace(marker.TraceID) != "" {
				traceID = strings.TrimSpace(marker.TraceID)
				break
			}
		}
	}

	var selectedOutbox *controlOutboxRow
	if traceID != "" {
		for i := range outbox {
			if outbox[i].TraceID.Valid && strings.TrimSpace(outbox[i].TraceID.String) == traceID {
				selectedOutbox = &outbox[i]
				break
			}
		}
	}
	if selectedOutbox == nil && len(outbox) == 1 {
		selectedOutbox = &outbox[0]
		if traceID == "" && selectedOutbox.TraceID.Valid {
			traceID = strings.TrimSpace(selectedOutbox.TraceID.String)
		}
	}

	var selectedAck *policyAckRow
	if traceID != "" {
		for i := range acks {
			if acks[i].QCReference.Valid && strings.TrimSpace(acks[i].QCReference.String) == traceID {
				selectedAck = &acks[i]
				break
			}
		}
	}

	return selectedOutbox, selectedAck, traceID
}

func materializedAckTs(ack *policyAckRow) int64 {
	if ack == nil {
		return 0
	}
	if ack.AckedAt.Valid {
		return ack.AckedAt.Time.UTC().UnixMilli()
	}
	if !ack.ObservedAt.IsZero() {
		return ack.ObservedAt.UTC().UnixMilli()
	}
	return 0
}

func materializedOutboxAckTs(outbox *controlOutboxRow) int64 {
	if outbox == nil || !outbox.AckedAt.Valid {
		return 0
	}
	return outbox.AckedAt.Time.UTC().UnixMilli()
}

func (s *Server) handleControlLeases(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleControlLeaseForceTakeover(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET/POST methods allowed", http.StatusMethodNotAllowed)
		return
	}
	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT lease_key, holder_id, epoch, lease_until, updated_at
		FROM control_dispatcher_leases
		ORDER BY lease_key ASC
	`)
	if err != nil {
		writeErrorResponse(w, r, "LEASE_QUERY_FAILED", "failed to load dispatcher leases", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	leaseRows := make([]leaseRowDTO, 0, 4)
	for rows.Next() {
		var (
			leaseKey   string
			holderID   string
			epoch      int64
			leaseUntil time.Time
			updatedAt  time.Time
		)
		if scanErr := rows.Scan(&leaseKey, &holderID, &epoch, &leaseUntil, &updatedAt); scanErr != nil {
			writeErrorResponse(w, r, "LEASE_SCAN_FAILED", "failed to read lease row", http.StatusInternalServerError)
			return
		}
		staleBy := now.Sub(leaseUntil).Milliseconds()
		leaseRows = append(leaseRows, leaseRowDTO{
			LeaseKey:   leaseKey,
			HolderID:   holderID,
			Epoch:      epoch,
			LeaseUntil: leaseUntil.UTC().Unix(),
			UpdatedAt:  updatedAt.UTC().Unix(),
			IsActive:   now.Before(leaseUntil),
			StaleByMs:  maxInt64(staleBy, 0),
			NowUnixMs:  now.UnixMilli(),
		})
	}
	if err := rows.Err(); err != nil {
		writeErrorResponse(w, r, "LEASE_ITERATION_FAILED", "failed to iterate lease rows", http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"leases":                     leaseRows,
		"control_mutation_safe_mode": s.controlMutationsSafeMode.Load(),
	}
	if s.outboxStats != nil {
		if stats, ok := s.outboxStats.GetPolicyOutboxDispatcherStats(); ok {
			resp["dispatcher"] = stats
		}
	}

	writeJSONResponse(w, r, NewSuccessResponse(resp), http.StatusOK)
}

func (s *Server) handleControlAcksList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, err := parseControlLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeErrorResponse(w, r, "INVALID_LIMIT", err.Error(), http.StatusBadRequest)
		return
	}

	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		return
	}

	where := []string{"1=1"}
	args := make([]interface{}, 0, 12)
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}
	if tenantScope != "" {
		where = append(where, fmt.Sprintf("pa.tenant = $%d", len(args)+1))
		args = append(args, tenantScope)
	}
	if policyID := strings.TrimSpace(r.URL.Query().Get("policy_id")); policyID != "" {
		where = append(where, fmt.Sprintf("pa.policy_id = $%d", len(args)+1))
		args = append(args, policyID)
	}
	if result := strings.TrimSpace(r.URL.Query().Get("result")); result != "" {
		where = append(where, fmt.Sprintf("pa.result = $%d", len(args)+1))
		args = append(args, result)
	}
	if instance := strings.TrimSpace(r.URL.Query().Get("controller_instance")); instance != "" {
		where = append(where, fmt.Sprintf("pa.controller_instance = $%d", len(args)+1))
		args = append(args, instance)
	}
	if tenant := strings.TrimSpace(r.URL.Query().Get("tenant")); tenant != "" && tenantScope == "" {
		where = append(where, fmt.Sprintf("pa.tenant = $%d", len(args)+1))
		args = append(args, tenant)
	}
	if region := strings.TrimSpace(r.URL.Query().Get("region")); region != "" {
		where = append(where, fmt.Sprintf("pa.region = $%d", len(args)+1))
		args = append(args, region)
	}
	if fromRaw := strings.TrimSpace(r.URL.Query().Get("from")); fromRaw != "" {
		from, parseErr := parseControlTimeFilter(fromRaw)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_FROM", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("COALESCE(pa.acked_at, pa.observed_at) >= $%d", len(args)+1))
		args = append(args, from)
	}
	if toRaw := strings.TrimSpace(r.URL.Query().Get("to")); toRaw != "" {
		to, parseErr := parseControlTimeFilter(toRaw)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_TO", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("COALESCE(pa.acked_at, pa.observed_at) <= $%d", len(args)+1))
		args = append(args, to)
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("cursor")); cursor != "" {
		at, policyID, controller, parseErr := decodeAckCursor(cursor)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_CURSOR", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where,
			fmt.Sprintf("(COALESCE(pa.acked_at, pa.observed_at) < $%d OR (COALESCE(pa.acked_at, pa.observed_at) = $%d AND (pa.policy_id < $%d OR (pa.policy_id = $%d AND pa.controller_instance < $%d))))", len(args)+1, len(args)+1, len(args)+2, len(args)+2, len(args)+3),
		)
		args = append(args, at, policyID, controller)
	}

	fetchLimit := limit + 1
	query := fmt.Sprintf(`
		SELECT
			pa.policy_id, pa.controller_instance,
			pa.scope_identifier, pa.tenant, pa.region,
			pa.result, pa.reason, pa.error_code,
			pa.applied_at, pa.acked_at,
			pa.qc_reference, pa.fast_path,
			pa.rule_hash, pa.producer_id,
			pa.observed_at,
			COALESCE(hist.ack_history_count, 0) AS ack_history_count,
			hist.first_event_acked_at,
			hist.latest_event_acked_at
		FROM policy_acks pa
		LEFT JOIN (
			SELECT
				policy_id,
				controller_instance,
				count(*) AS ack_history_count,
				min(acked_at) AS first_event_acked_at,
				max(acked_at) AS latest_event_acked_at
			FROM policy_ack_events
			GROUP BY policy_id, controller_instance
		) hist
		  ON hist.policy_id = pa.policy_id
		 AND hist.controller_instance = pa.controller_instance
		WHERE %s
		ORDER BY COALESCE(pa.acked_at, pa.observed_at) DESC, pa.policy_id DESC, pa.controller_instance DESC
		LIMIT $%d`, strings.Join(where, " AND "), len(args)+1)
	args = append(args, fetchLimit)

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		writeErrorResponse(w, r, "ACKS_QUERY_FAILED", "failed to query control acks", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]policyAckPayload, 0, limit)
	for rows.Next() {
		var row policyAckRow
		if scanErr := rows.Scan(
			&row.PolicyID, &row.ControllerInstance,
			&row.ScopeIdentifier, &row.Tenant, &row.Region,
			&row.Result, &row.Reason, &row.ErrorCode,
			&row.AppliedAt, &row.AckedAt,
			&row.QCReference, &row.FastPath,
			&row.RuleHash, &row.ProducerID,
			&row.ObservedAt,
			&row.AckHistoryCount,
			&row.FirstEventAckedAt,
			&row.LatestEventAckedAt,
		); scanErr != nil {
			writeErrorResponse(w, r, "ACKS_SCAN_FAILED", "failed to read control ack row", http.StatusInternalServerError)
			return
		}
		items = append(items, policyAckToPayload(row))
	}
	if err := rows.Err(); err != nil {
		writeErrorResponse(w, r, "ACKS_ITERATION_FAILED", "failed to iterate control ack rows", http.StatusInternalServerError)
		return
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		t := time.Unix(last.ObservedAt, 0).UTC()
		if last.AckedAt > 0 {
			t = time.Unix(last.AckedAt, 0).UTC()
		}
		nextCursor = encodeAckCursor(t, last.PolicyID, last.ControllerInstance)
		items = items[:limit]
	}

	writeJSONResponse(w, r, NewSuccessResponse(controlAcksListResponse{
		Rows:       items,
		Pagination: controlListMeta{Limit: limit, NextCursor: nextCursor},
	}), http.StatusOK)
}

func (s *Server) loadOutboxBacklog(ctx context.Context) (policyoutbox.BacklogStats, error) {
	if s.outboxStats != nil {
		if stats, ok := s.outboxStats.GetPolicyOutboxBacklogStats(ctx); ok {
			return stats, nil
		}
	}

	db, err := s.getDB()
	if err != nil {
		return policyoutbox.BacklogStats{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
	defer cancel()

	var stats policyoutbox.BacklogStats
	err = db.QueryRowContext(queryCtx, `
		SELECT
			COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'retry' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'publishing' THEN 1 ELSE 0 END), 0),
			COALESCE(COUNT(*), 0),
			COALESCE(SUM(CASE WHEN status = 'published' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'acked' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'terminal_failed' THEN 1 ELSE 0 END), 0),
			COALESCE(MAX(CASE WHEN status IN ('pending','retry','publishing') THEN CAST(EXTRACT(EPOCH FROM now() - created_at) * 1000 AS INT8) ELSE 0 END), 0)
		FROM control_policy_outbox
	`).Scan(
		&stats.Pending,
		&stats.Retry,
		&stats.Publishing,
		&stats.TotalRows,
		&stats.PublishedRows,
		&stats.AckedRows,
		&stats.TerminalRows,
		&stats.OldestPendingAge,
	)
	return stats, err
}

func (s *Server) getDB() (*sql.DB, error) {
	provider, ok := s.storage.(interface{ GetDB() *sql.DB })
	if !ok {
		return nil, fmt.Errorf("storage adapter does not support GetDB()")
	}
	db := provider.GetDB()
	if db == nil {
		return nil, fmt.Errorf("db not available")
	}
	return db, nil
}

func parseControlLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return controlListDefaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("limit must be an integer")
	}
	if limit <= 0 {
		return 0, fmt.Errorf("limit must be greater than zero")
	}
	if limit > controlListMaxLimit {
		return controlListMaxLimit, nil
	}
	return limit, nil
}

func parseControlTimeFilter(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("time filter is empty")
	}
	if unixVal, err := strconv.ParseInt(raw, 10, 64); err == nil {
		switch {
		case unixVal > 1_000_000_000_000:
			return time.UnixMilli(unixVal).UTC(), nil
		default:
			return time.Unix(unixVal, 0).UTC(), nil
		}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("time must be unix seconds/ms or RFC3339")
	}
	return t.UTC(), nil
}

func isValidOutboxStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "pending", "publishing", "published", "retry", "terminal_failed", "acked":
		return true
	default:
		return false
	}
}

func outboxRowToDTO(row controlOutboxRow) controlOutboxRowDTO {
	dto := controlOutboxRowDTO{
		ID:          row.ID,
		BlockHeight: row.BlockHeight,
		BlockTS:     row.BlockTS,
		TxIndex:     row.TxIndex,
		PolicyID:    row.PolicyID,
		Status:      row.Status,
		Retries:     row.Retries,
		LeaseEpoch:  row.LeaseEpoch,
		CreatedAt:   row.CreatedAt.UTC().Unix(),
		UpdatedAt:   row.UpdatedAt.UTC().Unix(),
	}
	if row.TraceID.Valid {
		dto.TraceID = row.TraceID.String
	}
	if row.AIEventTsMs.Valid {
		dto.AIEventTsMs = row.AIEventTsMs.Int64
	}
	if row.SourceEventID.Valid {
		dto.SourceEventID = row.SourceEventID.String
	}
	if row.SourceEventTsMs.Valid {
		dto.SourceEventTsMs = row.SourceEventTsMs.Int64
	}
	if row.NextRetryAt.Valid {
		dto.NextRetryAt = row.NextRetryAt.Time.UTC().Unix()
	}
	if row.LastError.Valid {
		dto.LastError = row.LastError.String
	}
	if row.LeaseHolder.Valid {
		dto.LeaseHolder = row.LeaseHolder.String
	}
	if row.KafkaTopic.Valid {
		dto.KafkaTopic = row.KafkaTopic.String
	}
	if row.KafkaPartition.Valid {
		dto.KafkaPartition = row.KafkaPartition.Int64
	}
	if row.KafkaOffset.Valid {
		dto.KafkaOffset = row.KafkaOffset.Int64
	}
	if row.PublishedAt.Valid {
		dto.PublishedAt = row.PublishedAt.Time.UTC().Unix()
	}
	if row.AckResult.Valid {
		dto.AckResult = row.AckResult.String
	}
	if row.AckReason.Valid {
		dto.AckReason = row.AckReason.String
	}
	if row.AckController.Valid {
		dto.AckController = row.AckController.String
	}
	if row.AckedAt.Valid {
		dto.AckedAt = row.AckedAt.Time.UTC().Unix()
	}
	if len(row.RuleHash) > 0 {
		dto.RuleHashHex = hex.EncodeToString(row.RuleHash)
	}
	return dto
}

func policyAckToPayload(row policyAckRow) policyAckPayload {
	p := policyAckPayload{
		PolicyID:           row.PolicyID,
		ControllerInstance: row.ControllerInstance,
		Result:             row.Result,
		FastPath:           row.FastPath,
		ObservedAt:         row.ObservedAt.UTC().Unix(),
		AckHistoryCount:    row.AckHistoryCount,
	}
	if row.ScopeIdentifier.Valid {
		p.ScopeIdentifier = row.ScopeIdentifier.String
	}
	if row.Tenant.Valid {
		p.Tenant = row.Tenant.String
	}
	if row.Region.Valid {
		p.Region = row.Region.String
	}
	if row.Reason.Valid {
		p.Reason = row.Reason.String
	}
	if row.ErrorCode.Valid {
		p.ErrorCode = row.ErrorCode.String
	}
	if row.AppliedAt.Valid {
		p.AppliedAt = row.AppliedAt.Time.UTC().Unix()
	}
	if row.AckedAt.Valid {
		p.AckedAt = row.AckedAt.Time.UTC().Unix()
	}
	if row.QCReference.Valid {
		p.QCReference = row.QCReference.String
	}
	if row.FirstEventAckedAt.Valid {
		p.FirstEventAckedAt = row.FirstEventAckedAt.Time.UTC().Unix()
	}
	if row.LatestEventAckedAt.Valid {
		p.LatestEventAckedAt = row.LatestEventAckedAt.Time.UTC().Unix()
	}
	if len(row.RuleHash) > 0 {
		p.RuleHashHex = hex.EncodeToString(row.RuleHash)
	}
	if len(row.ProducerID) > 0 {
		p.ProducerIDHex = hex.EncodeToString(row.ProducerID)
	}
	return p
}

func encodeOutboxCursor(createdAt time.Time, id string) string {
	payload := fmt.Sprintf("%d|%s", createdAt.UTC().UnixNano(), id)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeOutboxCursor(cursor string) (time.Time, string, error) {
	buf, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("cursor decode failed")
	}
	parts := strings.SplitN(string(buf), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("cursor format invalid")
	}
	ns, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("cursor timestamp invalid")
	}
	if strings.TrimSpace(parts[1]) == "" {
		return time.Time{}, "", fmt.Errorf("cursor id missing")
	}
	return time.Unix(0, ns).UTC(), parts[1], nil
}

func encodeAckCursor(at time.Time, policyID, controller string) string {
	payload := fmt.Sprintf("%d|%s|%s", at.UTC().UnixNano(), policyID, controller)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeAckCursor(cursor string) (time.Time, string, string, error) {
	buf, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("cursor decode failed")
	}
	parts := strings.SplitN(string(buf), "|", 3)
	if len(parts) != 3 {
		return time.Time{}, "", "", fmt.Errorf("cursor format invalid")
	}
	ns, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("cursor timestamp invalid")
	}
	if strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return time.Time{}, "", "", fmt.Errorf("cursor key fields missing")
	}
	return time.Unix(0, ns).UTC(), parts[1], parts[2], nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (s *Server) resolveTenantScope(r *http.Request) (string, *tenantScopeError) {
	headerTenant := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	queryTenant := strings.TrimSpace(r.URL.Query().Get("tenant"))
	if headerTenant != "" && queryTenant != "" && !strings.EqualFold(headerTenant, queryTenant) {
		return "", &tenantScopeError{
			Code:       "TENANT_SCOPE_CONFLICT",
			Message:    "tenant in header and query must match",
			HTTPStatus: http.StatusForbidden,
		}
	}

	scope := headerTenant
	if scope == "" {
		scope = queryTenant
	}
	requireScope := false
	if s != nil && s.config != nil {
		requireScope = strings.EqualFold(strings.TrimSpace(s.config.Environment), "production") || s.config.RequireAuth
	}
	if requireScope && scope == "" {
		return "", &tenantScopeError{
			Code:       "TENANT_SCOPE_REQUIRED",
			Message:    "tenant scope is required",
			HTTPStatus: http.StatusBadRequest,
		}
	}
	return scope, nil
}
