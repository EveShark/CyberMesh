"use client"

import { useMemo } from "react"

import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"

interface NetworkNode {
  id: string
  role: string
  status: "healthy" | "degraded" | "down"
  throughputBytes?: number
}

interface NetworkSummaryData {
  nodes?: NetworkNode[]
  peerCount?: number
  totalPeers?: number
  consensusRound?: number
  leaderStability?: number
  averageLatencyMs?: number
  updatedAt?: string
}

interface NetworkSummaryProps {
  data?: NetworkSummaryData
}

function formatLatency(ms?: number) {
  if (ms === undefined) return "--"
  return `${ms.toFixed(1)} ms`
}

export function NetworkSummary({ data }: NetworkSummaryProps) {
  const topNodes = useMemo(() => {
    return (data?.nodes ?? [])
      .slice()
      .sort((a, b) => (b.throughputBytes ?? 0) - (a.throughputBytes ?? 0))
      .slice(0, 5)
  }, [data?.nodes])

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 text-sm">
        <div className="rounded-lg border border-border/40 bg-card/40 p-4">
          <p className="text-xs text-muted-foreground">Connected Peers</p>
          <p className="text-xl font-semibold text-foreground">
            {data?.peerCount ?? "--"}
            {data?.totalPeers ? ` / ${data.totalPeers}` : ""}
          </p>
        </div>
        <div className="rounded-lg border border-border/40 bg-card/40 p-4">
          <p className="text-xs text-muted-foreground">Consensus Round</p>
          <p className="text-xl font-semibold text-foreground">{data?.consensusRound ?? "--"}</p>
        </div>
        <div className="rounded-lg border border-border/40 bg-card/40 p-4">
          <p className="text-xs text-muted-foreground">Leader Stability</p>
          <p className="text-xl font-semibold text-foreground">{data?.leaderStability ? `${data.leaderStability.toFixed(0)}%` : "--"}</p>
        </div>
        <div className="rounded-lg border border-border/40 bg-card/40 p-4">
          <p className="text-xs text-muted-foreground">Avg Latency</p>
          <p className="text-xl font-semibold text-foreground">{formatLatency(data?.averageLatencyMs)}</p>
        </div>
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between text-sm font-medium text-foreground">
          <span>Top Throughput Nodes</span>
          <Badge variant="outline" className="text-xs">
            Updated {data?.updatedAt ? new Date(data.updatedAt).toLocaleTimeString() : "--"}
          </Badge>
        </div>

        {topNodes.length ? (
          <div className="space-y-2">
            {topNodes.map((node) => (
              <Card key={node.id} className="border border-border/30 bg-card/40">
                <CardContent className="p-3">
                  <div className="flex items-center justify-between text-sm">
                    <div>
                      <p className="font-semibold text-foreground">{node.role.toUpperCase()}</p>
                      <p className="text-xs text-muted-foreground">{node.id.slice(0, 12)}...</p>
                    </div>
                    <div className="text-right">
                      <Badge variant={node.status === "healthy" ? "default" : "secondary"} className="text-xs">
                        {node.status}
                      </Badge>
                      <p className="text-xs text-muted-foreground mt-1">
                        {(node.throughputBytes ?? 0).toLocaleString()} B/s
                      </p>
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">No network telemetry available.</p>
        )}
      </div>
    </div>
  )
}
