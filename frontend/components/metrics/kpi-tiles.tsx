"use client"

import type React from "react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Cpu, MemoryStick, AlertTriangle, Activity } from "lucide-react"

// TODO: integrate GET /metrics
const mockKpiData = {
  cpu: { value: 68.5, change: "+2.3%", trend: "up" as const },
  memory: { value: 74.2, change: "-1.8%", trend: "down" as const },
  errorRate: { value: 0.12, change: "+0.05%", trend: "up" as const },
  requestsPerSec: { value: 2847, change: "+15.2%", trend: "up" as const },
}

interface KpiTileProps {
  title: string
  value: string
  change: string
  trend: "up" | "down" | "stable"
  icon: React.ReactNode
  unit?: string
}

function KpiTile({ title, value, change, trend, icon, unit }: KpiTileProps) {
  const getTrendColor = () => {
    switch (trend) {
      case "up":
        return title === "Error Rate" ? "status-critical" : "status-healthy"
      case "down":
        return title === "Error Rate" ? "status-healthy" : "status-critical"
      default:
        return "text-muted-foreground"
    }
  }

  const getTrendIcon = () => {
    switch (trend) {
      case "up":
        return "↗"
      case "down":
        return "↘"
      default:
        return "→"
    }
  }

  return (
    <Card className="glass-card">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
        <div className="text-muted-foreground">{icon}</div>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold text-foreground">
          {value}
          {unit && <span className="text-sm text-muted-foreground ml-1">{unit}</span>}
        </div>
        <div className={`flex items-center text-xs mt-1 ${getTrendColor()}`}>
          <span className="mr-1">{getTrendIcon()}</span>
          <span>{change}</span>
          <span className="text-muted-foreground ml-1">vs last hour</span>
        </div>
      </CardContent>
    </Card>
  )
}

export function KpiTiles() {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
      <KpiTile
        title="CPU Usage"
        value={mockKpiData.cpu.value.toString()}
        unit="%"
        change={mockKpiData.cpu.change}
        trend={mockKpiData.cpu.trend}
        icon={<Cpu className="h-4 w-4" />}
      />
      <KpiTile
        title="Memory Usage"
        value={mockKpiData.memory.value.toString()}
        unit="%"
        change={mockKpiData.memory.change}
        trend={mockKpiData.memory.trend}
        icon={<MemoryStick className="h-4 w-4" />}
      />
      <KpiTile
        title="Error Rate"
        value={mockKpiData.errorRate.value.toString()}
        unit="%"
        change={mockKpiData.errorRate.change}
        trend={mockKpiData.errorRate.trend}
        icon={<AlertTriangle className="h-4 w-4" />}
      />
      <KpiTile
        title="Requests/sec"
        value={mockKpiData.requestsPerSec.value.toLocaleString()}
        change={mockKpiData.requestsPerSec.change}
        trend={mockKpiData.requestsPerSec.trend}
        icon={<Activity className="h-4 w-4" />}
      />
    </div>
  )
}
