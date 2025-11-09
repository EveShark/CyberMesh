"use client"

import { useMemo } from "react"

import type { BackendNetworkEdge, BackendNetworkOverview } from "@/lib/api"
import { resolveNodeAlias, resolveDisplayName } from "@/lib/node-alias"
import type { NetworkEdge, NetworkNode, NetworkOverview, VotingStatusEntry } from "@/p2p-consensus/lib/types"

import { useDashboardData } from "./use-dashboard-data"

const mapNetworkNodes = (nodes: BackendNetworkOverview["nodes"]): NetworkNode[] =>
  nodes.map((node) => ({
    id: node.id,
    name: resolveNodeAlias(node.id, node.name),
    status: node.status,
    latency: node.latency,
    uptime: node.uptime,
    throughputBytes: node.throughput_bytes,
    lastSeen: node.last_seen ?? null,
    inboundRateBps: node.inbound_rate_bps,
  }))

const mapVotingStatus = (entries: BackendNetworkOverview["voting_status"]): VotingStatusEntry[] =>
  entries.map((entry) => ({
    nodeId: entry.node_id,
    voting: entry.voting,
  }))

const mapNetworkEdges = (edges: BackendNetworkEdge[] | undefined): NetworkEdge[] => {
  if (!edges || edges.length === 0) return []
  return edges.map((edge) => ({
    source: edge.source,
    target: edge.target,
    direction: edge.direction,
    status: edge.status,
    confidence: edge.confidence,
    reportedBy: edge.reported_by,
    updatedAt: edge.updated_at,
  }))
}

const toNetworkOverview = (source: BackendNetworkOverview): NetworkOverview => {
  const nodes = mapNetworkNodes(source.nodes)
  const edges = mapNetworkEdges(source.edges)
  let leaderName: string | null = null

  const leaderId = source.leader_id ?? null
  if (leaderId) {
    const leaderMatch = nodes.find((node) => node.id === leaderId)
    if (leaderMatch) {
      leaderName = leaderMatch.name
    } else {
      leaderName = resolveDisplayName(leaderId, source.leader ?? leaderId)
    }
  } else if (source.leader) {
    leaderName = resolveDisplayName(source.leader, source.leader)
  }

  return {
    connectedPeers: source.connected_peers,
    totalPeers: source.total_peers,
    expectedPeers: source.expected_peers ?? nodes.length,
    averageLatencyMs: source.average_latency_ms,
    consensusRound: source.consensus_round,
    leaderStability: source.leader_stability,
    phase: source.phase,
    leader: leaderName,
    leaderId,
    nodes,
    votingStatus: mapVotingStatus(source.voting_status),
    edges,
    selfId: source.self ?? null,
    inboundRateBps: source.inbound_rate_bps,
    outboundRateBps: source.outbound_rate_bps,
    updatedAt: source.updated_at,
  }
}

const DEFAULT_NETWORK_REFRESH_MS = 15000

export function useNetworkData(refreshInterval = DEFAULT_NETWORK_REFRESH_MS) {
  const { data: dashboard, error, isLoading, mutate } = useDashboardData(refreshInterval)

  const overview = useMemo(() => {
    if (!dashboard?.network) return undefined
    return toNetworkOverview(dashboard.network)
  }, [dashboard?.network])

  return {
    data: overview,
    error,
    isLoading: !overview && isLoading,
    refresh: mutate,
  }
}
