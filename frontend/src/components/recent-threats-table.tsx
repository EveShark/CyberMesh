"use client"

import type { ReactNode } from "react"
import { Waves, Biohazard, AlertTriangle, Scan, Shield } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"

interface DetectionBreakdown {
  threatType: string
  published: number
  total: number
  abstained: number
}

interface RecentThreatsTableProps {
  detections: DetectionBreakdown[]
  lastDetectionTime?: number
}

const severityIcons: Record<string, ReactNode> = {
  ddos: <Waves className="h-5 w-5 text-primary" />,
  malware: <Biohazard className="h-5 w-5 text-primary" />,
  anomaly: <AlertTriangle className="h-5 w-5 text-primary" />,
  port_scan: <Scan className="h-5 w-5 text-primary" />,
}

function formatRelativeTime(timestamp?: number) {
  if (!timestamp) return "--"
  const diffMs = Date.now() - timestamp * 1000
  const diffMinutes = Math.floor(diffMs / 60000)
  if (diffMinutes < 1) return "just now"
  if (diffMinutes < 60) return `${diffMinutes}m ago`
  const diffHours = Math.floor(diffMinutes / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  const diffDays = Math.floor(diffHours / 24)
  return `${diffDays}d ago`
}

export function RecentThreatsTable({ detections, lastDetectionTime }: RecentThreatsTableProps) {
  const totalPublished = detections.reduce((sum, item) => sum + item.published, 0)
  const totalPending = detections.reduce((sum, item) => sum + Math.max(item.total - item.published, 0), 0)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">AI Detection Activity</h2>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Badge variant="destructive">{totalPending} pending</Badge>
          <Badge variant="outline">Last detection {formatRelativeTime(lastDetectionTime)}</Badge>
        </div>
      </div>

      <Card className="glass-card">
        <CardHeader>
          <CardTitle className="text-lg">Threat Types</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {detections.map((item) => {
              const publishedPercentage = item.total > 0 ? Math.round((item.published / item.total) * 100) : 0
              const icon = severityIcons[item.threatType] ?? <Shield className="h-5 w-5 text-primary" />

              return (
                <div
                  key={item.threatType}
                  className="flex items-center justify-between p-4 rounded-lg border border-border/50 hover:bg-muted/30 transition-colors"
                >
                  <div className="flex items-center gap-4 flex-1">
                    <div>{icon}</div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1">
                        <p className="font-semibold text-sm capitalize">{item.threatType.replace(/_/g, " ")}</p>
                        <Badge variant="outline" className="text-xs">
                          {item.total} detections
                        </Badge>
                        <Badge variant="default" className="text-xs">
                          {item.published} published
                        </Badge>
                      </div>
                      <div className="w-full bg-muted rounded-full h-1.5">
                        <div
                          className="h-1.5 rounded-full bg-gradient-to-r from-primary to-accent"
                          style={{ width: `${Math.min(100, publishedPercentage)}%` }}
                        />
                      </div>
                    </div>
                  </div>

                  <div className="text-right text-sm">
                    <p className="font-medium">{item.abstained}</p>
                    <p className="text-xs text-muted-foreground">Abstained</p>
                  </div>
                </div>
              )
            })}

            {detections.length === 0 && (
              <div className="text-center py-8">
                <p className="text-muted-foreground">No detections recorded yet</p>
              </div>
            )}
          </div>

          <div className="mt-6 flex items-center justify-between text-sm text-muted-foreground">
            <span>Total published detections</span>
            <Badge variant="secondary">{totalPublished}</Badge>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
