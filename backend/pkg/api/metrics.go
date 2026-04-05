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

	"backend/pkg/control/policyoutbox"
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
	dbIntegrityTotal := uint64(0)
	dbTxLocationMismatchTotal := uint64(0)
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
			dbTxLocationMismatchTotal = snap.TxLocationMismatchTotal
			for _, count := range snap.PersistIntegrityKindTotals {
				dbIntegrityTotal += count
			}
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
			for _, stage := range []string{
				"tx_begin_wait",
				"tx_body_exec",
				"persist_attempt_total",
				"upsert_block",
				"upsert_transactions",
				"upsert_transactions_prepare",
				"upsert_transactions_insert_query",
				"upsert_transactions_insert_scan",
				"upsert_transactions_insert_chunks",
				"upsert_transactions_conflict_list_build",
				"upsert_transactions_total_inner",
				"upsert_transactions_before_verify",
				"upsert_transactions_after_verify",
				"upsert_transactions_call_wall",
				"upsert_transactions_return_success",
				"upsert_transactions_return_integrity_error",
				"upsert_transactions_return_other_error",
				"upsert_transactions_verify_total",
				"upsert_transactions_verify_query",
				"upsert_transactions_verify_scan",
				"upsert_transactions_verify_compare",
				"upsert_transactions_verify_compare_total",
				"upsert_transactions_verify_compare_error",
				"upsert_transactions_verify_recheck_in_tx",
				"upsert_transactions_verify_conflicts",
				"upsert_transactions_verify_location_mismatch",
				"upsert_transactions_verify_location_mismatch_audit",
				"upsert_transactions_verify_forensics",
				"upsert_transactions_verify_forensics_block_lookup",
				"upsert_transactions_outbox_batch",
				"upsert_snapshot",
				"commit",
			} {
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
				var retrySerializationTotal uint64
				var retryDeadlockTotal uint64
				var retryLockUnavailableTotal uint64
				for key, count := range snap.PersistFailureClassTotals {
					stage := key
					class := "other"
					if idx := strings.Index(key, ":"); idx > 0 && idx < len(key)-1 {
						stage = key[:idx]
						class = key[idx+1:]
					}
					switch class {
					case "retry_serialization":
						retrySerializationTotal += count
					case "retry_deadlock":
						retryDeadlockTotal += count
					case "retry_lock_not_available":
						retryLockUnavailableTotal += count
					}
					fmt.Fprintf(w, "cockroach_persist_failures_total{stage=\"%s\",class=\"%s\"} %d\n", stage, class, count)
				}
				fmt.Fprintf(w, "# HELP cockroach_tx_serialization_restarts_total Total serialization restart errors observed during persistence.\n")
				fmt.Fprintf(w, "# TYPE cockroach_tx_serialization_restarts_total counter\n")
				fmt.Fprintf(w, "cockroach_tx_serialization_restarts_total %d\n", retrySerializationTotal)
				fmt.Fprintf(w, "# HELP cockroach_tx_deadlock_retries_total Total deadlock retry errors observed during persistence.\n")
				fmt.Fprintf(w, "# TYPE cockroach_tx_deadlock_retries_total counter\n")
				fmt.Fprintf(w, "cockroach_tx_deadlock_retries_total %d\n", retryDeadlockTotal)
				fmt.Fprintf(w, "# HELP cockroach_tx_lock_unavailable_retries_total Total lock-not-available retry errors observed during persistence.\n")
				fmt.Fprintf(w, "# TYPE cockroach_tx_lock_unavailable_retries_total counter\n")
				fmt.Fprintf(w, "cockroach_tx_lock_unavailable_retries_total %d\n", retryLockUnavailableTotal)
			}
			if len(snap.PersistContentionSignalTotals) > 0 {
				fmt.Fprintf(w, "# HELP cockroach_persist_contention_signal_total Heuristic contention signals observed during persistence stages.\n")
				fmt.Fprintf(w, "# TYPE cockroach_persist_contention_signal_total counter\n")
				for signal, count := range snap.PersistContentionSignalTotals {
					fmt.Fprintf(w, "cockroach_persist_contention_signal_total{signal=\"%s\"} %d\n", signal, count)
				}
			}
			if len(snap.PersistDiagnosticSignalTotals) > 0 {
				fmt.Fprintf(w, "# HELP cockroach_persist_diagnostic_signal_total Diagnostic branch-entry and failure-path signals observed during persistence verification.\n")
				fmt.Fprintf(w, "# TYPE cockroach_persist_diagnostic_signal_total counter\n")
				for signal, count := range snap.PersistDiagnosticSignalTotals {
					fmt.Fprintf(w, "cockroach_persist_diagnostic_signal_total{signal=\"%s\"} %d\n", signal, count)
				}
				lifecycleAuditDeferred := snap.PersistDiagnosticSignalTotals["lifecycle_audit_deferred"]
				fmt.Fprintf(w, "# HELP control_policy_lifecycle_audit_deferred_total Total lifecycle audit writes deferred to protect durable commit budget.\n")
				fmt.Fprintf(w, "# TYPE control_policy_lifecycle_audit_deferred_total counter\n")
				fmt.Fprintf(w, "control_policy_lifecycle_audit_deferred_total %d\n", lifecycleAuditDeferred)
				fmt.Fprintf(w, "# HELP control_policy_outbox_refresh_rewrite_total Total semantic-refresh rewrite branch outcomes by reason.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_refresh_rewrite_total counter\n")
				reasons := []string{
					"skipped_nonmutable_status",
					"skipped_same_hash",
					"no_rows",
					"applied",
				}
				for _, reason := range reasons {
					key := "outbox_semantic_refresh_" + reason
					if reason == "skipped_nonmutable_status" {
						key = "outbox_semantic_refresh_skipped_nonmutable_status"
					}
					if reason == "skipped_same_hash" {
						key = "outbox_semantic_refresh_skipped_same_hash"
					}
					if reason == "no_rows" {
						key = "outbox_semantic_refresh_no_rows"
					}
					if reason == "applied" {
						key = "outbox_semantic_refresh_applied"
					}
					fmt.Fprintf(w, "control_policy_outbox_refresh_rewrite_total{reason=\"%s\"} %d\n", reason, snap.PersistDiagnosticSignalTotals[key])
				}
			}
			if len(snap.PersistIntegrityKindTotals) > 0 {
				fmt.Fprintf(w, "# HELP cockroach_integrity_failure_total Deterministic integrity failure signals by kind observed in persistence verification.\n")
				fmt.Fprintf(w, "# TYPE cockroach_integrity_failure_total counter\n")
				for kind, count := range snap.PersistIntegrityKindTotals {
					fmt.Fprintf(w, "cockroach_integrity_failure_total{kind=\"%s\"} %d\n", kind, count)
				}
			}
			fmt.Fprintf(w, "# HELP cockroach_tx_upsert_blocks_total Blocks handled by transaction upsert.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_upsert_blocks_total counter\n")
			fmt.Fprintf(w, "cockroach_tx_upsert_blocks_total %d\n", snap.TxUpsertBlocks)
			fmt.Fprintf(w, "# HELP cockroach_tx_upsert_rows_total Transaction rows handled by upsert.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_upsert_rows_total counter\n")
			fmt.Fprintf(w, "cockroach_tx_upsert_rows_total %d\n", snap.TxUpsertRows)
			fmt.Fprintf(w, "# HELP cockroach_tx_upsert_conflicts_total Upsert conflicts that required verification readback.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_upsert_conflicts_total counter\n")
			fmt.Fprintf(w, "cockroach_tx_upsert_conflicts_total %d\n", snap.TxUpsertConflicts)
			fmt.Fprintf(w, "# HELP cockroach_tx_upsert_payload_bytes_total Serialized payload bytes processed in transaction upsert.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_upsert_payload_bytes_total counter\n")
			fmt.Fprintf(w, "cockroach_tx_upsert_payload_bytes_total %d\n", snap.TxUpsertPayloadBytes)
			fmt.Fprintf(w, "# HELP cockroach_tx_upsert_conflict_ratio Conflict ratio for transaction upsert rows.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_upsert_conflict_ratio gauge\n")
			fmt.Fprintf(w, "cockroach_tx_upsert_conflict_ratio %.6f\n", snap.TxUpsertConflictRatio)
			fmt.Fprintf(w, "# HELP cockroach_tx_location_mismatch_total Total transaction location mismatch detections during conflict verification.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_location_mismatch_total counter\n")
			if len(snap.TxLocationMismatchByLabel) > 0 {
				for label, count := range snap.TxLocationMismatchByLabel {
					source := label.Source
					if source == "" {
						source = "unknown"
					}
					kind := label.Kind
					if kind == "" {
						kind = "unknown"
					}
					fmt.Fprintf(w, "cockroach_tx_location_mismatch_total{source=\"%s\",kind=\"%s\"} %d\n", source, kind, count)
				}
			} else if len(snap.TxLocationMismatchBySource) == 0 {
				fmt.Fprintf(w, "cockroach_tx_location_mismatch_total %d\n", snap.TxLocationMismatchTotal)
			} else {
				for source, count := range snap.TxLocationMismatchBySource {
					fmt.Fprintf(w, "cockroach_tx_location_mismatch_total{source=\"%s\"} %d\n", source, count)
				}
			}
			if len(snap.TxBatchModeTotals) > 0 {
				fmt.Fprintf(w, "# HELP cockroach_tx_batch_mode_total Transaction upsert execution mode counts.\n")
				fmt.Fprintf(w, "# TYPE cockroach_tx_batch_mode_total counter\n")
				for mode, count := range snap.TxBatchModeTotals {
					fmt.Fprintf(w, "cockroach_tx_batch_mode_total{mode=\"%s\"} %d\n", mode, count)
				}
			}
			if len(snap.OutboxBatchModeTotals) > 0 {
				fmt.Fprintf(w, "# HELP cockroach_outbox_batch_mode_total Outbox upsert execution mode counts.\n")
				fmt.Fprintf(w, "# TYPE cockroach_outbox_batch_mode_total counter\n")
				for mode, count := range snap.OutboxBatchModeTotals {
					fmt.Fprintf(w, "cockroach_outbox_batch_mode_total{mode=\"%s\"} %d\n", mode, count)
				}
			}
			fmt.Fprintf(w, "# HELP cockroach_tx_batch_configured_size Configured transaction upsert batch size.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_batch_configured_size gauge\n")
			fmt.Fprintf(w, "cockroach_tx_batch_configured_size %d\n", snap.BatchTxConfiguredSize)
			fmt.Fprintf(w, "# HELP cockroach_outbox_batch_configured_size Configured outbox upsert batch size.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_batch_configured_size gauge\n")
			fmt.Fprintf(w, "cockroach_outbox_batch_configured_size %d\n", snap.BatchOutboxConfiguredSize)
			fmt.Fprintf(w, "# HELP cockroach_tx_verify_chunk_size Configured chunk size for transaction conflict verification.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_verify_chunk_size gauge\n")
			fmt.Fprintf(w, "cockroach_tx_verify_chunk_size %d\n", snap.TxVerifyChunkSize)
			fmt.Fprintf(w, "# HELP cockroach_tx_verify_mode Conflict verification query execution mode (in_tx=1/out_of_tx=1).\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_verify_mode gauge\n")
			if snap.TxVerifyUseTx {
				fmt.Fprintf(w, "cockroach_tx_verify_mode{mode=\"in_tx\"} 1\n")
				fmt.Fprintf(w, "cockroach_tx_verify_mode{mode=\"out_of_tx\"} 0\n")
			} else {
				fmt.Fprintf(w, "cockroach_tx_verify_mode{mode=\"in_tx\"} 0\n")
				fmt.Fprintf(w, "cockroach_tx_verify_mode{mode=\"out_of_tx\"} 1\n")
			}
			fmt.Fprintf(w, "# HELP cockroach_persist_tx_isolation Persistence transaction isolation level (serializable=1/read_committed=1).\n")
			fmt.Fprintf(w, "# TYPE cockroach_persist_tx_isolation gauge\n")
			if snap.PersistTxIsolation == "read_committed" {
				fmt.Fprintf(w, "cockroach_persist_tx_isolation{level=\"serializable\"} 0\n")
				fmt.Fprintf(w, "cockroach_persist_tx_isolation{level=\"read_committed\"} 1\n")
			} else {
				fmt.Fprintf(w, "cockroach_persist_tx_isolation{level=\"serializable\"} 1\n")
				fmt.Fprintf(w, "cockroach_persist_tx_isolation{level=\"read_committed\"} 0\n")
			}
			fmt.Fprintf(w, "# HELP cockroach_tx_batch_canary_enabled Whether transaction upsert canary fallback is enabled (1/0).\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_batch_canary_enabled gauge\n")
			if snap.BatchCanaryEnabled {
				fmt.Fprintf(w, "cockroach_tx_batch_canary_enabled 1\n")
			} else {
				fmt.Fprintf(w, "cockroach_tx_batch_canary_enabled 0\n")
			}
			fmt.Fprintf(w, "# HELP cockroach_tx_batch_fallback_active Whether transaction upsert fallback mode is active (1/0).\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_batch_fallback_active gauge\n")
			if snap.TxBatchFallbackActive {
				fmt.Fprintf(w, "cockroach_tx_batch_fallback_active 1\n")
			} else {
				fmt.Fprintf(w, "cockroach_tx_batch_fallback_active 0\n")
			}
			fmt.Fprintf(w, "# HELP cockroach_tx_batch_fallback_until_unix_ms Unix milliseconds when fallback mode expires.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_batch_fallback_until_unix_ms gauge\n")
			fmt.Fprintf(w, "cockroach_tx_batch_fallback_until_unix_ms %d\n", snap.TxBatchFallbackUntilUnixMs)
			fmt.Fprintf(w, "# HELP cockroach_tx_batch_fallback_activations_total Canary fallback activations.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_batch_fallback_activations_total counter\n")
			fmt.Fprintf(w, "cockroach_tx_batch_fallback_activations_total %d\n", snap.TxBatchFallbackActivations)
			fmt.Fprintf(w, "# HELP cockroach_outbox_batch_fallback_active Whether outbox upsert fallback mode is active (1/0).\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_batch_fallback_active gauge\n")
			if snap.OutboxBatchFallbackActive {
				fmt.Fprintf(w, "cockroach_outbox_batch_fallback_active 1\n")
			} else {
				fmt.Fprintf(w, "cockroach_outbox_batch_fallback_active 0\n")
			}
			fmt.Fprintf(w, "# HELP cockroach_outbox_batch_fallback_until_unix_ms Unix milliseconds when outbox fallback mode expires.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_batch_fallback_until_unix_ms gauge\n")
			fmt.Fprintf(w, "cockroach_outbox_batch_fallback_until_unix_ms %d\n", snap.OutboxBatchFallbackUntilUnixMs)
			fmt.Fprintf(w, "# HELP cockroach_outbox_batch_fallback_activations_total Outbox canary fallback activations.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_batch_fallback_activations_total counter\n")
			fmt.Fprintf(w, "cockroach_outbox_batch_fallback_activations_total %d\n", snap.OutboxBatchFallbackActivations)
			if len(snap.TxBatchCanaryBadByReason) > 0 {
				fmt.Fprintf(w, "# HELP cockroach_tx_batch_canary_bad_total Tx canary samples by reason.\n")
				fmt.Fprintf(w, "# TYPE cockroach_tx_batch_canary_bad_total counter\n")
				for reason, count := range snap.TxBatchCanaryBadByReason {
					fmt.Fprintf(w, "cockroach_tx_batch_canary_bad_total{reason=\"%s\"} %d\n", reason, count)
				}
			}
			if len(snap.OutboxBatchCanaryBadByReason) > 0 {
				fmt.Fprintf(w, "# HELP cockroach_outbox_batch_canary_bad_total Outbox canary samples by reason.\n")
				fmt.Fprintf(w, "# TYPE cockroach_outbox_batch_canary_bad_total counter\n")
				for reason, count := range snap.OutboxBatchCanaryBadByReason {
					fmt.Fprintf(w, "cockroach_outbox_batch_canary_bad_total{reason=\"%s\"} %d\n", reason, count)
				}
			}
			fmt.Fprintf(w, "# HELP cockroach_tx_batch_current_size Current transaction upsert batch size after adaptive tuning.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_batch_current_size gauge\n")
			fmt.Fprintf(w, "cockroach_tx_batch_current_size %d\n", snap.BatchTxCurrentSize)
			fmt.Fprintf(w, "# HELP cockroach_outbox_batch_current_size Current outbox upsert batch size after adaptive tuning.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_batch_current_size gauge\n")
			fmt.Fprintf(w, "cockroach_outbox_batch_current_size %d\n", snap.BatchOutboxCurrentSize)
			fmt.Fprintf(w, "# HELP cockroach_batch_adaptive_scale_up_total Adaptive batch scale-up operations.\n")
			fmt.Fprintf(w, "# TYPE cockroach_batch_adaptive_scale_up_total counter\n")
			fmt.Fprintf(w, "cockroach_batch_adaptive_scale_up_total %d\n", snap.BatchAdaptiveScaleUpTotal)
			fmt.Fprintf(w, "# HELP cockroach_batch_adaptive_scale_down_total Adaptive batch scale-down operations.\n")
			fmt.Fprintf(w, "# TYPE cockroach_batch_adaptive_scale_down_total counter\n")
			fmt.Fprintf(w, "cockroach_batch_adaptive_scale_down_total %d\n", snap.BatchAdaptiveScaleDownTotal)
			fmt.Fprintf(w, "# HELP cockroach_tx_batch_adaptive_scale_up_total Transaction batch adaptive scale-up operations.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_batch_adaptive_scale_up_total counter\n")
			fmt.Fprintf(w, "cockroach_tx_batch_adaptive_scale_up_total %d\n", snap.TxBatchAdaptiveScaleUpTotal)
			fmt.Fprintf(w, "# HELP cockroach_tx_batch_adaptive_scale_down_total Transaction batch adaptive scale-down operations.\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_batch_adaptive_scale_down_total counter\n")
			fmt.Fprintf(w, "cockroach_tx_batch_adaptive_scale_down_total %d\n", snap.TxBatchAdaptiveScaleDownTotal)
			fmt.Fprintf(w, "# HELP cockroach_outbox_batch_adaptive_scale_up_total Outbox batch adaptive scale-up operations.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_batch_adaptive_scale_up_total counter\n")
			fmt.Fprintf(w, "cockroach_outbox_batch_adaptive_scale_up_total %d\n", snap.OutboxBatchAdaptiveScaleUpTotal)
			fmt.Fprintf(w, "# HELP cockroach_outbox_batch_adaptive_scale_down_total Outbox batch adaptive scale-down operations.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_batch_adaptive_scale_down_total counter\n")
			fmt.Fprintf(w, "cockroach_outbox_batch_adaptive_scale_down_total %d\n", snap.OutboxBatchAdaptiveScaleDownTotal)
			fmt.Fprintf(w, "# HELP cockroach_outbox_insert_retries_total Retryable outbox insert attempts observed.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_insert_retries_total counter\n")
			fmt.Fprintf(w, "cockroach_outbox_insert_retries_total %d\n", snap.OutboxInsertRetriesTotal)
			fmt.Fprintf(w, "# HELP cockroach_outbox_insert_retry_serialization_total Outbox insert retries classified as SQLSTATE 40001.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_insert_retry_serialization_total counter\n")
			fmt.Fprintf(w, "cockroach_outbox_insert_retry_serialization_total %d\n", snap.OutboxInsertRetrySerialization)
			fmt.Fprintf(w, "# HELP cockroach_outbox_returning_rows_total Total outbox rows returned by INSERT .. RETURNING.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_returning_rows_total counter\n")
			fmt.Fprintf(w, "cockroach_outbox_returning_rows_total %d\n", snap.OutboxReturningRowsTotal)
			fmt.Fprintf(w, "# HELP cockroach_outbox_returning_estimated_bytes_total Estimated bytes returned by outbox INSERT .. RETURNING.\n")
			fmt.Fprintf(w, "# TYPE cockroach_outbox_returning_estimated_bytes_total counter\n")
			fmt.Fprintf(w, "cockroach_outbox_returning_estimated_bytes_total %d\n", snap.OutboxReturningEstimatedBytes)
			fmt.Fprintf(w, "# HELP cockroach_tx_payload_mode Payload persistence mode for transactions (full=1, minimal=0).\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_payload_mode gauge\n")
			if snap.TxStoreFullPayload {
				fmt.Fprintf(w, "cockroach_tx_payload_mode{mode=\"full\"} 1\n")
				fmt.Fprintf(w, "cockroach_tx_payload_mode{mode=\"minimal\"} 0\n")
			} else {
				fmt.Fprintf(w, "cockroach_tx_payload_mode{mode=\"full\"} 0\n")
				fmt.Fprintf(w, "cockroach_tx_payload_mode{mode=\"minimal\"} 1\n")
			}
			fmt.Fprintf(w, "# HELP cockroach_tx_store_full_payload Whether full transaction payload JSON is persisted (1/0).\n")
			fmt.Fprintf(w, "# TYPE cockroach_tx_store_full_payload gauge\n")
			if snap.TxStoreFullPayload {
				fmt.Fprintf(w, "cockroach_tx_store_full_payload 1\n")
			} else {
				fmt.Fprintf(w, "cockroach_tx_store_full_payload 0\n")
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
		fmt.Fprintf(w, "# HELP kafka_consumer_replay_rejected_admit_total Total messages rejected at pre-admit replay guard.\n")
		fmt.Fprintf(w, "# TYPE kafka_consumer_replay_rejected_admit_total counter\n")
		fmt.Fprintf(w, "kafka_consumer_replay_rejected_admit_total %d\n", consStats.ReplayRejectedAdmit)
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
			writeCommitLatency := func(base, help string, buckets []utils.HistogramBucket, count uint64, sumMs, p95Ms float64) {
				if count == 0 {
					return
				}
				fmt.Fprintf(w, "# HELP %s %s\n", base, help)
				fmt.Fprintf(w, "# TYPE %s histogram\n", base)
				writePrometheusHistogram(w, base, buckets, count, sumMs)
				fmt.Fprintf(w, "# HELP %s_p95_seconds 95th percentile %s\n", base, strings.TrimSuffix(help, "."))
				fmt.Fprintf(w, "# TYPE %s_p95_seconds gauge\n", base)
				fmt.Fprintf(w, "%s_p95_seconds %.6f\n", base, p95Ms/1000.0)
			}
			fmt.Fprintf(w, "# HELP control_commit_log_sample_every_n Commit info log sampling interval.\n")
			fmt.Fprintf(w, "# TYPE control_commit_log_sample_every_n gauge\n")
			fmt.Fprintf(w, "control_commit_log_sample_every_n %d\n", commitStats.LogSampleEvery)
			fmt.Fprintf(w, "# HELP control_commit_logs_suppressed_total Total commit-path logs suppressed by sampling/throttling.\n")
			fmt.Fprintf(w, "# TYPE control_commit_logs_suppressed_total counter\n")
			fmt.Fprintf(w, "control_commit_logs_suppressed_total %d\n", commitStats.LogsSuppressed)
			fmt.Fprintf(w, "# HELP control_persist_backfill_pending Current number of pending backfill entries.\n")
			fmt.Fprintf(w, "# TYPE control_persist_backfill_pending gauge\n")
			fmt.Fprintf(w, "control_persist_backfill_pending %d\n", commitStats.BackfillPending)
			fmt.Fprintf(w, "# HELP control_persist_backfill_oldest_pending_age_ms Oldest pending backfill entry age in milliseconds.\n")
			fmt.Fprintf(w, "# TYPE control_persist_backfill_oldest_pending_age_ms gauge\n")
			fmt.Fprintf(w, "control_persist_backfill_oldest_pending_age_ms %d\n", commitStats.BackfillOldestPendingAgeMs)
			fmt.Fprintf(w, "# HELP control_persist_pending_age_ms Oldest pending backfill entry age in milliseconds.\n")
			fmt.Fprintf(w, "# TYPE control_persist_pending_age_ms gauge\n")
			fmt.Fprintf(w, "control_persist_pending_age_ms %d\n", commitStats.BackfillOldestPendingAgeMs)
			fmt.Fprintf(w, "# HELP control_persist_backfill_dropped_total Total pending backfill entries dropped due to cap/eviction.\n")
			fmt.Fprintf(w, "# TYPE control_persist_backfill_dropped_total counter\n")
			fmt.Fprintf(w, "control_persist_backfill_dropped_total %d\n", commitStats.BackfillDropped)
			fmt.Fprintf(w, "# HELP control_persist_backfill_non_retryable_quarantined_total Total non-retryable backfill entries quarantined after repeated failures.\n")
			fmt.Fprintf(w, "# TYPE control_persist_backfill_non_retryable_quarantined_total counter\n")
			fmt.Fprintf(w, "control_persist_backfill_non_retryable_quarantined_total %d\n", commitStats.BackfillNonRetryQuarantined)
			fmt.Fprintf(w, "# HELP control_tx_replay_rejected_total Total transactions rejected by replay filter.\n")
			fmt.Fprintf(w, "# TYPE control_tx_replay_rejected_total counter\n")
			fmt.Fprintf(w, "control_tx_replay_rejected_total{stage=\"admit\"} %d\n", commitStats.ReplayRejectedAdmit)
			fmt.Fprintf(w, "control_tx_replay_rejected_total{stage=\"build\"} %d\n", commitStats.ReplayRejectedBuild)
			fmt.Fprintf(w, "# HELP control_tx_replay_filter_size Current number of committed tx identities tracked by replay filter.\n")
			fmt.Fprintf(w, "# TYPE control_tx_replay_filter_size gauge\n")
			fmt.Fprintf(w, "control_tx_replay_filter_size %d\n", commitStats.ReplayFilterSize)
			fmt.Fprintf(w, "# HELP control_tx_replay_filter_evictions_total Total replay filter evictions due to cap.\n")
			fmt.Fprintf(w, "# TYPE control_tx_replay_filter_evictions_total counter\n")
			fmt.Fprintf(w, "control_tx_replay_filter_evictions_total %d\n", commitStats.ReplayFilterEvictions)
			fmt.Fprintf(w, "# HELP control_persist_enqueue_total Total persistence enqueue attempts by source and role.\n")
			fmt.Fprintf(w, "# TYPE control_persist_enqueue_total counter\n")
			fmt.Fprintf(w, "control_persist_enqueue_total{source=\"commit\",role=\"proposer\"} %d\n", commitStats.PersistEnqueueCommitProposer)
			fmt.Fprintf(w, "control_persist_enqueue_total{source=\"commit\",role=\"non_proposer\"} %d\n", commitStats.PersistEnqueueCommitNonProposer)
			fmt.Fprintf(w, "control_persist_enqueue_total{source=\"backfill\",role=\"proposer\"} %d\n", commitStats.PersistEnqueueBackfillProposer)
			fmt.Fprintf(w, "control_persist_enqueue_total{source=\"backfill\",role=\"non_proposer\"} %d\n", commitStats.PersistEnqueueBackfillNonProposer)
			fmt.Fprintf(w, "# HELP control_persist_enqueue_dropped_non_proposer_total Total persistence enqueue requests dropped by proposer-only safety guard.\n")
			fmt.Fprintf(w, "# TYPE control_persist_enqueue_dropped_non_proposer_total counter\n")
			fmt.Fprintf(w, "control_persist_enqueue_dropped_non_proposer_total %d\n", commitStats.PersistEnqueueDroppedNonProposer)
			fmt.Fprintf(w, "# HELP control_persist_enqueue_errors_total Total persistence enqueue errors.\n")
			fmt.Fprintf(w, "# TYPE control_persist_enqueue_errors_total counter\n")
			fmt.Fprintf(w, "control_persist_enqueue_errors_total %d\n", commitStats.PersistEnqueueErrors)
			fmt.Fprintf(w, "# HELP control_persist_enqueue_timeouts_total Total persistence enqueue timeout errors.\n")
			fmt.Fprintf(w, "# TYPE control_persist_enqueue_timeouts_total counter\n")
			fmt.Fprintf(w, "control_persist_enqueue_timeouts_total %d\n", commitStats.PersistEnqueueTimeouts)
			fmt.Fprintf(w, "# HELP control_persist_enqueue_timeout_total Total persistence enqueue timeout errors partitioned by source.\n")
			fmt.Fprintf(w, "# TYPE control_persist_enqueue_timeout_total counter\n")
			fmt.Fprintf(w, "control_persist_enqueue_timeout_total{source=\"commit\"} %d\n", commitStats.PersistEnqueueTimeoutsCommit)
			fmt.Fprintf(w, "control_persist_enqueue_timeout_total{source=\"backfill\"} %d\n", commitStats.PersistEnqueueTimeoutsBackfill)
			fmt.Fprintf(w, "# HELP control_persist_enqueue_wait_ms_sum Sum of persistence enqueue wait time in milliseconds by source.\n")
			fmt.Fprintf(w, "# TYPE control_persist_enqueue_wait_ms_sum counter\n")
			fmt.Fprintf(w, "control_persist_enqueue_wait_ms_sum{source=\"commit\"} %d\n", commitStats.PersistEnqueueWaitCommitSumMs)
			fmt.Fprintf(w, "control_persist_enqueue_wait_ms_sum{source=\"backfill\"} %d\n", commitStats.PersistEnqueueWaitBackfillSumMs)
			fmt.Fprintf(w, "# HELP control_persist_enqueue_wait_ms_count Count of persistence enqueue attempts by source.\n")
			fmt.Fprintf(w, "# TYPE control_persist_enqueue_wait_ms_count counter\n")
			fmt.Fprintf(w, "control_persist_enqueue_wait_ms_count{source=\"commit\"} %d\n", commitStats.PersistEnqueueWaitCommitCount)
			fmt.Fprintf(w, "control_persist_enqueue_wait_ms_count{source=\"backfill\"} %d\n", commitStats.PersistEnqueueWaitBackfillCount)
			fmt.Fprintf(w, "# HELP control_persist_direct_fallback_total Total commit-path direct persistence fallback attempts by result.\n")
			fmt.Fprintf(w, "# TYPE control_persist_direct_fallback_total counter\n")
			fmt.Fprintf(w, "control_persist_direct_fallback_total{result=\"attempt\"} %d\n", commitStats.PersistDirectFallbackAttempts)
			fmt.Fprintf(w, "control_persist_direct_fallback_total{result=\"success\"} %d\n", commitStats.PersistDirectFallbackSuccess)
			fmt.Fprintf(w, "control_persist_direct_fallback_total{result=\"failure\"} %d\n", commitStats.PersistDirectFallbackFailures)
			fmt.Fprintf(w, "control_persist_direct_fallback_total{result=\"throttled\"} %d\n", commitStats.PersistDirectFallbackThrottled)
			fmt.Fprintf(w, "control_persist_direct_fallback_total{result=\"skipped_pressure\"} %d\n", commitStats.PersistDirectFallbackSkippedPressure)
			fmt.Fprintf(w, "control_persist_direct_fallback_total{result=\"skipped_distress\"} %d\n", commitStats.PersistDirectFallbackSkippedDistress)
			fmt.Fprintf(w, "# HELP control_persist_direct_fallback_in_flight Current number of direct fallback persist operations running.\n")
			fmt.Fprintf(w, "# TYPE control_persist_direct_fallback_in_flight gauge\n")
			fmt.Fprintf(w, "control_persist_direct_fallback_in_flight %d\n", commitStats.PersistDirectFallbackInFlight)
			fmt.Fprintf(w, "# HELP control_persist_writer_takeover_activations_total Total non-proposer takeover activations in proposer_primary mode.\n")
			fmt.Fprintf(w, "# TYPE control_persist_writer_takeover_activations_total counter\n")
			fmt.Fprintf(w, "control_persist_writer_takeover_activations_total %d\n", commitStats.PersistWriterTakeoverActivations)
			fmt.Fprintf(w, "# HELP control_persist_total_budget_exhausted_total Total persistence tasks stopped by total wall-clock budget cap.\n")
			fmt.Fprintf(w, "# TYPE control_persist_total_budget_exhausted_total counter\n")
			fmt.Fprintf(w, "control_persist_total_budget_exhausted_total %d\n", commitStats.PersistTotalBudgetExhausted)
			fmt.Fprintf(w, "# HELP control_persist_attempt_timeout_capped_total Total attempt timeouts capped by remaining total budget.\n")
			fmt.Fprintf(w, "# TYPE control_persist_attempt_timeout_capped_total counter\n")
			fmt.Fprintf(w, "control_persist_attempt_timeout_capped_total %d\n", commitStats.PersistAttemptTimeoutCapped)
			fmt.Fprintf(w, "# HELP control_persist_onpersisted_delay_ms_sum Sum of delay from PersistBlock success to onPersisted callback in milliseconds.\n")
			fmt.Fprintf(w, "# TYPE control_persist_onpersisted_delay_ms_sum counter\n")
			fmt.Fprintf(w, "control_persist_onpersisted_delay_ms_sum %d\n", commitStats.PersistOnPersistedDelaySumMs)
			fmt.Fprintf(w, "# HELP control_persist_onpersisted_delay_ms_count Count of PersistBlock success events observed for onPersisted delay.\n")
			fmt.Fprintf(w, "# TYPE control_persist_onpersisted_delay_ms_count counter\n")
			fmt.Fprintf(w, "control_persist_onpersisted_delay_ms_count %d\n", commitStats.PersistOnPersistedDelayCount)
			fmt.Fprintf(w, "# HELP control_persist_metadata_update_ms_sum Sum of SaveCommittedBlock metadata update duration in milliseconds.\n")
			fmt.Fprintf(w, "# TYPE control_persist_metadata_update_ms_sum counter\n")
			fmt.Fprintf(w, "control_persist_metadata_update_ms_sum %d\n", commitStats.PersistMetadataUpdateSumMs)
			fmt.Fprintf(w, "# HELP control_persist_metadata_update_ms_count Count of SaveCommittedBlock metadata update attempts.\n")
			fmt.Fprintf(w, "# TYPE control_persist_metadata_update_ms_count counter\n")
			fmt.Fprintf(w, "control_persist_metadata_update_ms_count %d\n", commitStats.PersistMetadataUpdateCount)
			fmt.Fprintf(w, "# HELP control_persist_metadata_update_failures_total Total SaveCommittedBlock metadata update failures.\n")
			fmt.Fprintf(w, "# TYPE control_persist_metadata_update_failures_total counter\n")
			fmt.Fprintf(w, "control_persist_metadata_update_failures_total %d\n", commitStats.PersistMetadataUpdateFailures)
			fmt.Fprintf(w, "# HELP control_persist_execute_total Total persistence execution attempts by role and decision.\n")
			fmt.Fprintf(w, "# TYPE control_persist_execute_total counter\n")
			fmt.Fprintf(w, "control_persist_execute_total{role=\"proposer\",decision=\"allowed\"} %d\n", commitStats.PersistExecuteProposer)
			fmt.Fprintf(w, "control_persist_execute_total{role=\"non_proposer\",decision=\"allowed\"} %d\n", commitStats.PersistExecuteNonProposer)
			fmt.Fprintf(w, "control_persist_execute_total{role=\"non_proposer\",decision=\"dropped_non_owner\"} %d\n", commitStats.PersistExecuteDroppedNonOwner)
			fmt.Fprintf(w, "# HELP control_apply_block_runs_total Total ApplyBlock executions observed by the wiring layer.\n")
			fmt.Fprintf(w, "# TYPE control_apply_block_runs_total counter\n")
			fmt.Fprintf(w, "control_apply_block_runs_total %d\n", commitStats.ApplyBlockRuns)
			fmt.Fprintf(w, "# HELP control_apply_block_txs_total Total transactions processed by ApplyBlock, partitioned by transaction type.\n")
			fmt.Fprintf(w, "# TYPE control_apply_block_txs_total counter\n")
			fmt.Fprintf(w, "control_apply_block_txs_total{type=\"event\"} %d\n", commitStats.ApplyBlockEventTxs)
			fmt.Fprintf(w, "control_apply_block_txs_total{type=\"evidence\"} %d\n", commitStats.ApplyBlockEvidenceTxs)
			fmt.Fprintf(w, "control_apply_block_txs_total{type=\"policy\"} %d\n", commitStats.ApplyBlockPolicyTxs)
			writeCommitLatency("control_apply_block_total_seconds", "Histogram of ApplyBlock total latency in seconds.", commitStats.ApplyBlockTotalBuckets, commitStats.ApplyBlockTotalCount, commitStats.ApplyBlockTotalSumMs, commitStats.ApplyBlockTotalP95Ms)
			writeCommitLatency("control_apply_block_validate_seconds", "Histogram of ApplyBlock validation latency in seconds.", commitStats.ApplyBlockValidateBuckets, commitStats.ApplyBlockValidateCount, commitStats.ApplyBlockValidateSumMs, commitStats.ApplyBlockValidateP95Ms)
			writeCommitLatency("control_apply_block_nonce_check_seconds", "Histogram of ApplyBlock nonce-check latency in seconds.", commitStats.ApplyBlockNonceCheckBuckets, commitStats.ApplyBlockNonceCheckCount, commitStats.ApplyBlockNonceCheckSumMs, commitStats.ApplyBlockNonceCheckP95Ms)
			writeCommitLatency("control_apply_block_reducer_event_seconds", "Histogram of ApplyBlock event reducer latency in seconds.", commitStats.ApplyBlockReducerEventBuckets, commitStats.ApplyBlockReducerEventCount, commitStats.ApplyBlockReducerEventSumMs, commitStats.ApplyBlockReducerEventP95Ms)
			writeCommitLatency("control_apply_block_reducer_evidence_seconds", "Histogram of ApplyBlock evidence reducer latency in seconds.", commitStats.ApplyBlockReducerEvidenceBuckets, commitStats.ApplyBlockReducerEvidenceCount, commitStats.ApplyBlockReducerEvidenceSumMs, commitStats.ApplyBlockReducerEvidenceP95Ms)
			writeCommitLatency("control_apply_block_reducer_policy_seconds", "Histogram of ApplyBlock policy reducer latency in seconds.", commitStats.ApplyBlockReducerPolicyBuckets, commitStats.ApplyBlockReducerPolicyCount, commitStats.ApplyBlockReducerPolicySumMs, commitStats.ApplyBlockReducerPolicyP95Ms)
			writeCommitLatency("control_apply_block_commit_state_seconds", "Histogram of ApplyBlock state-commit latency in seconds.", commitStats.ApplyBlockCommitStateBuckets, commitStats.ApplyBlockCommitStateCount, commitStats.ApplyBlockCommitStateSumMs, commitStats.ApplyBlockCommitStateP95Ms)
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
			fmt.Fprintf(w, "# HELP control_policy_ack_partition_assignments_total Total partition assignments observed by ACK consumer group sessions.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_partition_assignments_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_partition_assignments_total %d\n", ackStats.PartitionAssignments)
			fmt.Fprintf(w, "# HELP control_policy_ack_partition_revocations_total Total partition revocations observed by ACK consumer group sessions.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_partition_revocations_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_partition_revocations_total %d\n", ackStats.PartitionRevocations)
			fmt.Fprintf(w, "# HELP control_policy_ack_partitions_owned_current Current number of ACK topic partitions owned by this consumer instance.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_partitions_owned_current gauge\n")
			fmt.Fprintf(w, "control_policy_ack_partitions_owned_current %d\n", ackStats.PartitionsOwnedCurrent)
		}
		if causal, ok := s.outboxStats.GetPolicyAckCausalStats(); ok {
			fmt.Fprintf(w, "# HELP control_policy_causal_skew_corrections_total Total causal latency corrections due to clock skew.\n")
			fmt.Fprintf(w, "# TYPE control_policy_causal_skew_corrections_total counter\n")
			fmt.Fprintf(w, "control_policy_causal_skew_corrections_total %d\n", causal.SkewCorrectionsTotal)
			fmt.Fprintf(w, "# HELP control_policy_ack_correlation_command_total Total ACK correlations matched via policy_id+command_id.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_correlation_command_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_correlation_command_total %d\n", causal.CorrelationCommand)
			fmt.Fprintf(w, "# HELP control_policy_ack_correlation_exact_total Total ACK correlations matched via policy_id+rule_hash+trace_id.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_correlation_exact_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_correlation_exact_total %d\n", causal.CorrelationExact)
			fmt.Fprintf(w, "# HELP control_policy_ack_correlation_fallback_hash_total Total ACK correlations matched via policy_id+rule_hash fallback.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_correlation_fallback_hash_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_correlation_fallback_hash_total %d\n", causal.CorrelationFallbackHash)
			fmt.Fprintf(w, "# HELP control_policy_ack_correlation_fallback_trace_total Total ACK correlations matched via policy_id+trace_id fallback.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_correlation_fallback_trace_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_correlation_fallback_trace_total %d\n", causal.CorrelationFallbackTrace)
			fmt.Fprintf(w, "# HELP control_policy_ack_correlation_no_match_total Total ACK events with no outbox correlation match.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_correlation_no_match_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_correlation_no_match_total %d\n", causal.CorrelationNoMatch)
			fmt.Fprintf(w, "# HELP control_policy_ack_correlation_errors_total Total ACK correlation query errors.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_correlation_errors_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_correlation_errors_total %d\n", causal.CorrelationErrors)
			if causal.CorrelationLatencyCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_ack_correlation_latency_seconds Histogram of ACK outbox-correlation query/update latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_ack_correlation_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_ack_correlation_latency_seconds", causal.CorrelationLatencyBuckets, causal.CorrelationLatencyCount, causal.CorrelationLatencySumMs)
				fmt.Fprintf(w, "# HELP control_policy_ack_correlation_latency_p95_seconds 95th percentile ACK correlation latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_ack_correlation_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_ack_correlation_latency_p95_seconds %.6f\n", causal.CorrelationLatencyP95Ms/1000.0)
			}
			fmt.Fprintf(w, "# HELP control_policy_ack_ai_event_unit_corrections_total Total AI-event timestamp unit corrections during ACK causal computation.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_ai_event_unit_corrections_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_ai_event_unit_corrections_total %d\n", causal.AIEventUnitCorrections)
			fmt.Fprintf(w, "# HELP control_policy_ack_ai_event_invalid_total Total invalid AI-event timestamps skipped during ACK causal computation.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_ai_event_invalid_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_ai_event_invalid_total %d\n", causal.AIEventInvalidTotal)
			fmt.Fprintf(w, "# HELP control_policy_ack_source_event_unit_corrections_total Total source-event timestamp unit corrections during ACK causal computation.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_source_event_unit_corrections_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_source_event_unit_corrections_total %d\n", causal.SourceEventUnitCorrections)
			fmt.Fprintf(w, "# HELP control_policy_ack_source_event_invalid_total Total invalid source-event timestamps skipped during ACK causal computation.\n")
			fmt.Fprintf(w, "# TYPE control_policy_ack_source_event_invalid_total counter\n")
			fmt.Fprintf(w, "control_policy_ack_source_event_invalid_total %d\n", causal.SourceEventInvalidTotal)
			if causal.AIToAckCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_ai_to_ack_latency_seconds Histogram of corrected AI-to-ACK causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_ai_to_ack_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_ai_to_ack_latency_seconds", causal.AIToAckBuckets, causal.AIToAckCount, causal.AIToAckSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_ai_to_ack_latency_p95_seconds 95th percentile corrected AI-to-ACK latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_ai_to_ack_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_ai_to_ack_latency_p95_seconds %.6f\n", causal.AIToAckP95Ms/1000.0)
			}
			if causal.SourceToAckCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_source_to_ack_latency_seconds Histogram of corrected source-event-to-ACK causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_source_to_ack_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_source_to_ack_latency_seconds", causal.SourceToAckBuckets, causal.SourceToAckCount, causal.SourceToAckSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_source_to_ack_latency_p95_seconds 95th percentile corrected source-event-to-ACK latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_source_to_ack_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_source_to_ack_latency_p95_seconds %.6f\n", causal.SourceToAckP95Ms/1000.0)
			}
			if causal.PublishToAckCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_publish_to_ack_latency_seconds Histogram of corrected publish-to-ACK causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_publish_to_ack_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_publish_to_ack_latency_seconds", causal.PublishToAckBuckets, causal.PublishToAckCount, causal.PublishToAckSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_publish_to_ack_latency_p95_seconds 95th percentile corrected publish-to-ACK latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_publish_to_ack_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_publish_to_ack_latency_p95_seconds %.6f\n", causal.PublishToAckP95Ms/1000.0)
			}
			if causal.PublishToAppliedCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_published_to_applied_latency_seconds Histogram of corrected published-to-applied causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_published_to_applied_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_published_to_applied_latency_seconds", causal.PublishToAppliedBuckets, causal.PublishToAppliedCount, causal.PublishToAppliedSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_published_to_applied_latency_p95_seconds 95th percentile corrected published-to-applied latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_published_to_applied_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_published_to_applied_latency_p95_seconds %.6f\n", causal.PublishToAppliedP95Ms/1000.0)
			}
			if causal.AppliedToAckCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_applied_to_acked_latency_seconds Histogram of corrected applied-to-acked causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_applied_to_acked_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_applied_to_acked_latency_seconds", causal.AppliedToAckBuckets, causal.AppliedToAckCount, causal.AppliedToAckSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_applied_to_acked_latency_p95_seconds 95th percentile corrected applied-to-acked latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_applied_to_acked_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_applied_to_acked_latency_p95_seconds %.6f\n", causal.AppliedToAckP95Ms/1000.0)
			}
		}
		if pub, ok := s.outboxStats.GetPolicyPublisherStats(); ok {
			if len(pub.DedupeSuppressedByReason) > 0 {
				fmt.Fprintf(w, "# HELP control_policy_publish_dedupe_suppressed_total Total policy publish events suppressed by dedupe window, grouped by reason.\n")
				fmt.Fprintf(w, "# TYPE control_policy_publish_dedupe_suppressed_total counter\n")
				for reason, count := range pub.DedupeSuppressedByReason {
					fmt.Fprintf(w, "control_policy_publish_dedupe_suppressed_total{reason=\"%s\"} %d\n", sanitizeLabelValue(reason), count)
				}
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
			fmt.Fprintf(w, "# HELP control_policy_outbox_wake_signals_total Total coalesced JIT wake signals delivered to dispatcher.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_wake_signals_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_wake_signals_total %d\n", outbox.WakeSignalsTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_wake_signals_dropped_total Total dispatcher wake signals dropped due to full wake queue.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_wake_signals_dropped_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_wake_signals_dropped_total %d\n", outbox.WakeSignalsDroppedTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_wake_queue_depth Current dispatcher wake queue depth.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_wake_queue_depth gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_wake_queue_depth %d\n", outbox.WakeQueueDepth)
			fmt.Fprintf(w, "# HELP control_policy_outbox_wake_queue_depth_avg Average dispatcher wake queue depth sampled during Notify calls.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_wake_queue_depth_avg gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_wake_queue_depth_avg %.6f\n", outbox.WakeQueueDepthAvg)
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
			fmt.Fprintf(w, "# HELP control_policy_outbox_claim_timeouts_total Total outbox claim phase timeouts.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_claim_timeouts_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_claim_timeouts_total %d\n", outbox.ClaimTimeouts)
			fmt.Fprintf(w, "# HELP control_policy_outbox_claim_budget_exhausted_total Total outbox claim phase budget-exhausted outcomes.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_claim_budget_exhausted_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_claim_budget_exhausted_total %d\n", outbox.ClaimBudgetExhausted)
			fmt.Fprintf(w, "# HELP control_policy_outbox_reclaim_timeouts_total Total outbox reclaim phase timeouts.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_reclaim_timeouts_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_reclaim_timeouts_total %d\n", outbox.ReclaimTimeouts)
			fmt.Fprintf(w, "# HELP control_policy_outbox_mark_timeouts_total Total outbox mark phase timeouts.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_mark_timeouts_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_mark_timeouts_total %d\n", outbox.MarkTimeouts)
			fmt.Fprintf(w, "# HELP control_policy_outbox_throttled_logs_total Total outbox logs suppressed by throttle.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_throttled_logs_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_throttled_logs_total %d\n", outbox.ThrottledLogsTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_skew_corrections_total Total outbox causal latency corrections due to clock skew.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_skew_corrections_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_skew_corrections_total %d\n", outbox.SkewCorrectionsTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_ai_event_unit_corrections_total Total AI-event timestamp unit corrections in outbox causal computation.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_ai_event_unit_corrections_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_ai_event_unit_corrections_total %d\n", outbox.AIEventUnitCorrections)
			fmt.Fprintf(w, "# HELP control_policy_outbox_ai_event_invalid_total Total invalid AI-event timestamps skipped in outbox causal computation.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_ai_event_invalid_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_ai_event_invalid_total %d\n", outbox.AIEventInvalidTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_source_event_unit_corrections_total Total source-event timestamp unit corrections in outbox causal computation.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_source_event_unit_corrections_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_source_event_unit_corrections_total %d\n", outbox.SourceEventUnitCorrections)
			fmt.Fprintf(w, "# HELP control_policy_outbox_source_event_invalid_total Total invalid source-event timestamps skipped in outbox causal computation.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_source_event_invalid_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_source_event_invalid_total %d\n", outbox.SourceEventInvalidTotal)
			fmt.Fprintf(w, "# HELP control_policy_outbox_publish_scope_fallback_total Total policy publish scope derivations that fell back to global routing.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_publish_scope_fallback_total counter\n")
			fmt.Fprintf(w, "control_policy_outbox_publish_scope_fallback_total %d\n", outbox.PublishScopeFallbacks)
			if len(outbox.PublishScopeResultTotals) > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_publish_scope_total Policy outbox publish attempts grouped by scope kind and result.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_publish_scope_total counter\n")
				for key, count := range outbox.PublishScopeResultTotals {
					scopeKind := key
					result := "unknown"
					if idx := strings.Index(key, ":"); idx > 0 && idx < len(key)-1 {
						scopeKind = key[:idx]
						result = key[idx+1:]
					}
					fmt.Fprintf(w, "control_policy_outbox_publish_scope_total{scope_kind=\"%s\",result=\"%s\"} %d\n", scopeKind, result, count)
				}
			}
			if len(outbox.PublishScopeRouteTotals) > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_publish_scope_route_total Policy outbox publish attempts grouped by routing scope kind, bucketed flag, and result.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_publish_scope_route_total counter\n")
				for key, count := range outbox.PublishScopeRouteTotals {
					parts := strings.SplitN(key, ":", 3)
					if len(parts) != 3 {
						continue
					}
					scopeKind := sanitizeLabelValue(parts[0])
					bucketed := sanitizeLabelValue(parts[1])
					result := sanitizeLabelValue(parts[2])
					fmt.Fprintf(w, "control_policy_outbox_publish_scope_route_total{scope_kind=\"%s\",bucketed=\"%s\",result=\"%s\"} %d\n", scopeKind, bucketed, result, count)
				}
			}
			if len(outbox.PublishResultTotals) > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_publish_total Policy outbox publish attempts grouped by topic and result.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_publish_total counter\n")
				for key, count := range outbox.PublishResultTotals {
					topic := key
					result := "unknown"
					if idx := strings.LastIndex(key, ":"); idx > 0 && idx < len(key)-1 {
						topic = key[:idx]
						result = key[idx+1:]
					}
					fmt.Fprintf(w, "control_policy_outbox_publish_total{topic=\"%s\",result=\"%s\"} %d\n", topic, result, count)
				}
			}
			if len(outbox.PublishPartitionTotals) > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_publish_partition_total Policy outbox publish attempts grouped by topic, partition, and result.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_publish_partition_total counter\n")
				for key, count := range outbox.PublishPartitionTotals {
					topic := "unknown"
					partition := "unknown"
					result := "unknown"
					parts := strings.SplitN(key, ":", 3)
					if len(parts) == 3 {
						topic = parts[0]
						partition = parts[1]
						result = parts[2]
					}
					fmt.Fprintf(w, "control_policy_outbox_publish_partition_total{topic=\"%s\",partition=\"%s\",result=\"%s\"} %d\n", topic, partition, result, count)
				}
			}
			if len(outbox.TimeoutCauseTotals) > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_timeout_cause_total Policy outbox timeout and pressure outcomes by cause.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_timeout_cause_total counter\n")
				for cause, count := range outbox.TimeoutCauseTotals {
					fmt.Fprintf(w, "control_policy_outbox_timeout_cause_total{cause=\"%s\"} %d\n", sanitizeLabelValue(cause), count)
				}
			}
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
			if outbox.OutboxCreatedToClaimedCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_created_to_claimed_latency_seconds Histogram of outbox row created-to-claimed latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_created_to_claimed_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_outbox_created_to_claimed_latency_seconds", outbox.OutboxCreatedToClaimedBuckets, outbox.OutboxCreatedToClaimedCount, outbox.OutboxCreatedToClaimedSumMs)
				fmt.Fprintf(w, "# HELP control_policy_outbox_created_to_claimed_latency_p95_seconds 95th percentile outbox row created-to-claimed latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_created_to_claimed_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_outbox_created_to_claimed_latency_p95_seconds %.6f\n", outbox.OutboxCreatedToClaimedP95Ms/1000.0)
			}
			if outbox.WakeToClaimCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_wake_to_claim_latency_seconds Histogram of dispatcher wake-to-first-claim latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_wake_to_claim_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_outbox_wake_to_claim_latency_seconds", outbox.WakeToClaimBuckets, outbox.WakeToClaimCount, outbox.WakeToClaimSumMs)
				fmt.Fprintf(w, "# HELP control_policy_outbox_wake_to_claim_latency_p95_seconds 95th percentile dispatcher wake-to-first-claim latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_wake_to_claim_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_outbox_wake_to_claim_latency_p95_seconds %.6f\n", outbox.WakeToClaimP95Ms/1000.0)
			}
			if outbox.MarkLatencyCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_outbox_mark_latency_seconds Histogram of outbox state-mark latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_mark_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_outbox_mark_latency_seconds", outbox.MarkLatencyBuckets, outbox.MarkLatencyCount, outbox.MarkLatencySumMs)
				fmt.Fprintf(w, "# HELP control_policy_outbox_mark_latency_p95_seconds 95th percentile outbox state-mark latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_outbox_mark_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_outbox_mark_latency_p95_seconds %.6f\n", outbox.MarkLatencyP95Ms/1000.0)
			}
			if outbox.SourceToPublishCount > 0 {
				fmt.Fprintf(w, "# HELP control_policy_causal_source_to_publish_latency_seconds Histogram of corrected source-event-to-publish causal latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_source_to_publish_latency_seconds histogram\n")
				writePrometheusHistogram(w, "control_policy_causal_source_to_publish_latency_seconds", outbox.SourceToPublishBuckets, outbox.SourceToPublishCount, outbox.SourceToPublishSumMs)
				fmt.Fprintf(w, "# HELP control_policy_causal_source_to_publish_latency_p95_seconds 95th percentile corrected source-event-to-publish latency in seconds.\n")
				fmt.Fprintf(w, "# TYPE control_policy_causal_source_to_publish_latency_p95_seconds gauge\n")
				fmt.Fprintf(w, "control_policy_causal_source_to_publish_latency_p95_seconds %.6f\n", outbox.SourceToPublishP95Ms/1000.0)
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
			fmt.Fprintf(w, "# HELP control_policy_outbox_backlog_cache_age_seconds Age of cached outbox backlog summary counters in seconds.\n")
			fmt.Fprintf(w, "# TYPE control_policy_outbox_backlog_cache_age_seconds gauge\n")
			fmt.Fprintf(w, "control_policy_outbox_backlog_cache_age_seconds %.3f\n", float64(backlog.BacklogCacheAgeMs)/1000.0)
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
			if causal, ok := s.outboxStats.GetPolicyAckCausalStats(); ok {
				if outbox, ok := s.outboxStats.GetPolicyOutboxDispatcherStats(); ok {
					gates := s.evaluateControlPolicyGates(nowTime, backlog, outbox, causal, ackRatio, dbIntegrityTotal, dbTxLocationMismatchTotal)
					fmt.Fprintf(w, "# HELP control_policy_gate_published_pending_lt_500 Gate 1: published-pending rows below 500 and stable.\n")
					fmt.Fprintf(w, "# TYPE control_policy_gate_published_pending_lt_500 gauge\n")
					fmt.Fprintf(w, "control_policy_gate_published_pending_lt_500 %.0f\n", boolToFloat(gates.PublishedPendingLT500))
					fmt.Fprintf(w, "# HELP control_policy_gate_backlog_drain_rate_gt5rps Gate 2: backlog draining faster than 5 rows/second.\n")
					fmt.Fprintf(w, "# TYPE control_policy_gate_backlog_drain_rate_gt5rps gauge\n")
					fmt.Fprintf(w, "control_policy_gate_backlog_drain_rate_gt5rps %.0f\n", boolToFloat(gates.DrainRateGT5))
					fmt.Fprintf(w, "# HELP control_policy_backlog_drain_rate_rows_per_second Current backlog drain rate in rows per second.\n")
					fmt.Fprintf(w, "# TYPE control_policy_backlog_drain_rate_rows_per_second gauge\n")
					fmt.Fprintf(w, "control_policy_backlog_drain_rate_rows_per_second %.6f\n", gates.DrainRateRowsPerSecond)
					fmt.Fprintf(w, "# HELP control_policy_gate_outbox_created_to_claimed_p95_lt_2s Gate 3: outbox created-to-claimed p95 below 2s.\n")
					fmt.Fprintf(w, "# TYPE control_policy_gate_outbox_created_to_claimed_p95_lt_2s gauge\n")
					fmt.Fprintf(w, "control_policy_gate_outbox_created_to_claimed_p95_lt_2s %.0f\n", boolToFloat(gates.OutboxCreatedToClaimedP95LT2s))
					fmt.Fprintf(w, "# HELP control_policy_gate_published_to_applied_p95_lt_5s Gate 4: published-to-applied p95 below 5s.\n")
					fmt.Fprintf(w, "# TYPE control_policy_gate_published_to_applied_p95_lt_5s gauge\n")
					fmt.Fprintf(w, "control_policy_gate_published_to_applied_p95_lt_5s %.0f\n", boolToFloat(gates.PublishedToAppliedP95LT5s))
					fmt.Fprintf(w, "# HELP control_policy_gate_applied_to_acked_p95_lt_10s Gate 5: applied-to-acked p95 below 10s.\n")
					fmt.Fprintf(w, "# TYPE control_policy_gate_applied_to_acked_p95_lt_10s gauge\n")
					fmt.Fprintf(w, "control_policy_gate_applied_to_acked_p95_lt_10s %.0f\n", boolToFloat(gates.AppliedToAckedP95LT10s))
					fmt.Fprintf(w, "# HELP control_policy_gate_ack_over_published_gt_0_9 Gate 6: ack-over-published ratio above 0.9.\n")
					fmt.Fprintf(w, "# TYPE control_policy_gate_ack_over_published_gt_0_9 gauge\n")
					fmt.Fprintf(w, "control_policy_gate_ack_over_published_gt_0_9 %.0f\n", boolToFloat(gates.AckOverPublishedGT09))
					fmt.Fprintf(w, "# HELP control_policy_gate_no_integrity_mismatch_regression Gate 7: no integrity/mismatch regression signals.\n")
					fmt.Fprintf(w, "# TYPE control_policy_gate_no_integrity_mismatch_regression gauge\n")
					fmt.Fprintf(w, "control_policy_gate_no_integrity_mismatch_regression %.0f\n", boolToFloat(gates.NoIntegrityMismatchRegression))
					fmt.Fprintf(w, "# HELP control_policy_gate_soak_windows_met Gate 8: short and long soak windows both satisfied.\n")
					fmt.Fprintf(w, "# TYPE control_policy_gate_soak_windows_met gauge\n")
					fmt.Fprintf(w, "control_policy_gate_soak_windows_met %.0f\n", boolToFloat(gates.SoakWindowsMet))
					fmt.Fprintf(w, "# HELP control_policy_release_ready All control-policy gates satisfied.\n")
					fmt.Fprintf(w, "# TYPE control_policy_release_ready gauge\n")
					fmt.Fprintf(w, "control_policy_release_ready %.0f\n", boolToFloat(gates.ReleaseReady))
				}
			}
		}
	}
	fmt.Fprintf(w, "# HELP control_mutation_blocked_safe_mode_total Total mutation requests blocked by safe mode.\n")
	fmt.Fprintf(w, "# TYPE control_mutation_blocked_safe_mode_total counter\n")
	fmt.Fprintf(w, "control_mutation_blocked_safe_mode_total %d\n", s.controlMutationBlockedSafeMode.Load())
	fmt.Fprintf(w, "# HELP control_mutation_blocked_kill_switch_total Total mutation requests blocked by kill-switch.\n")
	fmt.Fprintf(w, "# TYPE control_mutation_blocked_kill_switch_total counter\n")
	fmt.Fprintf(w, "control_mutation_blocked_kill_switch_total %d\n", s.controlMutationBlockedKillSwitch.Load())
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
	if s.currentControlMutationSafeModeWithTimeout() {
		fmt.Fprintf(w, "control_mutation_safe_mode_enabled 1\n")
	} else {
		fmt.Fprintf(w, "control_mutation_safe_mode_enabled 0\n")
	}
	fmt.Fprintf(w, "# HELP control_mutation_kill_switch_enabled Global control mutation kill-switch state (1=enabled).\n")
	fmt.Fprintf(w, "# TYPE control_mutation_kill_switch_enabled gauge\n")
	if s.currentControlMutationKillSwitchWithTimeout() {
		fmt.Fprintf(w, "control_mutation_kill_switch_enabled 1\n")
	} else {
		fmt.Fprintf(w, "control_mutation_kill_switch_enabled 0\n")
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
		fmt.Fprintf(w, "# HELP api_route_requests_total Total HTTP requests per method/path/status class.\n")
		fmt.Fprintf(w, "# TYPE api_route_requests_total counter\n")
		fmt.Fprintf(w, "# HELP api_route_request_errors_total Total error responses per method/path/status class.\n")
		fmt.Fprintf(w, "# TYPE api_route_request_errors_total counter\n")
		fmt.Fprintf(w, "# HELP api_route_request_duration_seconds Histogram of request latency in seconds per method/path/status class.\n")
		fmt.Fprintf(w, "# TYPE api_route_request_duration_seconds histogram\n")
		fmt.Fprintf(w, "# HELP api_route_request_duration_p95_seconds 95th percentile request latency in seconds per method/path/status class.\n")
		fmt.Fprintf(w, "# TYPE api_route_request_duration_p95_seconds gauge\n")
		fmt.Fprintf(w, "# HELP api_route_request_duration_p99_seconds 99th percentile request latency in seconds per method/path/status class.\n")
		fmt.Fprintf(w, "# TYPE api_route_request_duration_p99_seconds gauge\n")
		fmt.Fprintf(w, "# HELP api_route_response_bytes_total Total response bytes per method/path/status class.\n")
		fmt.Fprintf(w, "# TYPE api_route_response_bytes_total counter\n")
		fmt.Fprintf(w, "# HELP api_route_cache_hits_total Cache hits observed per method/path/status class.\n")
		fmt.Fprintf(w, "# TYPE api_route_cache_hits_total counter\n")
		fmt.Fprintf(w, "# HELP api_route_cache_misses_total Cache misses observed per method/path/status class.\n")
		fmt.Fprintf(w, "# TYPE api_route_cache_misses_total counter\n")
		for _, snap := range routeSnapshots {
			labels := fmt.Sprintf("method=\"%s\",path=\"%s\",status_class=\"%s\"", sanitizeLabelValue(snap.method), sanitizeLabelValue(snap.path), sanitizeLabelValue(snap.statusClass))
			fmt.Fprintf(w, "api_route_requests_total{%s} %d\n", labels, snap.requests)
			fmt.Fprintf(w, "api_route_request_errors_total{%s} %d\n", labels, snap.errors)
			writePrometheusHistogramWithLabels(w, "api_route_request_duration_seconds", labels, snap.latencyBuckets, snap.latencyCount, snap.latencySumMs)
			fmt.Fprintf(w, "api_route_request_duration_p95_seconds{%s} %.6f\n", labels, snap.latencyP95Ms/1000.0)
			fmt.Fprintf(w, "api_route_request_duration_p99_seconds{%s} %.6f\n", labels, snap.latencyP99Ms/1000.0)
			fmt.Fprintf(w, "api_route_response_bytes_total{%s} %d\n", labels, snap.bytes)
			fmt.Fprintf(w, "api_route_cache_hits_total{%s} %d\n", labels, snap.cacheHits)
			fmt.Fprintf(w, "api_route_cache_misses_total{%s} %d\n", labels, snap.cacheMisses)
		}
	}

	dashboardSnapshots := s.snapshotDashboardSectionMetrics()
	if len(dashboardSnapshots) > 0 {
		fmt.Fprintf(w, "# HELP api_dashboard_section_duration_seconds Histogram of dashboard overview section build latency in seconds.\n")
		fmt.Fprintf(w, "# TYPE api_dashboard_section_duration_seconds histogram\n")
		fmt.Fprintf(w, "# HELP api_dashboard_section_duration_p95_seconds 95th percentile dashboard overview section build latency in seconds.\n")
		fmt.Fprintf(w, "# TYPE api_dashboard_section_duration_p95_seconds gauge\n")
		fmt.Fprintf(w, "# HELP api_dashboard_section_duration_p99_seconds 99th percentile dashboard overview section build latency in seconds.\n")
		fmt.Fprintf(w, "# TYPE api_dashboard_section_duration_p99_seconds gauge\n")
		for _, snap := range dashboardSnapshots {
			labels := fmt.Sprintf("section=\"%s\"", sanitizeLabelValue(snap.section))
			writePrometheusHistogramWithLabels(w, "api_dashboard_section_duration_seconds", labels, snap.buckets, snap.count, snap.sumMs)
			fmt.Fprintf(w, "api_dashboard_section_duration_p95_seconds{%s} %.6f\n", labels, snap.p95Ms/1000.0)
			fmt.Fprintf(w, "api_dashboard_section_duration_p99_seconds{%s} %.6f\n", labels, snap.p99Ms/1000.0)
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

type controlPolicyGateSnapshot struct {
	PublishedPendingLT500         bool
	DrainRateGT5                  bool
	DrainRateRowsPerSecond        float64
	OutboxCreatedToClaimedP95LT2s bool
	PublishedToAppliedP95LT5s     bool
	AppliedToAckedP95LT10s        bool
	AckOverPublishedGT09          bool
	NoIntegrityMismatchRegression bool
	SoakWindowsMet                bool
	ReleaseReady                  bool
}

func (s *Server) evaluateControlPolicyGates(now time.Time, backlog policyoutbox.BacklogStats, outbox policyoutbox.DispatcherStats, causal PolicyAckCausalStats, ackRatio float64, dbIntegrityTotal uint64, dbTxLocationMismatchTotal uint64) controlPolicyGateSnapshot {
	g := controlPolicyGateSnapshot{}
	publishedPending := backlog.PublishedRows
	g.PublishedPendingLT500 = publishedPending < 500 && backlog.OldestPendingAge <= 5*60*1000

	g.OutboxCreatedToClaimedP95LT2s = outbox.OutboxCreatedToClaimedCount > 0 && outbox.OutboxCreatedToClaimedP95Ms <= 2000
	g.PublishedToAppliedP95LT5s = causal.PublishToAppliedCount > 0 && causal.PublishToAppliedP95Ms <= 5000
	g.AppliedToAckedP95LT10s = causal.AppliedToAckCount > 0 && causal.AppliedToAckP95Ms <= 10000
	g.AckOverPublishedGT09 = ackRatio > 0.9

	// No regression means no new terminal failures/fenced update spikes/correlation errors and no DB integrity+mismatch growth.
	g.NoIntegrityMismatchRegression = outbox.TerminalFailedTotal == 0 && outbox.FencedUpdateFailures == 0 && causal.CorrelationErrors == 0

	s.controlGateStateMu.Lock()
	defer s.controlGateStateMu.Unlock()
	if s.controlGateLastSampleAt.IsZero() {
		s.controlGateLastSampleAt = now
		s.controlGateLastPublishedRows = backlog.PublishedRows
		s.controlGateLastAckedRows = backlog.AckedRows
		s.controlGateLastOldestPendingAgeMs = backlog.OldestPendingAge
		s.controlGateLastIntegrityTotal = dbIntegrityTotal
		s.controlGateLastTxMismatchTotal = dbTxLocationMismatchTotal
		g.DrainRateRowsPerSecond = 0
		g.DrainRateGT5 = false
		g.NoIntegrityMismatchRegression = false
	} else {
		elapsed := now.Sub(s.controlGateLastSampleAt).Seconds()
		if elapsed > 0 {
			prevBacklog := s.controlGateLastPublishedRows - s.controlGateLastAckedRows
			currBacklog := backlog.PublishedRows - backlog.AckedRows
			drained := float64(prevBacklog - currBacklog)
			g.DrainRateRowsPerSecond = drained / elapsed
			g.DrainRateGT5 = g.DrainRateRowsPerSecond > 5.0 && backlog.OldestPendingAge <= s.controlGateLastOldestPendingAgeMs
		}
		if dbIntegrityTotal > s.controlGateLastIntegrityTotal || dbTxLocationMismatchTotal > s.controlGateLastTxMismatchTotal {
			g.NoIntegrityMismatchRegression = false
		}
		s.controlGateLastSampleAt = now
		s.controlGateLastPublishedRows = backlog.PublishedRows
		s.controlGateLastAckedRows = backlog.AckedRows
		s.controlGateLastOldestPendingAgeMs = backlog.OldestPendingAge
		s.controlGateLastIntegrityTotal = dbIntegrityTotal
		s.controlGateLastTxMismatchTotal = dbTxLocationMismatchTotal
	}

	// Gate 8 requires sustained healthy behavior across two consecutive windows:
	// 15 minutes load + 30 minutes steady-state = 45 minutes continuous pass.
	soakPrereq := g.PublishedPendingLT500 &&
		g.DrainRateGT5 &&
		g.OutboxCreatedToClaimedP95LT2s &&
		g.PublishedToAppliedP95LT5s &&
		g.AppliedToAckedP95LT10s &&
		g.AckOverPublishedGT09 &&
		g.NoIntegrityMismatchRegression
	if soakPrereq {
		if s.controlGateContinuousPassSince.IsZero() {
			s.controlGateContinuousPassSince = now
		}
		g.SoakWindowsMet = now.Sub(s.controlGateContinuousPassSince) >= 45*time.Minute
	} else {
		s.controlGateContinuousPassSince = time.Time{}
		g.SoakWindowsMet = false
	}

	g.ReleaseReady = g.PublishedPendingLT500 &&
		g.DrainRateGT5 &&
		g.OutboxCreatedToClaimedP95LT2s &&
		g.PublishedToAppliedP95LT5s &&
		g.AppliedToAckedP95LT10s &&
		g.AckOverPublishedGT09 &&
		g.NoIntegrityMismatchRegression &&
		g.SoakWindowsMet
	return g
}

func boolToFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
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
	writePrometheusHistogramWithLabels(w, name, "", buckets, count, sumMs)
}

func writePrometheusHistogramWithLabels(w io.Writer, name, labels string, buckets []utils.HistogramBucket, count uint64, sumMs float64) {
	var cumulative uint64
	for _, bucket := range buckets {
		cumulative += bucket.Count
		le := "+Inf"
		if !math.IsInf(bucket.UpperBound, 1) {
			le = fmt.Sprintf("%.6f", bucket.UpperBound/1000.0)
		}
		if labels == "" {
			fmt.Fprintf(w, "%s_bucket{le=\"%s\"} %d\n", name, le, cumulative)
		} else {
			fmt.Fprintf(w, "%s_bucket{%s,le=\"%s\"} %d\n", name, labels, le, cumulative)
		}
	}
	if labels == "" {
		fmt.Fprintf(w, "%s_sum %.6f\n", name, sumMs/1000.0)
		fmt.Fprintf(w, "%s_count %d\n", name, count)
	} else {
		fmt.Fprintf(w, "%s_sum{%s} %.6f\n", name, labels, sumMs/1000.0)
		fmt.Fprintf(w, "%s_count{%s} %d\n", name, labels, count)
	}
}
