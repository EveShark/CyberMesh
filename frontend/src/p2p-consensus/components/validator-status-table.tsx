"use client"

import { useMemo, useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Loader2, Crown, Shield, TrendingUp, Activity, Clock, Zap } from "lucide-react"

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
  leaderId?: string | null
  leaderStability?: number
  isLoading?: boolean
}

function colorValue(colorVar: string, alpha?: number) {
  if (alpha === undefined) {
    return `var(${colorVar})`
  }

  const clamped = Math.min(1, Math.max(0, alpha))
  const percent = (clamped * 100).toFixed(1).replace(/\.0$/, "")
  return `color-mix(in srgb, var(${colorVar}) ${percent}%, transparent)`
}

function statusPalette(status?: string) {
  const tone = status?.toLowerCase() ?? ""
  if (tone === "healthy") return { colorVar: "--status-healthy", label: "Healthy" }
  if (tone === "warning") return { colorVar: "--status-warning", label: "Warning" }
  if (tone === "unknown") return { colorVar: "--status-warning", label: "Unknown" }
  if (tone === "offline") return { colorVar: "--status-critical", label: "Offline" }
  return { colorVar: "--status-critical", label: "Critical" }
}

function formatNodeId(value?: string | null) {
  if (!value) return "--"
  if (value.length <= 14) return value
  return `${value.slice(0, 6)}…${value.slice(-4)}`
}

export default function ValidatorStatusTable({ nodes, leader, leaderId, leaderStability, isLoading }: ValidatorStatusTableProps) {
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)

  const validators = useMemo(() => {
    return nodes.slice(0, 5).map((node) => ({
      ...node,
      role: "validator",
      isLeader: leaderId ? node.id === leaderId : leader ? Boolean(node.name?.includes(leader) || node.id.includes(leader)) : false,
    }))
  }, [nodes, leader, leaderId])

  const networkHealth = useMemo(() => {
    const activeNodes = validators.filter((v) => {
      const status = v.status?.toLowerCase() || ""
      return status === "healthy" || status === "warning"
    }).length

    const total = validators.length
    const canLose = Math.floor((total - 1) / 3)
    const riskLow = activeNodes >= Math.ceil((2 * total + 1) / 3)
    const percentage = total > 0 ? (activeNodes / total) * 100 : 0

    return {
      activeNodes,
      total,
      percentage,
      canLose,
      riskLevel: riskLow ? "LOW" : "HIGH",
      statusLabel: riskLow ? "SAFE" : "AT RISK",
    }
  }, [validators])

  const formatLastSeen = (lastSeen?: string | number | null) => {
    if (lastSeen === null || lastSeen === undefined) return "--"

    let raw: number | string = lastSeen
    if (typeof raw === "string" && /^\d+$/.test(raw)) {
      raw = Number(raw)
    }

    if (typeof raw === "number") {
      const millis = raw < 1_000_000_000_000 ? raw * 1000 : raw
      const numericDate = new Date(millis)
      if (!Number.isNaN(numericDate.getTime())) {
        return formatRelativeTime(numericDate)
      }
    }

    const date = new Date(raw)
    if (Number.isNaN(date.getTime())) return "--"
    return formatRelativeTime(date)
  }

  const formatRelativeTime = (date: Date) => {
    const diffMs = Date.now() - date.getTime()
    const diffSec = Math.floor(diffMs / 1000)
    const diffMin = Math.floor(diffSec / 60)

    if (diffSec < 60) return `${diffSec}s ago`
    if (diffMin < 60) return `${diffMin}m ago`
    return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
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
      <CardContent className="space-y-6 p-6">
        <div className="grid gap-6 lg:grid-cols-[2fr,1fr]">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {validators.map((validator) => {
              const palette = statusPalette(validator.status)
              const isSelected = selectedNodeId === validator.id
              const latencyLabel =
                typeof validator.latency === "number" ? `${Math.round(validator.latency)}ms` : "--"
              const borderColor = isSelected ? colorValue(palette.colorVar, 0.45) : undefined
              return (
                <button
                  key={validator.id}
                  type="button"
                  onClick={() => setSelectedNodeId(isSelected ? null : validator.id)}
                  className={`relative flex h-full flex-col gap-4 rounded-2xl border border-border/25 backdrop-blur-xl p-5 text-left transition-all duration-300 ${
                    isSelected ? "bg-background/90 shadow-2xl" : "bg-background/70 shadow-lg hover:border-border/50"
                  }`}
                  style={{ borderColor }}
                >
                  <span
                    className="absolute -right-2 -top-2 h-5 w-5 rounded-full shadow-lg"
                    style={{ backgroundColor: colorValue(palette.colorVar) }}
                  />
                  {isSelected && (
                    <span
                      className="pointer-events-none absolute inset-0 rounded-2xl border"
                      style={{
                        borderColor: colorValue(palette.colorVar, 0.5),
                        boxShadow: `0 0 0 1px ${colorValue(palette.colorVar, 0.35)}`,
                      }}
                    />
                  )}
                  <div className="relative flex items-start justify-between gap-3">
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <p className="text-sm font-semibold text-foreground">{validator.name || validator.id}</p>
                        {validator.isLeader && <Crown className="h-4 w-4 text-[color:var(--status-warning)]" />}
                      </div>
                      <p
                        className="font-mono text-[11px]"
                        style={{ color: colorValue("--muted-foreground", 0.8) }}
                        title={validator.id}
                      >
                        {formatNodeId(validator.id)}
                      </p>
                    </div>
                    <Badge
                      variant="outline"
                      className="rounded-full px-3 py-1 text-xs font-medium"
                      style={{
                        borderColor: colorValue(palette.colorVar, 0.35),
                        background: `linear-gradient(135deg, ${colorValue(palette.colorVar, 0.18)}, ${colorValue(palette.colorVar, 0.05)})`,
                        color: colorValue(palette.colorVar),
                      }}
                    >
                      {palette.label}
                    </Badge>
                  </div>

                  <div className="grid grid-cols-2 gap-3 text-xs">
                    <div>
                      <p className="text-muted-foreground">Latency</p>
                      <p className="font-mono text-sm font-semibold" style={{ color: colorValue(palette.colorVar) }}>
                        {latencyLabel}
                      </p>
                    </div>
                    <div>
                      <p className="text-muted-foreground">Uptime</p>
                      <p className="text-sm font-semibold text-foreground">
                        {typeof validator.uptime === "number" ? `${validator.uptime.toFixed(1)}%` : "--"}
                      </p>
                    </div>
                    <div>
                      <p className="text-muted-foreground">Last seen</p>
                      <p className="text-sm text-foreground">{formatLastSeen(validator.lastSeen)}</p>
                    </div>
                    <div>
                      <p className="text-muted-foreground">Role</p>
                      <p className="text-sm text-foreground capitalize">{validator.role}</p>
                    </div>
                  </div>

                  {isSelected && (
                    <div className="rounded-xl border border-border/30 bg-background/75 p-3 text-xs">
                      <div className="flex items-center gap-2 text-muted-foreground">
                        <Activity className="h-4 w-4" />
                        <span>Node insights</span>
                      </div>
                      <div className="mt-2 grid grid-cols-2 gap-3">
                        <div>
                          <p className="text-muted-foreground">Status</p>
                          <p className="font-semibold" style={{ color: colorValue(palette.colorVar) }}>
                            {palette.label}
                          </p>
                        </div>
                        <div>
                          <p className="text-muted-foreground">Selection</p>
                          <p className="font-semibold text-foreground">{validator.isLeader ? "Leader" : "Validator"}</p>
                        </div>
                        <div>
                          <p className="text-muted-foreground">Latency trend</p>
                          <p className="font-semibold text-foreground">Stable</p>
                        </div>
                        <div>
                          <p className="text-muted-foreground">Last seen</p>
                          <p className="font-semibold text-foreground">{formatLastSeen(validator.lastSeen)}</p>
                        </div>
                      </div>
                    </div>
                  )}
                </button>
              )
            })}
          </div>

          <aside className="space-y-4">
            <div className="rounded-2xl border border-border/40 bg-background/70 p-5">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">Network health</p>
                  <p className="mt-1 text-xl font-semibold text-foreground">
                    {networkHealth.activeNodes}/{networkHealth.total} active nodes
                  </p>
                  <p className="text-xs" style={{ color: colorValue("--muted-foreground", 0.8) }}>
                    {networkHealth.percentage.toFixed(0)}% operational capacity
                  </p>
                </div>
                <Shield className="h-6 w-6 text-primary" />
              </div>
              <div className="mt-4 h-2 w-full overflow-hidden rounded-full bg-border/60">
                <div
                  className="h-full rounded-full bg-primary"
                  style={{ width: `${Math.min(100, networkHealth.percentage)}%` }}
                />
              </div>
            </div>

            <div className="rounded-2xl border border-border/40 bg-background/70 p-5 space-y-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">Leader stability</p>
                  <p className="text-xl font-semibold text-foreground">
                    {leaderStability !== undefined ? `${leaderStability.toFixed(1)}%` : "--"}
                  </p>
                </div>
                <Clock className="h-5 w-5 text-muted-foreground" />
              </div>
              <p className="text-xs text-muted-foreground">
                Stability reflects leader rotation cadence and consensus uptime.
              </p>
            </div>

            <div className="rounded-2xl border border-border/40 bg-background/70 p-5 space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <TrendingUp className="h-4 w-4" />
                  <span className="text-xs uppercase tracking-wide">Consensus risk</span>
                </div>
                <Badge
                  style={{
                    background: `linear-gradient(135deg, ${colorValue(networkHealth.riskLevel === "LOW" ? "--status-healthy" : "--status-critical", 0.18)}, ${colorValue(networkHealth.riskLevel === "LOW" ? "--status-healthy" : "--status-critical", 0.05)})`,
                    borderColor: colorValue(networkHealth.riskLevel === "LOW" ? "--status-healthy" : "--status-critical", 0.35),
                    color: colorValue(networkHealth.riskLevel === "LOW" ? "--status-healthy" : "--status-critical"),
                  }}
                >
                  {networkHealth.statusLabel}
                </Badge>
              </div>
              <p className="text-sm font-semibold text-foreground">
                {networkHealth.riskLevel} risk – can lose {networkHealth.canLose} more node
                {networkHealth.canLose !== 1 ? "s" : ""} before quorum is compromised.
              </p>
              <p className="text-xs text-muted-foreground">
                Healthy nodes maintain safety margin for PBFT consensus. Critical nodes reduce available fault tolerance.
              </p>
            </div>
          </aside>
        </div>

        {selectedNodeId && (
          <div className="rounded-2xl border border-border/40 bg-background/70 p-5 backdrop-blur-xl">
            {(() => {
              const node = validators.find((v) => v.id === selectedNodeId)
              if (!node) return null
              const palette = statusPalette(node.status)
              return (
                <div className="flex flex-col gap-4">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                      <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
                        {node.isLeader && <Crown className="h-5 w-5 text-[color:var(--status-warning)]" />}
                        {node.name || node.id}
                      </h3>
                      <p className="font-mono text-xs text-muted-foreground" title={node.id}>
                        {formatNodeId(node.id)}
                      </p>
                    </div>
                    <Badge
                      variant="outline"
                      style={{
                        borderColor: colorValue(palette.colorVar, 0.3),
                        background: `linear-gradient(135deg, ${colorValue(palette.colorVar, 0.18)}, ${colorValue(palette.colorVar, 0.05)})`,
                        color: colorValue(palette.colorVar),
                      }}
                    >
                      {palette.label}
                    </Badge>
                  </div>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-3 text-sm">
                    <InfoTile label="Latency" value={typeof node.latency === "number" ? `${Math.round(node.latency)}ms` : "--"} />
                    <InfoTile label="Uptime" value={typeof node.uptime === "number" ? `${node.uptime.toFixed(1)}%` : "--"} />
                    <InfoTile label="Last Seen" value={formatLastSeen(node.lastSeen)} />
                  </div>
                  <div className="rounded-xl border border-border/30 bg-background/80 p-4 text-xs text-muted-foreground">
                    <div className="flex items-center gap-2 text-foreground">
                      <Zap className="h-4 w-4" />
                      <span>Validator role insights</span>
                    </div>
                    <p className="mt-2">
                      {node.isLeader
                        ? "Currently serving as consensus leader. Monitoring timing offsets and quorum acknowledgements."
                        : "Participating as validating replica. Tracking commit acknowledgements and checkpoint intervals."}
                    </p>
                  </div>
                </div>
              )
            })()}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

interface InfoTileProps {
  label: string
  value: string
}

function InfoTile({ label, value }: InfoTileProps) {
  return (
    <div className="rounded-xl border border-border/30 bg-background/70 p-4">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 text-base font-semibold text-foreground">{value}</p>
    </div>
  )
}
