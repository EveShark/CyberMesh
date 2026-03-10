import type { AIEngineData, LoopStatusData, DetectionLoopMetrics, SuspiciousEntitiesData, DetectionStreamData, EnginePerformance, SentinelPerformance, SuspiciousEntityType } from "@/types/ai-engine";
import type { DashboardAIRaw, AiSuspiciousNodeRaw, AiDetectionHistoryEntryRaw, AiEngineMetricRaw, AiSentinelMetricRaw } from "../types/raw";
import { formatSeconds, formatPercent, getSeverityFromScore, truncateHash } from "./utils";
import { getNodeName } from "@/config/validator-names";

export function adaptAIEngine(raw?: DashboardAIRaw): AIEngineData {
  if (!raw) {
    return {
      loopStatus: {
        status: "Unknown",
        statusMessage: "No data available",
        detectionsPerMin: null,
        publishSuccess: null,
        publishToCandidateRatio: null,
        lastIteration: null,
        sinceLastIteration: null,
      },
      detectionLoop: {
        status: "Unknown",
        interval: null,
        avgLatency: null,
        lastLatency: null,
        sinceDetection: null,
        cacheAge: null,
        health: "Unknown",
        targetLatency: 100,
        iterationsCount: 0
      },
      suspiciousEntities: { entities: [], networkStatus: "Unknown" },
      detectionStream: { events: [], totalCount: 0, publishedCount: 0, heldForReview: 0 },
      engines: [],
      sentinel: null,
      updatedAt: new Date().toISOString()
    };
  }

  const loop = raw.metrics?.loop;
  const derived = raw.metrics?.derived;
  const counters = loop?.counters;

  // Calculate publish to candidate ratio from counters
  const publishToCandidateRatio = counters && counters.detections_total > 0
    ? (counters.detections_published / counters.detections_total) * 100
    : null;

  const interval = loop?.configured_interval_seconds != null
    ? `~${loop.configured_interval_seconds.toFixed(0)}s`
    : (derived?.iterations_per_minute
      ? `~${(60 / derived.iterations_per_minute).toFixed(1)}s`
      : null);

  return {
    loopStatus: {
      status: loop?.running ? "Running" : (loop?.status === "running" ? "Running" : "Stopped"),
      statusMessage: loop?.message || (loop?.running ? "Publishing detections" : "Detection paused"),
      detectionsPerMin: derived?.detections_per_minute != null
        ? Math.round(derived.detections_per_minute * 100) / 100
        : null,
      publishSuccess: derived?.publish_success_ratio != null
        ? Math.round(derived.publish_success_ratio * 100)
        : (counters?.detections_published && counters?.detections_total
          ? Math.round((counters.detections_published / counters.detections_total) * 100)
          : null),
      publishToCandidateRatio: publishToCandidateRatio != null
        ? Math.round(publishToCandidateRatio * 10) / 10
        : null,
      lastIteration: formatSeconds(loop?.seconds_since_last_iteration),
      sinceLastIteration: formatSeconds(loop?.seconds_since_last_detection),
    },
    detectionLoop: {
      status: loop?.running ? "Running" : (loop?.status === "running" ? "Running" : "Stopped"),
      interval: interval,
      avgLatency: loop?.avg_latency_ms != null
        ? Math.round(loop.avg_latency_ms * 100) / 100
        : null,
      lastLatency: loop?.last_latency_ms != null
        ? Math.round(loop.last_latency_ms * 100) / 100
        : null,
      sinceDetection: formatSeconds(loop?.seconds_since_last_detection),
      cacheAge: formatSeconds(loop?.cache_age_seconds),
      health: loop?.healthy !== false ? "Healthy" : "Degraded",
      targetLatency: 100,
      iterationsCount: counters?.loop_iterations ?? 0,
    },
    suspiciousEntities: adaptSuspiciousEntities(raw.suspicious?.nodes ?? []),
    detectionStream: adaptDetectionStream(raw.history?.detections ?? []),
    engines: adaptEngines(raw.metrics?.engines ?? []),
    sentinel: adaptSentinel(raw.metrics?.sentinel),
    updatedAt: new Date().toISOString(),
  };
}

function adaptSuspiciousEntities(nodes: AiSuspiciousNodeRaw[]): SuspiciousEntitiesData {
  const entities = nodes.map((node) => {
    // Suspicion score from backend could be 0-1 or already 0-100
    // Normalize to 0-100 range for display
    const rawScore = node.suspicion_score;
    const normalizedScore = rawScore > 1 ? Math.min(rawScore, 100) : rawScore * 100;
    const entityType = normalizeEntityType(node.entity_type);

    return {
      name: getEntityName(node.id, entityType),
      events: node.event_count,
      maxSeverity: node.suspicion_score > 80 ? 100 : Math.round(node.suspicion_score),
      lastSeen: node.last_seen,
      hash: truncateHash(node.id) || "",
      fullHash: node.id,
      score: Math.round(normalizedScore * 100) / 100, // Round to 2 decimal places
      threats: (node.threat_types ?? []).map(t => t.toUpperCase()), // Capitalize: ddos -> DDOS
      severity: getSeverityFromScore(rawScore > 1 ? rawScore / 100 : rawScore),
      entityType,
      kindLabel: entityTypeLabel(entityType),
    };
  });

  return {
    entities,
    networkStatus: entities.length === 0
      ? "All clear"
      : `${entities.length} suspicious ${entities.length > 1 ? "entities" : "entity"} flagged`,
  };
}

function normalizeEntityType(value?: string): SuspiciousEntityType {
  switch ((value || "").toLowerCase()) {
    case "validator":
      return "validator";
    case "network_source":
      return "network_source";
    case "network_target":
      return "network_target";
    default:
      return "unknown";
  }
}

function entityTypeLabel(entityType: SuspiciousEntityType): string {
  switch (entityType) {
    case "validator":
      return "Validator";
    case "network_source":
      return "Source";
    case "network_target":
      return "Target";
    default:
      return "Entity";
  }
}

function getEntityName(id: string, entityType: SuspiciousEntityType): string {
  if (entityType === "validator") {
    return getNodeName(id);
  }
  const value = id.includes(":") ? id.split(":").slice(1).join(":") : id;
  return value || id;
}

function adaptDetectionStream(detections: AiDetectionHistoryEntryRaw[]): DetectionStreamData {
  return {
    events: detections.map((d, i) => ({
      id: i,
      threatType: d.threat_type,
      confidence: Math.round(d.confidence * 100),
      timestamp: d.timestamp,
      finalScore: Math.round(d.final_score * 100),
      decision: d.should_publish ? "Published" : "Held",
      metadata: d.metadata || {}
    })),
    totalCount: detections.length,
    publishedCount: detections.filter(d => d.should_publish).length,
    heldForReview: detections.filter(d => !d.should_publish).length,
  };
}

function adaptEngines(engines: AiEngineMetricRaw[]): EnginePerformance[] {
  return engines.map(e => ({
    name: e.engine,
    status: e.ready ? "Healthy" : "Warning",
    throughput: e.throughput_per_minute.toFixed(1) + "/min",
    publishRate: formatPercent(e.publish_ratio) || "0%",
    avgLatency: e.last_latency_ms != null
      ? Math.round(e.last_latency_ms * 10) / 10 + "ms"
      : "--",
    confidence: e.avg_confidence != null
      ? formatPercent(e.avg_confidence) || "--"
      : "--",
    published: e.published
  }));
}

function adaptSentinel(raw?: AiSentinelMetricRaw): SentinelPerformance | null {
  if (!raw) {
    return null;
  }

  const entityType = normalizeEntityType(raw.last_entity_type);
  const entityLabel = raw.last_entity_type ? entityTypeLabel(entityType) : null;
  const entityName = raw.last_entity_id ? getEntityName(raw.last_entity_id, entityType) : null;

  let status: SentinelPerformance["status"] = "Idle";
  if ((raw.detections_total ?? 0) > 0) {
    status = "Active";
  } else if ((raw.events_total ?? 0) > 0) {
    status = "Observing";
  }

  return {
    status,
    eventsTotal: raw.events_total ?? 0,
    detectionsTotal: raw.detections_total ?? 0,
    publishRate: formatPercent(raw.publish_ratio) || "0%",
    topThreat: raw.top_threat_type ? raw.top_threat_type.toUpperCase() : null,
    lastEntity: entityName,
    entityLabel,
    lastDetection: formatUnixSeconds(raw.last_detection_time),
  };
}

function formatUnixSeconds(timestamp?: number): string | null {
  if (timestamp == null || !Number.isFinite(timestamp)) {
    return null;
  }

  const date = new Date(timestamp * 1000);
  if (Number.isNaN(date.getTime())) {
    return null;
  }

  return date.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}
