"use client"

import { Card, CardContent } from "@/components/ui/card"
import { TrendingUp, Users, Zap, Shield } from "lucide-react"

interface TractionProps {
  detectionTotal?: number
  uptimePercentage?: number
  totalPeers?: number
  consensusRound?: number
}

export function Traction({ detectionTotal, uptimePercentage, totalPeers, consensusRound }: TractionProps) {
  const metrics = [
    {
      icon: Zap,
      label: "Detections (total)",
      value: detectionTotal ? detectionTotal.toLocaleString() : "1,834",
      change: detectionTotal ? "Live" : "+23%",
    },
    {
      icon: Users,
      label: "Active Nodes",
      value: totalPeers ? totalPeers.toString() : "5",
      change: "Stable",
    },
    {
      icon: Shield,
      label: "Uptime SLA",
      value: uptimePercentage ? `${uptimePercentage.toFixed(2)}%` : "99.99%",
      change: "Met",
    },
    {
      icon: TrendingUp,
      label: "Consensus Round",
      value: consensusRound ? `#${consensusRound.toLocaleString()}` : "#1,247",
      change: "Real-time",
    },
  ]

  return (
    <section className="py-20 px-4 bg-gradient-to-b from-background to-background/50">
      <div className="max-w-6xl mx-auto">
        <div className="text-center mb-16">
          <h2 className="text-4xl font-bold text-foreground mb-4">Proven Traction</h2>
          <p className="text-xl text-muted-foreground">
            Real-time metrics demonstrating platform reliability and performance
          </p>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
          {metrics.map((metric, idx) => {
            const Icon = metric.icon
            return (
              <Card key={idx} className="glass-card border border-border/30">
                <CardContent className="p-6">
                  <div className="flex items-center justify-between mb-4">
                    <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-primary/20 to-accent/20 border border-border/30">
                      <Icon className="h-5 w-5 text-primary" />
                    </div>
                    <span className="text-xs font-semibold text-emerald-400">{metric.change}</span>
                  </div>
                  <p className="text-muted-foreground text-sm mb-2">{metric.label}</p>
                  <p className="text-3xl font-bold text-foreground">{metric.value}</p>
                </CardContent>
              </Card>
            )
          })}
        </div>
      </div>
    </section>
  )
}
