"use client"

import { useEffect, useMemo, useRef, useState } from "react"

import { useDashboardData } from "./use-dashboard-data"

interface ThreatBreakdownEntry {
  threatType: string
  severity: "critical" | "high" | "medium" | "low"
  published: number
  abstained: number
  total: number
}

interface ThreatLifetimeTotals {
  total: number
  published: number
  abstained: number
  rateLimited?: number
  errors?: number
}

interface ThreatsApiResponse {
  timestamp: number
  source?: string
  fallbackReason?: string
  detectionLoop: {
    running: boolean
    status: string
    blocking: boolean
    healthy: boolean
    message?: string
    issues?: string[]
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
  feed: ThreatFeedEntry[]
  stats?: ThreatStats | null
  avgResponseTimeMs?: number
  lifetimeTotals?: ThreatLifetimeTotals
}

interface ThreatTrendPoint {
  timestamp: number
  publishedDelta: number
  totalDelta: number
}

interface ThreatFeedEntry {
  id: string
  type: string
  severity: "critical" | "high" | "medium" | "low"
  title: string
  description: string
  source: string
  block_height: number
  timestamp: number
  confidence: number
  tx_hash: string
}

interface ThreatStats {
  critical_count: number
  high_count: number
  medium_count: number
  low_count: number
  total_count: number
}

const DEFAULT_THREATS_REFRESH_MS = 10000

export function useThreatsData(refreshInterval = DEFAULT_THREATS_REFRESH_MS) {
  const { data: dashboard, error, isLoading, mutate } = useDashboardData(refreshInterval)

  const previousTotalsRef = useRef<{
    timestamp: number
    published: number
    overall: number
    kind: "lifetime" | "window"
  } | null>(null)

  const [trend, setTrend] = useState<ThreatTrendPoint[]>([])

  useEffect(() => {
    if (!dashboard) return

    const windowTotals = dashboard.threats.breakdown.totals
    const loopCounters = dashboard.threats.detection_loop?.counters ?? dashboard.ai.metrics?.loop?.counters
    const lifetimeTotals = dashboard.threats.lifetime_totals

    let totalsSource = {
      published: windowTotals.published,
      overall: windowTotals.overall,
      kind: "window" as const,
    }

    if (lifetimeTotals) {
      totalsSource = {
        published: lifetimeTotals.published,
        overall: lifetimeTotals.total,
        kind: "lifetime" as const,
      }
    } else if (loopCounters) {
      const published = typeof loopCounters.detections_published === "number" ? loopCounters.detections_published : 0
      const total = typeof loopCounters.detections_total === "number" ? loopCounters.detections_total : 0
      totalsSource = {
        published,
        overall: Math.max(total, published),
        kind: "lifetime" as const,
      }
    }

    const prev = previousTotalsRef.current
    if (prev && prev.kind !== totalsSource.kind) {
      previousTotalsRef.current = null
      setTrend([])
    }

    const reference = previousTotalsRef.current
    if (reference) {
      const elapsedSeconds = Math.max(1, (dashboard.threats.timestamp - reference.timestamp) / 1000)
      const publishedDelta = Math.max(0, totalsSource.published - reference.published)
      const overallDelta = Math.max(0, totalsSource.overall - reference.overall)

      setTrend((history) => {
        const next = [
          ...history,
          {
            timestamp: dashboard.threats.timestamp,
            publishedDelta: publishedDelta / elapsedSeconds,
            totalDelta: overallDelta / elapsedSeconds,
          },
        ]
        return next.slice(-40)
      })
    }

    previousTotalsRef.current = {
      timestamp: dashboard.threats.timestamp,
      published: totalsSource.published,
      overall: totalsSource.overall,
      kind: totalsSource.kind,
    }
  }, [dashboard])

  const feed = useMemo(() => dashboard?.threats.feed ?? [], [dashboard?.threats.feed])
  const threatStats = useMemo(() => dashboard?.threats.stats ?? null, [dashboard?.threats.stats])
  const lifetimeTotals = useMemo<ThreatLifetimeTotals | undefined>(() => {
    if (!dashboard) return undefined

    const direct = dashboard.threats.lifetime_totals
    if (direct) {
      return {
        total: direct.total,
        published: direct.published,
        abstained: direct.abstained,
        rateLimited: direct.rate_limited,
        errors: direct.errors,
      }
    }

    const counters = dashboard.threats.detection_loop?.counters ?? dashboard.ai.metrics?.loop?.counters
    if (counters) {
      const published = typeof counters.detections_published === "number" ? counters.detections_published : 0
      const total = typeof counters.detections_total === "number" ? counters.detections_total : published
      const abstained = Math.max(0, total - published)
      return {
        total,
        published,
        abstained,
        rateLimited: typeof counters.detections_rate_limited === "number" ? counters.detections_rate_limited : undefined,
        errors: typeof counters.errors === "number" ? counters.errors : undefined,
      }
    }

    return undefined
  }, [dashboard])
  const threatTypes = useMemo<ThreatBreakdownEntry[]>(() => {
    if (!dashboard) return []
    return dashboard.threats.breakdown.threat_types.map((entry) => ({
      threatType: entry.threat_type,
      severity: entry.severity,
      published: entry.published,
      abstained: entry.abstained,
      total: entry.total,
    }))
  }, [dashboard])

  const severitySeries = useMemo(() => {
    if (!dashboard) return []
    return Object.entries(dashboard.threats.breakdown.severity).map(([name, value]) => ({
      name,
      value,
    }))
  }, [dashboard])

  const lastDetectionTime = useMemo(() => {
    const loop = dashboard?.ai.metrics?.loop
    const secondsSince = loop?.seconds_since_last_detection
    if (typeof secondsSince === "number") {
      const millis = dashboard?.timestamp ?? Date.now()
      return new Date(millis - secondsSince * 1000)
    }
    return undefined
  }, [dashboard])

  const threatsData = useMemo<ThreatsApiResponse | undefined>(() => {
    if (!dashboard) return undefined
    const loop = dashboard.ai.metrics?.loop
    const derived = dashboard.ai.metrics?.derived

    const loopMetrics: Record<string, number | string> = {}
    if (loop?.avg_latency_ms !== undefined) loopMetrics.avg_latency_ms = loop.avg_latency_ms
    if (loop?.last_latency_ms !== undefined) loopMetrics.last_latency_ms = loop.last_latency_ms
    if (derived?.detections_per_minute !== undefined) loopMetrics.detections_per_minute = derived.detections_per_minute
    if (derived?.publish_rate_per_minute !== undefined) loopMetrics.publish_rate_per_minute = derived.publish_rate_per_minute
    if (loop?.seconds_since_last_detection !== undefined) loopMetrics.seconds_since_last_detection = loop.seconds_since_last_detection
    if (loop?.counters?.detections_total !== undefined) loopMetrics.detections_total = loop.counters.detections_total
    if (loop?.counters?.detections_published !== undefined) loopMetrics.detections_published = loop.counters.detections_published
    if (loop?.counters?.detections_rate_limited !== undefined) loopMetrics.detections_rate_limited = loop.counters.detections_rate_limited
    if (loop?.counters?.errors !== undefined) loopMetrics.errors = loop.counters.errors

    return {
      timestamp: dashboard.threats.timestamp,
      source: dashboard.threats.source,
      fallbackReason: dashboard.threats.fallback_reason,
      detectionLoop: loop
        ? {
            running: loop.running,
            status: loop.status ?? (loop.running ? "ok" : "stopped"),
            blocking: Boolean(loop.blocking),
            healthy: loop.healthy ?? loop.running,
            message: loop.message ?? undefined,
            issues: loop.issues ?? [],
            metrics: loopMetrics,
          }
        : null,
      breakdown: {
        threatTypes,
        severity: dashboard.threats.breakdown.severity,
        totals: dashboard.threats.breakdown.totals,
      },
      feed,
      stats: threatStats,
      avgResponseTimeMs: dashboard.threats.avg_response_time_ms,
      lifetimeTotals,
    }
  }, [dashboard, feed, threatStats, lifetimeTotals, threatTypes])

  const backendStats = dashboard?.backend.stats

  const primaryLoading = !dashboard && isLoading

  return {
    data: threatsData,
    error,
    isLoading: primaryLoading,
    mutate,
    severitySeries,
    trend,
    threatTypes,
    feed,
    threatStats,
    lastDetectionTime,
    // Backend stats for hero metrics
    backendStats,
    backendError: undefined,
    backendLoading: false,
    // Combined loading state
    isLoadingAny: primaryLoading,
  }
}
