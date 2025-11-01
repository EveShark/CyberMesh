"use client"

import { useState, useEffect } from "react"
import useSWR from "swr"

import type { BlockSummary } from "@/lib/api"
import { BlockStore } from "@/lib/block-store"

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

export function usePersistentBlocks(limit = 20) {
  const [blockStore] = useState(() => new BlockStore())
  const [localBlocks, setLocalBlocks] = useState<BlockSummary[]>([])

  const { data, error, isLoading, mutate } = useSWR<BlocksResponse>(
    `/api/blocks?start=0&limit=${limit}`,
    fetcher,
    {
      refreshInterval: 10000,
      dedupingInterval: 5000,
      revalidateOnFocus: false,
      revalidateOnReconnect: true,
      onSuccess: (newData) => {
        if (newData?.blocks && newData.blocks.length > 0) {
          blockStore.addBlocks(newData.blocks)
          setLocalBlocks(blockStore.getLatest(limit))
        }
      },
    }
  )

  useEffect(() => {
    setLocalBlocks(blockStore.getLatest(limit))
  }, [blockStore, limit])

  return {
    blocks: localBlocks,
    pagination: data?.pagination,
    isLoading,
    error,
    refresh: mutate,
  }
}
