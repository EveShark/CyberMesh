import { useMemo, useState, type ComponentType } from "react"
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend } from "recharts"
import { Loader2, Activity, GitCommit, Zap, BarChart3, RefreshCw, Vote } from "lucide-react"
import { TimeWindowToggle, type TimeWindow, filterByTimeWindow, getTimeWindowLabel, DEFAULT_TIME_WINDOW } from "@/components/ui/time-window-toggle"

interface VoteTimelineProps {
  votes: Array<{ type: string; count: number; timestamp: number }>
  isLoading?: boolean
}

interface VoteBucket {
  timestamp: number
  proposals: number
  votes: number
  commits: number
  view_changes: number
  time: string
}

// Stacked area chart series colors
const SERIES = {
  proposals: { key: "proposals", colorVar: "--chart-1", label: "Proposals" },
  votes: { key: "votes", colorVar: "--chart-2", label: "Votes" },
  commits: { key: "commits", colorVar: "--chart-3", label: "Commits" },
  view_changes: { key: "view_changes", colorVar: "--chart-4", label: "View Changes" },
} as const

function colorValue(colorVar: string, alpha?: number) {
  if (alpha === undefined) {
    return `hsl(var(${colorVar}))`
  }

  const clamped = Math.min(1, Math.max(0, alpha))
  const percent = (clamped * 100).toFixed(1).replace(/\.0$/, "")
  return `color-mix(in srgb, hsl(var(${colorVar})) ${percent}%, transparent)`
}

export default function VoteTimeline({ votes, isLoading }: VoteTimelineProps) {
  const [timeWindow, setTimeWindow] = useState<TimeWindow>(DEFAULT_TIME_WINDOW)

  // Filter votes by selected time window
  const filteredVotes = useMemo(() => {
    return filterByTimeWindow(votes || [], timeWindow)
  }, [votes, timeWindow])

  const chartData = useMemo<VoteBucket[]>(() => {
    if (!filteredVotes || filteredVotes.length === 0) return []

    const buckets = new Map<number, VoteBucket>()

    filteredVotes.forEach((vote) => {
      if (!buckets.has(vote.timestamp)) {
        buckets.set(vote.timestamp, {
          timestamp: vote.timestamp,
          proposals: 0,
          votes: 0,
          commits: 0,
          view_changes: 0,
          time: "",
        })
      }

      const entry = buckets.get(vote.timestamp)!
      const type = vote.type.toLowerCase()

      if (type.includes("proposal")) entry.proposals += vote.count
      else if (type.includes("commit")) entry.commits += vote.count
      else if (type.includes("view")) entry.view_changes += vote.count
      else entry.votes += vote.count
    })

    return Array.from(buckets.values())
      .sort((a, b) => a.timestamp - b.timestamp)
      .map((bucket) => ({
        ...bucket,
        time: new Date(bucket.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" }),
      }))
  }, [filteredVotes])

  // Calculate totals across all filtered data (not just last bucket)
  const windowTotals = useMemo(() => {
    if (chartData.length === 0) {
      return { proposals: 0, votes: 0, commits: 0, view_changes: 0, total: 0 }
    }

    const totals = chartData.reduce(
      (acc, bucket) => ({
        proposals: acc.proposals + bucket.proposals,
        votes: acc.votes + bucket.votes,
        commits: acc.commits + bucket.commits,
        view_changes: acc.view_changes + bucket.view_changes,
      }),
      { proposals: 0, votes: 0, commits: 0, view_changes: 0 }
    );

    return {
      ...totals,
      total: totals.proposals + totals.votes + totals.commits + totals.view_changes,
    }
  }, [chartData])

  const totalActivity = useMemo(() => {
    return chartData.reduce((sum, bucket) => sum + bucket.proposals + bucket.votes + bucket.commits + bucket.view_changes, 0)
  }, [chartData])

  if (isLoading) {
    return (
      <div className="glass-frost rounded-lg p-6 space-y-6">
        <div className="flex items-start gap-3">
          <div className="rounded-lg border border-primary/30 bg-primary/10 p-2">
            <Vote className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-foreground">Vote Timeline</h3>
            <p className="text-sm text-muted-foreground">Consensus activity (30s buckets)</p>
          </div>
        </div>
        <div className="flex items-center justify-center min-h-[300px]">
          <div className="space-y-3 text-center">
            <Loader2 className="mx-auto h-6 w-6 animate-spin text-primary" />
            <p className="text-sm text-muted-foreground">Loading vote data...</p>
          </div>
        </div>
      </div>
    )
  }

  if (chartData.length === 0) {
    return (
      <div className="glass-frost rounded-lg p-6 space-y-6">
        <div className="flex items-start gap-3">
          <div className="rounded-lg border border-primary/30 bg-primary/10 p-2">
            <Vote className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-foreground">Vote Timeline</h3>
            <p className="text-sm text-muted-foreground">Consensus activity (30s buckets)</p>
          </div>
        </div>
        <div className="flex items-center justify-center min-h-[300px]">
          <div className="space-y-3 text-center">
            <Activity className="mx-auto h-10 w-10 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">No vote activity data available</p>
            <p className="text-xs text-muted-foreground">Waiting for consensus activity...</p>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="glass-frost rounded-lg p-6 space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <div className="rounded-lg border border-primary/30 bg-primary/10 p-2">
            <Vote className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-foreground">Vote Timeline</h3>
            <p className="text-sm text-muted-foreground">
              {filteredVotes.length > 0
                ? `${chartData.length} buckets | ${getTimeWindowLabel(timeWindow)}`
                : "Consensus activity (30s buckets)"}
            </p>
          </div>
        </div>
        <TimeWindowToggle value={timeWindow} onChange={setTimeWindow} />
      </div>

      {/* Key Insights */}
      <div>
        <p className="text-sm font-semibold text-foreground mb-3">Key Insights</p>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          <div className="rounded-xl border border-border/40 bg-background/70 p-3">
            <div className="flex items-center gap-2 mb-1">
              <GitCommit className="h-4 w-4" style={{ color: colorValue(SERIES.proposals.colorVar) }} />
              <p className="text-xs" style={{ color: colorValue(SERIES.proposals.colorVar) }}>Proposals</p>
            </div>
            <p className="text-2xl font-bold text-foreground">{windowTotals.proposals}</p>
            <p className="text-xs text-muted-foreground">in window</p>
          </div>
          <div className="rounded-xl border border-border/40 bg-background/70 p-3">
            <div className="flex items-center gap-2 mb-1">
              <Zap className="h-4 w-4" style={{ color: colorValue(SERIES.votes.colorVar) }} />
              <p className="text-xs" style={{ color: colorValue(SERIES.votes.colorVar) }}>Votes</p>
            </div>
            <p className="text-2xl font-bold text-foreground">{windowTotals.votes}</p>
            <p className="text-xs text-muted-foreground">in window</p>
          </div>
          <div className="rounded-xl border border-border/40 bg-background/70 p-3">
            <div className="flex items-center gap-2 mb-1">
              <BarChart3 className="h-4 w-4" style={{ color: colorValue(SERIES.commits.colorVar) }} />
              <p className="text-xs" style={{ color: colorValue(SERIES.commits.colorVar) }}>Commits</p>
            </div>
            <p className="text-2xl font-bold text-foreground">{windowTotals.commits}</p>
            <p className="text-xs text-muted-foreground">in window</p>
          </div>
          <div className="rounded-xl border border-border/40 bg-background/70 p-3">
            <div className="flex items-center gap-2 mb-1">
              <RefreshCw className="h-4 w-4" style={{ color: colorValue(SERIES.view_changes.colorVar) }} />
              <p className="text-xs" style={{ color: colorValue(SERIES.view_changes.colorVar) }}>View Changes</p>
            </div>
            <p className="text-2xl font-bold text-foreground">{windowTotals.view_changes}</p>
            <p className="text-xs text-muted-foreground">in window</p>
          </div>
        </div>
      </div>

      {/* Chart */}
      <div className="rounded-xl border border-border/40 bg-background/70 p-4">
        <ResponsiveContainer width="100%" height={320}>
          <LineChart data={chartData}>
            <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border) / 0.3)" vertical={false} />
            <XAxis
              dataKey="time"
              stroke={colorValue("--muted-foreground")}
              fontSize={11}
              tickLine={false}
              axisLine={false}
            />
            {/* Left Y-axis for smaller values (proposals, votes, commits) */}
            <YAxis
              yAxisId="left"
              stroke={colorValue("--muted-foreground")}
              fontSize={11}
              tickLine={false}
              axisLine={false}
              allowDecimals={false}
            />
            {/* Right Y-axis for larger values (view_changes) */}
            <YAxis
              yAxisId="right"
              orientation="right"
              stroke={colorValue(SERIES.view_changes.colorVar)}
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
            {/* Proposals, Votes, Commits use left axis */}
            <Line
              yAxisId="left"
              type="monotone"
              dataKey="proposals"
              stroke={colorValue(SERIES.proposals.colorVar)}
              strokeWidth={2}
              dot={{ r: 3, fill: colorValue(SERIES.proposals.colorVar) }}
              activeDot={{ r: 5 }}
              name={SERIES.proposals.label}
            />
            <Line
              yAxisId="left"
              type="monotone"
              dataKey="votes"
              stroke={colorValue(SERIES.votes.colorVar)}
              strokeWidth={2}
              dot={{ r: 3, fill: colorValue(SERIES.votes.colorVar) }}
              activeDot={{ r: 5 }}
              name={SERIES.votes.label}
            />
            <Line
              yAxisId="left"
              type="monotone"
              dataKey="commits"
              stroke={colorValue(SERIES.commits.colorVar)}
              strokeWidth={2}
              dot={{ r: 3, fill: colorValue(SERIES.commits.colorVar) }}
              activeDot={{ r: 5 }}
              name={SERIES.commits.label}
            />
            {/* View Changes uses right axis (larger scale) */}
            <Line
              yAxisId="right"
              type="monotone"
              dataKey="view_changes"
              stroke={colorValue(SERIES.view_changes.colorVar)}
              strokeWidth={2}
              dot={{ r: 3, fill: colorValue(SERIES.view_changes.colorVar) }}
              activeDot={{ r: 5 }}
              name={SERIES.view_changes.label}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>

      {/* Activity Summary */}
      <div className="grid gap-4 lg:grid-cols-[2fr,1fr]">
        <div className="rounded-xl border border-border/40 bg-background/70 p-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Activity breakdown in window</p>
          <p className="mt-2 text-sm text-muted-foreground">
            {windowTotals.votes} votes · {windowTotals.proposals} proposals · {windowTotals.commits} commits
            {windowTotals.view_changes > 0 ? ` · ${windowTotals.view_changes} view changes` : ""}
          </p>
        </div>
        <div className="rounded-xl border border-border/40 bg-background/70 p-4">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Total activity</p>
          <p className="mt-2 text-2xl font-bold text-foreground">{totalActivity}</p>
          <p className="text-xs text-muted-foreground">Across {chartData.length} time buckets</p>
        </div>
      </div>
    </div>
  )
}