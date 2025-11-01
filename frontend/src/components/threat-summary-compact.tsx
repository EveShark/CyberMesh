"use client"

import Link from "next/link"
import { ArrowRight, Shield, AlertTriangle } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"

interface ThreatSummaryCompactProps {
  detectionsTotal?: number
  criticalCount?: number
  highCount?: number
  status?: string
  lastUpdated?: number
}

export function ThreatSummaryCompact({
  detectionsTotal = 0,
  criticalCount = 0,
  highCount = 0,
  status = "unknown",
  lastUpdated,
}: ThreatSummaryCompactProps) {
  const statusConfig = {
    running: { label: "Active", color: "bg-green-500/10 text-green-400 border-green-500/30" },
    paused: { label: "Paused", color: "bg-yellow-500/10 text-yellow-400 border-yellow-500/30" },
    error: { label: "Error", color: "bg-red-500/10 text-red-400 border-red-500/30" },
    unknown: { label: "Unknown", color: "bg-slate-500/10 text-slate-400 border-slate-500/30" },
  }

  const currentStatus = status.toLowerCase() as keyof typeof statusConfig
  const statusInfo = statusConfig[currentStatus] || statusConfig.unknown

  const timeSinceDetection = lastUpdated
    ? Math.floor((Date.now() - lastUpdated) / 1000)
    : null

  const formatTime = (seconds: number | null) => {
    if (seconds === null) return "N/A"
    if (seconds < 60) return `${seconds}s ago`
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`
    return `${Math.floor(seconds / 3600)}h ago`
  }

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader className="pb-3 border-b border-border/30">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base font-medium flex items-center gap-2">
            <Shield className="h-4 w-4 text-purple-400" />
            Threat Detection
          </CardTitle>
          <Badge className={statusInfo.color} variant="outline">
            {statusInfo.label}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 pt-4">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <p className="text-xs text-muted-foreground mb-1">Total Detections</p>
            <p className="text-lg font-semibold text-foreground">{detectionsTotal.toLocaleString()}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground mb-1">Critical Threats</p>
            <div className="flex items-center gap-1">
              <p className="text-lg font-semibold text-red-400">{criticalCount}</p>
              {criticalCount > 0 && <AlertTriangle className="h-4 w-4 text-red-400" />}
            </div>
          </div>
        </div>
        
        <div className="grid grid-cols-2 gap-4">
          <div>
            <p className="text-xs text-muted-foreground mb-1">High Severity</p>
            <p className="text-lg font-semibold text-orange-400">{highCount}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground mb-1">Last Detection</p>
            <p className="text-sm font-medium text-foreground">{formatTime(timeSinceDetection)}</p>
          </div>
        </div>

        <Link
          href="/threats"
          className="flex items-center justify-center gap-2 text-sm text-purple-400 hover:text-purple-300 pt-3 border-t border-border/30 transition-colors"
        >
          <Shield className="h-4 w-4" />
          View Threat Analysis
          <ArrowRight className="h-3 w-3" />
        </Link>
      </CardContent>
    </Card>
  )
}
