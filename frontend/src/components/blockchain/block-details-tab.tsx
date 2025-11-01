"use client"

import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Copy, Check, ShieldCheck, FileText, Clock, Database, Hash, Loader2, AlertCircle } from "lucide-react"
import { useMemo } from "react"
import type { ReactNode } from "react"

import { useCopyToClipboard } from "@/hooks/use-copy-to-clipboard"
import { useBlockDetails } from "@/hooks/use-block-details"
import type { BlockSummary } from "@/lib/api"

interface BlockDetailsTabProps {
  block?: BlockSummary | null
  onVerifyClick?: () => void
}

export function BlockDetailsTab({ block, onVerifyClick }: BlockDetailsTabProps) {
  const selectedHeight = block?.height
  const { block: detailedBlock, isLoading, error } = useBlockDetails(selectedHeight, {
    includeTransactions: true,
    enabled: Boolean(selectedHeight !== undefined),
  })
  const { copy, isCopied, error: copyError } = useCopyToClipboard()

  const displayBlock = useMemo(() => detailedBlock ?? block ?? null, [block, detailedBlock])

  if (!displayBlock) {
    return (
      <div className="flex flex-col items-center justify-center py-12 space-y-4">
        <div className="p-4 rounded-full bg-muted/20">
          <FileText className="h-12 w-12 text-muted-foreground" />
        </div>
        <div className="text-center space-y-2">
          <h3 className="text-lg font-semibold text-foreground">No Block Selected</h3>
          <p className="text-sm text-muted-foreground max-w-sm">
            Choose a block from the recent activity list to load its full details, transactions, and consensus info.
          </p>
        </div>
      </div>
    )
  }

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
    if (hash.length <= length * 2) {
      return hash
    }
    if (hash.startsWith("0x")) {
      return `${hash.slice(0, length + 2)}...${hash.slice(-length)}`
    }
    return `${hash.slice(0, length)}...${hash.slice(-length)}`
  }

  const transactions = displayBlock.transactions ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <h3 className="text-lg font-semibold text-foreground">
            Block #{displayBlock.height.toLocaleString()}
          </h3>
          <p className="text-sm text-muted-foreground mt-1">Committed block details direct from the validators</p>
        </div>
        <Button onClick={onVerifyClick} size="sm" className="hover-scale" disabled={!displayBlock}>
          <ShieldCheck className="h-4 w-4 mr-2" />
          Verify This Block
        </Button>
      </div>

      {error ? (
        <div className="flex items-center gap-2 rounded-lg border border-status-critical/40 bg-status-critical/10 px-3 py-2 text-sm text-status-critical">
          <AlertCircle className="h-4 w-4" />
          {error}
        </div>
      ) : null}

      {/* Block Information */}
      <Card className="bg-card/40 border border-border/40 p-4">
        <div className="flex items-center justify-between">
          <h4 className="text-sm font-semibold text-foreground flex items-center gap-2">
            <Database className="h-4 w-4 text-primary" />
            Block Information
          </h4>
          {isLoading ? (
            <span className="text-xs text-muted-foreground flex items-center gap-1">
              <Loader2 className="h-3 w-3 animate-spin" />
              Loading latest data…
            </span>
          ) : null}
        </div>
        <div className="space-y-3 text-sm mt-3">
          <InfoRow label="Height" value={displayBlock.height.toLocaleString()} mono />
          <HashRow label="Block Hash" value={displayBlock.hash} formatHash={formatHash} onCopy={copy} isCopied={isCopied} />
          <HashRow label="Parent Hash" value={displayBlock.parent_hash} formatHash={formatHash} />
          <HashRow label="State Root" value={displayBlock.state_root} formatHash={formatHash} />
          <InfoRow
            label="Timestamp"
            value={
              <span className="flex items-center gap-2">
                <Clock className="h-4 w-4 text-muted-foreground" />
                {formatTimestamp(displayBlock.timestamp)}
              </span>
            }
          />
          <InfoRow label="Proposer" value={formatHash(displayBlock.proposer, 10)} mono />
          <InfoRow label="Transactions" value={displayBlock.transaction_count.toLocaleString()} />
          <InfoRow label="Estimated Size" value={`${(displayBlock.size_bytes / 1024).toFixed(2)} KB`} />
        </div>
        {copyError ? (
          <p className="mt-3 text-xs text-status-critical">Copy failed: {copyError}</p>
        ) : null}
      </Card>

      {/* Transactions */}
      <Card className="bg-card/40 border border-border/40 p-4">
        <h4 className="text-sm font-semibold text-foreground mb-3 flex items-center gap-2">
          <FileText className="h-4 w-4 text-primary" />
          Transactions ({displayBlock.transaction_count})
        </h4>
        {isLoading ? (
          <div className="flex items-center justify-center py-6 text-sm text-muted-foreground gap-2">
            <Loader2 className="h-4 w-4 animate-spin" />
            Fetching transactions…
          </div>
        ) : transactions.length > 0 ? (
          <div className="space-y-2">
            {transactions.map((tx) => (
              <button
                key={tx.hash}
                onClick={() => {
                  copy(tx.hash)
                }}
                className="w-full flex items-center justify-between p-3 rounded-lg border border-border/20 hover:bg-accent/20 transition-colors cursor-pointer group text-left"
                title="Click to copy transaction hash"
              >
                <div className="flex items-center gap-3">
                  <Hash className="h-4 w-4 text-muted-foreground" />
                  <div className="flex flex-col items-start gap-1">
                    <code className="text-xs font-mono text-foreground">{formatHash(tx.hash, 12)}</code>
                    {tx.type && (
                      <Badge variant="outline" className="text-xs">
                        {tx.type.replace(/_/g, " ")}
                      </Badge>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {typeof tx.size_bytes === "number" && (
                    <span className="text-xs text-muted-foreground">{(tx.size_bytes / 1024).toFixed(1)} KB</span>
                  )}
                  {isCopied ? (
                    <Check className="h-4 w-4 text-status-healthy" />
                  ) : (
                    <Copy className="h-4 w-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                  )}
                </div>
              </button>
            ))}
          </div>
        ) : displayBlock.transaction_count > 0 ? (
          <p className="text-sm text-muted-foreground text-center py-4">
            Transactions recorded for this block, but the backend does not expose their payloads yet.
          </p>
        ) : (
          <p className="text-sm text-muted-foreground text-center py-4">No transactions in this block</p>
        )}
      </Card>
    </div>
  )
}

interface InfoRowProps {
  label: string
  value: ReactNode
  mono?: boolean
}

function InfoRow({ label, value, mono = false }: InfoRowProps) {
  return (
    <div className="flex items-center justify-between py-2 border-b border-border/20 last:border-b-0">
      <span className="text-muted-foreground">{label}</span>
      <span className={mono ? "font-mono text-sm text-foreground" : "text-foreground"}>{value}</span>
    </div>
  )
}

interface HashRowProps {
  label: string
  value: string
  formatHash: (hash: string, length?: number) => string
  onCopy?: (hash: string) => Promise<boolean>
  isCopied?: boolean
}

function HashRow({ label, value, formatHash, onCopy, isCopied }: HashRowProps) {
  return (
    <div className="flex items-center justify-between py-2 border-b border-border/20">
      <span className="text-muted-foreground">{label}</span>
      <div className="flex items-center gap-2">
        <code className="text-xs font-mono text-foreground bg-card/60 px-2 py-1 rounded">{formatHash(value)}</code>
        {onCopy ? (
          <button
            onClick={() => onCopy(value)}
            className="p-1 hover:bg-accent/50 rounded transition-colors text-muted-foreground hover:text-foreground"
            title={isCopied ? "Copied!" : "Copy hash"}
          >
            {isCopied ? <Check className="h-4 w-4 text-status-healthy" /> : <Copy className="h-4 w-4" />}
          </button>
        ) : null}
      </div>
    </div>
  )
}
