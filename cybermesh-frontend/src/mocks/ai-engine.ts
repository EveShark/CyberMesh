import type { AIEngineData } from "@/types/ai-engine";
import type { DashboardAIRaw } from "@/lib/api/types/raw";
import { adaptAIEngine } from "@/lib/api/adapters/ai-engine.adapter";

// RAW Backend Data (Mock)
// Function to generate dynamic realistic mock data
// Stateful storage for detection history
const detectionState = {
  lastUpdated: Date.now(),
  detections: [] as any[],
  totalCount: 924,
  publishedCount: 924,
  iterationCount: 868
};

// Initialize with some history
if (detectionState.detections.length === 0) {
  const now = Date.now();
  for (let i = 0; i < 5; i++) {
    detectionState.detections.push({
      id: `det-${now - i * 5000}`,
      timestamp: new Date(now - i * 15000).toISOString(),
      source: ["ML Ensemble", "Rules Engine", "Math Engine"][i % 3],
      threat_type: ["ddos", "botnet_c2", "sql_injection"][i % 3],
      severity: [9, 7, 5][i % 3],
      confidence: 0.95 - (i * 0.02),
      final_score: 0.98 - (i * 0.03),
      should_publish: true,
      decision: "published",
      metadata: { latency_ms: 45 + i * 2 }
    });
  }
}

export const getMockAIEngineRaw = (): DashboardAIRaw => {
  // Logic for suspicious nodes "loop": 0, 1, or 2 nodes
  const stateCounter = Math.floor(Date.now() / 30000) % 3;

  // Update stateful metrics
  const now = Date.now();
  const elapsed = now - detectionState.lastUpdated;

  // Probabilistic new detection every ~5 seconds
  if (elapsed > 5000 && Math.random() > 0.3) {
    const newDetection = {
      id: `det-${now}`,
      timestamp: new Date(now).toISOString(),
      source: ["ML Ensemble", "Rules Engine", "Math Engine"][Math.floor(Math.random() * 3)],
      threat_type: ["ddos", "botnet_c2", "limit_break", "anomaly"][Math.floor(Math.random() * 4)],
      severity: Math.floor(Math.random() * 5) + 5,
      confidence: 0.85 + Math.random() * 0.14,
      final_score: 0.9 + Math.random() * 0.1,
      should_publish: true,
      decision: "published",
      metadata: {
        latency_ms: 60 + Math.random() * 40,
        candidate_count: Math.floor(Math.random() * 5) + 1
      }
    };

    detectionState.detections.unshift(newDetection);
    if (detectionState.detections.length > 20) detectionState.detections.pop();

    detectionState.totalCount++;
    detectionState.publishedCount++;
    detectionState.lastUpdated = now;
  }

  detectionState.iterationCount++;

  const allPossibleNodes = [
    {
      id: "d9661d66c0", // Draco (simulated)
      status: "suspicious",
      suspicion_score: 1.0,
      event_count: 56,
      last_seen: new Date(Date.now() - 1000 * 60 * 2).toISOString(),
      threat_types: ["ddos"]
    },
    {
      id: "b722e1a4f2", // Cassiopeia (simulated)
      status: "suspicious",
      suspicion_score: 0.65,
      event_count: 12,
      last_seen: new Date(Date.now() - 1000 * 60 * 15).toISOString(),
      threat_types: ["data_leakage"]
    }
  ];

  let activeNodes: typeof allPossibleNodes = [];
  if (stateCounter === 1) activeNodes = [allPossibleNodes[0]];
  if (stateCounter === 2) activeNodes = allPossibleNodes;

  // Realistic jitter for cache age and latency
  const jitter = (Math.sin(Date.now() / 5000) + 1) / 2; // 0 to 1
  const cacheAge = 0.4 + jitter * 0.8; // 0.4s to 1.2s
  const lastLatency = 78.25 + (jitter - 0.5) * 4; // 76.25 to 80.25

  return {
    metrics: {
      status: "running",
      state: "running",
      loop: {
        running: true,
        status: "running",
        message: "Processing telemetry streams",
        avg_latency_ms: 74.81,
        last_latency_ms: lastLatency,
        seconds_since_last_detection: (now - detectionState.lastUpdated) / 1000,
        seconds_since_last_iteration: 0.4,
        cache_age_seconds: cacheAge,
        healthy: true,
        counters: {
          detections_total: detectionState.totalCount,
          detections_published: detectionState.publishedCount,
          detections_rate_limited: 0,
          errors: 0,
          loop_iterations: detectionState.iterationCount
        }
      },
      derived: {
        detections_per_minute: 10 + (Math.sin(Date.now() / 20000) * 5) + (Math.random() * 2), // 5-17 detections/min
        iterations_per_minute: 11 + (Math.sin(Date.now() / 15000) * 3) + (Math.random() * 2), // 8-16 iterations/min
        publish_rate_per_minute: 9 + (Math.sin(Date.now() / 18000) * 4) + (Math.random() * 2), // 5-15 publishes/min
        publish_success_ratio: 0.98 + (Math.random() * 0.02)
      },
      engines: [
        {
          engine: "ML Ensemble",
          ready: true,
          candidates: 868 + Math.floor(Math.random() * 5),
          published: 868,
          publish_ratio: 0.99,
          throughput_per_minute: 8.5 + Math.random() * 3,
          avg_confidence: 0.94 + Math.random() * 0.05,
          last_latency_ms: 78.3 + Math.random() * 5
        },
        {
          engine: "Rules Engine",
          ready: true,
          candidates: 12,
          published: 12,
          publish_ratio: 1.0,
          throughput_per_minute: 2.0 + Math.random(),
          avg_confidence: 0.98,
          last_latency_ms: 45.6 + Math.random() * 2
        },
        {
          engine: "Math Engine",
          ready: true,
          candidates: 44,
          published: 44,
          publish_ratio: 1.0,
          throughput_per_minute: 1.5 + Math.random(),
          avg_confidence: 0.88 + Math.random() * 0.04,
          last_latency_ms: 82.1 + Math.random() * 10
        }
      ],
      variants: []
    },
    suspicious: {
      nodes: activeNodes,
      updated_at: new Date().toISOString()
    },
    history: {
      detections: detectionState.detections,
      count: detectionState.detections.length,
      updated_at: new Date().toISOString()
    }
  };
};

export const mockAIEngineRaw = getMockAIEngineRaw();

// Adapted Frontend Data
export const mockAIEngineData: AIEngineData = adaptAIEngine(mockAIEngineRaw);
