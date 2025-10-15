"use client"

import type React from "react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, LineChart, Line } from "recharts"
import { Database, Zap, HardDrive } from "lucide-react"

// TODO: integrate GET /storage/health
const mockInfraData = {
  kafka: {
    status: "healthy",
    throughput: "45.2k msg/sec",
    partitions: 24,
    consumers: 8,
    lag: "12ms",
  },
  redis: {
    status: "healthy",
    memory: "2.4GB / 8GB",
    connections: 156,
    hitRate: "98.7%",
    latency: "0.8ms",
  },
  cockroachdb: {
    status: "healthy",
    queries: "1,847 qps",
    connections: 45,
    storage: "127GB / 500GB",
    replication: "3x",
  },
}

const kafkaMetrics = [
  { time: "14:20", messages: 42000, lag: 15 },
  { time: "14:22", messages: 45200, lag: 12 },
  { time: "14:24", messages: 43800, lag: 18 },
  { time: "14:26", messages: 47100, lag: 10 },
  { time: "14:28", messages: 44900, lag: 14 },
  { time: "14:30", messages: 46300, lag: 11 },
]

const redisMetrics = [
  { time: "14:20", hits: 2847, misses: 43 },
  { time: "14:22", hits: 3021, misses: 38 },
  { time: "14:24", hits: 2956, misses: 51 },
  { time: "14:26", hits: 3184, misses: 29 },
  { time: "14:28", hits: 3098, misses: 42 },
  { time: "14:30", hits: 3267, misses: 35 },
]

interface InfraCardProps {
  title: string
  icon: React.ReactNode
  status: "healthy" | "warning" | "critical"
  metrics: Record<string, string | number>
}

function InfraCard({ title, icon, status, metrics }: InfraCardProps) {
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

  return (
    <Card className="glass-card">
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-4">
        <div className="flex items-center gap-2">
          <div className="text-primary">{icon}</div>
          <CardTitle className="text-lg font-semibold text-foreground">{title}</CardTitle>
        </div>
        <Badge variant="outline" className={`${getStatusColor()} text-white border-0`}>
          {status.charAt(0).toUpperCase() + status.slice(1)}
        </Badge>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-4">
          {Object.entries(metrics).map(([key, value]) => (
            <div key={key}>
              <p className="text-sm text-muted-foreground capitalize">{key.replace(/([A-Z])/g, " $1")}</p>
              <p className="text-sm font-semibold text-foreground">{value}</p>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

export function InfraStats() {
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-xl font-semibold text-foreground">Infrastructure Health</h3>
        <Badge variant="outline" className="text-sm">
          Live Monitoring
        </Badge>
      </div>

      {/* Infrastructure Cards */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <InfraCard
          title="Kafka"
          icon={<Zap className="h-5 w-5" />}
          status="healthy"
          metrics={{
            throughput: mockInfraData.kafka.throughput,
            partitions: mockInfraData.kafka.partitions,
            consumers: mockInfraData.kafka.consumers,
            lag: mockInfraData.kafka.lag,
          }}
        />
        <InfraCard
          title="Redis"
          icon={<Database className="h-5 w-5" />}
          status="healthy"
          metrics={{
            memory: mockInfraData.redis.memory,
            connections: mockInfraData.redis.connections,
            hitRate: mockInfraData.redis.hitRate,
            latency: mockInfraData.redis.latency,
          }}
        />
        <InfraCard
          title="CockroachDB"
          icon={<HardDrive className="h-5 w-5" />}
          status="healthy"
          metrics={{
            queries: mockInfraData.cockroachdb.queries,
            connections: mockInfraData.cockroachdb.connections,
            storage: mockInfraData.cockroachdb.storage,
            replication: mockInfraData.cockroachdb.replication,
          }}
        />
      </div>

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
                <BarChart data={kafkaMetrics}>
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
                <LineChart data={redisMetrics}>
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
