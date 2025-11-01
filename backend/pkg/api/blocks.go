package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/pkg/block"
	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
	"github.com/lib/pq"
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

	// Get latest block height from storage (database)
	latestHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get latest height", utils.ZapError(err))
		writeErrorFromUtils(w, r, NewInternalError("failed to get latest height"))
		return
	}

	// Get block from storage
	block, err := s.storage.GetBlock(ctx, latestHeight)
	if err != nil {
		if errors.Is(err, cockroach.ErrBlockNotFound) {
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
		// For parse errors, use 0 as placeholder for current height in error message
		latestHeight, _ := s.storage.GetLatestHeight(ctx)
		writeErrorFromUtils(w, r, NewInvalidHeightError(0, latestHeight))
		return
	}

	// Validate height - get current height from storage
	currentHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get latest height", utils.ZapError(err))
		writeErrorFromUtils(w, r, NewInternalError("failed to get latest height"))
		return
	}
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
		if errors.Is(err, cockroach.ErrBlockNotFound) {
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

	// Parse limit parameter first to apply sane defaults
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

	// Get current height from storage (database)
	currentHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get latest height", utils.ZapError(err))
		writeErrorFromUtils(w, r, NewInternalError("failed to get latest height"))
		return
	}

	// Parse start parameter (optional). If missing, default to latest window.
	startStr := query.Get("start")
	var start uint64
	if startStr == "" {
		if currentHeight+1 <= uint64(limit) {
			start = 0
		} else {
			start = currentHeight - uint64(limit) + 1
		}
	} else {
		parsedStart, err := parseUint64(startStr)
		if err != nil {
			writeErrorFromUtils(w, r, NewInvalidParamsError("start", "must be valid uint64"))
			return
		}
		start = parsedStart
	}

	if start > currentHeight {
		writeErrorFromUtils(w, r, NewInvalidHeightError(start, currentHeight))
		return
	}

	// Get minimum height from database to handle gaps in block heights
	minHeight, err := s.storage.GetMinHeight(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get min height", utils.ZapError(err))
		// Don't fail entirely, just use start as-is
		minHeight = start
	}

	// If requested start is before minimum height, adjust to first available block
	effectiveStart := start
	if start < minHeight {
		effectiveStart = minHeight
	}

	// Fetch blocks
	blocks := make([]BlockResponse, 0, limit)
	blockHeights := make([]uint64, 0, limit)
	fetchedCount := 0

	for height := effectiveStart; height <= currentHeight && fetchedCount < limit; height++ {
		block, err := s.storage.GetBlock(ctx, height)
		if err != nil {
			if errors.Is(err, cockroach.ErrBlockNotFound) {
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
		blockHeights = append(blockHeights, height)
		fetchedCount++
	}

	// SECURITY FIX: Batch query for anomaly counts (prevents N+1 DoS attack)
	anomalyCounts, err := s.batchGetAnomalyCounts(ctx, blockHeights)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get anomaly counts", utils.ZapError(err))
		// Don't fail - continue with 0 counts
		anomalyCounts = make(map[uint64]int)
	}

	// Apply anomaly counts to blocks
	for i := range blocks {
		blocks[i].AnomalyCount = anomalyCounts[blocks[i].Height]
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
// Note: AnomalyCount should be set separately via batch query for performance
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
		AnomalyCount:     0, // Set separately via batchGetAnomalyCounts
		SizeBytes:        0, // unknown without full serialization; keep 0
	}

	return resp
}

// batchGetAnomalyCounts retrieves anomaly counts for multiple blocks in a single query
// SECURITY: Prevents N+1 query DoS attack
func (s *Server) batchGetAnomalyCounts(ctx context.Context, heights []uint64) (map[uint64]int, error) {
	if len(heights) == 0 {
		return make(map[uint64]int), nil
	}

	// SECURITY: Add timeout protection
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Get concrete adapter to access database
	adapter, ok := s.storage.(interface {
		GetDB() *sql.DB
	})
	if !ok {
		return nil, fmt.Errorf("storage adapter does not support GetDB()")
	}

	// SECURITY: Use parameterized query with ANY clause to prevent SQL injection
	query := `
        SELECT block_height, COUNT(*)
        FROM transactions
        WHERE block_height = ANY($1::bigint[]) AND tx_type = 'evidence'
        GROUP BY block_height
    `

	// Convert []uint64 to []int64 for postgres array
	heightsInt64 := make([]int64, len(heights))
	for i, h := range heights {
		heightsInt64[i] = int64(h)
	}

	rows, err := adapter.GetDB().QueryContext(queryCtx, query, pq.Array(heightsInt64))
	if err != nil {
		return nil, fmt.Errorf("failed to query anomaly counts: %w", err)
	}
	defer rows.Close()

	result := make(map[uint64]int)
	for rows.Next() {
		var height uint64
		var count int
		if err := rows.Scan(&height, &count); err != nil {
			s.logger.WarnContext(ctx, "failed to scan anomaly count row", utils.ZapError(err))
			continue
		}
		result[height] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anomaly count rows: %w", err)
	}

	return result, nil
}
