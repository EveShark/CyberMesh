import type { NetworkData, TopologyNode, Telemetry, Topology, Activity, Consensus, ValidatorStatus, VoteTimelinePoint, ActivityTimelinePoint, NodeStatus, NodeRole, ConsensusSummary, RiskLevel, Proposal, Vote } from "@/types/network";
import type { NetworkOverviewRaw, NetworkNodeRaw, DashboardBlockMetricsRaw, ConsensusOverviewRaw, ConsensusProposalRaw, ConsensusVoteRaw } from "../types/raw";
import { formatRate, formatMs, truncateHash } from "./utils";
import { getNodeName, truncateNodeId } from "@/config/validator-names";

export function adaptNetwork(raw?: NetworkOverviewRaw, blockMetrics?: DashboardBlockMetricsRaw, consensus?: ConsensusOverviewRaw): NetworkData {
  if (!raw) {
    return {
      telemetry: {
        connectedPeers: "0",
        avgLatency: 0,
        consensusRound: 0,
        leaderStability: 0,
        inboundRate: "0 B/s",
        outboundRate: "0 B/s"
      },
      topology: {
        leader: "Unknown",
        nodes: []
      },
      activity: {
        totalProposals: 0,
        peakRate: 0,
        avgRate: 0,
        timeline: [],
        proposals: []
      },
      consensus: {
        proposals: 0,
        votes: 0,
        commits: 0,
        viewChanges: 0,
        totalActivity: 0,
        validators: [],
        summary: {
          activeNodes: 0,
          operationalCapacity: "0%",
          leaderStability: "0%",
          risk: "CRITICAL" as RiskLevel
        },
        timeline: [],
        rawVotes: []
      }
    };
  }

  const nodes = adaptNodes(raw.nodes || [], raw.leader, raw.leader_id);

  // Leader stability workaround: backend returns 0 when viewChanges >= 100 (long-running cluster)
  // Derive from peer connectivity as a fallback when backend returns 0
  const backendStability = raw.leader_stability ?? 0;
  const connectivityPercent = raw.total_peers > 0
    ? (raw.connected_peers / raw.total_peers) * 100
    : 0;
  // Use backend value if > 0, otherwise derive from connectivity (full connectivity = 100% stability)
  const leaderStabilityPercent = backendStability > 0 ? backendStability : connectivityPercent;

  // Adapt proposal activity from consensus data
  const proposalTimeline = adaptProposalTimeline(consensus?.proposals || []);

  // Adapt vote timeline from consensus data
  const voteTimeline = adaptVoteTimeline(consensus?.votes || []);

  // Calculate total activity counts
  const totalProposals = consensus?.proposals?.length || 0;
  const totalVotes = consensus?.votes?.reduce((sum, v) => sum + v.count, 0) || 0;

  return {
    telemetry: {
      connectedPeers: raw.connected_peers.toString(),
      avgLatency: Math.round(raw.average_latency_ms * 100) / 100, // Round to 2 decimals
      consensusRound: raw.consensus_round,
      leaderStability: Math.round(leaderStabilityPercent * 10) / 10, // Round to 1 decimal
      inboundRate: formatRate(raw.inbound_rate_bps) || "0 B/s",
      outboundRate: formatRate(raw.outbound_rate_bps) || "0 B/s",
    },
    topology: {
      leader: getNodeName(raw.leader_id) || raw.leader || "Unknown",
      leaderId: raw.leader_id || "",
      nodes: nodes,
    },
    activity: {
      totalProposals: totalProposals,
      peakRate: calculatePeakRate(proposalTimeline),
      avgRate: calculateAvgRate(proposalTimeline),
      timeline: proposalTimeline,
      proposals: (consensus?.proposals || []).map(p => ({
        block: p.block,
        view: p.view,
        hash: p.hash,
        proposer: p.proposer,
        timestamp: p.timestamp
      }))
    },
    consensus: {
      proposals: totalProposals,
      votes: totalVotes,
      commits: consensus?.votes?.find(v => v.type === "commit")?.count || 0,
      viewChanges: consensus?.votes?.find(v => v.type === "view_change")?.count || 0,
      totalActivity: totalProposals + totalVotes,
      validators: adaptValidators(raw.nodes || [], raw.voting_status || [], raw.leader, raw.leader_id),
      summary: {
        activeNodes: raw.connected_peers,
        operationalCapacity: `${Math.round((raw.connected_peers / raw.total_peers) * 100)}%`,
        leaderStability: `${Math.round(leaderStabilityPercent)}%`,
        risk: getRiskLevel(raw.connected_peers, raw.total_peers, leaderStabilityPercent)
      },
      timeline: voteTimeline,
      rawVotes: (consensus?.votes || []).map(v => ({
        type: v.type,
        count: v.count,
        timestamp: v.timestamp
      }))
    }
  };
}

function normalizeId(id: string | undefined): string {
  if (!id) return "";
  return id.toLowerCase().replace(/^0x/, "");
}

function adaptNodes(rawNodes: NetworkNodeRaw[], leaderName: string | undefined, leaderId: string | undefined): TopologyNode[] {
  const normalizedLeaderId = normalizeId(leaderId);

  return rawNodes.map(node => {
    // Uptime from backend is already in percentage form (0-100), not ratio
    // But sometimes it could be ratio (0-1) - handle both cases
    const uptimeValue = node.uptime > 1 ? node.uptime : node.uptime * 100;
    const normalizedNodeId = normalizeId(node.id);
    const isLeader = node.name === leaderName || (normalizedLeaderId && normalizedNodeId === normalizedLeaderId);

    return {
      name: getNodeName(node.id),
      hash: truncateNodeId(node.id, 6, 4),
      fullId: node.id, // Full ID for leader matching
      latency: formatMs(node.latency) || "0ms",
      uptime: `${Math.min(uptimeValue, 100).toFixed(1)}%`, // Cap at 100%
      role: isLeader ? "leader" : "validator" as NodeRole,
      status: getNodeStatus(node.status, node.latency)
    };
  });
}

function adaptValidators(
  rawNodes: NetworkNodeRaw[],
  votingStatus: { node_id: string; voting: boolean }[],
  leaderName: string | undefined,
  leaderId: string | undefined
): ValidatorStatus[] {
  const votingMap = new Map(votingStatus.map(v => [v.node_id, v.voting]));
  const normalizedLeaderId = normalizeId(leaderId);

  return rawNodes.map(node => {
    const isVoting = votingMap.get(node.id) ?? true;
    const normalizedNodeId = normalizeId(node.id);
    const isLeader = node.name === leaderName || (normalizedLeaderId && normalizedNodeId === normalizedLeaderId);
    const nodeStatus = getNodeStatus(node.status, node.latency);

    return {
      name: getNodeName(node.id),
      hash: truncateNodeId(node.id, 6, 4),
      status: isVoting ? nodeStatus : "Warning" as NodeStatus,
      latency: formatMs(node.latency) || "0ms",
      lastSeen: node.last_seen ? formatLastSeen(node.last_seen) : "Just now",
      role: isLeader ? "leader" : "validator" as NodeRole
    };
  });
}

function getNodeStatus(status: string, latency: number): NodeStatus {
  const normalizedStatus = status.toLowerCase();
  if (normalizedStatus === "active" || normalizedStatus === "healthy") {
    return latency > 1000 ? "Warning" : "Healthy";
  }
  if (normalizedStatus === "warning" || normalizedStatus === "degraded") {
    return "Warning";
  }
  return "Critical";
}

function formatLastSeen(isoTimestamp: string): string {
  try {
    const date = new Date(isoTimestamp);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffSec = Math.floor(diffMs / 1000);

    if (diffSec < 60) return `${diffSec}s ago`;
    if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
    if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
    return date.toLocaleDateString();
  } catch {
    return "Unknown";
  }
}

function adaptProposalTimeline(proposals: ConsensusProposalRaw[]): ActivityTimelinePoint[] {
  if (!proposals || proposals.length === 0) return [];

  // Group proposals into 30-second buckets
  const bucketMs = 30000;
  const buckets = new Map<number, number>();

  for (const proposal of proposals) {
    const bucketKey = Math.floor(proposal.timestamp / bucketMs) * bucketMs;
    buckets.set(bucketKey, (buckets.get(bucketKey) || 0) + 1);
  }

  // Convert to timeline points
  const sortedKeys = Array.from(buckets.keys()).sort((a, b) => a - b);

  return sortedKeys.map(key => ({
    time: new Date(key).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    value: buckets.get(key) || 0
  }));
}

function adaptVoteTimeline(votes: ConsensusVoteRaw[]): VoteTimelinePoint[] {
  if (!votes || votes.length === 0) return [];

  // Group votes by timestamp into 30-second buckets
  const bucketMs = 30000;
  const buckets = new Map<number, { proposals: number; votes: number; commits: number; viewChanges: number }>();

  for (const vote of votes) {
    const bucketKey = Math.floor(vote.timestamp / bucketMs) * bucketMs;
    const existing = buckets.get(bucketKey) || { proposals: 0, votes: 0, commits: 0, viewChanges: 0 };

    if (vote.type === "proposal" || vote.type === "pre-prepare") {
      existing.proposals += vote.count;
    } else if (vote.type === "commit") {
      existing.commits += vote.count;
    } else if (vote.type === "view_change") {
      existing.viewChanges += vote.count;
    } else {
      existing.votes += vote.count;
    }

    buckets.set(bucketKey, existing);
  }

  // Convert to timeline points
  const sortedKeys = Array.from(buckets.keys()).sort((a, b) => a - b);

  return sortedKeys.map(key => {
    const data = buckets.get(key)!;
    return {
      time: new Date(key).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      proposals: data.proposals,
      votes: data.votes,
      commits: data.commits,
      viewChanges: data.viewChanges
    };
  });
}

function calculatePeakRate(timeline: ActivityTimelinePoint[]): number {
  if (timeline.length === 0) return 0;
  return Math.max(...timeline.map(t => t.value));
}

function calculateAvgRate(timeline: ActivityTimelinePoint[]): number {
  if (timeline.length === 0) return 0;
  const total = timeline.reduce((sum, t) => sum + t.value, 0);
  return Math.round((total / timeline.length) * 10) / 10;
}

function getRiskLevel(connected: number, total: number, stability: number): RiskLevel {
  const connectivity = connected / total;

  if (connectivity >= 0.8 && stability >= 80) return "SAFE";
  if (connectivity >= 0.6 && stability >= 50) return "LOW";
  if (connectivity >= 0.4 && stability >= 30) return "MEDIUM";
  if (connectivity >= 0.2) return "HIGH";
  return "CRITICAL";
}


