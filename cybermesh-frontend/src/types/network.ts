export type NodeRole = "leader" | "validator";
export type NodeStatus = "Healthy" | "Warning" | "Critical";
export type RiskLevel = "SAFE" | "LOW" | "MEDIUM" | "HIGH" | "CRITICAL";

export interface Telemetry {
  connectedPeers: string;
  avgLatency: number;
  consensusRound: number;
  leaderStability: number;
  inboundRate: string;
  outboundRate: string;
}

export interface TopologyNode {
  name: string;
  hash: string;
  fullId?: string; // Full hex ID for leader matching
  latency: string;
  uptime: string;
  role: NodeRole;
  status?: NodeStatus;
}

export interface Topology {
  leader: string;
  leaderId?: string;
  nodes: TopologyNode[];
}

export interface ActivityTimelinePoint {
  time: string;
  value: number;
}

export interface Proposal {
  block: number;
  view?: number;
  hash?: string;
  proposer?: string;
  timestamp: number; // UnixMilli - real timestamp from backend
}

export interface Activity {
  totalProposals: number;
  peakRate: number;
  avgRate: number;
  timeline: ActivityTimelinePoint[];
  proposals: Proposal[]; // Raw proposals with real timestamps for filtering
}

export interface ValidatorStatus {
  name: string;
  hash?: string;
  status: NodeStatus;
  latency: string;
  lastSeen: string;
  role: NodeRole;
}

export interface ConsensusSummary {
  activeNodes: number;
  operationalCapacity: string;
  leaderStability: string;
  risk: RiskLevel;
}

export interface VoteTimelinePoint {
  time: string;
  proposals: number;
  votes: number;
  commits: number;
  viewChanges: number;
}

export interface Vote {
  type: string;
  count: number;
  timestamp: number; // UnixMilli - real timestamp from backend
}

export interface Consensus {
  proposals: number;
  votes: number;
  commits: number;
  viewChanges: number;
  totalActivity: number;
  validators: ValidatorStatus[];
  summary: ConsensusSummary;
  timeline: VoteTimelinePoint[];
  rawVotes: Vote[]; // Raw votes with real timestamps for filtering
}

export interface NetworkData {
  telemetry: Telemetry;
  topology: Topology;
  activity: Activity;
  consensus: Consensus;
}
