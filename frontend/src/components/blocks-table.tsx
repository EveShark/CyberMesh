"use client"

import Link from "next/link"
import { ArrowRight } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import type { BlockSummary } from "@/lib/api"

interface BlocksTableProps {
  blocks: BlockSummary[]
  isLoading?: boolean
  error?: unknown
}

export default function BlocksTable({ blocks, isLoading, error }: BlocksTableProps) {
  return (
    <Card className="glass-card border border-border/30">
      <CardHeader>
        <CardTitle className="flex items-center justify-between text-lg text-foreground">
          Latest Blocks
          <span className="text-xs font-normal text-muted-foreground">
            {isLoading ? "Loading..." : error ? "Error" : `${blocks.length} rows`}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="text-left text-muted-foreground border-b">
            <tr>
              <th className="py-2 pr-4">Time</th>
              <th className="py-2 pr-4">Height</th>
              <th className="py-2 pr-4">Hash</th>
              <th className="py-2 pr-4">Transactions</th>
              <th className="py-2 pr-4">Size (bytes)</th>
              <th className="py-2 pr-4">Proposer</th>
            </tr>
          </thead>
          <tbody>
            {blocks.length === 0 && !isLoading && !error ? (
              <tr>
                <td className="py-4 text-muted-foreground" colSpan={6}>
                  No blocks found.
                </td>
              </tr>
            ) : (
              blocks.map((block) => (
                <tr key={block.hash} className="border-b last:border-b-0">
                  <td className="py-2 pr-4 text-muted-foreground">
                    {new Date((block.timestamp > 1_000_000_000_000 ? block.timestamp : block.timestamp * 1000)).toLocaleString()}
                  </td>
                  <td className="py-2 pr-4 text-foreground">{block.height}</td>
                  <td className="py-2 pr-4">
                    <code className="text-xs text-foreground">{block.hash.slice(0, 10)}…</code>
                  </td>
                  <td className="py-2 pr-4">
                    <span className="text-foreground">{block.transaction_count ?? block.transactions?.length ?? 0}</span>
                  </td>
                  <td className="py-2 pr-4">
                    <span className="text-foreground">{block.size_bytes}</span>
                  </td>
                  <td className="py-2 pr-4 text-muted-foreground">{block.proposer}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <div className="mt-4 pt-3 border-t border-border">
        <Link
          href="/blockchain/blocks"
          className="flex items-center justify-center gap-2 text-sm text-primary hover:text-primary/80 transition-colors"
        >
          View All Blocks
          <ArrowRight className="h-3 w-3" />
        </Link>
      </div>
      </CardContent>
    </Card>
  )
}
