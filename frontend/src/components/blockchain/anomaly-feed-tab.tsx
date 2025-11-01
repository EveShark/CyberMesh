"use client"

import { useState, useEffect } from "react"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { AlertTriangle, Shield, Activity, Clock, Filter, ChevronDown, Loader2 } from "lucide-react"
import type { AnomalyDetail, AnomalyStatsResponse } from "@/lib/api"

export function AnomalyFeedTab() {
  const [severityFilter, setSeverityFilter] = useState("all")
  const [displayCount, setDisplayCount] = useState(10)
  const [anomalies, setAnomalies] = useState<AnomalyDetail[]>([])
  const [stats, setStats] = useState<AnomalyStatsResponse | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Fetch real anomaly data from backend
  useEffect(() => {
    const fetchAnomalies = async () => {
      try {
        setIsLoading(true)
        setError(null)
        
        const params = new URLSearchParams()
        params.set("limit", "100")
        if (severityFilter !== "all") {
          params.set("severity", severityFilter)
        }

        const res = await fetch(`/api/anomalies?${params.toString()}`, { cache: "no-store" })
        if (!res.ok) {
          const message = await res.text().catch(() => res.statusText)
          throw new Error(message || `Request failed (${res.status})`)
        }

        const data = (await res.json()) as {
          anomalies: { anomalies: AnomalyDetail[] }
          stats: AnomalyStatsResponse
        }

        setAnomalies(data.anomalies?.anomalies ?? [])
        setStats(data.stats ?? null)
      } catch (err) {
        console.error("Failed to fetch anomalies:", err)
        setError(err instanceof Error ? err.message : "Failed to load anomalies")
      } finally {
        setIsLoading(false)
      }
    }

    fetchAnomalies()
  }, [severityFilter])

  // Filter anomalies (already filtered by backend if severity != "all")
  const visibleAnomalies = anomalies.slice(0, displayCount)

  // Get severity color
  const getSeverityColor = (severity: AnomalyDetail["severity"]) => {
    switch (severity) {
      case "critical":
        return "bg-status-critical/20 text-status-critical border-status-critical/40"
      case "high":
        return "bg-status-warning/20 text-status-warning border-status-warning/40"
      case "medium":
        return "bg-yellow-500/20 text-yellow-500 border-yellow-500/40"
      case "low":
        return "bg-blue-500/20 text-blue-500 border-blue-500/40"
    }
  }

  // Get type badge - supports any type from backend
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

  // Format relative time
  const getRelativeTime = (timestamp: number) => {
    const now = Date.now()
    const diff = now - timestamp * 1000
    const seconds = Math.floor(diff / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)

    if (hours > 0) return `${hours}h ago`
    if (minutes > 0) return `${minutes}m ago`
    return `${seconds}s ago`
  }

  // Get counts from real API stats
  const criticalCount = stats?.critical_count || 0
  const highCount = stats?.high_count || 0
  const mediumCount = stats?.medium_count || 0
  const lowCount = stats?.low_count || 0

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h3 className="text-lg font-semibold text-foreground">Recent Anomalies</h3>
        <p className="text-sm text-muted-foreground mt-1">Live threat detection feed from blockchain consensus</p>
      </div>

      {/* Stats Cards */}
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
        <Card className="bg-yellow-500/10 border-yellow-500/40 p-3">
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">Medium</p>
            <p className="text-2xl font-bold text-yellow-500">{mediumCount}</p>
          </div>
        </Card>
        <Card className="bg-blue-500/10 border-blue-500/40 p-3">
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">Low</p>
            <p className="text-2xl font-bold text-blue-500">{lowCount}</p>
          </div>
        </Card>
      </div>

      {/* Filter */}
      <div className="flex items-center gap-2">
        <Filter className="h-4 w-4 text-muted-foreground" />
        <Select value={severityFilter} onValueChange={setSeverityFilter}>
          <SelectTrigger className="w-40">
            <SelectValue />
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
      </div>

      {/* Loading State */}
      {isLoading && (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <span className="ml-3 text-muted-foreground">Loading anomalies from blockchain...</span>
        </div>
      )}

      {/* Error State */}
      {error && !isLoading && (
        <Card className="bg-status-critical/10 border-status-critical/40 p-6">
          <p className="text-status-critical">{error}</p>
        </Card>
      )}

      {/* Anomaly Feed */}
      {!isLoading && !error && (
        <div className="space-y-3">
          {visibleAnomalies.length > 0 ? (
          visibleAnomalies.map((anomaly) => (
            <Card
              key={anomaly.id}
              className="bg-card/40 border-l-4 hover:bg-accent/20 transition-colors"
              style={{
                borderLeftColor:
                  anomaly.severity === "critical"
                    ? "hsl(var(--status-critical))"
                    : anomaly.severity === "high"
                    ? "hsl(var(--status-warning))"
                    : anomaly.severity === "medium"
                    ? "rgb(234 179 8)"
                    : "rgb(59 130 246)",
              }}
            >
              <div className="p-4 space-y-3">
                {/* Header */}
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-start gap-3 flex-1 min-w-0">
                    <div
                      className={`p-2 rounded-lg ${
                        anomaly.severity === "critical" || anomaly.severity === "high"
                          ? "bg-status-critical/20"
                          : "bg-status-warning/20"
                      }`}
                    >
                      <AlertTriangle
                        className={`h-5 w-5 ${
                          anomaly.severity === "critical" || anomaly.severity === "high"
                            ? "text-status-critical"
                            : "text-status-warning"
                        }`}
                      />
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <h4 className="text-sm font-semibold text-foreground">{anomaly.title}</h4>
                        <Badge variant="outline" className="text-xs">
                          {getTypeBadge(anomaly.type)}
                        </Badge>
                      </div>
                      <p className="text-xs text-muted-foreground mt-1">{anomaly.description}</p>
                    </div>
                  </div>
                  <Badge className={getSeverityColor(anomaly.severity)}>
                    {anomaly.severity.toUpperCase()}
                  </Badge>
                </div>

                {/* Details */}
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
                  <div className="ml-auto">
                    <span className="font-medium text-foreground">{anomaly.confidence}%</span> confidence
                  </div>
                </div>
              </div>
            </Card>
            ))
          ) : (
            <Card className="bg-card/40 border border-border/40 p-8">
              <p className="text-center text-muted-foreground">
                {severityFilter === "all" 
                  ? "No anomalies detected yet. System is monitoring..." 
                  : `No ${severityFilter} anomalies found`}
              </p>
            </Card>
          )}
        </div>
      )}

      {/* Load More */}
      {!isLoading && anomalies.length > visibleAnomalies.length && (
        <div className="flex justify-center">
          <Button variant="outline" onClick={() => setDisplayCount((prev) => prev + 10)}>
            <ChevronDown className="h-4 w-4 mr-2" />
            Load More
          </Button>
        </div>
      )}

      {/* Count */}
      {!isLoading && anomalies.length > 0 && (
        <div className="text-center text-sm text-muted-foreground">
          Showing {visibleAnomalies.length} of {anomalies.length} anomalies
          {severityFilter !== "all" && ` (filtered by ${severityFilter})`}
        </div>
      )}
    </div>
  )
}
