package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"backend/pkg/control/policystate"
	"backend/pkg/utils"
)

type workflowSummaryDTO struct {
	WorkflowID          string `json:"workflow_id"`
	RequestID           string `json:"request_id,omitempty"`
	CommandID           string `json:"command_id,omitempty"`
	LatestPolicyID      string `json:"latest_policy_id,omitempty"`
	LatestOutboxID      string `json:"latest_outbox_id,omitempty"`
	LatestStatus        string `json:"latest_status,omitempty"`
	LatestTraceID       string `json:"latest_trace_id,omitempty"`
	LatestCreatedAt     int64  `json:"latest_created_at,omitempty"`
	LatestPublishedAt   int64  `json:"latest_published_at,omitempty"`
	LatestAckedAt       int64  `json:"latest_acked_at,omitempty"`
	LatestAckEventID    string `json:"latest_ack_event_id,omitempty"`
	LatestAckResult     string `json:"latest_ack_result,omitempty"`
	LatestAckController string `json:"latest_ack_controller,omitempty"`
	PolicyCount         int64  `json:"policy_count,omitempty"`
	OutboxCount         int64  `json:"outbox_count,omitempty"`
	AckCount            int64  `json:"ack_count,omitempty"`
}

type workflowListResponse struct {
	Rows       []workflowSummaryDTO `json:"rows"`
	Pagination controlListMeta      `json:"pagination"`
}

type workflowDetailResponse struct {
	Summary  workflowSummaryDTO    `json:"summary"`
	Policies []policySummaryDTO    `json:"policies"`
	Acks     []policyAckPayload    `json:"acks,omitempty"`
	Outbox   []controlOutboxRowDTO `json:"outbox,omitempty"`
}

type workflowRollbackResponse struct {
	ActionID         string                `json:"action_id"`
	CommandID        string                `json:"command_id,omitempty"`
	WorkflowID       string                `json:"workflow_id"`
	ActionType       string                `json:"action_type"`
	IdempotentReplay bool                  `json:"idempotent_replay"`
	AffectedPolicies int                   `json:"affected_policies"`
	Outbox           []controlOutboxRowDTO `json:"outbox"`
}

type workflowListRow struct {
	WorkflowID          string
	RequestID           sql.NullString
	CommandID           sql.NullString
	LatestPolicyID      string
	LatestOutboxID      string
	LatestStatus        string
	LatestTraceID       sql.NullString
	LatestCreatedAt     time.Time
	LatestPublishedAt   sql.NullTime
	LatestAckedAt       sql.NullTime
	LatestAckEventID    sql.NullString
	LatestAckResult     sql.NullString
	LatestAckController sql.NullString
	PolicyCount         int64
	OutboxCount         int64
	AckCount            int64
}

func (s *Server) handleWorkflowsList(w http.ResponseWriter, r *http.Request) {
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
	schema, _ := loadControlSchemaSupport(ctx, db)
	if !requireControlSchemaColumn(w, r, schema.OutboxWorkflowID, "workflow_id", "024") {
		return
	}

	args := []interface{}{}
	where := []string{"workflow_id IS NOT NULL", "workflow_id <> ''"}
	where, args = appendAccessBoundOutboxFilter(where, args, tenantScope)
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		if !isValidOutboxStatus(status) {
			writeErrorResponse(w, r, "INVALID_STATUS", "status must be one of pending,publishing,published,retry,terminal_failed,acked", http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, status)
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("cursor")); cursor != "" {
		cursorAt, cursorWorkflowID, parseErr := decodeWorkflowCursor(cursor)
		if parseErr != nil {
			writeErrorResponse(w, r, "INVALID_CURSOR", parseErr.Error(), http.StatusBadRequest)
			return
		}
		where = append(where, fmt.Sprintf("(created_at, workflow_id) < ($%d, $%d)", len(args)+1, len(args)+2))
		args = append(args, cursorAt, cursorWorkflowID)
	}
	ackScopeArg := len(args) + 1
	ackScopeClause := accessBoundAckWhereClause(ackScopeArg, tenantScope)
	if tenantScope != "" {
		args = append(args, tenantScope)
	}
	args = append(args, limit+1)

	query := fmt.Sprintf(`
		WITH latest_outbox AS (
			SELECT * FROM (
				SELECT
					workflow_id,
					%s AS request_id,
					%s AS command_id,
					policy_id,
					id::STRING AS outbox_id,
					status,
					trace_id,
					created_at,
					published_at,
					acked_at,
					ROW_NUMBER() OVER (PARTITION BY workflow_id ORDER BY created_at DESC, id DESC) AS rn
				FROM control_policy_outbox
				WHERE %s
			) ranked
			WHERE rn = 1
		),
		workflow_policy_counts AS (
			SELECT workflow_id, COUNT(DISTINCT policy_id) AS policy_count, COUNT(*) AS outbox_count
			FROM control_policy_outbox
			WHERE workflow_id IN (SELECT workflow_id FROM latest_outbox)
			GROUP BY workflow_id
		),
		latest_ack AS (
			SELECT * FROM (
				SELECT
					workflow_id, %s, controller_instance, result, acked_at,
					ROW_NUMBER() OVER (PARTITION BY workflow_id ORDER BY acked_at DESC NULLS LAST, observed_at DESC, controller_instance ASC) AS rn
				FROM policy_acks
				WHERE workflow_id IN (SELECT workflow_id FROM latest_outbox)%s
			) ranked
			WHERE rn = 1
		),
		ack_counts AS (
			SELECT workflow_id, COUNT(*) AS ack_count
			FROM policy_ack_events
			WHERE workflow_id IN (SELECT workflow_id FROM latest_outbox)%s
			GROUP BY workflow_id
		)
		SELECT
			lo.workflow_id,
			lo.request_id, lo.command_id,
			lo.policy_id, lo.outbox_id, lo.status, lo.trace_id, lo.created_at, lo.published_at, lo.acked_at,
			la.ack_event_id, la.result, la.controller_instance,
			COALESCE(wpc.policy_count, 0),
			COALESCE(wpc.outbox_count, 0),
			COALESCE(ac.ack_count, 0)
		FROM latest_outbox lo
		LEFT JOIN workflow_policy_counts wpc ON wpc.workflow_id = lo.workflow_id
		LEFT JOIN latest_ack la ON la.workflow_id = lo.workflow_id
		LEFT JOIN ack_counts ac ON ac.workflow_id = lo.workflow_id
		ORDER BY lo.created_at DESC, lo.workflow_id DESC
		LIMIT $%d
	`, controlOutboxRequestIDSelectExpr(schema), controlOutboxCommandIDSelectExpr(schema), strings.Join(where, " AND "), controlAckEventIDSelectExpr(schema), ackScopeClause, ackScopeClause, len(args))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		writeErrorResponse(w, r, "WORKFLOWS_QUERY_FAILED", "failed to query workflows", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := make([]workflowSummaryDTO, 0, limit+1)
	for rows.Next() {
		var row workflowListRow
		if scanErr := rows.Scan(
			&row.WorkflowID, &row.RequestID, &row.CommandID,
			&row.LatestPolicyID, &row.LatestOutboxID, &row.LatestStatus, &row.LatestTraceID, &row.LatestCreatedAt, &row.LatestPublishedAt, &row.LatestAckedAt,
			&row.LatestAckEventID, &row.LatestAckResult, &row.LatestAckController,
			&row.PolicyCount, &row.OutboxCount, &row.AckCount,
		); scanErr != nil {
			writeErrorResponse(w, r, "WORKFLOWS_SCAN_FAILED", "failed to read workflow row", http.StatusInternalServerError)
			return
		}
		items = append(items, workflowListRowToDTO(row))
	}
	if err := rows.Err(); err != nil {
		writeErrorResponse(w, r, "WORKFLOWS_ITERATION_FAILED", "failed to iterate workflow rows", http.StatusInternalServerError)
		return
	}
	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		nextCursor = encodeWorkflowCursor(time.Unix(last.LatestCreatedAt, 0).UTC(), last.WorkflowID)
		items = items[:limit]
	}
	if err := overlayStateOnWorkflowSummaries(ctx, db, items); err != nil {
		writeErrorResponse(w, r, "POLICY_STATE_READ_FAILED", "failed to read workflow state projection", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, r, NewSuccessResponse(workflowListResponse{
		Rows:       items,
		Pagination: controlListMeta{Limit: limit, NextCursor: nextCursor},
	}), http.StatusOK)
}

func (s *Server) handleWorkflowsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if hasWorkflowMutationSuffix(strings.TrimSuffix(s.config.BasePath, "/"), r.URL.Path, ":rollback") {
			s.handleWorkflowRollback(w, r)
			return
		}
		writeErrorResponse(w, r, "INVALID_MUTATION_PATH", "path must end with :rollback", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET/POST methods allowed", http.StatusMethodNotAllowed)
		return
	}
	workflowID, ok := extractWorkflowRoute(strings.TrimSuffix(s.config.BasePath, "/"), r.URL.Path)
	if !ok || workflowID == "" {
		writeErrorResponse(w, r, "INVALID_WORKFLOW_ID", "workflow_id is required", http.StatusBadRequest)
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
	policies, err := s.loadWorkflowPolicySummaries(ctx, db, workflowID, tenantScope)
	if err != nil {
		writeErrorResponse(w, r, "WORKFLOW_LOOKUP_FAILED", "failed to load workflow detail", http.StatusInternalServerError)
		return
	}
	if len(policies) == 0 {
		writeErrorResponse(w, r, "WORKFLOW_NOT_FOUND", "workflow not found", http.StatusNotFound)
		return
	}
	if err := overlayStateOnPolicySummaries(ctx, db, policies); err != nil {
		writeErrorResponse(w, r, "POLICY_STATE_READ_FAILED", "failed to read workflow policy state projection", http.StatusInternalServerError)
		return
	}
	acks, outbox, err := s.loadWorkflowRecentEvents(ctx, db, workflowID, tenantScope)
	if err != nil {
		writeErrorResponse(w, r, "WORKFLOW_LOOKUP_FAILED", "failed to load workflow events", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, r, NewSuccessResponse(workflowDetailResponse{
		Summary:  buildWorkflowSummaryFromPolicies(workflowID, policies, acks),
		Policies: policies,
		Acks:     acks,
		Outbox:   outbox,
	}), http.StatusOK)
}

func (s *Server) handleWorkflowRollback(w http.ResponseWriter, r *http.Request) {
	workflowID, ok := extractWorkflowMutationRoute(strings.TrimSuffix(s.config.BasePath, "/"), r.URL.Path, ":rollback")
	if !ok || workflowID == "" {
		writeErrorResponse(w, r, "INVALID_WORKFLOW_ID", "workflow_id is required", http.StatusBadRequest)
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
	if req.WorkflowID != "" && req.WorkflowID != workflowID {
		writeErrorResponse(w, r, "INVALID_WORKFLOW_ID", "body workflow_id must match path workflow_id", http.StatusBadRequest)
		return
	}
	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.controlMutationTimeout())
	defer cancel()
	actor := s.resolveMutationActor(r, tenantScope)
	requestID := getRequestID(r.Context())
	commandID := generateCommandID()
	if gateErr := s.enforceMutationThrottle(actor, "rollback|"+workflowID); gateErr != nil {
		writeErrorResponse(w, r, gateErr.Code, gateErr.Message, gateErr.HTTPStatus)
		return
	}
	replay, replayErr := s.tryWorkflowRollbackReplay(ctx, db, idemKey, actor, workflowID)
	if replayErr != nil {
		writeErrorResponse(w, r, "MUTATION_REPLAY_LOOKUP_FAILED", "failed to check idempotency replay", http.StatusInternalServerError)
		return
	}
	if replay != nil {
		writeJSONResponse(w, r, NewSuccessResponse(*replay), http.StatusOK)
		return
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		writeErrorResponse(w, r, "MUTATION_TX_BEGIN_FAILED", "failed to open mutation transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	latestRows, err := s.loadWorkflowLatestOutboxRows(ctx, tx, workflowID, tenantScope)
	if err != nil {
		writeErrorResponse(w, r, "WORKFLOW_LOOKUP_FAILED", "failed to resolve workflow rollback targets", http.StatusInternalServerError)
		return
	}
	if len(latestRows) == 0 {
		writeErrorResponse(w, r, "WORKFLOW_NOT_FOUND", "workflow not found", http.StatusNotFound)
		return
	}
	candidates := make([]controlOutboxRow, 0, len(latestRows))
	for _, row := range latestRows {
		if strings.EqualFold(extractPolicyPayloadAction(row.Payload), "remove") {
			continue
		}
		if validateOutboxTransition("revoke", row.Status) != nil {
			continue
		}
		candidates = append(candidates, row)
	}
	if len(candidates) == 0 {
		writeErrorResponse(w, r, "INVALID_STATE_TRANSITION", "workflow has no mutable policies to rollback", http.StatusConflict)
		return
	}
	latestHeight := int64(0)
	for _, row := range candidates {
		if row.BlockHeight > latestHeight {
			latestHeight = row.BlockHeight
		}
	}
	if s.stateStore != nil {
		if current := int64(s.stateStore.Latest()); current > latestHeight {
			latestHeight = current
		}
	}
	created := make([]controlOutboxRowDTO, 0, len(candidates))
	for _, source := range candidates {
		updated, insertErr := insertRevokeOutboxRow(ctx, tx, latestHeight, source, requestID, commandID, workflowID)
		if insertErr != nil {
			writeErrorResponse(w, r, "WORKFLOW_ROLLBACK_FAILED", "failed to create rollback revoke row", http.StatusInternalServerError)
			return
		}
		if err := policystate.Refresh(ctx, db, tx, updated.PolicyID); err != nil {
			writeErrorResponse(w, r, "POLICY_STATE_REFRESH_FAILED", "failed to refresh policy state projection", http.StatusInternalServerError)
			return
		}
		created = append(created, outboxRowToDTO(updated))
	}
	actionID, err := insertControlActionJournal(ctx, tx, controlActionJournalInsert{
		ActionType:     "rollback",
		TargetKind:     "workflow",
		LeaseKey:       workflowID,
		WorkflowID:     workflowID,
		Actor:          actor,
		ReasonCode:     req.ReasonCode,
		ReasonText:     req.ReasonText,
		IdempotencyKey: idemKey,
		RequestID:      requestID,
		BeforeStatus:   fmt.Sprintf("%d_policies", len(candidates)),
		AfterStatus:    "rollback_enqueued",
		TenantScope:    tenantScope,
		Classification: req.Classification,
	})
	if err != nil {
		if isUniqueConstraintErr(err) {
			existing, lookupErr := lookupControlActionByIdempotency(ctx, db, "rollback", idemKey, actor)
			if lookupErr != nil {
				writeErrorResponse(w, r, "MUTATION_REPLAY_LOOKUP_FAILED", "failed to check idempotency replay", http.StatusInternalServerError)
				return
			}
			if existing != nil && (existing.TargetKind != "workflow" || !existing.LeaseKey.Valid || existing.LeaseKey.String != workflowID) {
				writeErrorResponse(w, r, "IDEMPOTENCY_KEY_CONFLICT", "idempotency key already used for a different mutation target", http.StatusConflict)
				return
			}
			replay, replayErr = s.tryWorkflowRollbackReplay(ctx, db, idemKey, actor, workflowID)
			if replayErr == nil && replay != nil {
				writeJSONResponse(w, r, NewSuccessResponse(*replay), http.StatusOK)
				return
			}
		}
		writeErrorResponse(w, r, "ACTION_JOURNAL_WRITE_FAILED", "failed to persist workflow rollback audit", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorResponse(w, r, "MUTATION_TX_COMMIT_FAILED", "failed to commit workflow rollback transaction", http.StatusInternalServerError)
		return
	}
	if s.outboxStats != nil {
		s.outboxStats.NotifyPolicyOutboxDispatcher()
	}
	if s.audit != nil {
		s.audit.Log("workflow.rollback", utils.AuditWarn, map[string]interface{}{
			"request_id":      requestID,
			"command_id":      commandID,
			"workflow_id":     workflowID,
			"actor":           actor,
			"action_type":     "rollback",
			"reason_code":     req.ReasonCode,
			"tenant_scope":    tenantScope,
			"idempotency_key": idemKey,
			"affected_count":  len(created),
		})
	}
	writeJSONResponse(w, r, NewSuccessResponse(workflowRollbackResponse{
		ActionID:         actionID,
		CommandID:        commandID,
		WorkflowID:       workflowID,
		ActionType:       "rollback",
		IdempotentReplay: false,
		AffectedPolicies: len(created),
		Outbox:           created,
	}), http.StatusOK)
}

func (s *Server) loadWorkflowPolicySummaries(ctx context.Context, db *sql.DB, workflowID, tenantScope string) ([]policySummaryDTO, error) {
	schema, _ := loadControlSchemaSupport(ctx, db)
	if !schema.OutboxWorkflowID {
		return nil, fmt.Errorf("workflow_id column not available")
	}
	args := []interface{}{workflowID}
	where := []string{"workflow_id = $1"}
	where, args = appendAccessBoundOutboxFilter(where, args, tenantScope)
	ackScopeClause := accessBoundAckWhereClause(2, tenantScope)
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
			WHERE policy_id IN (SELECT policy_id FROM latest_outbox)%s
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
	`, controlOutboxRequestIDSelectExpr(schema), controlOutboxCommandIDSelectExpr(schema), controlOutboxWorkflowIDSelectExpr(schema), controlOutboxAnomalyIDSelectExpr(schema), controlOutboxFlowIDSelectExpr(), controlOutboxSourceIDSelectExpr(), controlOutboxSourceTypeSelectExpr(), controlOutboxSensorIDSelectExpr(), controlOutboxValidatorIDSelectExpr(), controlOutboxScopeIdentifierSelectExpr(), controlOutboxSentinelSelectExpr(schema), strings.Join(where, " AND "), controlAckEventIDSelectExpr(schema), controlAckTraceSelectExpr(schema), controlAckSourceEventSelectExpr(schema), controlAckSentinelEventSelectExpr(schema), ackScopeClause, ackScopeClause)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]policySummaryDTO, 0, 32)
	for rows.Next() {
		var row policyListRow
		if scanErr := rows.Scan(
			&row.PolicyID, &row.RequestID, &row.CommandID, &row.WorkflowID,
			&row.AnomalyID, &row.FlowID, &row.SourceID, &row.SourceType, &row.SensorID, &row.ValidatorID, &row.ScopeIdentifier,
			&row.TraceID, &row.SourceEventID, &row.SentinelEventID,
			&row.LatestOutboxID, &row.LatestStatus, &row.LatestCreatedAt, &row.LatestPublishedAt, &row.LatestAckedAt,
			&row.LatestAckEventID, &row.LatestAckResult, &row.LatestAckController, &row.LatestAckTraceID, &row.LatestAckSourceEvent, &row.LatestAckSentinel,
			&row.OutboxCount, &row.AckCount,
		); scanErr != nil {
			return nil, scanErr
		}
		items = append(items, policyListRowToDTO(row))
	}
	return items, rows.Err()
}

func (s *Server) loadWorkflowRecentEvents(ctx context.Context, db *sql.DB, workflowID, tenantScope string) ([]policyAckPayload, []controlOutboxRowDTO, error) {
	schema, _ := loadControlSchemaSupport(ctx, db)
	outboxWhere, outboxArgs := appendAccessBoundOutboxClause("workflow_id = $1", []interface{}{workflowID}, tenantScope)
	outboxRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			%s, %s, %s, %s, %s, %s, %s, %s, %s, %s,
			payload, status, retries, next_retry_at, last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at, ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT 50
	`, controlOutboxRequestIDSelectExpr(schema), controlOutboxCommandIDSelectExpr(schema), controlOutboxWorkflowIDSelectExpr(schema), controlOutboxAnomalyIDSelectExpr(schema), controlOutboxFlowIDSelectExpr(), controlOutboxSourceIDSelectExpr(), controlOutboxSourceTypeSelectExpr(), controlOutboxSensorIDSelectExpr(), controlOutboxValidatorIDSelectExpr(), controlOutboxScopeIdentifierSelectExpr(), outboxWhere), outboxArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer outboxRows.Close()
	outbox := make([]controlOutboxRowDTO, 0, 32)
	for outboxRows.Next() {
		var row controlOutboxRow
		if scanErr := outboxRows.Scan(
			&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
			&row.RequestID, &row.CommandID, &row.WorkflowID, &row.AnomalyID, &row.FlowID, &row.SourceID, &row.SourceType, &row.SensorID, &row.ValidatorID, &row.ScopeIdentifier,
			&row.Payload, &row.Status, &row.Retries, &row.NextRetryAt, &row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
			&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt, &row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
			&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
		); scanErr != nil {
			return nil, nil, scanErr
		}
		outbox = append(outbox, outboxRowToDTO(row))
	}
	if err := outboxRows.Err(); err != nil {
		return nil, nil, err
	}

	ackWhere := "workflow_id = $1"
	ackArgs := []interface{}{workflowID}
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
		LIMIT 100
	`, controlAckEventIDSelectExpr(schema), controlAckRequestIDSelectExpr(schema), controlAckCommandIDSelectExpr(schema), controlAckWorkflowIDSelectExpr(schema), controlAckTraceSelectExpr(schema), controlAckSourceEventSelectExpr(schema), controlAckSentinelEventSelectExpr(schema), ackWhere), ackArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer ackRows.Close()
	acks := make([]policyAckPayload, 0, 64)
	for ackRows.Next() {
		var row policyAckRow
		if scanErr := ackRows.Scan(
			&row.PolicyID, &row.AckEventID, &row.RequestID, &row.CommandID, &row.WorkflowID, &row.ControllerInstance,
			&row.ScopeIdentifier, &row.Tenant, &row.Region, &row.Result, &row.Reason, &row.ErrorCode,
			&row.AppliedAt, &row.AckedAt, &row.QCReference, &row.TraceID, &row.SourceEventID, &row.SentinelEventID, &row.FastPath,
			&row.RuleHash, &row.ProducerID, &row.ObservedAt,
		); scanErr != nil {
			return nil, nil, scanErr
		}
		acks = append(acks, policyAckToPayload(row))
	}
	return acks, outbox, ackRows.Err()
}

func (s *Server) loadWorkflowLatestOutboxRows(ctx context.Context, tx *sql.Tx, workflowID, tenantScope string) ([]controlOutboxRow, error) {
	outboxWhere, outboxArgs := appendAccessBoundOutboxClause("workflow_id = $1", []interface{}{workflowID}, tenantScope)
	rows, err := tx.QueryContext(ctx, `
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			request_id, command_id, workflow_id, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, sentinel_event_id, payload,
			status, retries, next_retry_at, last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at, ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE `+outboxWhere+`
		ORDER BY created_at DESC, id DESC
		FOR UPDATE
	`, outboxArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]controlOutboxRow, 0, 32)
	seenPolicies := make(map[string]struct{})
	for rows.Next() {
		var row controlOutboxRow
		if scanErr := rows.Scan(
			&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
			&row.RequestID, &row.CommandID, &row.WorkflowID, &row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Payload,
			&row.Status, &row.Retries, &row.NextRetryAt, &row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
			&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt, &row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
			&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
		); scanErr != nil {
			return nil, scanErr
		}
		if _, seen := seenPolicies[row.PolicyID]; seen {
			continue
		}
		seenPolicies[row.PolicyID] = struct{}{}
		items = append(items, row)
	}
	return items, rows.Err()
}

func (s *Server) tryWorkflowRollbackReplay(ctx context.Context, db *sql.DB, idempotencyKey, actor, workflowID string) (*workflowRollbackResponse, error) {
	var actionID, requestID string
	err := db.QueryRowContext(ctx, `
		SELECT action_id::STRING, request_id
		FROM control_actions_journal
		WHERE action_type = 'rollback'
		  AND idempotency_key = $1
		  AND actor = $2
		  AND target_kind = 'workflow'
		  AND lease_key = $3
		ORDER BY created_at DESC
		LIMIT 1
	`, idempotencyKey, actor, workflowID).Scan(&actionID, &requestID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			request_id, command_id, workflow_id, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, sentinel_event_id, payload,
			status, retries, next_retry_at, last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at, ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE request_id = $1 AND workflow_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 200
	`, requestID, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commandID := ""
	seen := make(map[string]struct{})
	outbox := make([]controlOutboxRowDTO, 0, 32)
	for rows.Next() {
		var row controlOutboxRow
		if scanErr := rows.Scan(
			&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
			&row.RequestID, &row.CommandID, &row.WorkflowID, &row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Payload,
			&row.Status, &row.Retries, &row.NextRetryAt, &row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
			&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt, &row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
			&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
		); scanErr != nil {
			return nil, scanErr
		}
		if row.CommandID.Valid && commandID == "" {
			commandID = strings.TrimSpace(row.CommandID.String)
		}
		if strings.EqualFold(extractPolicyPayloadAction(row.Payload), "remove") {
			if _, ok := seen[row.PolicyID]; ok {
				continue
			}
			seen[row.PolicyID] = struct{}{}
			outbox = append(outbox, outboxRowToDTO(row))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &workflowRollbackResponse{
		ActionID:         actionID,
		CommandID:        commandID,
		WorkflowID:       workflowID,
		ActionType:       "rollback",
		IdempotentReplay: true,
		AffectedPolicies: len(outbox),
		Outbox:           outbox,
	}, nil
}

func buildWorkflowSummaryFromPolicies(workflowID string, policies []policySummaryDTO, acks []policyAckPayload) workflowSummaryDTO {
	summary := workflowSummaryDTO{WorkflowID: workflowID, PolicyCount: int64(len(policies))}
	if len(policies) > 0 {
		first := policies[0]
		summary.RequestID = first.RequestID
		summary.CommandID = first.CommandID
		summary.LatestPolicyID = first.PolicyID
		summary.LatestOutboxID = first.LatestOutboxID
		summary.LatestStatus = first.LatestStatus
		summary.LatestTraceID = first.TraceID
		summary.LatestCreatedAt = first.LatestCreatedAt
		summary.LatestPublishedAt = first.LatestPublishedAt
		summary.LatestAckedAt = first.LatestAckedAt
		for _, policy := range policies {
			summary.OutboxCount += policy.OutboxCount
			summary.AckCount += policy.AckCount
		}
	}
	if len(acks) > 0 {
		ack := acks[0]
		summary.LatestAckEventID = ack.AckEventID
		summary.LatestAckResult = ack.Result
		summary.LatestAckController = ack.ControllerInstance
		if summary.LatestAckedAt == 0 {
			summary.LatestAckedAt = ack.AckedAt
		}
	}
	return summary
}

func workflowListRowToDTO(row workflowListRow) workflowSummaryDTO {
	dto := workflowSummaryDTO{
		WorkflowID:          row.WorkflowID,
		LatestPolicyID:      row.LatestPolicyID,
		LatestOutboxID:      row.LatestOutboxID,
		LatestStatus:        row.LatestStatus,
		LatestCreatedAt:     row.LatestCreatedAt.UTC().Unix(),
		PolicyCount:         row.PolicyCount,
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
	if row.LatestTraceID.Valid {
		dto.LatestTraceID = row.LatestTraceID.String
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

func extractWorkflowRoute(basePath, urlPath string) (string, bool) {
	base := strings.TrimSuffix(basePath, "/") + "/workflows/"
	if !strings.HasPrefix(urlPath, base) {
		return "", false
	}
	rest := strings.Trim(strings.TrimPrefix(urlPath, base), "/")
	if rest == "" || strings.Contains(rest, "/") || strings.HasSuffix(rest, ":rollback") {
		return "", false
	}
	return rest, true
}

func extractWorkflowMutationRoute(basePath, urlPath, suffix string) (string, bool) {
	base := strings.TrimSuffix(basePath, "/") + "/workflows/"
	if !strings.HasPrefix(urlPath, base) {
		return "", false
	}
	rest := strings.Trim(strings.TrimPrefix(urlPath, base), "/")
	if rest == "" || !strings.HasSuffix(rest, suffix) {
		return "", false
	}
	workflowID := strings.TrimSpace(strings.Trim(strings.TrimSuffix(rest, suffix), "/"))
	if workflowID == "" || strings.Contains(workflowID, "/") {
		return "", false
	}
	return workflowID, true
}

func hasWorkflowMutationSuffix(basePath, urlPath, suffix string) bool {
	_, ok := extractWorkflowMutationRoute(basePath, urlPath, suffix)
	return ok
}

func encodeWorkflowCursor(createdAt time.Time, workflowID string) string {
	return encodeOutboxCursor(createdAt, workflowID)
}

func decodeWorkflowCursor(cursor string) (time.Time, string, error) {
	return decodeOutboxCursor(cursor)
}
