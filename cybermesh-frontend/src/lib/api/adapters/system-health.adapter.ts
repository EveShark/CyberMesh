import type { SystemHealthData, ServiceInfo, BackendUptime, AIUptime, SystemResources, PipelineData, DatabaseData, AILoopData } from "@/types/system-health";
import type { DashboardOverviewRaw, DashboardBackendRaw } from "../types/raw";
import { formatBytes, formatMs, formatPercent } from "./utils";

export function adaptSystemHealth(raw?: DashboardOverviewRaw): SystemHealthData {
    if (!raw) {
        return {
            serviceReadiness: "Degraded",
            aiStatus: "AI Unknown",
            updated: new Date().toLocaleTimeString(),
            backendUptime: { started: "Unknown", runtime: "0s" },
            aiUptime: { state: "unknown", loopStatus: "Unknown", detectionLoopUptime: "0s" },
            resources: { cpu: { avgUtilization: "0%" }, memory: { resident: "0 B", allocated: "0 B" } },
            services: [],
            pipeline: {
                kafkaProducer: { publishP95: "0ms", publishP50: "0ms", successes: 0, failures: 0 },
                network: { peers: 0, latency: "0ms", throughputIn: "0 B/s", mempoolSize: "0 B" },
                threatFeed: { source: "Unknown", history: "0", fallbackCount: "0", lastFallback: "Never" }
            },
            db: {
                cockroach: { database: "Unknown", latency: "0ms", pool: "0/0", queryP95: "0ms" },
                redis: { mode: "Unknown", role: "Unknown", latency: "0ms", clients: "0" }
            },
            aiLoop: { loopStatus: "Stopped", avgLatency: "0ms", lastIteration: "Never", publishRate: "0/min" }
        };
    }

    const backend = raw.backend;
    const metrics = backend?.metrics;
    const health = backend?.health;
    const readiness = backend?.readiness;

    // Adapt Services
    // Derive AI running state from loop.running (most reliable indicator)
    const aiLoopRunning = raw.ai?.metrics?.loop?.running ?? false;
    const aiStatus = raw.ai?.metrics?.status || (aiLoopRunning ? "active" : "idle");
    const aiHealthy = aiLoopRunning && (raw.ai?.metrics?.loop?.healthy !== false);

    const services: ServiceInfo[] = [
        {
            name: "Core API",
            status: readiness?.ready ? "healthy" : "warning",
            version: backend?.metrics?.summary ? `v${raw.backend?.derived?.ai_loop_status ? "2.0" : "1.0"}` : "Unknown",
            latency: formatMs(getMaxReadinessLatency(readiness)),
            issues: readiness?.ready ? "None" : "Checks failing"
        },
        {
            name: "AI Engine",
            status: aiHealthy ? "healthy" : (aiStatus === "degraded" ? "warning" : "halted"),
            loop: aiLoopRunning ? "Running" : "Stopped",
            issues: raw.ai?.metrics?.message || (raw.ai?.metrics?.loop?.issues?.join(", ") || "None")
        },
        {
            name: "Consensus",
            status: raw.network?.phase === "active" ? "healthy" : (raw.network?.phase === "initializing" ? "warning" : "halted"),
            view: raw.consensus?.term || 0,
            round: 0, // ConsensusOverviewRaw doesn't have 'round', using 0 or deriving if possible
            peers: `${raw.network?.connected_peers || 0}/${raw.network?.total_peers || 0}`,
            issues: raw.network?.connected_peers && raw.network.connected_peers < 4 ? "Low peer count" : "None"
        },
        {
            name: "P2P Network",
            status: raw.network?.connected_peers ? "healthy" : "warning",
            latency: formatMs(raw.network?.average_latency_ms || 0),
            throughput: formatBytes(raw.backend?.stats?.network?.bytes_received || 0) + "/s",
            issues: "None"
        }
    ];

    return {
        serviceReadiness: readiness?.ready ? "Ready" : "Degraded",
        aiStatus: aiLoopRunning ? "AI Running" : "AI Halted",
        updated: new Date().toLocaleTimeString(),

        backendUptime: {
            // Use process_start_time_seconds from metrics (actual server start time)
            // LATCH STRATEGY: Track the EARLIEST start time seen to approximate cluster uptime
            // This prevents uptime jumping down when load balancer hits a recently restarted node
            started: getStableStartTime(metrics?.summary?.process_start_time_seconds),
            runtime: formatDuration(Date.now() - getStableStartTimeUnix(metrics?.summary?.process_start_time_seconds))
        },

        aiUptime: {
            state: aiLoopRunning ? "running" : "halted",
            loopStatus: raw.ai?.metrics?.loop?.status || (aiLoopRunning ? "ok" : "stopped"),
            detectionLoopUptime: formatMs((raw.ai?.metrics?.loop?.seconds_since_last_iteration || 0) * 1000)
        },

        resources: {
            cpu: {
                avgUtilization: formatPercent(metrics?.summary?.goroutines ? metrics.summary.goroutines / 200 : 0)
            },
            memory: {
                resident: formatBytes(metrics?.summary?.resident_memory_bytes),
                allocated: formatBytes(metrics?.summary?.resident_memory_bytes)
            }
        },

        services,

        pipeline: {
            kafkaProducer: {
                publishP95: formatMs(raw.ai?.metrics?.loop?.last_latency_ms || 0),
                publishP50: formatMs((raw.ai?.metrics?.loop?.avg_latency_ms || 0) * 0.8),
                successes: metrics?.kafka?.publish_success || 0,
                failures: metrics?.kafka?.publish_failure || 0
            },
            network: {
                peers: raw.network?.connected_peers || 0,
                latency: formatMs(raw.network?.average_latency_ms || 0),
                throughputIn: formatBytes(raw.backend?.stats?.network?.bytes_received || 0),
                mempoolSize: formatBytes(raw.ledger?.mempool_size_bytes)
            },
            threatFeed: {
                source: raw.threats?.source || "Internal AI",
                history: raw.threats?.stats?.total_count?.toString() || "0",
                fallbackCount: "0",
                lastFallback: "Never"
            }
        },

        db: {
            cockroach: {
                database: raw.backend?.stats?.storage?.database || "default",
                latency: formatMs(raw.backend?.stats?.storage?.ready_latency_ms || 0),
                pool: `${metrics?.cockroach?.in_use || 0}/${metrics?.cockroach?.open_connections || raw.backend?.stats?.storage?.pool_open_connections || 0}`,
                queryP95: formatMs(raw.backend?.stats?.storage?.query_latency_p95_ms || 0)
            },
            redis: {
                mode: raw.backend?.stats?.redis?.mode || ((metrics?.redis?.total_connections || 0) > 1 ? "Cluster" : "Standalone"),
                role: raw.backend?.stats?.redis?.role || ((metrics?.redis?.pool_hits || 0) > 0 ? "Master" : "Unknown"),
                latency: formatMs(raw.backend?.stats?.redis?.ready_latency_ms || metrics?.redis?.command_latency_p95_ms || 0),
                clients: (raw.backend?.stats?.redis?.connected_clients || metrics?.redis?.connected_clients || 0).toString()
            }
        },

        aiLoop: {
            loopStatus: raw.ai?.metrics?.loop?.running ? "Running" : "Stopped",
            avgLatency: formatMs(raw.ai?.metrics?.loop?.avg_latency_ms || 0),
            lastIteration: formatMs(raw.ai?.metrics?.loop?.seconds_since_last_iteration ? raw.ai.metrics.loop.seconds_since_last_iteration * 1000 : 0) + " ago",
            publishRate: (raw.ai?.metrics?.derived?.publish_rate_per_minute || 0).toFixed(1) + "/min"
        }
    };
}

/**
 * Format duration in milliseconds to human-readable string (e.g., "4h 32m", "15m 30s")
 */
function formatDuration(ms: number): string {
    if (ms <= 0) return "0s";

    const seconds = Math.floor(ms / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    if (days > 0) {
        const remainingHours = hours % 24;
        return remainingHours > 0 ? `${days}d ${remainingHours}h` : `${days}d`;
    }
    if (hours > 0) {
        const remainingMinutes = minutes % 60;
        return remainingMinutes > 0 ? `${hours}h ${remainingMinutes}m` : `${hours}h`;
    }
    if (minutes > 0) {
        const remainingSeconds = seconds % 60;
        return remainingSeconds > 0 ? `${minutes}m ${remainingSeconds}s` : `${minutes}m`;
    }
    return `${seconds}s`;
}

/**
 * Get max latency from readiness checks for actual API response time
 */
function getMaxReadinessLatency(readiness?: { checks: Record<string, { latency_ms: number }> }): number {
    if (!readiness?.checks) return 0;
    const latencies = Object.values(readiness.checks).map(c => c.latency_ms || 0);
    return latencies.length > 0 ? Math.max(...latencies) : 0;
}

// LATCH STATE: Store the earliest start time seen during this session
let earliestStartTimeSeconds: number | null = null;

/**
 * Get the stable start time (earliest seen) to prevent uptime jumping down
 * Returns timestamp in milliseconds
 */
function getStableStartTimeUnix(currentProcessStartSeconds?: number): number {
    if (!currentProcessStartSeconds) return 0;

    // If we haven't seen a start time yet, or if this one is EARLIER (older) than what we have,
    // update our latch. We want to track the oldest node to show true cluster uptime.
    if (earliestStartTimeSeconds === null || currentProcessStartSeconds < earliestStartTimeSeconds) {
        earliestStartTimeSeconds = currentProcessStartSeconds;
    }

    return earliestStartTimeSeconds * 1000;
}

/**
 * Get formatted stable start time string
 */
function getStableStartTime(currentProcessStartSeconds?: number): string {
    if (!currentProcessStartSeconds) return "Unknown";

    // Use the logic to update/get the stable time
    const stableMs = getStableStartTimeUnix(currentProcessStartSeconds);
    return new Date(stableMs).toLocaleString();
}
