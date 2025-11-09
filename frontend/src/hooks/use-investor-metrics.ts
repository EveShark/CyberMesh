"use client"

import { useMemo } from "react"

import { useDashboardData } from "./use-dashboard-data"

interface InvestorMetricsResponse {
  latencyMs: number | null
  uptimePercentage: number | null
  detectionAccuracy: number | null
  detectionTotal: number | null
  consensusRound: number | null
}

export function useInvestorMetrics(refreshInterval = 60000) {
  const { data: dashboard, error, isLoading } = useDashboardData(refreshInterval)

  const metrics: InvestorMetricsResponse | null = useMemo(() => {
    if (!dashboard) return null

    const networkLatency = dashboard.backend.stats.network?.avg_latency_ms ?? null
    const healthStatus = dashboard.backend.health.status?.toLowerCase() ?? "unknown"
    const uptime = healthStatus === "healthy" ? 100 : healthStatus === "warning" ? 85 : null
    const publishRatio = dashboard.ai.metrics?.derived?.publish_success_ratio
    const detectionAccuracy = publishRatio != null ? Math.round(publishRatio * 1000) / 10 : null
    const detectionTotal = dashboard.threats.breakdown.totals.overall ?? null

    return {
      latencyMs: typeof networkLatency === "number" ? Math.round(networkLatency) : null,
      uptimePercentage: uptime,
      detectionAccuracy,
      detectionTotal,
      consensusRound: dashboard.consensus?.term ?? null,
    }
  }, [dashboard])

  const unsupported = metrics?.latencyMs == null

  return {
    data: metrics,
    error,
    isLoading: !dashboard && isLoading,
    unsupported,
  }
}
