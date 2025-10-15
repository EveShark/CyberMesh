"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

interface ServiceHealth {
  name: string
  status: "healthy" | "warning" | "critical" | "maintenance"
  uptime: string
  responseTime: number
  throughput: string
  errorRate: number
  lastCheck: string
  version: string
  instances: {
    total: number
    healthy: number
    unhealthy: number
  }
}

// Mock service health data
const mockServices: ServiceHealth[] = [
  {
    name: "Gateways",
    status: "warning",
    uptime: "99.8%",
    responseTime: 145,
    throughput: "2.4K req/s",
    errorRate: 0.2,
    lastCheck: "30s ago",
    version: "v2.1.4",
    instances: {
      total: 5,
      healthy: 4,
      unhealthy: 1,
    },
  },
  {
    name: "AI Services",
    status: "healthy",
    uptime: "99.9%",
    responseTime: 89,
    throughput: "1.8K req/s",
    errorRate: 0.05,
    lastCheck: "15s ago",
    version: "v3.2.1",
    instances: {
      total: 2,
      healthy: 2,
      unhealthy: 0,
    },
  },
  {
    name: "Kafka",
    status: "healthy",
    uptime: "99.95%",
    responseTime: 23,
    throughput: "15.2K msg/s",
    errorRate: 0.01,
    lastCheck: "10s ago",
    version: "v2.8.1",
    instances: {
      total: 3,
      healthy: 3,
      unhealthy: 0,
    },
  },
  {
    name: "CockroachDB",
    status: "healthy",
    uptime: "99.99%",
    responseTime: 12,
    throughput: "8.7K ops/s",
    errorRate: 0.001,
    lastCheck: "5s ago",
    version: "v23.1.12",
    instances: {
      total: 3,
      healthy: 3,
      unhealthy: 0,
    },
  },
  {
    name: "Redis",
    status: "healthy",
    uptime: "99.97%",
    responseTime: 3,
    throughput: "12.1K ops/s",
    errorRate: 0.02,
    lastCheck: "20s ago",
    version: "v7.2.3",
    instances: {
      total: 2,
      healthy: 2,
      unhealthy: 0,
    },
  },
]

const getStatusColor = (status: ServiceHealth["status"]) => {
  switch (status) {
    case "healthy":
      return "status-healthy"
    case "warning":
      return "status-warning"
    case "critical":
      return "status-critical"
    case "maintenance":
      return "text-blue-600"
    default:
      return "text-muted-foreground"
  }
}

const getStatusBadgeVariant = (status: ServiceHealth["status"]) => {
  switch (status) {
    case "healthy":
      return "default"
    case "warning":
      return "secondary"
    case "critical":
      return "destructive"
    case "maintenance":
      return "outline"
    default:
      return "outline"
  }
}

const getServiceIcon = (name: string) => {
  switch (name.toLowerCase()) {
    case "gateways":
      return "🌐"
    case "ai services":
      return "🧠"
    case "kafka":
      return "📨"
    case "cockroachdb":
      return "🗄️"
    case "redis":
      return "⚡"
    default:
      return "🔧"
  }
}

const getResponseTimeColor = (responseTime: number) => {
  if (responseTime < 50) return "status-healthy"
  if (responseTime < 150) return "status-warning"
  return "status-critical"
}

const getErrorRateColor = (errorRate: number) => {
  if (errorRate < 0.1) return "status-healthy"
  if (errorRate < 1) return "status-warning"
  return "status-critical"
}

export function ServiceHealthGrid() {
  // TODO: integrate GET /services/health

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Service Health</h2>
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="text-sm">
            {mockServices.filter((s) => s.status === "healthy").length} healthy
          </Badge>
          <Badge variant="secondary" className="text-sm">
            {mockServices.filter((s) => s.status === "warning").length} warnings
          </Badge>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-6">
        {mockServices.map((service) => {
          const healthPercentage = Math.round((service.instances.healthy / service.instances.total) * 100)

          return (
            <Card key={service.name} className="glass-card hover:shadow-lg transition-all duration-200">
              <CardHeader className="pb-4">
                <div className="flex items-center justify-between">
                  <CardTitle className="text-lg font-semibold flex items-center gap-2">
                    <span className="text-xl">{getServiceIcon(service.name)}</span>
                    {service.name}
                  </CardTitle>
                  <Badge
                    variant={getStatusBadgeVariant(service.status)}
                    className={cn(
                      "text-xs",
                      service.status === "healthy" && "bg-status-healthy text-white",
                      service.status === "warning" && "bg-status-warning text-white",
                      service.status === "critical" && "bg-status-critical text-white",
                    )}
                  >
                    {service.status}
                  </Badge>
                </div>
                <p className="text-sm text-muted-foreground">Version {service.version}</p>
              </CardHeader>

              <CardContent className="space-y-4">
                {/* Instance Health */}
                <div className="space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Instances</span>
                    <span className="font-medium">
                      {service.instances.healthy}/{service.instances.total}
                    </span>
                  </div>
                  <div className="w-full bg-muted rounded-full h-2">
                    <div
                      className={cn(
                        "h-2 rounded-full transition-all",
                        healthPercentage === 100
                          ? "bg-status-healthy"
                          : healthPercentage >= 80
                            ? "bg-status-warning"
                            : "bg-status-critical",
                      )}
                      style={{ width: `${healthPercentage}%` }}
                    />
                  </div>
                </div>

                {/* Key Metrics */}
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <p className="text-xs text-muted-foreground">Uptime</p>
                    <p className="font-semibold text-sm">{service.uptime}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">Throughput</p>
                    <p className="font-semibold text-sm">{service.throughput}</p>
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <p className="text-xs text-muted-foreground">Response Time</p>
                    <p className={cn("font-semibold text-sm", getResponseTimeColor(service.responseTime))}>
                      {service.responseTime}ms
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground">Error Rate</p>
                    <p className={cn("font-semibold text-sm", getErrorRateColor(service.errorRate))}>
                      {service.errorRate}%
                    </p>
                  </div>
                </div>

                <div className="pt-3 border-t border-border/50">
                  <div className="flex justify-between text-xs">
                    <span className="text-muted-foreground">Last check</span>
                    <span className={cn("font-medium", getStatusColor(service.status))}>{service.lastCheck}</span>
                  </div>
                </div>
              </CardContent>
            </Card>
          )
        })}
      </div>
    </div>
  )
}
