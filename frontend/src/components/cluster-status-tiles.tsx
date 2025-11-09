"use client"

import { Crown, RefreshCcw, Globe2, Zap } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

interface ClusterStatusTilesProps {
  stats?: {
    chain?: {
      height: number
      state_version: number
    }
    consensus?: {
      view: number
      round: number
      validator_count: number
      quorum_size: number
      leader?: string
      leader_id?: string
      current_leader?: string
      current_leader_alias?: string
    }
    network?: {
      peer_count: number
      inbound_peers: number
      outbound_peers: number
      bytes_received: number
      bytes_sent: number
      avg_latency_ms?: number
    }
  }
  readiness?: {
    ready: boolean
    phase?: string
    checks: Record<string, string>
    details?: Record<string, unknown>
  }
  isLoading?: boolean
  error?: Error | null
}

function formatIdentifier(id?: string) {
  if (!id) return "Unknown"
  if (id.length <= 12) return id
  return `${id.slice(0, 8)}…${id.slice(-6)}`
}

function getLeaderDisplay(consensus?: {
  leader?: string
  leader_id?: string
  current_leader?: string
  current_leader_alias?: string
}) {
  if (!consensus) {
    return { primary: "Unknown" }
  }

  const alias = consensus.leader ?? consensus.current_leader_alias
  const leaderId = consensus.leader_id ?? consensus.current_leader

  if (alias) {
    return {
      primary: alias,
      secondary: leaderId ? formatIdentifier(leaderId) : undefined,
    }
  }

  return {
    primary: formatIdentifier(leaderId),
  }
}

function extractNumber(details: Record<string, unknown> | undefined, key: string): number | undefined {
  if (!details) return undefined
  const value = details[key]
  if (typeof value === "number") return value
  if (typeof value === "string") {
    const parsed = Number.parseInt(value, 10)
    return Number.isNaN(parsed) ? undefined : parsed
  }
  return undefined
}

export function ClusterStatusTiles({ stats, readiness, isLoading, error }: ClusterStatusTilesProps) {
  if (isLoading) {
    return <div className="text-center py-8">Loading cluster status…</div>
  }

  if (error) {
    return <div className="text-center py-8 text-destructive">Failed to load cluster status</div>
  }

  const consensus = stats?.consensus
  const network = stats?.network

  const leaderDisplay = getLeaderDisplay(consensus)
  const phase = readiness?.phase ?? (readiness?.ready ? "active" : "initializing")

  const connectedPeers = extractNumber(readiness?.details, "p2p_connected_peers") ?? network?.peer_count ?? 0
  const requiredPeers = extractNumber(readiness?.details, "p2p_required_peers") ?? consensus?.quorum_size ?? 0
  const peerHealthPercentage = requiredPeers > 0 ? Math.round((connectedPeers / requiredPeers) * 100) : 0
  const clampedPeerHealthPercentage = Math.min(100, Math.max(peerHealthPercentage, 0))

  const readyChecks = readiness?.checks ?? {}

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Cluster Status</h2>
        <Badge
          variant={readiness?.ready ? "default" : "secondary"}
          className={cn(
            "text-sm capitalize",
            readiness?.ready ? "bg-status-healthy text-white" : "bg-status-warning text-white",
          )}
        >
          {phase}
        </Badge>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <Card className="glass-card hover:shadow-lg transition-all duration-200">
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <Crown className="h-4 w-4 text-primary" />
              Consensus Leader
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div>
              <p className="font-semibold text-lg">{leaderDisplay.primary}</p>
              <p className="text-xs text-muted-foreground">
                {leaderDisplay.secondary ? `${leaderDisplay.secondary} • ` : ""}Quorum size {consensus?.quorum_size ?? "--"}
              </p>
            </div>
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">Validators</span>
              <span className="font-medium">{consensus?.validator_count ?? "--"}</span>
            </div>
          </CardContent>
        </Card>

        <Card className="glass-card hover:shadow-lg transition-all duration-200">
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <RefreshCcw className="h-4 w-4 text-primary" />
              Consensus Progress
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">View</span>
              <span className="font-semibold">{consensus?.view ?? "--"}</span>
            </div>
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">Round</span>
              <span className="font-semibold">{consensus?.round ?? "--"}</span>
            </div>
            <div className="flex justify-between text-xs">
              <span className="text-muted-foreground">Chain Height</span>
              <span className="font-semibold">{stats?.chain?.height ?? "--"}</span>
            </div>
          </CardContent>
        </Card>

        <Card className="glass-card hover:shadow-lg transition-all duration-200">
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <Zap className="h-4 w-4 text-primary" />
              Service Readiness
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-xs">
            {Object.entries(readyChecks).map(([name, status]) => (
              <div key={name} className="flex justify-between">
                <span className="text-muted-foreground capitalize">{name.replace(/_/g, " ")}</span>
                <span
                  className={cn(
                    "font-semibold capitalize",
                    status === "ok" || status === "genesis" ? "text-emerald-500" : "text-amber-500",
                  )}
                >
                  {status}
                </span>
              </div>
            ))}
            {Object.keys(readyChecks).length === 0 && (
              <p className="text-muted-foreground">No readiness data available</p>
            )}
          </CardContent>
        </Card>

        <Card className="glass-card hover:shadow-lg transition-all duration-200">
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <Globe2 className="h-4 w-4 text-primary" />
              Network Peers
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div>
              <p className="font-semibold text-2xl">
                {connectedPeers}/{requiredPeers || "?"}
              </p>
              <p className="text-xs text-muted-foreground">Connected / Required peers</p>
            </div>

            <div className="space-y-2">
              <div className="flex justify-between text-xs">
                <span className="text-muted-foreground">Health</span>
                <span
                  className={cn(
                    "font-medium",
                    clampedPeerHealthPercentage >= 90
                      ? "status-healthy"
                      : clampedPeerHealthPercentage >= 70
                        ? "status-warning"
                        : "status-critical",
                  )}
                >
                  {clampedPeerHealthPercentage}%
                </span>
              </div>
              <div className="w-full bg-muted rounded-full h-1.5">
                <div
                  className={cn(
                    "h-1.5 rounded-full transition-all",
                    clampedPeerHealthPercentage >= 90
                      ? "bg-status-healthy"
                      : clampedPeerHealthPercentage >= 70
                        ? "bg-status-warning"
                        : "bg-status-critical",
                  )}
                  style={{ width: `${clampedPeerHealthPercentage}%` }}
                />
              </div>
            </div>

            <div className="flex justify-between text-xs border-t border-border/50 pt-2">
              <span className="text-muted-foreground">Latency</span>
              <span className="font-medium">{network?.avg_latency_ms ?? "--"} ms</span>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
