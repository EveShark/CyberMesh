"use client"

import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { CheckCircle2, XCircle, AlertCircle } from "lucide-react"

export type ServiceStatus = "healthy" | "warning" | "critical" | "maintenance"

export interface ServiceStatusDescriptor {
  name: string
  status: ServiceStatus
  uptimePercent?: number
  uptimeSeconds?: number
  latencyMs?: number
  lastCheck?: string
  details?: string
}

interface ServiceStatusGridProps {
  services: ServiceStatusDescriptor[]
}

const statusConfig: Record<Exclude<ServiceStatus, "maintenance">, { icon: typeof CheckCircle2; color: string; bg: string; textColor: string }> = {
  healthy: {
    icon: CheckCircle2,
    color: "text-emerald-400",
    bg: "bg-emerald-500/20 border-emerald-500/30",
    textColor: "text-emerald-100",
  },
  warning: {
    icon: AlertCircle,
    color: "text-yellow-400",
    bg: "bg-yellow-500/20 border-yellow-500/30",
    textColor: "text-yellow-100",
  },
  critical: {
    icon: XCircle,
    color: "text-red-400",
    bg: "bg-red-500/20 border-red-500/30",
    textColor: "text-red-100",
  },
}

function getStatusPresentation(status: ServiceStatus) {
  if (status === "maintenance") {
    return {
      icon: AlertCircle,
      color: "text-blue-400",
      bg: "bg-blue-500/20 border-blue-500/30",
      textColor: "text-blue-100",
      label: "Maintenance",
    }
  }

  const config = statusConfig[status]
  return {
    icon: config.icon,
    color: config.color,
    bg: config.bg,
    textColor: config.textColor,
    label: status.charAt(0).toUpperCase() + status.slice(1),
  }
}

export function ServiceStatusGrid({ services }: ServiceStatusGridProps) {
  if (!services.length) {
    return (
      <div className="rounded-lg border border-dashed border-border/40 p-6 text-center text-sm text-muted-foreground">
        No service status data available.
      </div>
    )
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      {services.map((service) => {
        const presentation = getStatusPresentation(service.status)
        const Icon = presentation.icon

        const uptimeDisplay = (() => {
          if (typeof service.uptimePercent === "number" && Number.isFinite(service.uptimePercent)) {
            return `${service.uptimePercent.toFixed(2)}%`
          }
          if (typeof service.uptimeSeconds === "number" && Number.isFinite(service.uptimeSeconds)) {
            const totalSeconds = Math.max(0, Math.floor(service.uptimeSeconds))
            const days = Math.floor(totalSeconds / 86400)
            const hours = Math.floor((totalSeconds % 86400) / 3600)
            const minutes = Math.floor((totalSeconds % 3600) / 60)

            if (days > 0) return `${days}d ${hours}h`
            if (hours > 0) return `${hours}h ${minutes}m`
            return `${minutes}m`
          }
          return "--"
        })()

        const latencyDisplay =
          typeof service.latencyMs === "number" && Number.isFinite(service.latencyMs)
            ? `${service.latencyMs.toFixed(1)}ms`
            : "--"

        return (
          <Card key={service.name} className="glass-card border border-border/30 hover:border-primary/50 transition-all">
            <CardContent className="p-6 space-y-4">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-3">
                  <Icon className={`h-5 w-5 ${presentation.color}`} />
                  <div>
                    <p className="font-semibold text-foreground">{service.name}</p>
                    <p className="text-xs text-muted-foreground">
                      {service.lastCheck ? `Last check ${service.lastCheck}` : "No recent check recorded"}
                    </p>
                  </div>
                </div>
                <Badge className={`${presentation.bg} border ${presentation.textColor} text-xs font-semibold`}>
                  {presentation.label}
                </Badge>
              </div>

              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Uptime</p>
                  <p className="text-lg font-bold text-foreground">{uptimeDisplay}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Latency</p>
                  <p className="text-lg font-bold text-foreground">{latencyDisplay}</p>
                </div>
              </div>

              {service.details ? <p className="text-xs text-muted-foreground leading-relaxed">{service.details}</p> : null}
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}
