import type { BlockchainData } from "@/types/blockchain";
import type { DashboardBlocksRaw, DashboardLedgerRaw } from "@/lib/api/types/raw";
import { adaptBlockchain } from "@/lib/api/adapters/blockchain.adapter";

export const mockLedgerRaw: DashboardLedgerRaw = {
  latest_height: 101447,
  state_version: 1234,
  total_transactions: 377113,
  avg_block_time_seconds: 78.13,
  avg_block_size_bytes: 68608,
  state_root: "0x1a2b3c4d5e6f7890abcdef1234567890abcdef1234567890abcdef1234567890",
  last_block_hash: "0xdd90c75d93803feb0123456789abcdef0123456789abcdef0123d039ba66ddb9",
  snapshot_block_height: 101447,
  snapshot_transaction_count: 377113,
  snapshot_timestamp: Date.now() / 1000,
  pending_transactions: 12,
  mempool_size_bytes: 24576,
  reputation_changes: 3,
  policy_changes: 1,
  quarantine_changes: 0,
};

// RAW Backend Data (Mock Generator)

// Stateful store for the mock blockchain
let state = {
  lastUpdated: Date.now(),
  latestHeight: 101447,
  blocks: [
    { height: 101447, hash: "0xdd90c75d93803feb...d039ba66ddb94f46", parent_hash: "0x...", timestamp: Date.now() / 1000, transaction_count: 44, proposer: "Orion", size_bytes: 68608, anomaly_count: 0 },
    { height: 101446, hash: "0xab82c3...21ea08", parent_hash: "0x...", timestamp: (Date.now() / 1000) - 5, transaction_count: 38, proposer: "Orion", size_bytes: 65536, anomaly_count: 1 },
    { height: 101445, hash: "0x7f9a12...c4b3e2", parent_hash: "0x...", timestamp: (Date.now() / 1000) - 10, transaction_count: 52, proposer: "Lyra", size_bytes: 71680, anomaly_count: 0 },
    { height: 101444, hash: "0x3c8f91...e7d4a5", parent_hash: "0x...", timestamp: (Date.now() / 1000) - 15, transaction_count: 29, proposer: "Orion", size_bytes: 62464, anomaly_count: 0 },
    { height: 101443, hash: "0xdc8e24...21ea08", parent_hash: "0x...", timestamp: (Date.now() / 1000) - 20, transaction_count: 44, proposer: "Cygnus", size_bytes: 68608, anomaly_count: 0 }
  ]
};

const SIMULATION_INTERVAL_MS = 5000; // New block every 5 seconds (accelerated)

export const getMockBlockchainRaw = (): DashboardBlocksRaw => {
  const now = Date.now();

  // Calculate how many blocks we should have mined since last update
  // Limit to max 5 blocks at once to prevent crazy catch-up
  const elapsed = now - state.lastUpdated;
  const blocksToMine = Math.floor(elapsed / SIMULATION_INTERVAL_MS);

  if (blocksToMine > 0) {
    for (let i = 0; i < Math.min(blocksToMine, 5); i++) {
      state.latestHeight++;
      const newBlock = {
        height: state.latestHeight,
        hash: `0x${Math.random().toString(16).substring(2, 10)}...${Math.random().toString(16).substring(2, 10)}`,
        parent_hash: state.blocks[0]?.hash || "0x...",
        timestamp: (state.lastUpdated + ((i + 1) * SIMULATION_INTERVAL_MS)) / 1000,
        transaction_count: Math.floor(Math.random() * 50) + 10,
        proposer: ["Orion", "Lyra", "Cygnus", "Draco", "Vela"][Math.floor(Math.random() * 5)],
        size_bytes: 60000 + Math.floor(Math.random() * 20000),
        anomaly_count: Math.random() > 0.9 ? 1 : 0
      };
      state.blocks.unshift(newBlock);
      state.blocks = state.blocks.slice(0, 10); // Keep last 10
    }
    state.lastUpdated = now;
  }

  // Update Ledger too
  mockLedgerRaw.latest_height = state.latestHeight;
  mockLedgerRaw.last_block_hash = state.blocks[0].hash;
  mockLedgerRaw.total_transactions += blocksToMine * 40; // rough tx count
  mockLedgerRaw.pending_transactions = Math.floor(Math.random() * 50); // Jitter mempool
  mockLedgerRaw.mempool_size_bytes = mockLedgerRaw.pending_transactions * 2048;

  // Generate a realistic 24-hour timeline
  const decisionTimeline: any[] = [];
  const pointsPerHour = 6;
  for (let i = 0; i < 24 * pointsPerHour; i++) {
    const timeOffset = i * 600000;
    const timestamp = now - timeOffset;
    const hourOfDay = new Date(timestamp).getHours();
    const activityCurve = Math.sin((hourOfDay - 6) / 24 * Math.PI * 2) * 0.5 + 0.5;
    const baseVolume = 100 + Math.floor(activityCurve * 400);
    const volume = baseVolume + Math.floor(Math.random() * 50);
    const rejectionRate = 0.02 + (Math.random() * 0.05);
    const rejected = Math.floor(volume * rejectionRate);

    decisionTimeline.push({
      time: new Date(timestamp).toISOString(),
      height: state.latestHeight - Math.floor(timeOffset / 5000),
      hash: "0x...",
      proposer: "0x...",
      approved: volume - rejected,
      rejected: rejected
    });
  }
  decisionTimeline.reverse();

  return {
    metrics: {
      latest_height: state.latestHeight,
      total_transactions: mockLedgerRaw.total_transactions,
      avg_block_time_seconds: 5.0, // Matching our simulation
      avg_block_size_bytes: 68608 + Math.floor(Math.random() * 1000),
      success_rate: 0.98,
      anomaly_count: 13 + Math.floor(Math.random() * 5),
      network: {
        peer_count: 4,
        inbound_peers: 4 + Math.floor(Math.random() * 2),
        outbound_peers: 4,
        avg_latency_ms: 2200 + Math.floor(Math.random() * 200),
        bytes_received: blocksToMine * 50000,
        bytes_sent: blocksToMine * 50000
      }
    },
    recent: state.blocks,
    decision_timeline: decisionTimeline,
    pagination: { limit: 10, start: 0, end: 10, total: 100, has_more: true }
  };
};

export const mockBlockchainRaw: DashboardBlocksRaw = getMockBlockchainRaw();

export const mockBlockchainData: BlockchainData = adaptBlockchain(mockBlockchainRaw, mockLedgerRaw);

export interface BlockchainResponse extends BlockchainData {
  updatedAt: string;
}

export const mockBlockchainResponse: BlockchainResponse = {
  ...mockBlockchainData,
  updatedAt: new Date().toISOString(),
};
