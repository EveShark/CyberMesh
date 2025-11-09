"use client"

import { useMemo } from "react"
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Loader2, TrendingUp, BarChart3 } from "lucide-react"

interface ProposalChartProps {
  proposals: Array<{ block: number; view?: number; timestamp: number; proposer?: string; hash?: string }>
  isLoading?: boolean
}

const COLOR_START = "--status-healthy"
const COLOR_END = "--color-consensus"

function colorValue(colorVar: string, alpha?: number) {
  if (alpha === undefined) {
    return `var(${colorVar})`
  }

  const clamped = Math.min(1, Math.max(0, alpha))
  const percent = (clamped * 100).toFixed(1).replace(/\.0$/, "")
  return `color-mix(in srgb, var(${colorVar}) ${percent}%, transparent)`
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
    const counts = chartData.map((d) => d.count)
    const maxCount = counts.length > 0 ? Math.max(...counts) : 0
    const timeSpanHours = chartData.length || 1
    const avgRate = Number((total / timeSpanHours).toFixed(1))

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
  const activityHours = Math.max(1, chartData.length)
  const insightMessage = stats.avgRate > 5
    ? "High proposal throughput indicates active consensus participation."
    : "Proposal volume within normal parameters; consensus steady."

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader>
        <CardTitle>Proposal Activity</CardTitle>
        <CardDescription>
          {proposals.length > 0 ? `Recent ${proposals.length} proposals` : "Recent proposal submissions"}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6 p-6">
        {hasData ? (
          <>
            <div className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
              <StatCard label="Total Proposals" colorVar="--status-healthy" value={stats.total} sublabel="all time" />
              <StatCard label="Peak Rate" colorVar="--color-consensus" value={stats.peakRate} sublabel="per hour" />
              <StatCard label="Average Rate" colorVar="--accent" value={stats.avgRate.toFixed(1)} sublabel="per hour" />
            </div>

            <div className="rounded-2xl border border-border/40 bg-background/70 p-4">
              <ResponsiveContainer width="100%" height={320}>
                <LineChart data={chartData}>
                  <defs>
                    <linearGradient id="proposal-line" x1="0" y1="0" x2="1" y2="0">
                      <stop offset="0%" stopColor={colorValue(COLOR_START)} />
                      <stop offset="100%" stopColor={colorValue(COLOR_END)} />
                    </linearGradient>
                    <linearGradient id="proposal-fill" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={colorValue(COLOR_START)} stopOpacity={0.28} />
                      <stop offset="100%" stopColor={colorValue(COLOR_END)} stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke={colorValue("--border", 0.3)} />
                  <XAxis
                    dataKey="hour"
                    stroke={colorValue("--muted-foreground")}
                    fontSize={11}
                    tickLine={false}
                    axisLine={false}
                  />
                  <YAxis
                    stroke={colorValue("--muted-foreground")}
                    fontSize={11}
                    tickLine={false}
                    axisLine={false}
                    allowDecimals={false}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: colorValue("--background"),
                      border: `1px solid ${colorValue("--border")}`,
                      borderRadius: "0.75rem",
                    }}
                    labelStyle={{ color: colorValue("--foreground"), fontWeight: 600 }}
                  />
                  <Line
                    type="monotone"
                    dataKey="count"
                    stroke="url(#proposal-line)"
                    strokeWidth={3}
                    dot={{ fill: colorValue(COLOR_START), r: 5, strokeWidth: 2, stroke: colorValue("--background") }}
                    activeDot={{ r: 7, strokeWidth: 2, stroke: colorValue(COLOR_START) }}
                    fill="url(#proposal-fill)"
                    strokeLinejoin="round"
                    strokeLinecap="round"
                    name="Proposals"
                  />
                </LineChart>
              </ResponsiveContainer>
            </div>

            <div className="rounded-2xl border border-border/40 bg-background/70 p-5">
              <div className="flex items-start gap-3">
                <div
                  className="rounded-xl border p-2"
                  style={{
                    background: `linear-gradient(135deg, ${colorValue(COLOR_START, 0.12)}, ${colorValue(COLOR_END, 0.05)})`,
                    borderColor: colorValue(COLOR_START, 0.3),
                  }}
                >
                  <TrendingUp className="h-5 w-5" style={{ color: colorValue(COLOR_START) }} />
                </div>
                <div className="space-y-1">
                  <p className="text-sm font-semibold text-foreground">Network Performance</p>
                  <p className="text-xs text-muted-foreground">{insightMessage}</p>
                  <p
                    className="text-xs"
                    style={{ color: colorValue("--muted-foreground", 0.8) }}
                  >
                    Tracking {chartData.length} time buckets (~{activityHours} hr{activityHours > 1 ? "s" : ""})
                  </p>
                </div>
              </div>
            </div>
          </>
        ) : (
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="text-center space-y-3">
              <BarChart3 className="mx-auto h-10 w-10 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">No proposal data available</p>
              <p className="text-xs text-muted-foreground">Waiting for consensus activity...</p>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

interface StatCardProps {
  label: string
  colorVar: string
  value: number | string
  sublabel: string
}

function StatCard({ label, colorVar, value, sublabel }: StatCardProps) {
  const accent = colorValue(colorVar)
  return (
    <div
      className="rounded-xl border border-transparent p-4 shadow-sm"
      style={{
        background: `linear-gradient(135deg, ${colorValue(colorVar, 0.16)}, ${colorValue(colorVar, 0.05)})`,
        borderColor: colorValue(colorVar, 0.3),
      }}
    >
      <p className="text-xs font-medium" style={{ color: accent }}>
        {label}
      </p>
      <p className="mt-2 font-mono text-3xl font-semibold text-foreground">{value}</p>
      <p className="text-xs text-muted-foreground">{sublabel}</p>
    </div>
  )
}
