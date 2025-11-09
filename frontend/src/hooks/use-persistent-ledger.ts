"use client"

import { useEffect, useMemo, useState } from "react"

import type { BlockSummary } from "@/lib/api"
import { BlockStore } from "@/lib/block-store"
import { TimelineStore, type DecisionPoint } from "@/lib/timeline-store"

import { useDashboardData } from "./use-dashboard-data"

const DEFAULT_LEDGER_REFRESH_MS = 10000

export function usePersistentLedger(refreshInterval = DEFAULT_LEDGER_REFRESH_MS) {
  const [blockStore] = useState(() => new BlockStore())
  const [timelineStore] = useState(() => new TimelineStore())
  const [recentBlocks, setRecentBlocks] = useState<BlockSummary[]>([])
  const [decisionTimeline, setDecisionTimeline] = useState<DecisionPoint[]>([])

  const { data: dashboard, error, isLoading, mutate } = useDashboardData(refreshInterval)

  const latestSnapshot = useMemo(() => {
    return {
      blocks: dashboard?.blocks.recent ?? [],
      timeline: dashboard?.blocks.decision_timeline ?? [],
    }
  }, [dashboard])

  useEffect(() => {
    setRecentBlocks(blockStore.getLatest(20))
    setDecisionTimeline(timelineStore.getAll())
  }, [blockStore, timelineStore])

  useEffect(() => {
    if (latestSnapshot.blocks.length > 0) {
      blockStore.addBlocks(latestSnapshot.blocks)
      setRecentBlocks(blockStore.getLatest(20))
    }
    if (latestSnapshot.timeline.length > 0) {
      timelineStore.addPoints(latestSnapshot.timeline)
      setDecisionTimeline(timelineStore.getAll())
    }
  }, [blockStore, timelineStore, latestSnapshot])

  return {
    data: {
      recentBlocks,
      decisionTimeline,
    },
    error,
    isLoading: !dashboard && isLoading,
    refresh: mutate,
  }
}
