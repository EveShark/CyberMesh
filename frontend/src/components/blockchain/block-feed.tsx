"use client"

import { useState, useEffect } from "react"

import { Button } from "@/components/ui/button"
import { ChevronDown, Loader2, AlertTriangle, ChevronRight } from "lucide-react"
import type { BlockSummary } from "@/lib/api"

interface BlockFeedProps {
  blocks: BlockSummary[]
  isLoading?: boolean
  onBlockClick?: (block: BlockSummary) => void
  searchQuery?: string
  blockTypeFilter?: string
  timeRangeFilter?: string
  proposerFilter?: string
  maxVisible?: number
}

export function BlockFeed({
  blocks,
  isLoading = false,
  onBlockClick,
  searchQuery = "",
  blockTypeFilter = "all",
  timeRangeFilter = "all",
  proposerFilter = "all",
  maxVisible = 10,
}: BlockFeedProps) {
  const [visibleBlocks, setVisibleBlocks] = useState<BlockSummary[]>([])
  const [displayCount, setDisplayCount] = useState(maxVisible)

  // Utility functions
  const formatTimestamp = (timestamp: number) => {
    const date = new Date(timestamp > 1_000_000_000_000 ? timestamp : timestamp * 1000)
    return date.toLocaleString("en-US", {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    })
  }

  const formatHash = (hash: string, length = 6) => {
    if (hash.length <= length * 2) return hash
    if (hash.startsWith("0x")) {
      return `${hash.slice(0, length + 2)}...${hash.slice(-length)}`
    }
    return `${hash.slice(0, length)}...${hash.slice(-length)}`
  }

  // Apply filters
  const filteredBlocks = blocks.filter((block) => {
    // Search filter
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      const matchesHeight = block.height.toString().includes(query)
      const matchesHash = block.hash.toLowerCase().includes(query)
      const matchesProposer = block.proposer.toLowerCase().includes(query)
      
      if (!matchesHeight && !matchesHash && !matchesProposer) {
        return false
      }
    }

    // Block type filter
    if (blockTypeFilter !== "all") {
      const hasAnomalies = (block.anomaly_count ?? 0) > 0
      if (blockTypeFilter === "anomalies" && !hasAnomalies) {
        return false
      }
      if (blockTypeFilter === "normal" && hasAnomalies) {
        return false
      }
      // "failed" would need additional data from backend
    }

    // Time range filter
    if (timeRangeFilter !== "all") {
      const blockTime = block.timestamp > 1_000_000_000_000 ? block.timestamp : block.timestamp * 1000
      const now = Date.now()
      const ranges: Record<string, number> = {
        "1h": 3600000,
        "6h": 21600000,
        "24h": 86400000,
        "7d": 604800000,
      }
      const range = ranges[timeRangeFilter]
      if (range && now - blockTime > range) {
        return false
      }
    }

    // Proposer filter
    if (proposerFilter !== "all") {
      // Match proposer by truncated hash or validator name
      if (!block.proposer.toLowerCase().includes(proposerFilter.toLowerCase())) {
        return false
      }
    }

    return true
  })

  // Update visible blocks when filters change
  useEffect(() => {
    setVisibleBlocks(filteredBlocks.slice(0, displayCount))
  }, [filteredBlocks, displayCount])

  // Get real anomaly count from backend data
  const getAnomalyCount = (block: BlockSummary): number => {
    // Use real anomaly_count field from backend
    return block.anomaly_count || 0
  }

  const handleLoadMore = () => {
    setDisplayCount((prev) => prev + 10)
  }

  if (isLoading && visibleBlocks.length === 0) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <span className="ml-3 text-muted-foreground">Loading blocks...</span>
      </div>
    )
  }

  if (visibleBlocks.length === 0) {
    return (
      <div className="glass-card p-12 rounded-xl border border-border/40 text-center">
        <p className="text-muted-foreground">
          {searchQuery || blockTypeFilter !== "all" || timeRangeFilter !== "all" || proposerFilter !== "all"
            ? "No blocks match your filters"
            : "No blocks available"}
        </p>
        {(searchQuery || blockTypeFilter !== "all" || timeRangeFilter !== "all" || proposerFilter !== "all") && (
          <p className="text-sm text-muted-foreground mt-2">Try adjusting your search or filters</p>
        )}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Table with Headers */}
      <div className="space-y-1">
        {/* Table Headers */}
        <div className="flex items-center justify-between p-3 text-sm font-medium text-muted-foreground border-b border-border/40 bg-muted/20">
          <div className="flex items-center gap-4 min-w-0 flex-1">
            <span className="w-16">HEIGHT</span>
            <span className="w-20">TIME</span>
            <span className="w-12">TXS</span>
            <span className="w-32">HASH</span>
          </div>
          <div className="flex-shrink-0 w-24">
            <span>PROPOSER</span>
          </div>
        </div>
        
        {/* Block Rows */}
        {visibleBlocks.map((block) => (
          <div
            key={block.height}
            className="blockchain-card-compact flex items-center justify-between p-3 hover:bg-primary/10 transition-colors cursor-pointer border-b border-border/10"
            onClick={() => onBlockClick?.(block)}
          >
            <div className="flex items-center gap-4 min-w-0 flex-1">
              <div className="flex items-center gap-2 w-16">
                <span className="font-mono text-sm text-foreground font-medium">
                  #{block.height.toLocaleString()}
                </span>
                {(block.anomaly_count ?? 0) > 0 && (
                  <AlertTriangle className="h-4 w-4 text-orange-500 flex-shrink-0" />
                )}
              </div>
              
              <span className="text-xs text-muted-foreground whitespace-nowrap w-20">
                {formatTimestamp(block.timestamp)}
              </span>
              
              <span className="text-xs text-foreground w-12">
                {block.transaction_count}tx
              </span>
              
              <code className="text-xs font-mono text-muted-foreground truncate w-32">
                {formatHash(block.hash)}
              </code>
            </div>
            
            <div className="flex items-center gap-3 flex-shrink-0 w-24">
              <span className="text-xs text-muted-foreground font-mono truncate">
                {block.proposer || 'Unknown'}
              </span>
              
              <ChevronRight className="h-4 w-4 text-muted-foreground hover:text-primary transition-colors" />
            </div>
          </div>
        ))}
      </div>

      {/* Load More Button */}
      {filteredBlocks.length > visibleBlocks.length && (
        <div className="flex justify-center pt-2">
          <Button
            variant="outline"
            onClick={handleLoadMore}
            className="hover-scale border-border/40"
          >
            <ChevronDown className="h-4 w-4 mr-2" />
            Load 10 More Blocks
          </Button>
        </div>
      )}

      {/* Results Count */}
      <div className="text-center text-sm text-muted-foreground space-y-2">
        <p>
          Showing {visibleBlocks.length} of {filteredBlocks.length} blocks
          {filteredBlocks.length !== blocks.length && ` (filtered from ${blocks.length} total)`}
        </p>
        {blocks.length > 20 && (
          <p className="text-xs">
            Tip: Use search to jump to specific block height or hash
          </p>
        )}
      </div>
    </div>
  )
}
