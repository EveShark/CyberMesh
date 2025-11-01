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
  const detectionLoopRunning = data?.detectionLoop?.running ?? false

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
              <Badge variant={detectionLoopRunning ? "destructive" : "secondary"} className={detectionLoopRunning ? "pulse-ring" : ""}>
                <AlertTriangle className="h-3 w-3 mr-1" />
                {detectionLoopRunning ? "LIVE" : "PAUSED"}
              </Badge>
              <Badge variant="outline" className="text-xs">
                Blocked: {totals?.published.toFixed?.(0) ?? "--"}
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
            totalDetected={totals?.overall ?? 0}
            published={totals?.published ?? 0}
            abstained={totals?.abstained ?? 0}
            avgResponseTimeMs={undefined} // TODO: Backend to provide this
            validatorCount={backendStats?.stats.consensus?.validator_count}
            quorumSize={backendStats?.stats.consensus?.quorum_size}
            isLoading={isLoadingAny}
            lastUpdated={data?.timestamp}
          />
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
                validatorCount={backendStats?.stats.consensus?.validator_count}
              />
            </div>
          </div>

          {/* Right Column - Summary Cards */}
          <div className="space-y-6">
            <div className="glass-card border border-border/30 rounded-lg p-6">
              <h3 className="text-lg font-semibold text-foreground mb-4">Threat Summary</h3>
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
                  <Badge variant={detectionLoopRunning ? "destructive" : "secondary"}>
                    {detectionLoopRunning ? "RUNNING" : "PAUSED"}
                  </Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Validator Count</span>
                  <span className="text-sm font-medium text-foreground">
                    {backendStats?.stats.consensus?.validator_count ?? "--"}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">Quorum Size</span>
                  <span className="text-sm font-medium text-foreground">
                    {backendStats?.stats.consensus?.quorum_size ?? "--"}
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
