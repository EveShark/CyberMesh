import { useState, useMemo } from "react";
import { Helmet } from "react-helmet-async";
import {
  NetworkHeader,
  TelemetryStrip,
  NetworkGraph,
  ProposalChart,
  VoteTimeline,
  ValidatorStatusGrid,
  ConsensusSummaryFooter,
} from "@/components/features/network";
import { useNetworkData } from "@/hooks/data/use-network-data";
import { SkeletonCard, SkeletonChart, SkeletonMetric } from "@/components/ui/skeleton-card";
import { ApiErrorFallback } from "@/components/ui/api-error-fallback";
import { PullToRefreshIndicator } from "@/components/ui/pull-to-refresh";
import { usePullToRefresh } from "@/hooks/ui/use-pull-to-refresh";
import { useIsMobile } from "@/hooks/ui/use-mobile";


const Network = () => {
  const { data, isLoading, error, refetch } = useNetworkData({ pollingInterval: 10000 });
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
          title="Failed to load network data"
        />
      </div>
    );
  }

  // Use API data (demo mode handled in hook)
  const networkData = data?.data;

  // Show loading state when data is not yet available
  const showSkeleton = isLoading && !networkData;

  const handleRefresh = async () => {
    await refetch();
  };

  // Transform topology nodes to NetworkGraph format
  const graphNodes = useMemo(() => {
    if (!networkData) return [];
    return networkData.topology.nodes.map((node) => ({
      id: node.fullId || node.hash, // Use full ID for leader matching
      name: node.name,
      status: node.status || "Healthy",
      latency: parseInt(node.latency.replace("ms", ""), 10),
      uptime: parseFloat(node.uptime.replace("%", "")),
      lastSeen: networkData.consensus.validators.find((v) => v.name === node.name)?.lastSeen || null,
    }));
  }, [networkData]);

  // Generate edges (mesh topology - all nodes connected)
  const graphEdges = useMemo(() => {
    const edges: { source: string; target: string }[] = [];
    for (let i = 0; i < graphNodes.length; i++) {
      for (let j = i + 1; j < graphNodes.length; j++) {
        edges.push({ source: graphNodes[i].id, target: graphNodes[j].id });
      }
    }
    return edges;
  }, [graphNodes]);

  // Use real proposals from backend with actual timestamps
  const proposals = useMemo(() => {
    if (!networkData) return [];
    return networkData.activity.proposals || [];
  }, [networkData]);

  // Use real votes from backend with actual timestamps
  const votes = useMemo(() => {
    if (!networkData) return [];
    return networkData.consensus.rawVotes || [];
  }, [networkData]);

  return (
    <>
      <Helmet>
        <title>Consensus Network | CyberMesh</title>
        <meta name="description" content="Distributed validation topology with real-time P2P visualization and PBFT consensus status." />
      </Helmet>

      <div className="bg-background relative">
        <PullToRefreshIndicator
          pullDistance={pullDistance}
          isRefreshing={isRefreshing}
          progress={progress}
        />
        <div className="container mx-auto px-4 sm:px-6 py-8 max-w-7xl space-y-6">
          <NetworkHeader onRefresh={handleRefresh} isRefreshing={isRefreshing} />

          {showSkeleton ? (
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
              {[1, 2, 3, 4].map((i) => (
                <SkeletonMetric key={i} />
              ))}
            </div>
          ) : (
            <TelemetryStrip telemetry={networkData.telemetry} />
          )}

          {showSkeleton ? (
            <div className="rounded-xl p-5 glass-frost h-80 flex items-center justify-center">
              <div className="text-center space-y-3">
                <div className="w-16 h-16 mx-auto rounded-full bg-muted animate-pulse" />
                <div className="space-y-2">
                  <div className="h-4 w-32 mx-auto bg-muted animate-pulse rounded" />
                  <div className="h-3 w-24 mx-auto bg-muted animate-pulse rounded" />
                </div>
              </div>
            </div>
          ) : (
            <NetworkGraph
              nodes={graphNodes}
              edges={graphEdges}
              leader={networkData.topology.leader}
              leaderId={networkData.topology.leaderId}
              isLoading={isRefreshing}
            />
          )}

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {showSkeleton ? (
              <>
                <SkeletonChart bars={10} />
                <SkeletonChart bars={10} />
              </>
            ) : (
              <>
                <ProposalChart proposals={proposals} isLoading={isRefreshing} />
                <VoteTimeline votes={votes} isLoading={isRefreshing} />
              </>
            )}
          </div>

          {showSkeleton ? (
            <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
              {[1, 2, 3, 4, 5].map((i) => (
                <SkeletonCard key={i} rows={2} />
              ))}
            </div>
          ) : (
            <ValidatorStatusGrid
              validators={networkData.consensus.validators}
              leader={networkData.topology.leader}
            />
          )}

          {showSkeleton ? (
            <SkeletonCard rows={1} />
          ) : (
            <ConsensusSummaryFooter summary={networkData.consensus.summary} />
          )}
        </div>
      </div>
    </>
  );
};

export default Network;
