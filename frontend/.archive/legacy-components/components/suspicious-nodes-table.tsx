import { Card } from "@/components/ui/card"
import { AlertTriangle, CheckCircle2, AlertCircle } from "lucide-react"

interface SuspiciousNode {
  id: string
  status: string
  uptime: number
  suspicionScore: number
}

interface SuspiciousNodesTableProps {
  nodes: SuspiciousNode[]
}

export default function SuspiciousNodesTable({ nodes }: SuspiciousNodesTableProps) {
  const getStatusIcon = (status: string) => {
    switch (status) {
      case "healthy":
        return <CheckCircle2 className="w-4 h-4 text-green-500" />
      case "warning":
        return <AlertCircle className="w-4 h-4 text-amber-500" />
      default:
        return <AlertTriangle className="w-4 h-4 text-red-500" />
    }
  }

  return (
    <Card className="bg-card/50 border border-border/50 backdrop-blur-sm p-6 hover:border-primary/30 transition-all duration-300">
      <h3 className="text-lg font-semibold mb-4 text-foreground">Suspicious Nodes</h3>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border/50">
              <th className="text-left py-4 px-4 text-muted-foreground font-medium">Node ID</th>
              <th className="text-left py-4 px-4 text-muted-foreground font-medium">Status</th>
              <th className="text-left py-4 px-4 text-muted-foreground font-medium">Uptime</th>
              <th className="text-left py-4 px-4 text-muted-foreground font-medium">Suspicion Score</th>
            </tr>
          </thead>
          <tbody>
            {nodes.map((node) => (
              <tr key={node.id} className="border-b border-border/30 hover:bg-primary/5 transition-colors duration-200">
                <td className="py-4 px-4 font-mono text-sm text-foreground">{node.id.slice(0, 16)}...</td>
                <td className="py-4 px-4">
                  <div className="flex items-center gap-2">
                    {getStatusIcon(node.status)}
                    <span
                      className={`px-3 py-1 rounded-full text-xs font-medium ${
                        node.status === "healthy"
                          ? "bg-green-500/15 text-green-400 border border-green-500/30"
                          : node.status === "warning"
                            ? "bg-amber-500/15 text-amber-400 border border-amber-500/30"
                            : "bg-red-500/15 text-red-400 border border-red-500/30"
                      }`}
                    >
                      {node.status}
                    </span>
                  </div>
                </td>
                <td className="py-4 px-4 text-foreground font-medium">{node.uptime}%</td>
                <td className="py-4 px-4">
                  <div className="flex items-center gap-3">
                    <div className="w-20 bg-muted/50 rounded-full h-2 border border-border/50 overflow-hidden">
                      <div
                        className="bg-gradient-to-r from-red-500 to-red-600 h-2 rounded-full transition-all duration-300"
                        style={{ width: `${node.suspicionScore}%` }}
                      ></div>
                    </div>
                    <span className="text-foreground font-semibold min-w-fit">{node.suspicionScore}%</span>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  )
}
