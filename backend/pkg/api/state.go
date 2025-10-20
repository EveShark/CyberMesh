package api

import (
	"context"
	"net/http"
	"strings"

	"backend/pkg/utils"
)

// handleState handles GET /state/:key
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract key from path
	path := strings.TrimPrefix(r.URL.Path, s.config.BasePath+"/state/")
	key := strings.TrimPrefix(path, "/")

	if key == "" || key == "root" {
		writeErrorFromUtils(w, r, NewInvalidKeyError(key, "key cannot be empty"))
		return
	}

	s.handleStateByKey(w, r, key)
}

// handleStateRoot handles GET /state/root
func (s *Server) handleStateRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.stateStore == nil {
		writeErrorFromUtils(w, r, NewUnavailableError("state store"))
		return
	}

	// Get latest version
	latest := s.stateStore.Latest()

	// Get root for latest version
	root, exists := s.stateStore.Root(latest)
	if !exists {
		writeErrorFromUtils(w, r, NewStateNotFoundError("root"))
		return
	}

	response := StateRootResponse{
		Root:    encodeHex(root[:]),
		Version: latest,
		Height:  latest, // Assuming version == height
	}

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

// handleStateByKey handles GET /state/:key with optional version
func (s *Server) handleStateByKey(w http.ResponseWriter, r *http.Request, keyHex string) {
	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	if s.stateStore == nil {
		writeErrorFromUtils(w, r, NewUnavailableError("state store"))
		return
	}

	// Validate key
	if validationErr := validateStateKey(keyHex); validationErr != nil {
		if apiErr, ok := validationErr.(*utils.Error); ok {
			writeErrorFromUtils(w, r, apiErr)
		} else {
			writeErrorResponse(w, r, "INVALID_KEY", validationErr.Error(), http.StatusBadRequest)
		}
		return
	}

	// Parse key from hex
	key, err := parseHex(keyHex)
	if err != nil {
		writeErrorFromUtils(w, r, NewInvalidKeyError(keyHex, "invalid hex encoding"))
		return
	}

	// Get version parameter (optional)
	var version uint64
	versionStr := r.URL.Query().Get("version")
	if versionStr != "" {
		parsedVersion, err := parseUint64(versionStr)
		if err != nil {
			writeErrorFromUtils(w, r, NewInvalidParamsError("version", "must be valid uint64"))
			return
		}
		version = parsedVersion
	} else {
		// Use latest version
		version = s.stateStore.Latest()
	}

	// Get value from state store
	value, exists := s.stateStore.Get(version, key)
	if !exists {
		writeErrorFromUtils(w, r, NewStateNotFoundError(keyHex))
		return
	}

	// Build response
	response := StateResponse{
		Key:     keyHex,
		Value:   encodeHex(value),
		Version: version,
		// Proof is optional and not implemented in current state store
		// Proof: "",
	}

	s.logger.InfoContext(ctx, "state query",
		utils.ZapString("key", keyHex),
		utils.ZapUint64("version", version),
		utils.ZapInt("value_size", len(value)))

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}
