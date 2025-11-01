import { NextResponse } from "next/server"

import { aiApi, backendApi, parsePrometheusText } from "@/lib/api"

type SummaryValue = number | undefined

function extractMetric(
  samples: ReturnType<typeof parsePrometheusText>,
  metricName: string,
  labels: Record<string, string> = {},
): SummaryValue {
  for (const sample of samples) {
    if (sample.metric !== metricName) continue

    const matches = Object.entries(labels).every(([key, value]) => sample.labels[key] === value)
    if (matches) {
      return sample.value
    }
  }
  return undefined
}

export async function GET() {
  try {
    const [health, readiness, stats, metricsText] = await Promise.all([
      backendApi.getHealth(),
      backendApi.getReady(),
      backendApi.getStats(),
      backendApi.getMetricsText(),
    ])

    const backendSamples = parsePrometheusText(metricsText)

    const aiResults = await Promise.allSettled([aiApi.getHealth(), aiApi.getReady(), backendApi.getAIMetrics()])

    const aiHealth = aiResults[0].status === "fulfilled" ? aiResults[0].value : null
    const aiReady = aiResults[1].status === "fulfilled" ? aiResults[1].value : null
    const aiMetrics = aiResults[2].status === "fulfilled" ? aiResults[2].value : null

    const metricsSummary = {
      cpuSecondsTotal: extractMetric(backendSamples, "process_cpu_seconds_total"),
      residentMemoryBytes: extractMetric(backendSamples, "process_resident_memory_bytes"),
      virtualMemoryBytes: extractMetric(backendSamples, "process_virtual_memory_bytes"),
      heapAllocBytes: extractMetric(backendSamples, "go_memstats_heap_alloc_bytes"),
      heapSysBytes: extractMetric(backendSamples, "go_memstats_heap_sys_bytes"),
      goroutines: extractMetric(backendSamples, "go_goroutines"),
      threads: extractMetric(backendSamples, "go_threads"),
      gcPauseSeconds: extractMetric(backendSamples, "go_gc_duration_seconds_sum"),
      gcCount: extractMetric(backendSamples, "go_gc_cycles_total"),
      processStartTimeSeconds: extractMetric(backendSamples, "process_start_time_seconds"),
    }

    const derivedBackendMetrics = {
      mempoolLatencyMs:
        typeof stats.mempool?.oldest_tx_age_seconds === "number"
          ? stats.mempool.oldest_tx_age_seconds * 1000
          : undefined,
      consensusLatencyMs:
        typeof stats.chain?.avg_block_time_seconds === "number"
          ? stats.chain.avg_block_time_seconds * 1000
          : undefined,
      p2pLatencyMs: typeof stats.network?.avg_latency_ms === "number" ? stats.network.avg_latency_ms : undefined,
    }

    const aiLatencyMs = aiMetrics?.loop?.avg_latency_ms ?? undefined

    return NextResponse.json(
      {
        timestamp: Date.now(),
        backend: {
          health,
          readiness,
          stats,
          metrics: {
            summary: metricsSummary,
            samples: backendSamples,
          },
          derived: derivedBackendMetrics,
        },
        ai: {
          health: aiHealth,
          ready: aiReady,
          detectionStats: aiMetrics,
          derived: {
            detectionLatencyMs: Number.isFinite(aiLatencyMs) ? aiLatencyMs : undefined,
          },
        },
      },
      { status: 200 },
    )
  } catch (error) {
    return NextResponse.json(
      {
        error: "system_health_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
