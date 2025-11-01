"use client"

import { useMemo } from "react"
import {
  Activity,
  AlertCircle,
  BrainCircuit,
  Cpu,
  Database,
  Network,
  PackageCheck,
  RefreshCw,
  Server,
  Zap,
} from "lucide-react"

import { ServiceHealthGrid } from "@/components/service-health-grid"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { PageContainer } from "@/components/page-container"
import { useSystemHealthData } from "@/hooks/use-system-health-data"
import type {
  AIStatsSummary,
  KafkaStatsSummary,
  NetworkStatsSummary,
  RedisStatsSummary,
  StorageStatsSummary,
} from "@/lib/api"
import { cn } from "@/lib/utils"

type ReadinessValue = import("@/lib/api").ReadinessCheck | string | undefined

function extractStatus(value: ReadinessValue): string | undefined {
  if (!value) return undefined
  if (typeof value === "string") return value
  return value.status
}

function humanizeStatus(value: ReadinessValue): string {
  const status = extractStatus(value)
  if (!status) return "Unknown"
  switch (status) {
    case "ok":
    case "single_node":
    case "genesis":
      return "Healthy"
    case "not configured":
      return "Maintenance"
    case "not ready":
    case "insufficient":
    case "no_peers":
      return "Warning"
    default:
      return status.replace(/_/g, " ").replace(/^\w/, (c) => c.toUpperCase())
  }
}

function formatDuration(seconds?: number): string {
  if (seconds === undefined || Number.isNaN(seconds)) return "--"
  const s = Math.max(0, Math.floor(seconds))
  const days = Math.floor(s / 86400)
  const hours = Math.floor((s % 86400) / 3600)
  const minutes = Math.floor((s % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

function formatBytes(bytes?: number): string {
  if (bytes === undefined || !Number.isFinite(bytes) || bytes < 0) return "--"
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(2)} TB`
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(2)} GB`
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`
  if (bytes >= 1e3) return `${(bytes / 1e3).toFixed(1)} KB`
  return `${bytes.toFixed(0)} B`
}

function formatMilliseconds(ms?: number): string {
  if (ms === undefined || !Number.isFinite(ms) || ms < 0) return "--"
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)} s`
  return `${ms.toFixed(1)} ms`
}

function formatPercentage(value?: number): string {
  if (value === undefined || !Number.isFinite(value)) return "--"
  return `${value.toFixed(1)}%`
}

function formatCount(value?: number): string {
  if (value === undefined || !Number.isFinite(value)) return "--"
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return value.toLocaleString()
}

function formatThroughputBytesPerSec(value?: number): string {
  if (value === undefined || !Number.isFinite(value) || value < 0) return "--"
  const mbps = (value * 8) / 1_000_000
  if (mbps >= 1) return `${mbps.toFixed(2)} Mb/s`
  return `${(value / 1024).toFixed(1)} KB/s`
}

function sumRecord(record?: Record<string, number>): number | undefined {
  if (!record) return undefined
  const values = Object.values(record)
  if (values.length === 0) return undefined
  return values.reduce((acc, curr) => acc + curr, 0)
}

function computeCpuPercent(cpuSeconds?: number, uptimeSeconds?: number): number | undefined {
  if (!cpuSeconds || !uptimeSeconds || uptimeSeconds <= 0) return undefined
  return Math.min(100, Math.max(0, (cpuSeconds / uptimeSeconds) * 100))
}

function resolveStatusVariant(status: ReadinessValue) {
  const value = extractStatus(status)
  switch (value) {
    case "ok":
    case "single_node":
    case "genesis":
      return { variant: "outline" as const, className: "border-emerald-500 text-emerald-500" }
    case "not ready":
    case "insufficient":
    case "no_peers":
      return { variant: "outline" as const, className: "border-yellow-500 text-yellow-500" }
    case "not configured":
      return { variant: "outline" as const, className: "border-blue-500 text-blue-500" }
    default:
      return { variant: "outline" as const, className: "border-muted-foreground text-muted-foreground" }
  }
}

export default function SystemHealthPageClient() {
  const { data, error, isLoading, mutate, keyMetrics, aiLatencyMs } = useSystemHealthData(5000)

  const readiness = data?.backend.readiness
  const checks = readiness?.checks ?? {}
  const storageStats = data?.backend.stats.storage as StorageStatsSummary | undefined
  const redisStats = data?.backend.stats.redis as RedisStatsSummary | undefined
  const kafkaStats = data?.backend.stats.kafka as KafkaStatsSummary | undefined
  const aiStats = data?.backend.stats.ai_service as AIStatsSummary | undefined
  const networkStats = data?.backend.stats.network as NetworkStatsSummary | undefined

  const uptimeSeconds = keyMetrics.uptimeSeconds
  const cpuPercent = keyMetrics.cpuPercent ?? computeCpuPercent(data?.backend.metrics.summary.cpuSecondsTotal, uptimeSeconds)
  const memoryBytes = keyMetrics.residentMemoryBytes ?? data?.backend.metrics.summary.residentMemoryBytes
  const aiUptimeSeconds = keyMetrics.aiUptimeSeconds
  const mempoolBytes = keyMetrics.mempoolBytes ?? data?.backend.stats.mempool?.size_bytes

  const kafkaPublishP95 = kafkaStats?.publish_latency_p95_ms ?? kafkaStats?.publish_latency_ms
  const kafkaPublishP50 = kafkaStats?.publish_latency_p50_ms

  const networkInbound = networkStats?.inbound_throughput_bytes_per_second
  const networkOutbound = networkStats?.outbound_throughput_bytes_per_second

  const latestUpdatedAt = data ? new Date(data.timestamp).toLocaleTimeString() : null

  const statusBadge = readiness?.ready
    ? { label: "Operational", className: "bg-emerald-500/10 text-emerald-400" }
    : { label: "Degraded", className: "bg-yellow-500/10 text-yellow-400" }

  const aiStatus = aiStats?.detection_loop_running
    ? { label: "AI Loop Running", className: "bg-emerald-500/10 text-emerald-400" }
    : { label: "AI Loop Paused", className: "bg-yellow-500/10 text-yellow-400" }

  const hasError = Boolean(error)

  return (
    <div className="min-h-screen">
      <PageContainer align="left" className="py-6 lg:py-8 space-y-8">
        <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-3xl font-bold text-foreground">System Health</h1>
            <p className="text-sm text-muted-foreground">Operational readiness across consensus, storage, and AI pipeline</p>
          </div>
          <div className="flex items-center gap-3">
            {error ? (
              <Badge variant="destructive" className="flex items-center gap-1 text-xs">
                <AlertCircle className="h-3 w-3" /> Data unavailable
              </Badge>
            ) : null}
            {latestUpdatedAt ? <Badge variant="outline" className="text-xs">Updated {latestUpdatedAt}</Badge> : null}
            <Badge variant={readiness?.ready ? "outline" : "destructive"} className={cn("flex items-center gap-1.5 text-xs", readiness?.ready ? "border-emerald-500 text-emerald-500" : "")}>
              <PackageCheck className="h-3 w-3" /> {readiness?.ready ? "Operational" : "Degraded"}
            </Badge>
            <Badge variant={aiStats?.detection_loop_running ? "outline" : "secondary"} className={cn("flex items-center gap-1.5 text-xs", aiStats?.detection_loop_running ? "border-emerald-500 text-emerald-500" : "")}>
              <BrainCircuit className="h-3 w-3" /> {aiStats?.detection_loop_running ? "AI Running" : "AI Paused"}
            </Badge>
            <Button variant="outline" size="sm" onClick={() => mutate()} disabled={isLoading}>
              <RefreshCw className={cn("mr-2 h-4 w-4", isLoading ? "animate-spin" : undefined)} /> Refresh
            </Button>
          </div>
        </header>

        {error ? (
          <div className="flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
            <AlertCircle className="h-5 w-5" />
            <div>
              <p className="font-medium">Unable to fetch system health</p>
              <p className="text-destructive/80">{error instanceof Error ? error.message : "Unknown error"}</p>
            </div>
          </div>
        ) : null}

        <section className="w-full grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          <Card className="glass-card border border-border/30">
            <CardContent className="p-6 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-muted-foreground">Backend Uptime</p>
                  <p className="text-xs text-muted-foreground/80">Started {formatDuration(uptimeSeconds)} ago</p>
                </div>
                <Server className="h-5 w-5 text-primary" />
              </div>
              <p className="text-3xl font-bold text-foreground font-mono tracking-tight">{formatDuration(uptimeSeconds)}</p>
              <p className="text-xs text-muted-foreground">Process runtime</p>
            </CardContent>
          </Card>

          <Card className="glass-card border border-border/30">
            <CardContent className="p-6 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-muted-foreground">AI Uptime</p>
                  <p className="text-xs text-muted-foreground/80">{aiStats?.state ? `State: ${aiStats.state}` : "Detection loop"}</p>
                </div>
                <BrainCircuit className="h-5 w-5 text-primary" />
              </div>
              <p className="text-3xl font-bold text-foreground font-mono tracking-tight">{formatDuration(aiUptimeSeconds)}</p>
              <p className="text-xs text-muted-foreground">Detection loop uptime</p>
            </CardContent>
          </Card>

          <Card className="glass-card border border-border/30">
            <CardContent className="p-6 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-muted-foreground">Average CPU</p>
                  <p className="text-xs text-muted-foreground/80">Per process (avg)</p>
                </div>
                <Cpu className="h-5 w-5 text-primary" />
              </div>
              <p className="text-3xl font-bold text-foreground font-mono tracking-tight">{formatPercentage(cpuPercent)}</p>
              <p className="text-xs text-muted-foreground">Utilization</p>
            </CardContent>
          </Card>

          <Card className="glass-card border border-border/30">
            <CardContent className="p-6 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm font-medium text-muted-foreground">Resident Memory</p>
                  <p className="text-xs text-muted-foreground/80">Current usage</p>
                </div>
                <Activity className="h-5 w-5 text-primary" />
              </div>
              <p className="text-3xl font-bold text-foreground font-mono tracking-tight">{formatBytes(memoryBytes)}</p>
              <p className="text-xs text-muted-foreground">RAM allocated</p>
            </CardContent>
          </Card>
        </section>

        <Card className="glass-card border border-border/30">
          <CardContent className="p-6 space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-xl font-semibold text-foreground">Service Readiness</h2>
                <p className="text-sm text-muted-foreground">Overall subsystem status reported by the backend</p>
              </div>
              <Badge {...resolveStatusVariant(readiness?.ready ? "ok" : "not ready")}>
                {readiness?.ready ? "Ready" : "Degraded"}
              </Badge>
            </div>
            <ServiceHealthGrid
              readiness={readiness}
              aiHealth={data?.ai ?? undefined}
              stats={data?.backend.stats}
              aiStats={aiStats}
              aiLatencyMs={aiLatencyMs ?? aiStats?.detection_loop_avg_latency_ms}
            />
          </CardContent>
        </Card>

        <section className="grid gap-6 md:grid-cols-3">
          <Card className="glass-card border border-border/30">
            <CardContent className="p-6 space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-lg font-semibold text-foreground">Pipeline & Network</h3>
                <Badge {...resolveStatusVariant(checks.kafka)}>{humanizeStatus(checks.kafka)}</Badge>
              </div>
              
              <div className="space-y-3">
                <div className="flex items-center gap-2 mb-2">
                  <Network className="h-4 w-4 text-primary" />
                  <span className="text-sm font-semibold text-foreground">Kafka Producer</span>
                </div>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Publish P95</p>
                    <p className="text-lg font-semibold text-foreground">{formatMilliseconds(kafkaPublishP95)}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Publish P50</p>
                    <p className="text-lg font-semibold text-foreground">{formatMilliseconds(kafkaPublishP50)}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Successes</p>
                    <p className="text-lg font-semibold text-foreground">{formatCount(kafkaStats?.publish_successes)}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Failures</p>
                    <p className="text-lg font-semibold text-foreground">{formatCount(kafkaStats?.publish_failures)}</p>
                  </div>
                </div>
              </div>

              <div className="space-y-3">
                <div className="flex items-center gap-2 mb-2">
                  <Zap className="h-4 w-4 text-primary" />
                  <span className="text-sm font-semibold text-foreground">Network & Mempool</span>
                </div>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Peers</p>
                    <p className="text-lg font-semibold text-foreground">{networkStats?.peer_count ?? 0}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Latency</p>
                    <p className="text-lg font-semibold text-foreground">{formatMilliseconds(networkStats?.avg_latency_ms)}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Throughput In</p>
                    <p className="text-lg font-semibold text-foreground">{formatThroughputBytesPerSec(networkInbound)}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Mempool Size</p>
                    <p className="text-lg font-semibold text-foreground">{formatBytes(mempoolBytes)}</p>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card className="glass-card border border-border/30">
            <CardContent className="p-6 space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-lg font-semibold text-foreground">Storage & Databases</h3>
                <Badge {...resolveStatusVariant(checks.storage)}>{humanizeStatus(checks.storage)}</Badge>
              </div>

              <div className="space-y-3">
                <div className="flex items-center gap-2 mb-2">
                  <Database className="h-4 w-4 text-primary" />
                  <span className="text-sm font-semibold text-foreground">CockroachDB</span>
                </div>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Database</p>
                    <p className="text-sm font-semibold text-foreground truncate">{storageStats?.database ?? "--"}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Latency</p>
                    <p className="text-sm font-semibold text-foreground">{formatMilliseconds(storageStats?.ready_latency_ms)}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Pool</p>
                    <p className="text-sm font-semibold text-foreground">{storageStats?.pool_in_use ?? 0}/{storageStats?.pool_open_connections ?? 0}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Query P95</p>
                    <p className="text-sm font-semibold text-foreground">{formatMilliseconds(storageStats?.query_latency_p95_ms)}</p>
                  </div>
                </div>
              </div>

              <div className="space-y-3">
                <div className="flex items-center gap-2 mb-2">
                  <Server className="h-4 w-4 text-primary" />
                  <span className="text-sm font-semibold text-foreground">Redis</span>
                </div>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Mode</p>
                    <p className="text-sm font-semibold text-foreground">{redisStats?.mode ?? "--"}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Role</p>
                    <p className="text-sm font-semibold text-foreground">{redisStats?.role ?? "--"}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Latency</p>
                    <p className="text-sm font-semibold text-foreground">{formatMilliseconds(redisStats?.ready_latency_ms)}</p>
                  </div>
                  <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                    <p className="text-xs text-muted-foreground">Clients</p>
                    <p className="text-sm font-semibold text-foreground">{formatCount(redisStats?.connected_clients)}</p>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card className="glass-card border border-border/30">
            <CardContent className="p-6 space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-lg font-semibold text-foreground">AI Detection Loop</h3>
                <Badge {...resolveStatusVariant(checks.ai_service)}>{humanizeStatus(checks.ai_service)}</Badge>
              </div>
              
              <div className="grid grid-cols-2 gap-3 text-sm">
                <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                  <p className="text-xs text-muted-foreground">Loop Status</p>
                  <p className="text-lg font-semibold text-foreground">{aiStats?.detection_loop_running ? "Running" : "Paused"}</p>
                </div>
                <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                  <p className="text-xs text-muted-foreground">Average Latency</p>
                  <p className="text-lg font-semibold text-foreground">{formatMilliseconds(aiLatencyMs ?? aiStats?.detection_loop_avg_latency_ms)}</p>
                </div>
                <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                  <p className="text-xs text-muted-foreground">Last Iteration</p>
                  <p className="text-lg font-semibold text-foreground">{formatMilliseconds(aiStats?.detection_loop_last_latency_ms)}</p>
                </div>
                <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                  <p className="text-xs text-muted-foreground">Publish Rate</p>
                  <p className="text-lg font-semibold text-foreground">{aiStats?.publish_rate_per_minute ? `${aiStats.publish_rate_per_minute.toFixed(1)}/min` : "--"}</p>
                </div>
              </div>
            </CardContent>
          </Card>
        </section>
      </PageContainer>
    </div>
  )
}