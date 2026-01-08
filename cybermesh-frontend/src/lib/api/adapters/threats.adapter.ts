import type { ThreatsData, ThreatBreakdown, VolumeDataPoint, DetectionLatencyPoint, DetectionLoopMetrics, ThreatSeverity, SystemStatus, DataWindow } from "@/types/threats";
import type { DashboardThreatsRaw, DashboardThreatTypeSummaryRaw, AiDetectionHistoryRaw, AiDetectionHistoryEntryRaw } from "../types/raw";
import { formatPercent, formatMs } from "./utils";

export function adaptThreats(raw?: DashboardThreatsRaw, validatorCount: number = 5, aiHistory?: AiDetectionHistoryRaw): ThreatsData {
  if (!raw) {
    return {
      severity: { critical: { count: 0, percent: "0%" }, high: { count: 0, percent: "0%" }, medium: { count: 0, percent: "0%" }, low: { count: 0, percent: "0%" } },
      breakdown: [],
      systemStatus: "HALTED" as SystemStatus,
      blockedCount: "0",
      updated: "",
      lifetimeTotal: 0,
      publishedCount: 0,
      publishedPercent: "0%",
      abstainedCount: 0,
      abstainedPercent: "0%",
      avgResponseTime: "0ms",
      validatorCount,
      quorumSize: Math.ceil(validatorCount * 2 / 3),
      snapshot: { totalDetected: 0, publishedSnapshot: 0, abstainedSnapshot: 0, loopStatus: "HALTED" as SystemStatus, lastDetection: "" },
      volumeHistory: [],
      latencyHistory: [],
      loopMetrics: { running: false, avgLatencyMs: 0, lastLatencyMs: 0, secondsSinceLastDetection: 0, iterationsTotal: 0 },
      dataWindow: { itemCount: 0, timeRangeLabel: "No data available" }
    };
  }

  const totals = raw.breakdown?.totals || { published: 0, abstained: 0, overall: 0 };
  const lifetime = raw.lifetime_totals || { total: 0, published: 0, abstained: 0 };

  // Generate volume history from AI detection history
  const volumeHistory = generateVolumeHistory(aiHistory?.detections || []);
  const latencyHistory = generateLatencyHistory(aiHistory?.detections || []);

  return {
    systemStatus: raw.detection_loop?.running ? "LIVE" : "HALTED",
    blockedCount: formatNumber(totals.published),
    updated: new Date().toLocaleTimeString(),
    lifetimeTotal: lifetime.total,
    publishedCount: totals.published,
    publishedPercent: formatPercent(totals.overall > 0 ? totals.published / totals.overall : 0) || "0%",
    abstainedCount: totals.abstained,
    abstainedPercent: formatPercent(totals.overall > 0 ? totals.abstained / totals.overall : 0) || "0%",
    avgResponseTime: formatMs(raw.avg_response_time_ms || raw.detection_loop?.avg_latency_ms || 0) || "0ms",
    validatorCount,
    quorumSize: Math.ceil(validatorCount * 2 / 3),
    severity: adaptSeverity(raw.breakdown?.severity, totals.overall),
    breakdown: adaptBreakdown(raw.breakdown?.threat_types || []),
    snapshot: {
      totalDetected: lifetime.total,
      publishedSnapshot: lifetime.published,
      abstainedSnapshot: lifetime.abstained,
      loopStatus: raw.detection_loop?.running ? "LIVE" : "HALTED",
      lastDetection: raw.detection_loop?.seconds_since_last_detection
        ? `${raw.detection_loop.seconds_since_last_detection.toFixed(1)}s ago`
        : new Date().toLocaleTimeString(),
    },
    volumeHistory,
    latencyHistory,
    loopMetrics: {
      running: raw.detection_loop?.running || false,
      avgLatencyMs: raw.detection_loop?.avg_latency_ms || 0,
      lastLatencyMs: raw.detection_loop?.last_latency_ms || 0,
      secondsSinceLastDetection: raw.detection_loop?.seconds_since_last_detection || 0,
      iterationsTotal: raw.detection_loop?.counters?.loop_iterations || 0,
    },
    dataWindow: calculateDataWindow(aiHistory?.detections || []),
  };
}

function formatNumber(num: number): string {
  return num.toLocaleString();
}

function adaptSeverity(severity: Record<string, number> | undefined, total: number) {
  const s = severity || {};
  const getPercent = (count: number) => formatPercent(total > 0 ? count / total : 0) || "0%";

  return {
    critical: { count: s["critical"] || 0, percent: getPercent(s["critical"] || 0) },
    high: { count: s["high"] || 0, percent: getPercent(s["high"] || 0) },
    medium: { count: s["medium"] || 0, percent: getPercent(s["medium"] || 0) },
    low: { count: s["low"] || 0, percent: getPercent(s["low"] || 0) },
  };
}

function adaptBreakdown(types: DashboardThreatTypeSummaryRaw[]): ThreatBreakdown[] {
  return types.map(t => ({
    type: t.threat_type,
    severity: t.severity.toUpperCase() as ThreatSeverity,
    blocked: t.published,
    monitored: t.abstained,
    blockRate: formatPercent(t.total > 0 ? t.published / t.total : 0) || "0%",
    total: t.total
  }));
}

/**
 * Generate volume history by aggregating detections into time buckets
 * Groups detections by 10-second intervals for the chart display
 */
function generateVolumeHistory(detections: AiDetectionHistoryEntryRaw[]): VolumeDataPoint[] {
  if (!detections || detections.length === 0) {
    return [];
  }

  // Sort detections by timestamp
  const sorted = [...detections].sort((a, b) =>
    new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
  );

  // Group by 10-second intervals
  const bucketMs = 10000; // 10 seconds
  const buckets = new Map<number, number>();

  for (const detection of sorted) {
    const ts = new Date(detection.timestamp).getTime();
    const bucketKey = Math.floor(ts / bucketMs) * bucketMs;
    buckets.set(bucketKey, (buckets.get(bucketKey) || 0) + 1);
  }

  // Convert to VolumeDataPoint array
  const volumeData: VolumeDataPoint[] = [];
  const sortedKeys = Array.from(buckets.keys()).sort((a, b) => a - b);

  // Use all data for client-side filtering
  for (const key of sortedKeys) {
    const count = buckets.get(key) || 0;
    // Convert count per 10 seconds to detections per second
    // Use integer math to avoid floating-point precision issues (e.g., 4.000000000000001)
    const detectionsPerSecond = Math.round((count / 10) * 10) / 10;
    volumeData.push({
      time: new Date(key).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
      value: detectionsPerSecond
    });
  }

  return volumeData;
}

/**
 * Generate latency history for performance chart
 */
function generateLatencyHistory(detections: AiDetectionHistoryEntryRaw[]): DetectionLatencyPoint[] {
  if (!detections || detections.length === 0) {
    return [];
  }

  return detections
    .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
    .map(d => ({
      time: new Date(d.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
      timestamp: new Date(d.timestamp).getTime(),
      latency: (d.metadata?.latency_ms as number) || 0,
      candidates: (d.metadata?.candidate_count as number) || 1,
      confidence: d.confidence
    }));
}

/**
 * Calculate data window information from detection timestamps
 * Shows users how many items are in the sample and what time range they cover
 */
function calculateDataWindow(detections: AiDetectionHistoryEntryRaw[]): DataWindow {
  if (!detections || detections.length === 0) {
    return {
      itemCount: 0,
      timeRangeLabel: "No data available"
    };
  }

  const itemCount = detections.length;

  // Sort by timestamp to find oldest and newest
  const sorted = [...detections].sort((a, b) =>
    new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
  );

  const oldestTimestamp = sorted[0].timestamp;
  const newestTimestamp = sorted[sorted.length - 1].timestamp;

  const oldestTime = new Date(oldestTimestamp).getTime();
  const newestTime = new Date(newestTimestamp).getTime();
  const diffMs = newestTime - oldestTime;

  // Format the time range
  const timeRangeLabel = formatTimeRange(diffMs);

  return {
    itemCount,
    timeRangeLabel,
    oldestTimestamp,
    newestTimestamp
  };
}

/**
 * Format milliseconds into a human-readable time range
 */
function formatTimeRange(diffMs: number): string {
  if (diffMs <= 0) return "Current moment";

  const seconds = Math.floor(diffMs / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (days > 0) {
    const remainingHours = hours % 24;
    if (remainingHours > 0) {
      return `Last ${days}d ${remainingHours}h`;
    }
    return `Last ${days} day${days > 1 ? "s" : ""}`;
  }

  if (hours > 0) {
    const remainingMinutes = minutes % 60;
    if (remainingMinutes > 0) {
      return `Last ${hours}h ${remainingMinutes}m`;
    }
    return `Last ${hours} hour${hours > 1 ? "s" : ""}`;
  }

  if (minutes > 0) {
    return `Last ${minutes} minute${minutes > 1 ? "s" : ""}`;
  }

  return `Last ${seconds} second${seconds > 1 ? "s" : ""}`;
}
