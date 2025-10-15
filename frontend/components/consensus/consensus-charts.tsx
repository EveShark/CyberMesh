"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, LineChart, Line } from "recharts"
import { TrendingUp, BarChart3 } from "lucide-react"

// TODO: integrate GET /consensus/metrics
const mockProposalData = [
  { round: "R1240", proposals: 12, accepted: 11, rejected: 1 },
  { round: "R1241", proposals: 15, accepted: 14, rejected: 1 },
  { round: "R1242", proposals: 8, accepted: 8, rejected: 0 },
  { round: "R1243", proposals: 18, accepted: 16, rejected: 2 },
  { round: "R1244", proposals: 22, accepted: 20, rejected: 2 },
  { round: "R1245", proposals: 14, accepted: 13, rejected: 1 },
  { round: "R1246", proposals: 19, accepted: 18, rejected: 1 },
  { round: "R1247", proposals: 16, accepted: 15, rejected: 1 },
]

const mockVoteTimelineData = [
  { time: "14:20", votes: 8, decisions: 1, latency: 120 },
  { time: "14:22", votes: 11, decisions: 1, latency: 95 },
  { time: "14:24", votes: 9, decisions: 1, latency: 110 },
  { time: "14:26", votes: 12, decisions: 2, latency: 85 },
  { time: "14:28", votes: 15, decisions: 2, latency: 75 },
  { time: "14:30", votes: 10, decisions: 1, latency: 105 },
  { time: "14:32", votes: 13, decisions: 2, latency: 90 },
  { time: "14:34", votes: 11, decisions: 1, latency: 100 },
]

const proposalChartConfig = {
  accepted: {
    label: "Accepted",
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

export function ConsensusCharts() {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
      {/* Proposal Counts Chart */}
      <Card className="glass-card border-0">
        <CardHeader>
          <CardTitle className="text-lg font-semibold text-foreground flex items-center gap-2">
            <BarChart3 className="h-5 w-5 text-primary" />
            Proposal Counts per Round
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ChartContainer config={proposalChartConfig} className="h-80">
            <BarChart data={mockProposalData} margin={{ top: 20, right: 30, left: 20, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="round" />
              <YAxis />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Bar dataKey="accepted" fill="var(--chart-2)" name="Accepted" radius={[2, 2, 0, 0]} />
              <Bar dataKey="rejected" fill="var(--chart-1)" name="Rejected" radius={[2, 2, 0, 0]} />
            </BarChart>
          </ChartContainer>
        </CardContent>
      </Card>

      {/* Vote/Decision Timeline Chart */}
      <Card className="glass-card border-0">
        <CardHeader>
          <CardTitle className="text-lg font-semibold text-foreground flex items-center gap-2">
            <TrendingUp className="h-5 w-5 text-primary" />
            Vote & Decision Timeline
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ChartContainer config={timelineChartConfig} className="h-80">
            <LineChart data={mockVoteTimelineData} margin={{ top: 20, right: 30, left: 20, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="time" />
              <YAxis />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Line
                type="monotone"
                dataKey="votes"
                stroke="var(--chart-3)"
                strokeWidth={2}
                dot={{ fill: "var(--chart-3)", strokeWidth: 2, r: 4 }}
                name="Votes"
              />
              <Line
                type="monotone"
                dataKey="decisions"
                stroke="var(--chart-4)"
                strokeWidth={2}
                dot={{ fill: "var(--chart-4)", strokeWidth: 2, r: 4 }}
                name="Decisions"
              />
            </LineChart>
          </ChartContainer>
        </CardContent>
      </Card>
    </div>
  )
}
