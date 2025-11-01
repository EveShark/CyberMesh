"use client"

import { useState, useEffect, useCallback } from "react"
import type { BackendHealth, BlockSummary, StatsSummary, NetworkStatsSummary } from "@/lib/api"

interface BlockchainMetrics {
  latestHeight: number
  totalTransactions: number
  avgBlockTime: number
  successRate: number
  anomalyCount: number
  isLive: boolean
  network?: NetworkStatsSummary
}

export function useBlockchainData(autoRefresh = true, refreshInterval = 5000) {
  const [blocks, setBlocks] = useState<BlockSummary[]>([])
  const [metrics, setMetrics] = useState<BlockchainMetrics | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchBlocks = useCallback(async () => {
    try {
      const res = await fetch(`/api/blocks?start=0&limit=100`, { cache: "no-store" })
      if (!res.ok) {
        const message = await res.text().catch(() => res.statusText)
        throw new Error(message || `Request failed with status ${res.status}`)
      }
      const data = (await res.json()) as { blocks?: BlockSummary[] }
      setBlocks(data.blocks ?? [])
      setError(null)
    } catch (err) {
      console.error("Failed to fetch blocks:", err)
      setError(err instanceof Error ? err.message : "Failed to fetch blocks")
    }
  }, [])

  const fetchMetrics = useCallback(async () => {
    try {
      const [statsRes, healthRes] = await Promise.all([
        fetch(`/api/backend/stats`, { cache: "no-store" }),
        fetch(`/api/backend/health`, { cache: "no-store" }),
      ])

      if (!statsRes.ok) {
        const message = await statsRes.text().catch(() => statsRes.statusText)
        throw new Error(message || `Failed to fetch stats (${statsRes.status})`)
      }

      if (!healthRes.ok) {
        const message = await healthRes.text().catch(() => healthRes.statusText)
        throw new Error(message || `Failed to fetch health (${healthRes.status})`)
      }

      const { stats } = (await statsRes.json()) as { stats: StatsSummary }
      const { health } = (await healthRes.json()) as { health: BackendHealth }

      setMetrics({
        latestHeight: stats.chain?.height || 0,
        totalTransactions: stats.chain?.total_transactions || 0,
        avgBlockTime: stats.chain?.avg_block_time_seconds || 0,
        successRate: stats.chain?.success_rate || 0,
        anomalyCount: 0,
        isLive: health.status === "ok",
        network: stats.network,
      })
      setError(null)
    } catch (err) {
      console.error("Failed to fetch metrics:", err)
      setError(err instanceof Error ? err.message : "Failed to fetch metrics")
    }
  }, [])

  const refreshData = useCallback(async () => {
    setIsLoading(true)
    await fetchBlocks()
    await fetchMetrics()
    setIsLoading(false)
  }, [fetchBlocks, fetchMetrics])

  // Calculate total anomaly count from blocks after they're fetched
  useEffect(() => {
    if (blocks.length > 0 && metrics) {
      const totalAnomalies = blocks.reduce((sum, block) => sum + (block.anomaly_count || 0), 0)
      if (totalAnomalies !== metrics.anomalyCount) {
        setMetrics({ ...metrics, anomalyCount: totalAnomalies })
      }
    }
  }, [blocks, metrics])

  // Initial fetch
  useEffect(() => {
    refreshData()
  }, [refreshData])

  // Auto-refresh
  useEffect(() => {
    if (!autoRefresh) return

    const interval = setInterval(() => {
      refreshData()
    }, refreshInterval)

    return () => clearInterval(interval)
  }, [autoRefresh, refreshInterval, refreshData])

  return {
    blocks,
    metrics,
    isLoading,
    error,
    refreshData,
  }
}
