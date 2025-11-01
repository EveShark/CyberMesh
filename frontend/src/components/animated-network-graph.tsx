"use client"

import { useMemo, useState } from "react"
import { motion } from "framer-motion"
import { useHealth } from "./use-health"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Eye, EyeOff } from "lucide-react"

type Pos = { x: number; y: number }

function getPentagonPositions(width: number, height: number): Record<number, Pos> {
  const cx = width / 2
  const cy = height / 2
  const r = Math.min(width, height) * 0.32
  const positions: Record<number, Pos> = {}
  for (let i = 0; i < 5; i++) {
    const angle = ((-90 + i * 72) * Math.PI) / 180
    positions[i] = { x: cx + r * Math.cos(angle), y: cy + r * Math.sin(angle) }
  }
  return positions
}

const statusColor: Record<string, string> = {
  healthy: "var(--chart-2)",
  degraded: "var(--chart-4)",
  down: "var(--destructive)",
}

const statusGlow: Record<string, string> = {
  healthy: "rgba(96, 165, 250, 0.5)",
  degraded: "rgba(239, 68, 68, 0.3)",
  down: "rgba(239, 68, 68, 0.5)",
}

export default function AnimatedNetworkGraph() {
  const { data, isLoading, error } = useHealth()
  const [showLabels, setShowLabels] = useState(true)
  const [hoveredNode, setHoveredNode] = useState<number | null>(null)
  const width = 600
  const height = 420

  const nodes = useMemo(() => data?.nodes?.slice(0, 5) ?? [], [data?.nodes])
  const idToIndex = useMemo(() => {
    const map = new Map<string, number>()
    nodes.forEach((n, i) => map.set(n.id, i))
    return map
  }, [nodes])

  const positions = useMemo(() => getPentagonPositions(width, height), [width, height])

  const edges: Array<[number, number]> = useMemo(() => {
    const e: Array<[number, number]> = []
    nodes.forEach((n) => {
      n.peers?.forEach((pid) => {
        const a = idToIndex.get(n.id)
        const b = idToIndex.get(pid)
        if (a != null && b != null && a < b) e.push([a, b])
      })
    })
    return e
  }, [nodes, idToIndex])

  return (
    <Card className="glass-card overflow-hidden">
      <CardContent className="p-6">
        {/* Header with toggle */}
        <div className="flex items-center justify-between mb-6">
          <div className="text-center flex-1">
            <h3 className="text-xl font-semibold text-foreground">P2P Network Mesh</h3>
            <p className="text-sm text-muted-foreground mt-1">
              {isLoading
                ? "Loading network topology..."
                : error
                  ? "Error loading network"
                  : `${nodes.length} nodes • Updated ${new Date(data?.updatedAt ?? Date.now()).toLocaleTimeString()}`}
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowLabels(!showLabels)}
            className="ml-4"
            title={showLabels ? "Hide labels" : "Show labels"}
          >
            {showLabels ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
          </Button>
        </div>

        {/* Animated SVG */}
        <div className="flex justify-center">
          <svg
            width={width}
            height={height}
            viewBox={`0 0 ${width} ${height}`}
            role="img"
            aria-label="P2P Mesh Graph"
            className="drop-shadow-lg"
          >
            <defs>
              {/* Gradient definitions for nodes */}
              {nodes.map((n, i) => (
                <radialGradient key={`grad-${i}`} id={`nodeGradient-${i}`}>
                  <stop offset="0%" stopColor={statusColor[n.status] ?? "var(--chart-3)"} stopOpacity="0.3" />
                  <stop offset="100%" stopColor={statusColor[n.status] ?? "var(--chart-3)"} stopOpacity="0" />
                </radialGradient>
              ))}
            </defs>

            {/* Animated edges with flow effect */}
            <motion.g>
              {edges.map(([a, b], idx) => (
                <motion.g key={`edge-${idx}`}>
                  {/* Base line */}
                  <line
                    x1={positions[a].x}
                    y1={positions[a].y}
                    x2={positions[b].x}
                    y2={positions[b].y}
                    stroke="var(--border)"
                    strokeWidth={2}
                    opacity={0.3}
                  />
                  {/* Animated flowing line */}
                  <motion.line
                    x1={positions[a].x}
                    y1={positions[a].y}
                    x2={positions[b].x}
                    y2={positions[b].y}
                    stroke="var(--accent)"
                    strokeWidth={2}
                    opacity={0.6}
                    initial={{ pathLength: 0 }}
                    animate={{ pathLength: 1 }}
                    transition={{
                      duration: 2,
                      repeat: Number.POSITIVE_INFINITY,
                      ease: "linear",
                      delay: idx * 0.1,
                    }}
                    strokeDasharray="5,5"
                  />
                </motion.g>
              ))}
            </motion.g>

            {/* Animated nodes */}
            <motion.g>
              {nodes.map((n, i) => (
                <motion.g
                  key={n.id}
                  onMouseEnter={() => setHoveredNode(i)}
                  onMouseLeave={() => setHoveredNode(null)}
                  style={{ cursor: "pointer" }}
                >
                  {/* Glow effect */}
                  <motion.circle
                    cx={positions[i].x}
                    cy={positions[i].y}
                    r={28}
                    fill={statusGlow[n.status] ?? "rgba(96, 165, 250, 0.2)"}
                    animate={{
                      r: hoveredNode === i ? 35 : 28,
                      opacity: hoveredNode === i ? 0.8 : 0.4,
                    }}
                    transition={{ duration: 0.3 }}
                  />

                  {/* Main node circle */}
                  <motion.circle
                    cx={positions[i].x}
                    cy={positions[i].y}
                    r={20}
                    fill="var(--background)"
                    stroke={statusColor[n.status] ?? "var(--chart-3)"}
                    strokeWidth={3}
                    animate={{
                      r: hoveredNode === i ? 24 : 20,
                      filter:
                        hoveredNode === i
                          ? "drop-shadow(0 0 12px var(--accent))"
                          : "drop-shadow(0 0 4px rgba(0,0,0,0.1))",
                    }}
                    transition={{ duration: 0.3 }}
                  />

                  {/* Pulse animation for healthy nodes */}
                  {n.status === "healthy" && (
                    <motion.circle
                      cx={positions[i].x}
                      cy={positions[i].y}
                      r={20}
                      fill="none"
                      stroke={statusColor[n.status]}
                      strokeWidth={2}
                      opacity={0}
                      animate={{
                        r: [20, 32],
                        opacity: [0.8, 0],
                      }}
                      transition={{
                        duration: 2,
                        repeat: Number.POSITIVE_INFINITY,
                        ease: "easeOut",
                      }}
                    />
                  )}

                  {/* Node label - conditionally rendered */}
                  {showLabels && (
                    <motion.g initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: 0.2 }}>
                      <text
                        x={positions[i].x}
                        y={positions[i].y + 42}
                        textAnchor="middle"
                        className="text-xs font-medium"
                        fill="var(--muted-foreground)"
                      >
                        {n.role}
                      </text>
                      <text
                        x={positions[i].x}
                        y={positions[i].y + 58}
                        textAnchor="middle"
                        className="text-[10px]"
                        fill="var(--muted-foreground)"
                        opacity={0.7}
                      >
                        {n.id.slice(0, 8)}...
                      </text>
                    </motion.g>
                  )}
                </motion.g>
              ))}
            </motion.g>
          </svg>
        </div>

        {/* Legend */}
        <div className="mt-8 flex justify-center gap-8 flex-wrap">
          <div className="flex items-center gap-2">
            <div className="w-3 h-3 rounded-full" style={{ backgroundColor: statusColor.healthy }} />
            <span className="text-sm text-muted-foreground">Healthy</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-3 h-3 rounded-full" style={{ backgroundColor: statusColor.degraded }} />
            <span className="text-sm text-muted-foreground">Degraded</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-3 h-3 rounded-full" style={{ backgroundColor: statusColor.down }} />
            <span className="text-sm text-muted-foreground">Down</span>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
