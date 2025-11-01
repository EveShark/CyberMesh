"use client"

import type React from "react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, LineChart, Line } from "recharts"
import { Database, Zap, HardDrive } from "lucide-react"

type InfraStatus = "healthy" | "warning" | "critical"

interface InfraStatsProps {
  kafkaTopicRate?: number
  redisHitRate?: number
  redisMissRate?: number
  cockroachQueries?: number
  readinessChecks?: Record<string, string>
}

export function InfraStats({ kafkaTopicRate, redisHitRate, redisMissRate, cockroachQueries, readinessChecks }: InfraStatsProps) {
  const services = [
    {
      name: "Kafka",
      icon: <Zap className="h-4 w-4" />,
      status: "healthy" as InfraStatus,
      throughput: kafkaTopicRate ? `${Math.round(kafkaTopicRate).toLocaleString()}/sec` : undefined,
      hits: undefined,
      misses: undefined,
      queries: undefined,
      readiness: readinessChecks?.mempool ?? readinessChecks?.storage ?? "unknown",
    },
    {
      name: "Redis",
      icon: <Database className="h-4 w-4" />,
      status: "healthy" as InfraStatus,
      throughput: undefined,
      hits: redisHitRate ? `${redisHitRate.toFixed(0)}/sec` : undefined,
      misses: redisMissRate ? `${redisMissRate.toFixed(0)}/sec` : undefined,
      queries: undefined,
      readiness: undefined,
    },
    {
      name: "CockroachDB",
      icon: <HardDrive className="h-4 w-4" />,
      status: "healthy" as InfraStatus,
      throughput: undefined,
      hits: undefined,
      misses: undefined,
      queries: cockroachQueries ? `${cockroachQueries.toFixed(0)} qps` : undefined,
      readiness: undefined,
    },
  ]

  const getStatusColor = (status: InfraStatus) => {
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

  return (
    <div className="space-y-6">
      <Card className="glass-card">
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="text-xl font-semibold text-foreground">Infrastructure Health</CardTitle>
            <Badge variant="outline" className="text-xs">Live Monitoring</Badge>
          </div>
        </CardHeader>
        <CardContent className="p-0">
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-border/50">
                  <th className="text-left px-6 py-3 text-xs font-medium text-muted-foreground uppercase">Service</th>
                  <th className="text-center px-4 py-3 text-xs font-medium text-muted-foreground uppercase">Status</th>
                  <th className="text-right px-4 py-3 text-xs font-medium text-muted-foreground uppercase">Throughput</th>
                  <th className="text-right px-4 py-3 text-xs font-medium text-muted-foreground uppercase">Hits/Sec</th>
                  <th className="text-right px-4 py-3 text-xs font-medium text-muted-foreground uppercase">Misses/Sec</th>
                  <th className="text-right px-6 py-3 text-xs font-medium text-muted-foreground uppercase">Queries/Sec</th>
                </tr>
              </thead>
              <tbody>
                {services.map((service, idx) => (
                  <tr key={service.name} className={idx !== services.length - 1 ? "border-b border-border/30" : ""}>
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-2">
                        <div className="text-primary">{service.icon}</div>
                        <span className="font-medium text-foreground">{service.name}</span>
                      </div>
                    </td>
                    <td className="px-4 py-4 text-center">
                      <Badge variant="outline" className={`${getStatusColor(service.status)} text-white border-0 text-xs`}>
                        {service.status.charAt(0).toUpperCase() + service.status.slice(1)}
                      </Badge>
                    </td>
                    <td className="px-4 py-4 text-right font-semibold text-foreground">{service.throughput ?? "--"}</td>
                    <td className="px-4 py-4 text-right font-semibold text-foreground">{service.hits ?? "--"}</td>
                    <td className="px-4 py-4 text-right font-semibold text-foreground">{service.misses ?? "--"}</td>
                    <td className="px-6 py-4 text-right font-semibold text-foreground">{service.queries ?? "--"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      {/* Infrastructure Charts */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <Card className="glass-card">
          <CardHeader>
            <CardTitle className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Zap className="h-5 w-5 text-primary" />
              Kafka Message Throughput
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-64">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={kafkaTopicRate ? [{ time: "Now", messages: Math.round(kafkaTopicRate) }] : []}>
                  <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.3} />
                  <XAxis dataKey="time" stroke="var(--muted-foreground)" fontSize={12} />
                  <YAxis stroke="var(--muted-foreground)" fontSize={12} />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "var(--background)",
                      border: "1px solid var(--border)",
                      borderRadius: "8px",
                      backdropFilter: "blur(8px)",
                    }}
                  />
                  <Bar dataKey="messages" fill="var(--chart-1)" name="Messages/sec" radius={[2, 2, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>

        <Card className="glass-card">
          <CardHeader>
            <CardTitle className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Database className="h-5 w-5 text-primary" />
              Redis Hit/Miss Ratio
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-64">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart
                  data={redisHitRate !== undefined || redisMissRate !== undefined ? [{ time: "Now", hits: redisHitRate ?? 0, misses: redisMissRate ?? 0 }] : []}
                >
                  <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" opacity={0.3} />
                  <XAxis dataKey="time" stroke="var(--muted-foreground)" fontSize={12} />
                  <YAxis stroke="var(--muted-foreground)" fontSize={12} />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "var(--background)",
                      border: "1px solid var(--border)",
                      borderRadius: "8px",
                      backdropFilter: "blur(8px)",
                    }}
                  />
                  <Line
                    type="monotone"
                    dataKey="hits"
                    stroke="var(--chart-2)"
                    strokeWidth={2}
                    dot={{ fill: "var(--chart-2)", strokeWidth: 2, r: 4 }}
                    name="Cache Hits"
                  />
                  <Line
                    type="monotone"
                    dataKey="misses"
                    stroke="var(--chart-4)"
                    strokeWidth={2}
                    dot={{ fill: "var(--chart-4)", strokeWidth: 2, r: 4 }}
                    name="Cache Misses"
                  />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
