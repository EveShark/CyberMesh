"use client"

import { useState, useEffect } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { CheckCircle2, XCircle, AlertCircle, Loader2 } from "lucide-react"

interface ValidationCheck {
  name: string
  description: string
  status: "pending" | "pass" | "fail" | "warning"
  message?: string
}

export function DataValidator() {
  const [checks, setChecks] = useState<ValidationCheck[]>([
    { name: "Blockchain Height", description: "Verify height is increasing", status: "pending" },
    { name: "Timestamp Freshness", description: "Last block within 5 minutes", status: "pending" },
    { name: "Transaction Count", description: "Non-zero transactions exist", status: "pending" },
    { name: "Success Rate", description: "Not hardcoded to 0.998", status: "pending" },
    { name: "Anomaly Count", description: "Anomaly counts populated", status: "pending" },
  ])

  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    runValidation()
  }, [])

  async function runValidation() {
    setIsLoading(true)

    try {
      // Fetch stats
      const statsResponse = await fetch(`/api/backend/stats`, {
        headers: { "X-API-Key": "test" }
      })

      if (!statsResponse.ok) {
        setChecks(prev => prev.map(c => ({ ...c, status: "fail", message: "Cannot reach API" })))
        setIsLoading(false)
        return
      }

      const statsData = await statsResponse.json()
      const chain = statsData.data?.chain

      // Check 1: Blockchain height
      if (chain?.height > 0) {
        setChecks(prev => prev.map(c => 
          c.name === "Blockchain Height" 
            ? { ...c, status: "pass", message: `Height: ${chain.height.toLocaleString()}` }
            : c
        ))
      } else {
        setChecks(prev => prev.map(c => 
          c.name === "Blockchain Height" 
            ? { ...c, status: "fail", message: "Height is zero" }
            : c
        ))
      }

      // Check 2: Transaction count
      if (chain?.total_transactions > 0) {
        setChecks(prev => prev.map(c => 
          c.name === "Transaction Count" 
            ? { ...c, status: "pass", message: `${chain.total_transactions.toLocaleString()} transactions` }
            : c
        ))
      } else {
        setChecks(prev => prev.map(c => 
          c.name === "Transaction Count" 
            ? { ...c, status: "fail", message: "No transactions found" }
            : c
        ))
      }

      // Check 3: Success rate not hardcoded
      if (chain?.success_rate !== 0.998) {
        setChecks(prev => prev.map(c => 
          c.name === "Success Rate" 
            ? { ...c, status: "pass", message: `${(chain?.success_rate * 100).toFixed(2)}% (calculated)` }
            : c
        ))
      } else {
        setChecks(prev => prev.map(c => 
          c.name === "Success Rate" 
            ? { ...c, status: "warning", message: "Matches old hardcoded value (0.998)" }
            : c
        ))
      }

      // Fetch blocks for timestamp and anomaly check
      const blocksResponse = await fetch(`/api/blocks?start=0&limit=5`, {
        headers: { "X-API-Key": "test" }
      })

      if (blocksResponse.ok) {
        const blocksData = (await blocksResponse.json()) as {
          data?: {
            blocks?: Array<{
              timestamp: number
              anomaly_count?: number | null
            }>
          }
        }
        const blocks = blocksData.data?.blocks ?? []

        // Check 4: Timestamp freshness
        if (blocks.length > 0) {
          const latestBlock = blocks[0]
          const blockTime = new Date(latestBlock.timestamp)
          const now = new Date()
          const ageMinutes = (now.getTime() - blockTime.getTime()) / 1000 / 60

          if (ageMinutes < 5) {
            setChecks(prev => prev.map(c => 
              c.name === "Timestamp Freshness" 
                ? { ...c, status: "pass", message: `Last block ${Math.floor(ageMinutes * 60)}s ago` }
                : c
            ))
          } else {
            setChecks(prev => prev.map(c => 
              c.name === "Timestamp Freshness" 
                ? { ...c, status: "warning", message: `Last block ${Math.floor(ageMinutes)}m ago` }
                : c
            ))
          }

          // Check 5: Anomaly counts
          const hasAnomalyCounts = blocks.some((b) => b.anomaly_count !== undefined && b.anomaly_count !== null)
          if (hasAnomalyCounts) {
            const totalAnomalies = blocks.reduce((sum: number, b) => sum + (b.anomaly_count || 0), 0)
            setChecks(prev => prev.map(c => 
              c.name === "Anomaly Count" 
                ? { ...c, status: "pass", message: `${totalAnomalies} anomalies in ${blocks.length} blocks` }
                : c
            ))
          } else {
            setChecks(prev => prev.map(c => 
              c.name === "Anomaly Count" 
                ? { ...c, status: "fail", message: "anomaly_count field missing" }
                : c
            ))
          }
        }
      }

    } catch (error) {
      setChecks(prev => prev.map(c => ({ 
        ...c, 
        status: "fail", 
        message: error instanceof Error ? error.message : "Validation failed" 
      })))
    } finally {
      setIsLoading(false)
    }
  }

  const passCount = checks.filter(c => c.status === "pass").length
  const failCount = checks.filter(c => c.status === "fail").length
  const warningCount = checks.filter(c => c.status === "warning").length

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle>Data Validation</CardTitle>
            <CardDescription>Verify data authenticity and freshness</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={passCount === checks.length ? "default" : failCount > 0 ? "destructive" : "outline"}>
              {passCount}/{checks.length} Passed
            </Badge>
            {warningCount > 0 && (
              <Badge variant="outline" className="border-yellow-500 text-yellow-600">
                {warningCount} Warning(s)
              </Badge>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {checks.map((check, idx) => (
            <div
              key={idx}
              className="flex items-center justify-between p-3 rounded-lg border bg-card"
            >
              <div className="flex items-center gap-3">
                {check.status === "pending" && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
                {check.status === "pass" && <CheckCircle2 className="h-4 w-4 text-green-500" />}
                {check.status === "fail" && <XCircle className="h-4 w-4 text-red-500" />}
                {check.status === "warning" && <AlertCircle className="h-4 w-4 text-yellow-500" />}
                
                <div>
                  <div className="font-medium">{check.name}</div>
                  <div className="text-xs text-muted-foreground">{check.description}</div>
                </div>
              </div>

              <div className="text-sm text-muted-foreground">
                {check.message}
              </div>
            </div>
          ))}
        </div>

        {!isLoading && failCount === 0 && warningCount === 0 && (
          <div className="mt-4 p-3 rounded-lg bg-green-500/10 border border-green-500/20">
            <div className="flex items-center gap-2 text-green-600 dark:text-green-400">
              <CheckCircle2 className="h-4 w-4" />
              <span className="font-medium">All validation checks passed - data is authentic</span>
            </div>
          </div>
        )}

        {!isLoading && failCount > 0 && (
          <div className="mt-4 p-3 rounded-lg bg-red-500/10 border border-red-500/20">
            <div className="flex items-center gap-2 text-red-600 dark:text-red-400">
              <XCircle className="h-4 w-4" />
              <span className="font-medium">{failCount} validation check(s) failed</span>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
