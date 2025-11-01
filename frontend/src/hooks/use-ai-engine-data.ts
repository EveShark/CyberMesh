"use client"

import useSWR from "swr"

import type {
  AiDetectionHistoryResponse,
  AiMetricsResponse,
  AiSuspiciousNodesResponse,
} from "@/lib/api"

interface AiMetricsEnvelope {
  metrics: AiMetricsResponse
}

interface AiDetectionsEnvelope {
  history: AiDetectionHistoryResponse
  suspicious: AiSuspiciousNodesResponse
}

interface AiHealthResponse {
  health: Record<string, unknown>
  ready: Record<string, unknown>
}

const fetcher = async <T>(url: string): Promise<T> => {
  const res = await fetch(url, { cache: "no-store" })
  if (!res.ok) {
    const text = await res.text().catch(() => "")
    throw new Error(`Request failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ""}`)
  }
  return (await res.json()) as T
}

export function useAiEngineData(refreshInterval = 5000) {
  const {
    data: metrics,
    error: metricsError,
    isLoading: metricsLoading,
  } = useSWR<AiMetricsEnvelope>("/api/ai/metrics", fetcher, { refreshInterval })

  const {
    data: detections,
    error: detectionsError,
    isLoading: detectionsLoading,
  } = useSWR<AiDetectionsEnvelope>("/api/ai/detections", fetcher, { refreshInterval })

  const {
    data: health,
    error: healthError,
    isLoading: healthLoading,
  } = useSWR<AiHealthResponse>("/api/ai/health", fetcher, { refreshInterval })

  const isLoading = metricsLoading || detectionsLoading || healthLoading
  const error = metricsError || detectionsError || healthError

  return {
    isLoading,
    error,
    metrics: metrics?.metrics,
    history: detections?.history,
    suspicious: detections?.suspicious,
    health,
  }
}
