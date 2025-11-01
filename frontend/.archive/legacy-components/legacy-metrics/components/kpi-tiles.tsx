"use client"

import type React from "react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Cpu, MemoryStick, AlertTriangle, Activity } from "lucide-react"

type TrendDirection = "up" | "down" | "stable"

interface KpiTileProps {
  title: string
  value?: string
  change?: string
  trend?: TrendDirection
  icon: React.ReactNode
  unit?: string
  mutedLabel?: string
}

function getTrendIcon(trend?: TrendDirection) {
  switch (trend) {
    case "up":
      return "↗"
    case "down":
      return "↘"
    default:
      return "→"
  }
}

function getTrendColor(title: string, trend?: TrendDirection) {
  switch (trend) {
    case "up":
      return title === "Error Rate" ? "text-status-critical" : "text-status-healthy"
    case "down":
      return title === "Error Rate" ? "text-status-healthy" : "text-status-critical"
    default:
      return "text-muted-foreground"
  }
}

function formatValue(value?: string, fallback = "--") {
  return value ?? fallback
}

function KpiTile({ title, value, change, trend, icon, unit, mutedLabel }: KpiTileProps) {
  return (
    <Card className="glass-card">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
        <div className="text-muted-foreground">{icon}</div>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold text-foreground">
          {formatValue(value)}
          {unit && <span className="text-sm text-muted-foreground ml-1">{unit}</span>}
        </div>
        {change ? (
          <div className={`flex items-center text-xs mt-1 ${getTrendColor(title, trend)}`}>
            <span className="mr-1">{getTrendIcon(trend)}</span>
            <span>{change}</span>
            <span className="text-muted-foreground ml-1">{mutedLabel ?? "vs previous"}</span>
          </div>
        ) : (
          <div className="text-xs text-muted-foreground mt-1">{mutedLabel ?? "No change data"}</div>
        )}
      </CardContent>
    </Card>
  )
}

export interface KpiTilesProps {
  cpuPercent?: number
  memoryBytes?: number
  requestRatePerSec?: number
  errorRatePercent?: number
}

function formatPercent(value?: number, fractionDigits = 1) {
  if (value === undefined) return undefined
  return value.toFixed(fractionDigits)
}

function formatBytesToGB(value?: number) {
  if (value === undefined) return undefined
  return (value / (1024 * 1024 * 1024)).toFixed(2)
}

function formatNumber(value?: number) {
  if (value === undefined) return undefined
  return value.toLocaleString()
}

export function KpiTiles({ cpuPercent, memoryBytes, requestRatePerSec, errorRatePercent }: KpiTilesProps) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
      <KpiTile
        title="CPU Usage"
        value={formatPercent(cpuPercent)}
        unit={cpuPercent !== undefined ? "%" : undefined}
        icon={<Cpu className="h-4 w-4" />}
        mutedLabel="Process CPU time"
      />
      <KpiTile
        title="Memory Usage"
        value={formatBytesToGB(memoryBytes)}
        unit={memoryBytes !== undefined ? "GB" : undefined}
        icon={<MemoryStick className="h-4 w-4" />}
        mutedLabel="Resident memory"
      />
      <KpiTile
        title="Error Rate"
        value={formatPercent(errorRatePercent, 2)}
        unit={errorRatePercent !== undefined ? "%" : undefined}
        icon={<AlertTriangle className="h-4 w-4" />}
        mutedLabel="Error percentage"
      />
      <KpiTile
        title="Requests/sec"
        value={formatNumber(requestRatePerSec)}
        icon={<Activity className="h-4 w-4" />}
        mutedLabel="Total requests"
      />
    </div>
  )
}
