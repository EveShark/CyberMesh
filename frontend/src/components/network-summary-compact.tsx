"use client"

import Link from "next/link"
import { ArrowRight, Wifi, Activity } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"

interface NetworkSummaryCompactProps {
  peerCount?: number
  totalPeers?: number
  consensusRound?: number
  leaderStability?: number
  averageLatencyMs?: number
}

export function NetworkSummaryCompact({
  peerCount = 0,
  totalPeers = 0,
  consensusRound = 0,
  leaderStability = 0,
  averageLatencyMs = 0,
}: NetworkSummaryCompactProps) {
  const peerRatio = totalPeers > 0 ? (peerCount / totalPeers) * 100 : 0
  const status = peerRatio >= 75 ? "healthy" : peerRatio >= 50 ? "warning" : "critical"

  const statusConfig = {
    healthy: { label: "Healthy", color: "bg-green-500/10 text-green-400 border-green-500/30" },
    warning: { label: "Degraded", color: "bg-yellow-500/10 text-yellow-400 border-yellow-500/30" },
    critical: { label: "Critical", color: "bg-red-500/10 text-red-400 border-red-500/30" },
  }

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader className="pb-3 border-b border-border/30">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base font-medium flex items-center gap-2">
            <Wifi className="h-4 w-4 text-blue-400" />
            Network
          </CardTitle>
          <Badge className={statusConfig[status].color} variant="outline">
            {statusConfig[status].label}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 pt-4">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <p className="text-xs text-muted-foreground mb-1">Connected Peers</p>
            <p className="text-lg font-semibold text-foreground">
              {peerCount} / {totalPeers}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground mb-1">Consensus Round</p>
            <p className="text-lg font-semibold text-foreground">#{consensusRound}</p>
          </div>
        </div>
        
        <div className="grid grid-cols-2 gap-4">
          <div>
            <p className="text-xs text-muted-foreground mb-1">Leader Stability</p>
            <p className="text-lg font-semibold text-foreground">{leaderStability.toFixed(1)}%</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground mb-1">Avg Latency</p>
            <p className="text-lg font-semibold text-foreground">{averageLatencyMs.toFixed(0)}ms</p>
          </div>
        </div>

        <Link
          href="/network"
          className="flex items-center justify-center gap-2 text-sm text-blue-400 hover:text-blue-300 pt-3 border-t border-border/30 transition-colors"
        >
          <Activity className="h-4 w-4" />
          View Network Details
          <ArrowRight className="h-3 w-3" />
        </Link>
      </CardContent>
    </Card>
  )
}
