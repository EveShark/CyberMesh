"use client"

import { ResponsiveContainer, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip } from "recharts"

export type VotePhase = "pre-prepare" | "prepare" | "commit" | "decide" | "none"

export interface PbftStatusProps {
  phase?: VotePhase
  leader?: string
  round?: number
  term?: number
  votes?: Array<{ node: string; vote: VotePhase }>
  updatedAt?: string
  isLoading?: boolean
  error?: unknown
}

export default function PbftLiveVisualizer({
  phase = "pre-prepare",
  leader,
  round,
  term,
  votes = [],
  updatedAt,
  isLoading,
  error,
}: PbftStatusProps) {
  const chartData = votes.map((vote) => ({
    node: vote.node,
    value: 1,
    phase: vote.vote,
  }))

  return (
    <div className="rounded-xl border bg-card/60 backdrop-blur-md p-4">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">PBFT Status</h3>
        <div className="text-xs text-muted-foreground">
          {isLoading ? "Loading..." : error ? "Error" : `Updated ${new Date(updatedAt ?? Date.now()).toLocaleTimeString()}`}
        </div>
      </div>
      <div className="mb-4 grid grid-cols-2 gap-3 md:grid-cols-4">
        <div className="rounded-lg border bg-background/60 p-3">
          <p className="text-xs text-muted-foreground">Phase</p>
          <p className="font-medium text-foreground capitalize">{phase}</p>
        </div>
        <div className="rounded-lg border bg-background/60 p-3">
          <p className="text-xs text-muted-foreground">Leader</p>
          <p className="font-medium text-foreground">{leader ?? "—"}</p>
        </div>
        <div className="rounded-lg border bg-background/60 p-3">
          <p className="text-xs text-muted-foreground">Round</p>
          <p className="font-medium text-foreground">{round ?? "—"}</p>
        </div>
        <div className="rounded-lg border bg-background/60 p-3">
          <p className="text-xs text-muted-foreground">Term</p>
          <p className="font-medium text-foreground">{term ?? "—"}</p>
        </div>
      </div>

      <div className="h-[260px]">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={chartData}>
            <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
            <XAxis dataKey="node" stroke="var(--muted-foreground)" />
            <YAxis stroke="var(--muted-foreground)" hide />
            <Tooltip
              contentStyle={{
                background: "var(--card)",
                border: "1px solid var(--border)",
                color: "var(--foreground)",
              }}
              labelStyle={{ color: "var(--muted-foreground)" }}
            />
            {/* Use OKLCH-safe variables directly */}
            <Bar dataKey="value" fill="var(--chart-1)" radius={[6, 6, 0, 0]} />
          </BarChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}
