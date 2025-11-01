"use client"

import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

import type { BlockSummary } from "@/lib/api"

interface RecentBlocksTableProps {
  blocks: BlockSummary[]
  isLoading?: boolean
  error?: unknown
}

export function RecentBlocksTable({ blocks, isLoading, error }: RecentBlocksTableProps) {
  return (
    <Card className="glass-card">
      <CardHeader>
        <CardTitle className="text-xl font-semibold text-foreground">Recent Blocks</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex items-center justify-between text-sm text-muted-foreground mb-2">
          <span>{isLoading ? "Loading blocks..." : error ? "Error loading recent blocks" : `${blocks.length} rows`}</span>
        </div>
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
            {blocks.length === 0 && !isLoading ? (
              <TableRow>
                <TableCell colSpan={4} className="text-center text-muted-foreground py-6">
                  No recent blocks found.
                </TableCell>
              </TableRow>
            ) : null}
            {blocks.map((block) => {
              const timestamp = new Date((block.timestamp > 1_000_000_000_000 ? block.timestamp : block.timestamp * 1000)).toLocaleString()
              const hashLabel = block.hash ? `${block.hash.slice(0, 12)}…` : `Height ${block.height}`
              return (
                <TableRow key={`${block.hash || block.height}`} className="hover:bg-accent/5">
                  <TableCell className="font-mono text-sm text-foreground">{hashLabel}</TableCell>
                  <TableCell className="text-muted-foreground">{timestamp}</TableCell>
                  <TableCell className="text-foreground">{block.proposer || "—"}</TableCell>
                  <TableCell>
                    <Badge className="bg-status-healthy text-white">Committed</Badge>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
