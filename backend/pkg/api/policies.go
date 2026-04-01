package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"backend/pkg/control/policystate"
	"backend/pkg/utils"
)

type policyAckRow struct {
	PolicyID           string
	AckEventID         sql.NullString
	RequestID          sql.NullString
	CommandID          sql.NullString
	WorkflowID         sql.NullString
	ControllerInstance string
	ScopeIdentifier    sql.NullString
	Tenant             sql.NullString
	Region             sql.NullString
	Result             string
	Reason             sql.NullString
	ErrorCode          sql.NullString
	AppliedAt          sql.NullTime
	AckedAt            sql.NullTime
	QCReference        sql.NullString
	TraceID            sql.NullString
	SourceEventID      sql.NullString
	SentinelEventID    sql.NullString
	FastPath           bool
	RuleHash           []byte
	ProducerID         []byte
	ObservedAt         time.Time
	AckHistoryCount    int64
	FirstEventAckedAt  sql.NullTime
	LatestEventAckedAt sql.NullTime
}

type policyAckResponse struct {
	PolicyID string             `json:"policy_id"`
	Count    int                `json:"count"`
	Acks     []policyAckPayload `json:"acks"`
}

type policyAckPayload struct {
	PolicyID           string `json:"policy_id,omitempty"`
	AckEventID         string `json:"ack_event_id,omitempty"`
	RequestID          string `json:"request_id,omitempty"`
	CommandID          string `json:"command_id,omitempty"`
	WorkflowID         string `json:"workflow_id,omitempty"`
	ControllerInstance string `json:"controller_instance"`
	ScopeIdentifier    string `json:"scope_identifier,omitempty"`
	Tenant             string `json:"tenant,omitempty"`
	Region             string `json:"region,omitempty"`
	Result             string `json:"result"`
	Reason             string `json:"reason,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	AppliedAt          int64  `json:"applied_at,omitempty"`
	AckedAt            int64  `json:"acked_at,omitempty"`
	QCReference        string `json:"qc_reference,omitempty"`
	TraceID            string `json:"trace_id,omitempty"`
	SourceEventID      string `json:"source_event_id,omitempty"`
	SentinelEventID    string `json:"sentinel_event_id,omitempty"`
	FastPath           bool   `json:"fast_path"`
	RuleHashHex        string `json:"rule_hash_hex,omitempty"`
	ProducerIDHex      string `json:"producer_id_hex,omitempty"`
	ObservedAt         int64  `json:"observed_at"`
	AckHistoryCount    int64  `json:"ack_history_count,omitempty"`
	FirstEventAckedAt  int64  `json:"first_event_acked_at,omitempty"`
	LatestEventAckedAt int64  `json:"latest_event_acked_at,omitempty"`
}

type policySummaryDTO struct {
	PolicyID            string `json:"policy_id"`
	RequestID           string `json:"request_id,omitempty"`
	CommandID           string `json:"command_id,omitempty"`
	WorkflowID          string `json:"workflow_id,omitempty"`
	AnomalyID           string `json:"anomaly_id,omitempty"`
	FlowID              string `json:"flow_id,omitempty"`
	SourceID            string `json:"source_id,omitempty"`
	SourceType          string `json:"source_type,omitempty"`
	SensorID            string `json:"sensor_id,omitempty"`
	ValidatorID         string `json:"validator_id,omitempty"`
	ScopeIdentifier     string `json:"scope_identifier,omitempty"`
	TraceID             string `json:"trace_id,omitempty"`
	SourceEventID       string `json:"source_event_id,omitempty"`
	SentinelEventID     string `json:"sentinel_event_id,omitempty"`
	LatestOutboxID      string `json:"latest_outbox_id,omitempty"`
	LatestStatus        string `json:"latest_status,omitempty"`
	LatestCreatedAt     int64  `json:"latest_created_at,omitempty"`
	LatestPublishedAt   int64  `json:"latest_published_at,omitempty"`
	LatestAckedAt       int64  `json:"latest_acked_at,omitempty"`
	LatestAckEventID    string `json:"latest_ack_event_id,omitempty"`
	LatestAckResult     string `json:"latest_ack_result,omitempty"`
	LatestAckController string `json:"latest_ack_controller,omitempty"`
	OutboxCount         int64  `json:"outbox_count,omitempty"`
	AckCount            int64  `json:"ack_count,omitempty"`
}

type policyListResponse struct {
	Rows       []policySummaryDTO `json:"rows"`
	Pagination controlListMeta    `json:"pagination"`
}

type policyDetailResponse struct {
	Summary policySummaryDTO     `json:"summary"`
	Trace   controlTraceResponse `json:"trace"`
}

type policyCoverageResponse struct {
	PolicyID              string  `json:"policy_id"`
	OutboxCount           int64   `json:"outbox_count"`
	PendingCount          int64   `json:"pending_count"`
	PublishingCount       int64   `json:"publishing_count"`
	PublishedCount        int64   `json:"published_count"`
	RetryCount            int64   `json:"retry_count"`
	TerminalFailedCount   int64   `json:"terminal_failed_count"`
	AckedCount            int64   `json:"acked_count"`
	PublishedOrAckedCount int64   `json:"published_or_acked_count"`
	AckHistoryCount       int64   `json:"ack_history_count"`
	LatestAckEventID      string  `json:"latest_ack_event_id,omitempty"`
	LatestAckResult       string  `json:"latest_ack_result,omitempty"`
	LatestAckController   string  `json:"latest_ack_controller,omitempty"`
	LatestAckedAt         int64   `json:"latest_acked_at,omitempty"`
	PublishCoverageRatio  float64 `json:"publish_coverage_ratio"`
	AckCoverageRatio      float64 `json:"ack_coverage_ratio"`
}

type policyListRow struct {
	PolicyID             string
	RequestID            sql.NullString
	CommandID            sql.NullString
	WorkflowID           sql.NullString
	AnomalyID            string
	FlowID               string
	SourceID             string
	SourceType           string
	SensorID             string
	ValidatorID          string
	ScopeIdentifier      string
	TraceID              sql.NullString
	SourceEventID        sql.NullString
	SentinelEventID      sql.NullString
	LatestOutboxID       string
	LatestStatus         string
	LatestCreatedAt      time.Time
	LatestPublishedAt    sql.NullTime
	LatestAckedAt        sql.NullTime
	LatestAckEventID     sql.NullString
	LatestAckResult      sql.NullString
	LatestAckController  sql.NullString
	LatestAckTraceID     sql.NullString
	LatestAckSourceEvent sql.NullString
	LatestAckSentinel    sql.NullString
	OutboxCount          int64
	AckCount             int64
}

// handlePoliciesList handles:
// - GET /policies
func (s *Server) handlePoliciesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	limit, err := parseControlLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeErrorResponse(w, r, "INVALID_LIMIT", err.Error(), http.StatusBadRequest)
		return
	}
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}

	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	schemaCtx, schemaCancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	schema, _ := loadControlSchemaSupport(schemaCtx, db)
	schemaCancel()

	args := []interface{}{}
	where := []string{"1=1"}
	if tenantScope != "" {
		tenantArg := len(args) + 1
		where = append(where, fmt.Sprintf("(EXISTS (SELECT 1 FROM policy_acks pa WHERE pa.policy_id = control_policy_outbox.policy_id AND pa.tenant = $%d) OR %s = $%d)", tenantArg, outboxPayloadStringExpr("{tenant}", "{metadata,tenant}", "{trace,tenant}", "{target,tenant}", "{target,tenant_id}"), tenantArg))
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
	if workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id")); workflowID != "" {
		if !requireControlSchemaColumn(w, r, schema.OutboxWorkflowID, "workflow_id", "024") {
			return
		}
		where = append(where, fmt.Sprintf("workflow_id = $%d", len(args)+1))
		args = append(args, workflowID)
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("cursor")); cursor != "" {
		cursorAt, cursorPolicyID, parseErr := decodePolicyCursor(cursor)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_CURSOR", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("(created_at, policy_id) < ($%d, $%d)", len(args)+1, len(args)+2))
		args = append(args, cursorAt, cursorPolicyID)
	}
	args = append(args, limit+1)

	query := fmt.Sprintf(`
		WITH latest_outbox AS (
			SELECT * FROM (
				SELECT
					policy_id,
					id::STRING AS outbox_id,
					%s, %s, %s, %s, %s, %s, %s, %s, %s, %s,
					trace_id, source_event_id, %s,
					status, created_at, published_at, acked_at,
					ROW_NUMBER() OVER (PARTITION BY policy_id ORDER BY created_at DESC, id DESC) AS rn
				FROM control_policy_outbox
				WHERE %s
			) ranked
			WHERE rn = 1
		),
		outbox_counts AS (
			SELECT policy_id, COUNT(*) AS outbox_count
			FROM control_policy_outbox
			WHERE policy_id IN (SELECT policy_id FROM latest_outbox)
			GROUP BY policy_id
		),
		latest_ack AS (
			SELECT * FROM (
				SELECT
					policy_id, %s, controller_instance, result, acked_at, observed_at,
					%s, %s, %s,
					ROW_NUMBER() OVER (PARTITION BY policy_id ORDER BY acked_at DESC NULLS LAST, observed_at DESC, controller_instance ASC) AS rn
				FROM policy_acks
				WHERE policy_id IN (SELECT policy_id FROM latest_outbox)%s
			) ranked
			WHERE rn = 1
		),
		ack_counts AS (
			SELECT policy_id, COUNT(*) AS ack_count
			FROM policy_ack_events
			WHERE policy_id IN (SELECT policy_id FROM latest_outbox)
			GROUP BY policy_id
		)
		SELECT
			lo.policy_id,
			lo.request_id, lo.command_id, lo.workflow_id,
			lo.anomaly_id, lo.flow_id, lo.source_id, lo.source_type, lo.sensor_id, lo.validator_id, lo.scope_identifier,
			lo.trace_id, lo.source_event_id, lo.sentinel_event_id,
			lo.outbox_id, lo.status, lo.created_at, lo.published_at, lo.acked_at,
			la.ack_event_id, la.result, la.controller_instance, la.trace_id, la.source_event_id, la.sentinel_event_id,
			COALESCE(oc.outbox_count, 0),
			COALESCE(ac.ack_count, 0)
		FROM latest_outbox lo
		LEFT JOIN outbox_counts oc ON oc.policy_id = lo.policy_id
		LEFT JOIN latest_ack la ON la.policy_id = lo.policy_id
		LEFT JOIN ack_counts ac ON ac.policy_id = lo.policy_id
		ORDER BY lo.created_at DESC, lo.policy_id DESC
		LIMIT $%d
	`, controlOutboxRequestIDSelectExpr(schema), controlOutboxCommandIDSelectExpr(schema), controlOutboxWorkflowIDSelectExpr(schema), controlOutboxAnomalyIDSelectExpr(schema), controlOutboxFlowIDSelectExpr(), controlOutboxSourceIDSelectExpr(), controlOutboxSourceTypeSelectExpr(), controlOutboxSensorIDSelectExpr(), controlOutboxValidatorIDSelectExpr(), controlOutboxScopeIdentifierSelectExpr(), controlOutboxSentinelSelectExpr(schema), strings.Join(where, " AND "), controlAckEventIDSelectExpr(schema), controlAckTraceSelectExpr(schema), controlAckSourceEventSelectExpr(schema), controlAckSentinelEventSelectExpr(schema), ackTenantWhereClause(tenantScope), len(args))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		writeErrorResponse(w, r, "POLICIES_QUERY_FAILED", "failed to query policies", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]policySummaryDTO, 0, limit+1)
	for rows.Next() {
		var row policyListRow
		if scanErr := rows.Scan(
			&row.PolicyID,
			&row.RequestID, &row.CommandID, &row.WorkflowID,
			&row.AnomalyID, &row.FlowID, &row.SourceID, &row.SourceType, &row.SensorID, &row.ValidatorID, &row.ScopeIdentifier,
			&row.TraceID, &row.SourceEventID, &row.SentinelEventID,
			&row.LatestOutboxID, &row.LatestStatus, &row.LatestCreatedAt, &row.LatestPublishedAt, &row.LatestAckedAt,
			&row.LatestAckEventID, &row.LatestAckResult, &row.LatestAckController, &row.LatestAckTraceID, &row.LatestAckSourceEvent, &row.LatestAckSentinel,
			&row.OutboxCount, &row.AckCount,
		); scanErr != nil {
			writeErrorResponse(w, r, "POLICIES_SCAN_FAILED", "failed to read policy row", http.StatusInternalServerError)
			return
		}
		items = append(items, policyListRowToDTO(row))
	}
	if err := rows.Err(); err != nil {
		writeErrorResponse(w, r, "POLICIES_ITERATION_FAILED", "failed to iterate policy rows", http.StatusInternalServerError)
		return
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		nextCursor = encodePolicyCursor(time.Unix(last.LatestCreatedAt, 0).UTC(), last.PolicyID)
		items = items[:limit]
	}
	if err := overlayStateOnPolicySummaries(ctx, db, items); err != nil {
		writeErrorResponse(w, r, "POLICY_STATE_READ_FAILED", "failed to read policy state projection", http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, r, NewSuccessResponse(policyListResponse{
		Rows:       items,
		Pagination: controlListMeta{Limit: limit, NextCursor: nextCursor},
	}), http.StatusOK)
}

// handlePoliciesGet handles:
// - GET /policies/{policy_id}
// - GET /policies/{policy_id}/coverage
// - POST /policies/{policy_id}:revoke
// - POST /policies/{policy_id}:approve
// - POST /policies/{policy_id}:reject
func (s *Server) handlePoliciesGet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		switch {
		case hasPolicyMutationSuffix(strings.TrimSuffix(s.config.BasePath, "/"), r.URL.Path, ":revoke"):
			s.handlePolicyRevoke(w, r)
		case hasPolicyMutationSuffix(strings.TrimSuffix(s.config.BasePath, "/"), r.URL.Path, ":approve"):
			s.handlePolicyDecision(w, r, "approve")
		case hasPolicyMutationSuffix(strings.TrimSuffix(s.config.BasePath, "/"), r.URL.Path, ":reject"):
			s.handlePolicyDecision(w, r, "reject")
		default:
			writeErrorResponse(w, r, "INVALID_MUTATION_PATH", "path must end with :revoke, :approve, or :reject", http.StatusBadRequest)
		}
		return
	}
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET/POST methods allowed", http.StatusMethodNotAllowed)
		return
	}
	policyID, coverage, ok := extractPolicyRoute(strings.TrimSuffix(s.config.BasePath, "/"), r.URL.Path)
	if !ok || policyID == "" {
		writeErrorResponse(w, r, "INVALID_POLICY_ID", "policy_id is required", http.StatusBadRequest)
		return
	}
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}

	trace, err := s.loadPolicyTraceDetail(r.Context(), policyID, tenantScope)
	if err != nil {
		writeErrorResponse(w, r, "POLICY_LOOKUP_FAILED", "failed to load policy detail", http.StatusInternalServerError)
		return
	}
	if len(trace.Outbox) == 0 && len(trace.Acks) == 0 && trace.PolicyID == "" {
		writeErrorResponse(w, r, "POLICY_NOT_FOUND", "policy not found", http.StatusNotFound)
		return
	}

	if coverage {
		writeJSONResponse(w, r, NewSuccessResponse(buildPolicyCoverage(trace)), http.StatusOK)
		return
	}
	summary := buildPolicySummary(trace)
	if db, dbErr := s.getDB(); dbErr == nil {
		if rows, stateErr := loadPolicyStateRows(r.Context(), db, []string{policyID}); stateErr != nil {
			writeErrorResponse(w, r, "POLICY_STATE_READ_FAILED", "failed to read policy state projection", http.StatusInternalServerError)
			return
		} else if row, ok := rows[policyID]; ok {
			overlayStateOnPolicySummary(&summary, row)
		}
	}
	writeJSONResponse(w, r, NewSuccessResponse(policyDetailResponse{
		Summary: summary,
		Trace:   trace,
	}), http.StatusOK)
}

func (s *Server) handlePolicyRevoke(w http.ResponseWriter, r *http.Request) {
	policyID, ok := extractPolicyMutationRoute(strings.TrimSuffix(s.config.BasePath, "/"), r.URL.Path, ":revoke")
	if !ok || policyID == "" {
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
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.controlMutationTimeout())
	defer cancel()
	row, err := s.loadLatestPolicyOutboxRow(ctx, db, policyID)
	if err == sql.ErrNoRows {
		writeErrorResponse(w, r, "POLICY_NOT_FOUND", "policy not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeErrorResponse(w, r, "POLICY_LOOKUP_FAILED", "failed to resolve policy revoke target", http.StatusInternalServerError)
		return
	}
	if tenantScope != "" {
		visible, visErr := s.policyVisibleToTenant(ctx, db, row, tenantScope)
		if visErr != nil {
			writeErrorResponse(w, r, "TENANT_SCOPE_QUERY_FAILED", "failed to verify tenant scope", http.StatusInternalServerError)
			return
		}
		if !visible {
			writeErrorResponse(w, r, "TENANT_SCOPE_FORBIDDEN", "requested policy is outside tenant scope", http.StatusForbidden)
			return
		}
	}
	if strings.EqualFold(extractPolicyPayloadAction(row.Payload), "remove") {
		writeErrorResponse(w, r, "INVALID_STATE_TRANSITION", "policy already represents a revoke action", http.StatusConflict)
		return
	}
	s.handleControlOutboxMutation(w, r, row.ID+":revoke")
}

func (s *Server) handlePolicyDecision(w http.ResponseWriter, r *http.Request, action string) {
	suffix := ":" + action
	policyID, ok := extractPolicyMutationRoute(strings.TrimSuffix(s.config.BasePath, "/"), r.URL.Path, suffix)
	if !ok || policyID == "" {
		writeErrorResponse(w, r, "INVALID_POLICY_ID", "policy_id is required", http.StatusBadRequest)
		return
	}
	if err := s.requireControlMutationAllowed(r); err != nil {
		writeErrorResponse(w, r, err.Code, err.Message, err.HTTPStatus)
		return
	}

	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idemKey == "" {
		writeErrorResponse(w, r, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required", http.StatusBadRequest)
		return
	}
	if len(idemKey) > 128 {
		writeErrorResponse(w, r, "INVALID_IDEMPOTENCY_KEY", "idempotency key too long", http.StatusBadRequest)
		return
	}

	req, err := parseControlMutationRequest(r)
	if err != nil {
		writeErrorResponse(w, r, "INVALID_MUTATION_REQUEST", err.Error(), http.StatusBadRequest)
		return
	}
	if !reasonCodePattern.MatchString(req.ReasonCode) {
		writeErrorResponse(w, r, "INVALID_REASON_CODE", "reason_code format is invalid", http.StatusBadRequest)
		return
	}
	if len(req.ReasonText) == 0 || len(req.ReasonText) > 512 {
		writeErrorResponse(w, r, "INVALID_REASON_TEXT", "reason_text is required and must be <= 512 chars", http.StatusBadRequest)
		return
	}

	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}
	if s.config.ControlMutationRequireTenant && strings.TrimSpace(tenantScope) == "" {
		s.controlMutationBlockedTenantScope.Add(1)
		writeErrorResponse(w, r, "TENANT_SCOPE_REQUIRED", "tenant scope is required for mutation endpoints", http.StatusBadRequest)
		return
	}

	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.controlMutationTimeout())
	defer cancel()

	actor := s.resolveMutationActor(r)
	requestID := getRequestID(r.Context())
	commandID := generateCommandID()
	workflowID := resolveWorkflowID(r, req.WorkflowID)
	if gateErr := s.enforceMutationThrottle(actor, action+"|"+policyID); gateErr != nil {
		writeErrorResponse(w, r, gateErr.Code, gateErr.Message, gateErr.HTTPStatus)
		return
	}

	var result decisionResult
	for attempt := 0; attempt < 3; attempt++ {
		result, err = s.executePolicyDecision(ctx, db, policyID, action, actor, tenantScope, idemKey, requestID, commandID, workflowID, req)
		if err == nil {
			if s.outboxStats != nil {
				s.outboxStats.NotifyPolicyOutboxDispatcher()
			}
			if s.audit != nil {
				s.audit.Log("policy.decision", utils.AuditInfo, map[string]interface{}{
					"request_id":      requestID,
					"command_id":      commandID,
					"workflow_id":     workflowID,
					"actor":           actor,
					"action_type":     action,
					"policy_id":       policyID,
					"source_outbox":   result.sourceRowID,
					"created_outbox":  result.createdRow.ID,
					"reason_code":     req.ReasonCode,
					"tenant_scope":    tenantScope,
					"idempotency_key": idemKey,
					"classification":  req.Classification,
				})
			}
			writeJSONResponse(w, r, NewSuccessResponse(result.resp), http.StatusOK)
			return
		}
		if apiErr, ok := err.(*apiError); ok {
			writeErrorResponse(w, r, apiErr.Code, apiErr.Message, apiErr.HTTPStatus)
			return
		}
		if !isRetryableDecisionMutationErr(err) || attempt == 2 {
			writeErrorResponse(w, r, "MUTATION_TX_COMMIT_FAILED", "failed to commit mutation transaction", http.StatusInternalServerError)
			return
		}
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
}

type decisionResult struct {
	resp        controlMutationResponse
	sourceRowID string
	createdRow  controlOutboxRow
}

type apiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func (s *Server) executePolicyDecision(ctx context.Context, db *sql.DB, policyID, action, actor, tenantScope, idemKey, requestID, commandID, workflowID string, req controlMutationRequest) (decisionResult, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return decisionResult{}, &apiError{Code: "MUTATION_TX_BEGIN_FAILED", Message: "failed to open mutation transaction", HTTPStatus: http.StatusInternalServerError}
	}
	defer tx.Rollback()

	row, err := s.loadLatestPolicyOutboxRow(ctx, tx, policyID)
	if err == sql.ErrNoRows {
		return decisionResult{}, &apiError{Code: "POLICY_NOT_FOUND", Message: "policy not found", HTTPStatus: http.StatusNotFound}
	}
	if err != nil {
		return decisionResult{}, &apiError{Code: "POLICY_LOOKUP_FAILED", Message: "failed to resolve policy mutation target", HTTPStatus: http.StatusInternalServerError}
	}
	if tenantScope != "" {
		visible, visErr := s.policyVisibleToTenant(ctx, tx, row, tenantScope)
		if visErr != nil {
			return decisionResult{}, &apiError{Code: "TENANT_SCOPE_QUERY_FAILED", Message: "failed to verify tenant scope", HTTPStatus: http.StatusInternalServerError}
		}
		if !visible {
			return decisionResult{}, &apiError{Code: "TENANT_SCOPE_FORBIDDEN", Message: "requested policy is outside tenant scope", HTTPStatus: http.StatusForbidden}
		}
	}
	if strings.EqualFold(extractPolicyPayloadAction(row.Payload), "remove") {
		return decisionResult{}, &apiError{Code: "INVALID_STATE_TRANSITION", Message: "policy already represents a revoke action", HTTPStatus: http.StatusConflict}
	}
	if policyDecisionRecorded(row.Payload) {
		return decisionResult{}, &apiError{Code: "INVALID_STATE_TRANSITION", Message: "policy decision already recorded", HTTPStatus: http.StatusConflict}
	}
	if !policyRequiresApproval(row.Payload) {
		return decisionResult{}, &apiError{Code: "INVALID_STATE_TRANSITION", Message: "policy is not awaiting approval", HTTPStatus: http.StatusConflict}
	}

	if replay, replayErr := s.tryPolicyDecisionReplay(ctx, tx, action, idemKey, actor, row.ID); replayErr != nil {
		return decisionResult{}, &apiError{Code: "MUTATION_REPLAY_LOOKUP_FAILED", Message: "failed to check idempotency replay", HTTPStatus: http.StatusInternalServerError}
	} else if replay != nil {
		return decisionResult{resp: *replay}, nil
	}

	latestHeight := row.BlockHeight
	if s.stateStore != nil {
		if current := int64(s.stateStore.Latest()); current > latestHeight {
			latestHeight = current
		}
	}
	updated, err := insertPolicyDecisionOutboxRow(ctx, tx, latestHeight, row, requestID, commandID, workflowID, action, req)
	if err != nil {
		return decisionResult{}, err
	}
	if err := policystate.Refresh(ctx, db, tx, updated.PolicyID); err != nil {
		return decisionResult{}, &apiError{Code: "POLICY_STATE_REFRESH_FAILED", Message: "failed to refresh policy state projection", HTTPStatus: http.StatusInternalServerError}
	}

	targetKind := "outbox_" + action
	actionID, err := insertControlActionJournal(ctx, tx, controlActionJournalInsert{
		ActionType:       action,
		TargetKind:       targetKind,
		OutboxID:         row.ID,
		LeaseKey:         updated.ID,
		WorkflowID:       workflowID,
		PolicyID:         row.PolicyID,
		Actor:            actor,
		ReasonCode:       req.ReasonCode,
		ReasonText:       req.ReasonText,
		IdempotencyKey:   idemKey,
		RequestID:        requestID,
		BeforeStatus:     row.Status,
		AfterStatus:      updated.Status,
		TenantScope:      tenantScope,
		Classification:   req.Classification,
		BeforeLeaseEpoch: row.LeaseEpoch,
		AfterLeaseEpoch:  updated.LeaseEpoch,
	})
	if err != nil {
		if isUniqueConstraintErr(err) {
			existing, lookupErr := lookupControlActionByIdempotency(ctx, db, action, idemKey, actor)
			if lookupErr != nil {
				return decisionResult{}, &apiError{Code: "MUTATION_REPLAY_LOOKUP_FAILED", Message: "failed to check idempotency replay", HTTPStatus: http.StatusInternalServerError}
			}
			if existing != nil && (existing.TargetKind != targetKind || !existing.OutboxID.Valid || existing.OutboxID.String != row.ID) {
				return decisionResult{}, &apiError{Code: "IDEMPOTENCY_KEY_CONFLICT", Message: "idempotency key already used for a different mutation target", HTTPStatus: http.StatusConflict}
			}
			if replay, replayErr := s.tryPolicyDecisionReplay(ctx, db, action, idemKey, actor, row.ID); replayErr == nil && replay != nil {
				return decisionResult{resp: *replay}, nil
			}
		}
		return decisionResult{}, &apiError{Code: "ACTION_JOURNAL_WRITE_FAILED", Message: "failed to persist mutation audit", HTTPStatus: http.StatusInternalServerError}
	}

	if err := tx.Commit(); err != nil {
		return decisionResult{}, err
	}

	return decisionResult{
		resp: controlMutationResponse{
			ActionID:         actionID,
			CommandID:        commandID,
			WorkflowID:       workflowID,
			ActionType:       action,
			IdempotentReplay: false,
			Outbox:           outboxRowToDTO(updated),
		},
		sourceRowID: row.ID,
		createdRow:  updated,
	}, nil
}

func isRetryableDecisionMutationErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "40001") || strings.Contains(msg, "serialization") || strings.Contains(msg, "restart transaction") {
		return true
	}
	return strings.Contains(msg, "uq_control_policy_outbox_tx") || strings.Contains(msg, "duplicate key")
}

// handlePolicyAcks handles:
// - GET /policies/acks?policy_id=<uuid>
// - GET /policies/acks/<uuid>
func (s *Server) handlePolicyAcks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	policyID := strings.TrimSpace(r.URL.Query().Get("policy_id"))
	if policyID == "" {
		prefix := strings.TrimSuffix(s.config.BasePath, "/") + "/policies/acks/"
		if strings.HasPrefix(r.URL.Path, prefix) {
			policyID = strings.TrimSpace(strings.TrimPrefix(r.URL.Path, prefix))
			policyID = strings.Trim(policyID, "/")
		}
	}
	if policyID == "" {
		writeErrorResponse(w, r, "INVALID_POLICY_ID", "policy_id required", http.StatusBadRequest)
		return
	}

	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	schemaCtx, schemaCancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	schema, _ := loadControlSchemaSupport(schemaCtx, db)
	schemaCancel()

	rows, err := db.QueryContext(ctx, `
		SELECT
			pa.policy_id, `+controlAckEventIDSelectExpr(schema)+`, `+controlAckRequestIDSelectExpr(schema)+`, `+controlAckCommandIDSelectExpr(schema)+`, `+controlAckWorkflowIDSelectExpr(schema)+`, pa.controller_instance,
			pa.scope_identifier, pa.tenant, pa.region,
			pa.result, pa.reason, pa.error_code,
			pa.applied_at, pa.acked_at,
			pa.qc_reference, `+controlAckTraceSelectExpr(schema)+`, `+controlAckSourceEventSelectExpr(schema)+`, `+controlAckSentinelEventSelectExpr(schema)+`, pa.fast_path,
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
		WHERE pa.policy_id = $1
		ORDER BY pa.acked_at DESC NULLS LAST, pa.observed_at DESC
		LIMIT 100
	`, policyID)
	if err != nil {
		s.recordAPIRequest(http.StatusInternalServerError)
		if s.logger != nil {
			s.logger.WarnContext(ctx, "policy acks query failed", utils.ZapString("policy_id", policyID), utils.ZapError(err))
		}
		writeErrorResponse(w, r, "ACKS_QUERY_FAILED", "failed to query policy acks", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	acks := make([]policyAckPayload, 0)
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
			writeErrorResponse(w, r, "ACKS_SCAN_FAILED", "failed to read policy ack row", http.StatusInternalServerError)
			return
		}
		acks = append(acks, policyAckToPayload(row))
	}
	if err := rows.Err(); err != nil {
		writeErrorResponse(w, r, "ACKS_ITERATION_FAILED", "failed to iterate policy acks", http.StatusInternalServerError)
		return
	}

	resp := policyAckResponse{
		PolicyID: policyID,
		Count:    len(acks),
		Acks:     acks,
	}

	writeJSONResponse(w, r, NewSuccessResponse(resp), http.StatusOK)
}

func (s *Server) loadPolicyTraceAcks(ctx context.Context, db *sql.DB, schema controlSchemaSupport, policyID, tenantScope string) ([]policyAckRow, []policyAckPayload, error) {
	ackWhere := "policy_id = $1"
	ackArgs := []interface{}{policyID}
	if tenantScope != "" {
		ackWhere += fmt.Sprintf(" AND tenant = $%d", len(ackArgs)+1)
		ackArgs = append(ackArgs, tenantScope)
	}
	for _, tableName := range []string{"policy_ack_events", "policy_acks"} {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`
			SELECT
				policy_id, %s, %s, %s, %s, controller_instance,
				scope_identifier, tenant, region,
				result, reason, error_code,
				applied_at, acked_at,
				qc_reference, %s, %s, %s, fast_path,
				rule_hash, producer_id,
				observed_at
			FROM %s
			WHERE %s
			ORDER BY acked_at DESC NULLS LAST, observed_at DESC
			LIMIT 200
		`, controlAckEventIDSelectExpr(schema), controlAckRequestIDSelectExpr(schema), controlAckCommandIDSelectExpr(schema), controlAckWorkflowIDSelectExpr(schema), controlAckTraceSelectExpr(schema), controlAckSourceEventSelectExpr(schema), controlAckSentinelEventSelectExpr(schema), tableName, ackWhere), ackArgs...)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()

		ackRowsRaw := make([]policyAckRow, 0, 32)
		acks := make([]policyAckPayload, 0, 32)
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
			); scanErr != nil {
				return nil, nil, scanErr
			}
			ackRowsRaw = append(ackRowsRaw, row)
			acks = append(acks, policyAckToPayload(row))
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		if len(ackRowsRaw) > 0 {
			return ackRowsRaw, acks, nil
		}
	}
	return nil, nil, nil
}

func (s *Server) loadPolicyTraceDetail(parent context.Context, policyID, tenantScope string) (controlTraceResponse, error) {
	db, err := s.getDB()
	if err != nil {
		return controlTraceResponse{}, err
	}
	schemaCtx, schemaCancel := context.WithTimeout(parent, s.config.RequestTimeout)
	schema, _ := loadControlSchemaSupport(schemaCtx, db)
	schemaCancel()

	ctx, cancel := context.WithTimeout(parent, s.controlTraceTimeout())
	defer cancel()

	outboxWhere := "policy_id = $1"
	outboxArgs := []interface{}{policyID}
	if tenantScope != "" {
		tenantArg := len(outboxArgs) + 1
		outboxWhere += fmt.Sprintf(" AND (EXISTS (SELECT 1 FROM policy_acks pa WHERE pa.policy_id = control_policy_outbox.policy_id AND pa.tenant = $%d) OR %s = $%d)", tenantArg, outboxPayloadStringExpr("{tenant}", "{metadata,tenant}", "{trace,tenant}", "{target,tenant}", "{target,tenant_id}"), tenantArg)
		outboxArgs = append(outboxArgs, tenantScope)
	}

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
		return controlTraceResponse{}, err
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
			return controlTraceResponse{}, scanErr
		}
		outboxRowsRaw = append(outboxRowsRaw, row)
		outbox = append(outbox, outboxRowToDTO(row))
	}
	if err := outboxRows.Err(); err != nil {
		return controlTraceResponse{}, err
	}

	ackRowsRaw, acks, err := s.loadPolicyTraceAcks(ctx, db, schema, policyID, tenantScope)
	if err != nil {
		return controlTraceResponse{}, err
	}

	runtimeMarkers := make([]runtimeTraceMarkerDTO, 0)
	if s.traceStats != nil {
		for _, marker := range filterScopedRuntimeMarkers(s.traceStats.GetPolicyRuntimeTrace(policyID), outbox, acks, tenantScope != "") {
			runtimeMarkers = append(runtimeMarkers, runtimeTraceMarkerToDTO(marker))
		}
	}

	materialized := materializeControlTrace(outboxRowsRaw, ackRowsRaw, runtimeMarkers)
	traceID, sourceEventID, sentinelEventID := materializedTraceLineage(outboxRowsRaw, ackRowsRaw, materialized)
	return controlTraceResponse{
		PolicyID:        policyID,
		TraceID:         traceID,
		SourceEventID:   sourceEventID,
		SentinelEventID: sentinelEventID,
		Outbox:          outbox,
		Acks:            acks,
		RuntimeMarkers:  runtimeMarkers,
		Materialized:    materialized,
	}, nil
}

func buildPolicySummary(trace controlTraceResponse) policySummaryDTO {
	summary := policySummaryDTO{
		PolicyID: trace.PolicyID,
	}
	if len(trace.Outbox) > 0 {
		row := trace.Outbox[0]
		summary.RequestID = row.RequestID
		summary.CommandID = row.CommandID
		summary.WorkflowID = row.WorkflowID
		summary.AnomalyID = row.AnomalyID
		summary.FlowID = row.FlowID
		summary.SourceID = row.SourceID
		summary.SourceType = row.SourceType
		summary.SensorID = row.SensorID
		summary.ValidatorID = row.ValidatorID
		summary.ScopeIdentifier = row.ScopeIdentifier
		summary.TraceID = row.TraceID
		summary.SourceEventID = row.SourceEventID
		summary.SentinelEventID = row.SentinelEventID
		summary.LatestOutboxID = row.ID
		summary.LatestStatus = row.Status
		summary.LatestCreatedAt = row.CreatedAt
		summary.LatestPublishedAt = row.PublishedAt
		summary.LatestAckedAt = row.AckedAt
		summary.LatestAckResult = row.AckResult
		summary.OutboxCount = int64(len(trace.Outbox))
	}
	if len(trace.Acks) > 0 {
		ack := trace.Acks[0]
		if summary.RequestID == "" {
			summary.RequestID = ack.RequestID
		}
		if summary.CommandID == "" {
			summary.CommandID = ack.CommandID
		}
		if summary.WorkflowID == "" {
			summary.WorkflowID = ack.WorkflowID
		}
		if summary.TraceID == "" {
			summary.TraceID = ack.TraceID
		}
		if summary.SourceEventID == "" {
			summary.SourceEventID = ack.SourceEventID
		}
		if summary.SentinelEventID == "" {
			summary.SentinelEventID = ack.SentinelEventID
		}
		if summary.ScopeIdentifier == "" {
			summary.ScopeIdentifier = ack.ScopeIdentifier
		}
		summary.LatestAckEventID = ack.AckEventID
		if summary.LatestAckResult == "" {
			summary.LatestAckResult = ack.Result
		}
		summary.LatestAckController = ack.ControllerInstance
		if summary.LatestAckedAt == 0 {
			summary.LatestAckedAt = ack.AckedAt
		}
		summary.AckCount = int64(len(trace.Acks))
	}
	if summary.TraceID == "" {
		summary.TraceID = trace.TraceID
	}
	if summary.SourceEventID == "" {
		summary.SourceEventID = trace.SourceEventID
	}
	if summary.SentinelEventID == "" {
		summary.SentinelEventID = trace.SentinelEventID
	}
	return summary
}

func buildPolicyCoverage(trace controlTraceResponse) policyCoverageResponse {
	coverage := policyCoverageResponse{PolicyID: trace.PolicyID}
	for _, row := range trace.Outbox {
		coverage.OutboxCount++
		switch row.Status {
		case "pending":
			coverage.PendingCount++
		case "publishing":
			coverage.PublishingCount++
		case "published":
			coverage.PublishedCount++
			coverage.PublishedOrAckedCount++
		case "retry":
			coverage.RetryCount++
		case "terminal_failed":
			coverage.TerminalFailedCount++
		case "acked":
			coverage.AckedCount++
			coverage.PublishedOrAckedCount++
		}
	}
	coverage.AckHistoryCount = int64(len(trace.Acks))
	if len(trace.Acks) > 0 {
		ack := trace.Acks[0]
		coverage.LatestAckEventID = ack.AckEventID
		coverage.LatestAckResult = ack.Result
		coverage.LatestAckController = ack.ControllerInstance
		if ack.AckedAt > 0 {
			coverage.LatestAckedAt = ack.AckedAt
		} else {
			coverage.LatestAckedAt = ack.ObservedAt
		}
	}
	if coverage.OutboxCount > 0 {
		coverage.PublishCoverageRatio = float64(coverage.PublishedOrAckedCount) / float64(coverage.OutboxCount)
		coverage.AckCoverageRatio = float64(coverage.AckedCount) / float64(coverage.OutboxCount)
	}
	return coverage
}

func policyListRowToDTO(row policyListRow) policySummaryDTO {
	dto := policySummaryDTO{
		PolicyID:            row.PolicyID,
		AnomalyID:           strings.TrimSpace(row.AnomalyID),
		FlowID:              strings.TrimSpace(row.FlowID),
		SourceID:            strings.TrimSpace(row.SourceID),
		SourceType:          strings.TrimSpace(row.SourceType),
		SensorID:            strings.TrimSpace(row.SensorID),
		ValidatorID:         strings.TrimSpace(row.ValidatorID),
		ScopeIdentifier:     strings.TrimSpace(row.ScopeIdentifier),
		LatestOutboxID:      row.LatestOutboxID,
		LatestStatus:        row.LatestStatus,
		LatestCreatedAt:     row.LatestCreatedAt.UTC().Unix(),
		OutboxCount:         row.OutboxCount,
		AckCount:            row.AckCount,
		LatestAckController: strings.TrimSpace(row.LatestAckController.String),
		LatestAckResult:     strings.TrimSpace(row.LatestAckResult.String),
	}
	if row.RequestID.Valid {
		dto.RequestID = row.RequestID.String
	}
	if row.CommandID.Valid {
		dto.CommandID = row.CommandID.String
	}
	if row.WorkflowID.Valid {
		dto.WorkflowID = row.WorkflowID.String
	}
	if row.TraceID.Valid {
		dto.TraceID = row.TraceID.String
	}
	if row.SourceEventID.Valid {
		dto.SourceEventID = row.SourceEventID.String
	}
	if row.SentinelEventID.Valid {
		dto.SentinelEventID = row.SentinelEventID.String
	}
	if !row.TraceID.Valid && row.LatestAckTraceID.Valid {
		dto.TraceID = row.LatestAckTraceID.String
	}
	if !row.SourceEventID.Valid && row.LatestAckSourceEvent.Valid {
		dto.SourceEventID = row.LatestAckSourceEvent.String
	}
	if !row.SentinelEventID.Valid && row.LatestAckSentinel.Valid {
		dto.SentinelEventID = row.LatestAckSentinel.String
	}
	if row.LatestPublishedAt.Valid {
		dto.LatestPublishedAt = row.LatestPublishedAt.Time.UTC().Unix()
	}
	if row.LatestAckedAt.Valid {
		dto.LatestAckedAt = row.LatestAckedAt.Time.UTC().Unix()
	}
	if row.LatestAckEventID.Valid {
		dto.LatestAckEventID = row.LatestAckEventID.String
	}
	return dto
}

func extractPolicyRoute(basePath, urlPath string) (policyID string, coverage bool, ok bool) {
	base := strings.TrimSuffix(basePath, "/") + "/policies/"
	if !strings.HasPrefix(urlPath, base) {
		return "", false, false
	}
	rest := strings.Trim(strings.TrimPrefix(urlPath, base), "/")
	if rest == "" || strings.HasPrefix(rest, "acks") {
		return "", false, false
	}
	if strings.HasSuffix(rest, "/coverage") {
		return strings.TrimSuffix(rest, "/coverage"), true, true
	}
	return rest, false, true
}

func extractPolicyMutationRoute(basePath, urlPath, suffix string) (policyID string, ok bool) {
	base := strings.TrimSuffix(basePath, "/") + "/policies/"
	if !strings.HasPrefix(urlPath, base) {
		return "", false
	}
	rest := strings.Trim(strings.TrimPrefix(urlPath, base), "/")
	if rest == "" || strings.HasPrefix(rest, "acks") || !strings.HasSuffix(rest, suffix) {
		return "", false
	}
	policyID = strings.TrimSuffix(rest, suffix)
	policyID = strings.TrimSpace(strings.Trim(policyID, "/"))
	if policyID == "" || strings.Contains(policyID, "/") {
		return "", false
	}
	return policyID, true
}

func hasPolicyMutationSuffix(basePath, urlPath, suffix string) bool {
	_, ok := extractPolicyMutationRoute(basePath, urlPath, suffix)
	return ok
}

func encodePolicyCursor(createdAt time.Time, policyID string) string {
	return encodeOutboxCursor(createdAt, policyID)
}

func decodePolicyCursor(cursor string) (time.Time, string, error) {
	return decodeOutboxCursor(cursor)
}

func ackTenantWhereClause(tenantScope string) string {
	if tenantScope == "" {
		return ""
	}
	return " AND tenant = $1"
}

func (s *Server) loadLatestPolicyOutboxRow(ctx context.Context, query rowQueryer, policyID string) (controlOutboxRow, error) {
	var row controlOutboxRow
	err := query.QueryRowContext(ctx, `
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			request_id, command_id, workflow_id, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, sentinel_event_id, payload,
			status, retries, next_retry_at, last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at, ack_result, ack_reason, ack_controller,
			acked_at, created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE policy_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, policyID).Scan(
		&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
		&row.RequestID, &row.CommandID, &row.WorkflowID, &row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Payload,
		&row.Status, &row.Retries, &row.NextRetryAt, &row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
		&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt, &row.AckResult, &row.AckReason, &row.AckController,
		&row.AckedAt, &row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
	)
	return row, err
}

func (s *Server) policyVisibleToTenant(ctx context.Context, query rowQueryer, row controlOutboxRow, tenantScope string) (bool, error) {
	if strings.TrimSpace(tenantScope) == "" {
		return true, nil
	}
	var visible bool
	err := query.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM policy_acks pa
			WHERE pa.policy_id = $1 AND pa.tenant = $2
		)
	`, row.PolicyID, tenantScope).Scan(&visible)
	if err != nil {
		return false, err
	}
	if visible {
		return true, nil
	}
	return tenantScopeMatchesOutboxRow(row, tenantScope), nil
}

func extractPolicyPayloadAction(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(asString(payload["action"])))
}

func policyRequiresApproval(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	guardrails, _ := payload["guardrails"].(map[string]any)
	if guardrails == nil {
		return false
	}
	switch v := guardrails["approval_required"].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
	default:
		return false
	}
}

func extractPolicyPayloadControlAction(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	for _, candidate := range []string{
		asString(payload["control_action"]),
		extractMapString(extractMap(payload["metadata"]), "control_action"),
		extractMapString(extractMap(payload["trace"]), "control_action"),
	} {
		if value := strings.ToLower(strings.TrimSpace(candidate)); value != "" {
			return value
		}
	}
	return ""
}

func extractMap(v any) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func policyDecisionRecorded(raw []byte) bool {
	switch extractPolicyPayloadControlAction(raw) {
	case "approve", "reject":
		return true
	default:
		return false
	}
}

func buildPolicyDecisionPayload(source controlOutboxRow, now time.Time, requestID, commandID, workflowID, controlAction string, req controlMutationRequest) ([]byte, string, error) {
	if len(source.Payload) == 0 {
		return nil, "", fmt.Errorf("source outbox payload is empty")
	}
	var raw map[string]any
	if err := json.Unmarshal(source.Payload, &raw); err != nil {
		return nil, "", fmt.Errorf("decode source payload: %w", err)
	}
	decisionTraceID := fmt.Sprintf("trace:%s:%s:%d", controlAction, source.PolicyID, now.UnixMilli())
	raw["control_action"] = controlAction
	if strings.TrimSpace(requestID) != "" {
		raw["request_id"] = strings.TrimSpace(requestID)
	}
	if strings.TrimSpace(commandID) != "" {
		raw["command_id"] = strings.TrimSpace(commandID)
	}
	if strings.TrimSpace(workflowID) != "" {
		raw["workflow_id"] = strings.TrimSpace(workflowID)
	}
	if strings.TrimSpace(source.AnomalyID) != "" && extractMapString(raw, "anomaly_id") == "" {
		raw["anomaly_id"] = strings.TrimSpace(source.AnomalyID)
	}
	if source.SourceEventID.Valid && strings.TrimSpace(source.SourceEventID.String) != "" && extractMapString(raw, "source_event_id") == "" {
		raw["source_event_id"] = strings.TrimSpace(source.SourceEventID.String)
	}
	if source.SourceEventTsMs.Valid && source.SourceEventTsMs.Int64 > 0 {
		if _, ok := raw["source_event_ts_ms"]; !ok {
			raw["source_event_ts_ms"] = source.SourceEventTsMs.Int64
		}
	}
	if source.SentinelEventID.Valid && strings.TrimSpace(source.SentinelEventID.String) != "" {
		raw["sentinel_event_id"] = strings.TrimSpace(source.SentinelEventID.String)
	}

	metadata, _ := raw["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
		raw["metadata"] = metadata
	}
	metadata["control_action"] = controlAction
	metadata["decision_reason_code"] = req.ReasonCode
	metadata["decision_reason_text"] = req.ReasonText
	metadata["trace_id"] = decisionTraceID
	if source.TraceID.Valid && strings.TrimSpace(source.TraceID.String) != "" {
		metadata["parent_trace_id"] = strings.TrimSpace(source.TraceID.String)
	}
	if strings.TrimSpace(requestID) != "" {
		metadata["request_id"] = strings.TrimSpace(requestID)
	}
	if strings.TrimSpace(commandID) != "" {
		metadata["command_id"] = strings.TrimSpace(commandID)
	}
	if strings.TrimSpace(workflowID) != "" {
		metadata["workflow_id"] = strings.TrimSpace(workflowID)
	}
	if strings.TrimSpace(source.AnomalyID) != "" && extractMapString(metadata, "anomaly_id") == "" {
		metadata["anomaly_id"] = strings.TrimSpace(source.AnomalyID)
	}
	if source.SourceEventID.Valid && strings.TrimSpace(source.SourceEventID.String) != "" && extractMapString(metadata, "source_event_id") == "" {
		metadata["source_event_id"] = strings.TrimSpace(source.SourceEventID.String)
	}
	if source.SourceEventTsMs.Valid && source.SourceEventTsMs.Int64 > 0 {
		if _, ok := metadata["source_event_ts_ms"]; !ok {
			metadata["source_event_ts_ms"] = source.SourceEventTsMs.Int64
		}
	}
	if source.SentinelEventID.Valid && strings.TrimSpace(source.SentinelEventID.String) != "" {
		metadata["sentinel_event_id"] = strings.TrimSpace(source.SentinelEventID.String)
	}

	trace, _ := raw["trace"].(map[string]any)
	if trace == nil {
		trace = map[string]any{}
		raw["trace"] = trace
	}
	trace["id"] = decisionTraceID
	trace["control_action"] = controlAction
	if strings.TrimSpace(requestID) != "" {
		trace["request_id"] = strings.TrimSpace(requestID)
	}
	if strings.TrimSpace(commandID) != "" {
		trace["command_id"] = strings.TrimSpace(commandID)
	}
	if strings.TrimSpace(workflowID) != "" {
		trace["workflow_id"] = strings.TrimSpace(workflowID)
	}

	auditMap, _ := raw["audit"].(map[string]any)
	if auditMap == nil {
		auditMap = map[string]any{}
		raw["audit"] = auditMap
	}
	auditMap["reason_code"] = req.ReasonCode

	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, "", fmt.Errorf("encode decision payload: %w", err)
	}
	return encoded, decisionTraceID, nil
}

func insertPolicyDecisionOutboxRow(ctx context.Context, tx *sql.Tx, latestHeight int64, source controlOutboxRow, requestID, commandID, workflowID, controlAction string, req controlMutationRequest) (controlOutboxRow, error) {
	now := time.Now().UTC()
	if anomalyID, resolveErr := resolveOutboxAnomalyID(ctx, tx, source); resolveErr != nil {
		return controlOutboxRow{}, fmt.Errorf("resolve policy decision anomaly lineage: %w", resolveErr)
	} else if anomalyID != "" {
		source.AnomalyID = anomalyID
	}
	payload, traceID, err := buildPolicyDecisionPayload(source, now, requestID, commandID, workflowID, controlAction, req)
	if err != nil {
		return controlOutboxRow{}, err
	}
	var txIndex int
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(tx_index), 0) - 1
		FROM control_policy_outbox
		WHERE block_height = $1
	`, latestHeight).Scan(&txIndex)
	if err != nil {
		return controlOutboxRow{}, fmt.Errorf("allocate decision tx_index: %w", err)
	}
	ruleHash := sha256.Sum256(payload)

	var outboxID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO control_policy_outbox (
			block_height, block_ts, tx_index, policy_id, rule_hash, payload,
			request_id, command_id, workflow_id, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, sentinel_event_id,
			status, next_retry_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10, $11, NULLIF($12, ''), NULLIF($13, 0), NULLIF($14, ''),
			'pending', NOW(), NOW(), NOW()
		)
		RETURNING id::STRING
	`, latestHeight, now.Unix(), txIndex, source.PolicyID, ruleHash[:], payload,
		requestID, commandID, workflowID, traceID, now.UnixMilli(), source.SourceEventID.String, source.SourceEventTsMs.Int64, source.SentinelEventID.String,
	).Scan(&outboxID)
	if err != nil {
		return controlOutboxRow{}, fmt.Errorf("insert policy decision outbox row: %w", err)
	}
	return loadOutboxRowByID(ctx, tx, outboxID)
}

func (s *Server) tryPolicyDecisionReplay(ctx context.Context, query rowQueryer, action, idempotencyKey, actor, sourceOutboxID string) (*controlMutationResponse, error) {
	var (
		actionID      string
		createdOutbox sql.NullString
	)
	targetKind := "outbox_" + action
	err := query.QueryRowContext(ctx, `
		SELECT action_id::STRING, lease_key
		FROM control_actions_journal
		WHERE action_type = $1
		  AND idempotency_key = $2
		  AND actor = $3
		  AND target_kind = $4
		  AND outbox_id = $5::UUID
		ORDER BY created_at DESC
		LIMIT 1
	`, action, idempotencyKey, actor, targetKind, sourceOutboxID).Scan(&actionID, &createdOutbox)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !createdOutbox.Valid || strings.TrimSpace(createdOutbox.String) == "" {
		return nil, fmt.Errorf("%s replay missing created outbox id", action)
	}
	row, err := loadOutboxRowByID(ctx, query, strings.TrimSpace(createdOutbox.String))
	if err != nil {
		return nil, err
	}
	return &controlMutationResponse{
		ActionID:         actionID,
		ActionType:       action,
		IdempotentReplay: true,
		Outbox:           outboxRowToDTO(row),
	}, nil
}
