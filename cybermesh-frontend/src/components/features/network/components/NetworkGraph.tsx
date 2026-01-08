import { useEffect, useMemo, useRef, useState } from "react"
import { AlertCircle, Loader2, Crown, Activity, Zap, XCircle } from "lucide-react"

interface NetworkNode {
  id: string
  name: string
  status: string
  latency?: number
  lastSeen?: string | null
  uptime?: number
}

interface NetworkEdge {
  source: string
  target: string
  direction?: string
  status?: string
}

interface NetworkGraphProps {
  nodes: NetworkNode[]
  edges: NetworkEdge[]
  leader?: string
  leaderId?: string
  isLoading?: boolean
  error?: Error | null
}

interface PositionedNode extends NetworkNode {
  x: number
  y: number
  isLeader: boolean
}

function getStatusClass(status?: string) {
  const tone = status?.toLowerCase() ?? ""
  if (tone === "healthy") {
    return { bg: "hsl(var(--status-healthy))", text: "hsl(var(--status-healthy))" }
  }
  if (tone === "warning") {
    return { bg: "hsl(var(--status-warning))", text: "hsl(var(--status-warning))" }
  }
  return { bg: "hsl(var(--status-critical))", text: "hsl(var(--status-critical))" }
}

function formatNodeId(id?: string) {
  if (!id) return "--"
  if (id.length <= 14) return id
  return `${id.slice(0, 6)}…${id.slice(-4)}`
}

export default function NetworkGraph({ nodes, edges, leader, leaderId, isLoading, error }: NetworkGraphProps) {
  const [selectedNode, setSelectedNode] = useState<PositionedNode | null>(null)
  const [hoveredNodeId, setHoveredNodeId] = useState<string | null>(null)
  const [isMobile, setIsMobile] = useState(false)
  const containerRef = useRef<HTMLDivElement | null>(null)
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const [layout, setLayout] = useState<PositionedNode[]>([])

  const validators = useMemo(() => {
    return nodes.slice(0, 5).map((node) => {
      const normalizedLeaderId = leaderId?.toLowerCase().replace("0x", "") || ""
      const normalizedNodeId = node.id?.toLowerCase().replace("0x", "") || ""
      const isLeader = normalizedLeaderId
        ? normalizedNodeId === normalizedLeaderId
        : leader
          ? Boolean(node.name?.includes(leader) || node.id.includes(leader))
          : false
      return { ...node, isLeader }
    })
  }, [nodes, leader, leaderId])

  const allHealthyConnected = useMemo(() => {
    if (validators.length === 0) return false
    const healthy = validators.every((node) => node.status?.toLowerCase() === "healthy")
    if (!healthy) return false
    if (validators.length <= 1) return true
    return edges.length > 0
  }, [validators, edges])

  // Calculate stats for Key Insights - must be before any early returns
  const avgLatency = useMemo(() => {
    const latencies = validators.filter(n => typeof n.latency === 'number').map(n => n.latency!)
    if (latencies.length === 0) return 0
    return Math.round(latencies.reduce((a, b) => a + b, 0) / latencies.length)
  }, [validators])

  const leaderNode = useMemo(() => {
    return validators.find(n => n.isLeader)
  }, [validators])

  useEffect(() => {
    const checkMobile = () => setIsMobile(window.innerWidth < 768)
    checkMobile()
    window.addEventListener("resize", checkMobile)
    return () => window.removeEventListener("resize", checkMobile)
  }, [])

  useEffect(() => {
    const recomputeLayout = () => {
      const container = containerRef.current
      if (!container) return
      const rect = container.getBoundingClientRect()
      const centerX = rect.width / 2
      const centerY = rect.height / 2
      const radius = Math.min(rect.width, rect.height) * 0.45

      const positioned = validators.map((node, index) => {
        const angle = (index * (2 * Math.PI)) / Math.max(validators.length, 1) - Math.PI / 2
        return {
          ...node,
          x: centerX + radius * Math.cos(angle),
          y: centerY + radius * Math.sin(angle) * 0.85,
        }
      })
      setLayout(positioned)
    }

    recomputeLayout()
    window.addEventListener("resize", recomputeLayout)
    return () => window.removeEventListener("resize", recomputeLayout)
  }, [validators])

  useEffect(() => {
    const canvas = canvasRef.current
    const container = containerRef.current
    if (!canvas || !container || isMobile || layout.length === 0) return

    const ctx = canvas.getContext("2d")
    if (!ctx) return

    const draw = () => {
      const rect = container.getBoundingClientRect()
      canvas.width = rect.width
      canvas.height = rect.height
      ctx.clearRect(0, 0, canvas.width, canvas.height)

      const positionMap = new Map(layout.map((node) => [node.id, node]))

      const effectiveEdges = edges.length > 0 ? edges : layout.map((node, index) => ({ source: node.id, target: layout[(index + 1) % layout.length].id }))

      ctx.lineWidth = 2

      effectiveEdges.forEach((edge) => {
        const source = positionMap.get(edge.source)
        const target = positionMap.get(edge.target)
        if (!source || !target) return

        const isCritical = source.status?.toLowerCase() === "critical" || target.status?.toLowerCase() === "critical"
        const isHover = hoveredNodeId === source.id || hoveredNodeId === target.id

        // Connection line colors: hover glow blue, default slate gray
        ctx.strokeStyle = isHover ? "rgba(96,165,250,0.6)" : "rgba(148,163,184,0.35)"
        ctx.setLineDash(isCritical ? [8, 8] : [])
        ctx.beginPath()
        ctx.moveTo(source.x, source.y)
        ctx.lineTo(target.x, target.y)
        ctx.stroke()
      })

      ctx.setLineDash([])
    }

    draw()

    const resizeObserver = new ResizeObserver(draw)
    resizeObserver.observe(container)
    return () => resizeObserver.disconnect()
  }, [edges, hoveredNodeId, isMobile, layout])

  const handleSelect = (node: PositionedNode) => {
    setSelectedNode((current) => (current?.id === node.id ? null : node))
  }

  const renderLoadingState = () => (
    <div className="flex items-center justify-center min-h-[300px] md:min-h-[500px]">
      <div className="text-center space-y-3">
        <Loader2 className="h-8 w-8 animate-spin text-primary mx-auto" />
        <p className="text-sm text-muted-foreground">Loading network topology...</p>
      </div>
    </div>
  )

  if (isLoading) {
    return renderLoadingState()
  }

  if (error) {
    return (
      <div className="flex items-center justify-center min-h-[300px] md:min-h-[500px]">
        <div className="text-center space-y-3">
          <AlertCircle className="h-8 w-8 mx-auto" style={{ color: "hsl(var(--status-critical))" }} />
          <p className="text-sm text-muted-foreground">Failed to load network topology</p>
          <p className="text-xs" style={{ color: "hsl(var(--status-critical))" }}>{error.message}</p>
        </div>
      </div>
    )
  }

  if (validators.length === 0) {
    return (
      <div className="flex items-center justify-center min-h-[300px] md:min-h-[500px]">
        <div className="text-center space-y-3">
          <p className="text-sm text-muted-foreground">No validator nodes found</p>
          <p className="text-xs text-muted-foreground">Waiting for network data...</p>
        </div>
      </div>
    )
  }

  if (isMobile) {
    return (
      <div className="space-y-4">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          {validators.map((node) => {
            const styles = getStatusClass(node.status)
            const isSelected = selectedNode?.id === node.id
            return (
              <button
                key={node.id}
                type="button"
                onClick={() => handleSelect({ ...node, x: 0, y: 0 })}
                className={`text-left rounded-2xl border transition-all duration-300 backdrop-blur-xl p-4 ${isSelected ? "border-primary/50 bg-background/80 shadow-xl" : "border-border/30 bg-background/60 shadow-lg"
                  }`}
              >
                <div className="flex items-center justify-between mb-3">
                  <div className="flex items-center gap-2">
                    {node.isLeader && <Crown className="h-5 w-5" style={{ color: "hsl(var(--status-warning))" }} />}
                    <div>
                      <p className="text-sm font-semibold text-foreground">{node.name}</p>
                      <p className="font-mono text-[11px] text-muted-foreground" title={node.id}>
                        {formatNodeId(node.id)}
                      </p>
                    </div>
                  </div>
                  <span className="h-3 w-3 rounded-full shadow-lg" style={{ backgroundColor: styles.bg }} />
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div>
                    <p className="text-xs text-muted-foreground">Latency</p>
                    <p className="text-lg font-bold" style={{ color: styles.text }}>
                      {typeof node.latency === "number" ? `${Math.round(node.latency)}ms` : "--"}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">Uptime</p>
                    <p className="text-lg font-bold text-foreground">
                      {typeof node.uptime === "number" ? `${node.uptime.toFixed(1)}%` : "--"}
                    </p>
                  </div>
                </div>

                {isSelected && (
                  <div className="mt-3 pt-3 border-t border-border/30 text-xs text-muted-foreground grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <div>
                      <p className="uppercase tracking-wide">Status</p>
                      <p className="font-semibold" style={{ color: styles.text }}>
                        {node.status}
                      </p>
                    </div>
                    <div>
                      <p className="uppercase tracking-wide">Last seen</p>
                      <p>{formatLastSeen(node.lastSeen)}</p>
                    </div>
                  </div>
                )}
              </button>
            )
          })}
        </div>
      </div>
    )
  }


  return (
    <div className="glass-frost rounded-lg p-6 space-y-6">
      {/* Header */}
      <div className="flex items-start gap-3">
        <div className="rounded-lg border border-primary/30 bg-primary/10 p-2">
          <Activity className="h-5 w-5 text-primary" />
        </div>
        <div>
          <h3 className="text-lg font-semibold text-foreground">5-Node Cluster Topology</h3>
          <p className="text-sm text-muted-foreground">Real-time P2P network visualization with PBFT consensus</p>
        </div>
      </div>

      {/* Key Insights */}
      <div>
        <p className="text-sm font-semibold text-foreground mb-3">Key Insights</p>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <p className="text-xs text-muted-foreground">Active Nodes</p>
            <p className="mt-1 text-2xl font-bold" style={{ color: "hsl(var(--status-healthy))" }}>{validators.length}</p>
            <p className="text-xs text-muted-foreground">of 5 total</p>
          </div>
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <p className="text-xs text-muted-foreground">Leader Status</p>
            <div className="mt-1 flex items-center gap-1.5">
              <Crown className="h-4 w-4" style={{ color: "hsl(var(--status-warning))" }} />
              <p className="text-lg font-bold text-foreground">{leaderNode?.name || "--"}</p>
            </div>
            <p className="text-xs text-muted-foreground">current leader</p>
          </div>
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <p className="text-xs text-muted-foreground">Avg Latency</p>
            <p className="mt-1 text-2xl font-bold text-foreground">{avgLatency}ms</p>
            <p className="text-xs text-muted-foreground">network avg</p>
          </div>
          <div className="rounded-xl border border-border/40 bg-background/70 p-4">
            <p className="text-xs text-muted-foreground">Connection Status</p>
            <p className="mt-1 text-lg font-bold" style={{ color: allHealthyConnected ? "hsl(var(--status-healthy))" : "hsl(var(--status-warning))" }}>
              {allHealthyConnected ? "Mesh Connected" : "Partial"}
            </p>
            <p className="text-xs text-muted-foreground">{edges.length} active links</p>
          </div>
        </div>
      </div>

      {/* Network Visualization */}
      <div ref={containerRef} className="relative mx-auto w-full max-w-4xl rounded-xl border border-border/40 bg-background/70" style={{ minHeight: 520 }}>
        <canvas ref={canvasRef} className="absolute inset-0 pointer-events-none" />

        {/* Consensus Hub - Central node using primary color */}
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="relative">
            <div
              className={`flex h-24 w-24 items-center justify-center rounded-full border backdrop-blur-xl transition-colors duration-500 ${allHealthyConnected
                  ? "border-primary/60 bg-primary/15 shadow-[0_0_35px_hsl(var(--primary)/0.35)]"
                  : "border-border/40 bg-primary/10"
                }`}
            >
              <div className="text-center">
                <Zap className="mx-auto h-6 w-6 text-primary" />
                <p className="text-[10px] text-muted-foreground">Consensus</p>
              </div>
            </div>
            <div
              className={`absolute inset-0 rounded-full border transition-colors duration-500 ${allHealthyConnected ? "border-primary/50 animate-pulse" : "border-primary/20"
                }`}
            />
            {allHealthyConnected && (
              <div className="absolute inset-0 rounded-full bg-primary/10 blur-xl" />
            )}
          </div>
        </div>

        {layout.map((node) => {
          const styles = getStatusClass(node.status)
          const isHovered = hoveredNodeId === node.id
          const isSelected = selectedNode?.id === node.id
          return (
            <button
              key={node.id}
              type="button"
              onClick={() => handleSelect(node)}
              onMouseEnter={() => setHoveredNodeId(node.id)}
              onMouseLeave={() => setHoveredNodeId(null)}
              className={`group absolute flex w-40 sm:w-44 -translate-x-1/2 -translate-y-1/2 flex-col rounded-2xl border backdrop-blur-xl transition-transform duration-300 ${isSelected
                  ? "scale-105 border-primary/60 bg-background/90 shadow-2xl"
                  : isHovered
                    ? "scale-105 border-border/40 bg-background/80 shadow-xl"
                    : "scale-100 border-border/20 bg-background/60 shadow-lg"
                }`}
              style={{ left: node.x, top: node.y }}
            >
              {/* Leader crown icon using warning color */}
              {node.isLeader && (
                <span className="absolute -top-6 left-1/2 -translate-x-1/2 animate-bounce">
                  <Crown className="h-5 w-5" style={{ color: "hsl(var(--status-warning))" }} />
                </span>
              )}
              <span
                className="absolute -right-2 -top-2 h-5 w-5 rounded-full shadow-lg"
                style={{ backgroundColor: styles.bg }}
              />
              {isHovered || isSelected ? (
                <span
                  className="absolute inset-0 rounded-2xl opacity-40 blur-xl"
                  style={{ backgroundColor: styles.bg }}
                />
              ) : null}
              <div className="relative flex h-full flex-col justify-between gap-3 p-4 text-left">
                <div>
                  <p className="truncate text-sm font-semibold text-foreground" title={node.name}>
                    {node.name}
                  </p>
                  <p
                    className="font-mono text-[11px] text-muted-foreground"
                    title={node.id}
                  >
                    {formatNodeId(node.id)}
                  </p>
                </div>
                <div className="space-y-2 text-xs">
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Latency</span>
                    <span className="font-mono font-semibold" style={{ color: styles.text }}>
                      {typeof node.latency === "number" ? `${Math.round(node.latency)}ms` : "--"}
                    </span>
                  </div>
                  <div className="flex items-center justify-between text-muted-foreground">
                    <span>Uptime</span>
                    <span className="font-semibold text-foreground">
                      {typeof node.uptime === "number" ? `${node.uptime.toFixed(1)}%` : "--"}
                    </span>
                  </div>
                </div>
              </div>
            </button>
          )
        })}
      </div>

      {selectedNode && (
        <div className="rounded-2xl border border-border/40 bg-background/70 p-6 backdrop-blur-xl">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
                {selectedNode.isLeader && <Crown className="h-5 w-5" style={{ color: "hsl(var(--status-warning))" }} />}
                {selectedNode.name}
              </h3>
              <p className="font-mono text-xs text-muted-foreground" title={selectedNode.id}>
                {formatNodeId(selectedNode.id)}
              </p>
            </div>
            <button
              type="button"
              onClick={() => setSelectedNode(null)}
              className="text-muted-foreground transition-colors hover:text-foreground"
            >
              <XCircle className="h-5 w-5" />
            </button>
          </div>
          <div className="mt-4 grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
            <div className="rounded-xl border border-border/30 bg-background/80 p-4">
              <div className="flex items-center gap-2 text-muted-foreground">
                <Activity className="h-4 w-4" />
                <span>Status</span>
              </div>
              <p
                className="mt-2 text-lg font-semibold"
                style={{ color: getStatusClass(selectedNode.status).text }}
              >
                {selectedNode.status}
              </p>
            </div>
            <div className="rounded-xl border border-border/30 bg-background/80 p-4">
              <div className="flex items-center gap-2 text-muted-foreground">
                <Zap className="h-4 w-4" />
                <span>Latency</span>
              </div>
              <p className="mt-2 text-lg font-semibold text-foreground">
                {typeof selectedNode.latency === "number" ? `${Math.round(selectedNode.latency)}ms` : "--"}
              </p>
            </div>
            <div className="rounded-xl border border-border/30 bg-background/80 p-4">
              <div className="flex items-center gap-2 text-muted-foreground">
                <XCircle className="h-4 w-4" />
                <span>Last Seen</span>
              </div>
              <p className="mt-2 text-lg font-semibold text-foreground">{formatLastSeen(selectedNode.lastSeen)}</p>
            </div>
          </div>
        </div>
      )}

      {/* Legend */}
      <div className="rounded-xl border border-border/40 bg-background/70 p-4">
        <p className="text-sm font-semibold text-foreground mb-4">Legend</p>
        <div className="grid grid-cols-1 gap-4 text-xs text-muted-foreground md:grid-cols-2">
          <div className="space-y-3">
            <p className="uppercase tracking-wide text-xs text-muted-foreground">Node Status</p>
            {[
              { label: "Healthy", color: "hsl(var(--status-healthy))", description: "Node operating normally" },
              { label: "Warning", color: "hsl(var(--status-warning))", description: "Node experiencing issues" },
              { label: "Critical", color: "hsl(var(--status-critical))", description: "Node failing or offline" },
            ].map((status) => (
              <div key={status.label} className="flex items-center gap-3">
                <span className="flex h-4 w-4 items-center justify-center rounded-full" style={{ backgroundColor: status.color }}>
                  <span className="h-2 w-2 rounded-full bg-background" />
                </span>
                <div>
                  <p className="font-medium text-foreground">{status.label}</p>
                  <p className="text-xs text-muted-foreground">{status.description}</p>
                </div>
              </div>
            ))}
          </div>
          <div className="space-y-3">
            <p className="uppercase tracking-wide text-xs text-muted-foreground">Network Elements</p>
            <div className="flex items-center gap-3">
              <Crown className="h-5 w-5" style={{ color: "hsl(var(--status-warning))" }} />
              <div>
                <p className="font-medium text-foreground">Leader Node</p>
                <p className="text-xs text-muted-foreground">Current consensus leader</p>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <span className="h-0.5 w-12 bg-border" />
              <div>
                <p className="font-medium text-foreground">Active Connection</p>
                <p className="text-xs text-muted-foreground">Healthy node-to-node link</p>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <span className="h-0.5 w-12 border-t border-dashed border-border" />
              <div>
                <p className="font-medium text-foreground">Degraded Connection</p>
                <p className="text-xs text-muted-foreground">Link involving critical node</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function formatLastSeen(lastSeen?: string | number | null) {
  if (lastSeen === null || lastSeen === undefined) return "--"

  let value: number | string = lastSeen
  if (typeof value === "string" && /^\d+$/.test(value)) {
    value = Number(value)
  }

  if (typeof value === "number") {
    const millis = value < 1_000_000_000_000 ? value * 1000 : value
    const numericDate = new Date(millis)
    if (!Number.isNaN(numericDate.getTime())) {
      return formatRelativeTime(numericDate)
    }
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return "--"
  return formatRelativeTime(date)
}

function formatRelativeTime(date: Date) {
  const diffMs = Date.now() - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  if (diffSec < 60) return `${diffSec}s ago`
  if (diffMin < 60) return `${diffMin}m ago`
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}
