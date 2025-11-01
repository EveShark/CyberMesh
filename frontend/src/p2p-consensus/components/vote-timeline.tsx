"use client"

import { useMemo } from "react"
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from "recharts"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Loader2 } from "lucide-react"

interface VoteTimelineProps {
  votes: Array<{ type: string; count: number; timestamp: number }>
  isLoading?: boolean
}

export default function VoteTimeline({ votes, isLoading }: VoteTimelineProps) {
  // Process vote data into time series
  const chartData = useMemo(() => {
    if (!votes || votes.length === 0) return []

    const voteMap = new Map<
      number,
      { timestamp: number; proposals: number; votes: number; commits: number; view_changes: number }
    >()

    votes.forEach((v) => {
      if (!voteMap.has(v.timestamp)) {
        voteMap.set(v.timestamp, { timestamp: v.timestamp, proposals: 0, votes: 0, commits: 0, view_changes: 0 })
      }

      const entry = voteMap.get(v.timestamp)!
      const type = v.type.toLowerCase()

      if (type.includes("proposal")) {
        entry.proposals += v.count
      } else if (type.includes("commit")) {
        entry.commits += v.count
      } else if (type.includes("view")) {
        entry.view_changes += v.count
      } else {
        entry.votes += v.count
      }
    })

    return Array.from(voteMap.values())
      .sort((a, b) => a.timestamp - b.timestamp)
      .map((entry) => ({
        ...entry,
        time: new Date(entry.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" }),
      }))
  }, [votes])

  // Calculate current activity (last bucket)
  const currentActivity = useMemo(() => {
    if (chartData.length === 0) {
      return { proposals: 0, votes: 0, commits: 0, view_changes: 0 }
    }
    return chartData[chartData.length - 1]
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
            <div className="text-center space-y-3">
              <Loader2 className="h-6 w-6 animate-spin text-primary mx-auto" />
              <p className="text-sm text-muted-foreground">Loading vote data...</p>
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
        <CardTitle>Vote Timeline</CardTitle>
        <CardDescription>Consensus activity (30s buckets)</CardDescription>
      </CardHeader>
      <CardContent className="p-6 space-y-4">
        {hasData ? (
          <>
            <ResponsiveContainer width="100%" height={300}>
              <AreaChart data={chartData}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" opacity={0.3} vertical={false} />
                <XAxis
                  dataKey="time"
                  stroke="hsl(var(--muted-foreground))"
                  fontSize={10}
                  tickLine={false}
                  axisLine={false}
                  interval="preserveStartEnd"
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
                <Legend
                  wrapperStyle={{ fontSize: "12px" }}
                  iconType="circle"
                />
                <Area
                  type="monotone"
                  dataKey="proposals"
                  stackId="1"
                  stroke="#10b981"
                  fill="#10b981"
                  fillOpacity={0.6}
                  name="Proposals"
                />
                <Area
                  type="monotone"
                  dataKey="votes"
                  stackId="1"
                  stroke="#3b82f6"
                  fill="#3b82f6"
                  fillOpacity={0.6}
                  name="Votes"
                />
                <Area
                  type="monotone"
                  dataKey="commits"
                  stackId="1"
                  stroke="#f59e0b"
                  fill="#f59e0b"
                  fillOpacity={0.6}
                  name="Commits"
                />
                <Area
                  type="monotone"
                  dataKey="view_changes"
                  stackId="1"
                  stroke="#ef4444"
                  fill="#ef4444"
                  fillOpacity={0.6}
                  name="View Changes"
                />
              </AreaChart>
            </ResponsiveContainer>

            {/* Current Activity Summary */}
            <div className="rounded-lg border border-border/30 bg-background/60 p-3 space-y-1">
              <p className="text-xs font-semibold text-foreground">Current Activity (last 30s):</p>
              <p className="text-xs text-muted-foreground">
                {currentActivity.votes} votes, {currentActivity.proposals} proposals, {currentActivity.commits} commits
                {currentActivity.view_changes > 0 && `, ${currentActivity.view_changes} view changes`}
              </p>
            </div>

            {/* Legend Manual */}
            <div className="flex items-center justify-center gap-6 text-xs flex-wrap">
              <div className="flex items-center gap-2">
                <div className="h-3 w-3 rounded-full bg-[#10b981]"></div>
                <span className="text-muted-foreground">Proposals</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="h-3 w-3 rounded-full bg-[#3b82f6]"></div>
                <span className="text-muted-foreground">Votes</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="h-3 w-3 rounded-full bg-[#f59e0b]"></div>
                <span className="text-muted-foreground">Commits</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="h-3 w-3 rounded-full bg-[#ef4444]"></div>
                <span className="text-muted-foreground">View Changes</span>
              </div>
            </div>
          </>
        ) : (
          <div className="flex items-center justify-center min-h-[300px]">
            <div className="text-center space-y-3">
              <p className="text-sm text-muted-foreground">No vote activity data available</p>
              <p className="text-xs text-muted-foreground">Waiting for consensus activity...</p>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
