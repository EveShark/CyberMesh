"use client"

import { useMemo, useState, useEffect } from "react"

import { 
  AlertTriangle, Activity, BrainCircuit, Clock, GaugeCircle, RefreshCw, ChevronDown, ChevronUp,
  CheckCircle, Circle, Lightbulb, ShieldAlert, Shield, BarChart3
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { PageContainer } from "@/components/page-container"
import { AiEngineStats } from "@/components/ai-engine-stats"
import { useAiEngineData } from "@/hooks/use-ai-engine-data"

function extractUptimeSeconds(record?: Record<string, unknown> | null): number | undefined {
  if (!record || typeof record !== "object") return undefined
  const value = (record as { uptime_seconds?: unknown }).uptime_seconds
  return typeof value === "number" && Number.isFinite(value) ? value : undefined
}

function formatNumber(value?: number, digits = 1) {
  if (typeof value !== "number" || Number.isNaN(value)) return "--"
  return value.toFixed(digits)
}

function formatPercent(value?: number, digits = 1) {
  if (typeof value !== "number" || Number.isNaN(value)) return "--"
  return `${(value * 100).toFixed(digits)}%`
}

function formatTimestamp(iso?: string) {
  if (!iso) return "--"
  try {
    const date = new Date(iso)
    return date.toLocaleString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      month: "short",
      day: "2-digit",
    })
  } catch {
    return iso
  }
}

export default function AiEnginePageContent() {
  const { isLoading, error, metrics, history, suspicious, health } = useAiEngineData(5000)

  // Client-side state for trend tracking and UI
  const [latencyHistory, setLatencyHistory] = useState<Array<{ time: number; latency: number }>>([])
  const [showAllDetections, setShowAllDetections] = useState(false)

  const loop = metrics?.loop
  const derived = metrics?.derived

  const recentDetections = useMemo(() => history?.detections ?? [], [history])
  const trackedVariants = useMemo(() => metrics?.variants ?? [], [metrics])
  const trackedEngines = useMemo(() => metrics?.engines ?? [], [metrics])
  const suspiciousNodes = useMemo(() => suspicious?.nodes ?? [], [suspicious])

  // Track latency trend (last 20 data points = 100s history at 5s interval)
  useEffect(() => {
    if (loop?.last_latency_ms !== undefined) {
      setLatencyHistory((prev) => {
        const updated = [...prev, { time: Date.now(), latency: loop.last_latency_ms }]
        return updated.slice(-20) // Keep last 20 points
      })
    }
  }, [loop])

  // Calculated insights
  const displayedDetections = showAllDetections ? recentDetections : recentDetections.slice(0, 5)
  const hiddenCount = Math.max(0, recentDetections.length - 5)

  const detectionStats = useMemo(() => {
    const published = recentDetections.filter((d) => d.should_publish).length
    const held = recentDetections.length - published
    const publishRate = recentDetections.length > 0 ? (published / recentDetections.length) * 100 : 0
    return { total: recentDetections.length, published, held, publishRate }
  }, [recentDetections])

  const variantInsights = useMemo(() => {
    if (trackedVariants.length === 0) return null
    const sorted = [...trackedVariants].sort((a, b) => b.publish_ratio - a.publish_ratio)
    const topPerformer = sorted[0]
    const needsTuning = sorted.filter((v) => v.publish_ratio < 0.7)
    return { topPerformer, needsTuning }
  }, [trackedVariants])

  const suspiciousGroups = useMemo(() => {
    const highRisk = suspiciousNodes.filter((n) => n.suspicion_score > 80)
    const watchList = suspiciousNodes.filter((n) => n.suspicion_score >= 60 && n.suspicion_score <= 80)
    return { highRisk, watchList }
  }, [suspiciousNodes])

  const totalIterations = useMemo(() => {
    if (!derived?.iterations_per_minute) return null
    const uptime = extractUptimeSeconds(health?.health)
    if (uptime === undefined) return null
    return Math.floor(derived.iterations_per_minute * (uptime / 60))
  }, [derived?.iterations_per_minute, health?.health])

  const loopHealth = useMemo(() => {
    if (!loop?.avg_latency_ms) return { status: "unknown", color: "text-muted-foreground" }
    if (loop.avg_latency_ms < 100) return { status: "Stable", color: "text-green-500" }
    return { status: "Degraded", color: "text-yellow-500" }
  }, [loop?.avg_latency_ms])

  const heroCards = [
    {
      label: "Loop Status",
      value: loop?.running ? "Running" : "Stopped",
      helper: loop?.running ? "Publishing detections" : "Awaiting scheduler",
      icon: Activity,
    },
    {
      label: "Detections / min",
      value: formatNumber(derived?.detections_per_minute),
      helper: "Published decisions", 
      icon: BrainCircuit,
    },
    {
      label: "Publish Success",
      value: formatPercent(derived?.publish_success_ratio ?? 0),
      helper: "Publish to candidate ratio",
      icon: GaugeCircle,
    },
    {
      label: "Last iteration",
      value:
        loop?.seconds_since_last_iteration !== undefined
          ? `${loop.seconds_since_last_iteration.toFixed(1)}s`
          : "--",
      helper: "Since last iteration",
      icon: Clock,
    },
  ]

  return (
    <PageContainer align="left" className="py-6 lg:py-8 space-y-8">
      <header className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="space-y-2">
          <h1 className="text-3xl font-bold text-foreground">AI Engine Telemetry</h1>
          <p className="text-sm text-muted-foreground">
            Real-time instrumentation for detection loop, variant pipelines, and suspicious validator signals.
          </p>
        </div>
        <div className="flex items-center gap-3">
          {error ? (
            <Badge variant="destructive" className="flex items-center gap-1 text-xs">
              <AlertTriangle className="h-3 w-3" /> Sync error
            </Badge>
          ) : null}
          <Badge variant={isLoading ? "outline" : "default"} className="flex items-center gap-1 text-xs">
            {isLoading ? (
              <>
                <RefreshCw className="h-3 w-3 animate-spin" /> Syncing
              </>
            ) : (
              "Live"
            )}
          </Badge>
          <Button size="sm" variant="outline" onClick={() => window.location.reload()} disabled={isLoading}>
            <RefreshCw className="mr-2 h-4 w-4" /> Refresh
          </Button>
        </div>
      </header>

      <section className="w-full grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        {heroCards.map((card) => {
          const Icon = card.icon
          return (
            <Card key={card.label} className="glass-card border border-border/30">
              <CardContent className="p-6 space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">{card.label}</span>
                  <Icon className="h-5 w-5 text-primary" />
                </div>
                <p className="text-3xl font-bold font-mono tracking-tight text-foreground">{card.value}</p>
                <p className="text-xs text-muted-foreground">{card.helper}</p>
              </CardContent>
            </Card>
          )
        })}
      </section>

      {/* Detection Loop Metrics - Dedicated Section */}
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-xl font-semibold text-foreground">
            <BrainCircuit className="h-6 w-6 text-primary" /> Detection Loop Metrics
          </CardTitle>
        </CardHeader>
        <CardContent className="p-6 space-y-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <span className="text-sm font-medium text-muted-foreground">Status:</span>
              <Badge variant={loop?.running ? "default" : "outline"} className="text-sm flex items-center gap-1">
                {loop?.running ? (
                  <>
                    <CheckCircle className="h-3 w-3 text-green-500" /> Running
                  </>
                ) : (
                  <>
                    <Circle className="h-3 w-3 text-muted-foreground" /> Idle
                  </>
                )}
              </Badge>
            </div>
            <div className="text-sm text-muted-foreground">
              Interval: ~{derived?.iterations_per_minute ? (60 / derived.iterations_per_minute).toFixed(1) : "5"}s
            </div>
          </div>

          {/* 4-metric grid */}
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
            <div className="rounded-lg border border-border/30 bg-background/60 p-4 space-y-2">
              <p className="text-xs text-muted-foreground">Avg Latency</p>
              <p className="text-2xl font-bold font-mono text-foreground">{formatNumber(loop?.avg_latency_ms, 2)}ms</p>
            </div>
            <div className="rounded-lg border border-border/30 bg-background/60 p-4 space-y-2">
              <p className="text-xs text-muted-foreground">Last Latency</p>
              <p className="text-2xl font-bold font-mono text-foreground">{formatNumber(loop?.last_latency_ms, 2)}ms</p>
            </div>
            <div className="rounded-lg border border-border/30 bg-background/60 p-4 space-y-2">
              <p className="text-xs text-muted-foreground">Since Detection</p>
              <p className="text-2xl font-bold font-mono text-foreground">
                {loop?.seconds_since_last_detection !== undefined
                  ? `${loop.seconds_since_last_detection.toFixed(1)}s`
                  : "--"}
              </p>
            </div>
            <div className="rounded-lg border border-border/30 bg-background/60 p-4 space-y-2">
              <p className="text-xs text-muted-foreground">Cache Age</p>
              <p className="text-2xl font-bold font-mono text-foreground">
                {loop?.cache_age_seconds !== undefined ? `${loop.cache_age_seconds.toFixed(1)}s` : "--"}
              </p>
            </div>
          </div>

          {/* Performance Trend Chart */}
          {latencyHistory.length > 1 && (
            <div className="space-y-3">
              <p className="text-sm font-medium text-muted-foreground">Performance Trend (last 100s):</p>
              <div className="h-32 rounded-lg border border-border/30 bg-background/60 p-4">
                <div className="h-full flex items-end justify-between gap-1">
                  {latencyHistory.map((point, idx) => {
                    const maxLatency = Math.max(...latencyHistory.map((p) => p.latency), 1)
                    const height = (point.latency / maxLatency) * 100
                    return (
                      <div
                        key={idx}
                        className="flex-1 bg-primary/70 hover:bg-primary transition-colors rounded-t"
                        style={{ height: `${height}%`, minHeight: "2px" }}
                        title={`${point.latency.toFixed(2)}ms`}
                      />
                    )
                  })}
                </div>
              </div>
            </div>
          )}

          {/* Health Status */}
          <div className="flex items-center justify-between pt-4 border-t border-border/30 text-sm">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Health:</span>
              <span className={`font-semibold ${loopHealth.color} inline-flex items-center gap-1`}>
                {loopHealth.status === "Stable" ? (
                  <CheckCircle className="h-4 w-4 text-green-500" />
                ) : (
                  <AlertTriangle className="h-4 w-4 text-amber-500" />
                )}
                {loopHealth.status}
              </span>
              <span className="text-muted-foreground">
                (avg {formatNumber(loop?.avg_latency_ms, 2)}ms, target &lt;100ms)
              </span>
            </div>
            {totalIterations !== null && (
              <div className="text-muted-foreground">
                Iterations: <span className="font-semibold text-foreground">{totalIterations.toLocaleString()}</span>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* AI Engine Performance */}
      <section>
        <AiEngineStats engines={trackedEngines} />
      </section>

      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold text-foreground">Variant performance</h2>
            <p className="text-sm text-muted-foreground">Publish ratios and confidence by model variant</p>
          </div>
        </div>
        <Card className="glass-card">
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="px-6">Variant</TableHead>
                  <TableHead className="text-right">Throughput</TableHead>
                  <TableHead className="text-right">Publish Ratio</TableHead>
                  <TableHead className="text-right">Avg Confidence</TableHead>
                  <TableHead className="text-right">Published</TableHead>
                  <TableHead className="px-6 text-right">Last Updated</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {trackedVariants.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className="py-6 text-center text-muted-foreground">
                      No variant telemetry captured.
                    </TableCell>
                  </TableRow>
                ) : null}
                {trackedVariants.map((variant) => (
                  <TableRow key={variant.variant} className="border-border/40">
                    <TableCell className="px-6 font-medium text-foreground">
                      <div className="flex flex-col">
                        <span>{variant.variant}</span>
                        {variant.engines?.length ? (
                          <span className="text-xs text-muted-foreground">{variant.engines.join(", ")}</span>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      {formatNumber(variant.throughput_per_minute)} / min
                    </TableCell>
                    <TableCell className="text-right">
                      {formatPercent(variant.publish_ratio)}
                    </TableCell>
                    <TableCell className="text-right">
                      {typeof variant.avg_confidence === "number"
                        ? formatPercent(variant.avg_confidence)
                        : "--"}
                    </TableCell>
                    <TableCell className="text-right">
                      {variant.published.toLocaleString()}
                    </TableCell>
                    <TableCell className="px-6 text-right">
                      {variant.last_updated ? formatTimestamp(new Date(variant.last_updated * 1000).toISOString()) : "--"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        {/* Variant Insights */}
        {variantInsights && (
          <div className="rounded-lg border border-border/30 bg-background/60 p-4 space-y-2 text-sm">
            <p className="font-medium text-foreground flex items-center gap-2">
              <Lightbulb className="h-4 w-4 text-primary" /> Insights:
            </p>
            {variantInsights.topPerformer && (
              <p className="text-muted-foreground">
                • Best: <span className="font-semibold text-foreground">{variantInsights.topPerformer.variant}</span> (
                {formatPercent(variantInsights.topPerformer.publish_ratio)} publish rate,{" "}
                {formatPercent(variantInsights.topPerformer.avg_confidence ?? undefined)} confidence)
              </p>
            )}
            {variantInsights.needsTuning.length > 0 && (
              <p className="text-muted-foreground">
                • Action: <span className="font-semibold text-amber-500">{variantInsights.needsTuning.map((v) => v.variant).join(", ")}</span>{" "}
                below 70% threshold - consider retraining
              </p>
            )}
          </div>
        )}
      </section>

      {/* Suspicious Validators - with risk grouping */}
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-lg font-semibold text-foreground">
            <AlertTriangle className="h-5 w-5 text-amber-500" /> Suspicious Validators
          </CardTitle>
        </CardHeader>
        <CardContent className="p-6 space-y-6">
          {suspiciousNodes.length === 0 ? (
            <p className="text-sm text-muted-foreground">All validators appear healthy.</p>
          ) : (
            <>
              {/* High Risk */}
              {suspiciousGroups.highRisk.length > 0 && (
                <div className="space-y-3">
                  <p className="text-sm font-medium text-red-500 flex items-center gap-2">
                    <ShieldAlert className="h-4 w-4" /> High Risk (Score &gt;80):
                  </p>
                  {suspiciousGroups.highRisk.map((node) => (
                    <div key={node.id} className="rounded-lg border border-red-500/30 bg-red-500/5 p-4">
                      <div className="flex items-center justify-between mb-3">
                        <div className="space-y-1">
                          <p className="font-semibold text-foreground">{node.alias ?? node.id}</p>
                          <p className="text-xs text-muted-foreground">
                            {node.reason ?? "No reason provided"}
                            {node.alias ? ` • ${node.id}` : ""}
                          </p>
                        </div>
                        <Badge variant="outline" className="uppercase border-red-500 text-red-500">
                          {node.status || "CRITICAL"}
                        </Badge>
                      </div>
                      <div className="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2">
                        <div>
                          <span className="text-foreground">Score:</span> {node.suspicion_score.toFixed(2)}
                        </div>
                        <div>
                          <span className="text-foreground">Events:</span> {node.event_count}
                        </div>
                        <div>
                          <span className="text-foreground">Last seen:</span> {formatTimestamp(node.last_seen)}
                        </div>
                        {node.threat_types?.length ? (
                          <div>
                            <span className="text-foreground">Threats:</span> {node.threat_types.join(", ")}
                          </div>
                        ) : null}
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {/* Watch List */}
              {suspiciousGroups.watchList.length > 0 && (
                <div className="space-y-3">
                  <p className="text-sm font-medium text-amber-500 flex items-center gap-2">
                    <AlertTriangle className="h-4 w-4" /> Watch List (Score 60-80):
                  </p>
                  {suspiciousGroups.watchList.map((node) => (
                    <div key={node.id} className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-4">
                      <div className="flex items-center justify-between mb-3">
                        <div className="space-y-1">
                          <p className="font-semibold text-foreground">{node.alias ?? node.id}</p>
                          <p className="text-xs text-muted-foreground">
                            {node.reason ?? "No reason provided"}
                            {node.alias ? ` • ${node.id}` : ""}
                          </p>
                        </div>
                        <Badge variant="outline" className="uppercase border-amber-500 text-amber-500">
                          {node.status || "WARNING"}
                        </Badge>
                      </div>
                      <div className="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2">
                        <div>
                          <span className="text-foreground">Score:</span> {node.suspicion_score.toFixed(2)}
                        </div>
                        <div>
                          <span className="text-foreground">Events:</span> {node.event_count}
                        </div>
                        <div>
                          <span className="text-foreground">Last seen:</span> {formatTimestamp(node.last_seen)}
                        </div>
                        {node.threat_types?.length ? (
                          <div>
                            <span className="text-foreground">Threats:</span> {node.threat_types.join(", ")}
                          </div>
                        ) : null}
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {/* Network Status Summary */}
              <div className="pt-4 border-t border-border/30 text-sm space-y-1">
                <p className="text-muted-foreground">
                  <span className="inline-flex items-center gap-1">
                    <Shield className="h-4 w-4" /> Network Status:
                  </span>{" "}
                  <span className="font-semibold text-foreground">
                    {suspiciousNodes.length}/5 validators flagged
                  </span>
                </p>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Real-Time Detection Stream - at bottom with Show More */}
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-lg font-semibold text-foreground">
            <Activity className="h-5 w-5 text-primary" /> Real-Time Detection Stream
          </CardTitle>
        </CardHeader>
        <CardContent className="p-6">
          <div className="space-y-4">
            {recentDetections.length === 0 ? (
              <p className="text-sm text-muted-foreground">No detection history recorded.</p>
            ) : (
              <>
                {displayedDetections.map((detection) => (
                <div key={`${detection.timestamp}-${detection.validator_id ?? "unknown"}`} className="rounded-lg border border-border/30 bg-card/60 p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
                      <Badge variant="outline" className="uppercase">
                        {detection.threat_type}
                      </Badge>
                      <span>{(detection.confidence * 100).toFixed(1)}% confidence</span>
                    </div>
                    <span className="text-xs text-muted-foreground">{formatTimestamp(detection.timestamp)}</span>
                  </div>
                  <div className="mt-2 grid gap-2 text-xs text-muted-foreground sm:grid-cols-2">
                    <div>
                      <span className="text-foreground">Final score:</span> {(detection.final_score * 100).toFixed(1)}%
                    </div>
                    <div>
                      <span className="text-foreground">Decision:</span> {detection.should_publish ? "Published" : "Held"}
                    </div>
                    {detection.validator_id ? (
                      <div>
                        <span className="text-foreground">Validator:</span> {detection.validator_alias ?? detection.validator_id}
                        {detection.validator_alias && (
                          <span className="text-muted-foreground"> ({detection.validator_id})</span>
                        )}
                      </div>
                    ) : null}
                    {detection.metadata ? (
                      <div className="truncate">
                        <span className="text-foreground">Metadata:</span> {JSON.stringify(detection.metadata)}
                      </div>
                    ) : null}
                  </div>
                </div>
              ))}

                {/* Show More Button */}
                {recentDetections.length > 5 && (
                  <div className="pt-4">
                    <Button
                      variant="outline"
                      onClick={() => setShowAllDetections(!showAllDetections)}
                      className="w-full"
                    >
                      {showAllDetections ? (
                        <>
                          <ChevronUp className="mr-2 h-4 w-4" /> Show Less
                        </>
                      ) : (
                        <>
                          <ChevronDown className="mr-2 h-4 w-4" /> Show More ({hiddenCount} hidden)
                        </>
                      )}
                    </Button>
                  </div>
                )}

                {/* Summary Stats */}
                <div className="pt-4 border-t border-border/30 text-sm text-muted-foreground">
                  <span className="inline-flex items-center gap-1">
                    <BarChart3 className="h-4 w-4" /> Summary:
                  </span>{" "}
                  <span className="font-semibold text-foreground">{detectionStats.total} total</span> |{" "}
                  <span className="font-semibold text-foreground">{detectionStats.published} published</span> (
                  {detectionStats.publishRate.toFixed(1)}%) |{" "}
                  <span className="font-semibold text-foreground">{detectionStats.held} held</span> for review
                </div>
              </>
            )}
          </div>
        </CardContent>
      </Card>
    </PageContainer>
  )
}
