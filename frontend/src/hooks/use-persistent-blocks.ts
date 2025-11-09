"use client"

import { useEffect, useMemo, useState } from "react"

import type { BlockSummary, DashboardBlockPagination } from "@/lib/api"
import { BlockStore } from "@/lib/block-store"

import { useDashboardData } from "./use-dashboard-data"

const DEFAULT_PERSISTENT_BLOCK_REFRESH_MS = 20000

export function usePersistentBlocks(limit = 20) {
  const [blockStore] = useState(() => new BlockStore())
  const [localBlocks, setLocalBlocks] = useState<BlockSummary[]>([])

  const { data: dashboard, error, isLoading, mutate } = useDashboardData(DEFAULT_PERSISTENT_BLOCK_REFRESH_MS)

  const recentBlocks = useMemo(() => dashboard?.blocks.recent ?? [], [dashboard])
  const pagination: DashboardBlockPagination | undefined = dashboard?.blocks.pagination

  useEffect(() => {
    if (recentBlocks.length === 0) return
    blockStore.addBlocks(recentBlocks)
    setLocalBlocks(blockStore.getLatest(limit))
  }, [blockStore, recentBlocks, limit])

  useEffect(() => {
    setLocalBlocks(blockStore.getLatest(limit))
  }, [blockStore, limit])

  return {
    blocks: localBlocks,
    pagination,
    isLoading: !dashboard && isLoading,
    error,
    refresh: mutate,
  }
}
