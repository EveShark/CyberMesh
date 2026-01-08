import { Helmet } from "react-helmet-async";
import {
  ThreatsHeader,
  DetectionSummaryPanel,
  ThreatSeverityChart,
  DetectionBreakdownTable,
  ThreatSnapshotPanel,
} from "@/components/features/threats";
import DetectionPerformanceChart from "@/components/features/threats/components/DetectionPerformanceChart";
import { useThreatsData } from "@/hooks/data/use-threats-data";
import { SkeletonCard, SkeletonTable, SkeletonChart } from "@/components/ui/skeleton-card";
import { ApiErrorFallback } from "@/components/ui/api-error-fallback";
import { PullToRefreshIndicator } from "@/components/ui/pull-to-refresh";
import { usePullToRefresh } from "@/hooks/ui/use-pull-to-refresh";
import { useIsMobile } from "@/hooks/ui/use-mobile";


const Threats = () => {
  const { data, isLoading, error, refetch } = useThreatsData({ pollingInterval: 10000 });
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
          title="Failed to load threat data"
        />
      </div>
    );
  }

  // Use API data (demo mode handled in hook)
  const threatsData = data?.data;

  // Show loading state when data is not yet available
  const showSkeleton = isLoading && !threatsData;

  const handleRefresh = async () => {
    await refetch();
  };

  return (
    <>
      <Helmet>
        <title>Threat Intelligence | CyberMesh</title>
        <meta name="description" content="Real-time threat intelligence, severity analysis, and security incident monitoring." />
      </Helmet>

      <div className="bg-background relative">
        <PullToRefreshIndicator
          pullDistance={pullDistance}
          isRefreshing={isRefreshing}
          progress={progress}
        />
        <div className="container mx-auto px-4 sm:px-6 py-8 max-w-7xl space-y-6">
          {/* Header */}
          {showSkeleton ? (
            <SkeletonCard rows={1} />
          ) : (
            <ThreatsHeader
              systemStatus={threatsData.systemStatus}
              blockedCount={threatsData.blockedCount}
              onRefresh={handleRefresh}
              isRefreshing={isRefreshing}
            />
          )}

          {/* Detection Summary */}
          {showSkeleton ? (
            <SkeletonCard rows={2} />
          ) : (
            <DetectionSummaryPanel
              systemStatus={threatsData.systemStatus}
              lifetimeTotal={threatsData.lifetimeTotal}
              publishedCount={threatsData.publishedCount}
              publishedPercent={threatsData.publishedPercent}
              abstainedCount={threatsData.abstainedCount}
              abstainedPercent={threatsData.abstainedPercent}
              avgResponseTime={threatsData.avgResponseTime}
              validatorCount={threatsData.validatorCount}
              quorumSize={threatsData.quorumSize}
              updated={threatsData.updated}
            />
          )}

          {/* Charts Grid */}
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            {showSkeleton ? (
              <>
                <SkeletonChart bars={6} />
                <SkeletonChart bars={8} />
              </>
            ) : (
              <>
                <ThreatSeverityChart
                  severity={threatsData.severity}
                  systemStatus={threatsData.systemStatus}
                  dataWindow={threatsData.dataWindow}
                />
                <DetectionPerformanceChart
                  latencyHistory={threatsData.latencyHistory || []}
                  loopMetrics={threatsData.loopMetrics}
                  lifetimeTotal={threatsData.lifetimeTotal}
                  publishedCount={threatsData.publishedCount}
                  systemStatus={threatsData.systemStatus}
                />
              </>
            )}
          </div>

          {/* Detection Breakdown Table */}
          {showSkeleton ? (
            <SkeletonTable rows={4} columns={5} />
          ) : (
            <DetectionBreakdownTable
              breakdown={threatsData.breakdown}
              systemStatus={threatsData.systemStatus}
            />
          )}

          {/* Threat Snapshot */}
          {showSkeleton ? (
            <SkeletonCard rows={2} />
          ) : (
            <ThreatSnapshotPanel
              snapshot={threatsData.snapshot}
              systemStatus={threatsData.systemStatus}
              validatorCount={threatsData.validatorCount}
              quorumSize={threatsData.quorumSize}
            />
          )}
        </div>
      </div >
    </>
  );
};

export default Threats;
