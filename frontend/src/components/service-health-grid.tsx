"use client"

import type { ReactNode } from "react"
import { Database, Layers, Inbox, Cog, Globe2, RadioTower, Brain, Bug, BrainCircuit } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import type {
  ReadinessCheck,
  StatsSummary,
  StorageStatsSummary,
  KafkaStatsSummary,
  RedisStatsSummary,
  AIStatsSummary,
} from "@/lib/api"

interface ServiceHealthGridProps {
  readiness?: {
    checks: Record<string, ReadinessCheck | string>
    details?: Record<string, unknown>
    timestamp?: number
  }
  aiHealth?: {
    health?: Record<string, unknown> | null
    ready?: Record<string, unknown> | null
  }
  stats?: StatsSummary
  aiStats?: AIStatsSummary
  aiLatencyMs?: number
}

type ServiceStatus = "healthy" | "warning" | "critical" | "maintenance"

interface DerivedService {
  name: string
  status: ServiceStatus
  details?: string
  lastCheck?: string
  icon: ReactNode
  metrics?: Array<{ label: string; value: string }>
}

type ReadinessValue = import("@/lib/api").ReadinessCheck | string | undefined

function extractStatus(value: ReadinessValue): string | undefined {
  if (!value) return undefined
  if (typeof value === "string") return value
  return value.status
}

function mapStatus(value: ReadinessValue): ServiceStatus {
  const status = extractStatus(value)?.toLowerCase()
  if (!status) return "warning"

  switch (status) {
    case "ok":
      return "healthy"
    case "not configured":
      return "maintenance"
    case "critical":
    case "error":
    case "failed":
    case "not ready":
      return "critical"
    case "warning":
    case "degraded":
    case "single_node":
    case "genesis":
    case "insufficient":
    case "no_peers":
      return "warning"
    default:
      return status === "ok" ? "healthy" : "warning"
  }
}

function statusVariant(status: ServiceStatus) {
  switch (status) {
    case "healthy":
      return { variant: "default" as const, className: "bg-status-healthy text-white" }
    case "warning":
      return { variant: "secondary" as const, className: "bg-status-warning text-white" }
    case "critical":
      return { variant: "destructive" as const, className: "" }
    case "maintenance":
      return { variant: "outline" as const, className: "" }
  }
}

function formatMilliseconds(ms?: number): string {
  if (ms === undefined || !Number.isFinite(ms) || ms < 0) return "--"
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)} s`
  return `${ms.toFixed(1)} ms`
}

function formatDuration(seconds?: number): string {
  if (seconds === undefined || !Number.isFinite(seconds) || seconds < 0) return "--"
  const s = Math.floor(seconds)
  const days = Math.floor(s / 86400)
  const hours = Math.floor((s % 86400) / 3600)
  const minutes = Math.floor((s % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

function formatBytes(bytes?: number): string {
  if (bytes === undefined || !Number.isFinite(bytes) || bytes < 0) return "--"
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(2)} GB`
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`
  if (bytes >= 1e3) return `${(bytes / 1e3).toFixed(1)} KB`
  return `${bytes.toFixed(0)} B`
}

function formatPercentage(value?: number): string {
  if (value === undefined || !Number.isFinite(value)) return "--"
  return `${(value * 100).toFixed(1)}%`
}

function formatCount(value?: number): string {
  if (value === undefined || !Number.isFinite(value)) return "--"
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return value.toLocaleString()
}

function sumRecord(record?: Record<string, number>): number | undefined {
  if (!record) return undefined
  const values = Object.values(record)
  if (values.length === 0) return undefined
  return values.reduce((acc, curr) => acc + curr, 0)
}

function formatNumber(value?: number, fractionDigits = 1): string {
  if (value === undefined || !Number.isFinite(value)) return "--"
  return value.toLocaleString(undefined, { maximumFractionDigits: fractionDigits })
}

export function ServiceHealthGrid({ readiness, aiHealth, stats, aiStats, aiLatencyMs }: ServiceHealthGridProps) {
  const checks = readiness?.checks ?? {}
  const timestamp = readiness?.timestamp
  const storageStats = stats?.storage as StorageStatsSummary | undefined
  const kafkaStats = stats?.kafka as KafkaStatsSummary | undefined
  const redisStats = stats?.redis as RedisStatsSummary | undefined
  const consensusStats = stats?.consensus
  const mempoolStats = stats?.mempool
  const networkStats = stats?.network
  const chainStats = stats?.chain

  const kafkaLagTotal = sumRecord(kafkaStats?.consumer_partition_lag)
  const kafkaHighWater = sumRecord(kafkaStats?.consumer_partition_highwater)

  const services: DerivedService[] = [
    {
      name: "Storage",
      status: mapStatus(checks.storage),
      details:
        typeof readiness?.details?.storage_error === "string" ? readiness?.details?.storage_error : undefined,
      icon: <Database className="h-5 w-5 text-primary" />,
      metrics: [
        { label: "Latency", value: formatMilliseconds(storageStats?.ready_latency_ms) },
        { label: "Query P95", value: formatMilliseconds(storageStats?.query_latency_p95_ms) },
        {
          label: "Pool",
          value: `${storageStats?.pool_in_use ?? 0}/${storageStats?.pool_open_connections ?? 0} in-use`,
        },
      ],
    },
    {
      name: "State Store",
      status: mapStatus(checks.state),
      details:
        typeof readiness?.details?.state_error === "string" ? readiness?.details?.state_error : undefined,
      icon: <Layers className="h-5 w-5 text-primary" />,
      metrics: [
        { label: "State version", value: chainStats?.state_version ? chainStats.state_version.toLocaleString() : "--" },
        { label: "Success", value: chainStats?.success_rate ? formatPercentage(chainStats.success_rate) : "--" },
      ],
    },
    {
      name: "Mempool",
      status: mapStatus(checks.mempool),
      icon: <Inbox className="h-5 w-5 text-primary" />,
      metrics: [
        { label: "Pending", value: formatCount(mempoolStats?.pending_transactions) },
        { label: "Size", value: formatBytes(mempoolStats?.size_bytes) },
        { label: "Oldest", value: formatDuration(mempoolStats?.oldest_tx_age_seconds) },
      ],
    },
    {
      name: "Consensus Engine",
      status: mapStatus(checks.consensus),
      details:
        typeof readiness?.details?.consensus_error === "string"
          ? readiness?.details?.consensus_error
          : undefined,
      icon: <Cog className="h-5 w-5 text-primary" />,
      metrics: [
        { label: "View", value: consensusStats?.view !== undefined ? consensusStats.view.toLocaleString() : "--" },
        { label: "Round", value: consensusStats?.round !== undefined ? consensusStats.round.toLocaleString() : "--" },
        { label: "Quorum", value: consensusStats?.quorum_size !== undefined ? consensusStats.quorum_size.toLocaleString() : "--" },
      ],
    },
    {
      name: "P2P Quorum",
      status: mapStatus(checks.p2p_quorum),
      details:
        typeof readiness?.details?.p2p_quorum_error === "string"
          ? readiness?.details?.p2p_quorum_error
          : undefined,
      icon: <Globe2 className="h-5 w-5 text-primary" />,
      metrics: [
        {
          label: "Peers",
          value: `${networkStats?.peer_count ?? 0} (${networkStats?.inbound_peers ?? 0} in / ${networkStats?.outbound_peers ?? 0} out)`,
        },
        { label: "Latency", value: formatMilliseconds(networkStats?.avg_latency_ms) },
        {
          label: "Throughput",
          value: `${formatBytes(networkStats?.inbound_throughput_bytes_per_second)} /s in · ${formatBytes(networkStats?.outbound_throughput_bytes_per_second)} /s out`,
        },
      ],
    },
    {
      name: "Kafka",
      status: mapStatus(checks.kafka),
      details:
        typeof readiness?.details?.kafka_error === "string" ? readiness?.details?.kafka_error : undefined,
      icon: <RadioTower className="h-5 w-5 text-primary" />,
      metrics: [
        { label: "Publish P95", value: formatMilliseconds(kafkaStats?.publish_latency_p95_ms ?? kafkaStats?.publish_latency_ms) },
        { label: "Lag", value: formatCount(kafkaLagTotal) },
        { label: "Highwater", value: formatCount(kafkaHighWater) },
      ],
    },
    {
      name: "Redis",
      status: mapStatus(checks.redis),
      details:
        typeof readiness?.details?.redis_error === "string" ? readiness?.details?.redis_error : undefined,
      icon: <Brain className="h-5 w-5 text-primary" />,
      metrics: [
        { label: "Latency", value: formatMilliseconds(redisStats?.command_latency_p95_ms ?? redisStats?.ready_latency_ms) },
        { label: "Clients", value: formatCount(redisStats?.connected_clients) },
        { label: "Errors", value: formatCount(redisStats?.command_errors_total) },
      ],
    },
    {
      name: "CockroachDB",
      status: mapStatus(checks.cockroach),
      details:
        typeof readiness?.details?.cockroach_message === "string"
          ? (readiness.details.cockroach_message as string)
          : undefined,
      icon: <Bug className="h-5 w-5 text-primary" />,
      metrics: [
        { label: "Version", value: storageStats?.version ?? "--" },
        { label: "Txn P95", value: formatMilliseconds(storageStats?.transaction_latency_p95_ms) },
        { label: "Slow Txns", value: formatCount(storageStats?.slow_transaction_count) },
      ],
    },
  ]

  if (aiHealth?.ready) {
    const aiReady = aiHealth.ready as { ready?: boolean; state?: string }
    const aiServiceStatus: ServiceStatus = (() => {
      if (aiStats?.detection_loop_status === "critical" || aiStats?.detection_loop_status === "stopped" || aiStats?.detection_loop_blocking) {
        return "critical"
      }
      if (aiStats?.detection_loop_status === "degraded") {
        return "warning"
      }
      return aiReady?.ready ? "healthy" : "warning"
    })()

    const detailParts: string[] = []
    if (aiReady?.state) {
      detailParts.push(`State: ${aiReady.state}`)
    }
    if (aiStats?.detection_loop_status && aiStats.detection_loop_status !== "ok") {
      detailParts.push(`Loop: ${aiStats.detection_loop_status}`)
    }
    if (aiStats?.message) {
      detailParts.push(aiStats.message)
    } else if (aiStats?.detection_loop_issues && aiStats.detection_loop_issues.length > 0) {
      detailParts.push(aiStats.detection_loop_issues.slice(0, 2).join("; "))
    }

    services.push({
      name: "AI Service",
      status: aiServiceStatus,
      details: detailParts.length > 0 ? detailParts.join(" • ") : undefined,
      icon: <BrainCircuit className="h-5 w-5 text-primary" />,
      metrics: [
        { label: "Loop", value: aiStats?.detection_loop_status ?? (aiStats?.detection_loop_running ? "ok" : "stopped") },
        { label: "Publish/min", value: formatNumber(aiStats?.publish_rate_per_minute, 1) },
        { label: "Detections/min", value: formatNumber(aiStats?.detections_per_minute, 1) },
        { label: "Latency", value: formatMilliseconds(aiLatencyMs ?? aiStats?.detection_loop_avg_latency_ms) },
      ],
    })
  }

  const lastChecked = timestamp ? new Date(timestamp * 1000).toLocaleString() : "--"

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Service Health</h2>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <span>Last checked</span>
          <Badge variant="outline">{lastChecked}</Badge>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-6">
        {services.map((service) => {
          const statusInfo = statusVariant(service.status)

          return (
            <Card key={service.name} className="glass-card hover:shadow-lg transition-all duration-200">
              <CardHeader className="pb-4">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-lg font-semibold flex items-center gap-2">
                    <span>{service.icon}</span>
                    {service.name}
                  </CardTitle>
                  <Badge variant={statusInfo.variant} className={cn("text-xs capitalize", statusInfo.className)}>
                    {service.status}
                  </Badge>
                </div>
              </CardHeader>

              <CardContent className="space-y-3 text-sm">
                {service.metrics && service.metrics.length > 0 ? (
                  <div className="space-y-2">
                    {service.metrics.map((metric) => (
                      <div key={`${service.name}-${metric.label}`} className="flex items-center justify-between text-xs">
                        <span className="text-muted-foreground">{metric.label}</span>
                        <span className="text-foreground font-semibold">{metric.value}</span>
                      </div>
                    ))}
                  </div>
                ) : null}
                <p className="text-xs text-muted-foreground">
                  {service.details ?? "No issues reported"}
                </p>
              </CardContent>
            </Card>
          )
        })}
      </div>
    </div>
  )
}
