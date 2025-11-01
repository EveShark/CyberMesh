import { Card } from "@/components/ui/card"
import { TrendingUp } from "lucide-react"

interface MetricsBarProps {
  connectedPeers: number
  avgLatency: number
  consensusRound: number
  leaderStability: number
}

export default function MetricsBar({ connectedPeers, avgLatency, consensusRound, leaderStability }: MetricsBarProps) {
  const metrics = [
    { label: "Connected Peers", value: connectedPeers, unit: "", icon: "👥", color: "from-primary to-accent" },
    { label: "Avg Latency", value: avgLatency, unit: "ms", icon: "⚡", color: "from-accent to-primary" },
    { label: "Consensus Round", value: consensusRound, unit: "", icon: "🔄", color: "from-primary to-secondary" },
    { label: "Leader Stability", value: leaderStability, unit: "%", icon: "✓", color: "from-secondary to-accent" },
  ]

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 w-full">
      {metrics.map((metric, idx) => (
        <Card
          key={idx}
          className="bg-card/50 border border-border/50 backdrop-blur-sm p-5 hover:border-primary/50 transition-all duration-300 hover:shadow-lg hover:shadow-primary/10 group"
        >
          <div className="flex items-start justify-between mb-3">
            <div className="text-sm font-medium text-muted-foreground">{metric.label}</div>
            <div className="text-lg opacity-60 group-hover:opacity-100 transition-opacity">{metric.icon}</div>
          </div>
          <div className="flex items-baseline gap-1">
            <div className="text-3xl font-bold bg-gradient-to-r from-primary to-accent bg-clip-text text-transparent">
              {metric.value}
            </div>
            {metric.unit && <span className="text-sm text-muted-foreground">{metric.unit}</span>}
          </div>
          <div className="mt-3 flex items-center gap-1 text-xs text-primary/70">
            <TrendingUp className="w-3 h-3" />
            <span>Live</span>
          </div>
        </Card>
      ))}
    </div>
  )
}
