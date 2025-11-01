"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import useSWR from "swr"

interface ThreatBreakdownEntry {
  threatType: string
  severity: "critical" | "high" | "medium" | "low"
  published: number
  abstained: number
  total: number
}

interface ThreatsApiResponse {
  timestamp: number
  detectionLoop: {
    running: boolean
    metrics?: Record<string, number | string>
  } | null
  breakdown: {
    threatTypes: ThreatBreakdownEntry[]
    severity: Record<"critical" | "high" | "medium" | "low", number>
    totals: {
      published: number
      abstained: number
      overall: number
    }
  }
  metrics: {
    samples: Array<{
      metric: string
      value: number
      labels: Record<string, string>
    }>
  }
}

interface ThreatTrendPoint {
  timestamp: number
  publishedDelta: number
  totalDelta: number
}

const fetcher = async <T>(url: string): Promise<T> => {
  const res = await fetch(url, { cache: "no-store" })
  if (!res.ok) {
    const text = await res.text().catch(() => "")
    throw new Error(`Request failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ""}`)
  }
  return (await res.json()) as T
}

interface BackendStatsResponse {
  stats: {
    chain?: {
      height: number
      state_version: number
      total_transactions?: number
      success_rate: number
      avg_block_time_seconds?: number
      avg_block_size_bytes?: number
    }
    consensus?: {
      view: number
      round: number
      validator_count: number
      quorum_size: number
      current_leader?: string
    }
    mempool?: {
      pending_transactions: number
      size_bytes: number
    }
  }
}

export function useThreatsData(refreshInterval = 5000) {
  const { data, error, isLoading, mutate } = useSWR<ThreatsApiResponse>('/api/threats', fetcher, {
    refreshInterval,
  })

  // Fetch backend stats for additional metrics (validators, response time, etc)
  const { data: backendStats, error: backendError, isLoading: backendLoading } = useSWR<BackendStatsResponse>(
    '/api/backend/stats',
    fetcher,
    {
      refreshInterval,
      // Don't fail if backend stats unavailable - graceful degradation
      shouldRetryOnError: false,
    }
  )

  const previousTotalsRef = useRef<{
    timestamp: number
    published: number
    overall: number
  } | null>(null)

  const [trend, setTrend] = useState<ThreatTrendPoint[]>([])

  useEffect(() => {
    if (!data) return

    const totals = data.breakdown.totals
    const prev = previousTotalsRef.current
    if (prev) {
      const elapsedSeconds = Math.max(1, (data.timestamp - prev.timestamp) / 1000)
      const publishedDelta = Math.max(0, totals.published - prev.published)
      const overallDelta = Math.max(0, totals.overall - prev.overall)

      setTrend((history) => {
        const next = [
          ...history,
          {
            timestamp: data.timestamp,
            publishedDelta: publishedDelta / elapsedSeconds,
            totalDelta: overallDelta / elapsedSeconds,
          },
        ]
        return next.slice(-40)
      })
    }

    previousTotalsRef.current = {
      timestamp: data.timestamp,
      published: totals.published,
      overall: totals.overall,
    }
  }, [data])

  const severitySeries = useMemo(() => {
    if (!data) return []
    return Object.entries(data.breakdown.severity).map(([name, value]) => ({
      name,
      value,
    }))
  }, [data])

  const threatTypes = data?.breakdown.threatTypes ?? []

  const lastDetectionTime = useMemo(() => {
    const metrics = data?.detectionLoop?.metrics
    if (!metrics) return undefined
    const value = metrics.last_detection_time
    if (typeof value === "number") {
      return new Date(value * 1000)
    }
    return undefined
  }, [data])

  return {
    data,
    error,
    isLoading,
    mutate,
    severitySeries,
    trend,
    threatTypes,
    lastDetectionTime,
    // Backend stats for hero metrics
    backendStats,
    backendError,
    backendLoading,
    // Combined loading state
    isLoadingAny: isLoading || backendLoading,
  }
}
