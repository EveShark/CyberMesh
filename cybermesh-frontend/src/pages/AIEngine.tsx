import { Helmet } from "react-helmet-async";
import {
  AIEngineHeader,
  LoopStatusCard,
  DetectionLoopCard,
  AIEnginePerformanceTable,
  SuspiciousValidatorsCard,
  DetectionStreamFeed,
} from "@/components/features/ai-engine";
import { useAIEngineData } from "@/hooks/data/use-ai-engine-data";
import { SkeletonCard, SkeletonTable, SkeletonChart } from "@/components/ui/skeleton-card";
import { ApiErrorFallback } from "@/components/ui/api-error-fallback";
import { PullToRefreshIndicator } from "@/components/ui/pull-to-refresh";
import { usePullToRefresh } from "@/hooks/ui/use-pull-to-refresh";
import { useIsMobile } from "@/hooks/ui/use-mobile";


const AIEngine = () => {
  const { data, isLoading, error, refetch } = useAIEngineData({ pollingInterval: 5000 });
  const isMobile = useIsMobile();

  const { pullDistance, isRefreshing, progress } = usePullToRefresh({
    onRefresh: async () => {
      await refetch();
    },
    disabled: !isMobile,
  });

  // Use API data (demo mode handled in hook) - fallback to empty object for skeleton rendering
  const aiData = data?.data;

  // Show error fallback with retry option (only if no cached data)
  if (error && !data) {
    return (
      <div className="bg-background min-h-full p-8">
        <ApiErrorFallback
          error={error}
          onRetry={() => refetch()}
          title="Failed to load AI engine data"
        />
      </div>
    );
  }

  // Show loading state when data is not yet available
  const showSkeleton = isLoading && !aiData;

  return (
    <>
      <Helmet>
        <title>Detection Engine | CyberMesh</title>
        <meta
          name="description"
          content="Autonomous threat detection with real-time instrumentation for detection loop, variant pipelines, and validator signals."
        />
      </Helmet>

      <div className="bg-background relative">
        <PullToRefreshIndicator
          pullDistance={pullDistance}
          isRefreshing={isRefreshing}
          progress={progress}
        />
        <div className="container mx-auto px-4 sm:px-6 py-8 max-w-7xl">
          <AIEngineHeader />

          {/* Row 1: Status Cards */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-6">
            {showSkeleton ? (
              <>
                <SkeletonCard rows={3} />
                <SkeletonCard rows={3} />
              </>
            ) : (
              <>
                <LoopStatusCard data={aiData.loopStatus} />
                <DetectionLoopCard data={aiData.detectionLoop} />
              </>
            )}
          </div>

          {/* Row 2: Full Width Performance Table */}
          <div className="mb-6">
            {showSkeleton ? (
              <SkeletonTable rows={4} columns={6} />
            ) : (
              <AIEnginePerformanceTable engines={aiData.engines} />
            )}
          </div>

          {/* Row 3: Suspicious Validators (full width) */}
          <div className="mb-6">
            {showSkeleton ? (
              <SkeletonCard rows={4} />
            ) : (
              <SuspiciousValidatorsCard data={aiData.validators} />
            )}
          </div>

          {/* Row 4: Full Width Detection Stream */}
          <div className="mb-6">
            {showSkeleton ? (
              <SkeletonTable rows={6} columns={5} />
            ) : (
              <DetectionStreamFeed data={aiData.detectionStream} />
            )}
          </div>
        </div>
      </div>
    </>
  );
};

export default AIEngine;
