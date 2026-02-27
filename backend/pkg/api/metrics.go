package api

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	runtimeMetrics "runtime/metrics"
	"strings"
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

	var buf bytes.Buffer
	s.writePrometheusMetrics(&buf)
	filtered := s.filterPrometheusMetrics(buf.Bytes())
	useGzip := s.config.MetricsCompress && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")

	if useGzip {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		gz := gzip.NewWriter(w)
		if _, err := gz.Write(filtered); err != nil {
			gz.Close()
			s.logger.Warn("failed to write gzip metrics", utils.ZapError(err))
			return
		}
		if err := gz.Close(); err != nil {
			s.logger.Warn("failed to finalize gzip metrics", utils.ZapError(err))
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(filtered); err != nil {
		s.logger.Warn("failed to write metrics response", utils.ZapError(err))
	}
}

func (s *Server) filterPrometheusMetrics(data []byte) []byte {
	if len(s.config.MetricsAllowedPrefixes) == 0 {
		return data
	}
	allowMetric := func(name string) bool {
		for _, prefix := range s.config.MetricsAllowedPrefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 1024), 4*1024*1024)
	var currentMetric string
	include := false
	var filtered bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "# HELP"):
			fields := strings.Fields(trimmed)
			if len(fields) >= 3 {
				currentMetric = fields[2]
				include = allowMetric(currentMetric)
			} else {
				include = false
			}
			if include {
				filtered.WriteString(line)
				filtered.WriteByte('\n')
			}
		case strings.HasPrefix(trimmed, "# TYPE"):
			fields := strings.Fields(trimmed)
			if len(fields) >= 3 {
				currentMetric = fields[2]
				include = allowMetric(currentMetric)
			}
			if include {
				filtered.WriteString(line)
				filtered.WriteByte('\n')
			}
		case trimmed == "":
			if include {
				filtered.WriteByte('\n')
			}
		default:
			metricName := trimmed
			if idx := strings.IndexAny(metricName, " {"); idx >= 0 {
				metricName = metricName[:idx]
			}
			if allowMetric(metricName) {
				currentMetric = metricName
				include = true
			} else if metricName != currentMetric {
				include = false
			}

			if include {
				filtered.WriteString(line)
				filtered.WriteByte('\n')
			}
		}
	}

	if err := scanner.Err(); err != nil && s.logger != nil {
		s.logger.Warn("failed to filter prometheus metrics", utils.ZapError(err))
	}

	return filtered.Bytes()
}

// writePrometheusMetrics writes metrics in Prometheus text format
func (s *Server) writePrometheusMetrics(w io.Writer) {
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
			for _, stage := range []string{"upsert_block", "upsert_transactions", "upsert_snapshot", "commit"} {
				count, ok := snap.PersistStageCount[stage]
				if !ok || count == 0 {
					continue
				}
				base := "cockroach_persist_" + stage + "_latency_seconds"
				fmt.Fprintf(w, "# HELP %s Histogram of CockroachDB persist stage latency in seconds.\n", base)
				fmt.Fprintf(w, "# TYPE %s histogram\n", base)
				writePrometheusHistogram(w, base, snap.PersistStageBuckets[stage], count, snap.PersistStageSumMs[stage])
				fmt.Fprintf(w, "# HELP %s_p95_seconds 95th percentile persist stage latency in seconds.\n", base)
				fmt.Fprintf(w, "# TYPE %s_p95_seconds gauge\n", base)
				fmt.Fprintf(w, "%s_p95_seconds %.6f\n", base, snap.PersistStageP95Ms[stage]/1000.0)
			}
			if len(snap.PersistFailureClassTotals) > 0 {
				fmt.Fprintf(w, "# HELP cockroach_persist_failures_total Total CockroachDB persist failures by stage/class.\n")
				fmt.Fprintf(w, "# TYPE cockroach_persist_failures_total counter\n")
				for key, count := range snap.PersistFailureClassTotals {
					stage := key
					class := "other"
					if idx := strings.Index(key, ":"); idx > 0 && idx < len(key)-1 {
						stage = key[:idx]
						class = key[idx+1:]
					}
					fmt.Fprintf(w, "cockroach_persist_failures_total{stage=\"%s\",class=\"%s\"} %d\n", stage, class, count)
				}
			}
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
		fmt.Fprintf(w, "# HELP kafka_producer_logs_throttled_total Total producer success logs suppressed by throttling.\n")
		fmt.Fprintf(w, "# TYPE kafka_producer_logs_throttled_total counter\n")
		fmt.Fprintf(w, "kafka_producer_logs_throttled_total %d\n", prodStats.LogsThrottled)
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

	if s.outboxStats != nil {
		if commitStats, ok := s.outboxStats.GetCommitPathStats(); ok {
			fmt.Fprintf(w, "# HELP control_commit_log_sample_every_n Commit info log sampling interval.\n")
			fmt.Fprintf(w, "# TYPE control_commit_log_sample_every_n gauge\n")
			fmt.Fprintf(w, "control_commit_log_sample_every_n %d\n", commitStats.LogSampleEvery)
			fmt.Fprintf(w, "# HELP control_commit_logs_suppressed_total Total commit-path logs suppressed by sampling/throttling.\n")
			fmt.Fprintf(w, "# TYPE control_commit_logs_suppressed_total counter\n")
			fmt.Fprintf(w, "control_commit_logs_suppressed_total %d\n", commitStats.LogsSuppressed)
		}

		if ackStats, ok := s.outboxStats.GetPolicyAckConsumerStats(); ok {
			fmt.Fprintf(w, "# HELP control_policy_ack_processed_total Total ACK events successfully persisted.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_processed_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_processed_total %d\n", ackStats.ProcessedTotal)
			fmt.Fprintf(w, "# HELP control_policy_ack_rejected_total Total ACK events rejected as invalid/permanent.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_rejected_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_rejected_total %d\n", ackStats.RejectedTotal)
			fmt.Fprintf(w, "# HELP control_policy_ack_store_retry_attempts_total Total ACK store retry attempts.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_store_retry_attempts_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_store_retry_attempts_total %d\n", ackStats.StoreRetryAttempts)
			fmt.Fprintf(w, "# HELP control_policy_ack_store_retry_exhausted_total Total ACK events that exhausted store retries.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_store_retry_exhausted_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_store_retry_exhausted_total %d\n", ackStats.StoreRetryExhausted)
			fmt.Fprintf(w, "# HELP control_policy_ack_dlq_published_total Total ACK rejects published to DLQ.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_dlq_published_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_dlq_published_total %d\n", ackStats.DLQPublishedTotal)
			fmt.Fprintf(w, "# HELP control_policy_ack_dlq_publish_failures_total Total ACK DLQ publish failures.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_dlq_publish_failures_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_dlq_publish_failures_total %d\n", ackStats.DLQPublishFailures)
			fmt.Fprintf(w, "# HELP control_policy_ack_loop_errors_total Total consumer-loop errors from Kafka Consume.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_loop_errors_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_loop_errors_total %d\n", ackStats.LoopErrors)
			fmt.Fprintf(w, "# HELP control_policy_ack_work_queue_waits_total Total times ACK consume loop waited for worker queue capacity.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_work_queue_waits_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_work_queue_waits_total %d\n", ackStats.WorkQueueWaits)
			fmt.Fprintf(w, "# HELP control_policy_ack_soft_throttle_activations_total Total soft-throttle activations in ACK consumer.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_soft_throttle_activations_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_soft_throttle_activations_total %d\n", ackStats.SoftThrottleActivations)
			fmt.Fprintf(w, "# HELP control_policy_ack_logs_throttled_total Total ACK consumer logs suppressed by throttling.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_logs_throttled_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_logs_throttled_total %d\n", ackStats.LogsThrottled)
		}
		if causal, ok := s.outboxStats.GetPolicyAckCausalStats(); ok {
			fmt.Fprintf(w, "# HELP control_policy_causal_skew_corrections_total Total causal latency corrections due to clock skew.\n")
			fmt.Fprintf(w, "# TYPE control_policy_causal_skew_corrections_total counter\n")
			fmt.Fprintf(w, "control_policy_causal_skew_corrections_total %d\n", causal.SkewCorrectionsTotal)
			if causal.AIToAckCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_ai_to_ack_latency_seconds Histogram of corrected AI-to-ACK causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_ai_to_ack_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_ai_to_ack_latency_seconds", causal.AIToAckBuckets, causal.AIToAckCount, causal.AIToAckSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_ai_to_ack_latency_p95_seconds 95th percentile corrected AI-to-ACK latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_ai_to_ack_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_ai_to_ack_latency_p95_seconds %.6f\n", causal.AIToAckP95Ms/1000.0)
			}
			if causal.PublishToAckCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_publish_to_ack_latency_seconds Histogram of corrected publish-to-ACK causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_publish_to_ack_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_publish_to_ack_latency_seconds", causal.PublishToAckBuckets, causal.PublishToAckCount, causal.PublishToAckSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_publish_to_ack_latency_p95_seconds 95th percentile corrected publish-to-ACK latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_publish_to_ack_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_publish_to_ack_latency_p95_seconds %.6f\n", causal.PublishToAckP95Ms/1000.0)
			}
		}

		if outbox, ok := s.outboxStats.GetPolicyOutboxDispatcherStats(); ok {
			fmt.Fprintf(w, "# HELP control_policy_outbox_dispatcher_lease_acquire_attempts_total Total lease acquire attempts by dispatcher.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_dispatcher_lease_acquire_attempts_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_dispatcher_lease_acquire_attempts_total %d\n", outbox.LeaseAcquireAttempts)
			fmt.Fprintf(w, "# HELP control_policy_outbox_dispatcher_lease_acquire_success_total Total successful lease acquisitions.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_dispatcher_lease_acquire_success_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_dispatcher_lease_acquire_success_total %d\n", outbox.LeaseAcquireSuccesses)
			fmt.Fprintf(w, "# HELP control_policy_outbox_dispatcher_lease_acquire_errors_total Total lease acquisition errors.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_dispatcher_lease_acquire_errors_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_dispatcher_lease_acquire_errors_total %d\n", outbox.LeaseAcquireErrors)
			fmt.Fprintf(w, "# HELP control_policy_outbox_dispatcher_lease_held Dispatcher lease ownership state (1=held).\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_dispatcher_lease_held gauge\n")
			if outbox.LeaseHeld {
				fmt.Fprintf(w, "control_policy_outbox_dispatcher_lease_held 1\n")
			} else {
				fmt.Fprintf(w, "control_policy_outbox_dispatcher_lease_held 0\n")
			}
			fmt.Fprintf(w, "# HELP control_policy_outbox_dispatcher_last_epoch Last observed lease epoch.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_dispatcher_last_epoch gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_dispatcher_last_epoch %d\n", outbox.LastLeaseEpoch)
			fmt.Fprintf(w, "# HELP control_policy_outbox_dispatcher_ticks_total Total dispatcher loop ticks.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_dispatcher_ticks_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_dispatcher_ticks_total %d\n", outbox.TicksTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_rows_claimed_total Total rows claimed by dispatcher.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_rows_claimed_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_rows_claimed_total %d\n", outbox.RowsClaimedTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_current_batch_size Current adaptive outbox claim batch size.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_current_batch_size gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_current_batch_size %d\n", outbox.CurrentBatchSize)
			fmt.Fprintf(w, "# HELP control_policy_outbox_batch_scale_up_total Total adaptive claim batch size scale-up actions.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_batch_scale_up_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_batch_scale_up_total %d\n", outbox.BatchScaleUpTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_batch_scale_down_total Total adaptive claim batch size scale-down actions.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_batch_scale_down_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_batch_scale_down_total %d\n", outbox.BatchScaleDownTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_publish_queue_waits_total Total times dispatcher publish queue hit capacity.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_publish_queue_waits_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_publish_queue_waits_total %d\n", outbox.PublishQueueWaits)
			fmt.Fprintf(w, "# HELP control_policy_outbox_mark_queue_waits_total Total times dispatcher mark queue hit capacity.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_mark_queue_waits_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_mark_queue_waits_total %d\n", outbox.MarkQueueWaits)
			fmt.Fprintf(w, "# HELP control_policy_outbox_published_total Total outbox rows published to Kafka.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_published_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_published_total %d\n", outbox.PublishedTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_retry_total Total outbox rows scheduled for retry.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_retry_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_retry_total %d\n", outbox.RetryTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_terminal_failed_total Total outbox rows marked terminal failed.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_terminal_failed_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_terminal_failed_total %d\n", outbox.TerminalFailedTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_fenced_update_failures_total Total fenced/no-op state transitions.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_fenced_update_failures_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_fenced_update_failures_total %d\n", outbox.FencedUpdateFailures)
			fmt.Fprintf(w, "# HELP control_policy_outbox_throttled_logs_total Total outbox logs suppressed by throttle.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_throttled_logs_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_throttled_logs_total %d\n", outbox.ThrottledLogsTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_skew_corrections_total Total outbox causal latency corrections due to clock skew.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_skew_corrections_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_skew_corrections_total %d\n", outbox.SkewCorrectionsTotal)
			if outbox.PublishLatencyCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_publish_latency_seconds Histogram of durable outbox publish latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_publish_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_outbox_publish_latency_seconds", outbox.PublishLatencyBuckets, outbox.PublishLatencyCount, outbox.PublishLatencySumMs)
				fmt.Fprintf(w, "# HELP control_policy_outbox_publish_latency_p95_seconds 95th percentile durable outbox publish latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_publish_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_outbox_publish_latency_p95_seconds %.6f\n", outbox.PublishLatencyP95Ms/1000.0)
			}
			if outbox.ClaimLatencyCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_claim_latency_seconds Histogram of outbox claim query latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_claim_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_outbox_claim_latency_seconds", outbox.ClaimLatencyBuckets, outbox.ClaimLatencyCount, outbox.ClaimLatencySumMs)
				fmt.Fprintf(w, "# HELP control_policy_outbox_claim_latency_p95_seconds 95th percentile outbox claim query latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_claim_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_outbox_claim_latency_p95_seconds %.6f\n", outbox.ClaimLatencyP95Ms/1000.0)
			}
			if outbox.MarkLatencyCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_mark_latency_seconds Histogram of outbox state-mark latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_mark_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_outbox_mark_latency_seconds", outbox.MarkLatencyBuckets, outbox.MarkLatencyCount, outbox.MarkLatencySumMs)
				fmt.Fprintf(w, "# HELP control_policy_outbox_mark_latency_p95_seconds 95th percentile outbox state-mark latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_mark_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_outbox_mark_latency_p95_seconds %.6f\n", outbox.MarkLatencyP95Ms/1000.0)
			}
			if outbox.TickLatencyCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_tick_latency_seconds Histogram of dispatcher tick duration in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_tick_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_outbox_tick_latency_seconds", outbox.TickLatencyBuckets, outbox.TickLatencyCount, outbox.TickLatencySumMs)
				fmt.Fprintf(w, "# HELP control_policy_outbox_tick_latency_p95_seconds 95th percentile dispatcher tick duration in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_tick_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_outbox_tick_latency_p95_seconds %.6f\n", outbox.TickLatencyP95Ms/1000.0)
			}
			if outbox.AIToPublishCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_ai_to_publish_latency_seconds Histogram of corrected AI-to-publish causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_ai_to_publish_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_ai_to_publish_latency_seconds", outbox.AIToPublishBuckets, outbox.AIToPublishCount, outbox.AIToPublishSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_ai_to_publish_latency_p95_seconds 95th percentile corrected AI-to-publish latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_ai_to_publish_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_ai_to_publish_latency_p95_seconds %.6f\n", outbox.AIToPublishP95Ms/1000.0)
			}
			if outbox.CommitToPublishCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_commit_to_publish_latency_seconds Histogram of corrected commit-to-publish causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_commit_to_publish_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_commit_to_publish_latency_seconds", outbox.CommitToPublishBuckets, outbox.CommitToPublishCount, outbox.CommitToPublishSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_commit_to_publish_latency_p95_seconds 95th percentile corrected commit-to-publish latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_commit_to_publish_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_commit_to_publish_latency_p95_seconds %.6f\n", outbox.CommitToPublishP95Ms/1000.0)
			}
		}

		if backlog, ok := s.outboxStats.GetPolicyOutboxBacklogStats(ctx); ok {
			fmt.Fprintf(w, "# HELP control_policy_outbox_backlog_pending Number of pending outbox rows.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_backlog_pending gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_backlog_pending %d\n", backlog.Pending)
			fmt.Fprintf(w, "# HELP control_policy_outbox_backlog_retry Number of retry outbox rows.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_backlog_retry gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_backlog_retry %d\n", backlog.Retry)
			fmt.Fprintf(w, "# HELP control_policy_outbox_backlog_publishing Number of publishing outbox rows.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_backlog_publishing gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_backlog_publishing %d\n", backlog.Publishing)
			fmt.Fprintf(w, "# HELP control_policy_outbox_oldest_pending_age_seconds Age of oldest publish-eligible outbox row in seconds.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_oldest_pending_age_seconds gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_oldest_pending_age_seconds %.3f\n", float64(backlog.OldestPendingAge)/1000.0)
			fmt.Fprintf(w, "# HELP control_policy_outbox_rows_total Total outbox rows (proxy for committed policy tx count).\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_rows_total gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_rows_total %d\n", backlog.TotalRows)
			fmt.Fprintf(w, "# HELP control_policy_outbox_published_rows Number of outbox rows currently in published status.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_published_rows gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_published_rows %d\n", backlog.PublishedRows)
			fmt.Fprintf(w, "# HELP control_policy_outbox_acked_rows Number of outbox rows currently in acked status.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_acked_rows gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_acked_rows %d\n", backlog.AckedRows)
			fmt.Fprintf(w, "# HELP control_policy_outbox_terminal_rows Number of outbox rows currently in terminal_failed status.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_terminal_rows gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_terminal_rows %d\n", backlog.TerminalRows)
			committed := float64(backlog.TotalRows)
			publishedOrAcked := float64(backlog.PublishedRows + backlog.AckedRows)
			ackRatio := 1.0
			coverage := 1.0
			if committed > 0 {
				coverage = publishedOrAcked / committed
			}
			if publishedOrAcked > 0 {
				ackRatio = float64(backlog.AckedRows) / publishedOrAcked
			}
			fmt.Fprintf(w, "# HELP control_policy_publish_coverage_ratio Ratio of publish-complete rows over committed policy rows.\n")
			fmt.Fprintf(w, "# TYPE control_policy_publish_coverage_ratio gauge\n")
			fmt.Fprintf(w, "control_policy_publish_coverage_ratio %.6f\n", coverage)
			fmt.Fprintf(w, "# HELP control_policy_ack_closure_ratio Ratio of acked rows over publish-complete rows.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_closure_ratio gauge\n")
			fmt.Fprintf(w, "control_policy_ack_closure_ratio %.6f\n", ackRatio)
		}
	}
	fmt.Fprintf(w, "# HELP control_mutation_blocked_safe_mode_total Total mutation requests blocked by safe mode.\n")
	fmt.Fprintf(w, "# TYPE control_mutation_blocked_safe_mode_total counter\n")
	fmt.Fprintf(w, "control_mutation_blocked_safe_mode_total %d\n", s.controlMutationBlockedSafeMode.Load())
	fmt.Fprintf(w, "# HELP control_mutation_blocked_consensus_gate_total Total mutation requests blocked by consensus health gate.\n")
	fmt.Fprintf(w, "# TYPE control_mutation_blocked_consensus_gate_total counter\n")
	fmt.Fprintf(w, "control_mutation_blocked_consensus_gate_total %d\n", s.controlMutationBlockedConsensus.Load())
	fmt.Fprintf(w, "# HELP control_mutation_blocked_tenant_scope_total Total mutation requests blocked by tenant-scope policy.\n")
	fmt.Fprintf(w, "# TYPE control_mutation_blocked_tenant_scope_total counter\n")
	fmt.Fprintf(w, "control_mutation_blocked_tenant_scope_total %d\n", s.controlMutationBlockedTenantScope.Load())
	fmt.Fprintf(w, "# HELP control_mutation_rate_limited_total Total mutation requests blocked by operator rate limits.\n")
	fmt.Fprintf(w, "# TYPE control_mutation_rate_limited_total counter\n")
	fmt.Fprintf(w, "control_mutation_rate_limited_total %d\n", s.controlMutationRateLimitedTotal.Load())
	fmt.Fprintf(w, "# HELP control_mutation_cooldown_blocked_total Total mutation requests blocked by per-target cooldown windows.\n")
	fmt.Fprintf(w, "# TYPE control_mutation_cooldown_blocked_total counter\n")
	fmt.Fprintf(w, "control_mutation_cooldown_blocked_total %d\n", s.controlMutationCooldownBlockedTotal.Load())
	fmt.Fprintf(w, "# HELP control_api_breaker_open_total Total requests rejected due to open control API circuit breaker.\n")
	fmt.Fprintf(w, "# TYPE control_api_breaker_open_total counter\n")
	fmt.Fprintf(w, "control_api_breaker_open_total %d\n", s.controlBreakerOpenTotal.Load())
	fmt.Fprintf(w, "# HELP control_api_timeout_total Total control API operations that hit deadline exceeded.\n")
	fmt.Fprintf(w, "# TYPE control_api_timeout_total counter\n")
	fmt.Fprintf(w, "control_api_timeout_total %d\n", s.controlAPITimeoutTotal.Load())
	fmt.Fprintf(w, "# HELP control_mutation_timeout_total Total control mutation operations that hit deadline exceeded.\n")
	fmt.Fprintf(w, "# TYPE control_mutation_timeout_total counter\n")
	fmt.Fprintf(w, "control_mutation_timeout_total %d\n", s.controlMutationTimeoutTotal.Load())
	fmt.Fprintf(w, "# HELP control_mutation_safe_mode_enabled Global control mutation safe mode state (1=enabled).\n")
	fmt.Fprintf(w, "# TYPE control_mutation_safe_mode_enabled gauge\n")
	if s.controlMutationsSafeMode.Load() {
		fmt.Fprintf(w, "control_mutation_safe_mode_enabled 1\n")
	} else {
		fmt.Fprintf(w, "control_mutation_safe_mode_enabled 0\n")
	}

	// Consensus wiring/validation metrics
	if s.engine != nil {
		snap := s.engine.GetMetrics()
		fmt.Fprintf(w, "# HELP consensus_parent_hash_mismatches_total Total parent-hash validation mismatches observed by the node.\n")
		fmt.Fprintf(w, "# TYPE consensus_parent_hash_mismatches_total counter\n")
		fmt.Fprintf(w, "consensus_parent_hash_mismatches_total %d\n", snap.ParentHashMismatches)
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

	routeSnapshots := s.snapshotRouteMetrics()
	if len(routeSnapshots) > 0 {
		fmt.Fprintf(w, "# HELP api_route_requests_total Total HTTP requests per method/path.\n")
		fmt.Fprintf(w, "# TYPE api_route_requests_total counter\n")
		fmt.Fprintf(w, "# HELP api_route_request_errors_total Total error responses per method/path.\n")
		fmt.Fprintf(w, "# TYPE api_route_request_errors_total counter\n")
		fmt.Fprintf(w, "# HELP api_route_request_duration_seconds_sum Total request duration in seconds per method/path.\n")
		fmt.Fprintf(w, "# TYPE api_route_request_duration_seconds_sum counter\n")
		fmt.Fprintf(w, "# HELP api_route_response_bytes_total Total response bytes per method/path.\n")
		fmt.Fprintf(w, "# TYPE api_route_response_bytes_total counter\n")
		fmt.Fprintf(w, "# HELP api_route_cache_hits_total Cache hits observed per method/path.\n")
		fmt.Fprintf(w, "# TYPE api_route_cache_hits_total counter\n")
		fmt.Fprintf(w, "# HELP api_route_cache_misses_total Cache misses observed per method/path.\n")
		fmt.Fprintf(w, "# TYPE api_route_cache_misses_total counter\n")
		for _, snap := range routeSnapshots {
			labels := fmt.Sprintf("method=\"%s\",path=\"%s\"", sanitizeLabelValue(snap.method), sanitizeLabelValue(snap.path))
			fmt.Fprintf(w, "api_route_requests_total{%s} %d\n", labels, snap.requests)
			fmt.Fprintf(w, "api_route_request_errors_total{%s} %d\n", labels, snap.errors)
			fmt.Fprintf(w, "api_route_request_duration_seconds_sum{%s} %.6f\n", labels, float64(snap.latencyMicros)/1_000_000.0)
			fmt.Fprintf(w, "api_route_response_bytes_total{%s} %d\n", labels, snap.bytes)
			fmt.Fprintf(w, "api_route_cache_hits_total{%s} %d\n", labels, snap.cacheHits)
			fmt.Fprintf(w, "api_route_cache_misses_total{%s} %d\n", labels, snap.cacheMisses)
		}
	}

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

func sanitizeLabelValue(value string) string {
	if value == "" {
		return value
	}
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "", "\r", "")
	return replacer.Replace(value)
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

func writePrometheusHistogram(w io.Writer, name string, buckets []utils.HistogramBucket, count uint64, sumMs float64) {
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
