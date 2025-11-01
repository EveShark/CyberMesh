"use client"

import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ArrowRight, Clock, FileText, AlertTriangle, CheckCircle, XCircle, Copy, Check } from "lucide-react"
import { useCopyToClipboard } from "@/hooks/use-copy-to-clipboard"

interface BlockCardProps {
  height: number
  hash: string
  timestamp: number
  proposer: string
  transactionCount: number
  anomalyCount: number
  consensusStatus: "approved" | "delayed" | "failed"
  consensusDetails?: string
  onClick?: () => void
}

export function BlockCard({
  height,
  hash,
  timestamp,
  proposer,
  transactionCount,
  anomalyCount,
  consensusStatus,
  consensusDetails,
  onClick,
}: BlockCardProps) {
  const { copy, isCopied } = useCopyToClipboard()

  // Calculate relative time
  const getRelativeTime = (timestamp: number) => {
    const now = Date.now()
    const diff = now - timestamp * 1000
    const seconds = Math.floor(diff / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    const days = Math.floor(hours / 24)

    if (days > 0) return `${days}d ago`
    if (hours > 0) return `${hours}h ago`
    if (minutes > 0) return `${minutes}m ago`
    if (seconds > 0) return `${seconds}s ago`
    return "Just now"
  }

  // Truncate hash
  const truncateHash = (hash: string) => {
    if (hash.startsWith("0x")) {
      return `${hash.slice(0, 10)}...${hash.slice(-6)}`
    }
    return `${hash.slice(0, 8)}...${hash.slice(-4)}`
  }

  // Get card border color based on status
  const getBorderColor = () => {
    if (consensusStatus === "failed") return "border-status-critical/60"
    if (anomalyCount > 0) return "border-status-warning/60"
    return "border-border/40"
  }

  // Get consensus badge
  const getConsensusBadge = () => {
    switch (consensusStatus) {
      case "approved":
        return (
          <Badge className="bg-status-healthy/20 text-status-healthy border-status-healthy/40 border">
            <CheckCircle className="h-3 w-3 mr-1" />
            Approved
          </Badge>
        )
      case "delayed":
        return (
          <Badge className="bg-status-warning/20 text-status-warning border-status-warning/40 border">
            <Clock className="h-3 w-3 mr-1" />
            Delayed
          </Badge>
        )
      case "failed":
        return (
          <Badge className="bg-status-critical/20 text-status-critical border-status-critical/40 border">
            <XCircle className="h-3 w-3 mr-1" />
            Failed
          </Badge>
        )
    }
  }

  return (
    <Card
      className={`bg-card/40 backdrop-blur-sm p-3 border-2 ${getBorderColor()} transition-all hover:shadow-lg hover:scale-[1.02] ${
        onClick ? "cursor-pointer" : ""
      }`}
      onClick={onClick}
    >
      <div className="space-y-3">
        {/* Header: Height & Timestamp */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <h3 className="text-lg font-bold text-foreground">Block #{height.toLocaleString()}</h3>
            {anomalyCount > 0 && (
              <Badge variant="destructive" className="bg-status-warning/20 text-status-warning border-status-warning/40">
                <AlertTriangle className="h-3 w-3 mr-1" />
                {anomalyCount} {anomalyCount === 1 ? "Anomaly" : "Anomalies"}
              </Badge>
            )}
          </div>
          <div className="flex items-center gap-1 text-xs text-muted-foreground">
            <Clock className="h-3 w-3" />
            {getRelativeTime(timestamp)}
          </div>
        </div>

        {/* Divider */}
        <div className="border-t border-border/30 my-2"></div>

        {/* Block Info */}
        <div className="space-y-1.5 text-sm">
          <div className="flex items-center justify-between gap-2">
            <span className="text-muted-foreground">Hash:</span>
            <div className="flex items-center gap-2">
              <code className="text-xs font-mono text-foreground bg-card/60 px-2 py-1 rounded">
                {truncateHash(hash)}
              </code>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  copy(hash)
                }}
                className="p-1 hover:bg-accent/50 rounded transition-colors text-muted-foreground hover:text-foreground"
                title={isCopied ? "Copied!" : "Copy hash for verification"}
              >
                {isCopied ? (
                  <Check className="h-4 w-4 text-status-healthy" />
                ) : (
                  <Copy className="h-4 w-4" />
                )}
              </button>
            </div>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">Proposer:</span>
            <span className="text-foreground font-medium">{proposer}</span>
          </div>
        </div>

        {/* Stats Row */}
        <div className="flex items-center gap-3 pt-2">
          <div className="flex items-center gap-1 text-sm">
            <FileText className="h-4 w-4 text-primary" />
            <span className="text-foreground font-medium">{transactionCount}</span>
            <span className="text-muted-foreground text-xs">Transaction{transactionCount !== 1 ? "s" : ""}</span>
          </div>
          {anomalyCount === 0 && (
            <div className="flex items-center gap-1 text-sm">
              <CheckCircle className="h-4 w-4 text-status-healthy" />
              <span className="text-muted-foreground text-xs">No Anomalies</span>
            </div>
          )}
        </div>

        {/* Consensus Status */}
        <div className="flex items-center justify-between pt-2 border-t border-border/30">
          <div className="flex items-center gap-2">
            {getConsensusBadge()}
            {consensusDetails && (
              <span className="text-xs text-muted-foreground">({consensusDetails})</span>
            )}
          </div>
          {onClick && (
            <Button 
              variant="ghost" 
              size="sm" 
              className="hover:bg-accent/50 text-primary"
              onClick={(e) => {
                e.stopPropagation()
                onClick?.()
                // Scroll to block details card in sidebar
                setTimeout(() => {
                  const detailsCard = document.querySelector('[data-block-details]')
                  detailsCard?.scrollIntoView({ 
                    behavior: 'smooth', 
                    block: 'center' 
                  })
                }, 100)
              }}
            >
              View Details
              <ArrowRight className="h-4 w-4 ml-1" />
            </Button>
          )}
        </div>
      </div>
    </Card>
  )
}
