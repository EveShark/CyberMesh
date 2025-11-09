"use client"

import { useMemo } from "react"

import type { BackendReady, BackendHealth, StatsSummary, NetworkStatsSummary, BackendNetworkOverview } from "@/lib/api"
import type { NetworkOverview } from "@/p2p-consensus/lib/types"
import type { ServiceStatusDescriptor, ServiceStatus } from "@/components/service-status-grid"

import { useDashboardData } from "./use-dashboard-data"

const DEFAULT_OVERVIEW_REFRESH_MS = 15000

type ReadinessValue = import("@/lib/api").ReadinessCheck | string | undefined

const toTitle = (value: string) =>
  value
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase())

const mapNetworkNodes = (nodes: BackendNetworkOverview["nodes"]): NetworkOverview["nodes"] =>
  nodes.map((node) => ({
    id: node.id,
    name: node.name,
    status: node.status,
    latency: node.latency,
    uptime: node.uptime,
    throughputBytes: node.throughput_bytes,
    lastSeen: node.last_seen ?? null,
    inboundRateBps: node.inbound_rate_bps,
  }))

const mapVotingStatus = (entries: BackendNetworkOverview["voting_status"]): NetworkOverview["votingStatus"] =>
  entries.map((entry) => ({
    nodeId: entry.node_id,
    voting: entry.voting,
  }))

const mapNetworkEdges = (edges: BackendNetworkOverview["edges"]): NetworkOverview["edges"] => {
  if (!edges || edges.length === 0) return []
  return edges.map((edge) => ({
    source: edge.source,
    target: edge.target,
    direction: edge.direction,
    status: edge.status,
    confidence: edge.confidence,
    reportedBy: edge.reported_by,
    updatedAt: edge.updated_at,
  }))
}

const toNetworkOverview = (source: BackendNetworkOverview): NetworkOverview => ({
  connectedPeers: source.connected_peers,
  totalPeers: source.total_peers,
  expectedPeers: source.expected_peers ?? source.nodes.length,
  averageLatencyMs: source.average_latency_ms,
  consensusRound: source.consensus_round,
  leaderStability: source.leader_stability,
  phase: source.phase,
  leader: source.leader ?? null,
  leaderId: source.leader_id ?? null,
  nodes: mapNetworkNodes(source.nodes),
  votingStatus: mapVotingStatus(source.voting_status),
  edges: mapNetworkEdges(source.edges),
  selfId: source.self ?? null,
  inboundRateBps: source.inbound_rate_bps,
  outboundRateBps: source.outbound_rate_bps,
  updatedAt: source.updated_at,
})

function extractStatus(value: ReadinessValue): string | undefined {
  if (!value) return undefined
  if (typeof value === "string") return value
  return value.status
}

function mapStatus(value?: ReadinessValue): ServiceStatus {
  const status = extractStatus(value)
  switch (status) {
    case "ok":
      return "healthy"
    case "single_node":
    case "genesis":
      return "warning"
    case "not configured":
      return "maintenance"
    case "not ready":
    case "insufficient":
    case "no_peers":
      return "warning"
    case "error":
    case "failed":
      return "critical"
    default:
      return status === "ok" ? "healthy" : "warning"
  }
}

export function useOverviewData(refreshInterval = DEFAULT_OVERVIEW_REFRESH_MS) {
  const { data: dashboard, error, isLoading, mutate } = useDashboardData(refreshInterval)

  const readiness = dashboard?.backend.readiness
  const backendMetrics = dashboard?.backend.metrics
  const stats = dashboard?.backend.stats

  const services: ServiceStatusDescriptor[] = useMemo(() => {
    if (!readiness) return []

    const checks = readiness.checks ?? {}
    const details = (readiness as BackendReady & { details?: Record<string, unknown> }).details ?? {}
    const lastCheck = readiness.timestamp
      ? new Date(readiness.timestamp * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
      : undefined

    return Object.keys(checks).map((key) => {
      const detailKey = `${key}_error`
      const detailValue = typeof details[detailKey] === "string" ? (details[detailKey] as string) : undefined

      return {
        name: toTitle(key),
        status: mapStatus(checks[key]),
        lastCheck,
        details: detailValue,
      }
    })
  }, [readiness])

  const uptimeSeconds = useMemo(() => {
    if (!dashboard?.backend.metrics.summary.process_start_time_seconds) return undefined
    const processStart = dashboard.backend.metrics.summary.process_start_time_seconds
    return Math.max(0, Math.floor(dashboard.timestamp / 1000) - Math.floor(processStart))
  }, [dashboard])

  const backendSummary = useMemo(() => {
    if (!dashboard) {
      return {
        readiness: undefined,
        services: [] as ServiceStatusDescriptor[],
        uptimeSeconds: undefined,
        status: undefined as string | undefined,
        health: undefined as BackendHealth | undefined,
        stats: undefined as StatsSummary | undefined,
      }
    }

    return {
      readiness,
      services,
      uptimeSeconds,
      status: readiness?.ready ? "ready" : "not ready",
      health: dashboard.backend.health as BackendHealth,
      stats: stats as StatsSummary,
    }
  }, [dashboard, readiness, services, stats, uptimeSeconds])

  const metricsSummary = useMemo(() => {
    if (!dashboard) {
      return {
        lastUpdated: undefined,
        cpuSeconds: undefined,
        residentMemoryBytes: undefined,
        requestErrors: undefined,
        requestTotal: undefined,
        kafkaPublishSuccessTotal: undefined,
        kafkaPublishFailureTotal: undefined,
        kafkaBrokerCount: undefined,
        redisPoolHits: undefined,
        redisPoolMisses: undefined,
        redisPoolTotalConnections: undefined,
        cockroachOpenConnections: undefined,
        readinessChecks: {},
      }
    }

    return {
      lastUpdated: dashboard.timestamp,
      cpuSeconds: backendMetrics?.summary.cpu_seconds_total,
      residentMemoryBytes: backendMetrics?.summary.resident_memory_bytes,
      requestErrors: backendMetrics?.requests.errors,
      requestTotal: backendMetrics?.requests.total,
      kafkaPublishSuccessTotal: backendMetrics?.kafka.publish_success,
      kafkaPublishFailureTotal: backendMetrics?.kafka.publish_failure,
      kafkaBrokerCount: backendMetrics?.kafka.broker_count,
      redisPoolHits: backendMetrics?.redis.pool_hits,
      redisPoolMisses: backendMetrics?.redis.pool_misses,
      redisPoolTotalConnections: backendMetrics?.redis.total_connections,
      cockroachOpenConnections: backendMetrics?.cockroach.open_connections,
      readinessChecks: readiness?.checks ?? {},
    }
  }, [backendMetrics, dashboard, readiness?.checks])

  const network = useMemo(() => {
    if (!dashboard?.network) return undefined
    return toNetworkOverview(dashboard.network)
  }, [dashboard?.network])

  const networkStatsDetail = stats?.network as NetworkStatsSummary | undefined

  const aiSummary = useMemo(() => {
    const metrics = dashboard?.ai.metrics
    const loop = metrics?.loop
    const derived = metrics?.derived
    const totals = dashboard?.threats.breakdown.totals
    const severity = dashboard?.threats.breakdown.severity ?? {
      critical: 0,
      high: 0,
      medium: 0,
      low: 0,
    }

    return {
      status: loop?.running ? "running" : "paused",
      metrics: {
        avg_latency_ms: loop?.avg_latency_ms,
        last_latency_ms: loop?.last_latency_ms,
        detections_per_minute: derived?.detections_per_minute,
        publish_rate_per_minute: derived?.publish_rate_per_minute,
        iterations_per_minute: derived?.iterations_per_minute,
        error_rate_per_hour: derived?.error_rate_per_hour,
        publish_success_ratio: derived?.publish_success_ratio,
        seconds_since_last_detection: loop?.seconds_since_last_detection,
        seconds_since_last_iteration: loop?.seconds_since_last_iteration,
        cache_age_seconds: loop?.cache_age_seconds,
      },
      samples: [],
      detectionsPublished: totals?.published ?? 0,
      detectionsTotal: totals?.overall ?? 0,
      severityBreakdown: severity as Record<"critical" | "high" | "medium" | "low", number>,
      threatTypes: dashboard?.threats.breakdown.threat_types ?? [],
      uptimeSeconds: undefined,
      checks: dashboard?.ai.ready ?? {},
      lastUpdated: dashboard?.threats.timestamp,
    }
  }, [dashboard])

  const readinessKpi = useMemo(() => {
    if (!readiness) {
      return {
        status: "not_ready",
        passing: 0,
        total: 0,
        lastCheckedMs: undefined,
      }
    }

    const checks = readiness.checks ?? {}
    const healthyStatuses = new Set(["ok", "healthy", "single_node", "genesis"])
    const passing = Object.values(checks).reduce((count, value) => {
      const status = typeof value === "string" ? value : value?.status
      if (status && healthyStatuses.has(status)) {
        return count + 1
      }
      return count
    }, 0)

    return {
      status: readiness.ready ? "ready" : "not_ready",
      passing,
      total: Object.keys(checks).length,
      lastCheckedMs: readiness.timestamp ? readiness.timestamp * 1000 : undefined,
    }
  }, [readiness])

  const aiKpi = useMemo(() => {
    const metrics = dashboard?.ai.metrics
    const loop = metrics?.loop
    const derived = metrics?.derived

    return {
      status: loop?.running ? "running" : "paused",
      publishRate: typeof derived?.publish_rate_per_minute === "number" ? derived.publish_rate_per_minute : undefined,
      detectionsRate: typeof derived?.detections_per_minute === "number" ? derived.detections_per_minute : undefined,
      avgLatencyMs: typeof loop?.avg_latency_ms === "number" ? loop.avg_latency_ms : undefined,
      lastLatencyMs: typeof loop?.last_latency_ms === "number" ? loop.last_latency_ms : undefined,
      lastUpdatedMs: aiSummary.lastUpdated ? aiSummary.lastUpdated : undefined,
    }
  }, [aiSummary.lastUpdated, dashboard?.ai.metrics])

  const networkKpi = useMemo(() => {
    const history = networkStatsDetail?.history ?? []
    const latest = history.length > 0 ? history[history.length - 1] : undefined
    const previous = history.length > 1 ? history[history.length - 2] : undefined
    const latencyDelta = latest && previous ? latest.avgLatencyMs - previous.avgLatencyMs : undefined

    return {
      peerCount: networkStatsDetail?.peer_count,
      totalPeers: networkStatsDetail?.peer_count,
      avgLatencyMs: latest?.avgLatencyMs,
      latencyDelta,
    }
  }, [networkStatsDetail])

  const apiKpi = useMemo(() => {
    const total = metricsSummary.requestTotal ?? 0
    const errorsCount = metricsSummary.requestErrors ?? 0
    const errorRate = total > 0 ? (errorsCount / total) * 100 : 0

    return {
      totalRequests: total,
      errorRate,
    }
  }, [metricsSummary.requestErrors, metricsSummary.requestTotal])

  const trendStats = useMemo(() => {
    const detectionsRate = aiKpi.detectionsRate
    const publishRate = aiKpi.publishRate
    const latencyDelta = networkKpi.latencyDelta

    const blocks = dashboard?.blocks.recent ?? []
    let blocksPerHour: number | undefined
    if (blocks.length > 1) {
      const sorted = [...blocks].sort((a, b) => a.timestamp - b.timestamp)
      const first = sorted[0]
      const last = sorted[sorted.length - 1]
      const durationSeconds = Math.max(1, last.timestamp - first.timestamp)
      blocksPerHour = (sorted.length / durationSeconds) * 3600
    }

    return {
      detectionsRate,
      publishRate,
      latencyDelta,
      blocksPerHour,
    }
  }, [aiKpi.detectionsRate, aiKpi.publishRate, dashboard?.blocks.recent, networkKpi.latencyDelta])

  const alertsSummary = useMemo(() => {
    const severity = dashboard?.threats.breakdown.severity ?? {
      critical: 0,
      high: 0,
      medium: 0,
      low: 0,
    }
    const totals = dashboard?.threats.breakdown.totals

    return {
      critical: severity.critical ?? 0,
      high: severity.high ?? 0,
      medium: severity.medium ?? 0,
      low: severity.low ?? 0,
      published: totals?.published ?? 0,
      totalDetections: totals?.overall ?? 0,
      lastUpdated: dashboard?.threats.timestamp,
    }
  }, [dashboard])

  const networkSummary = useMemo(() => {
    if (!network) return undefined

    const mappedNodes = network.nodes.slice(0, 12).map((node) => ({
      id: node.id,
      name: node.name,
      status: node.status,
      latency: node.latency,
      uptime: node.uptime,
      lastSeen: node.lastSeen,
      throughputBytes: node.throughputBytes,
    }))

    return {
      nodes: mappedNodes,
      peerCount: network.connectedPeers,
      totalPeers: network.totalPeers,
      consensusRound: network.consensusRound,
      leaderStability: network.leaderStability,
      averageLatencyMs: network.averageLatencyMs,
      updatedAt: network.updatedAt,
    }
  }, [network])

  const latestBlocks = useMemo(() => {
    if (!dashboard?.blocks.recent) return []
    return [...dashboard.blocks.recent].sort((a, b) => b.height - a.height)
  }, [dashboard?.blocks.recent])

  const blockPagination = dashboard?.blocks.pagination

  const ledgerSummary = useMemo(() => {
    const base = dashboard?.ledger
    if (!base) return undefined

    const mempoolStats = stats?.mempool
    const pending = base.pending_transactions ?? mempoolStats?.pending_transactions
    const sizeBytes = base.mempool_size_bytes ?? mempoolStats?.size_bytes

    let oldestMs = base.mempool_oldest_tx_age_ms
    if (oldestMs === undefined && mempoolStats) {
      if (typeof mempoolStats.oldest_tx_age_ms === "number") {
        oldestMs = mempoolStats.oldest_tx_age_ms
      } else if (typeof mempoolStats.oldest_tx_age_seconds === "number") {
        oldestMs = mempoolStats.oldest_tx_age_seconds * 1000
      }
    }

    return {
      ...base,
      pending_transactions: pending ?? 0,
      mempool_size_bytes: sizeBytes ?? 0,
      mempool_oldest_tx_age_ms: oldestMs,
    }
  }, [dashboard?.ledger, stats?.mempool])

  const refresh = async () => {
    await mutate()
  }

  return {
    isLoading: !dashboard && isLoading,
    error,
    data: {
      backend: backendSummary,
      ledger: ledgerSummary,
    },
    metrics: metricsSummary,
    network: networkSummary,
    ai: {
      detectionsTotal: aiSummary.detectionsTotal,
      severityBreakdown: aiSummary.severityBreakdown,
      status: aiSummary.status,
      lastUpdated: aiSummary.lastUpdated,
    },
    latestBlocks,
    blockPagination,
    overviewKpis: {
      readiness: readinessKpi,
      ai: aiKpi,
      network: networkKpi,
      api: apiKpi,
    },
    trendStats,
    alerts: alertsSummary,
    refresh,
  }
}