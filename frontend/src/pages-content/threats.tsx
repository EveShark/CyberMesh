"use client"

import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ThreatsTable, type ThreatAggregate } from "@/components/threats-table"
import { ThreatDetailDrawer } from "@/components/threat-detail-drawer"
import { ThreatCharts } from "@/components/threat-charts"
import { ThreatHeroMetrics } from "@/components/threat-hero-metrics"
import { ThreatLoadingSkeleton } from "@/components/threat-loading-skeleton"
import { AlertTriangle, RefreshCw } from "lucide-react"
import { useThreatsData } from "@/hooks/use-threats-data"
import { PageContainer } from "@/components/page-container"

export default function ThreatsPage() {
  const { 
    data, 
    severitySeries, 
    trend, 
    threatTypes, 
    lastDetectionTime, 
    mutate, 
    isLoading,
    backendStats,
    isLoadingAny
  } = useThreatsData()
  const [selectedThreat, setSelectedThreat] = useState<ThreatAggregate | null>(null)

  const totals = data?.breakdown.totals
  const lifetimeTotals = data?.lifetimeTotals ?? (totals
    ? {
        total: totals.overall,
        published: totals.published,
        abstained: totals.abstained,
      }
    : undefined)
  const detectionLoopRunning = data?.detectionLoop?.running ?? false
  const detectionLoopStatus = data?.detectionLoop?.status ?? (detectionLoopRunning ? "ok" : "stopped")
  const detectionLoopBlocking = data?.detectionLoop?.blocking ?? false
  const detectionLoopMessage = data?.detectionLoop?.message || (data?.detectionLoop?.issues?.[0] ?? "")
  const threatSource = data?.source ?? "history"
  const fallbackReason = data?.fallbackReason
  const humanizedSource = threatSource.replace(/_/g, " ")
  const humanizedFallback = fallbackReason ? fallbackReason.replace(/_/g, " ") : ""
  const statusVariant = (() => {
    if (detectionLoopStatus === "critical" || detectionLoopStatus === "stopped" || detectionLoopBlocking) {
      return "destructive"
    }
    if (detectionLoopStatus === "degraded") {
      return "outline"
    }
    return detectionLoopRunning ? "destructive" : "secondary"
  })()

  const statusLabel = (() => {
    if (detectionLoopStatus === "critical" || detectionLoopStatus === "stopped" || detectionLoopBlocking) {
      return "HALTED"
    }
    if (detectionLoopStatus === "degraded") {
      return "DEGRADED"
    }
    return detectionLoopRunning ? "LIVE" : "PAUSED"
  })()
  const blockedCount = typeof lifetimeTotals?.published === "number"
    ? lifetimeTotals.published
    : typeof totals?.published === "number"
      ? totals.published
      : null

  return (
    <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20">
      <div className="border-b border-medium bg-card/30 backdrop-blur-sm">
        <PageContainer align="left" className="py-4">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-3xl font-bold tracking-tight">Threats</h1>
              <p className="text-muted-foreground mt-1">Real-time threat monitoring and analysis dashboard</p>
            </div>
            <div className="flex items-center gap-3">
              <Badge variant={statusVariant} className={detectionLoopRunning && statusVariant === "destructive" ? "pulse-ring" : ""}>
                <AlertTriangle className="h-3 w-3 mr-1" />
                {statusLabel}
              </Badge>
              {threatSource === "feed_fallback" ? (
                <Badge variant="outline" className="text-xs border-yellow-500 text-yellow-500">
                  Feed fallback
                </Badge>
              ) : null}
              {threatSource === "empty" ? (
                <Badge variant="secondary" className="text-xs">
                  No detections
                </Badge>
              ) : null}
              <Badge variant="outline" className="text-xs">
                Blocked: {blockedCount !== null ? blockedCount.toLocaleString() : "--"}
              </Badge>
              <Button variant="outline" size="sm" onClick={() => mutate()} disabled={isLoading} className="hover-scale">
                <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? "animate-spin" : ""}`} /> Refresh
              </Button>
            </div>
          </div>
        </PageContainer>
      </div>

      <PageContainer align="left" className="py-8 space-y-8">
        {/* Show loading skeleton on initial load */}
        {isLoadingAny && !data ? (
          <ThreatLoadingSkeleton />
        ) : (
          <>
        {/* Hero Metrics Section */}
        <section>
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-foreground mb-2">Detection Summary</h2>
            <p className="text-muted-foreground">Real-time threat detection metrics and system performance</p>
          </div>
          <ThreatHeroMetrics
            totalDetected={lifetimeTotals?.total ?? 0}
            published={lifetimeTotals?.published ?? 0}
            abstained={lifetimeTotals?.abstained ?? Math.max(0, (lifetimeTotals?.total ?? 0) - (lifetimeTotals?.published ?? 0))}
            avgResponseTimeMs={data?.avgResponseTimeMs}
            validatorCount={backendStats?.consensus?.validator_count}
            quorumSize={backendStats?.consensus?.quorum_size}
            isLoading={isLoadingAny}
            lastUpdated={data?.timestamp}
            scopeLabel="Lifetime totals"
          />
          <p className="text-xs text-muted-foreground/80 mt-2">
            Charts and tables below reflect the most recent detections snapshot (up to 500 events).
          </p>
        </section>

        {/* 2-Column Layout: Charts + Detection Breakdown with Sidebar */}
        <section className="grid gap-6 lg:grid-cols-[2fr,1fr]">
          {/* Left Column */}
          <div className="space-y-6">
            <div>
              <div className="mb-6">
                <h2 className="text-xl font-semibold text-foreground mb-2">Threat Analytics</h2>
                <p className="text-muted-foreground">Severity distribution and detection rates</p>
              </div>
              <ThreatCharts severity={severitySeries} trend={trend} />
            </div>

            <div>
              <div className="mb-6">
                <h2 className="text-xl font-semibold text-foreground mb-2">Detection Breakdown</h2>
                <p className="text-muted-foreground">Aggregated AI detections by threat type</p>
              </div>
              <ThreatsTable
                threats={threatTypes}
                onThreatSelect={(aggregate) => {
                  setSelectedThreat(aggregate)
                }}
                detectionLoopRunning={detectionLoopRunning}
                lastDetectionTime={lastDetectionTime}
                validatorCount={backendStats?.consensus?.validator_count}
              />
            </div>
          </div>

          {/* Right Column - Summary Cards */}
          <div className="space-y-6">
            <div className="glass-card border border-border/30 rounded-lg p-6">
              <h3 className="text-lg font-semibold text-foreground mb-4">Threat Summary</h3>
              <p className="text-xs text-muted-foreground mb-3">Recent snapshot (last 500 detections)</p>
              <div className="space-y-4">
                <div className="flex items-center justify-between p-3 rounded-lg bg-background/60">
                  <span className="text-sm text-muted-foreground">Total Detected</span>
                  <span className="text-2xl font-bold text-foreground">{totals?.overall ?? 0}</span>
                </div>
                <div className="flex items-center justify-between p-3 rounded-lg bg-background/60">
                  <span className="text-sm text-muted-foreground">Published</span>
                  <span className="text-2xl font-bold text-primary">{totals?.published ?? 0}</span>
                </div>
                <div className="flex items-center justify-between p-3 rounded-lg bg-background/60">
                  <span className="text-sm text-muted-foreground">Abstained</span>
                  <span className="text-2xl font-bold text-muted-foreground">{totals?.abstained ?? 0}</span>
                </div>
              </div>
            </div>

            <div className="glass-card border border-border/30 rounded-lg p-6">
              <h3 className="text-lg font-semibold text-foreground mb-4">Detection Status</h3>
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Detection Loop</span>
                  <Badge variant={statusVariant}>{statusLabel}</Badge>
                </div>
                {detectionLoopMessage && (
                  <div className="text-xs text-muted-foreground border border-border/40 rounded-md bg-background/60 p-2">
                    {detectionLoopMessage}
                  </div>
                )}
                {threatSource !== "history" ? (
                  <div className="text-[11px] text-muted-foreground/80">
                    Data source: {humanizedSource}
                    {humanizedFallback ? ` (${humanizedFallback})` : ""}
                  </div>
                ) : null}
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Validator Count</span>
                  <span className="text-sm font-medium text-foreground">
                    {backendStats?.consensus?.validator_count ?? "--"}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Quorum Size</span>
                  <span className="text-sm font-medium text-foreground">
                    {backendStats?.consensus?.quorum_size ?? "--"}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Last Detection</span>
                  <span className="text-xs font-mono text-muted-foreground">
                    {lastDetectionTime ? new Date(lastDetectionTime).toLocaleTimeString() : "--"}
                  </span>
                </div>
              </div>
            </div>

            <div className="glass-card border border-border/30 rounded-lg p-6">
              <h3 className="text-lg font-semibold text-foreground mb-4">System Info</h3>
              <div className="space-y-2 text-sm">
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Version</span>
                  <span className="text-foreground">v2.1.0</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Last Update</span>
                  <span className="text-foreground text-xs">
                    {data ? new Date(data.timestamp).toLocaleTimeString() : "--"}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </section>

        <ThreatDetailDrawer threat={selectedThreat} open={Boolean(selectedThreat)} onClose={() => setSelectedThreat(null)} lastDetectionTime={lastDetectionTime} />
          </>
        )}
      </PageContainer>
    </div>
  )
}
