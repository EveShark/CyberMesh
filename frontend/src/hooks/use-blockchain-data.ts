"use client"

import { useMemo } from "react"

import type {
  BlockSummary,
  DashboardBlockPagination,
  DashboardLedgerSection,
  NetworkStatsSummary,
} from "@/lib/api"

import { useDashboardData } from "./use-dashboard-data"

interface BlockchainMetrics {
  latestHeight: number
  totalTransactions: number
  avgBlockTime: number
  avgBlockSize: number
  successRate: number
  anomalyCount: number
  isLive: boolean
  network?: NetworkStatsSummary
}

const DEFAULT_BLOCKCHAIN_REFRESH_MS = 20000

export function useBlockchainData(autoRefresh = true, refreshInterval = DEFAULT_BLOCKCHAIN_REFRESH_MS) {
  const effectiveInterval = autoRefresh ? refreshInterval : 0
  const { data: dashboard, error, isLoading, mutate } = useDashboardData(effectiveInterval)

  const blocks = useMemo<BlockSummary[]>(() => {
    if (!dashboard) return []
    return dashboard.blocks.recent ?? []
  }, [dashboard])

  const metrics = useMemo<BlockchainMetrics | null>(() => {
    if (!dashboard) return null
    const blockMetrics = dashboard.blocks.metrics

    const anomalyCount = (dashboard.blocks.recent ?? []).reduce((sum, block) => sum + (block.anomaly_count ?? 0), 0)

    return {
      latestHeight: blockMetrics.latest_height ?? 0,
      totalTransactions: blockMetrics.total_transactions ?? 0,
      avgBlockTime: blockMetrics.avg_block_time_seconds ?? 0,
      avgBlockSize: blockMetrics.avg_block_size_bytes ?? 0,
      successRate: blockMetrics.success_rate ?? 0,
      anomalyCount,
      isLive: (dashboard.blocks.recent ?? []).length > 0,
      network: blockMetrics.network as NetworkStatsSummary | undefined,
    }
  }, [dashboard])

  const pagination: DashboardBlockPagination | undefined = dashboard?.blocks.pagination
  const ledger: DashboardLedgerSection | undefined = dashboard?.ledger ?? undefined

  const refreshData = async () => {
    await mutate()
  }

  return {
    blocks,
    metrics,
    pagination,
    ledger,
    isLoading: !dashboard && isLoading,
    error,
    refreshData,
  }
}
