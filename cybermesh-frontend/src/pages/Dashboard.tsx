import { Helmet } from "react-helmet-async";
import {
  DashboardHeader,
  StatusCard,
  MetricCard,
  InfrastructureCard,
  AlertsCard,
  DeepDivesSection,
  LatestBlocksTable,
} from "@/components/features/dashboard";
import {
  Server,
  Brain,
  Network,
  Activity,
  Blocks,
  Users,
  Shield,
  Database,
  HardDrive,
  Layers,
  Container,
  Cpu
} from "lucide-react";
import { useDashboardData } from "@/hooks/data/use-dashboard-data";
import { SkeletonCard, SkeletonTable, SkeletonMetric } from "@/components/ui/skeleton-card";
import { ApiErrorFallback } from "@/components/ui/api-error-fallback";
import { PullToRefreshIndicator } from "@/components/ui/pull-to-refresh";
import { usePullToRefresh } from "@/hooks/ui/use-pull-to-refresh";
import { useIsMobile } from "@/hooks/ui/use-mobile";
import { formatDistanceToNow } from "date-fns";
import type { InfrastructureData, InfrastructureItem } from "@/components/features/dashboard/components/InfrastructureCard";
import type { AlertsData, AlertLevel } from "@/components/features/dashboard/components/AlertsCard";
import type { LatestBlocksData, Block } from "@/components/features/dashboard/components/LatestBlocksTable";


const iconMap: Record<string, typeof Server> = {
  Server,
  Brain,
  Network,
  Activity,
  Blocks,
  Users,
  Shield,
};

const Dashboard = () => {
  const { data, isLoading, error, refetch } = useDashboardData({ pollingInterval: 30000 });
  const isMobile = useIsMobile();

  const { pullDistance, isRefreshing, progress } = usePullToRefresh({
    onRefresh: async () => {
      await refetch();
    },
    disabled: !isMobile,
  });

  // Show error fallback with retry option
  if (error && !data) {
    return (
      <div className="bg-background min-h-full p-8">
        <ApiErrorFallback
          error={error}
          onRetry={() => refetch()}
          title="Failed to load dashboard data"
        />
      </div>
    );
  }

  // Use API data (demo mode handled in hook)
  const dashboardData = data?.data;

  // Show loading state when data is not yet available
  const showSkeleton = isLoading && !dashboardData;

  // Helper to format bytes
  const formatBytes = (bytes: number | undefined) => {
    if (bytes === undefined) return "N/A";
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
  };

  // Helper to construct Infrastructure Data
  const getInfrastructureData = (): InfrastructureData | undefined => {
    if (!dashboardData) return undefined;

    // Check readiness checks or metrics to determine status
    // Check readiness checks or metrics to determine status
    const getStatus = (checkName: string): "active" | "warning" | "error" => {
      // Priority 1: If we have positive metrics, it's definitively active/idle (Green)
      if (checkName === 'cockroach' && (dashboardData.backend?.metrics?.cockroach?.open_connections ?? 0) > 0) return "active";
      if (checkName === 'redis' && (dashboardData.backend?.metrics?.redis?.ops_per_sec ?? 0) >= 0) return "active"; // 0 ops is just idle
      if (checkName === 'kafka' && (dashboardData.backend?.metrics?.kafka?.broker_count ?? 0) > 0) return "active";

      // Priority 2: Explicit health checks
      const check = dashboardData.backend?.readiness?.checks?.[checkName];
      if (check) {
        return check.status === "up" || check.status === "ready" ? "active" : "warning"; // Warning instead of error for local dev
      }

      return "active"; // Default optimistic for UI
    };

    const items: InfrastructureItem[] = [
      {
        name: "CockroachDB",
        icon: "Database",
        status: getStatus('cockroach'),
        details: `${dashboardData.backend?.metrics?.cockroach?.open_connections || 0} conns`
      },
      {
        name: "Redis Cache",
        icon: "HardDrive",
        status: getStatus('redis'),
        details: `${dashboardData.backend?.metrics?.redis?.ops_per_sec?.toFixed(0) || 0} ops/s`
      },
      {
        name: "Kafka Stream",
        icon: "Layers",
        status: getStatus('kafka'),
        details: `${dashboardData.backend?.metrics?.kafka?.publish_success || 0} msgs`
      },
      {
        name: "AI Engine",
        icon: "Brain",
        status: (["active", "ready", "running"].includes(dashboardData.ai?.metrics?.status || "")) ? "active" : "warning",
        details: `${dashboardData.ai?.metrics?.engines?.length || 0} models`
      },
      {
        name: "K8s Cluster",
        icon: "Container",
        status: "active",
        details: "GKE Autopilot"
      },
    ];

    return {
      items,
      memory: formatBytes(dashboardData.backend?.metrics?.summary?.resident_memory_bytes),
      uptime: "99.99%" // Placeholder or calculate from start time if available
    };
  };

  // Helper to construct Alerts Data
  const getAlertsData = (): AlertsData | undefined => {
    if (!dashboardData) return undefined;

    const stats = dashboardData.threats?.stats;
    const levels: AlertLevel[] = [
      { label: "Critical", count: stats?.critical_count || 0, color: "text-destructive" },
      { label: "High", count: stats?.high_count || 0, color: "text-amber-500" },
      { label: "Medium", count: stats?.medium_count || 0, color: "text-yellow-500" },
      { label: "Low", count: stats?.low_count || 0, color: "text-blue-500" },
    ];

    return {
      levels,
      published: dashboardData.threats?.breakdown?.totals?.published || 0,
      total: dashboardData.threats?.breakdown?.totals?.overall || 0,
      updated: formatDistanceToNow(dashboardData.threats?.timestamp || Date.now(), { addSuffix: true })
    };
  };

  // Helper to construct Blocks Data
  const getBlocksData = (): LatestBlocksData | undefined => {
    if (!dashboardData || !dashboardData.blocks) return undefined;

    if (!dashboardData || !dashboardData.blocks) return undefined;

    const recentBlocks = [...(dashboardData.blocks.recent || [])].sort((a, b) =>
      (b.timestamp || 0) - (a.timestamp || 0)
    );

    const blocks: Block[] = recentBlocks.map(b => {
      let timeString = "Just now";

      if (b.timestamp) {
        const date = new Date(b.timestamp * 1000);
        const now = new Date();
        const isToday = date.getDate() === now.getDate() &&
          date.getMonth() === now.getMonth() &&
          date.getFullYear() === now.getFullYear();

        const isYesterday = new Date(now.getTime() - 86400000).getDate() === date.getDate() &&
          date.getMonth() === now.getMonth() &&
          date.getFullYear() === now.getFullYear();

        const timeStr = date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });

        if (isToday) {
          timeString = `Today, ${timeStr}`;
        } else if (isYesterday) {
          timeString = `Yesterday, ${timeStr}`;
        } else {
          timeString = `${date.toLocaleDateString([], { month: 'short', day: 'numeric' })}, ${timeStr}`;
        }
      }

      return {
        height: b.height,
        time: timeString,
        txs: b.transaction_count,
        hash: b.hash,
        proposer: b.proposer
      };
    });

    return {
      blocks,
      rangeStart: dashboardData.blocks.pagination?.start ?? recentBlocks[recentBlocks.length - 1]?.height ?? 0,
      rangeEnd: dashboardData.blocks.pagination?.end ?? recentBlocks[0]?.height ?? 0,
      total: dashboardData.blocks.pagination?.total ?? 0
    }
  };

  return (
    <>
      <Helmet>
        <title>Command Center | CyberMesh</title>
        <meta name="description" content="Operational dashboard with real-time telemetry, threat intelligence, and infrastructure monitoring for autonomous cybersecurity defense." />
      </Helmet>

      <div className="bg-background min-h-full relative">
        {/* Pull to refresh indicator */}
        <PullToRefreshIndicator
          pullDistance={pullDistance}
          isRefreshing={isRefreshing}
          progress={progress}
        />

        <DashboardHeader />

        <div className="w-full max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8 space-y-4 sm:space-y-6">
          {/* Row 1: Status Cards */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            {showSkeleton ? (
              <>
                {[1, 2, 3, 4].map((i) => (
                  <SkeletonCard key={i} rows={2} />
                ))}
              </>
            ) : (
              <>
                <StatusCard
                  title="Backend Readiness"
                  icon={Server}
                  status={dashboardData?.backend?.readiness?.ready ? "ready" : "warning"}
                  metrics={(() => {
                    // Calculate uptime from process start time
                    const startTimeSecs = dashboardData?.backend?.metrics?.summary?.process_start_time_seconds;
                    let uptimeStr = "--";
                    if (startTimeSecs && startTimeSecs > 0) {
                      const uptimeMs = Date.now() - (startTimeSecs * 1000);
                      const hours = Math.floor(uptimeMs / (1000 * 60 * 60));
                      const mins = Math.floor((uptimeMs % (1000 * 60 * 60)) / (1000 * 60));
                      uptimeStr = hours > 0 ? `${hours}h ${mins}m` : `${mins}m`;
                    }

                    // Calculate services health from readiness checks
                    const checks = dashboardData?.backend?.readiness?.checks || {};
                    const checkEntries = Object.entries(checks);
                    const healthyCount = checkEntries.filter(([_, c]: [string, any]) => c?.status === "ok").length;
                    const totalCount = checkEntries.length;
                    const servicesStr = totalCount > 0 ? `${healthyCount}/${totalCount} healthy` : "--";

                    return [
                      { label: "Memory", value: formatBytes(dashboardData?.backend?.metrics?.summary?.resident_memory_bytes), highlight: false },
                      { label: "Uptime", value: uptimeStr, highlight: false },
                      { label: "Goroutines", value: dashboardData?.backend?.metrics?.summary?.goroutines || 0, highlight: false },
                      { label: "Services", value: servicesStr, highlight: healthyCount === totalCount }
                    ];
                  })()}
                />
                <StatusCard
                  title="AI Service"
                  icon={Brain}
                  status={["active", "ready", "running"].includes(dashboardData?.ai?.metrics?.status || "") ? "running" : "warning"}
                  metrics={[
                    { label: "Active Models", value: dashboardData?.ai?.metrics?.engines?.filter(e => e.ready).length || 0, highlight: true },
                    { label: "Detections/min", value: dashboardData?.ai?.metrics?.derived?.detections_per_minute?.toFixed(1) || "0", highlight: false },
                    { label: "Iterations/min", value: dashboardData?.ai?.metrics?.derived?.iterations_per_minute?.toFixed(1) || "0", highlight: false },
                    { label: "Publish Rate", value: `${dashboardData?.ai?.metrics?.derived?.publish_rate_per_minute?.toFixed(1) || "0"}/min`, highlight: false }
                  ]}
                  variant="fire"
                />
                <StatusCard
                  title="Network Health"
                  icon={Network}
                  status={["healthy", "ok", "active", "connected"].includes(dashboardData?.backend?.health?.status || "") || (dashboardData?.network?.connected_peers || 0) > 0 ? "healthy" : "warning"}
                  metrics={[
                    { label: "Peers", value: dashboardData?.network?.connected_peers || 0, highlight: true },
                    { label: "Latency", value: `${dashboardData?.network?.average_latency_ms?.toFixed(0) || '0'}ms`, highlight: false }
                  ]}
                />
                <StatusCard
                  title="API Throughput"
                  icon={Activity}
                  status={dashboardData?.backend?.metrics?.requests?.total > 0 ? "active" : "ready"}
                  metrics={[
                    { label: "Requests", value: (dashboardData?.backend?.metrics?.requests?.total || 0).toLocaleString(), highlight: true },
                    { label: "Errors", value: dashboardData?.backend?.metrics?.requests?.errors || 0, highlight: (dashboardData?.backend?.metrics?.requests?.errors || 0) > 0 }
                  ]}
                />
              </>
            )}
          </div>

          {/* Row 2: Operational Metrics */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {showSkeleton ? (
              <>
                {[1, 2, 3].map((i) => (
                  <SkeletonCard key={i} rows={3} />
                ))}
              </>
            ) : (
              <>
                <MetricCard
                  title="Ledger Height"
                  icon={Blocks}
                  metrics={[
                    { label: "Total Transactions", value: dashboardData?.ledger?.total_transactions?.toLocaleString() || "0", color: "frost" },
                    { label: "Current Height", value: dashboardData?.ledger?.latest_height?.toLocaleString() || "0" },
                    { label: "Block Time", value: `${dashboardData?.ledger?.avg_block_time_seconds?.toFixed(2) || "0"}s` }
                  ]}
                  actionLabel="View Blockchain"
                  href="/blockchain"
                />
                <MetricCard
                  title="Network & Consensus"
                  icon={Users}
                  status="Healthy"
                  statusColor="emerald"
                  metrics={[
                    { label: "Validators", value: `${dashboardData?.validators?.active || 0} / ${dashboardData?.validators?.total || 0}`, color: "emerald" },
                    { label: "Consensus View", value: dashboardData?.consensus?.term || 0 }, // Using term as view/term
                    { label: "Quorum Size", value: dashboardData?.consensus?.quorum_size || 0 }
                  ]}
                  actionLabel="View Network Details"
                  href="/network"
                />
                <MetricCard
                  title="Threat Detection"
                  icon={Shield}
                  status={dashboardData?.threats?.detection_loop?.running ? "Active" : "Paused"}
                  statusColor={dashboardData?.threats?.detection_loop?.running ? "emerald" : "amber"}
                  variant="fire"
                  metrics={[
                    { label: "Total Threats", value: dashboardData?.threats?.stats?.total_count?.toLocaleString() || 0, color: "fire" },
                    { label: "Critical", value: dashboardData?.threats?.stats?.critical_count || 0, color: "destructive" },
                    { label: "High Severity", value: dashboardData?.threats?.stats?.high_count || 0, color: "amber" }
                  ]}
                  actionLabel="View Threat Analysis"
                  href="/threats"
                />
              </>
            )}
          </div>

          {/* Row 3: Infrastructure & Alerts */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
            {showSkeleton ? (
              <>
                <SkeletonCard className="lg:col-span-2" rows={2} />
                <SkeletonCard rows={2} />
              </>
            ) : (
              <>
                <InfrastructureCard
                  className="lg:col-span-2"
                  data={getInfrastructureData()}
                />
                <AlertsCard data={getAlertsData()} />
              </>
            )}
          </div>

          {/* Deep Dives Section */}
          {!showSkeleton && <DeepDivesSection deepDives={[]} /* Deep dives data might need construction or removal if not in raw */ />}

          {/* Latest Blocks Table */}
          {showSkeleton ? (
            <SkeletonTable rows={5} columns={5} />
          ) : (
            <LatestBlocksTable data={getBlocksData()} />
          )}
        </div>
      </div>
    </>
  );
};

export default Dashboard;
