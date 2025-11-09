"use client"

import { useCallback, useMemo } from "react"

import type {
  AiDetectionHistoryResponse,
  AiMetricsResponse,
  AiSuspiciousNodesResponse,
} from "@/lib/api"
import { resolveDisplayName } from "@/lib/node-alias"

import { useDashboardData } from "./use-dashboard-data"

interface AiHealthResponse {
  health: Record<string, unknown> | null
  ready: Record<string, unknown> | null
}

const DEFAULT_AI_REFRESH_MS = 15000

export function useAiEngineData(refreshInterval = DEFAULT_AI_REFRESH_MS) {
  const { data: dashboard, error, isLoading } = useDashboardData(refreshInterval)

  const validatorAliasMap = useMemo(() => {
    const map = new Map<string, string>()
    const validators = dashboard?.validators?.validators ?? []
    validators.forEach((validator) => {
      if (!validator.id) return
      const key = validator.id.toLowerCase()
      const alias = validator.alias || validator.id
      map.set(key, alias)
    })
    return map
  }, [dashboard?.validators?.validators])

  const resolveAlias = useCallback(
    (id?: string | null, fallback?: string) => {
      if (!id) return fallback
      const key = id.toLowerCase()
      if (validatorAliasMap.has(key)) {
        return validatorAliasMap.get(key) ?? fallback ?? id
      }
      return resolveDisplayName(id, fallback ?? id)
    },
    [validatorAliasMap],
  )

  const health = useMemo<AiHealthResponse | undefined>(() => {
    if (!dashboard) return undefined
    return {
      health: (dashboard.ai.health as Record<string, unknown> | undefined) ?? null,
      ready: (dashboard.ai.ready as Record<string, unknown> | undefined) ?? null,
    }
  }, [dashboard])

  const suspicious = useMemo<AiSuspiciousNodesResponse | undefined>(() => {
    const source = dashboard?.ai.suspicious as AiSuspiciousNodesResponse | undefined
    if (!source) return undefined
    const nodes = (source.nodes ?? []).map((node) => ({
      ...node,
      alias: resolveAlias(node.id, node.id),
    }))
    return {
      ...source,
      nodes,
    }
  }, [dashboard?.ai.suspicious, resolveAlias])

  const history = useMemo<AiDetectionHistoryResponse | undefined>(() => {
    const source = dashboard?.ai.history as AiDetectionHistoryResponse | undefined
    if (!source) return undefined
    const detections = (source.detections ?? []).map((entry) => ({
      ...entry,
      validator_alias: entry.validator_id ? resolveAlias(entry.validator_id, entry.validator_id) : undefined,
    }))
    return {
      ...source,
      detections,
    }
  }, [dashboard?.ai.history, resolveAlias])

  return {
    isLoading: !dashboard && isLoading,
    error,
    metrics: dashboard?.ai.metrics as AiMetricsResponse | undefined,
    history,
    suspicious,
    health,
  }
}
