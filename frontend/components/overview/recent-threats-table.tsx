"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

interface Threat {
  id: string
  time: string
  type: string
  severity: "low" | "medium" | "high" | "critical"
  node: string
  description: string
  status: "active" | "mitigated" | "investigating"
  source: string
}

// Mock recent threats data
const mockThreats: Threat[] = [
  {
    id: "THR-2024-001",
    time: "2m ago",
    type: "Brute Force",
    severity: "high",
    node: "gw-05",
    description: "Multiple failed authentication attempts detected",
    status: "investigating",
    source: "192.168.1.45",
  },
  {
    id: "THR-2024-002",
    time: "15m ago",
    type: "DDoS",
    severity: "critical",
    node: "gw-02",
    description: "Unusual traffic spike from multiple sources",
    status: "mitigated",
    source: "Multiple IPs",
  },
  {
    id: "THR-2024-003",
    time: "1h ago",
    type: "Malware",
    severity: "medium",
    node: "ai-01",
    description: "Suspicious file execution detected",
    status: "mitigated",
    source: "Internal",
  },
  {
    id: "THR-2024-004",
    time: "2h ago",
    type: "Data Exfiltration",
    severity: "high",
    node: "crdb-02",
    description: "Abnormal data access patterns identified",
    status: "investigating",
    source: "10.0.0.23",
  },
  {
    id: "THR-2024-005",
    time: "4h ago",
    type: "Port Scan",
    severity: "low",
    node: "gw-01",
    description: "Network reconnaissance activity detected",
    status: "mitigated",
    source: "203.0.113.42",
  },
  {
    id: "THR-2024-006",
    time: "6h ago",
    type: "SQL Injection",
    severity: "medium",
    node: "gw-03",
    description: "Malicious SQL queries blocked",
    status: "mitigated",
    source: "198.51.100.15",
  },
]

const getSeverityColor = (severity: Threat["severity"]) => {
  switch (severity) {
    case "low":
      return "text-blue-600"
    case "medium":
      return "status-warning"
    case "high":
      return "text-orange-600"
    case "critical":
      return "status-critical"
    default:
      return "text-muted-foreground"
  }
}

const getSeverityBadgeVariant = (severity: Threat["severity"]) => {
  switch (severity) {
    case "low":
      return "outline"
    case "medium":
      return "secondary"
    case "high":
      return "secondary"
    case "critical":
      return "destructive"
    default:
      return "outline"
  }
}

const getStatusColor = (status: Threat["status"]) => {
  switch (status) {
    case "active":
      return "status-critical"
    case "investigating":
      return "status-warning"
    case "mitigated":
      return "status-healthy"
    default:
      return "text-muted-foreground"
  }
}

const getStatusBadgeVariant = (status: Threat["status"]) => {
  switch (status) {
    case "active":
      return "destructive"
    case "investigating":
      return "secondary"
    case "mitigated":
      return "default"
    default:
      return "outline"
  }
}

const getThreatIcon = (type: string) => {
  switch (type.toLowerCase()) {
    case "brute force":
      return "🔓"
    case "ddos":
      return "🌊"
    case "malware":
      return "🦠"
    case "data exfiltration":
      return "📤"
    case "port scan":
      return "🔍"
    case "sql injection":
      return "💉"
    default:
      return "⚠️"
  }
}

export function RecentThreatsTable() {
  // TODO: integrate GET /threats/recent

  const activeThreatCount = mockThreats.filter((t) => t.status === "active").length
  const investigatingCount = mockThreats.filter((t) => t.status === "investigating").length

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold tracking-tight">Recent Threats</h2>
        <div className="flex items-center gap-2">
          {activeThreatCount > 0 && (
            <Badge variant="destructive" className="text-sm">
              {activeThreatCount} active
            </Badge>
          )}
          {investigatingCount > 0 && (
            <Badge variant="secondary" className="text-sm">
              {investigatingCount} investigating
            </Badge>
          )}
          <Button variant="outline" size="sm">
            View All
          </Button>
        </div>
      </div>

      <Card className="glass-card">
        <CardHeader>
          <CardTitle className="text-lg">Threat Activity</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {mockThreats.map((threat, index) => (
              <div
                key={threat.id}
                className={cn(
                  "flex items-center justify-between p-4 rounded-lg border border-border/50 hover:bg-muted/30 transition-colors",
                  index === 0 && threat.status === "investigating" && "bg-yellow-50/50 dark:bg-yellow-950/20",
                  index === 1 && threat.status === "mitigated" && "bg-green-50/50 dark:bg-green-950/20",
                )}
              >
                <div className="flex items-center gap-4 flex-1">
                  <div className="text-2xl">{getThreatIcon(threat.type)}</div>

                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <p className="font-semibold text-sm">{threat.id}</p>
                      <Badge
                        variant={getSeverityBadgeVariant(threat.severity)}
                        className={cn(
                          "text-xs",
                          threat.severity === "critical" && "bg-status-critical text-white",
                          threat.severity === "high" && "bg-orange-600 text-white",
                          threat.severity === "medium" && "bg-status-warning text-white",
                        )}
                      >
                        {threat.severity.toUpperCase()}
                      </Badge>
                      <Badge
                        variant={getStatusBadgeVariant(threat.status)}
                        className={cn(
                          "text-xs",
                          threat.status === "mitigated" && "bg-status-healthy text-white",
                          threat.status === "investigating" && "bg-status-warning text-white",
                          threat.status === "active" && "bg-status-critical text-white",
                        )}
                      >
                        {threat.status}
                      </Badge>
                    </div>

                    <p className="text-sm font-medium">{threat.type}</p>
                    <p className="text-xs text-muted-foreground truncate">{threat.description}</p>
                  </div>
                </div>

                <div className="flex items-center gap-6 text-sm">
                  <div className="text-right">
                    <p className="font-medium">{threat.time}</p>
                    <p className="text-xs text-muted-foreground">Time</p>
                  </div>

                  <div className="text-right">
                    <p className="font-medium font-mono">{threat.node}</p>
                    <p className="text-xs text-muted-foreground">Node</p>
                  </div>

                  <div className="text-right min-w-[100px]">
                    <p className="font-medium font-mono text-xs">{threat.source}</p>
                    <p className="text-xs text-muted-foreground">Source</p>
                  </div>
                </div>
              </div>
            ))}
          </div>

          {mockThreats.length === 0 && (
            <div className="text-center py-8">
              <p className="text-muted-foreground">No recent threats detected</p>
              <p className="text-sm text-muted-foreground mt-1">Your system is secure</p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
