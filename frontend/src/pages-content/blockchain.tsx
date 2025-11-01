"use client"

import { useEffect, useMemo, useState } from "react"
import { Activity, AlertCircle, AlertTriangle, BarChart3, Blocks, ChevronRight, FileText, Loader2, RefreshCw, Search } from "lucide-react"
import Link from "next/link"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { DecisionTimelineTab } from "@/components/blockchain/decision-timeline-tab"
import { NetworkSnapshot } from "@/components/blockchain/network-snapshot"
import { BlockDetailsPanel } from "@/components/blockchain/block-details-panel"
import { BlocksTable } from "@/components/blockchain/blocks-table"
import { useBlockchainData } from "@/hooks/use-blockchain-data"
import type { BlockSummary } from "@/lib/api"
import { PageContainer } from "@/components/page-container"

type BlockTypeFilter = "all" | "anomalies" | "normal"
type TimeRangeFilter = "all" | "1h" | "6h" | "24h" | "7d"

export default function BlockchainActivityContent() {
  const { blocks, metrics, isLoading, error, refreshData } = useBlockchainData(true, 6000)

  const [searchQuery, setSearchQuery] = useState("")
  const [blockTypeFilter, setBlockTypeFilter] = useState<BlockTypeFilter>("all")
  const [timeRangeFilter, setTimeRangeFilter] = useState<TimeRangeFilter>("all")
  const [proposerFilter, setProposerFilter] = useState("all")
  const [selectedBlock, setSelectedBlock] = useState<BlockSummary | null>(null)

  const sortedBlocks = useMemo(() => [...blocks].sort((a, b) => b.height - a.height), [blocks])

  // Filter blocks based on search and filters
  const filteredBlocks = useMemo(() => {
    return sortedBlocks.filter((block) => {
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
        if (!block.proposer.toLowerCase().includes(proposerFilter.toLowerCase())) {
          return false
        }
      }

      return true
    })
  }, [sortedBlocks, searchQuery, blockTypeFilter, timeRangeFilter, proposerFilter])

  const proposerOptions = useMemo(() => {
    const unique = new Set<string>()
    sortedBlocks.forEach((block) => {
      if (block.proposer) {
        unique.add(block.proposer)
      }
    })
    return Array.from(unique).sort()
  }, [sortedBlocks])

  const latestUpdatedAt = useMemo(() => {
    if (!sortedBlocks.length) {
      return null
    }
    return sortedBlocks[0]?.timestamp ?? null
  }, [sortedBlocks])

  useEffect(() => {
    if (sortedBlocks.length === 0) {
      setSelectedBlock(null)
      return
    }

    if (!selectedBlock) {
      setSelectedBlock(sortedBlocks[0])
      return
    }

    const updated = sortedBlocks.find((block) => block.height === selectedBlock.height)
    if (updated && updated.hash !== selectedBlock.hash) {
      setSelectedBlock(updated)
    }
  }, [sortedBlocks, selectedBlock])

  const handleBlockSelect = (block: BlockSummary) => {
    setSelectedBlock(block)
  }

  return (
    <PageContainer align="left" className="flex flex-col gap-8 py-6 lg:py-8">
      <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-border/60 bg-gradient-to-br from-primary/20 to-accent/20">
            <Blocks className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h1 className="text-3xl font-bold text-foreground">Blockchain Activity</h1>
            <p className="text-sm text-muted-foreground">Live block production, anomaly detection, and proposer telemetry</p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          {isLoading ? (
            <Badge variant="outline" className="flex items-center gap-1 text-xs">
              <Loader2 className="h-3 w-3 animate-spin" /> Syncing…
            </Badge>
          ) : null}
          {error ? (
            <Badge variant="destructive" className="flex items-center gap-1 text-xs">
              <AlertCircle className="h-3 w-3" /> {error}
            </Badge>
          ) : null}
          {latestUpdatedAt ? (
            <Badge variant="outline" className="text-xs">
              Updated {new Date(latestUpdatedAt * 1000).toLocaleTimeString()}
            </Badge>
          ) : null}
          <Button variant="outline" size="sm" onClick={refreshData}>
            <RefreshCw className="mr-2 h-4 w-4" /> Refresh
          </Button>
        </div>
      </header>

      <div className="space-y-6">
        {/* Top 4 KPIs */}
        <section className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
        <MetricCard
          label="Latest Height"
          helper="Most recent committed block"
          value={metrics?.latestHeight ? metrics.latestHeight.toLocaleString() : "--"}
        />
        <MetricCard
          label="Total Transactions"
          helper="Cumulative transactions observed"
          value={metrics?.totalTransactions ? metrics.totalTransactions.toLocaleString() : "--"}
        />
        <MetricCard
          label="Avg Block Time"
          helper="Backend-reported average block time"
          value={metrics?.avgBlockTime ? `${metrics.avgBlockTime.toFixed(2)}s` : "--"}
        />
        <MetricCard
          label="Success Rate"
          helper="Backend execution success rate"
          value={metrics ? `${(metrics.successRate * 100).toFixed(1)}%` : "--"}
        />
        </section>

        {/* Transaction Analysis Timeline */}
        <section>
        <Card className="glass-card border border-border/30">
          <CardHeader className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
            <div className="flex items-center gap-3">
              <Activity className="h-5 w-5 text-primary" />
              <div>
                <CardTitle className="text-lg text-foreground">Transaction Analysis Timeline</CardTitle>
                <p className="text-sm text-muted-foreground">Last 50 blocks - Normal vs anomaly transaction distribution</p>
              </div>
            </div>
            <Badge variant="outline" className="text-xs">
              Auto-refreshing every 6s
            </Badge>
          </CardHeader>
          <CardContent>
            <DecisionTimelineTab />
          </CardContent>
        </Card>
        </section>

        {/* Side-by-Side Panels */}
        <section className="grid gap-6 xl:grid-cols-2">
        <div>
          {/* Network Snapshot Panel */}
          <NetworkSnapshot
            blocksWithAnomalies={sortedBlocks.filter((block) => (block.anomaly_count ?? 0) > 0).length}
            totalAnomalies={metrics?.anomalyCount ?? 0}
            avgLatency={metrics?.network?.avg_latency_ms}
          />
        </div>
        
        <div>
          {/* Block Details Panel */}
          <BlockDetailsPanel block={selectedBlock} />
        </div>
        </section>

        {/* Search & Filters Section */}
        <section>
        <Card className="glass-card border border-border/30 p-6">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <div className="flex-1">
              <Label htmlFor="block-search" className="text-xs uppercase tracking-wide text-muted-foreground">
                Search by height, hash, or proposer
              </Label>
              <div className="mt-1.5 flex items-center gap-2 rounded-lg border border-border/40 bg-background/60 px-3 py-1.5">
                <Search className="h-4 w-4 text-muted-foreground" />
                <Input
                  id="block-search"
                  value={searchQuery}
                  onChange={(event) => setSearchQuery(event.target.value)}
                  placeholder="e.g. 1072 or 0xabc…"
                  className="h-7 border-0 bg-transparent px-0 text-sm focus-visible:ring-0"
                />
              </div>
            </div>
            <div className="grid w-full gap-3 sm:grid-cols-2 lg:w-auto lg:grid-cols-3">
              <FilterSelect
                label="Block Type"
                value={blockTypeFilter}
                onValueChange={(value) => setBlockTypeFilter(value as BlockTypeFilter)}
                options={[
                  { value: "all", label: "All" },
                  { value: "anomalies", label: "With anomalies" },
                  { value: "normal", label: "Clean blocks" },
                ]}
              />
              <FilterSelect
                label="Time Range"
                value={timeRangeFilter}
                onValueChange={(value) => setTimeRangeFilter(value as TimeRangeFilter)}
                options={[
                  { value: "all", label: "Any time" },
                  { value: "1h", label: "Past hour" },
                  { value: "6h", label: "Past 6 hours" },
                  { value: "24h", label: "Past 24 hours" },
                  { value: "7d", label: "Past 7 days" },
                ]}
              />
              <FilterSelect
                label="Proposer"
                value={proposerFilter}
                onValueChange={setProposerFilter}
                options={[
                  { value: "all", label: "All proposers" },
                  ...proposerOptions.map((proposer) => ({ value: proposer, label: proposer })),
                ]}
              />
            </div>
          </div>
        </Card>
        </section>

        {/* Latest Blocks Section */}
        <section>
          <BlocksTable
            blocks={filteredBlocks}
            onBlockSelect={handleBlockSelect}
            selectedBlock={selectedBlock}
            isLoading={isLoading && !sortedBlocks.length}
          />
          {error && (
            <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/5 p-3 text-sm text-red-600 dark:text-red-400 mt-4">
              <AlertTriangle className="h-4 w-4" />
              {error}
            </div>
          )}
        </section>
      </div>
    </PageContainer>
  )
}



interface MetricCardProps {
  label: string
  helper: string
  value: string
}

function MetricCard({ label, helper, value }: MetricCardProps) {
  return (
    <Card className="glass-card border border-border/30">
      <CardContent className="space-y-3 p-6">
        <div className="flex items-center justify-between">
          <span className="text-sm text-muted-foreground">{label}</span>
        </div>
        <p className="text-3xl font-bold text-foreground tracking-tight">{value}</p>
        <p className="text-xs text-muted-foreground">{helper}</p>
      </CardContent>
    </Card>
  )
}

interface SnapshotRowProps {
  label: string
  metric: string
  helper: string
}

function SnapshotRow({ label, metric, helper }: SnapshotRowProps) {
  return (
    <div className="rounded-lg border border-border/20 bg-background/60 px-4 py-3">
      <p className="text-sm text-muted-foreground">{label}</p>
      <p className="text-2xl font-semibold text-foreground">{metric}</p>
      <p className="text-xs text-muted-foreground mt-1">{helper}</p>
    </div>
  )
}

interface FilterSelectProps {
  label: string
  value: string
  onValueChange: (value: string) => void
  options: Array<{ value: string; label: string }>
}

function FilterSelect({ label, value, onValueChange, options }: FilterSelectProps) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs uppercase tracking-wide text-muted-foreground">{label}</Label>
      <Select value={value} onValueChange={onValueChange}>
        <SelectTrigger className="h-8 border-border/40 bg-background/60 text-sm">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}
