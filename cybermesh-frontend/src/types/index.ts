/**
 * Types - Main exports
 * 
 * Organized into:
 * - api.ts       - API communication types
 * - ai-engine.ts - AI engine domain types
 * - blockchain.ts - Blockchain domain types
 * - network.ts   - Network domain types
 * - system-health.ts - System health domain types
 * - threats.ts   - Threats domain types
 * 
 * Note: Some types have naming conflicts. Import from specific files when needed.
 */

// API types (includes backend-api.ts)
export * from './api';

// AI Engine types
export type {
  LoopStatus as AILoopStatus,
  HealthStatus,
  SeverityLevel,
  LoopStatusData,
  DetectionLoopMetrics,
  Validator,
  ValidatorsData,
  DetectionEvent,
  DetectionStreamData,
} from './ai-engine';

// Blockchain types
export type {
  BlockSummary,
  BlockMetrics,
  LedgerSnapshot,
  TimelineDataPoint,
  NetworkSnapshot,
  BlockTransaction,
  BlockDetail,
  BlockchainData,
} from './blockchain';

// Network types
export type {
  NodeRole,
  NodeStatus,
  RiskLevel,
  Telemetry,
  TopologyNode,
  Topology,
  ActivityTimelinePoint,
  Activity,
  ValidatorStatus,
  ConsensusSummary,
  VoteTimelinePoint,
  Consensus,
  NetworkData,
} from './network';

// System Health types
export type {
  ServiceStatus,
  ReadinessStatus,
  AIStatus,
  LoopStatus as SystemLoopStatus,
  BackendUptime,
  AIUptime,
  SystemResources,
  ServiceInfo,
  KafkaProducerData,
  NetworkData as SystemNetworkData,
  ThreatFeedData,
  PipelineData,
  CockroachData,
  RedisData,
  DatabaseData,
  AILoopData,
  SystemHealthData,
} from './system-health';

export { getStatusColors, displayValue } from './system-health';

// Threats types
export type {
  SystemStatus,
  ThreatSeverity,
  SeverityData,
  SeverityDistribution,
  ThreatBreakdown,
  ThreatSnapshot,
  VolumeDataPoint,
  ThreatsData,
} from './threats';
