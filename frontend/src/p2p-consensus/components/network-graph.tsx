"use client"

import { useEffect, useRef, useState } from "react"
import { AlertCircle, Loader2, Crown } from "lucide-react"

interface NetworkNode {
  id: string
  name: string
  status: string
  latency?: number
  lastSeen?: string | null
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
  isLoading?: boolean
  error?: Error | null
}

export default function NetworkGraph({ nodes, edges, leader, isLoading, error }: NetworkGraphProps) {
  const svgRef = useRef<SVGSVGElement>(null)
  const [dimensions, setDimensions] = useState({ width: 800, height: 500 })

  // Resize handler
  useEffect(() => {
    const handleResize = () => {
      if (svgRef.current) {
        const parent = svgRef.current.parentElement
        if (parent) {
          setDimensions({
            width: parent.clientWidth,
            height: Math.max(400, Math.min(600, parent.clientWidth * 0.6)),
          })
        }
      }
    }

    handleResize()
    window.addEventListener("resize", handleResize)
    return () => window.removeEventListener("resize", handleResize)
  }, [])

  // Filter to only 5 validator nodes
  const validators = nodes.filter((n) => {
    const match = n.id.match(/validator-(\d+)/) || n.name?.match(/validator-(\d+)/)
    if (match) {
      const num = parseInt(match[1])
      return num >= 0 && num <= 4
    }
    return false
  })

  // Derive role from node index
  const deriveRole = (node: NetworkNode): string => {
    const match = node.id.match(/validator-(\d+)/) || node.name?.match(/validator-(\d+)/)
    if (match) {
      const roles = ["control", "gateway", "storage", "observer", "ingest"]
      const index = parseInt(match[1])
      return roles[index] || "validator"
    }
    return "validator"
  }

  // Layout: Pentagon formation for 5 nodes
  const calculateLayout = () => {
    const centerX = dimensions.width / 2
    const centerY = dimensions.height / 2
    const radius = Math.min(dimensions.width, dimensions.height) * 0.3

    return validators.map((node, index) => {
      const angle = (index * 2 * Math.PI) / 5 - Math.PI / 2 // Start from top
      return {
        ...node,
        x: centerX + radius * Math.cos(angle),
        y: centerY + radius * Math.sin(angle),
        role: deriveRole(node),
        isLeader: leader ? node.id.includes(leader) || node.name?.includes(leader) : false,
      }
    })
  }

  const positionedNodes = calculateLayout()

  // Get status color
  const getStatusColor = (status: string) => {
    const s = status?.toLowerCase() || ""
    if (s.includes("active") || s.includes("online")) return "#10b981" // green
    if (s.includes("sync") || s.includes("degraded")) return "#f59e0b" // yellow
    return "#ef4444" // red
  }

  // Loading state
  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="text-center space-y-3">
          <Loader2 className="h-8 w-8 animate-spin text-primary mx-auto" />
          <p className="text-sm text-muted-foreground">Loading network topology...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="text-center space-y-3">
          <AlertCircle className="h-8 w-8 text-red-500 mx-auto" />
          <p className="text-sm text-muted-foreground">Failed to load network topology</p>
          <p className="text-xs text-red-500">{error.message}</p>
        </div>
      </div>
    )
  }

  // Empty state
  if (positionedNodes.length === 0) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="text-center space-y-3">
          <p className="text-sm text-muted-foreground">No validator nodes found</p>
          <p className="text-xs text-muted-foreground">Waiting for network data...</p>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <svg
        ref={svgRef}
        width={dimensions.width}
        height={dimensions.height}
        className="border border-border/20 rounded-lg bg-background/40"
      >
        {/* Draw edges */}
        <g>
          {edges.map((edge, idx) => {
            const sourceNode = positionedNodes.find((n) => n.id === edge.source || edge.source.includes(n.id))
            const targetNode = positionedNodes.find((n) => n.id === edge.target || edge.target.includes(n.id))

            if (sourceNode && targetNode) {
              return (
                <line
                  key={`edge-${idx}`}
                  x1={sourceNode.x}
                  y1={sourceNode.y}
                  x2={targetNode.x}
                  y2={targetNode.y}
                  stroke="hsl(var(--border))"
                  strokeWidth="2"
                  strokeOpacity="0.4"
                  strokeDasharray={sourceNode.status === "offline" || targetNode.status === "offline" ? "5,5" : "none"}
                />
              )
            }
            return null
          })}
        </g>

        {/* Draw nodes */}
        <g>
          {positionedNodes.map((node) => {
            const color = getStatusColor(node.status)
            return (
              <g key={node.id}>
                {/* Outer glow for leader */}
                {node.isLeader && (
                  <circle cx={node.x} cy={node.y} r="30" fill={color} opacity="0.2" />
                )}

                {/* Node circle */}
                <circle
                  cx={node.x}
                  cy={node.y}
                  r="20"
                  fill={color}
                  stroke="hsl(var(--border))"
                  strokeWidth="2"
                  opacity="0.9"
                />

                {/* Leader crown */}
                {node.isLeader && (
                  <foreignObject x={node.x - 10} y={node.y - 40} width="20" height="20">
                    <Crown className="h-5 w-5 text-amber-500" />
                  </foreignObject>
                )}

                {/* Node label */}
                <text
                  x={node.x}
                  y={node.y + 40}
                  textAnchor="middle"
                  fill="hsl(var(--foreground))"
                  fontSize="12"
                  fontWeight="500"
                >
                  {node.name || node.id}
                </text>

                {/* Role label */}
                <text
                  x={node.x}
                  y={node.y + 55}
                  textAnchor="middle"
                  fill="hsl(var(--muted-foreground))"
                  fontSize="10"
                >
                  ({node.role})
                </text>
              </g>
            )
          })}
        </g>
      </svg>

      {/* Legend */}
      <div className="flex items-center justify-center gap-6 text-xs">
        <div className="flex items-center gap-2">
          <div className="h-3 w-3 rounded-full bg-[#10b981]"></div>
          <span className="text-muted-foreground">Active</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="h-3 w-3 rounded-full bg-[#f59e0b]"></div>
          <span className="text-muted-foreground">Syncing</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="h-3 w-3 rounded-full bg-[#ef4444]"></div>
          <span className="text-muted-foreground">Offline</span>
        </div>
        <div className="flex items-center gap-2">
          <Crown className="h-4 w-4 text-amber-500" />
          <span className="text-muted-foreground">Current Leader</span>
        </div>
      </div>
    </div>
  )
}
