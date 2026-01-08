import { useState, useMemo } from "react";
import { Helmet } from "react-helmet-async";
import { BlockSummary, BlockDetail } from "@/types/blockchain";
import {
  BlockchainHeader,
  BlockMetricsGrid,
  LedgerSnapshotPanel,
  TransactionTimelineChart,
  NetworkSnapshotPanel,
  BlockFiltersSection,
  BlockDetailsPanel,
  BlockchainTable,
} from "@/components/features/blockchain";
import { useBlockchainData } from "@/hooks/data/use-blockchain-data";
import { SkeletonCard, SkeletonTable, SkeletonChart, SkeletonMetric } from "@/components/ui/skeleton-card";
import { ApiErrorFallback } from "@/components/ui/api-error-fallback";
import { PullToRefreshIndicator } from "@/components/ui/pull-to-refresh";
import { usePullToRefresh } from "@/hooks/ui/use-pull-to-refresh";
import { useIsMobile } from "@/hooks/ui/use-mobile";


const Blockchain = () => {
  const { data, isLoading, error, refetch } = useBlockchainData({ pollingInterval: 10000 });
  const isMobile = useIsMobile();

  const { pullDistance, isRefreshing, progress } = usePullToRefresh({
    onRefresh: async () => {
      await refetch();
    },
    disabled: !isMobile,
  });

  // Filter states
  const [searchQuery, setSearchQuery] = useState("");
  const [blockType, setBlockType] = useState("all");
  const [timeRange, setTimeRange] = useState("all"); // Default to ALL to ensure blocks show up
  const [proposer, setProposer] = useState("all");
  const [selectedBlock, setSelectedBlock] = useState<BlockDetail | null>(null);

  // Use API data (demo mode handled in hook)
  const blockchainData = data?.data;

  // DEBUG: Log the data to console
  console.log('[Blockchain Page] Data:', {
    isLoading,
    hasData: !!data,
    hasBlockchainData: !!blockchainData,
    blocksCount: blockchainData?.latestBlocks?.length || 0,
    error: error?.message
  });

  // Show loading state when data is not yet available
  const showSkeleton = isLoading && !blockchainData;

  // Show error fallback with retry option
  if (error && !data) {
    return (
      <div className="bg-background min-h-full p-8">
        <ApiErrorFallback
          error={error}
          onRetry={() => refetch()}
          title="Failed to load blockchain data"
        />
      </div>
    );
  }

  const handleRefresh = async () => {
    await refetch();
  };

  const handleBlockSelect = (block: BlockSummary) => {
    setSelectedBlock({
      height: block.height,
      hash: block.hash,
      timestamp: new Date().toLocaleString(), // Use current date/time formatting
      txCount: parseInt(block.txs) || 0,
      size: block.size || "--",
      transactions: block.transactions || [], // Use transactions from the block
    });
  };

  // Extract unique proposers for filter dropdown
  const uniqueProposers: string[] = blockchainData?.latestBlocks
    ? [...new Set(blockchainData.latestBlocks.map(b => b.proposer))]
    : [];

  // Apply client-side filters to blocks
  const filteredBlocks = useMemo(() => {
    if (!blockchainData?.latestBlocks) return [];

    let result = [...blockchainData.latestBlocks];

    // Filter by search query (height or hash)
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      result = result.filter(block =>
        block.height.toString().includes(query) ||
        block.hash.toLowerCase().includes(query)
      );
    }

    // Filter by block type - currently BlockSummary doesn't have anomaly data
    // This filter is a placeholder for when the backend provides anomaly info
    // if (blockType !== "all") { ... }
    // Filter by proposer
    if (proposer !== "all") {
      result = result.filter(block => block.proposer === proposer);
    }

    // Filter by time range
    if (timeRange !== "all") {
      const now = Date.now();
      const ranges: Record<string, number> = {
        "1h": 60 * 60 * 1000,
        "24h": 24 * 60 * 60 * 1000,
        "7d": 7 * 24 * 60 * 60 * 1000,
      };
      const cutoff = ranges[timeRange];
      // Use rawTimestamp if available (reliable), otherwise skip filter to avoid hiding data
      if (cutoff) {
        result = result.filter(block => {
          if (!block.rawTimestamp) return true; // Keep if no timestamp data
          return now - block.rawTimestamp <= cutoff;
        });
      }
    }

    // Sort by time (newest first)
    return result.sort((a, b) => {
      const timeA = a.rawTimestamp || 0;
      const timeB = b.rawTimestamp || 0;
      return timeB - timeA;
    });

    return result;
  }, [blockchainData?.latestBlocks, searchQuery, blockType, proposer, timeRange]);

  return (
    <>
      <Helmet>
        <title>Integrity Ledger | CyberMesh</title>
        <meta name="description" content="Immutable audit trail with live block production, anomaly detection, and proposer telemetry." />
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
            <BlockchainHeader
              updated={blockchainData.updated}
              isRefreshing={isRefreshing}
              onRefresh={handleRefresh}
            />
          )}

          {/* Key Metrics Grid */}
          {showSkeleton ? (
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              {[1, 2, 3, 4].map((i) => (
                <SkeletonMetric key={i} />
              ))}
            </div>
          ) : (
            <BlockMetricsGrid metrics={blockchainData.metrics} />
          )}

          {/* Ledger Snapshot & Network Snapshot */}
          <div className="grid lg:grid-cols-2 gap-6">
            {showSkeleton ? (
              <>
                <SkeletonCard rows={3} />
                <SkeletonCard rows={3} />
              </>
            ) : (
              <>
                <LedgerSnapshotPanel ledger={blockchainData.ledger} />
                <NetworkSnapshotPanel snapshot={blockchainData.networkSnapshot} />
              </>
            )}
          </div>

          {/* Transaction Timeline Chart */}
          {showSkeleton ? (
            <SkeletonChart bars={12} />
          ) : (
            <TransactionTimelineChart data={blockchainData.timelineData} />
          )}

          {/* Filters */}
          <BlockFiltersSection
            searchQuery={searchQuery}
            onSearchChange={setSearchQuery}
            blockType={blockType}
            onBlockTypeChange={setBlockType}
            timeRange={timeRange}
            onTimeRangeChange={setTimeRange}
            proposer={proposer}
            onProposerChange={setProposer}
            proposers={uniqueProposers}
          />

          {/* Block Details & Latest Blocks Table */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 md:gap-6">
            {/* On mobile, show table first, then details */}
            <div className="lg:col-span-2 order-2 lg:order-2 min-w-0">
              {showSkeleton ? (
                <SkeletonTable rows={5} columns={5} />
              ) : (
                <BlockchainTable
                  blocks={filteredBlocks}
                  total={filteredBlocks.length}
                  onBlockSelect={handleBlockSelect}
                  selectedHeight={selectedBlock?.height}
                />
              )}
            </div>
            <div className="lg:col-span-1 order-1 lg:order-1 min-w-0">
              {showSkeleton ? (
                <SkeletonCard rows={4} />
              ) : (
                <BlockDetailsPanel block={selectedBlock} />
              )}
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

export default Blockchain;
