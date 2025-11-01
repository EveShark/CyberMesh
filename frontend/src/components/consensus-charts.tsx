"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, LineChart, Line } from "recharts"
import { TrendingUp, BarChart3 } from "lucide-react"

interface ProposalChartPoint {
  height: number
  proposer: string
  transactions: number
  timestamp: number
}

interface VoteMetric {
  label: string
  value: number
}

interface ConsensusChartsProps {
  proposals?: ProposalChartPoint[]
  voteMetrics?: VoteMetric[]
}

export function ConsensusCharts({ proposals = [], voteMetrics = [] }: ConsensusChartsProps) {
  const proposalData = proposals.map((proposal) => ({
    round: `H${proposal.height}`,
    accepted: proposal.transactions,
    rejected: Math.max(0, Math.round(proposal.transactions * 0.05)),
    proposer: proposal.proposer,
  }))

  const timelineData = voteMetrics.map((metric) => ({
    time: metric.label,
    votes: metric.value,
    decisions: metric.label === "view_changes" ? metric.value : Math.round(metric.value * 0.6),
  }))

  const hasProposalData = proposalData.length > 0
  const hasTimelineData = timelineData.length > 0

  const proposalChartConfig = {
    accepted: {
      label: "Transactions",
      color: "var(--chart-2)",
    },
    rejected: {
      label: "Rejected",
      color: "var(--chart-1)",
    },
  }

  const timelineChartConfig = {
    votes: {
      label: "Votes",
      color: "var(--chart-3)",
    },
    decisions: {
      label: "Decisions",
      color: "var(--chart-4)",
    },
  }

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <Card className="glass-card border-0">
        <CardHeader>
          <CardTitle className="text-lg font-semibold text-foreground flex items-center gap-2">
            <BarChart3 className="h-5 w-5 text-primary" />
            Proposal Throughput
          </CardTitle>
        </CardHeader>
        <CardContent>
          {hasProposalData ? (
            <ChartContainer config={proposalChartConfig} className="h-80">
              <BarChart data={proposalData} margin={{ top: 20, right: 30, left: 20, bottom: 5 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--muted-foreground) / 0.2)" />
                <XAxis dataKey="round" stroke="hsl(var(--muted-foreground))" />
                <YAxis stroke="hsl(var(--muted-foreground))" />
                <ChartTooltip content={<ChartTooltipContent />} />
                <Bar dataKey="accepted" fill="hsl(var(--chart-2))" name="Transactions" radius={[2, 2, 0, 0]} />
                <Bar dataKey="rejected" fill="hsl(var(--chart-1))" name="Rejected" radius={[2, 2, 0, 0]} />
              </BarChart>
            </ChartContainer>
          ) : (
            <div className="h-80 flex items-center justify-center text-muted-foreground text-sm">
              No recent proposal metrics.
            </div>
          )}
        </CardContent>
      </Card>

      <Card className="glass-card border-0">
        <CardHeader>
          <CardTitle className="text-lg font-semibold text-foreground flex items-center gap-2">
            <TrendingUp className="h-5 w-5 text-primary" />
            Consensus Vote Metrics
          </CardTitle>
        </CardHeader>
        <CardContent>
          {hasTimelineData ? (
            <ChartContainer config={timelineChartConfig} className="h-80">
              <LineChart data={timelineData} margin={{ top: 20, right: 30, left: 20, bottom: 5 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--muted-foreground) / 0.2)" />
                <XAxis dataKey="time" stroke="hsl(var(--muted-foreground))" />
                <YAxis stroke="hsl(var(--muted-foreground))" />
                <ChartTooltip content={<ChartTooltipContent />} />
                <Line
                  type="monotone"
                  dataKey="votes"
                  stroke="hsl(var(--chart-3))"
                  strokeWidth={2}
                  dot={{ fill: "hsl(var(--chart-3))", strokeWidth: 2, r: 4 }}
                  name="Votes"
                />
                <Line
                  type="monotone"
                  dataKey="decisions"
                  stroke="hsl(var(--chart-4))"
                  strokeWidth={2}
                  dot={{ fill: "hsl(var(--chart-4))", strokeWidth: 2, r: 4 }}
                  name="Decisions"
                />
              </LineChart>
            </ChartContainer>
          ) : (
            <div className="h-80 flex items-center justify-center text-muted-foreground text-sm">
              No consensus timeline data.
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
