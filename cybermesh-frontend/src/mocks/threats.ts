import type { ThreatsData } from "@/types/threats";
import type { DashboardThreatsRaw, AiDetectionHistoryRaw } from "@/lib/api/types/raw";
import { adaptThreats } from "@/lib/api/adapters/threats.adapter";

// RAW Backend Data
// Function to generate dynamic realistic threat data
export const getMockData = (): { raw: DashboardThreatsRaw; history: AiDetectionHistoryRaw } => {
  // Simulate jitter in detection counts and latencies
  const timeMod = Math.floor(Date.now() / 10000) % 10;
  const jitter = (Math.sin(Date.now() / 8000) + 1) / 2; // 0 to 1

  // Dynamic breakdown counts
  const ddosCount = 1000 + Math.floor(jitter * 300);
  const total = 5610 + Math.floor(jitter * 500);

  // Dynamic Latency
  const currentLatency = 78.4 + (Math.random() * 10 - 5);

  // Generate synthetic detection history for the last 10 minutes (for volume chart)
  const historyEntries: any[] = [];
  const feedEntries: any[] = [];
  const now = Date.now();
  const historyWindowMs = 10 * 60 * 1000;

  // Base rate: ~2 detections per second with bursts
  for (let t = now - historyWindowMs; t < now; t += 500 + Math.random() * 1500) {
    if (Math.random() > 0.4) { // 60% chance of detection
      // Create a burst of 1-3 detections
      const burstSize = 1 + Math.floor(Math.random() * 3);
      for (let i = 0; i < burstSize; i++) {
        const timestampMs = t + i * 100;
        const type = ["ddos", "botnet_c2", "sql_injection", "mitm"][Math.floor(Math.random() * 4)];
        const severityStr = ["critical", "high"][Math.floor(Math.random() * 2)];
        const severityScore = severityStr === "critical" ? 9 : 7;
        const source = ["ML Ensemble", "Rules Engine", "Math Engine"][Math.floor(Math.random() * 3)];

        // Entry for AI History (ISO Timestamp)
        historyEntries.push({
          timestamp: new Date(timestampMs).toISOString(),
          source: source,
          threat_type: type,
          severity: severityScore,
          confidence: 0.8 + Math.random() * 0.2,
          final_score: 0.9,
          should_publish: true,
          metadata: {}
        });

        // Entry for Feed (Seconds Timestamp)
        feedEntries.push({
          id: `hist-${timestampMs}-${i}`,
          type: type,
          severity: severityStr,
          title: "Anomaly Detected",
          description: "Suspicious activity detected by engine.",
          source: source,
          block_height: 100000 + Math.floor(timestampMs / 1000),
          timestamp: timestampMs / 1000,
          confidence: 0.8 + Math.random() * 0.2,
          tx_hash: "0x..."
        });
      }
    }
  }

  // Get the last 5 items for the feed
  const feed = feedEntries.slice(-5).reverse();

  const raw: DashboardThreatsRaw = {
    timestamp: Date.now(),
    detection_loop: {
      running: true,
      avg_latency_ms: currentLatency,
      last_latency_ms: currentLatency + (Math.random() * 5),
      healthy: true,
      counters: {
        detections_total: 4125 + historyEntries.length,
        detections_published: 3950 + Math.floor(historyEntries.length * 0.9),
        loop_iterations: 3800 + Math.floor(jitter * 20),
        detections_rate_limited: 0,
        errors: 0
      }
    },
    breakdown: {
      threat_types: [
        { threat_type: "ddos", published: ddosCount - 200, abstained: 200, total: ddosCount, severity: "critical" },
        { threat_type: "botnet_c2", published: 850, abstained: 150, total: 1000, severity: "critical" },
        { threat_type: "sql_injection", published: 920, abstained: 80, total: 1000, severity: "high" },
        { threat_type: "mitm", published: 300, abstained: 200, total: 500, severity: "high" },
        { threat_type: "ransomware_sig", published: 150, abstained: 10, total: 160, severity: "critical" },
        { threat_type: "xss", published: 400, abstained: 100, total: 500, severity: "medium" },
        { threat_type: "brute_force", published: 600, abstained: 50, total: 650, severity: "medium" },
        { threat_type: "anomaly_detection", published: 200, abstained: 100, total: 300, severity: "low" },
        { threat_type: "port_scan", published: 150, abstained: 50, total: 200, severity: "low" }
      ],
      severity: {
        "critical": 2460 + Math.floor(jitter * 100),
        "high": 1500,
        "medium": 1150,
        "low": 500
      },
      totals: {
        published: 4820 + Math.floor(jitter * 50),
        abstained: 790,
        overall: total
      }
    },
    feed: feed,
    lifetime_totals: {
      total: 89432 + Math.floor(Date.now() / 1000),
      published: 82000 + Math.floor(Date.now() / 1200),
      abstained: 7000 + Math.floor(Date.now() / 5000),
      rate_limited: 432,
      errors: 0
    },
    stats: {
      total_count: 5610,
      critical_count: 2460,
      high_count: 1500,
      medium_count: 1150,
      low_count: 500
    },
    avg_response_time_ms: currentLatency
  };

  const history: AiDetectionHistoryRaw = {
    detections: historyEntries,
    count: historyEntries.length,
    updated_at: new Date().toISOString()
  };

  return { raw, history };
};

export const getMockThreatsRaw = (): DashboardThreatsRaw => getMockData().raw;

export const mockThreatsRaw = getMockThreatsRaw();

// Adapted Frontend Data
export const mockThreatsData: ThreatsData = adaptThreats(mockThreatsRaw);

export interface ThreatsResponse extends ThreatsData {
  updatedAt: string;
}

export const mockThreatsResponse: ThreatsResponse = {
  ...mockThreatsData,
  updatedAt: new Date().toISOString(),
};
