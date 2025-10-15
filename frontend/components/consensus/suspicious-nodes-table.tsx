"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { AlertTriangle, Shield, Clock } from "lucide-react"

// TODO: integrate GET /consensus/metrics
const mockSuspiciousNodes = [
  {
    nodeId: "node-gw-03",
    suspicionScore: 0.85,
    reason: "Delayed vote responses",
    lastSeen: "2m ago",
    severity: "high",
  },
  {
    nodeId: "node-crdb-02",
    suspicionScore: 0.62,
    reason: "Inconsistent proposals",
    lastSeen: "5m ago",
    severity: "medium",
  },
  {
    nodeId: "node-gw-01",
    suspicionScore: 0.43,
    reason: "Network latency spikes",
    lastSeen: "1m ago",
    severity: "low",
  },
  {
    nodeId: "node-ai-02",
    suspicionScore: 0.71,
    reason: "Byzantine behavior detected",
    lastSeen: "3m ago",
    severity: "high",
  },
]

function getSeverityColor(severity: string) {
  switch (severity) {
    case "high":
      return "bg-red-500/10 text-red-600 border-red-500/20"
    case "medium":
      return "bg-yellow-500/10 text-yellow-600 border-yellow-500/20"
    case "low":
      return "bg-blue-500/10 text-blue-600 border-blue-500/20"
    default:
      return "bg-gray-500/10 text-gray-600 border-gray-500/20"
  }
}

function getSeverityIcon(severity: string) {
  switch (severity) {
    case "high":
      return <AlertTriangle className="h-3 w-3" />
    case "medium":
      return <Shield className="h-3 w-3" />
    case "low":
      return <Clock className="h-3 w-3" />
    default:
      return null
  }
}

export function SuspiciousNodesTable() {
  return (
    <Card className="glass-card border-0">
      <CardHeader>
        <CardTitle className="text-lg font-semibold text-foreground flex items-center gap-2">
          <AlertTriangle className="h-5 w-5 text-amber-500" />
          Suspicious Nodes
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b border-border/50">
                <th className="text-left py-3 px-4 text-sm font-medium text-muted-foreground">Node ID</th>
                <th className="text-left py-3 px-4 text-sm font-medium text-muted-foreground">Suspicion Score</th>
                <th className="text-left py-3 px-4 text-sm font-medium text-muted-foreground">Reason</th>
                <th className="text-left py-3 px-4 text-sm font-medium text-muted-foreground">Last Seen</th>
                <th className="text-left py-3 px-4 text-sm font-medium text-muted-foreground">Severity</th>
              </tr>
            </thead>
            <tbody>
              {mockSuspiciousNodes.map((node, index) => (
                <tr key={node.nodeId} className="border-b border-border/30 hover:bg-muted/20 transition-colors">
                  <td className="py-3 px-4">
                    <div className="font-mono text-sm text-foreground">{node.nodeId}</div>
                  </td>
                  <td className="py-3 px-4">
                    <div className="flex items-center gap-2">
                      <div className="text-sm font-medium text-foreground">
                        {(node.suspicionScore * 100).toFixed(0)}%
                      </div>
                      <div className="w-16 h-2 bg-muted rounded-full overflow-hidden">
                        <div
                          className={`h-full transition-all duration-300 ${
                            node.suspicionScore > 0.7
                              ? "bg-red-500"
                              : node.suspicionScore > 0.5
                                ? "bg-yellow-500"
                                : "bg-blue-500"
                          }`}
                          style={{ width: `${node.suspicionScore * 100}%` }}
                        />
                      </div>
                    </div>
                  </td>
                  <td className="py-3 px-4">
                    <div className="text-sm text-muted-foreground">{node.reason}</div>
                  </td>
                  <td className="py-3 px-4">
                    <div className="text-sm text-muted-foreground">{node.lastSeen}</div>
                  </td>
                  <td className="py-3 px-4">
                    <Badge variant="secondary" className={`${getSeverityColor(node.severity)} flex items-center gap-1`}>
                      {getSeverityIcon(node.severity)}
                      {node.severity}
                    </Badge>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  )
}
