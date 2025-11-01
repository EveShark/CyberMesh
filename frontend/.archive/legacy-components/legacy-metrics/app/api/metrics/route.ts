import { NextResponse } from "next/server"

import { backendApi, parsePrometheusText } from "@/lib/api"

const CPU_USAGE_METRIC = "process_cpu_seconds_total"
const RESIDENT_MEMORY_METRIC = "process_resident_memory_bytes"
const ERROR_RATE_METRIC = "cybermesh_api_request_errors_total"
const REQUEST_TOTAL_METRIC = "cybermesh_api_requests_total"
const KAFKA_THROUGHPUT_METRIC = "cybermesh_kafka_messages_total"
const REDIS_HITS_METRIC = "cybermesh_redis_hits_total"
const REDIS_MISSES_METRIC = "cybermesh_redis_misses_total"
const COCKROACH_QUERIES_METRIC = "cybermesh_crdb_queries_total"

function getLatestSample(samples: ReturnType<typeof parsePrometheusText>, metric: string, labelFilters?: Record<string, string>) {
  const filtered = samples.filter((sample) => {
    if (sample.metric !== metric) return false
    if (!labelFilters) return true
    return Object.entries(labelFilters).every(([key, value]) => sample.labels[key] === value)
  })

  if (!filtered.length) return undefined
  return filtered[filtered.length - 1]
}

export async function GET() {
  try {
    const [stats, readiness, backendMetricsText, aiMetrics] = await Promise.all([
      backendApi.getStats(),
      backendApi.getReady(),
      backendApi.getMetricsText(),
      backendApi.getAIMetrics().catch(() => null),
    ])

    const backendSamples = parsePrometheusText(backendMetricsText)

    const response = {
      timestamp: Date.now(),
      backend: {
        stats,
        readiness,
        metrics: {
          samples: backendSamples,
        },
      },
      ai: {
        metrics: aiMetrics,
      },
      kpis: {
        cpuSeconds: getLatestSample(backendSamples, CPU_USAGE_METRIC)?.value,
        residentMemoryBytes: getLatestSample(backendSamples, RESIDENT_MEMORY_METRIC)?.value,
        requestErrors: getLatestSample(backendSamples, ERROR_RATE_METRIC)?.value,
        requestTotal: getLatestSample(backendSamples, REQUEST_TOTAL_METRIC)?.value,
      },
      infrastructure: {
        kafka: getLatestSample(backendSamples, KAFKA_THROUGHPUT_METRIC),
        redisHits: getLatestSample(backendSamples, REDIS_HITS_METRIC),
        redisMisses: getLatestSample(backendSamples, REDIS_MISSES_METRIC),
        cockroachQueries: getLatestSample(backendSamples, COCKROACH_QUERIES_METRIC),
      },
    }

    return NextResponse.json(response, { status: 200 })
  } catch (error) {
    return NextResponse.json(
      {
        error: "metrics_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
