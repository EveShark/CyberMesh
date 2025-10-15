"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

interface ClusterStatus {
  leader: {
    nodeId: string
    nodeName: string
    status: "active" | "inactive"
    electedAt: string
  }
  term: {
    current: number
    duration: string
    lastChange: string
  }
  phase: {
    current: "stable" | "election" | "recovery" | "maintenance"
    description: string
    startedAt: string
  }
  peers: {
    total: number
    connected: number
    disconnected: number
    lastSync: string
  }
}

// Mock cluster status data
const mockClusterStatus: ClusterStatus = {
  leader: {
    nodeId: "orion-9441",
    nodeName: "Orion",
    status: "active",
    electedAt: "2025-10-01T08:30:00Z",
  },
  term: {
    current: 1247,
    duration: "3d 4h 23m",
    lastChange: "3d ago",
  },
  phase: {
    current: "stable",
    description: "All systems operational",
    startedAt: "2025-10-01T08:30:00Z",
  },
  peers: {
    total: 5,
    connected: 5,
    disconnected: 0,
    lastSync: "2s ago",
  },
}

const getPhaseColor = (phase: ClusterStatus["phase"]["current"]) => {
  switch (phase) {
    case "stable":
      return "status-healthy"
    case "election":
      return "status-warning"
    case "recovery":
      return "status-warning"
    case "maintenance":
      return "text-blue-600"
    default:
      return "text-muted-foreground"
  }
}

const getPhaseBadgeVariant = (phase: ClusterStatus["phase"]["current"]) => {
  switch (phase) {
    case "stable":
      return "default"
    case "election":
      return "secondary"
    case "recovery":
      return "secondary"
    case "maintenance":
      return "outline"
    default:
      return "outline"
  }
}

const getLeaderStatusColor = (status: ClusterStatus["leader"]["status"]) => {
  return status === "active" ? "status-healthy" : "status-critical"
}

export function ClusterStatusTiles() {
  // TODO: integrate GET /leader/status
  // TODO: integrate GET /consensus/status

  const peerHealthPercentage = Math.round((mockClusterStatus.peers.connected / mockClusterStatus.peers.total) * 100)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Cluster Status</h2>
        <Badge
          variant={getPhaseBadgeVariant(mockClusterStatus.phase.current)}
          className={cn(
            "text-sm",
            mockClusterStatus.phase.current === "stable" && "bg-status-healthy text-white",
            mockClusterStatus.phase.current === "election" && "bg-status-warning text-white",
            mockClusterStatus.phase.current === "recovery" && "bg-status-warning text-white",
          )}
        >
          {mockClusterStatus.phase.current.toUpperCase()}
        </Badge>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {/* Leader Status */}
        <Card className="glass-card hover:shadow-lg transition-all duration-200">
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <span className="text-lg">👑</span>
              Leader
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div>
              <p className="font-semibold text-lg">{mockClusterStatus.leader.nodeName}</p>
              <p className="text-xs text-muted-foreground">ID: {mockClusterStatus.leader.nodeId}</p>
            </div>

            <div className="flex items-center gap-2">
              <div
                className={cn(
                  "w-2 h-2 rounded-full",
                  mockClusterStatus.leader.status === "active" ? "bg-status-healthy" : "bg-status-critical",
                )}
              />
              <span className={cn("text-sm font-medium", getLeaderStatusColor(mockClusterStatus.leader.status))}>
                {mockClusterStatus.leader.status.toUpperCase()}
              </span>
            </div>

            <div className="pt-2 border-t border-border/50">
              <div className="flex justify-between text-xs">
                <span className="text-muted-foreground">Elected</span>
                <span className="font-medium">3d ago</span>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Term Status */}
        <Card className="glass-card hover:shadow-lg transition-all duration-200">
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <span className="text-lg">🔄</span>
              Term
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div>
              <p className="font-semibold text-2xl">#{mockClusterStatus.term.current}</p>
              <p className="text-xs text-muted-foreground">Current term</p>
            </div>

            <div className="space-y-1">
              <div className="flex justify-between text-xs">
                <span className="text-muted-foreground">Duration</span>
                <span className="font-medium">{mockClusterStatus.term.duration}</span>
              </div>
              <div className="flex justify-between text-xs">
                <span className="text-muted-foreground">Last change</span>
                <span className="font-medium">{mockClusterStatus.term.lastChange}</span>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Phase Status */}
        <Card className="glass-card hover:shadow-lg transition-all duration-200">
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <span className="text-lg">⚡</span>
              Phase
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div>
              <p className={cn("font-semibold text-lg capitalize", getPhaseColor(mockClusterStatus.phase.current))}>
                {mockClusterStatus.phase.current}
              </p>
              <p className="text-xs text-muted-foreground">{mockClusterStatus.phase.description}</p>
            </div>

            <div className="pt-2 border-t border-border/50">
              <div className="flex justify-between text-xs">
                <span className="text-muted-foreground">Started</span>
                <span className="font-medium">3d ago</span>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Peer Count */}
        <Card className="glass-card hover:shadow-lg transition-all duration-200">
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <span className="text-lg">🌐</span>
              Peers
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div>
              <p className="font-semibold text-2xl">
                {mockClusterStatus.peers.connected}/{mockClusterStatus.peers.total}
              </p>
              <p className="text-xs text-muted-foreground">Connected peers</p>
            </div>

            <div className="space-y-2">
              <div className="flex justify-between text-xs">
                <span className="text-muted-foreground">Health</span>
                <span
                  className={cn(
                    "font-medium",
                    peerHealthPercentage >= 90
                      ? "status-healthy"
                      : peerHealthPercentage >= 70
                        ? "status-warning"
                        : "status-critical",
                  )}
                >
                  {peerHealthPercentage}%
                </span>
              </div>
              <div className="w-full bg-muted rounded-full h-1.5">
                <div
                  className={cn(
                    "h-1.5 rounded-full transition-all",
                    peerHealthPercentage >= 90
                      ? "bg-status-healthy"
                      : peerHealthPercentage >= 70
                        ? "bg-status-warning"
                        : "bg-status-critical",
                  )}
                  style={{ width: `${peerHealthPercentage}%` }}
                />
              </div>
            </div>

            <div className="pt-2 border-t border-border/50">
              <div className="flex justify-between text-xs">
                <span className="text-muted-foreground">Last sync</span>
                <span className="font-medium status-healthy">{mockClusterStatus.peers.lastSync}</span>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
