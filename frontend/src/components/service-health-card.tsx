"use client"

import { useMemo } from "react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"

const STATUS_LABELS: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline" }> = {
  running: { label: "Running", variant: "default" },
  ready: { label: "Ready", variant: "default" },
  ok: { label: "Healthy", variant: "default" },
  healthy: { label: "Healthy", variant: "default" },
  paused: { label: "Paused", variant: "secondary" },
  warning: { label: "Warning", variant: "secondary" },
  degraded: { label: "Degraded", variant: "secondary" },
  error: { label: "Error", variant: "destructive" },
  failed: { label: "Failed", variant: "destructive" },
}

function formatDuration(seconds?: number) {
  if (!seconds || seconds <= 0) return "--"
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)

  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

interface ServiceHealthCardProps {
  title: string
  status?: string
  uptimeSeconds?: number
  checks?: Record<string, unknown>
  loading?: boolean
}

export function ServiceHealthCard({ title, status, uptimeSeconds, checks, loading }: ServiceHealthCardProps) {
  const normalizedStatus = (status ?? "unknown").toString().toLowerCase()
  const presentation = STATUS_LABELS[normalizedStatus] ?? { label: status ?? "Unknown", variant: "outline" as const }

  const checkEntries = useMemo(() => {
    if (!checks) return []
    return Object.entries(checks)
      .filter(([key]) => !key.endsWith("_error"))
      .map(([key, value]) => ({
        name: key
          .replace(/_/g, " ")
          .replace(/\b\w/g, (letter) => letter.toUpperCase()),
        value: typeof value === "string" || typeof value === "number" ? value : JSON.stringify(value),
      }))
  }, [checks])

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader className="flex flex-row items-start justify-between gap-4">
        <div>
          <CardTitle className="text-lg font-semibold text-foreground">{title}</CardTitle>
          <p className="text-xs text-muted-foreground">Uptime: {formatDuration(uptimeSeconds)}</p>
        </div>
        <Badge variant={presentation.variant} className="text-xs">
          {presentation.label}
        </Badge>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="space-y-3">
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-5/6" />
            <Skeleton className="h-4 w-3/4" />
          </div>
        ) : checkEntries.length ? (
          <div className="space-y-3">
            {checkEntries.map((entry) => (
              <div key={entry.name} className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">{entry.name}</span>
                <span className="font-medium text-foreground">{entry.value}</span>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">No readiness checks reported.</p>
        )}
      </CardContent>
    </Card>
  )
}
