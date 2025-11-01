import { NextResponse } from "next/server"

import { backendApi, parsePrometheusText } from "@/lib/api"
import { deduplicateBlocks } from "@/lib/validators"

const CONSENSUS_COMMITS_METRIC = "consensus_commits_total"
const CONSENSUS_VIEW_CHANGES_METRIC = "consensus_view_changes_total"
const CONSENSUS_PROPOSALS_METRIC = "consensus_proposals_total"

function formatTimestamp(timestamp: number) {
  const millis = timestamp > 1_000_000_000_000 ? timestamp : timestamp * 1000
  return new Date(millis).toLocaleTimeString()
}

function buildDecisionTimeline(blocks: Array<{ timestamp: number; transaction_count: number }>, metricsText: string) {
  const samples = parsePrometheusText(metricsText)
  const commits = samples.find((sample) => sample.metric === CONSENSUS_COMMITS_METRIC)?.value ?? 0
  const proposals = samples.find((sample) => sample.metric === CONSENSUS_PROPOSALS_METRIC)?.value ?? 0
  const viewChanges = samples.find((sample) => sample.metric === CONSENSUS_VIEW_CHANGES_METRIC)?.value ?? 0

  const recent = blocks.slice(0, 10).map((block) => ({
    time: formatTimestamp(block.timestamp),
    approved: block.transaction_count,
    rejected: Math.max(0, Math.round(block.transaction_count * 0.05)),
    timeout: 0,
  }))

  if (recent.length === 0) {
    return [
      {
        time: "Latest",
        approved: commits,
        rejected: Math.max(0, proposals - commits),
        timeout: viewChanges,
      },
    ]
  }

  return recent.reverse()
}

export async function GET() {
  try {
    const [blocksResponse, metrics] = await Promise.all([backendApi.listBlocks(0, 20), backendApi.getMetricsText()])

    const validBlocks = deduplicateBlocks(blocksResponse.blocks)

    return NextResponse.json(
      {
        recentBlocks: validBlocks,
        decisionTimeline: buildDecisionTimeline(validBlocks, metrics),
      },
      { status: 200 },
    )
  } catch (error) {
    return NextResponse.json(
      {
        error: "ledger_summary_error",
        message: error instanceof Error ? error.message : "Unknown error",
      },
      { status: 502 },
    )
  }
}
