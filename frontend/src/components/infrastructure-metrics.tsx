"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer, type TooltipProps } from "recharts"

interface InfrastructurePoint {
  timestamp: number
  cpuPercent?: number
  residentMemoryBytes?: number
  heapAllocBytes?: number
  networkBytesSent?: number
  networkBytesReceived?: number
  mempoolSizeBytes?: number
}

interface InfrastructureMetricsProps {
  data: InfrastructurePoint[]
}

function formatTime(timestamp: number) {
  return new Date(timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

function bytesToGigabytes(bytes?: number) {
  return typeof bytes === "number" ? bytes / (1024 * 1024 * 1024) : undefined
}

function bytesToMegabytes(bytes?: number) {
  return typeof bytes === "number" ? bytes / (1024 * 1024) : undefined
}

function bytesPerSecondToMbps(bytesPerSecond?: number) {
  return typeof bytesPerSecond === "number" ? (bytesPerSecond * 8) / (1024 * 1024) : undefined
}

const tooltipFormatter: TooltipProps<number, string>["formatter"] = (value, name) => {
  const numericValue = typeof value === "number" ? value : undefined
  if (numericValue === undefined) return ["--", name]

  switch (name) {
    case "cpu":
      return [`${numericValue.toFixed(1)}%`, "CPU"]
    case "memoryGb":
      return [`${numericValue.toFixed(2)} GB`, "Memory"]
    case "heapGb":
      return [`${numericValue.toFixed(2)} GB`, "Heap"]
    case "mempoolMb":
      return [`${numericValue.toFixed(1)} MB`, "Mempool"]
    case "networkOutMbps":
      return [`${numericValue.toFixed(2)} Mb/s`, "Network Out"]
    case "networkInMbps":
      return [`${numericValue.toFixed(2)} Mb/s`, "Network In"]
    default:
      return [numericValue, name]
  }
}

export function InfrastructureMetrics({ data }: InfrastructureMetricsProps) {
  const chartData = data.map((point, index) => {
    const prev = index > 0 ? data[index - 1] : null
    const elapsedSeconds = prev ? Math.max(1, (point.timestamp - prev.timestamp) / 1000) : undefined

    const sentRate = elapsedSeconds && point.networkBytesSent !== undefined ? point.networkBytesSent / elapsedSeconds : undefined
    const receivedRate = elapsedSeconds && point.networkBytesReceived !== undefined ? point.networkBytesReceived / elapsedSeconds : undefined

    return {
      time: formatTime(point.timestamp),
      cpu: point.cpuPercent,
      memoryGb: bytesToGigabytes(point.residentMemoryBytes),
      heapGb: bytesToGigabytes(point.heapAllocBytes),
      mempoolMb: bytesToMegabytes(point.mempoolSizeBytes),
      networkOutMbps: bytesPerSecondToMbps(sentRate),
      networkInMbps: bytesPerSecondToMbps(receivedRate),
    }
  })

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader>
        <CardTitle>Infrastructure Metrics</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={320}>
          <LineChart data={chartData} margin={{ left: 12, right: 24, top: 16, bottom: 8 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.3} />
            <XAxis dataKey="time" stroke="var(--muted-foreground)" tickLine={false} axisLine={false} minTickGap={24} />
            <YAxis stroke="var(--muted-foreground)" tickLine={false} axisLine={false} width={60} domain={["dataMin", "dataMax"]} />
            <Tooltip
              contentStyle={{
                backgroundColor: "var(--background)",
                border: "1px solid var(--border)",
                borderRadius: "8px",
              }}
              formatter={tooltipFormatter}
            />
            <Legend formatter={(value) => {
              switch (value) {
                case "cpu":
                  return "CPU %"
                case "memoryGb":
                  return "Memory (GB)"
                case "heapGb":
                  return "Heap (GB)"
                case "mempoolMb":
                  return "Mempool (MB)"
                case "networkOutMbps":
                  return "Network Out (Mb/s)"
                case "networkInMbps":
                  return "Network In (Mb/s)"
                default:
                  return value
              }
            }} />
            <Line dataKey="cpu" stroke="var(--chart-1)" strokeWidth={2} dot={false} connectNulls />
            <Line dataKey="memoryGb" stroke="var(--chart-2)" strokeWidth={2} dot={false} connectNulls />
            <Line dataKey="heapGb" stroke="var(--chart-3)" strokeWidth={2} dot={false} connectNulls />
            <Line dataKey="mempoolMb" stroke="var(--chart-4)" strokeWidth={2} dot={false} connectNulls />
            <Line dataKey="networkOutMbps" stroke="var(--chart-5)" strokeWidth={2} dot={false} connectNulls />
            <Line dataKey="networkInMbps" stroke="var(--chart-1)" strokeDasharray="4 4" dot={false} connectNulls />
          </LineChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  )
}
