package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	cockroach "backend/pkg/storage/cockroach"
)

const (
	runtimeRepairActionType = "runtime_repair"
	runtimeRepairLeaseKey   = "control.runtime.repair"
	runtimeRepairTargetKind = "lease"
	runtimeRepairMaxRetries = 3
)

type controlRuntimeRepairRequest struct {
	DryRun              bool   `json:"dry_run"`
	IncludeOutboxRescue bool   `json:"include_outbox_rescue"`
	ReasonCode          string `json:"reason_code"`
	ReasonText          string `json:"reason_text"`
}

type controlRuntimeRepairResponse struct {
	ActionID                string           `json:"action_id,omitempty"`
	IdempotentReplay        bool             `json:"idempotent_replay"`
	DryRun                  bool             `json:"dry_run"`
	ReasonCode              string           `json:"reason_code"`
	DeletedRowsByTable      map[string]int64 `json:"deleted_rows_by_table"`
	OutboxRowsRecovered     int64            `json:"outbox_rows_recovered"`
	GenesisCertificatesPre  int64            `json:"genesis_certificates_pre"`
	GenesisCertificatesPost int64            `json:"genesis_certificates_post"`
}

func (s *Server) handleControlRuntimeRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only POST method allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/control/runtime:repair") {
		writeErrorResponse(w, r, "INVALID_PATH", "path must end with /control/runtime:repair", http.StatusBadRequest)
		return
	}
	if err := s.requireControlRuntimeRepairAllowed(r); err != nil {
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
	var req controlRuntimeRepairRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErrorResponse(w, r, "INVALID_REQUEST", "invalid JSON body", http.StatusBadRequest)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErrorResponse(w, r, "INVALID_REQUEST", "invalid JSON body", http.StatusBadRequest)
		return
	}
	req.ReasonCode = strings.ToLower(strings.TrimSpace(req.ReasonCode))
	req.ReasonText = strings.TrimSpace(req.ReasonText)
	if !reasonCodePattern.MatchString(req.ReasonCode) {
		writeErrorResponse(w, r, "INVALID_REASON_CODE", "reason_code format is invalid", http.StatusBadRequest)
		return
	}
	if len(req.ReasonText) == 0 || len(req.ReasonText) > 512 {
		writeErrorResponse(w, r, "INVALID_REASON_TEXT", "reason_text is required and must be <= 512 chars", http.StatusBadRequest)
		return
	}
	requestSignature := runtimeRepairRequestSignature(req)
	requestClassification := runtimeRepairSignatureClassification(requestSignature)
	tenantScope := strings.TrimSpace(readActiveAccessCookie(r))
	if resolvedScope, scopeErr := s.resolveTenantScope(r); scopeErr == nil {
		tenantScope = strings.TrimSpace(resolvedScope)
	} else if tenantScope == "" {
		tenantScope = strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		if tenantScope == "" {
			tenantScope = strings.TrimSpace(r.URL.Query().Get("tenant"))
		}
		if tenantScope == "" {
			tenantScope = strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		}
	}
	if s.config.ControlMutationRequireTenant && strings.TrimSpace(tenantScope) == "" {
		s.controlMutationBlockedTenantScope.Add(1)
		writeErrorResponse(w, r, "TENANT_SCOPE_REQUIRED", "tenant scope is required for mutation endpoints", http.StatusBadRequest)
		return
	}
	db, err := s.getDB()
	if err != nil {
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.controlMutationTimeout())
	defer cancel()
	actor := s.resolveMutationActor(r, tenantScope)
	requestID := getRequestID(r.Context())
	workflowID := resolveWorkflowID(r, "")

	replay, replayErr := lookupRuntimeRepairActionByIdempotency(ctx, db, idemKey, actor)
	if replayErr != nil {
		s.noteControlTimeout(replayErr, true)
		writeErrorResponse(w, r, "MUTATION_REPLAY_LOOKUP_FAILED", "failed to check idempotency replay", http.StatusInternalServerError)
		return
	}
	if replay != nil {
		if strings.TrimSpace(replay.TargetKind) != runtimeRepairTargetKind || strings.TrimSpace(replay.LeaseKey.String) != runtimeRepairLeaseKey {
			writeErrorResponse(w, r, "IDEMPOTENCY_KEY_CONFLICT", "idempotency key already used for a different mutation target", http.StatusConflict)
			return
		}
		if strings.TrimSpace(replay.Classification.String) != requestClassification {
			writeErrorResponse(w, r, "IDEMPOTENCY_KEY_CONFLICT", "idempotency key already used with different runtime repair parameters", http.StatusConflict)
			return
		}
		writeJSONResponse(w, r, NewSuccessResponse(controlRuntimeRepairResponse{
			ActionID:         replay.ActionID,
			IdempotentReplay: true,
			DryRun:           req.DryRun,
			ReasonCode:       req.ReasonCode,
		}), http.StatusOK)
		return
	}

	resp, actionID, replayResp, apiErr := s.executeControlRuntimeRepairMutation(ctx, db, runtimeRepairMutationArgs{
		Request:               req,
		Actor:                 actor,
		WorkflowID:            workflowID,
		RequestID:             requestID,
		TenantScope:           tenantScope,
		IdempotencyKey:        idemKey,
		RequestClassification: requestClassification,
	})
	if apiErr != nil {
		writeErrorResponse(w, r, apiErr.Code, apiErr.Message, apiErr.HTTPStatus)
		return
	}
	if replayResp != nil {
		writeJSONResponse(w, r, NewSuccessResponse(*replayResp), http.StatusOK)
		return
	}
	resp.ActionID = actionID
	resp.IdempotentReplay = false
	if s.audit != nil {
		_ = s.audit.Critical("control.runtime_repair", map[string]interface{}{
			"request_id":                getRequestID(r.Context()),
			"action_id":                 actionID,
			"dry_run":                   resp.DryRun,
			"reason_code":               req.ReasonCode,
			"reason_text":               req.ReasonText,
			"idempotency_key":           idemKey,
			"tenant_scope":              tenantScope,
			"deleted_rows_by_table":     resp.DeletedRowsByTable,
			"outbox_rows_recovered":     resp.OutboxRowsRecovered,
			"genesis_certificates_pre":  resp.GenesisCertificatesPre,
			"genesis_certificates_post": resp.GenesisCertificatesPost,
		})
	}
	writeJSONResponse(w, r, NewSuccessResponse(resp), http.StatusOK)
}

func repairAfterStatus(dryRun bool) string {
	if dryRun {
		return "dry_run"
	}
	return "repair_applied"
}

type runtimeRepairMutationArgs struct {
	Request               controlRuntimeRepairRequest
	Actor                 string
	WorkflowID            string
	RequestID             string
	TenantScope           string
	IdempotencyKey        string
	RequestClassification string
}

func (s *Server) executeControlRuntimeRepairMutation(
	ctx context.Context,
	db *sql.DB,
	args runtimeRepairMutationArgs,
) (controlRuntimeRepairResponse, string, *controlRuntimeRepairResponse, *apiError) {
	for attempt := 1; attempt <= runtimeRepairMaxRetries; attempt++ {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{})
		if err != nil {
			s.noteControlTimeout(err, true)
			return controlRuntimeRepairResponse{}, "", nil, &apiError{Code: "MUTATION_TX_BEGIN_FAILED", Message: "failed to open mutation transaction", HTTPStatus: http.StatusInternalServerError}
		}

		resp, err := s.executeControlRuntimeRepairTx(ctx, tx, args.Request)
		if err != nil {
			_ = tx.Rollback()
			if runtimeRepairRetryable(err) && attempt < runtimeRepairMaxRetries {
				continue
			}
			s.noteControlTimeout(err, true)
			return controlRuntimeRepairResponse{}, "", nil, &apiError{Code: "CONTROL_RUNTIME_REPAIR_FAILED", Message: "failed to execute runtime repair", HTTPStatus: http.StatusInternalServerError}
		}

		actionID, err := insertControlActionJournal(ctx, tx, controlActionJournalInsert{
			ActionType:     runtimeRepairActionType,
			TargetKind:     runtimeRepairTargetKind,
			LeaseKey:       runtimeRepairLeaseKey,
			WorkflowID:     args.WorkflowID,
			Actor:          args.Actor,
			ReasonCode:     args.Request.ReasonCode,
			ReasonText:     args.Request.ReasonText,
			IdempotencyKey: args.IdempotencyKey,
			RequestID:      args.RequestID,
			BeforeStatus:   "repair_requested",
			AfterStatus:    repairAfterStatus(args.Request.DryRun),
			TenantScope:    args.TenantScope,
			Classification: args.RequestClassification,
		})
		if err != nil {
			_ = tx.Rollback()
			if isUniqueConstraintErr(err) {
				existing, lookupErr := lookupRuntimeRepairActionByIdempotency(ctx, db, args.IdempotencyKey, args.Actor)
				if lookupErr != nil {
					s.noteControlTimeout(lookupErr, true)
					return controlRuntimeRepairResponse{}, "", nil, &apiError{Code: "MUTATION_REPLAY_LOOKUP_FAILED", Message: "failed to check idempotency replay", HTTPStatus: http.StatusInternalServerError}
				}
				if existing != nil {
					if strings.TrimSpace(existing.TargetKind) != runtimeRepairTargetKind || strings.TrimSpace(existing.LeaseKey.String) != runtimeRepairLeaseKey {
						return controlRuntimeRepairResponse{}, "", nil, &apiError{Code: "IDEMPOTENCY_KEY_CONFLICT", Message: "idempotency key already used for a different mutation target", HTTPStatus: http.StatusConflict}
					}
					if strings.TrimSpace(existing.Classification.String) != args.RequestClassification {
						return controlRuntimeRepairResponse{}, "", nil, &apiError{Code: "IDEMPOTENCY_KEY_CONFLICT", Message: "idempotency key already used with different runtime repair parameters", HTTPStatus: http.StatusConflict}
					}
					replay := controlRuntimeRepairResponse{
						ActionID:         existing.ActionID,
						IdempotentReplay: true,
						DryRun:           args.Request.DryRun,
						ReasonCode:       args.Request.ReasonCode,
					}
					return controlRuntimeRepairResponse{}, "", &replay, nil
				}
			}
			if runtimeRepairRetryable(err) && attempt < runtimeRepairMaxRetries {
				continue
			}
			return controlRuntimeRepairResponse{}, "", nil, &apiError{Code: "ACTION_JOURNAL_WRITE_FAILED", Message: "failed to persist mutation audit", HTTPStatus: http.StatusInternalServerError}
		}

		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			if runtimeRepairRetryable(err) && attempt < runtimeRepairMaxRetries {
				continue
			}
			s.noteControlTimeout(err, true)
			return controlRuntimeRepairResponse{}, "", nil, &apiError{Code: "MUTATION_TX_COMMIT_FAILED", Message: "failed to commit mutation transaction", HTTPStatus: http.StatusInternalServerError}
		}

		return resp, actionID, nil, nil
	}

	return controlRuntimeRepairResponse{}, "", nil, &apiError{Code: "CONTROL_RUNTIME_REPAIR_FAILED", Message: "failed to execute runtime repair", HTTPStatus: http.StatusInternalServerError}
}

func runtimeRepairRetryable(err error) bool {
	return cockroach.IsRetryable(err)
}

func (s *Server) executeControlRuntimeRepairTx(ctx context.Context, tx *sql.Tx, req controlRuntimeRepairRequest) (controlRuntimeRepairResponse, error) {
	if tx == nil {
		return controlRuntimeRepairResponse{}, fmt.Errorf("tx required")
	}
	targetTables := []string{
		"consensus_proposals",
		"consensus_votes",
		"consensus_qcs",
		"consensus_metadata",
		"control_dispatcher_leases",
	}
	rowCounts := make(map[string]int64, len(targetTables))
	genesisPre, err := countRows(ctx, tx, "genesis_certificates")
	if err != nil {
		return controlRuntimeRepairResponse{}, err
	}
	for _, table := range targetTables {
		count, err := countRows(ctx, tx, table)
		if err != nil {
			return controlRuntimeRepairResponse{}, err
		}
		rowCounts[table] = count
	}
	livelockStateKey := strings.TrimSpace(s.config.ConsensusLivelockStateKey)
	staleRuntimeStateRows, err := countRuntimeRepairStaleStateRows(ctx, tx, livelockStateKey)
	if err != nil {
		return controlRuntimeRepairResponse{}, err
	}
	rowCounts["control_runtime_state_stale"] = staleRuntimeStateRows
	resp := controlRuntimeRepairResponse{
		DryRun:                  req.DryRun,
		ReasonCode:              req.ReasonCode,
		DeletedRowsByTable:      rowCounts,
		GenesisCertificatesPre:  genesisPre,
		GenesisCertificatesPost: genesisPre,
	}
	if req.DryRun {
		return resp, nil
	}

	deleted := make(map[string]int64, len(targetTables))
	for _, table := range targetTables {
		res, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			return controlRuntimeRepairResponse{}, err
		}
		affected, _ := res.RowsAffected()
		deleted[table] = affected
	}
	var outboxRecovered int64
	if req.IncludeOutboxRescue {
		res, err := tx.ExecContext(ctx, `
			UPDATE control_policy_outbox
			SET status='retry',
				next_retry_at=now(),
				lease_holder=NULL,
				lease_epoch=NULL,
				updated_at=now()
			WHERE status='publishing'
			  AND kafka_offset IS NULL
		`)
		if err != nil {
			return controlRuntimeRepairResponse{}, err
		}
		outboxRecovered, _ = res.RowsAffected()
	}
	runtimeStateDeleted, err := deleteRuntimeRepairStaleStateRows(ctx, tx, livelockStateKey)
	if err != nil {
		return controlRuntimeRepairResponse{}, err
	}
	deleted["control_runtime_state_stale"] = runtimeStateDeleted
	_, err = tx.ExecContext(ctx, `
		UPSERT INTO control_runtime_state (state_key, enabled, reason_code, reason_text, updated_at)
		VALUES ($1, false, $2, $3, now())
	`, strings.TrimSpace(s.config.ConsensusLivelockStateKey), "runtime_repair", req.ReasonText)
	if err != nil {
		return controlRuntimeRepairResponse{}, err
	}
	genesisPost, err := countRows(ctx, tx, "genesis_certificates")
	if err != nil {
		return controlRuntimeRepairResponse{}, err
	}
	resp.DeletedRowsByTable = deleted
	resp.OutboxRowsRecovered = outboxRecovered
	resp.GenesisCertificatesPost = genesisPost
	return resp, nil
}

func countRuntimeRepairStaleStateRows(ctx context.Context, tx *sql.Tx, livelockStateKey string) (int64, error) {
	var count int64
	err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM control_runtime_state
		WHERE state_key NOT IN ($1, $2, $3)
	`, controlRuntimeStateKeyMutationsSafeMode, controlRuntimeStateKeyMutationsKillSwitch, livelockStateKey).Scan(&count)
	return count, err
}

func deleteRuntimeRepairStaleStateRows(ctx context.Context, tx *sql.Tx, livelockStateKey string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		DELETE FROM control_runtime_state
		WHERE state_key NOT IN ($1, $2, $3)
	`, controlRuntimeStateKeyMutationsSafeMode, controlRuntimeStateKeyMutationsKillSwitch, livelockStateKey)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	return affected, nil
}

func countRows(ctx context.Context, db rowQueryer, table string) (int64, error) {
	var count int64
	err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count)
	return count, err
}

type runtimeRepairReplayRecord struct {
	ActionID       string
	TargetKind     string
	LeaseKey       sql.NullString
	Classification sql.NullString
}

func runtimeRepairRequestSignature(req controlRuntimeRepairRequest) string {
	raw := fmt.Sprintf("dry_run=%t|include_outbox_rescue=%t|reason_code=%s|reason_text=%s",
		req.DryRun,
		req.IncludeOutboxRescue,
		strings.TrimSpace(req.ReasonCode),
		strings.TrimSpace(req.ReasonText),
	)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func runtimeRepairSignatureClassification(signature string) string {
	return "runtime_repair_sig:" + strings.TrimSpace(signature)
}

func lookupRuntimeRepairActionByIdempotency(ctx context.Context, db *sql.DB, idempotencyKey, actor string) (*runtimeRepairReplayRecord, error) {
	var row runtimeRepairReplayRecord
	err := db.QueryRowContext(ctx, `
		SELECT action_id::STRING, target_kind, lease_key, classification
		FROM control_actions_journal
		WHERE action_type = $1
		  AND idempotency_key = $2
		  AND actor = $3
		ORDER BY created_at DESC
		LIMIT 1
	`, runtimeRepairActionType, idempotencyKey, actor).Scan(&row.ActionID, &row.TargetKind, &row.LeaseKey, &row.Classification)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
