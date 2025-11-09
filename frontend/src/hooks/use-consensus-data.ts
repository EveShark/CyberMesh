"use client"

import { useMemo } from "react"

import type { BackendConsensusOverview } from "@/lib/api"
import { resolveDisplayName } from "@/lib/node-alias"
import type {
  ConsensusOverview,
  ConsensusProposal,
  ConsensusVote,
  SuspiciousNode,
} from "@/p2p-consensus/lib/types"

import { useDashboardData } from "./use-dashboard-data"

const mapProposals = (proposals: BackendConsensusOverview["proposals"]): ConsensusProposal[] =>
  proposals.map((proposal) => ({
    block: proposal.block,
    timestamp: proposal.timestamp,
  }))

const mapVotes = (votes: BackendConsensusOverview["votes"]): ConsensusVote[] =>
  votes.map((vote) => ({
    type: vote.type,
    count: vote.count,
    timestamp: vote.timestamp,
  }))

const mapSuspiciousNodes = (nodes: BackendConsensusOverview["suspicious_nodes"]): SuspiciousNode[] =>
  nodes.map((node) => ({
    id: node.id,
    status: node.status,
    uptime: Math.round(node.uptime),
    suspicionScore: Math.round(node.suspicion_score),
    reason: node.reason,
  }))

const toConsensusOverview = (source: BackendConsensusOverview): ConsensusOverview => {
  const leaderId = source.leader_id ?? null
  const leaderAlias = leaderId ? resolveDisplayName(leaderId, source.leader ?? leaderId) : source.leader ?? null

  return {
    leader: leaderAlias,
    leaderId,
    term: source.term,
    phase: source.phase,
    activePeers: source.active_peers,
    quorumSize: source.quorum_size,
    proposals: mapProposals(source.proposals ?? []),
    votes: mapVotes(source.votes ?? []),
    suspiciousNodes: mapSuspiciousNodes(source.suspicious_nodes ?? []),
    updatedAt: source.updated_at,
  }
}

const DEFAULT_CONSENSUS_REFRESH_MS = 15000

export function useConsensusData(refreshInterval = DEFAULT_CONSENSUS_REFRESH_MS) {
  const { data: dashboard, error, isLoading, mutate } = useDashboardData(refreshInterval)

  const overview = useMemo(() => {
    if (!dashboard?.consensus) return undefined
    return toConsensusOverview(dashboard.consensus)
  }, [dashboard?.consensus])

  return {
    data: overview,
    error,
    isLoading: !overview && isLoading,
    refresh: mutate,
  }
}
