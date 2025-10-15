package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"backend/pkg/utils"
)

const (
	apiVersion = "1.0.0"
)

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().Unix(),
		Version:   apiVersion,
	}

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

// handleReadiness handles GET /ready
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	checks := make(map[string]string)
	details := make(map[string]interface{})
	allReady := true

	// Check storage
	if s.storage != nil {
		if err := s.checkStorage(ctx); err != nil {
			checks["storage"] = "not ready"
			details["storage_error"] = err.Error()
			allReady = false
		} else {
			checks["storage"] = "ok"
		}
	} else {
		checks["storage"] = "not configured"
		allReady = false
	}

	// Check state store
	if s.stateStore != nil {
		if err := s.checkStateStore(ctx); err != nil {
			checks["state"] = "not ready"
			details["state_error"] = err.Error()
			allReady = false
		} else {
			checks["state"] = "ok"
			details["state_version"] = s.stateStore.Latest()
		}
	} else {
		checks["state"] = "not configured"
		allReady = false
	}

	// Check mempool
	if s.mempool != nil {
		checks["mempool"] = "ok"
	} else {
		checks["mempool"] = "not configured"
	}

	// Check consensus engine
	if s.engine != nil {
		status := s.engine.GetStatus()
		activation := s.engine.GetActivationStatus()
		details["consensus_ready_validators"] = activation.ReadyValidators
		details["consensus_required_ready"] = activation.RequiredReady
		if status.Running && s.engine.IsConsensusActive() && activation.HasQuorum {
			checks["consensus"] = "ok"
		} else {
			checks["consensus"] = "not ready"
			allReady = false
			if !status.Running {
				details["consensus_error"] = "engine not running"
			} else if !s.engine.IsConsensusActive() {
				details["consensus_error"] = "consensus not active"
			} else if !activation.HasQuorum {
				details["consensus_error"] = "activation quorum not met"
			}
		}
	} else {
		checks["consensus"] = "not configured"
		// Not strictly fatal if API can serve historical data; do not flip allReady here
	}

	// Check P2P peer connectivity for multi-node deployments
	if s.p2pRouter != nil && s.engine != nil {
		activation := s.engine.GetActivationStatus()
		details["p2p_connected_peers"] = activation.ConnectedPeers
		details["p2p_active_peers"] = activation.ActivePeers
		totalValidators := len(s.engine.ListValidators())
		details["p2p_total_validators"] = totalValidators
		details["p2p_required_peers"] = activation.RequiredPeers
		details["p2p_quorum_2f+1"] = activation.RequiredReady

		if totalValidators <= 1 {
			// Single validator deployment (dev/test)
			checks["p2p_quorum"] = "single_node"
		} else if activation.ConnectedPeers >= activation.RequiredPeers {
			checks["p2p_quorum"] = "ok"
		} else if activation.ConnectedPeers == 0 {
			// No peers - cluster not formed yet
			checks["p2p_quorum"] = "no_peers"
			details["p2p_quorum_error"] = fmt.Sprintf("need %d peers, have 0", activation.RequiredPeers)
			allReady = false
		} else {
			// Some peers but below quorum threshold
			checks["p2p_quorum"] = "insufficient"
			details["p2p_quorum_error"] = fmt.Sprintf("connected %d, need %d (quorum %d/%d validators)",
				activation.ConnectedPeers, activation.RequiredPeers, activation.RequiredReady, totalValidators)
			allReady = false
		}
	} else {
		checks["p2p"] = "not configured"
	}

	response := ReadinessResponse{
		Ready:     allReady,
		Checks:    checks,
		Timestamp: time.Now().Unix(),
		Details:   details,
	}

	statusCode := http.StatusOK
	if !allReady {
		statusCode = http.StatusServiceUnavailable
	}

	writeJSONResponse(w, r, NewSuccessResponse(response), statusCode)
}

// checkStorage verifies storage is accessible
func (s *Server) checkStorage(ctx context.Context) error {
	if s.storage == nil {
		return NewUnavailableError("storage not initialized")
	}
	if err := s.storage.Ping(ctx); err != nil {
		return NewUnavailableError("storage ping failed")
	}
	return nil
}

// checkStateStore verifies state store is accessible
func (s *Server) checkStateStore(ctx context.Context) error {
	if s.stateStore == nil {
		return NewUnavailableError("state store not initialized")
	}

	// Verify we can get the latest version
	latest := s.stateStore.Latest()

	// Verify we can get the root for the latest version
	_, exists := s.stateStore.Root(latest)
	if !exists {
		return NewUnavailableError("state root not found")
	}

	return nil
}

// Helper functions for JSON responses

// writeJSONResponse writes a successful JSON response
func writeJSONResponse(w http.ResponseWriter, r *http.Request, response *Response, statusCode int) {
	w.Header().Set(HeaderContentType, ContentTypeJSON)
	w.WriteHeader(statusCode)

	// Add request ID to response if available
	if requestID := getRequestID(r.Context()); requestID != "" {
		if response.Meta == nil {
			response.Meta = &MetaDTO{}
		}
		response.Meta.RequestID = requestID
	}

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(response); err != nil {
		// Log encoding error but response already started
		// Can't change status code at this point
	}
}

// writeErrorResponse writes an error JSON response
func writeErrorResponse(w http.ResponseWriter, r *http.Request, code, message string, statusCode int) {
	requestID := getRequestID(r.Context())
	response := NewErrorResponseSimple(code, message, requestID)

	w.Header().Set(HeaderContentType, ContentTypeJSON)
	w.WriteHeader(statusCode)

	encoder := json.NewEncoder(w)
	encoder.Encode(response)
}

// writeErrorFromUtils writes an error response from utils.Error
func writeErrorFromUtils(w http.ResponseWriter, r *http.Request, err *utils.Error) {
	requestID := getRequestID(r.Context())
	response := NewErrorResponse(err, requestID)

	statusCode := err.GetHTTPStatus()
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}

	w.Header().Set(HeaderContentType, ContentTypeJSON)
	w.WriteHeader(statusCode)

	encoder := json.NewEncoder(w)
	encoder.Encode(response)
}
