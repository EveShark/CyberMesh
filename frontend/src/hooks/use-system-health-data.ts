"use client"

import { useMemo } from "react"

import type {
  BackendReady,
  BackendHealth,
  StatsSummary,
  PrometheusSample,
  AiMetricsResponse,
  DashboardRuntimeMetrics,
  DashboardBackendDerived,
} from "@/lib/api"

import { useDashboardData } from "./use-dashboard-data"

interface BackendMetricsSummary {
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

interface SystemHealthApiResponse {
  timestamp: number
  backend: {
    health: BackendHealth
    readiness: BackendReady
    stats: StatsSummary
    metrics: {
      summary: BackendMetricsSummary
      samples: PrometheusSample[]
    }
    derived?: {
      mempoolLatencyMs?: number
      consensusLatencyMs?: number
      p2pLatencyMs?: number
      aiLoopStatus?: string
      aiLoopStatusUpdatedAt?: number
      aiLoopBlocking?: boolean
      aiLoopIssues?: string
      threatDataSource?: string
      threatSourceUpdatedAt?: number
      threatFallbackCount?: number
      threatFallbackReason?: string
      threatFallbackAt?: number
    }
  }
  ai: {
    health: Record<string, unknown> | null
    ready: Record<string, unknown> | null
    detectionStats?: AiMetricsResponse | null
    derived?: {
      detectionLatencyMs?: number
    }
  }
}

interface InfrastructurePoint {
  timestamp: number
  cpuPercent?: number
  residentMemoryBytes?: number
  heapAllocBytes?: number
  networkBytesSent?: number
  networkBytesReceived?: number
  mempoolSizeBytes?: number
}

const DEFAULT_SYSTEM_HEALTH_REFRESH_MS = 15000

const normalizeMetricsSummary = (summary?: DashboardRuntimeMetrics): BackendMetricsSummary => {
  if (!summary) {
    return {}
  }
  return {
    cpuSecondsTotal: summary.cpu_seconds_total,
    residentMemoryBytes: summary.resident_memory_bytes,
    virtualMemoryBytes: summary.virtual_memory_bytes,
    heapAllocBytes: summary.heap_alloc_bytes,
    heapSysBytes: summary.heap_sys_bytes,
    goroutines: summary.goroutines,
    threads: summary.threads,
    gcPauseSeconds: summary.gc_pause_seconds,
    gcCount: summary.gc_count,
    processStartTimeSeconds: summary.process_start_time_seconds,
  }
}

const buildDerivedMetrics = (
  stats: StatsSummary | undefined,
  provided?: DashboardBackendDerived,
): SystemHealthApiResponse["backend"]["derived"] => {
  const mempoolSeconds = stats?.mempool?.oldest_tx_age_seconds
  const mempoolMs = stats?.mempool?.oldest_tx_age_ms
  const consensusSeconds = stats?.chain?.avg_block_time_seconds
  const p2pLatencyMs = stats?.network?.avg_latency_ms

  return {
    mempoolLatencyMs:
      provided?.mempool_latency_ms ??
      (typeof mempoolMs === "number"
        ? mempoolMs
        : typeof mempoolSeconds === "number"
        ? mempoolSeconds * 1000
        : undefined),
    consensusLatencyMs:
      provided?.consensus_latency_ms ??
      (typeof consensusSeconds === "number" ? consensusSeconds * 1000 : undefined),
    p2pLatencyMs: provided?.p2p_latency_ms ?? p2pLatencyMs,
    aiLoopStatus: provided?.ai_loop_status,
    aiLoopStatusUpdatedAt: provided?.ai_loop_status_updated_at,
    aiLoopBlocking: provided?.ai_loop_blocking,
    aiLoopIssues: provided?.ai_loop_issues,
    threatDataSource: provided?.threat_data_source,
    threatSourceUpdatedAt: provided?.threat_source_updated_at,
    threatFallbackCount: provided?.threat_fallback_count,
    threatFallbackReason: provided?.threat_fallback_reason,
    threatFallbackAt: provided?.threat_fallback_at,
  }
}

export function useSystemHealthData(refreshInterval = DEFAULT_SYSTEM_HEALTH_REFRESH_MS) {
  const { data: dashboard, error, isLoading: dashboardLoading, mutate } = useDashboardData(refreshInterval)

  const data: SystemHealthApiResponse | undefined = useMemo(() => {
    if (!dashboard) {
      return undefined
    }

    const summary = normalizeMetricsSummary(dashboard.backend.metrics.summary)
    const derived = buildDerivedMetrics(dashboard.backend.stats, dashboard.backend.derived)
    const detectionLatencyMs = dashboard.ai.metrics?.loop?.avg_latency_ms

    return {
      timestamp: dashboard.timestamp,
      backend: {
        health: dashboard.backend.health,
        readiness: dashboard.backend.readiness,
        stats: dashboard.backend.stats,
        metrics: {
          summary,
          samples: [],
        },
        derived,
      },
      ai: {
        health: (dashboard.ai.health as Record<string, unknown> | undefined) ?? null,
        ready: (dashboard.ai.ready as Record<string, unknown> | undefined) ?? null,
        detectionStats: dashboard.ai.metrics,
        derived: {
          detectionLatencyMs: typeof detectionLatencyMs === "number" ? detectionLatencyMs : undefined,
        },
      },
    }
  }, [dashboard])

  const history = useMemo<InfrastructurePoint[]>(() => {
    const samples = dashboard?.backend?.history
    if (!samples || samples.length === 0) return []

    return samples.map((sample) => ({
      timestamp: sample.timestamp,
      cpuPercent: sample.cpu_percent,
      residentMemoryBytes: sample.resident_memory_bytes,
      heapAllocBytes: sample.heap_alloc_bytes,
      networkBytesSent: sample.network_bytes_sent,
      networkBytesReceived: sample.network_bytes_received,
      mempoolSizeBytes: sample.mempool_size_bytes,
    }))
  }, [dashboard?.backend?.history])

  const latestHistory = history[history.length - 1]

  const keyMetrics = useMemo(() => {
    if (!data) {
      return {
        uptimeSeconds: undefined,
        aiUptimeSeconds: undefined,
        cpuPercent: undefined,
        residentMemoryBytes: undefined,
        heapAllocBytes: undefined,
        mempoolBytes: undefined,
      }
    }

    const uptimeSeconds = (() => {
      const processStart = data.backend.metrics.summary.processStartTimeSeconds
      if (!processStart) return undefined
      const nowSeconds = Math.floor(data.timestamp / 1000)
      return nowSeconds - processStart
    })()

    const aiUptimeSeconds = (() => {
      const uptime = data.ai.health && typeof data.ai.health === "object" ? (data.ai.health as Record<string, unknown>).uptime_seconds : undefined
      return typeof uptime === "number" ? uptime : undefined
    })()

    return {
      uptimeSeconds,
      aiUptimeSeconds,
      cpuPercent: latestHistory?.cpuPercent,
      residentMemoryBytes: latestHistory?.residentMemoryBytes,
      heapAllocBytes: latestHistory?.heapAllocBytes,
      mempoolBytes: latestHistory?.mempoolSizeBytes,
    }
  }, [data, latestHistory])

  const backendLatencyMetrics = useMemo(() => {
    const derived = data?.backend.derived ?? {}

    return {
      storageLatencyMs: undefined,
      stateLatencyMs: undefined,
      mempoolLatencyMs:
        derived?.mempoolLatencyMs ??
        (typeof data?.backend.stats.mempool?.oldest_tx_age_seconds === "number"
          ? data.backend.stats.mempool.oldest_tx_age_seconds * 1000
          : undefined),
      consensusLatencyMs:
        derived?.consensusLatencyMs ??
        (typeof data?.backend.stats.chain?.avg_block_time_seconds === "number"
          ? data.backend.stats.chain.avg_block_time_seconds * 1000
          : undefined),
      p2pLatencyMs:
        derived?.p2pLatencyMs ??
        (typeof data?.backend.stats.network?.avg_latency_ms === "number"
          ? data.backend.stats.network?.avg_latency_ms
          : undefined),
    }
  }, [data?.backend])

  const aiLatencyMs = useMemo(() => {
    const derived = data?.ai.derived?.detectionLatencyMs
    if (typeof derived === "number" && Number.isFinite(derived)) return derived
    const loopLatency = data?.ai.detectionStats?.loop?.avg_latency_ms
    return typeof loopLatency === "number" && Number.isFinite(loopLatency) ? loopLatency : undefined
  }, [data?.ai])

  return {
    data,
    error,
    isLoading: !data && dashboardLoading,
    mutate,
    keyMetrics,
    history,
    backendLatencyMetrics,
    aiLatencyMs,
  }
}