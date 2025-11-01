"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

export interface EngineOverview {
  name: string
  ready: boolean
  detectionsPublished?: number
  notes?: string
}

interface DetectionEnginesGridProps {
  engines: EngineOverview[]
}

export function DetectionEnginesGrid({ engines }: DetectionEnginesGridProps) {
  if (!engines.length) {
    return <div className="text-sm text-muted-foreground">No engine metrics reported yet.</div>
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
      {engines.map((engine) => {
        const variant = engine.ready ? "default" : "secondary"
        const badgeClass = cn("text-xs", engine.ready ? "bg-status-healthy text-white" : "bg-status-warning text-white")

        return (
          <Card key={engine.name} className="glass-card border border-border/30 hover:border-primary/50 transition-all">
            <CardHeader className="pb-3">
              <div className="flex items-start justify-between">
                <CardTitle className="text-lg capitalize">{engine.name}</CardTitle>
                <Badge variant={variant} className={badgeClass}>
                  {engine.ready ? "Ready" : "Not Ready"}
                </Badge>
              </div>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">Published detections</span>
                <span className="font-semibold">{engine.detectionsPublished ?? "--"}</span>
              </div>
              <div className="text-muted-foreground">
                {engine.notes ?? "Engine status reported via metrics."}
              </div>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}
