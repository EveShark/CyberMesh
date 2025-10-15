"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { LineChart, Line, XAxis, YAxis, CartesianGrid } from "recharts"

// Mock data for decision timeline
const decisionData = [
  { time: "14:25", approved: 12, rejected: 1, timeout: 0 },
  { time: "14:26", approved: 15, rejected: 2, timeout: 1 },
  { time: "14:27", approved: 18, rejected: 1, timeout: 0 },
  { time: "14:28", approved: 14, rejected: 3, timeout: 2 },
  { time: "14:29", approved: 16, rejected: 1, timeout: 1 },
  { time: "14:30", approved: 13, rejected: 2, timeout: 3 },
  { time: "14:31", approved: 17, rejected: 1, timeout: 0 },
  { time: "14:32", approved: 19, rejected: 2, timeout: 1 },
]

const chartConfig = {
  approved: {
    label: "Approved",
    color: "var(--chart-1)",
  },
  rejected: {
    label: "Rejected",
    color: "var(--chart-4)",
  },
  timeout: {
    label: "Timeout",
    color: "var(--chart-3)",
  },
}

export function DecisionTimelineChart() {
  return (
    <Card className="glass-card">
      <CardHeader>
        <CardTitle className="text-xl font-semibold text-foreground">Decision Timeline</CardTitle>
      </CardHeader>
      <CardContent>
        <ChartContainer config={chartConfig} className="h-80">
          <LineChart data={decisionData}>
            <CartesianGrid strokeDasharray="3 3" />
            <XAxis dataKey="time" />
            <YAxis />
            <ChartTooltip content={<ChartTooltipContent />} />
            <Line
              type="monotone"
              dataKey="approved"
              stroke="var(--color-approved)"
              strokeWidth={2}
              dot={{ fill: "var(--color-approved)", strokeWidth: 2, r: 4 }}
              name="Approved"
            />
            <Line
              type="monotone"
              dataKey="rejected"
              stroke="var(--color-rejected)"
              strokeWidth={2}
              dot={{ fill: "var(--color-rejected)", strokeWidth: 2, r: 4 }}
              name="Rejected"
            />
            <Line
              type="monotone"
              dataKey="timeout"
              stroke="var(--color-timeout)"
              strokeWidth={2}
              dot={{ fill: "var(--color-timeout)", strokeWidth: 2, r: 4 }}
              name="Timeout"
            />
          </LineChart>
        </ChartContainer>
        {/* TODO: integrate GET /blockchain/stats */}
      </CardContent>
    </Card>
  )
}
