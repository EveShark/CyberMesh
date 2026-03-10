package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"backend/pkg/utils"
)

type policyAckRow struct {
	PolicyID           string
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
	FastPath           bool   `json:"fast_path"`
	RuleHashHex        string `json:"rule_hash_hex,omitempty"`
	ProducerIDHex      string `json:"producer_id_hex,omitempty"`
	ObservedAt         int64  `json:"observed_at"`
	AckHistoryCount    int64  `json:"ack_history_count,omitempty"`
	FirstEventAckedAt  int64  `json:"first_event_acked_at,omitempty"`
	LatestEventAckedAt int64  `json:"latest_event_acked_at,omitempty"`
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

	rows, err := db.QueryContext(ctx, `
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
