import { Helmet } from "react-helmet-async";
import { toast } from "@/hooks/common/use-toast";
import { isDemoMode } from "@/config/demo-mode";
import {
  SystemHealthHeader,
  UptimeResourcesPanel,
  ServiceHealthGrid,
  PipelineNetworkPanel,
  DatabasePanel,
  AILoopPanel,
} from "@/components/features/system-health";
import { useSystemHealthData } from "@/hooks/data/use-system-health-data";
import { SkeletonCard, SkeletonTable, SkeletonMetric } from "@/components/ui/skeleton-card";
import { InlineErrorBanner } from "@/components/ui/inline-error-banner";
import { BackendStatusPanel } from "@/components/ui/backend-status-panel";
import { PullToRefreshIndicator } from "@/components/ui/pull-to-refresh";
import { usePullToRefresh } from "@/hooks/ui/use-pull-to-refresh";
import { useIsMobile } from "@/hooks/ui/use-mobile";


const SystemHealth = () => {
  const { data, isLoading, error, refetch } = useSystemHealthData({ pollingInterval: 15000 });
  const isMobile = useIsMobile();

  const { pullDistance, isRefreshing, progress } = usePullToRefresh({
    onRefresh: async () => {
      await refetch();
      toast({
        title: "Data Refreshed",
        description: "System health data has been updated.",
      });
    },
    disabled: !isMobile,
  });

  // Use API data (demo mode handled in hook)
  const healthData = data?.data;

  // Show loading state when data is not yet available
  const showSkeleton = isLoading && !healthData;

  // Show inline error banner if error occurred but still show fallback data
  const showError = !!error;

  const handleRefresh = async () => {
    await refetch();
    toast({
      title: "Data Refreshed",
      description: "System health data has been updated.",
    });
  };

  return (
    <div className="bg-background relative">
      <PullToRefreshIndicator
        pullDistance={pullDistance}
        isRefreshing={isRefreshing}
        progress={progress}
      />
      <Helmet>
        <title>Infrastructure | CyberMesh</title>
        <meta name="description" content="Service health monitoring, resource utilization, and infrastructure metrics in real-time." />
      </Helmet>

      <div className="container mx-auto px-4 sm:px-6 py-8 max-w-7xl">
        {/* Inline error banner - non-blocking */}
        {showError && (
          <div className="mb-6">
            <InlineErrorBanner
              message="Failed to load live data. Showing cached data."
              onRetry={() => refetch()}
            />
          </div>
        )}

        {showSkeleton ? (
          <div className="space-y-6">
            <SkeletonCard rows={2} />
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              {[1, 2, 3, 4].map((i) => (
                <SkeletonMetric key={i} />
              ))}
            </div>
            <SkeletonCard rows={3} />
            <SkeletonTable rows={3} columns={4} />
            <SkeletonCard rows={2} />
          </div>
        ) : (
          <>
            <SystemHealthHeader
              data={healthData}
              onRefresh={handleRefresh}
              isRefreshing={isRefreshing}
            />
            <UptimeResourcesPanel data={healthData} />
            <ServiceHealthGrid services={healthData.services} />
            <PipelineNetworkPanel data={healthData.pipeline} />
            <DatabasePanel data={healthData.db} />
            <AILoopPanel data={healthData.aiLoop} />

            {/* Backend Status Panel - Only show in live mode */}
            {!isDemoMode() && (
              <div className="mt-6">
                <BackendStatusPanel />
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
};

export default SystemHealth;
