import { useMemo, useState } from "react"
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend } from "recharts"
import { Loader2, TrendingUp, BarChart3, Send } from "lucide-react"
import { TimeWindowToggle, type TimeWindow, filterByTimeWindow, getTimeWindowLabel, DEFAULT_TIME_WINDOW } from "@/components/ui/time-window-toggle"

interface ProposalChartProps {
  proposals: Array<{ block: number; view?: number; timestamp: number; proposer?: string; hash?: string }>
  isLoading?: boolean
}

// Line gradient: start with healthy green, end with consensus purple
const COLOR_START = "--status-healthy"
const COLOR_END = "--color-consensus"
// Stat card colors
const COLOR_PEAK = "--color-consensus"
const COLOR_AVG = "--accent"

function colorValue(colorVar: string, alpha?: number) {
  if (alpha === undefined) {
    return `hsl(var(${colorVar}))`
  }

  const clamped = Math.min(1, Math.max(0, alpha))
  const percent = (clamped * 100).toFixed(1).replace(/\.0$/, "")
  return `color-mix(in srgb, hsl(var(${colorVar})) ${percent}%, transparent)`
}

export default function ProposalChart({ proposals, isLoading }: ProposalChartProps) {
  const [timeWindow, setTimeWindow] = useState<TimeWindow>(DEFAULT_TIME_WINDOW)

  // Filter proposals by selected time window
  const filteredProposals = useMemo(() => {
    return filterByTimeWindow(proposals || [], timeWindow)
  }, [proposals, timeWindow])

  // Group proposals by hour
  const chartData = useMemo(() => {
    if (!filteredProposals || filteredProposals.length === 0) return []

    const groupedByHour: Record<string, number> = {}

    filteredProposals.forEach((p) => {
      const date = new Date(p.timestamp)
      const hourKey = `${date.getMonth() + 1}/${date.getDate()} ${date.getHours().toString().padStart(2, "0")}:00`
      groupedByHour[hourKey] = (groupedByHour[hourKey] || 0) + 1
    })

    return Object.entries(groupedByHour)
      .map(([hour, count]) => ({ hour, count }))
      .sort((a, b) => a.hour.localeCompare(b.hour))
  }, [filteredProposals])

  // Calculate stats
  const stats = useMemo(() => {
    if (!filteredProposals || filteredProposals.length === 0) {
      return { total: 0, peakRate: 0, avgRate: 0 }
    }

    const total = filteredProposals.length
    const counts = chartData.map((d) => d.count)
    const maxCount = counts.length > 0 ? Math.max(...counts) : 0
    const timeSpanHours = chartData.length || 1
    const avgRate = Number((total / timeSpanHours).toFixed(1))

    return {
      total,
      peakRate: maxCount,
      avgRate,
    }
  }, [filteredProposals, chartData])

  if (isLoading) {
    return (
      <div className="glass-frost rounded-lg p-6 space-y-6">
        <div className="flex items-start gap-3">
          <div className="rounded-lg border border-primary/30 bg-primary/10 p-2">
            <Send className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-foreground">Proposal Activity</h3>
            <p className="text-sm text-muted-foreground">Recent proposal submissions</p>
          </div>
        </div>
        <div className="flex items-center justify-center min-h-[300px]">
          <div className="text-center space-y-3">
            <Loader2 className="h-6 w-6 animate-spin text-primary mx-auto" />
            <p className="text-sm text-muted-foreground">Loading proposal data...</p>
          </div>
        </div>
      </div>
    )
  }

  const hasData = chartData.length > 0
  const activityHours = Math.max(1, chartData.length)
  const insightMessage = stats.avgRate > 5
    ? "High proposal throughput indicates active consensus participation."
    : "Proposal volume within normal parameters; consensus steady."

  return (
    <div className="glass-frost rounded-lg p-6 space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <div className="rounded-lg border border-primary/30 bg-primary/10 p-2">
            <Send className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-foreground">Proposal Activity</h3>
            <p className="text-sm text-muted-foreground">
              {filteredProposals.length > 0 ? `${filteredProposals.length} proposals | ${getTimeWindowLabel(timeWindow)}` : "Recent proposal submissions"}
            </p>
          </div>
        </div>
        <TimeWindowToggle value={timeWindow} onChange={setTimeWindow} />
      </div>

      {hasData ? (
        <>
          {/* Key Insights */}
          <div>
            <p className="text-sm font-semibold text-foreground mb-3">Key Insights</p>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div className="rounded-xl border border-border/40 bg-background/70 p-4">
                <p className="text-xs text-muted-foreground">Total Proposals</p>
                <p className="mt-1 text-2xl font-bold" style={{ color: colorValue(COLOR_START) }}>{stats.total}</p>
                <p className="text-xs text-muted-foreground">in window</p>
              </div>
              <div className="rounded-xl border border-border/40 bg-background/70 p-4">
                <p className="text-xs text-muted-foreground">Peak Rate</p>
                <p className="mt-1 text-2xl font-bold" style={{ color: colorValue(COLOR_PEAK) }}>{stats.peakRate}</p>
                <p className="text-xs text-muted-foreground">per hour</p>
              </div>
              <div className="rounded-xl border border-border/40 bg-background/70 p-4">
                <p className="text-xs text-muted-foreground">Average Rate</p>
                <p className="mt-1 text-2xl font-bold" style={{ color: colorValue(COLOR_AVG) }}>{stats.avgRate.toFixed(1)}</p>
                <p className="text-xs text-muted-foreground">per hour</p>
              </div>
            </div>
          </div>

          {/* Chart */}
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <ResponsiveContainer width="100%" height={320}>
              <AreaChart data={chartData}>
                <defs>
                  <linearGradient id="proposal-line" x1="0" y1="0" x2="1" y2="0">
                    <stop offset="0%" stopColor={colorValue(COLOR_START)} />
                    <stop offset="100%" stopColor={colorValue(COLOR_END)} />
                  </linearGradient>
                  <linearGradient id="proposal-fill" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={colorValue(COLOR_START)} stopOpacity={0.4} />
                    <stop offset="100%" stopColor={colorValue(COLOR_END)} stopOpacity={0.05} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border) / 0.3)" vertical={false} />
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
                <Legend
                  wrapperStyle={{ paddingTop: "1rem" }}
                  formatter={(value) => <span className="text-xs text-muted-foreground">{value}</span>}
                />
                <Area
                  type="monotone"
                  dataKey="count"
                  stroke="url(#proposal-line)"
                  strokeWidth={3}
                  fill="url(#proposal-fill)"
                  dot={{ fill: colorValue(COLOR_START), r: 5, strokeWidth: 2, stroke: colorValue("--background") }}
                  activeDot={{ r: 7, strokeWidth: 2, stroke: colorValue(COLOR_START) }}
                  name="Proposals"
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>

          {/* Network Performance Insight */}
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <div className="flex items-start gap-3">
              <div
                className="rounded-lg border p-2"
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
                <p className="text-xs text-muted-foreground/80">
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
    </div>
  )
}