"use client"

import type React from "react"

import { Shield, Server, Cpu, Activity } from "lucide-react"
import { useMode } from "@/lib/mode-context"

interface HeroKPIStripProps {
  blockHeight?: number
  pendingTransactions?: number
  activeValidators?: number
  totalDetections?: number
  publishedDetections?: number
}

interface KPIMetric {
  label: string
  value: string
  helper?: string
  icon: React.ReactNode
}

export function HeroKPIStrip({
  blockHeight,
  pendingTransactions,
  activeValidators,
  totalDetections,
  publishedDetections,
}: HeroKPIStripProps) {
  const { mode } = useMode()

  const metrics: KPIMetric[] = [
    {
      label: "Latest Block Height",
      value: blockHeight !== undefined ? blockHeight.toLocaleString() : "--",
      helper: "Chain progression",
      icon: <Shield className="h-5 w-5" />,
    },
    {
      label: "Pending Transactions",
      value: pendingTransactions !== undefined ? pendingTransactions.toLocaleString() : "--",
      helper: "Mempool depth",
      icon: <Server className="h-5 w-5" />,
    },
    {
      label: "Active Validators",
      value: activeValidators !== undefined ? `${activeValidators}` : "--",
      helper: "Consensus quorum",
      icon: <Cpu className="h-5 w-5" />,
    },
    {
      label: mode === "investor" ? "Detections Published" : "AI Detections",
      value: publishedDetections !== undefined ? publishedDetections.toLocaleString() : "--",
      helper:
        totalDetections !== undefined && publishedDetections !== undefined
          ? `${publishedDetections}/${totalDetections} published`
          : undefined,
      icon: <Activity className="h-5 w-5" />,
    },
  ]

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
      {metrics.map((metric, idx) => (
        <div
          key={idx}
          className="glass-card border border-border/30 rounded-lg p-6 hover:border-primary/50 transition-all duration-300"
        >
            <div className="flex items-start justify-between mb-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-primary/20 to-accent/20 border border-border/30 text-primary">
                {metric.icon}
              </div>
            </div>
          <p className="text-muted-foreground text-sm mb-1">{metric.label}</p>
          <p className="text-3xl font-bold text-foreground">{metric.value}</p>
          {metric.helper && <p className="text-xs text-muted-foreground mt-1">{metric.helper}</p>}
        </div>
      ))}
    </div>
  )
}
