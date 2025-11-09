"use client"

import { useMemo } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { CheckCircle2, XCircle, Loader2, Clock, AlertCircle } from "lucide-react"

import { useDashboardData } from "@/hooks/use-dashboard-data"

interface SnapshotTest {
  name: string
  target: string
  status: "pending" | "success" | "error" | "warning"
  message?: string
  latency?: number
}

const REFRESH_INTERVAL_MS = 15000

function normalizeStatus(status?: string | null): SnapshotTest["status"] {
  if (!status) return "warning"
  const lower = status.toLowerCase()
  if (lower === "ok" || lower === "healthy") {
    return "success"
  }
  if (lower === "single_node" || lower === "genesis" || lower === "degraded" || lower === "not configured") {
    return "warning"
  }
  return "error"
}

export function ApiHealthMonitor() {
  const { data: dashboard, isLoading, error } = useDashboardData(REFRESH_INTERVAL_MS)

  const tests = useMemo<SnapshotTest[]>(() => {
    if (isLoading && !dashboard) {
      return [
        { name: "Dashboard Snapshot", target: "/api/dashboard/overview", status: "pending" },
        { name: "Backend Health", target: "backend.health", status: "pending" },
        { name: "Consensus Payload", target: "dashboard.consensus", status: "pending" },
        { name: "Blocks Feed", target: "dashboard.blocks.recent", status: "pending" },
        { name: "Threat Feed", target: "dashboard.threats.feed", status: "pending" },
        { name: "AI Metrics", target: "dashboard.ai.metrics", status: "pending" },
      ]
    }

    if (!dashboard) {
      return [
        {
          name: "Dashboard Snapshot",
          target: "/api/dashboard/overview",
          status: "error",
          message: error instanceof Error ? error.message : "Snapshot unavailable",
        },
      ]
    }

    const latency = Date.now() - dashboard.timestamp

    return [
      {
        name: "Dashboard Snapshot",
        target: "/api/dashboard/overview",
        status: "success",
        latency,
        message: `Age ${(latency / 1000).toFixed(1)}s`,
      },
      {
        name: "Backend Health",
        target: "backend.health",
        status: normalizeStatus(dashboard.backend.health.status),
        message: dashboard.backend.health.status,
      },
      {
        name: "Consensus Payload",
        target: "dashboard.consensus",
        status: dashboard.consensus ? "success" : "error",
        message: dashboard.consensus
          ? `Leader ${dashboard.consensus.leader ?? dashboard.consensus.leader_id ?? "n/a"}`
          : "Missing",
      },
      {
        name: "Ledger Snapshot",
        target: "dashboard.ledger",
        status: dashboard.ledger ? "success" : "warning",
        message: dashboard.ledger
          ? `Height ${dashboard.ledger.latest_height.toLocaleString()} • Version ${dashboard.ledger.state_version.toLocaleString()}`
          : "No ledger snapshot",
      },
      {
        name: "Blocks Feed",
        target: "dashboard.blocks.recent",
        status: dashboard.blocks.recent && dashboard.blocks.recent.length > 0 ? "success" : "error",
        message:
          dashboard.blocks.recent && dashboard.blocks.recent.length > 0
            ? `${dashboard.blocks.recent.length} blocks cached`
            : "No blocks",
      },
      {
        name: "Threat Feed",
        target: "dashboard.threats.feed",
        status: dashboard.threats.feed && dashboard.threats.feed.length > 0 ? "success" : "warning",
        message:
          dashboard.threats.feed && dashboard.threats.feed.length > 0
            ? `${dashboard.threats.feed.length} anomalies`
            : "No anomalies in snapshot",
      },
      {
        name: "AI Metrics",
        target: "dashboard.ai.metrics",
        status: dashboard.ai.metrics ? normalizeStatus(dashboard.ai.metrics.status ?? "ok") : "warning",
        message: dashboard.ai.metrics ? dashboard.ai.metrics.message ?? "Available" : "Metrics unavailable",
      },
    ]
  }, [dashboard, error, isLoading])

  const successCount = tests.filter((t) => t.status === "success").length
  const errorCount = tests.filter((t) => t.status === "error").length
  const pendingCount = tests.filter((t) => t.status === "pending").length
  const warningCount = tests.filter((t) => t.status === "warning").length
  const avgLatencySamples = tests.filter((t) => typeof t.latency === "number")
  const avgLatencyValue = avgLatencySamples.length
    ? avgLatencySamples.reduce((sum, t) => sum + (t.latency ?? 0), 0) / avgLatencySamples.length
    : undefined

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle>API Health Monitor</CardTitle>
            <CardDescription>Snapshot-driven validation of consolidated telemetry</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Badge
              variant={
                errorCount === 0 && pendingCount === 0 && warningCount === 0
                  ? "default"
                  : errorCount > 0
                    ? "destructive"
                    : "outline"
              }
            >
              {successCount}/{tests.length} Healthy
            </Badge>
            {avgLatencyValue !== undefined && (
              <Badge variant="outline">
                <Clock className="mr-1 h-3 w-3" /> {avgLatencyValue.toFixed(0)}ms
              </Badge>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {tests.map((test, idx) => (
            <div key={idx} className="flex items-center justify-between rounded-lg border bg-card p-3">
              <div className="flex items-center gap-3">
                {test.status === "pending" && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
                {test.status === "success" && <CheckCircle2 className="h-4 w-4 text-green-500" />}
                {test.status === "error" && <XCircle className="h-4 w-4 text-red-500" />}
                {test.status === "warning" && <AlertCircle className="h-4 w-4 text-yellow-500" />}
                <div>
                  <div className="font-medium">{test.name}</div>
                  <div className="text-xs text-muted-foreground">{test.target}</div>
                </div>
              </div>

              <div className="flex items-center gap-3 text-sm text-muted-foreground">
                {test.latency !== undefined && (
                  <div className="flex items-center gap-1">
                    <Clock className="h-3 w-3" />
                    {test.latency.toFixed(0)}ms age
                  </div>
                )}
                {test.message && <span>{test.message}</span>}
              </div>
            </div>
          ))}
        </div>

        {errorCount === 0 && pendingCount === 0 && warningCount === 0 ? (
          <div className="mt-4 rounded-lg border border-green-500/20 bg-green-500/10 p-3">
            <div className="flex items-center gap-2 text-green-600 dark:text-green-400">
              <CheckCircle2 className="h-4 w-4" />
              <span className="font-medium">All consolidated APIs are healthy.</span>
            </div>
          </div>
        ) : null}

        {warningCount > 0 && errorCount === 0 ? (
          <div className="mt-4 rounded-lg border border-yellow-500/20 bg-yellow-500/10 p-3">
            <div className="flex items-center gap-2 text-yellow-600 dark:text-yellow-400">
              <AlertCircle className="h-4 w-4" />
              <span className="font-medium">{warningCount} check(s) returned warnings</span>
            </div>
          </div>
        ) : null}

        {errorCount > 0 && (
          <div className="mt-4 rounded-lg border border-red-500/20 bg-red-500/10 p-3">
            <div className="flex items-center gap-2 text-red-600 dark:text-red-400">
              <XCircle className="h-4 w-4" />
              <span className="font-medium">{errorCount} check(s) failing</span>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
