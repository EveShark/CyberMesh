package api

import (
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

	s.controlMutationsSafeMode.Store(req.Enabled)
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
