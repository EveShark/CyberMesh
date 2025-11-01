"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import useSWR from "swr"

import type { BackendReady, BackendHealth, StatsSummary, PrometheusSample, AiMetricsResponse } from "@/lib/api"

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

const fetcher = async <T>(url: string): Promise<T> => {
  const res = await fetch(url, { cache: "no-store" })
  if (!res.ok) {
    const text = await res.text().catch(() => "")
    throw new Error(`Request failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ""}`)
  }
  return (await res.json()) as T
}

export function useSystemHealthData(refreshInterval = 5000) {
  const { data, error, isLoading, mutate } = useSWR<SystemHealthApiResponse>('/api/system-health', fetcher, {
    refreshInterval,
  })

  const previousSampleRef = useRef<{
    timestamp: number
    cpuSecondsTotal?: number
    networkBytesSent?: number
    networkBytesReceived?: number
    mempoolSizeBytes?: number
  } | null>(null)

  const [history, setHistory] = useState<InfrastructurePoint[]>([])

  useEffect(() => {
    if (!data) return

    const now = data.timestamp
    const summary = data.backend.metrics.summary
    const stats = data.backend.stats

    const prev = previousSampleRef.current
    let cpuPercent: number | undefined
    let networkBytesSentDelta: number | undefined
    let networkBytesReceivedDelta: number | undefined
    let mempoolBytes: number | undefined

    if (summary.cpuSecondsTotal !== undefined && prev?.cpuSecondsTotal !== undefined) {
      const deltaSeconds = summary.cpuSecondsTotal - prev.cpuSecondsTotal
      const elapsed = (now - prev.timestamp) / 1000
      if (elapsed > 0 && deltaSeconds >= 0) {
        cpuPercent = Math.min(100, Math.max(0, (deltaSeconds / elapsed) * 100))
      }
    }

    if (stats.network) {
      const sent = stats.network.bytes_sent
      const received = stats.network.bytes_received

      if (typeof sent === "number" && prev?.networkBytesSent !== undefined) {
        const delta = sent - prev.networkBytesSent
        if (delta >= 0) {
          networkBytesSentDelta = delta
        }
      }

      if (typeof received === "number" && prev?.networkBytesReceived !== undefined) {
        const delta = received - prev.networkBytesReceived
        if (delta >= 0) {
          networkBytesReceivedDelta = delta
        }
      }
    }

    if (stats.mempool && typeof stats.mempool.size_bytes === "number") {
      mempoolBytes = stats.mempool.size_bytes
    }

    const point: InfrastructurePoint = {
      timestamp: now,
      cpuPercent,
      residentMemoryBytes: summary.residentMemoryBytes,
      heapAllocBytes: summary.heapAllocBytes,
      networkBytesSent: networkBytesSentDelta,
      networkBytesReceived: networkBytesReceivedDelta,
      mempoolSizeBytes: mempoolBytes,
    }

    setHistory((prevHistory) => {
      const next = [...prevHistory, point]
      return next.slice(-50)
    })

    previousSampleRef.current = {
      timestamp: now,
      cpuSecondsTotal: summary.cpuSecondsTotal,
      networkBytesSent: stats.network?.bytes_sent,
      networkBytesReceived: stats.network?.bytes_received,
      mempoolSizeBytes: mempoolBytes,
    }
  }, [data])

  const latestHistory = history[history.length - 1]

  const keyMetrics = useMemo(() => {
    const uptimeSeconds = (() => {
      const processStart = data?.backend.metrics.summary.processStartTimeSeconds
      if (!processStart) return undefined
      const nowSeconds = Math.floor(data.timestamp / 1000)
      return nowSeconds - processStart
    })()

    const aiUptimeSeconds = (() => {
      const uptime = data?.ai.health && typeof data.ai.health === "object" ? (data.ai.health as Record<string, unknown>).uptime_seconds : undefined
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
        derived.mempoolLatencyMs ??
        (typeof data?.backend.stats.mempool?.oldest_tx_age_seconds === "number"
          ? data.backend.stats.mempool.oldest_tx_age_seconds * 1000
          : undefined),
      consensusLatencyMs:
        derived.consensusLatencyMs ??
        (typeof data?.backend.stats.chain?.avg_block_time_seconds === "number"
          ? data.backend.stats.chain.avg_block_time_seconds * 1000
          : undefined),
      p2pLatencyMs:
        derived.p2pLatencyMs ??
        (typeof data?.backend.stats.network?.avg_latency_ms === "number"
          ? data.backend.stats.network.avg_latency_ms
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
    isLoading,
    mutate,
    keyMetrics,
    history,
    backendLatencyMetrics,
    aiLatencyMs,
  }
}
