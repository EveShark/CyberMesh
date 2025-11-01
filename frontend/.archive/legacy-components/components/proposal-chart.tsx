"use client"

import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Cell } from "recharts"
import { Card } from "@/components/ui/card"

interface ProposalChartProps {
  proposals: Array<{ block: number; timestamp: number }>
}

export default function ProposalChart({ proposals }: ProposalChartProps) {
  const chartData = proposals.slice(-16).map((p, i) => ({
    block: `#${p.block}`,
    value: Math.floor(Math.random() * 100) + 50,
    timestamp: p.timestamp,
    index: i,
  }))

  const getBarColor = (value: number) => {
    if (value > 80) return "#00d9ff" // Bright cyan for high consensus
    if (value > 60) return "#4dd9ff" // Medium cyan
    return "#80e1ff" // Light cyan for lower consensus
  }

  return (
    <Card className="card-modern p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h3 className="text-lg font-semibold text-foreground">Proposal Series</h3>
          <p className="text-sm text-muted-foreground mt-1">Last 16 blocks</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-3 h-3 rounded-full bg-cyan-500 animate-pulse"></div>
          <span className="text-xs text-muted-foreground">Active</span>
        </div>
      </div>

      <ResponsiveContainer width="100%" height={320}>
        <BarChart data={chartData} margin={{ top: 20, right: 20, left: -10, bottom: 20 }}>
          <CartesianGrid
            strokeDasharray="3 3"
            stroke="rgba(0, 217, 255, 0.08)"
            vertical={false}
            horizontalPoints={[0, 25, 50, 75, 100]}
          />

          <XAxis
            dataKey="block"
            stroke="rgba(148, 163, 184, 0.4)"
            style={{ fontSize: "11px", fontWeight: 500 }}
            tick={{ fill: "rgba(148, 163, 184, 0.6)" }}
            axisLine={{ stroke: "rgba(0, 217, 255, 0.1)" }}
          />

          <YAxis
            stroke="rgba(148, 163, 184, 0.4)"
            style={{ fontSize: "11px" }}
            tick={{ fill: "rgba(148, 163, 184, 0.6)" }}
            axisLine={{ stroke: "rgba(0, 217, 255, 0.1)" }}
            domain={[0, 100]}
          />

          <Tooltip
            contentStyle={{
              backgroundColor: "rgba(10, 14, 26, 0.95)",
              border: "1px solid rgba(0, 217, 255, 0.3)",
              borderRadius: "8px",
              boxShadow: "0 8px 32px rgba(0, 217, 255, 0.1)",
              backdropFilter: "blur(10px)",
            }}
            labelStyle={{ color: "#00d9ff", fontWeight: 600 }}
            cursor={{ fill: "rgba(0, 217, 255, 0.08)" }}
            formatter={(value) => [`${value}%`, "Consensus"]}
          />

          <Bar
            dataKey="value"
            radius={[8, 8, 0, 0]}
            isAnimationActive={true}
            animationDuration={800}
            animationEasing="ease-out"
          >
            {chartData.map((entry, index) => (
              <Cell key={`cell-${index}`} fill={getBarColor(entry.value)} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>

      <div className="mt-6 grid grid-cols-3 gap-4 pt-4 border-t border-border/50">
        <div className="text-center">
          <p className="text-xs text-muted-foreground mb-1">Avg Consensus</p>
          <p className="text-lg font-semibold text-cyan-400">
            {Math.round(chartData.reduce((a, b) => a + b.value, 0) / chartData.length)}%
          </p>
        </div>
        <div className="text-center">
          <p className="text-xs text-muted-foreground mb-1">Peak</p>
          <p className="text-lg font-semibold text-cyan-400">{Math.max(...chartData.map((d) => d.value))}%</p>
        </div>
        <div className="text-center">
          <p className="text-xs text-muted-foreground mb-1">Blocks</p>
          <p className="text-lg font-semibold text-cyan-400">{chartData.length}</p>
        </div>
      </div>
    </Card>
  )
}
