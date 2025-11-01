"use client"

import { Loader2, AlertCircle } from "lucide-react"

import { KpiTiles } from "@/components/kpi-tiles"
import { AiEngineStats } from "@/components/ai-engine-stats"
import { InfraStats } from "@/components/infra-stats"
import { Badge } from "@/components/ui/badge"
import { useMetricsData } from "@/hooks/use-metrics-data"

export default function MetricsPage() {
  const { data, kpis, isLoading, error } = useMetricsData()

  const aiEngines = (data?.ai.metrics.samples ?? [])
    .filter((sample) => sample.metric === "ml_engine_ready")
    .map((sample) => ({
      title: sample.labels.engine_type ?? "Engine",
      status: sample.value > 0 ? ("healthy" as const) : ("warning" as const),
    }))

  const infra = {
    kafkaTopicRate: data?.infrastructure.kafka?.value,
    redisHitRate: data?.infrastructure.redisHits?.value,
    redisMissRate: data?.infrastructure.redisMisses?.value,
    cockroachQueries: data?.infrastructure.cockroachQueries?.value,
    readinessChecks: data?.backend.readiness.checks,
  }

  return (
    <div className="min-h-screen">
      <div className="w-full max-w-[1920px] mx-auto px-4 sm:px-6 lg:px-8 xl:px-12 2xl:px-16 py-6 lg:py-8 space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground">System Metrics</h1>
          <p className="text-muted-foreground">Real-time performance monitoring and infrastructure health</p>
        </div>
        <div className="flex items-center gap-2 text-sm">
          {isLoading ? (
            <Badge variant="outline" className="flex items-center gap-1">
              <Loader2 className="h-3 w-3 animate-spin" /> Fetching
            </Badge>
          ) : null}
          {error ? (
            <Badge variant="destructive" className="flex items-center gap-1">
              <AlertCircle className="h-3 w-3" /> Error loading metrics
            </Badge>
          ) : null}
          {data ? <Badge variant="outline">Updated {new Date(data.timestamp).toLocaleTimeString()}</Badge> : null}
        </div>
      </div>

      <section>
        <h2 className="text-xl font-semibold text-foreground mb-4">Key Performance Indicators</h2>
        <KpiTiles
          cpuPercent={kpis.cpuPercent}
          memoryBytes={kpis.memoryUsageBytes}
          requestRatePerSec={kpis.requestRatePerSec}
          errorRatePercent={kpis.errorRatePercent}
        />
      </section>

      <section>
        <AiEngineStats engines={aiEngines} />
      </section>

      <section>
        <InfraStats
          kafkaTopicRate={infra.kafkaTopicRate}
          redisHitRate={infra.redisHitRate}
          redisMissRate={infra.redisMissRate}
          cockroachQueries={infra.cockroachQueries}
          readinessChecks={infra.readinessChecks}
        />
      </section>
      </div>
    </div>
  )
}
