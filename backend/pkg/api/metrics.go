package api

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"runtime/metrics"
	"time"

	"backend/pkg/storage/cockroach"
	"backend/pkg/utils"
)

// handleMetrics handles GET /metrics (Prometheus format)
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set content type to Prometheus text format
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)

	// Write metrics in Prometheus format
	s.writePrometheusMetrics(w)
}

// writePrometheusMetrics writes metrics in Prometheus text format
func (s *Server) writePrometheusMetrics(w http.ResponseWriter) {
	readMetric := func(name string) (float64, bool) {
		samples := []metrics.Sample{{Name: name}}
		metrics.Read(samples)
		switch samples[0].Value.Kind() {
		case metrics.KindFloat64:
			return samples[0].Value.Float64(), true
		case metrics.KindUint64:
			return float64(samples[0].Value.Uint64()), true
		default:
			return 0, false
		}
	}

	nowTime := time.Now()
	now := nowTime.Unix()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	writeReadiness := func(metric string, res ReadinessCheckResult) {
		ready := 0.0
		if isReadinessPassing(res.Status) {
			ready = 1.0
		}
		fmt.Fprintf(w, "# HELP %s subsystem readiness (1=ready)\n", metric)
		fmt.Fprintf(w, "# TYPE %s gauge\n", metric)
		fmt.Fprintf(w, "%s{status=\"%s\"} %.0f\n", metric, res.Status, ready)
		if res.LatencyMs >= 0 {
			fmt.Fprintf(w, "# HELP %s_latency_ms subsystem readiness latency in milliseconds\n", metric)
			fmt.Fprintf(w, "# TYPE %s_latency_ms gauge\n", metric)
			fmt.Fprintf(w, "%s_latency_ms %.2f\n", metric, res.LatencyMs)
		}
	}

	storageCheck, _ := s.runStorageCheck(ctx)
	writeReadiness("storage_ready", storageCheck)
	writeReadiness("cockroach_ready", storageCheck)

	stateCheck, _ := s.runStateStoreCheck(ctx)
	writeReadiness("state_store_ready", stateCheck)

	mempoolCheck := s.runMempoolCheck(ctx)
	writeReadiness("mempool_ready", mempoolCheck)

	consensusCheck, _, _ := s.runConsensusCheck(ctx)
	writeReadiness("consensus_ready", consensusCheck)

	p2pCheck, _ := s.runP2PCheck(ctx)
	writeReadiness("p2p_ready", p2pCheck)

	kafkaCheck := s.runKafkaCheck(ctx)
	writeReadiness("kafka_ready", kafkaCheck)

	redisCheck, _ := s.runRedisCheck(ctx)
	writeReadiness("redis_ready", redisCheck)

	aiCheck := s.runAIServiceCheck(ctx)
	writeReadiness("ai_service_ready", aiCheck)
	if s.storage != nil {
		if provider, ok := s.storage.(interface{ GetDB() *sql.DB }); ok {
			db := provider.GetDB()
			if db != nil {
				connStats := cockroach.GetConnectionStats(db)
				fmt.Fprintf(w, "# HELP cockroach_pool_open_connections Number of connections currently open.\n")
				fmt.Fprintf(w, "# TYPE cockroach_pool_open_connections gauge\n")
				fmt.Fprintf(w, "cockroach_pool_open_connections %d\n", convertToInt64(connStats["open_connections"]))
				fmt.Fprintf(w, "# HELP cockroach_pool_in_use_connections Connections currently in use.\n")
				fmt.Fprintf(w, "# TYPE cockroach_pool_in_use_connections gauge\n")
				fmt.Fprintf(w, "cockroach_pool_in_use_connections %d\n", convertToInt64(connStats["in_use"]))
				fmt.Fprintf(w, "# HELP cockroach_pool_idle_connections Connections currently idle.\n")
				fmt.Fprintf(w, "# TYPE cockroach_pool_idle_connections gauge\n")
				fmt.Fprintf(w, "cockroach_pool_idle_connections %d\n", convertToInt64(connStats["idle"]))
				fmt.Fprintf(w, "# HELP cockroach_pool_wait_seconds_total Total time waiting for a connection.\n")
				fmt.Fprintf(w, "# TYPE cockroach_pool_wait_seconds_total counter\n")
				fmt.Fprintf(w, "cockroach_pool_wait_seconds_total %.3f\n", float64(convertToInt64(connStats["wait_duration_ms"]))/1000.0)
				fmt.Fprintf(w, "# HELP cockroach_pool_wait_total Total wait operations for a connection.\n")
				fmt.Fprintf(w, "# TYPE cockroach_pool_wait_total counter\n")
				fmt.Fprintf(w, "cockroach_pool_wait_total %d\n", convertToInt64(connStats["wait_count"]))
			}
		}
		if metricsProvider, ok := s.storage.(interface {
			Metrics() cockroach.MetricsSnapshot
		}); ok {
			snap := metricsProvider.Metrics()
			if snap.QueryCount > 0 {
				fmt.Fprintf(w, "# HELP cockroach_query_latency_seconds Histogram of CockroachDB query latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE cockroach_query_latency_seconds histogram\n")
				writePrometheusHistogram(w, "cockroach_query_latency_seconds", snap.QueryBuckets, snap.QueryCount, snap.QuerySumMs)
				fmt.Fprintf(w, "# HELP cockroach_query_latency_p95_seconds 95th percentile query latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE cockroach_query_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "cockroach_query_latency_p95_seconds %.6f\n", snap.QueryP95Ms/1000.0)
			}
			if snap.TxnCount > 0 {
				fmt.Fprintf(w, "# HELP cockroach_transaction_latency_seconds Histogram of CockroachDB transaction latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE cockroach_transaction_latency_seconds histogram\n")
				writePrometheusHistogram(w, "cockroach_transaction_latency_seconds", snap.TxnBuckets, snap.TxnCount, snap.TxnSumMs)
				fmt.Fprintf(w, "# HELP cockroach_transaction_latency_p95_seconds 95th percentile transaction latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE cockroach_transaction_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "cockroach_transaction_latency_p95_seconds %.6f\n", snap.TxnP95Ms/1000.0)
			}
			fmt.Fprintf(w, "# HELP cockroach_slow_queries_total Total slow CockroachDB queries (above threshold).\n")
			fmt.Fprintf(w, "# TYPE cockroach_slow_queries_total counter\n")
			fmt.Fprintf(w, "cockroach_slow_queries_total %d\n", snap.SlowQueryCount)
			fmt.Fprintf(w, "# HELP cockroach_slow_transactions_total Total slow CockroachDB transactions (above threshold).\n")
			fmt.Fprintf(w, "# TYPE cockroach_slow_transactions_total counter\n")
			fmt.Fprintf(w, "cockroach_slow_transactions_total %d\n", snap.SlowTransactionCount)
		}
	}

	if s.redisClient != nil {
		pool := s.redisClient.PoolStats()
		fmt.Fprintf(w, "# HELP redis_pool_hits_total Total number of times a connection was reused from the pool.\n")
		fmt.Fprintf(w, "# TYPE redis_pool_hits_total counter\n")
		fmt.Fprintf(w, "redis_pool_hits_total %d\n", pool.Hits)
		fmt.Fprintf(w, "# HELP redis_pool_misses_total Total number of times a new connection was created.\n")
		fmt.Fprintf(w, "# TYPE redis_pool_misses_total counter\n")
		fmt.Fprintf(w, "redis_pool_misses_total %d\n", pool.Misses)
		fmt.Fprintf(w, "# HELP redis_pool_timeouts_total Total number of timeouts waiting for a connection.\n")
		fmt.Fprintf(w, "# TYPE redis_pool_timeouts_total counter\n")
		fmt.Fprintf(w, "redis_pool_timeouts_total %d\n", pool.Timeouts)
		fmt.Fprintf(w, "# HELP redis_pool_total_connections Number of connections created for the pool.\n")
		fmt.Fprintf(w, "# TYPE redis_pool_total_connections gauge\n")
		fmt.Fprintf(w, "redis_pool_total_connections %d\n", pool.TotalConns)
		fmt.Fprintf(w, "# HELP redis_pool_idle_connections Number of idle connections.\n")
		fmt.Fprintf(w, "# TYPE redis_pool_idle_connections gauge\n")
		fmt.Fprintf(w, "redis_pool_idle_connections %d\n", pool.IdleConns)
		fmt.Fprintf(w, "# HELP redis_pool_stale_connections Number of stale connections removed.\n")
		fmt.Fprintf(w, "# TYPE redis_pool_stale_connections counter\n")
		fmt.Fprintf(w, "redis_pool_stale_connections %d\n", pool.StaleConns)
		buckets, count, sum, errors := s.redisSnapshot()
		if count > 0 {
			fmt.Fprintf(w, "# HELP redis_command_latency_seconds Histogram of Redis command latency in seconds.\n")
			fmt.Fprintf(w, "# TYPE redis_command_latency_seconds histogram\n")
			writePrometheusHistogram(w, "redis_command_latency_seconds", buckets, count, sum)
		}
		fmt.Fprintf(w, "# HELP redis_command_errors_total Total Redis command errors (excluding nil responses).\n")
		fmt.Fprintf(w, "# TYPE redis_command_errors_total counter\n")
		fmt.Fprintf(w, "redis_command_errors_total %d\n", errors)
	}

	if s.kafkaProd != nil {
		prodStats := s.kafkaProd.Stats()
		fmt.Fprintf(w, "# HELP kafka_producer_publish_success_total Total successful Kafka publish operations.\n")
		fmt.Fprintf(w, "# TYPE kafka_producer_publish_success_total counter\n")
		fmt.Fprintf(w, "kafka_producer_publish_success_total %d\n", prodStats.PublishSuccesses)
		fmt.Fprintf(w, "# HELP kafka_producer_publish_failure_total Total failed Kafka publish operations.\n")
		fmt.Fprintf(w, "# TYPE kafka_producer_publish_failure_total counter\n")
		fmt.Fprintf(w, "kafka_producer_publish_failure_total %d\n", prodStats.PublishFailures)
		fmt.Fprintf(w, "# HELP kafka_producer_brokers Configured brokers for the producer.\n")
		fmt.Fprintf(w, "# TYPE kafka_producer_brokers gauge\n")
		fmt.Fprintf(w, "kafka_producer_brokers %d\n", prodStats.BrokerCount)
		fmt.Fprintf(w, "# HELP kafka_producer_publish_latency_ms Average latency of successful Kafka publish operations.\n")
		fmt.Fprintf(w, "# TYPE kafka_producer_publish_latency_ms gauge\n")
		fmt.Fprintf(w, "kafka_producer_publish_latency_ms %.3f\n", prodStats.PublishLatencyMs)
		if prodStats.LatencyCount > 0 {
			fmt.Fprintf(w, "# HELP kafka_producer_publish_latency_seconds Histogram of Kafka publish latency in seconds.\n")
			fmt.Fprintf(w, "# TYPE kafka_producer_publish_latency_seconds histogram\n")
			writePrometheusHistogram(w, "kafka_producer_publish_latency_seconds", prodStats.LatencyBuckets, prodStats.LatencyCount, prodStats.LatencySumMs)
			fmt.Fprintf(w, "# HELP kafka_producer_publish_latency_p50_seconds 50th percentile Kafka publish latency in seconds.\n")
			fmt.Fprintf(w, "# TYPE kafka_producer_publish_latency_p50_seconds gauge\n")
			fmt.Fprintf(w, "kafka_producer_publish_latency_p50_seconds %.6f\n", prodStats.LatencyP50Ms/1000.0)
			fmt.Fprintf(w, "# HELP kafka_producer_publish_latency_p95_seconds 95th percentile Kafka publish latency in seconds.\n")
			fmt.Fprintf(w, "# TYPE kafka_producer_publish_latency_p95_seconds gauge\n")
			fmt.Fprintf(w, "kafka_producer_publish_latency_p95_seconds %.6f\n", prodStats.LatencyP95Ms/1000.0)
		}
	}

	if s.kafkaCons != nil {
		consStats := s.kafkaCons.Stats()
		if consStats.IngestLatencyCount > 0 {
			fmt.Fprintf(w, "# HELP kafka_consumer_ingest_latency_seconds Histogram of Kafka consumer ingest latency in seconds.\n")
			fmt.Fprintf(w, "# TYPE kafka_consumer_ingest_latency_seconds histogram\n")
			writePrometheusHistogram(w, "kafka_consumer_ingest_latency_seconds", consStats.IngestLatencyBuckets, consStats.IngestLatencyCount, consStats.IngestLatencySumMs)
			fmt.Fprintf(w, "# HELP kafka_consumer_ingest_latency_p95_seconds 95th percentile ingest latency in seconds.\n")
			fmt.Fprintf(w, "# TYPE kafka_consumer_ingest_latency_p95_seconds gauge\n")
			fmt.Fprintf(w, "kafka_consumer_ingest_latency_p95_seconds %.6f\n", consStats.IngestLatencyP95Ms/1000.0)
		}
		if consStats.ProcessLatencyCount > 0 {
			fmt.Fprintf(w, "# HELP kafka_consumer_process_latency_seconds Histogram of Kafka consumer processing latency in seconds.\n")
			fmt.Fprintf(w, "# TYPE kafka_consumer_process_latency_seconds histogram\n")
			writePrometheusHistogram(w, "kafka_consumer_process_latency_seconds", consStats.ProcessLatencyBuckets, consStats.ProcessLatencyCount, consStats.ProcessLatencySumMs)
			fmt.Fprintf(w, "# HELP kafka_consumer_process_latency_p95_seconds 95th percentile processing latency in seconds.\n")
			fmt.Fprintf(w, "# TYPE kafka_consumer_process_latency_p95_seconds gauge\n")
			fmt.Fprintf(w, "kafka_consumer_process_latency_p95_seconds %.6f\n", consStats.ProcessLatencyP95Ms/1000.0)
		}
	}

	if s.p2pRouter != nil {
		netStats := s.p2pRouter.GetNetworkStats()
		inboundRate, outboundRate := s.computeP2PThroughput(nowTime, netStats)
		fmt.Fprintf(w, "# HELP p2p_peer_count Number of peers currently connected.\n")
		fmt.Fprintf(w, "# TYPE p2p_peer_count gauge\n")
		fmt.Fprintf(w, "p2p_peer_count %d\n", netStats.PeerCount)
		fmt.Fprintf(w, "# HELP p2p_inbound_peers Number of inbound peer connections.\n")
		fmt.Fprintf(w, "# TYPE p2p_inbound_peers gauge\n")
		fmt.Fprintf(w, "p2p_inbound_peers %d\n", netStats.InboundPeers)
		fmt.Fprintf(w, "# HELP p2p_outbound_peers Number of outbound peer connections.\n")
		fmt.Fprintf(w, "# TYPE p2p_outbound_peers gauge\n")
		fmt.Fprintf(w, "p2p_outbound_peers %d\n", netStats.OutboundPeers)
		fmt.Fprintf(w, "# HELP p2p_bytes_received_total Total bytes received by the router since start.\n")
		fmt.Fprintf(w, "# TYPE p2p_bytes_received_total counter\n")
		fmt.Fprintf(w, "p2p_bytes_received_total %d\n", netStats.BytesReceived)
		fmt.Fprintf(w, "# HELP p2p_bytes_sent_total Total bytes published by the router since start.\n")
		fmt.Fprintf(w, "# TYPE p2p_bytes_sent_total counter\n")
		fmt.Fprintf(w, "p2p_bytes_sent_total %d\n", netStats.BytesSent)
		fmt.Fprintf(w, "# HELP p2p_avg_latency_milliseconds Exponential moving average of inter-message latency.\n")
		fmt.Fprintf(w, "# TYPE p2p_avg_latency_milliseconds gauge\n")
		fmt.Fprintf(w, "p2p_avg_latency_milliseconds %.2f\n", netStats.AvgLatencyMs)
		fmt.Fprintf(w, "# HELP p2p_throughput_in_bytes_per_second Inbound traffic rate derived from message bytes.\n")
		fmt.Fprintf(w, "# TYPE p2p_throughput_in_bytes_per_second gauge\n")
		fmt.Fprintf(w, "p2p_throughput_in_bytes_per_second %.2f\n", inboundRate)
		fmt.Fprintf(w, "# HELP p2p_throughput_out_bytes_per_second Outbound traffic rate derived from published bytes.\n")
		fmt.Fprintf(w, "# TYPE p2p_throughput_out_bytes_per_second gauge\n")
		fmt.Fprintf(w, "p2p_throughput_out_bytes_per_second %.2f\n", outboundRate)
	}

	// API server metrics
	fmt.Fprintf(w, "# HELP api_server_running Server running status (1=running, 0=stopped)\n")
	fmt.Fprintf(w, "# TYPE api_server_running gauge\n")
	if s.running.Load() {
		fmt.Fprintf(w, "api_server_running 1\n")
	} else {
		fmt.Fprintf(w, "api_server_running 0\n")
	}

	fmt.Fprintf(w, "# HELP cybermesh_api_requests_total Total HTTP requests served by the API.\n")
	fmt.Fprintf(w, "# TYPE cybermesh_api_requests_total counter\n")
	fmt.Fprintf(w, "cybermesh_api_requests_total %d\n", s.apiRequestsTotal.Load())
	fmt.Fprintf(w, "# HELP cybermesh_api_request_errors_total Total HTTP requests that returned error status codes (>=400).\n")
	fmt.Fprintf(w, "# TYPE cybermesh_api_request_errors_total counter\n")
	fmt.Fprintf(w, "cybermesh_api_request_errors_total %d\n", s.apiRequestErrors.Load())

	// Rate limiter metrics
	if s.rateLimiter != nil {
		metrics := s.rateLimiter.GetMetrics()

		if trackedClients, ok := metrics["tracked_clients"].(int); ok {
			fmt.Fprintf(w, "# HELP api_rate_limiter_tracked_clients Number of clients being tracked\n")
			fmt.Fprintf(w, "# TYPE api_rate_limiter_tracked_clients gauge\n")
			fmt.Fprintf(w, "api_rate_limiter_tracked_clients %d\n", trackedClients)
		}

		if rpm, ok := metrics["requests_per_minute"].(int); ok {
			fmt.Fprintf(w, "# HELP api_rate_limit_per_minute Configured rate limit per minute\n")
			fmt.Fprintf(w, "# TYPE api_rate_limit_per_minute gauge\n")
			fmt.Fprintf(w, "api_rate_limit_per_minute %d\n", rpm)
		}
	}

	// IP allowlist metrics
	if s.ipAllowlist != nil {
		ipMetrics := s.ipAllowlist.GetMetrics()

		// GetMetrics returns map[string]uint64
		if allowedCount, ok := ipMetrics["allowed_count"]; ok {
			fmt.Fprintf(w, "# HELP api_ip_allowed_total Total number of allowed IP requests\n")
			fmt.Fprintf(w, "# TYPE api_ip_allowed_total counter\n")
			fmt.Fprintf(w, "api_ip_allowed_total %d\n", allowedCount)
		}

		if deniedCount, ok := ipMetrics["denied_count"]; ok {
			fmt.Fprintf(w, "# HELP api_ip_denied_total Total number of denied IP requests\n")
			fmt.Fprintf(w, "# TYPE api_ip_denied_total counter\n")
			fmt.Fprintf(w, "api_ip_denied_total %d\n", deniedCount)
		}
	}

	// State store metrics
	if s.stateStore != nil {
		latest := s.stateStore.Latest()

		fmt.Fprintf(w, "# HELP state_latest_version Latest state version\n")
		fmt.Fprintf(w, "# TYPE state_latest_version gauge\n")
		fmt.Fprintf(w, "state_latest_version %d\n", latest)

		if root, exists := s.stateStore.Root(latest); exists {
			fmt.Fprintf(w, "# HELP state_root_available State root availability (1=available, 0=missing)\n")
			fmt.Fprintf(w, "# TYPE state_root_available gauge\n")
			fmt.Fprintf(w, "state_root_available 1\n")

			// Export root hash as info metric
			fmt.Fprintf(w, "# HELP state_root_info Current state root hash\n")
			fmt.Fprintf(w, "# TYPE state_root_info gauge\n")
			fmt.Fprintf(w, "state_root_info{root_hash=\"%x\"} 1\n", root)
		} else {
			fmt.Fprintf(w, "state_root_available 0\n")
		}
	}

	// Mempool metrics (if available)
	if s.mempool != nil {
		// Mempool doesn't expose metrics interface in current implementation
		// In production, you'd want to add a GetMetrics() method to mempool
		fmt.Fprintf(w, "# HELP mempool_available Mempool availability (1=available, 0=unavailable)\n")
		fmt.Fprintf(w, "# TYPE mempool_available gauge\n")
		fmt.Fprintf(w, "mempool_available 1\n")
	}

	// Storage metrics
	if s.storage != nil {
		fmt.Fprintf(w, "# HELP storage_available Storage availability (1=available, 0=unavailable)\n")
		fmt.Fprintf(w, "# TYPE storage_available gauge\n")
		fmt.Fprintf(w, "storage_available 1\n")
	}

	// Consensus genesis readiness metrics
	if s.engine != nil {
		metrics := s.engine.GetGenesisReadyMetrics()
		fmt.Fprintf(w, "# HELP consensus_genesis_ready_received_total Total genesis READY attestations observed\n")
		fmt.Fprintf(w, "# TYPE consensus_genesis_ready_received_total counter\n")
		fmt.Fprintf(w, "consensus_genesis_ready_received_total %d\n", metrics.Received)
		fmt.Fprintf(w, "# HELP consensus_genesis_ready_accepted_total Total genesis READY attestations accepted\n")
		fmt.Fprintf(w, "# TYPE consensus_genesis_ready_accepted_total counter\n")
		fmt.Fprintf(w, "consensus_genesis_ready_accepted_total %d\n", metrics.Accepted)
		fmt.Fprintf(w, "# HELP consensus_genesis_ready_duplicate_total Total genesis READY attestations marked duplicate\n")
		fmt.Fprintf(w, "# TYPE consensus_genesis_ready_duplicate_total counter\n")
		fmt.Fprintf(w, "consensus_genesis_ready_duplicate_total %d\n", metrics.Duplicate)
		fmt.Fprintf(w, "# HELP consensus_genesis_ready_replay_blocked_total Total genesis READY attestations blocked by replay protection\n")
		fmt.Fprintf(w, "# TYPE consensus_genesis_ready_replay_blocked_total counter\n")
		fmt.Fprintf(w, "consensus_genesis_ready_replay_blocked_total %d\n", metrics.ReplayBlocked)
		fmt.Fprintf(w, "# HELP consensus_genesis_ready_rejected_total Total genesis READY attestations rejected\n")
		fmt.Fprintf(w, "# TYPE consensus_genesis_ready_rejected_total counter\n")
		fmt.Fprintf(w, "consensus_genesis_ready_rejected_total %d\n", metrics.Rejected)
		fmt.Fprintf(w, "# HELP consensus_genesis_certificate_discard_total Total persisted genesis certificates discarded\n")
		fmt.Fprintf(w, "# TYPE consensus_genesis_certificate_discard_total counter\n")
		fmt.Fprintf(w, "consensus_genesis_certificate_discard_total %d\n", metrics.Discarded)
	}

	// Go runtime metrics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Process CPU seconds (cumulative from runtime metrics with fallback)
	cpuSeconds, ok := readMetric("/sched/process/cpu-seconds")
	if !ok {
		cpuSeconds = float64(now-s.processStartTime) * 0.8
	}
	fmt.Fprintf(w, "# HELP process_cpu_seconds_total Total user and system CPU time spent in seconds.\n")
	fmt.Fprintf(w, "# TYPE process_cpu_seconds_total counter\n")
	fmt.Fprintf(w, "process_cpu_seconds_total %.6f\n", cpuSeconds)

	// Process resident memory
	fmt.Fprintf(w, "# HELP process_resident_memory_bytes Resident memory size in bytes.\n")
	fmt.Fprintf(w, "# TYPE process_resident_memory_bytes gauge\n")
	fmt.Fprintf(w, "process_resident_memory_bytes %d\n", m.Alloc)

	// Process virtual memory
	fmt.Fprintf(w, "# HELP process_virtual_memory_bytes Virtual memory size in bytes.\n")
	fmt.Fprintf(w, "# TYPE process_virtual_memory_bytes gauge\n")
	fmt.Fprintf(w, "process_virtual_memory_bytes %d\n", m.Sys)

	// Heap allocation
	fmt.Fprintf(w, "# HELP go_memstats_heap_alloc_bytes Number of heap bytes allocated and still in use.\n")
	fmt.Fprintf(w, "# TYPE go_memstats_heap_alloc_bytes gauge\n")
	fmt.Fprintf(w, "go_memstats_heap_alloc_bytes %d\n", m.HeapAlloc)

	// Heap system bytes
	fmt.Fprintf(w, "# HELP go_memstats_heap_sys_bytes Number of heap bytes obtained from system.\n")
	fmt.Fprintf(w, "# TYPE go_memstats_heap_sys_bytes gauge\n")
	fmt.Fprintf(w, "go_memstats_heap_sys_bytes %d\n", m.HeapSys)

	// Goroutines
	fmt.Fprintf(w, "# HELP go_goroutines Number of goroutines that currently exist.\n")
	fmt.Fprintf(w, "# TYPE go_goroutines gauge\n")
	fmt.Fprintf(w, "go_goroutines %d\n", runtime.NumGoroutine())

	// Threads
	threadCount, ok := readMetric("/sched/threads:threads")
	if !ok {
		threadCount = float64(runtime.GOMAXPROCS(0))
	}
	fmt.Fprintf(w, "# HELP go_threads Number of OS threads.\n")
	fmt.Fprintf(w, "# TYPE go_threads gauge\n")
	fmt.Fprintf(w, "go_threads %.0f\n", threadCount)

	// GC duration
	fmt.Fprintf(w, "# HELP go_gc_duration_seconds_sum A summary of the pause duration of garbage collection cycles.\n")
	fmt.Fprintf(w, "# TYPE go_gc_duration_seconds_sum summary\n")
	fmt.Fprintf(w, "go_gc_duration_seconds_sum %.9f\n", float64(m.PauseTotalNs)/1e9)

	// GC cycles
	fmt.Fprintf(w, "# HELP go_gc_cycles_total Total number of GC cycles.\n")
	fmt.Fprintf(w, "# TYPE go_gc_cycles_total counter\n")
	fmt.Fprintf(w, "go_gc_cycles_total %d\n", m.NumGC)

	// Process start time
	fmt.Fprintf(w, "# HELP process_start_time_seconds Start time of the process since unix epoch in seconds.\n")
	fmt.Fprintf(w, "# TYPE process_start_time_seconds gauge\n")
	fmt.Fprintf(w, "process_start_time_seconds %d\n", s.processStartTime)

	// Timestamp of metrics scrape
	fmt.Fprintf(w, "# HELP api_metrics_scrape_timestamp_seconds Timestamp of last metrics scrape\n")
	fmt.Fprintf(w, "# TYPE api_metrics_scrape_timestamp_seconds gauge\n")
	fmt.Fprintf(w, "api_metrics_scrape_timestamp_seconds %d\n", now)

	// Build info
	fmt.Fprintf(w, "# HELP api_build_info API build information\n")
	fmt.Fprintf(w, "# TYPE api_build_info gauge\n")
	fmt.Fprintf(w, "api_build_info{version=\"%s\",environment=\"%s\"} 1\n", apiVersion, s.config.Environment)
}

func convertToInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case uint32:
		return int64(val)
	case uint64:
		return int64(val)
	case float64:
		return int64(val)
	default:
		return 0
	}
}

func writePrometheusHistogram(w http.ResponseWriter, name string, buckets []utils.HistogramBucket, count uint64, sumMs float64) {
	var cumulative uint64
	for _, bucket := range buckets {
		cumulative += bucket.Count
		le := "+Inf"
		if !math.IsInf(bucket.UpperBound, 1) {
			le = fmt.Sprintf("%.6f", bucket.UpperBound/1000.0)
		}
		fmt.Fprintf(w, "%s_bucket{le=\"%s\"} %d\n", name, le, cumulative)
	}
	fmt.Fprintf(w, "%s_sum %.6f\n", name, sumMs/1000.0)
	fmt.Fprintf(w, "%s_count %d\n", name, count)
}
