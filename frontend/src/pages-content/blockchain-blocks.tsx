"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { Activity, AlertCircle, Loader2, RefreshCw, Search } from "lucide-react"
import Link from "next/link"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { PageContainer } from "@/components/page-container"
import type { BlockSummary } from "@/lib/api"

interface ApiResponse {
  blocks: BlockSummary[]
  pagination: {
    start: number
    limit: number
    total: number
    next?: string
  }
}

const PAGE_SIZE_OPTIONS = [10, 25, 50, 100]

export function BlocksExplorerContent() {
  const [rows, setRows] = useState<BlockSummary[]>([])
  const [pagination, setPagination] = useState<ApiResponse["pagination"] | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [start, setStart] = useState(0)
  const [limit, setLimit] = useState(25)
  const [searchQuery, setSearchQuery] = useState("")

  const fetchPage = useCallback(
    async (startValue: number, limitValue: number) => {
      setIsLoading(true)
      setError(null)
      try {
        const params = new URLSearchParams({ start: String(startValue), limit: String(limitValue) })
        const res = await fetch(`/api/blocks?${params.toString()}`, { cache: "no-store" })
        if (!res.ok) {
          const message = await res.text().catch(() => res.statusText)
          throw new Error(message || `Failed to load blocks (${res.status})`)
        }
        const data = (await res.json()) as ApiResponse
        setRows(data.blocks)
        setPagination(data.pagination)
        setStart(startValue)
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load blocks")
        setRows([])
        setPagination(null)
      } finally {
        setIsLoading(false)
      }
    },
    [],
  )

  useEffect(() => {
    void fetchPage(0, limit)
  }, [fetchPage, limit])

  const filteredRows = useMemo(() => {
    if (!searchQuery.trim()) {
      return rows
    }
    const query = searchQuery.trim().toLowerCase()
    return rows.filter((block) => {
      const matchesHeight = block.height.toString().includes(query)
      const matchesHash = block.hash.toLowerCase().includes(query)
      const matchesProposer = block.proposer.toLowerCase().includes(query)
      return matchesHeight || matchesHash || matchesProposer
    })
  }, [rows, searchQuery])

  const handleNext = () => {
    if (pagination?.next) {
      const nextUrl = new URL(pagination.next, "https://placeholder.local")
      const nextStart = Number.parseInt(nextUrl.searchParams.get("start") ?? `${start + limit}`, 10)
      void fetchPage(Number.isNaN(nextStart) ? start + limit : nextStart, limit)
    } else if (pagination) {
      const candidate = pagination.start + pagination.limit
      if (candidate <= pagination.total) {
        void fetchPage(candidate, limit)
      }
    }
  }

  const handlePrev = () => {
    if (!pagination) return
    const previousStart = Math.max(0, pagination.start - limit)
    if (previousStart !== pagination.start) {
      void fetchPage(previousStart, limit)
    }
  }

  return (
    <PageContainer align="left" className="flex flex-col gap-8 py-6 lg:py-8">
      <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-border/60 bg-gradient-to-br from-primary/20 to-accent/20">
            <Activity className="h-5 w-5 text-primary" />
          </div>
          <div>
            <h1 className="text-3xl font-bold text-foreground">Block Explorer</h1>
            <p className="text-sm text-muted-foreground">Deep dive into committed blocks with filters and pagination.</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" asChild>
            <Link href="/blockchain">Back to dashboard</Link>
          </Button>
          <Button variant="outline" size="sm" onClick={() => void fetchPage(start, limit)}>
            <RefreshCw className="mr-2 h-4 w-4" /> Refresh
          </Button>
        </div>
      </header>

      <Card className="border border-border/30 bg-card/20 p-4 space-y-4">
        <div className="grid gap-3 md:grid-cols-[2fr,1fr,1fr] md:items-end">
          <div>
            <Label htmlFor="search" className="text-xs uppercase tracking-wide text-muted-foreground">
              Filter blocks
            </Label>
            <div className="mt-1.5 flex items-center gap-2 rounded-lg border border-border/40 bg-background/60 px-3 py-1.5">
              <Search className="h-4 w-4 text-muted-foreground" />
              <Input
                id="search"
                value={searchQuery}
                onChange={(event) => setSearchQuery(event.target.value)}
                placeholder="Search height, hash, or proposer"
                className="h-7 border-0 bg-transparent px-0 text-sm focus-visible:ring-0"
              />
            </div>
          </div>
          <div>
            <Label className="text-xs uppercase tracking-wide text-muted-foreground">Page size</Label>
            <Select value={String(limit)} onValueChange={(value) => setLimit(Number.parseInt(value, 10))}>
              <SelectTrigger className="h-8 border-border/40 bg-background/60 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PAGE_SIZE_OPTIONS.map((size) => (
                  <SelectItem key={size} value={String(size)}>
                    {size} rows
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label className="text-xs uppercase tracking-wide text-muted-foreground">Current window</Label>
            <div className="mt-1.5 text-sm text-muted-foreground">
              {pagination ? `Heights ${pagination.start} – ${pagination.start + rows.length - 1}` : "--"}
            </div>
          </div>
        </div>
        {error ? (
          <div className="flex items-center gap-2 rounded-lg border border-status-critical/40 bg-status-critical/10 px-3 py-2 text-sm text-status-critical">
            <AlertCircle className="h-4 w-4" />
            {error}
          </div>
        ) : null}
      </Card>

      <Card className="border border-border/30 bg-card/40">
        <CardHeader className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
          <div>
            <CardTitle className="text-lg text-foreground">Blocks</CardTitle>
            <p className="text-sm text-muted-foreground">
              Showing {filteredRows.length.toLocaleString()} of {rows.length.toLocaleString()} loaded rows. Total chain height: {pagination?.total.toLocaleString() ?? "--"}
            </p>
          </div>
          {isLoading ? (
            <Badge variant="outline" className="flex items-center gap-1 text-xs">
              <Loader2 className="h-3 w-3 animate-spin" /> Loading…
            </Badge>
          ) : null}
        </CardHeader>
        <CardContent className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-xs uppercase tracking-wide text-muted-foreground">
              <tr className="border-b border-border/40 text-left">
                <th className="px-3 py-2">Height</th>
                <th className="px-3 py-2">Hash</th>
                <th className="px-3 py-2">Proposer</th>
                <th className="px-3 py-2">Timestamp</th>
                <th className="px-3 py-2 text-right">Txs</th>
                <th className="px-3 py-2 text-right">Anomalies</th>
              </tr>
            </thead>
            <tbody>
              {filteredRows.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-3 py-8 text-center text-muted-foreground">
                    {isLoading ? "Loading blocks…" : "No blocks match your filters."}
                  </td>
                </tr>
              ) : (
                filteredRows.map((block) => (
                  <tr key={block.hash} className="border-b border-border/20">
                    <td className="px-3 py-2 font-mono text-xs">{block.height.toLocaleString()}</td>
                    <td className="px-3 py-2 font-mono text-xs break-all">{block.hash}</td>
                    <td className="px-3 py-2 font-mono text-xs break-all">{block.proposer}</td>
                    <td className="px-3 py-2">
                      {new Date(block.timestamp * 1000).toLocaleString()}
                    </td>
                    <td className="px-3 py-2 text-right">{block.transaction_count.toLocaleString()}</td>
                    <td className="px-3 py-2 text-right">{(block.anomaly_count ?? 0).toLocaleString()}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </CardContent>
      </Card>

      <div className="flex items-center justify-between">
        <Button variant="outline" size="sm" onClick={handlePrev} disabled={isLoading || start === 0}>
          Previous
        </Button>
        <div className="text-xs text-muted-foreground">
          Window starting at {start.toLocaleString()} of approximately {pagination?.total.toLocaleString() ?? "--"} blocks
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={handleNext}
          disabled={isLoading || (pagination ? pagination.start + pagination.limit > pagination.total : true)}
        >
          Next
        </Button>
      </div>
    </PageContainer>
  )
}
