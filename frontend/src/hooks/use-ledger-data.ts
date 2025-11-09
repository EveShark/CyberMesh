"use client"

import { useMemo } from "react"

import type { BlockSummary, DashboardLedgerSection } from "@/lib/api"

import { useDashboardData } from "./use-dashboard-data"

interface LedgerSummaryResponse {
  summary?: DashboardLedgerSection
  recentBlocks: BlockSummary[]
  decisionTimeline: Array<{
    time: string
    height: number
    hash: string
    proposer: string
    approved: number
    rejected: number
    timeout: number
  }>
}

const DEFAULT_LEDGER_REFRESH_MS = 20000

export function useLedgerData(refreshInterval = DEFAULT_LEDGER_REFRESH_MS) {
  const { data: dashboard, error, isLoading, mutate } = useDashboardData(refreshInterval)

  const data = useMemo<LedgerSummaryResponse | undefined>(() => {
    if (!dashboard) return undefined
    return {
      summary: dashboard.ledger,
      recentBlocks: dashboard.blocks.recent ?? [],
      decisionTimeline: dashboard.blocks.decision_timeline ?? [],
    }
  }, [dashboard])

  return {
    data,
    error,
    isLoading: !data && isLoading,
    refresh: mutate,
  }
}
