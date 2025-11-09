"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { PieChart, Pie, Cell, AreaChart, Area, XAxis, YAxis, CartesianGrid } from "recharts"
import { TrendingUp, PieChartIcon } from "lucide-react"

interface SeverityPoint {
  name: string
  value: number
}

interface TrendPoint {
  timestamp: number
  publishedDelta: number
  totalDelta: number
}

interface ThreatChartsProps {
  severity: SeverityPoint[]
  trend: TrendPoint[]
}

const colors: Record<string, string> = {
  critical: "var(--color-threat-critical)",
  high: "var(--color-threat-high)",
  medium: "var(--color-threat-medium)",
  low: "var(--color-threat-low)",
}

function formatTime(timestamp: number) {
  return new Date(timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

export function ThreatCharts({ severity, trend }: ThreatChartsProps) {
  const severityData = severity.map((item) => ({
    name: item.name.charAt(0).toUpperCase() + item.name.slice(1),
    value: item.value,
  }))

  const severityTotal = severityData.reduce((sum, item) => sum + item.value, 0)

  const trendData = trend.map((point) => ({
    time: formatTime(point.timestamp),
    published: point.publishedDelta,
    total: point.totalDelta,
  }))

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <Card className="glass-card">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-foreground">
            <PieChartIcon className="h-5 w-5 text-primary" />
            Threat Severity Distribution
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ChartContainer className="h-80" config={{}}>
            <PieChart>
              <Pie
                data={severityData}
                cx="50%"
                cy="50%"
                innerRadius={0}
                outerRadius={120}
                paddingAngle={2}
                dataKey="value"
                nameKey="name"
                label={(entry) => {
                  if (severityTotal <= 0) return "0%"
                  const pct = (entry.value / severityTotal) * 100
                  return `${pct.toFixed(0)}%`
                }}
                labelLine={false}
              >
                {severityData.map((entry) => (
                  <Cell key={entry.name} fill={colors[entry.name.toLowerCase()] ?? "hsl(var(--chart-1))"} />
                ))}
              </Pie>
              <ChartTooltip content={<ChartTooltipContent />} />
            </PieChart>
          </ChartContainer>
          <div className="grid grid-cols-2 gap-3 mt-4">
            {severityData.map((item) => {
              const percentage = severityTotal > 0 ? ((item.value / severityTotal) * 100).toFixed(1) : "0.0"
              return (
                <div key={item.name} className="flex items-center gap-2">
                  <div className="w-4 h-4 rounded-full" style={{ backgroundColor: colors[item.name.toLowerCase()] ?? "hsl(var(--chart-1))" }} />
                  <span className="text-sm font-medium text-foreground">{item.name}</span>
                  <span className="text-sm font-mono ml-auto text-muted-foreground">{item.value.toFixed(0)} ({percentage}%)</span>
                </div>
              )
            })}
          </div>
        </CardContent>
      </Card>

      <Card className="glass-card">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-foreground">
            <TrendingUp className="h-5 w-5 text-primary" />
            Threat Volume (detections/s)
          </CardTitle>
        </CardHeader>
        <CardContent className="pr-6">
          <ChartContainer className="h-80 w-full" config={{}}>
            <AreaChart data={trendData} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--muted-foreground) / 0.4)" />
              <XAxis dataKey="time" stroke="hsl(var(--muted-foreground))" />
              <YAxis stroke="hsl(var(--muted-foreground))" width={40} />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Area type="monotone" dataKey="total" stroke="var(--color-threat-critical)" fill="var(--color-threat-critical)" fillOpacity={0.3} strokeWidth={2} name="Total" dot={{ fill: 'var(--color-threat-critical)', r: 3 }} />
              <Area type="monotone" dataKey="published" stroke="hsl(var(--chart-2))" fill="hsl(var(--chart-2))" fillOpacity={0.4} strokeWidth={2} name="Published" dot={{ fill: 'hsl(var(--chart-2))', r: 3 }} />
            </AreaChart>
          </ChartContainer>
        </CardContent>
      </Card>
    </div>
  )
}
