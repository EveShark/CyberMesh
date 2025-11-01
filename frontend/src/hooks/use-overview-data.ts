"use client"

import { useMemo, useState, useEffect } from "react"
import useSWR from "swr"

import type {
  BackendReady,
  BackendHealth,
  StatsSummary,
  BlockSummary,
  PrometheusSample,
  AiMetricsResponse,
  NetworkStatsSummary,
  BackendNetworkOverview,
} from "@/lib/api"
import type { NetworkOverview } from "@/p2p-consensus/lib/types"
import type { ServiceStatusDescriptor, ServiceStatus } from "@/components/service-status-grid"
import { BlockStore } from "@/lib/block-store"

interface SystemHealthResponse {
  timestamp: number
  backend: {
    health: BackendHealth
    readiness: BackendReady
    stats: StatsSummary
    metrics: {
      summary: {
        cpuSecondsTotal?: number
        residentMemoryBytes?: number
        virtualMemoryBytes?: number
        heapAllocBytes?: number
        heapSysBytes?: number
        goroutines?: number
        threads?: number
        gcPauseSeconds?: number
        gcCount?: number
        processStartTimeSeconds?: number
      }
      samples: PrometheusSample[]
    }
  }
  ai: {
    health: Record<string, unknown> | null
    ready: Record<string, unknown> | null
  }
}

interface ThreatsResponse {
  timestamp: number
  detectionLoop: {
    running: boolean
    metrics?: Record<string, number | string>
  } | null
  breakdown: {
    threatTypes: Array<{
      threatType: string
      published: number
      abstained: number
      total: number
      severity: "critical" | "high" | "medium" | "low"
    }>
    severity: Record<"critical" | "high" | "medium" | "low", number>
    totals: {
      published: number
      abstained: number
      overall: number
    }
  }
  metrics: {
    samples: PrometheusSample[]
  }
}

interface BlocksResponse {
  blocks: BlockSummary[]
  pagination: {
    start: number
    limit: number
    total: number
    next?: string
  }
}

interface AiHealthResponse {
  health: Record<string, unknown>
  ready: Record<string, unknown>
}

interface AiMetricsRouteResponse {
  metrics: AiMetricsResponse
}

const fetcher = async <T>(url: string): Promise<T> => {
  const res = await fetch(url, { cache: "no-store" })
  if (!res.ok) {
    const text = await res.text().catch(() => "")
    throw new Error(`Request failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ""}`)
  }
  return (await res.json()) as T
}

type SummaryValue = number | undefined

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

function extractMetric(
  samples: PrometheusSample[] | undefined,
  metricName: string,
  labels: Record<string, string> = {},
): SummaryValue {
  if (!samples || samples.length === 0) {
    return undefined
  }

  for (const sample of samples) {
    if (sample.metric !== metricName) continue
    const matches = Object.entries(labels).every(([key, value]) => sample.labels[key] === value)
    if (matches) {
      return sample.value
    }
  }

  return undefined
}

type ReadinessValue = import("@/lib/api").ReadinessCheck | string | undefined

function extractStatus(value: ReadinessValue): string | undefined {
  if (!value) return undefined
  if (typeof value === "string") return value
  return value.status
}

function mapStatus(value?: ReadinessValue): ServiceStatus {
  const status = extractStatus(value)
  switch (status) {
    case "ok":
    case "single_node":
    case "genesis":
      return "healthy"
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

export function useOverviewData(refreshInterval = 5000) {
  const [blockStore] = useState(() => new BlockStore())
  const [localBlocks, setLocalBlocks] = useState<BlockSummary[]>([])

  const {
    data: systemHealth,
    error: systemHealthError,
    isLoading: systemHealthLoading,
    mutate: refreshSystemHealth,
  } = useSWR<SystemHealthResponse>('/api/system-health', fetcher, {
    refreshInterval,
  })

  const {
    data: threats,
    error: threatsError,
    isLoading: threatsLoading,
    mutate: refreshThreats,
  } = useSWR<ThreatsResponse>('/api/threats', fetcher, {
    refreshInterval,
  })

  const networkFetcher = async (): Promise<NetworkOverview> => {
    const response = await fetch("/api/network/overview", { cache: "no-store" })
    if (!response.ok) {
      const text = await response.text().catch(() => "")
      throw new Error(
        `Network overview request failed: ${response.status} ${response.statusText}${text ? ` - ${text}` : ""}`,
      )
    }
    const backendOverview = (await response.json()) as BackendNetworkOverview
    return toNetworkOverview(backendOverview)
  }

  const {
    data: network,
    error: networkError,
    isLoading: networkLoading,
    mutate: refreshNetwork,
  } = useSWR<NetworkOverview>("/api/network/overview", networkFetcher, {
    refreshInterval,
  })

  const latestChainHeight = systemHealth?.backend.stats?.chain?.height
  const blockLimit = 10
  const blockStart =
    typeof latestChainHeight === "number" && Number.isFinite(latestChainHeight)
      ? Math.max(Number(latestChainHeight) - (blockLimit - 1), 0)
      : 0
  const blocksKey = typeof latestChainHeight === "number" ? `/api/blocks?start=${blockStart}&limit=${blockLimit}` : `/api/blocks?start=0&limit=${blockLimit}`

  const {
    data: blocksData,
    error: blocksError,
    isLoading: blocksLoading,
    mutate: refreshBlocks,
  } = useSWR<BlocksResponse>(blocksKey, fetcher, {
    refreshInterval: 10000,
    dedupingInterval: 5000,
    revalidateOnFocus: false,
  })

  useEffect(() => {
    if (blocksData?.blocks && blocksData.blocks.length > 0) {
      blockStore.addBlocks(blocksData.blocks)
      setLocalBlocks(blockStore.getLatest(blockLimit))
    }
  }, [blockLimit, blockStore, blocksData])

  useEffect(() => {
    setLocalBlocks(blockStore.getLatest(10))
  }, [blockStore])

  const {
    data: aiMetrics,
    error: aiMetricsError,
    isLoading: aiMetricsLoading,
    mutate: refreshAiMetrics,
  } = useSWR<AiMetricsRouteResponse>('/api/ai/metrics', fetcher, {
    refreshInterval,
  })

  const {
    data: aiHealth,
    error: aiHealthError,
    isLoading: aiHealthLoading,
    mutate: refreshAiHealth,
  } = useSWR<AiHealthResponse>('/api/ai/health', fetcher, {
    refreshInterval,
  })

  const services: ServiceStatusDescriptor[] = useMemo(() => {
    const readiness = systemHealth?.backend.readiness
    if (!readiness) return []

    const checks = readiness.checks ?? {}
    const details = (systemHealth.backend.readiness as BackendReady & { details?: Record<string, unknown> }).details ?? {}
    const lastCheck = readiness.timestamp
      ? new Date(readiness.timestamp * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
      : undefined

    return Object.keys(checks).map((key) => {
      const detailKey = `${key}_error`
      const detailValue = typeof details[detailKey] === "string" ? (details[detailKey] as string) : undefined
      const name = key
        .replace(/_/g, " ")
        .replace(/\b\w/g, (letter) => letter.toUpperCase())

      return {
        name,
        status: mapStatus(checks[key]),
        lastCheck,
        details: detailValue,
      }
    })
  }, [systemHealth])

  const backendSummary = useMemo(() => {
    if (!systemHealth) {
      return {
        readiness: undefined,
        services: [],
        uptimeSeconds: undefined,
        status: undefined as string | undefined,
        health: undefined as BackendHealth | undefined,
        stats: undefined as StatsSummary | undefined,
      }
    }

    const processStart = systemHealth.backend.metrics.summary.processStartTimeSeconds
    const uptimeSeconds = processStart ? Math.max(0, Math.floor(systemHealth.timestamp / 1000) - processStart) : undefined
    const readinessStatus = systemHealth.backend.readiness.ready ? "ready" : "not ready"

    return {
      readiness: systemHealth.backend.readiness,
      services,
      uptimeSeconds,
      status: readinessStatus,
      health: systemHealth.backend.health,
      stats: systemHealth.backend.stats,
    }
  }, [services, systemHealth])

  const metricsSummary = useMemo(() => {
    const readinessChecks = systemHealth?.backend.readiness.checks ?? {}
    const samples = systemHealth?.backend.metrics.samples ?? []
    const summary = systemHealth?.backend.metrics.summary

    const cpuSample = summary?.cpuSecondsTotal
    const memorySample = summary?.residentMemoryBytes
    const totalRequests = extractMetric(samples, "cybermesh_api_requests_total")
    const errorRequests = extractMetric(samples, "cybermesh_api_request_errors_total")
    const kafkaPublishSuccessTotal = extractMetric(samples, "kafka_producer_publish_success_total")
    const kafkaPublishFailureTotal = extractMetric(samples, "kafka_producer_publish_failure_total")
    const kafkaBrokerCount = extractMetric(samples, "kafka_producer_brokers")
    const redisPoolHits = extractMetric(samples, "redis_pool_hits_total")
    const redisPoolMisses = extractMetric(samples, "redis_pool_misses_total")
    const redisPoolTotalConnections = extractMetric(samples, "redis_pool_total_connections")
    const cockroachOpenConnections = extractMetric(samples, "cockroach_pool_open_connections")

    return {
      lastUpdated: systemHealth?.timestamp,
      cpuSeconds: cpuSample,
      residentMemoryBytes: memorySample,
      requestErrors: errorRequests,
      requestTotal: totalRequests,
      kafkaPublishSuccessTotal,
      kafkaPublishFailureTotal,
      kafkaBrokerCount,
      redisPoolHits,
      redisPoolMisses,
      redisPoolTotalConnections,
      cockroachOpenConnections,
      readinessChecks,
    }
  }, [systemHealth])

  const aiSummary = useMemo(() => {
    const metricsPayload = aiMetrics?.metrics
    const loopSummary = metricsPayload?.loop
    const derived = metricsPayload?.derived

    const loopMetrics: Record<string, number | string | undefined> = {
      avg_latency_ms: loopSummary?.avg_latency_ms,
      last_latency_ms: loopSummary?.last_latency_ms,
      detections_per_minute: derived?.detections_per_minute,
      publish_rate_per_minute: derived?.publish_rate_per_minute,
      iterations_per_minute: derived?.iterations_per_minute,
      error_rate_per_hour: derived?.error_rate_per_hour,
      publish_success_ratio: derived?.publish_success_ratio,
      seconds_since_last_detection: loopSummary?.seconds_since_last_detection,
      seconds_since_last_iteration: loopSummary?.seconds_since_last_iteration,
      cache_age_seconds: loopSummary?.cache_age_seconds,
    }

    const detectionLoopRunning = loopSummary?.running ?? false
    const totals = threats?.breakdown.totals
    const severity = threats?.breakdown.severity
    const healthObject = (aiHealth?.health ?? {}) as Record<string, unknown>
    const readyObject = (aiHealth?.ready ?? {}) as Record<string, unknown>

    return {
      status: detectionLoopRunning ? "running" : "paused",
      metrics: loopMetrics,
      samples: [],
      detectionsPublished: totals?.published ?? 0,
      detectionsTotal: totals?.overall ?? 0,
      severityBreakdown: severity,
      threatTypes: threats?.breakdown.threatTypes ?? [],
      uptimeSeconds: typeof healthObject.uptime_seconds === "number" ? (healthObject.uptime_seconds as number) : undefined,
      checks: readyObject,
      lastUpdated: threats?.timestamp,
    }
  }, [aiHealth, aiMetrics, threats])

  const readinessKpi = useMemo(() => {
    const readiness = systemHealth?.backend.readiness
    const checks = readiness?.checks ?? {}
    const healthyStatuses = new Set(["ok", "healthy", "single_node", "genesis"])
    const passing = Object.values(checks).reduce((count, value) => {
      const status = typeof value === "string" ? value : value?.status
      if (status && healthyStatuses.has(status)) {
        return count + 1
      }
      return count
    }, 0)
    const total = Object.keys(checks).length
    const lastCheckedMs = readiness?.timestamp ? readiness.timestamp * 1000 : undefined

    return {
      status: readiness?.ready ? "ready" : "not_ready",
      passing,
      total,
      lastCheckedMs,
    }
  }, [systemHealth?.backend.readiness])

  const aiKpi = useMemo(() => {
    const metricsPayload = aiMetrics?.metrics
    const loopSummary = metricsPayload?.loop
    const derived = metricsPayload?.derived

    return {
      status: loopSummary?.running ? "running" : "paused",
      publishRate: typeof derived?.publish_rate_per_minute === "number" ? derived.publish_rate_per_minute : undefined,
      detectionsRate: typeof derived?.detections_per_minute === "number" ? derived.detections_per_minute : undefined,
      avgLatencyMs: typeof loopSummary?.avg_latency_ms === "number" ? loopSummary.avg_latency_ms : undefined,
      lastLatencyMs: typeof loopSummary?.last_latency_ms === "number" ? loopSummary.last_latency_ms : undefined,
      lastUpdatedMs: aiSummary.lastUpdated ? aiSummary.lastUpdated * 1000 : undefined,
    }
  }, [aiMetrics, aiSummary.lastUpdated])

  const networkStatsDetail = systemHealth?.backend.stats.network as NetworkStatsSummary | undefined

  const networkKpi = useMemo(() => {
    const peerCount = network?.connectedPeers
    const totalPeers = network?.totalPeers
    const avgLatencyMs = network?.averageLatencyMs
    const history = networkStatsDetail?.history ?? []
    let latencyDelta: number | undefined
    if (history.length >= 2) {
      const first = history[0]
      const last = history[history.length - 1]
      latencyDelta = last.avg_latency_ms - first.avg_latency_ms
    }

    return {
      peerCount,
      totalPeers,
      avgLatencyMs,
      latencyDelta,
    }
  }, [network?.averageLatencyMs, network?.connectedPeers, network?.totalPeers, networkStatsDetail?.history])

  const apiKpi = useMemo(() => {
    const totalRequests = metricsSummary.requestTotal ?? 0
    const errorRequests = metricsSummary.requestErrors ?? 0
    const errorRate = totalRequests > 0 ? (errorRequests / totalRequests) * 100 : undefined

    return {
      totalRequests,
      errorRate,
    }
  }, [metricsSummary.requestErrors, metricsSummary.requestTotal])

  const trendStats = useMemo(() => {
    const detectionsRate = aiKpi.detectionsRate
    const publishRate = aiKpi.publishRate
    const latencyDelta = networkKpi.latencyDelta

    const sortedBlocks = [...localBlocks].sort((a, b) => b.height - a.height)
    let blocksPerHour: number | undefined
    if (sortedBlocks.length >= 2) {
      const newest = sortedBlocks[0]
      const oldest = sortedBlocks[sortedBlocks.length - 1]
      const deltaSeconds = (newest.timestamp - oldest.timestamp) || 0
      const blockSpan = newest.height - oldest.height
      if (deltaSeconds > 0 && blockSpan > 0) {
        blocksPerHour = (blockSpan * 3600) / deltaSeconds
      }
    }

    return {
      detectionsRate,
      publishRate,
      latencyDelta,
      blocksPerHour,
    }
  }, [aiKpi.detectionsRate, aiKpi.publishRate, localBlocks, networkKpi.latencyDelta])

  const alertsSummary = useMemo(() => {
    const severity = threats?.breakdown.severity
    const totals = threats?.breakdown.totals
    return {
      critical: severity?.critical ?? 0,
      high: severity?.high ?? 0,
      medium: severity?.medium ?? 0,
      low: severity?.low ?? 0,
      totalDetections: totals?.overall ?? 0,
      published: totals?.published ?? 0,
      lastUpdated: threats?.timestamp ? threats.timestamp * 1000 : undefined,
    }
  }, [threats])

  const networkSummary = useMemo(() => {
    if (!network) return undefined
    const mappedNodes = (network.nodes ?? []).map((node) => {
      const status: "healthy" | "degraded" | "down" =
        node.status === "critical" ? "down" : node.status === "warning" ? "degraded" : "healthy"

      return {
        id: node.id,
        role: node.name ?? node.id,
        status,
        throughputBytes: node.throughputBytes,
      }
    })

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

  const latestBlocks = useMemo(() => localBlocks, [localBlocks])

  const anyLoading =
    systemHealthLoading ||
    threatsLoading ||
    networkLoading ||
    blocksLoading ||
    aiMetricsLoading ||
    aiHealthLoading

  const firstError =
    systemHealthError ||
    threatsError ||
    networkError ||
    blocksError ||
    aiMetricsError ||
    aiHealthError

  const refresh = async () => {
    await Promise.all([
      refreshSystemHealth(),
      refreshThreats(),
      refreshNetwork(),
      refreshBlocks(),
      refreshAiMetrics(),
      refreshAiHealth(),
    ])
  }

  return {
    isLoading: anyLoading,
    error: firstError,
    data: {
      backend: backendSummary,
    },
    metrics: metricsSummary,
    network: networkSummary,
    ai: aiSummary,
    latestBlocks,
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
