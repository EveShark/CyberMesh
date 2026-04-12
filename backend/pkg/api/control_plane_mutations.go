package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	consensusapi "backend/pkg/consensus/api"
	"backend/pkg/control/policystate"
	"backend/pkg/utils"
	"github.com/google/uuid"
)

var reasonCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{1,63}$`)
var errLeaseEpochFence = errors.New("lease epoch precondition failed")

type controlMutationRequest struct {
	ReasonCode         string `json:"reason_code"`
	ReasonText         string `json:"reason_text"`
	Classification     string `json:"classification,omitempty"`
	WorkflowID         string `json:"workflow_id,omitempty"`
	ExpectedLeaseEpoch *int64 `json:"expected_lease_epoch,omitempty"`
}

type controlMutationResponse struct {
	ActionID         string              `json:"action_id"`
	CommandID        string              `json:"command_id,omitempty"`
	WorkflowID       string              `json:"workflow_id,omitempty"`
	ActionType       string              `json:"action_type"`
	IdempotentReplay bool                `json:"idempotent_replay"`
	Outbox           controlOutboxRowDTO `json:"outbox,omitempty"`
	Lease            *leaseRowDTO        `json:"lease,omitempty"`
}

type controlActionJournalRow struct {
	ActionID   string
	TargetKind string
	OutboxID   sql.NullString
	LeaseKey   sql.NullString
}

func (s *Server) handleControlOutboxMutation(w http.ResponseWriter, r *http.Request, rowRef string) {
	const endpointName = "control.outbox.mutation"
	if err := s.checkControlBreaker(endpointName); err != nil {
		writeErrorResponse(w, r, "CONTROL_API_CIRCUIT_OPEN", "control mutation endpoint temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	breakerFailure := false
	defer func() {
		if breakerFailure {
			s.recordControlBreakerFailure(endpointName)
			return
		}
		s.recordControlBreakerSuccess(endpointName)
	}()
	if r.Method != http.MethodPost {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only POST method allowed", http.StatusMethodNotAllowed)
		return
	}

	rowID, action, ok := parseOutboxMutationRef(rowRef)
	if !ok {
		writeErrorResponse(w, r, "INVALID_MUTATION_PATH", "path must use :retry, :requeue, :mark-terminal, or :revoke", http.StatusBadRequest)
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
	if hdrEpoch := strings.TrimSpace(r.Header.Get("X-Expected-Lease-Epoch")); hdrEpoch != "" {
		parsed, parseErr := strconv.ParseInt(hdrEpoch, 10, 64)
		if parseErr != nil || parsed <= 0 {
			writeErrorResponse(w, r, "INVALID_EXPECTED_LEASE_EPOCH", "X-Expected-Lease-Epoch must be a positive integer", http.StatusBadRequest)
			return
		}
		req.ExpectedLeaseEpoch = &parsed
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
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.controlMutationTimeout())
	defer cancel()
	actor := s.resolveMutationActor(r, tenantScope)
	requestID := getRequestID(r.Context())
	commandID := generateCommandID()
	workflowID := resolveWorkflowID(r, req.WorkflowID)
	// Rate limit remains per actor, while cooldown is keyed by mutation target.
	if gateErr := s.enforceMutationThrottle(actor, action+"|"+rowID); gateErr != nil {
		writeErrorResponse(w, r, gateErr.Code, gateErr.Message, gateErr.HTTPStatus)
		return
	}

	var (
		replay    *controlMutationResponse
		replayErr error
	)
	if action == "revoke" {
		replay, replayErr = s.tryOutboxRevokeReplay(ctx, db, idemKey, actor, rowID)
	} else {
		replay, replayErr = s.tryOutboxMutationReplay(ctx, db, action, idemKey, actor, rowID)
	}
	if replayErr != nil {
		s.noteControlTimeout(replayErr, true)
		breakerFailure = true
		writeErrorResponse(w, r, "MUTATION_REPLAY_LOOKUP_FAILED", "failed to check idempotency replay", http.StatusInternalServerError)
		return
	}
	if replay != nil {
		writeJSONResponse(w, r, NewSuccessResponse(*replay), http.StatusOK)
		return
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		breakerFailure = true
		writeErrorResponse(w, r, "MUTATION_TX_BEGIN_FAILED", "failed to open mutation transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	row, err := s.loadOutboxRowForUpdate(ctx, tx, rowID)
	if err == sql.ErrNoRows {
		writeErrorResponse(w, r, "OUTBOX_ROW_NOT_FOUND", "outbox row not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.noteControlTimeout(err, true)
		breakerFailure = true
		writeErrorResponse(w, r, "OUTBOX_ROW_LOCK_FAILED", "failed to lock outbox row", http.StatusInternalServerError)
		return
	}

	if tenantScope != "" {
		var visible bool
		err = tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM policy_acks pa
				WHERE pa.policy_id = $1 AND pa.tenant = $2
			)
		`, row.PolicyID, tenantScope).Scan(&visible)
		if err != nil {
			s.noteControlTimeout(err, true)
			breakerFailure = true
			writeErrorResponse(w, r, "TENANT_SCOPE_QUERY_FAILED", "failed to verify tenant scope", http.StatusInternalServerError)
			return
		}
		if !visible && tenantScopeMatchesOutboxRow(row, tenantScope) {
			visible = true
		}
		if !visible {
			writeErrorResponse(w, r, "TENANT_SCOPE_FORBIDDEN", "requested outbox row is outside tenant scope", http.StatusForbidden)
			return
		}
	}

	before := row.Status
	if err := validateOutboxTransition(action, before); err != nil {
		writeErrorResponse(w, r, "INVALID_STATE_TRANSITION", err.Error(), http.StatusConflict)
		return
	}
	if err := validateLeaseEpochFence(action, row, req); err != nil {
		writeErrorResponse(w, r, "LEASE_EPOCH_FENCE_CONFLICT", err.Error(), http.StatusConflict)
		return
	}
	if err := validateOutboxRaceSafety(action, row); err != nil {
		writeErrorResponse(w, r, "OUTBOX_RACE_SAFETY_BLOCKED", err.Error(), http.StatusConflict)
		return
	}

	updated := row
	journalTargetKind := "outbox"
	journalOutboxID := rowID
	journalLeaseKey := ""
	if action == "revoke" {
		latestHeight := row.BlockHeight
		if s.stateStore != nil {
			if current := int64(s.stateStore.Latest()); current > latestHeight {
				latestHeight = current
			}
		}
		updated, err = insertRevokeOutboxRow(ctx, tx, latestHeight, row, requestID, commandID, workflowID)
		if err != nil {
			s.noteControlTimeout(err, true)
			breakerFailure = true
			writeErrorResponse(w, r, "OUTBOX_REVOKE_FAILED", "failed to create revoke outbox row", http.StatusInternalServerError)
			return
		}
		journalTargetKind = "outbox_revoke"
		journalLeaseKey = updated.ID
	} else {
		if err := applyOutboxMutation(ctx, tx, rowID, action, req); err != nil {
			s.noteControlTimeout(err, true)
			if errors.Is(err, errLeaseEpochFence) {
				writeErrorResponse(w, r, "LEASE_EPOCH_FENCE_CONFLICT", "lease epoch precondition failed", http.StatusConflict)
				return
			}
			breakerFailure = true
			writeErrorResponse(w, r, "OUTBOX_MUTATION_FAILED", "failed to update outbox row", http.StatusInternalServerError)
			return
		}

		updated, err = s.loadOutboxRowForUpdate(ctx, tx, rowID)
		if err != nil {
			s.noteControlTimeout(err, true)
			breakerFailure = true
			writeErrorResponse(w, r, "OUTBOX_ROW_RELOAD_FAILED", "failed to reload mutated outbox row", http.StatusInternalServerError)
			return
		}
	}
	if action != "revoke" {
		if err := policystate.Refresh(ctx, db, tx, updated.PolicyID); err != nil {
			s.noteControlTimeout(err, true)
			breakerFailure = true
			writeErrorResponse(w, r, "POLICY_STATE_REFRESH_FAILED", "failed to refresh policy state projection", http.StatusInternalServerError)
			return
		}
	}

	actionID, err := insertControlActionJournal(ctx, tx, controlActionJournalInsert{
		ActionType:       action,
		TargetKind:       journalTargetKind,
		OutboxID:         journalOutboxID,
		LeaseKey:         journalLeaseKey,
		WorkflowID:       workflowID,
		PolicyID:         row.PolicyID,
		Actor:            actor,
		ReasonCode:       req.ReasonCode,
		ReasonText:       req.ReasonText,
		IdempotencyKey:   idemKey,
		RequestID:        requestID,
		BeforeStatus:     before,
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
				s.noteControlTimeout(lookupErr, true)
				breakerFailure = true
				writeErrorResponse(w, r, "MUTATION_REPLAY_LOOKUP_FAILED", "failed to check idempotency replay", http.StatusInternalServerError)
				return
			}
			if existing != nil && (existing.TargetKind != journalTargetKind || !existing.OutboxID.Valid || existing.OutboxID.String != rowID) {
				writeErrorResponse(w, r, "IDEMPOTENCY_KEY_CONFLICT", "idempotency key already used for a different mutation target", http.StatusConflict)
				return
			}
			if action == "revoke" {
				replay, replayErr = s.tryOutboxRevokeReplay(ctx, db, idemKey, actor, rowID)
			} else {
				replay, replayErr = s.tryOutboxMutationReplay(ctx, db, action, idemKey, actor, rowID)
			}
			if replayErr == nil && replay != nil {
				writeJSONResponse(w, r, NewSuccessResponse(*replay), http.StatusOK)
				return
			}
		}
		breakerFailure = true
		writeErrorResponse(w, r, "ACTION_JOURNAL_WRITE_FAILED", "failed to persist mutation audit", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		s.noteControlTimeout(err, true)
		breakerFailure = true
		writeErrorResponse(w, r, "MUTATION_TX_COMMIT_FAILED", "failed to commit mutation transaction", http.StatusInternalServerError)
		return
	}

	if s.outboxStats != nil {
		s.outboxStats.NotifyPolicyOutboxDispatcher()
	}

	if s.audit != nil {
		s.audit.Log("control.mutation", utils.AuditInfo, map[string]interface{}{
			"request_id":      requestID,
			"command_id":      commandID,
			"workflow_id":     workflowID,
			"actor":           actor,
			"action_type":     action,
			"target_kind":     journalTargetKind,
			"outbox_id":       journalOutboxID,
			"before_status":   before,
			"after_status":    updated.Status,
			"reason_code":     req.ReasonCode,
			"tenant_scope":    tenantScope,
			"idempotency_key": idemKey,
			"classification":  req.Classification,
		})
	}

	writeJSONResponse(w, r, NewSuccessResponse(controlMutationResponse{
		ActionID:         actionID,
		CommandID:        commandID,
		WorkflowID:       workflowID,
		ActionType:       action,
		IdempotentReplay: false,
		Outbox:           outboxRowToDTO(updated),
	}), http.StatusOK)
}

func (s *Server) handleControlLeaseForceTakeover(w http.ResponseWriter, r *http.Request) {
	const endpointName = "control.lease.force_takeover"
	if err := s.checkControlBreaker(endpointName); err != nil {
		writeErrorResponse(w, r, "CONTROL_API_CIRCUIT_OPEN", "control lease endpoint temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	breakerFailure := false
	defer func() {
		if breakerFailure {
			s.recordControlBreakerFailure(endpointName)
			return
		}
		s.recordControlBreakerSuccess(endpointName)
	}()
	if r.Method != http.MethodPost {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only POST method allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/control/leases:force-takeover") {
		writeErrorResponse(w, r, "INVALID_MUTATION_PATH", "path must end with /control/leases:force-takeover", http.StatusBadRequest)
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
		writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.controlMutationTimeout())
	defer cancel()
	actor := s.resolveMutationActor(r, tenantScope)
	requestID := getRequestID(r.Context())
	commandID := generateCommandID()
	workflowID := resolveWorkflowID(r, "")

	replay, replayErr := s.tryLeaseTakeoverReplay(ctx, db, idemKey, actor)
	if replayErr != nil {
		s.noteControlTimeout(replayErr, true)
		breakerFailure = true
		writeErrorResponse(w, r, "MUTATION_REPLAY_LOOKUP_FAILED", "failed to check idempotency replay", http.StatusInternalServerError)
		return
	}
	if replay != nil {
		writeJSONResponse(w, r, NewSuccessResponse(*replay), http.StatusOK)
		return
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		s.noteControlTimeout(err, true)
		breakerFailure = true
		writeErrorResponse(w, r, "MUTATION_TX_BEGIN_FAILED", "failed to open mutation transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	leaseKey := strings.TrimSpace(r.URL.Query().Get("lease_key"))
	if leaseKey == "" {
		leaseKey = strings.TrimSpace(r.Header.Get("X-Lease-Key"))
	}
	if leaseKey == "" {
		leaseKey = strings.TrimSpace(r.Header.Get("CONTROL_POLICY_OUTBOX_LEASE_KEY"))
	}
	if leaseKey == "" {
		leaseKey = "control.policy.dispatcher"
	}
	if gateErr := s.enforceMutationThrottle(actor, "force_takeover|"+leaseKey); gateErr != nil {
		writeErrorResponse(w, r, gateErr.Code, gateErr.Message, gateErr.HTTPStatus)
		return
	}

	var row leaseRowDTO
	var leaseUntil, updatedAt time.Time
	err = tx.QueryRowContext(ctx, `
		UPDATE control_dispatcher_leases
		SET holder_id = $2,
			epoch = epoch + 1,
			lease_until = now() - INTERVAL '1 second',
			updated_at = now()
		WHERE lease_key = $1
		RETURNING lease_key, holder_id, epoch, lease_until, updated_at
	`, leaseKey, "forced:"+actor).Scan(&row.LeaseKey, &row.HolderID, &row.Epoch, &leaseUntil, &updatedAt)
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO control_dispatcher_leases (lease_key, holder_id, epoch, lease_until, updated_at)
			VALUES ($1, $2, 1, now() - INTERVAL '1 second', now())
			RETURNING lease_key, holder_id, epoch, lease_until, updated_at
		`, leaseKey, "forced:"+actor).Scan(&row.LeaseKey, &row.HolderID, &row.Epoch, &leaseUntil, &updatedAt)
	}
	if err != nil {
		breakerFailure = true
		writeErrorResponse(w, r, "LEASE_FORCE_TAKEOVER_FAILED", "failed to force lease takeover", http.StatusInternalServerError)
		return
	}
	row.LeaseUntil = leaseUntil.UTC().Unix()
	row.UpdatedAt = updatedAt.UTC().Unix()
	row.NowUnixMs = time.Now().UTC().UnixMilli()
	row.IsActive = false
	row.StaleByMs = maxInt64(time.Now().UTC().Sub(leaseUntil).Milliseconds(), 0)

	actionID, err := insertControlActionJournal(ctx, tx, controlActionJournalInsert{
		ActionType:       "force_takeover",
		TargetKind:       "lease",
		LeaseKey:         leaseKey,
		WorkflowID:       workflowID,
		Actor:            actor,
		ReasonCode:       req.ReasonCode,
		ReasonText:       req.ReasonText,
		IdempotencyKey:   idemKey,
		RequestID:        requestID,
		BeforeStatus:     "active",
		AfterStatus:      "expired",
		TenantScope:      tenantScope,
		Classification:   req.Classification,
		BeforeLeaseEpoch: maxInt64(row.Epoch-1, 0),
		AfterLeaseEpoch:  row.Epoch,
	})
	if err != nil {
		if isUniqueConstraintErr(err) {
			existing, lookupErr := lookupControlActionByIdempotency(ctx, db, "force_takeover", idemKey, actor)
			if lookupErr != nil {
				s.noteControlTimeout(lookupErr, true)
				breakerFailure = true
				writeErrorResponse(w, r, "MUTATION_REPLAY_LOOKUP_FAILED", "failed to check idempotency replay", http.StatusInternalServerError)
				return
			}
			if existing != nil && (existing.TargetKind != "lease" || !existing.LeaseKey.Valid || existing.LeaseKey.String != leaseKey) {
				writeErrorResponse(w, r, "IDEMPOTENCY_KEY_CONFLICT", "idempotency key already used for a different mutation target", http.StatusConflict)
				return
			}
			replay, replayErr := s.tryLeaseTakeoverReplay(ctx, db, idemKey, actor)
			if replayErr == nil && replay != nil {
				writeJSONResponse(w, r, NewSuccessResponse(*replay), http.StatusOK)
				return
			}
		}
		breakerFailure = true
		writeErrorResponse(w, r, "ACTION_JOURNAL_WRITE_FAILED", "failed to persist mutation audit", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		s.noteControlTimeout(err, true)
		breakerFailure = true
		writeErrorResponse(w, r, "MUTATION_TX_COMMIT_FAILED", "failed to commit mutation transaction", http.StatusInternalServerError)
		return
	}

	if s.audit != nil {
		s.audit.Log("control.lease_force_takeover", utils.AuditWarn, map[string]interface{}{
			"request_id":      requestID,
			"command_id":      commandID,
			"workflow_id":     workflowID,
			"actor":           actor,
			"action_type":     "force_takeover",
			"lease_key":       leaseKey,
			"reason_code":     req.ReasonCode,
			"tenant_scope":    tenantScope,
			"idempotency_key": idemKey,
		})
	}

	writeJSONResponse(w, r, NewSuccessResponse(controlMutationResponse{
		ActionID:         actionID,
		CommandID:        commandID,
		WorkflowID:       workflowID,
		ActionType:       "force_takeover",
		IdempotentReplay: false,
		Lease:            &row,
	}), http.StatusOK)
}

type controlMutationGateError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (s *Server) requireControlMutationAllowed(r *http.Request) *controlMutationGateError {
	if !s.config.ControlMutationsEnabled {
		return &controlMutationGateError{Code: "CONTROL_MUTATIONS_DISABLED", Message: "control mutations are disabled", HTTPStatus: http.StatusForbidden}
	}
	enabled, err := s.currentControlMutationSafeModeState(r.Context())
	if err != nil {
		return &controlMutationGateError{Code: "CONTROL_SAFE_MODE_STATE_UNAVAILABLE", Message: "failed to load shared safe mode state", HTTPStatus: http.StatusServiceUnavailable}
	}
	if enabled {
		s.controlMutationBlockedSafeMode.Add(1)
		return &controlMutationGateError{Code: "CONTROL_MUTATIONS_SAFE_MODE", Message: "control mutations are disabled by safe mode", HTTPStatus: http.StatusServiceUnavailable}
	}
	killSwitchEnabled, err := s.currentControlMutationKillSwitchState(r.Context())
	if err != nil {
		return &controlMutationGateError{Code: "CONTROL_KILL_SWITCH_STATE_UNAVAILABLE", Message: "failed to load shared kill-switch state", HTTPStatus: http.StatusServiceUnavailable}
	}
	if killSwitchEnabled {
		s.controlMutationBlockedKillSwitch.Add(1)
		return &controlMutationGateError{Code: "CONTROL_KILL_SWITCH_ENABLED", Message: "control mutations are disabled by kill-switch", HTTPStatus: http.StatusServiceUnavailable}
	}
	if s.config.ControlMutationRequireConsensus {
		if s.engine == nil {
			s.controlMutationBlockedConsensus.Add(1)
			return &controlMutationGateError{Code: "CONSENSUS_NOT_AVAILABLE", Message: "consensus engine not available", HTTPStatus: http.StatusServiceUnavailable}
		}
		status := s.engine.GetStatus()
		activation := consensusapi.PrivateGetActivationStatus(s.engine)
		if !(status.Running && s.engine.IsConsensusActive() && activation.HasQuorum) {
			s.controlMutationBlockedConsensus.Add(1)
			return &controlMutationGateError{Code: "CONSENSUS_HEALTH_GATE_BLOCKED", Message: "consensus health gate blocked mutation", HTTPStatus: http.StatusConflict}
		}
	}
	return nil
}

func (s *Server) resolveMutationActor(r *http.Request, tenantScope string) string {
	principalID := strings.TrimSpace(s.resolveLegacyPrincipalID(r))
	if principalID == "" {
		principalID = "legacy:unauthenticated"
	}
	tenantScope = strings.TrimSpace(tenantScope)
	if tenantScope != "" {
		return principalID + ":" + tenantScope
	}
	return principalID
}

func parseOutboxMutationRef(rowRef string) (string, string, bool) {
	parts := strings.SplitN(rowRef, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	rowID := strings.TrimSpace(parts[0])
	actionSuffix := strings.TrimSpace(parts[1])
	if rowID == "" {
		return "", "", false
	}
	switch actionSuffix {
	case "retry":
		return rowID, "retry", true
	case "requeue":
		return rowID, "requeue", true
	case "mark-terminal":
		return rowID, "mark_terminal", true
	case "revoke":
		return rowID, "revoke", true
	default:
		return "", "", false
	}
}

func parseControlMutationRequest(r *http.Request) (controlMutationRequest, error) {
	var req controlMutationRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return controlMutationRequest{}, fmt.Errorf("invalid JSON body")
	}
	// Reject trailing tokens to avoid ambiguous/malformed mutation payloads.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return controlMutationRequest{}, fmt.Errorf("invalid JSON body")
	}
	req.ReasonCode = strings.ToLower(strings.TrimSpace(req.ReasonCode))
	req.ReasonText = strings.TrimSpace(req.ReasonText)
	req.Classification = strings.ToLower(strings.TrimSpace(req.Classification))
	req.WorkflowID = strings.TrimSpace(req.WorkflowID)
	if req.ExpectedLeaseEpoch != nil && *req.ExpectedLeaseEpoch <= 0 {
		return controlMutationRequest{}, fmt.Errorf("invalid expected_lease_epoch")
	}
	return req, nil
}

func resolveWorkflowID(r *http.Request, bodyWorkflowID string) string {
	if r != nil {
		if headerWorkflowID, ok := normalizeWorkflowID(r.Header.Get("X-Workflow-Id")); ok {
			return headerWorkflowID
		}
	}
	if workflowID, ok := normalizeWorkflowID(bodyWorkflowID); ok {
		return workflowID
	}
	return generateWorkflowID()
}

func (s *Server) loadOutboxRowForUpdate(ctx context.Context, tx *sql.Tx, rowID string) (controlOutboxRow, error) {
	var row controlOutboxRow
	err := tx.QueryRowContext(ctx, `
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			request_id, command_id, workflow_id, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, sentinel_event_id, payload,
			status, retries, next_retry_at, last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at, ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE id = $1::UUID
		FOR UPDATE
	`, rowID).Scan(
		&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
		&row.RequestID, &row.CommandID, &row.WorkflowID, &row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Payload,
		&row.Status, &row.Retries, &row.NextRetryAt, &row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
		&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt, &row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
		&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
	)
	return row, err
}

func validateOutboxTransition(action, status string) error {
	switch action {
	case "retry":
		if status != "terminal_failed" {
			return fmt.Errorf("retry allowed only from terminal_failed")
		}
	case "requeue":
		if status != "retry" && status != "publishing" {
			return fmt.Errorf("requeue allowed only from retry or publishing")
		}
	case "mark_terminal":
		if status != "pending" && status != "retry" && status != "publishing" {
			return fmt.Errorf("mark-terminal allowed only from pending, retry, or publishing")
		}
	case "revoke":
		if status == "terminal_failed" {
			return fmt.Errorf("revoke not allowed from terminal_failed")
		}
	default:
		return fmt.Errorf("unsupported action")
	}
	return nil
}

func validateLeaseEpochFence(action string, row controlOutboxRow, req controlMutationRequest) error {
	requiresFence := action == "requeue" || action == "mark_terminal"
	if !requiresFence {
		return nil
	}
	if row.Status != "publishing" && row.Status != "retry" {
		return nil
	}
	if row.LeaseEpoch <= 0 {
		return nil
	}
	if req.ExpectedLeaseEpoch == nil {
		return fmt.Errorf("expected_lease_epoch is required for %s when status=%s", action, row.Status)
	}
	if *req.ExpectedLeaseEpoch != row.LeaseEpoch {
		return fmt.Errorf("expected_lease_epoch=%d does not match current lease_epoch=%d", *req.ExpectedLeaseEpoch, row.LeaseEpoch)
	}
	return nil
}

func validateOutboxRaceSafety(action string, row controlOutboxRow) error {
	if action == "revoke" {
		return nil
	}
	if row.AckedAt.Valid || row.AckResult.Valid {
		return fmt.Errorf("row already acked; mutation blocked to prevent ack/publish race")
	}
	// Defensive guard: if publish metadata exists but status is still mutable, prefer fail-closed.
	if (action == "requeue" || action == "mark_terminal" || action == "retry") && row.PublishedAt.Valid {
		return fmt.Errorf("row has publish metadata; mutation blocked to prevent publish/ack race")
	}
	return nil
}

func tenantScopeMatchesOutboxRow(row controlOutboxRow, tenantScope string) bool {
	if strings.TrimSpace(tenantScope) == "" || len(row.Payload) == 0 {
		return false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return false
	}
	candidates := []string{
		extractMapString(payload, "tenant"),
	}
	if metadata, ok := payload["metadata"].(map[string]interface{}); ok {
		candidates = append(candidates, extractMapString(metadata, "tenant"))
	}
	if trace, ok := payload["trace"].(map[string]interface{}); ok {
		candidates = append(candidates, extractMapString(trace, "tenant"))
	}
	if target, ok := payload["target"].(map[string]interface{}); ok {
		candidates = append(candidates, extractMapString(target, "tenant"))
		candidates = append(candidates, extractMapString(target, "tenant_id"))
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == strings.TrimSpace(tenantScope) {
			return true
		}
	}
	return false
}

func loadOutboxRowByID(ctx context.Context, query rowQueryer, rowID string) (controlOutboxRow, error) {
	var row controlOutboxRow
	err := query.QueryRowContext(ctx, `
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			request_id, command_id, workflow_id, trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, sentinel_event_id, payload,
			status, retries, next_retry_at, last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at, ack_result, ack_reason, ack_controller,
			acked_at, created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE id = $1::UUID
	`, rowID).Scan(
		&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
		&row.RequestID, &row.CommandID, &row.WorkflowID, &row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Payload,
		&row.Status, &row.Retries, &row.NextRetryAt, &row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
		&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt, &row.AckResult, &row.AckReason, &row.AckController,
		&row.AckedAt, &row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
	)
	return row, err
}

func resolveOutboxAnomalyID(ctx context.Context, query rowQueryer, row controlOutboxRow) (string, error) {
	if anomalyID := strings.TrimSpace(row.AnomalyID); anomalyID != "" {
		return anomalyID, nil
	}
	if ctxValues := parseOutboxOperationalContext(row.Payload); strings.TrimSpace(ctxValues.AnomalyID) != "" {
		return strings.TrimSpace(ctxValues.AnomalyID), nil
	}
	if !controlQueryerTableExists(ctx, query, "anomalies") {
		return "", nil
	}
	for _, lookup := range []struct {
		column string
		value  string
	}{
		{column: "source_event_id", value: strings.TrimSpace(row.SourceEventID.String)},
		{column: "sentinel_event_id", value: strings.TrimSpace(row.SentinelEventID.String)},
	} {
		if lookup.value == "" {
			continue
		}
		var anomalyID string
		err := query.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT anomaly_id
			FROM anomalies
			WHERE %s = $1
			ORDER BY detected_at DESC, created_at DESC
			LIMIT 1
		`, lookup.column), lookup.value).Scan(&anomalyID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(anomalyID) != "" {
			return strings.TrimSpace(anomalyID), nil
		}
	}
	return "", nil
}

func controlQueryerTableExists(ctx context.Context, query rowQueryer, tableName string) bool {
	var exists bool
	err := query.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name = $1
		)
	`, tableName).Scan(&exists)
	return err == nil && exists
}

type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

func (s *Server) tryOutboxRevokeReplay(ctx context.Context, db *sql.DB, idempotencyKey, actor, rowID string) (*controlMutationResponse, error) {
	var (
		actionID    string
		revokeRowID sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT action_id::STRING, lease_key
		FROM control_actions_journal
		WHERE action_type = 'revoke'
		  AND idempotency_key = $1
		  AND actor = $2
		  AND target_kind = 'outbox_revoke'
		  AND outbox_id = $3::UUID
		ORDER BY created_at DESC
		LIMIT 1
	`, idempotencyKey, actor, rowID).Scan(&actionID, &revokeRowID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !revokeRowID.Valid || strings.TrimSpace(revokeRowID.String) == "" {
		return nil, fmt.Errorf("revoke replay missing created outbox id")
	}

	queryCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
	defer cancel()
	row, err := loadOutboxRowByID(queryCtx, db, strings.TrimSpace(revokeRowID.String))
	if err != nil {
		return nil, err
	}
	return &controlMutationResponse{
		ActionID:         actionID,
		ActionType:       "revoke",
		IdempotentReplay: true,
		Outbox:           outboxRowToDTO(row),
	}, nil
}

func buildRevokeOutboxPayload(row controlOutboxRow, now time.Time, requestID, commandID, workflowID string) ([]byte, string, error) {
	if len(row.Payload) == 0 {
		return nil, "", fmt.Errorf("source outbox payload is empty")
	}
	var raw map[string]any
	if err := json.Unmarshal(row.Payload, &raw); err != nil {
		return nil, "", fmt.Errorf("decode source payload: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(asString(raw["action"])))
	if action == "remove" {
		return nil, "", fmt.Errorf("source payload already represents revoke")
	}
	ruleType := strings.TrimSpace(asString(raw["rule_type"]))
	if ruleType == "" {
		ruleType = "block"
	}
	if ruleType != "block" {
		return nil, "", fmt.Errorf("revoke only supported for block policies")
	}

	revokeTraceID := fmt.Sprintf("trace:revoke:%s:%d", row.PolicyID, now.UnixMilli())
	payload := map[string]any{
		"schema_version": 1,
		"policy_id":      row.PolicyID,
		"rule_type":      "block",
		"action":         "remove",
		"trace": map[string]any{
			"id": revokeTraceID,
		},
	}
	if strings.TrimSpace(requestID) != "" {
		payload["request_id"] = strings.TrimSpace(requestID)
		trace, _ := payload["trace"].(map[string]any)
		trace["request_id"] = strings.TrimSpace(requestID)
	}
	if strings.TrimSpace(commandID) != "" {
		payload["command_id"] = strings.TrimSpace(commandID)
		trace, _ := payload["trace"].(map[string]any)
		trace["command_id"] = strings.TrimSpace(commandID)
	}
	if strings.TrimSpace(workflowID) != "" {
		payload["workflow_id"] = strings.TrimSpace(workflowID)
		trace, _ := payload["trace"].(map[string]any)
		trace["workflow_id"] = strings.TrimSpace(workflowID)
	}
	if row.TraceID.Valid && strings.TrimSpace(row.TraceID.String) != "" {
		payload["metadata"] = map[string]any{
			"parent_trace_id": strings.TrimSpace(row.TraceID.String),
			"trace_id":        revokeTraceID,
		}
	}
	if strings.TrimSpace(row.AnomalyID) != "" {
		payload["anomaly_id"] = strings.TrimSpace(row.AnomalyID)
	}
	if strings.TrimSpace(requestID) != "" {
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
			payload["metadata"] = metadata
		}
		metadata["request_id"] = strings.TrimSpace(requestID)
	}
	if strings.TrimSpace(commandID) != "" {
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
			payload["metadata"] = metadata
		}
		metadata["command_id"] = strings.TrimSpace(commandID)
	}
	if strings.TrimSpace(workflowID) != "" {
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
			payload["metadata"] = metadata
		}
		metadata["workflow_id"] = strings.TrimSpace(workflowID)
	}
	if row.SourceEventID.Valid && strings.TrimSpace(row.SourceEventID.String) != "" {
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
			payload["metadata"] = metadata
		}
		metadata["source_event_id"] = strings.TrimSpace(row.SourceEventID.String)
		payload["source_event_id"] = strings.TrimSpace(row.SourceEventID.String)
	}
	if row.SourceEventTsMs.Valid && row.SourceEventTsMs.Int64 > 0 {
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
			payload["metadata"] = metadata
		}
		metadata["source_event_ts_ms"] = row.SourceEventTsMs.Int64
		payload["source_event_ts_ms"] = row.SourceEventTsMs.Int64
	}
	if row.SentinelEventID.Valid && strings.TrimSpace(row.SentinelEventID.String) != "" {
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
			payload["metadata"] = metadata
		}
		metadata["sentinel_event_id"] = strings.TrimSpace(row.SentinelEventID.String)
		payload["sentinel_event_id"] = strings.TrimSpace(row.SentinelEventID.String)
	}
	if strings.TrimSpace(row.AnomalyID) != "" {
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
			payload["metadata"] = metadata
		}
		metadata["anomaly_id"] = strings.TrimSpace(row.AnomalyID)
	}
	if tenant := firstNonEmpty(asString(raw["tenant"]), nestedString(raw, "target", "tenant"), nestedString(raw, "target", "tenant_id")); tenant != "" {
		payload["tenant"] = tenant
	}
	if region := firstNonEmpty(asString(raw["region"]), nestedString(raw, "target", "region")); region != "" {
		payload["region"] = region
	}
	if guardrails, ok := raw["guardrails"].(map[string]any); ok {
		if requiresAck, exists := guardrails["requires_ack"]; exists {
			payload["guardrails"] = map[string]any{"requires_ack": requiresAck}
		}
	}
	if audit, ok := raw["audit"].(map[string]any); ok && len(audit) > 0 {
		payload["audit"] = audit
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("encode revoke payload: %w", err)
	}
	return encoded, revokeTraceID, nil
}

func insertRevokeOutboxRow(ctx context.Context, tx *sql.Tx, latestHeight int64, source controlOutboxRow, requestID, commandID, workflowID string) (controlOutboxRow, error) {
	now := time.Now().UTC()
	if anomalyID, resolveErr := resolveOutboxAnomalyID(ctx, tx, source); resolveErr != nil {
		return controlOutboxRow{}, fmt.Errorf("resolve revoke anomaly lineage: %w", resolveErr)
	} else if anomalyID != "" {
		source.AnomalyID = anomalyID
	}
	payload, traceID, err := buildRevokeOutboxPayload(source, now, requestID, commandID, workflowID)
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
		return controlOutboxRow{}, fmt.Errorf("allocate revoke tx_index: %w", err)
	}
	ruleHash := sha256.Sum256(payload)

	var revokeID string
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
	).Scan(&revokeID)
	if err != nil {
		return controlOutboxRow{}, fmt.Errorf("insert revoke outbox row: %w", err)
	}
	return loadOutboxRowByID(ctx, tx, revokeID)
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func nestedString(root map[string]any, path ...string) string {
	cur := any(root)
	for _, segment := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[segment]
		if !ok {
			return ""
		}
	}
	return asString(cur)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func applyOutboxMutation(ctx context.Context, tx *sql.Tx, rowID, action string, req controlMutationRequest) error {
	var q string
	args := make([]interface{}, 0, 4)
	args = append(args, rowID)
	switch action {
	case "retry":
		q = `
			UPDATE control_policy_outbox
			SET status = 'pending',
				next_retry_at = NULL,
				last_error = NULL,
				lease_holder = NULL,
				updated_at = now()
			WHERE id = $1::UUID
		`
	case "requeue":
		q = `
			UPDATE control_policy_outbox
			SET status = 'pending',
				next_retry_at = NULL,
				last_error = $2,
				lease_holder = NULL,
				updated_at = now()
			WHERE id = $1::UUID
		`
		args = append(args, fmt.Sprintf("manual_%s:%s", action, req.ReasonCode))
	case "mark_terminal":
		q = `
			UPDATE control_policy_outbox
			SET status = 'terminal_failed',
				next_retry_at = NULL,
				lease_holder = NULL,
				last_error = $2,
				updated_at = now()
			WHERE id = $1::UUID
		`
		args = append(args, fmt.Sprintf("manual_%s:%s", action, req.ReasonCode))
	default:
		return errors.New("unsupported action")
	}

	if req.ExpectedLeaseEpoch != nil {
		placeholder := len(args) + 1
		q += fmt.Sprintf(" AND lease_epoch = $%d", placeholder)
		args = append(args, *req.ExpectedLeaseEpoch)
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if req.ExpectedLeaseEpoch != nil {
		affected, affErr := res.RowsAffected()
		if affErr == nil && affected == 0 {
			return errLeaseEpochFence
		}
	}
	return nil
}

type controlActionJournalInsert struct {
	ActionType       string
	TargetKind       string
	OutboxID         string
	LeaseKey         string
	WorkflowID       string
	PolicyID         string
	Actor            string
	ReasonCode       string
	ReasonText       string
	IdempotencyKey   string
	RequestID        string
	BeforeStatus     string
	AfterStatus      string
	TenantScope      string
	Classification   string
	BeforeLeaseEpoch int64
	AfterLeaseEpoch  int64
}

func insertControlActionJournal(ctx context.Context, tx *sql.Tx, p controlActionJournalInsert) (string, error) {
	var actionID string
	var outboxArg interface{}
	if strings.TrimSpace(p.OutboxID) == "" {
		outboxArg = nil
	} else {
		outboxArg = p.OutboxID
	}
	var leaseArg interface{}
	if strings.TrimSpace(p.LeaseKey) == "" {
		leaseArg = nil
	} else {
		leaseArg = p.LeaseKey
	}
	var workflowArg interface{}
	if strings.TrimSpace(p.WorkflowID) == "" {
		workflowArg = nil
	} else {
		workflowArg = p.WorkflowID
	}
	var policyArg interface{}
	if strings.TrimSpace(p.PolicyID) == "" {
		policyArg = nil
	} else {
		policyArg = p.PolicyID
	}
	var tenantArg interface{}
	if strings.TrimSpace(p.TenantScope) == "" {
		tenantArg = nil
	} else {
		tenantArg = p.TenantScope
	}
	var classArg interface{}
	if strings.TrimSpace(p.Classification) == "" {
		classArg = nil
	} else {
		classArg = p.Classification
	}
	decisionRaw := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%d|%d",
		p.ActionType, p.TargetKind, p.OutboxID, p.LeaseKey, p.Actor,
		p.ReasonCode, p.ReasonText, p.IdempotencyKey, p.BeforeLeaseEpoch, p.AfterLeaseEpoch,
	)
	sum := sha256.Sum256([]byte(decisionRaw))
	decisionHash := hex.EncodeToString(sum[:])

	err := tx.QueryRowContext(ctx, `
		INSERT INTO control_actions_journal (
			action_type, target_kind, outbox_id, lease_key, workflow_id, policy_id, actor,
			reason_code, reason_text, idempotency_key, request_id,
			before_status, after_status, tenant_scope, classification,
			before_lease_epoch, after_lease_epoch, decision_hash
		)
		VALUES ($1, $2, $3::UUID, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING action_id::STRING
	`,
		p.ActionType, p.TargetKind, outboxArg, leaseArg, workflowArg, policyArg, p.Actor,
		p.ReasonCode, p.ReasonText, p.IdempotencyKey, p.RequestID,
		p.BeforeStatus, p.AfterStatus, tenantArg, classArg,
		p.BeforeLeaseEpoch, p.AfterLeaseEpoch, decisionHash,
	).Scan(&actionID)
	return actionID, err
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key")
}

func generateCommandID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func generateWorkflowID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func lookupControlActionByIdempotency(ctx context.Context, db *sql.DB, actionType, idempotencyKey, actor string) (*controlActionJournalRow, error) {
	var row controlActionJournalRow
	err := db.QueryRowContext(ctx, `
		SELECT action_id::STRING, target_kind, outbox_id::STRING, lease_key
		FROM control_actions_journal
		WHERE action_type = $1
		  AND idempotency_key = $2
		  AND actor = $3
		ORDER BY created_at DESC
		LIMIT 1
	`, actionType, idempotencyKey, actor).Scan(&row.ActionID, &row.TargetKind, &row.OutboxID, &row.LeaseKey)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Server) tryOutboxMutationReplay(ctx context.Context, db *sql.DB, action, idempotencyKey, actor, rowID string) (*controlMutationResponse, error) {
	var actionID string
	err := db.QueryRowContext(ctx, `
		SELECT action_id::STRING
		FROM control_actions_journal
		WHERE action_type = $1
		  AND idempotency_key = $2
		  AND actor = $3
		  AND target_kind = 'outbox'
		  AND outbox_id = $4::UUID
		ORDER BY created_at DESC
		LIMIT 1
	`, action, idempotencyKey, actor, rowID).Scan(&actionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	queryCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
	defer cancel()
	var row controlOutboxRow
	err = db.QueryRowContext(queryCtx, `
		SELECT
			id::STRING, block_height, block_ts, tx_index, policy_id,
			trace_id, ai_event_ts_ms, source_event_id, source_event_ts_ms, sentinel_event_id, status, retries, next_retry_at,
			last_error, lease_holder, lease_epoch, kafka_topic,
			kafka_partition, kafka_offset, published_at,
			ack_result, ack_reason, ack_controller, acked_at,
			created_at, updated_at, rule_hash
		FROM control_policy_outbox
		WHERE id = $1::UUID
	`, rowID).Scan(
		&row.ID, &row.BlockHeight, &row.BlockTS, &row.TxIndex, &row.PolicyID,
		&row.TraceID, &row.AIEventTsMs, &row.SourceEventID, &row.SourceEventTsMs, &row.SentinelEventID, &row.Status, &row.Retries, &row.NextRetryAt,
		&row.LastError, &row.LeaseHolder, &row.LeaseEpoch, &row.KafkaTopic,
		&row.KafkaPartition, &row.KafkaOffset, &row.PublishedAt,
		&row.AckResult, &row.AckReason, &row.AckController, &row.AckedAt,
		&row.CreatedAt, &row.UpdatedAt, &row.RuleHash,
	)
	if err != nil {
		return nil, err
	}

	resp := controlMutationResponse{
		ActionID:         actionID,
		ActionType:       action,
		IdempotentReplay: true,
		Outbox:           outboxRowToDTO(row),
	}
	return &resp, nil
}

func (s *Server) tryLeaseTakeoverReplay(ctx context.Context, db *sql.DB, idempotencyKey, actor string) (*controlMutationResponse, error) {
	var (
		actionID string
		leaseKey sql.NullString
	)
	err := db.QueryRowContext(ctx, `
		SELECT action_id::STRING, lease_key
		FROM control_actions_journal
		WHERE action_type = 'force_takeover'
		  AND idempotency_key = $1
		  AND actor = $2
		  AND target_kind = 'lease'
		ORDER BY created_at DESC
		LIMIT 1
	`, idempotencyKey, actor).Scan(&actionID, &leaseKey)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !leaseKey.Valid || strings.TrimSpace(leaseKey.String) == "" {
		return nil, nil
	}

	var row leaseRowDTO
	var leaseUntil, updatedAt time.Time
	err = db.QueryRowContext(ctx, `
		SELECT lease_key, holder_id, epoch, lease_until, updated_at
		FROM control_dispatcher_leases
		WHERE lease_key = $1
	`, leaseKey.String).Scan(&row.LeaseKey, &row.HolderID, &row.Epoch, &leaseUntil, &updatedAt)
	if err != nil {
		return nil, err
	}
	row.LeaseUntil = leaseUntil.UTC().Unix()
	row.UpdatedAt = updatedAt.UTC().Unix()
	now := time.Now().UTC()
	row.NowUnixMs = now.UnixMilli()
	row.IsActive = now.Before(leaseUntil)
	row.StaleByMs = maxInt64(now.Sub(leaseUntil).Milliseconds(), 0)

	resp := controlMutationResponse{
		ActionID:         actionID,
		ActionType:       "force_takeover",
		IdempotentReplay: true,
		Lease:            &row,
	}
	return &resp, nil
}
