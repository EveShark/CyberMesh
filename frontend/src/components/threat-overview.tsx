"use client"

import { useMemo } from "react"

import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"

interface ThreatBreakdownEntry {
  threatType: string
  published: number
  abstained: number
  total: number
  severity: "critical" | "high" | "medium" | "low"
}

interface ThreatOverviewData {
  status?: string
  detectionsPublished?: number
  detectionsTotal?: number
  severityBreakdown?: Record<"critical" | "high" | "medium" | "low", number>
  threatTypes?: ThreatBreakdownEntry[]
  metrics?: Record<string, number | string>
  lastUpdated?: number
}

interface ThreatOverviewProps {
  data?: ThreatOverviewData
}

const severityColors: Record<string, string> = {
  critical: "bg-status-critical",
  high: "bg-chart-2",
  medium: "bg-chart-3",
  low: "bg-chart-5",
}

export function ThreatOverview({ data }: ThreatOverviewProps) {
  const topThreats = useMemo(() => {
    return (data?.threatTypes ?? [])
      .slice()
      .sort((a, b) => b.total - a.total)
      .slice(0, 4)
  }, [data?.threatTypes])

  const totalDetections = data?.detectionsTotal ?? 0

  const statusLabel = (data?.status ?? "Paused").toString().toUpperCase()

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm text-muted-foreground">Detection Loop</p>
          <p className="text-2xl font-semibold text-foreground">{totalDetections.toLocaleString()}</p>
          <p className="text-xs text-muted-foreground">Total detections</p>
        </div>
        <div className="text-right space-y-2">
          <Badge variant={statusLabel === "RUNNING" ? "default" : "outline"} className="text-xs">
            {statusLabel}
          </Badge>
          <div className="text-xs text-muted-foreground">
            Published: {data?.detectionsPublished?.toLocaleString() ?? "--"}
          </div>
          <div className="text-xs text-muted-foreground">
            Last update: {data?.lastUpdated ? new Date(data.lastUpdated).toLocaleTimeString() : "--"}
          </div>
        </div>
      </div>

      <div className="space-y-3">
        {(["critical", "high", "medium", "low"] as const).map((level) => {
          const count = data?.severityBreakdown?.[level] ?? 0
          const percent = totalDetections ? Math.min(100, (count / totalDetections) * 100) : 0
          return (
            <div key={level} className="space-y-1">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span className="flex items-center gap-2 capitalize">
                  <span className={`h-2 w-2 rounded-full ${severityColors[level] ?? "bg-muted"}`} />
                  {level}
                </span>
                <span>{count.toLocaleString()}</span>
              </div>
              <Progress value={percent} className="h-2 bg-muted" />
            </div>
          )
        })}
      </div>

      <div className="space-y-3">
        <p className="text-sm font-semibold text-foreground">Top threat types</p>
        {topThreats.length ? (
          <div className="space-y-2">
            {topThreats.map((threat) => (
              <div key={threat.threatType} className="flex items-center justify-between text-sm">
                <div>
                  <p className="font-medium text-foreground capitalize">{threat.threatType.replace(/_/g, " ")}</p>
                  <p className="text-xs text-muted-foreground">
                    Published {threat.published.toLocaleString()} • Abstained {threat.abstained.toLocaleString()}
                  </p>
                </div>
                <Badge variant="outline" className="text-xs capitalize">
                  {threat.severity}
                </Badge>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">No threat data available.</p>
        )}
      </div>
    </div>
  )
}
