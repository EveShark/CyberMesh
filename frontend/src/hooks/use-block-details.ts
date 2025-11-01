"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { backendApi } from "@/lib/api"
import type { BlockSummary } from "@/lib/api"

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
  { includeTransactions = true, enabled = true }: UseBlockDetailsOptions = {},
): UseBlockDetailsResult {
  const [block, setBlock] = useState<BlockSummary | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const shouldFetch = useMemo(() => enabled && typeof height === "number" && height >= 0, [enabled, height])

  const cancelOngoing = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
  }, [])

  const load = useCallback(async () => {
    if (!shouldFetch) {
      setBlock(null)
      setError(null)
      return
    }

    cancelOngoing()
    const controller = new AbortController()
    abortRef.current = controller
    setIsLoading(true)
    setError(null)

    try {
      const result = await backendApi.getBlockByHeight(height!, includeTransactions)
      if (!controller.signal.aborted) {
        setBlock(result)
      }
    } catch (err) {
      if (!controller.signal.aborted) {
        setError(err instanceof Error ? err.message : "Failed to load block details")
        setBlock(null)
      }
    } finally {
      if (!controller.signal.aborted) {
        setIsLoading(false)
      }
    }
  }, [cancelOngoing, height, includeTransactions, shouldFetch])

  useEffect(() => {
    load()
    return cancelOngoing
  }, [load, cancelOngoing])

  const refresh = useCallback(async () => {
    await load()
  }, [load])

  return {
    block,
    isLoading,
    error,
    refresh,
  }
}
