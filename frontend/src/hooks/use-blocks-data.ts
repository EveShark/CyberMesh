"use client"

import { useMemo } from "react"

import type { BlockSummary, DashboardBlockPagination } from "@/lib/api"

import { useDashboardData } from "./use-dashboard-data"

export function useBlocksData(limit = 50) {
  const { data: dashboard, error, isLoading, mutate } = useDashboardData(15000)

  const blocks = useMemo<BlockSummary[]>(() => {
    if (!dashboard) return []
    const recent = dashboard.blocks.recent ?? []
    if (limit <= 0) return recent
    if (recent.length <= limit) return recent
    return recent.slice(recent.length - limit)
  }, [dashboard, limit])

  const pagination: DashboardBlockPagination | undefined = dashboard?.blocks.pagination

  return {
    blocks,
    pagination,
    isLoading: !dashboard && isLoading,
    error,
    refresh: mutate,
  }
}
