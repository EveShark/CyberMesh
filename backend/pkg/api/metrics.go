package api

import (
	"fmt"
	"net/http"
	"time"
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
	now := time.Now().Unix()

	// API server metrics
	fmt.Fprintf(w, "# HELP api_server_running Server running status (1=running, 0=stopped)\n")
	fmt.Fprintf(w, "# TYPE api_server_running gauge\n")
	if s.running.Load() {
		fmt.Fprintf(w, "api_server_running 1\n")
	} else {
		fmt.Fprintf(w, "api_server_running 0\n")
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
	}

	// Timestamp of metrics scrape
	fmt.Fprintf(w, "# HELP api_metrics_scrape_timestamp_seconds Timestamp of last metrics scrape\n")
	fmt.Fprintf(w, "# TYPE api_metrics_scrape_timestamp_seconds gauge\n")
	fmt.Fprintf(w, "api_metrics_scrape_timestamp_seconds %d\n", now)

	// Build info
	fmt.Fprintf(w, "# HELP api_build_info API build information\n")
	fmt.Fprintf(w, "# TYPE api_build_info gauge\n")
	fmt.Fprintf(w, "api_build_info{version=\"%s\",environment=\"%s\"} 1\n", apiVersion, s.config.Environment)
}
