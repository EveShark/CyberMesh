"use client"

import { useState, useEffect } from "react"
import useSWR from "swr"

import type { BlockSummary } from "@/lib/api"
import { BlockStore } from "@/lib/block-store"
import { TimelineStore, type DecisionPoint } from "@/lib/timeline-store"

interface LedgerSummaryResponse {
  recentBlocks: BlockSummary[]
  decisionTimeline: DecisionPoint[]
}

const fetcher = async <T>(url: string): Promise<T> => {
  const res = await fetch(url, { cache: "no-store" })
  if (!res.ok) {
    const text = await res.text().catch(() => "")
    throw new Error(`Request failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ""}`)
  }
  return (await res.json()) as T
}

export function usePersistentLedger(refreshInterval = 10000) {
  const [blockStore] = useState(() => new BlockStore())
  const [timelineStore] = useState(() => new TimelineStore())
  const [localBlocks, setLocalBlocks] = useState<BlockSummary[]>([])
  const [localTimeline, setLocalTimeline] = useState<DecisionPoint[]>([])

  const { error, isLoading, mutate } = useSWR<LedgerSummaryResponse>("/api/ledger/summary", fetcher, {
    refreshInterval,
    dedupingInterval: 5000,
    revalidateOnFocus: false,
    revalidateOnReconnect: true,
    onSuccess: (newData) => {
      if (newData?.recentBlocks && newData.recentBlocks.length > 0) {
        blockStore.addBlocks(newData.recentBlocks)
        setLocalBlocks(blockStore.getLatest(20))
      }
      if (newData?.decisionTimeline && newData.decisionTimeline.length > 0) {
        timelineStore.addPoints(newData.decisionTimeline)
        setLocalTimeline(timelineStore.getAll())
      }
    },
  })

  useEffect(() => {
    setLocalBlocks(blockStore.getLatest(20))
    setLocalTimeline(timelineStore.getAll())
  }, [blockStore, timelineStore])

  return {
    data: {
      recentBlocks: localBlocks,
      decisionTimeline: localTimeline,
    },
    error,
    isLoading,
    refresh: mutate,
  }
}
