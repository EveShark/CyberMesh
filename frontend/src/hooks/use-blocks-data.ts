"use client"

import useSWR from "swr"

import type { BlockSummary } from "@/lib/api"

interface BlocksResponse {
  blocks: BlockSummary[]
  pagination: {
    start: number
    limit: number
    total: number
    next?: string
  }
}

const fetcher = async <T>(url: string): Promise<T> => {
  const res = await fetch(url, { cache: "no-store" })
  if (!res.ok) {
    const text = await res.text().catch(() => "")
    throw new Error(`Request failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ""}`)
  }
  return (await res.json()) as T
}

export function useBlocksData(limit = 50) {
  // First fetch to get total count
  const { data: initialData } = useSWR<BlocksResponse>(`/api/blocks?start=0&limit=1`, fetcher, {
    revalidateOnFocus: false,
  })

  // Calculate start position to get the latest blocks
  // If total=19 and limit=10, start should be 19-10=9 to get blocks 9-18
  const total = initialData?.pagination?.total ?? 0
  const calculatedStart = Math.max(0, total - limit)
  
  const { data, error, isLoading, mutate } = useSWR<BlocksResponse>(
    total > 0 ? `/api/blocks?start=${calculatedStart}&limit=${limit}` : null,
    fetcher,
    {
      refreshInterval: 5000,
      revalidateOnFocus: false,
    }
  )

  return {
    blocks: data?.blocks ?? [],
    pagination: data?.pagination,
    isLoading: isLoading || !initialData,
    error,
    refresh: mutate,
  }
}
