package api

import (
	"net/http"
	"strings"

	"backend/pkg/consensus/types"
	"backend/pkg/utils"
)

// handleValidators handles GET /validators and GET /validators/:id
func (s *Server) handleValidators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if this is a specific validator query
	path := strings.TrimPrefix(r.URL.Path, s.config.BasePath+"/validators")
	path = strings.TrimPrefix(path, "/")

	if path == "" || path == "?" || strings.HasPrefix(path, "?") {
		// List validators
		s.handleValidatorList(w, r)
		return
	}

	// Single validator by ID
	s.handleValidatorByID(w, r, path)
}

// handleValidatorList handles GET /validators
func (s *Server) handleValidatorList(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		writeErrorFromUtils(w, r, NewUnavailableError("consensus engine"))
		return
	}

	statusParam := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if err := validateStatus(statusParam); err != nil {
		if apiErr, ok := err.(*utils.Error); ok {
			writeErrorFromUtils(w, r, apiErr)
		} else {
			writeErrorResponse(w, r, "INVALID_PARAMS", err.Error(), http.StatusBadRequest)
		}
		return
	}

	validators := s.engine.ListValidators()
	responses := make([]ValidatorResponse, 0, len(validators))

	for _, info := range validators {
		switch statusParam {
		case "active":
			if !info.IsActive {
				continue
			}
		case "inactive":
			if info.IsActive {
				continue
			}
		}
		responses = append(responses, validatorInfoToResponse(info))
	}

	response := ValidatorListResponse{
		Validators: responses,
		Total:      len(responses),
	}

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

// handleValidatorByID handles GET /validators/:id
func (s *Server) handleValidatorByID(w http.ResponseWriter, r *http.Request, id string) {
	if s.engine == nil {
		writeErrorFromUtils(w, r, NewUnavailableError("consensus engine"))
		return
	}

	if err := validateValidatorID(id); err != nil {
		if apiErr, ok := err.(*utils.Error); ok {
			writeErrorFromUtils(w, r, apiErr)
		} else {
			writeErrorResponse(w, r, "INVALID_PARAMS", err.Error(), http.StatusBadRequest)
		}
		return
	}

	id = sanitizeString(id)

	bytes, err := parseHex(id)
	if err != nil {
		writeErrorFromUtils(w, r, NewInvalidParamsError("id", "must be valid hex"))
		return
	}
	var expected types.ValidatorID
	if len(bytes) != len(expected) {
		writeErrorFromUtils(w, r, NewInvalidParamsError("id", "validator ID must be 32 bytes"))
		return
	}

	var validatorID types.ValidatorID
	copy(validatorID[:], bytes)

	info, err := s.engine.ValidatorInfo(validatorID)
	if err != nil || info == nil {
		writeErrorFromUtils(w, r, NewValidatorNotFoundError(id))
		return
	}

	response := validatorInfoToResponse(*info)
	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

func validatorInfoToResponse(info types.ValidatorInfo) ValidatorResponse {
	status := "inactive"
	if info.IsActive {
		status = "active"
	}

	resp := ValidatorResponse{
		ID:             encodeHex(info.ID[:]),
		PublicKey:      encodeHex(info.PublicKey),
		Status:         status,
		VotingPower:    0,
		JoinedAtHeight: info.JoinedView,
	}

	if info.Reputation > 0 {
		resp.UptimePercentage = info.Reputation
	}

	return resp
}
