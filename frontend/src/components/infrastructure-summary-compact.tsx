"use client"

import { Server, Database, Activity } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"

interface InfrastructureSummaryCompactProps {
  kafkaSuccessTotal?: number
  kafkaFailureTotal?: number
  kafkaBrokerCount?: number
  redisHits?: number
  redisMisses?: number
  redisTotalConnections?: number
  cockroachOpenConnections?: number
  residentMemoryBytes?: number
  uptimeSeconds?: number
}

export function InfrastructureSummaryCompact({
  kafkaSuccessTotal,
  kafkaFailureTotal,
  kafkaBrokerCount,
  redisHits,
  redisMisses,
  redisTotalConnections,
  cockroachOpenConnections,
  residentMemoryBytes,
  uptimeSeconds,
}: InfrastructureSummaryCompactProps) {
  const formatMemory = (bytes?: number) => {
    if (!bytes || bytes === 0) return "0 MB"
    const mb = bytes / (1024 * 1024)
    if (mb < 1024) return `${mb.toFixed(0)} MB`
    return `${(mb / 1024).toFixed(2)} GB`
  }

  const formatUptime = (seconds?: number) => {
    if (!seconds || seconds === 0) return "N/A"
    const hours = Math.floor(seconds / 3600)
    const days = Math.floor(hours / 24)
    if (days > 0) return `${days}d ${hours % 24}h`
    return `${hours}h ${Math.floor((seconds % 3600) / 60)}m`
  }

  const kafkaStatus = (() => {
    if (kafkaSuccessTotal !== undefined && kafkaSuccessTotal > 0) {
      return "healthy"
    }
    if (kafkaFailureTotal !== undefined && kafkaFailureTotal > 0) {
      return "warning"
    }
    if (kafkaBrokerCount !== undefined && kafkaBrokerCount > 0) {
      return "warning"
    }
    return "unknown"
  })()

  const redisStatus = (() => {
    const hits = redisHits ?? 0
    const misses = redisMisses ?? 0
    if (hits === 0 && misses === 0) {
      return "unknown"
    }
    if (misses > 0 && hits === 0) {
      return "warning"
    }
    return "healthy"
  })()

  const cockroachStatus = (() => {
    if (cockroachOpenConnections !== undefined) {
      if (cockroachOpenConnections > 0) {
        return "healthy"
      }
      return "warning"
    }
    return "unknown"
  })()

  const statusBadge = (status: string) => {
    const config = {
      healthy: "bg-green-500/10 text-green-400 border-green-500/30",
      warning: "bg-yellow-500/10 text-yellow-400 border-yellow-500/30",
      unknown: "bg-slate-500/10 text-slate-400 border-slate-500/30",
    }
    return config[status as keyof typeof config] || config.unknown
  }

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader className="pb-3 border-b border-border/30">
        <CardTitle className="text-base font-medium flex items-center gap-2">
          <Server className="h-4 w-4 text-emerald-400" />
          Infrastructure
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 pt-4">
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Activity className="h-3 w-3 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">Kafka</span>
            </div>
            <Badge className={statusBadge(kafkaStatus)} variant="outline">
              {kafkaStatus === "healthy" ? "Active" : kafkaStatus === "warning" ? "Degraded" : "Unknown"}
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground/70">
            {kafkaSuccessTotal !== undefined || kafkaFailureTotal !== undefined
              ? `${(kafkaSuccessTotal ?? 0).toFixed(0)} success / ${(kafkaFailureTotal ?? 0).toFixed(0)} fail`
              : kafkaBrokerCount !== undefined
                ? `${kafkaBrokerCount.toFixed(0)} brokers`
                : "No broker activity"}
          </p>
          
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Database className="h-3 w-3 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">Redis</span>
            </div>
            <Badge className={statusBadge(redisStatus)} variant="outline">
              {redisStatus === "healthy" ? "Active" : redisStatus === "warning" ? "Degraded" : "Unknown"}
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground/70">
            {redisHits !== undefined || redisMisses !== undefined
              ? `${(redisHits ?? 0).toFixed(0)} hits / ${(redisMisses ?? 0).toFixed(0)} misses`
              : redisTotalConnections !== undefined
                ? `${redisTotalConnections.toFixed(0)} total connections`
                : "No command metrics"}
          </p>
          
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Database className="h-3 w-3 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">CockroachDB</span>
            </div>
            <Badge className={statusBadge(cockroachStatus)} variant="outline">
              {cockroachStatus === "healthy" ? "Active" : cockroachStatus === "warning" ? "Degraded" : "Unknown"}
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground/70">
            {cockroachOpenConnections !== undefined
              ? `${cockroachOpenConnections.toFixed(0)} open connections`
              : "No pool metrics"}
          </p>
        </div>

        <div className="grid grid-cols-2 gap-4 pt-2 border-t border-border/30">
          <div>
            <p className="text-xs text-muted-foreground mb-1">Memory Usage</p>
            <p className="text-sm font-semibold text-foreground">{formatMemory(residentMemoryBytes)}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground mb-1">System Uptime</p>
            <p className="text-sm font-semibold text-foreground">{formatUptime(uptimeSeconds)}</p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
