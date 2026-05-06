package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"backend/pkg/control/policyoutbox"
	"backend/pkg/control/policytrace"
	"backend/pkg/observability"
	"backend/pkg/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	RequestID       sql.NullString
	CommandID       sql.NullString
	WorkflowID      sql.NullString
	AnomalyID       string
	FlowID          string
	SourceID        string
	SourceType      string
	SensorID        string
	ValidatorID     string
	ScopeIdentifier string
	TraceID         sql.NullString
	AIEventTsMs     sql.NullInt64
	SourceEventID   sql.NullString
	SourceEventTsMs sql.NullInt64
	SentinelEventID sql.NullString
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
	RequestID       string `json:"request_id,omitempty"`
	CommandID       string `json:"command_id,omitempty"`
	WorkflowID      string `json:"workflow_id,omitempty"`
	AnomalyID       string `json:"anomaly_id,omitempty"`
	FlowID          string `json:"flow_id,omitempty"`
	SourceID        string `json:"source_id,omitempty"`
	SourceType      string `json:"source_type,omitempty"`
	SensorID        string `json:"sensor_id,omitempty"`
	ValidatorID     string `json:"validator_id,omitempty"`
	ScopeIdentifier string `json:"scope_identifier,omitempty"`
	TraceID         string `json:"trace_id,omitempty"`
	AIEventTsMs     int64  `json:"ai_event_ts_ms,omitempty"`
	SourceEventID   string `json:"source_event_id,omitempty"`
	SourceEventTsMs int64  `json:"source_event_ts_ms,omitempty"`
	SentinelEventID string `json:"sentinel_event_id,omitempty"`
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
	PolicyID        string                  `json:"policy_id"`
	TraceID         string                  `json:"trace_id,omitempty"`
	SourceEventID   string                  `json:"source_event_id,omitempty"`
	SentinelEventID string                  `json:"sentinel_event_id,omitempty"`
	Outbox          []controlOutboxRowDTO   `json:"outbox"`
	Acks            []policyAckPayload      `json:"acks"`
	RuntimeMarkers  []runtimeTraceMarkerDTO `json:"runtime_markers,omitempty"`
	Materialized    *materializedTraceDTO   `json:"materialized,omitempty"`
}

type runtimeTraceMarkerDTO struct {
	Stage       string `json:"stage"`
	PolicyID    string `json:"policy_id"`
	TraceID     string `json:"trace_id,omitempty"`
	Reason      string `json:"reason,omitempty"`
	TimestampMs int64  `json:"t_ms"`
	Height      uint64 `json:"height,omitempty"`
	View        uint64 `json:"view,omitempty"`
	QCTsMs      int64  `json:"qc_ts_ms,omitempty"`
	OutboxID    string `json:"outbox_id,omitempty"`
	Partition   int32  `json:"partition,omitempty"`
	Offset      int64  `json:"offset,omitempty"`
}

type controlTraceEventRow struct {
	EventID         string
	EventKey        string
	PolicyID        string
	TraceID         string
	Stage           string
	StageClass      string
	StageSource     string
	TimestampMs     int64
	RequestID       sql.NullString
	CommandID       sql.NullString
	WorkflowID      sql.NullString
	SourceEventID   sql.NullString
	SentinelEventID sql.NullString
	OutboxID        sql.NullString
	AckEventID      sql.NullString
	RuleHash        []byte
	ScopeIdentifier sql.NullString
	Tenant          sql.NullString
	Region          sql.NullString
	Reason          sql.NullString
	Height          sql.NullInt64
	TxIndex         sql.NullInt64
	ViewNo          sql.NullInt64
	QCTsMs          sql.NullInt64
	KafkaPartition  sql.NullInt64
	KafkaOffset     sql.NullInt64
	DetailsJSON     []byte
	CreatedAt       time.Time
}

type materializedTraceDTO struct {
	TraceID             string                          `json:"trace_id,omitempty"`
	SourceEventID       string                          `json:"source_event_id,omitempty"`
	SentinelEventID     string                          `json:"sentinel_event_id,omitempty"`
	SourceEventTsMs     int64                           `json:"source_event_ts_ms,omitempty"`
	AIEventTsMs         int64                           `json:"ai_event_ts_ms,omitempty"`
	OutboxAck           *materializedOutboxAckDTO       `json:"outbox_ack,omitempty"`
	FirstPolicyAck      *materializedTraceAckSummaryDTO `json:"first_policy_ack,omitempty"`
	LatestPolicyAck     *materializedTraceAckSummaryDTO `json:"latest_policy_ack,omitempty"`
	ActionHistory       []materializedTraceActionDTO    `json:"action_history,omitempty"`
	CurrentAction       *materializedTraceActionDTO     `json:"current_action,omitempty"`
	LastCompletedAction *materializedTraceActionDTO     `json:"last_completed_action,omitempty"`
	Stages              []materializedTraceStageDTO     `json:"stages,omitempty"`
	RuntimeStages       []materializedTraceStageDTO     `json:"runtime_stages,omitempty"`
	DurableStages       []materializedTraceStageDTO     `json:"durable_stages,omitempty"`
	AnomalyStages       []materializedTraceStageDTO     `json:"anomaly_stages,omitempty"`
	Latencies           []materializedTraceLatencyDTO   `json:"latencies,omitempty"`
	Anomalies           []materializedTraceAnomalyDTO   `json:"anomalies,omitempty"`
}

type materializedOutboxAckDTO struct {
	Result     string `json:"result,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Controller string `json:"controller,omitempty"`
	AckedAtMs  int64  `json:"acked_at_ms,omitempty"`
}

type materializedTraceAckSummaryDTO struct {
	ControllerInstance string `json:"controller_instance,omitempty"`
	Result             string `json:"result,omitempty"`
	Reason             string `json:"reason,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	AppliedAtMs        int64  `json:"applied_at_ms,omitempty"`
	AckedAtMs          int64  `json:"acked_at_ms,omitempty"`
	TraceID            string `json:"trace_id,omitempty"`
	SourceEventID      string `json:"source_event_id,omitempty"`
	SentinelEventID    string `json:"sentinel_event_id,omitempty"`
}

type materializedTraceActionDTO struct {
	TraceID          string                          `json:"trace_id,omitempty"`
	Action           string                          `json:"action,omitempty"`
	RuleType         string                          `json:"rule_type,omitempty"`
	RollbackPolicyID string                          `json:"rollback_policy_id,omitempty"`
	CreatedAtMs      int64                           `json:"created_at_ms,omitempty"`
	PublishedAtMs    int64                           `json:"published_at_ms,omitempty"`
	OutboxStatus     string                          `json:"outbox_status,omitempty"`
	OutboxAck        *materializedOutboxAckDTO       `json:"outbox_ack,omitempty"`
	LatestPolicyAck  *materializedTraceAckSummaryDTO `json:"latest_policy_ack,omitempty"`
	SourceEventID    string                          `json:"source_event_id,omitempty"`
	SentinelEventID  string                          `json:"sentinel_event_id,omitempty"`
}

type materializedTraceStageDTO struct {
	Stage       string `json:"stage"`
	Source      string `json:"source"`
	TimestampMs int64  `json:"t_ms"`
}

type materializedTraceLatencyDTO struct {
	Name       string `json:"name"`
	DurationMs int64  `json:"duration_ms"`
	Negative   bool   `json:"negative,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type materializedTraceAnomalyDTO struct {
	Code             string   `json:"code"`
	Message          string   `json:"message"`
	RelatedStages    []string `json:"related_stages,omitempty"`
	SelectedOutboxID string   `json:"selected_outbox_id,omitempty"`
	SiblingOutboxIDs []string `json:"sibling_outbox_ids,omitempty"`
}

type materializedTraceSelection struct {
	Outbox    *controlOutboxRow
	Ack       *policyAckRow
	TraceID   string
	Anomalies []materializedTraceAnomalyDTO
}

type outboxOperationalContext struct {
	AnomalyID       string
	FlowID          string
	SourceID        string
	SourceType      string
	SensorID        string
	ValidatorID     string
	ScopeIdentifier string
}

type outboxOperationalFilters struct {
	AnomalyID       string
	FlowID          string
	SourceID        string
	SourceType      string
	SensorID        string
	ValidatorID     string
	ScopeIdentifier string
}

type tenantScopeError struct {
	Code       string
	Message    string
	HTTPStatus int
}

type controlSchemaSupport struct {
	OutboxRequestID           bool
	OutboxCommandID           bool
	OutboxWorkflowID          bool
	OutboxSentinelEventID     bool
	TraceEventsTable          bool
	StatePoliciesTable        bool
	StatePoliciesPolicyID     bool
	StatePoliciesPolicyIDText bool
	StatePoliciesTenant       bool
	AnomaliesTable            bool
	AckEventID                bool
	AckRequestID              bool
	AckCommandID              bool
	AckWorkflowID             bool
	AckTraceID                bool
	AckSourceEventID          bool
	AckSentinelEventID        bool
}

func (e *tenantScopeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func loadControlSchemaSupport(ctx context.Context, db *sql.DB) (controlSchemaSupport, error) {
	return controlSchemaSupport{
		OutboxRequestID:           controlTableHasColumn(ctx, db, "control_policy_outbox", "request_id"),
		OutboxCommandID:           controlTableHasColumn(ctx, db, "control_policy_outbox", "command_id"),
		OutboxWorkflowID:          controlTableHasColumn(ctx, db, "control_policy_outbox", "workflow_id"),
		OutboxSentinelEventID:     controlTableHasColumn(ctx, db, "control_policy_outbox", "sentinel_event_id"),
		TraceEventsTable:          controlTableExists(ctx, db, "control_policy_trace_events"),
		StatePoliciesTable:        controlTableExists(ctx, db, "state_policies"),
		StatePoliciesPolicyID:     controlTableHasColumn(ctx, db, "state_policies", "policy_id"),
		StatePoliciesPolicyIDText: controlTableHasColumn(ctx, db, "state_policies", "policy_id_text"),
		StatePoliciesTenant:       controlTableHasColumn(ctx, db, "state_policies", "tenant"),
		AnomaliesTable:            controlTableExists(ctx, db, "anomalies"),
		AckEventID:                controlTableHasColumn(ctx, db, "policy_acks", "ack_event_id") && controlTableHasColumn(ctx, db, "policy_ack_events", "ack_event_id"),
		AckRequestID:              controlTableHasColumn(ctx, db, "policy_acks", "request_id") && controlTableHasColumn(ctx, db, "policy_ack_events", "request_id"),
		AckCommandID:              controlTableHasColumn(ctx, db, "policy_acks", "command_id") && controlTableHasColumn(ctx, db, "policy_ack_events", "command_id"),
		AckWorkflowID:             controlTableHasColumn(ctx, db, "policy_acks", "workflow_id") && controlTableHasColumn(ctx, db, "policy_ack_events", "workflow_id"),
		AckTraceID:                controlTableHasColumn(ctx, db, "policy_acks", "trace_id") && controlTableHasColumn(ctx, db, "policy_ack_events", "trace_id"),
		AckSourceEventID:          controlTableHasColumn(ctx, db, "policy_acks", "source_event_id") && controlTableHasColumn(ctx, db, "policy_ack_events", "source_event_id"),
		AckSentinelEventID:        controlTableHasColumn(ctx, db, "policy_acks", "sentinel_event_id") && controlTableHasColumn(ctx, db, "policy_ack_events", "sentinel_event_id"),
	}, nil
}

func controlOutboxRequestIDSelectExpr(schema controlSchemaSupport) string {
	if schema.OutboxRequestID {
		return "request_id"
	}
	return "NULL::STRING AS request_id"
}

func controlOutboxCommandIDSelectExpr(schema controlSchemaSupport) string {
	if schema.OutboxCommandID {
		return "command_id"
	}
	return "NULL::STRING AS command_id"
}

func controlOutboxWorkflowIDSelectExpr(schema controlSchemaSupport) string {
	if schema.OutboxWorkflowID {
		return "workflow_id"
	}
	return "NULL::STRING AS workflow_id"
}

func controlOutboxAnomalyIDSelectExpr(schema controlSchemaSupport) string {
	payloadAnomalyExpr := outboxPayloadStringExpr("{anomaly_id}", "{metadata,anomaly_id}", "{params,anomaly_id}", "{params,metadata,anomaly_id}")
	_ = schema
	return payloadAnomalyExpr + " AS anomaly_id"
}

func controlOutboxFlowIDSelectExpr() string {
	return outboxPayloadStringExpr("{flow_id}", "{metadata,flow_id}", "{trace,flow_id}", "{input,flow_id}", "{params,flow_id}", "{params,metadata,flow_id}", "{params,trace,flow_id}", "{params,input,flow_id}") + " AS flow_id"
}

func controlOutboxSourceIDSelectExpr() string {
	return outboxPayloadStringExpr("{source_id}", "{metadata,source_id}", "{trace,source_id}", "{input,source_id}", "{params,source_id}", "{params,metadata,source_id}", "{params,trace,source_id}", "{params,input,source_id}") + " AS source_id"
}

func controlOutboxSourceTypeSelectExpr() string {
	return outboxPayloadStringExpr("{source_type}", "{metadata,source_type}", "{trace,source_type}", "{input,source_type}", "{params,source_type}", "{params,metadata,source_type}", "{params,trace,source_type}", "{params,input,source_type}") + " AS source_type"
}

func controlOutboxSensorIDSelectExpr() string {
	return outboxPayloadStringExpr("{sensor_id}", "{metadata,sensor_id}", "{trace,sensor_id}", "{input,sensor_id}", "{params,sensor_id}", "{params,metadata,sensor_id}", "{params,trace,sensor_id}", "{params,input,sensor_id}") + " AS sensor_id"
}

func controlOutboxValidatorIDSelectExpr() string {
	return outboxPayloadStringExpr("{validator_id}", "{metadata,validator_id}", "{trace,validator_id}", "{params,validator_id}", "{params,metadata,validator_id}", "{params,trace,validator_id}") + " AS validator_id"
}

func controlOutboxScopeIdentifierSelectExpr() string {
	return outboxPayloadStringExpr("{scope_identifier}", "{metadata,scope_identifier}", "{trace,scope_identifier}", "{params,scope_identifier}", "{params,metadata,scope_identifier}", "{params,trace,scope_identifier}") + " AS scope_identifier"
}

func controlAckEventIDSelectExpr(schema controlSchemaSupport) string {
	if schema.AckEventID {
		return "ack_event_id"
	}
	return "NULL::STRING AS ack_event_id"
}

func controlAckRequestIDSelectExpr(schema controlSchemaSupport) string {
	if schema.AckRequestID {
		return "request_id"
	}
	return "NULL::STRING AS request_id"
}

func controlAckCommandIDSelectExpr(schema controlSchemaSupport) string {
	if schema.AckCommandID {
		return "command_id"
	}
	return "NULL::STRING AS command_id"
}

func controlAckWorkflowIDSelectExpr(schema controlSchemaSupport) string {
	if schema.AckWorkflowID {
		return "workflow_id"
	}
	return "NULL::STRING AS workflow_id"
}

func controlTableHasColumn(ctx context.Context, db *sql.DB, tableName, columnName string) bool {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = $1
			  AND column_name = $2
		)
	`, tableName, columnName).Scan(&exists)
	return err == nil && exists
}

func controlTableExists(ctx context.Context, db *sql.DB, tableName string) bool {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name = $1
		)
	`, tableName).Scan(&exists)
	return err == nil && exists
}

func tenantHasPolicyProjection(ctx context.Context, db *sql.DB, schema controlSchemaSupport, tenantScope string) (bool, error) {
	if db == nil || strings.TrimSpace(tenantScope) == "" {
		return true, nil
	}
	if !schema.StatePoliciesTable || !schema.StatePoliciesTenant {
		return true, nil
	}
	var found bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM state_policies
			WHERE tenant = $1
			LIMIT 1
		)
	`, strings.TrimSpace(tenantScope)).Scan(&found)
	if err != nil {
		return false, err
	}
	return found, nil
}

func controlOutboxSentinelSelectExpr(schema controlSchemaSupport) string {
	if schema.OutboxSentinelEventID {
		return "sentinel_event_id"
	}
	return "NULL::STRING AS sentinel_event_id"
}

func controlAckTraceSelectExpr(schema controlSchemaSupport) string {
	if schema.AckTraceID {
		return "trace_id"
	}
	return "NULL::STRING AS trace_id"
}

func controlAckSourceEventSelectExpr(schema controlSchemaSupport) string {
	if schema.AckSourceEventID {
		return "source_event_id"
	}
	return "NULL::STRING AS source_event_id"
}

func controlAckSentinelEventSelectExpr(schema controlSchemaSupport) string {
	if schema.AckSentinelEventID {
		return "sentinel_event_id"
	}
	return "NULL::STRING AS sentinel_event_id"
}

func requireControlSchemaColumn(w http.ResponseWriter, r *http.Request, supported bool, param, migration string) bool {
	if supported {
		return true
	}
	writeErrorResponse(w, r, "CONTROL_SCHEMA_UPGRADE_REQUIRED", fmt.Sprintf("%s filter requires DB migration %s", param, migration), http.StatusServiceUnavailable)
	return false
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
	if !controlOutboxHasNarrowingFilter(r.URL.Query()) {
		writeErrorResponse(w, r, "OUTBOX_FILTER_REQUIRED", "at least one narrowing filter is required", http.StatusBadRequest)
		return
	}

	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		return
	}
	schemaCtx, schemaCancel := context.WithTimeout(r.Context(), s.controlReadTimeout())
	schema, _ := loadControlSchemaSupport(schemaCtx, db)
	schemaCancel()
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}
	projectionCtx, projectionCancel := context.WithTimeout(r.Context(), s.controlReadTimeout())
	hasProjection, projErr := tenantHasPolicyProjection(projectionCtx, db, schema, tenantScope)
	projectionCancel()
	if projErr != nil {
		writeErrorResponse(w, r, "OUTBOX_QUERY_FAILED", "failed to query outbox rows", http.StatusInternalServerError)
		return
	}
	if !hasProjection {
		writeJSONResponse(w, r, NewSuccessResponse(controlOutboxListResponse{
			Rows:       []controlOutboxRowDTO{},
			Pagination: controlListMeta{Limit: limit},
		}), http.StatusOK)
		return
	}

	where := make([]string, 0, 8)
	args := make([]interface{}, 0, 16)
	payloadFilters := outboxOperationalFilters{}
	where = append(where, "1=1")
	where, args = appendAccessBoundOutboxFilterWithProjection(where, args, tenantScope, schema)

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
	if requestID := strings.TrimSpace(r.URL.Query().Get("request_id")); requestID != "" {
		if !requireControlSchemaColumn(w, r, schema.OutboxRequestID, "request_id", "022") {
			return
		}
		where = append(where, fmt.Sprintf("request_id = $%d", len(args)+1))
		args = append(args, requestID)
	}
	if commandID := strings.TrimSpace(r.URL.Query().Get("command_id")); commandID != "" {
		if !requireControlSchemaColumn(w, r, schema.OutboxCommandID, "command_id", "023") {
			return
		}
		where = append(where, fmt.Sprintf("command_id = $%d", len(args)+1))
		args = append(args, commandID)
	}
	if workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id")); workflowID != "" {
		if !requireControlSchemaColumn(w, r, schema.OutboxWorkflowID, "workflow_id", "024") {
			return
		}
		where = append(where, fmt.Sprintf("workflow_id = $%d", len(args)+1))
		args = append(args, workflowID)
	}
	if traceID := strings.TrimSpace(r.URL.Query().Get("trace_id")); traceID != "" {
		where = append(where, fmt.Sprintf("trace_id = $%d", len(args)+1))
		args = append(args, traceID)
	}
	if sentinelEventID := strings.TrimSpace(r.URL.Query().Get("sentinel_event_id")); sentinelEventID != "" {
		if !requireControlSchemaColumn(w, r, schema.OutboxSentinelEventID, "sentinel_event_id", "019") {
			return
		}
		where = append(where, fmt.Sprintf("sentinel_event_id = $%d", len(args)+1))
		args = append(args, sentinelEventID)
	}
	if anomalyID := strings.TrimSpace(r.URL.Query().Get("anomaly_id")); anomalyID != "" {
		payloadFilters.AnomalyID = anomalyID
	}
	if flowID := strings.TrimSpace(r.URL.Query().Get("flow_id")); flowID != "" {
		payloadFilters.FlowID = flowID
	}
	if sourceID := strings.TrimSpace(r.URL.Query().Get("source_id")); sourceID != "" {
		payloadFilters.SourceID = sourceID
	}
	if sourceType := strings.TrimSpace(r.URL.Query().Get("source_type")); sourceType != "" {
		payloadFilters.SourceType = sourceType
	}
	if sensorID := strings.TrimSpace(r.URL.Query().Get("sensor_id")); sensorID != "" {
		payloadFilters.SensorID = sensorID
	}
	if validatorID := strings.TrimSpace(r.URL.Query().Get("validator_id")); validatorID != "" {
		payloadFilters.ValidatorID = validatorID
	}
	if scopeIdentifier := strings.TrimSpace(r.URL.Query().Get("scope_identifier")); scopeIdentifier != "" {
		payloadFilters.ScopeIdentifier = scopeIdentifier
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
	if payloadFilters.hasAny() && fetchLimit < 1000 {
		fetchLimit = 1000
	}
	query := fmt.Sprintf(`
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, %s, payload, status, retries, next_retry_at,
			last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at,
			ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`, controlOutboxRequestIDSelectExpr(schema), controlOutboxCommandIDSelectExpr(schema), controlOutboxWorkflowIDSelectExpr(schema), controlOutboxAnomalyIDSelectExpr(schema), controlOutboxFlowIDSelectExpr(), controlOutboxSourceIDSelectExpr(), controlOutboxSourceTypeSelectExpr(), controlOutboxSensorIDSelectExpr(), controlOutboxValidatorIDSelectExpr(), controlOutboxScopeIdentifierSelectExpr(), controlOutboxSentinelSelectExpr(schema), strings.Join(where, " AND "), len(args)+1)
	args = append(args, fetchLimit)

	ctx, cancel := context.WithTimeout(r.Context(), s.controlReadTimeout())
	defer cancel()

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if isTransientControlStorageError(err) {
			writeJSONResponse(w, r, NewSuccessResponse(controlOutboxListResponse{
				Rows:       []controlOutboxRowDTO{},
				Pagination: controlListMeta{Limit: limit},
			}), http.StatusOK)
			return
		}
		writeErrorResponse(w, r, "OUTBOX_QUERY_FAILED", "failed to query outbox rows", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]controlOutboxRowDTO, 0, limit)
	for rows.Next() {
		var row controlOutboxRow
		scanErr := rows.Scan(
			&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
			&row.RequestID, &row.CommandID, &row.WorkflowID, &row.AnomalyID, &row.FlowID, &row.SourceID, &row.SourceType, &row.SensorID, &row.ValidatorID, &row.ScopeIdentifier, &row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Payload, &row.Status, &row.Retries, &row.NextRetryAt,
			&row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
			&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt,
			&row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
			&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
		)
		if scanErr != nil {
			writeErrorResponse(w, r, "OUTBOX_SCAN_FAILED", "failed to read outbox row", http.StatusInternalServerError)
			return
		}
		dto := outboxRowToDTO(row)
		if !payloadFilters.matches(dto) {
			continue
		}
		items = append(items, dto)
	}
	if err := rows.Err(); err != nil {
		if isTransientControlStorageError(err) {
			writeJSONResponse(w, r, NewSuccessResponse(controlOutboxListResponse{
				Rows:       []controlOutboxRowDTO{},
				Pagination: controlListMeta{Limit: limit},
			}), http.StatusOK)
			return
		}
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
	ctx, cancel := context.WithTimeout(r.Context(), s.controlReadTimeout())
	defer cancel()
	schema, _ := loadControlSchemaSupport(ctx, db)

	var row controlOutboxRow
	err = db.QueryRowContext(ctx, `
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			`+controlOutboxRequestIDSelectExpr(schema)+`, `+controlOutboxCommandIDSelectExpr(schema)+`, `+controlOutboxWorkflowIDSelectExpr(schema)+`, `+controlOutboxAnomalyIDSelectExpr(schema)+`, `+controlOutboxFlowIDSelectExpr()+`, `+controlOutboxSourceIDSelectExpr()+`, `+controlOutboxSourceTypeSelectExpr()+`, `+controlOutboxSensorIDSelectExpr()+`, `+controlOutboxValidatorIDSelectExpr()+`, `+controlOutboxScopeIdentifierSelectExpr()+`, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, `+controlOutboxSentinelSelectExpr(schema)+`, payload, status, retries, next_retry_at,
			last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at,
			ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE id = $1::UUID
	`, rowID).Scan(
		&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
		&row.RequestID, &row.CommandID, &row.WorkflowID, &row.AnomalyID, &row.FlowID, &row.SourceID, &row.SourceType, &row.SensorID, &row.ValidatorID, &row.ScopeIdentifier, &row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Payload, &row.Status, &row.Retries, &row.NextRetryAt,
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
		visible, visErr := s.outboxVisibleToAccess(ctx, db, row, tenantScope)
		if visErr != nil {
			writeErrorResponse(w, r, "OUTBOX_SCOPE_QUERY_FAILED", "failed to evaluate outbox row scope", http.StatusInternalServerError)
			return
		}
		if !visible {
			writeErrorResponse(w, r, "OUTBOX_ROW_NOT_FOUND", "outbox row not found", http.StatusNotFound)
			return
		}
	}

	writeJSONResponse(w, r, NewSuccessResponse(outboxRowToDTO(row)), http.StatusOK)
}

func (s *Server) handleControlTraceList(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.Tracer("backend/control-plane").Start(
		r.Context(),
		"backend.control.trace_list",
		trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.route", "/control/trace"),
			attribute.String("query.policy_id", strings.TrimSpace(r.URL.Query().Get("policy_id"))),
			attribute.String("query.trace_id", strings.TrimSpace(r.URL.Query().Get("trace_id"))),
		),
	)
	defer span.End()
	r = r.WithContext(ctx)
	const endpointName = "control.trace.list"
	if err := s.checkControlBreaker(endpointName); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "control_breaker_open")
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
	schemaCtx, schemaCancel := context.WithTimeout(r.Context(), s.controlReadTimeout())
	schema, _ := loadControlSchemaSupport(schemaCtx, db)
	schemaCancel()

	where := []string{"1=1"}
	args := make([]interface{}, 0, 16)
	payloadFilters := outboxOperationalFilters{}
	where, args = appendAccessBoundOutboxFilterWithProjection(where, args, tenantScope, schema)
	if policyID != "" {
		where = append(where, fmt.Sprintf("policy_id = $%d", len(args)+1))
		args = append(args, policyID)
	}
	if requestID := strings.TrimSpace(r.URL.Query().Get("request_id")); requestID != "" {
		if !requireControlSchemaColumn(w, r, schema.OutboxRequestID, "request_id", "022") {
			return
		}
		where = append(where, fmt.Sprintf("request_id = $%d", len(args)+1))
		args = append(args, requestID)
	}
	if commandID := strings.TrimSpace(r.URL.Query().Get("command_id")); commandID != "" {
		if !requireControlSchemaColumn(w, r, schema.OutboxCommandID, "command_id", "023") {
			return
		}
		where = append(where, fmt.Sprintf("command_id = $%d", len(args)+1))
		args = append(args, commandID)
	}
	if workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id")); workflowID != "" {
		if !requireControlSchemaColumn(w, r, schema.OutboxWorkflowID, "workflow_id", "024") {
			return
		}
		where = append(where, fmt.Sprintf("workflow_id = $%d", len(args)+1))
		args = append(args, workflowID)
	}
	if traceID != "" {
		where = append(where, fmt.Sprintf("trace_id = $%d", len(args)+1))
		args = append(args, traceID)
	}
	if sentinelEventID := strings.TrimSpace(r.URL.Query().Get("sentinel_event_id")); sentinelEventID != "" {
		if !requireControlSchemaColumn(w, r, schema.OutboxSentinelEventID, "sentinel_event_id", "019") {
			return
		}
		where = append(where, fmt.Sprintf("sentinel_event_id = $%d", len(args)+1))
		args = append(args, sentinelEventID)
	}
	if anomalyID := strings.TrimSpace(r.URL.Query().Get("anomaly_id")); anomalyID != "" {
		payloadFilters.AnomalyID = anomalyID
	}
	if flowID := strings.TrimSpace(r.URL.Query().Get("flow_id")); flowID != "" {
		payloadFilters.FlowID = flowID
	}
	if sourceID := strings.TrimSpace(r.URL.Query().Get("source_id")); sourceID != "" {
		payloadFilters.SourceID = sourceID
	}
	if sourceType := strings.TrimSpace(r.URL.Query().Get("source_type")); sourceType != "" {
		payloadFilters.SourceType = sourceType
	}
	if sensorID := strings.TrimSpace(r.URL.Query().Get("sensor_id")); sensorID != "" {
		payloadFilters.SensorID = sensorID
	}
	if validatorID := strings.TrimSpace(r.URL.Query().Get("validator_id")); validatorID != "" {
		payloadFilters.ValidatorID = validatorID
	}
	if scopeIdentifier := strings.TrimSpace(r.URL.Query().Get("scope_identifier")); scopeIdentifier != "" {
		payloadFilters.ScopeIdentifier = scopeIdentifier
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
			%s, %s, %s, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, %s, status, retries, next_retry_at,
			last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at,
			ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`, controlOutboxRequestIDSelectExpr(schema), controlOutboxCommandIDSelectExpr(schema), controlOutboxWorkflowIDSelectExpr(schema), controlOutboxSentinelSelectExpr(schema), strings.Join(where, " AND "), len(args)+1)
	args = append(args, fetchLimit)

	ctx, cancel := context.WithTimeout(r.Context(), s.controlTraceTimeout())
	defer cancel()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if isTransientControlStorageError(err) {
			writeJSONResponse(w, r, NewSuccessResponse(controlOutboxListResponse{
				Rows:       []controlOutboxRowDTO{},
				Pagination: controlListMeta{Limit: limit},
			}), http.StatusOK)
			return
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "trace_query_failed")
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
			&row.RequestID, &row.CommandID, &row.WorkflowID, &row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Status, &row.Retries, &row.NextRetryAt,
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
		span.RecordError(err)
		span.SetStatus(codes.Error, "trace_iteration_failed")
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
	span.SetAttributes(attribute.Int("trace.rows", len(items)))
	span.SetStatus(codes.Ok, "trace_list_ok")
	s.recordControlBreakerSuccess(endpointName)
}

func (s *Server) handleControlTraceByPolicy(w http.ResponseWriter, r *http.Request) {
	ctx, span := observability.Tracer("backend/control-plane").Start(
		r.Context(),
		"backend.control.trace_by_policy",
		trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.route", "/control/trace/{policy_id}"),
		),
	)
	defer span.End()
	r = r.WithContext(ctx)
	const endpointName = "control.trace.by_policy"
	if err := s.checkControlBreaker(endpointName); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "control_breaker_open")
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
	span.SetAttributes(attribute.String("policy.id", policyID))
	if policyID == "" {
		span.SetStatus(codes.Error, "invalid_policy_id")
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
	schemaCtx, schemaCancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	schema, _ := loadControlSchemaSupport(schemaCtx, db)
	schemaCancel()

	ctx, cancel := context.WithTimeout(r.Context(), s.controlTraceTimeout())
	defer cancel()

	outboxWhere := "policy_id = $1"
	outboxArgs := []interface{}{policyID}
	outboxWhere, outboxArgs = appendAccessBoundOutboxClause(outboxWhere, outboxArgs, tenantScope)
	outboxRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, %s, payload, status, retries, next_retry_at,
			last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at,
			ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT 200
	`, controlOutboxRequestIDSelectExpr(schema), controlOutboxCommandIDSelectExpr(schema), controlOutboxWorkflowIDSelectExpr(schema), controlOutboxAnomalyIDSelectExpr(schema), controlOutboxFlowIDSelectExpr(), controlOutboxSourceIDSelectExpr(), controlOutboxSourceTypeSelectExpr(), controlOutboxSensorIDSelectExpr(), controlOutboxValidatorIDSelectExpr(), controlOutboxScopeIdentifierSelectExpr(), controlOutboxSentinelSelectExpr(schema), outboxWhere), outboxArgs...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "trace_outbox_query_failed")
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
			&row.RequestID, &row.CommandID, &row.WorkflowID, &row.AnomalyID, &row.FlowID, &row.SourceID, &row.SourceType, &row.SensorID, &row.ValidatorID, &row.ScopeIdentifier, &row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Payload, &row.Status, &row.Retries, &row.NextRetryAt,
			&row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
			&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt,
			&row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
			&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
		); scanErr != nil {
			span.RecordError(scanErr)
			span.SetStatus(codes.Error, "trace_outbox_scan_failed")
			writeErrorResponse(w, r, "TRACE_OUTBOX_SCAN_FAILED", "failed to read trace outbox row", http.StatusInternalServerError)
			s.recordControlBreakerFailure(endpointName)
			return
		}
		outboxRowsRaw = append(outboxRowsRaw, row)
		outbox = append(outbox, outboxRowToDTO(row))
	}

	ackWhere := "policy_id = $1"
	ackArgs := []interface{}{policyID}
	ackWhere, ackArgs = appendAccessBoundAckFilter(ackWhere, ackArgs, tenantScope)
	ackRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			policy_id, %s, %s, %s, %s, controller_instance,
			scope_identifier, tenant, region,
			result, reason, error_code,
			applied_at, acked_at,
			qc_reference, %s, %s, %s, fast_path,
			rule_hash, producer_id,
			observed_at
		FROM policy_ack_events
		WHERE %s
		ORDER BY acked_at DESC NULLS LAST, observed_at DESC
		LIMIT 200
	`, controlAckEventIDSelectExpr(schema), controlAckRequestIDSelectExpr(schema), controlAckCommandIDSelectExpr(schema), controlAckWorkflowIDSelectExpr(schema), controlAckTraceSelectExpr(schema), controlAckSourceEventSelectExpr(schema), controlAckSentinelEventSelectExpr(schema), ackWhere), ackArgs...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "trace_acks_query_failed")
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
			&row.PolicyID, &row.AckEventID, &row.RequestID, &row.CommandID, &row.WorkflowID, &row.ControllerInstance,
			&row.ScopeIdentifier, &row.Tenant, &row.Region,
			&row.Result, &row.Reason, &row.ErrorCode,
			&row.AppliedAt, &row.AckedAt,
			&row.QCReference, &row.TraceID, &row.SourceEventID, &row.SentinelEventID, &row.FastPath,
			&row.RuleHash, &row.ProducerID,
			&row.ObservedAt,
		); scanErr != nil {
			span.RecordError(scanErr)
			span.SetStatus(codes.Error, "trace_acks_scan_failed")
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

	traceEvents, err := loadPolicyTraceEvents(ctx, db, schema, policyID, tenantScope, outboxRowsRaw, ackRowsRaw)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "trace_events_query_failed")
		writeErrorResponse(w, r, "TRACE_EVENTS_QUERY_FAILED", "failed to load canonical trace events", http.StatusInternalServerError)
		s.recordControlBreakerFailure(endpointName)
		return
	}

	materialized := materializeControlTraceWithEvents(traceEvents, outboxRowsRaw, ackRowsRaw, runtimeMarkers)
	traceID, sourceEventID, sentinelEventID := materializedTraceLineage(outboxRowsRaw, ackRowsRaw, materialized)

	writeJSONResponse(w, r, NewSuccessResponse(controlTraceResponse{
		PolicyID:        policyID,
		TraceID:         traceID,
		SourceEventID:   sourceEventID,
		SentinelEventID: sentinelEventID,
		Outbox:          outbox,
		Acks:            acks,
		RuntimeMarkers:  runtimeMarkers,
		Materialized:    materialized,
	}), http.StatusOK)
	span.SetAttributes(
		attribute.Int("trace.outbox_rows", len(outbox)),
		attribute.Int("trace.ack_rows", len(acks)),
		attribute.Int("trace.canonical_events", len(traceEvents)),
		attribute.Int("trace.runtime_markers", len(runtimeMarkers)),
	)
	span.SetStatus(codes.Ok, "trace_by_policy_ok")
	s.recordControlBreakerSuccess(endpointName)
}

func runtimeTraceMarkerToDTO(marker policytrace.Marker) runtimeTraceMarkerDTO {
	return runtimeTraceMarkerDTO{
		Stage:       marker.Stage,
		PolicyID:    marker.PolicyID,
		TraceID:     marker.TraceID,
		Reason:      marker.Reason,
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
		if traceID := policyAckEffectiveTraceID(ack.TraceID, ack.QCReference); traceID != "" {
			allowedTraceIDs[traceID] = struct{}{}
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

func appendDistinctStrings(dst []string, seen map[string]struct{}, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func collectTraceEventLineage(policyID string, outbox []controlOutboxRow, acks []policyAckRow) (traceIDs []string, sourceEventIDs []string, sentinelEventIDs []string) {
	traceSeen := make(map[string]struct{}, 8)
	sourceSeen := make(map[string]struct{}, 8)
	sentinelSeen := make(map[string]struct{}, 8)
	for _, row := range outbox {
		traceIDs = appendDistinctStrings(traceIDs, traceSeen, strings.TrimSpace(row.TraceID.String))
		sourceEventIDs = appendDistinctStrings(sourceEventIDs, sourceSeen, strings.TrimSpace(row.SourceEventID.String))
		sentinelEventIDs = appendDistinctStrings(sentinelEventIDs, sentinelSeen, strings.TrimSpace(row.SentinelEventID.String))
	}
	for _, row := range acks {
		traceIDs = appendDistinctStrings(traceIDs, traceSeen, strings.TrimSpace(row.TraceID.String))
		sourceEventIDs = appendDistinctStrings(sourceEventIDs, sourceSeen, strings.TrimSpace(row.SourceEventID.String))
		sentinelEventIDs = appendDistinctStrings(sentinelEventIDs, sentinelSeen, strings.TrimSpace(row.SentinelEventID.String))
	}
	return traceIDs, sourceEventIDs, sentinelEventIDs
}

func loadPolicyTraceEvents(ctx context.Context, db *sql.DB, schema controlSchemaSupport, policyID, tenantScope string, outbox []controlOutboxRow, acks []policyAckRow) ([]controlTraceEventRow, error) {
	if db == nil || !schema.TraceEventsTable {
		return nil, nil
	}

	traceIDs, sourceEventIDs, sentinelEventIDs := collectTraceEventLineage(policyID, outbox, acks)
	whereParts := make([]string, 0, 4)
	args := make([]interface{}, 0, 16)
	if strings.TrimSpace(policyID) != "" {
		whereParts = append(whereParts, fmt.Sprintf("policy_id = $%d", len(args)+1))
		args = append(args, strings.TrimSpace(policyID))
	}
	for _, traceID := range traceIDs {
		whereParts = append(whereParts, fmt.Sprintf("trace_id = $%d", len(args)+1))
		args = append(args, traceID)
	}
	for _, sourceEventID := range sourceEventIDs {
		whereParts = append(whereParts, fmt.Sprintf("source_event_id = $%d", len(args)+1))
		args = append(args, sourceEventID)
	}
	for _, sentinelEventID := range sentinelEventIDs {
		whereParts = append(whereParts, fmt.Sprintf("sentinel_event_id = $%d", len(args)+1))
		args = append(args, sentinelEventID)
	}
	if len(whereParts) == 0 {
		return nil, nil
	}

	where := "(" + strings.Join(whereParts, " OR ") + ")"
	if strings.TrimSpace(tenantScope) != "" {
		where += fmt.Sprintf(" AND ((tenant = $%d) OR (scope_identifier = 'cluster') OR (scope_identifier IS NULL))", len(args)+1)
		args = append(args, tenantScope)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			event_id, event_key, policy_id, trace_id, stage, stage_class, stage_source, timestamp_ms,
			request_id, command_id, workflow_id, source_event_id, sentinel_event_id, outbox_id, ack_event_id, rule_hash,
			scope_identifier, tenant, region, reason, height, tx_index, view_no, qc_ts_ms, kafka_partition, kafka_offset,
			details_json, created_at
		FROM control_policy_trace_events
		WHERE `+where+`
		ORDER BY timestamp_ms ASC, created_at ASC, event_id ASC
		LIMIT 1000
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]controlTraceEventRow, 0, 64)
	for rows.Next() {
		var row controlTraceEventRow
		if scanErr := rows.Scan(
			&row.EventID, &row.EventKey, &row.PolicyID, &row.TraceID, &row.Stage, &row.StageClass, &row.StageSource, &row.TimestampMs,
			&row.RequestID, &row.CommandID, &row.WorkflowID, &row.SourceEventID, &row.SentinelEventID, &row.OutboxID, &row.AckEventID, &row.RuleHash,
			&row.ScopeIdentifier, &row.Tenant, &row.Region, &row.Reason, &row.Height, &row.TxIndex, &row.ViewNo, &row.QCTsMs, &row.KafkaPartition, &row.KafkaOffset,
			&row.DetailsJSON, &row.CreatedAt,
		); scanErr != nil {
			return nil, scanErr
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func materializeControlTraceWithEvents(events []controlTraceEventRow, outbox []controlOutboxRow, acks []policyAckRow, runtime []runtimeTraceMarkerDTO) *materializedTraceDTO {
	if len(events) == 0 {
		return materializeControlTrace(outbox, acks, runtime)
	}
	return materializeControlTraceFromCanonicalEvents(events, outbox, acks, runtime)
}

func materializeControlTraceFromCanonicalEvents(events []controlTraceEventRow, outbox []controlOutboxRow, acks []policyAckRow, runtime []runtimeTraceMarkerDTO) *materializedTraceDTO {
	if len(events) == 0 && len(outbox) == 0 && len(acks) == 0 && len(runtime) == 0 {
		return nil
	}

	fallbackSelection := selectMaterializedTraceContext(outbox, acks, runtime)
	traceID := selectCanonicalEventTraceID(events, fallbackSelection.TraceID)
	if traceID == "" {
		return materializeControlTrace(outbox, acks, runtime)
	}

	firstAck, latestAck := selectMaterializedAckBounds(acks, traceID)
	primaryAck := latestAck
	if primaryAck == nil {
		primaryAck = fallbackSelection.Ack
	}

	anomalies := append([]materializedTraceAnomalyDTO(nil), fallbackSelection.Anomalies...)
	sourceEventID := ""
	sentinelEventID := ""
	sourceEventTsMs := int64(0)
	aiEventTsMs := int64(0)
	primaryOutbox := fallbackSelection.Outbox
	sourceEventIDs := make(map[string]struct{}, 4)
	sentinelEventIDs := make(map[string]struct{}, 4)
	addLineageID := func(dst map[string]struct{}, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			dst[value] = struct{}{}
		}
	}
	if primaryOutbox != nil {
		if primaryOutbox.SourceEventID.Valid {
			sourceEventID = strings.TrimSpace(primaryOutbox.SourceEventID.String)
			addLineageID(sourceEventIDs, sourceEventID)
		}
		if primaryOutbox.SourceEventTsMs.Valid {
			sourceEventTsMs = primaryOutbox.SourceEventTsMs.Int64
		}
		if primaryOutbox.SentinelEventID.Valid {
			sentinelEventID = strings.TrimSpace(primaryOutbox.SentinelEventID.String)
			addLineageID(sentinelEventIDs, sentinelEventID)
		}
	}
	for _, ack := range acks {
		if strings.TrimSpace(ack.TraceID.String) != "" && strings.TrimSpace(ack.TraceID.String) != traceID {
			continue
		}
		if ack.SourceEventID.Valid {
			addLineageID(sourceEventIDs, ack.SourceEventID.String)
		}
		if ack.SentinelEventID.Valid {
			addLineageID(sentinelEventIDs, ack.SentinelEventID.String)
		}
	}

	selectedOutboxID := ""
	if primaryOutbox != nil {
		selectedOutboxID = strings.TrimSpace(primaryOutbox.ID)
	}
	selectedAckEventID := ""
	if primaryAck != nil && primaryAck.AckEventID.Valid {
		selectedAckEventID = strings.TrimSpace(primaryAck.AckEventID.String)
	}
	canonicalEvents := filterCanonicalTraceEvents(events, traceID, sourceEventIDs, sentinelEventIDs, selectedOutboxID, selectedAckEventID)
	if len(canonicalEvents) == 0 {
		return materializeControlTrace(outbox, acks, runtime)
	}
	stages := make([]materializedTraceStageDTO, 0, len(canonicalEvents)+1)

	for _, event := range canonicalEvents {
		if sourceEventID == "" && event.SourceEventID.Valid {
			sourceEventID = strings.TrimSpace(event.SourceEventID.String)
			addLineageID(sourceEventIDs, sourceEventID)
		}
		if sentinelEventID == "" && event.SentinelEventID.Valid {
			sentinelEventID = strings.TrimSpace(event.SentinelEventID.String)
			addLineageID(sentinelEventIDs, sentinelEventID)
		}
		if aiEventTsMs == 0 && strings.TrimSpace(event.Stage) == policytrace.StageAIDecisionDone {
			aiEventTsMs = event.TimestampMs
		}
		if event.TimestampMs <= 0 || strings.TrimSpace(event.Stage) == "" {
			continue
		}
		stageSource := materializedStageSourceFromEvent(event)
		stages = append(stages, materializedTraceStageDTO{
			Stage:       strings.TrimSpace(event.Stage),
			Source:      stageSource,
			TimestampMs: event.TimestampMs,
		})
		if strings.EqualFold(strings.TrimSpace(event.StageClass), string(policytrace.StageClassAnomaly)) {
			message := "canonical trace anomaly event"
			if event.Reason.Valid && strings.TrimSpace(event.Reason.String) != "" {
				message = strings.TrimSpace(event.Reason.String)
			}
			anomalies = append(anomalies, materializedTraceAnomalyDTO{
				Code:          strings.TrimSpace(event.Stage),
				Message:       message,
				RelatedStages: []string{strings.TrimSpace(event.Stage)},
			})
		}
	}

	if normalized, _, valid := utils.NormalizeTimestampMs(sourceEventTsMs, utils.TimestampNormalizeTraceCompatible); valid {
		sourceEventTsMs = normalized
	} else {
		sourceEventTsMs = 0
	}
	if aiEventTsMs == 0 {
		aiEventTsMs = stageTimestamp(stages, policytrace.StageAIDecisionDone)
	}
	if normalized, _, valid := utils.NormalizeTimestampMs(aiEventTsMs, utils.TimestampNormalizeTraceCompatible); valid {
		aiEventTsMs = normalized
	} else {
		aiEventTsMs = 0
	}
	if sourceEventTsMs > 0 {
		stages = append(stages, materializedTraceStageDTO{
			Stage:       "t_source_event",
			Source:      "durable",
			TimestampMs: sourceEventTsMs,
		})
	}
	if sentinelEventID == "" {
		sentinelEventID = firstNonEmptyAckField(acks, func(row policyAckRow) sql.NullString { return row.SentinelEventID }, traceID)
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

	if selectedAckEventID != "" {
		var attemptAnomalies []materializedTraceAnomalyDTO
		stages, attemptAnomalies = canonicalizeSelectedAckAttemptStages(stages)
		anomalies = append(anomalies, attemptAnomalies...)
	}

	outboxCreatedMs := stageTimestamp(stages, policytrace.StageOutboxRowCreated)
	if outboxCreatedMs > 0 {
		filteredStages := make([]materializedTraceStageDTO, 0, len(stages))
		excludedStages := make([]string, 0, 8)
		for _, stage := range stages {
			if isPostOutboxReplayRuntimeStage(stage, outboxCreatedMs) {
				excludedStages = append(excludedStages, stage.Stage)
				continue
			}
			filteredStages = append(filteredStages, stage)
		}
		if len(excludedStages) > 0 {
			stages = filteredStages
			anomalies = append(anomalies, materializedTraceAnomalyDTO{
				Code:          "post_outbox_runtime_stage",
				Message:       "excluded runtime stages that occurred after outbox persistence for the selected trace",
				RelatedStages: excludedStages,
			})
		}
	}
	publishedMs := stageTimestamp(stages, policytrace.StageControlPublishAck)
	outboxAckMs := stageTimestamp(stages, "t_outbox_acked")
	if outboxAckMs == 0 {
		outboxAckMs = materializedOutboxAckTs(primaryOutbox)
	}
	firstPolicyAckMs := firstStageTimestamp(stages, policytrace.StageAckPersisted)
	if firstPolicyAckMs == 0 {
		firstPolicyAckMs = firstStageTimestamp(stages, policytrace.StageAck)
	}
	if firstPolicyAckMs == 0 {
		firstPolicyAckMs = materializedAckTs(firstAck)
	}
	policyAckMs := lastStageTimestamp(stages, policytrace.StageAckPersisted)
	if policyAckMs == 0 {
		policyAckMs = lastStageTimestamp(stages, policytrace.StageAck)
	}
	if policyAckMs == 0 {
		policyAckMs = materializedAckTs(primaryAck)
	}

	latencies := make([]materializedTraceLatencyDTO, 0, 24)
	appendLatency := func(name string, start, end int64, startStage string, endStage string) {
		if start <= 0 || end <= 0 {
			return
		}
		if duration, ok, negative := utils.DurationMillis(start, end); ok {
			latencies = append(latencies, materializedTraceLatencyDTO{Name: name, DurationMs: duration})
		} else if negative {
			latencies = append(latencies, materializedTraceLatencyDTO{
				Name:       name,
				DurationMs: end - start,
				Negative:   true,
				Reason:     "selected timestamps are causally inverted",
			})
			anomalies = append(anomalies, materializedTraceAnomalyDTO{
				Code:          "negative_latency",
				Message:       fmt.Sprintf("%s is negative because %s precedes %s for the selected canonical events", name, endStage, startStage),
				RelatedStages: []string{startStage, endStage},
			})
		}
	}

	appendLatency("source_to_ai_decision", sourceEventTsMs, aiEventTsMs, "t_source_event", policytrace.StageAIDecisionDone)
	appendLatency("ai_decision_to_outbox_persisted", aiEventTsMs, outboxCreatedMs, policytrace.StageAIDecisionDone, policytrace.StageOutboxRowCreated)
	appendLatency("outbox_persisted_to_published", outboxCreatedMs, publishedMs, policytrace.StageOutboxRowCreated, policytrace.StageControlPublishAck)
	appendLatency("published_to_outbox_ack", publishedMs, outboxAckMs, policytrace.StageControlPublishAck, "t_outbox_acked")
	appendLatency("outbox_persisted_to_outbox_ack", outboxCreatedMs, outboxAckMs, policytrace.StageOutboxRowCreated, "t_outbox_acked")
	appendLatency("published_to_first_policy_ack", publishedMs, firstPolicyAckMs, policytrace.StageControlPublishAck, "t_first_policy_ack")
	appendLatency("source_to_first_policy_ack", sourceEventTsMs, firstPolicyAckMs, "t_source_event", "t_first_policy_ack")
	appendLatency("ai_decision_to_first_policy_ack", aiEventTsMs, firstPolicyAckMs, policytrace.StageAIDecisionDone, "t_first_policy_ack")
	appendLatency("published_to_policy_ack", publishedMs, policyAckMs, policytrace.StageControlPublishAck, "t_policy_ack")
	appendLatency("source_to_policy_ack", sourceEventTsMs, policyAckMs, "t_source_event", "t_policy_ack")
	appendLatency("ai_decision_to_policy_ack", aiEventTsMs, policyAckMs, policytrace.StageAIDecisionDone, "t_policy_ack")
	appendLatency("published_to_ack", publishedMs, policyAckMs, policytrace.StageControlPublishAck, policytrace.StageAck)
	appendLatency("source_to_ack", sourceEventTsMs, policyAckMs, "t_source_event", policytrace.StageAck)
	appendLatency("ai_decision_to_ack", aiEventTsMs, policyAckMs, policytrace.StageAIDecisionDone, policytrace.StageAck)
	appendLatency("telemetry_ingest_to_ai_decision", stageTimestamp(stages, policytrace.StageTelemetryIngest), aiEventTsMs, policytrace.StageTelemetryIngest, policytrace.StageAIDecisionDone)
	appendLatency("telemetry_to_sentinel_emit", stageTimestamp(stages, policytrace.StageTelemetryIngest), stageTimestamp(stages, policytrace.StageSentinelEmit), policytrace.StageTelemetryIngest, policytrace.StageSentinelEmit)
	appendLatency("sentinel_emit_to_ai_decision", stageTimestamp(stages, policytrace.StageSentinelEmit), aiEventTsMs, policytrace.StageSentinelEmit, policytrace.StageAIDecisionDone)
	appendLatency("ai_decision_to_backend_consume", aiEventTsMs, stageTimestamp(stages, policytrace.StageBackendConsume), policytrace.StageAIDecisionDone, policytrace.StageBackendConsume)
	appendLatency("backend_consume_to_commit", stageTimestamp(stages, policytrace.StageBackendConsume), stageTimestamp(stages, policytrace.StageCommit), policytrace.StageBackendConsume, policytrace.StageCommit)
	appendLatency("commit_to_publish", stageTimestamp(stages, policytrace.StageCommit), publishedMs, policytrace.StageCommit, policytrace.StageControlPublishAck)
	committedPublishStartMs := stageTimestampAny(stages, policytrace.StageCommittedPublishStart, policytrace.StageFastPublishStart)
	committedPublishAckMs := stageTimestampAny(stages, policytrace.StageCommittedPublishAck, policytrace.StageFastPublishAck)
	appendLatency("state_apply_to_committed_publish_start", stageTimestamp(stages, policytrace.StageStateApplyDone), committedPublishStartMs, policytrace.StageStateApplyDone, policytrace.StageCommittedPublishStart)
	appendLatency("committed_publish_start_to_ack", committedPublishStartMs, committedPublishAckMs, policytrace.StageCommittedPublishStart, policytrace.StageCommittedPublishAck)
	appendLatency("committed_publish_ack_to_enforcement_consume", committedPublishAckMs, stageTimestamp(stages, policytrace.StageEnforcementConsume), policytrace.StageCommittedPublishAck, policytrace.StageEnforcementConsume)
	appendLatency("state_apply_to_fast_publish_start", stageTimestamp(stages, policytrace.StageStateApplyDone), committedPublishStartMs, policytrace.StageStateApplyDone, policytrace.StageFastPublishStart)
	appendLatency("fast_publish_start_to_ack", committedPublishStartMs, committedPublishAckMs, policytrace.StageFastPublishStart, policytrace.StageFastPublishAck)
	appendLatency("fast_publish_ack_to_enforcement_consume", committedPublishAckMs, stageTimestamp(stages, policytrace.StageEnforcementConsume), policytrace.StageFastPublishAck, policytrace.StageEnforcementConsume)
	appendLatency("publish_to_enforcement_consume", publishedMs, stageTimestamp(stages, policytrace.StageEnforcementConsume), policytrace.StageControlPublishAck, policytrace.StageEnforcementConsume)
	appendLatency("enforcement_consume_to_apply", stageTimestamp(stages, policytrace.StageEnforcementConsume), stageTimestamp(stages, policytrace.StageEnforcementApplyDone), policytrace.StageEnforcementConsume, policytrace.StageEnforcementApplyDone)
	appendLatency("enforcement_apply_to_ack_publish_start", stageTimestamp(stages, policytrace.StageEnforcementApplyDone), stageTimestamp(stages, policytrace.StageAckPublishStart), policytrace.StageEnforcementApplyDone, policytrace.StageAckPublishStart)
	appendLatency("ack_publish_start_to_done", stageTimestamp(stages, policytrace.StageAckPublishStart), stageTimestamp(stages, policytrace.StageAckPublishDone), policytrace.StageAckPublishStart, policytrace.StageAckPublishDone)
	appendLatency("ack_publish_done_to_ack_persisted", stageTimestamp(stages, policytrace.StageAckPublishDone), policyAckMs, policytrace.StageAckPublishDone, policytrace.StageAckPersisted)
	appendLatency("publish_to_ack", publishedMs, policyAckMs, policytrace.StageControlPublishAck, policytrace.StageAck)

	if publishedMs == 0 && (outboxAckMs > 0 || firstPolicyAckMs > 0 || policyAckMs > 0) {
		selectedOutboxID := ""
		if primaryOutbox != nil {
			selectedOutboxID = primaryOutbox.ID
		}
		anomalies = append(anomalies, materializedTraceAnomalyDTO{
			Code:             "missing_publish_stage",
			Message:          "canonical trace has ACK state but no publish stage",
			RelatedStages:    []string{policytrace.StageControlPublishAck, "t_outbox_acked", "t_policy_ack"},
			SelectedOutboxID: selectedOutboxID,
		})
		anomalies = append(anomalies, materializedTraceAnomalyDTO{
			Code:             policytrace.StageAckedWithoutPublish,
			Message:          "ACK exists without a matching publish marker in the selected causal chain",
			RelatedStages:    []string{policytrace.StageControlPublishAck, policytrace.StageAckPersisted},
			SelectedOutboxID: selectedOutboxID,
		})
	}
	if stageTimestampAny(stages, policytrace.StageCommittedPublishAck, policytrace.StageFastPublishAck) > 0 && stageTimestamp(stages, policytrace.StageOutboxReplayNoop) > 0 {
		anomalies = append(anomalies, materializedTraceAnomalyDTO{
			Code:          "outbox_replay_after_committed_publish",
			Message:       "durable outbox replay observed an existing committed-publish ACK and no-oped",
			RelatedStages: []string{policytrace.StageCommittedPublishAck, policytrace.StageOutboxReplayNoop},
		})
		anomalies = append(anomalies, materializedTraceAnomalyDTO{
			Code:          "outbox_replay_after_fast_publish",
			Message:       "durable outbox replay observed an existing committed-publish ACK and no-oped",
			RelatedStages: []string{policytrace.StageFastPublishAck, policytrace.StageOutboxReplayNoop},
		})
	}
	if hasDuplicatePolicyCommand(outbox, acks) {
		anomalies = append(anomalies, materializedTraceAnomalyDTO{
			Code:          "duplicate_policy_command",
			Message:       "multiple records share the same policy command identity",
			RelatedStages: []string{policytrace.StageOutboxRowCreated, policytrace.StageAckPersisted},
		})
	}
	appendCanonicalTraceCompletenessAnomalies(&anomalies, stages)

	actionHistory := materializedActionHistory(outbox, acks)
	currentAction, lastCompletedAction := summarizeActionHistory(actionHistory)
	runtimeStages, durableStages, anomalyStages := splitMaterializedStages(stages)
	return &materializedTraceDTO{
		TraceID:             traceID,
		SourceEventID:       sourceEventID,
		SentinelEventID:     sentinelEventID,
		SourceEventTsMs:     sourceEventTsMs,
		AIEventTsMs:         aiEventTsMs,
		OutboxAck:           materializedOutboxAck(primaryOutbox),
		FirstPolicyAck:      materializedAckSummary(firstAck),
		LatestPolicyAck:     materializedAckSummary(primaryAck),
		ActionHistory:       actionHistory,
		CurrentAction:       currentAction,
		LastCompletedAction: lastCompletedAction,
		Stages:              stages,
		RuntimeStages:       runtimeStages,
		DurableStages:       durableStages,
		AnomalyStages:       anomalyStages,
		Latencies:           latencies,
		Anomalies:           anomalies,
	}
}

func hasDuplicatePolicyCommand(outbox []controlOutboxRow, acks []policyAckRow) bool {
	outboxSeen := make(map[string]struct{})
	for _, row := range outbox {
		policyID := strings.TrimSpace(row.PolicyID)
		commandID := strings.TrimSpace(row.CommandID.String)
		traceID := strings.TrimSpace(row.TraceID.String)
		if policyID == "" || (commandID == "" && traceID == "") {
			continue
		}
		key := policyID + "|" + commandID + "|" + traceID
		if _, ok := outboxSeen[key]; ok {
			return true
		}
		outboxSeen[key] = struct{}{}
	}
	ackSeen := make(map[string]struct{})
	for _, row := range acks {
		policyID := strings.TrimSpace(row.PolicyID)
		commandID := strings.TrimSpace(row.CommandID.String)
		traceID := strings.TrimSpace(row.TraceID.String)
		if policyID == "" || (commandID == "" && traceID == "") {
			continue
		}
		key := policyID + "|" + commandID + "|" + traceID
		if _, ok := ackSeen[key]; ok {
			return true
		}
		ackSeen[key] = struct{}{}
	}
	return false
}

func isPostOutboxReplayRuntimeStage(stage materializedTraceStageDTO, outboxCreatedMs int64) bool {
	if outboxCreatedMs <= 0 || stage.TimestampMs <= outboxCreatedMs {
		return false
	}
	switch strings.TrimSpace(stage.Source) {
	case "durable", "anomaly":
		return false
	}
	switch strings.TrimSpace(stage.Stage) {
	case policytrace.StageSentinelConsume,
		policytrace.StageSentinelAnalysisDone,
		policytrace.StageSentinelEmit,
		policytrace.StageSentinelPublishAck,
		policytrace.StageAISentinelConsume,
		policytrace.StageAIDecisionDone,
		policytrace.StageAIProducerSendStart,
		policytrace.StageAIProducerAck,
		policytrace.StagePolicyHandlerEnter,
		policytrace.StageDecodeDone,
		policytrace.StageBackendConsume,
		policytrace.StageBackendVerifiedDone,
		policytrace.StageMempoolEnqueued,
		policytrace.StageNonceIdempotencyOK,
		policytrace.StageLeaderSelected,
		policytrace.StageProposeStart,
		policytrace.StageProposalBroadcast,
		policytrace.StageQCFormed,
		policytrace.StageCommit,
		policytrace.StageStateApplyDone:
		return true
	default:
		return false
	}
}

func appendCanonicalTraceCompletenessAnomalies(anomalies *[]materializedTraceAnomalyDTO, stages []materializedTraceStageDTO) {
	if anomalies == nil || len(stages) == 0 {
		return
	}
	required := []string{
		policytrace.StageTelemetryIngest,
		policytrace.StageSentinelConsume,
		policytrace.StageSentinelAnalysisDone,
		policytrace.StageSentinelEmit,
		policytrace.StageAIDecisionDone,
		policytrace.StageAIProducerSendStart,
		policytrace.StageAIProducerAck,
		policytrace.StageBackendConsume,
		policytrace.StageOutboxRowCreated,
		policytrace.StageControlPublishAck,
		policytrace.StageEnforcementConsume,
		policytrace.StageEnforcementApplyDone,
		policytrace.StageAckPublishStart,
		policytrace.StageAckPublishDone,
	}
	missing := make([]string, 0, len(required)+1)
	for _, stage := range required {
		if stageTimestamp(stages, stage) == 0 {
			missing = append(missing, stage)
		}
	}
	if stageTimestamp(stages, policytrace.StageAckPersisted) == 0 && stageTimestamp(stages, policytrace.StageAck) == 0 {
		missing = append(missing, policytrace.StageAckPersisted)
	}
	if len(missing) == 0 {
		return
	}
	*anomalies = append(*anomalies, materializedTraceAnomalyDTO{
		Code:          "canonical_trace_incomplete",
		Message:       "canonical trace is missing source-authored stages required for full layer-by-layer latency",
		RelatedStages: missing,
	})
}

func materializedStageSourceFromEvent(event controlTraceEventRow) string {
	stageClass := strings.TrimSpace(event.StageClass)
	switch stageClass {
	case string(policytrace.StageClassDurable):
		return "durable"
	case string(policytrace.StageClassAnomaly):
		return "anomaly"
	default:
		source := strings.TrimSpace(event.StageSource)
		if source == "" {
			return "runtime"
		}
		return source
	}
}

func selectCanonicalEventTraceID(events []controlTraceEventRow, fallbackTraceID string) string {
	fallbackTraceID = strings.TrimSpace(fallbackTraceID)
	if fallbackTraceID != "" {
		for _, event := range events {
			if strings.TrimSpace(event.TraceID) == fallbackTraceID {
				return fallbackTraceID
			}
		}
	}
	type traceScore struct {
		traceID     string
		latestTs    int64
		eventCount  int
		durableSeen bool
	}
	scores := make(map[string]traceScore, len(events))
	for _, event := range events {
		traceID := strings.TrimSpace(event.TraceID)
		if traceID == "" {
			continue
		}
		score := scores[traceID]
		score.traceID = traceID
		score.eventCount++
		if event.TimestampMs > score.latestTs {
			score.latestTs = event.TimestampMs
		}
		if strings.EqualFold(strings.TrimSpace(event.StageClass), string(policytrace.StageClassDurable)) {
			score.durableSeen = true
		}
		scores[traceID] = score
	}
	best := traceScore{}
	for _, score := range scores {
		if best.traceID == "" ||
			(score.durableSeen && !best.durableSeen) ||
			(score.durableSeen == best.durableSeen && score.latestTs > best.latestTs) ||
			(score.durableSeen == best.durableSeen && score.latestTs == best.latestTs && score.eventCount > best.eventCount) ||
			(score.durableSeen == best.durableSeen && score.latestTs == best.latestTs && score.eventCount == best.eventCount && score.traceID < best.traceID) {
			best = score
		}
	}
	return best.traceID
}

func filterCanonicalTraceEvents(events []controlTraceEventRow, traceID string, sourceEventIDs map[string]struct{}, sentinelEventIDs map[string]struct{}, selectedOutboxID string, selectedAckEventID string) []controlTraceEventRow {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil
	}
	selectedOutboxID = strings.TrimSpace(selectedOutboxID)
	selectedAckEventID = strings.TrimSpace(selectedAckEventID)
	filtered := make([]controlTraceEventRow, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.TraceID) != traceID && !eventMatchesLineage(event, sourceEventIDs, sentinelEventIDs) {
			continue
		}
		if !eventMatchesSelectedCausalChain(event, selectedOutboxID, selectedAckEventID) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func eventMatchesSelectedCausalChain(event controlTraceEventRow, selectedOutboxID string, selectedAckEventID string) bool {
	stage := strings.TrimSpace(event.Stage)
	if selectedOutboxID != "" && eventStageBelongsToOutbox(stage) && event.OutboxID.Valid && strings.TrimSpace(event.OutboxID.String) != selectedOutboxID {
		return false
	}
	if selectedAckEventID != "" && eventStageBelongsToAck(stage) {
		if !event.AckEventID.Valid {
			return false
		}
		if strings.TrimSpace(event.AckEventID.String) != selectedAckEventID {
			return false
		}
	}
	return true
}

func canonicalizeSelectedAckAttemptStages(stages []materializedTraceStageDTO) ([]materializedTraceStageDTO, []materializedTraceAnomalyDTO) {
	enforcementConsumeMs := firstStageTimestamp(stages, policytrace.StageEnforcementConsume)
	if enforcementConsumeMs <= 0 {
		return stages, nil
	}

	selected := make(map[string]int64, 12)
	if ts := firstStageTimestampAtOrAfter(stages, policytrace.StageEnforcementConsume, enforcementConsumeMs); ts > 0 {
		selected[policytrace.StageEnforcementConsume] = ts
	}
	if ts := firstStageTimestampAtOrAfter(stages, policytrace.StageEnforcementApplyDone, enforcementConsumeMs); ts > 0 {
		selected[policytrace.StageEnforcementApplyDone] = ts
	}
	applyDoneMs := selected[policytrace.StageEnforcementApplyDone]
	ackStartFloor := enforcementConsumeMs
	if applyDoneMs > 0 {
		ackStartFloor = applyDoneMs
	}
	if ts := firstStageTimestampAtOrAfter(stages, policytrace.StageAckPublishStart, ackStartFloor); ts > 0 {
		selected[policytrace.StageAckPublishStart] = ts
	}
	ackDoneFloor := selected[policytrace.StageAckPublishStart]
	if ackDoneFloor <= 0 {
		ackDoneFloor = ackStartFloor
	}
	if ts := firstStageTimestampAtOrAfter(stages, policytrace.StageAckPublishDone, ackDoneFloor); ts > 0 {
		selected[policytrace.StageAckPublishDone] = ts
	}
	ackReceivedFloor := selected[policytrace.StageAckPublishDone]
	if ackReceivedFloor <= 0 {
		ackReceivedFloor = ackDoneFloor
	}
	if ts := firstStageTimestampAtOrAfter(stages, policytrace.StageAckReceived, ackReceivedFloor); ts > 0 {
		selected[policytrace.StageAckReceived] = ts
	}
	ackPersistedFloor := selected[policytrace.StageAckReceived]
	if ackPersistedFloor <= 0 {
		ackPersistedFloor = ackReceivedFloor
	}
	if ts := firstStageTimestampAtOrAfter(stages, policytrace.StageAckPersisted, ackPersistedFloor); ts > 0 {
		selected[policytrace.StageAckPersisted] = ts
	}

	if stage, ts := lastStageTimestampAtOrBeforeAny(stages, []string{policytrace.StageCommittedPublishAck, policytrace.StageFastPublishAck}, enforcementConsumeMs); ts > 0 {
		selected[stage] = ts
		if startStage, startTs := lastStageTimestampAtOrBeforeAny(stages, []string{policytrace.StageCommittedPublishStart, policytrace.StageFastPublishStart}, ts); startTs > 0 {
			selected[startStage] = startTs
			if stateApplyTs := lastStageTimestampAtOrBefore(stages, policytrace.StageStateApplyDone, startTs); stateApplyTs > 0 {
				selected[policytrace.StageStateApplyDone] = stateApplyTs
				if commitTs := lastStageTimestampAtOrBefore(stages, policytrace.StageCommit, stateApplyTs); commitTs > 0 {
					selected[policytrace.StageCommit] = commitTs
				}
			}
		}
	}

	selectedPublishStages := map[string]struct{}{}
	for _, stage := range []string{
		policytrace.StageCommittedPublishStart,
		policytrace.StageCommittedPublishAck,
		policytrace.StageFastPublishStart,
		policytrace.StageFastPublishAck,
	} {
		if selected[stage] > 0 {
			selectedPublishStages[stage] = struct{}{}
		}
	}

	dropped := 0
	filtered := make([]materializedTraceStageDTO, 0, len(stages))
	kept := make(map[string]bool, len(selected))
	for _, stage := range stages {
		name := strings.TrimSpace(stage.Stage)
		if desiredTs, ok := selected[name]; ok {
			if stage.TimestampMs == desiredTs && !kept[name] {
				filtered = append(filtered, stage)
				kept[name] = true
			} else {
				dropped++
			}
			continue
		}
		if isAttemptScopedStage(name) {
			dropped++
			continue
		}
		if isCommittedPublishStage(name) && len(selectedPublishStages) > 0 {
			dropped++
			continue
		}
		filtered = append(filtered, stage)
	}
	if dropped == 0 {
		return filtered, nil
	}
	return filtered, []materializedTraceAnomalyDTO{{
		Code:          "duplicate_policy_attempts_collapsed",
		Message:       "multiple publish/enforcement/ACK attempts shared the selected trace; latency projection uses the selected ACK causal attempt",
		RelatedStages: []string{policytrace.StageCommittedPublishAck, policytrace.StageEnforcementConsume, policytrace.StageAckPersisted},
	}}
}

func isAttemptScopedStage(stage string) bool {
	switch strings.TrimSpace(stage) {
	case policytrace.StageCommit,
		policytrace.StageStateApplyDone,
		policytrace.StageEnforcementConsume,
		policytrace.StageEnforcementApplyDone,
		policytrace.StageEnforcementPersisted,
		policytrace.StageAckPublishStart,
		policytrace.StageAckPublishDone,
		policytrace.StageAckReceived,
		policytrace.StageAckPersisted,
		policytrace.StageAck:
		return true
	default:
		return false
	}
}

func isCommittedPublishStage(stage string) bool {
	switch strings.TrimSpace(stage) {
	case policytrace.StageCommittedPublishStart,
		policytrace.StageCommittedPublishAck,
		policytrace.StageFastPublishStart,
		policytrace.StageFastPublishAck:
		return true
	default:
		return false
	}
}

func eventStageBelongsToOutbox(stage string) bool {
	switch strings.TrimSpace(stage) {
	case policytrace.StageOutboxRowCreated,
		policytrace.StageOutboxClaimed,
		policytrace.StageControlPublishStart,
		policytrace.StageControlPublishAck,
		policytrace.StageOutboxMarkDone:
		return true
	default:
		return false
	}
}

func eventStageBelongsToAck(stage string) bool {
	switch strings.TrimSpace(stage) {
	case policytrace.StageEnforcementConsume,
		policytrace.StageEnforcementApplyDone,
		policytrace.StageEnforcementPersisted,
		policytrace.StageAckPublishStart,
		policytrace.StageAckPublishDone,
		policytrace.StageAckReceived,
		policytrace.StageAckPersisted,
		policytrace.StageAck:
		return true
	default:
		return false
	}
}

func eventMatchesLineage(event controlTraceEventRow, sourceEventIDs map[string]struct{}, sentinelEventIDs map[string]struct{}) bool {
	if event.SourceEventID.Valid {
		if _, ok := sourceEventIDs[strings.TrimSpace(event.SourceEventID.String)]; ok {
			return true
		}
	}
	if event.SentinelEventID.Valid {
		if _, ok := sentinelEventIDs[strings.TrimSpace(event.SentinelEventID.String)]; ok {
			return true
		}
	}
	return false
}

func splitMaterializedStages(stages []materializedTraceStageDTO) ([]materializedTraceStageDTO, []materializedTraceStageDTO, []materializedTraceStageDTO) {
	if len(stages) == 0 {
		return nil, nil, nil
	}
	runtimeStages := make([]materializedTraceStageDTO, 0, len(stages))
	durableStages := make([]materializedTraceStageDTO, 0, len(stages))
	anomalyStages := make([]materializedTraceStageDTO, 0, len(stages))
	for _, stage := range stages {
		switch strings.TrimSpace(stage.Source) {
		case "durable":
			durableStages = append(durableStages, stage)
		case "anomaly":
			anomalyStages = append(anomalyStages, stage)
		default:
			runtimeStages = append(runtimeStages, stage)
		}
	}
	return runtimeStages, durableStages, anomalyStages
}

func materializeControlTrace(outbox []controlOutboxRow, acks []policyAckRow, runtime []runtimeTraceMarkerDTO) *materializedTraceDTO {
	if len(outbox) == 0 && len(acks) == 0 && len(runtime) == 0 {
		return nil
	}

	selection := selectMaterializedTraceContext(outbox, acks, runtime)
	primaryOutbox := selection.Outbox
	primaryAck := selection.Ack
	traceID := selection.TraceID
	firstAck, latestAck := selectMaterializedAckBounds(acks, traceID)
	if latestAck != nil {
		primaryAck = latestAck
	}
	anomalies := append([]materializedTraceAnomalyDTO(nil), selection.Anomalies...)
	sourceEventID := ""
	sentinelEventID := ""
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
		if primaryOutbox.SentinelEventID.Valid {
			sentinelEventID = primaryOutbox.SentinelEventID.String
		}
	}
	if normalized, _, valid := utils.NormalizeTimestampMs(sourceEventTsMs, utils.TimestampNormalizeTraceCompatible); valid {
		sourceEventTsMs = normalized
	} else {
		sourceEventTsMs = 0
	}
	if normalized, _, valid := utils.NormalizeTimestampMs(aiEventTsMs, utils.TimestampNormalizeTraceCompatible); valid {
		aiEventTsMs = normalized
	} else {
		aiEventTsMs = 0
	}
	if sentinelEventID == "" {
		if ackSentinel := firstNonEmptyAckField(acks, func(row policyAckRow) sql.NullString { return row.SentinelEventID }, traceID); ackSentinel != "" {
			sentinelEventID = ackSentinel
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
	appendLatency := func(name string, start, end int64, startStage string, endStage string) {
		if start <= 0 || end <= 0 {
			return
		}
		if duration, ok, negative := utils.DurationMillis(start, end); ok {
			latencies = append(latencies, materializedTraceLatencyDTO{Name: name, DurationMs: duration})
		} else if negative {
			latencies = append(latencies, materializedTraceLatencyDTO{
				Name:       name,
				DurationMs: end - start,
				Negative:   true,
				Reason:     "selected timestamps are causally inverted",
			})
			anomalies = append(anomalies, materializedTraceAnomalyDTO{
				Code:          "negative_latency",
				Message:       fmt.Sprintf("%s is negative because %s precedes %s for the selected durable rows", name, endStage, startStage),
				RelatedStages: []string{startStage, endStage},
			})
		}
	}
	appendLatency("source_to_ai_decision", sourceEventTsMs, aiEventTsMs, "t_source_event", "t_ai_decision_done")
	appendLatency("ai_decision_to_outbox_persisted", aiEventTsMs, outboxCreatedMs, "t_ai_decision_done", "t_outbox_row_created")
	appendLatency("outbox_persisted_to_published", outboxCreatedMs, publishedMs, "t_outbox_row_created", "t_control_publish_ack")
	appendLatency("published_to_outbox_ack", publishedMs, outboxAckMs, "t_control_publish_ack", "t_outbox_acked")
	appendLatency("outbox_persisted_to_outbox_ack", outboxCreatedMs, outboxAckMs, "t_outbox_row_created", "t_outbox_acked")
	appendLatency("published_to_first_policy_ack", publishedMs, firstPolicyAckMs, "t_control_publish_ack", "t_first_policy_ack")
	appendLatency("source_to_first_policy_ack", sourceEventTsMs, firstPolicyAckMs, "t_source_event", "t_first_policy_ack")
	appendLatency("ai_decision_to_first_policy_ack", aiEventTsMs, firstPolicyAckMs, "t_ai_decision_done", "t_first_policy_ack")
	appendLatency("published_to_policy_ack", publishedMs, policyAckMs, "t_control_publish_ack", "t_policy_ack")
	appendLatency("source_to_policy_ack", sourceEventTsMs, policyAckMs, "t_source_event", "t_policy_ack")
	appendLatency("ai_decision_to_policy_ack", aiEventTsMs, policyAckMs, "t_ai_decision_done", "t_policy_ack")
	appendLatency("published_to_ack", publishedMs, policyAckMs, "t_control_publish_ack", "t_ack")
	appendLatency("source_to_ack", sourceEventTsMs, policyAckMs, "t_source_event", "t_ack")
	appendLatency("ai_decision_to_ack", aiEventTsMs, policyAckMs, "t_ai_decision_done", "t_ack")
	appendLatency("telemetry_ingest_to_ai_decision", upstreamStages["t_telemetry_ingest"], aiEventTsMs, "t_telemetry_ingest", "t_ai_decision_done")
	appendLatency("telemetry_to_sentinel_emit", upstreamStages["t_telemetry_ingest"], upstreamStages["t_sentinel_emit"], "t_telemetry_ingest", "t_sentinel_emit")
	appendLatency("sentinel_emit_to_ai_decision", upstreamStages["t_sentinel_emit"], aiEventTsMs, "t_sentinel_emit", "t_ai_decision_done")
	appendLatency("ai_decision_to_backend_consume", aiEventTsMs, stageTimestamp(stages, "t_backend_consume"), "t_ai_decision_done", "t_backend_consume")
	appendLatency("backend_consume_to_commit", stageTimestamp(stages, "t_backend_consume"), stageTimestamp(stages, "t_commit"), "t_backend_consume", "t_commit")
	appendLatency("commit_to_publish", stageTimestamp(stages, "t_commit"), publishedMs, "t_commit", "t_control_publish_ack")
	appendLatency("publish_to_ack", publishedMs, policyAckMs, "t_control_publish_ack", "t_ack")

	if primaryOutbox != nil && !primaryOutbox.PublishedAt.Valid && (outboxAckMs > 0 || firstPolicyAckMs > 0 || policyAckMs > 0) {
		anomalies = append(anomalies, materializedTraceAnomalyDTO{
			Code:             "missing_publish_stage",
			Message:          "selected durable outbox row has ACK state but no published_at timestamp",
			RelatedStages:    []string{"t_control_publish_ack", "t_outbox_acked", "t_policy_ack"},
			SelectedOutboxID: primaryOutbox.ID,
		})
	}

	actionHistory := materializedActionHistory(outbox, acks)
	currentAction, lastCompletedAction := summarizeActionHistory(actionHistory)
	runtimeStages, durableStages, anomalyStages := splitMaterializedStages(stages)
	return &materializedTraceDTO{
		TraceID:             traceID,
		SourceEventID:       sourceEventID,
		SentinelEventID:     sentinelEventID,
		SourceEventTsMs:     sourceEventTsMs,
		AIEventTsMs:         aiEventTsMs,
		OutboxAck:           materializedOutboxAck(primaryOutbox),
		FirstPolicyAck:      materializedAckSummary(firstAck),
		LatestPolicyAck:     materializedAckSummary(primaryAck),
		ActionHistory:       actionHistory,
		CurrentAction:       currentAction,
		LastCompletedAction: lastCompletedAction,
		Stages:              stages,
		RuntimeStages:       runtimeStages,
		DurableStages:       durableStages,
		AnomalyStages:       anomalyStages,
		Latencies:           latencies,
		Anomalies:           anomalies,
	}
}

func materializedTraceLineage(outbox []controlOutboxRow, acks []policyAckRow, materialized *materializedTraceDTO) (string, string, string) {
	if materialized != nil {
		return materialized.TraceID, materialized.SourceEventID, materialized.SentinelEventID
	}

	traceID := ""
	sourceEventID := ""
	sentinelEventID := ""
	if len(outbox) > 0 {
		if outbox[0].TraceID.Valid {
			traceID = strings.TrimSpace(outbox[0].TraceID.String)
		}
		if outbox[0].SourceEventID.Valid {
			sourceEventID = strings.TrimSpace(outbox[0].SourceEventID.String)
		}
		if outbox[0].SentinelEventID.Valid {
			sentinelEventID = strings.TrimSpace(outbox[0].SentinelEventID.String)
		}
	}
	if traceID == "" {
		traceID = firstNonEmptyAckField(acks, func(row policyAckRow) sql.NullString { return row.TraceID }, "")
	}
	if sourceEventID == "" {
		sourceEventID = firstNonEmptyAckField(acks, func(row policyAckRow) sql.NullString { return row.SourceEventID }, traceID)
	}
	if sentinelEventID == "" {
		sentinelEventID = firstNonEmptyAckField(acks, func(row policyAckRow) sql.NullString { return row.SentinelEventID }, traceID)
	}
	return traceID, sourceEventID, sentinelEventID
}

func firstNonEmptyAckField(acks []policyAckRow, field func(policyAckRow) sql.NullString, traceID string) string {
	for _, ack := range acks {
		if traceID != "" {
			if policyAckRowTraceID(ack) != traceID {
				continue
			}
		}
		value := field(ack)
		if value.Valid && strings.TrimSpace(value.String) != "" {
			return strings.TrimSpace(value.String)
		}
	}
	return ""
}

func materializedOutboxAck(outbox *controlOutboxRow) *materializedOutboxAckDTO {
	if outbox == nil {
		return nil
	}
	if !outbox.AckedAt.Valid && !outbox.AckResult.Valid && !outbox.AckReason.Valid && !outbox.AckController.Valid {
		return nil
	}
	dto := &materializedOutboxAckDTO{}
	if outbox.AckResult.Valid {
		dto.Result = outbox.AckResult.String
	}
	if outbox.AckReason.Valid {
		dto.Reason = outbox.AckReason.String
	}
	if outbox.AckController.Valid {
		dto.Controller = outbox.AckController.String
	}
	if outbox.AckedAt.Valid {
		dto.AckedAtMs = outbox.AckedAt.Time.UTC().UnixMilli()
	}
	return dto
}

func materializedAckSummary(ack *policyAckRow) *materializedTraceAckSummaryDTO {
	if ack == nil {
		return nil
	}
	dto := &materializedTraceAckSummaryDTO{}
	dto.ControllerInstance = ack.ControllerInstance
	dto.Result = ack.Result
	if ack.Reason.Valid {
		dto.Reason = ack.Reason.String
	}
	if ack.ErrorCode.Valid {
		dto.ErrorCode = ack.ErrorCode.String
	}
	if ack.AppliedAt.Valid {
		dto.AppliedAtMs = ack.AppliedAt.Time.UTC().UnixMilli()
	}
	if ack.AckedAt.Valid {
		dto.AckedAtMs = ack.AckedAt.Time.UTC().UnixMilli()
	}
	if ack.TraceID.Valid {
		dto.TraceID = ack.TraceID.String
	}
	if ack.SourceEventID.Valid {
		dto.SourceEventID = ack.SourceEventID.String
	}
	if ack.SentinelEventID.Valid {
		dto.SentinelEventID = ack.SentinelEventID.String
	}
	return dto
}

func materializedActionHistory(outbox []controlOutboxRow, acks []policyAckRow) []materializedTraceActionDTO {
	if len(outbox) == 0 {
		return nil
	}

	latestAckByTrace := make(map[string]*policyAckRow, len(acks))
	for i := range acks {
		traceID := policyAckRowTraceID(acks[i])
		if traceID == "" {
			continue
		}
		existing := latestAckByTrace[traceID]
		if existing == nil || materializedAckTs(&acks[i]) >= materializedAckTs(existing) {
			latestAckByTrace[traceID] = &acks[i]
		}
	}

	history := make([]materializedTraceActionDTO, 0, len(outbox))
	for i := len(outbox) - 1; i >= 0; i-- {
		row := outbox[i]
		action, ruleType, rollbackPolicyID := tracePayloadAction(row.Payload)
		item := materializedTraceActionDTO{
			Action:           action,
			RuleType:         ruleType,
			RollbackPolicyID: rollbackPolicyID,
			OutboxStatus:     row.Status,
			OutboxAck:        materializedOutboxAck(&row),
		}
		if row.TraceID.Valid {
			item.TraceID = strings.TrimSpace(row.TraceID.String)
		}
		if !row.CreatedAt.IsZero() {
			item.CreatedAtMs = row.CreatedAt.UTC().UnixMilli()
		}
		if row.PublishedAt.Valid {
			item.PublishedAtMs = row.PublishedAt.Time.UTC().UnixMilli()
		}
		if row.SourceEventID.Valid {
			item.SourceEventID = strings.TrimSpace(row.SourceEventID.String)
		}
		if row.SentinelEventID.Valid {
			item.SentinelEventID = strings.TrimSpace(row.SentinelEventID.String)
		}
		if item.TraceID != "" {
			item.LatestPolicyAck = materializedAckSummary(latestAckByTrace[item.TraceID])
		}
		history = append(history, item)
	}
	return history
}

func summarizeActionHistory(history []materializedTraceActionDTO) (*materializedTraceActionDTO, *materializedTraceActionDTO) {
	if len(history) == 0 {
		return nil, nil
	}
	current := history[len(history)-1]
	var lastCompleted *materializedTraceActionDTO
	for i := len(history) - 1; i >= 0; i-- {
		if actionIsCompleted(history[i]) {
			item := history[i]
			lastCompleted = &item
			break
		}
	}
	return &current, lastCompleted
}

func actionIsCompleted(action materializedTraceActionDTO) bool {
	switch strings.TrimSpace(action.OutboxStatus) {
	case "acked", "terminal":
		return true
	}
	if action.OutboxAck != nil {
		return true
	}
	if action.LatestPolicyAck != nil {
		return true
	}
	return false
}

func tracePayloadAction(payload []byte) (string, string, string) {
	if len(payload) == 0 {
		return "", "", ""
	}
	var root map[string]interface{}
	if err := json.Unmarshal(payload, &root); err != nil {
		return "", "", ""
	}
	params, ok := root["params"].(map[string]interface{})
	if !ok {
		params = root
	}
	action, _ := params["action"].(string)
	ruleType, _ := params["rule_type"].(string)
	rollbackPolicyID, _ := params["rollback_policy_id"].(string)
	return strings.TrimSpace(action), strings.TrimSpace(ruleType), strings.TrimSpace(rollbackPolicyID)
}

func selectMaterializedAckBounds(acks []policyAckRow, traceID string) (*policyAckRow, *policyAckRow) {
	if strings.TrimSpace(traceID) == "" {
		return nil, nil
	}
	var first *policyAckRow
	var latest *policyAckRow
	for i := range acks {
		if traceID != "" {
			if policyAckRowTraceID(acks[i]) != traceID {
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
		if ts, _, valid := utils.NormalizeTimestampMsAny(rawStages[name], utils.TimestampNormalizeTraceCompatible); valid && ts > 0 {
			stages[name] = ts
		}
	}
	return stages
}

func stageTimestamp(stages []materializedTraceStageDTO, stage string) int64 {
	for _, item := range stages {
		if item.Stage == stage && item.TimestampMs > 0 {
			return item.TimestampMs
		}
	}
	return 0
}

func stageTimestampAny(stages []materializedTraceStageDTO, stageNames ...string) int64 {
	for _, stage := range stageNames {
		if ts := stageTimestamp(stages, stage); ts > 0 {
			return ts
		}
	}
	return 0
}

func firstStageTimestamp(stages []materializedTraceStageDTO, stage string) int64 {
	for _, item := range stages {
		if item.Stage == stage && item.TimestampMs > 0 {
			return item.TimestampMs
		}
	}
	return 0
}

func lastStageTimestamp(stages []materializedTraceStageDTO, stage string) int64 {
	for i := len(stages) - 1; i >= 0; i-- {
		if stages[i].Stage == stage && stages[i].TimestampMs > 0 {
			return stages[i].TimestampMs
		}
	}
	return 0
}

func firstStageTimestampAtOrAfter(stages []materializedTraceStageDTO, stage string, floor int64) int64 {
	var ts int64
	for _, item := range stages {
		if item.Stage != stage || item.TimestampMs < floor {
			continue
		}
		if ts == 0 || item.TimestampMs < ts {
			ts = item.TimestampMs
		}
	}
	return ts
}

func lastStageTimestampAtOrBefore(stages []materializedTraceStageDTO, stage string, ceiling int64) int64 {
	var ts int64
	for _, item := range stages {
		if item.Stage != stage || item.TimestampMs > ceiling {
			continue
		}
		if item.TimestampMs > ts {
			ts = item.TimestampMs
		}
	}
	return ts
}

func lastStageTimestampAtOrBeforeAny(stages []materializedTraceStageDTO, stageNames []string, ceiling int64) (string, int64) {
	stageSet := make(map[string]struct{}, len(stageNames))
	for _, stage := range stageNames {
		stageSet[stage] = struct{}{}
	}
	var selectedStage string
	var ts int64
	for _, item := range stages {
		if _, ok := stageSet[item.Stage]; !ok || item.TimestampMs > ceiling {
			continue
		}
		if item.TimestampMs > ts {
			selectedStage = item.Stage
			ts = item.TimestampMs
		}
	}
	return selectedStage, ts
}

func selectMaterializedTraceRows(outbox []controlOutboxRow, acks []policyAckRow, runtime []runtimeTraceMarkerDTO) (*controlOutboxRow, *policyAckRow, string) {
	selection := selectMaterializedTraceContext(outbox, acks, runtime)
	return selection.Outbox, selection.Ack, selection.TraceID
}

func selectMaterializedTraceContext(outbox []controlOutboxRow, acks []policyAckRow, runtime []runtimeTraceMarkerDTO) materializedTraceSelection {
	traceID := ""
	for _, row := range outbox {
		if row.TraceID.Valid && strings.TrimSpace(row.TraceID.String) != "" {
			traceID = strings.TrimSpace(row.TraceID.String)
			break
		}
	}
	if traceID == "" {
		for _, ack := range acks {
			if ackTraceID := policyAckRowTraceID(ack); ackTraceID != "" {
				traceID = ackTraceID
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
	anomalies := make([]materializedTraceAnomalyDTO, 0, 2)
	if traceID != "" {
		candidateIdx := make([]int, 0, len(outbox))
		for i := range outbox {
			if outbox[i].TraceID.Valid && strings.TrimSpace(outbox[i].TraceID.String) == traceID {
				candidateIdx = append(candidateIdx, i)
			}
		}
		if len(candidateIdx) > 0 {
			selectedIdx := candidateIdx[0]
			for _, idx := range candidateIdx[1:] {
				if controlOutboxRowCanonicalLess(outbox[idx], outbox[selectedIdx]) {
					selectedIdx = idx
				}
			}
			selectedOutbox = &outbox[selectedIdx]
			if len(candidateIdx) > 1 {
				siblingIDs := make([]string, 0, len(candidateIdx)-1)
				for _, idx := range candidateIdx {
					if idx == selectedIdx {
						continue
					}
					siblingIDs = append(siblingIDs, outbox[idx].ID)
				}
				anomalies = append(anomalies, materializedTraceAnomalyDTO{
					Code:             "duplicate_outbox_rows",
					Message:          fmt.Sprintf("multiple durable outbox rows share trace_id %q; canonical selection preferred the earliest publish-capable row", traceID),
					SelectedOutboxID: outbox[selectedIdx].ID,
					SiblingOutboxIDs: siblingIDs,
				})
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
			if policyAckRowTraceID(acks[i]) == traceID {
				selectedAck = &acks[i]
				break
			}
		}
	}

	return materializedTraceSelection{
		Outbox:    selectedOutbox,
		Ack:       selectedAck,
		TraceID:   traceID,
		Anomalies: anomalies,
	}
}

func controlOutboxRowCanonicalLess(a, b controlOutboxRow) bool {
	if rankA, rankB := controlOutboxCanonicalStatusRank(a), controlOutboxCanonicalStatusRank(b); rankA != rankB {
		return rankA < rankB
	}
	if a.BlockHeight != b.BlockHeight {
		if a.BlockHeight == 0 {
			return false
		}
		if b.BlockHeight == 0 {
			return true
		}
		return a.BlockHeight < b.BlockHeight
	}
	if a.TxIndex != b.TxIndex {
		return a.TxIndex < b.TxIndex
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		if a.CreatedAt.IsZero() {
			return false
		}
		if b.CreatedAt.IsZero() {
			return true
		}
		return a.CreatedAt.Before(b.CreatedAt)
	}
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		if a.UpdatedAt.IsZero() {
			return false
		}
		if b.UpdatedAt.IsZero() {
			return true
		}
		return a.UpdatedAt.Before(b.UpdatedAt)
	}
	return strings.TrimSpace(a.ID) < strings.TrimSpace(b.ID)
}

func controlOutboxCanonicalStatusRank(row controlOutboxRow) int {
	switch {
	case row.AckedAt.Valid || strings.EqualFold(strings.TrimSpace(row.Status), "acked"):
		return 0
	case row.PublishedAt.Valid || strings.EqualFold(strings.TrimSpace(row.Status), "published"):
		return 1
	case strings.EqualFold(strings.TrimSpace(row.Status), "publishing"):
		return 2
	case strings.EqualFold(strings.TrimSpace(row.Status), "retry"):
		return 3
	case strings.EqualFold(strings.TrimSpace(row.Status), "pending"):
		return 4
	case strings.EqualFold(strings.TrimSpace(row.Status), "terminal_failed"):
		return 5
	default:
		return 6
	}
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

func policyAckRowTraceID(row policyAckRow) string {
	return policyAckEffectiveTraceID(nullStringValue(row.TraceID), nullStringValue(row.QCReference))
}

func policyAckEffectiveTraceID(traceID, qcReference string) string {
	if trimmed := strings.TrimSpace(traceID); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(qcReference)
}

func nullStringValue(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return strings.TrimSpace(v.String)
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
		"leases":                       leaseRows,
		"control_mutation_safe_mode":   s.currentControlMutationSafeMode(r.Context()),
		"control_mutation_kill_switch": s.currentControlMutationKillSwitch(r.Context()),
	}
	if s.outboxStats != nil {
		if stats, ok := s.outboxStats.GetPolicyOutboxDispatcherStats(); ok {
			resp["dispatcher"] = sanitizeDispatcherStats(stats)
		}
	}

	writeJSONResponse(w, r, NewSuccessResponse(resp), http.StatusOK)
}

func sanitizeDispatcherStats(stats policyoutbox.DispatcherStats) policyoutbox.DispatcherStats {
	stats.PublishLatencyBuckets = sanitizeHistogramBuckets(stats.PublishLatencyBuckets)
	stats.PublishLatencySumMs = sanitizeFloat(stats.PublishLatencySumMs)
	stats.PublishLatencyP95Ms = sanitizeFloat(stats.PublishLatencyP95Ms)
	stats.ClaimLatencyBuckets = sanitizeHistogramBuckets(stats.ClaimLatencyBuckets)
	stats.ClaimLatencySumMs = sanitizeFloat(stats.ClaimLatencySumMs)
	stats.ClaimLatencyP95Ms = sanitizeFloat(stats.ClaimLatencyP95Ms)
	stats.LeaseAcquireLatencyBuckets = sanitizeHistogramBuckets(stats.LeaseAcquireLatencyBuckets)
	stats.LeaseAcquireLatencySumMs = sanitizeFloat(stats.LeaseAcquireLatencySumMs)
	stats.LeaseAcquireLatencyP95Ms = sanitizeFloat(stats.LeaseAcquireLatencyP95Ms)
	stats.MarkLatencyBuckets = sanitizeHistogramBuckets(stats.MarkLatencyBuckets)
	stats.MarkLatencySumMs = sanitizeFloat(stats.MarkLatencySumMs)
	stats.MarkLatencyP95Ms = sanitizeFloat(stats.MarkLatencyP95Ms)
	stats.TickLatencyBuckets = sanitizeHistogramBuckets(stats.TickLatencyBuckets)
	stats.TickLatencySumMs = sanitizeFloat(stats.TickLatencySumMs)
	stats.TickLatencyP95Ms = sanitizeFloat(stats.TickLatencyP95Ms)
	stats.AIToPublishBuckets = sanitizeHistogramBuckets(stats.AIToPublishBuckets)
	stats.AIToPublishSumMs = sanitizeFloat(stats.AIToPublishSumMs)
	stats.AIToPublishP95Ms = sanitizeFloat(stats.AIToPublishP95Ms)
	stats.SourceToPublishBuckets = sanitizeHistogramBuckets(stats.SourceToPublishBuckets)
	stats.SourceToPublishSumMs = sanitizeFloat(stats.SourceToPublishSumMs)
	stats.SourceToPublishP95Ms = sanitizeFloat(stats.SourceToPublishP95Ms)
	stats.CommitToPublishBuckets = sanitizeHistogramBuckets(stats.CommitToPublishBuckets)
	stats.CommitToPublishSumMs = sanitizeFloat(stats.CommitToPublishSumMs)
	stats.CommitToPublishP95Ms = sanitizeFloat(stats.CommitToPublishP95Ms)
	stats.OutboxCreatedToClaimedBuckets = sanitizeHistogramBuckets(stats.OutboxCreatedToClaimedBuckets)
	stats.OutboxCreatedToClaimedSumMs = sanitizeFloat(stats.OutboxCreatedToClaimedSumMs)
	stats.OutboxCreatedToClaimedP95Ms = sanitizeFloat(stats.OutboxCreatedToClaimedP95Ms)
	stats.WakeQueueDepthAvg = sanitizeFloat(stats.WakeQueueDepthAvg)
	stats.WakeToClaimBuckets = sanitizeHistogramBuckets(stats.WakeToClaimBuckets)
	stats.WakeToClaimSumMs = sanitizeFloat(stats.WakeToClaimSumMs)
	stats.WakeToClaimP95Ms = sanitizeFloat(stats.WakeToClaimP95Ms)
	return stats
}

func sanitizeHistogramBuckets(src []utils.HistogramBucket) []utils.HistogramBucket {
	if len(src) == 0 {
		return nil
	}
	out := make([]utils.HistogramBucket, len(src))
	copy(out, src)
	for i := range out {
		if math.IsInf(out[i].UpperBound, 1) || math.IsNaN(out[i].UpperBound) {
			out[i].UpperBound = math.MaxFloat64
		} else {
			out[i].UpperBound = sanitizeFloat(out[i].UpperBound)
		}
	}
	return out
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
	schemaCtx, schemaCancel := context.WithTimeout(r.Context(), s.controlReadTimeout())
	schema, _ := loadControlSchemaSupport(schemaCtx, db)
	schemaCancel()

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
	if traceID := strings.TrimSpace(r.URL.Query().Get("trace_id")); traceID != "" {
		if !requireControlSchemaColumn(w, r, schema.AckTraceID, "trace_id", "018") {
			return
		}
		where = append(where, fmt.Sprintf("pa.trace_id = $%d", len(args)+1))
		args = append(args, traceID)
	}
	if sourceEventID := strings.TrimSpace(r.URL.Query().Get("source_event_id")); sourceEventID != "" {
		if !requireControlSchemaColumn(w, r, schema.AckSourceEventID, "source_event_id", "018") {
			return
		}
		where = append(where, fmt.Sprintf("pa.source_event_id = $%d", len(args)+1))
		args = append(args, sourceEventID)
	}
	if sentinelEventID := strings.TrimSpace(r.URL.Query().Get("sentinel_event_id")); sentinelEventID != "" {
		if !requireControlSchemaColumn(w, r, schema.AckSentinelEventID, "sentinel_event_id", "018") {
			return
		}
		where = append(where, fmt.Sprintf("pa.sentinel_event_id = $%d", len(args)+1))
		args = append(args, sentinelEventID)
	}
	if ackEventID := strings.TrimSpace(r.URL.Query().Get("ack_event_id")); ackEventID != "" {
		if !requireControlSchemaColumn(w, r, schema.AckEventID, "ack_event_id", "021") {
			return
		}
		where = append(where, fmt.Sprintf("pa.ack_event_id = $%d", len(args)+1))
		args = append(args, ackEventID)
	}
	if requestID := strings.TrimSpace(r.URL.Query().Get("request_id")); requestID != "" {
		if !requireControlSchemaColumn(w, r, schema.AckRequestID, "request_id", "022") {
			return
		}
		where = append(where, fmt.Sprintf("pa.request_id = $%d", len(args)+1))
		args = append(args, requestID)
	}
	if commandID := strings.TrimSpace(r.URL.Query().Get("command_id")); commandID != "" {
		if !requireControlSchemaColumn(w, r, schema.AckCommandID, "command_id", "023") {
			return
		}
		where = append(where, fmt.Sprintf("pa.command_id = $%d", len(args)+1))
		args = append(args, commandID)
	}
	if workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id")); workflowID != "" {
		if !requireControlSchemaColumn(w, r, schema.AckWorkflowID, "workflow_id", "024") {
			return
		}
		where = append(where, fmt.Sprintf("pa.workflow_id = $%d", len(args)+1))
		args = append(args, workflowID)
	}
	if result := strings.TrimSpace(r.URL.Query().Get("result")); result != "" {
		where = append(where, fmt.Sprintf("pa.result = $%d", len(args)+1))
		args = append(args, result)
	}
	if instance := strings.TrimSpace(r.URL.Query().Get("controller_instance")); instance != "" {
		where = append(where, fmt.Sprintf("pa.controller_instance = $%d", len(args)+1))
		args = append(args, instance)
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
			pa.policy_id, %s, %s, %s, %s, pa.controller_instance,
			pa.scope_identifier, pa.tenant, pa.region,
			pa.result, pa.reason, pa.error_code,
			pa.applied_at, pa.acked_at,
			pa.qc_reference, %s, %s, %s, pa.fast_path,
			pa.rule_hash, pa.producer_id,
			pa.observed_at,
			COALESCE(hist.ack_history_count, 0) AS ack_history_count,
			hist.first_event_acked_at,
			hist.latest_event_acked_at
		FROM policy_acks pa
		LEFT JOIN LATERAL (
			SELECT
				count(*) AS ack_history_count,
				min(acked_at) AS first_event_acked_at,
				max(acked_at) AS latest_event_acked_at
			FROM policy_ack_events pae
			WHERE pae.policy_id = pa.policy_id
			  AND pae.controller_instance = pa.controller_instance
		) hist
		  ON true
		WHERE %s
		ORDER BY COALESCE(pa.acked_at, pa.observed_at) DESC, pa.policy_id DESC, pa.controller_instance DESC
		LIMIT $%d`, controlAckEventIDSelectExpr(schema), controlAckRequestIDSelectExpr(schema), controlAckCommandIDSelectExpr(schema), controlAckWorkflowIDSelectExpr(schema), controlAckTraceSelectExpr(schema), controlAckSourceEventSelectExpr(schema), controlAckSentinelEventSelectExpr(schema), strings.Join(where, " AND "), len(args)+1)
	args = append(args, fetchLimit)

	ctx, cancel := context.WithTimeout(r.Context(), s.controlReadTimeout())
	defer cancel()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if isTransientControlStorageError(err) {
			writeJSONResponse(w, r, NewSuccessResponse(controlAcksListResponse{
				Rows:       []policyAckPayload{},
				Pagination: controlListMeta{Limit: limit},
			}), http.StatusOK)
			return
		}
		writeErrorResponse(w, r, "ACKS_QUERY_FAILED", "failed to query control acks", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]policyAckPayload, 0, limit)
	for rows.Next() {
		var row policyAckRow
		if scanErr := rows.Scan(
			&row.PolicyID, &row.AckEventID, &row.RequestID, &row.CommandID, &row.WorkflowID, &row.ControllerInstance,
			&row.ScopeIdentifier, &row.Tenant, &row.Region,
			&row.Result, &row.Reason, &row.ErrorCode,
			&row.AppliedAt, &row.AckedAt,
			&row.QCReference, &row.TraceID, &row.SourceEventID, &row.SentinelEventID, &row.FastPath,
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

func isTransientControlStorageError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "i/o timeout"):
		return true
	case strings.Contains(msg, "context deadline exceeded"):
		return true
	case strings.Contains(msg, "hostname resolving error"):
		return true
	case strings.Contains(msg, "failed to connect"):
		return true
	default:
		return false
	}
}

func controlOutboxHasNarrowingFilter(values url.Values) bool {
	if values == nil {
		return false
	}
	for _, key := range []string{
		"status",
		"policy_id",
		"request_id",
		"command_id",
		"workflow_id",
		"trace_id",
		"sentinel_event_id",
		"anomaly_id",
		"flow_id",
		"source_id",
		"source_type",
		"sensor_id",
		"validator_id",
		"scope_identifier",
		"block_height",
		"from",
		"to",
		"cursor",
	} {
		if strings.TrimSpace(values.Get(key)) != "" {
			return true
		}
	}
	return false
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
	dto.AnomalyID = strings.TrimSpace(row.AnomalyID)
	dto.FlowID = strings.TrimSpace(row.FlowID)
	dto.SourceID = strings.TrimSpace(row.SourceID)
	dto.SourceType = strings.TrimSpace(row.SourceType)
	dto.SensorID = strings.TrimSpace(row.SensorID)
	dto.ValidatorID = strings.TrimSpace(row.ValidatorID)
	dto.ScopeIdentifier = strings.TrimSpace(row.ScopeIdentifier)
	ctx := parseOutboxOperationalContext(row.Payload)
	if dto.AnomalyID == "" {
		dto.AnomalyID = ctx.AnomalyID
	}
	if dto.FlowID == "" {
		dto.FlowID = ctx.FlowID
	}
	if dto.SourceID == "" {
		dto.SourceID = ctx.SourceID
	}
	if dto.SourceType == "" {
		dto.SourceType = ctx.SourceType
	}
	if dto.SensorID == "" {
		dto.SensorID = ctx.SensorID
	}
	if dto.ValidatorID == "" {
		dto.ValidatorID = ctx.ValidatorID
	}
	if dto.ScopeIdentifier == "" {
		dto.ScopeIdentifier = ctx.ScopeIdentifier
	}
	if row.RequestID.Valid {
		dto.RequestID = row.RequestID.String
	} else if requestID := parseOutboxRequestID(row.Payload); requestID != "" {
		dto.RequestID = requestID
	}
	if row.CommandID.Valid {
		dto.CommandID = row.CommandID.String
	} else if commandID := parseOutboxCommandID(row.Payload); commandID != "" {
		dto.CommandID = commandID
	}
	if row.WorkflowID.Valid {
		dto.WorkflowID = row.WorkflowID.String
	} else if workflowID := parseOutboxWorkflowID(row.Payload); workflowID != "" {
		dto.WorkflowID = workflowID
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
	if row.SentinelEventID.Valid {
		dto.SentinelEventID = row.SentinelEventID.String
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

func parseOutboxRequestID(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	requestID := extractMapString(payload, "request_id")
	if metadata, ok := payload["metadata"].(map[string]interface{}); ok && requestID == "" {
		requestID = extractMapString(metadata, "request_id")
	}
	if trace, ok := payload["trace"].(map[string]interface{}); ok && requestID == "" {
		requestID = extractMapString(trace, "request_id")
	}
	if requestID == "" {
		if params, ok := payload["params"].(map[string]interface{}); ok {
			requestID = parseOutboxRequestIDMap(params)
		}
	}
	return requestID
}

func parseOutboxRequestIDMap(payload map[string]interface{}) string {
	requestID := extractMapString(payload, "request_id")
	if metadata, ok := payload["metadata"].(map[string]interface{}); ok && requestID == "" {
		requestID = extractMapString(metadata, "request_id")
	}
	if trace, ok := payload["trace"].(map[string]interface{}); ok && requestID == "" {
		requestID = extractMapString(trace, "request_id")
	}
	return requestID
}

func parseOutboxCommandID(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	commandID := parseOutboxCommandIDMap(payload)
	if commandID == "" {
		if params, ok := payload["params"].(map[string]interface{}); ok {
			commandID = parseOutboxCommandIDMap(params)
		}
	}
	return commandID
}

func parseOutboxCommandIDMap(payload map[string]interface{}) string {
	commandID := extractMapString(payload, "command_id")
	if metadata, ok := payload["metadata"].(map[string]interface{}); ok && commandID == "" {
		commandID = extractMapString(metadata, "command_id")
	}
	if trace, ok := payload["trace"].(map[string]interface{}); ok && commandID == "" {
		commandID = extractMapString(trace, "command_id")
	}
	return commandID
}

func parseOutboxWorkflowID(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	workflowID := parseOutboxWorkflowIDMap(payload)
	if workflowID == "" {
		if params, ok := payload["params"].(map[string]interface{}); ok {
			workflowID = parseOutboxWorkflowIDMap(params)
		}
	}
	return workflowID
}

func parseOutboxWorkflowIDMap(payload map[string]interface{}) string {
	workflowID := extractMapString(payload, "workflow_id")
	if metadata, ok := payload["metadata"].(map[string]interface{}); ok && workflowID == "" {
		workflowID = extractMapString(metadata, "workflow_id")
	}
	if trace, ok := payload["trace"].(map[string]interface{}); ok && workflowID == "" {
		workflowID = extractMapString(trace, "workflow_id")
	}
	return workflowID
}

func outboxPayloadStringExpr(paths ...string) string {
	parts := make([]string, 0, len(paths))
	payloadExpr := "(convert_from(control_policy_outbox.payload, 'UTF8')::JSONB)"
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("NULLIF(%s #>> '%s', '')", payloadExpr, path))
	}
	if len(parts) == 0 {
		return "''"
	}
	return fmt.Sprintf("COALESCE(%s, '')", strings.Join(parts, ", "))
}

func (f outboxOperationalFilters) hasAny() bool {
	return f.AnomalyID != "" ||
		f.FlowID != "" ||
		f.SourceID != "" ||
		f.SourceType != "" ||
		f.SensorID != "" ||
		f.ValidatorID != "" ||
		f.ScopeIdentifier != ""
}

func (f outboxOperationalFilters) matches(row controlOutboxRowDTO) bool {
	if f.AnomalyID != "" && row.AnomalyID != f.AnomalyID {
		return false
	}
	if f.FlowID != "" && row.FlowID != f.FlowID {
		return false
	}
	if f.SourceID != "" && row.SourceID != f.SourceID {
		return false
	}
	if f.SourceType != "" && row.SourceType != f.SourceType {
		return false
	}
	if f.SensorID != "" && row.SensorID != f.SensorID {
		return false
	}
	if f.ValidatorID != "" && row.ValidatorID != f.ValidatorID {
		return false
	}
	if f.ScopeIdentifier != "" && row.ScopeIdentifier != f.ScopeIdentifier {
		return false
	}
	return true
}

func parseOutboxOperationalContext(raw []byte) outboxOperationalContext {
	if len(raw) == 0 {
		return outboxOperationalContext{}
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return outboxOperationalContext{}
	}
	ctx := extractOperationalContext(payload)
	if params, ok := payload["params"].(map[string]interface{}); ok {
		mergeOperationalContext(&ctx, extractOperationalContext(params))
	}
	return ctx
}

func mergeOperationalContext(dst *outboxOperationalContext, src outboxOperationalContext) {
	if dst == nil {
		return
	}
	if dst.AnomalyID == "" {
		dst.AnomalyID = src.AnomalyID
	}
	if dst.FlowID == "" {
		dst.FlowID = src.FlowID
	}
	if dst.SourceID == "" {
		dst.SourceID = src.SourceID
	}
	if dst.SourceType == "" {
		dst.SourceType = src.SourceType
	}
	if dst.SensorID == "" {
		dst.SensorID = src.SensorID
	}
	if dst.ValidatorID == "" {
		dst.ValidatorID = src.ValidatorID
	}
	if dst.ScopeIdentifier == "" {
		dst.ScopeIdentifier = src.ScopeIdentifier
	}
}

func extractOperationalContext(payload map[string]interface{}) outboxOperationalContext {
	if payload == nil {
		return outboxOperationalContext{}
	}
	ctx := outboxOperationalContext{
		AnomalyID:       extractMapString(payload, "anomaly_id"),
		FlowID:          extractMapString(payload, "flow_id"),
		SourceID:        extractMapString(payload, "source_id"),
		SourceType:      extractMapString(payload, "source_type"),
		SensorID:        extractMapString(payload, "sensor_id"),
		ValidatorID:     extractMapString(payload, "validator_id"),
		ScopeIdentifier: extractMapString(payload, "scope_identifier"),
	}
	if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
		if ctx.AnomalyID == "" {
			ctx.AnomalyID = extractMapString(metadata, "anomaly_id")
		}
		if ctx.FlowID == "" {
			ctx.FlowID = extractMapString(metadata, "flow_id")
		}
		if ctx.SourceID == "" {
			ctx.SourceID = extractMapString(metadata, "source_id")
		}
		if ctx.SourceType == "" {
			ctx.SourceType = extractMapString(metadata, "source_type")
		}
		if ctx.SensorID == "" {
			ctx.SensorID = extractMapString(metadata, "sensor_id")
		}
		if ctx.ValidatorID == "" {
			ctx.ValidatorID = extractMapString(metadata, "validator_id")
		}
		if ctx.ScopeIdentifier == "" {
			ctx.ScopeIdentifier = extractMapString(metadata, "scope_identifier")
		}
	}
	if trace, ok := payload["trace"].(map[string]interface{}); ok {
		if ctx.FlowID == "" {
			ctx.FlowID = extractMapString(trace, "flow_id")
		}
		if ctx.SourceID == "" {
			ctx.SourceID = extractMapString(trace, "source_id")
		}
		if ctx.SourceType == "" {
			ctx.SourceType = extractMapString(trace, "source_type")
		}
		if ctx.SensorID == "" {
			ctx.SensorID = extractMapString(trace, "sensor_id")
		}
		if ctx.ValidatorID == "" {
			ctx.ValidatorID = extractMapString(trace, "validator_id")
		}
		if ctx.ScopeIdentifier == "" {
			ctx.ScopeIdentifier = extractMapString(trace, "scope_identifier")
		}
	}
	if input, ok := payload["input"].(map[string]interface{}); ok {
		if ctx.FlowID == "" {
			ctx.FlowID = extractMapString(input, "flow_id")
		}
		if ctx.SourceID == "" {
			ctx.SourceID = extractMapString(input, "source_id")
		}
		if ctx.SourceType == "" {
			ctx.SourceType = extractMapString(input, "source_type")
		}
		if ctx.SensorID == "" {
			ctx.SensorID = extractMapString(input, "sensor_id")
		}
	}
	return ctx
}

func extractMapString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	value, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
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
	if row.AckEventID.Valid {
		p.AckEventID = row.AckEventID.String
	}
	if row.RequestID.Valid {
		p.RequestID = row.RequestID.String
	}
	if row.CommandID.Valid {
		p.CommandID = row.CommandID.String
	}
	if row.WorkflowID.Valid {
		p.WorkflowID = row.WorkflowID.String
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
	if row.TraceID.Valid {
		p.TraceID = row.TraceID.String
	}
	if row.SourceEventID.Valid {
		p.SourceEventID = row.SourceEventID.String
	}
	if row.SentinelEventID.Valid {
		p.SentinelEventID = row.SentinelEventID.String
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
	envelope, err := s.resolveNormalizedSecurityEnvelope(r)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(envelope.Access.ActiveAccessID), nil
}
