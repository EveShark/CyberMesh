"use client"

import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

// Mock data for recent blocks
const recentBlocks = [
  {
    id: "0x1a2b3c4d",
    timestamp: "2024-01-15 14:32:15",
    proposer: "Node-Alpha-01",
    decision: "Approved",
    status: "healthy" as const,
  },
  {
    id: "0x2b3c4d5e",
    timestamp: "2024-01-15 14:31:45",
    proposer: "Node-Beta-02",
    decision: "Approved",
    status: "healthy" as const,
  },
  {
    id: "0x3c4d5e6f",
    timestamp: "2024-01-15 14:31:12",
    proposer: "Node-Gamma-03",
    decision: "Rejected",
    status: "warning" as const,
  },
  {
    id: "0x4d5e6f7g",
    timestamp: "2024-01-15 14:30:38",
    proposer: "Node-Delta-04",
    decision: "Approved",
    status: "healthy" as const,
  },
  {
    id: "0x5e6f7g8h",
    timestamp: "2024-01-15 14:30:05",
    proposer: "Node-Alpha-01",
    decision: "Timeout",
    status: "critical" as const,
  },
]

export function RecentBlocksTable() {
  return (
    <Card className="glass-card">
      <CardHeader>
        <CardTitle className="text-xl font-semibold text-foreground">Recent Blocks</CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="text-muted-foreground">Block ID</TableHead>
              <TableHead className="text-muted-foreground">Timestamp</TableHead>
              <TableHead className="text-muted-foreground">Proposer</TableHead>
              <TableHead className="text-muted-foreground">Decision</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {recentBlocks.map((block) => (
              <TableRow key={block.id} className="hover:bg-accent/5">
                <TableCell className="font-mono text-sm text-foreground">{block.id}</TableCell>
                <TableCell className="text-muted-foreground">{block.timestamp}</TableCell>
                <TableCell className="text-foreground">{block.proposer}</TableCell>
                <TableCell>
                  <Badge
                    variant={
                      block.status === "healthy" ? "default" : block.status === "warning" ? "secondary" : "destructive"
                    }
                    className={
                      block.status === "healthy"
                        ? "bg-status-healthy text-white"
                        : block.status === "warning"
                          ? "bg-status-warning text-white"
                          : "bg-status-critical text-white"
                    }
                  >
                    {block.decision}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {/* TODO: integrate GET /blockchain/stats */}
      </CardContent>
    </Card>
  )
}
