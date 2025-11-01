"use client"

import { useMemo } from "react"
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Loader2 } from "lucide-react"

interface ProposalChartProps {
  proposals: Array<{ block: number; view?: number; timestamp: number; proposer?: string; hash?: string }>
  isLoading?: boolean
}

export default function ProposalChart({ proposals, isLoading }: ProposalChartProps) {
  // Group proposals by hour
  const chartData = useMemo(() => {
    if (!proposals || proposals.length === 0) return []

    const groupedByHour: Record<string, number> = {}

    proposals.forEach((p) => {
      const date = new Date(p.timestamp)
      const hourKey = `${date.getHours().toString().padStart(2, "0")}:00`
      groupedByHour[hourKey] = (groupedByHour[hourKey] || 0) + 1
    })

    return Object.entries(groupedByHour)
      .map(([hour, count]) => ({ hour, count }))
      .sort((a, b) => a.hour.localeCompare(b.hour))
  }, [proposals])

  // Calculate stats
  const stats = useMemo(() => {
    if (!proposals || proposals.length === 0) {
      return { total: 0, peakRate: 0, avgRate: 0 }
    }

    const total = proposals.length
    const counts = chartData.map(d => d.count)
    const maxCount = counts.length > 0 ? Math.max(...counts) : 0
    const timeSpanHours = chartData.length || 1
    const avgRate = (total / timeSpanHours).toFixed(1)

    return {
      total,
      peakRate: maxCount,
      avgRate,
    }
  }, [proposals, chartData])

  if (isLoading) {
    return (
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle>Proposal Activity</CardTitle>
          <CardDescription>Recent proposal submissions</CardDescription>
        </CardHeader>
        <CardContent className="p-6">
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="text-center space-y-3">
              <Loader2 className="h-6 w-6 animate-spin text-primary mx-auto" />
              <p className="text-sm text-muted-foreground">Loading proposal data...</p>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  const hasData = chartData.length > 0

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader>
        <CardTitle>Proposal Activity</CardTitle>
        <CardDescription>Recent proposal submissions (last {proposals.length})</CardDescription>
      </CardHeader>
      <CardContent className="p-6 space-y-4">
        {hasData ? (
          <>
            <ResponsiveContainer width="100%" height={300}>
              <LineChart data={chartData}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" opacity={0.3} vertical={false} />
                <XAxis
                  dataKey="hour"
                  stroke="hsl(var(--muted-foreground))"
                  fontSize={11}
                  tickLine={false}
                  axisLine={false}
                />
                <YAxis
                  stroke="hsl(var(--muted-foreground))"
                  fontSize={11}
                  tickLine={false}
                  axisLine={false}
                  allowDecimals={false}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: "hsl(var(--background))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: "8px",
                  }}
                  labelStyle={{ color: "hsl(var(--foreground))" }}
                />
                <Line
                  type="monotone"
                  dataKey="count"
                  stroke="#10b981"
                  strokeWidth={2}
                  dot={{ fill: "#10b981", r: 4 }}
                  activeDot={{ r: 6 }}
                  name="Proposals"
                />
              </LineChart>
            </ResponsiveContainer>

            {/* Stats */}
            <div className="grid grid-cols-3 gap-4">
              <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                <p className="text-xs text-muted-foreground">Total</p>
                <p className="text-2xl font-bold text-foreground">{stats.total}</p>
              </div>
              <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                <p className="text-xs text-muted-foreground">Peak Rate</p>
                <p className="text-2xl font-bold text-foreground">{stats.peakRate}/hr</p>
              </div>
              <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                <p className="text-xs text-muted-foreground">Avg Rate</p>
                <p className="text-2xl font-bold text-foreground">{stats.avgRate}/hr</p>
              </div>
            </div>
          </>
        ) : (
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="text-center space-y-3">
              <p className="text-sm text-muted-foreground">No proposal data available</p>
              <p className="text-xs text-muted-foreground">Waiting for consensus activity...</p>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
