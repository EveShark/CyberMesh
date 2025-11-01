"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { LineChart, Line, XAxis, YAxis, CartesianGrid } from "recharts"

import type { DecisionPoint } from "@/lib/timeline-store"

const chartConfig = {
  approved: {
    label: "Approved",
    color: "hsl(var(--chart-1))",
  },
  rejected: {
    label: "Rejected",
    color: "hsl(var(--chart-4))",
  },
  timeout: {
    label: "Timeout",
    color: "hsl(var(--chart-3))",
  },
}

const lineColorMap = {
  approved: "hsl(var(--chart-1))",
  rejected: "hsl(var(--chart-4))",
  timeout: "hsl(var(--chart-3))",
}

interface DecisionTimelineChartProps {
  data: DecisionPoint[]
}

export function DecisionTimelineChart({ data }: DecisionTimelineChartProps) {
  const hasData = data.length > 0

  return (
    <Card className="glass-card">
      <CardHeader>
        <CardTitle className="text-xl font-semibold text-foreground">Decision Timeline</CardTitle>
      </CardHeader>
      <CardContent>
        {hasData ? (
          <ChartContainer config={chartConfig} className="h-80">
            <LineChart data={data}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--muted-foreground) / 0.2)" />
              <XAxis dataKey="time" stroke="hsl(var(--muted-foreground))" />
              <YAxis stroke="hsl(var(--muted-foreground))" />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Line
                type="monotone"
                dataKey="approved"
                stroke={lineColorMap.approved}
                strokeWidth={2}
                dot={{ fill: lineColorMap.approved, strokeWidth: 2, r: 4 }}
                name="Approved"
              />
              <Line
                type="monotone"
                dataKey="rejected"
                stroke={lineColorMap.rejected}
                strokeWidth={2}
                dot={{ fill: lineColorMap.rejected, strokeWidth: 2, r: 4 }}
                name="Rejected"
              />
              <Line
                type="monotone"
                dataKey="timeout"
                stroke={lineColorMap.timeout}
                strokeWidth={2}
                dot={{ fill: lineColorMap.timeout, strokeWidth: 2, r: 4 }}
                name="Timeout"
              />
            </LineChart>
          </ChartContainer>
        ) : (
          <div className="h-80 flex items-center justify-center text-muted-foreground text-sm">
            No decision timeline data available.
          </div>
        )}
      </CardContent>
    </Card>
  )
}
