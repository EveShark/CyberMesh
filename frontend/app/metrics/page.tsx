"use client"

import { KpiTiles } from "@/components/metrics/kpi-tiles"
import { AiEngineStats } from "@/components/metrics/ai-engine-stats"
import { InfraStats } from "@/components/metrics/infra-stats"

export default function MetricsPage() {
  return (
    <div className="flex-1 space-y-8 p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground">System Metrics</h1>
          <p className="text-muted-foreground">Real-time performance monitoring and infrastructure health</p>
        </div>
      </div>

      {/* KPI Overview */}
      <section>
        <h2 className="text-xl font-semibold text-foreground mb-4">Key Performance Indicators</h2>
        <KpiTiles />
      </section>

      {/* AI Engine Stats */}
      <section>
        <AiEngineStats />
      </section>

      {/* Infrastructure Stats */}
      <section>
        <InfraStats />
      </section>
    </div>
  )
}
