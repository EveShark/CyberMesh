"use client"

import { useState, useEffect } from "react"
import { Card } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { LineChart, Line, XAxis, YAxis, CartesianGrid, ResponsiveContainer } from "recharts"
import { TrendingUp, TrendingDown, Minus, Loader2 } from "lucide-react"
import type { BlockSummary } from "@/lib/api"

interface DecisionTimelineTabProps {
  data?: Array<{
    time: string
    normal_txs?: number
    anomaly_txs?: number
    total?: number
    // Legacy fields (kept for backwards compatibility)
    approved?: number
    rejected?: number
    timeout?: number
  }>
}

const chartConfig = {
  normal_txs: {
    label: "Normal Transactions",
    color: "hsl(var(--status-healthy))",
  },
  anomaly_txs: {
    label: "Anomaly Transactions",
    color: "hsl(var(--status-warning))",
  },
}

export function DecisionTimelineTab({ data }: DecisionTimelineTabProps) {
  const [blocks, setBlocks] = useState<BlockSummary[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Fetch real block data
  useEffect(() => {
    const fetchBlocks = async () => {
      try {
        setIsLoading(true)
        const res = await fetch(`/api/blocks?start=0&limit=50`, { cache: "no-store" })
        if (!res.ok) {
          const message = await res.text().catch(() => res.statusText)
          throw new Error(message || `Request failed (${res.status})`)
        }
        const data = (await res.json()) as { blocks?: BlockSummary[] }
        setBlocks(data.blocks ?? [])
        setError(null)
      } catch (err) {
        console.error("Failed to fetch blocks:", err)
        setError(err instanceof Error ? err.message : "Failed to load timeline data")
      } finally {
        setIsLoading(false)
      }
    }

    fetchBlocks()
  }, [])

  // Convert real blocks to chart data
  // Show normal vs anomaly transactions in each block
  const chartData = blocks.map(block => ({
    time: `Block ${block.height}`,
    normal_txs: (block.transaction_count || 0) - (block.anomaly_count || 0),
    anomaly_txs: block.anomaly_count || 0,
    total: block.transaction_count || 0,
  }))

  // Debug logging to see actual data values
  console.log("Timeline Debug - Raw blocks:", blocks.length)
  console.log("Timeline Debug - Chart data sample:", chartData.slice(0, 3))
  console.log("Timeline Debug - Normal txs values:", chartData.map(d => d.normal_txs))

  const sampleData = data || chartData

  // Calculate stats
  const totalNormal = sampleData.reduce((sum, item) => sum + ("normal_txs" in item ? (item.normal_txs || 0) : 0), 0)
  const totalAnomalies = sampleData.reduce((sum, item) => sum + ("anomaly_txs" in item ? (item.anomaly_txs || 0) : 0), 0)
  const totalTransactions = totalNormal + totalAnomalies

  const normalRate = totalTransactions > 0 ? (totalNormal / totalTransactions) * 100 : 0
  const health = normalRate >= 98 ? "Excellent" : normalRate >= 95 ? "Good" : "Needs Attention"
  const healthColor = normalRate >= 98 ? "text-status-healthy" : normalRate >= 95 ? "text-status-warning" : "text-status-critical"

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <span className="ml-3 text-muted-foreground">Loading timeline data...</span>
      </div>
    )
  }

  if (error) {
    return (
      <Card className="bg-status-critical/10 border-status-critical/40 p-6">
        <p className="text-status-critical">{error}</p>
      </Card>
    )
  }

  return (
    <div className="space-y-6">
      {/* Chart */}
      <div className="space-y-3">
        {/* Legend */}
        <div className="flex items-center justify-center gap-6 text-sm">
          <div className="flex items-center gap-2">
            <div className="h-3 w-3 rounded-full bg-[#10b981]"></div>
            <span className="text-muted-foreground">Normal Transactions</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="h-3 w-3 rounded-full bg-[#f59e0b]"></div>
            <span className="text-muted-foreground">Anomalies</span>
          </div>
        </div>
        
        <ChartContainer config={chartConfig} className="h-64 w-full">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={sampleData} margin={{ top: 5, right: 5, left: 5, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--muted-foreground) / 0.2)" vertical={false} />
              <XAxis
                dataKey="time"
                stroke="hsl(var(--muted-foreground))"
                fontSize={10}
                tickLine={false}
                axisLine={false}
                tick={{ fill: "hsl(var(--muted-foreground))" }}
              />
              <YAxis
                stroke="hsl(var(--muted-foreground))"
                fontSize={12}
                tickLine={false}
                axisLine={false}
                tick={{ fill: "hsl(var(--muted-foreground))" }}
              />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Line
                type="monotone"
                dataKey="normal_txs"
                stroke="#10b981"
                strokeWidth={2}
                dot={{ fill: "#10b981", strokeWidth: 2, r: 3 }}
                activeDot={{ r: 5 }}
                name="Normal Transactions"
              />
              <Line
                type="monotone"
                dataKey="anomaly_txs"
                stroke="#f59e0b"
                strokeWidth={2}
                dot={{ fill: "#f59e0b", strokeWidth: 2, r: 3 }}
                activeDot={{ r: 5 }}
                name="Anomaly Transactions"
              />
            </LineChart>
          </ResponsiveContainer>
        </ChartContainer>
      </div>

      {/* Key Insights */}
      <div>
        <h4 className="text-sm font-semibold text-foreground mb-3">Key Insights</h4>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          {/* Normal Transaction Rate */}
          <Card className="bg-card/40 border border-border/40 p-4">
            <div className="flex items-start justify-between">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Normal Tx Rate</p>
                <p className="text-2xl font-bold text-status-healthy">{normalRate.toFixed(1)}%</p>
                <p className="text-xs text-muted-foreground">{totalNormal} of {totalTransactions}</p>
              </div>
              <div className="p-2 rounded-lg bg-status-healthy/10">
                <TrendingUp className="h-4 w-4 text-status-healthy" />
              </div>
            </div>
          </Card>

          {/* Anomalies */}
          <Card className="bg-card/40 border border-border/40 p-4">
            <div className="flex items-start justify-between">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Anomalies</p>
                <p className={`text-2xl font-bold ${totalAnomalies > 10 ? "text-status-critical" : totalAnomalies > 5 ? "text-status-warning" : "text-muted-foreground"}`}>
                  {totalAnomalies}
                </p>
                <p className="text-xs text-muted-foreground">in last {sampleData.length} blocks</p>
              </div>
              <div className={`p-2 rounded-lg ${totalAnomalies > 5 ? "bg-status-warning/10" : "bg-muted/10"}`}>
                {totalAnomalies > 5 ? (
                  <TrendingDown className="h-4 w-4 text-status-warning" />
                ) : (
                  <Minus className="h-4 w-4 text-muted-foreground" />
                )}
              </div>
            </div>
          </Card>

          {/* Chain Health */}
          <Card className="bg-card/40 border border-border/40 p-4">
            <div className="flex items-start justify-between">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Chain Health</p>
                <p className={`text-2xl font-bold ${healthColor}`}>{health}</p>
                <p className="text-xs text-muted-foreground">Overall status</p>
              </div>
              <div className={`p-2 rounded-lg ${normalRate >= 98 ? "bg-status-healthy/10" : "bg-status-warning/10"}`}>
                {normalRate >= 98 ? (
                  <TrendingUp className="h-4 w-4 text-status-healthy" />
                ) : (
                  <Minus className="h-4 w-4 text-status-warning" />
                )}
              </div>
            </div>
          </Card>
        </div>
      </div>
    </div>
  )
}
