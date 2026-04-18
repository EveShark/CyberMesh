package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type auditJournalRow struct {
	ActionID         string
	ActionType       string
	TargetKind       string
	OutboxID         sql.NullString
	LeaseKey         sql.NullString
	WorkflowID       sql.NullString
	PolicyID         sql.NullString
	Actor            string
	ReasonCode       string
	ReasonText       string
	IdempotencyKey   string
	RequestID        string
	BeforeStatus     sql.NullString
	AfterStatus      sql.NullString
	TenantScope      sql.NullString
	Classification   sql.NullString
	BeforeLeaseEpoch sql.NullInt64
	AfterLeaseEpoch  sql.NullInt64
	DecisionHash     sql.NullString
	CreatedAt        time.Time
}

type auditEntryDTO struct {
	ActionID         string `json:"action_id"`
	ActionType       string `json:"action_type"`
	TargetKind       string `json:"target_kind"`
	OutboxID         string `json:"outbox_id,omitempty"`
	LeaseKey         string `json:"lease_key,omitempty"`
	WorkflowID       string `json:"workflow_id,omitempty"`
	PolicyID         string `json:"policy_id,omitempty"`
	Actor            string `json:"actor"`
	ReasonCode       string `json:"reason_code"`
	ReasonText       string `json:"reason_text"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
	RequestID        string `json:"request_id"`
	BeforeStatus     string `json:"before_status,omitempty"`
	AfterStatus      string `json:"after_status,omitempty"`
	TenantScope      string `json:"tenant_scope,omitempty"`
	Classification   string `json:"classification,omitempty"`
	BeforeLeaseEpoch int64  `json:"before_lease_epoch,omitempty"`
	AfterLeaseEpoch  int64  `json:"after_lease_epoch,omitempty"`
	DecisionHash     string `json:"decision_hash,omitempty"`
	CreatedAt        int64  `json:"created_at"`
}

type auditListResponse struct {
	Rows       []auditEntryDTO `json:"rows"`
	Pagination controlListMeta `json:"pagination"`
}

type auditExportResponse struct {
	Format     string          `json:"format"`
	Rows       []auditEntryDTO `json:"rows"`
	ExportedAt int64           `json:"exported_at"`
}

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, err := parseControlLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeErrorResponse(w, r, "INVALID_LIMIT", err.Error(), http.StatusBadRequest)
		return
	}
	rows, nextCursor, queryErr := s.queryAuditRows(r, limit+1)
	if queryErr != nil {
		writeErrorResponse(w, r, queryErr.Code, queryErr.Message, queryErr.HTTPStatus)
		return
	}
	if len(rows) > limit {
		last := rows[limit-1]
		nextCursor = encodeAuditCursor(time.Unix(last.CreatedAt, 0).UTC(), last.ActionID)
		rows = rows[:limit]
	}
	writeJSONResponse(w, r, NewSuccessResponse(auditListResponse{
		Rows:       rows,
		Pagination: controlListMeta{Limit: limit, NextCursor: nextCursor},
	}), http.StatusOK)
}

func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 1000
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 5000 {
			writeErrorResponse(w, r, "INVALID_LIMIT", "limit must be between 1 and 5000", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	rows, _, queryErr := s.queryAuditRows(r, limit)
	if queryErr != nil {
		writeErrorResponse(w, r, queryErr.Code, queryErr.Message, queryErr.HTTPStatus)
		return
	}
	writeJSONResponse(w, r, NewSuccessResponse(auditExportResponse{
		Format:     "json",
		Rows:       rows,
		ExportedAt: time.Now().UTC().Unix(),
	}), http.StatusOK)
}

type auditQueryError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (s *Server) queryAuditRows(r *http.Request, limit int) ([]auditEntryDTO, string, *auditQueryError) {
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		return nil, "", &auditQueryError{Code: scopeErr.Code, Message: scopeErr.Message, HTTPStatus: scopeErr.HTTPStatus}
	}
	db, err := s.getDB()
	if err != nil {
		return nil, "", &auditQueryError{Code: "STORAGE_UNAVAILABLE", Message: "storage unavailable", HTTPStatus: http.StatusServiceUnavailable}
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.controlReadTimeout())
	defer cancel()

	args := []interface{}{}
	where := []string{"1=1"}
	where, args = appendAccessBoundAuditFilter(where, args, tenantScope)
	if policyID := strings.TrimSpace(r.URL.Query().Get("policy_id")); policyID != "" {
		where = append(where, fmt.Sprintf("COALESCE(caj.policy_id, co_created.policy_id, co.policy_id) = $%d", len(args)+1))
		args = append(args, policyID)
	}
	if workflowID := strings.TrimSpace(r.URL.Query().Get("workflow_id")); workflowID != "" {
		where = append(where, fmt.Sprintf("(COALESCE(caj.workflow_id, co_created.workflow_id, co.workflow_id, CASE WHEN caj.target_kind = 'workflow' THEN caj.lease_key ELSE '' END) = $%d)", len(args)+1))
		args = append(args, workflowID)
	}
	if actionType := strings.TrimSpace(r.URL.Query().Get("action_type")); actionType != "" {
		where = append(where, fmt.Sprintf("caj.action_type = $%d", len(args)+1))
		args = append(args, actionType)
	}
	if actor := strings.TrimSpace(r.URL.Query().Get("actor")); actor != "" {
		where = append(where, fmt.Sprintf("caj.actor = $%d", len(args)+1))
		args = append(args, actor)
	}
	if window := strings.TrimSpace(r.URL.Query().Get("window")); window != "" {
		dur, err := time.ParseDuration(window)
		if err != nil || dur <= 0 {
			return nil, "", &auditQueryError{Code: "INVALID_WINDOW", Message: "window must be a positive duration like 24h", HTTPStatus: http.StatusBadRequest}
		}
		where = append(where, fmt.Sprintf("caj.created_at >= $%d", len(args)+1))
		args = append(args, time.Now().UTC().Add(-dur))
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("cursor")); cursor != "" {
		cursorAt, cursorActionID, err := decodeAuditCursor(cursor)
		if err != nil {
			return nil, "", &auditQueryError{Code: "INVALID_CURSOR", Message: err.Error(), HTTPStatus: http.StatusBadRequest}
		}
		where = append(where, fmt.Sprintf("(caj.created_at, caj.action_id::STRING) < ($%d, $%d)", len(args)+1, len(args)+2))
		args = append(args, cursorAt, cursorActionID)
	}
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			caj.action_id::STRING,
			caj.action_type,
			caj.target_kind,
			caj.outbox_id::STRING,
			caj.lease_key,
			COALESCE(caj.workflow_id, CASE
				WHEN caj.target_kind = 'workflow' THEN caj.lease_key
				ELSE COALESCE(co_created.workflow_id, co.workflow_id)
			END) AS workflow_id,
			COALESCE(caj.policy_id, co_created.policy_id, co.policy_id) AS policy_id,
			caj.actor,
			caj.reason_code,
			caj.reason_text,
			caj.idempotency_key,
			caj.request_id,
			caj.before_status,
			caj.after_status,
			caj.tenant_scope,
			caj.classification,
			caj.before_lease_epoch,
			caj.after_lease_epoch,
			caj.decision_hash,
			caj.created_at
		FROM control_actions_journal caj
		LEFT JOIN control_policy_outbox co ON co.id = caj.outbox_id
		LEFT JOIN control_policy_outbox co_created ON co_created.id = CASE
			WHEN caj.lease_key ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
				THEN caj.lease_key::UUID
			ELSE NULL
		END
		WHERE %s
		ORDER BY caj.created_at DESC, caj.action_id DESC
		LIMIT $%d
	`, strings.Join(where, " AND "), len(args)), args...)
	if err != nil {
		if isTransientControlStorageError(err) {
			return []auditEntryDTO{}, "", nil
		}
		return nil, "", &auditQueryError{Code: "AUDIT_QUERY_FAILED", Message: "failed to query audit entries", HTTPStatus: http.StatusInternalServerError}
	}
	defer rows.Close()

	items := make([]auditEntryDTO, 0, limit)
	for rows.Next() {
		var row auditJournalRow
		if scanErr := rows.Scan(
			&row.ActionID, &row.ActionType, &row.TargetKind, &row.OutboxID, &row.LeaseKey, &row.WorkflowID, &row.PolicyID,
			&row.Actor, &row.ReasonCode, &row.ReasonText, &row.IdempotencyKey, &row.RequestID,
			&row.BeforeStatus, &row.AfterStatus, &row.TenantScope, &row.Classification,
			&row.BeforeLeaseEpoch, &row.AfterLeaseEpoch, &row.DecisionHash, &row.CreatedAt,
		); scanErr != nil {
			return nil, "", &auditQueryError{Code: "AUDIT_SCAN_FAILED", Message: "failed to read audit entry", HTTPStatus: http.StatusInternalServerError}
		}
		items = append(items, auditJournalRowToDTO(row))
	}
	if err := rows.Err(); err != nil {
		if isTransientControlStorageError(err) {
			return []auditEntryDTO{}, "", nil
		}
		return nil, "", &auditQueryError{Code: "AUDIT_ITERATION_FAILED", Message: "failed to iterate audit entries", HTTPStatus: http.StatusInternalServerError}
	}
	return items, "", nil
}

func auditJournalRowToDTO(row auditJournalRow) auditEntryDTO {
	dto := auditEntryDTO{
		ActionID:       row.ActionID,
		ActionType:     row.ActionType,
		TargetKind:     row.TargetKind,
		Actor:          row.Actor,
		ReasonCode:     row.ReasonCode,
		ReasonText:     row.ReasonText,
		IdempotencyKey: row.IdempotencyKey,
		RequestID:      row.RequestID,
		CreatedAt:      row.CreatedAt.UTC().Unix(),
	}
	if row.OutboxID.Valid {
		dto.OutboxID = row.OutboxID.String
	}
	if row.LeaseKey.Valid {
		dto.LeaseKey = row.LeaseKey.String
	}
	if row.WorkflowID.Valid {
		dto.WorkflowID = row.WorkflowID.String
	}
	if row.PolicyID.Valid {
		dto.PolicyID = row.PolicyID.String
	}
	if row.BeforeStatus.Valid {
		dto.BeforeStatus = row.BeforeStatus.String
	}
	if row.AfterStatus.Valid {
		dto.AfterStatus = row.AfterStatus.String
	}
	if row.TenantScope.Valid {
		dto.TenantScope = row.TenantScope.String
	}
	if row.Classification.Valid {
		dto.Classification = row.Classification.String
	}
	if row.BeforeLeaseEpoch.Valid {
		dto.BeforeLeaseEpoch = row.BeforeLeaseEpoch.Int64
	}
	if row.AfterLeaseEpoch.Valid {
		dto.AfterLeaseEpoch = row.AfterLeaseEpoch.Int64
	}
	if row.DecisionHash.Valid {
		dto.DecisionHash = row.DecisionHash.String
	}
	return dto
}

func encodeAuditCursor(createdAt time.Time, actionID string) string {
	return encodeOutboxCursor(createdAt, actionID)
}

func decodeAuditCursor(cursor string) (time.Time, string, error) {
	return decodeOutboxCursor(cursor)
}
