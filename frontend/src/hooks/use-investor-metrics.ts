"use client"

import useSWR from "swr"

interface InvestorMetricsResponse {
  latencyMs: number
  uptimePercentage: number
  detectionAccuracy: number
  detectionTotal: number
  consensusRound: number
}

const fetcher = async <T>(url: string): Promise<T> => {
  const res = await fetch(url, { cache: "no-store" })
  if (!res.ok) {
    const text = await res.text().catch(() => "")
    throw new Error(`Request failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ""}`)
  }
  return (await res.json()) as T
}

export function useInvestorMetrics(refreshInterval = 60000) {
  const { data, error, isLoading } = useSWR<InvestorMetricsResponse>("/api/investors/metrics", fetcher, {
    refreshInterval,
  })

  return {
    data,
    error,
    isLoading,
  }
}
