"use client"

import { BarChart3 } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

interface NetworkSnapshotProps {
  blocksWithAnomalies: number
  totalAnomalies: number
  avgLatency?: number
  className?: string
}

export function NetworkSnapshot({ 
  blocksWithAnomalies, 
  totalAnomalies, 
  avgLatency,
  className = "" 
}: NetworkSnapshotProps) {
  return (
    <Card className={`glass-card border border-border/30 ${className}`}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-lg text-foreground">
          <BarChart3 className="h-4 w-4" />
          Network Snapshot
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3">
          <div className="rounded-lg border border-border/20 bg-background/60 px-4 py-3">
            <p className="text-sm text-muted-foreground">Blocks with anomalies</p>
            <p className="text-2xl font-semibold text-foreground">
              {blocksWithAnomalies.toLocaleString()}
            </p>
            <p className="text-xs text-muted-foreground mt-1">Count derived from latest API batch</p>
          </div>
          <div className="rounded-lg border border-border/20 bg-background/60 px-4 py-3">
            <p className="text-sm text-muted-foreground">Total anomalies</p>
            <p className="text-2xl font-semibold text-foreground">
              {totalAnomalies.toLocaleString()}
            </p>
            <p className="text-xs text-muted-foreground mt-1">Evidence transactions observed in recent fetch</p>
          </div>
          {avgLatency !== undefined && (
            <div className="rounded-lg border border-border/20 bg-background/60 px-4 py-3">
              <p className="text-sm text-muted-foreground">Peer latency (avg)</p>
              <p className="text-2xl font-semibold text-foreground">
                {avgLatency.toFixed(0)} ms
              </p>
              <p className="text-xs text-muted-foreground mt-1">Reported by validator network overview</p>
            </div>
          )}
        </div>
        <p className="text-xs text-muted-foreground leading-relaxed">
          Data updates every refresh cycle from backend API.
        </p>
      </CardContent>
    </Card>
  )
}
