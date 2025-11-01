"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { CheckCircle2, AlertCircle, AlertTriangle } from "lucide-react"

export interface SlaMetric {
  metric: string
  target: string
  current?: string
  status?: "met" | "warning" | "breach"
  description?: string
}

interface SLATrackerProps {
  metrics: SlaMetric[]
}

function StatusIcon({ status }: { status?: SlaMetric["status"] }) {
  if (!status) return null
  if (status === "met") {
    return <CheckCircle2 className="h-5 w-5 text-emerald-400" />
  }
  if (status === "warning") {
    return <AlertTriangle className="h-5 w-5 text-yellow-400" />
  }
  return <AlertCircle className="h-5 w-5 text-red-400" />
}

export function SLATracker({ metrics }: SLATrackerProps) {
  if (!metrics.length) {
    return (
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle>SLA Compliance</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">No SLA data available.</p>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader>
        <CardTitle>SLA Compliance</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {metrics.map((item) => (
            <div key={item.metric} className="flex items-start justify-between gap-4 pb-4 border-b border-border/20 last:border-0">
              <div className="space-y-1">
                <p className="font-semibold text-foreground leading-tight">{item.metric}</p>
                <p className="text-xs text-muted-foreground">Target: {item.target}</p>
                {item.description ? <p className="text-xs text-muted-foreground/80">{item.description}</p> : null}
              </div>
              <div className="flex items-center gap-3">
                <div className="text-right text-sm">
                  <p className="font-bold text-foreground">{item.current ?? "--"}</p>
                </div>
                <StatusIcon status={item.status} />
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
