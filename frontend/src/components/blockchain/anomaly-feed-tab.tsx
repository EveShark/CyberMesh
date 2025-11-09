"use client"

import { useMemo, useState, type CSSProperties } from "react"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { AlertTriangle, Shield, Activity, Clock, Filter, ChevronDown, Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"

import type { AnomalyDetail, AnomalyStatsResponse } from "@/lib/api"
import { useThreatsData } from "@/hooks/use-threats-data"

const SEVERITY_COLORS = {
  critical: "var(--status-critical)",
  high: "var(--status-warning)",
  medium: "var(--color-threat-medium)",
  low: "var(--color-threat-low)",
} as const

const mixColor = (color: string, percent: number) => `color-mix(in srgb, ${color} ${percent}%, transparent)`

export function AnomalyFeedTab() {
  const [severityFilter, setSeverityFilter] = useState("all")
  const [displayCount, setDisplayCount] = useState(10)

  const { feed, threatStats, isLoading, error, mutate } = useThreatsData(10000)

  const filteredFeed = useMemo(() => {
    if (!feed) return []
    if (severityFilter === "all") return feed
    return feed.filter((item) => item.severity === severityFilter)
  }, [feed, severityFilter])

  const visibleAnomalies = useMemo(() => filteredFeed.slice(0, displayCount), [filteredFeed, displayCount])

  const stats: AnomalyStatsResponse | null = useMemo(() => {
    if (threatStats) return threatStats
    if (!feed) return null
    return feed.reduce<AnomalyStatsResponse>(
      (acc, anomaly) => {
        acc.total_count += 1
        acc[`${anomaly.severity}_count` as keyof AnomalyStatsResponse] += 1
        return acc
      },
      { critical_count: 0, high_count: 0, medium_count: 0, low_count: 0, total_count: 0 },
    )
  }, [feed, threatStats])

  const criticalCount = stats?.critical_count ?? 0
  const highCount = stats?.high_count ?? 0
  const mediumCount = stats?.medium_count ?? 0
  const lowCount = stats?.low_count ?? 0

  const getSeverityStyles = (severity: AnomalyDetail["severity"]) => {
    const color = SEVERITY_COLORS[severity]
    return {
      className: "border",
      style: {
        color,
        borderColor: mixColor(color, 40),
        backgroundColor: mixColor(color, 20),
      } as CSSProperties,
    }
  }

  const getTypeBadge = (type: string) => {
    const labels: Record<string, string> = {
      ddos: "DDoS",
      malware: "Malware",
      phishing: "Phishing",
      botnet: "Botnet",
      data_exfiltration: "Data Exfil",
      ransomware: "Ransomware",
      exploit: "Exploit",
      backdoor: "Backdoor",
    }
    return labels[type] || type.toUpperCase()
  }

  const getRelativeTime = (timestamp: number) => {
    const now = Date.now()
    const epochMillis = timestamp < 1_000_000_000_000 ? timestamp * 1000 : timestamp
    const diff = now - epochMillis
    const seconds = Math.floor(diff / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)

    if (hours > 0) return `${hours}h ago`
    if (minutes > 0) return `${minutes}m ago`
    return `${seconds}s ago`
  }

  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-lg font-semibold text-foreground">Recent Anomalies</h3>
        <p className="text-sm text-muted-foreground mt-1">Live threat detection feed from blockchain consensus</p>
      </div>

      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <Card className="bg-status-critical/10 border-status-critical/40 p-3">
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">Critical</p>
            <p className="text-2xl font-bold text-status-critical">{criticalCount}</p>
          </div>
        </Card>
        <Card className="bg-status-warning/10 border-status-warning/40 p-3">
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">High</p>
            <p className="text-2xl font-bold text-status-warning">{highCount}</p>
          </div>
        </Card>
        <Card
          className="p-3"
          style={{
            backgroundColor: mixColor(SEVERITY_COLORS.medium, 10),
            borderColor: mixColor(SEVERITY_COLORS.medium, 40),
          }}
        >
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">Medium</p>
            <p className="text-2xl font-bold" style={{ color: SEVERITY_COLORS.medium }}>
              {mediumCount}
            </p>
          </div>
        </Card>
        <Card
          className="p-3"
          style={{
            backgroundColor: mixColor(SEVERITY_COLORS.low, 10),
            borderColor: mixColor(SEVERITY_COLORS.low, 40),
          }}
        >
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">Low</p>
            <p className="text-2xl font-bold" style={{ color: SEVERITY_COLORS.low }}>
              {lowCount}
            </p>
          </div>
        </Card>
      </div>

      <div className="flex items-center gap-2 flex-wrap">
        <Filter className="h-4 w-4 text-muted-foreground" />
        <Select value={severityFilter} onValueChange={setSeverityFilter}>
          <SelectTrigger className="w-40">
            <SelectValue placeholder="Severity" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Severities</SelectItem>
            <SelectItem value="critical">Critical</SelectItem>
            <SelectItem value="high">High</SelectItem>
            <SelectItem value="medium">Medium</SelectItem>
            <SelectItem value="low">Low</SelectItem>
          </SelectContent>
        </Select>
        {severityFilter !== "all" && (
          <Button variant="ghost" size="sm" onClick={() => setSeverityFilter("all")}>
            Clear
          </Button>
        )}
        <Button variant="outline" size="sm" onClick={() => mutate?.()} disabled={isLoading}>
          <ChevronDown className="mr-2 h-4 w-4" /> Refresh
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <span className="ml-3 text-muted-foreground">Loading anomalies from blockchain...</span>
        </div>
      ) : null}

      {error && !isLoading ? (
        <Card className="bg-status-critical/10 border-status-critical/40 p-6">
          <div className="flex gap-3 items-start">
            <AlertTriangle className="h-5 w-5 text-status-critical" />
            <div>
              <h4 className="text-sm font-semibold text-status-critical">Failed to load anomalies</h4>
              <p className="text-xs text-muted-foreground">
                {error instanceof Error ? error.message : String(error)}
              </p>
            </div>
          </div>
        </Card>
      ) : null}

      {!isLoading && !error ? (
        <div className="space-y-3">
          {visibleAnomalies.length === 0 ? (
            <Card className="bg-card/40 border border-border/40 p-8 text-center text-muted-foreground">
              {severityFilter === "all"
                ? "No anomalies detected yet. System is monitoring..."
                : `No ${severityFilter} anomalies found`}
            </Card>
          ) : (
            visibleAnomalies.map((anomaly) => {
              const severity = getSeverityStyles(anomaly.severity)
              return (
                <Card
                  key={anomaly.id}
                  className={cn("transition-colors", severity.className)}
                  style={severity.style}
                >
                  <div className="p-4 space-y-3">
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex items-start gap-3 flex-1 min-w-0">
                        <div className="p-2 rounded-lg bg-background/60 shadow-sm">
                          <AlertTriangle className="h-5 w-5" style={{ color: SEVERITY_COLORS[anomaly.severity] }} />
                        </div>
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2 flex-wrap">
                            <h4 className="text-sm font-semibold text-foreground">{anomaly.title}</h4>
                            <Badge variant="outline" className="text-xs">
                              {getTypeBadge(anomaly.type)}
                            </Badge>
                          </div>
                          <p className="text-xs text-muted-foreground mt-1 line-clamp-2">{anomaly.description}</p>
                        </div>
                      </div>
                      <Badge className={cn("text-xs", severity.className)} style={severity.style}>
                        {anomaly.severity.toUpperCase()}
                      </Badge>
                    </div>

                    <div className="flex items-center gap-4 text-xs text-muted-foreground flex-wrap">
                      <div className="flex items-center gap-1">
                        <Shield className="h-3 w-3" />
                        <span>Block #{anomaly.block_height.toLocaleString()}</span>
                      </div>
                      <div className="flex items-center gap-1">
                        <Activity className="h-3 w-3" />
                        <span>Source: {anomaly.source}</span>
                      </div>
                      <div className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        <span>{getRelativeTime(anomaly.timestamp)}</span>
                      </div>
                      <div className="ml-auto font-medium text-foreground">
                        {Math.round((anomaly.confidence ?? 0) * 10) / 10}% confidence
                      </div>
                    </div>
                  </div>
                </Card>
              )
            })
          )}
        </div>
      ) : null}

      {!isLoading && filteredFeed.length > visibleAnomalies.length ? (
        <div className="flex justify-center">
          <Button variant="outline" onClick={() => setDisplayCount((prev) => prev + 10)}>
            <ChevronDown className="h-4 w-4 mr-2" /> Load 10 more
          </Button>
        </div>
      ) : null}

      {!isLoading && filteredFeed.length > 0 ? (
        <div className="text-center text-sm text-muted-foreground">
          Showing {visibleAnomalies.length} of {filteredFeed.length} anomalies
          {severityFilter !== "all" ? ` (filtered by ${severityFilter})` : ""}
        </div>
      ) : null}
    </div>
  )
}
