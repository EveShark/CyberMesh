"use client"

import { useMemo } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Activity, Database, FileText, AlertTriangle, TrendingUp, Loader2 } from "lucide-react"

import { useDashboardData } from "@/hooks/use-dashboard-data"

interface LiveMetricsSnapshot {
  height: number
  totalTransactions: number
  evidenceCount: number
  successRate: number
  lastBlockLabel?: string
  avgBlockTimeSeconds?: number
  avgTxPerBlock?: number
}

const REFRESH_INTERVAL_MS = 15000

const formatRelative = (timestampSeconds?: number) => {
  if (!timestampSeconds) return undefined
  const blockTime = new Date(timestampSeconds * 1000)
  const diffSeconds = Math.max(0, Math.floor((Date.now() - blockTime.getTime()) / 1000))
  if (diffSeconds < 60) return `${diffSeconds}s ago`
  if (diffSeconds < 3600) return `${Math.floor(diffSeconds / 60)}m ago`
  return `${Math.floor(diffSeconds / 3600)}h ago`
}

export function LiveMetrics() {
  const { data: dashboard, isLoading, error } = useDashboardData(REFRESH_INTERVAL_MS)

  const metrics = useMemo<LiveMetricsSnapshot | null>(() => {
    if (!dashboard) return null

    const chain = dashboard.backend.stats.chain
    const blockMetrics = dashboard.blocks.metrics
    const threatsStats = dashboard.threats.stats
    const recentBlocks = dashboard.blocks.recent

    const latestHeight = blockMetrics.latest_height ?? chain?.height ?? 0
    const totalTransactions = blockMetrics.total_transactions ?? chain?.total_transactions ?? 0
    const successRaw = blockMetrics.success_rate ?? chain?.success_rate ?? 0
    const successRate = successRaw <= 1 ? successRaw * 100 : successRaw
    const evidenceCount = threatsStats?.total_count ?? dashboard.threats.breakdown?.totals?.published ?? 0

    const lastBlockLabel = formatRelative(recentBlocks?.[0]?.timestamp)
    const avgBlockTimeSeconds = blockMetrics.avg_block_time_seconds ?? chain?.avg_block_time_seconds
    const avgTxPerBlock = latestHeight > 0 ? totalTransactions / latestHeight : undefined

    return {
      height: latestHeight,
      totalTransactions,
      evidenceCount,
      successRate,
      lastBlockLabel,
      avgBlockTimeSeconds,
      avgTxPerBlock,
    }
  }, [dashboard])

  const errorMessage = useMemo(() => {
    if (!error) return undefined
    return error instanceof Error ? error.message : String(error)
  }, [error])

  if (isLoading && !metrics) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Live Blockchain Metrics</CardTitle>
          <CardDescription>Real-time data sourced from the consolidated dashboard snapshot</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-8 text-muted-foreground">
            <Loader2 className="mr-2 h-5 w-5 animate-spin" /> Syncing latest metrics…
          </div>
        </CardContent>
      </Card>
    )
  }

  if (!metrics) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Live Blockchain Metrics</CardTitle>
          <CardDescription>Real-time data sourced from the consolidated dashboard snapshot</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="p-4 rounded-lg bg-red-500/10 border border-red-500/20 text-red-600 dark:text-red-400">
            {errorMessage ?? "Metrics are unavailable in the current snapshot."}
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
            <CardDescription>Powered by `/api/dashboard/overview`</CardDescription>
          </div>
          <Badge variant="default" className="bg-green-500">
            <Activity className="mr-1 h-3 w-3" /> Live
          </Badge>
        </div>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          <div className="rounded-lg border bg-card p-4">
            <div className="mb-2 flex items-center gap-2">
              <Database className="h-4 w-4 text-blue-500" />
              <div className="text-sm font-medium text-muted-foreground">Blockchain Height</div>
            </div>
            <div className="text-3xl font-bold">{metrics.height.toLocaleString()}</div>
            {metrics.lastBlockLabel ? (
              <div className="mt-1 text-xs text-muted-foreground">Last block {metrics.lastBlockLabel}</div>
            ) : null}
          </div>

          <div className="rounded-lg border bg-card p-4">
            <div className="mb-2 flex items-center gap-2">
              <FileText className="h-4 w-4 text-purple-500" />
              <div className="text-sm font-medium text-muted-foreground">Total Transactions</div>
            </div>
            <div className="text-3xl font-bold">{metrics.totalTransactions.toLocaleString()}</div>
            {metrics.avgBlockTimeSeconds ? (
              <div className="mt-1 text-xs text-muted-foreground">
                Avg block time {metrics.avgBlockTimeSeconds.toFixed(1)}s
              </div>
            ) : null}
          </div>

          <div className="rounded-lg border bg-card p-4">
            <div className="mb-2 flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 text-orange-500" />
              <div className="text-sm font-medium text-muted-foreground">Evidence Events</div>
            </div>
            <div className="text-3xl font-bold">{metrics.evidenceCount.toLocaleString()}</div>
            <div className="mt-1 text-xs text-muted-foreground">
              {(metrics.totalTransactions > 0
                ? ((metrics.evidenceCount / metrics.totalTransactions) * 100).toFixed(1)
                : "0.0")}% of tx volume
            </div>
          </div>

          <div className="rounded-lg border bg-card p-4">
            <div className="mb-2 flex items-center gap-2">
              <TrendingUp className="h-4 w-4 text-green-500" />
              <div className="text-sm font-medium text-muted-foreground">Success Rate</div>
            </div>
            <div className="text-3xl font-bold">{metrics.successRate.toFixed(2)}%</div>
            <div className="mt-1 text-xs text-green-600 dark:text-green-400">Consensus confirmations</div>
          </div>

          <div className="rounded-lg border bg-card p-4">
            <div className="mb-2 flex items-center gap-2">
              <Activity className="h-4 w-4 text-cyan-500" />
              <div className="text-sm font-medium text-muted-foreground">Avg Txs per Block</div>
            </div>
            <div className="text-3xl font-bold">
              {metrics.avgTxPerBlock ? metrics.avgTxPerBlock.toFixed(1) : "--"}
            </div>
            <div className="mt-1 text-xs text-muted-foreground">Rolling snapshot window</div>
          </div>

          <div className="rounded-lg border bg-card p-4">
            <div className="mb-2 flex items-center gap-2">
              <Database className="h-4 w-4 text-indigo-500" />
              <div className="text-sm font-medium text-muted-foreground">Data Source</div>
            </div>
            <div className="text-lg font-bold">Dashboard Snapshot</div>
            <div className="mt-1 text-xs text-green-600 dark:text-green-400">✓ `/dashboard/overview`</div>
          </div>
        </div>

        <div className="mt-4 rounded-lg border border-green-500/20 bg-green-500/10 p-3">
          <div className="flex items-center gap-2 text-green-600 dark:text-green-400">
            <Activity className="h-4 w-4" />
            <span className="font-medium">Live blockchain telemetry with zero legacy polling.</span>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
