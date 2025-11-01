"use client"

import { useEffect, useMemo, useState } from "react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { LineChart, Line, CartesianGrid, XAxis, YAxis, ResponsiveContainer } from "recharts"

interface TrafficSparklineProps {
  blockHeight?: number
  pendingTransactions?: number
}

interface HistoryPoint {
  timestamp: number
  blockHeight: number
  pendingTransactions: number
}

export function TrafficSparkline({ blockHeight, pendingTransactions }: TrafficSparklineProps) {
  const [history, setHistory] = useState<HistoryPoint[]>([])

  useEffect(() => {
    if (blockHeight === undefined || pendingTransactions === undefined) {
      return
    }

    setHistory((prev) => {
      const next = [...prev, { timestamp: Date.now(), blockHeight, pendingTransactions }]
      return next.slice(-50)
    })
  }, [blockHeight, pendingTransactions])

  const chartData = useMemo(
    () =>
      history.map((point) => ({
        time: new Date(point.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
        blockHeight: point.blockHeight,
        pendingTransactions: point.pendingTransactions,
      })),
    [history],
  )

  const latest = history[history.length - 1]
  const previous = history[history.length - 2]

  const heightDelta = latest && previous ? latest.blockHeight - previous.blockHeight : 0
  const pendingDelta = latest && previous ? latest.pendingTransactions - previous.pendingTransactions : 0

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Chain Activity</h2>
        <Badge variant="outline" className="text-sm">
          Last {chartData.length} samples
        </Badge>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card className="glass-card">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Latest Height</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold text-foreground">{latest?.blockHeight ?? "--"}</p>
            <p className="text-xs text-muted-foreground mt-1">Δ {heightDelta >= 0 ? "+" : ""}{heightDelta}</p>
          </CardContent>
        </Card>

        <Card className="glass-card">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Pending Transactions</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-bold text-foreground">{latest?.pendingTransactions ?? "--"}</p>
            <p className="text-xs text-muted-foreground mt-1">Δ {pendingDelta >= 0 ? "+" : ""}{pendingDelta}</p>
          </CardContent>
        </Card>
      </div>

      <Card className="glass-card">
        <CardHeader>
          <CardTitle className="text-lg text-foreground">Block Activity</CardTitle>
        </CardHeader>
        <CardContent className="h-64">
          {chartData.length > 1 ? (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData}>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.2} />
                <XAxis dataKey="time" stroke="var(--muted-foreground)" fontSize={12} tickMargin={8} />
                <YAxis stroke="var(--muted-foreground)" fontSize={12} width={60} />
                <ChartTooltip content={<ChartTooltipContent />} />
                <Line
                  type="monotone"
                  dataKey="blockHeight"
                  stroke="var(--chart-1)"
                  strokeWidth={2}
                  dot={false}
                  name="Block Height"
                />
                <Line
                  type="monotone"
                  dataKey="pendingTransactions"
                  stroke="var(--chart-2)"
                  strokeWidth={2}
                  dot={false}
                  name="Pending Tx"
                />
              </LineChart>
            </ResponsiveContainer>
          ) : (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              Waiting for metrics…
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
