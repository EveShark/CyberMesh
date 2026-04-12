package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"backend/pkg/utils"
)

type controlSafeModeToggleRequest struct {
	Enabled    bool   `json:"enabled"`
	ReasonCode string `json:"reason_code"`
	ReasonText string `json:"reason_text"`
}

type controlSafeModeToggleResponse struct {
	Enabled bool `json:"enabled"`
}

type controlKillSwitchToggleRequest struct {
	Enabled    bool   `json:"enabled"`
	ReasonCode string `json:"reason_code"`
	ReasonText string `json:"reason_text"`
}

type controlKillSwitchToggleResponse struct {
	Enabled bool `json:"enabled"`
}

func (s *Server) handleControlSafeModeToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only POST method allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/control/safe-mode:toggle") {
		writeErrorResponse(w, r, "INVALID_PATH", "path must end with /control/safe-mode:toggle", http.StatusBadRequest)
		return
	}

	var req controlSafeModeToggleRequest
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
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}

	if s.storage != nil {
		db, err := s.getDB()
		if err != nil {
			writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.controlMutationTimeout())
		defer cancel()
		requestID := getRequestID(r.Context())
		if requestID == "" {
			requestID = generateCommandID()
		}
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if idempotencyKey == "" {
			idempotencyKey = requestID
		}
		actionType := "safe_mode_disable"
		if req.Enabled {
			actionType = "safe_mode_enable"
		}
		if _, err := persistControlRuntimeToggle(ctx, db, controlRuntimeToggleMutation{
			StateKey:       controlRuntimeStateKeyMutationsSafeMode,
			ActionType:     actionType,
			Enabled:        req.Enabled,
			Actor:          s.resolveMutationActor(r, tenantScope),
			ReasonCode:     req.ReasonCode,
			ReasonText:     req.ReasonText,
			IdempotencyKey: idempotencyKey,
			RequestID:      requestID,
			TenantScope:    tenantScope,
		}); err != nil {
			s.noteControlTimeout(err, false)
			writeErrorResponse(w, r, "CONTROL_SAFE_MODE_UPDATE_FAILED", "failed to persist safe mode state", http.StatusInternalServerError)
			return
		}
	}

	s.controlMutationsSafeMode.Store(req.Enabled)
	if s.controlSafeMode != nil {
		s.controlSafeMode.Store(req.Enabled)
	}
	if s.audit != nil {
		s.audit.Log("control.safe_mode_toggle", utils.AuditWarn, map[string]interface{}{
			"request_id":  getRequestID(r.Context()),
			"enabled":     req.Enabled,
			"reason_code": req.ReasonCode,
			"reason_text": req.ReasonText,
		})
	}

	writeJSONResponse(w, r, NewSuccessResponse(controlSafeModeToggleResponse{Enabled: req.Enabled}), http.StatusOK)
}

func (s *Server) handleControlKillSwitchToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only POST method allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/control/kill-switch:toggle") {
		writeErrorResponse(w, r, "INVALID_PATH", "path must end with /control/kill-switch:toggle", http.StatusBadRequest)
		return
	}

	var req controlKillSwitchToggleRequest
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
	tenantScope, scopeErr := s.resolveTenantScope(r)
	if scopeErr != nil {
		writeErrorResponse(w, r, scopeErr.Code, scopeErr.Message, scopeErr.HTTPStatus)
		return
	}

	if s.storage != nil {
		db, err := s.getDB()
		if err != nil {
			writeErrorResponse(w, r, "STORAGE_UNAVAILABLE", err.Error(), http.StatusInternalServerError)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.controlMutationTimeout())
		defer cancel()
		requestID := getRequestID(r.Context())
		if requestID == "" {
			requestID = generateCommandID()
		}
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if idempotencyKey == "" {
			idempotencyKey = requestID
		}
		actionType := "kill_switch_disable"
		if req.Enabled {
			actionType = "kill_switch_enable"
		}
		if _, err := persistControlRuntimeToggle(ctx, db, controlRuntimeToggleMutation{
			StateKey:       controlRuntimeStateKeyMutationsKillSwitch,
			ActionType:     actionType,
			Enabled:        req.Enabled,
			Actor:          s.resolveMutationActor(r, tenantScope),
			ReasonCode:     req.ReasonCode,
			ReasonText:     req.ReasonText,
			IdempotencyKey: idempotencyKey,
			RequestID:      requestID,
			TenantScope:    tenantScope,
		}); err != nil {
			s.noteControlTimeout(err, false)
			writeErrorResponse(w, r, "CONTROL_KILL_SWITCH_UPDATE_FAILED", "failed to persist kill-switch state", http.StatusInternalServerError)
			return
		}
	}

	s.controlMutationsKillSwitch.Store(req.Enabled)
	if s.audit != nil {
		s.audit.Log("control.kill_switch_toggle", utils.AuditCritical, map[string]interface{}{
			"request_id":  getRequestID(r.Context()),
			"enabled":     req.Enabled,
			"reason_code": req.ReasonCode,
			"reason_text": req.ReasonText,
		})
	}

	writeJSONResponse(w, r, NewSuccessResponse(controlKillSwitchToggleResponse{Enabled: req.Enabled}), http.StatusOK)
}
