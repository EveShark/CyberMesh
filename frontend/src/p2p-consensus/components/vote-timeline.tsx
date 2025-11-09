"use client"

import { useMemo, type ComponentType } from "react"
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Loader2, Activity, GitCommit, Zap, BarChart3, RefreshCw } from "lucide-react"

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

const SERIES = {
  proposals: { key: "proposals", colorVar: "--chart-1", label: "Proposals" },
  votes: { key: "votes", colorVar: "--chart-2", label: "Votes" },
  commits: { key: "commits", colorVar: "--chart-3", label: "Commits" },
  view_changes: { key: "view_changes", colorVar: "--chart-4", label: "View Changes" },
} as const

function colorValue(colorVar: string, alpha?: number) {
  if (alpha === undefined) {
    return `var(${colorVar})`
  }

  const clamped = Math.min(1, Math.max(0, alpha))
  const percent = (clamped * 100).toFixed(1).replace(/\.0$/, "")
  return `color-mix(in srgb, var(${colorVar}) ${percent}%, transparent)`
}

export default function VoteTimeline({ votes, isLoading }: VoteTimelineProps) {
  const chartData = useMemo<VoteBucket[]>(() => {
    if (!votes || votes.length === 0) return []

    const buckets = new Map<number, VoteBucket>()

    votes.forEach((vote) => {
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
  }, [votes])

  const currentActivity = useMemo(() => {
    if (chartData.length === 0) {
      return { proposals: 0, votes: 0, commits: 0, view_changes: 0, total: 0 }
    }

    const last = chartData[chartData.length - 1]
    return {
      ...last,
      total: last.proposals + last.votes + last.commits + last.view_changes,
    }
  }, [chartData])

  const totalActivity = useMemo(() => {
    return chartData.reduce((sum, bucket) => sum + bucket.proposals + bucket.votes + bucket.commits + bucket.view_changes, 0)
  }, [chartData])

  if (isLoading) {
    return (
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle>Vote Timeline</CardTitle>
          <CardDescription>Consensus activity (30s buckets)</CardDescription>
        </CardHeader>
        <CardContent className="p-6">
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="space-y-3 text-center">
              <Loader2 className="mx-auto h-6 w-6 animate-spin text-primary" />
              <p className="text-sm text-muted-foreground">Loading vote data...</p>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  if (chartData.length === 0) {
    return (
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle>Vote Timeline</CardTitle>
          <CardDescription>Consensus activity (30s buckets)</CardDescription>
        </CardHeader>
        <CardContent className="p-6">
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="space-y-3 text-center">
              <Activity className="mx-auto h-10 w-10 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">No vote activity data available</p>
              <p className="text-xs text-muted-foreground">Waiting for consensus activity...</p>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader>
        <CardTitle>Vote Timeline</CardTitle>
        <CardDescription>Consensus activity (30s buckets)</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6 p-6">
        <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
          <StatCard icon={GitCommit} label="Proposals" colorVar={SERIES.proposals.colorVar} value={currentActivity.proposals} />
          <StatCard icon={Zap} label="Votes" colorVar={SERIES.votes.colorVar} value={currentActivity.votes} />
          <StatCard icon={BarChart3} label="Commits" colorVar={SERIES.commits.colorVar} value={currentActivity.commits} />
          <StatCard icon={RefreshCw} label="View Changes" colorVar={SERIES.view_changes.colorVar} value={currentActivity.view_changes} />
        </div>

        <div className="rounded-2xl border border-border/40 bg-background/70 p-4">
          <ResponsiveContainer width="100%" height={320}>
            <AreaChart data={chartData}>
              <defs>
                {Object.values(SERIES).map(({ key, colorVar }) => (
                  <linearGradient key={key} id={`gradient-${key}`} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={colorValue(colorVar)} stopOpacity={0.35} />
                    <stop offset="95%" stopColor={colorValue(colorVar)} stopOpacity={0} />
                  </linearGradient>
                ))}
              </defs>
              <CartesianGrid strokeDasharray="3 3" stroke={colorValue("--border", 0.3)} />
              <XAxis
                dataKey="time"
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
              {Object.values(SERIES).map(({ key, colorVar, label }) => (
                <Area
                  key={key}
                  type="monotone"
                  dataKey={key}
                  stackId="1"
                  stroke={colorValue(colorVar)}
                  strokeWidth={2}
                  fill={`url(#gradient-${key})`}
                  name={label}
                  dot={false}
                />
              ))}
            </AreaChart>
          </ResponsiveContainer>
        </div>

        <div className="grid gap-4 lg:grid-cols-[2fr,1fr]">
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Current activity (last 30s)</p>
            <p className="mt-2 text-sm text-muted-foreground">
              {currentActivity.votes} votes · {currentActivity.proposals} proposals · {currentActivity.commits} commits
              {currentActivity.view_changes > 0 ? ` · ${currentActivity.view_changes} view changes` : ""}
            </p>
          </div>
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Total activity</p>
            <p className="mt-2 text-2xl font-bold text-foreground">{totalActivity}</p>
            <p className="text-xs text-muted-foreground">Across {chartData.length} time buckets</p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

interface StatCardProps {
  icon: ComponentType<{ className?: string }>
  label: string
  colorVar: string
  value: number
}

function StatCard({ icon: Icon, label, colorVar, value }: StatCardProps) {
  const color = colorValue(colorVar)
  return (
    <div
      className="flex flex-col justify-between rounded-xl border border-transparent p-3 shadow-sm"
      style={{
        background: `linear-gradient(135deg, ${colorValue(colorVar, 0.18)}, ${colorValue(colorVar, 0.06)})`,
        borderColor: colorValue(colorVar, 0.35),
      }}
    >
      <div className="mb-1 flex items-center gap-2">
        <Icon className="h-4 w-4" style={{ color }} />
        <p className="text-xs font-medium" style={{ color }}>
          {label}
        </p>
      </div>
      <p className="font-mono text-2xl font-semibold text-foreground">{value}</p>
      <p className="text-xs text-muted-foreground">last 30s</p>
    </div>
  )
}
