package api

import (
	"time"

	"backend/pkg/utils"
)

// Standard API response wrapper
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorDTO   `json:"error,omitempty"`
	Meta    *MetaDTO    `json:"meta,omitempty"`
}

// ErrorDTO represents an API error
type ErrorDTO struct {
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

// MetaDTO contains response metadata
type MetaDTO struct {
	RequestID string `json:"request_id,omitempty"`
	Timestamp int64  `json:"timestamp"`
	Version   string `json:"version,omitempty"`
}

// PaginationDTO contains pagination information
type PaginationDTO struct {
	Start int    `json:"start"`
	Limit int    `json:"limit"`
	Total int    `json:"total"`
	Next  string `json:"next,omitempty"`
}

// Health & Readiness DTOs

// HealthResponse represents /health response
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
	Version   string `json:"version"`
}

// ReadinessResponse represents /ready response
type ReadinessResponse struct {
	Ready     bool                   `json:"ready"`
	Checks    map[string]string      `json:"checks"`
	Timestamp int64                  `json:"timestamp"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Phase     string                 `json:"phase,omitempty"`
}

// Block DTOs

// BlockResponse represents a block
type BlockResponse struct {
	Height           uint64                 `json:"height"`
	Hash             string                 `json:"hash"`
	ParentHash       string                 `json:"parent_hash"`
	StateRoot        string                 `json:"state_root"`
	Timestamp        int64                  `json:"timestamp"`
	Proposer         string                 `json:"proposer"`
	TransactionCount int                    `json:"transaction_count"`
	SizeBytes        int                    `json:"size_bytes"`
	Transactions     []TransactionResponse  `json:"transactions,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// TransactionResponse represents a transaction in a block
type TransactionResponse struct {
	Hash      string                 `json:"hash"`
	Type      string                 `json:"type"`
	SizeBytes int                    `json:"size_bytes"`
	Timestamp int64                  `json:"timestamp,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// BlockListResponse represents a list of blocks
type BlockListResponse struct {
	Blocks     []BlockResponse `json:"blocks"`
	Pagination *PaginationDTO  `json:"pagination"`
}

// State DTOs

// StateResponse represents a state key-value pair
type StateResponse struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Version uint64 `json:"version"`
	Proof   string `json:"proof,omitempty"`
}

// StateRootResponse represents the current state root
type StateRootResponse struct {
	Root    string `json:"root"`
	Version uint64 `json:"version"`
	Height  uint64 `json:"height"`
}

// Validator DTOs

// ValidatorResponse represents a validator
type ValidatorResponse struct {
	ID               string  `json:"id"`
	PublicKey        string  `json:"public_key"`
	VotingPower      int64   `json:"voting_power"`
	Status           string  `json:"status"`
	ProposedBlocks   uint64  `json:"proposed_blocks,omitempty"`
	UptimePercentage float64 `json:"uptime_percentage,omitempty"`
	LastActiveHeight uint64  `json:"last_active_height,omitempty"`
	JoinedAtHeight   uint64  `json:"joined_at_height,omitempty"`
}

// ValidatorListResponse represents a list of validators
type ValidatorListResponse struct {
	Validators []ValidatorResponse `json:"validators"`
	Total      int                 `json:"total"`
}

// Statistics DTOs

// StatsResponse represents system statistics
type StatsResponse struct {
	Chain     *ChainStats     `json:"chain"`
	Consensus *ConsensusStats `json:"consensus"`
	Mempool   *MempoolStats   `json:"mempool"`
	Network   *NetworkStats   `json:"network,omitempty"`
}

// ChainStats represents blockchain statistics
type ChainStats struct {
	Height            uint64  `json:"height"`
	StateVersion      uint64  `json:"state_version"`
	TotalTransactions uint64  `json:"total_transactions"`
	AvgBlockTime      float64 `json:"avg_block_time_seconds,omitempty"`
	AvgBlockSize      int     `json:"avg_block_size_bytes,omitempty"`
}

// ConsensusStats represents consensus statistics
type ConsensusStats struct {
	View           uint64 `json:"view"`
	Round          uint64 `json:"round"`
	ValidatorCount int    `json:"validator_count"`
	QuorumSize     int    `json:"quorum_size"`
	CurrentLeader  string `json:"current_leader,omitempty"`
}

// MempoolStats represents mempool statistics
type MempoolStats struct {
	PendingTransactions int   `json:"pending_transactions"`
	SizeBytes           int64 `json:"size_bytes"`
	OldestTxAge         int64 `json:"oldest_tx_age_seconds,omitempty"`
}

// NetworkStats represents network statistics
type NetworkStats struct {
	PeerCount     int     `json:"peer_count"`
	InboundPeers  int     `json:"inbound_peers"`
	OutboundPeers int     `json:"outbound_peers"`
	BytesReceived uint64  `json:"bytes_received"`
	BytesSent     uint64  `json:"bytes_sent"`
	AvgLatencyMs  float64 `json:"avg_latency_ms,omitempty"`
}

// Request parameter DTOs

// BlockQueryParams represents block query parameters
type BlockQueryParams struct {
	Height     *uint64 // specific height
	IncludeTxs bool    // include transaction details
	Start      uint64  // range query start
	Limit      int     // range query limit (max 100)
}

// StateQueryParams represents state query parameters
type StateQueryParams struct {
	Key     string  // state key (hex-encoded)
	Version *uint64 // specific version (optional, default: latest)
}

// ValidatorQueryParams represents validator query parameters
type ValidatorQueryParams struct {
	Status string // filter by status (active, inactive, all)
}

// Helper functions for creating responses

// NewSuccessResponse creates a success response
func NewSuccessResponse(data interface{}) *Response {
	return &Response{
		Success: true,
		Data:    data,
		Meta: &MetaDTO{
			Timestamp: time.Now().Unix(),
		},
	}
}

// NewErrorResponse creates an error response from utils.Error
func NewErrorResponse(err *utils.Error, requestID string) *Response {
	return &Response{
		Success: false,
		Error: &ErrorDTO{
			Code:      string(err.Code),
			Message:   err.Message,
			Details:   err.Details,
			RequestID: requestID,
			Timestamp: time.Now().Unix(),
		},
	}
}

// NewErrorResponseSimple creates a simple error response
func NewErrorResponseSimple(code, message, requestID string) *Response {
	return &Response{
		Success: false,
		Error: &ErrorDTO{
			Code:      code,
			Message:   message,
			RequestID: requestID,
			Timestamp: time.Now().Unix(),
		},
	}
}

// NewPaginatedResponse creates a paginated response
func NewPaginatedResponse(data interface{}, pagination *PaginationDTO) *Response {
	return &Response{
		Success: true,
		Data: map[string]interface{}{
			"items":      data,
			"pagination": pagination,
		},
		Meta: &MetaDTO{
			Timestamp: time.Now().Unix(),
		},
	}
}

// Rate limit header constants
const (
	HeaderRateLimitLimit     = "X-RateLimit-Limit"
	HeaderRateLimitRemaining = "X-RateLimit-Remaining"
	HeaderRateLimitReset     = "X-RateLimit-Reset"
	HeaderRequestID          = "X-Request-ID"
	HeaderContentType        = "Content-Type"
)

// Content type constants
const (
	ContentTypeJSON = "application/json"
	ContentTypeText = "text/plain"
)
