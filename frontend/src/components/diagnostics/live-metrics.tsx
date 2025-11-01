"use client"

import { useState, useEffect } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Activity, Database, FileText, AlertTriangle, TrendingUp, Loader2 } from "lucide-react"

interface BlockchainMetrics {
  height: number
  totalTransactions: number
  evidenceCount: number
  successRate: number
  lastBlockTime?: string
  avgBlockTime?: number
}

export function LiveMetrics() {
  const [metrics, setMetrics] = useState<BlockchainMetrics | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchMetrics()
  }, [])

  async function fetchMetrics() {
    setIsLoading(true)
    setError(null)

    try {
      // Fetch stats using Next.js API proxy
      const statsResponse = await fetch(`/api/backend/stats`, {
        headers: { "X-API-Key": "test" }
      })

      if (!statsResponse.ok) {
        throw new Error(`Stats API returned ${statsResponse.status}`)
      }

      const statsData = await statsResponse.json()

      // Fetch anomaly stats
      const anomalyResponse = await fetch(`/api/backend/anomalies/stats`, {
        headers: { "X-API-Key": "test" }
      })

      let evidenceCount = 0
      if (anomalyResponse.ok) {
        const anomalyData = await anomalyResponse.json()
        evidenceCount = anomalyData.data?.total_count || 0
      }

      // Fetch latest blocks to calculate last block time
      const blocksResponse = await fetch(`/api/blocks?start=0&limit=2`, {
        headers: { "X-API-Key": "test" }
      })

      let lastBlockTime = undefined
      let avgBlockTime = undefined

      if (blocksResponse.ok) {
        const blocksData = await blocksResponse.json()
        if (blocksData.data?.blocks?.length > 0) {
          const latestBlock = blocksData.data.blocks[0]
          const now = new Date()
          const blockTime = new Date(latestBlock.timestamp)
          const diffSeconds = Math.floor((now.getTime() - blockTime.getTime()) / 1000)
          lastBlockTime = diffSeconds < 60 ? `${diffSeconds}s ago` : `${Math.floor(diffSeconds / 60)}m ago`

          // Calculate average block time if we have 2 blocks
          if (blocksData.data.blocks.length === 2) {
            const block1Time = new Date(blocksData.data.blocks[0].timestamp).getTime()
            const block2Time = new Date(blocksData.data.blocks[1].timestamp).getTime()
            avgBlockTime = Math.abs(block1Time - block2Time) / 1000
          }
        }
      }

      setMetrics({
        height: statsData.data?.chain?.height || 0,
        totalTransactions: statsData.data?.chain?.total_transactions || 0,
        evidenceCount,
        successRate: (statsData.data?.chain?.success_rate || 0) * 100,
        lastBlockTime,
        avgBlockTime
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch metrics")
    } finally {
      setIsLoading(false)
    }
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Live Blockchain Metrics</CardTitle>
          <CardDescription>Real-time data from the blockchain</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
          </div>
        </CardContent>
      </Card>
    )
  }

  if (error) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Live Blockchain Metrics</CardTitle>
          <CardDescription>Real-time data from the blockchain</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="p-4 rounded-lg bg-red-500/10 border border-red-500/20 text-red-600 dark:text-red-400">
            Error: {error}
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle>Live Blockchain Metrics</CardTitle>
            <CardDescription>Real-time data from CockroachDB</CardDescription>
          </div>
          <Badge variant="default" className="bg-green-500">
            <Activity className="h-3 w-3 mr-1" />
            Live
          </Badge>
        </div>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {/* Blockchain Height */}
          <div className="p-4 rounded-lg border bg-card">
            <div className="flex items-center gap-2 mb-2">
              <Database className="h-4 w-4 text-blue-500" />
              <div className="text-sm font-medium text-muted-foreground">Blockchain Height</div>
            </div>
            <div className="text-3xl font-bold">{metrics?.height.toLocaleString()}</div>
            {metrics?.lastBlockTime && (
              <div className="text-xs text-muted-foreground mt-1">
                Last block: {metrics.lastBlockTime}
              </div>
            )}
          </div>

          {/* Total Transactions */}
          <div className="p-4 rounded-lg border bg-card">
            <div className="flex items-center gap-2 mb-2">
              <FileText className="h-4 w-4 text-purple-500" />
              <div className="text-sm font-medium text-muted-foreground">Total Transactions</div>
            </div>
            <div className="text-3xl font-bold">{metrics?.totalTransactions.toLocaleString()}</div>
            {metrics?.avgBlockTime && (
              <div className="text-xs text-muted-foreground mt-1">
                Avg block time: {metrics.avgBlockTime.toFixed(0)}s
              </div>
            )}
          </div>

          {/* Evidence Count */}
          <div className="p-4 rounded-lg border bg-card">
            <div className="flex items-center gap-2 mb-2">
              <AlertTriangle className="h-4 w-4 text-orange-500" />
              <div className="text-sm font-medium text-muted-foreground">Evidence Transactions</div>
            </div>
            <div className="text-3xl font-bold">{metrics?.evidenceCount.toLocaleString()}</div>
            <div className="text-xs text-muted-foreground mt-1">
              {((metrics?.evidenceCount || 0) / (metrics?.totalTransactions || 1) * 100).toFixed(1)}% of total
            </div>
          </div>

          {/* Success Rate */}
          <div className="p-4 rounded-lg border bg-card">
            <div className="flex items-center gap-2 mb-2">
              <TrendingUp className="h-4 w-4 text-green-500" />
              <div className="text-sm font-medium text-muted-foreground">Success Rate</div>
            </div>
            <div className="text-3xl font-bold">{metrics?.successRate.toFixed(2)}%</div>
            <div className="text-xs text-green-600 dark:text-green-400 mt-1">
              ✓ Calculated from DB
            </div>
          </div>

          {/* Transactions per Block */}
          <div className="p-4 rounded-lg border bg-card">
            <div className="flex items-center gap-2 mb-2">
              <Activity className="h-4 w-4 text-cyan-500" />
              <div className="text-sm font-medium text-muted-foreground">Avg Txs per Block</div>
            </div>
            <div className="text-3xl font-bold">
              {((metrics?.totalTransactions || 0) / (metrics?.height || 1)).toFixed(1)}
            </div>
            <div className="text-xs text-muted-foreground mt-1">
              Real-time average
            </div>
          </div>

          {/* Data Source */}
          <div className="p-4 rounded-lg border bg-card">
            <div className="flex items-center gap-2 mb-2">
              <Database className="h-4 w-4 text-indigo-500" />
              <div className="text-sm font-medium text-muted-foreground">Data Source</div>
            </div>
            <div className="text-lg font-bold">CockroachDB</div>
            <div className="text-xs text-green-600 dark:text-green-400 mt-1">
              ✓ Production database
            </div>
          </div>
        </div>

        <div className="mt-4 p-3 rounded-lg bg-green-500/10 border border-green-500/20">
          <div className="flex items-center gap-2 text-green-600 dark:text-green-400">
            <Activity className="h-4 w-4" />
            <span className="font-medium">Blockchain is live and producing blocks</span>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
