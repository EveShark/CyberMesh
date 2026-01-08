import type {
  DashboardOverviewRaw,
  DashboardBackendRaw,
  DashboardLedgerRaw,
  DashboardValidatorsRaw,
  NetworkOverviewRaw,
  ConsensusOverviewRaw,
  DashboardBlocksRaw,
  DashboardThreatsRaw,
  DashboardAIRaw
} from "@/lib/api/types/raw";
import { getMockAIEngineRaw } from "./ai-engine";
import { getMockThreatsRaw } from "./threats";
import { getMockBlockchainRaw, mockLedgerRaw } from "./blockchain";
import { getMockNetworkRaw } from "./network";

import {
  DashboardOverviewData
} from "@/types/dashboard";

// Mock raw backend dashboard response
export const getMockDashboardRaw = (): DashboardOverviewRaw => {
  const ai = getMockAIEngineRaw();
  const threats = getMockThreatsRaw();
  const blocks = getMockBlockchainRaw(); // This mines blocks and updates mockLedgerRaw
  const network = getMockNetworkRaw();

  // Sync consensus round with block height
  network.consensus_round = blocks.metrics.latest_height;

  // Generate dynamic ConsensusOverviewRaw based on Network
  const consensus: ConsensusOverviewRaw = {
    term: 24,
    phase: network.phase,
    active_peers: network.connected_peers,
    quorum_size: 4,
    proposals: Array.from({ length: 40 }).map((_, i) => ({
      block: network.consensus_round - i,
      view: 24,
      hash: `0x${Math.random().toString(16).substring(2, 10)}`,
      proposer: ["Orion", "Lyra", "Eridani", "Cygnus"][i % 4],
      timestamp: Date.now() - (i * 5000) // One every 5s
    })),
    votes: Array.from({ length: 120 }).map((_, i) => {
      const type = i % 2 === 0 ? "prevote" : "precommit";
      const baseTime = Date.now() - (Math.floor(i / 2) * 5000); // Pair votes per round
      return {
        type: type,
        count: 4 + (Math.random() > 0.9 ? -1 : 0), // Mostly 4/4, rarely 3/4
        timestamp: baseTime + (type === "precommit" ? 200 : 0) // slight delay for precommit
      };
    }),
    suspicious_nodes: ai.suspicious.nodes.map(n => ({
      id: n.id,
      status: n.status,
      uptime: 0.95 + Math.random() * 0.05,
      suspicion_score: n.suspicion_score,
      reason: n.threat_types.join(", ")
    })),
    updated_at: new Date().toISOString()
  };

  return {
    timestamp: Date.now(),
    backend: {
      health: { status: "ok", timestamp: Date.now() },
      readiness: {
        ready: true,
        checks: {
          cockroach: { status: "ok", latency_ms: 8 + Math.random() * 5 },
          redis: { status: "ok", latency_ms: 2 + Math.random() * 3 },
          kafka: { status: "ok", latency_ms: 12 + Math.random() * 8 },
          state_store: { status: "ok", latency_ms: 1 + Math.random() * 2 },
          mempool: { status: "ok", latency_ms: 1 + Math.random() * 2 },
          consensus: { status: "ok", latency_ms: 5 + Math.random() * 5 },
          p2p: { status: "ok", latency_ms: 3 + Math.random() * 4 },
          ai_service: { status: "ok", latency_ms: 45 + Math.random() * 20 },
          storage: { status: "ok", latency_ms: 6 + Math.random() * 4 }
        }
      },
      stats: {},
      metrics: {
        summary: {
          cpu_seconds_total: 100.0 + (Date.now() - 1767445193000) / 1000,
          resident_memory_bytes: 12500000 + Math.floor(Math.random() * 2000000),
          goroutines: 230 + Math.floor(Math.random() * 20),
          process_start_time_seconds: 1767445193 // Realistic start time
        },
        requests: { total: 4833 + Math.floor(Date.now() / 1000), errors: 21 },
        kafka: { publish_success: 100, publish_failure: 0, broker_count: 3 },
        redis: {
          total_connections: 10,
          idle_connections: 5,
          timeouts: 0,
          command_errors: 0,
          pool_hits: 100,
          pool_misses: 0,
          connected_clients: 2,
          ops_per_sec: 15 + Math.random() * 5,
          command_latency_p95_ms: 1.2 + Math.random()
        },
        cockroach: {
          open_connections: 5,
          in_use: 2,
          idle: 3,
          wait_seconds: 0.001,
          wait_total: 150
        }
      },
      derived: {},
      history: []
    },
    ledger: { ...mockLedgerRaw }, // Use the ledger updated by blockchain mock
    validators: {
      total: 5,
      active: 5,
      inactive: 0,
      validators: [],
      updated_at: Date.now()
    },
    network: network,
    consensus: consensus,
    blocks: blocks,
    threats: threats,
    ai: ai
  };
};

export const mockDashboardRaw: DashboardOverviewRaw = getMockDashboardRaw();

// Adapted dashboard data (frontend-ready format)
// Note: We are using the static initial call for the export, 
// but the hook should call getMockDashboardRaw()
export const getMockDashboardData = (): DashboardOverviewData => {
  const raw = getMockDashboardRaw();
  return {
    timestamp: raw.timestamp,
    backend: raw.backend,
    ledger: raw.ledger,
    validators: raw.validators,
    network: raw.network,
    consensus: raw.consensus,
    blocks: raw.blocks,
    threats: raw.threats,
    ai: raw.ai
  };
};

export const mockDashboardData: DashboardOverviewData = getMockDashboardData();
