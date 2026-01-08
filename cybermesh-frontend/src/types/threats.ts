export type SystemStatus = "LIVE" | "HALTED";
export type ThreatSeverity = "CRITICAL" | "HIGH" | "MEDIUM" | "LOW";

export interface SeverityData {
  count: number;
  percent: string;
}

export interface SeverityDistribution {
  critical: SeverityData;
  high: SeverityData;
  medium: SeverityData;
  low: SeverityData;
}

export interface ThreatBreakdown {
  type: string;
  severity: ThreatSeverity;
  blocked: number;
  monitored: number;
  blockRate: string;
  total: number;
}

export interface ThreatSnapshot {
  totalDetected: number;
  publishedSnapshot: number;
  abstainedSnapshot: number;
  loopStatus: SystemStatus;
  lastDetection: string;
}

export interface VolumeDataPoint {
  time: string;
  value: number;
}

// Detection latency data point for the performance chart
export interface DetectionLatencyPoint {
  time: string;
  timestamp: number;
  latency: number; // ms
  candidates: number;
  confidence: number;
}

// Lifetime detection totals from backend
export interface DetectionLifetimeTotals {
  total: number;
  published: number;
  abstained: number;
  rateLimited: number;
  errors: number;
}

// Detection loop performance metrics
export interface DetectionLoopMetrics {
  running: boolean;
  avgLatencyMs: number;
  lastLatencyMs: number;
  secondsSinceLastDetection: number;
  iterationsTotal: number;
}

export interface DataWindow {
  itemCount: number;
  timeRangeLabel: string; // e.g., "Last 2.3 hours"
  oldestTimestamp?: string;
  newestTimestamp?: string;
}

export interface ThreatsData {
  systemStatus: SystemStatus;
  blockedCount: string;
  updated: string;
  lifetimeTotal: number;
  publishedCount: number;
  publishedPercent: string;
  abstainedCount: number;
  abstainedPercent: string;
  avgResponseTime: string;
  validatorCount: number;
  quorumSize: number;
  severity: SeverityDistribution;
  breakdown: ThreatBreakdown[];
  snapshot: ThreatSnapshot;
  volumeHistory: VolumeDataPoint[];
  latencyHistory: DetectionLatencyPoint[]; // New latency history
  loopMetrics: DetectionLoopMetrics;       // New loop metrics
  dataWindow: DataWindow;
}

