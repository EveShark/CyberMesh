/**
 * Zod Runtime Validation Schemas for API Responses
 * 
 * These schemas validate API responses at runtime to catch
 * data structure mismatches between frontend expectations and backend reality.
 */

import { z } from 'zod';

// ============= Common Schemas =============

export const apiResponseSchema = <T extends z.ZodType>(dataSchema: T) =>
  z.object({
    data: dataSchema.optional(),
    error: z.string().optional(),
    updatedAt: z.string().optional(),
  });

// ============= Blockchain Schemas =============

export const blockMetricsSchema = z.object({
  latestHeight: z.number().nullable().optional(),
  totalTransactions: z.number().nullable().optional(),
  avgBlockTime: z.string().nullable().optional(),
  avgBlockSize: z.string().nullable().optional(),
  successRate: z.string().nullable().optional(),
  pendingTxs: z.number().nullable().optional(),
  mempoolSize: z.string().nullable().optional(),
});

export const ledgerSnapshotSchema = z.object({
  snapshotHeight: z.string().nullable().optional(),
  stateVersion: z.number().nullable().optional(),
  rollingBlockTime: z.string().nullable().optional(),
  rollingBlockSize: z.string().nullable().optional(),
  stateRoot: z.string().nullable().optional(),
  lastBlockHash: z.string().nullable().optional(),
  snapshotTime: z.string().nullable().optional(),
  reputationChanges: z.number().optional(),
  policyUpdates: z.number().optional(),
  quarantineUpdates: z.number().optional(),
});

export const timelineDataPointSchema = z.object({
  time: z.string(),
  approved: z.number(),
  rejected: z.number(),
});

export const networkSnapshotSchema = z.object({
  blocksAnomalies: z.number().optional(),
  totalAnomalies: z.number().optional(),
  peerLatencyAvg: z.string().optional(),
});

export const blockTransactionSchema = z.object({
  hash: z.string(),
  size: z.string(),
});

export const blockDetailSchema = z.object({
  height: z.number(),
  hash: z.string(),
  timestamp: z.string(),
  txCount: z.number(),
  size: z.string(),
  transactions: z.array(blockTransactionSchema),
});

export const blockSummarySchema = z.object({
  height: z.number(),
  time: z.string(),
  txs: z.string(),
  hash: z.string(),
  proposer: z.string(),
});

export const blockchainDataSchema = z.object({
  metrics: blockMetricsSchema.optional(),
  ledger: ledgerSnapshotSchema.optional(),
  timelineData: z.array(timelineDataPointSchema).optional(),
  networkSnapshot: networkSnapshotSchema.optional(),
  selectedBlock: blockDetailSchema.nullable().optional(),
  latestBlocks: z.array(blockSummarySchema).optional(),
});

export const blockchainResponseSchema = apiResponseSchema(blockchainDataSchema);

// ============= Threats Schemas =============

export const severityDataSchema = z.object({
  count: z.number(),
  percent: z.string(),
});

export const severityDistributionSchema = z.object({
  critical: severityDataSchema,
  high: severityDataSchema,
  medium: severityDataSchema,
  low: severityDataSchema,
});

export const threatBreakdownSchema = z.object({
  type: z.string(),
  severity: z.enum(["CRITICAL", "HIGH", "MEDIUM", "LOW"]),
  blocked: z.number(),
  monitored: z.number(),
  blockRate: z.string(),
  total: z.number(),
});

export const threatSnapshotSchema = z.object({
  totalDetected: z.number(),
  publishedSnapshot: z.number(),
  abstainedSnapshot: z.number(),
  loopStatus: z.enum(["LIVE", "HALTED"]),
  lastDetection: z.string(),
});

export const volumeDataPointSchema = z.object({
  time: z.string(),
  value: z.number(),
});

export const threatsDataSchema = z.object({
  systemStatus: z.enum(["LIVE", "HALTED"]).optional(),
  blockedCount: z.string().optional(),
  lifetimeTotal: z.number().optional(),
  publishedCount: z.number().optional(),
  publishedPercent: z.string().optional(),
  abstainedCount: z.number().optional(),
  abstainedPercent: z.string().optional(),
  avgResponseTime: z.string().optional(),
  validatorCount: z.number().optional(),
  quorumSize: z.number().optional(),
  severity: severityDistributionSchema.optional(),
  breakdown: z.array(threatBreakdownSchema).optional(),
  snapshot: threatSnapshotSchema.optional(),
  volumeHistory: z.array(volumeDataPointSchema).optional(),
});

export const threatsResponseSchema = apiResponseSchema(threatsDataSchema);

// ============= System Health Schemas =============

export const backendUptimeSchema = z.object({
  started: z.string(),
  runtime: z.string(),
});

export const aiUptimeSchema = z.object({
  state: z.enum(["running", "halted", "unknown"]),
  loopStatus: z.string(),
  detectionLoopUptime: z.string(),
});

export const systemResourcesSchema = z.object({
  cpu: z.object({
    avgUtilization: z.string(),
  }),
  memory: z.object({
    resident: z.string(),
    allocated: z.string().nullable(),
  }),
});

export const serviceInfoSchema = z.object({
  name: z.string(),
  status: z.enum(["healthy", "warning", "degraded", "halted"]),
  latency: z.string().optional(),
  queryP95: z.string().optional(),
  pool: z.string().optional(),
  version: z.union([z.string(), z.number()]).optional(),
  success: z.string().optional(),
  pending: z.number().optional(),
  size: z.string().optional(),
  oldest: z.string().optional(),
  view: z.number().optional(),
  round: z.number().optional(),
  quorum: z.number().optional(),
  peers: z.string().optional(),
  throughput: z.string().optional(),
  publishP95: z.string().optional(),
  lag: z.number().optional(),
  highwater: z.string().optional(),
  clients: z.string().nullable().optional(),
  errors: z.number().optional(),
  txnP95: z.string().optional(),
  slowTxns: z.number().optional(),
  loop: z.string().optional(),
  publishMin: z.number().optional(),
  detectionsMin: z.number().optional(),
  issues: z.string(),
});

export const kafkaProducerDataSchema = z.object({
  publishP95: z.string(),
  publishP50: z.string(),
  successes: z.number(),
  failures: z.number(),
});

export const networkDataSchema = z.object({
  peers: z.number(),
  latency: z.string(),
  throughputIn: z.string(),
  mempoolSize: z.string(),
});

export const threatFeedDataSchema = z.object({
  source: z.string(),
  history: z.string(),
  fallbackCount: z.string().nullable(),
  lastFallback: z.string().nullable(),
});

export const pipelineDataSchema = z.object({
  kafkaProducer: kafkaProducerDataSchema,
  network: networkDataSchema,
  threatFeed: threatFeedDataSchema,
});

export const cockroachDataSchema = z.object({
  database: z.string(),
  latency: z.string(),
  pool: z.string(),
  queryP95: z.string(),
});

export const redisDataSchema = z.object({
  mode: z.string(),
  role: z.string().nullable(),
  latency: z.string(),
  clients: z.string().nullable(),
});

export const databaseDataSchema = z.object({
  cockroach: cockroachDataSchema,
  redis: redisDataSchema,
});

export const aiLoopDataSchema = z.object({
  loopStatus: z.enum(["Running", "Paused", "Stopped"]),
  avgLatency: z.string(),
  lastIteration: z.string(),
  publishRate: z.string(),
});

export const systemHealthDataSchema = z.object({
  serviceReadiness: z.enum(["Ready", "Degraded", "Halted"]).optional(),
  aiStatus: z.enum(["AI Running", "AI Halted", "AI Unknown"]).optional(),
  backendUptime: backendUptimeSchema.optional(),
  aiUptime: aiUptimeSchema.optional(),
  resources: systemResourcesSchema.optional(),
  services: z.array(serviceInfoSchema).optional(),
  pipeline: pipelineDataSchema.optional(),
  db: databaseDataSchema.optional(),
  aiLoop: aiLoopDataSchema.optional(),
});

export const systemHealthResponseSchema = apiResponseSchema(systemHealthDataSchema);

// ============= Validation Utility =============

export interface ValidationResult<T> {
  success: boolean;
  data?: T;
  errors?: z.ZodError['errors'];
  warnings?: string[];
}

/**
 * Validates API response data against a Zod schema.
 * Returns validation result with detailed error information.
 */
export function validateApiResponse<T>(
  schema: z.ZodType<T>,
  data: unknown,
  endpoint: string
): ValidationResult<T> {
  const result = schema.safeParse(data);
  
  if (result.success) {
    return { success: true, data: result.data };
  }
  
  // Log validation errors for debugging
  console.warn(`[API Validation] ${endpoint} response validation failed:`, {
    errors: result.error.errors.map(e => ({
      path: e.path.join('.'),
      message: e.message,
      received: e.code === 'invalid_type' ? (e as z.ZodInvalidTypeIssue).received : undefined,
    })),
  });
  
  return {
    success: false,
    errors: result.error.errors,
    warnings: result.error.errors.map(
      e => `Field "${e.path.join('.')}": ${e.message}`
    ),
  };
}

/**
 * Validates and returns data, falling back to original data if validation fails.
 * Useful when you want to log issues but not break the app.
 */
export function validateWithFallback<T>(
  schema: z.ZodType<T>,
  data: unknown,
  endpoint: string
): T {
  const result = validateApiResponse(schema, data, endpoint);
  
  if (result.success && result.data) {
    return result.data;
  }
  
  // Return original data as fallback (type assertion needed)
  return data as T;
}

// ============= Type Exports =============

export type BlockchainData = z.infer<typeof blockchainDataSchema>;
export type ThreatsData = z.infer<typeof threatsDataSchema>;
export type SystemHealthData = z.infer<typeof systemHealthDataSchema>;
