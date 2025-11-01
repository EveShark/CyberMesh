"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Brain, Shield, Zap, TrendingUp } from "lucide-react"

interface WhyCyberMeshProps {
  detectionAccuracy?: number
  latencyMs?: number
  throughput?: number
}

export function WhyCyberMesh({ detectionAccuracy, latencyMs, throughput }: WhyCyberMeshProps) {
  const reasons = [
    {
      icon: Brain,
      title: "Advanced AI Detection",
      description: `Specialized ML ensembles delivering ${(detectionAccuracy ?? 99.0).toFixed(1)}% accuracy across diverse attack surfaces.`,
    },
    {
      icon: Shield,
      title: "Byzantine Fault Tolerant",
      description: "PBFT consensus ensures security even with 1/3 of nodes compromised",
    },
    {
      icon: Zap,
      title: "Ultra-low Latency",
      description: `Real-time response with ${(latencyMs ?? 12).toFixed(1)}ms average detection latency end-to-end.`,
    },
    {
      icon: TrendingUp,
      title: "Proven Scalability",
      description: `Sustains ${(throughput ?? 12450).toLocaleString()} tx/sec with consistent performance under load.`,
    },
  ]

  return (
    <section className="py-20 px-4">
      <div className="max-w-6xl mx-auto">
        <div className="text-center mb-16">
          <h2 className="text-4xl font-bold text-foreground mb-4">Why CyberMesh?</h2>
          <p className="text-xl text-muted-foreground">
            The only platform combining AI-powered detection with Byzantine consensus
          </p>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {reasons.map((reason, idx) => {
            const Icon = reason.icon
            return (
              <Card key={idx} className="glass-card border border-border/30 hover:border-primary/50 transition-all">
                <CardHeader>
                  <div className="flex items-center gap-3">
                    <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-primary/20 to-accent/20 border border-border/30">
                      <Icon className="h-5 w-5 text-primary" />
                    </div>
                    <CardTitle className="text-lg">{reason.title}</CardTitle>
                  </div>
                </CardHeader>
                <CardContent>
                  <p className="text-muted-foreground">{reason.description}</p>
                </CardContent>
              </Card>
            )
          })}
        </div>
      </div>
    </section>
  )
}
