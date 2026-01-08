import type { BlockchainData, BlockSummary, TimelineDataPoint, BlockDetail } from "@/types/blockchain";
import type { DashboardBlocksRaw, BlockResponseRaw, DashboardLedgerRaw } from "../types/raw";
import { formatBytes, formatMs, formatPercent, formatNumber, truncateHash } from "./utils";
import { getNodeName } from "@/config/validator-names";

export function adaptBlockchain(raw?: DashboardBlocksRaw, ledger?: DashboardLedgerRaw): BlockchainData {
  if (!raw) {
    return {
      updated: new Date().toLocaleTimeString(),
      metrics: { latestHeight: 0, totalTransactions: 0, avgBlockTime: "--", avgBlockSize: "--", successRate: "--", pendingTxs: 0, mempoolSize: "--" },
      ledger: { snapshotHeight: "--", stateVersion: 0, stateRoot: "--", lastBlockHash: "--", rollingBlockTime: "--", rollingBlockSize: "--", snapshotTime: "--", reputationChanges: 0, policyUpdates: 0, quarantineUpdates: 0 },
      latestBlocks: [],
      networkSnapshot: { blocksAnomalies: 0, totalAnomalies: 0, peerLatencyAvg: "--" },
      timelineData: [],
      selectedBlock: null
    };
  }

  const latestBlocks = raw.recent ? adaptBlocks(raw.recent) : [];

  // Try to use decision_timeline from backend, fallback to generating from blocks
  let timelineData: TimelineDataPoint[] = [];

  if (raw.decision_timeline && raw.decision_timeline.length > 0) {
    // Use backend-provided timeline
    timelineData = raw.decision_timeline.map(d => ({
      time: d.time || new Date().toISOString(),
      approved: d.approved,
      rejected: d.rejected
    }));
  } else if (raw.recent && raw.recent.length > 0) {
    // Generate timeline from block data: txCount = total, anomalyCount = rejected
    // Group blocks by hour for cleaner visualization
    const blocksByHour = new Map<string, { approved: number; rejected: number; timestamp: number }>();

    for (const block of raw.recent) {
      const timestamp = block.timestamp * 1000;
      const date = new Date(timestamp);
      const hourKey = `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}-${date.getHours()}`;

      const existing = blocksByHour.get(hourKey) || { approved: 0, rejected: 0, timestamp };
      existing.approved += Math.max(0, (block.transaction_count || 0) - (block.anomaly_count || 0));
      existing.rejected += block.anomaly_count || 0;
      if (timestamp < existing.timestamp) existing.timestamp = timestamp;
      blocksByHour.set(hourKey, existing);
    }

    // Convert to timeline, sorted by time
    timelineData = Array.from(blocksByHour.values())
      .sort((a, b) => a.timestamp - b.timestamp)
      .map(item => ({
        time: new Date(item.timestamp).toISOString(),
        approved: item.approved,
        rejected: item.rejected
      }));
  }

  // Use latest_height from ledger as primary source, fallback to metrics
  const currentHeight = ledger?.latest_height ?? raw.metrics.latest_height ?? 0;

  // Snapshot height: use snapshot_block_height if valid, otherwise use latest_height
  const snapshotHeight = (ledger?.snapshot_block_height && ledger.snapshot_block_height > 0)
    ? ledger.snapshot_block_height
    : currentHeight;

  const ledgerData = {
    snapshotHeight: formatNumber(snapshotHeight) || "--",
    stateVersion: ledger?.state_version ?? 0,
    stateRoot: formatHash(ledger?.state_root),
    lastBlockHash: formatHash(ledger?.last_block_hash),
    rollingBlockTime: formatMs(raw.metrics.avg_block_time_seconds * 1000) || "--",
    rollingBlockSize: formatBytes(raw.metrics.avg_block_size_bytes) || "--",
    snapshotTime: ledger?.snapshot_timestamp && ledger.snapshot_timestamp > 0
      ? new Date(ledger.snapshot_timestamp * 1000).toLocaleTimeString()
      : "--",
    reputationChanges: ledger?.reputation_changes ?? 0,
    policyUpdates: ledger?.policy_changes ?? 0,
    quarantineUpdates: ledger?.quarantine_changes ?? 0
  };

  return {
    updated: new Date().toLocaleTimeString(),
    metrics: {
      latestHeight: raw.metrics.latest_height,
      totalTransactions: raw.metrics.total_transactions,
      avgBlockTime: formatMs(raw.metrics.avg_block_time_seconds * 1000) || "--",
      avgBlockSize: formatBytes(raw.metrics.avg_block_size_bytes) || "--",
      successRate: formatPercent(raw.metrics.success_rate) || "--",
      pendingTxs: ledger?.pending_transactions ?? 0,
      mempoolSize: formatBytes(ledger?.mempool_size_bytes) || "0 KB",
    },
    ledger: ledgerData,
    latestBlocks,
    networkSnapshot: {
      blocksAnomalies: raw.metrics.anomaly_count || 0,
      totalAnomalies: raw.metrics.anomaly_count || 0,
      peerLatencyAvg: formatMs(raw.metrics.network?.avg_latency_ms) || "--"
    },
    timelineData,
    selectedBlock: null
  };
}

/**
 * Adapt blocks from direct /blocks API response
 */
export function adaptBlocksFromApi(blocks: BlockResponseRaw[]): BlockSummary[] {
  return adaptBlocks(blocks);
}

/**
 * Adapt a single block for detailed view
 */
export function adaptBlockDetail(block: BlockResponseRaw): BlockDetail {
  return {
    height: block.height,
    hash: truncateHash(block.hash) || block.hash,
    timestamp: new Date(block.timestamp * 1000).toLocaleString(),
    txCount: block.transaction_count,
    size: formatBytes(block.size_bytes) || "--",
    transactions: (block.transactions || []).map((tx, i) => ({
      id: i,
      hash: truncateHash(tx.hash) || tx.hash,
      type: tx.type,
      size: formatBytes(tx.size_bytes) || "--",
      status: tx.status || "confirmed"
    }))
  };
}

function adaptBlocks(blocks: BlockResponseRaw[]): BlockSummary[] {
  return blocks.map(b => ({
    height: b.height,
    hash: truncateHash(b.hash) || "",
    proposer: getNodeName(b.proposer) || b.proposer || "Unknown",
    time: new Date(b.timestamp * 1000).toLocaleTimeString(),
    rawTimestamp: b.timestamp * 1000,
    txs: b.transaction_count?.toString() || "0",
    size: formatBytes(b.size_bytes) || "--",
    anomalyCount: b.anomaly_count || 0,
    // Include raw transaction data for detail view
    transactions: (b.transactions || []).map((tx, i) => ({
      id: i,
      hash: truncateHash(tx.hash) || tx.hash,
      type: tx.type,
      size: formatBytes(tx.size_bytes) || "--",
      status: tx.status || "confirmed"
    }))
  }));
}

/**
 * Format hash for display - show truncated version or placeholder
 */
function formatHash(hash?: string | null): string {
  if (!hash || hash === "Unknown" || hash === "0x" || hash === "") {
    return "Unknown";
  }
  // Return truncated hash for display
  return truncateHash(hash) || hash;
}
