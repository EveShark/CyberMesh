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
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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

interface MetricRowProps {
  label: string
  value: string
  helper?: string
}

function MetricRow({ label, value, helper }: MetricRowProps) {
  return (
    <div className="flex items-center justify-between rounded-lg bg-background/60 px-3 py-2">
      <div className="flex flex-col">
        <span className="text-sm font-medium text-foreground">{label}</span>
        {helper ? <span className="text-xs text-muted-foreground">{helper}</span> : null}
      </div>
      <span className="text-sm font-semibold text-foreground">{value}</span>
    </div>
  )
}

export default function SystemHealthPageClient() {
  const { data, error, isLoading, mutate, keyMetrics, backendLatencyMetrics, aiLatencyMs } = useSystemHealthData(5000)

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

  const kafkaConsumerLag = useMemo(() => sumRecord(kafkaStats?.consumer_partition_lag), [kafkaStats?.consumer_partition_lag])
  const kafkaHighWater = useMemo(() => sumRecord(kafkaStats?.consumer_partition_highwater), [kafkaStats?.consumer_partition_highwater])
  const kafkaPublishP95 = kafkaStats?.publish_latency_p95_ms ?? kafkaStats?.publish_latency_ms
  const kafkaPublishP50 = kafkaStats?.publish_latency_p50_ms
  const kafkaConsumerIngestP95 = kafkaStats?.consumer_ingest_latency_p95_ms
  const kafkaConsumerProcessP95 = kafkaStats?.consumer_process_latency_p95_ms

  const networkInbound = networkStats?.inbound_throughput_bytes_per_second
  const networkOutbound = networkStats?.outbound_throughput_bytes_per_second

  const networkTrend = useMemo(() => {
    const history = networkStats?.history
    if (!history || history.length < 2) return undefined
    const first = history[0]
    const last = history[history.length - 1]
    const start = new Date(first.timestamp).getTime()
    const end = new Date(last.timestamp).getTime()
    const durationSeconds = (end - start) / 1000
    if (!Number.isFinite(durationSeconds) || durationSeconds <= 0) return undefined
    const inboundDelta = last.bytes_received - first.bytes_received
    const outboundDelta = last.bytes_sent - first.bytes_sent
    return {
      avgInbound: inboundDelta / durationSeconds,
      avgOutbound: outboundDelta / durationSeconds,
      peerDelta: last.peer_count - first.peer_count,
      since: first.timestamp,
    }
  }, [networkStats?.history])

  const networkTimestamp = useMemo(() => {
    if (!networkStats?.timestamp) return undefined
    const date = new Date(networkStats.timestamp)
    if (Number.isNaN(date.getTime())) return undefined
    return date.toLocaleTimeString()
  }, [networkStats?.timestamp])

  const latestUpdatedAt = data ? new Date(data.timestamp).toLocaleTimeString() : null

  const statusBadge = readiness?.ready
    ? { label: "Operational", className: "bg-emerald-500/10 text-emerald-400" }
    : { label: "Degraded", className: "bg-yellow-500/10 text-yellow-400" }

  const aiStatus = aiStats?.detection_loop_running
    ? { label: "AI Loop Running", className: "bg-emerald-500/10 text-emerald-400" }
    : { label: "AI Loop Paused", className: "bg-yellow-500/10 text-yellow-400" }

  const hasError = Boolean(error)

  return (
    <div className="min-h-screen w-full">
      {/* Header - Full Width */}
      <header className="border-b border-border/30 bg-card/20 backdrop-blur-sm px-6 py-4">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-3xl font-bold text-foreground">System Health</h1>
            <p className="text-muted-foreground">Operational readiness across consensus, storage, and AI pipeline</p>
          </div>
          <div className="flex items-center gap-3">
            {latestUpdatedAt ? <Badge variant="outline" className="text-xs">Updated {latestUpdatedAt}</Badge> : null}
            <Button variant="outline" size="sm" onClick={() => mutate()} disabled={isLoading}>
              <RefreshCw className={cn("mr-2 h-4 w-4", isLoading ? "animate-spin" : undefined)} /> Refresh
            </Button>
          </div>
        </div>

        <div className="flex items-center gap-3 mt-4">
          <Badge variant={readiness?.ready ? "outline" : "destructive"} className={cn("flex items-center gap-1.5 text-xs", readiness?.ready ? "border-emerald-500 text-emerald-500" : "")}>
            <PackageCheck className="h-3 w-3" /> {statusBadge.label}
          </Badge>
          <Badge variant={aiStats?.detection_loop_running ? "outline" : "secondary"} className={cn("flex items-center gap-1.5 text-xs", aiStats?.detection_loop_running ? "border-emerald-500 text-emerald-500" : "")}>
            <BrainCircuit className="h-3 w-3" /> {aiStatus.label}
          </Badge>
        </div>

        {hasError ? (
          <div className="flex items-start gap-3 rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive mt-4">
            <AlertCircle className="h-5 w-5" />
            <div>
              <p className="font-medium">Unable to fetch system health</p>
              <p className="text-destructive/80">{error instanceof Error ? error.message : "Unknown error"}</p>
            </div>
          </div>
        ) : null}
      </header>

      {/* KPI Strip - 4 Columns, Edge to Edge */}
      <section className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-px bg-border/20 p-px">
        <div className="bg-card/40 backdrop-blur-sm p-6">
          <div className="flex items-center justify-between mb-3">
            <span className="text-sm font-medium text-muted-foreground">Backend Uptime</span>
            <Server className="h-5 w-5 text-primary" />
          </div>
          <p className="text-3xl font-bold text-foreground font-mono tracking-tight">{formatDuration(uptimeSeconds)}</p>
          <p className="text-xs text-muted-foreground mt-2">Started {formatDuration(uptimeSeconds)} ago</p>
        </div>

        <div className="bg-card/40 backdrop-blur-sm p-6">
          <div className="flex items-center justify-between mb-3">
            <span className="text-sm font-medium text-muted-foreground">AI Uptime</span>
            <BrainCircuit className="h-5 w-5 text-primary" />
          </div>
          <p className="text-3xl font-bold text-foreground font-mono tracking-tight">{formatDuration(aiUptimeSeconds)}</p>
          <p className="text-xs text-muted-foreground mt-2">{aiStats?.state ? `State: ${aiStats.state}` : "Detection loop uptime"}</p>
        </div>

        <div className="bg-card/40 backdrop-blur-sm p-6">
          <div className="flex items-center justify-between mb-3">
            <span className="text-sm font-medium text-muted-foreground">Average CPU</span>
            <Cpu className="h-5 w-5 text-primary" />
          </div>
          <p className="text-3xl font-bold text-foreground font-mono tracking-tight">{formatPercentage(cpuPercent)}</p>
          <p className="text-xs text-muted-foreground mt-2">Per process (avg)</p>
        </div>

        <div className="bg-card/40 backdrop-blur-sm p-6">
          <div className="flex items-center justify-between mb-3">
            <span className="text-sm font-medium text-muted-foreground">Resident Memory</span>
            <Activity className="h-5 w-5 text-primary" />
          </div>
          <p className="text-3xl font-bold text-foreground font-mono tracking-tight">{formatBytes(memoryBytes)}</p>
          <p className="text-xs text-muted-foreground mt-2">Current usage</p>
        </div>
      </section>

      {/* Service Readiness - Full Width */}
      <section className="bg-card/40 backdrop-blur-sm border-y border-border/30">
        <div className="p-6">
          <div className="flex items-center justify-between mb-6">
            <div>
              <h3 className="text-lg font-semibold text-foreground">Service Readiness</h3>
              <p className="text-sm text-muted-foreground">Overall subsystem status reported by the backend</p>
            </div>
            <Badge {...resolveStatusVariant(readiness?.ready ? "ok" : "not ready")}>{readiness?.ready ? "Ready" : "Degraded"}</Badge>
          </div>
          <ServiceHealthGrid
            readiness={readiness}
            aiHealth={data?.ai ?? undefined}
            stats={data?.backend.stats}
            aiStats={aiStats}
            aiLatencyMs={aiLatencyMs ?? aiStats?.detection_loop_avg_latency_ms}
          />
        </div>
      </section>

      {/* 3-Column Layout - Full Width, No Gaps */}
      <section className="grid grid-cols-1 lg:grid-cols-3 gap-px bg-border/20 p-px">
        {/* Pipeline & Network */}
        <div className="bg-card/40 backdrop-blur-sm p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-lg font-semibold text-foreground">Pipeline & Network</h3>
            <Badge {...resolveStatusVariant(checks.kafka)}>{humanizeStatus(checks.kafka)}</Badge>
          </div>
          
          <div className="space-y-4">
            <div className="space-y-3 text-sm rounded-xl border border-border/40 bg-background/60 p-4">
              <div className="mb-2 flex items-center gap-2">
                <Network className="h-4 w-4 text-primary" />
                <span className="text-sm font-semibold text-foreground">Kafka Producer</span>
              </div>
              <MetricRow label="Publish P95" value={formatMilliseconds(kafkaPublishP95)} helper={`Avg ${formatMilliseconds(kafkaStats?.publish_latency_ms)}`} />
              <MetricRow label="Publish P50" value={formatMilliseconds(kafkaPublishP50)} />
              <MetricRow label="Successes" value={formatCount(kafkaStats?.publish_successes)} />
              <MetricRow label="Failures" value={formatCount(kafkaStats?.publish_failures)} />
            </div>

            <div className="space-y-3 text-sm rounded-xl border border-border/40 bg-background/60 p-4">
              <div className="mb-2 flex items-center gap-2">
                <Zap className="h-4 w-4 text-primary" />
                <span className="text-sm font-semibold text-foreground">Network & Mempool</span>
              </div>
              <MetricRow label="Peers" value={`${networkStats?.peer_count ?? 0} peers`} />
              <MetricRow label="Latency" value={formatMilliseconds(networkStats?.avg_latency_ms)} />
              <MetricRow label="Throughput" value={`${formatThroughputBytesPerSec(networkInbound)} in`} />
              <MetricRow label="Mempool" value={formatBytes(mempoolBytes)} helper="Queue depth" />
            </div>
          </div>
        </div>

        {/* Storage & Databases */}
        <div className="bg-card/40 backdrop-blur-sm p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-lg font-semibold text-foreground">Storage & Databases</h3>
            <Badge {...resolveStatusVariant(checks.storage)}>{humanizeStatus(checks.storage)}</Badge>
          </div>

          <div className="space-y-4">
            <div className="rounded-xl border border-border/40 bg-background/60 p-4">
              <div className="mb-3 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Database className="h-4 w-4 text-primary" />
                  <span className="text-sm font-semibold text-foreground">CockroachDB</span>
                </div>
                <Badge {...resolveStatusVariant(checks.cockroach)}>{humanizeStatus(checks.cockroach)}</Badge>
              </div>
              <div className="space-y-3 text-sm">
                <MetricRow label="Database" value={storageStats?.database ?? "--"} />
                <MetricRow label="Latency" value={formatMilliseconds(storageStats?.ready_latency_ms)} />
                <MetricRow label="Pool" value={`${storageStats?.pool_in_use ?? 0}/${storageStats?.pool_open_connections ?? 0}`} helper="Connections" />
                <MetricRow label="Query P95" value={formatMilliseconds(storageStats?.query_latency_p95_ms)} />
              </div>
            </div>

            <div className="rounded-xl border border-border/40 bg-background/60 p-4">
              <div className="mb-3 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Server className="h-4 w-4 text-primary" />
                  <span className="text-sm font-semibold text-foreground">Redis</span>
                </div>
                <Badge {...resolveStatusVariant(checks.redis)}>{humanizeStatus(checks.redis)}</Badge>
              </div>
              <div className="space-y-3 text-sm">
                <MetricRow label="Mode" value={redisStats?.mode ?? "--"} />
                <MetricRow label="Role" value={redisStats?.role ?? "--"} />
                <MetricRow label="Latency" value={formatMilliseconds(redisStats?.ready_latency_ms)} />
                <MetricRow label="Clients" value={formatCount(redisStats?.connected_clients)} />
              </div>
            </div>
          </div>
        </div>

        {/* AI Detection Loop */}
        <div className="bg-card/40 backdrop-blur-sm p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-lg font-semibold text-foreground">AI Detection Loop</h3>
            <Badge {...resolveStatusVariant(checks.ai_service)}>{humanizeStatus(checks.ai_service)}</Badge>
          </div>
          <div className="space-y-3 text-sm">
            <MetricRow label="Loop Status" value={aiStats?.detection_loop_running ? "Running" : "Paused"} />
            <MetricRow label="Average Latency" value={formatMilliseconds(aiLatencyMs ?? aiStats?.detection_loop_avg_latency_ms)} />
            <MetricRow label="Last Iteration" value={formatMilliseconds(aiStats?.detection_loop_last_latency_ms)} />
            <MetricRow label="Publish Rate" value={aiStats?.publish_rate_per_minute ? `${aiStats.publish_rate_per_minute.toFixed(1)}/min` : "--"} />
          </div>
        </div>
      </section>
    </div>
  )
}
