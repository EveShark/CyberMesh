// Status types for conditional styling
export type ServiceStatus = "healthy" | "warning" | "degraded" | "halted";
export type ReadinessStatus = "Ready" | "Degraded" | "Halted";
export type AIStatus = "AI Running" | "AI Halted" | "AI Unknown";
export type LoopStatus = "Running" | "Paused" | "Stopped";

// Backend uptime data
export interface BackendUptime {
  started: string;
  runtime: string;
}

// AI uptime data
export interface AIUptime {
  state: "running" | "halted" | "unknown";
  loopStatus: string;
  detectionLoopUptime: string;
}

// System resources
export interface SystemResources {
  cpu: {
    avgUtilization: string;
  };
  memory: {
    resident: string;
    allocated: string | null;
  };
}

// Individual service info for the health grid
export interface ServiceInfo {
  name: string;
  status: ServiceStatus;
  // Optional metrics - different services have different metrics
  latency?: string;
  queryP95?: string;
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
  publishP95?: string;
  lag?: number;
  highwater?: string;
  clients?: string | null;
  errors?: number;
  txnP95?: string;
  slowTxns?: number;
  loop?: string;
  publishMin?: number;
  detectionsMin?: number;
  issues: string;
}

// Kafka producer data
export interface KafkaProducerData {
  publishP95: string;
  publishP50: string;
  successes: number;
  failures: number;
}

// Network data
export interface NetworkData {
  peers: number;
  latency: string;
  throughputIn: string;
  mempoolSize: string;
}

// Threat feed data
export interface ThreatFeedData {
  source: string;
  history: string;
  fallbackCount: string | null;
  lastFallback: string | null;
}

// Pipeline data
export interface PipelineData {
  kafkaProducer: KafkaProducerData;
  network: NetworkData;
  threatFeed: ThreatFeedData;
}

// CockroachDB data
export interface CockroachData {
  database: string;
  latency: string;
  pool: string;
  queryP95: string;
}

// Redis data
export interface RedisData {
  mode: string;
  role: string | null;
  latency: string;
  clients: string | null;
}

// Database panel data
export interface DatabaseData {
  cockroach: CockroachData;
  redis: RedisData;
}

// AI Loop data
export interface AILoopData {
  loopStatus: LoopStatus;
  avgLatency: string;
  lastIteration: string;
  publishRate: string;
}

// Main system health data structure
export interface SystemHealthData {
  serviceReadiness: ReadinessStatus;
  aiStatus: AIStatus;
  updated: string;
  backendUptime: BackendUptime;
  aiUptime: AIUptime;
  resources: SystemResources;
  services: ServiceInfo[];
  pipeline: PipelineData;
  db: DatabaseData;
  aiLoop: AILoopData;
}

// Helper function to get status color classes
export const getStatusColors = (status: ServiceStatus | ReadinessStatus | AIStatus | LoopStatus | string): string => {
  const normalizedStatus = status.toLowerCase();
  
  if (normalizedStatus === "healthy" || normalizedStatus === "ready" || normalizedStatus === "ai running" || normalizedStatus === "running" || normalizedStatus === "ok") {
    return "bg-emerald-500/20 text-emerald-400 border-emerald-500/30";
  }
  if (normalizedStatus === "warning" || normalizedStatus === "degraded" || normalizedStatus === "paused") {
    return "bg-amber-500/20 text-amber-400 border-amber-500/30";
  }
  if (normalizedStatus === "halted" || normalizedStatus === "ai halted" || normalizedStatus === "stopped") {
    return "bg-red-500/20 text-red-400 border-red-500/30";
  }
  return "bg-muted/20 text-muted-foreground border-border/30";
};

// Helper to display value or "--" for empty
export const displayValue = (value: string | number | null | undefined): string => {
  if (value === null || value === undefined || value === "") {
    return "--";
  }
  return String(value);
};
