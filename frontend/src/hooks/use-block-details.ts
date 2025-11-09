"use client"

import { useCallback, useMemo } from "react"

import type { BlockSummary } from "@/lib/api"

import { useDashboardData } from "./use-dashboard-data"

interface UseBlockDetailsOptions {
  includeTransactions?: boolean
  enabled?: boolean
}

interface UseBlockDetailsResult {
  block: BlockSummary | null
  isLoading: boolean
  error: string | null
  refresh: () => Promise<void>
}

export function useBlockDetails(
  height?: number,
  { includeTransactions: _includeTransactions = true, enabled = true }: UseBlockDetailsOptions = {},
): UseBlockDetailsResult {
  const { data: dashboard, error: dashboardError, isLoading, mutate } = useDashboardData(15000)

  void _includeTransactions

  const shouldLookup = useMemo(() => enabled && typeof height === "number" && height >= 0, [enabled, height])

  const block = useMemo<BlockSummary | null>(() => {
    if (!shouldLookup || !dashboard) {
      return null
    }

    const recent = dashboard.blocks?.recent ?? []
    const match = recent.find((candidate) => candidate.height === height)
    return match ?? null
  }, [dashboard, height, shouldLookup])

  const error = useMemo(() => {
    if (!shouldLookup) {
      return null
    }
    if (dashboardError) {
      return dashboardError instanceof Error ? dashboardError.message : String(dashboardError)
    }
    if (dashboard && !block) {
      return "Block not found in dashboard snapshot"
    }
    return null
  }, [block, dashboard, dashboardError, shouldLookup])

  const refresh = useCallback(async () => {
    await mutate()
  }, [mutate])

  return {
    block,
    isLoading: shouldLookup && (isLoading || (!dashboard && !block)),
    error,
    refresh,
  }
}
