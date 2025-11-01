"use client"

import { Card, CardContent } from "@/components/ui/card"
import { CheckCircle2 } from "lucide-react"
import { DotFilledIcon } from "@radix-ui/react-icons"

interface RoadmapProps {
  detectionTotal?: number
}

export function Roadmap({ detectionTotal }: RoadmapProps) {
  const phases = [
    {
      quarter: "Q4 2024",
      title: "Foundation",
      items: [
        "5-node PBFT consensus",
        `${detectionTotal ? detectionTotal.toLocaleString() : "4.2M"} threats processed`,
        "99.9% uptime",
      ],
      completed: true,
    },
    {
      quarter: "Q1 2025",
      title: "Scale",
      items: ["Multi-region deployment", "Enhanced ML models", "Enterprise API"],
      completed: false,
    },
    {
      quarter: "Q2 2025",
      title: "Ecosystem",
      items: ["Partner integrations", "Advanced analytics", "Custom models"],
      completed: false,
    },
    {
      quarter: "Q3 2025",
      title: "Global",
      items: ["10+ regions", "1M+ tx/sec", "Enterprise SLA"],
      completed: false,
    },
  ]

  return (
    <section className="py-20 px-4">
      <div className="max-w-6xl mx-auto">
        <div className="text-center mb-16">
          <h2 className="text-4xl font-bold text-foreground mb-4">Product Roadmap</h2>
          <p className="text-xl text-muted-foreground">Strategic milestones for 2024-2025</p>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
          {phases.map((phase, idx) => (
            <Card
              key={idx}
              className={`glass-card border ${phase.completed ? "border-emerald-500/30" : "border-border/30"}`}
            >
              <CardContent className="p-6">
                <div className="flex items-center gap-2 mb-4">
                  {phase.completed ? (
                    <CheckCircle2 className="h-5 w-5 text-emerald-400" />
                  ) : (
                    <DotFilledIcon className="h-5 w-5 text-muted-foreground" />
                  )}
                  <span className="text-sm font-semibold text-muted-foreground">{phase.quarter}</span>
                </div>
                <h3 className="text-lg font-bold text-foreground mb-4">{phase.title}</h3>
                <ul className="space-y-2">
                  {phase.items.map((item, i) => (
                    <li key={i} className="text-sm text-muted-foreground flex items-start gap-2">
                      <span className="text-primary mt-1">•</span>
                      <span>{item}</span>
                    </li>
                  ))}
                </ul>
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    </section>
  )
}
