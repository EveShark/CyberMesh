"use client"

import { useMemo } from "react"
import useSWR from "swr"

import type { PrometheusSample, StatsSummary, BackendReady } from "@/lib/api"

interface MetricsApiResponse {
  timestamp: number
  backend: {
    stats: StatsSummary
    readiness: BackendReady
    metrics: {
      samples: PrometheusSample[]
    }
  }
  ai: {
    detectionStats: {
      detection_loop?: {
        running: boolean
        metrics?: Record<string, number | string>
      }
    } | null
    metrics: {
      samples: PrometheusSample[]
    }
  }
  kpis: {
    cpuSeconds?: number
    residentMemoryBytes?: number
    requestErrors?: number
    requestTotal?: number
  }
  infrastructure: {
    kafka?: PrometheusSample
    redisHits?: PrometheusSample
    redisMisses?: PrometheusSample
    cockroachQueries?: PrometheusSample
  }
}

const fetcher = async <T>(url: string): Promise<T> => {
  const res = await fetch(url, { cache: "no-store" })
  if (!res.ok) {
    const text = await res.text().catch(() => "")
    throw new Error(`Request failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ""}`)
  }
  return (await res.json()) as T
}

export function useMetricsData(refreshInterval = 5000) {
  const { data, error, isLoading } = useSWR<MetricsApiResponse>("/api/metrics", fetcher, {
    refreshInterval,
  })

  const kpis = useMemo(() => {
    if (!data) {
      return {
        cpuPercent: undefined,
        memoryUsageBytes: undefined,
        requestRatePerSec: undefined,
        errorRatePercent: undefined,
      }
    }

    const memoryBytes = data.kpis.residentMemoryBytes
    const requestTotal = data.kpis.requestTotal
    const requestErrors = data.kpis.requestErrors

    return {
      cpuPercent: undefined,
      memoryUsageBytes: memoryBytes,
      requestRatePerSec: undefined,
      errorRatePercent: requestTotal && requestErrors ? (requestErrors / requestTotal) * 100 : undefined,
    }
  }, [data])

  return {
    data,
    kpis,
    error,
    isLoading,
  }
}
