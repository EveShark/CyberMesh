"use client"

import { useMemo } from "react"

import { InfraStats } from "@/components/infra-stats"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"

interface InfrastructureSummary {
  lastUpdated?: number
  cpuSeconds?: number
  residentMemoryBytes?: number
  requestErrors?: number
  requestTotal?: number
  kafkaTopicRate?: number
  redisHitRate?: number
  redisMissRate?: number
  cockroachQueries?: number
  readinessChecks?: Record<string, string>
}

interface InfrastructureStatsProps {
  data?: InfrastructureSummary
}

function formatMemory(bytes?: number) {
  if (!bytes || bytes <= 0) return "--"
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1) return `${gb.toFixed(2)} GB`
  const mb = bytes / (1024 * 1024)
  return `${mb.toFixed(1)} MB`
}

export function InfrastructureStats({ data }: InfrastructureStatsProps) {
  const headerBadge = useMemo(() => {
    if (!data?.lastUpdated) return "--"
    return new Date(data.lastUpdated).toLocaleTimeString()
  }, [data?.lastUpdated])

  return (
    <div className="space-y-4">
      <Card className="glass-card border border-border/30">
        <CardContent className="p-0">
          <InfraStats
            kafkaTopicRate={data?.kafkaTopicRate}
            redisHitRate={data?.redisHitRate}
            redisMissRate={data?.redisMissRate}
            cockroachQueries={data?.cockroachQueries}
            readinessChecks={data?.readinessChecks}
          />
        </CardContent>
      </Card>

      <div className="grid grid-cols-2 gap-4 text-sm">
        <div className="rounded-lg border border-border/40 bg-card/40 p-4">
          <p className="text-xs text-muted-foreground">Memory Usage</p>
          <p className="text-xl font-semibold text-foreground">{formatMemory(data?.residentMemoryBytes)}</p>
        </div>
        <div className="rounded-lg border border-border/40 bg-card/40 p-4">
          <p className="text-xs text-muted-foreground">Last Updated</p>
          <Badge variant="outline" className="mt-1 text-xs">
            {headerBadge}
          </Badge>
        </div>
      </div>
    </div>
  )
}
