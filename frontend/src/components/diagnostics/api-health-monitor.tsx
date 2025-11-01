"use client"

import { useState, useEffect } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { CheckCircle2, XCircle, Loader2, Clock } from "lucide-react"

interface EndpointTest {
  name: string
  endpoint: string
  method: string
  requiresAuth: boolean
  status: "pending" | "success" | "error"
  statusCode?: number
  responseTime?: number
  error?: string
}

const DEFAULT_TESTS: EndpointTest[] = [
  { name: "Health Check", endpoint: "/api/v1/health", method: "GET", requiresAuth: false, status: "pending" },
  { name: "Stats", endpoint: "/api/v1/stats", method: "GET", requiresAuth: true, status: "pending" },
  { name: "Blocks", endpoint: "/api/v1/blocks?start=0&limit=5", method: "GET", requiresAuth: true, status: "pending" },
  { name: "Anomalies", endpoint: "/api/v1/anomalies?limit=5", method: "GET", requiresAuth: true, status: "pending" },
  { name: "Anomaly Stats", endpoint: "/api/v1/anomalies/stats", method: "GET", requiresAuth: true, status: "pending" },
]

export function ApiHealthMonitor() {
  const [tests, setTests] = useState<EndpointTest[]>(() => DEFAULT_TESTS.map((test) => ({ ...test })))

  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    let isCancelled = false

    const run = async () => {
      setIsLoading(true)

      for (let i = 0; i < DEFAULT_TESTS.length; i++) {
        if (isCancelled) {
          break
        }

        const test = DEFAULT_TESTS[i]
        const startTime = Date.now()

        try {
          const headers: HeadersInit = {
            "Content-Type": "application/json",
          }

          if (test.requiresAuth) {
            headers["X-API-Key"] = "test"
          }

          const response = await fetch(`/api/backend${test.endpoint.replace("/api/v1", "")}`, {
            method: test.method,
            headers,
          })

          const responseTime = Date.now() - startTime

          setTests((prev) =>
            prev.map((t, idx) =>
              idx === i
                ? {
                    ...t,
                    status: response.ok ? "success" : "error",
                    statusCode: response.status,
                    responseTime,
                    error: response.ok ? undefined : `HTTP ${response.status}`,
                  }
                : t,
            ),
          )
        } catch (error) {
          const responseTime = Date.now() - startTime

          setTests((prev) =>
            prev.map((t, idx) =>
              idx === i
                ? {
                    ...t,
                    status: "error",
                    responseTime,
                    error: error instanceof Error ? error.message : "Network error",
                  }
                : t,
            ),
          )
        }

        // Small delay between tests
        await new Promise((resolve) => setTimeout(resolve, 100))
      }

      if (!isCancelled) {
        setIsLoading(false)
      }
    }

    void run()

    return () => {
      isCancelled = true
    }
  }, [])

  const successCount = tests.filter(t => t.status === "success").length
  const errorCount = tests.filter(t => t.status === "error").length
  const avgResponseTime = tests
    .filter(t => t.responseTime)
    .reduce((sum, t) => sum + (t.responseTime || 0), 0) / tests.filter(t => t.responseTime).length

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle>API Health Monitor</CardTitle>
            <CardDescription>Real-time endpoint testing and response time tracking</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={successCount === tests.length ? "default" : "destructive"}>
              {successCount}/{tests.length} Healthy
            </Badge>
            {!isLoading && avgResponseTime && (
              <Badge variant="outline">
                Avg: {avgResponseTime.toFixed(0)}ms
              </Badge>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {tests.map((test, idx) => (
            <div
              key={idx}
              className="flex items-center justify-between p-3 rounded-lg border bg-card"
            >
              <div className="flex items-center gap-3">
                {test.status === "pending" && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
                {test.status === "success" && <CheckCircle2 className="h-4 w-4 text-green-500" />}
                {test.status === "error" && <XCircle className="h-4 w-4 text-red-500" />}
                
                <div>
                  <div className="font-medium">{test.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {test.method} {test.endpoint}
                  </div>
                </div>
              </div>

              <div className="flex items-center gap-3">
                {test.statusCode && (
                  <Badge variant={test.status === "success" ? "default" : "destructive"}>
                    {test.statusCode}
                  </Badge>
                )}
                {test.responseTime && (
                  <div className="flex items-center gap-1 text-sm text-muted-foreground">
                    <Clock className="h-3 w-3" />
                    {test.responseTime}ms
                  </div>
                )}
                {test.error && (
                  <div className="text-sm text-red-500">{test.error}</div>
                )}
              </div>
            </div>
          ))}
        </div>

        {!isLoading && errorCount === 0 && (
          <div className="mt-4 p-3 rounded-lg bg-green-500/10 border border-green-500/20">
            <div className="flex items-center gap-2 text-green-600 dark:text-green-400">
              <CheckCircle2 className="h-4 w-4" />
              <span className="font-medium">All API endpoints are operational</span>
            </div>
          </div>
        )}

        {!isLoading && errorCount > 0 && (
          <div className="mt-4 p-3 rounded-lg bg-red-500/10 border border-red-500/20">
            <div className="flex items-center gap-2 text-red-600 dark:text-red-400">
              <XCircle className="h-4 w-4" />
              <span className="font-medium">{errorCount} endpoint(s) failing</span>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
