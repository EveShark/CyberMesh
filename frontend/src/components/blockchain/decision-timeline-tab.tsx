"use client"

import { useMemo } from "react"
import { Card } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { LineChart, Line, XAxis, YAxis, CartesianGrid, ResponsiveContainer } from "recharts"
import { TrendingUp, TrendingDown, Minus } from "lucide-react"

import type { DashboardOverviewResponse } from "@/lib/api"
import { useDashboardData } from "@/hooks/use-dashboard-data"

const chartConfig = {
  normal_txs: {
    label: "Approved",
    color: "var(--status-healthy)",
  },
  anomaly_txs: {
    label: "Rejected",
    color: "var(--status-warning)",
  },
}

const NORMAL_COLOR = "var(--status-healthy)"
const ANOMALY_COLOR = "var(--status-warning)"

const DEFAULT_REFRESH_MS = 15000

type DecisionTimelineEntry = DashboardOverviewResponse["blocks"]["decision_timeline"][number]

const toChartPoint = (entry: DecisionTimelineEntry) => {
  const approved = entry.approved ?? 0
  const rejected = entry.rejected ?? 0
  const timeout = entry.timeout ?? 0
  const total = approved + rejected + timeout
  return {
    time: entry.time,
    height: entry.height,
    hash: entry.hash,
    proposer: entry.proposer,
    normal_txs: approved,
    anomaly_txs: rejected,
    timeout,
    total,
  }
}

export function DecisionTimelineTab() {
  const { data: dashboard, isLoading } = useDashboardData(DEFAULT_REFRESH_MS)

  const chartData = useMemo(() => {
    if (!dashboard?.blocks.decision_timeline?.length) return []
    return dashboard.blocks.decision_timeline.map(toChartPoint)
  }, [dashboard?.blocks.decision_timeline])

  const totals = useMemo(() => {
    return chartData.reduce(
      (acc, point) => {
        acc.approved += point.normal_txs ?? 0
        acc.rejected += point.anomaly_txs ?? 0
        acc.timeout += point.timeout ?? 0
        acc.total += point.total ?? 0
        return acc
      },
      { approved: 0, rejected: 0, timeout: 0, total: 0 },
    )
  }, [chartData])

  const totalNormal = totals.approved
  const totalRejected = totals.rejected
  const totalTimeout = totals.timeout
  const totalVotes = totals.total

  const normalRate = totalVotes > 0 ? (totalNormal / totalVotes) * 100 : 0

  if (isLoading && chartData.length === 0) {
    return (
      <div className="flex items-center justify-center py-12 text-muted-foreground">
        Loading timeline data...
      </div>
    )
  }

  if (chartData.length === 0) {
    return (
      <Card className="bg-card/40 border border-border/40 p-6 text-center text-muted-foreground">
        No recent decision timeline data available.
      </Card>
    )
  }

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <div className="flex items-center justify-center gap-6 text-sm">
          <div className="flex items-center gap-2">
            <div className="h-3 w-3 rounded-full" style={{ backgroundColor: NORMAL_COLOR }} />
            <span className="text-muted-foreground">Approved</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="h-3 w-3 rounded-full" style={{ backgroundColor: ANOMALY_COLOR }} />
            <span className="text-muted-foreground">Rejected</span>
          </div>
        </div>

        <ChartContainer config={chartConfig} className="h-64 w-full">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 5, right: 5, left: 5, bottom: 5 }}>
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
            <ChartTooltip
              content={
                <ChartTooltipContent
                  formatter={(value, name) => {
                    const label = name === "normal_txs" ? "Approved" : name === "anomaly_txs" ? "Rejected" : name === "timeout" ? "Timeout" : name
                    return (
                      <div className="flex w-full items-center justify-between gap-4">
                        <span className="text-muted-foreground">{label}</span>
                        <span className="font-semibold text-foreground">{value as number}</span>
                      </div>
                    )
                  }}
                  labelFormatter={(_, payload) => {
                    const [item] = payload ?? []
                    if (!item) return null
                    const point = item.payload as ReturnType<typeof toChartPoint>
                    const height = point.height ? `#${point.height.toLocaleString()}` : "—"
                    const proposer = point.proposer
                      ? point.proposer.length > 16
                        ? `${point.proposer.slice(0, 10)}…${point.proposer.slice(-6)}`
                        : point.proposer
                      : "unknown"
                    return (
                      <div className="flex flex-col gap-1">
                        <span className="text-xs uppercase tracking-wide text-muted-foreground">{point.time}</span>
                        <span className="text-sm font-semibold text-foreground">Block {height}</span>
                        <span className="text-xs text-muted-foreground">Proposer {proposer}</span>
                      </div>
                    )
                  }}
                />
              }
            />
              <Line
                type="monotone"
                dataKey="normal_txs"
                stroke={NORMAL_COLOR}
                strokeWidth={2}
                dot={{ fill: NORMAL_COLOR, strokeWidth: 2, r: 3 }}
                activeDot={{ r: 5 }}
                name="Approved"
              />
              <Line
                type="monotone"
                dataKey="anomaly_txs"
                stroke={ANOMALY_COLOR}
                strokeWidth={2}
                dot={{ fill: ANOMALY_COLOR, strokeWidth: 2, r: 3 }}
                activeDot={{ r: 5 }}
                name="Rejected"
              />
            </LineChart>
          </ResponsiveContainer>
        </ChartContainer>
      </div>

      <div>
        <h4 className="text-sm font-semibold text-foreground mb-3">Key Insights</h4>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <Card className="bg-card/40 border border-border/40 p-4">
            <div className="flex items-start justify-between">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Approval Rate</p>
                <p className="text-2xl font-bold text-status-healthy">{normalRate.toFixed(1)}%</p>
                <p className="text-xs text-muted-foreground">
                  {totalNormal} approvals of {totalVotes} total votes
                </p>
              </div>
              <div className="p-2 rounded-lg bg-status-healthy/10">
                <TrendingUp className="h-4 w-4 text-status-healthy" />
              </div>
            </div>
          </Card>

          <Card className="bg-card/40 border border-border/40 p-4">
            <div className="flex items-start justify-between">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Rejections</p>
                <p className={`text-2xl font-bold ${totalRejected > 5 ? "text-status-warning" : "text-muted-foreground"}`}>
                  {totalRejected}
                </p>
                <p className="text-xs text-muted-foreground">over last {chartData.length} blocks</p>
              </div>
              <div className="p-2 rounded-lg bg-status-warning/10">
                {totalRejected > 5 ? <TrendingDown className="h-4 w-4 text-status-warning" /> : <Minus className="h-4 w-4 text-muted-foreground" />}
              </div>
            </div>
          </Card>

          <Card className="bg-card/40 border border-border/40 p-4">
            <div className="flex items-start justify-between">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">Timeouts</p>
                <p className={`text-2xl font-bold ${totalTimeout > 0 ? "text-status-warning" : "text-muted-foreground"}`}>
                  {totalTimeout}
                </p>
                <p className="text-xs text-muted-foreground">Unmet quorum votes</p>
              </div>
              <div className="p-2 rounded-lg bg-muted/10">
                {totalTimeout > 0 ? <TrendingDown className="h-4 w-4 text-status-warning" /> : <Minus className="h-4 w-4 text-muted-foreground" />}
              </div>
            </div>
          </Card>
        </div>
      </div>
    </div>
  )
}