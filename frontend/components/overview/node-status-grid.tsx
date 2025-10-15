"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

interface NodeStatus {
  id: string
  role: "Control" | "Gateway" | "Storage" | "Observer" | "Ingest"
  status: "healthy" | "warning" | "critical"
  name: string
  uptime: string
  cpu: number
  memory: number
  lastSeen: string
  port: number
}

// Mock data for the CyberMesh 5-node topology: 1 Control, 1 Gateway, 1 Storage, 1 Observer, 1 Ingest
const mockNodes: NodeStatus[] = [
  {
    id: "orion",
    role: "Control",
    status: "healthy",
    name: "Orion",
    uptime: "3d 4h",
    cpu: 42,
    memory: 61,
    lastSeen: "1s ago",
    port: 9441,
  },
  {
    id: "lyra",
    role: "Gateway",
    status: "healthy",
    name: "Lyra",
    uptime: "3d 3h",
    cpu: 36,
    memory: 49,
    lastSeen: "2s ago",
    port: 9442,
  },
  {
    id: "draco",
    role: "Storage",
    status: "healthy",
    name: "Draco",
    uptime: "3d 2h",
    cpu: 51,
    memory: 72,
    lastSeen: "3s ago",
    port: 9443,
  },
  {
    id: "cygnus",
    role: "Observer",
    status: "warning",
    name: "Cygnus",
    uptime: "2d 22h",
    cpu: 68,
    memory: 64,
    lastSeen: "4s ago",
    port: 9444,
  },
  {
    id: "vela",
    role: "Ingest",
    status: "healthy",
    name: "Vela",
    uptime: "2d 18h",
    cpu: 29,
    memory: 40,
    lastSeen: "1s ago",
    port: 9445,
  },
]

const getStatusColor = (status: NodeStatus["status"]) => {
  switch (status) {
    case "healthy":
      return "status-healthy"
    case "warning":
      return "status-warning"
    case "critical":
      return "status-critical"
    default:
      return "text-muted-foreground"
  }
}

const getStatusBadgeVariant = (status: NodeStatus["status"]) => {
  switch (status) {
    case "healthy":
      return "default"
    case "warning":
      return "secondary"
    case "critical":
      return "destructive"
    default:
      return "outline"
  }
}

const getRoleIcon = (role: NodeStatus["role"]) => {
  switch (role) {
    case "Control":
      return "👑"
    case "Gateway":
      return "🌐"
    case "Storage":
      return "💾"
    case "Observer":
      return "👁️"
    case "Ingest":
      return "📥"
    default:
      return "🖥️"
  }
}

export function NodeStatusGrid() {
  // TODO: integrate GET /nodes/health

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Node Status</h2>
        <Badge variant="outline" className="text-sm">
          {mockNodes.length} nodes total
        </Badge>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
        {mockNodes.map((node) => (
          <Card key={node.id} className="glass-card hover:shadow-lg transition-all duration-200">
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <CardTitle className="text-sm font-medium flex items-center gap-2">
                  <span className="text-lg">{getRoleIcon(node.role)}</span>
                  {node.role}
                </CardTitle>
                <Badge
                  variant={getStatusBadgeVariant(node.status)}
                  className={cn(
                    "text-xs",
                    node.status === "healthy" && "bg-status-healthy text-white",
                    node.status === "warning" && "bg-status-warning text-white",
                    node.status === "critical" && "bg-status-critical text-white",
                  )}
                >
                  {node.status}
                </Badge>
              </div>
            </CardHeader>
            <CardContent className="space-y-3">
              <div>
                <p className="font-medium text-sm">{node.name}</p>
                <p className="text-xs text-muted-foreground">
                  ID: {node.id} • Port {node.port}
                </p>
              </div>

              <div className="space-y-2">
                <div className="flex justify-between text-xs">
                  <span className="text-muted-foreground">CPU</span>
                  <span
                    className={cn(
                      "font-medium",
                      node.cpu > 80 ? "text-destructive" : node.cpu > 60 ? "text-yellow-600" : "text-foreground",
                    )}
                  >
                    {node.cpu}%
                  </span>
                </div>
                <div className="w-full bg-muted rounded-full h-1.5">
                  <div
                    className={cn(
                      "h-1.5 rounded-full transition-all",
                      node.cpu > 80 ? "bg-destructive" : node.cpu > 60 ? "bg-yellow-500" : "bg-primary",
                    )}
                    style={{ width: `${node.cpu}%` }}
                  />
                </div>
              </div>

              <div className="space-y-2">
                <div className="flex justify-between text-xs">
                  <span className="text-muted-foreground">Memory</span>
                  <span
                    className={cn(
                      "font-medium",
                      node.memory > 80 ? "text-destructive" : node.memory > 60 ? "text-yellow-600" : "text-foreground",
                    )}
                  >
                    {node.memory}%
                  </span>
                </div>
                <div className="w-full bg-muted rounded-full h-1.5">
                  <div
                    className={cn(
                      "h-1.5 rounded-full transition-all",
                      node.memory > 80 ? "bg-destructive" : node.memory > 60 ? "bg-yellow-500" : "bg-primary",
                    )}
                    style={{ width: `${node.memory}%` }}
                  />
                </div>
              </div>

              <div className="pt-2 border-t border-border/50">
                <div className="flex justify-between text-xs">
                  <span className="text-muted-foreground">Uptime</span>
                  <span className="font-medium">{node.uptime}</span>
                </div>
                <div className="flex justify-between text-xs mt-1">
                  <span className="text-muted-foreground">Last seen</span>
                  <span className={cn("font-medium", getStatusColor(node.status))}>{node.lastSeen}</span>
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
