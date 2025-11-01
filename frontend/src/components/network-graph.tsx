"use client"

import { useMemo } from "react"

interface NetworkNode {
  id: string
  role: string
  status: "healthy" | "degraded" | "down"
  peers?: string[]
}

interface NetworkGraphProps {
  nodes: NetworkNode[]
  isLoading?: boolean
  error?: unknown
  updatedAt?: string
}

type Pos = { x: number; y: number }

function getPentagonPositions(width: number, height: number): Record<number, Pos> {
  // Arrange 5 nodes in a pentagon
  const cx = width / 2
  const cy = height / 2
  const r = Math.min(width, height) * 0.36
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
  down: "var(--destructive)", // ensure defined in globals
}

export default function NetworkGraph({ nodes, isLoading, error, updatedAt }: NetworkGraphProps) {
  const width = 560
  const height = 360

  const limitedNodes = nodes.slice(0, 5)
  const idToIndex = useMemo(() => {
    const map = new Map<string, number>()
    limitedNodes.forEach((n, i) => map.set(n.id, i))
    return map
  }, [limitedNodes])

  const positions = useMemo(() => getPentagonPositions(width, height), [width, height])

  const edges: Array<[number, number]> = useMemo(() => {
    const e: Array<[number, number]> = []
    limitedNodes.forEach((n) => {
      n.peers?.forEach((pid) => {
        const a = idToIndex.get(n.id)
        const b = idToIndex.get(pid)
        if (a != null && b != null && a < b) e.push([a, b])
      })
    })
    return e
  }, [limitedNodes, idToIndex])

  return (
    <div className="rounded-xl border bg-card/60 backdrop-blur-md p-4">
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">Network Mesh</h3>
        <span className="text-xs text-muted-foreground">
          {isLoading
            ? "Loading..."
            : error
              ? "Error"
              : `Updated ${new Date(updatedAt ?? Date.now()).toLocaleTimeString()}`}
        </span>
      </div>
      <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label="P2P Mesh Graph">
        {/* Edges */}
        <g>
          {edges.map(([a, b], idx) => (
            <line
              key={idx}
              x1={positions[a].x}
              y1={positions[a].y}
              x2={positions[b].x}
              y2={positions[b].y}
              stroke="var(--border)"
              strokeWidth={1.5}
              opacity={0.7}
            />
          ))}
        </g>
        {/* Nodes */}
        <g>
          {limitedNodes.map((n, i) => (
            <g key={n.id}>
              <circle
                cx={positions[i].x}
                cy={positions[i].y}
                r={20}
                fill="var(--background)"
                stroke={statusColor[n.status] ?? "var(--chart-3)"}
                strokeWidth={3}
              />
              <text
                x={positions[i].x}
                y={positions[i].y + 36}
                textAnchor="middle"
                className="text-xs"
                fill="var(--muted-foreground)"
              >
                {n.role} • {n.status}
              </text>
              <text
                x={positions[i].x}
                y={positions[i].y + 52}
                textAnchor="middle"
                className="text-[10px]"
                fill="var(--muted-foreground)"
              >
                {n.id}
              </text>
            </g>
          ))}
        </g>
      </svg>
    </div>
  )
}
