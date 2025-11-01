import { Card } from "@/components/ui/card"
import { AlertTriangle, CheckCircle2, AlertCircle } from "lucide-react"

import { resolveDisplayName } from "@/lib/node-alias"

interface SuspiciousNode {
  id: string
  status: string
  uptime: number
  suspicionScore: number
  reason?: string
}

interface SuspiciousNodesTableProps {
  nodes: SuspiciousNode[]
}

export default function SuspiciousNodesTable({ nodes }: SuspiciousNodesTableProps) {
  const getStatusIcon = (status: string) => {
    switch (status) {
      case "healthy":
        return <CheckCircle2 className="w-4 h-4" style={{ color: "var(--status-healthy)" }} />
      case "warning":
        return <AlertCircle className="w-4 h-4" style={{ color: "var(--status-warning)" }} />
      default:
        return <AlertTriangle className="w-4 h-4" style={{ color: "var(--status-critical)" }} />
    }
  }

  return (
    <Card className="glass-card border border-border/40 p-6">
      <h3 className="text-lg font-semibold mb-4 text-foreground">Suspicious Nodes</h3>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border/50">
              <th className="text-left py-4 px-4 text-muted-foreground font-medium">Node ID</th>
              <th className="text-left py-4 px-4 text-muted-foreground font-medium">Status</th>
              <th className="text-left py-4 px-4 text-muted-foreground font-medium">Uptime</th>
              <th className="text-left py-4 px-4 text-muted-foreground font-medium">Suspicion Score</th>
              <th className="text-left py-4 px-4 text-muted-foreground font-medium">Reason</th>
            </tr>
          </thead>
          <tbody>
            {nodes.length === 0 ? (
              <tr className="border-b border-border/30">
                <td colSpan={5} className="py-8 px-4 text-center text-muted-foreground">
                  All validators are healthy. No suspicious behavior detected.
                </td>
              </tr>
            ) : (
              nodes.map((node) => {
                const displayName = resolveDisplayName(node.id, node.id)
                const trimmedId = displayName.length > 0 ? displayName : "unknown"
                const suspicion = Math.round(node.suspicionScore ?? 0)

                return (
                  <tr
                    key={node.id}
                    className="border-b border-border/30 hover:bg-primary/5 transition-colors duration-200"
                  >
                    <td className="py-4 px-4 font-mono text-sm text-foreground">{trimmedId}</td>
                    <td className="py-4 px-4">
                      <div className="flex items-center gap-2">
                        {getStatusIcon(node.status)}
                        <span
                          className="px-3 py-1 rounded-full text-xs font-medium"
                          style={{
                            backgroundColor:
                              node.status === "healthy"
                                ? "color-mix(in oklch, var(--status-healthy) 18%, transparent)"
                                : node.status === "warning"
                                  ? "color-mix(in oklch, var(--status-warning) 18%, transparent)"
                                  : "color-mix(in oklch, var(--status-critical) 18%, transparent)",
                            color:
                              node.status === "healthy"
                                ? "var(--status-healthy)"
                                : node.status === "warning"
                                  ? "var(--status-warning)"
                                  : "var(--status-critical)",
                            border: `1px solid ${
                              node.status === "healthy"
                                ? "color-mix(in oklch, var(--status-healthy) 35%, transparent)"
                                : node.status === "warning"
                                  ? "color-mix(in oklch, var(--status-warning) 35%, transparent)"
                                  : "color-mix(in oklch, var(--status-critical) 35%, transparent)"
                            }`,
                          }}
                        >
                          {node.status}
                        </span>
                      </div>
                    </td>
                    <td className="py-4 px-4 text-foreground font-medium">{node.uptime}%</td>
                    <td className="py-4 px-4">
                      <div className="flex items-center gap-3">
                        <div className="w-20 bg-muted/40 rounded-full h-2 border border-border/40 overflow-hidden">
                          <div
                            className="h-2 rounded-full transition-all duration-300"
                            style={{
                              width: `${suspicion}%`,
                              background: "linear-gradient(90deg, var(--status-critical), color-mix(in oklch, var(--status-critical) 55%, transparent))",
                            }}
                          ></div>
                        </div>
                        <span className="text-foreground font-semibold min-w-fit">{suspicion}%</span>
                      </div>
                    </td>
                    <td className="py-4 px-4 text-muted-foreground">
                      {node.reason ? (
                        <span className="font-mono text-xs break-words text-foreground/80">{node.reason}</span>
                      ) : (
                        <span className="text-foreground/50">—</span>
                      )}
                    </td>
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>
    </Card>
  )
}
