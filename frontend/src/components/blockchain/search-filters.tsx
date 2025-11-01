"use client"

import { useState } from "react"
import { Search, Filter, RefreshCw } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Card } from "@/components/ui/card"

interface SearchFiltersProps {
  searchQuery: string
  onSearchChange: (query: string) => void
  blockTypeFilter: string
  onBlockTypeChange: (type: string) => void
  timeRangeFilter: string
  onTimeRangeChange: (range: string) => void
  proposerFilter: string
  onProposerChange: (proposer: string) => void
  isAutoRefresh: boolean
  onAutoRefreshToggle: () => void
  onRefreshNow?: () => void
  proposerList?: string[]
}

export function SearchFilters({
  searchQuery,
  onSearchChange,
  blockTypeFilter,
  onBlockTypeChange,
  timeRangeFilter,
  onTimeRangeChange,
  proposerFilter,
  onProposerChange,
  isAutoRefresh,
  onAutoRefreshToggle,
  onRefreshNow,
  proposerList = ["All Proposers", "Validator-1", "Validator-2", "Validator-3"],
}: SearchFiltersProps) {
  const [activeFiltersCount, setActiveFiltersCount] = useState(0)

  // Count active filters
  const updateActiveFilters = () => {
    let count = 0
    if (searchQuery) count++
    if (blockTypeFilter !== "all") count++
    if (timeRangeFilter !== "all") count++
    if (proposerFilter !== "all") count++
    setActiveFiltersCount(count)
  }

  return (
    <Card className="glass-card p-4 border border-border/40">
      <div className="flex flex-col gap-4">
        {/* Search Bar */}
        <div className="flex flex-col lg:flex-row gap-3">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <Input
              type="text"
              placeholder="Search by height, hash, or proposer..."
              value={searchQuery}
              onChange={(e) => {
                onSearchChange(e.target.value)
                updateActiveFilters()
              }}
              className="pl-10 bg-card/60 border-border/40 focus:border-primary/60"
            />
            {searchQuery && (
              <button
                onClick={() => {
                  onSearchChange("")
                  updateActiveFilters()
                }}
                className="absolute right-3 top-1/2 transform -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <span className="text-xs">✕</span>
              </button>
            )}
          </div>

          {/* Refresh Controls */}
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="default"
              onClick={onRefreshNow}
              className="hover-scale border-border/40"
            >
              <RefreshCw className="h-4 w-4 mr-2" />
              Refresh
            </Button>
            <Button
              variant={isAutoRefresh ? "default" : "outline"}
              size="default"
              onClick={onAutoRefreshToggle}
              className={`hover-scale ${isAutoRefresh ? "bg-status-healthy hover:bg-status-healthy/90" : "border-border/40"}`}
            >
              <div className="flex items-center gap-2">
                {isAutoRefresh && (
                  <span className="relative flex h-2 w-2">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-white opacity-75"></span>
                    <span className="relative inline-flex rounded-full h-2 w-2 bg-white"></span>
                  </span>
                )}
                <span>Auto-refresh: {isAutoRefresh ? "ON" : "OFF"}</span>
              </div>
            </Button>
          </div>
        </div>

        {/* Filters Row */}
        <div className="flex flex-col sm:flex-row gap-3 items-start sm:items-center">
          <div className="flex items-center gap-2">
            <Filter className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm text-muted-foreground">Filters:</span>
            {activeFiltersCount > 0 && (
              <Badge variant="secondary" className="bg-primary/20 text-primary">
                {activeFiltersCount} active
              </Badge>
            )}
          </div>

          <div className="flex flex-wrap gap-2 flex-1">
            {/* Block Type Filter */}
            <Select value={blockTypeFilter} onValueChange={onBlockTypeChange}>
              <SelectTrigger className="w-[160px] bg-card/60 border-border/40">
                <SelectValue placeholder="Block Type" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Blocks</SelectItem>
                <SelectItem value="normal">Normal Only</SelectItem>
                <SelectItem value="anomalies">Anomalies Only</SelectItem>
                <SelectItem value="failed">Failed Consensus</SelectItem>
              </SelectContent>
            </Select>

            {/* Time Range Filter */}
            <Select value={timeRangeFilter} onValueChange={onTimeRangeChange}>
              <SelectTrigger className="w-[160px] bg-card/60 border-border/40">
                <SelectValue placeholder="Time Range" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Time</SelectItem>
                <SelectItem value="1h">Last Hour</SelectItem>
                <SelectItem value="6h">Last 6 Hours</SelectItem>
                <SelectItem value="24h">Last 24 Hours</SelectItem>
                <SelectItem value="7d">Last 7 Days</SelectItem>
              </SelectContent>
            </Select>

            {/* Proposer Filter */}
            <Select value={proposerFilter} onValueChange={onProposerChange}>
              <SelectTrigger className="w-[160px] bg-card/60 border-border/40">
                <SelectValue placeholder="Proposer" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Proposers</SelectItem>
                {proposerList.slice(1).map((proposer) => (
                  <SelectItem key={proposer} value={proposer.toLowerCase()}>
                    {proposer}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {/* Clear Filters Button */}
            {activeFiltersCount > 0 && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  onSearchChange("")
                  onBlockTypeChange("all")
                  onTimeRangeChange("all")
                  onProposerChange("all")
                  setActiveFiltersCount(0)
                }}
                className="text-muted-foreground hover:text-foreground"
              >
                Clear All
              </Button>
            )}
          </div>
        </div>
      </div>
    </Card>
  )
}
