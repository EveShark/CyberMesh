// System Health mock data extracted from SystemHealth.tsx
import type { SystemHealthData } from "@/types/system-health";

const START_TIME = Date.now() - (4 * 60 * 60 * 1000 + 51 * 60 * 1000); // 4h 51m ago start

const formatDuration = (ms: number) => {
  const hours = Math.floor(ms / (1000 * 60 * 60));
  const minutes = Math.floor((ms % (1000 * 60 * 60)) / (1000 * 60));
  return `${hours}h ${minutes}m`;
};

export const getMockSystemHealthData = (): SystemHealthData => {
  const now = Date.now();
  const uptimeMs = now - START_TIME;
  const timeMod = now / 1000;

  // CPU Sine Wave (30% to 90%)
  const cpuLoad = 60 + Math.sin(timeMod / 10) * 20 + Math.random() * 10;

  // Memory Jitter
  const memUsage = 1.01 + Math.sin(timeMod / 20) * 0.05 + Math.random() * 0.01;

  // Service Latency Jitter
  const getLatency = (base: number) => (base + Math.random() * base * 0.5).toFixed(1);

  // Dynamic Service Health Generator
  const getServiceHealth = (name: string, baseLatency: number) => {
    const latencyVal = parseFloat(getLatency(baseLatency));
    // Simulate occasional latency spikes or warnings (5% chance)
    const isSpiking = Math.random() > 0.95;
    const currentLatency = isSpiking ? latencyVal * 3 : latencyVal;

    let status: "healthy" | "warning" | "halted" = "healthy";
    let issues = "No issues reported";

    if (currentLatency > baseLatency * 2.5) {
      status = "warning";
      issues = `${name} latency high: ${currentLatency.toFixed(1)}ms`;
    }

    // Random rare failure (1% chance)
    if (Math.random() > 0.99) {
      status = "halted";
      issues = "Connection timeout";
    }

    return {
      status,
      latency: `${currentLatency.toFixed(1)} ms`,
      issues
    };
  };

  return {
    serviceReadiness: "Ready",
    aiStatus: "AI Running",
    updated: new Date().toLocaleTimeString(),
    backendUptime: { started: formatDuration(uptimeMs) + " ago", runtime: formatDuration(uptimeMs) },
    aiUptime: { state: "running", loopStatus: "Loop: Ok • " + formatDuration(uptimeMs % 3600000) + " ago", detectionLoopUptime: formatDuration(uptimeMs - 60000) },
    resources: { cpu: { avgUtilization: `${cpuLoad.toFixed(1)}%` }, memory: { resident: `${memUsage.toFixed(2)} GB`, allocated: null } },
    services: [
      { name: "Storage", ...getServiceHealth("Storage", 12) },
      { name: "State Store", ...getServiceHealth("State Store", 8), version: 214, success: "100.0%" },
      { name: "Mempool", ...getServiceHealth("Mempool", 5), pending: 900 + Math.floor(Math.random() * 50), size: "526.8 KB", oldest: "4h 32m" },
      { name: "Consensus Engine", ...getServiceHealth("Consensus Engine", 8), view: 111470 + Math.floor(timeMod / 5), round: 101395 + Math.floor(timeMod / 5), quorum: 3 },
      { name: "P2P Quorum", ...getServiceHealth("P2P Quorum", 900), peers: "4 (3 in / 4 out)", throughput: `${(500 + Math.random() * 50).toFixed(0)} B/s in · 144 B/s out` },
      { name: "Kafka", ...getServiceHealth("Kafka", 1500), publishP95: `${(1.3 + Math.random() * 0.2).toFixed(2)} s`, lag: Math.floor(Math.random() * 5), highwater: "419.3K" },
      { name: "Redis", ...getServiceHealth("Redis", 450), clients: null, errors: Math.floor(Math.random() * 20) },
      { name: "CockroachDB", ...getServiceHealth("CockroachDB", 1200), txnP95: `${(1.7 + Math.random() * 0.2).toFixed(2)} s`, slowTxns: 50 + Math.floor(Math.random() * 5) },
      { name: "AI Service", ...getServiceHealth("AI Service", 580), loop: "ok", publishMin: 12, detectionsMin: 12 }
    ],
    pipeline: {
      kafkaProducer: { publishP95: `${(1.4 + Math.random() * 0.1).toFixed(2)} s`, publishP50: `${getLatency(440)} ms`, successes: 38 + Math.floor(timeMod / 10), failures: 0 },
      network: { peers: 4, latency: `${getLatency(960)} ms`, throughputIn: "0.5 KB/s", mempoolSize: "526.8 KB" },
      threatFeed: { source: "History", history: "Updated 0m ago", fallbackCount: null, lastFallback: null }
    },
    db: {
      cockroach: { database: "defaultdb", latency: `${getLatency(12)} ms`, pool: "0/2", queryP95: `${getLatency(43)} ms` },
      redis: { mode: "standalone", role: null, latency: `${getLatency(350)} ms`, clients: null }
    },
    aiLoop: {
      loopStatus: "Running",
      avgLatency: `${getLatency(580)} ms`,
      lastIteration: `${getLatency(350)} ms`,
      publishRate: `${(10 + Math.random() * 5).toFixed(1)}/min`
    }
  };
};

export const mockSystemHealthData: SystemHealthData = getMockSystemHealthData();

export interface SystemHealthResponse extends SystemHealthData {
  updatedAt: string;
}

export const mockSystemHealthResponse: SystemHealthResponse = {
  ...mockSystemHealthData,
  updatedAt: new Date().toISOString(),
};
