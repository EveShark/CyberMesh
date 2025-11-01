import { NextResponse } from "next/server"

import { backendApi, parsePrometheusText } from "@/lib/api"

const NETWORK_BYTES_METRIC = "cybermesh_network_bytes_total"
const CONSENSUS_VIEW_CHANGES_METRIC = "consensus_view_changes_total"

export async function GET() {
  try {
    const [stats, validators, metricsText] = await Promise.all([
      backendApi.getStats(),
      backendApi.listValidators(),
      backendApi.getMetricsText(),
    ])

    const samples = parsePrometheusText(metricsText)
    const networkBytes = samples.filter((sample) => sample.metric === NETWORK_BYTES_METRIC)
    const viewChanges = samples.find((sample) => sample.metric === CONSENSUS_VIEW_CHANGES_METRIC)?.value ?? 0

    const nodes = validators.validators.map((validator) => ({
      id: validator.id,
      role: validator.public_key ? "validator" : "node",
      status: validator.status === "active" ? "healthy" : "degraded",
      peers: [],
      throughputBytes: networkBytes.find((sample) => sample.labels.node_id === validator.id)?.value ?? 0,
    }))

    const connectedPeers = stats.network?.peer_count ?? nodes.length

    return NextResponse.json(
      {
        nodes,
        metrics: {
          connectedPeers,
          totalPeers: nodes.length,
          consensusRound: stats.consensus?.round ?? 0,
          leaderStability: viewChanges === 0 ? 100 : Math.max(0, 100 - viewChanges),
          averageLatencyMs: stats.network?.avg_latency_ms ?? 0,
        },
        updatedAt: new Date().toISOString(),
      },
      { status: 200 },
    )
  } catch (error) {
    return NextResponse.json(
      {
        error: "network_health_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
