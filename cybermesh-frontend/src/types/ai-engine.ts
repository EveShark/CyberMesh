export type LoopStatus = "Running" | "Stopped" | "Unknown";
export type HealthStatus = "Healthy" | "Degraded" | "Unknown";
export type SeverityLevel = "Critical" | "High" | "Medium" | "Low";

export interface LoopStatusData {
  status: LoopStatus;
  statusMessage: string;
  detectionsPerMin: number | null;
  publishSuccess: number | null;
  publishToCandidateRatio: number | null;
  lastIteration: string | null;
  sinceLastIteration: string | null;
}

export interface DetectionLoopMetrics {
  status: LoopStatus;
  interval: string | null;
  avgLatency: number | null;
  lastLatency: number | null;
  sinceDetection: string | null;
  cacheAge: string | null;
  health: HealthStatus;
  targetLatency: number;
  iterationsCount: number;
}

export interface Validator {
  name: string;
  events: number;
  maxSeverity: number;
  lastSeen: string;
  hash: string;
  fullHash: string;
  score: number;
  threats: string[];
  severity: SeverityLevel;
}

export interface ValidatorsData {
  validators: Validator[];
  networkStatus: string;
}

export interface DetectionEvent {
  id: number;
  threatType: string;
  confidence: number;
  timestamp: string;
  finalScore: number;
  decision: string;
  metadata: {
    candidate_count?: number;
    latency_ms?: number;
    [key: string]: unknown;
  };
}

export interface DetectionStreamData {
  events: DetectionEvent[];
  totalCount: number;
  publishedCount: number;
  heldForReview: number;
}

export interface EnginePerformance {
  name: string;
  status: "Healthy" | "Warning" | "Error";
  throughput: string;
  publishRate: string;
  avgLatency: string;
  confidence: string;
  published: number;
}

export interface AIEngineData {
  loopStatus: LoopStatusData;
  detectionLoop: DetectionLoopMetrics;
  validators: ValidatorsData;
  detectionStream: DetectionStreamData;
  engines: EnginePerformance[];
  updatedAt: string;
}
