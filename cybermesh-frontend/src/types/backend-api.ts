/**
 * Go Backend API Response Types
 * 
 * These interfaces define the raw response structures from the Go backend.
 * Edge functions transform these into the frontend-expected formats.
 */

// ============= Common Types =============

export interface GoApiResponse<T> {
  data?: T;
  error?: string;
  updatedAt?: string;
}

// ============= Blockchain Backend Types =============

export interface GoBlockMetrics {
  latest_height: number | null;
  total_transactions: number | null;
  avg_block_time: string | null;
  avg_block_size: string | null;
  success_rate: string | null;
  pending_txs: number | null;
  mempool_size: string | null;
}

export interface GoLedgerSnapshot {
  snapshot_height: string | null;
  state_version: number | null;
  rolling_block_time: string | null;
  rolling_block_size: string | null;
  state_root: string | null;
  last_block_hash: string | null;
  snapshot_time: string | null;
  reputation_changes: number;
  policy_updates: number;
  quarantine_updates: number;
}

export interface GoTimelineDataPoint {
  time: string;
  approved: number;
  rejected: number;
}

export interface GoNetworkSnapshot {
  blocks_anomalies: number;
  total_anomalies: number;
  peer_latency_avg: string;
}

export interface GoBlockTransaction {
  hash: string;
  size: string;
}

export interface GoBlockDetail {
  height: number;
  hash: string;
  timestamp: string;
  tx_count: number;
  size: string;
  transactions: GoBlockTransaction[];
}

export interface GoBlockSummary {
  height: number;
  time: string;
  txs: string;
  hash: string;
  proposer: string;
}

export interface GoBlockchainResponse {
  updated?: string;
  metrics?: GoBlockMetrics;
  ledger?: GoLedgerSnapshot;
  timeline_data?: GoTimelineDataPoint[];
  network_snapshot?: GoNetworkSnapshot;
  selected_block?: GoBlockDetail | null;
  latest_blocks?: GoBlockSummary[];
  // Alternative flat structure
  blocks?: GoBlockSummary[];
  stats?: GoBlockMetrics;
}

// ============= Threats Backend Types =============

export interface GoSeverityData {
  count: number;
  percent: string;
}

export interface GoSeverityDistribution {
  critical: GoSeverityData;
  high: GoSeverityData;
  medium: GoSeverityData;
  low: GoSeverityData;
}

export interface GoThreatBreakdown {
  type: string;
  severity: "CRITICAL" | "HIGH" | "MEDIUM" | "LOW";
  blocked: number;
  monitored: number;
  block_rate: string;
  total: number;
}

export interface GoThreatSnapshot {
  total_detected: number;
  published_snapshot: number;
  abstained_snapshot: number;
  loop_status: "LIVE" | "HALTED";
  last_detection: string;
}

export interface GoVolumeDataPoint {
  time: string;
  value: number;
}

export interface GoThreatsResponse {
  system_status?: "LIVE" | "HALTED";
  blocked_count?: string;
  updated?: string;
  lifetime_total?: number;
  published_count?: number;
  published_percent?: string;
  abstained_count?: number;
  abstained_percent?: string;
  avg_response_time?: string;
  validator_count?: number;
  quorum_size?: number;
  severity?: GoSeverityDistribution;
  breakdown?: GoThreatBreakdown[];
  snapshot?: GoThreatSnapshot;
  volume_history?: GoVolumeDataPoint[];
  // Alternative nested structure
  summary?: {
    total: number;
    blocked: number;
    severity: GoSeverityDistribution;
  };
  detections?: GoThreatBreakdown[];
}

// ============= System Health Backend Types =============

export interface GoBackendUptime {
  started: string;
  runtime: string;
}

export interface GoAIUptime {
  state: "running" | "halted" | "unknown";
  loop_status: string;
  detection_loop_uptime: string;
}

export interface GoSystemResources {
  cpu: {
    avg_utilization: string;
  };
  memory: {
    resident: string;
    allocated: string | null;
  };
}

export interface GoServiceInfo {
  name: string;
  status: "healthy" | "warning" | "degraded" | "halted";
  latency?: string;
  query_p95?: string;
  pool?: string;
  version?: string | number;
  success?: string;
  pending?: number;
  size?: string;
  oldest?: string;
  view?: number;
  round?: number;
  quorum?: number;
  peers?: string;
  throughput?: string;
  publish_p95?: string;
  lag?: number;
  highwater?: string;
  clients?: string | null;
  errors?: number;
  txn_p95?: string;
  slow_txns?: number;
  loop?: string;
  publish_min?: number;
  detections_min?: number;
  issues: string;
}

export interface GoKafkaProducerData {
  publish_p95: string;
  publish_p50: string;
  successes: number;
  failures: number;
}

export interface GoNetworkData {
  peers: number;
  latency: string;
  throughput_in: string;
  mempool_size: string;
}

export interface GoThreatFeedData {
  source: string;
  history: string;
  fallback_count: string | null;
  last_fallback: string | null;
}

export interface GoPipelineData {
  kafka_producer: GoKafkaProducerData;
  network: GoNetworkData;
  threat_feed: GoThreatFeedData;
}

export interface GoCockroachData {
  database: string;
  latency: string;
  pool: string;
  query_p95: string;
}

export interface GoRedisData {
  mode: string;
  role: string | null;
  latency: string;
  clients: string | null;
}

export interface GoDatabaseData {
  cockroach: GoCockroachData;
  redis: GoRedisData;
}

export interface GoAILoopData {
  loop_status: "Running" | "Paused" | "Stopped";
  avg_latency: string;
  last_iteration: string;
  publish_rate: string;
}

export interface GoSystemHealthResponse {
  service_readiness?: "Ready" | "Degraded" | "Halted";
  ai_status?: "AI Running" | "AI Halted" | "AI Unknown";
  updated?: string;
  backend_uptime?: GoBackendUptime;
  ai_uptime?: GoAIUptime;
  resources?: GoSystemResources;
  services?: GoServiceInfo[];
  pipeline?: GoPipelineData;
  db?: GoDatabaseData;
  ai_loop?: GoAILoopData;
  // Alternative flat structure
  status?: {
    readiness: string;
    ai: string;
  };
  uptime?: GoBackendUptime;
}

// ============= Transformation Utilities =============

/**
 * Converts snake_case keys to camelCase
 */
export function snakeToCamel(str: string): string {
  return str.replace(/_([a-z])/g, (_, letter) => letter.toUpperCase());
}

/**
 * Recursively transforms an object's keys from snake_case to camelCase
 */
export function transformKeys<T>(obj: unknown): T {
  if (obj === null || obj === undefined) {
    return obj as T;
  }
  
  if (Array.isArray(obj)) {
    return obj.map(item => transformKeys(item)) as T;
  }
  
  if (typeof obj === 'object') {
    const transformed: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(obj as Record<string, unknown>)) {
      transformed[snakeToCamel(key)] = transformKeys(value);
    }
    return transformed as T;
  }
  
  return obj as T;
}
