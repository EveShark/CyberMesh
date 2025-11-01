"use client"

import useSWR from "swr"

import type { BackendConsensusOverview } from "@/lib/api"
import { resolveDisplayName } from "@/lib/node-alias"
import type {
  ConsensusOverview,
  ConsensusProposal,
  ConsensusVote,
  SuspiciousNode,
} from "@/p2p-consensus/lib/types"

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

const fetchConsensusOverview = async (): Promise<ConsensusOverview> => {
  const response = await fetch("/api/consensus/overview", { cache: "no-store" })
  if (!response.ok) {
    const text = await response.text().catch(() => "")
    throw new Error(`Consensus overview request failed: ${response.status} ${response.statusText}${text ? ` - ${text}` : ""}`)
  }
  const backendOverview = (await response.json()) as BackendConsensusOverview
  return toConsensusOverview(backendOverview)
}

export function useConsensusData(refreshInterval = 5000) {
  const { data, error, isLoading, mutate } = useSWR<ConsensusOverview>(
    "consensus-overview",
    fetchConsensusOverview,
    { refreshInterval }
  )

  return {
    data,
    error,
    isLoading,
    refresh: mutate,
  }
}
