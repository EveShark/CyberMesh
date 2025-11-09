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

// ReadinessCheckResult represents a single readiness check result
type ReadinessCheckResult struct {
	Status    string  `json:"status"`
	LatencyMs float64 `json:"latency_ms"`
	Message   string  `json:"message,omitempty"`
}

// ReadinessResponse represents /ready response
type ReadinessResponse struct {
	Ready     bool                            `json:"ready"`
	Checks    map[string]ReadinessCheckResult `json:"checks"`
	Timestamp int64                           `json:"timestamp"`
	Details   map[string]interface{}          `json:"details,omitempty"`
	Phase     string                          `json:"phase,omitempty"`
	Warnings  []string                        `json:"warnings,omitempty"`
}

// Block DTOs

// BlockResponse represents a block
type BlockResponse struct {
	Height             uint64                 `json:"height"`
	Hash               string                 `json:"hash"`
	ParentHash         string                 `json:"parent_hash"`
	StateRoot          string                 `json:"state_root"`
	Timestamp          int64                  `json:"timestamp"`
	Proposer           string                 `json:"proposer"`
	TransactionCount   int                    `json:"transaction_count"`
	AnomalyCount       int                    `json:"anomaly_count"`
	SizeBytes          int                    `json:"size_bytes"`
	SizeBytesEstimated bool                   `json:"size_bytes_estimated,omitempty"`
	Transactions       []TransactionResponse  `json:"transactions,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
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
	Kafka     *KafkaStats     `json:"kafka,omitempty"`
	Storage   *StorageStats   `json:"storage,omitempty"`
	Redis     *RedisStats     `json:"redis,omitempty"`
	AI        *AIStats        `json:"ai_service,omitempty"`
}

// ChainStats represents blockchain statistics
type ChainStats struct {
	Height            uint64  `json:"height"`
	StateVersion      uint64  `json:"state_version"`
	TotalTransactions uint64  `json:"total_transactions"`
	SuccessRate       float64 `json:"success_rate"`
	AvgBlockTime      float64 `json:"avg_block_time_seconds,omitempty"`
	AvgBlockSize      int     `json:"avg_block_size_bytes,omitempty"`
}

// ConsensusStats represents consensus statistics
type ConsensusStats struct {
	View               uint64 `json:"view"`
	Round              uint64 `json:"round"`
	ValidatorCount     int    `json:"validator_count"`
	QuorumSize         int    `json:"quorum_size"`
	CurrentLeader      string `json:"current_leader,omitempty"`
	CurrentLeaderID    string `json:"current_leader_id,omitempty"`
	CurrentLeaderAlias string `json:"current_leader_alias,omitempty"`
}

// MempoolStats represents mempool statistics
type MempoolStats struct {
	PendingTransactions int     `json:"pending_transactions"`
	SizeBytes           int64   `json:"size_bytes"`
	OldestTxAge         int64   `json:"oldest_tx_age_seconds,omitempty"`
	OldestTxAgeMs       float64 `json:"oldest_tx_age_ms,omitempty"`
}

// NetworkStats represents network statistics
type NetworkStats struct {
	PeerCount             int                  `json:"peer_count"`
	InboundPeers          int                  `json:"inbound_peers"`
	OutboundPeers         int                  `json:"outbound_peers"`
	BytesReceived         uint64               `json:"bytes_received"`
	BytesSent             uint64               `json:"bytes_sent"`
	AvgLatencyMs          float64              `json:"avg_latency_ms,omitempty"`
	InboundThroughputBps  float64              `json:"inbound_throughput_bytes_per_second,omitempty"`
	OutboundThroughputBps float64              `json:"outbound_throughput_bytes_per_second,omitempty"`
	Timestamp             time.Time            `json:"timestamp"`
	History               []NetworkTrendSample `json:"history,omitempty"`
}

// NetworkTrendSample captures historical network telemetry for charting.
type NetworkTrendSample struct {
	Timestamp     time.Time `json:"timestamp"`
	PeerCount     int       `json:"peer_count"`
	InboundPeers  int       `json:"inbound_peers"`
	OutboundPeers int       `json:"outbound_peers"`
	AvgLatencyMs  float64   `json:"avg_latency_ms"`
	BytesReceived uint64    `json:"bytes_received"`
	BytesSent     uint64    `json:"bytes_sent"`
}

// KafkaStats represents Kafka telemetry
type KafkaStats struct {
	Status                   string           `json:"status"`
	ReadyLatencyMs           float64          `json:"ready_latency_ms"`
	PublishLatencyMs         float64          `json:"publish_latency_ms,omitempty"`
	PublishSuccesses         uint64           `json:"publish_successes"`
	PublishFailures          uint64           `json:"publish_failures"`
	BrokerCount              int              `json:"broker_count,omitempty"`
	Topics                   []string         `json:"topics,omitempty"`
	LastPartition            *int32           `json:"last_publish_partition,omitempty"`
	LastOffset               *int64           `json:"last_publish_offset,omitempty"`
	LastPublishUnix          *int64           `json:"last_publish_unix,omitempty"`
	LastError                string           `json:"last_error,omitempty"`
	ConsumerGroup            string           `json:"consumer_group,omitempty"`
	ConsumerTopics           []string         `json:"consumer_topics,omitempty"`
	ConsumerTopicPartitions  map[string]int   `json:"consumer_topic_partitions,omitempty"`
	ConsumerOffsets          map[string]int64 `json:"consumer_partition_offsets,omitempty"`
	ConsumerLag              map[string]int64 `json:"consumer_partition_lag,omitempty"`
	ConsumerHighWater        map[string]int64 `json:"consumer_partition_highwater,omitempty"`
	ConsumerAssigned         int              `json:"consumer_assigned_partitions,omitempty"`
	ConsumerConsumed         uint64           `json:"consumer_messages_consumed,omitempty"`
	ConsumerVerified         uint64           `json:"consumer_messages_verified,omitempty"`
	ConsumerAdmitted         uint64           `json:"consumer_messages_admitted,omitempty"`
	ConsumerFailed           uint64           `json:"consumer_messages_failed,omitempty"`
	ConsumerLastUnix         *int64           `json:"consumer_last_message_unix,omitempty"`
	PublishLatencyP50Ms      float64          `json:"publish_latency_p50_ms,omitempty"`
	PublishLatencyP95Ms      float64          `json:"publish_latency_p95_ms,omitempty"`
	PublishLatencyHistogram  []LatencyBucket  `json:"publish_latency_histogram,omitempty"`
	ConsumerIngestP95Ms      float64          `json:"consumer_ingest_latency_p95_ms,omitempty"`
	ConsumerProcessP95Ms     float64          `json:"consumer_process_latency_p95_ms,omitempty"`
	ConsumerIngestHistogram  []LatencyBucket  `json:"consumer_ingest_latency_histogram,omitempty"`
	ConsumerProcessHistogram []LatencyBucket  `json:"consumer_process_latency_histogram,omitempty"`
}

// LatencyBucket represents a histogram bucket exported via the API.
type LatencyBucket struct {
	UpperBoundMs float64 `json:"upper_bound_ms"`
	Count        uint64  `json:"count"`
}

// StorageStats represents CockroachDB telemetry
type StorageStats struct {
	Status                      string            `json:"status"`
	ReadyLatencyMs              float64           `json:"ready_latency_ms"`
	PoolOpenConnections         int               `json:"pool_open_connections,omitempty"`
	PoolInUse                   int               `json:"pool_in_use,omitempty"`
	PoolIdle                    int               `json:"pool_idle,omitempty"`
	PoolWaitSeconds             float64           `json:"pool_wait_seconds_total,omitempty"`
	PoolWaitTotal               int64             `json:"pool_wait_total,omitempty"`
	Database                    string            `json:"database,omitempty"`
	Version                     string            `json:"version,omitempty"`
	NodeID                      int64             `json:"node_id,omitempty"`
	QueryLatencyP95Ms           float64           `json:"query_latency_p95_ms,omitempty"`
	QueryLatencyHistogram       []LatencyBucket   `json:"query_latency_histogram,omitempty"`
	TransactionLatencyP95Ms     float64           `json:"transaction_latency_p95_ms,omitempty"`
	TransactionLatencyHistogram []LatencyBucket   `json:"transaction_latency_histogram,omitempty"`
	SlowQueryCount              uint64            `json:"slow_query_count,omitempty"`
	SlowTransactionCount        uint64            `json:"slow_transaction_count,omitempty"`
	SlowQueries                 map[string]uint64 `json:"slow_queries,omitempty"`
	SlowTransactions            map[string]uint64 `json:"slow_transactions,omitempty"`
}

// RedisStats represents Redis telemetry
type RedisStats struct {
	Status                  string          `json:"status"`
	ReadyLatencyMs          float64         `json:"ready_latency_ms"`
	Mode                    string          `json:"mode,omitempty"`
	Role                    string          `json:"role,omitempty"`
	Version                 string          `json:"version,omitempty"`
	ConnectedClients        int             `json:"connected_clients,omitempty"`
	OpsPerSec               int             `json:"ops_per_sec,omitempty"`
	UptimeSeconds           int64           `json:"uptime_seconds,omitempty"`
	TotalConnections        int64           `json:"total_connections_received,omitempty"`
	TotalCommands           int64           `json:"total_commands_processed,omitempty"`
	Message                 string          `json:"message,omitempty"`
	CommandLatencyP95Ms     float64         `json:"command_latency_p95_ms,omitempty"`
	CommandLatencyHistogram []LatencyBucket `json:"command_latency_histogram,omitempty"`
	CommandErrors           uint64          `json:"command_errors_total,omitempty"`
}

// AIStats represents AI service telemetry
type AIStats struct {
	Status                 string   `json:"status"`
	ReadyLatencyMs         float64  `json:"ready_latency_ms"`
	State                  string   `json:"state,omitempty"`
	DetectionLoopRunning   bool     `json:"detection_loop_running"`
	DetectionLoopStatus    string   `json:"detection_loop_status,omitempty"`
	DetectionLoopBlocking  bool     `json:"detection_loop_blocking,omitempty"`
	DetectionLoopHealthy   bool     `json:"detection_loop_healthy,omitempty"`
	DetectionLoopIssues    []string `json:"detection_loop_issues,omitempty"`
	DetectionLoopLatencyMs float64  `json:"detection_loop_avg_latency_ms,omitempty"`
	DetectionLoopLastMs    float64  `json:"detection_loop_last_latency_ms,omitempty"`
	DetectionsPerMinute    *float64 `json:"detections_per_minute,omitempty"`
	PublishRatePerMinute   *float64 `json:"publish_rate_per_minute,omitempty"`
	IterationsPerMinute    *float64 `json:"iterations_per_minute,omitempty"`
	ErrorRatePerHour       *float64 `json:"error_rate_per_hour,omitempty"`
	PublishSuccessRatio    *float64 `json:"publish_success_ratio,omitempty"`
	SecondsSinceDetection  *float64 `json:"seconds_since_last_detection,omitempty"`
	SecondsSinceIteration  *float64 `json:"seconds_since_last_iteration,omitempty"`
	CacheAgeSeconds        *float64 `json:"cache_age_seconds,omitempty"`
	Message                string   `json:"message,omitempty"`
}

// AIMetricsResponse represents detailed AI engine metrics
type AIMetricsResponse struct {
	Status   string                  `json:"status"`
	State    string                  `json:"state"`
	Message  string                  `json:"message,omitempty"`
	Loop     *AiDetectionLoopSummary `json:"loop,omitempty"`
	Derived  *AiMetricsDerived       `json:"derived,omitempty"`
	Engines  []AiEngineMetric        `json:"engines"`
	Variants []AiVariantMetric       `json:"variants"`
}

// AiDetectionLoopSummary captures runtime loop metrics
type AiDetectionLoopSummary struct {
	Running                   bool                     `json:"running"`
	Status                    string                   `json:"status,omitempty"`
	Message                   string                   `json:"message,omitempty"`
	Issues                    []string                 `json:"issues,omitempty"`
	Blocking                  bool                     `json:"blocking,omitempty"`
	Healthy                   bool                     `json:"healthy,omitempty"`
	AvgLatencyMs              float64                  `json:"avg_latency_ms"`
	LastLatencyMs             float64                  `json:"last_latency_ms"`
	SecondsSinceLastDetection *float64                 `json:"seconds_since_last_detection,omitempty"`
	SecondsSinceLastIteration *float64                 `json:"seconds_since_last_iteration,omitempty"`
	CacheAgeSeconds           *float64                 `json:"cache_age_seconds,omitempty"`
	Counters                  *AiDetectionLoopCounters `json:"counters,omitempty"`
}

type AiDetectionLoopCounters struct {
	DetectionsTotal       uint64 `json:"detections_total"`
	DetectionsPublished   uint64 `json:"detections_published"`
	DetectionsRateLimited uint64 `json:"detections_rate_limited"`
	Errors                uint64 `json:"errors"`
	LoopIterations        uint64 `json:"loop_iterations"`
}

// AiMetricsDerived contains derived KPIs
type AiMetricsDerived struct {
	DetectionsPerMinute  *float64 `json:"detections_per_minute,omitempty"`
	PublishRatePerMinute *float64 `json:"publish_rate_per_minute,omitempty"`
	IterationsPerMinute  *float64 `json:"iterations_per_minute,omitempty"`
	ErrorRatePerHour     *float64 `json:"error_rate_per_hour,omitempty"`
	PublishSuccessRatio  *float64 `json:"publish_success_ratio,omitempty"`
}

// AiEngineMetric captures per-engine analytics
type AiEngineMetric struct {
	Engine              string   `json:"engine"`
	Ready               bool     `json:"ready"`
	Candidates          int64    `json:"candidates"`
	Published           int64    `json:"published"`
	PublishRatio        float64  `json:"publish_ratio"`
	ThroughputPerMinute float64  `json:"throughput_per_minute"`
	AvgConfidence       *float64 `json:"avg_confidence,omitempty"`
	ThreatTypes         []string `json:"threat_types,omitempty"`
	LastLatencyMs       *float64 `json:"last_latency_ms,omitempty"`
	LastUpdated         *float64 `json:"last_updated,omitempty"`
}

// AiVariantMetric captures per-variant analytics
type AiVariantMetric struct {
	Variant             string   `json:"variant"`
	Engines             []string `json:"engines,omitempty"`
	Total               int64    `json:"total"`
	Published           int64    `json:"published"`
	PublishRatio        float64  `json:"publish_ratio"`
	ThroughputPerMinute float64  `json:"throughput_per_minute"`
	AvgConfidence       *float64 `json:"avg_confidence,omitempty"`
	ThreatTypes         []string `json:"threat_types,omitempty"`
	LastUpdated         *float64 `json:"last_updated,omitempty"`
}

// AiDetectionHistoryEntry represents a single detection event from the AI service
type AiDetectionHistoryEntry struct {
	Timestamp     string                 `json:"timestamp"`
	Source        string                 `json:"source"`
	ValidatorID   string                 `json:"validator_id,omitempty"`
	ThreatType    string                 `json:"threat_type"`
	Severity      int                    `json:"severity"`
	Confidence    float64                `json:"confidence"`
	FinalScore    float64                `json:"final_score"`
	ShouldPublish bool                   `json:"should_publish"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// AiDetectionHistoryResponse wraps history entries
type AiDetectionHistoryResponse struct {
	Detections []AiDetectionHistoryEntry `json:"detections"`
	Count      int                       `json:"count"`
	UpdatedAt  string                    `json:"updated_at"`
}

// AiSuspiciousNode mirrors AI service suspicious node payload
type AiSuspiciousNode struct {
	ID             string   `json:"id"`
	Status         string   `json:"status"`
	SuspicionScore float64  `json:"suspicion_score"`
	EventCount     int      `json:"event_count"`
	LastSeen       string   `json:"last_seen"`
	ThreatTypes    []string `json:"threat_types,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	Uptime         float64  `json:"uptime,omitempty"`
}

// AiSuspiciousNodesResponse wraps AI suspicious node data
type AiSuspiciousNodesResponse struct {
	Nodes     []AiSuspiciousNode `json:"nodes"`
	UpdatedAt string             `json:"updated_at"`
}

// Network overview DTOs

// NetworkNodeDTO represents a validator/node entry in the network overview response.
type NetworkNodeDTO struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	Latency         float64    `json:"latency"`
	Uptime          float64    `json:"uptime"`
	ThroughputBytes uint64     `json:"throughput_bytes"`
	LastSeen        *time.Time `json:"last_seen,omitempty"`
	InboundRateBps  float64    `json:"inbound_rate_bps,omitempty"`
}

// NetworkEdgeDTO represents an adjacency between nodes in the topology graph.
type NetworkEdgeDTO struct {
	Source     string     `json:"source"`
	Target     string     `json:"target"`
	Direction  string     `json:"direction"`
	Status     string     `json:"status"`
	Confidence string     `json:"confidence,omitempty"`
	ReportedBy string     `json:"reported_by,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

// VotingStatusDTO indicates whether a validator is currently participating in consensus voting.
type VotingStatusDTO struct {
	NodeID string `json:"node_id"`
	Voting bool   `json:"voting"`
}

// NetworkOverviewResponse aggregates network health and validator participation data.
type NetworkOverviewResponse struct {
	ConnectedPeers   int               `json:"connected_peers"`
	TotalPeers       int               `json:"total_peers"`
	ExpectedPeers    int               `json:"expected_peers"`
	AverageLatencyMs float64           `json:"average_latency_ms"`
	ConsensusRound   uint64            `json:"consensus_round"`
	LeaderStability  float64           `json:"leader_stability"`
	Phase            string            `json:"phase"`
	Leader           string            `json:"leader,omitempty"`
	LeaderID         string            `json:"leader_id,omitempty"`
	Nodes            []NetworkNodeDTO  `json:"nodes"`
	VotingStatus     []VotingStatusDTO `json:"voting_status"`
	Edges            []NetworkEdgeDTO  `json:"edges,omitempty"`
	Self             string            `json:"self,omitempty"`
	InboundRateBps   float64           `json:"inbound_rate_bps,omitempty"`
	OutboundRateBps  float64           `json:"outbound_rate_bps,omitempty"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// Consensus overview DTOs

// ConsensusProposalDTO describes a recent proposal observation.
type ConsensusProposalDTO struct {
	Block     uint64 `json:"block"`
	View      uint64 `json:"view"`
	Hash      string `json:"hash,omitempty"`
	Proposer  string `json:"proposer,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// ConsensusVoteDTO aggregates vote-related metrics for visualization.
type ConsensusVoteDTO struct {
	Type      string `json:"type"`
	Count     uint64 `json:"count"`
	Timestamp int64  `json:"timestamp"`
}

// SuspiciousNodeDTO represents a validator flagged for potential issues.
type SuspiciousNodeDTO struct {
	ID             string  `json:"id"`
	Status         string  `json:"status"`
	Uptime         float64 `json:"uptime"`
	SuspicionScore float64 `json:"suspicion_score"`
	Reason         string  `json:"reason,omitempty"`
}

// ConsensusOverviewResponse aggregates consensus status and anomaly indicators.
type ConsensusOverviewResponse struct {
	Leader          string                 `json:"leader,omitempty"`
	LeaderID        string                 `json:"leader_id,omitempty"`
	Term            uint64                 `json:"term"`
	Phase           string                 `json:"phase"`
	ActivePeers     int                    `json:"active_peers"`
	QuorumSize      int                    `json:"quorum_size"`
	Proposals       []ConsensusProposalDTO `json:"proposals"`
	Votes           []ConsensusVoteDTO     `json:"votes"`
	SuspiciousNodes []SuspiciousNodeDTO    `json:"suspicious_nodes"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// SuspiciousNodesResponse is returned by /anomalies/suspicious-nodes.
type SuspiciousNodesResponse struct {
	Nodes     []SuspiciousNodeDTO `json:"nodes"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// DashboardOverviewResponse is the single snapshot consumed by the dashboard UI.
// It merges runtime health, ledger state, validator/consensus insights, recent blocks
// (including transaction metadata), and threat/AI telemetry into one payload so the
// frontend can hydrate every tab without additional polling.
type DashboardOverviewResponse struct {
	Timestamp  int64                       `json:"timestamp"`
	Backend    DashboardBackendSection     `json:"backend"`
	Ledger     *DashboardLedgerSection     `json:"ledger,omitempty"`
	Validators *DashboardValidatorsSection `json:"validators,omitempty"`
	Network    *NetworkOverviewResponse    `json:"network,omitempty"`
	Consensus  *ConsensusOverviewResponse  `json:"consensus,omitempty"`
	Blocks     DashboardBlocksSection      `json:"blocks"`
	Threats    DashboardThreatsSection     `json:"threats"`
	AI         DashboardAISection          `json:"ai"`
}

// DashboardLedgerSection captures the current ledger snapshot derived from the
// latest state version along with mempool pressure indicators.
type DashboardLedgerSection struct {
	LatestHeight             uint64  `json:"latest_height"`
	StateVersion             uint64  `json:"state_version"`
	TotalTransactions        uint64  `json:"total_transactions"`
	AvgBlockTimeSeconds      float64 `json:"avg_block_time_seconds"`
	AvgBlockSizeBytes        int     `json:"avg_block_size_bytes"`
	StateRoot                string  `json:"state_root,omitempty"`
	LastBlockHash            string  `json:"last_block_hash,omitempty"`
	SnapshotBlockHeight      uint64  `json:"snapshot_block_height"`
	SnapshotTransactionCount int     `json:"snapshot_transaction_count"`
	SnapshotTimestamp        int64   `json:"snapshot_timestamp"`
	ReputationChanges        int     `json:"reputation_changes"`
	PolicyChanges            int     `json:"policy_changes"`
	QuarantineChanges        int     `json:"quarantine_changes"`
	PendingTransactions      int     `json:"pending_transactions"`
	MempoolSizeBytes         int64   `json:"mempool_size_bytes"`
	MempoolOldestTxAgeMs     float64 `json:"mempool_oldest_tx_age_ms"`
}

// DashboardValidatorsSection summarizes validator activity and exposes a
// lightweight snapshot for UI drill-downs.
type DashboardValidatorsSection struct {
	Total      int                          `json:"total"`
	Active     int                          `json:"active"`
	Inactive   int                          `json:"inactive"`
	Validators []DashboardValidatorSnapshot `json:"validators"`
	UpdatedAt  int64                        `json:"updated_at"`
}

// DashboardValidatorSnapshot extends the public validator payload with alias,
// uptime, and liveness metadata needed for the consolidated dashboard views.
type DashboardValidatorSnapshot struct {
	ValidatorResponse
	Alias        string `json:"alias"`
	PeerID       string `json:"peer_id,omitempty"`
	LastSeenUnix int64  `json:"last_seen_unix,omitempty"`
}

type DashboardBackendSection struct {
	Health    HealthResponse                  `json:"health"`
	Readiness ReadinessResponse               `json:"readiness"`
	Stats     StatsResponse                   `json:"stats"`
	Metrics   DashboardBackendMetrics         `json:"metrics"`
	Derived   DashboardBackendDerived         `json:"derived"`
	History   []DashboardBackendHistorySample `json:"history"`
}
type DashboardBackendDerived struct {
	MempoolLatencyMs      *float64 `json:"mempool_latency_ms,omitempty"`
	ConsensusLatencyMs    *float64 `json:"consensus_latency_ms,omitempty"`
	P2PLatencyMs          *float64 `json:"p2p_latency_ms,omitempty"`
	AiLoopStatus          string   `json:"ai_loop_status,omitempty"`
	AiLoopStatusUpdatedAt *int64   `json:"ai_loop_status_updated_at,omitempty"`
	AiLoopBlocking        *bool    `json:"ai_loop_blocking,omitempty"`
	AiLoopIssues          string   `json:"ai_loop_issues,omitempty"`
	ThreatDataSource      string   `json:"threat_data_source,omitempty"`
	ThreatSourceUpdatedAt *int64   `json:"threat_source_updated_at,omitempty"`
	ThreatFallbackCount   *uint64  `json:"threat_fallback_count,omitempty"`
	ThreatFallbackReason  string   `json:"threat_fallback_reason,omitempty"`
	ThreatFallbackAt      *int64   `json:"threat_fallback_at,omitempty"`
}

type DashboardBackendHistorySample struct {
	Timestamp            int64   `json:"timestamp"`
	CPUSecondsTotal      float64 `json:"cpu_seconds_total"`
	CPUPercent           float64 `json:"cpu_percent"`
	ResidentMemoryBytes  uint64  `json:"resident_memory_bytes"`
	HeapAllocBytes       uint64  `json:"heap_alloc_bytes"`
	NetworkBytesSent     uint64  `json:"network_bytes_sent"`
	NetworkBytesReceived uint64  `json:"network_bytes_received"`
	MempoolSizeBytes     int64   `json:"mempool_size_bytes"`
}

type DashboardBackendMetrics struct {
	Summary   DashboardRuntimeMetrics   `json:"summary"`
	Requests  DashboardRequestMetrics   `json:"requests"`
	Kafka     DashboardKafkaMetrics     `json:"kafka"`
	Redis     DashboardRedisMetrics     `json:"redis"`
	Cockroach DashboardCockroachMetrics `json:"cockroach"`
}

type DashboardRuntimeMetrics struct {
	CPUSecondsTotal         float64 `json:"cpu_seconds_total"`
	ResidentMemoryBytes     uint64  `json:"resident_memory_bytes"`
	VirtualMemoryBytes      uint64  `json:"virtual_memory_bytes"`
	HeapAllocBytes          uint64  `json:"heap_alloc_bytes"`
	HeapSysBytes            uint64  `json:"heap_sys_bytes"`
	Goroutines              uint64  `json:"goroutines"`
	Threads                 uint64  `json:"threads"`
	GCPauseSeconds          float64 `json:"gc_pause_seconds"`
	GCCount                 uint64  `json:"gc_count"`
	ProcessStartTimeSeconds float64 `json:"process_start_time_seconds"`
}

type DashboardRequestMetrics struct {
	Total  uint64 `json:"total"`
	Errors uint64 `json:"errors"`
}

type DashboardKafkaMetrics struct {
	PublishSuccess uint64 `json:"publish_success"`
	PublishFailure uint64 `json:"publish_failure"`
	BrokerCount    uint64 `json:"broker_count"`
}

type DashboardRedisMetrics struct {
	PoolHits            uint64  `json:"pool_hits"`
	PoolMisses          uint64  `json:"pool_misses"`
	TotalConnections    uint64  `json:"total_connections"`
	IdleConnections     uint64  `json:"idle_connections"`
	Timeouts            uint64  `json:"timeouts"`
	CommandErrors       uint64  `json:"command_errors"`
	CommandLatencyP95Ms float64 `json:"command_latency_p95_ms"`
	OpsPerSec           int     `json:"ops_per_sec"`
	ConnectedClients    int     `json:"connected_clients"`
}

type DashboardCockroachMetrics struct {
	OpenConnections int64   `json:"open_connections"`
	InUse           int64   `json:"in_use"`
	Idle            int64   `json:"idle"`
	WaitSeconds     float64 `json:"wait_seconds"`
	WaitTotal       int64   `json:"wait_total"`
}

// DashboardBlocksSection provides recent block metadata, transaction slices, and
// derived KPIs so blockchain-centric screens can render without extra requests.
type DashboardBlocksSection struct {
	Recent           []BlockResponse             `json:"recent"`
	DecisionTimeline []DashboardDecisionTimeline `json:"decision_timeline"`
	Metrics          DashboardBlockMetrics       `json:"metrics"`
	Pagination       DashboardBlockPagination    `json:"pagination"`
}

type DashboardDecisionTimeline struct {
	Time     string `json:"time"`
	Height   uint64 `json:"height"`
	Hash     string `json:"hash"`
	Proposer string `json:"proposer"`
	Approved int    `json:"approved"`
	Rejected int    `json:"rejected"`
	Timeout  int    `json:"timeout"`
}

type DashboardBlockMetrics struct {
	LatestHeight        uint64        `json:"latest_height"`
	TotalTransactions   uint64        `json:"total_transactions"`
	AvgBlockTimeSeconds float64       `json:"avg_block_time_seconds"`
	AvgBlockSizeBytes   int           `json:"avg_block_size_bytes,omitempty"`
	SuccessRate         float64       `json:"success_rate"`
	AnomalyCount        int           `json:"anomaly_count"`
	Network             *NetworkStats `json:"network,omitempty"`
}

type DashboardBlockPagination struct {
	Limit   int    `json:"limit"`
	Start   uint64 `json:"start"`
	End     uint64 `json:"end"`
	Total   uint64 `json:"total"`
	HasMore bool   `json:"has_more"`
}

// DashboardThreatsSection delivers anomaly detections, feed items, and severity
// statistics surfaced from the AI service.
type DashboardThreatsSection struct {
	Timestamp         int64                          `json:"timestamp"`
	DetectionLoop     *AiDetectionLoopSummary        `json:"detection_loop,omitempty"`
	Source            string                         `json:"source,omitempty"`
	FallbackReason    string                         `json:"fallback_reason,omitempty"`
	Breakdown         DashboardThreatBreakdown       `json:"breakdown"`
	Feed              []AnomalyResponse              `json:"feed,omitempty"`
	Stats             *AnomalyStatsResponse          `json:"stats,omitempty"`
	AvgResponseTimeMs *float64                       `json:"avg_response_time_ms,omitempty"`
	LifetimeTotals    *DashboardThreatLifetimeTotals `json:"lifetime_totals,omitempty"`
}

type DashboardThreatBreakdown struct {
	ThreatTypes []DashboardThreatTypeSummary `json:"threat_types"`
	Severity    map[string]int               `json:"severity"`
	Totals      DashboardThreatTotals        `json:"totals"`
}

type DashboardThreatTypeSummary struct {
	ThreatType string `json:"threat_type"`
	Published  int    `json:"published"`
	Abstained  int    `json:"abstained"`
	Total      int    `json:"total"`
	Severity   string `json:"severity"`
}

type DashboardThreatTotals struct {
	Published int `json:"published"`
	Abstained int `json:"abstained"`
	Overall   int `json:"overall"`
}

type DashboardThreatLifetimeTotals struct {
	Total       uint64 `json:"total"`
	Published   uint64 `json:"published"`
	Abstained   uint64 `json:"abstained"`
	RateLimited uint64 `json:"rate_limited"`
	Errors      uint64 `json:"errors"`
}

type DashboardAISection struct {
	Metrics    *AIMetricsResponse          `json:"metrics,omitempty"`
	History    *AiDetectionHistoryResponse `json:"history,omitempty"`
	Suspicious *AiSuspiciousNodesResponse  `json:"suspicious,omitempty"`
	Health     map[string]interface{}      `json:"health,omitempty"`
	Ready      map[string]interface{}      `json:"ready,omitempty"`
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

// Anomaly DTOs

// AnomalyResponse represents a detected anomaly/threat
type AnomalyResponse struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`     // ddos, malware, phishing, etc
	Severity    string  `json:"severity"` // critical, high, medium, low
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"` // IP, domain, etc
	BlockHeight uint64  `json:"block_height"`
	Timestamp   int64   `json:"timestamp"`
	Confidence  float64 `json:"confidence"`
	TxHash      string  `json:"tx_hash"`
}

// AnomalyListResponse represents a list of anomalies
type AnomalyListResponse struct {
	Anomalies []AnomalyResponse `json:"anomalies"`
	Count     int               `json:"count"`
}

// AnomalyStatsResponse represents anomaly statistics by severity
type AnomalyStatsResponse struct {
	CriticalCount int `json:"critical_count"`
	HighCount     int `json:"high_count"`
	MediumCount   int `json:"medium_count"`
	LowCount      int `json:"low_count"`
	TotalCount    int `json:"total_count"`
}
