/**
 * Performance metrics utility for tracking API latency and logging slow responses.
 * Logs responses over 1 second to the console for debugging.
 */

interface PerformanceEntry {
  endpoint: string;
  duration: number;
  timestamp: Date;
  status: "success" | "error";
}

interface EndpointMetrics {
  avgLatency: number;
  minLatency: number;
  maxLatency: number;
  p95Latency: number;
  totalRequests: number;
  errorCount: number;
  slowRequestCount: number;
}

// Threshold for slow request warning (in ms)
const SLOW_REQUEST_THRESHOLD = 1000;

// Maximum number of entries to keep per endpoint
const MAX_ENTRIES_PER_ENDPOINT = 100;

// Store performance entries by endpoint
const performanceStore = new Map<string, PerformanceEntry[]>();

/**
 * Record a performance entry for an API call
 */
export function recordApiPerformance(
  endpoint: string,
  duration: number,
  status: "success" | "error" = "success"
): void {
  const entry: PerformanceEntry = {
    endpoint,
    duration,
    timestamp: new Date(),
    status,
  };

  // Get or create entries array for this endpoint
  if (!performanceStore.has(endpoint)) {
    performanceStore.set(endpoint, []);
  }
  
  const entries = performanceStore.get(endpoint)!;
  entries.push(entry);
  
  // Keep only the last MAX_ENTRIES_PER_ENDPOINT entries
  if (entries.length > MAX_ENTRIES_PER_ENDPOINT) {
    entries.shift();
  }

  // Log slow requests to console
  if (duration > SLOW_REQUEST_THRESHOLD) {
    console.warn(
      `[PERF] Slow API response: ${endpoint} took ${duration.toFixed(0)}ms`,
      {
        endpoint,
        duration: `${duration.toFixed(0)}ms`,
        threshold: `${SLOW_REQUEST_THRESHOLD}ms`,
        timestamp: entry.timestamp.toISOString(),
      }
    );
  }

  // Log all requests in development
  if (import.meta.env.DEV) {
    const statusColor = status === "success" ? "color: #22c55e" : "color: #ef4444";
    const durationColor = duration > SLOW_REQUEST_THRESHOLD ? "color: #f59e0b" : "color: #6b7280";
    
    console.log(
      `%c[API]%c ${endpoint} %c${duration.toFixed(0)}ms%c [${status}]`,
      "color: #3b82f6; font-weight: bold",
      "color: inherit",
      durationColor,
      statusColor
    );
  }
}

/**
 * Calculate percentile from sorted array
 */
function percentile(sortedArray: number[], p: number): number {
  if (sortedArray.length === 0) return 0;
  const index = Math.ceil((p / 100) * sortedArray.length) - 1;
  return sortedArray[Math.max(0, index)];
}

/**
 * Get metrics for a specific endpoint
 */
export function getEndpointMetrics(endpoint: string): EndpointMetrics | null {
  const entries = performanceStore.get(endpoint);
  if (!entries || entries.length === 0) return null;

  const durations = entries.map(e => e.duration).sort((a, b) => a - b);
  const errorCount = entries.filter(e => e.status === "error").length;
  const slowRequestCount = entries.filter(e => e.duration > SLOW_REQUEST_THRESHOLD).length;

  return {
    avgLatency: durations.reduce((a, b) => a + b, 0) / durations.length,
    minLatency: Math.min(...durations),
    maxLatency: Math.max(...durations),
    p95Latency: percentile(durations, 95),
    totalRequests: entries.length,
    errorCount,
    slowRequestCount,
  };
}

/**
 * Get metrics for all endpoints
 */
export function getAllMetrics(): Map<string, EndpointMetrics> {
  const allMetrics = new Map<string, EndpointMetrics>();
  
  for (const endpoint of performanceStore.keys()) {
    const metrics = getEndpointMetrics(endpoint);
    if (metrics) {
      allMetrics.set(endpoint, metrics);
    }
  }
  
  return allMetrics;
}

/**
 * Log a summary of all endpoint metrics to console
 */
export function logMetricsSummary(): void {
  const metrics = getAllMetrics();
  
  if (metrics.size === 0) {
    console.log("[PERF] No metrics recorded yet");
    return;
  }

  console.group("[PERF] API Performance Summary");
  
  for (const [endpoint, m] of metrics) {
    console.log(
      `${endpoint}:\n` +
      `  Avg: ${m.avgLatency.toFixed(0)}ms | P95: ${m.p95Latency.toFixed(0)}ms\n` +
      `  Min: ${m.minLatency.toFixed(0)}ms | Max: ${m.maxLatency.toFixed(0)}ms\n` +
      `  Requests: ${m.totalRequests} | Errors: ${m.errorCount} | Slow: ${m.slowRequestCount}`
    );
  }
  
  console.groupEnd();
}

/**
 * Clear all recorded metrics
 */
export function clearMetrics(): void {
  performanceStore.clear();
}

// Expose to window for debugging in browser console
if (typeof window !== "undefined") {
  (window as unknown as { __apiMetrics: { getAll: typeof getAllMetrics; log: typeof logMetricsSummary; clear: typeof clearMetrics } }).__apiMetrics = {
    getAll: getAllMetrics,
    log: logMetricsSummary,
    clear: clearMetrics,
  };
}
