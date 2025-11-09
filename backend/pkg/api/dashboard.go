package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	runtimeMetrics "runtime/metrics"
	"sort"
	"strconv"
	"strings"
	"time"

	consapi "backend/pkg/consensus/api"
	"backend/pkg/utils"
)

func (s *Server) handleDashboardOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	now := time.Now()
	if cached, ok := s.getDashboardCache(now); ok {
		s.recordRouteCacheHit(r.Method, r.URL.Path)
		writeJSONResponse(w, r, NewSuccessResponse(cached), http.StatusOK)
		return
	}

	if s.config.DashboardCacheTTL > 0 {
		s.recordRouteCacheMiss(r.Method, r.URL.Path)
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.config.RequestTimeout)
	defer cancel()

	stats := s.buildStatsResponse(ctx)
	backendMetrics := s.buildDashboardBackendMetrics(stats)
	backendDerived := s.buildDashboardBackendDerived(stats)
	backendHistory := s.snapshotBackendHistory(stats, backendMetrics)
	readiness, _ := s.buildReadinessResponse(ctx)
	health := healthResponseFromReadiness(readiness)

	var networkOverview *NetworkOverviewResponse
	if s.engine != nil {
		if overview, err := s.buildNetworkOverview(ctx, time.Now().UTC()); err == nil {
			networkOverview = overview
		} else if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to build network overview", utils.ZapError(err))
		}
	}

	var consensusOverview *ConsensusOverviewResponse
	var consensusMetrics consapi.MetricsSnapshot
	if s.engine != nil {
		consensusMetrics = s.engine.GetMetrics()
		if overview, err := s.buildConsensusOverview(ctx, time.Now().UTC()); err == nil {
			consensusOverview = overview
		} else if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to build consensus overview", utils.ZapError(err))
		}
	}

	blocksSection := s.buildDashboardBlocks(ctx, stats, consensusMetrics)
	threatsSection := s.buildDashboardThreats(ctx)
	aiSection := s.buildDashboardAI(ctx)
	ledgerSection := s.buildDashboardLedger(ctx, stats)
	validatorSection := s.buildDashboardValidators(ctx)

	response := DashboardOverviewResponse{
		Timestamp: time.Now().UnixMilli(),
		Backend: DashboardBackendSection{
			Health:    health,
			Readiness: readiness,
			Stats:     stats,
			Metrics:   backendMetrics,
			Derived:   backendDerived,
			History:   backendHistory,
		},
		Ledger:     ledgerSection,
		Validators: validatorSection,
		Network:    networkOverview,
		Consensus:  consensusOverview,
		Blocks:     blocksSection,
		Threats:    threatsSection,
		AI:         aiSection,
	}

	s.setDashboardCache(&response, time.Now())
	writeJSONResponse(w, r, NewSuccessResponse(&response), http.StatusOK)
}

func (s *Server) buildDashboardBackendMetrics(stats StatsResponse) DashboardBackendMetrics {
	summary := s.snapshotRuntimeMetrics()
	requests := DashboardRequestMetrics{
		Total:  s.apiRequestsTotal.Load(),
		Errors: s.apiRequestErrors.Load(),
	}

	var kafka DashboardKafkaMetrics
	if stats.Kafka != nil {
		kafka.PublishSuccess = stats.Kafka.PublishSuccesses
		kafka.PublishFailure = stats.Kafka.PublishFailures
		kafka.BrokerCount = uint64(stats.Kafka.BrokerCount)
	}

	var redis DashboardRedisMetrics
	if stats.Redis != nil {
		if stats.Redis.TotalConnections > 0 {
			redis.TotalConnections = uint64(stats.Redis.TotalConnections)
		}
		redis.CommandErrors = stats.Redis.CommandErrors
		redis.CommandLatencyP95Ms = stats.Redis.CommandLatencyP95Ms
		redis.OpsPerSec = stats.Redis.OpsPerSec
		redis.ConnectedClients = stats.Redis.ConnectedClients
	}
	if s.redisClient != nil {
		pool := s.redisClient.PoolStats()
		redis.PoolHits = uint64(pool.Hits)
		redis.PoolMisses = uint64(pool.Misses)
		redis.TotalConnections = uint64(pool.TotalConns)
		redis.IdleConnections = uint64(pool.IdleConns)
		redis.Timeouts = uint64(pool.Timeouts)
	}

	var cockroach DashboardCockroachMetrics
	if stats.Storage != nil {
		cockroach.OpenConnections = int64(stats.Storage.PoolOpenConnections)
		cockroach.InUse = int64(stats.Storage.PoolInUse)
		cockroach.Idle = int64(stats.Storage.PoolIdle)
		cockroach.WaitSeconds = stats.Storage.PoolWaitSeconds
		cockroach.WaitTotal = stats.Storage.PoolWaitTotal
	}

	return DashboardBackendMetrics{
		Summary:   summary,
		Requests:  requests,
		Kafka:     kafka,
		Redis:     redis,
		Cockroach: cockroach,
	}
}

func (s *Server) loadAIMetricsSnapshot(ctx context.Context) (*AIMetricsResponse, bool) {
	metrics, err := s.collectAIMetrics(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to collect ai metrics for dashboard", utils.ZapError(err))
		}
		return &AIMetricsResponse{
			Status:   "degraded",
			Message:  err.Error(),
			Engines:  []AiEngineMetric{},
			Variants: []AiVariantMetric{},
		}, true
	}
	if metrics == nil {
		return nil, true
	}

	var issues []string
	statusDegraded := false
	if metrics.Loop != nil {
		loopStatus := strings.ToLower(metrics.Loop.Status)
		loopIssues := metrics.Loop.Issues
		switch loopStatus {
		case "critical", "stopped":
			statusDegraded = true
			loopIssues = append(loopIssues, "detection loop unavailable")
		case "degraded":
			if metrics.Loop.Blocking {
				statusDegraded = true
			}
		}

		if len(loopIssues) > 0 {
			issues = append(issues, loopIssues...)
		}
		if metrics.Loop.SecondsSinceLastIteration != nil && *metrics.Loop.SecondsSinceLastIteration > 20 {
			issues = append(issues, fmt.Sprintf("%.0fs since last iteration", *metrics.Loop.SecondsSinceLastIteration))
		}
		if metrics.Loop.SecondsSinceLastDetection != nil && *metrics.Loop.SecondsSinceLastDetection > 600 {
			issues = append(issues, fmt.Sprintf("%.0fs since last detection", *metrics.Loop.SecondsSinceLastDetection))
		}
	}

	const latencyWarningCeiling = 3000.0
	if metrics.Loop != nil && len(issues) > 0 {
		filtered := issues[:0]
		for _, issue := range issues {
			if strings.Contains(issue, "iteration latency") {
				if metrics.Loop.LastLatencyMs > latencyWarningCeiling {
					filtered = append(filtered, issue)
				}
				continue
			}
			if strings.Contains(issue, "loop latency") {
				if metrics.Loop.LastLatencyMs > latencyWarningCeiling {
					filtered = append(filtered, issue)
				}
				continue
			}
			filtered = append(filtered, issue)
		}
		issues = filtered
	}

	degraded := len(issues) > 0 || statusDegraded
	if degraded {
		metrics.Status = "degraded"
		summary := ""
		if len(issues) > 0 {
			summary = strings.Join(issues, ", ")
		} else if statusDegraded {
			summary = "detection loop degraded"
		}
		if summary != "" {
			if metrics.Message != "" {
				metrics.Message = metrics.Message + "; " + summary
			} else {
				metrics.Message = summary
			}
		}
	} else if metrics.Status == "" {
		metrics.Status = "ok"
	}

	if metrics.Loop != nil {
		s.recordLoopStatus(ctx, metrics.Loop.Status, metrics.Loop.Blocking, issues)
	}

	return metrics, degraded
}

func (s *Server) buildDashboardBackendDerived(stats StatsResponse) DashboardBackendDerived {
	pointer := func(val float64) *float64 {
		return &val
	}
	pointerInt := func(val int64) *int64 {
		return &val
	}
	pointerUint := func(val uint64) *uint64 {
		return &val
	}

	var mempoolLatency *float64
	if stats.Mempool != nil {
		switch {
		case stats.Mempool.OldestTxAgeMs > 0:
			mempoolLatency = pointer(stats.Mempool.OldestTxAgeMs)
		case stats.Mempool.OldestTxAge > 0:
			mempoolLatency = pointer(float64(stats.Mempool.OldestTxAge) * 1000)
		}
	}

	var consensusLatency *float64
	if stats.Chain != nil && stats.Chain.AvgBlockTime > 0 {
		consensusLatency = pointer(stats.Chain.AvgBlockTime * 1000)
	}

	var p2pLatency *float64
	if stats.Network != nil && stats.Network.AvgLatencyMs > 0 {
		p2pLatency = pointer(stats.Network.AvgLatencyMs)
	}

	derived := DashboardBackendDerived{
		MempoolLatencyMs:   mempoolLatency,
		ConsensusLatencyMs: consensusLatency,
		P2PLatencyMs:       p2pLatency,
	}

	if val := s.loopStatus.Load(); val != nil {
		if status, ok := val.(string); ok && status != "" && status != "unknown" {
			derived.AiLoopStatus = status
		}
	}
	if ts := s.loopStatusUpdated.Load(); ts > 0 {
		derived.AiLoopStatusUpdatedAt = pointerInt(ts)
	}
	if s.loopBlocking.Load() {
		blocking := true
		derived.AiLoopBlocking = &blocking
	}
	if val := s.loopIssues.Load(); val != nil {
		if issues, ok := val.(string); ok && issues != "" {
			derived.AiLoopIssues = issues
		}
	}
	if val := s.threatDataSource.Load(); val != nil {
		if source, ok := val.(string); ok && source != "" && source != "unknown" {
			derived.ThreatDataSource = source
		}
	}
	if ts := s.threatSourceUpdated.Load(); ts > 0 {
		derived.ThreatSourceUpdatedAt = pointerInt(ts)
	}
	if count := s.threatFallbackCount.Load(); count > 0 {
		derived.ThreatFallbackCount = pointerUint(count)
	}
	if val := s.threatFallbackReason.Load(); val != nil {
		if reason, ok := val.(string); ok && reason != "" {
			derived.ThreatFallbackReason = reason
		}
	}
	if ts := s.threatFallbackUpdated.Load(); ts > 0 {
		derived.ThreatFallbackAt = pointerInt(ts)
	}

	return derived
}

func (s *Server) snapshotBackendHistory(stats StatsResponse, metrics DashboardBackendMetrics) []DashboardBackendHistorySample {
	now := time.Now().UnixMilli()
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	sample := DashboardBackendHistorySample{
		Timestamp:           now,
		CPUSecondsTotal:     metrics.Summary.CPUSecondsTotal,
		ResidentMemoryBytes: metrics.Summary.ResidentMemoryBytes,
		HeapAllocBytes:      metrics.Summary.HeapAllocBytes,
	}

	if stats.Network != nil {
		sample.NetworkBytesSent = stats.Network.BytesSent
		sample.NetworkBytesReceived = stats.Network.BytesReceived
	}

	if stats.Mempool != nil {
		sample.MempoolSizeBytes = stats.Mempool.SizeBytes
	}

	if prev := s.lastHistorySnapshot; prev != nil {
		elapsedSeconds := float64(now-prev.timestamp) / 1000
		deltaCPU := metrics.Summary.CPUSecondsTotal - prev.cpuSecondsTotal
		if elapsedSeconds > 0 && deltaCPU >= 0 {
			cpuPercent := (deltaCPU / elapsedSeconds) * 100
			if cpuPercent < 0 {
				cpuPercent = 0
			} else if cpuPercent > 100 {
				cpuPercent = 100
			}
			sample.CPUPercent = cpuPercent
		}
	}

	s.lastHistorySnapshot = &backendHistorySnapshot{
		timestamp:            now,
		cpuSecondsTotal:      metrics.Summary.CPUSecondsTotal,
		networkBytesSent:     sample.NetworkBytesSent,
		networkBytesReceived: sample.NetworkBytesReceived,
		mempoolSizeBytes:     sample.MempoolSizeBytes,
	}

	s.metricsHistory = append(s.metricsHistory, sample)
	const maxHistory = 120
	if len(s.metricsHistory) > maxHistory {
		trim := make([]DashboardBackendHistorySample, maxHistory)
		copy(trim, s.metricsHistory[len(s.metricsHistory)-maxHistory:])
		s.metricsHistory = trim
	}

	historyCopy := make([]DashboardBackendHistorySample, len(s.metricsHistory))
	copy(historyCopy, s.metricsHistory)
	return historyCopy
}

func (s *Server) snapshotRuntimeMetrics() DashboardRuntimeMetrics {
	readMetric := func(name string) (float64, bool) {
		samples := []runtimeMetrics.Sample{{Name: name}}
		runtimeMetrics.Read(samples)
		switch samples[0].Value.Kind() {
		case runtimeMetrics.KindFloat64:
			return samples[0].Value.Float64(), true
		case runtimeMetrics.KindUint64:
			return float64(samples[0].Value.Uint64()), true
		default:
			return 0, false
		}
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	cpuSeconds, ok := readMetric("/sched/process/cpu-seconds")
	if !ok {
		elapsed := time.Now().Unix() - s.processStartTime
		if elapsed < 0 {
			elapsed = 0
		}
		cpuSeconds = float64(elapsed) * 0.8
	}

	gcCount := uint64(mem.NumGC)
	gcPause := float64(mem.PauseTotalNs) / 1e9

	goroutines, ok := readMetric("/sched/goroutines:goroutines")
	if !ok {
		goroutines = float64(runtime.NumGoroutine())
	}
	threads, ok := readMetric("/sched/threads:threads")
	if !ok {
		threads = goroutines
	}

	return DashboardRuntimeMetrics{
		CPUSecondsTotal:         cpuSeconds,
		ResidentMemoryBytes:     mem.Alloc,
		VirtualMemoryBytes:      mem.Sys,
		HeapAllocBytes:          mem.HeapAlloc,
		HeapSysBytes:            mem.HeapSys,
		Goroutines:              uint64(goroutines),
		Threads:                 uint64(threads),
		GCPauseSeconds:          gcPause,
		GCCount:                 gcCount,
		ProcessStartTimeSeconds: float64(s.processStartTime),
	}
}

func (s *Server) buildDashboardBlocks(ctx context.Context, stats StatsResponse, metrics consapi.MetricsSnapshot) DashboardBlocksSection {
	limit := s.config.DashboardBlockLimit
	avgBlockSize := 0.0
	if stats.Chain != nil && stats.Chain.AvgBlockSize > 0 {
		avgBlockSize = float64(stats.Chain.AvgBlockSize)
	}
	blocks, err := s.fetchLatestBlocks(ctx, limit, avgBlockSize)
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to fetch latest blocks", utils.ZapError(err))
		}
		blocks = []BlockResponse{}
	}

	decisionTimeline := s.buildDecisionTimeline(ctx, stats, blocks, metrics)

	var latestHeight uint64
	var totalTx uint64
	var avgBlockTime float64
	var successRate float64
	var avgBlockSizeBytes int
	var anomalyTotal int
	var networkStats *NetworkStats

	if stats.Chain != nil {
		latestHeight = stats.Chain.Height
		totalTx = stats.Chain.TotalTransactions
		avgBlockTime = stats.Chain.AvgBlockTime
		successRate = stats.Chain.SuccessRate
		avgBlockSizeBytes = stats.Chain.AvgBlockSize
	}

	for _, block := range blocks {
		anomalyTotal += block.AnomalyCount
	}

	var startHeight uint64
	var endHeight uint64
	if len(blocks) > 0 {
		startHeight = blocks[0].Height
		endHeight = blocks[len(blocks)-1].Height
	}

	totalHeight := latestHeight
	if totalHeight == 0 {
		totalHeight = endHeight
	}

	hasMore := len(blocks) == limit && startHeight > 1

	pagination := DashboardBlockPagination{
		Limit:   limit,
		Start:   startHeight,
		End:     endHeight,
		Total:   totalHeight,
		HasMore: hasMore,
	}

	if stats.Network != nil {
		copy := *stats.Network
		networkStats = &copy
	}

	return DashboardBlocksSection{
		Recent:           blocks,
		DecisionTimeline: decisionTimeline,
		Metrics: DashboardBlockMetrics{
			LatestHeight:        latestHeight,
			TotalTransactions:   totalTx,
			AvgBlockTimeSeconds: avgBlockTime,
			AvgBlockSizeBytes:   avgBlockSizeBytes,
			SuccessRate:         successRate,
			AnomalyCount:        anomalyTotal,
			Network:             networkStats,
		},
		Pagination: pagination,
	}
}

func (s *Server) buildDashboardLedger(ctx context.Context, stats StatsResponse) *DashboardLedgerSection {
	if stats.Chain == nil {
		return nil
	}

	section := &DashboardLedgerSection{
		LatestHeight:        stats.Chain.Height,
		StateVersion:        stats.Chain.StateVersion,
		TotalTransactions:   stats.Chain.TotalTransactions,
		AvgBlockTimeSeconds: stats.Chain.AvgBlockTime,
		AvgBlockSizeBytes:   stats.Chain.AvgBlockSize,
	}

	if stats.Mempool != nil {
		section.PendingTransactions = stats.Mempool.PendingTransactions
		section.MempoolSizeBytes = stats.Mempool.SizeBytes
		switch {
		case stats.Mempool.OldestTxAgeMs > 0:
			section.MempoolOldestTxAgeMs = stats.Mempool.OldestTxAgeMs
		case stats.Mempool.OldestTxAge > 0:
			section.MempoolOldestTxAgeMs = float64(stats.Mempool.OldestTxAge) * 1000
		}
	}

	version := stats.Chain.StateVersion
	if version > 0 {
		snapshotCtx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
		snapshot, err := s.storage.GetSnapshot(snapshotCtx, version)
		cancel()
		if err != nil {
			if s.logger != nil {
				s.logger.WarnContext(ctx, "failed to load ledger snapshot",
					utils.ZapError(err),
					utils.ZapUint64("state_version", version))
			}
		} else if snapshot != nil {
			if len(snapshot.StateRoot) > 0 {
				section.StateRoot = encodeHex(snapshot.StateRoot)
			}
			if len(snapshot.BlockHash) > 0 {
				section.LastBlockHash = encodeHex(snapshot.BlockHash)
			}
			section.SnapshotBlockHeight = snapshot.BlockHeight
			section.SnapshotTransactionCount = snapshot.TxCount
			section.ReputationChanges = snapshot.ReputationChanges
			section.PolicyChanges = snapshot.PolicyChanges
			section.QuarantineChanges = snapshot.QuarantineChanges
			if !snapshot.CreatedAt.IsZero() {
				section.SnapshotTimestamp = snapshot.CreatedAt.Unix()
			}
		}
	}

	return section
}

func (s *Server) buildDashboardValidators(ctx context.Context) *DashboardValidatorsSection {
	if s.engine == nil {
		return nil
	}

	validators := s.engine.ListValidators()
	section := &DashboardValidatorsSection{
		Validators: make([]DashboardValidatorSnapshot, 0, len(validators)),
		UpdatedAt:  time.Now().UnixMilli(),
	}

	for idx, info := range validators {
		resp := validatorInfoToResponse(info)
		resp.UptimePercentage = reputationToUptime(info.Reputation)
		snapshot := DashboardValidatorSnapshot{
			ValidatorResponse: resp,
			Alias:             s.resolveValidatorAlias(info, idx),
		}
		if info.PeerID != "" {
			snapshot.PeerID = info.PeerID
		}
		if !info.LastSeen.IsZero() {
			snapshot.LastSeenUnix = info.LastSeen.UTC().Unix()
		}
		if info.IsActive {
			section.Active++
		} else {
			section.Inactive++
		}
		section.Validators = append(section.Validators, snapshot)
	}

	section.Total = len(section.Validators)
	if len(section.Validators) == 0 {
		return section
	}

	sort.Slice(section.Validators, func(i, j int) bool {
		left := section.Validators[i].Alias
		right := section.Validators[j].Alias
		if left == right {
			return section.Validators[i].ID < section.Validators[j].ID
		}
		return strings.ToLower(left) < strings.ToLower(right)
	})

	return section
}

func (s *Server) buildDecisionTimeline(ctx context.Context, stats StatsResponse, blocks []BlockResponse, metrics consapi.MetricsSnapshot) []DashboardDecisionTimeline {
	if len(blocks) == 0 {
		return []DashboardDecisionTimeline{
			{
				Time:     "latest",
				Height:   metrics.BlocksCommitted,
				Hash:     "",
				Proposer: "",
				Approved: int(metrics.BlocksCommitted),
				Rejected: int(metrics.ProposalsReceived - metrics.BlocksCommitted),
				Timeout:  int(metrics.ViewChanges),
			},
		}
	}

	count := len(blocks)
	if count > 10 {
		blocks = blocks[count-10:]
	}

	if len(blocks) == 0 {
		return nil
	}

	latestHeight := blocks[len(blocks)-1].Height
	minHeight := blocks[0].Height
	limit := len(blocks)

	votes := s.loadRecentVotes(ctx, latestHeight, limit)
	qcs := s.loadRecentQCs(ctx, latestHeight, limit)

	voteByHeight := make(map[uint64]map[string]int)
	for _, vote := range votes {
		if vote.Height < minHeight || vote.Height > latestHeight {
			continue
		}
		hash := encodeHex(vote.BlockHash[:])
		heightVotes := voteByHeight[vote.Height]
		if heightVotes == nil {
			heightVotes = make(map[string]int)
			voteByHeight[vote.Height] = heightVotes
		}
		heightVotes[hash]++
	}

	qcSignatures := make(map[uint64]int)
	for _, qc := range qcs {
		if qc.Height < minHeight || qc.Height > latestHeight {
			continue
		}
		qcSignatures[qc.Height] = len(qc.Signatures)
	}

	quorum := 0
	if stats.Consensus != nil {
		quorum = stats.Consensus.QuorumSize
	}
	if quorum <= 0 && s.engine != nil {
		quorum = computeQuorumSize(len(s.engine.ListValidators()))
	}

	result := make([]DashboardDecisionTimeline, 0, len(blocks))
	for i := len(blocks) - 1; i >= 0; i-- {
		block := blocks[i]
		heightVotes := voteByHeight[block.Height]
		blockHash := block.Hash
		var committedVotes, rejectedVotes int
		for hash, count := range heightVotes {
			if hash == blockHash {
				committedVotes += count
			} else {
				rejectedVotes += count
			}
		}

		approved := qcSignatures[block.Height]
		if approved == 0 {
			approved = committedVotes
		}

		timeout := 0
		if quorum > 0 {
			missing := quorum - approved - rejectedVotes
			if missing > 0 && approved < quorum {
				timeout = missing
			}
			if timeout < 0 {
				timeout = 0
			}
		}

		result = append(result, DashboardDecisionTimeline{
			Time:     formatDashboardTimestamp(block.Timestamp),
			Height:   block.Height,
			Hash:     block.Hash,
			Proposer: block.Proposer,
			Approved: approved,
			Rejected: rejectedVotes,
			Timeout:  timeout,
		})
	}

	if len(result) == 0 {
		result = append(result, DashboardDecisionTimeline{
			Time:     "latest",
			Height:   latestHeight,
			Hash:     "",
			Proposer: "",
			Approved: int(metrics.BlocksCommitted),
			Rejected: int(metrics.ProposalsReceived - metrics.BlocksCommitted),
			Timeout:  int(metrics.ViewChanges),
		})
	}

	return result
}

func formatDashboardTimestamp(ts int64) string {
	if ts > 1_000_000_000_000 {
		return time.UnixMilli(ts).Format(time.RFC3339Nano)
	}
	return time.Unix(ts, 0).Format(time.RFC3339Nano)
}

func extractResponseLatency(metadata map[string]interface{}) (float64, bool) {
	if metadata == nil {
		return 0, false
	}

	keys := []string{
		"response_latency_ms",
		"response_time_ms",
		"latency_ms",
	}

	for _, key := range keys {
		if value, ok := metadata[key]; ok {
			switch v := value.(type) {
			case float64:
				if v >= 0 {
					return v, true
				}
			case float32:
				if v >= 0 {
					return float64(v), true
				}
			case int:
				if v >= 0 {
					return float64(v), true
				}
			case int64:
				if v >= 0 {
					return float64(v), true
				}
			case json.Number:
				if f, err := v.Float64(); err == nil && f >= 0 {
					return f, true
				}
			case string:
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && parsed >= 0 {
					return parsed, true
				}
			}
		}
	}

	return 0, false
}

func classifyThreatSeverity(score int) string {
	switch {
	case score >= 8:
		return "critical"
	case score >= 5:
		return "high"
	case score >= 3:
		return "medium"
	default:
		return "low"
	}
}

func severityPriority(label string) int {
	switch strings.ToLower(label) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func normalizeSeverityLabel(label string) string {
	switch strings.ToLower(label) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium":
		return "medium"
	default:
		return "low"
	}
}

func (s *Server) buildDashboardThreats(ctx context.Context) DashboardThreatsSection {
	result := DashboardThreatsSection{
		Timestamp: time.Now().UnixMilli(),
		Breakdown: DashboardThreatBreakdown{
			ThreatTypes: []DashboardThreatTypeSummary{},
			Severity:    map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0},
			Totals:      DashboardThreatTotals{},
		},
	}
	result.Source = "history"

	ctx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
	defer cancel()

	metrics, _ := s.loadAIMetricsSnapshot(ctx)
	if metrics != nil {
		result.DetectionLoop = metrics.Loop
		if metrics.Loop != nil && metrics.Loop.Counters != nil {
			counters := metrics.Loop.Counters
			abstained := uint64(0)
			if counters.DetectionsTotal >= counters.DetectionsPublished {
				abstained = counters.DetectionsTotal - counters.DetectionsPublished
			}
			result.LifetimeTotals = &DashboardThreatLifetimeTotals{
				Total:       counters.DetectionsTotal,
				Published:   counters.DetectionsPublished,
				Abstained:   abstained,
				RateLimited: counters.DetectionsRateLimited,
				Errors:      counters.Errors,
			}
		}
	}

	history, err := s.fetchAIDetectionHistory(ctx, 500, "", "")
	historyFetchErr := err
	if err != nil && s.logger != nil {
		s.logger.WarnContext(ctx, "failed to fetch ai detection history", utils.ZapError(err))
	}
	if history == nil {
		history = &AiDetectionHistoryResponse{}
	}

	severityMap := map[string]string{
		"ddos":              "critical",
		"dos":               "high",
		"malware":           "high",
		"anomaly":           "medium",
		"network_intrusion": "critical",
		"policy_violation":  "medium",
	}

	group := make(map[string]*DashboardThreatTypeSummary)
	var (
		responseLatencyTotal   float64
		responseLatencySamples int
	)
	statsFromHistory := &AnomalyStatsResponse{}

	for _, detection := range history.Detections {
		threatType := detection.ThreatType
		if threatType == "" {
			threatType = "unknown"
		}
		entry, ok := group[threatType]
		if !ok {
			entry = &DashboardThreatTypeSummary{ThreatType: threatType}
			group[threatType] = entry
		}
		if detection.ShouldPublish {
			entry.Published++
			result.Breakdown.Totals.Published++
		} else {
			entry.Abstained++
			result.Breakdown.Totals.Abstained++
		}
		entry.Total++
		bucket := classifyThreatSeverity(detection.Severity)
		if bucket == "medium" {
			if mapped, ok := severityMap[strings.ToLower(threatType)]; ok {
				bucket = mapped
			}
		}
		if current := entry.Severity; current == "" || severityPriority(bucket) > severityPriority(current) {
			entry.Severity = bucket
		}
		result.Breakdown.Severity[bucket]++

		statsFromHistory.TotalCount++
		switch bucket {
		case "critical":
			statsFromHistory.CriticalCount++
		case "high":
			statsFromHistory.HighCount++
		case "medium":
			statsFromHistory.MediumCount++
		default:
			statsFromHistory.LowCount++
		}

		if detection.Metadata != nil {
			if latency, ok := extractResponseLatency(detection.Metadata); ok {
				responseLatencyTotal += latency
				responseLatencySamples++
			}
		}
	}

	for _, entry := range group {
		result.Breakdown.ThreatTypes = append(result.Breakdown.ThreatTypes, *entry)
	}
	result.Breakdown.Totals.Overall = result.Breakdown.Totals.Published + result.Breakdown.Totals.Abstained

	if statsFromHistory.TotalCount > 0 {
		result.Stats = statsFromHistory
	}

	if responseLatencySamples > 0 {
		avg := responseLatencyTotal / float64(responseLatencySamples)
		result.AvgResponseTimeMs = &avg
	}

	const anomalyFeedLimit = 50
	var fallbackFeed []AnomalyResponse
	feedErr := false
	if anomalies, err := s.getAnomaliesFromDB(ctx, anomalyFeedLimit, ""); err == nil {
		result.Feed = anomalies
		fallbackFeed = anomalies
		if result.Stats == nil {
			result.Stats = summarizeAnomalyFeed(anomalies)
		}
	} else if err != nil {
		feedErr = true
		if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to load anomaly feed for dashboard", utils.ZapError(err))
		}
	}

	if statsFromHistory.TotalCount == 0 {
		if len(fallbackFeed) > 0 {
			if historyFetchErr != nil {
				result.FallbackReason = "history_unavailable"
			} else {
				result.FallbackReason = "history_empty"
			}
			result.Source = "feed_fallback"
			if result.Breakdown.Severity == nil {
				result.Breakdown.Severity = map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}
			} else {
				for _, key := range []string{"critical", "high", "medium", "low"} {
					result.Breakdown.Severity[key] = 0
				}
			}
			result.Breakdown.ThreatTypes = result.Breakdown.ThreatTypes[:0]
			result.Breakdown.Totals = DashboardThreatTotals{}

			fallbackGroup := make(map[string]*DashboardThreatTypeSummary)
			for _, anomaly := range fallbackFeed {
				threatType := anomaly.Type
				if threatType == "" {
					threatType = "unknown"
				}
				entry := fallbackGroup[threatType]
				if entry == nil {
					entry = &DashboardThreatTypeSummary{ThreatType: threatType}
					fallbackGroup[threatType] = entry
				}
				entry.Published++
				entry.Total++
				label := normalizeSeverityLabel(anomaly.Severity)
				if current := entry.Severity; current == "" || severityPriority(label) > severityPriority(current) {
					entry.Severity = label
				}
				result.Breakdown.Severity[label]++
				result.Breakdown.Totals.Published++
			}
			for _, entry := range fallbackGroup {
				result.Breakdown.ThreatTypes = append(result.Breakdown.ThreatTypes, *entry)
			}
			result.Breakdown.Totals.Overall = result.Breakdown.Totals.Published + result.Breakdown.Totals.Abstained
			if result.Stats == nil {
				result.Stats = summarizeAnomalyFeed(fallbackFeed)
			}
		} else {
			result.Source = "empty"
			if historyFetchErr != nil {
				result.FallbackReason = "history_unavailable"
			} else {
				result.FallbackReason = "history_empty"
			}
			if feedErr {
				if result.FallbackReason != "" {
					result.FallbackReason = result.FallbackReason + ",feed_unavailable"
				} else {
					result.FallbackReason = "feed_unavailable"
				}
			}
		}
	} else if historyFetchErr != nil {
		result.FallbackReason = "history_unavailable"
	}

	s.recordThreatSource(ctx, result.Source, result.FallbackReason)
	return result
}

func summarizeAnomalyFeed(feed []AnomalyResponse) *AnomalyStatsResponse {
	stats := &AnomalyStatsResponse{}
	for _, anomaly := range feed {
		stats.TotalCount++
		switch strings.ToLower(anomaly.Severity) {
		case "critical":
			stats.CriticalCount++
		case "high":
			stats.HighCount++
		case "medium":
			stats.MediumCount++
		default:
			stats.LowCount++
		}
	}
	return stats
}

func (s *Server) buildDashboardAI(ctx context.Context) DashboardAISection {
	ctx, cancel := context.WithTimeout(ctx, s.config.RequestTimeout)
	defer cancel()

	section := DashboardAISection{}

	if metrics, _ := s.loadAIMetricsSnapshot(ctx); metrics != nil {
		section.Metrics = metrics
	}

	if history, err := s.fetchAIDetectionHistory(ctx, 100, "", ""); err == nil {
		section.History = history
	}

	if suspicious, err := s.fetchAISuspiciousNodesDetailed(ctx, 20); err == nil {
		section.Suspicious = suspicious
	}

	if health, err := s.fetchAIServiceDocument(ctx, "/health"); err == nil {
		section.Health = health
	}
	if ready, err := s.fetchAIServiceDocument(ctx, "/ready"); err == nil {
		section.Ready = ready
	}

	return section
}

func (s *Server) fetchAIServiceDocument(ctx context.Context, path string) (map[string]interface{}, error) {
	if s.aiClient == nil || strings.TrimSpace(s.aiBaseURL) == "" {
		return nil, fmt.Errorf("ai service integration disabled")
	}

	resp, err := s.performAIRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ai service %s returned status %d", path, resp.StatusCode)
	}

	var document map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
		return nil, err
	}

	return document, nil
}
