"use client"

import { useMemo } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { Loader2, CheckCircle, RefreshCw, XCircle, Crown, Shield, TrendingUp } from "lucide-react"

interface NetworkNode {
  id: string
  name: string
  status: string
  latency?: number
  lastSeen?: string | null
  uptime?: number
}

interface ValidatorStatusTableProps {
  nodes: NetworkNode[]
  leader?: string | null
  leaderStability?: number
  isLoading?: boolean
}

export default function ValidatorStatusTable({ nodes, leader, leaderStability, isLoading }: ValidatorStatusTableProps) {
  // Filter to only 5 validator nodes and derive roles
  const validators = useMemo(() => {
    const validatorNodes = nodes.filter((n) => {
      const match = n.id.match(/validator-(\d+)/) || n.name?.match(/validator-(\d+)/)
      if (match) {
        const num = parseInt(match[1])
        return num >= 0 && num <= 4
      }
      return false
    })

    // Derive role from node index
    const roles = ["control", "gateway", "storage", "observer", "ingest"]

    return validatorNodes.map((node) => {
      const match = node.id.match(/validator-(\d+)/) || node.name?.match(/validator-(\d+)/)
      const index = match ? parseInt(match[1]) : 0
      const role = roles[index] || "validator"
      const isLeader = leader ? node.id.includes(leader) || node.name?.includes(leader) : false

      return {
        ...node,
        role,
        isLeader,
      }
    })
  }, [nodes, leader])

  // Calculate network health
  const networkHealth = useMemo(() => {
    const activeNodes = validators.filter((v) => {
      const status = v.status?.toLowerCase() || ""
      return status.includes("active") || status.includes("online")
    }).length

    const total = validators.length
    const canLose = Math.floor((total - 1) / 3) // Byzantine fault tolerance: f = (n-1)/3

    return {
      activeNodes,
      total,
      percentage: total > 0 ? ((activeNodes / total) * 100).toFixed(0) : 0,
      canLose,
      riskLevel: activeNodes >= Math.ceil((2 * total + 1) / 3) ? "LOW" : "HIGH",
    }
  }, [validators])

  // Format last seen
  const formatLastSeen = (lastSeen?: string | null) => {
    if (!lastSeen) return "--"
    const date = new Date(lastSeen)
    if (isNaN(date.getTime())) return "--"
    
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffSec = Math.floor(diffMs / 1000)
    const diffMin = Math.floor(diffSec / 60)

    if (diffSec < 60) return `${diffSec}s ago`
    if (diffMin < 60) return `${diffMin}m ago`
    return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  }

  // Get status badge
  const getStatusBadge = (status: string) => {
    const s = status?.toLowerCase() || ""
    if (s.includes("active") || s.includes("online")) {
      return (
        <Badge className="bg-green-500/10 text-green-500 hover:bg-green-500/20 flex items-center gap-1">
          <CheckCircle className="h-3 w-3" /> Active
        </Badge>
      )
    }
    if (s.includes("sync") || s.includes("degraded")) {
      return (
        <Badge className="bg-yellow-500/10 text-yellow-500 hover:bg-yellow-500/20 flex items-center gap-1">
          <RefreshCw className="h-3 w-3 animate-spin" /> Syncing
        </Badge>
      )
    }
    return (
      <Badge className="bg-red-500/10 text-red-500 hover:bg-red-500/20 flex items-center gap-1">
        <XCircle className="h-3 w-3" /> Offline
      </Badge>
    )
  }

  if (isLoading) {
    return (
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle>Validator Status</CardTitle>
          <CardDescription>5-node cluster health</CardDescription>
        </CardHeader>
        <CardContent className="p-6">
          <div className="flex items-center justify-center min-h-[200px]">
            <div className="text-center space-y-3">
              <Loader2 className="h-6 w-6 animate-spin text-primary mx-auto" />
              <p className="text-sm text-muted-foreground">Loading validator status...</p>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  if (validators.length === 0) {
    return (
      <Card className="glass-card border border-border/30">
        <CardHeader>
          <CardTitle>Validator Status</CardTitle>
          <CardDescription>5-node cluster health</CardDescription>
        </CardHeader>
        <CardContent className="p-6">
          <div className="flex items-center justify-center min-h-[200px]">
            <div className="text-center space-y-3">
              <p className="text-sm text-muted-foreground">No validator nodes found</p>
              <p className="text-xs text-muted-foreground">Waiting for network data...</p>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card className="glass-card border border-border/30">
      <CardHeader>
        <CardTitle>Validator Status</CardTitle>
        <CardDescription>5-node cluster health and Byzantine fault tolerance</CardDescription>
      </CardHeader>
      <CardContent className="p-6 space-y-4">
        <div className="rounded-lg border border-border/20 overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/30">
                <TableHead className="font-semibold">Node</TableHead>
                <TableHead className="font-semibold">Status</TableHead>
                <TableHead className="font-semibold">Role</TableHead>
                <TableHead className="font-semibold">Latency</TableHead>
                <TableHead className="font-semibold">Last Seen</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {validators.map((validator) => (
                <TableRow key={validator.id} className="hover:bg-accent/5">
                  <TableCell className="font-medium">
                    {validator.isLeader && <Crown className="h-4 w-4 mr-2 text-amber-500" />}
                    {validator.name || validator.id}
                  </TableCell>
                  <TableCell>{getStatusBadge(validator.status)}</TableCell>
                  <TableCell>
                    <span className="text-sm text-muted-foreground">{validator.role}</span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-sm">
                      {validator.latency !== undefined ? `${validator.latency.toFixed(0)}ms` : "--"}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="text-sm text-muted-foreground">{formatLastSeen(validator.lastSeen)}</span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>

        {/* Network Health Summary */}
        <div className="space-y-3 pt-4 border-t border-border/30">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-sm">
            <div className="rounded-lg border border-border/30 bg-background/60 p-3">
              <p className="text-xs text-muted-foreground mb-1">Network Health</p>
              <p className="text-lg font-semibold text-foreground">
                {networkHealth.activeNodes}/{networkHealth.total} nodes active ({networkHealth.percentage}%)
              </p>
            </div>
            <div className="rounded-lg border border-border/30 bg-background/60 p-3">
              <p className="text-xs text-muted-foreground mb-1">Leader Stability</p>
              <p className="text-lg font-semibold text-foreground">
                {leaderStability !== undefined ? `${leaderStability.toFixed(1)}%` : "--"}
              </p>
            </div>
          </div>

          <div className="text-sm space-y-1">
            <p className="flex items-center gap-2">
              <span className="text-muted-foreground inline-flex items-center gap-1">
                <Shield className="h-4 w-4" /> Byzantine Fault Tolerance:
              </span>
              <span className="font-medium text-foreground">
                {networkHealth.activeNodes}/{networkHealth.total} honest nodes ({networkHealth.percentage}%)
              </span>
              <Badge
                variant="outline"
                className={
                  networkHealth.riskLevel === "LOW"
                    ? "bg-green-500/10 text-green-500"
                    : "bg-red-500/10 text-red-500"
                }
              >
                {networkHealth.riskLevel === "LOW" ? "SAFE" : "AT RISK"}
              </Badge>
            </p>
            <p className="flex items-center gap-2">
              <span className="text-muted-foreground inline-flex items-center gap-1">
                <TrendingUp className="h-4 w-4" /> Consensus Risk:
              </span>
              <span className="font-medium text-foreground">
                {networkHealth.riskLevel} (can lose {networkHealth.canLose} more node
                {networkHealth.canLose !== 1 ? "s" : ""} before consensus fails)
              </span>
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
