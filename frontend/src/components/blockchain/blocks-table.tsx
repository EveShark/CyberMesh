"use client"

import { memo } from "react"
import { AlertTriangle, ChevronRight } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import type { BlockSummary } from "@/lib/api"

interface BlocksTableProps {
  blocks: BlockSummary[]
  onBlockSelect: (block: BlockSummary) => void
  selectedBlock?: BlockSummary | null
  isLoading?: boolean
  className?: string
}

interface BlockTableRowProps {
  block: BlockSummary
  onClick: () => void
  isSelected: boolean
}

const BlockTableRow = memo(function BlockTableRow({ block, onClick, isSelected }: BlockTableRowProps) {
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

  const hasAnomalies = (block.anomaly_count ?? 0) > 0

  return (
    <tr 
      className={`blocks-table-row ${isSelected ? 'bg-primary/5' : ''} cursor-pointer`}
      onClick={onClick}
      tabIndex={0}
      onKeyDown={(e) => e.key === 'Enter' && onClick()}
      role="row"
      aria-label={`Block ${block.height}, ${block.transaction_count} transactions`}
    >
      <td className="text-left">
        <div className="flex items-center gap-2">
          <span className="font-mono text-sm text-foreground font-medium">
            #{block.height.toLocaleString()}
          </span>
          {hasAnomalies && (
            <AlertTriangle className="h-4 w-4 text-orange-500 flex-shrink-0" />
          )}
        </div>
      </td>
      <td className="text-left">
        <span className="text-xs text-muted-foreground whitespace-nowrap">
          {formatTimestamp(block.timestamp)}
        </span>
      </td>
      <td className="text-left">
        <span className="text-xs text-foreground">
          {block.transaction_count}tx
        </span>
      </td>
      <td className="text-left">
        <code className="text-xs font-mono text-muted-foreground">
          {formatHash(block.hash)}
        </code>
      </td>
      <td className="text-left">
        <span className="text-xs text-muted-foreground font-mono truncate max-w-20">
          {block.proposer || 'Unknown'}
        </span>
      </td>
      <td className="text-right">
        <ChevronRight className="h-4 w-4 text-muted-foreground ml-auto" />
      </td>
    </tr>
  )
})

export const BlocksTable = memo(function BlocksTable({ 
  blocks, 
  onBlockSelect, 
  selectedBlock,
  isLoading = false, 
  className = "" 
}: BlocksTableProps) {
  if (isLoading && blocks.length === 0) {
    return (
      <Card className={`glass-card border border-border/30 ${className}`}>
        <CardHeader>
          <CardTitle className="text-lg text-foreground">Latest Blocks</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center py-12">
            <div className="text-center">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto mb-4"></div>
              <span className="text-muted-foreground">Loading blocks...</span>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  if (blocks.length === 0) {
    return (
      <Card className={`glass-card border border-border/30 ${className}`}>
        <CardHeader>
          <CardTitle className="text-lg text-foreground">Latest Blocks</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-center py-12">
            <p className="text-muted-foreground">No blocks available</p>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className={`glass-card border border-border/30 ${className}`}>
      <CardHeader>
        <CardTitle className="text-lg text-foreground">Latest Blocks</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="blocks-table-container overflow-x-auto">
          <table className="blocks-table w-full" role="table" aria-label="Latest blockchain blocks">
            <thead>
              <tr className="text-left">
                <th>HEIGHT</th>
                <th>TIME</th>
                <th>TXS</th>
                <th>HASH</th>
                <th>PROPOSER</th>
                <th className="w-8"></th>
              </tr>
            </thead>
            <tbody>
              {blocks.map((block) => (
                <BlockTableRow
                  key={block.height}
                  block={block}
                  onClick={() => onBlockSelect(block)}
                  isSelected={selectedBlock?.height === block.height}
                />
              ))}
            </tbody>
          </table>
        </div>
        
        {blocks.length >= 15 && (
          <div className="flex justify-center pt-4 mt-4 border-t border-border/20">
            <Button variant="outline" size="sm">
              Load More Blocks
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
})
