/**
 * Raw Backend DTO Types
 * These interfaces represent the exact JSON structure returned by the Go backend API.
 * Must be kept in sync with backend/pkg/api/dto.go
 */

// Common Response Wrapper
export interface BackendResponse<T> {
  success: boolean;
  data: T;
  error?: { code: string; message: string; details?: Record<string, unknown>; request_id?: string; timestamp?: number };
  meta?: { request_id?: string; timestamp: number; version?: string };
}

// Dashboard Overview Response
export interface DashboardOverviewRaw {
  timestamp: number; // milliseconds since epoch
  backend: DashboardBackendRaw;
  ledger?: DashboardLedgerRaw;
  validators?: DashboardValidatorsRaw;
  network?: NetworkOverviewRaw;
  consensus?: ConsensusOverviewRaw;
  blocks: DashboardBlocksRaw;
  threats: DashboardThreatsRaw;
  ai: DashboardAIRaw;
}

// Backend Section
export interface DashboardBackendRaw {
  health: { status: string; timestamp: number };
  readiness: { ready: boolean; checks: Record<string, { status: string; latency_ms: number }> };
  stats: StatsResponseRaw;
  metrics: DashboardBackendMetricsRaw;
  derived: DashboardBackendDerivedRaw;
  history: DashboardBackendHistorySampleRaw[];
}

export interface StatsResponseRaw {
  chain?: ChainStatsRaw;
  consensus?: ConsensusStatsRaw;
  mempool?: MempoolStatsRaw;
  network?: NetworkStatsRaw;
  storage?: StorageStatsRaw;
  redis?: RedisStatsRaw;
}

export interface ChainStatsRaw {
  height: number;
  state_version: number;
  total_transactions: number;
  avg_block_time_seconds?: number;
  avg_block_size_bytes?: number;
  success_rate: number;
}

export interface ConsensusStatsRaw {
  view: number;
  round: number;
  validator_count: number;
  quorum_size: number;
  current_leader?: string;
  current_leader_id?: string;
}

export interface MempoolStatsRaw {
  pending_transactions: number;
  size_bytes: number;
  oldest_tx_age_seconds?: number;
  oldest_tx_age_ms?: number;
}

export interface NetworkStatsRaw {
  peer_count: number;
  inbound_peers: number;
  outbound_peers: number;
  avg_latency_ms: number;
  bytes_received: number;
  bytes_sent: number;
}

export interface StorageStatsRaw {
  status?: string;
  ready_latency_ms?: number;
  pool_open_connections?: number;
  pool_in_use?: number;
  pool_idle?: number;
  database?: string;
  version?: string;
  node_id?: number;
  query_latency_p95_ms?: number;
  transaction_latency_p95_ms?: number;
}

export interface RedisStatsRaw {
  status?: string;
  ready_latency_ms?: number;
  mode?: string;
  role?: string;
  version?: string;
  connected_clients?: number;
  ops_per_sec?: number;
  uptime_seconds?: number;
  total_connections_received?: number;
  total_commands_processed?: number;
}

export interface DashboardBackendMetricsRaw {
  summary: { cpu_seconds_total: number; resident_memory_bytes: number; goroutines: number; process_start_time_seconds?: number };
  requests: { total: number; errors: number };
  kafka?: DashboardKafkaMetricsRaw;
  redis?: DashboardRedisMetricsRaw;
  cockroach?: DashboardCockroachMetricsRaw;
}

export interface DashboardKafkaMetricsRaw {
  publish_success: number;
  publish_failure: number;
  broker_count: number;
}

export interface DashboardRedisMetricsRaw {
  pool_hits: number;
  pool_misses: number;
  total_connections: number;
  idle_connections: number;
  timeouts: number;
  command_errors: number;
  command_latency_p95_ms: number;
  ops_per_sec: number;
  connected_clients: number;
}

export interface DashboardCockroachMetricsRaw {
  open_connections: number;
  in_use: number;
  idle: number;
  wait_seconds: number;
  wait_total: number;
}

export interface DashboardBackendDerivedRaw {
  ai_loop_status?: string;
  threat_data_source?: string;
}

export interface DashboardBackendHistorySampleRaw {
  timestamp: number;
  cpu_percent: number;
  resident_memory_bytes: number;
}

// Ledger Section
export interface DashboardLedgerRaw {
  latest_height: number;
  state_version: number;
  total_transactions: number;
  avg_block_time_seconds: number;
  avg_block_size_bytes?: number;
  state_root?: string;
  last_block_hash?: string;
  snapshot_block_height: number;
  snapshot_transaction_count?: number;
  snapshot_timestamp?: number;
  pending_transactions: number;
  mempool_size_bytes: number;
  reputation_changes: number;
  policy_changes: number;
  quarantine_changes: number;
  mempool_oldest_tx_age_ms?: number;
}

// Validators Section
export interface DashboardValidatorsRaw {
  total: number;
  active: number;
  inactive: number;
  validators: DashboardValidatorSnapshotRaw[];
  updated_at: number;
}

export interface DashboardValidatorSnapshotRaw {
  id: string;
  address: string;
  status: string;
  alias: string;
  last_seen_unix?: number;
}

// Network Overview
export interface NetworkOverviewRaw {
  connected_peers: number;
  total_peers: number;
  expected_peers: number;
  average_latency_ms: number;
  consensus_round: number;
  leader_stability: number;
  phase: string;
  leader?: string;
  leader_id?: string;
  nodes: NetworkNodeRaw[];
  voting_status: VotingStatusRaw[];
  edges?: NetworkEdgeRaw[];
  self?: string; // peer ID of self node
  inbound_rate_bps?: number;
  outbound_rate_bps?: number;
  updated_at: string; // ISO8601 timestamp
}

export interface NetworkNodeRaw {
  id: string;
  name: string;
  status: string;
  latency: number;
  uptime: number;
  throughput_bytes: number;
  last_seen?: string;
  inbound_rate_bps?: number;
}

export interface NetworkEdgeRaw {
  source: string;
  target: string;
  direction: string;
  status: string;
  confidence?: string;
  reported_by?: string;
  updated_at?: string;
}

export interface VotingStatusRaw {
  node_id: string;
  voting: boolean;
}

// Consensus Overview
export interface ConsensusOverviewRaw {
  leader?: string;
  leader_id?: string;
  term: number;
  phase: string;
  active_peers: number;
  quorum_size: number;
  proposals: ConsensusProposalRaw[];
  votes: ConsensusVoteRaw[];
  suspicious_nodes: SuspiciousNodeRaw[];
  updated_at: string; // ISO8601 timestamp
}

export interface ConsensusProposalRaw {
  block: number;
  view: number;
  hash?: string;
  proposer?: string;
  timestamp: number; // UnixMilli (milliseconds)
}

export interface ConsensusVoteRaw {
  type: string;
  count: number;
  timestamp: number; // UnixMilli (milliseconds)
}

export interface SuspiciousNodeRaw {
  id: string;
  status: string;
  uptime: number;
  suspicion_score: number;
  reason?: string;
}

// Blocks Section
export interface DashboardBlocksRaw {
  recent: BlockResponseRaw[];
  decision_timeline: DashboardDecisionTimelineRaw[];
  metrics: DashboardBlockMetricsRaw;
  pagination: DashboardBlockPaginationRaw;
}

export interface BlockResponseRaw {
  height: number;
  hash: string;
  parent_hash: string;
  state_root?: string;
  timestamp: number; // seconds since epoch
  proposer: string;
  transaction_count: number;
  anomaly_count?: number;
  size_bytes?: number;
  size_bytes_estimated?: boolean;
  transactions?: TransactionResponseRaw[];
  metadata?: Record<string, unknown>;
}

export interface TransactionResponseRaw {
  hash: string;
  type: string;
  size_bytes: number;
  timestamp?: number;
  status?: string;
  metadata?: Record<string, unknown>;
}

export interface DashboardDecisionTimelineRaw {
  time: string;
  height: number;
  hash: string;
  proposer: string;
  approved: number;
  rejected: number;
  timeout?: number;
}

export interface DashboardBlockMetricsRaw {
  latest_height: number;
  total_transactions: number;
  avg_block_time_seconds: number;
  avg_block_size_bytes?: number;
  success_rate: number;
  anomaly_count: number;
  network?: NetworkStatsRaw;
}

export interface DashboardBlockPaginationRaw {
  limit: number;
  start: number;
  end: number;
  total: number;
  has_more: boolean;
}

// Threats Section
export interface DashboardThreatsRaw {
  timestamp: number; // milliseconds since epoch
  detection_loop?: AiDetectionLoopRaw;
  source?: string;
  fallback_reason?: string;
  breakdown: DashboardThreatBreakdownRaw;
  feed?: AnomalyResponseRaw[];
  stats?: AnomalyStatsRaw;
  avg_response_time_ms?: number;
  lifetime_totals?: DashboardThreatLifetimeTotalsRaw;
}

export interface AiDetectionLoopRaw {
  running: boolean;
  status?: string;
  message?: string;
  issues?: string[];
  blocking?: boolean;
  healthy?: boolean;
  avg_latency_ms: number;
  last_latency_ms: number;
  seconds_since_last_detection?: number;
  seconds_since_last_iteration?: number;
  cache_age_seconds?: number;
  counters?: AiDetectionLoopCountersRaw;
}

export interface AiDetectionLoopCountersRaw {
  detections_total: number;
  detections_published: number;
  detections_rate_limited: number;
  errors: number;
  loop_iterations: number;
}

export interface DashboardThreatBreakdownRaw {
  threat_types: DashboardThreatTypeSummaryRaw[];
  severity: Record<string, number>;
  totals: DashboardThreatTotalsRaw;
}

export interface DashboardThreatTypeSummaryRaw {
  threat_type: string;
  published: number;
  abstained: number;
  total: number;
  severity: string;
}

export interface DashboardThreatTotalsRaw {
  published: number;
  abstained: number;
  overall: number;
}

export interface DashboardThreatLifetimeTotalsRaw {
  total: number;
  published: number;
  abstained: number;
  rate_limited?: number;
  errors?: number;
}

export interface AnomalyResponseRaw {
  id: string;
  type: string;
  severity: string;
  title: string;
  description: string;
  source: string;
  block_height: number;
  timestamp: number; // seconds since epoch
  confidence: number;
  tx_hash: string;
}

export interface AnomalyStatsRaw {
  critical_count: number;
  high_count: number;
  medium_count: number;
  low_count: number;
  total_count: number;
}

// AI Section
export interface DashboardAIRaw {
  metrics?: AIMetricsRaw;
  history?: AiDetectionHistoryRaw;
  suspicious?: AiSuspiciousNodesRaw;
  health?: Record<string, unknown>;
  ready?: Record<string, unknown>;
}

export interface AIMetricsRaw {
  status: string;
  state: string;
  message?: string;
  loop?: AiDetectionLoopRaw;
  derived?: AiMetricsDerivedRaw;
  engines: AiEngineMetricRaw[];
  variants: AiVariantMetricRaw[];
}

export interface AiMetricsDerivedRaw {
  detections_per_minute?: number;
  publish_rate_per_minute?: number;
  iterations_per_minute?: number;
  error_rate_per_hour?: number;
  publish_success_ratio?: number;
}

export interface AiEngineMetricRaw {
  engine: string;
  ready: boolean;
  candidates: number;
  published: number;
  publish_ratio: number;
  throughput_per_minute: number;
  avg_confidence?: number;
  threat_types?: string[];
  last_latency_ms?: number;
  last_updated?: number;
}

export interface AiVariantMetricRaw {
  variant: string;
  engines?: string[];
  total: number;
  published: number;
  publish_ratio: number;
  throughput_per_minute: number;
  avg_confidence?: number;
  threat_types?: string[];
  last_updated?: number;
}

export interface AiDetectionHistoryRaw {
  detections: AiDetectionHistoryEntryRaw[];
  count: number;
  updated_at: string; // ISO8601 timestamp
}

export interface AiDetectionHistoryEntryRaw {
  timestamp: string; // ISO8601 timestamp
  source: string;
  validator_id?: string;
  threat_type: string;
  severity: number;
  confidence: number;
  final_score: number;
  should_publish: boolean;
  metadata?: Record<string, unknown>;
}

export interface AiSuspiciousNodesRaw {
  nodes: AiSuspiciousNodeRaw[];
  updated_at: string; // ISO8601 timestamp
}

export interface AiSuspiciousNodeRaw {
  id: string;
  status: string;
  suspicion_score: number;
  event_count: number;
  last_seen: string; // ISO8601 timestamp
  threat_types?: string[];
  reason?: string;
  uptime?: number;
}
