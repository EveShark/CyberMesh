"use client"

import { FileText } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import type { BlockSummary } from "@/lib/api"

interface BlockDetailsPanelProps {
  block?: BlockSummary | null
  className?: string
}

export function BlockDetailsPanel({ block, className = "" }: BlockDetailsPanelProps) {
  const formatTimestamp = (timestamp: number) => {
    const date = new Date(timestamp > 1_000_000_000_000 ? timestamp : timestamp * 1000)
    return date.toLocaleString("en-US", {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    })
  }

  const formatHash = (hash: string, length = 16) => {
    if (hash.length <= length * 2) return hash
    if (hash.startsWith("0x")) {
      return `${hash.slice(0, length + 2)}...${hash.slice(-length)}`
    }
    return `${hash.slice(0, length)}...${hash.slice(-length)}`
  }

  if (!block) {
    return (
      <Card className={`glass-card border border-border/30 ${className}`}>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-lg text-foreground">
            <FileText className="h-4 w-4" />
            Block Details
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col items-center justify-center py-12 space-y-4">
            <div className="p-4 rounded-full bg-muted/20">
              <FileText className="h-12 w-12 text-muted-foreground" />
            </div>
            <div className="text-center space-y-2">
              <h3 className="text-lg font-semibold text-foreground">No Block Selected</h3>
              <p className="text-sm text-muted-foreground max-w-sm">
                Click a block from the Latest Blocks table to view its details and transactions.
              </p>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  const sizeBytes = Number.isFinite(block.size_bytes) ? block.size_bytes : 0
  const sizePrefix = block.size_bytes_estimated ? "≈" : ""
  const rawSizeSource = block.metadata?.["size_source"]
  const sizeSource = typeof rawSizeSource === "string" ? rawSizeSource.replace(/_/g, " ") : undefined

  return (
    <Card className={`glass-card border border-border/30 ${className}`}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-lg text-foreground">
          <FileText className="h-4 w-4" />
          Block Details
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          <div>
            <h3 className="text-lg font-semibold text-foreground">
              Block #{block.height.toLocaleString()}
            </h3>
            <p className="text-sm text-muted-foreground mt-1">Committed block details from validators</p>
          </div>
          
          <div className="space-y-3 text-sm">
            <div className="flex items-center justify-between py-2 border-b border-border/20">
              <span className="text-muted-foreground">Height</span>
              <span className="font-mono text-foreground">{block.height.toLocaleString()}</span>
            </div>
            <div className="flex items-center justify-between py-2 border-b border-border/20">
              <span className="text-muted-foreground">Hash</span>
              <code className="text-xs font-mono text-foreground bg-card/60 px-2 py-1 rounded">
                {formatHash(block.hash)}
              </code>
            </div>
            <div className="flex items-center justify-between py-2 border-b border-border/20">
              <span className="text-muted-foreground">Timestamp</span>
              <span className="text-foreground">{formatTimestamp(block.timestamp)}</span>
            </div>
            <div className="flex items-center justify-between py-2 border-b border-border/20">
              <span className="text-muted-foreground">Transactions</span>
              <span className="text-foreground">{block.transaction_count.toLocaleString()}</span>
            </div>
            <div className="flex items-center justify-between py-2">
              <span className="text-muted-foreground">Size</span>
              <span className="text-foreground">
                {sizePrefix}
                {(sizeBytes / 1024).toFixed(2)} KB
                {block.size_bytes_estimated && sizeSource ? (
                  <span className="ml-2 text-xs text-muted-foreground capitalize">{sizeSource}</span>
                ) : null}
              </span>
            </div>
          </div>

          {block.transactions && block.transactions.length > 0 && (
            <div className="space-y-2">
              <h4 className="text-sm font-semibold text-foreground">Transactions</h4>
              {block.transactions.slice(0, 3).map((tx) => (
                <div
                  key={tx.hash}
                  className="flex items-center justify-between p-2 rounded border border-border/20 hover:bg-accent/20 transition-colors"
                >
                  <code className="text-xs font-mono">{formatHash(tx.hash, 10)}</code>
                  <span className="text-xs text-muted-foreground">
                    {tx.size_bytes ? `${(tx.size_bytes / 1024).toFixed(1)} KB` : ''}
                  </span>
                </div>
              ))}
              {block.transactions.length > 3 && (
                <p className="text-xs text-muted-foreground text-center">
                  +{block.transactions.length - 3} more transactions
                </p>
              )}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
