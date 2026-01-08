export interface BlockMetrics {
  latestHeight: number | null;
  totalTransactions: number | null;
  avgBlockTime: string | null;
  avgBlockSize: string | null;
  successRate: string | null;
  pendingTxs: number | null;
  mempoolSize: string | null;
}

export interface LedgerSnapshot {
  snapshotHeight: string | null;
  stateVersion: number | null;
  rollingBlockTime: string | null;
  rollingBlockSize: string | null;
  stateRoot: string | null;
  lastBlockHash: string | null;
  snapshotTime: string | null;
  reputationChanges: number;
  policyUpdates: number;
  quarantineUpdates: number;
}

export interface TimelineDataPoint {
  time: string;
  approved: number;
  rejected: number;
}

export interface NetworkSnapshot {
  blocksAnomalies: number;
  totalAnomalies: number;
  peerLatencyAvg: string;
}

export interface BlockTransaction {
  id: number;
  hash: string;
  type: string;
  size: string;
  status: string;
}

export interface BlockDetail {
  height: number;
  hash: string;
  timestamp: string;
  txCount: number;
  size: string;
  transactions: BlockTransaction[];
}

export interface BlockSummary {
  height: number;
  time: string;
  txs: string;
  hash: string;
  proposer: string;
  size?: string;
  anomalyCount?: number;
  transactions?: BlockTransaction[]; // Raw transaction data for detail view
  rawTimestamp?: number; // Raw unix timestamp for filtering
}

export interface BlockchainData {
  updated: string;
  metrics: BlockMetrics;
  ledger: LedgerSnapshot;
  timelineData: TimelineDataPoint[];
  networkSnapshot: NetworkSnapshot;
  selectedBlock: BlockDetail | null;
  latestBlocks: BlockSummary[];
}
