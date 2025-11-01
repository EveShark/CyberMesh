"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

interface DetectionTimelineProps {
  running: boolean
  metrics: Record<string, number | string>
  lastDetectionTime?: number
}

function formatTimestamp(timestamp?: number) {
  if (!timestamp) return "--"
  return new Date(timestamp * 1000).toLocaleString()
}

export function DetectionTimeline({ running, metrics, lastDetectionTime }: DetectionTimelineProps) {
  const items: Array<{ label: string; value: string }> = [
    { label: "Loop status", value: running ? "Running" : "Paused" },
    {
      label: "Total detections",
      value: metrics.detections_total !== undefined ? String(metrics.detections_total) : "--",
    },
    {
      label: "Published",
      value: metrics.detections_published !== undefined ? String(metrics.detections_published) : "--",
    },
    {
      label: "Rate limited",
      value: metrics.detections_rate_limited !== undefined ? String(metrics.detections_rate_limited) : "--",
    },
    {
      label: "Errors",
      value: metrics.errors !== undefined ? String(metrics.errors) : "--",
    },
    {
      label: "Avg latency",
      value:
        metrics.avg_latency_ms !== undefined
          ? `${Number(metrics.avg_latency_ms).toFixed(2)} ms`
          : "--",
    },
    {
      label: "Last detection",
      value: formatTimestamp(lastDetectionTime),
    },
  ]

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader>
        <CardTitle>Detection Loop Overview</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3 text-sm">
          {items.map((item) => (
            <div key={item.label} className="flex justify-between">
              <span className="text-muted-foreground">{item.label}</span>
              <span className="font-semibold">{item.value}</span>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
