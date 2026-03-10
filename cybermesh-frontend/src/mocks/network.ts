import type { NetworkData } from "@/types/network";
import type { NetworkOverviewRaw, DashboardBlockMetricsRaw, ConsensusOverviewRaw, ConsensusProposalRaw, ConsensusVoteRaw } from "@/lib/api/types/raw";
import { adaptNetwork } from "@/lib/api/adapters/network.adapter";

export const mockBlockMetricsRaw: DashboardBlockMetricsRaw = {
  latest_height: 101504,
  total_transactions: 377500,
  avg_block_time_seconds: 2.5,
  avg_block_size_bytes: 68608,
  success_rate: 1.0,
  anomaly_count: 13,
};

const getLatencyColor = (latency: number) => {
  if (latency < 100) return "Active";
  if (latency < 500) return "Degraded";
  return "Lagging";
}

export const getMockNetworkRaw = (): NetworkOverviewRaw => {
  const timeMod = Date.now() / 1000;

  // Dynamic Peer Count (mostly 4, occasionally 3 or 5)
  const basePeers = 4;
  const peerJitter = Math.random() > 0.9 ? (Math.random() > 0.5 ? 1 : -1) : 0;
  const currentPeers = Math.max(2, Math.min(6, basePeers + peerJitter));

  // Throughput Simulation (Sine wave + noise)
  const globalTraffic = (Math.sin(timeMod / 10) + 1) * 500 + 200; // 200 to 1200 Bps
  const noise = Math.random() * 200;

  return {
    connected_peers: currentPeers,
    total_peers: currentPeers,
    expected_peers: 4,
    average_latency_ms: 1539 + (Math.sin(timeMod / 5) * 200) + (Math.random() * 50),
    consensus_round: 101504 + Math.floor(timeMod / 5), // Increment round every 5s
    leader_stability: 0.98 + (Math.random() * 0.02),
    phase: ["propose", "prevote", "precommit", "commit"][Math.floor((timeMod % 4))],
    leader: "Orion",
    nodes: [
      { id: "node-orion-001", name: "Orion", status: "Active", latency: 45 + Math.random() * 10, uptime: 1.0, last_seen: "0s ago", throughput_bytes: globalTraffic + noise },
      { id: "node-lyra-002", name: "Lyra", status: "Active", latency: 4697 + (Math.random() * 100 - 50), uptime: 0.99, last_seen: "0s ago", throughput_bytes: globalTraffic + noise },
      { id: "node-draco-003", name: "Draco", status: "Active", latency: 1020 + (Math.random() * 50 - 25), uptime: 0.99, last_seen: "0s ago", throughput_bytes: globalTraffic + noise },
      { id: "node-cygnus-004", name: "Cygnus", status: "Active", latency: 917 + (Math.random() * 30), uptime: 1.0, last_seen: "1s ago", throughput_bytes: globalTraffic + noise },
      { id: "node-vela-005", name: "Vela", status: "Active", latency: 442 + (Math.random() * 20), uptime: 1.0, last_seen: "1s ago", throughput_bytes: globalTraffic + noise }
    ],
    voting_status: [],
    inbound_rate_bps: globalTraffic + noise,
    outbound_rate_bps: (globalTraffic + noise) * (0.8 + Math.random() * 0.4),
    updated_at: new Date().toISOString()
  };
};

export const getMockConsensusRaw = (): ConsensusOverviewRaw => {
  const now = Date.now();
  const bucketMs = 5 * 60 * 1000; // 5-minute buckets
  const windowHours = 4;
  const totalBuckets = (windowHours * 60) / 5;

  const proposals: ConsensusProposalRaw[] = [];
  const votes: ConsensusVoteRaw[] = [];

  for (let i = totalBuckets - 1; i >= 0; i--) {
    const ts = now - i * bucketMs;

    // 1-3 proposals per bucket
    const proposalCount = 1 + Math.floor(Math.random() * 3);
    for (let p = 0; p < proposalCount; p++) {
      proposals.push({
        block: 101500 + (totalBuckets - i) * 2 + p,
        view: (totalBuckets - i) % 8,
        hash: `0xmockproposal${(totalBuckets - i).toString(16)}${p.toString(16)}`,
        proposer: ["Orion", "Lyra", "Draco", "Cygnus", "Vela"][(totalBuckets - i + p) % 5],
        timestamp: ts,
      });
    }

    // votes / commits / occasional view changes
    votes.push({ type: "vote", count: 8 + Math.floor(Math.random() * 5), timestamp: ts });
    votes.push({ type: "commit", count: 3 + Math.floor(Math.random() * 3), timestamp: ts });
    if (i % 6 === 0) {
      votes.push({ type: "view_change", count: 1 + Math.floor(Math.random() * 2), timestamp: ts });
    }
  }

  return {
    leader: "Orion",
    leader_id: "node-orion-001",
    term: 42,
    phase: "commit",
    active_peers: 5,
    quorum_size: 4,
    proposals,
    votes,
    suspicious_nodes: [],
    updated_at: new Date().toISOString(),
  };
};

export const mockNetworkRaw: NetworkOverviewRaw = getMockNetworkRaw();

export const mockNetworkData: NetworkData = adaptNetwork(mockNetworkRaw, mockBlockMetricsRaw, getMockConsensusRaw());

export interface NetworkResponse extends NetworkData {
  updatedAt: string;
}

export const mockNetworkResponse: NetworkResponse = {
  ...mockNetworkData,
  updatedAt: new Date().toISOString(),
};
