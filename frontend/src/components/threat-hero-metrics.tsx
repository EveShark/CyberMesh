"use client"

import { Card, CardContent } from "@/components/ui/card"
import { Activity, Shield, AlertTriangle, Zap } from "lucide-react"

interface ThreatHeroMetricsProps {
  totalDetected: number
  published: number
  abstained: number
  avgResponseTimeMs?: number
  validatorCount?: number
  quorumSize?: number
  isLoading?: boolean
  lastUpdated?: number
  scopeLabel?: string
}

export function ThreatHeroMetrics({
  totalDetected,
  published,
  abstained,
  avgResponseTimeMs,
  validatorCount,
  quorumSize,
  isLoading = false,
  lastUpdated,
  scopeLabel,
}: ThreatHeroMetricsProps) {
  const publishedPercent = totalDetected > 0 ? Math.round((published / totalDetected) * 100) : 0
  const abstainedPercent = totalDetected > 0 ? Math.round((abstained / totalDetected) * 100) : 0

  const formatNumber = (num: number) => {
    return num.toLocaleString()
  }

  const formatResponseTime = (ms?: number) => {
    if (ms === undefined || ms === null) return "N/A"
    if (ms < 1) return "<1ms"
    if (ms < 1000) return `${Math.round(ms)}ms`
    return `${(ms / 1000).toFixed(2)}s`
  }

  const metrics = [
    {
      title: "Threats Detected",
      value: formatNumber(totalDetected),
      subtitle: scopeLabel ?? "Total detections",
      icon: Activity,
      iconColor: "text-blue-500",
      bgGradient: "from-blue-500/10 to-blue-500/5",
    },
    {
      title: "Published",
      value: formatNumber(published),
      subtitle: `${publishedPercent}% of total`,
      icon: Shield,
      iconColor: "text-green-500",
      bgGradient: "from-green-500/10 to-green-500/5",
      tooltip: "Threats AI recommended for blocking",
    },
    {
      title: "Abstained",
      value: formatNumber(abstained),
      subtitle: `${abstainedPercent}% of total`,
      icon: AlertTriangle,
      iconColor: "text-yellow-500",
      bgGradient: "from-yellow-500/10 to-yellow-500/5",
      tooltip: "Threats marked uncertain by AI",
    },
    {
      title: "Avg Response Time",
      value: formatResponseTime(avgResponseTimeMs),
      subtitle: avgResponseTimeMs ? "Detection to execution" : "Pending backend data",
      icon: Zap,
      iconColor: "text-purple-500",
      bgGradient: "from-purple-500/10 to-purple-500/5",
      tooltip: "Average time from threat detection to blockchain commit",
    },
  ]

  if (isLoading) {
    return (
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
        {[1, 2, 3, 4].map((i) => (
          <Card key={i} className="glass-card border-white/10 animate-pulse">
            <CardContent className="p-6">
              <div className="h-24 bg-white/5 rounded" />
            </CardContent>
          </Card>
        ))}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {scopeLabel && (
        <div className="flex items-center justify-between text-xs uppercase tracking-wide text-muted-foreground">
          <span>{scopeLabel}</span>
        </div>
      )}
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
        {metrics.map((metric, index) => {
          const Icon = metric.icon
          return (
            <Card
              key={index}
              className="glass-card border-white/10 hover:border-white/20 transition-all duration-300 hover:scale-[1.02]"
              title={metric.tooltip}
            >
              <CardContent className="p-6">
                <div className={`flex items-start justify-between mb-4`}>
                  <div className={`p-3 rounded-lg bg-gradient-to-br ${metric.bgGradient}`}>
                    <Icon className={`h-6 w-6 ${metric.iconColor}`} />
                  </div>
                  <div className="text-right">
                    <div className="text-2xl font-bold text-foreground">{metric.value}</div>
                  </div>
                </div>
                <div>
                  <div className="text-sm font-medium text-muted-foreground mb-1">{metric.title}</div>
                  <div className="text-xs text-muted-foreground/80">{metric.subtitle}</div>
                </div>
              </CardContent>
            </Card>
          )
        })}
      </div>

      {/* Status bar */}
      <div className="flex items-center justify-between text-xs text-muted-foreground px-1">
        <div className="flex items-center gap-4">
          {validatorCount !== undefined && (
            <span>
              Validators: <span className="font-medium text-foreground">{validatorCount}</span>
            </span>
          )}
          {quorumSize !== undefined && (
            <span>
              Quorum: <span className="font-medium text-foreground">{quorumSize}</span>
            </span>
          )}
        </div>
        {lastUpdated && (
          <div>
            Last updated: <span className="font-medium">{new Date(lastUpdated).toLocaleTimeString()}</span>
          </div>
        )}
      </div>
    </div>
  )
}
