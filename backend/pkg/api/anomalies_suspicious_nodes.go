package api

import (
	"context"
	"net/http"
	"time"
)

const suspiciousNodesTimeout = 3 * time.Second

// handleSuspiciousNodes handles GET /anomalies/suspicious-nodes
func (s *Server) handleSuspiciousNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.engine == nil {
		writeErrorFromUtils(w, r, NewUnavailableError("consensus engine"))
		return
	}

	timeout := s.config.RequestTimeout
	if timeout <= 0 || timeout > suspiciousNodesTimeout {
		timeout = suspiciousNodesTimeout
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	now := time.Now().UTC()
	validators := s.engine.ListValidators()
	nodes := s.buildSuspiciousNodes(ctx, validators, now)

	response := SuspiciousNodesResponse{
		Nodes:     nodes,
		UpdatedAt: now,
	}

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}
