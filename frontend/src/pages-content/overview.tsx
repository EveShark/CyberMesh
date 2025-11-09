"use client"

import Link from "next/link"
import {
  Activity,
  AlertTriangle,
  ArrowDownRight,
  ArrowUpRight,
  BrainCircuit,
  CheckCircle2,
  Loader2,
  Network,
  RefreshCw,
  ShieldAlert,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { NetworkSummaryCompact } from "@/components/network-summary-compact"
import { ThreatSummaryCompact } from "@/components/threat-summary-compact"
import { InfrastructureSummaryCompact } from "@/components/infrastructure-summary-compact"
import { BlocksTable } from "@/components/blockchain/blocks-table"
import { useOverviewData } from "@/hooks/use-overview-data"
import { PageContainer } from "@/components/page-container"

function formatNumber(value?: number, fractionDigits = 0) {
  if (value === undefined || Number.isNaN(value)) return "--"
  return value.toLocaleString(undefined, { maximumFractionDigits: fractionDigits })
}

function formatPercent(value?: number, fractionDigits = 1) {
  if (value === undefined || Number.isNaN(value)) return "--"
  return `${value.toFixed(fractionDigits)}%`
}

function formatMilliseconds(ms?: number) {
  if (ms === undefined || Number.isNaN(ms)) return "--"
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)} s`
  return `${ms.toFixed(1)} ms`
}

function formatBytes(bytes?: number) {
  if (bytes === undefined || bytes === null || Number.isNaN(bytes)) return "--"
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let value = bytes
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index++
  }
  return `${value.toFixed(value >= 10 || index === 0 ? 0 : 1)} ${units[index]}`
}

function formatTime(timestampMs?: number) {
  if (!timestampMs) return "--"
  return new Date(timestampMs).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

function formatDelta(value?: number, suffix = "") {
  if (value === undefined || Number.isNaN(value)) return "--"
  const sign = value > 0 ? "+" : value < 0 ? "-" : "±"
  const magnitude = Math.abs(value)
  return `${sign}${magnitude.toFixed(1)}${suffix}`
}

interface HeroCardProps {
  title: string
  statusLabel?: string
  statusTone?: "success" | "warning" | "default"
  value: string
  helper?: string
  footer?: string
  icon: React.ComponentType<{ className?: string }>
}

function HeroCard({ title, statusLabel, statusTone = "default", value, helper, footer, icon: Icon }: HeroCardProps) {
  const badgeClass =
    statusTone === "success"
      ? "bg-emerald-500/10 text-emerald-400"
      : statusTone === "warning"
        ? "bg-yellow-500/10 text-yellow-400"
        : "bg-primary/10 text-primary"

  return (
    <Card className="glass-card border border-border/30">
      <CardContent className="p-6 space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm font-medium text-muted-foreground">{title}</p>
            {helper ? <p className="text-xs text-muted-foreground/80">{helper}</p> : null}
          </div>
          <div className="flex items-center gap-2">
            {statusLabel ? <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${badgeClass}`}>{statusLabel}</span> : null}
            <Icon className="h-5 w-5 text-primary" />
          </div>
        </div>
        <p className="text-3xl font-bold text-foreground font-mono tracking-tight">{value}</p>
        {footer ? <p className="text-xs text-muted-foreground">{footer}</p> : null}
      </CardContent>
    </Card>
  )
}

interface TrendChipProps {
  label: string
  value: string
  delta?: number
  suffix?: string
}

function TrendChip({ label, value, delta, suffix = "" }: TrendChipProps) {
  const hasDelta = delta !== undefined && !Number.isNaN(delta)
  const deltaValue = hasDelta ? formatDelta(delta, suffix) : undefined
  const deltaIsNegative = hasDelta ? (delta as number) < 0 : false
  const DeltaIcon = hasDelta ? (deltaIsNegative ? ArrowDownRight : ArrowUpRight) : null

  return (
    <div className="flex flex-col gap-1 rounded-lg border border-border/40 bg-background/60 px-3 py-2 text-xs">
      <span className="font-medium text-muted-foreground">{label}</span>
      <div className="flex items-center justify-between text-sm font-semibold text-foreground">
        <span>{value}</span>
        {DeltaIcon && deltaValue ? (
          <span className={`flex items-center gap-1 text-xs ${deltaIsNegative ? "text-emerald-400" : "text-yellow-400"}`}>
            <DeltaIcon className="h-3 w-3" />
            {deltaValue}
          </span>
        ) : null}
      </div>
    </div>
  )
}

export default function OverviewPage() {
  const {
    isLoading,
    error,
    data,
    metrics,
    network,
    ai,
    latestBlocks,
    overviewKpis,
    trendStats,
    alerts,
    refresh,
    blockPagination,
  } = useOverviewData()

  const backendStatus = overviewKpis.readiness.status === "ready" ? "READY" : "DEGRADED"
  const backendBadgeTone = overviewKpis.readiness.status === "ready" ? "success" : "warning"
  const backendChecksHelper = overviewKpis.readiness.total
    ? `${overviewKpis.readiness.passing}/${overviewKpis.readiness.total} checks passing`
    : "No checks reported"

  const aiStatusLabel = overviewKpis.ai.status === "running" ? "Running" : "Paused"
  const aiCardFooter = `Avg latency ${formatMilliseconds(overviewKpis.ai.avgLatencyMs)}`
  const aiCardHelper = overviewKpis.ai.publishRate
    ? `${formatNumber(overviewKpis.ai.publishRate, 1)} publish/min`
    : "Publish rate unavailable"

  const networkHelper = `Avg latency ${formatMilliseconds(overviewKpis.network.avgLatencyMs)}`
  const networkFooter = overviewKpis.network.totalPeers
    ? `${formatNumber(overviewKpis.network.peerCount)} of ${formatNumber(overviewKpis.network.totalPeers)} peers connected`
    : undefined

  const apiHelper = formatNumber(overviewKpis.api.totalRequests) + " total requests"
  const apiFooter = `Errors ${formatPercent(overviewKpis.api.errorRate, 2)}`

  const trendChips = [
    {
      label: "Detections/min",
      value: formatNumber(trendStats.detectionsRate, 1),
    },
    {
      label: "Publish rate",
      value: formatNumber(trendStats.publishRate, 1),
    },
    {
      label: "Latency Δ",
      value: formatDelta(trendStats.latencyDelta, " ms"),
      delta: trendStats.latencyDelta,
      suffix: " ms",
    },
    {
      label: "Blocks/hour",
      value: formatNumber(trendStats.blocksPerHour, 1),
    },
  ]

  return (
    <div className="min-h-screen">
      <PageContainer align="left" className="py-6 lg:py-8 space-y-8">
        <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-3xl font-bold text-foreground">CyberMesh Overview</h1>
            <p className="text-sm text-muted-foreground">Unified operating snapshot across consensus, AI, and infrastructure</p>
          </div>
          <div className="flex items-center gap-3">
            {error ? (
              <Badge variant="destructive" className="flex items-center gap-1 text-xs">
                <AlertTriangle className="h-3 w-3" /> Sync error
              </Badge>
            ) : null}
            <Badge variant={isLoading ? "outline" : "default"} className="flex items-center gap-1 text-xs">
              {isLoading ? <Loader2 className="h-3 w-3 animate-spin" /> : "Live"}
            </Badge>
            <Button variant="outline" size="sm" onClick={() => refresh()} disabled={isLoading}>
              <RefreshCw className="h-4 w-4 mr-2" /> Refresh
            </Button>
          </div>
        </header>

        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <HeroCard
            title="Backend Readiness"
            statusLabel={backendStatus}
            statusTone={backendBadgeTone}
            value={backendChecksHelper}
            helper={`Last check ${formatTime(overviewKpis.readiness.lastCheckedMs)}`}
            footer={`Uptime ${formatNumber(data?.backend.uptimeSeconds ? Math.floor(data.backend.uptimeSeconds / 3600) : undefined)}h (approx)`}
            icon={CheckCircle2}
          />
          <HeroCard
            title="AI Service"
            statusLabel={aiStatusLabel}
            statusTone={overviewKpis.ai.status === "running" ? "success" : "warning"}
            value={aiCardHelper}
            helper={`Updated ${formatTime(overviewKpis.ai.lastUpdatedMs)}`}
            footer={aiCardFooter}
            icon={BrainCircuit}
          />
          <HeroCard
            title="Network Health"
            statusLabel="Topology"
            value={networkFooter ?? "Peers data unavailable"}
            helper={networkHelper}
            footer={overviewKpis.network.latencyDelta !== undefined ? `Δ ${formatDelta(overviewKpis.network.latencyDelta, " ms")} past window` : undefined}
            icon={Network}
          />
          <HeroCard
            title="API Throughput"
            statusLabel="Traffic"
            value={formatNumber(overviewKpis.api.totalRequests)}
            helper={apiHelper}
            footer={apiFooter}
            icon={Activity}
          />
        </section>

        <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {trendChips.map((chip) => (
            <TrendChip key={chip.label} {...chip} />
          ))}
        </section>

        {data.ledger ? (
          <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            <Card className="glass-card border border-border/30">
              <CardContent className="p-6 space-y-2">
                <p className="text-sm text-muted-foreground">Ledger Height</p>
                <p className="text-3xl font-bold text-foreground">
                  {formatNumber(data.ledger.latest_height)}
                </p>
                <p className="text-xs text-muted-foreground">
                  State version {formatNumber(data.ledger.state_version)}
                </p>
              </CardContent>
            </Card>
            <Card className="glass-card border border-border/30">
              <CardContent className="p-6 space-y-2">
                <p className="text-sm text-muted-foreground">Pending Transactions</p>
                <p className="text-3xl font-bold text-foreground">
                  {formatNumber(data.ledger.pending_transactions ?? 0)}
                </p>
                <p className="text-xs text-muted-foreground">
                  Mempool size {formatBytes(data.ledger.mempool_size_bytes ?? undefined)}
                </p>
              </CardContent>
            </Card>
            <Card className="glass-card border border-border/30">
              <CardContent className="p-6 space-y-2">
                <p className="text-sm text-muted-foreground">Snapshot</p>
                <p className="text-3xl font-bold text-foreground">
                  {data.ledger.snapshot_block_height
                    ? `#${formatNumber(data.ledger.snapshot_block_height)}`
                    : "—"}
                </p>
                <p className="text-xs text-muted-foreground">
                  {data.ledger.snapshot_timestamp
                    ? new Date(data.ledger.snapshot_timestamp * 1000).toLocaleString()
                    : "Snapshot not captured"}
                </p>
              </CardContent>
            </Card>
          </section>
        ) : null}

        <section className="grid gap-6 md:grid-cols-3">
          <NetworkSummaryCompact
            peerCount={network?.peerCount}
            totalPeers={network?.totalPeers}
            consensusRound={network?.consensusRound}
            leaderStability={network?.leaderStability}
            averageLatencyMs={network?.averageLatencyMs}
          />
          <ThreatSummaryCompact
            detectionsTotal={ai?.detectionsTotal}
            criticalCount={ai?.severityBreakdown?.critical}
            highCount={ai?.severityBreakdown?.high}
            status={ai?.status}
            lastUpdated={ai?.lastUpdated}
          />
          <InfrastructureSummaryCompact
            kafkaSuccessTotal={metrics?.kafkaPublishSuccessTotal}
            kafkaFailureTotal={metrics?.kafkaPublishFailureTotal}
            kafkaBrokerCount={metrics?.kafkaBrokerCount}
            redisHits={metrics?.redisPoolHits}
            redisMisses={metrics?.redisPoolMisses}
            redisTotalConnections={metrics?.redisPoolTotalConnections}
            cockroachOpenConnections={metrics?.cockroachOpenConnections}
            residentMemoryBytes={metrics?.residentMemoryBytes}
            uptimeSeconds={data?.backend.uptimeSeconds}
          />
        </section>

        <section className="grid gap-6 lg:grid-cols-2">
          <Card className="glass-card border border-border/30">
            <CardContent className="space-y-4 p-6">
              <div className="flex items-center justify-between">
                <div>
                  <h2 className="text-xl font-semibold text-foreground">Recent Alerts</h2>
                  <p className="text-sm text-muted-foreground">Severity snapshot from AI detections</p>
                </div>
                <ShieldAlert className="h-5 w-5 text-primary" />
              </div>
              <div className="grid grid-cols-2 gap-3 text-sm">
                <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                  <p className="text-xs text-muted-foreground">Critical</p>
                  <p className="text-lg font-semibold text-foreground">{formatNumber(alerts.critical)}</p>
                </div>
                <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                  <p className="text-xs text-muted-foreground">High</p>
                  <p className="text-lg font-semibold text-foreground">{formatNumber(alerts.high)}</p>
                </div>
                <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                  <p className="text-xs text-muted-foreground">Medium</p>
                  <p className="text-lg font-semibold text-foreground">{formatNumber(alerts.medium)}</p>
                </div>
                <div className="rounded-lg border border-border/30 bg-background/60 p-3">
                  <p className="text-xs text-muted-foreground">Low</p>
                  <p className="text-lg font-semibold text-foreground">{formatNumber(alerts.low)}</p>
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                Published {formatNumber(alerts.published)} of {formatNumber(alerts.totalDetections)} detections • Updated {formatTime(alerts.lastUpdated)}
              </p>
            </CardContent>
          </Card>

          <Card className="glass-card border border-border/30">
            <CardContent className="flex flex-col gap-4 p-6">
              <div>
                <h2 className="text-xl font-semibold text-foreground">Deep dives</h2>
                <p className="text-sm text-muted-foreground">Jump into detailed telemetry views</p>
              </div>
              <div className="flex flex-col gap-2 sm:flex-row">
                <Button asChild variant="secondary" className="justify-center">
                  <Link href="/system-health">System Health</Link>
                </Button>
                <Button asChild variant="secondary" className="justify-center">
                  <Link href="/ai-engine">AI Engine</Link>
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">Use these shortcuts for rapid triage and remediation workflows.</p>
            </CardContent>
          </Card>
        </section>

        <section>
          <BlocksTable blocks={latestBlocks} isLoading={isLoading} pagination={blockPagination} />
        </section>
      </PageContainer>
    </div>
  )
}
