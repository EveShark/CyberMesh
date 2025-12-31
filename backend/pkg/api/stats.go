package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"backend/pkg/consensus/types"
	cockroach "backend/pkg/storage/cockroach"
	"backend/pkg/utils"
)

type aiDetectionEngineEntry struct {
	Engine              string   `json:"engine"`
	Ready               bool     `json:"ready"`
	Candidates          int64    `json:"candidates"`
	Published           int64    `json:"published"`
	PublishRatio        float64  `json:"publish_ratio"`
	ThroughputPerMinute float64  `json:"throughput_per_minute"`
	AvgConfidence       *float64 `json:"avg_confidence"`
	ThreatTypes         []string `json:"threat_types"`
	LastLatencyMs       *float64 `json:"last_latency_ms"`
	LastUpdated         *float64 `json:"last_updated"`
}

type aiDetectionVariantEntry struct {
	Variant             string   `json:"variant"`
	Engines             []string `json:"engines"`
	Total               int64    `json:"total"`
	Published           int64    `json:"published"`
	PublishRatio        float64  `json:"publish_ratio"`
	ThroughputPerMinute float64  `json:"throughput_per_minute"`
	AvgConfidence       *float64 `json:"avg_confidence"`
	ThreatTypes         []string `json:"threat_types"`
	LastUpdated         *float64 `json:"last_updated"`
}

type aiDetectionStatsPayload struct {
	Status        string `json:"status"`
	State         string `json:"state"`
	Message       string `json:"message"`
	DetectionLoop *struct {
		Running                   bool     `json:"running"`
		Status                    string   `json:"status"`
		Message                   string   `json:"message"`
		Issues                    []string `json:"issues"`
		Blocking                  bool     `json:"blocking"`
		Healthy                   bool     `json:"healthy"`
		AvgLatencyMs              float64  `json:"avg_latency_ms"`
		LastLatencyMs             float64  `json:"last_latency_ms"`
		SecondsSinceLastDetection *float64 `json:"seconds_since_last_detection"`
		SecondsSinceLastIteration *float64 `json:"seconds_since_last_iteration"`
		CacheAgeSeconds           *float64 `json:"cache_age_seconds"`
		// Metrics is intentionally schema-tolerant to avoid readiness failures
		// when the AI service evolves its detection_loop.metrics payload.
		Metrics map[string]interface{} `json:"metrics"`
	} `json:"detection_loop"`
	Derived *struct {
		DetectionsPerMinute  *float64 `json:"detections_per_minute"`
		PublishRatePerMinute *float64 `json:"publish_rate_per_minute"`
		IterationsPerMinute  *float64 `json:"iterations_per_minute"`
		ErrorRatePerHour     *float64 `json:"error_rate_per_hour"`
		PublishSuccessRatio  *float64 `json:"publish_success_ratio"`
	} `json:"derived"`
	Engines  []aiDetectionEngineEntry  `json:"engines"`
	Variants []aiDetectionVariantEntry `json:"variants"`
}

// handleStats handles GET /stats
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	response := s.buildStatsResponse(ctx)
	writeJSONResponse(w, r, NewSuccessResponse(response), http.StatusOK)
}

func (s *Server) buildStatsResponse(ctx context.Context) StatsResponse {
	response := StatsResponse{
		Chain:     s.getChainStats(ctx),
		Consensus: s.getConsensusStats(ctx),
		Mempool:   s.getMempoolStats(ctx),
		Network:   s.getNetworkStats(ctx),
	}

	if kafkaStats := s.getKafkaStats(ctx); kafkaStats != nil {
		response.Kafka = kafkaStats
	}

	if storageStats := s.getStorageStats(ctx); storageStats != nil {
		response.Storage = storageStats
	}

	if redisStats := s.getRedisStats(ctx); redisStats != nil {
		response.Redis = redisStats
	}

	if aiStats := s.getAIStats(ctx); aiStats != nil {
		response.AI = aiStats
	}

	sanitizeStatsResponse(&response)

	return response
}

// getChainStats returns blockchain statistics
func (s *Server) getChainStats(ctx context.Context) *ChainStats {
	stats := &ChainStats{}

	// Get latest height from storage (database) instead of in-memory state store
	if s.storage != nil {
		latestHeight, err := s.storage.GetLatestHeight(ctx)
		if err != nil {
			// Log error but return empty stats rather than failing the entire stats endpoint
			if s.logger != nil {
				s.logger.ErrorContext(ctx, "failed to get latest height for stats",
					utils.ZapError(err))
			}
		} else {
			stats.Height = latestHeight

			// Calculate average block time from recent blocks
			stats.AvgBlockTime = s.calculateAvgBlockTime(ctx, latestHeight)

			// Calculate average block size from recent blocks
			stats.AvgBlockSize = s.calculateAvgBlockSize(ctx, latestHeight)
		}

		// Get total transaction count from database
		stats.TotalTransactions = s.getTotalTransactionCount(ctx)

		// Calculate success rate from transaction status
		stats.SuccessRate = s.calculateSuccessRate(ctx)
	}

	// State version from in-memory state store (may differ from persisted height)
	if s.stateStore != nil {
		stats.StateVersion = s.stateStore.Latest()
	}

	return stats
}

// getConsensusStats returns consensus statistics
func (s *Server) getConsensusStats(ctx context.Context) *ConsensusStats {
	stats := &ConsensusStats{}

	if s.engine == nil {
		return stats
	}

	st := s.engine.GetStatus()
	stats.View = st.View
	stats.Round = st.Height

	validators := s.engine.ListValidators()
	stats.ValidatorCount = len(validators)
	if stats.ValidatorCount > 0 {
		// Use the same PBFT quorum calculation as consensus_overview (2f+1 where f=floor((n-1)/3))
		stats.QuorumSize = computeQuorumSize(stats.ValidatorCount)
	}

	var zeroLeader types.ValidatorID
	if st.CurrentLeader != zeroLeader {
		leaderHex := encodeHex(st.CurrentLeader[:])
		stats.CurrentLeader = leaderHex
		stats.CurrentLeaderID = leaderHex
		stats.CurrentLeaderAlias = s.resolveLeaderAlias(st.CurrentLeader, validators)
	}

	return stats
}

// getMempoolStats returns mempool statistics
func (s *Server) getMempoolStats(ctx context.Context) *MempoolStats {
	stats := &MempoolStats{}

	if s.mempool != nil {
		count, bytes, oldestTs := s.mempool.StatsDetailed()
		stats.PendingTransactions = count
		stats.SizeBytes = int64(bytes)
		if oldestTs > 0 {
			age := time.Now().Unix() - oldestTs
			if age < 0 {
				age = 0
			}
			stats.OldestTxAge = age
			stats.OldestTxAgeMs = float64(age) * 1000
		}
	}

	return stats
}

// getNetworkStats returns network statistics (optional)
func (s *Server) getNetworkStats(ctx context.Context) *NetworkStats {
	stats := &NetworkStats{}

	if s.p2pRouter != nil {
		report := s.p2pRouter.GetNetworkStats()
		stats.PeerCount = report.PeerCount
		stats.InboundPeers = report.InboundPeers
		stats.OutboundPeers = report.OutboundPeers
		stats.BytesReceived = report.BytesReceived
		stats.BytesSent = report.BytesSent
		stats.AvgLatencyMs = report.AvgLatencyMs
		if report.InboundRateBps > 0 {
			stats.InboundThroughputBps = report.InboundRateBps
		} else {
			inRate, _ := s.computeP2PThroughput(time.Now(), report)
			stats.InboundThroughputBps = inRate
		}
		if report.OutboundRateBps > 0 {
			stats.OutboundThroughputBps = report.OutboundRateBps
		} else {
			_, outRate := s.computeP2PThroughput(time.Now(), report)
			stats.OutboundThroughputBps = outRate
		}
		stats.Timestamp = report.Timestamp
		if len(report.History) > 0 {
			samples := make([]NetworkTrendSample, len(report.History))
			for i, sample := range report.History {
				samples[i] = NetworkTrendSample{
					Timestamp:     sample.Timestamp,
					PeerCount:     sample.PeerCount,
					InboundPeers:  sample.InboundPeers,
					OutboundPeers: sample.OutboundPeers,
					AvgLatencyMs:  sample.AvgLatencyMs,
					BytesReceived: sample.BytesReceived,
					BytesSent:     sample.BytesSent,
				}
			}
			stats.History = samples
		}
	}

	return stats
}

func (s *Server) getKafkaStats(ctx context.Context) *KafkaStats {
	if s.kafkaProd == nil && s.kafkaCons == nil {
		return nil
	}

	res := s.runKafkaCheck(ctx)
	stats := &KafkaStats{
		Status:         res.Status,
		ReadyLatencyMs: res.LatencyMs,
	}

	if s.kafkaProd != nil {
		producerStats := s.kafkaProd.Stats()
		stats.PublishLatencyMs = producerStats.PublishLatencyMs
		stats.PublishSuccesses = producerStats.PublishSuccesses
		stats.PublishFailures = producerStats.PublishFailures
		stats.BrokerCount = producerStats.BrokerCount
		stats.LastPartition = producerStats.LastPartition
		stats.LastOffset = producerStats.LastOffset
		stats.LastPublishUnix = producerStats.LastPublishUnix
		stats.LastError = producerStats.LastError
		if len(producerStats.LatencyBuckets) > 0 {
			stats.PublishLatencyP50Ms = producerStats.LatencyP50Ms
			stats.PublishLatencyP95Ms = producerStats.LatencyP95Ms
			stats.PublishLatencyHistogram = toLatencyBuckets(producerStats.LatencyBuckets)
		}
	}

	if s.kafkaCons != nil {
		consumerStats := s.kafkaCons.Stats()
		stats.ConsumerGroup = consumerStats.GroupID
		stats.ConsumerTopics = consumerStats.Topics
		stats.ConsumerTopicPartitions = consumerStats.TopicPartitions
		stats.ConsumerOffsets = consumerStats.PartitionOffsets
		stats.ConsumerLag = consumerStats.PartitionLag
		stats.ConsumerHighWater = consumerStats.PartitionHighwater
		stats.ConsumerAssigned = consumerStats.AssignedPartitions
		stats.ConsumerConsumed = consumerStats.MessagesConsumed
		stats.ConsumerVerified = consumerStats.MessagesVerified
		stats.ConsumerAdmitted = consumerStats.MessagesAdmitted
		stats.ConsumerFailed = consumerStats.MessagesFailed
		if consumerStats.LastMessageUnix > 0 {
			stats.ConsumerLastUnix = &consumerStats.LastMessageUnix
		}
		if len(consumerStats.IngestLatencyBuckets) > 0 {
			stats.ConsumerIngestP95Ms = consumerStats.IngestLatencyP95Ms
			stats.ConsumerIngestHistogram = toLatencyBuckets(consumerStats.IngestLatencyBuckets)
		}
		if len(consumerStats.ProcessLatencyBuckets) > 0 {
			stats.ConsumerProcessP95Ms = consumerStats.ProcessLatencyP95Ms
			stats.ConsumerProcessHistogram = toLatencyBuckets(consumerStats.ProcessLatencyBuckets)
		}
	}

	return stats
}

func (s *Server) getStorageStats(ctx context.Context) *StorageStats {
	if s.storage == nil {
		return nil
	}

	res, detail := s.runStorageCheck(ctx)
	stats := &StorageStats{
		Status:         res.Status,
		ReadyLatencyMs: res.LatencyMs,
	}

	if detail != nil {
		if v, ok := detail["database"].(string); ok {
			stats.Database = v
		}
		if v, ok := detail["version"].(string); ok {
			stats.Version = v
		}
		if v, ok := detail["node_id"].(int64); ok {
			stats.NodeID = v
		} else if v, ok := detail["node_id"].(int); ok {
			stats.NodeID = int64(v)
		}
	}

	if provider, ok := s.storage.(interface{ GetDB() *sql.DB }); ok {
		db := provider.GetDB()
		if db != nil {
			pool := cockroach.GetConnectionStats(db)
			if v, ok := pool["open_connections"].(int); ok {
				stats.PoolOpenConnections = v
			}
			if v, ok := pool["in_use"].(int); ok {
				stats.PoolInUse = v
			}
			if v, ok := pool["idle"].(int); ok {
				stats.PoolIdle = v
			}
			switch v := pool["wait_duration_ms"].(type) {
			case int:
				stats.PoolWaitSeconds = float64(v) / 1000.0
			case int64:
				stats.PoolWaitSeconds = float64(v) / 1000.0
			}
			switch v := pool["wait_count"].(type) {
			case int:
				stats.PoolWaitTotal = int64(v)
			case int64:
				stats.PoolWaitTotal = v
			}
		}
	}

	if metricsProvider, ok := s.storage.(interface {
		Metrics() cockroach.MetricsSnapshot
	}); ok {
		snap := metricsProvider.Metrics()
		if len(snap.QueryBuckets) > 0 {
			stats.QueryLatencyHistogram = toLatencyBuckets(snap.QueryBuckets)
			stats.QueryLatencyP95Ms = snap.QueryP95Ms
		}
		if len(snap.TxnBuckets) > 0 {
			stats.TransactionLatencyHistogram = toLatencyBuckets(snap.TxnBuckets)
			stats.TransactionLatencyP95Ms = snap.TxnP95Ms
		}
		if snap.SlowQueryCount > 0 {
			stats.SlowQueryCount = snap.SlowQueryCount
		}
		if snap.SlowTransactionCount > 0 {
			stats.SlowTransactionCount = snap.SlowTransactionCount
		}
		if len(snap.SlowQueries) > 0 {
			stats.SlowQueries = snap.SlowQueries
		}
		if len(snap.SlowTransactions) > 0 {
			stats.SlowTransactions = snap.SlowTransactions
		}
	}

	return stats
}

func (s *Server) getRedisStats(ctx context.Context) *RedisStats {
	if s.redisClient == nil {
		return nil
	}

	res, detail := s.runRedisCheck(ctx)
	stats := &RedisStats{
		Status:         res.Status,
		ReadyLatencyMs: res.LatencyMs,
	}

	if res.Message != "" {
		stats.Message = res.Message
	}

	if detail != nil {
		if v, ok := detail["mode"].(string); ok {
			stats.Mode = v
		}
		if v, ok := detail["role"].(string); ok {
			stats.Role = v
		}
		if v, ok := detail["version"].(string); ok {
			stats.Version = v
		}
		if v, ok := detail["connected_clients"].(int); ok {
			stats.ConnectedClients = v
		} else if v, ok := detail["connected_clients"].(float64); ok {
			stats.ConnectedClients = int(v)
		}
		if v, ok := detail["ops_per_sec"].(int); ok {
			stats.OpsPerSec = v
		} else if v, ok := detail["ops_per_sec"].(float64); ok {
			stats.OpsPerSec = int(v)
		}
		if v, ok := detail["uptime_seconds"].(int64); ok {
			stats.UptimeSeconds = v
		} else if v, ok := detail["uptime_seconds"].(int); ok {
			stats.UptimeSeconds = int64(v)
		} else if v, ok := detail["uptime_seconds"].(float64); ok {
			stats.UptimeSeconds = int64(v)
		}
		if v, ok := detail["total_connections_received"].(int64); ok {
			stats.TotalConnections = v
		} else if v, ok := detail["total_connections_received"].(int); ok {
			stats.TotalConnections = int64(v)
		} else if v, ok := detail["total_connections_received"].(float64); ok {
			stats.TotalConnections = int64(v)
		}
		if v, ok := detail["total_commands_processed"].(int64); ok {
			stats.TotalCommands = v
		} else if v, ok := detail["total_commands_processed"].(int); ok {
			stats.TotalCommands = int64(v)
		} else if v, ok := detail["total_commands_processed"].(float64); ok {
			stats.TotalCommands = int64(v)
		}
	}

	buckets, count, _, errors := s.redisSnapshot()
	if count > 0 {
		stats.CommandLatencyHistogram = toLatencyBuckets(buckets)
		stats.CommandLatencyP95Ms = s.redisMetrics.quantile(0.95)
	}
	stats.CommandErrors = errors

	return stats
}

func (s *Server) getAIStats(ctx context.Context) *AIStats {
	if s.aiClient == nil || s.aiBaseURL == "" {
		return nil
	}

	res := s.runAIServiceCheck(ctx)
	stats := &AIStats{
		Status:         res.Status,
		ReadyLatencyMs: res.LatencyMs,
	}
	if res.Message != "" {
		stats.Message = res.Message
	}

	payload, err := s.fetchAIDetectionStats(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to fetch AI detection stats", utils.ZapError(err))
		}
		return stats
	}

	if payload == nil {
		return stats
	}

	stats.State = payload.State
	if payload.DetectionLoop != nil {
		stats.DetectionLoopRunning = payload.DetectionLoop.Running
		stats.DetectionLoopStatus = payload.DetectionLoop.Status
		stats.DetectionLoopBlocking = payload.DetectionLoop.Blocking
		stats.DetectionLoopHealthy = payload.DetectionLoop.Healthy
		if len(payload.DetectionLoop.Issues) > 0 {
			stats.DetectionLoopIssues = append([]string(nil), payload.DetectionLoop.Issues...)
		}
		stats.DetectionLoopLatencyMs = payload.DetectionLoop.AvgLatencyMs
		stats.DetectionLoopLastMs = payload.DetectionLoop.LastLatencyMs
		stats.SecondsSinceDetection = payload.DetectionLoop.SecondsSinceLastDetection
		stats.SecondsSinceIteration = payload.DetectionLoop.SecondsSinceLastIteration
		stats.CacheAgeSeconds = payload.DetectionLoop.CacheAgeSeconds
		if stats.Message == "" && payload.DetectionLoop.Message != "" {
			stats.Message = payload.DetectionLoop.Message
		}
	}

	if payload.Derived != nil {
		stats.DetectionsPerMinute = payload.Derived.DetectionsPerMinute
		stats.PublishRatePerMinute = payload.Derived.PublishRatePerMinute
		stats.IterationsPerMinute = payload.Derived.IterationsPerMinute
		stats.ErrorRatePerHour = payload.Derived.ErrorRatePerHour
		stats.PublishSuccessRatio = payload.Derived.PublishSuccessRatio
	}

	if stats.Message == "" && payload.Message != "" {
		stats.Message = payload.Message
	}
	if stats.Message == "" && payload.Status != "" && payload.Status != "running" {
		stats.Message = fmt.Sprintf("ai status: %s", payload.Status)
	}

	return stats
}

func sanitizeStatsResponse(resp *StatsResponse) {
	if resp == nil {
		return
	}
	if resp.Chain != nil {
		resp.Chain.SuccessRate = sanitizeFloat(resp.Chain.SuccessRate)
		resp.Chain.AvgBlockTime = sanitizeFloat(resp.Chain.AvgBlockTime)
	}
	if resp.Mempool != nil {
		resp.Mempool.OldestTxAgeMs = sanitizeFloat(resp.Mempool.OldestTxAgeMs)
	}
	if resp.Network != nil {
		resp.Network.AvgLatencyMs = sanitizeFloat(resp.Network.AvgLatencyMs)
		resp.Network.InboundThroughputBps = sanitizeFloat(resp.Network.InboundThroughputBps)
		resp.Network.OutboundThroughputBps = sanitizeFloat(resp.Network.OutboundThroughputBps)
		for i := range resp.Network.History {
			resp.Network.History[i].AvgLatencyMs = sanitizeFloat(resp.Network.History[i].AvgLatencyMs)
		}
	}
	if resp.Kafka != nil {
		resp.Kafka.ReadyLatencyMs = sanitizeFloat(resp.Kafka.ReadyLatencyMs)
		resp.Kafka.PublishLatencyMs = sanitizeFloat(resp.Kafka.PublishLatencyMs)
		resp.Kafka.PublishLatencyP50Ms = sanitizeFloat(resp.Kafka.PublishLatencyP50Ms)
		resp.Kafka.PublishLatencyP95Ms = sanitizeFloat(resp.Kafka.PublishLatencyP95Ms)
		resp.Kafka.ConsumerIngestP95Ms = sanitizeFloat(resp.Kafka.ConsumerIngestP95Ms)
		resp.Kafka.ConsumerProcessP95Ms = sanitizeFloat(resp.Kafka.ConsumerProcessP95Ms)
		sanitizeLatencyBuckets(resp.Kafka.PublishLatencyHistogram)
		sanitizeLatencyBuckets(resp.Kafka.ConsumerIngestHistogram)
		sanitizeLatencyBuckets(resp.Kafka.ConsumerProcessHistogram)
	}
	if resp.Storage != nil {
		resp.Storage.ReadyLatencyMs = sanitizeFloat(resp.Storage.ReadyLatencyMs)
		resp.Storage.PoolWaitSeconds = sanitizeFloat(resp.Storage.PoolWaitSeconds)
		resp.Storage.QueryLatencyP95Ms = sanitizeFloat(resp.Storage.QueryLatencyP95Ms)
		resp.Storage.TransactionLatencyP95Ms = sanitizeFloat(resp.Storage.TransactionLatencyP95Ms)
		sanitizeLatencyBuckets(resp.Storage.QueryLatencyHistogram)
		sanitizeLatencyBuckets(resp.Storage.TransactionLatencyHistogram)
	}
	if resp.Redis != nil {
		resp.Redis.ReadyLatencyMs = sanitizeFloat(resp.Redis.ReadyLatencyMs)
		resp.Redis.CommandLatencyP95Ms = sanitizeFloat(resp.Redis.CommandLatencyP95Ms)
		sanitizeLatencyBuckets(resp.Redis.CommandLatencyHistogram)
	}
	if resp.AI != nil {
		resp.AI.ReadyLatencyMs = sanitizeFloat(resp.AI.ReadyLatencyMs)
		resp.AI.DetectionLoopLatencyMs = sanitizeFloat(resp.AI.DetectionLoopLatencyMs)
		resp.AI.DetectionLoopLastMs = sanitizeFloat(resp.AI.DetectionLoopLastMs)
		resp.AI.DetectionsPerMinute = sanitizeFloatPtr(resp.AI.DetectionsPerMinute)
		resp.AI.PublishRatePerMinute = sanitizeFloatPtr(resp.AI.PublishRatePerMinute)
		resp.AI.IterationsPerMinute = sanitizeFloatPtr(resp.AI.IterationsPerMinute)
		resp.AI.ErrorRatePerHour = sanitizeFloatPtr(resp.AI.ErrorRatePerHour)
		resp.AI.PublishSuccessRatio = sanitizeFloatPtr(resp.AI.PublishSuccessRatio)
		resp.AI.SecondsSinceDetection = sanitizeFloatPtr(resp.AI.SecondsSinceDetection)
		resp.AI.SecondsSinceIteration = sanitizeFloatPtr(resp.AI.SecondsSinceIteration)
		resp.AI.CacheAgeSeconds = sanitizeFloatPtr(resp.AI.CacheAgeSeconds)
	}
}

func sanitizeFloat(val float64) float64 {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0
	}
	return val
}

func sanitizeFloatPtr(val *float64) *float64 {
	if val == nil {
		return nil
	}
	if math.IsNaN(*val) || math.IsInf(*val, 0) {
		return nil
	}
	return val
}

func sanitizeLatencyBuckets(buckets []LatencyBucket) {
	for i := range buckets {
		if math.IsInf(buckets[i].UpperBoundMs, 1) {
			buckets[i].UpperBoundMs = math.MaxFloat64
		} else {
			buckets[i].UpperBoundMs = sanitizeFloat(buckets[i].UpperBoundMs)
		}
	}
}

func toLatencyBuckets(src []utils.HistogramBucket) []LatencyBucket {
	if len(src) == 0 {
		return nil
	}
	out := make([]LatencyBucket, len(src))
	for i, b := range src {
		out[i] = LatencyBucket{UpperBoundMs: b.UpperBound, Count: b.Count}
	}
	return out
}

func (s *Server) fetchAIDetectionStats(ctx context.Context) (*aiDetectionStatsPayload, error) {
	if s.aiClient == nil || s.aiBaseURL == "" {
		return nil, nil
	}

	endpoint := strings.TrimRight(s.aiBaseURL, "/") + "/detections/stats"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build AI stats request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if s.aiAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.aiAuthToken)
	}

	resp, err := s.aiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call AI stats endpoint: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// continue
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("ai stats endpoint returned status %d", resp.StatusCode)
	}

	var payload aiDetectionStatsPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode AI stats response: %w", err)
	}

	return &payload, nil
}

// calculateAvgBlockTime computes average block time from last 100 blocks
func (s *Server) calculateAvgBlockTime(ctx context.Context, currentHeight uint64) float64 {
	if currentHeight < 2 {
		return 0
	}

	// SECURITY: Add timeout protection
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Determine range: last 100 blocks or all available
	startHeight := uint64(0)
	if currentHeight > 100 {
		startHeight = currentHeight - 100
	}

	// Query timestamps from database
	query := `
		SELECT timestamp 
		FROM blocks 
		WHERE height >= $1 AND height <= $2 
		ORDER BY height ASC
	`

	// Get concrete adapter to access database
	adapter, ok := s.storage.(interface {
		GetDB() *sql.DB
	})
	if !ok {
		return 0
	}

	rows, err := adapter.GetDB().QueryContext(queryCtx, query, startHeight, currentHeight)
	if err != nil {
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "failed to query block timestamps",
				utils.ZapError(err))
		}
		return 0
	}
	defer rows.Close()

	var timestamps []time.Time
	for rows.Next() {
		var ts time.Time
		if err := rows.Scan(&ts); err != nil {
			if s.logger != nil {
				s.logger.WarnContext(ctx, "failed to scan block timestamp", utils.ZapError(err))
			}
			continue
		}
		if ts.IsZero() {
			continue
		}
		timestamps = append(timestamps, ts)
	}

	if len(timestamps) < 2 {
		return 0
	}

	elapsed := timestamps[len(timestamps)-1].Sub(timestamps[0])
	if elapsed <= 0 {
		return 0
	}

	avgSeconds := elapsed.Seconds() / float64(len(timestamps)-1)
	if avgSeconds < 0 {
		return 0
	}

	return avgSeconds
}

// calculateAvgBlockSize computes average block size from last 100 blocks
func (s *Server) calculateAvgBlockSize(ctx context.Context, currentHeight uint64) int {
	if currentHeight < 1 {
		return 0
	}

	// SECURITY: Add timeout protection
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Determine range: last 100 blocks or all available
	startHeight := uint64(0)
	if currentHeight > 100 {
		startHeight = currentHeight - 100
	}

	query := `
		SELECT b.tx_count, COALESCE(SUM(length(t.payload::text)), 0)
		FROM blocks b
		LEFT JOIN transactions t ON t.block_height = b.height
		WHERE b.height >= $1 AND b.height <= $2
		GROUP BY b.height, b.tx_count
	`

	adapter, ok := s.storage.(interface {
		GetDB() *sql.DB
	})
	if !ok {
		return 0
	}

	rows, err := adapter.GetDB().QueryContext(queryCtx, query, startHeight, currentHeight)
	if err != nil {
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "failed to query block sizes",
				utils.ZapError(err))
		}
		return 0
	}
	defer rows.Close()

	const (
		headerBytes    int64 = 256
		txByteEstimate int64 = 512
	)
	var (
		totalSize  int64
		blockCount int64
	)

	for rows.Next() {
		var (
			txCount      int
			payloadBytes int64
		)
		if err := rows.Scan(&txCount, &payloadBytes); err != nil {
			if s.logger != nil {
				s.logger.WarnContext(ctx, "failed to scan block size row", utils.ZapError(err))
			}
			continue
		}

		size := payloadBytes
		if size <= 0 {
			if txCount > 0 {
				size = int64(txCount)*txByteEstimate + headerBytes
			} else {
				size = headerBytes
			}
		} else {
			size += headerBytes
		}
		if size < 0 {
			size = 0
		}

		totalSize += size
		blockCount++
	}

	if err := rows.Err(); err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "error iterating block size rows", utils.ZapError(err))
		}
	}

	if blockCount == 0 {
		return 0
	}

	return int(totalSize / blockCount)
}

// getTotalTransactionCount returns total number of transactions across all blocks
func (s *Server) getTotalTransactionCount(ctx context.Context) uint64 {
	// SECURITY: Add timeout protection
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `SELECT COUNT(*) FROM transactions`

	// Get concrete adapter to access database
	adapter, ok := s.storage.(interface {
		GetDB() *sql.DB
	})
	if !ok {
		return 0
	}

	var count uint64
	err := adapter.GetDB().QueryRowContext(queryCtx, query).Scan(&count)
	if err != nil {
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "failed to get total transaction count",
				utils.ZapError(err))
		}
		return 0
	}

	return count
}

// calculateSuccessRate returns the ratio of successful transactions to total transactions
func (s *Server) calculateSuccessRate(ctx context.Context) float64 {
	// SECURITY: Add timeout protection
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT 
			COUNT(*) FILTER (WHERE status = 'success') as success_count,
			COUNT(*) as total_count
		FROM transactions
	`

	// Get concrete adapter to access database
	adapter, ok := s.storage.(interface {
		GetDB() *sql.DB
	})
	if !ok {
		return 0
	}

	var successCount, totalCount uint64
	err := adapter.GetDB().QueryRowContext(queryCtx, query).Scan(&successCount, &totalCount)
	if err != nil {
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "failed to calculate success rate",
				utils.ZapError(err))
		}
		return 0
	}

	if totalCount == 0 {
		return 0
	}

	return float64(successCount) / float64(totalCount)
}
