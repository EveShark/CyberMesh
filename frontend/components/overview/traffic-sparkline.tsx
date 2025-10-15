"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { LineChart, Line } from "recharts"
import { cn } from "@/lib/utils"

interface TrafficData {
  time: string
  requests: number
  bandwidth: number
  errors: number
  timestamp: number
}

// Mock traffic data for the last 24 hours (hourly data points)
const mockTrafficData: TrafficData[] = [
  { time: "00:00", requests: 1200, bandwidth: 45, errors: 2, timestamp: 1 },
  { time: "01:00", requests: 980, bandwidth: 38, errors: 1, timestamp: 2 },
  { time: "02:00", requests: 850, bandwidth: 32, errors: 0, timestamp: 3 },
  { time: "03:00", requests: 720, bandwidth: 28, errors: 1, timestamp: 4 },
  { time: "04:00", requests: 650, bandwidth: 25, errors: 0, timestamp: 5 },
  { time: "05:00", requests: 780, bandwidth: 30, errors: 1, timestamp: 6 },
  { time: "06:00", requests: 1100, bandwidth: 42, errors: 2, timestamp: 7 },
  { time: "07:00", requests: 1450, bandwidth: 55, errors: 3, timestamp: 8 },
  { time: "08:00", requests: 1800, bandwidth: 68, errors: 4, timestamp: 9 },
  { time: "09:00", requests: 2200, bandwidth: 82, errors: 5, timestamp: 10 },
  { time: "10:00", requests: 2400, bandwidth: 89, errors: 3, timestamp: 11 },
  { time: "11:00", requests: 2600, bandwidth: 95, errors: 6, timestamp: 12 },
  { time: "12:00", requests: 2800, bandwidth: 102, errors: 4, timestamp: 13 },
  { time: "13:00", requests: 2750, bandwidth: 98, errors: 5, timestamp: 14 },
  { time: "14:00", requests: 2900, bandwidth: 105, errors: 7, timestamp: 15 },
  { time: "15:00", requests: 3100, bandwidth: 112, errors: 8, timestamp: 16 },
  { time: "16:00", requests: 2950, bandwidth: 108, errors: 6, timestamp: 17 },
  { time: "17:00", requests: 2700, bandwidth: 98, errors: 4, timestamp: 18 },
  { time: "18:00", requests: 2400, bandwidth: 87, errors: 3, timestamp: 19 },
  { time: "19:00", requests: 2100, bandwidth: 76, errors: 2, timestamp: 20 },
  { time: "20:00", requests: 1900, bandwidth: 69, errors: 3, timestamp: 21 },
  { time: "21:00", requests: 1700, bandwidth: 62, errors: 2, timestamp: 22 },
  { time: "22:00", requests: 1500, bandwidth: 55, errors: 1, timestamp: 23 },
  { time: "23:00", requests: 1300, bandwidth: 48, errors: 2, timestamp: 24 },
]

interface MetricCardProps {
  title: string
  value: string
  change: string
  trend: "up" | "down" | "stable"
  icon: string
}

function MetricCard({ title, value, change, trend, icon }: MetricCardProps) {
  const getTrendColor = () => {
    switch (trend) {
      case "up":
        return "status-healthy"
      case "down":
        return "status-critical"
      case "stable":
        return "text-muted-foreground"
      default:
        return "text-muted-foreground"
    }
  }

  const getTrendIcon = () => {
    switch (trend) {
      case "up":
        return "↗"
      case "down":
        return "↘"
      case "stable":
        return "→"
      default:
        return "→"
    }
  }

  return (
    <div className="flex items-center justify-between p-4 rounded-lg border border-border/50 bg-card/50">
      <div className="flex items-center gap-3">
        <span className="text-2xl">{icon}</span>
        <div>
          <p className="text-sm text-muted-foreground">{title}</p>
          <p className="text-xl font-bold">{value}</p>
        </div>
      </div>
      <div className="text-right">
        <div className={cn("flex items-center gap-1 text-sm font-medium", getTrendColor())}>
          <span>{getTrendIcon()}</span>
          <span>{change}</span>
        </div>
        <p className="text-xs text-muted-foreground">vs last hour</p>
      </div>
    </div>
  )
}

const trafficChartConfig = {
  requests: {
    label: "Requests",
    color: "var(--chart-1)",
  },
  bandwidth: {
    label: "Bandwidth (MB/s)",
    color: "var(--chart-2)",
  },
  errors: {
    label: "Errors",
    color: "var(--chart-4)",
  },
}

export function TrafficSparkline() {
  // TODO: integrate GET /metrics

  const currentRequests = mockTrafficData[mockTrafficData.length - 1].requests
  const previousRequests = mockTrafficData[mockTrafficData.length - 2].requests
  const requestsChange = (((currentRequests - previousRequests) / previousRequests) * 100).toFixed(1)

  const currentBandwidth = mockTrafficData[mockTrafficData.length - 1].bandwidth
  const previousBandwidth = mockTrafficData[mockTrafficData.length - 2].bandwidth
  const bandwidthChange = (((currentBandwidth - previousBandwidth) / previousBandwidth) * 100).toFixed(1)

  const currentErrors = mockTrafficData[mockTrafficData.length - 1].errors
  const previousErrors = mockTrafficData[mockTrafficData.length - 2].errors
  const errorsChange = currentErrors - previousErrors

  const totalRequests = mockTrafficData.reduce((sum, data) => sum + data.requests, 0)
  const avgBandwidth = (
    mockTrafficData.reduce((sum, data) => sum + data.bandwidth, 0) / mockTrafficData.length
  ).toFixed(1)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Traffic & Throughput</h2>
        <Badge variant="outline" className="text-sm">
          Last 24 hours
        </Badge>
      </div>

      {/* Metrics Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <MetricCard
          title="Current Requests"
          value={`${currentRequests.toLocaleString()}/s`}
          change={`${requestsChange}%`}
          trend={
            Number.parseFloat(requestsChange) > 0 ? "up" : Number.parseFloat(requestsChange) < 0 ? "down" : "stable"
          }
          icon="📊"
        />
        <MetricCard
          title="Bandwidth Usage"
          value={`${currentBandwidth} MB/s`}
          change={`${bandwidthChange}%`}
          trend={
            Number.parseFloat(bandwidthChange) > 0 ? "up" : Number.parseFloat(bandwidthChange) < 0 ? "down" : "stable"
          }
          icon="🌐"
        />
        <MetricCard
          title="Error Rate"
          value={`${currentErrors}`}
          change={`${errorsChange >= 0 ? "+" : ""}${errorsChange}`}
          trend={errorsChange > 0 ? "up" : errorsChange < 0 ? "down" : "stable"}
          icon="⚠️"
        />
      </div>

      {/* Traffic Chart */}
      <Card className="glass-card">
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-lg text-foreground">Traffic Patterns</CardTitle>
            <div className="flex items-center gap-4 text-sm">
              <div className="flex items-center gap-2">
                <div className="w-3 h-3 rounded-full" style={{ backgroundColor: "var(--chart-1)" }}></div>
                <span className="text-muted-foreground">Requests</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="w-3 h-3 rounded-full" style={{ backgroundColor: "var(--chart-2)" }}></div>
                <span className="text-muted-foreground">Bandwidth</span>
              </div>
              <div className="flex items-center gap-2">
                <div className="w-3 h-3 rounded-full" style={{ backgroundColor: "var(--chart-4)" }}></div>
                <span className="text-muted-foreground">Errors</span>
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <ChartContainer config={trafficChartConfig} className="h-64 w-full">
            <LineChart data={mockTrafficData}>
              <ChartTooltip content={<ChartTooltipContent />} />
              <Line
                type="monotone"
                dataKey="requests"
                stroke="var(--chart-1)"
                strokeWidth={2}
                dot={false}
                name="Requests"
              />
              <Line
                type="monotone"
                dataKey="bandwidth"
                stroke="var(--chart-2)"
                strokeWidth={2}
                dot={false}
                name="Bandwidth (MB/s)"
              />
              <Line
                type="monotone"
                dataKey="errors"
                stroke="var(--chart-4)"
                strokeWidth={2}
                dot={false}
                name="Errors"
              />
            </LineChart>
          </ChartContainer>

          <div className="mt-4 pt-4 border-t border-border/50">
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
              <div>
                <p className="text-muted-foreground">Total Requests</p>
                <p className="font-semibold">{totalRequests.toLocaleString()}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Avg Bandwidth</p>
                <p className="font-semibold">{avgBandwidth} MB/s</p>
              </div>
              <div>
                <p className="text-muted-foreground">Peak Requests</p>
                <p className="font-semibold">
                  {Math.max(...mockTrafficData.map((d) => d.requests)).toLocaleString()}/s
                </p>
              </div>
              <div>
                <p className="text-muted-foreground">Total Errors</p>
                <p className="font-semibold">{mockTrafficData.reduce((sum, data) => sum + data.errors, 0)}</p>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
