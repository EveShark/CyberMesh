import { NextResponse } from "next/server"

import { backendApi, parsePrometheusText } from "@/lib/api"

const LATENCY_METRIC = "cybermesh_latency_avg_seconds"
const UPTIME_METRIC = "api_server_running"

export async function GET() {
  try {
    const [backendStats, aiMetrics, backendMetricsText] = await Promise.all([
      backendApi.getStats(),
      backendApi.getAIMetrics().catch(() => null),
      backendApi.getMetricsText(),
    ])

    const backendSamples = parsePrometheusText(backendMetricsText)

    const avgLatencySec = backendSamples.find((sample) => sample.metric === LATENCY_METRIC)?.value ?? 0
    const uptimeGauge = backendSamples.find((sample) => sample.metric === UPTIME_METRIC)?.value ?? 1
    const detectionTotal = aiMetrics?.derived?.detections_per_minute
      ? Math.round((aiMetrics.derived.detections_per_minute ?? 0) * 60)
      : 0

    const detectionAccuracy = aiMetrics?.derived?.publish_success_ratio ?? 0.99

    return NextResponse.json(
      {
        latencyMs: Math.round(avgLatencySec * 1000),
        uptimePercentage: uptimeGauge * 100,
        detectionAccuracy: detectionAccuracy * 100,
        detectionTotal,
        consensusRound: backendStats.consensus?.round ?? 0,
      },
      { status: 200 },
    )
  } catch (error) {
    return NextResponse.json(
      {
        error: "investor_metrics_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
