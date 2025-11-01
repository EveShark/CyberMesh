"use client"

import { Card } from "@/components/ui/card"
import { Activity, FileText, Clock, CheckCircle, AlertTriangle, Radio } from "lucide-react"

interface MetricsBarProps {
  latestHeight: number
  totalTransactions: number
  avgBlockTime: number
  successRate: number
  anomalyCount: number
  isLive: boolean
}

interface MetricCardProps {
  icon: React.ReactNode
  label: string
  value: string | number
  subtext?: string
  variant?: "default" | "success" | "warning" | "danger"
  onClick?: () => void
}

function MetricCard({ icon, label, value, subtext, variant = "default", onClick }: MetricCardProps) {
  const variantClasses = {
    default: "border-border/40 hover:border-primary/40",
    success: "border-status-healthy/40 hover:border-status-healthy/60",
    warning: "border-status-warning/40 hover:border-status-warning/60",
    danger: "border-status-critical/40 hover:border-status-critical/60",
  }

  const iconClasses = {
    default: "text-primary",
    success: "text-status-healthy",
    warning: "text-status-warning",
    danger: "text-status-critical",
  }

  return (
    <Card
      className={`glass-card p-4 transition-all ${variantClasses[variant]} ${
        onClick ? "cursor-pointer hover:scale-105" : ""
      }`}
      onClick={onClick}
    >
      <div className="flex items-start gap-3">
        <div className={`p-2 rounded-lg bg-card/60 ${iconClasses[variant]}`}>{icon}</div>
        <div className="flex-1 min-w-0">
          <p className="text-xs text-muted-foreground uppercase tracking-wide">{label}</p>
          <p className="text-2xl font-bold text-foreground mt-1 tabular-nums">{value}</p>
          {subtext && <p className="text-xs text-muted-foreground mt-1">{subtext}</p>}
        </div>
      </div>
    </Card>
  )
}

export function MetricsBar({
  latestHeight,
  totalTransactions,
  avgBlockTime,
  successRate,
  anomalyCount,
  isLive,
}: MetricsBarProps) {
  // Format large numbers with commas
  const formatNumber = (num: number) => {
    return num.toLocaleString()
  }

  // Format block time
  const formatBlockTime = (seconds: number) => {
    if (seconds < 1) return `${(seconds * 1000).toFixed(0)}ms`
    return `${seconds.toFixed(1)}s`
  }

  // Format success rate
  const formatSuccessRate = (rate: number) => {
    return `${(rate * 100).toFixed(1)}%`
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 lg:grid-cols-6 gap-4">
      {/* Block Height */}
      <MetricCard
        icon={<Activity className="h-5 w-5" />}
        label="Latest Height"
        value={formatNumber(latestHeight)}
        subtext="Current block"
        variant="default"
      />

      {/* Total Transactions */}
      <MetricCard
        icon={<FileText className="h-5 w-5" />}
        label="Total TXs"
        value={formatNumber(totalTransactions)}
        subtext="All time"
        variant="default"
      />

      {/* Avg Block Time */}
      <MetricCard
        icon={<Clock className="h-5 w-5" />}
        label="Avg Block Time"
        value={formatBlockTime(avgBlockTime)}
        subtext="Last 20 blocks"
        variant="default"
      />

      {/* Success Rate */}
      <MetricCard
        icon={<CheckCircle className="h-5 w-5" />}
        label="Success Rate"
        value={formatSuccessRate(successRate)}
        subtext={successRate >= 0.99 ? "Excellent" : successRate >= 0.95 ? "Good" : "Needs attention"}
        variant={successRate >= 0.99 ? "success" : successRate >= 0.95 ? "warning" : "danger"}
      />

      {/* Anomaly Count */}
      <MetricCard
        icon={<AlertTriangle className="h-5 w-5" />}
        label="Anomaly Blocks"
        value={anomalyCount}
        subtext={anomalyCount > 0 ? "Click to filter" : "No threats"}
        variant={anomalyCount > 10 ? "danger" : anomalyCount > 0 ? "warning" : "success"}
        onClick={anomalyCount > 0 ? () => console.log("Filter anomalies") : undefined}
      />

      {/* Live Status */}
      <MetricCard
        icon={
          <div className="relative">
            <Radio className="h-5 w-5" />
            {isLive && (
              <span className="absolute -top-1 -right-1 flex h-3 w-3">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-status-healthy opacity-75"></span>
                <span className="relative inline-flex rounded-full h-3 w-3 bg-status-healthy"></span>
              </span>
            )}
          </div>
        }
        label="Stream Status"
        value={isLive ? "Live" : "Paused"}
        variant={isLive ? "success" : "default"}
      />
    </div>
  )
}
