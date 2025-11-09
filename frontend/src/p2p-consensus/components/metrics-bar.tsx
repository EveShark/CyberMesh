import { Card } from "@/components/ui/card"
import { TrendingUp, Users, Zap, RefreshCcw, Target, ArrowDownCircle, ArrowUpCircle } from "lucide-react"
import type { LucideIcon } from "lucide-react"

interface MetricsBarProps {
  connectedPeers: number
  expectedPeers: number
  avgLatency: number
  consensusRound: number
  leaderStability: number
  inboundRateBps?: number
  outboundRateBps?: number
}

const formatNumber = (value?: number, digits = 0) => {
  if (value === undefined || value === null || Number.isNaN(value)) return "--"
  if (!Number.isFinite(value)) return "--"
  if (digits > 0) {
    return value.toFixed(digits)
  }
  return Math.round(value).toLocaleString()
}

const formatRateMetric = (rate?: number) => {
  if (!rate || rate <= 0) return "--"
  if (rate >= 1_000_000) return `${(rate / 1_000_000).toFixed(1)} MB/s`
  if (rate >= 1_000) return `${(rate / 1_000).toFixed(1)} KB/s`
  if (rate >= 100) return `${rate.toFixed(0)} B/s`
  return `${rate.toFixed(1)} B/s`
}

interface MetricCard {
  label: string
  value: string
  unit?: string
  icon: LucideIcon
}

export default function MetricsBar({
  connectedPeers,
  expectedPeers,
  avgLatency,
  consensusRound,
  leaderStability,
  inboundRateBps,
  outboundRateBps,
}: MetricsBarProps) {
  const connectedValue = expectedPeers > 0 ? `${connectedPeers}/${expectedPeers}` : formatNumber(connectedPeers)

  const metrics: MetricCard[] = [
    { label: "Connected Peers", value: connectedValue, unit: "", icon: Users },
    { label: "Avg Latency", value: formatNumber(avgLatency), unit: "ms", icon: Zap },
    { label: "Consensus Round", value: formatNumber(consensusRound), unit: "", icon: RefreshCcw },
    { label: "Leader Stability", value: formatNumber(leaderStability, 1), unit: "%", icon: Target },
  ]

  // Always render rate cards to avoid layout flicker during idle periods
  metrics.push({ label: "Inbound Rate", value: formatRateMetric(inboundRateBps), icon: ArrowDownCircle })
  metrics.push({ label: "Outbound Rate", value: formatRateMetric(outboundRateBps), icon: ArrowUpCircle })

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4 w-full">
      {metrics.map(({ label, value, unit, icon: Icon }) => (
        <Card
          key={label}
          className="glass-card border border-border/40 p-5 transition-all duration-300 hover:shadow-glow"
        >
          <div className="flex items-start justify-between mb-3">
            <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</span>
            <Icon className="h-4 w-4 text-muted-foreground" />
          </div>
          <div className="flex items-baseline gap-1">
            <span className="font-mono text-2xl font-semibold text-foreground">{value ?? "--"}</span>
            {unit ? <span className="text-sm text-muted-foreground">{unit}</span> : null}
          </div>
          <div className="mt-3 inline-flex items-center gap-1 rounded-full bg-muted/40 px-2 py-1 text-xs font-medium text-muted-foreground">
            <TrendingUp className="h-3 w-3 text-muted-foreground" />
            Live telemetry
          </div>
        </Card>
      ))}
    </div>
  )
}
