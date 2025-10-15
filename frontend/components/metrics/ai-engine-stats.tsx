"use client"

import type React from "react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Brain, Calculator, Shield, TrendingUp } from "lucide-react"

// TODO: integrate GET /metrics
const mockAiEngineData = {
  mlEngine: {
    status: "healthy",
    throughput: "1,247 ops/sec",
    accuracy: "94.8%",
    latency: "12ms",
    change: "+3.2%",
  },
  mathEngine: {
    status: "healthy",
    throughput: "3,891 calc/sec",
    accuracy: "99.9%",
    latency: "2ms",
    change: "+1.8%",
  },
  threatEngine: {
    status: "warning",
    throughput: "856 scans/sec",
    accuracy: "97.2%",
    latency: "45ms",
    change: "-2.1%",
  },
}

interface EngineCardProps {
  title: string
  icon: React.ReactNode
  status: "healthy" | "warning" | "critical"
  throughput: string
  accuracy: string
  latency: string
  change: string
}

function EngineCard({ title, icon, status, throughput, accuracy, latency, change }: EngineCardProps) {
  const getStatusColor = () => {
    switch (status) {
      case "healthy":
        return "bg-status-healthy"
      case "warning":
        return "bg-status-warning"
      case "critical":
        return "bg-status-critical"
      default:
        return "bg-muted"
    }
  }

  const getStatusText = () => {
    switch (status) {
      case "healthy":
        return "Healthy"
      case "warning":
        return "Warning"
      case "critical":
        return "Critical"
      default:
        return "Unknown"
    }
  }

  return (
    <Card className="glass-card">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
        <div className="flex items-center gap-2">
          <div className="text-primary">{icon}</div>
          <CardTitle className="text-lg font-semibold text-foreground">{title}</CardTitle>
        </div>
        <Badge variant="outline" className={`${getStatusColor()} text-white border-0`}>
          {getStatusText()}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <p className="text-sm text-muted-foreground">Throughput</p>
            <p className="text-lg font-semibold text-foreground">{throughput}</p>
          </div>
          <div>
            <p className="text-sm text-muted-foreground">Accuracy</p>
            <p className="text-lg font-semibold text-foreground">{accuracy}</p>
          </div>
        </div>
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-muted-foreground">Avg Latency</p>
            <p className="text-lg font-semibold text-foreground">{latency}</p>
          </div>
          <div className="flex items-center gap-1 text-sm">
            <TrendingUp className="h-3 w-3 text-muted-foreground" />
            <span className="text-muted-foreground">{change}</span>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

export function AiEngineStats() {
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-xl font-semibold text-foreground">AI Engine Performance</h3>
        <Badge variant="outline" className="text-sm">
          Real-time
        </Badge>
      </div>
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <EngineCard
          title="ML Engine"
          icon={<Brain className="h-5 w-5" />}
          status={mockAiEngineData.mlEngine.status as "healthy"}
          throughput={mockAiEngineData.mlEngine.throughput}
          accuracy={mockAiEngineData.mlEngine.accuracy}
          latency={mockAiEngineData.mlEngine.latency}
          change={mockAiEngineData.mlEngine.change}
        />
        <EngineCard
          title="Math Engine"
          icon={<Calculator className="h-5 w-5" />}
          status={mockAiEngineData.mathEngine.status as "healthy"}
          throughput={mockAiEngineData.mathEngine.throughput}
          accuracy={mockAiEngineData.mathEngine.accuracy}
          latency={mockAiEngineData.mathEngine.latency}
          change={mockAiEngineData.mathEngine.change}
        />
        <EngineCard
          title="Threat Engine"
          icon={<Shield className="h-5 w-5" />}
          status={mockAiEngineData.threatEngine.status as "warning"}
          throughput={mockAiEngineData.threatEngine.throughput}
          accuracy={mockAiEngineData.threatEngine.accuracy}
          latency={mockAiEngineData.threatEngine.latency}
          change={mockAiEngineData.threatEngine.change}
        />
      </div>
    </div>
  )
}
