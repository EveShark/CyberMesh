package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

    "backend/pkg/block"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
)

// handleBlocks handles GET /blocks and GET /blocks/:height
func (s *Server) handleBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if this is a specific block query
	path := strings.TrimPrefix(r.URL.Path, s.config.BasePath+"/blocks")
	path = strings.TrimPrefix(path, "/")

	if path == "" || path == "?" || strings.HasPrefix(path, "?") {
		// List blocks (range query)
		s.handleBlockList(w, r)
		return
	}

	// Single block by height
	s.handleBlockByHeight(w, r, path)
}

// handleBlockLatest handles GET /blocks/latest
func (s *Server) handleBlockLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	// Get latest block height from state store
	if s.stateStore == nil {
		writeErrorFromUtils(w, r, NewUnavailableError("state store"))
		return
	}

	latestHeight := s.stateStore.Latest()

	// Get block from storage
	block, err := s.storage.GetBlock(ctx, latestHeight)
	if err != nil {
		if err == cockroach.ErrBlockNotFound {
			writeErrorFromUtils(w, r, NewBlockNotFoundError(latestHeight))
			return
		}

		s.logger.ErrorContext(ctx, "failed to get latest block",
			utils.ZapUint64("height", latestHeight),
			utils.ZapError(err))

		writeErrorFromUtils(w, r, NewInternalError("failed to retrieve block"))
		return
	}

	// Convert to response DTO
	response := s.blockToResponse(block)

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

// handleBlockByHeight handles GET /blocks/:height
func (s *Server) handleBlockByHeight(w http.ResponseWriter, r *http.Request, heightStr string) {
	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	// Parse height
	height, err := parseUint64(heightStr)
	if err != nil {
		writeErrorFromUtils(w, r, NewInvalidHeightError(0, s.stateStore.Latest()))
		return
	}

	// Validate height
	currentHeight := s.stateStore.Latest()
	if validationErr := validateBlockHeight(height, currentHeight); validationErr != nil {
		if apiErr, ok := validationErr.(*utils.Error); ok {
			writeErrorFromUtils(w, r, apiErr)
		} else {
			writeErrorResponse(w, r, "INVALID_HEIGHT", validationErr.Error(), http.StatusBadRequest)
		}
		return
	}

    // Check if client wants transaction details
    includeTxs := parseBool(r.URL.Query().Get("include_txs"))

	// Get block from storage
	block, err := s.storage.GetBlock(ctx, height)
	if err != nil {
		if err == cockroach.ErrBlockNotFound {
			writeErrorFromUtils(w, r, NewBlockNotFoundError(height))
			return
		}

		s.logger.ErrorContext(ctx, "failed to get block",
			utils.ZapUint64("height", height),
			utils.ZapError(err))

		writeErrorFromUtils(w, r, NewInternalError("failed to retrieve block"))
		return
	}

    // Convert to response DTO
    response := s.blockToResponse(block)
    if includeTxs {
        metas, err := s.storage.ListTransactionsByBlock(ctx, height)
        if err != nil {
            s.logger.WarnContext(ctx, "failed to list transactions for block",
                utils.ZapUint64("height", height), utils.ZapError(err))
            writeErrorFromUtils(w, r, NewInternalError("failed to list transactions"))
            return
        }
        txs := make([]TransactionResponse, 0, len(metas))
        for _, m := range metas {
            // m.TxHash may be []byte (expected 32 bytes)
            txs = append(txs, TransactionResponse{
                Hash:      encodeHex(m.TxHash),
                Type:      m.TxType,
                SizeBytes: m.SizeBytes,
            })
        }
        response.Transactions = txs
    }

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

// handleBlockList handles GET /blocks?start=:start&limit=:limit
func (s *Server) handleBlockList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	query := r.URL.Query()

	// Parse start parameter
	startStr := query.Get("start")
	if startStr == "" {
		writeErrorFromUtils(w, r, NewInvalidParamsError("start", "required parameter"))
		return
	}

	start, err := parseUint64(startStr)
	if err != nil {
		writeErrorFromUtils(w, r, NewInvalidParamsError("start", "must be valid uint64"))
		return
	}

	// Parse limit parameter
	limitStr := query.Get("limit")
	limit := 10 // default
	if limitStr != "" {
		parsedLimit, err := parseInt(limitStr)
		if err != nil {
			writeErrorFromUtils(w, r, NewInvalidParamsError("limit", "must be valid integer"))
			return
		}
		limit, _ = validatePaginationLimit(parsedLimit)
	}

	// Validate start height
	currentHeight := s.stateStore.Latest()
	if start > currentHeight {
		writeErrorFromUtils(w, r, NewInvalidHeightError(start, currentHeight))
		return
	}

	// Fetch blocks
	blocks := make([]BlockResponse, 0, limit)
	fetchedCount := 0

	for height := start; height <= currentHeight && fetchedCount < limit; height++ {
		block, err := s.storage.GetBlock(ctx, height)
		if err != nil {
			if err == cockroach.ErrBlockNotFound {
				// Skip missing blocks (shouldn't happen but handle gracefully)
				continue
			}

			s.logger.ErrorContext(ctx, "failed to get block in range",
				utils.ZapUint64("height", height),
				utils.ZapError(err))

			// Continue with partial results rather than failing entirely
			break
		}

		blocks = append(blocks, s.blockToResponse(block))
		fetchedCount++
	}

	// Build pagination info
	pagination := &PaginationDTO{
		Start: int(start),
		Limit: limit,
		Total: int(currentHeight + 1), // +1 because height is 0-indexed
	}

	// Add next link if there are more blocks
	if start+uint64(limit) <= currentHeight {
		nextStart := start + uint64(limit)
		pagination.Next = s.config.BasePath + "/blocks?start=" + strconv.FormatUint(nextStart, 10) + "&limit=" + strconv.Itoa(limit)
	}

	response := BlockListResponse{
		Blocks:     blocks,
		Pagination: pagination,
	}

	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

// blockToResponse converts a block to BlockResponse DTO
func (s *Server) blockToResponse(b *block.AppBlock) BlockResponse {
    // Map real fields from AppBlock; no payloads are exposed
    h := b.GetHash()
    ph := b.GetParentHash()
    sr := b.StateRootHint()
    proposer := b.Proposer()

    resp := BlockResponse{
        Height:           b.GetHeight(),
        Hash:             encodeHex(h[:]),
        ParentHash:       encodeHex(ph[:]),
        StateRoot:        encodeHex(sr[:]),
        Timestamp:        b.GetTimestamp().Unix(),
        Proposer:         encodeHex(proposer[:]),
        TransactionCount: b.GetTransactionCount(),
        SizeBytes:        0, // unknown without full serialization; keep 0
    }

    return resp
}
