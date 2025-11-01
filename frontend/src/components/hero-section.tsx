"use client"

import { Button } from "@/components/ui/button"
import { Zap } from "lucide-react"
import { ArrowRightIcon } from "@radix-ui/react-icons"

interface HeroSectionProps {
  detectionAccuracy?: number
  latencyMs?: number
  uptimePercentage?: number
}

export function HeroSection({ detectionAccuracy, latencyMs, uptimePercentage }: HeroSectionProps) {
  const accuracyText = detectionAccuracy ? `${detectionAccuracy.toFixed(1)}%` : "99.2%"
  const latencyText = latencyMs ? `${latencyMs.toFixed(1)}ms` : "12.5ms"
  const uptimeText = uptimePercentage ? `${uptimePercentage.toFixed(2)}%` : "99.99%"

  return (
    <div className="relative min-h-screen flex items-center justify-center overflow-hidden pt-20 pb-12 px-4">
      {/* Background gradient */}
      <div className="absolute inset-0 -z-10">
        <div className="absolute top-0 left-1/4 w-96 h-96 bg-primary/20 rounded-full blur-3xl opacity-20" />
        <div className="absolute bottom-0 right-1/4 w-96 h-96 bg-accent/20 rounded-full blur-3xl opacity-20" />
      </div>

      <div className="max-w-4xl mx-auto text-center space-y-8">
        <div className="inline-flex items-center gap-2 px-4 py-2 rounded-full border border-primary/30 bg-primary/10">
          <Zap className="h-4 w-4 text-primary" />
          <span className="text-sm font-medium text-foreground">AI-Powered Security Infrastructure</span>
        </div>

        <h1 className="text-5xl md:text-6xl font-bold text-foreground leading-tight">
          Enterprise-Grade AI Detection at Scale
        </h1>

        <p className="text-xl text-muted-foreground max-w-2xl mx-auto">
          CyberMesh combines advanced ML models with Byzantine Fault Tolerant consensus to deliver {accuracyText} threat
          detection accuracy with sub-15ms latency.
        </p>

        <div className="flex flex-col sm:flex-row gap-4 justify-center pt-4">
          <Button size="lg" className="gap-2 bg-primary hover:bg-primary/90">
            Schedule Demo
            <ArrowRightIcon className="h-4 w-4" />
          </Button>
          <Button size="lg" variant="outline" className="border-border/30 hover:bg-accent/10 bg-transparent">
            View Metrics
          </Button>
        </div>

        {/* Key Stats */}
        <div className="grid grid-cols-3 gap-4 pt-12">
          <div className="glass-card border border-border/30 rounded-lg p-6">
            <p className="text-3xl font-bold text-primary">{accuracyText}</p>
            <p className="text-sm text-muted-foreground mt-2">Detection Accuracy</p>
          </div>
          <div className="glass-card border border-border/30 rounded-lg p-6">
            <p className="text-3xl font-bold text-primary">{latencyText}</p>
            <p className="text-sm text-muted-foreground mt-2">Avg Latency</p>
          </div>
          <div className="glass-card border border-border/30 rounded-lg p-6">
            <p className="text-3xl font-bold text-primary">{uptimeText}</p>
            <p className="text-sm text-muted-foreground mt-2">System Uptime</p>
          </div>
        </div>
      </div>
    </div>
  )
}
