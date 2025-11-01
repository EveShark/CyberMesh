"use client"

import { useEffect, useRef } from "react"
import { Card } from "@/components/ui/card"

interface Node {
  id: string
  name: string
  status: "healthy" | "warning" | "critical"
  latency: number
  uptime: number
}

interface NetworkGraphProps {
  nodes: Node[]
}

export default function NetworkGraph({ nodes }: NetworkGraphProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return

    const ctx = canvas.getContext("2d")
    if (!ctx) return

    canvas.width = canvas.offsetWidth
    canvas.height = canvas.offsetHeight

    const centerX = canvas.width / 2
    const centerY = canvas.height / 2
    const radius = Math.min(canvas.width, canvas.height) / 3

    const bgGradient = ctx.createRadialGradient(
      centerX,
      centerY,
      0,
      centerX,
      centerY,
      Math.max(canvas.width, canvas.height),
    )
    bgGradient.addColorStop(0, "rgba(10, 14, 26, 0.6)")
    bgGradient.addColorStop(1, "rgba(10, 14, 26, 0.9)")
    ctx.fillStyle = bgGradient
    ctx.fillRect(0, 0, canvas.width, canvas.height)

    ctx.strokeStyle = "rgba(0, 217, 255, 0.05)"
    ctx.lineWidth = 1
    const gridSize = 40
    for (let x = 0; x < canvas.width; x += gridSize) {
      ctx.beginPath()
      ctx.moveTo(x, 0)
      ctx.lineTo(x, canvas.height)
      ctx.stroke()
    }
    for (let y = 0; y < canvas.height; y += gridSize) {
      ctx.beginPath()
      ctx.moveTo(0, y)
      ctx.lineTo(canvas.width, y)
      ctx.stroke()
    }

    const nodeCount = Math.min(nodes.length, 5)
    const angleSlice = (Math.PI * 2) / nodeCount

    for (let i = 0; i < nodeCount; i++) {
      const angle1 = i * angleSlice - Math.PI / 2
      const x1 = centerX + radius * Math.cos(angle1)
      const y1 = centerY + radius * Math.sin(angle1)

      const nextI = (i + 1) % nodeCount
      const angle2 = nextI * angleSlice - Math.PI / 2
      const x2 = centerX + radius * Math.cos(angle2)
      const y2 = centerY + radius * Math.sin(angle2)

      // Outer glow layer
      ctx.strokeStyle = "rgba(0, 217, 255, 0.08)"
      ctx.lineWidth = 6
      ctx.beginPath()
      ctx.moveTo(x1, y1)
      ctx.lineTo(x2, y2)
      ctx.stroke()

      // Middle glow layer
      ctx.strokeStyle = "rgba(0, 217, 255, 0.15)"
      ctx.lineWidth = 3
      ctx.beginPath()
      ctx.moveTo(x1, y1)
      ctx.lineTo(x2, y2)
      ctx.stroke()

      // Main line with gradient
      const lineGradient = ctx.createLinearGradient(x1, y1, x2, y2)
      lineGradient.addColorStop(0, "rgba(0, 217, 255, 0.6)")
      lineGradient.addColorStop(0.5, "rgba(0, 217, 255, 0.8)")
      lineGradient.addColorStop(1, "rgba(0, 217, 255, 0.6)")
      ctx.strokeStyle = lineGradient
      ctx.lineWidth = 2
      ctx.beginPath()
      ctx.moveTo(x1, y1)
      ctx.lineTo(x2, y2)
      ctx.stroke()
    }

    for (let i = 0; i < nodeCount; i++) {
      const angle = i * angleSlice - Math.PI / 2
      const x = centerX + radius * Math.cos(angle)
      const y = centerY + radius * Math.sin(angle)

      const node = nodes[i]
      let color = "var(--status-healthy)"
      let glowColor = "var(--status-healthy-glow)"
      let rgbColor = "16, 185, 129"

      if (node.status === "warning") {
        color = "var(--status-warning)"
        glowColor = "var(--status-warning-glow)"
        rgbColor = "245, 158, 11"
      }
      if (node.status === "critical") {
        color = "var(--status-critical)"
        glowColor = "var(--status-critical-glow)"
        rgbColor = "239, 68, 68"
      }

      // Map CSS variables to actual colors
      const colorMap: Record<string, string> = {
        "var(--status-healthy)": "#10b981",
        "var(--status-warning)": "#f59e0b",
        "var(--status-critical)": "#ef4444",
      }
      const actualColor = colorMap[color] || color

      // Outer glow effect
      ctx.fillStyle = `rgba(${rgbColor}, 0.15)`
      ctx.beginPath()
      ctx.arc(x, y, 32, 0, Math.PI * 2)
      ctx.fill()

      // Middle glow effect
      ctx.fillStyle = `rgba(${rgbColor}, 0.08)`
      ctx.beginPath()
      ctx.arc(x, y, 28, 0, Math.PI * 2)
      ctx.fill()

      // Node circle
      ctx.fillStyle = actualColor
      ctx.beginPath()
      ctx.arc(x, y, 12, 0, Math.PI * 2)
      ctx.fill()

      // Node border with glow
      ctx.shadowColor = `rgba(${rgbColor}, 0.5)`
      ctx.shadowBlur = 12
      ctx.strokeStyle = actualColor
      ctx.lineWidth = 2.5
      ctx.beginPath()
      ctx.arc(x, y, 16, 0, Math.PI * 2)
      ctx.stroke()
      ctx.shadowColor = "transparent"

      // Node label
      ctx.fillStyle = "#f1f5f9"
      ctx.font = "bold 13px Geist, sans-serif"
      ctx.textAlign = "center"
      ctx.textBaseline = "middle"
      ctx.fillText(node.name, x, y + 36)

      // Latency info
      ctx.fillStyle = "rgba(148, 163, 184, 0.7)"
      ctx.font = "11px Geist, sans-serif"
      ctx.fillText(`${node.latency}ms`, x, y + 52)
    }

    ctx.shadowColor = "rgba(0, 217, 255, 0.3)"
    ctx.shadowBlur = 20
    ctx.fillStyle = "rgba(0, 217, 255, 0.9)"
    ctx.font = "bold 16px Geist, sans-serif"
    ctx.textAlign = "center"
    ctx.textBaseline = "middle"
    ctx.fillText("PBFT Consensus", centerX, centerY)
    ctx.shadowColor = "transparent"
  }, [nodes])

  return (
    <Card className="card-modern p-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-lg font-semibold text-foreground">Network Topology</h3>
        <div className="flex items-center gap-2 px-3 py-1 rounded-full bg-cyan-500/10 border border-cyan-500/20">
          <div className="w-2 h-2 rounded-full bg-cyan-500 animate-pulse-ring"></div>
          <span className="text-xs text-cyan-400">Live</span>
        </div>
      </div>
      <canvas ref={canvasRef} className="w-full h-96 bg-gradient-to-br from-slate-900/50 to-slate-950/50 rounded-xl" />
      <div className="mt-6 grid grid-cols-3 gap-3">
        <div className="status-badge status-healthy">
          <div className="w-2.5 h-2.5 rounded-full bg-green-500 animate-pulse"></div>
          <span>Healthy</span>
        </div>
        <div className="status-badge status-warning">
          <div className="w-2.5 h-2.5 rounded-full bg-amber-500 animate-pulse"></div>
          <span>Warning</span>
        </div>
        <div className="status-badge status-critical">
          <div className="w-2.5 h-2.5 rounded-full bg-red-500 animate-pulse"></div>
          <span>Critical</span>
        </div>
      </div>
    </Card>
  )
}
