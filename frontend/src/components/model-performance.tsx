"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from "recharts"

export interface VariantPerformanceDatum {
  name: string
  publish: number
  threshold: number
  dlq: number
  error: number
}

interface ModelPerformanceProps {
  data: VariantPerformanceDatum[]
}

export function ModelPerformance({ data }: ModelPerformanceProps) {
  if (!data.length) {
    return (
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle>Model Performance Metrics</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">No variant metrics available yet.</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader>
        <CardTitle>Model Performance Metrics</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={300}>
          <BarChart data={data}>
            <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.3} />
            <XAxis dataKey="name" stroke="var(--muted-foreground)" />
            <YAxis stroke="var(--muted-foreground)" allowDecimals={false} />
            <Tooltip
              contentStyle={{
                backgroundColor: "var(--background)",
                border: "1px solid var(--border)",
                borderRadius: "8px",
              }}
            />
            <Legend />
            <Bar dataKey="publish" fill="var(--chart-1)" name="Published" />
            <Bar dataKey="threshold" fill="var(--chart-2)" name="Threshold" />
            <Bar dataKey="dlq" fill="var(--chart-3)" name="DLQ" />
            <Bar dataKey="error" fill="var(--chart-4)" name="Errors" />
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}
