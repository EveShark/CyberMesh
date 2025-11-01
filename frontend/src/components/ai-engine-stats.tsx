"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Brain, Calculator, CircuitBoard, TrendingUp } from "lucide-react"

import type { AiEngineMetricSummary } from "@/lib/api"

type EngineStatus = "healthy" | "warning" | "critical"

export interface AiEngineStatsProps {
  engines?: AiEngineMetricSummary[]
}

function resolveEngineMeta(engine: string) {
  const key = engine.toLowerCase()
  switch (key) {
    case "ml":
      return { title: "ML Ensemble", icon: <Brain className="h-4 w-4" /> }
    case "math":
      return { title: "Statistical Engine", icon: <Calculator className="h-4 w-4" /> }
    case "rules":
      return { title: "Rules Engine", icon: <CircuitBoard className="h-4 w-4" /> }
    default:
      return { title: engine.toUpperCase(), icon: <CircuitBoard className="h-4 w-4" /> }
  }
}

function resolveStatus(metric: AiEngineMetricSummary | undefined): EngineStatus {
  if (!metric) return "warning"
  if (!metric.ready) return "critical"
  if (metric.publish_ratio >= 0.6) return "healthy"
  return "warning"
}

export function AiEngineStats({ engines = [] }: AiEngineStatsProps) {
  const cards = engines.map((metric) => {
    const meta = resolveEngineMeta(metric.engine)
    const avgConfidence = typeof metric.avg_confidence === "number" ? `${(metric.avg_confidence * 100).toFixed(1)}%` : "--"
    const throughput = `${metric.throughput_per_minute.toFixed(1)} / min`
    const publishRate = `${(metric.publish_ratio * 100).toFixed(1)}%`
    const latency = typeof metric.last_latency_ms === "number" ? `${metric.last_latency_ms.toFixed(1)} ms` : "--"
    const change = `${metric.published} published`

    return {
      title: meta.title,
      icon: meta.icon,
      status: resolveStatus(metric),
      throughput,
      accuracy: publishRate,
      latency,
      change,
      confidence: avgConfidence,
    }
  })

  const getStatusColor = (status: EngineStatus) => {
    switch (status) {
      case "healthy":
        return "bg-status-healthy"
      case "warning":
        return "bg-status-warning"
      case "critical":
        return "bg-status-critical"
      default:
        return "bg-muted"
    }
  }

  const getStatusText = (status: EngineStatus) => {
    switch (status) {
      case "healthy":
        return "Healthy"
      case "warning":
        return "Warning"
      case "critical":
        return "Critical"
      default:
        return "Unknown"
    }
  }

  return (
    <Card className="glass-card">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-xl font-semibold text-foreground">AI Engine Performance</CardTitle>
          <Badge variant="outline" className="text-xs">Real-time</Badge>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b border-border/50">
                <th className="text-left px-6 py-3 text-xs font-medium text-muted-foreground uppercase">Engine</th>
                <th className="text-center px-4 py-3 text-xs font-medium text-muted-foreground uppercase">Status</th>
                <th className="text-right px-4 py-3 text-xs font-medium text-muted-foreground uppercase">Throughput</th>
                <th className="text-right px-4 py-3 text-xs font-medium text-muted-foreground uppercase">Publish Rate</th>
                <th className="text-right px-4 py-3 text-xs font-medium text-muted-foreground uppercase">Avg Latency</th>
                <th className="text-right px-4 py-3 text-xs font-medium text-muted-foreground uppercase">Confidence</th>
                <th className="text-right px-6 py-3 text-xs font-medium text-muted-foreground uppercase">Published</th>
              </tr>
            </thead>
            <tbody>
              {cards.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-6 py-6 text-center text-muted-foreground">
                    Engine telemetry not yet recorded.
                  </td>
                </tr>
              ) : null}
              {cards.map((card, idx) => (
                <tr key={card.title} className={idx !== cards.length - 1 ? "border-b border-border/30" : ""}>
                  <td className="px-6 py-4">
                    <div className="flex items-center gap-2">
                      <div className="text-primary">{card.icon}</div>
                      <span className="font-medium text-foreground">{card.title}</span>
                    </div>
                  </td>
                  <td className="px-4 py-4 text-center">
                    <Badge variant="outline" className={`${getStatusColor(card.status as EngineStatus)} text-white border-0 text-xs`}>
                      {getStatusText(card.status as EngineStatus)}
                    </Badge>
                  </td>
                  <td className="px-4 py-4 text-right font-semibold text-foreground">{card.throughput ?? "--"}</td>
                  <td className="px-4 py-4 text-right font-semibold text-foreground">{card.accuracy ?? "--"}</td>
                  <td className="px-4 py-4 text-right font-semibold text-foreground">{card.latency ?? "--"}</td>
                  <td className="px-4 py-4 text-right font-semibold text-foreground">{card.confidence ?? "--"}</td>
                  <td className="px-6 py-4 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <TrendingUp className="h-3 w-3 text-muted-foreground" />
                      <span className="font-semibold text-foreground">{card.change ?? "--"}</span>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  )
}
