"use client"

import useSWR from "swr"

import type { BlockSummary } from "@/lib/api"

interface LedgerSummaryResponse {
  recentBlocks: BlockSummary[]
  decisionTimeline: Array<{
    time: string
    approved: number
    rejected: number
    timeout: number
  }>
}

const fetcher = async <T>(url: string): Promise<T> => {
  const res = await fetch(url, { cache: "no-store" })
  if (!res.ok) {
    const text = await res.text().catch(() => "")
    throw new Error(`Request failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ""}`)
  }
  return (await res.json()) as T
}

export function useLedgerData(refreshInterval = 5000) {
  const { data, error, isLoading, mutate } = useSWR<LedgerSummaryResponse>("/api/ledger/summary", fetcher, {
    refreshInterval,
  })

  return {
    data,
    error,
    isLoading,
    refresh: mutate,
  }
}
