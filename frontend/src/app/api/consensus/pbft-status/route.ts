import { NextResponse } from "next/server"

import { backendApi, parsePrometheusText } from "@/lib/api"

const CONSENSUS_PROPOSALS_METRIC = "consensus_proposals_total"
const CONSENSUS_COMMITS_METRIC = "consensus_commits_total"

export async function GET() {
  try {
    const [stats, validators, metricsText] = await Promise.all([
      backendApi.getStats(),
      backendApi.listValidators(),
      backendApi.getMetricsText(),
    ])

    const samples = parsePrometheusText(metricsText)
    const commitCount = samples.find((sample) => sample.metric === CONSENSUS_COMMITS_METRIC)?.value ?? 0
    const proposalCount = samples.find((sample) => sample.metric === CONSENSUS_PROPOSALS_METRIC)?.value ?? 0

    const phase = commitCount >= proposalCount ? "commit" : "prepare"

    const status = {
      leader: stats.consensus?.current_leader ?? "unknown",
      term: stats.consensus?.view ?? 0,
      phase,
      activePeers: validators.validators.filter((v) => v.status === "active").length,
      totalPeers: validators.total,
    }

    return NextResponse.json(status, { status: 200 })
  } catch (error) {
    return NextResponse.json(
      {
        error: "consensus_status_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
