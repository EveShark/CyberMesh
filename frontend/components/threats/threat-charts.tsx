"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { PieChart, Pie, Cell, AreaChart, Area, XAxis, YAxis, CartesianGrid } from "recharts"
import { TrendingUp, PieChartIcon } from "lucide-react"

// TODO: integrate GET /threats/stats
const severityData = [
  { name: "Critical", value: 12 },
  { name: "High", value: 28 },
  { name: "Medium", value: 45 },
  { name: "Low", value: 15 },
]

const volumeData = [
  { time: "00:00", threats: 5 },
  { time: "04:00", threats: 8 },
  { time: "08:00", threats: 15 },
  { time: "12:00", threats: 23 },
  { time: "16:00", threats: 18 },
  { time: "20:00", threats: 12 },
  { time: "24:00", threats: 7 },
]

const severityChartConfig = {
  Critical: {
    label: "Critical",
    color: "var(--chart-1)",
  },
  High: {
    label: "High",
    color: "var(--chart-2)",
  },
  Medium: {
    label: "Medium",
    color: "var(--chart-3)",
  },
  Low: {
    label: "Low",
    color: "var(--chart-4)",
  },
}

const volumeChartConfig = {
  threats: {
    label: "Threats",
    color: "var(--chart-1)",
  },
}

export function ThreatCharts() {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
      {/* Severity Distribution */}
      <Card className="glass-card">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-foreground">
            <PieChartIcon className="h-5 w-5 text-primary" />
            Threat Severity Distribution
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ChartContainer config={severityChartConfig} className="h-64">
            <PieChart>
              <Pie
                data={severityData}
                cx="50%"
                cy="50%"
                innerRadius={60}
                outerRadius={100}
                paddingAngle={2}
                dataKey="value"
                nameKey="name"
              >
                {severityData.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={severityChartConfig[entry.name].color} />
                ))}
              </Pie>
              <ChartTooltip content={<ChartTooltipContent />} />
            </PieChart>
          </ChartContainer>
          {/* legend pills */}
          <div className="grid grid-cols-2 gap-2 mt-4">
            {severityData.map((item, index) => (
              <div key={index} className="flex items-center gap-2">
                <div className="w-3 h-3 rounded-full" style={{ backgroundColor: `var(--color-${item.name})` }} />
                <span className="text-sm text-foreground">{item.name}</span>
                <span className="text-sm font-mono ml-auto text-muted-foreground">{item.value}</span>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      {/* Threat Volume Over Time */}
      <Card className="glass-card">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-foreground">
            <TrendingUp className="h-5 w-5 text-primary" />
            Threat Volume (24h)
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ChartContainer config={volumeChartConfig} className="h-64">
            <AreaChart data={volumeData}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="time" />
              <YAxis />
              <ChartTooltip content={<ChartTooltipContent />} />
              <Area
                type="monotone"
                dataKey="threats"
                stroke={volumeChartConfig.threats.color}
                fill={volumeChartConfig.threats.color}
                fillOpacity={0.2}
              />
            </AreaChart>
          </ChartContainer>
        </CardContent>
      </Card>
    </div>
  )
}
